package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/tingkai-c/localsend-cli/internal/approval"
	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/discovery"
	"github.com/tingkai-c/localsend-cli/internal/discovery/shared"
	"github.com/tingkai-c/localsend-cli/internal/handlers"
	"github.com/tingkai-c/localsend-cli/internal/history"
	"github.com/tingkai-c/localsend-cli/internal/pkg/server"
	"github.com/tingkai-c/localsend-cli/internal/trust"
	"github.com/tingkai-c/localsend-cli/internal/tui"
	"github.com/tingkai-c/localsend-cli/internal/utils/cert"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
	"github.com/tingkai-c/localsend-cli/static"
)

func WebServerMode(httpServer *http.ServeMux, port int) {
	err := os.MkdirAll(config.ConfigData.OutputDir, 0o755)
	if err != nil {
		logger.Errorf("Failed to create uploads directory %s: %v", config.ConfigData.OutputDir, err)
		return
	}
	if config.ConfigData.Functions.HttpFileServer {
		httpServer.HandleFunc("/", handlers.IndexFileHandler)
		httpServer.HandleFunc("/uploads/", handlers.FileServerHandler)
		httpServer.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.EmbeddedStaticFiles))))
		httpServer.HandleFunc("/send", handlers.NormalSendHandler) // Upload handler
	}
	ips, _ := discovery.GetLocalIP()
	localIP := ""
	for _, ip := range ips {
		ipStr := ip.String()
		if strings.HasPrefix(ipStr, "10.") || strings.HasPrefix(ipStr, "192.168.") {
			logger.Infof("If you opened the HTTP file server, you can view your files on %s", fmt.Sprintf("http://%v:%d", ip, port))
		}
		if strings.HasPrefix(ipStr, "192.168.") {
			localIP = ip.String()
		}
	}
	qr, err := qrcode.New(fmt.Sprintf("http://%s:%d", localIP, port), qrcode.Highest)
	if err != nil {
		fmt.Println("Failed to generate QR code:", err)
		return
	}

	fmt.Println(qr.ToString(false))
	waitForInterrupt("Web server stopped.")
}

func ReceiveMode() {
	err := os.MkdirAll(config.ConfigData.OutputDir, 0o755)
	if err != nil {
		logger.Errorf("Failed to create uploads directory %s: %v", config.ConfigData.OutputDir, err)
		return
	}
	discovery.ListenAndStartBroadcasts(nil)
	logger.Infof("Waiting to receive files (output: %s)...", config.ConfigData.OutputDir)
	waitForInterrupt("Receive mode stopped.")
}

func SendMode(filePath string) {
	err := handlers.SendFile(filePath)
	if err != nil {
		logger.Errorf("Send failed: %v", err)
	}
}

func SendTextMode(message string) {
	dir, err := os.MkdirTemp("", "localsend-cli-text-*")
	if err != nil {
		logger.Errorf("Failed to create temporary text payload: %v", err)
		ExitMode()
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "message.txt")
	if err := os.WriteFile(path, []byte(message), 0o600); err != nil {
		logger.Errorf("Failed to write temporary text payload: %v", err)
		ExitMode()
	}
	SendMode(path)
}

func ExitMode() {
	fmt.Println("Exiting program...")
	os.Exit(0)
}

func waitForInterrupt(message string) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)
	<-signalChan
	fmt.Println("\n" + message)
}

