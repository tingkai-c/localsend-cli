package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tingkai-c/localsend-cli/internal/approval"
	"github.com/tingkai-c/localsend-cli/internal/history"
	"github.com/tingkai-c/localsend-cli/internal/trust"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
)

// MainAction is the typed action returned by the main TUI shell. It is kept
// independent from rendered labels so emojis/copy can change without altering
// dispatch behavior.
type MainAction string

const (
	MainActionNone     MainAction = "none"
	MainActionExit     MainAction = "exit"
	MainActionSend     MainAction = "send"
	MainActionWeb      MainAction = "web"
	MainActionHistory  MainAction = "history"
	MainActionTrusted  MainAction = "trusted"
	MainActionSettings MainAction = "settings"
)

// MainResult is returned when the main dashboard exits.
type MainResult struct {
	Action   MainAction
	FilePath string
}

// MainDeps contains read-only data the dashboard needs to render summaries.
type MainDeps struct {
	DeviceName string
	Port       int
	OutputDir  string
	QuickSave  bool

	ConfigPath  string
	HistoryPath string
	TrustPath   string

	History []history.Record
	Trusted []trust.Entry

	ApprovalRequests <-chan approval.PendingRequest
	LogNotifications <-chan logger.LogEvent

	DeleteHistory func(id string) error
	ClearHistory  func() error
	ForgetTrusted func(query string) error
}

// RunMain opens the Bubble Tea dashboard and returns the selected action.
func RunMain(deps MainDeps) (MainResult, error) {
	program := bubbletea.NewProgram(newDashboardModel(deps))
	m, err := program.Run()
	if err != nil {
		return MainResult{}, err
	}
	if result, ok := m.(dashboardModel); ok {
		return result.result(), nil
	}
	return MainResult{Action: MainActionExit}, nil
}

type dashboardTextInputModel struct {
	value       string
	cursor      int
	placeholder string
	done        bool
}

func initialDashboardTextInputModel() dashboardTextInputModel {
	return dashboardTextInputModel{placeholder: "Enter file path..."}
}

func (m dashboardTextInputModel) Init() bubbletea.Cmd { return nil }

func pathSuggestions(input string) []string {
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

func (m dashboardTextInputModel) Update(msg bubbletea.Msg) (dashboardTextInputModel, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case bubbletea.MouseMsg:
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
			suggestions := pathSuggestions(m.value)
			if len(suggestions) > 0 {
				m.value = suggestions[0]
				m.cursor = len(m.value)
			}
		case "home":
			m.cursor = 0
		case "end":
			m.cursor = len(m.value)
		case "up", "down":
		case "enter":
			m.done = true
		default:
			char := msg.String()
			if isPathInputChar(char) {
				m.value = m.value[:m.cursor] + char + m.value[m.cursor:]
				m.cursor++
			}
		}
	}
	return m, nil
}

func isPathInputChar(char string) bool {
	return char == "." || char == "/" || char == "\\" || char == ":" || char == "-" || char == "_" ||
		(char >= "a" && char <= "z") || (char >= "A" && char <= "Z") || (char >= "0" && char <= "9")
}

