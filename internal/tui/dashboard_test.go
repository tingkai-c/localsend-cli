package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	bubbletea "github.com/charmbracelet/bubbletea"
	"github.com/sirupsen/logrus"
	"github.com/tingkai-c/localsend-cli/internal/approval"
	"github.com/tingkai-c/localsend-cli/internal/history"
	"github.com/tingkai-c/localsend-cli/internal/trust"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
)

func TestMainDashboardDispatchUsesTypedActionNotLabel(t *testing.T) {
	m := newDashboardModel(MainDeps{})
	m.items[0].label = "renamed send label"

	updated, cmd := m.activateCurrentItem()
	got := updated.(dashboardModel)
	if got.view != dashboardViewSend {
		t.Fatalf("expected typed send action to open send view, got %q", got.view)
	}
	if cmd != nil {
		t.Fatalf("send prompt should not quit immediately")
	}
}

func TestMainDashboardKeyNavigationAndQuit(t *testing.T) {
	m := newDashboardModel(MainDeps{})
	updated, _ := m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune("j")})
	m = updated.(dashboardModel)
	if m.cursor != 1 {
		t.Fatalf("j should move cursor to 1, got %d", m.cursor)
	}
	updated, _ = m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune("k")})
	m = updated.(dashboardModel)
	if m.cursor != 0 {
		t.Fatalf("k should move cursor to 0, got %d", m.cursor)
	}
	updated, cmd := m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyCtrlC})
	m = updated.(dashboardModel)
	if m.selected != MainActionExit {
		t.Fatalf("ctrl+c selected %q, want exit", m.selected)
	}
	if _, ok := cmd().(bubbletea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg from ctrl+c, got %T", cmd())
	}
}

func TestMainDashboardViewTransitions(t *testing.T) {
	cases := []struct {
		action MainAction
		view   dashboardView
	}{
		{MainActionHistory, dashboardViewHistory},
		{MainActionTrusted, dashboardViewTrusted},
		{MainActionSettings, dashboardViewSettings},
	}
	for _, tc := range cases {
		t.Run(string(tc.action), func(t *testing.T) {
			m := newDashboardModel(MainDeps{})
			for i, item := range m.items {
				if item.action == tc.action {
					m.cursor = i
					break
				}
			}
			updated, cmd := m.activateCurrentItem()
			got := updated.(dashboardModel)
			if got.view != tc.view {
				t.Fatalf("view = %q, want %q", got.view, tc.view)
			}
			if cmd != nil {
				t.Fatalf("placeholder screen should stay in TUI, got cmd")
			}

			updated, _ = got.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyEsc})
			got = updated.(dashboardModel)
			if got.view != dashboardViewMenu {
				t.Fatalf("esc should return to menu, got %q", got.view)
			}
		})
	}
}

func TestMainDashboardSendPromptResult(t *testing.T) {
	m := newDashboardModel(MainDeps{})
	updated, _ := m.activateCurrentItem()
	m = updated.(dashboardModel)
	for _, r := range "file.txt" {
		updated, _ = m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune{r}})
		m = updated.(dashboardModel)
	}
	updated, cmd := m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyEnter})
	m = updated.(dashboardModel)
	if m.selected != MainActionSend {
		t.Fatalf("selected = %q, want send", m.selected)
	}
	if m.result().FilePath != "file.txt" {
		t.Fatalf("file path = %q", m.result().FilePath)
	}
	if _, ok := cmd().(bubbletea.QuitMsg); !ok {
		t.Fatalf("expected send enter to quit, got %T", cmd())
	}
}

