package prompt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// withTTY forces isTTYFn to true for the duration of a test, restoring the
// original at the end. AskApproval refuses to prompt when stdin is not a TTY,
// so test machines (often non-TTY) need this override.
func withTTY(t *testing.T) {
	t.Helper()
	orig := isTTYFn
	isTTYFn = func() bool { return true }
	t.Cleanup(func() { isTTYFn = orig })
}

func files() []FileSummary {
	return []FileSummary{{Name: "a.txt", Size: 10}, {Name: "b.bin", Size: 1024}}
}

func TestAskApproval_Accept(t *testing.T) {
	withTTY(t)
	in := strings.NewReader("y\n")
	out := &bytes.Buffer{}
	d, err := askWith(context.Background(), in, out, "Alice", "fp123", files())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d != Accept {
		t.Fatalf("got decision %v, want Accept", d)
	}
}

func TestAskApproval_Reject(t *testing.T) {
	withTTY(t)
	in := strings.NewReader("n\n")
	out := &bytes.Buffer{}
	d, err := askWith(context.Background(), in, out, "Alice", "fp123", files())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d != Reject {
		t.Fatalf("got decision %v, want Reject", d)
	}
}

func TestAskApproval_Always(t *testing.T) {
	withTTY(t)
	in := strings.NewReader("a\n")
	out := &bytes.Buffer{}
	d, err := askWith(context.Background(), in, out, "Alice", "fp123", files())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d != AcceptAlways {
		t.Fatalf("got decision %v, want AcceptAlways", d)
	}
}

func TestAskApproval_RetriesOnInvalid(t *testing.T) {
	withTTY(t)
	in := strings.NewReader("maybe\nq\ny\n")
	out := &bytes.Buffer{}
	d, err := askWith(context.Background(), in, out, "Alice", "fp123", files())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d != Accept {
		t.Fatalf("got decision %v, want Accept after retries", d)
	}
}

func TestAskApproval_Timeout(t *testing.T) {
	withTTY(t)
	// Reader that never returns — simulates a user who walks away.
	in, _ := io.Pipe()
	out := &bytes.Buffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	d, err := askWith(ctx, in, out, "Alice", "fp123", files())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if d != Reject {
		t.Fatalf("on timeout decision must be Reject, got %v", d)
	}
}

func TestAskApproval_NoTTY(t *testing.T) {
	orig := isTTYFn
	isTTYFn = func() bool { return false }
	t.Cleanup(func() { isTTYFn = orig })

	d, err := askWith(context.Background(), strings.NewReader("y\n"), &bytes.Buffer{}, "Alice", "fp", files())
	if !errors.Is(err, ErrNoTTY) {
		t.Fatalf("want ErrNoTTY, got %v", err)
	}
	if d != Reject {
		t.Fatalf("decision must be Reject when no TTY, got %v", d)
	}
}

func TestAskApproval_Busy(t *testing.T) {
	withTTY(t)
	// Hold the lock by parking one prompt on a never-returning reader.
	holdIn, _ := io.Pipe()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, _ = askWith(ctx, holdIn, &bytes.Buffer{}, "First", "fpA", files())
	}()
	// Give the goroutine a moment to acquire the mutex.
	time.Sleep(20 * time.Millisecond)

	d, err := askWith(context.Background(), strings.NewReader("y\n"), &bytes.Buffer{}, "Second", "fpB", files())
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy while another prompt is active, got %v", err)
	}
	if d != Reject {
		t.Fatalf("decision must be Reject when busy, got %v", d)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1024 * 1024, "1.0 MiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
