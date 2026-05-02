package logger

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func configureTestLogger(t *testing.T, out io.Writer) {
	t.Helper()
	InitLogger()
	log := GetLogger()
	log.SetLevel(logrus.InfoLevel)
	log.SetOutput(out)
	log.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})
	log.SetReportCaller(false)
	t.Cleanup(func() {
		log.SetLevel(logrus.InfoLevel)
		log.SetOutput(io.Discard)
	})
}

func TestSuppressInfoAndBelowRestoresPreviousLevel(t *testing.T) {
	var out bytes.Buffer
	configureTestLogger(t, &out)

	Info("before")
	if !strings.Contains(out.String(), "before") {
		t.Fatalf("expected info before suppression, got %q", out.String())
	}

	out.Reset()
	restore := SuppressInfoAndBelow()
	Info("hidden")
	Warn("visible warning")
	if strings.Contains(out.String(), "hidden") {
		t.Fatalf("info should be suppressed, got %q", out.String())
	}
	if !strings.Contains(out.String(), "visible warning") {
		t.Fatalf("warn should remain visible, got %q", out.String())
	}

	out.Reset()
	restore()
	restore()
	Info("after")
	if !strings.Contains(out.String(), "after") {
		t.Fatalf("expected info after restore, got %q", out.String())
	}
}

func TestCaptureWarningsAndErrorsRoutesEventsAndRestoresOutput(t *testing.T) {
	var out bytes.Buffer
	configureTestLogger(t, &out)

	events, restore := CaptureWarningsAndErrors(2)
	Info("hidden info")
	Warn("visible warning")
	Error("visible error")
	if got := out.String(); got != "" {
		t.Fatalf("dashboard capture should suppress raw output, got %q", got)
	}

	first := readLogEvent(t, events)
	second := readLogEvent(t, events)
	if first.Level != logrus.WarnLevel || !strings.Contains(first.Message, "visible warning") {
		t.Fatalf("first event = %+v, want warning", first)
	}
	if second.Level != logrus.ErrorLevel || !strings.Contains(second.Message, "visible error") {
		t.Fatalf("second event = %+v, want error", second)
	}

	restore()
	restore()
	Warn("after restore")
	if !strings.Contains(out.String(), "after restore") {
		t.Fatalf("expected warning after restore in output, got %q", out.String())
	}
	select {
	case event := <-events:
		t.Fatalf("unexpected post-restore event: %+v", event)
	default:
	}
}

func TestCaptureWarningsAndErrorsDoesNotBlockWhenFull(t *testing.T) {
	var out bytes.Buffer
	configureTestLogger(t, &out)
	_, restore := CaptureWarningsAndErrors(1)
	defer restore()

	Warn("fills buffer")
	done := make(chan struct{})
	go func() {
		Warn("dropped instead of blocking")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("logging blocked when dashboard notification channel was full")
	}
}

func readLogEvent(t *testing.T, events <-chan LogEvent) LogEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log event")
		return LogEvent{}
	}
}
