package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tingkai-c/localsend-cli/internal/approval"
	"github.com/tingkai-c/localsend-cli/internal/config"
	"github.com/tingkai-c/localsend-cli/internal/models"
)

type approvalProviderFunc func(context.Context, approval.Request) (approval.Decision, error)

func (f approvalProviderFunc) AskApproval(ctx context.Context, req approval.Request) (approval.Decision, error) {
	return f(ctx, req)
}

func withReceiveApprovalProvider(t *testing.T, provider approval.Provider) {
	t.Helper()
	oldQuickSave := config.ConfigData.QuickSave
	config.ConfigData.QuickSave = false
	SetApprovalProvider(provider)
	t.Cleanup(func() {
		config.ConfigData.QuickSave = oldQuickSave
		SetApprovalProvider(nil)
	})
}

func prepareReceiveRequest(t *testing.T) *http.Request {
	t.Helper()
	body, err := json.Marshal(models.PrepareReceiveRequest{
		Info: models.Info{Alias: "Phone", Fingerprint: "test-fingerprint-prepare-receive"},
		Files: map[string]models.FileInfo{
			"file.bin": {ID: "file.bin", FileName: "file.bin", Size: 42},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/localsend/v2/prepare-upload", bytes.NewReader(body))
}

func TestPrepareReceiveUsesApprovalProviderAccept(t *testing.T) {
	called := false
	withReceiveApprovalProvider(t, approvalProviderFunc(func(ctx context.Context, req approval.Request) (approval.Decision, error) {
		called = true
		if req.Alias != "Phone" || len(req.Files) != 1 {
			t.Fatalf("unexpected approval request: %+v", req)
		}
		return approval.Decision{Action: approval.Accept, Reason: "test"}, nil
	}))

	rr := httptest.NewRecorder()
	PrepareReceive(rr, prepareReceiveRequest(t))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatalf("approval provider was not called")
	}
}

func TestPrepareReceiveProviderRejectMapsForbidden(t *testing.T) {
	withReceiveApprovalProvider(t, approvalProviderFunc(func(ctx context.Context, req approval.Request) (approval.Decision, error) {
		return approval.Decision{Action: approval.Reject, Reason: "test"}, nil
	}))

	rr := httptest.NewRecorder()
	PrepareReceive(rr, prepareReceiveRequest(t))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}