func TestMainDashboardRendersDependencyStates(t *testing.T) {
	m := newDashboardModel(MainDeps{
		DeviceName:  "Laptop",
		Port:        53317,
		OutputDir:   "/tmp/downloads",
		ConfigPath:  "/tmp/config.yaml",
		HistoryPath: "/tmp/history.json",
		TrustPath:   "/tmp/trusted.yaml",
	})

	m.view = dashboardViewHistory
	if view := m.View(); !strings.Contains(view, "No transfer history yet") {
		t.Fatalf("expected empty history state, got:\n%s", view)
	}

	m.view = dashboardViewTrusted
	if view := m.View(); !strings.Contains(view, "No trusted senders yet") {
		t.Fatalf("expected empty trusted state, got:\n%s", view)
	}

	m.deps.History = []history.Record{{
		Direction:   history.DirectionSent,
		Status:      history.StatusCompleted,
		FileName:    "photo.jpg",
		Size:        2048,
		PeerAlias:   "Phone",
		CompletedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}}
	m.deps.Trusted = []trust.Entry{{Fingerprint: "abcdef123456", Alias: "Phone", AddedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}}
	m.view = dashboardViewHistory
	if view := m.View(); !strings.Contains(view, "photo.jpg") || !strings.Contains(view, "Phone") {
		t.Fatalf("expected populated history state, got:\n%s", view)
	}
	m.view = dashboardViewTrusted
	if view := m.View(); !strings.Contains(view, "abcdef123456") || !strings.Contains(view, "Phone") {
		t.Fatalf("expected populated trusted state, got:\n%s", view)
	}
}

func TestMainDashboardApprovalModalDecisions(t *testing.T) {
	provider := approval.NewChannelProvider(1)
	decisionCh := make(chan approval.Decision, 1)
	go func() {
		decision, err := provider.AskApproval(context.Background(), approval.Request{
			Alias:       "Phone",
			Fingerprint: "abcdef1234567890",
			Files:       []approval.File{{Name: "photo.jpg", Size: 1024}},
		})
		if err != nil {
			t.Errorf("AskApproval() error = %v", err)
		}
		decisionCh <- decision
	}()

	m := newDashboardModel(MainDeps{ApprovalRequests: provider.Requests()})
	pending := <-provider.Requests()
	updated, _ := m.Update(approvalRequestedMsg{Pending: pending})
	m = updated.(dashboardModel)
	view := m.View()
	if !strings.Contains(view, "🔔 Incoming transfer") || !strings.Contains(view, "Phone") {
		t.Fatalf("approval modal not rendered:\n%s", view)
	}

	updated, _ = m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune("a")})
	m = updated.(dashboardModel)
	if m.pendingApproval != nil {
		t.Fatalf("approval should clear after decision")
	}
	select {
	case decision := <-decisionCh:
		if decision.Action != approval.AcceptAlways {
			t.Fatalf("decision = %+v, want accept always", decision)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for approval decision")
	}
}

func TestMainDashboardHistoryDeleteAndClear(t *testing.T) {
	deleted := ""
	cleared := false
	m := newDashboardModel(MainDeps{
		History: []history.Record{
			{ID: "one", FileName: "one.txt", CompletedAt: time.Now()},
			{ID: "two", FileName: "two.txt", CompletedAt: time.Now()},
		},
		DeleteHistory: func(id string) error { deleted = id; return nil },
		ClearHistory:  func() error { cleared = true; return nil },
	})
	m.view = dashboardViewHistory
	m.historyCursor = 1
	updated, _ := m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune("d")})
	m = updated.(dashboardModel)
	if deleted != "two" {
		t.Fatalf("deleted = %q, want two", deleted)
	}
	if len(m.deps.History) != 1 || m.deps.History[0].ID != "one" {
		t.Fatalf("history = %#v", m.deps.History)
	}
	updated, _ = m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune("c")})
	m = updated.(dashboardModel)
	if !cleared || len(m.deps.History) != 0 {
		t.Fatalf("clear failed: cleared=%v history=%#v", cleared, m.deps.History)
	}
}

