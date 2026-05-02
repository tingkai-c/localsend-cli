package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/approval"
	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/history"
	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/prompt"
	"github.com/tingkai-c/localsend-cli/internal/trust"

	"github.com/schollz/progressbar/v3"
	"github.com/tingkai-c/localsend-cli/internal/utils/clipboard"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
)

var (
	sessionIDCounter   = 0
	sessionMutex       sync.Mutex
	receiveSessions    = make(map[string]receiveSession)
	receiveSessionsMu  sync.RWMutex
	approvalProviderMu sync.RWMutex
	approvalProvider   approval.Provider = approval.NewManager(approval.StdinProvider{}, prompt.DefaultTimeout)
)

type receiveSession struct {
	fileNames   map[string]string // fileID -> fileName
	tokens      map[string]string // fileID -> expected token
	peerAlias   string
	fingerprint string
}

// SetApprovalProvider replaces the interactive approval provider. Passing nil
// restores the stdin provider used by explicit CLI receive mode. TUI callers
// install a channel provider so HTTP handlers never read stdin while Bubble Tea
// owns the terminal.
func SetApprovalProvider(provider approval.Provider) {
	approvalProviderMu.Lock()
	defer approvalProviderMu.Unlock()
	if provider == nil {
		approvalProvider = approval.NewManager(approval.StdinProvider{}, prompt.DefaultTimeout)
		return
	}
	approvalProvider = approval.NewManager(provider, prompt.DefaultTimeout)
}

func currentApprovalProvider() approval.Provider {
	approvalProviderMu.RLock()
	defer approvalProviderMu.RUnlock()
	return approvalProvider
}

// SetDashboardActive is retained for callers compiled against older package
// versions. Approval routing is now controlled by SetApprovalProvider.
func SetDashboardActive(active bool) {}

func PrepareReceive(w http.ResponseWriter, r *http.Request) {
	var req models.PrepareReceiveRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Infof("Received request from %s,device is %s", req.Info.Alias, req.Info.DeviceModel)

	authErr := authorizeIncoming(r.Context(), req.Info.Alias, req.Info.Fingerprint, req.Files)
	if authErr != nil {
		// authorizeIncoming logs the reason; HTTP status was set there.
		writeAuthError(w, authErr)
		return
	}

	sessionMutex.Lock()
	sessionIDCounter++
	sessionID := fmt.Sprintf("session-%d", sessionIDCounter)
	sessionMutex.Unlock()

	responseTokens := make(map[string]string)
	fileNames := make(map[string]string)
	for fileID, fileInfo := range req.Files {
		token := fmt.Sprintf("token-%s", fileID)
		responseTokens[fileID] = token
		fileNames[fileID] = fileInfo.FileName

		if strings.HasSuffix(fileInfo.FileName, ".txt") {
			logger.Success("TXT file content preview:", string(fileInfo.Preview))
			clipboard.WriteToClipBoard(fileInfo.Preview)
		}
	}
	receiveSessionsMu.Lock()
	receiveSessions[sessionID] = receiveSession{
		fileNames:   fileNames,
		tokens:      responseTokens,
		peerAlias:   req.Info.Alias,
		fingerprint: req.Info.Fingerprint,
	}
	receiveSessionsMu.Unlock()

	resp := models.PrepareReceiveResponse{
		SessionID: sessionID,
		Files:     responseTokens,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, approval.ErrBusy), errors.Is(err, prompt.ErrBusy):
		http.Error(w, "blocked by another session", http.StatusConflict)
	case errors.Is(err, approval.ErrTimeout), errors.Is(err, prompt.ErrTimeout):
		http.Error(w, "approval timed out", http.StatusForbidden)
	case errors.Is(err, approval.ErrNoTTY), errors.Is(err, approval.ErrNoProvider), errors.Is(err, prompt.ErrNoTTY):
		http.Error(w, "receiver requires Quick Save, trust, or TUI approval for headless use", http.StatusForbidden)
	default:
		http.Error(w, "rejected", http.StatusForbidden)
	}
}

// authorizeIncoming decides whether the session is allowed in. Order of
// precedence (each step short-circuits acceptance):
//  1. Quick Save bypasses every check.
//  2. A previously-trusted fingerprint bypasses the prompt.
//  3. The configured approval provider decides unknown senders.
//  4. No provider / no TTY / timeout rejects safely without hanging.
func authorizeIncoming(ctx context.Context, alias, fingerprint string, files map[string]models.FileInfo) error {
	request := approval.Request{
		Alias:       alias,
		Fingerprint: fingerprint,
		Files:       approvalFiles(files),
	}
	decision, err := (approval.Policy{
		QuickSave: config.ConfigData.QuickSave,
		Trust:     receiveTrustStore{},
		Provider:  currentApprovalProvider(),
		Timeout:   prompt.DefaultTimeout,
	}).Authorize(ctx, request)
	if err != nil {
		switch {
		case errors.Is(err, approval.ErrNoTTY), errors.Is(err, approval.ErrNoProvider):
			logger.Warn("Rejecting incoming session: approval is required but no interactive approval provider is available. Use TUI, open Receive mode with a TTY, enable Quick Save, or trust this sender first.")
		case errors.Is(err, approval.ErrTimeout):
			logger.Infof("Approval timed out for %s (%s)", alias, shortFP(fingerprint))
		case errors.Is(err, approval.ErrBusy):
			logger.Infof("Approval busy for %s (%s)", alias, shortFP(fingerprint))
		default:
			logger.Infof("Incoming transfer rejected for %s (%s): %v", alias, shortFP(fingerprint), err)
		}
		return err
	}
	switch decision.Action {
	case approval.AcceptAlways:
		logger.Infof("Trusted %s (%s) for future sessions", alias, shortFP(fingerprint))
	case approval.Accept:
		if decision.Reason == "trusted" {
			logger.Infof("Auto-accepting trusted fingerprint %s (%s)", shortFP(fingerprint), alias)
		}
	}
	return nil
}

