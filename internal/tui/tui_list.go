package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/models"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// selectDevice 使用 Bubble Tea 库显示可供选择的设备列表并等待用户选择
func SelectDevice(updates <-chan []models.SendModel) (string, error) {
	ips, err := SelectDevices(updates)
	if err != nil || len(ips) == 0 {
		return "", err
	}
	return ips[0], nil
}

// SelectDevices shows the nearby-device dashboard and supports multi-select.
// Space toggles recipients; Enter returns all toggled recipients, or the
// highlighted recipient when none are toggled for single-send compatibility.
func SelectDevices(updates <-chan []models.SendModel) ([]string, error) {
	// 创建一个带缓冲的内部 channel
	internalUpdates := make(chan []models.SendModel, 100)

	// 在后台持续从外部 channel 读取更新
	go func() {
		for devices := range updates {
			// 非阻塞方式发送到内部 channel
			select {
			case internalUpdates <- devices:
			default:
				// 如果 channel 满了，清空后重新发送
				select {
				case <-internalUpdates:
				default:
				}
				internalUpdates <- devices
			}
		}
	}()

	// 创建模型和 Bubble Tea 程序
	initModel := &model{
		devices:    []models.SendModel{},
		deviceMap:  make(map[string]models.SendModel),
		sortedKeys: make([]string, 0),
		selected:   make(map[string]bool),
		cursor:     0,
		updates:    internalUpdates,
	}

	cmd := bubbletea.NewProgram(initModel)
	m, err := cmd.Run()
	if err != nil {
		return nil, err
	}

	if m, ok := m.(model); ok && len(m.devices) > 0 {
		return m.selectedIPs(), nil
	}
	return nil, nil
}

// model 结构体用于 Bubble Tea
type model struct {
	devices    []models.SendModel
	deviceMap  map[string]models.SendModel // 使用 IP 作为键来存储设备
	sortedKeys []string                    // 保持固定的显示顺序
	selected   map[string]bool
	cursor     int
	updates    <-chan []models.SendModel
	width      int
	height     int
}

// DashboardAction 表示仪表盘可识别的高层操作。
type DashboardAction string

const (
	DashboardActionNone     DashboardAction = "none"
	DashboardActionQuit     DashboardAction = "quit"
	DashboardActionMoveUp   DashboardAction = "move_up"
	DashboardActionMoveDown DashboardAction = "move_down"
	DashboardActionSelect   DashboardAction = "select"
	DashboardActionToggle   DashboardAction = "toggle"
)

// DashboardResult 表示一次仪表盘更新后的结果。
type DashboardResult struct {
	Action DashboardAction
	Model  model
	Cmd    bubbletea.Cmd
}

var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 3).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7C5CFF"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#A78BFA")).
			Align(lipgloss.Center)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#111827")).
			Background(lipgloss.Color("#A78BFA")).
			Padding(0, 1)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	emptyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214"))
		// Slight emphasis for loading state.

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244"))
)

// TickMsg 用于定期触发更新
type TickMsg time.Time

// Init 实现 Bubble Tea 的 Init 方法
func (m model) Init() bubbletea.Cmd {
	return tick()
}

// tick 每秒钟触发一次
func tick() bubbletea.Cmd {
	return bubbletea.Tick(time.Second, func(t time.Time) bubbletea.Msg {
		return TickMsg(t)
	})
}

// Update 实现 Bubble Tea 的 Update 方法
func (m model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
	result := m.UpdateDashboard(msg)
	return result.Model, result.Cmd
}