func TestMainDashboardForgetTrusted(t *testing.T) {
	forgot := ""
	m := newDashboardModel(MainDeps{
		Trusted: []trust.Entry{
			{Fingerprint: "fp-one", Alias: "One", AddedAt: time.Now()},
			{Fingerprint: "fp-two", Alias: "Two", AddedAt: time.Now()},
		},
		ForgetTrusted: func(query string) error { forgot = query; return nil },
	})
	m.view = dashboardViewTrusted
	m.trustedCursor = 1
	updated, _ := m.updateKey(bubbletea.KeyMsg{Type: bubbletea.KeyRunes, Runes: []rune("x")})
	m = updated.(dashboardModel)
	if forgot != "fp-two" {
		t.Fatalf("forgot = %q, want fp-two", forgot)
	}
	if len(m.deps.Trusted) != 1 || m.deps.Trusted[0].Fingerprint != "fp-one" {
		t.Fatalf("trusted = %#v", m.deps.Trusted)
	}
}

func TestMainDashboardCentersInWindowAndUsesModernIcons(t *testing.T) {
	m := newDashboardModel(MainDeps{DeviceName: "Laptop", Port: 53317, OutputDir: "/tmp/downloads"})
	updated, _ := m.Update(bubbletea.WindowSizeMsg{Width: 100, Height: 36})
	m = updated.(dashboardModel)

	view := m.View()
	if firstLine := strings.SplitN(view, "\n", 2)[0]; strings.TrimSpace(firstLine) != "" {
		t.Fatalf("expected vertically centered dashboard to start with vertical padding, first line %q", firstLine)
	}
	if !strings.Contains(view, "✨ LocalSend CLI") || !strings.Contains(view, "🚀 Send") || !strings.Contains(view, "🌐 Web Portal") {
		t.Fatalf("expected modern dashboard icons, got:\n%s", view)
	}
	if strings.Contains(view, "📡 Receive") {
		t.Fatalf("dashboard should not render redundant receive menu entry:\n%s", view)
	}
}

func TestMainDashboardRendersLogNotification(t *testing.T) {
	m := newDashboardModel(MainDeps{DeviceName: "Laptop", Port: 53317, OutputDir: "/tmp/downloads"})
	updated, _ := m.Update(bubbletea.WindowSizeMsg{Width: 100, Height: 36})
	m = updated.(dashboardModel)

	updated, _ = m.Update(logNotificationMsg{Event: logger.LogEvent{
		Level:   logrus.ErrorLevel,
		Message: "failed to load trust file",
	}})
	m = updated.(dashboardModel)

	view := m.View()
	if !strings.Contains(view, "⚠ ERROR") || !strings.Contains(view, "failed to load trust file") {
		t.Fatalf("expected log notification in dashboard view, got:\n%s", view)
	}
	if !strings.Contains(view, "✨ LocalSend CLI") {
		t.Fatalf("notification should not replace dashboard content:\n%s", view)
	}

	updated, _ = m.Update(clearLogNotificationMsg{ID: m.logNotification.ID})
	m = updated.(dashboardModel)
	if strings.Contains(m.View(), "failed to load trust file") {
		t.Fatalf("notification should clear after matching expiry")
	}
}

func TestMainDashboardLogNotificationDoesNotReplaceApprovalModal(t *testing.T) {
	provider := approval.NewChannelProvider(1)
	go func() {
		_, _ = provider.AskApproval(context.Background(), approval.Request{
			Alias:       "Phone",
			Fingerprint: "abcdef1234567890",
			Files:       []approval.File{{Name: "photo.jpg", Size: 1024}},
		})
	}()
	pending := <-provider.Requests()
	defer pending.Respond(approval.Decision{Action: approval.Reject, Reason: "test"})

	m := newDashboardModel(MainDeps{DeviceName: "Laptop", Port: 53317, OutputDir: "/tmp/downloads"})
	updated, _ := m.Update(logNotificationMsg{Event: logger.LogEvent{
		Level:   logrus.WarnLevel,
		Message: "background warning",
	}})
	m = updated.(dashboardModel)
	updated, _ = m.Update(approvalRequestedMsg{Pending: pending})
	m = updated.(dashboardModel)

	view := m.View()
	if !strings.Contains(view, "🔔 Incoming transfer") || !strings.Contains(view, "background warning") {
		t.Fatalf("approval modal and notification should render together:\n%s", view)
	}
}