func (m dashboardTextInputModel) View() string {
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

func (m dashboardTextInputModel) Value() string { return m.value }

type dashboardView string

const (
	dashboardViewMenu     dashboardView = "menu"
	dashboardViewSend     dashboardView = "send"
	dashboardViewHistory  dashboardView = "history"
	dashboardViewTrusted  dashboardView = "trusted"
	dashboardViewSettings dashboardView = "settings"
)

type dashboardMenuItem struct {
	label  string
	action MainAction
}

type dashboardModel struct {
	view            dashboardView
	items           []dashboardMenuItem
	cursor          int
	textInput       dashboardTextInputModel
	suggestions     []string
	deps            MainDeps
	selected        MainAction
	historyCursor   int
	trustedCursor   int
	pendingApproval *approval.PendingRequest
	logNotification *dashboardLogNotification
	nextLogID       int
	width           int
	height          int
}

func newDashboardModel(deps MainDeps) dashboardModel {
	return dashboardModel{
		view: dashboardViewMenu,
		items: []dashboardMenuItem{
			{label: "🚀 Send", action: MainActionSend},
			{label: "🌐 Web Portal", action: MainActionWeb},
			{label: "🕘 Activity", action: MainActionHistory},
			{label: "🛡️ Trusted", action: MainActionTrusted},
			{label: "✨ Settings", action: MainActionSettings},
			{label: "⏻ Exit", action: MainActionExit},
		},
		textInput: initialDashboardTextInputModel(),
		deps:      deps,
		selected:  MainActionNone,
	}
}

func (m dashboardModel) Init() bubbletea.Cmd {
	return bubbletea.Batch(
		m.textInput.Init(),
		waitForApproval(m.deps.ApprovalRequests),
		waitForLogNotification(m.deps.LogNotifications),
	)
}

type approvalRequestedMsg struct {
	Pending approval.PendingRequest
}

type dashboardLogNotification struct {
	ID    int
	Event logger.LogEvent
}

type logNotificationMsg struct {
	Event logger.LogEvent
}

type clearLogNotificationMsg struct {
	ID int
}

const logNotificationTTL = 5 * time.Second

func waitForApproval(requests <-chan approval.PendingRequest) bubbletea.Cmd {
	if requests == nil {
		return nil
	}
	return func() bubbletea.Msg {
		pending, ok := <-requests
		if !ok {
			return nil
		}
		return approvalRequestedMsg{Pending: pending}
	}
}

func waitForLogNotification(events <-chan logger.LogEvent) bubbletea.Cmd {
	if events == nil {
		return nil
	}
	return func() bubbletea.Msg {
		event, ok := <-events
		if !ok {
			return nil
		}
		return logNotificationMsg{Event: event}
	}
}

func clearLogNotificationAfter(id int) bubbletea.Cmd {
	return bubbletea.Tick(logNotificationTTL, func(time.Time) bubbletea.Msg {
		return clearLogNotificationMsg{ID: id}
	})
}

var (
	dashPanelStyle = lipgloss.NewStyle().
			Padding(1, 3).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7C5CFF"))

	dashTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#A78BFA")).
			Align(lipgloss.Center).
			MarginBottom(1)

	dashMenuStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E5E7EB")).
			PaddingLeft(2)

	dashMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			PaddingLeft(2)

	dashSelectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#111827")).
				Background(lipgloss.Color("#A78BFA")).
				Bold(true).
				Padding(0, 1).
				MarginLeft(1).
				SetString("❯ ")

	dashUnselectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FAFAFA")).
				PaddingLeft(4)

	dashInputPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A78BFA")).
				Bold(true).
				PaddingLeft(2)

	dashInputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			PaddingLeft(1)

	dashNotificationStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FDE68A")).
				Background(lipgloss.Color("#451A03")).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#F59E0B")).
				Padding(0, 1).
				MaxWidth(52)
)

func (m dashboardModel) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg := msg.(type) {
	case approvalRequestedMsg:
		pending := msg.Pending
		m.pendingApproval = &pending
		return m, nil
	case logNotificationMsg:
		m.nextLogID++
		m.logNotification = &dashboardLogNotification{
			ID:    m.nextLogID,
			Event: msg.Event,
		}
		return m, bubbletea.Batch(waitForLogNotification(m.deps.LogNotifications), clearLogNotificationAfter(m.nextLogID))
	case clearLogNotificationMsg:
		if m.logNotification != nil && m.logNotification.ID == msg.ID {
			m.logNotification = nil
		}
		return m, nil
	case bubbletea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case bubbletea.MouseMsg:
		return m.updateMouse(msg)
	case bubbletea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m dashboardModel) updateMouse(msg bubbletea.MouseMsg) (bubbletea.Model, bubbletea.Cmd) {
	if m.view != dashboardViewMenu || msg.Type != bubbletea.MouseLeft {
		return m, nil
	}
	if msg.Y > 3 && msg.Y <= len(m.items)+3 {
		m.cursor = msg.Y - 4
		return m.activateCurrentItem()
	}
	return m, nil
}

func (m dashboardModel) updateKey(msg bubbletea.KeyMsg) (bubbletea.Model, bubbletea.Cmd) {
	if m.pendingApproval != nil {
		switch msg.String() {
		case "y", "enter":
			m.pendingApproval.Respond(approval.Decision{Action: approval.Accept, Reason: "tui"})
		case "a":
			m.pendingApproval.Respond(approval.Decision{Action: approval.AcceptAlways, Reason: "tui-always"})
		case "n", "esc":
			m.pendingApproval.Respond(approval.Decision{Action: approval.Reject, Reason: "tui-reject"})
		case "ctrl+c", "q":
			m.pendingApproval.Respond(approval.Decision{Action: approval.Reject, Reason: "tui-quit"})
			m.selected = MainActionExit
			m.pendingApproval = nil
			return m, bubbletea.Quit
		default:
			return m, nil
		}
		m.pendingApproval = nil
		return m, waitForApproval(m.deps.ApprovalRequests)
	}

	if m.view == dashboardViewSend {
		switch msg.String() {
		case "ctrl+c":
			m.selected = MainActionExit
			return m, bubbletea.Quit
		case "esc":
			m.view = dashboardViewMenu
			m.textInput = initialDashboardTextInputModel()
			m.suggestions = nil
			return m, nil
		}
		m.textInput, _ = m.textInput.Update(msg)
		if m.textInput.done {
			m.selected = MainActionSend
			return m, bubbletea.Quit
		}
		m.suggestions = pathSuggestions(m.textInput.value)
		if msg.String() == "tab" && len(m.suggestions) > 0 {
			m.textInput.value = m.suggestions[0]
			m.textInput.cursor = len(m.textInput.value)
		}
		return m, nil
	}

	if m.view != dashboardViewMenu {
		return m.updateDetailViewKey(msg)
	}

	switch msg.String() {
	case "ctrl+c", "q":
		m.selected = MainActionExit
		return m, bubbletea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = len(m.items) - 1
	case "enter":
		return m.activateCurrentItem()
	}
	return m, nil
}

func (m dashboardModel) updateDetailViewKey(msg bubbletea.KeyMsg) (bubbletea.Model, bubbletea.Cmd) {
	switch msg.String() {
	case "esc", "backspace", "h":
		m.view = dashboardViewMenu
		return m, nil
	case "ctrl+c", "q":
		m.selected = MainActionExit
		return m, bubbletea.Quit
	case "up", "k":
		m.moveDetailCursor(-1)
	case "down", "j":
		m.moveDetailCursor(1)
	case "d", "x":
		m.deleteCurrentDetailItem()
	case "c":
		if m.view == dashboardViewHistory && m.deps.ClearHistory != nil {
			if err := m.deps.ClearHistory(); err == nil {
				m.deps.History = nil
				m.historyCursor = 0
			}
		}
	}
	return m, nil
}

func (m *dashboardModel) moveDetailCursor(delta int) {
	switch m.view {
	case dashboardViewHistory:
		m.historyCursor = boundedCursor(m.historyCursor+delta, len(m.deps.History))
	case dashboardViewTrusted:
		m.trustedCursor = boundedCursor(m.trustedCursor+delta, len(m.deps.Trusted))
	}
}

func boundedCursor(cursor, length int) int {
	if length <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}

func (m *dashboardModel) deleteCurrentDetailItem() {
	switch m.view {
	case dashboardViewHistory:
		if len(m.deps.History) == 0 || m.deps.DeleteHistory == nil {
			return
		}
		m.historyCursor = boundedCursor(m.historyCursor, len(m.deps.History))
		record := m.deps.History[m.historyCursor]
		if err := m.deps.DeleteHistory(record.ID); err != nil {
			return
		}
		m.deps.History = append(m.deps.History[:m.historyCursor], m.deps.History[m.historyCursor+1:]...)
		m.historyCursor = boundedCursor(m.historyCursor, len(m.deps.History))
	case dashboardViewTrusted:
		if len(m.deps.Trusted) == 0 || m.deps.ForgetTrusted == nil {
			return
		}
		m.trustedCursor = boundedCursor(m.trustedCursor, len(m.deps.Trusted))
		entry := m.deps.Trusted[m.trustedCursor]
		query := entry.Fingerprint
		if query == "" {
			query = entry.Alias
		}
		if err := m.deps.ForgetTrusted(query); err != nil {
			return
		}
		m.deps.Trusted = append(m.deps.Trusted[:m.trustedCursor], m.deps.Trusted[m.trustedCursor+1:]...)
		m.trustedCursor = boundedCursor(m.trustedCursor, len(m.deps.Trusted))
	}
}

func (m dashboardModel) activateCurrentItem() (bubbletea.Model, bubbletea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return m, nil
	}
	action := m.items[m.cursor].action
	switch action {
	case MainActionSend:
		m.view = dashboardViewSend
		m.textInput = initialDashboardTextInputModel()
		m.suggestions = nil
		return m, nil
	case MainActionHistory:
		m.view = dashboardViewHistory
		return m, nil
	case MainActionTrusted:
		m.view = dashboardViewTrusted
		return m, nil
	case MainActionSettings:
		m.view = dashboardViewSettings
		return m, nil
	default:
		m.selected = action
		return m, bubbletea.Quit
	}
}