// showUsage prints the CLI help. Defined at package scope so it can be wired
// into flag.Usage before flag.Parse runs.
func showUsage() {
	fmt.Println("Usage: localsend-cli [command] [arguments]")
	fmt.Println("Run without a command to open the interactive TUI dashboard.")
	fmt.Println("Commands:")
	fmt.Println("  web                       Start Web mode")
	fmt.Println("  send <file_path>          Send file/folder; TUI selector supports multi-recipient Space toggles")
	fmt.Println("  send-text <message>       Send text as a LocalSend-compatible text file")
	fmt.Println("  receive                   Start Receive mode")
	fmt.Println("  forget <alias|fingerprint> Remove a sender from the trust list")
	fmt.Println("  trusted                   List trusted senders")
	fmt.Println("  history                   List transfer history")
	fmt.Println("  history-clear             Clear transfer history")
	fmt.Println("  help                      Display this help information")
	fmt.Println("Options:")
	fmt.Println("  --help                    Display this help information")
	fmt.Println("  --port=<number>           Specify server port (default: 53317)")
	fmt.Println("  --output-dir=<path>       Directory for received files (default: ~/Downloads/localsend-cli)")
	fmt.Println("  --device-name=<s>         Device alias broadcast over the network")
	fmt.Println("  --quick-save              Auto-accept every incoming session (skip approval prompt)")
	fmt.Println("")
	fmt.Println("  Config file:  " + config.ConfigPath())
	fmt.Println("  Trust file:   " + trust.Path())
	fmt.Println("  Env vars:     LOCALSEND_CLI_PORT, LOCALSEND_CLI_OUTPUT_DIR,")
	fmt.Println("                LOCALSEND_CLI_DEVICE_NAME, LOCALSEND_CLI_QUICK_SAVE")
	fmt.Println("  Precedence:   flag > env > config file > built-in default")
}

func flagParse(httpServer *http.ServeMux, port int, flagOpen *bool) {
	// 检查是否有 --help 参数
	for _, arg := range os.Args {
		if arg == "--help" || arg == "-h" {
			showUsage()
			ExitMode()
		}
	}

	// flag.Args() returns the positional arguments left after flag parsing,
	// so commands work whether flags come before or after the mode keyword
	// (e.g. `localsend-cli --port=12345 receive` and
	// `localsend-cli receive --port=12345` are both accepted).
	args := flag.Args()
	if len(args) == 0 {
		return
	}

	*flagOpen = true
	switch args[0] {
	case "web":
		WebServerMode(httpServer, port)
	case "send":
		if len(args) < 2 {
			logger.Error("Need file path")
			ExitMode()
		}
		SendMode(args[1])
	case "receive":
		ReceiveMode()
	case "send-text":
		if len(args) < 2 {
			logger.Error("Need text message")
			ExitMode()
		}
		SendTextMode(strings.Join(args[1:], " "))
	case "forget":
		if len(args) < 2 {
			logger.Error("Need alias or fingerprint to forget")
			ExitMode()
		}
		ForgetMode(args[1])
	case "trusted":
		ListTrustedMode()
	case "history":
		ListHistoryMode()
	case "history-clear":
		ClearHistoryMode()
	case "help":
		showUsage()
		ExitMode()
	}
}

// ForgetMode removes a sender from the trust list. Matching is by exact
// alias (case-insensitive), exact fingerprint, or fingerprint prefix of
// at least 8 chars.
func ForgetMode(query string) {
	if err := trust.Load(); err != nil {
		logger.Errorf("Failed to load trust file: %v", err)
		ExitMode()
	}
	removed, err := trust.Forget(query)
	if err != nil {
		logger.Errorf("Failed to update trust file: %v", err)
		ExitMode()
	}
	if len(removed) == 0 {
		fmt.Printf("No trusted sender matched %q.\n", query)
		return
	}
	for _, e := range removed {
		fmt.Printf("Forgot %s (%s)\n", e.Alias, e.Fingerprint)
	}
}

// ListTrustedMode prints all currently-trusted senders.
func ListTrustedMode() {
	if err := trust.Load(); err != nil {
		logger.Errorf("Failed to load trust file: %v", err)
		ExitMode()
	}
	entries := trust.List()
	if len(entries) == 0 {
		fmt.Println("No trusted senders.")
		return
	}
	fmt.Printf("Trusted senders (%s):\n", trust.Path())
	for _, e := range entries {
		alias := e.Alias
		if alias == "" {
			alias = "(no alias)"
		}
		fmt.Printf("  %s  %s  added %s\n", e.Fingerprint, alias, e.AddedAt.Format("2006-01-02"))
	}
}

