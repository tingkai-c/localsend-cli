package handlers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/discovery"
	"github.com/tingkai-c/localsend-cli/internal/discovery/shared"
	"github.com/tingkai-c/localsend-cli/internal/history"
	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/transfer"
	"github.com/tingkai-c/localsend-cli/internal/tui"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
	"github.com/tingkai-c/localsend-cli/internal/utils/sha256"
)

// SendFileToOtherDevicePrepare 函数
func SendFileToOtherDevicePrepare(ip string, path string) (*models.PrepareReceiveResponse, error) {
	// 准备所有文件的元数据
	files := make(map[string]models.FileInfo)
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			sha256Hash, err := sha256.CalculateSHA256(filePath)
			if err != nil {
				return fmt.Errorf("error calculating SHA256 hash: %w", err)
			}
			fileID, err := fileIDForPath(path, filePath)
			if err != nil {
				return err
			}
			fileMetadata := models.FileInfo{
				ID:       fileID,
				FileName: fileID,
				Size:     info.Size(),
				FileType: filepath.Ext(filePath),
				SHA256:   sha256Hash,
			}
			if fileMetadata.FileType == ".txt" && info.Size() <= 64*1024 {
				if preview, err := os.ReadFile(filePath); err == nil {
					fileMetadata.Preview = string(preview)
				}
			}
			files[fileMetadata.ID] = fileMetadata
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("error walking the path: %w", err)
	}

	// 创建并填充 PrepareReceiveRequest 结构体
	request := models.PrepareReceiveRequest{
		Info: models.Info{
			Alias:       shared.Message.Alias,
			Version:     shared.Message.Version,
			DeviceModel: shared.Message.DeviceModel,
			DeviceType:  shared.Message.DeviceType,
			Fingerprint: shared.Message.Fingerprint,
			Port:        shared.Message.Port,
			Protocol:    shared.Message.Protocol,
			Download:    shared.Message.Download,
		},
		Files: files,
	}

	// 将请求结构体编码为JSON
	requestJson, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("error encoding request to JSON: %w", err)
	}

	// 发送POST请求
	url := fmt.Sprintf("https://%s:53317/api/localsend/v2/prepare-upload", ip)
	client := &http.Client{
		Timeout: 60 * time.Second, // 传输超时
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // 忽略TLS
			},
		},
	}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(requestJson))
	if err != nil {
		return nil, fmt.Errorf("error sending POST request: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 204:
			return nil, fmt.Errorf("finished (No file transfer needed)")
		case 400:
			return nil, fmt.Errorf("invalid body")
		case 403:
			return nil, fmt.Errorf("rejected")
		case 500:
			return nil, fmt.Errorf("unknown error by receiver")
		}
		return nil, fmt.Errorf("failed to send metadata: received status code %d", resp.StatusCode)
	}

	// 解码响应JSON为PrepareReceiveResponse结构体
	var prepareReceiveResponse models.PrepareReceiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&prepareReceiveResponse); err != nil {
		return nil, fmt.Errorf("error decoding response JSON: %w", err)
	}

	return &prepareReceiveResponse, nil
}

func fileIDForPath(root, filePath string) (string, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat send root: %w", err)
	}
	if !rootInfo.IsDir() {
		return filepath.Base(filePath), nil
	}
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return "", fmt.Errorf("derive relative file id: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid relative file id for %s", filePath)
	}
	return filepath.ToSlash(rel), nil
}