func (m dashboardModel) result() MainResult {
	return MainResult{Action: m.selected, FilePath: m.textInput.Value()}
}

func (m dashboardModel) View() string {
	var s strings.Builder

	s.WriteString(dashTitleStyle.Render("✨ LocalSend CLI"))
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render("🟢 Receiver ready for Quick Save and trusted senders"))
	s.WriteString("\n")
	s.WriteString(dashMutedStyle.Render(fmt.Sprintf("💻 %s  •  Port %d", m.deps.DeviceName, m.deps.Port)))
	s.WriteString("\n")
	s.WriteString(dashMutedStyle.Render("📁 " + m.deps.OutputDir))
	s.WriteString("\n")
	s.WriteString(dashMutedStyle.Render(fmt.Sprintf("🕘 %d transfers  •  🛡️ %d trusted", len(m.deps.History), len(m.deps.Trusted))))
	s.WriteString("\n\n")

	if m.pendingApproval != nil {
		m.renderApproval(&s)
		return m.renderWithNotification(m.renderCenteredPanel(s.String()))
	}

	switch m.view {
	case dashboardViewMenu:
		m.renderMenu(&s)
	case dashboardViewSend:
		m.renderSend(&s)
	case dashboardViewHistory:
		m.renderHistory(&s)
	case dashboardViewTrusted:
		m.renderTrusted(&s)
	case dashboardViewSettings:
		m.renderSettings(&s)
	}

	return m.renderWithNotification(m.renderCenteredPanel(s.String()))
}

