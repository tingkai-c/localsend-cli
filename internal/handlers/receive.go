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
	"sync/atomic"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/prompt"
	"github.com/tingkai-c/localsend-cli/internal/trust"

	"github.com/schollz/progressbar/v3"
	"github.com/tingkai-c/localsend-cli/internal/utils/clipboard"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
)

var (
	sessionIDCounter  = 0
	sessionMutex      sync.Mutex
	receiveSessions   = make(map[string]receiveSession)
	receiveSessionsMu sync.RWMutex
	dashboardActive   atomic.Bool
)

type receiveSession struct {
	fileNames map[string]string // fileID -> fileName
	tokens    map[string]string // fileID -> expected token
}

var errUserRejected = errors.New("user rejected")

// SetDashboardActive prevents HTTP receive handlers from opening a stdin
// approval prompt while the Bubble Tea dashboard owns the terminal.
func SetDashboardActive(active bool) {
	dashboardActive.Store(active)
}

func PrepareReceive(w http.ResponseWriter, r *http.Request) {
	var req models.PrepareReceiveRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Infof("Received request from %s,device is %s", req.Info.Alias, req.Info.DeviceModel)

	authErr := authorizeIncoming(req.Info.Alias, req.Info.Fingerprint, req.Files)
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
		fileNames: fileNames,
		tokens:    responseTokens,
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
	case errors.Is(err, prompt.ErrBusy):
		http.Error(w, "blocked by another session", http.StatusConflict)
	case errors.Is(err, prompt.ErrTimeout):
		http.Error(w, "approval timed out", http.StatusForbidden)
	case errors.Is(err, prompt.ErrNoTTY):
		http.Error(w, "receiver requires Quick Save for headless use", http.StatusForbidden)
	default:
		http.Error(w, "rejected", http.StatusForbidden)
	}
}

// authorizeIncoming decides whether the session is allowed in. Order of
// precedence (each step short-circuits acceptance):
//  1. Quick Save bypasses every check.
//  2. A previously-trusted fingerprint bypasses the prompt.
//  3. No TTY -> reject without prompting (avoids hanging headless servers).
//  4. Otherwise prompt the user; persist the fingerprint on "Always".
func authorizeIncoming(alias, fingerprint string, files map[string]models.FileInfo) error {
	if config.ConfigData.QuickSave {
		return nil
	}
	if trust.IsTrusted(fingerprint) {
		logger.Infof("Auto-accepting trusted fingerprint %s (%s)", shortFP(fingerprint), alias)
		return nil
	}
	if dashboardActive.Load() {
		logger.Warn("Rejecting incoming session: approval is required while the dashboard owns the terminal. Open Receive mode, enable Quick Save, or trust this sender first.")
		return prompt.ErrNoTTY
	}
	if !prompt.IsTTY() {
		logger.Warn("Rejecting incoming session: stdin is not a TTY and quick_save is OFF. Set quick_save: true (or trust this sender once interactively) to receive on this host.")
		return prompt.ErrNoTTY
	}

	summaries := make([]prompt.FileSummary, 0, len(files))
	for _, f := range files {
		summaries = append(summaries, prompt.FileSummary{Name: f.FileName, Size: f.Size})
	}

	ctx, cancel := context.WithTimeout(context.Background(), prompt.DefaultTimeout)
	defer cancel()
	decision, err := prompt.AskApproval(ctx, alias, fingerprint, summaries)
	if err != nil {
		logger.Infof("Approval prompt error: %v", err)
		return err
	}
	switch decision {
	case prompt.AcceptAlways:
		if err := trust.Add(fingerprint, alias); err != nil {
			// Persisting failed but the user said yes; honor the answer for
			// this session and warn so they can investigate.
			logger.Errorf("Failed to persist trust for %s: %v", alias, err)
		} else {
			logger.Infof("Trusted %s (%s) for future sessions", alias, shortFP(fingerprint))
		}
		return nil
	case prompt.Accept:
		return nil
	default:
		return errUserRejected
	}
}

func shortFP(fp string) string {
	if len(fp) > 12 {
		return fp[:12] + "…"
	}
	return fp
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

	defer func() {
		receiveSessionsMu.Lock()
		delete(receiveSessions, sessionID)
		receiveSessionsMu.Unlock()
	}()

	filePath := filepath.Join(config.ConfigData.OutputDir, fileName)
	dir := filepath.Dir(filePath)
	err := os.MkdirAll(dir, os.ModePerm)
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
	w.WriteHeader(http.StatusOK)
}