func uploadFileWithEvents(ctx context.Context, jobID string, peer transfer.Peer, sessionId, fileId, token, filePath string, events transfer.EventSink) error {
	// 打开要发送的文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	// 获取文件大小用于进度条
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("error getting file info: %w", err)
	}
	fileSize := fileInfo.Size()
	events.Emit(transfer.Event{Kind: transfer.EventItemStarted, JobID: jobID, Peer: peer, ItemID: fileId, TotalBytes: fileSize})

	// Create progress bar
	bar := progressbar.NewOptions64(
		fileSize,
		progressbar.OptionSetDescription(fmt.Sprintf("Uploading %s", filepath.Base(filePath))),
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

	// 构建文件上传的 URL
	uploadURL := fmt.Sprintf("https://%s:53317/api/localsend/v2/upload?sessionId=%s&fileId=%s&token=%s",
		peer.IP, sessionId, fileId, token)

	// 使用 pipe 来避免将整个文件加载到内存中
	pr, pw := io.Pipe()

	// 创建一个错误通道来传递上传过程中的错误
	uploadErr := make(chan error, 1)

	progress := &transferProgressWriter{jobID: jobID, peer: peer, itemID: fileId, total: fileSize, events: events}
	go func() {
		defer pw.Close()
		// 在新的 goroutine 中写入文件数据
		_, err := io.Copy(io.MultiWriter(pw, bar, progress), file)
		if err != nil {
			uploadErr <- err
			return
		}
	}()

	// 创建带有 TLS 配置的 HTTP 客户端
	client := &http.Client{
		Timeout: 30 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // 跳过证书验证
			},
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: true,
		},
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "POST", uploadURL, pr)
	if err != nil {
		return fmt.Errorf("error creating POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = fileSize

	// 使用自定义客户端发送请求，而不是 http.DefaultClient
	resp, err := client.Do(req)

	// 检查是否被取消
	select {
	case <-ctx.Done():
		events.Emit(transfer.Event{Kind: transfer.EventCanceled, JobID: jobID, Peer: peer, ItemID: fileId, Err: ctx.Err()})
		return fmt.Errorf("transfer canceled")
	case err := <-uploadErr:
		if err != nil {
			return fmt.Errorf("upload failed: %w", err)
		}
	default:
		if err != nil {
			return fmt.Errorf("error sending file upload request: %w", err)
		}
	}

	// 检查响应
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 400:
			return fmt.Errorf("missing parameters")
		case 403:
			return fmt.Errorf("invalid token or IP address")
		case 409:
			return fmt.Errorf("blocked by another session")
		case 500:
			return fmt.Errorf("unknown error by receiver")
		}
		return fmt.Errorf("file upload failed: received status code %d", resp.StatusCode)
	}

	fmt.Println()
	logger.Success("File uploaded successfully")
	events.Emit(transfer.Event{Kind: transfer.EventItemCompleted, JobID: jobID, Peer: peer, ItemID: fileId, Bytes: fileSize, TotalBytes: fileSize})
	return nil
}

type transferProgressWriter struct {
	jobID  string
	peer   transfer.Peer
	itemID string
	total  int64
	sent   int64
	events transfer.EventSink
}

func (w *transferProgressWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.sent += int64(n)
	w.events.Emit(transfer.Event{Kind: transfer.EventBytesTransferred, JobID: w.jobID, Peer: w.peer, ItemID: w.itemID, Bytes: w.sent, TotalBytes: w.total})
	return n, nil
}

// SendFile 函数
func SendFile(path string) error {
	updates := make(chan []models.SendModel)
	discovery.ListenAndStartBroadcasts(updates)
	fmt.Println("Select recipients. Space toggles multiple devices; Enter sends to selected/current device:")
	ips, err := tui.SelectDevices(updates)
	if err != nil {
		return err
	}
	if len(ips) == 0 {
		return fmt.Errorf("no recipient selected")
	}
	peers := make([]transfer.Peer, 0, len(ips))
	for _, ip := range ips {
		peers = append(peers, transfer.Peer{IP: ip})
	}
	result := SendFileToRecipients(context.Background(), peers, path, cliEventSink())
	if result.Status() == transfer.StatusFailed {
		return fmt.Errorf("all recipients failed")
	}
	if result.Status() == transfer.StatusPartialSuccess {
		return fmt.Errorf("some recipients failed")
	}
	return nil
}

func SendFileToRecipients(ctx context.Context, peers []transfer.Peer, path string, events transfer.EventSink) transfer.Result {
	started := time.Now().UTC()
	jobID := fmt.Sprintf("send-%d", started.UnixNano())
	result := transfer.Result{JobID: jobID, StartedAt: started}
	events.Emit(transfer.Event{Kind: transfer.EventJobStarted, JobID: jobID, TotalBytes: totalBytes(path)})
	for _, peer := range peers {
		recipient := sendFileToRecipient(ctx, jobID, peer, path, events)
		result.Recipients = append(result.Recipients, recipient)
	}
	result.FinishedAt = time.Now().UTC()
	return result
}