// ListHistoryMode prints persisted transfer history.
func ListHistoryMode() {
	records, err := history.List()
	if err != nil {
		logger.Errorf("Failed to load history: %v", err)
		ExitMode()
	}
	if len(records) == 0 {
		fmt.Println("No transfer history.")
		return
	}
	fmt.Printf("Transfer history (%s):\n", history.Path())
	for _, record := range records {
		peer := record.PeerAlias
		if peer == "" {
			peer = record.PeerIP
		}
		if peer == "" {
			peer = "unknown peer"
		}
		when := record.CompletedAt.Format("2006-01-02 15:04:05")
		fmt.Printf("  %s  %-8s %-9s %8s  %-24s  %s\n", when, record.Direction, record.Status, humanBytes(record.Size), peer, record.FileName)
	}
}

// ClearHistoryMode removes all transfer history records.
func ClearHistoryMode() {
	if err := history.Clear(); err != nil {
		logger.Errorf("Failed to clear history: %v", err)
		ExitMode()
	}
	fmt.Println("Transfer history cleared.")
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

var (
	port       int
	outputDir  string
	deviceName string
	quickSave  bool
)

func init() {
	flag.IntVar(&port, "port", 53317, "Port to listen on")
	flag.StringVar(&outputDir, "output-dir", "", "Directory for received files (overrides config)")
	flag.StringVar(&deviceName, "device-name", "", "Device alias broadcast over the network (overrides config)")
	flag.BoolVar(&quickSave, "quick-save", false, "Skip the approval prompt and auto-accept every incoming session (overrides config)")
}

// applyFlagOverrides copies any explicitly-set CLI flags onto the loaded
// config so the precedence chain (flag > env > file > default) is honored.
// flag.Visit only iterates flags that were actually passed on the command
// line, which is exactly the signal we need.
func applyFlagOverrides() {
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port":
			config.ConfigData.Port = port
		case "output-dir":
			config.ConfigData.OutputDir = outputDir
		case "device-name":
			config.ConfigData.DeviceName = deviceName
			config.ConfigData.NameOfDevice = deviceName
		case "quick-save":
			config.ConfigData.QuickSave = quickSave
		}
	})
}

// certDir returns the directory where the TLS certificate and key are
// persisted. Falling back to the current directory keeps the program usable
// in environments where UserConfigDir is not writable.
func certDir() string {
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "localsend-cli")
	}
	return ".localsend-cli"
}

