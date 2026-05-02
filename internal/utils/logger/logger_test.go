package logger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestSuppressInfoAndBelowRestoresPreviousLevel(t *testing.T) {
	var out bytes.Buffer
	InitLogger(LogConfig{
		Level:        logrus.InfoLevel,
		Output:       &out,
		Formatter:    &logrus.TextFormatter{DisableTimestamp: true, DisableColors: true},
		ReportCaller: false,
	})

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