func sendFileToRecipient(ctx context.Context, jobID string, peer transfer.Peer, path string, events transfer.EventSink) transfer.RecipientResult {
	response, err := SendFileToOtherDevicePrepare(peer.IP, path)
	if err != nil {
		events.Emit(transfer.Event{Kind: transfer.EventRecipientFailed, JobID: jobID, Peer: peer, Err: err})
		recordSendFailure(path, peer.IP, err)
		return transfer.RecipientResult{Peer: peer, Error: err}
	}
	events.Emit(transfer.Event{Kind: transfer.EventRecipientPrepared, JobID: jobID, Peer: peer})

	recipientCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	logger.Info("Registering cancel handler for session: ", response.SessionID)
	RegisterCancelHandler(response.SessionID, cancel)
	defer UnregisterCancelHandler(response.SessionID)

	var sentItems []transfer.Item
	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		fileID, err := fileIDForPath(path, filePath)
		if err != nil {
			return err
		}
		token, ok := response.Files[fileID]
		if !ok {
			return fmt.Errorf("token not found for file: %s", fileID)
		}
		if err := uploadFileWithEvents(recipientCtx, jobID, peer, response.SessionID, fileID, token, filePath, events); err != nil {
			return fmt.Errorf("error uploading file: %w", err)
		}
		sentItems = append(sentItems, transfer.Item{ID: fileID, Name: info.Name(), Path: filePath, Size: info.Size(), FileType: filepath.Ext(filePath)})
		if _, err := history.Add(history.Record{
			Direction: history.DirectionSent,
			Status:    history.StatusCompleted,
			FileName:  info.Name(),
			Path:      filePath,
			Size:      info.Size(),
			PeerIP:    peer.IP,
		}); err != nil {
			logger.Warnf("Failed to record send history: %v", err)
		}
		return nil
	})
	if err != nil {
		events.Emit(transfer.Event{Kind: transfer.EventRecipientFailed, JobID: jobID, Peer: peer, Err: err})
		recordSendFailure(path, peer.IP, err)
		return transfer.RecipientResult{Peer: peer, Items: sentItems, Error: err}
	}
	events.Emit(transfer.Event{Kind: transfer.EventRecipientComplete, JobID: jobID, Peer: peer})
	return transfer.RecipientResult{Peer: peer, Items: sentItems}
}

func cliEventSink() transfer.EventSink {
	return func(event transfer.Event) {
		switch event.Kind {
		case transfer.EventRecipientPrepared:
			logger.Infof("Prepared recipient %s", event.Peer.IP)
		case transfer.EventRecipientFailed:
			logger.Errorf("Recipient %s failed: %v", event.Peer.IP, event.Err)
		case transfer.EventRecipientComplete:
			logger.Success("Recipient complete: ", event.Peer.IP)
		}
	}
}

func totalBytes(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func recordSendFailure(path, peerIP string, sendErr error) {
	if _, err := history.Add(history.Record{
		Direction: history.DirectionSent,
		Status:    history.StatusFailed,
		FileName:  filepath.Base(path),
		Path:      path,
		PeerIP:    peerIP,
		Error:     sendErr.Error(),
	}); err != nil {
		logger.Warnf("Failed to record failed send history: %v", err)
	}
}

func NormalSendHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("Handling upload request...") // Debug log - request start

	// Limit multipart form size (set to 10 MB).
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse multipart form: %v", err), http.StatusBadRequest)
		return
	}

	// 获取上传的目录名 (来自前端 hidden input)
	uploadedDirName := r.FormValue("directoryName")
	logger.Debugf("directoryName from form: '%s'\n", uploadedDirName) // Debug log - directoryName value

	// 获取所有上传的文件
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}

	uploadDir := config.ConfigData.OutputDir // 基础上传目录
	finalUploadDir := uploadDir              // 默认最终上传目录

	// 如果前端传递了目录名且不为空，才创建以目录名命名的子目录
	if uploadedDirName != "" {
		finalUploadDir = filepath.Join(uploadDir, uploadedDirName)
	} else {
		logger.Debug("No directoryName provided, uploading to root uploads dir.") // Debug log - no directoryName
	}
	logger.Debugf("Final upload directory: '%s'\n", finalUploadDir)

	// 创建最终的上传目录（如果不存在）
	if err := os.MkdirAll(finalUploadDir, os.ModePerm); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create upload directory: %v", err), http.StatusInternalServerError)
		return
	}

	// 遍历所有文件进行保存
	for _, fileHeader := range files {
		// 打开上传的文件
		file, err := fileHeader.Open()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to open file: %v", err), http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// 拼接目标路径 (使用 finalUploadDir 作为根目录)
		destPath := filepath.Join(finalUploadDir, fileHeader.Filename)
		logger.Infof("Saving file '%s' to destPath: '%s'\n", fileHeader.Filename, destPath) // Debug log - file dest path

		// 创建目标目录（如果不存在）
		if err := os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
			http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
			return
		}

		// 创建目标文件
		dst, err := os.Create(destPath)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to create file: %v", err), http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		// 将上传的文件内容写入目标文件
		if _, err := io.Copy(dst, file); err != nil {
			http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Upload completed: %d files, saved to %s\n", len(files), finalUploadDir)
}
