// Package prompt blocks PrepareReceive on a TTY approval prompt for incoming
// LocalSend sessions. Returns Accept / AcceptAlways / Reject so the caller
// can persist trust and choose the right HTTP status.
package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type Decision int

const (
	Reject Decision = iota
	Accept
	AcceptAlways
)

// DefaultTimeout is the default deadline for a user response. The HTTP
// goroutine handling PrepareReceive uses this so a sender's request can't
// hang forever on a forgotten prompt.
const DefaultTimeout = 60 * time.Second

var (
	ErrBusy    = errors.New("approval prompt is already active for another session")
	ErrTimeout = errors.New("approval prompt timed out")
	ErrNoTTY   = errors.New("stdin is not a TTY; cannot prompt")
	ErrAborted = errors.New("approval prompt aborted")
)

// FileSummary is the minimal info shown to the user. We don't print the full
// list; we cap to the first 3 names below.
type FileSummary struct {
	Name string
	Size int64
}

// promptMu enforces "only one prompt at a time". A second incoming session
// while a prompt is active gets ErrBusy, which the handler maps to HTTP 409.
var promptMu sync.Mutex

// Default reader/writer point at the real stdio. Tests swap them.
var (
	defaultReader io.Reader = os.Stdin
	defaultWriter io.Writer = os.Stderr
	isTTYFn                 = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// IsTTY reports whether stdin is interactive. Exposed so callers can choose
// between prompting and rejecting before they even acquire the lock.
func IsTTY() bool { return isTTYFn() }

// AskApproval prints a summary of the incoming session and reads a single-
// character answer from stdin. y/Y → Accept, n/N → Reject, a/A → AcceptAlways.
// Anything else re-prompts. Returns ErrTimeout when ctx fires, ErrBusy if a
// prompt is already running, ErrNoTTY when stdin is not interactive.
func AskApproval(ctx context.Context, alias, fingerprint string, files []FileSummary) (Decision, error) {
	return askWith(ctx, defaultReader, defaultWriter, alias, fingerprint, files)
}

func askWith(ctx context.Context, in io.Reader, out io.Writer, alias, fingerprint string, files []FileSummary) (Decision, error) {
	if !isTTYFn() {
		return Reject, ErrNoTTY
	}
	if !promptMu.TryLock() {
		return Reject, ErrBusy
	}
	defer promptMu.Unlock()

	printSummary(out, alias, fingerprint, files)

	answers := make(chan string, 1)
	readErr := make(chan error, 1)
	scanner := bufio.NewScanner(in)
	go func() {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				fmt.Fprint(out, "Accept files? [y]es / [n]o / [a]lways: ")
				continue
			}
			answers <- line
			return
		}
		if err := scanner.Err(); err != nil {
			readErr <- err
			return
		}
		readErr <- io.EOF
	}()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(out, "\n[localsend] approval timed out — rejecting.")
			return Reject, ErrTimeout
		case err := <-readErr:
			if errors.Is(err, io.EOF) {
				return Reject, ErrAborted
			}
			return Reject, fmt.Errorf("read stdin: %w", err)
		case ans := <-answers:
			switch strings.ToLower(ans)[:1] {
			case "y":
				return Accept, nil
			case "a":
				return AcceptAlways, nil
			case "n":
				return Reject, nil
			default:
				fmt.Fprint(out, "Please answer y, n, or a: ")
				go func() {
					for scanner.Scan() {
						line := strings.TrimSpace(scanner.Text())
						if line == "" {
							fmt.Fprint(out, "Please answer y, n, or a: ")
							continue
						}
						answers <- line
						return
					}
					if err := scanner.Err(); err != nil {
						readErr <- err
						return
					}
					readErr <- io.EOF
				}()
			}
		}
	}
}

func printSummary(out io.Writer, alias, fingerprint string, files []FileSummary) {
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}
	short := fingerprint
	if len(short) > 12 {
		short = short[:12] + "…"
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "[localsend] Incoming transfer\n")
	fmt.Fprintf(out, "  From:        %s (fingerprint %s)\n", alias, short)
	fmt.Fprintf(out, "  Files:       %d, total %s\n", len(files), humanBytes(totalSize))
	for i, f := range files {
		if i >= 3 {
			fmt.Fprintf(out, "  ...and %d more\n", len(files)-3)
			break
		}
		fmt.Fprintf(out, "  - %s (%s)\n", f.Name, humanBytes(f.Size))
	}
	fmt.Fprint(out, "Accept files? [y]es / [n]o / [a]lways: ")
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