// UpdateDashboard 将 Bubble Tea 消息转换为可测试的仪表盘结果。
func (m model) UpdateDashboard(msg bubbletea.Msg) DashboardResult {
	switch msg := msg.(type) {
	case bubbletea.KeyMsg:
		switch dashboardActionFromKey(msg.String()) {
		case DashboardActionQuit:
			return DashboardResult{Action: DashboardActionQuit, Model: m, Cmd: bubbletea.Quit}
		case DashboardActionMoveDown:
			if len(m.devices) > 0 {
				m.cursor = (m.cursor + 1) % len(m.devices) // 向下移动
			}
			return DashboardResult{Action: DashboardActionMoveDown, Model: m}
		case DashboardActionMoveUp:
			if len(m.devices) > 0 {
				m.cursor = (m.cursor - 1 + len(m.devices)) % len(m.devices) // 向上移动
			}
			return DashboardResult{Action: DashboardActionMoveUp, Model: m}
		case DashboardActionToggle:
			if len(m.devices) > 0 {
				if m.selected == nil {
					m.selected = make(map[string]bool)
				}
				ip := m.devices[m.cursor].IP
				m.selected[ip] = !m.selected[ip]
				if !m.selected[ip] {
					delete(m.selected, ip)
				}
			}
			return DashboardResult{Action: DashboardActionToggle, Model: m}
		case DashboardActionSelect:
			return DashboardResult{Action: DashboardActionSelect, Model: m, Cmd: bubbletea.Quit}
		}
	case TickMsg:
		select {
		case newDevices := <-m.updates:
			if m.deviceMap == nil {
				m.deviceMap = make(map[string]models.SendModel)
			}

			// 更新设备映射
			changed := false
			for _, device := range newDevices {
				if _, exists := m.deviceMap[device.IP]; !exists {
					m.deviceMap[device.IP] = device
					m.sortedKeys = append(m.sortedKeys, device.IP)
					changed = true
				}
			}

			// 只有在有新设备时才更新设备列表
			if changed {
				m.devices = make([]models.SendModel, 0, len(m.deviceMap))
				for _, key := range m.sortedKeys {
					if device, ok := m.deviceMap[key]; ok {
						m.devices = append(m.devices, device)
					}
				}

				// 确保光标不会超出设备列表范围
				if m.cursor >= len(m.devices) {
					m.cursor = len(m.devices) - 1
				}
			}
		default:
		}
		return DashboardResult{Action: DashboardActionNone, Model: m, Cmd: tick()}
	case bubbletea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return DashboardResult{Action: DashboardActionNone, Model: m}
}

func dashboardActionFromKey(key string) DashboardAction {
	switch key {
	case "q", "ctrl+c":
		return DashboardActionQuit
	case "down", "j":
		return DashboardActionMoveDown
	case "up", "k":
		return DashboardActionMoveUp
	case "enter":
		return DashboardActionSelect
	case " ":
		return DashboardActionToggle
	default:
		return DashboardActionNone
	}
}

// View 实现 Bubble Tea 的 View 方法
func (m model) View() string {
	panel := appStyle
	if m.width > 0 {
		panelWidth := m.width - 12
		if panelWidth > 58 {
			panelWidth = 58
		}
		if panelWidth > 32 {
			panel = panel.Width(panelWidth)
		}
	}

	if len(m.devices) == 0 {
		body := strings.Join([]string{
			titleStyle.Render("✨ LocalSend"),
			"",
			emptyStyle.Render("🛰️ Scanning for nearby devices…"),
			"",
			helpStyle.Render("Press Ctrl+C to exit"),
		}, "\n")
		return centerInTerminal(panel.Render(body), m.width, m.height)
	}

	lines := []string{
		titleStyle.Render("✨ LocalSend"),
		subtitleStyle.Render(fmt.Sprintf("📡 Nearby devices • %d online", len(m.devices))),
		"",
	}

	for i, device := range m.devices {
		mark := "○"
		if m.selected[device.IP] {
			mark = "◉"
		}
		line := fmt.Sprintf("  %s %s  %s", mark, device.DeviceName, subtitleStyle.Render(device.IP))
		if m.cursor == i {
			lines = append(lines, selectedStyle.Render("› "+line))
			continue
		}
		lines = append(lines, normalStyle.Render("  "+line))
	}

	lines = append(lines, "", helpStyle.Render("↕ Navigate • Space: toggle • Enter: send • Ctrl+C: quit"))
	return centerInTerminal(panel.Render(strings.Join(lines, "\n")), m.width, m.height)
}

func (m model) selectedIPs() []string {
	if len(m.selected) > 0 {
		ips := make([]string, 0, len(m.selected))
		for _, key := range m.sortedKeys {
			if m.selected[key] {
				ips = append(ips, key)
			}
		}
		return ips
	}
	if len(m.devices) == 0 || m.cursor < 0 || m.cursor >= len(m.devices) {
		return nil
	}
	return []string{m.devices[m.cursor].IP}
}