func main() {
	var flagOpen bool = false
	logger.InitLogger()

	// Parse flags up front so config values (port, output dir, device name)
	// are settled before anything reads them. flagParse() below still runs
	// for command dispatch (web/send/receive); flag.Parse is idempotent.
	flag.Usage = showUsage
	flag.Parse()
	applyFlagOverrides()

	// Lightweight subcommands that only touch the trust file should exit
	// without spinning up the cert + HTTPS server.
	if args := flag.Args(); len(args) > 0 {
		switch args[0] {
		case "send-text":
			if len(args) < 2 {
				logger.Error("Need text message")
				ExitMode()
			}
			SendTextMode(strings.Join(args[1:], " "))
			return
		case "forget":
			if len(args) < 2 {
				logger.Error("Need alias or fingerprint to forget")
				ExitMode()
			}
			ForgetMode(args[1])
			return
		case "trusted":
			ListTrustedMode()
			return
		case "history":
			ListHistoryMode()
			return
		case "history-clear":
			ClearHistoryMode()
			return
		}
	}

	dashboardMode := len(flag.Args()) == 0
	restoreDashboardLogs := func() {}
	if dashboardMode {
		restoreDashboardLogs = logger.SuppressInfoAndBelow()
		defer restoreDashboardLogs()
	}

	// Load the trust list so PrepareReceive can short-circuit prompts for
	// previously-approved fingerprints.
	if err := trust.Load(); err != nil {
		logger.Errorf("Failed to load trust file %s: %v", trust.Path(), err)
	}

	// LocalSend v2 mandates HTTPS with self-signed certificates pinned by
	// fingerprint. Generate or load a stable cert before starting the server
	// and the discovery broadcast so both advertise the same fingerprint.
	tlsCert, fp, err := cert.GenerateOrLoad(certDir())
	if err != nil {
		log.Fatalf("Failed to prepare TLS certificate: %v", err)
	}
	shared.Message.Fingerprint = fp
	shared.Message.Port = config.ConfigData.Port
	shared.Message.Alias = config.ConfigData.NameOfDevice

	// Start HTTPS server
	httpServer := server.New()

	/* Send and receive section */
	if config.ConfigData.Functions.LocalSendServer {
		httpServer.HandleFunc("/api/localsend/v2/prepare-upload", handlers.PrepareReceive)
		httpServer.HandleFunc("/api/localsend/v2/upload", handlers.ReceiveHandler)
		httpServer.HandleFunc("/api/localsend/v2/info", handlers.GetInfoHandler)
		httpServer.HandleFunc("/api/localsend/v2/cancel", handlers.HandleCancel)
		// LocalSend clients probe v1/info as a discovery sanity-check before
		// falling through to v2. Returning the v2 payload is safe — extra
		// fields are ignored by v1 clients.
		httpServer.HandleFunc("/api/localsend/v1/info", handlers.GetInfoHandler)
	}
	go func() {
		addr := ":" + fmt.Sprintf("%d", config.ConfigData.Port)
		logger.Info("Server started at " + addr + " (https, fingerprint=" + fp + ")")
		srv := &http.Server{
			Addr:      addr,
			Handler:   httpServer,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{tlsCert}},
		}
		if err := srv.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()
	// 参数解析
	flagParse(httpServer, config.ConfigData.Port, &flagOpen)

	if !flagOpen {
		discovery.ListenAndStartBroadcasts(nil)
		approvalProvider := approval.NewChannelProvider(1)
		handlers.SetApprovalProvider(approvalProvider)
		defer handlers.SetApprovalProvider(nil)

		records, err := history.List()
		if err != nil {
			logger.Warnf("Failed to load transfer history for dashboard: %v", err)
		}
		result, err := tui.RunMain(tui.MainDeps{
			DeviceName:       config.ConfigData.NameOfDevice,
			Port:             config.ConfigData.Port,
			OutputDir:        config.ConfigData.OutputDir,
			QuickSave:        config.ConfigData.QuickSave,
			ConfigPath:       config.ConfigPath(),
			HistoryPath:      history.Path(),
			TrustPath:        trust.Path(),
			History:          records,
			Trusted:          trust.List(),
			ApprovalRequests: approvalProvider.Requests(),
			DeleteHistory:    func(id string) error { _, err := history.Delete(id); return err },
			ClearHistory:     history.Clear,
			ForgetTrusted:    func(query string) error { _, err := trust.Forget(query); return err },
		})
		if err != nil {
			log.Fatal(err)
		}

		restoreDashboardLogs()

		switch result.Action {
		case tui.MainActionExit, tui.MainActionNone:
			ExitMode()
		case tui.MainActionSend:
			if result.FilePath == "" {
				fmt.Println("Send mode requires a file path")
				os.Exit(1)
			}
			handlers.SetApprovalProvider(nil)
			SendMode(result.FilePath)
		case tui.MainActionReceive:
			handlers.SetApprovalProvider(nil)
			ReceiveMode()
		case tui.MainActionWeb:
			handlers.SetApprovalProvider(nil)
			WebServerMode(httpServer, port)
		}
	}
}
