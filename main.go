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
	"sync"
	"syscall"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/discovery"
	"github.com/tingkai-c/localsend-cli/internal/discovery/shared"
	"github.com/tingkai-c/localsend-cli/internal/handlers"
	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/pkg/server"
	"github.com/tingkai-c/localsend-cli/internal/trust"
	"github.com/tingkai-c/localsend-cli/internal/utils/cert"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
	"github.com/tingkai-c/localsend-cli/static"
	qrcode "github.com/skip2/go-qrcode"
)

type textInputModel struct {
	value       string
	cursor      int
	placeholder string
	done        bool
}

func initialTextInputModel() textInputModel {
	return textInputModel{
		value:       "",
		cursor:      0,
		placeholder: "Enter file path...",
		done:        false,
	}
}

func (m textInputModel) Init() bubbletea.Cmd {
	return nil
}

func getPathSuggestions(input string) []string {
	if input == "" {
		input = "."
	}

	dir := input
	if !strings.HasSuffix(input, string(os.PathSeparator)) {
		dir = filepath.Dir(input)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil
	}

	prefix := filepath.Clean(input)
	var suggestions []string
	for _, file := range files {
		if strings.HasPrefix(filepath.Clean(file), prefix) {
			suggestions = append(suggestions, file)
		}
	}
	return suggestions
}

func (m textInputModel) Update(msg bubbletea.Msg) (textInputModel, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.MouseMsg:
		// 忽略鼠标事件
		return m, nil

	case bubbletea.KeyMsg:
		switch msg.String() {
		case "backspace":
			if m.cursor > 0 {
				m.value = m.value[:m.cursor-1] + m.value[m.cursor:]
				m.cursor--
			}
		case "left":
			if m.cursor > 0 {
				m.cursor--
			}
		case "right":
			if m.cursor < len(m.value) {
				m.cursor++
			}
		case "tab":
			suggestions := getPathSuggestions(m.value)
			if len(suggestions) > 0 {
				m.value = suggestions[0]
				m.cursor = len(m.value)
			}
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(m.value)
		case "up", "down":
			// Ignore up and down key+s

		case "enter":
			m.done = true

		default:
			if msg.String() != "enter" && msg.String() != "home" && msg.String() != "end" {
				// 只允许输入有效的路径字符
				char := msg.String()
				// 检查是否是有效的路径字符
				if char == "." || char == "/" || char == "\\" || char == ":" || char == "-" || char == "_" ||
					(char >= "a" && char <= "z") || (char >= "A" && char <= "Z") || (char >= "0" && char <= "9") {
					m.value = m.value[:m.cursor] + char + m.value[m.cursor:]
					m.cursor++
				}
			}
		}
	}
	return m, nil
}

func (m textInputModel) View() string {
	if len(m.value) == 0 {
		return m.placeholder
	}
	value := m.value
	cursor := m.cursor
	if cursor > len(value) {
		cursor = len(value)
	}
	return value[:cursor] + "_" + value[cursor:]
}

func (m textInputModel) Value() string {
	return m.value
}

type model struct {
	mode        string
	choices     []string
	cursor      int
	filePrompt  bool
	textInput   textInputModel
	suggestions []string
}

type appMode int

const (
	modeInteractive appMode = iota
	modeWeb
	modeSend
	modeReceive
	modeForget
	modeTrusted
	modeHelp
	modeExit
)

type appCommand struct {
	mode appMode
	arg  string
}

var tuiModes = map[string]appMode{
	"📤 Send":    modeSend,
	"📥 Receive": modeReceive,
	"🌎 Web":     modeWeb,
	"❌ Exit":    modeExit,
}

func initialModel() model {
	return model{
		mode:      "",
		choices:   []string{"📤 Send", "📥 Receive", "🌎 Web", "❌ Exit"},
		cursor:    0,
		textInput: initialTextInputModel(),
	}
}

func (m model) Init() bubbletea.Cmd {
	return m.textInput.Init()
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7571F9")).
			Border(lipgloss.RoundedBorder()).
			Padding(0, 2).
			MarginBottom(1)

	menuStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			PaddingLeft(4)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7571F9")).
				PaddingLeft(2).
				SetString("❯ ")

	unselectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FAFAFA")).
				PaddingLeft(4)

	inputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7571F9")).
				PaddingLeft(2)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			PaddingLeft(1)
)