type receiveTrustStore struct{}

func (receiveTrustStore) IsTrusted(fingerprint string) bool   { return trust.IsTrusted(fingerprint) }
func (receiveTrustStore) Add(fingerprint, alias string) error { return trust.Add(fingerprint, alias) }

func approvalFiles(files map[string]models.FileInfo) []approval.File {
	summaries := make([]approval.File, 0, len(files))
	for _, f := range files {
		summaries = append(summaries, approval.File{Name: f.FileName, Size: f.Size})
	}
	return summaries
}

func shortFP(fp string) string {
	if len(fp) > 12 {
		return fp[:12] + "…"
	}
	return fp
}

func safeOutputPath(outputDir, fileName string) (string, error) {
	if strings.TrimSpace(fileName) == "" {
		return "", fmt.Errorf("empty file name")
	}
	cleanName := filepath.Clean(fileName)
	if filepath.IsAbs(cleanName) || cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe file name")
	}
	base, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}
	target := filepath.Join(base, cleanName)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if targetAbs != base && !strings.HasPrefix(targetAbs, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("target escapes output dir")
	}
	return targetAbs, nil
}

func ReceiveHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	fileID := r.URL.Query().Get("fileId")
	token := r.URL.Query().Get("token")

	// 验证请求参数
	if sessionID == "" || fileID == "" || token == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	receiveSessionsMu.RLock()
	sessionData, ok := receiveSessions[sessionID]
	receiveSessionsMu.RUnlock()
	if !ok {
		http.Error(w, "invalid session", http.StatusBadRequest)
		return
	}
	expectedToken, ok := sessionData.tokens[fileID]
	if !ok || token != expectedToken {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	fileName, ok := sessionData.fileNames[fileID]
	if !ok {
		http.Error(w, "Invalid file ID", http.StatusBadRequest)
		return
	}

	filePath, err := safeOutputPath(config.ConfigData.OutputDir, fileName)
	if err != nil {
		http.Error(w, "Invalid file path", http.StatusBadRequest)
		logger.Errorf("Invalid receive path %q: %v", fileName, err)
		return
	}
	dir := filepath.Dir(filePath)
	err = os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		logger.Errorf("Error creating directory: %v", err)
		return
	}
	file, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		logger.Errorf("Error creating file: %v", err)
		return
	}
	defer file.Close()

	ctx := r.Context()
	contentLength := r.ContentLength

	bar := progressbar.NewOptions64(
		contentLength,
		progressbar.OptionSetDescription(fmt.Sprintf("Download %s", fileName)),
		progressbar.OptionSetWidth(15),
		progressbar.OptionShowBytes(true),
		progressbar.OptionThrottle(time.Second),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█",
			SaucerHead:    "█",
			SaucerPadding: "░",
			BarStart:      "|",
			BarEnd:        "|",
		}),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
	)

	buffer := make([]byte, 2*1024*1024)
	done := make(chan error, 1)

	go func() {
		for {
			n, err := r.Body.Read(buffer)
			if err != nil && err != io.EOF {
				done <- fmt.Errorf("Read file failed: %w", err)
				return
			}
			if n == 0 {
				done <- nil
				return
			}

			_, err = file.Write(buffer[:n])
			if err != nil {
				done <- fmt.Errorf("Write file failed: %w", err)
				return
			}

			bar.Add(n)
		}
	}()

	select {
	case err := <-done:
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			logger.Errorf("Transfer error: %v", err)
			os.Remove(filePath)
			return
		}
	case <-ctx.Done():
		// 请求被取消
		logger.Info("Transfer canceled by client")
		// 删除未完成的文件
		os.Remove(filePath)
		// 关闭连接
		if conn, ok := w.(http.CloseNotifier); ok {
			conn.CloseNotify()
		}
		return
	}

	logger.Success("File saved to:", filePath)
	if _, err := history.Add(history.Record{
		Direction:       history.DirectionReceived,
		Status:          history.StatusCompleted,
		FileName:        fileName,
		Path:            filePath,
		Size:            contentLength,
		PeerAlias:       sessionData.peerAlias,
		PeerFingerprint: sessionData.fingerprint,
	}); err != nil {
		logger.Warnf("Failed to record receive history: %v", err)
	}
	receiveSessionsMu.Lock()
	sessionData = receiveSessions[sessionID]
	delete(sessionData.tokens, fileID)
	delete(sessionData.fileNames, fileID)
	if len(sessionData.tokens) == 0 {
		delete(receiveSessions, sessionID)
	} else {
		receiveSessions[sessionID] = sessionData
	}
	receiveSessionsMu.Unlock()
	w.WriteHeader(http.StatusOK)
}
