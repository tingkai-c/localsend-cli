package approval

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeTrust struct {
	trusted map[string]bool
	added   []string
}

func (f *fakeTrust) IsTrusted(fp string) bool { return f.trusted[fp] }
func (f *fakeTrust) Add(fp, alias string) error {
	f.added = append(f.added, fp+":"+alias)
	f.trusted[fp] = true
	return nil
}

type providerFunc func(context.Context, Request) (Decision, error)

func (f providerFunc) AskApproval(ctx context.Context, req Request) (Decision, error) {
	return f(ctx, req)
}

func TestPolicyQuickSaveSkipsProvider(t *testing.T) {
	called := false
	decision, err := (Policy{
		QuickSave: true,
		Provider: providerFunc(func(context.Context, Request) (Decision, error) {
			called = true
			return Decision{Action: Reject}, nil
		}),
	}).Authorize(context.Background(), Request{Alias: "Phone"})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if called {
		t.Fatalf("quick save should skip provider")
	}
	if decision.Action != Accept || decision.Reason != "quick-save" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestPolicyTrustedSkipsProvider(t *testing.T) {
	called := false
	trust := &fakeTrust{trusted: map[string]bool{"fp": true}}
	decision, err := (Policy{
		Trust: trust,
		Provider: providerFunc(func(context.Context, Request) (Decision, error) {
			called = true
			return Decision{Action: Reject}, nil
		}),
	}).Authorize(context.Background(), Request{Alias: "Phone", Fingerprint: "fp"})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if called {
		t.Fatalf("trusted fingerprint should skip provider")
	}
	if decision.Action != Accept || decision.Reason != "trusted" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestPolicyNoProviderRejectsSafely(t *testing.T) {
	_, err := (Policy{}).Authorize(context.Background(), Request{Alias: "Phone"})
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("error = %v, want ErrNoProvider", err)
	}
}

func TestPolicyProviderDecisions(t *testing.T) {
	cases := []struct {
		name    string
		action  Action
		wantErr error
	}{
		{"accept", Accept, nil},
		{"reject", Reject, ErrRejected},
		{"always", AcceptAlways, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trust := &fakeTrust{trusted: map[string]bool{}}
			decision, err := (Policy{
				Trust: trust,
				Provider: providerFunc(func(context.Context, Request) (Decision, error) {
					return Decision{Action: tc.action}, nil
				}),
			}).Authorize(context.Background(), Request{Alias: "Phone", Fingerprint: "fp"})
			if tc.wantErr == nil && err != nil {
				t.Fatalf("Authorize() error = %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Authorize() error = %v, want %v", err, tc.wantErr)
			}
			if decision.Action != tc.action {
				t.Fatalf("decision = %q, want %q", decision.Action, tc.action)
			}
			if tc.action == AcceptAlways && len(trust.added) != 1 {
				t.Fatalf("accept always should persist trust, got %#v", trust.added)
			}
		})
	}
}

func TestManagerBusyAndTimeout(t *testing.T) {
	block := make(chan struct{})
	manager := NewManager(providerFunc(func(ctx context.Context, req Request) (Decision, error) {
		select {
		case <-block:
			return Decision{Action: Accept}, nil
		case <-ctx.Done():
			return Decision{Action: Reject}, ctx.Err()
		}
	}), 25*time.Millisecond)

	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = manager.AskApproval(context.Background(), Request{})
	}()
	<-started
	for i := 0; i < 50; i++ {
		_, err := manager.AskApproval(context.Background(), Request{})
		if errors.Is(err, ErrBusy) {
			close(block)
			return
		}
		time.Sleep(time.Millisecond)
	}
	close(block)
	t.Fatalf("expected ErrBusy while first approval is active")
}

func TestChannelProviderRoundTripAndTimeout(t *testing.T) {
	provider := NewChannelProvider(1)
	done := make(chan Decision, 1)
	go func() {
		decision, err := provider.AskApproval(context.Background(), Request{Alias: "Phone"})
		if err != nil {
			t.Errorf("AskApproval() error = %v", err)
		}
		done <- decision
	}()
	pending := <-provider.Requests()
	if pending.Request.Alias != "Phone" {
		t.Fatalf("pending alias = %q", pending.Request.Alias)
	}
	pending.Respond(Decision{Action: Accept, Reason: "test"})
	if decision := <-done; decision.Action != Accept {
		t.Fatalf("decision = %+v", decision)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := NewChannelProvider(1).AskApproval(ctx, Request{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout error = %v, want ErrTimeout", err)
	}
}
