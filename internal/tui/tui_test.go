package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/tingkai-c/localsend-cli/internal/models"
)

// TestSelectDevice 测试 SelectDevice 函数
func TestSelectDevice(t *testing.T) {
	// SelectDevice opens /dev/tty for the bubbletea program; CI has no TTY.
	if f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err != nil {
		t.Skip("no /dev/tty available, skipping interactive UI test")
	} else {
		f.Close()
	}

	// 创建一个设备更新 channel
	updates := make(chan []models.SendModel)

	// 模拟设备更新
	go func() {
		time.Sleep(1 * time.Second)
		updates <- []models.SendModel{
			{IP: "192.168.1.1", DeviceName: "Device 1"},
			{IP: "192.168.1.2", DeviceName: "Device 2"},
		}
		time.Sleep(1 * time.Second)
		updates <- []models.SendModel{
			{IP: "192.168.1.1", DeviceName: "Device 1"},
			{IP: "192.168.1.2", DeviceName: "Device 2"},
			{IP: "192.168.1.3", DeviceName: "Device 3"},
		}
	}()

	// 调用 SelectDevice 函数
	ip, err := SelectDevice(updates)
	if err != nil {
		t.Fatalf("SelectDevice returned an error: %v", err)
	}

	// 检查返回的 IP 是否在模拟的设备列表中
	expectedIPs := map[string]bool{
		"192.168.1.1": true,
		"192.168.1.2": true,
		"192.168.1.3": true,
	}
	if !expectedIPs[ip] {
		t.Fatalf("SelectDevice returned an unexpected IP: %s", ip)
	}
}

func TestViewPreview(t *testing.T) {
	m := model{
		devices: []models.SendModel{
			{IP: "192.168.1.10", DeviceName: "MacBook Pro"},
			{IP: "192.168.1.22", DeviceName: "Steam Deck"},
			{IP: "192.168.1.35", DeviceName: "Pixel 9"},
		},
		cursor: 1,
	}

	view := m.View()
	if !strings.Contains(view, "LocalSend") {
		t.Fatalf("expected title in view, got: %q", view)
	}
	if !strings.Contains(view, "Steam Deck") {
		t.Fatalf("expected selected device in view, got: %q", view)
	}

	t.Logf("TUI preview:\n%s", view)
}

func TestDashboardActionFromKeyMappings(t *testing.T) {
	cases := map[string]DashboardAction{
		"ctrl+c": DashboardActionQuit,
		"q":      DashboardActionQuit,
		"enter":  DashboardActionSelect,
		"j":      DashboardActionMoveDown,
		"k":      DashboardActionMoveUp,
		"noop":   DashboardActionNone,
	}

	for key, want := range cases {
		if got := dashboardActionFromKey(key); got != want {
			t.Fatalf("dashboardActionFromKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestUpdateDashboardCtrlCQuits(t *testing.T) {
	m := model{
		devices: []models.SendModel{
			{IP: "192.168.1.10", DeviceName: "MacBook Pro"},
		},
		cursor: 0,
	}

	result := m.UpdateDashboard(bubbletea.KeyMsg{Type: bubbletea.KeyCtrlC})
	if result.Action != DashboardActionQuit {
		t.Fatalf("expected quit action, got %q", result.Action)
	}
	if _, ok := result.Cmd().(bubbletea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg from ctrl+c, got %T", result.Cmd())
	}
	if result.Model.cursor != 0 {
		t.Fatalf("ctrl+c should not mutate cursor, got %d", result.Model.cursor)
	}
}

func TestUpdateDashboardEnterSelects(t *testing.T) {
	m := model{
		devices: []models.SendModel{
			{IP: "192.168.1.10", DeviceName: "MacBook Pro"},
		},
		cursor: 0,
	}

	result := m.UpdateDashboard(bubbletea.KeyMsg{Type: bubbletea.KeyEnter})
	if result.Action != DashboardActionSelect {
		t.Fatalf("expected select action, got %q", result.Action)
	}
	if _, ok := result.Cmd().(bubbletea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg from enter, got %T", result.Cmd())
	}
}