func (m model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.MouseMsg:
		if msg.Type == bubbletea.MouseLeft {
			if msg.Y > 3 && msg.Y <= len(m.choices)+3 {
				m.cursor = msg.Y - 4
				m.mode = m.choices[m.cursor]
				if m.mode == "📤 Send" {
					m.filePrompt = true
					return m, nil
				} else {
					return m, bubbletea.Quit
				}
			}
		}

	case bubbletea.KeyMsg:
		if m.filePrompt {
			if msg.String() == "ctrl+c" {
				return m, bubbletea.Quit
			}
			m.textInput, _ = m.textInput.Update(msg)
			if m.textInput.done {
				m.mode = "📤 Send"
				return m, bubbletea.Quit
			}
			m.suggestions = getPathSuggestions(m.textInput.value)
			switch msg.String() {
			case "tab":
				if len(m.suggestions) > 0 {
					if m.cursor >= len(m.suggestions)-1 {
						m.cursor = 0
					} else {
						m.cursor++
					}
					m.textInput.value = m.suggestions[m.cursor]
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = len(m.choices) - 1
		case "enter":
			if m.filePrompt {
				m.textInput, _ = m.textInput.Update(msg)
				if m.textInput.done {
					m.mode = "📤 Send"
					return m, bubbletea.Quit
				}
				return m, nil
			} else {
				m.mode = m.choices[m.cursor]
				if m.mode == "📤 Send" {
					m.filePrompt = true
					return m, nil
				} else {
					return m, bubbletea.Quit
				}
			}
		case "backspace", "tab":
			if m.filePrompt {
				m.textInput, _ = m.textInput.Update(msg)
				return m, nil
			}
		case "esc":
			if m.filePrompt {
				m.filePrompt = false
				m.textInput = initialTextInputModel()
			}
		default:
			if m.filePrompt {
				m.textInput, _ = m.textInput.Update(msg)
				return m, nil
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	var s strings.Builder

	// 标题
	s.WriteString(titleStyle.Render("💫 LocalSend CLI 💫"))
	s.WriteString("\n\n")

	// 菜单
	if m.mode == "" {
		for i, choice := range m.choices {
			if i == m.cursor {
				s.WriteString(selectedItemStyle.Render(choice))
			} else {
				s.WriteString(unselectedItemStyle.Render(choice))
			}
			s.WriteString("\n")
		}
	} else {
		// 显示当前模式
		s.WriteString(menuStyle.Render(m.mode))
		s.WriteString("\n\n")

		// 文件路径输入
		if m.filePrompt {
			s.WriteString(inputPromptStyle.Render("Enter file path: "))
			s.WriteString(inputStyle.Render(m.textInput.View()))
		}
	}

	return s.String()
}

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
		fmt.Println("生成二维码失败:", err)
		return
	}

	// 打印二维码到终端
	fmt.Println(qr.ToString(false))
	select {}
}

func ReceiveMode() {
	err := os.MkdirAll(config.ConfigData.OutputDir, 0o755)
	if err != nil {
		logger.Errorf("Failed to create uploads directory %s: %v", config.ConfigData.OutputDir, err)
		return
	}
	discovery.ListenAndStartBroadcasts(nil)
	logger.Infof("Waiting to receive files (output: %s)...", config.ConfigData.OutputDir)
	select {}
}

func SendMode(filePath string) {
	err := handlers.SendFile(filePath)
	if err != nil {
		logger.Errorf("Send failed: %v", err)
	}
}

func ExitMode() {
	fmt.Println("Exiting program...")
	os.Exit(0)
}

// showUsage prints the CLI help. Defined at package scope so it can be wired
// into flag.Usage before flag.Parse runs.
func showUsage() {
	fmt.Println("Usage: <command> [arguments]")
	fmt.Println("Commands:")
	fmt.Println("  web                       Start Web mode")
	fmt.Println("  send <file_path>          Start Send mode (file path required)")
	fmt.Println("  receive                   Start Receive mode")
	fmt.Println("  forget <alias|fingerprint> Remove a sender from the trust list")
	fmt.Println("  trusted                   List trusted senders")
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
	case "forget":
		if len(args) < 2 {
			logger.Error("Need alias or fingerprint to forget")
			ExitMode()
		}
		ForgetMode(args[1])
	case "trusted":
		ListTrustedMode()
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
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("\n收到中断信号，正在退出...")
		os.Exit(0)
	}()
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
		}
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
		// Run Bubble Tea program
		p := bubbletea.NewProgram(initialModel(), bubbletea.WithoutSignalHandler())
		m, err := p.Run()
		if err != nil {
			log.Fatal(err)
		}

		mTyped := m.(model)
		mode := mTyped.mode

		if mode == "❌ Exit" {
			ExitMode()
		}

		if mode == "📤 Send" {
			filePath := mTyped.textInput.Value()
			if filePath == "" {
				fmt.Println("Send mode requires a file path")
				os.Exit(1)
			}
			SendMode(filePath)
		}

		if mode == "📥 Receive" {
			ReceiveMode()
		}
		if mode == "🌎 Web" {
			WebServerMode(httpServer, port)
		}
	}
}