func (m dashboardModel) renderCenteredPanel(body string) string {
	panel := dashPanelStyle
	if m.width > 0 {
		panelWidth := m.width - 12
		if panelWidth > 64 {
			panelWidth = 64
		}
		if panelWidth > 36 {
			panel = panel.Width(panelWidth)
		}
	}
	return centerInTerminal(panel.Render(body), m.width, m.height)
}

func (m dashboardModel) renderWithNotification(body string) string {
	if m.logNotification == nil {
		return body
	}
	notification := m.renderLogNotification()
	if notification == "" {
		return body
	}
	if m.width > 0 {
		return lipgloss.PlaceHorizontal(m.width, lipgloss.Right, notification) + "\n" + body
	}
	return notification + "\n" + body
}

func (m dashboardModel) renderLogNotification() string {
	event := m.logNotification.Event
	label := strings.ToUpper(event.Level.String())
	switch event.Level.String() {
	case "warning":
		label = "WARN"
	case "error":
		label = "ERROR"
	case "fatal":
		label = "FATAL"
	case "panic":
		label = "PANIC"
	}
	message := strings.TrimSpace(event.Message)
	if message == "" {
		message = "Background log event"
	}
	if lipgloss.Width(message) > 46 {
		message = truncateCells(message, 45) + "…"
	}
	return dashNotificationStyle.Render(fmt.Sprintf("⚠ %s  %s", label, message))
}

func truncateCells(s string, max int) string {
	if max <= 0 || lipgloss.Width(s) <= max {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		next := b.String() + string(r)
		if lipgloss.Width(next) > max {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (m dashboardModel) renderApproval(s *strings.Builder) {
	req := m.pendingApproval.Request
	s.WriteString(dashInputPromptStyle.Render("🔔 Incoming transfer"))
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render(fmt.Sprintf("From: %s (%s)", req.Alias, shortFingerprint(req.Fingerprint))))
	s.WriteString("\n")
	var total int64
	for _, file := range req.Files {
		total += file.Size
	}
	s.WriteString(dashMenuStyle.Render(fmt.Sprintf("Files: %d, total %s", len(req.Files), humanBytes(total))))
	s.WriteString("\n")
	limit := len(req.Files)
	if limit > 5 {
		limit = 5
	}
	for _, file := range req.Files[:limit] {
		s.WriteString(dashMenuStyle.Render(fmt.Sprintf("  • %s (%s)", file.Name, humanBytes(file.Size))))
		s.WriteString("\n")
	}
	if len(req.Files) > limit {
		s.WriteString(dashMenuStyle.Render(fmt.Sprintf("  …and %d more", len(req.Files)-limit)))
		s.WriteString("\n")
	}
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render("y/Enter: accept • a: accept always/trust • n/Esc: reject"))
}

func shortFingerprint(fingerprint string) string {
	if len(fingerprint) > 12 {
		return fingerprint[:12] + "…"
	}
	return fingerprint
}

func (m dashboardModel) renderMenu(s *strings.Builder) {
	s.WriteString(dashInputPromptStyle.Render("⌘ Choose an action"))
	s.WriteString("\n")
	for i, item := range m.items {
		if i == m.cursor {
			s.WriteString(dashSelectedItemStyle.Render(item.label))
		} else {
			s.WriteString(dashUnselectedItemStyle.Render(item.label))
		}
		s.WriteString("\n")
	}
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render("↑/↓ or j/k: navigate • Enter: select • q/Ctrl+C: quit"))
}

