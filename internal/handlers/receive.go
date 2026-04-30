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

	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/prompt"
	"github.com/tingkai-c/localsend-cli/internal/trust"

	"github.com/tingkai-c/localsend-cli/internal/utils/clipboard"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
	"github.com/schollz/progressbar/v3"
)

var (
	sessionIDCounter = 0
	sessionMutex     sync.Mutex
	fileNames        = make(map[string]string) // 用于保存文件名
)

func PrepareReceive(w http.ResponseWriter, r *http.Request) {
	var req models.PrepareReceiveRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Infof("Received request from %s,device is %s", req.Info.Alias, req.Info.DeviceModel)

	if !authorizeIncoming(req.Info.Alias, req.Info.Fingerprint, req.Files) {
		// authorizeIncoming logs the reason; HTTP status was set there.
		writeAuthError(w, lastAuthErr())
		return
	}

	sessionMutex.Lock()
	sessionIDCounter++
	sessionID := fmt.Sprintf("session-%d", sessionIDCounter)
	sessionMutex.Unlock()

	files := make(map[string]string)
	for fileID, fileInfo := range req.Files {
		token := fmt.Sprintf("token-%s", fileID)
		files[fileID] = token

		// 保存文件名
		fileNames[fileID] = fileInfo.FileName

		if strings.HasSuffix(fileInfo.FileName, ".txt") {
			logger.Success("TXT file content preview:", string(fileInfo.Preview))
			clipboard.WriteToClipBoard(fileInfo.Preview)
		}
	}

	resp := models.PrepareReceiveResponse{
		SessionID: sessionID,
		Files:     files,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// authErr is set by authorizeIncoming so PrepareReceive can map the failure
// to the correct HTTP status without re-checking conditions inline. It's
// scoped to a single request via lastAuthErrKey on the goroutine — but Go
// has no such thing, so we just stash it on a per-request basis through a
// package-level value protected by the prompt mutex implicitly. This is
// fine: only one prompt may be active at a time, so the "in flight" auth
// failure is unique while it lives.
var (
	authErrMu sync.Mutex
	authErr   error
)

func setAuthErr(err error) {
	authErrMu.Lock()
	authErr = err
	authErrMu.Unlock()
}

func lastAuthErr() error {
	authErrMu.Lock()
	defer authErrMu.Unlock()
	err := authErr
	authErr = nil
	return err
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
//  3. No TTY → reject without prompting (avoids hanging headless servers).
//  4. Otherwise prompt the user; persist the fingerprint on "Always".
func authorizeIncoming(alias, fingerprint string, files map[string]models.FileInfo) bool {
	if config.ConfigData.QuickSave {
		return true
	}
	if trust.IsTrusted(fingerprint) {
		logger.Infof("Auto-accepting trusted fingerprint %s (%s)", shortFP(fingerprint), alias)
		return true
	}
	if !prompt.IsTTY() {
		logger.Warn("Rejecting incoming session: stdin is not a TTY and quick_save is OFF. Set quick_save: true (or trust this sender once interactively) to receive on this host.")
		setAuthErr(prompt.ErrNoTTY)
		return false
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
		setAuthErr(err)
		return false
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
		return true
	case prompt.Accept:
		return true
	default:
		setAuthErr(errors.New("user rejected"))
		return false
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

	// 使用 fileID 获取文件名
	fileName, ok := fileNames[fileID]
	if !ok {
		http.Error(w, "Invalid file ID", http.StatusBadRequest)
		return
	}

	// 生成文件路径，保留文件扩展名
	filePath := filepath.Join(config.ConfigData.OutputDir, fileName)
	// 创建文件夹（如果不存在）
	dir := filepath.Dir(filePath)
	err := os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		logger.Errorf("Error creating directory:", err)
		return
	}
	// 创建文件
	file, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		logger.Errorf("Error creating file:", err)
		return
	}
	defer file.Close()

	// 创建一个 context 来处理请求取消
	ctx := r.Context()

	// 创建文件后，获取文件大小
	contentLength := r.ContentLength

	// 创建进度条
	bar := progressbar.NewOptions64(
		contentLength,
		progressbar.OptionSetDescription(fmt.Sprintf("下载 %s", fileName)),
		progressbar.OptionSetWidth(15),
		progressbar.OptionShowBytes(true),
		progressbar.OptionThrottle(time.Second), // 降低刷新频率，减少闪烁
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(), // 完成时清除进度条
		progressbar.OptionSetRenderBlankState(true),
		progressbar.OptionSetPredictTime(true), // 预测剩余时间
		progressbar.OptionFullWidth(),          // 使用全宽显示
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "█", // 使用实心方块
			SaucerHead:    "█",
			SaucerPadding: "░", // 使用灰色方块作为背景
			BarStart:      "|",
			BarEnd:        "|",
		}),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
	)

	buffer := make([]byte, 2*1024*1024) // 2MB 缓冲区

	// 使用 channel 来处理传输完成或取消
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

	// 等待传输完成或取消
	select {
	case err := <-done:
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			logger.Errorf("Transfer error:", err)
			// 删除未完成的文件
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
