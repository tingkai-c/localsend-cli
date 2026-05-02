// Package approval owns UI-neutral authorization decisions for incoming
// LocalSend sessions. Handlers depend on this package instead of directly
// reading stdin, so a TUI can answer approval requests without terminal
// ownership conflicts.
package approval

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/prompt"
)

type Action string

const (
	Reject       Action = "reject"
	Accept       Action = "accept"
	AcceptAlways Action = "accept_always"
)

type File struct {
	Name string
	Size int64
}

type Request struct {
	Alias       string
	Fingerprint string
	Files       []File
	Deadline    time.Time
}

type Decision struct {
	Action Action
	Reason string
}

type Provider interface {
	AskApproval(context.Context, Request) (Decision, error)
}

type TrustStore interface {
	IsTrusted(fingerprint string) bool
	Add(fingerprint, alias string) error
}

var (
	ErrRejected   = errors.New("approval rejected")
	ErrBusy       = errors.New("approval request is already active")
	ErrTimeout    = errors.New("approval request timed out")
	ErrNoProvider = errors.New("no approval provider configured")
	ErrNoTTY      = errors.New("approval provider cannot prompt without a TTY")
)

type Policy struct {
	QuickSave bool
	Trust     TrustStore
	Provider  Provider
	Timeout   time.Duration
}

func (p Policy) Authorize(ctx context.Context, req Request) (Decision, error) {
	if p.QuickSave {
		return Decision{Action: Accept, Reason: "quick-save"}, nil
	}
	if p.Trust != nil && p.Trust.IsTrusted(req.Fingerprint) {
		return Decision{Action: Accept, Reason: "trusted"}, nil
	}
	if p.Provider == nil {
		return Decision{Action: Reject, Reason: "no-provider"}, ErrNoProvider
	}
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	decision, err := p.Provider.AskApproval(ctx, req)
	if err != nil {
		return Decision{Action: Reject, Reason: err.Error()}, normalizeErr(err)
	}
	switch decision.Action {
	case Accept:
		return decision, nil
	case AcceptAlways:
		if p.Trust != nil {
			if err := p.Trust.Add(req.Fingerprint, req.Alias); err != nil {
				return Decision{Action: Reject, Reason: "trust-persist-failed"}, fmt.Errorf("persist trust: %w", err)
			}
		}
		return decision, nil
	default:
		if decision.Reason == "" {
			decision.Reason = "rejected"
		}
		return decision, ErrRejected
	}
}

func normalizeErr(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, prompt.ErrTimeout), errors.Is(err, ErrTimeout):
		return ErrTimeout
	case errors.Is(err, prompt.ErrBusy), errors.Is(err, ErrBusy):
		return ErrBusy
	case errors.Is(err, prompt.ErrNoTTY), errors.Is(err, ErrNoTTY):
		return ErrNoTTY
	case errors.Is(err, ErrNoProvider):
		return ErrNoProvider
	default:
		return err
	}
}

// Manager serializes approval requests and applies a timeout around the
// provider. It is suitable for both stdin and TUI providers.
type Manager struct {
	provider Provider
	timeout  time.Duration
	mu       sync.Mutex
}

func NewManager(provider Provider, timeout time.Duration) *Manager {
	return &Manager{provider: provider, timeout: timeout}
}

func (m *Manager) AskApproval(ctx context.Context, req Request) (Decision, error) {
	if m == nil || m.provider == nil {
		return Decision{Action: Reject, Reason: "no-provider"}, ErrNoProvider
	}
	if !m.mu.TryLock() {
		return Decision{Action: Reject, Reason: "busy"}, ErrBusy
	}
	defer m.mu.Unlock()
	if m.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, m.timeout)
		defer cancel()
	}
	decision, err := m.provider.AskApproval(ctx, req)
	if err != nil {
		return Decision{Action: Reject, Reason: err.Error()}, normalizeErr(err)
	}
	return decision, nil
}

// StdinProvider adapts the legacy CLI prompt into the approval provider
// interface. It must only be used in explicit CLI/headless receive modes, not
// while a Bubble Tea program owns the terminal.
type StdinProvider struct{}

func (StdinProvider) AskApproval(ctx context.Context, req Request) (Decision, error) {
	files := make([]prompt.FileSummary, 0, len(req.Files))
	for _, file := range req.Files {
		files = append(files, prompt.FileSummary{Name: file.Name, Size: file.Size})
	}
	decision, err := prompt.AskApproval(ctx, req.Alias, req.Fingerprint, files)
	if err != nil {
		return Decision{Action: Reject, Reason: err.Error()}, normalizeErr(err)
	}
	switch decision {
	case prompt.Accept:
		return Decision{Action: Accept, Reason: "stdin"}, nil
	case prompt.AcceptAlways:
		return Decision{Action: AcceptAlways, Reason: "stdin-always"}, nil
	default:
		return Decision{Action: Reject, Reason: "stdin-reject"}, nil
	}
}

// PendingRequest is sent by ChannelProvider to a UI owner. The UI must reply
// exactly once using Respond.
type PendingRequest struct {
	Request  Request
	response chan Decision
}

func (p PendingRequest) Respond(decision Decision) {
	select {
	case p.response <- decision:
	default:
	}
}

// ChannelProvider bridges HTTP-handler goroutines to an event-loop UI such as
// Bubble Tea. Requests is receive-only to consumers so all replies go through
// PendingRequest.Respond.
type ChannelProvider struct {
	requests chan PendingRequest
}

func NewChannelProvider(buffer int) *ChannelProvider {
	if buffer < 1 {
		buffer = 1
	}
	return &ChannelProvider{requests: make(chan PendingRequest, buffer)}
}

func (p *ChannelProvider) Requests() <-chan PendingRequest {
	if p == nil {
		return nil
	}
	return p.requests
}

func (p *ChannelProvider) AskApproval(ctx context.Context, req Request) (Decision, error) {
	if p == nil {
		return Decision{Action: Reject, Reason: "no-provider"}, ErrNoProvider
	}
	pending := PendingRequest{Request: req, response: make(chan Decision, 1)}
	select {
	case p.requests <- pending:
	case <-ctx.Done():
		return Decision{Action: Reject, Reason: "timeout"}, ErrTimeout
	}
	select {
	case decision := <-pending.response:
		return decision, nil
	case <-ctx.Done():
		return Decision{Action: Reject, Reason: "timeout"}, ErrTimeout
	}
}