func (m dashboardModel) renderSend(s *strings.Builder) {
	s.WriteString(dashInputPromptStyle.Render("📦 File path: "))
	s.WriteString(dashInputStyle.Render(m.textInput.View()))
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render("Tab: complete path • Esc: back • Ctrl+C: quit"))
}

func (m dashboardModel) renderHistory(s *strings.Builder) {
	s.WriteString(dashInputPromptStyle.Render("🕘 Transfer history"))
	s.WriteString("\n")
	if len(m.deps.History) == 0 {
		s.WriteString(dashMenuStyle.Render("No transfer history yet."))
		s.WriteString("\n")
	} else {
		limit := len(m.deps.History)
		if limit > 8 {
			limit = 8
		}
		for i, record := range m.deps.History[:limit] {
			peer := record.PeerAlias
			if peer == "" {
				peer = record.PeerIP
			}
			if peer == "" {
				peer = "unknown peer"
			}
			prefix := "  "
			if i == m.historyCursor {
				prefix = "❯ "
			}
			s.WriteString(dashMenuStyle.Render(fmt.Sprintf("%s%s  %-8s %-9s %8s  %s", prefix, record.CompletedAt.Format("2006-01-02 15:04"), record.Direction, record.Status, humanBytes(record.Size), peer)))
			s.WriteString("\n")
			s.WriteString(dashMenuStyle.Render("  " + record.FileName))
			s.WriteString("\n")
		}
	}
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render("j/k: move • d: delete selected • c: clear all • Esc/backspace/h: dashboard • q: quit"))
}

func (m dashboardModel) renderTrusted(s *strings.Builder) {
	s.WriteString(dashInputPromptStyle.Render("🛡️ Trusted senders"))
	s.WriteString("\n")
	if len(m.deps.Trusted) == 0 {
		s.WriteString(dashMenuStyle.Render("No trusted senders yet."))
		s.WriteString("\n")
	} else {
		for i, entry := range m.deps.Trusted {
			alias := entry.Alias
			if alias == "" {
				alias = "(no alias)"
			}
			prefix := "  "
			if i == m.trustedCursor {
				prefix = "❯ "
			}
			s.WriteString(dashMenuStyle.Render(fmt.Sprintf("%s%s  %s  added %s", prefix, entry.Fingerprint, alias, entry.AddedAt.Format("2006-01-02"))))
			s.WriteString("\n")
		}
	}
	s.WriteString("\n")
	s.WriteString(dashMenuStyle.Render("j/k: move • d/x: forget selected • Esc/backspace/h: dashboard • q: quit"))
}

func (m dashboardModel) renderSettings(s *strings.Builder) {
	s.WriteString(dashInputPromptStyle.Render("✨ Settings / Help"))
	s.WriteString("\n")
	quickSave := "off"
	if m.deps.QuickSave {
		quickSave = "on"
	}
	lines := []string{
		"Device name: " + m.deps.DeviceName,
		fmt.Sprintf("Port: %d", m.deps.Port),
		"Output directory: " + m.deps.OutputDir,
		"Quick Save: " + quickSave,
		"Config file: " + m.deps.ConfigPath,
		"History file: " + m.deps.HistoryPath,
		"Trust file: " + m.deps.TrustPath,
		"",
		"Headless: use receive with --quick-save or trust a sender first.",
		"Keys: j/k or arrows move, Enter selects, Esc returns, q quits.",
	}
	for _, line := range lines {
		s.WriteString(dashMenuStyle.Render(line))
		s.WriteString("\n")
	}
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
