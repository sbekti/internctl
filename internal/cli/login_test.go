package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func TestWaitForDeviceApprovalPendingThenSuccess(t *testing.T) {
	t.Parallel()

	var pollCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/tokens" {
			http.NotFound(w, r)
			return
		}

		call := atomic.AddInt32(&pollCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			w.WriteHeader(http.StatusPreconditionRequired)
			_, _ = w.Write([]byte(`{"error":"authorization_pending","error_description":"pending approval"}`))
			return
		}

		_, _ = w.Write([]byte(`{"access_token":"access-token","token_type":"Bearer","expires_in_seconds":900,"refresh_token":"refresh-token"}`))
	}))
	defer server.Close()

	client, err := httpclient.New(server.URL, "default", session.BackendFile, session.NewManager(t.TempDir()))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	tokenResponse, err := waitForDeviceApproval(
		context.Background(),
		client,
		"device-code",
		time.Millisecond,
		time.Second,
	)
	if err != nil {
		t.Fatalf("waitForDeviceApproval returned error: %v", err)
	}
	if tokenResponse.AccessToken != "access-token" {
		t.Fatalf("access token = %q, want %q", tokenResponse.AccessToken, "access-token")
	}
	if got := atomic.LoadInt32(&pollCalls); got != 2 {
		t.Fatalf("poll call count = %d, want 2", got)
	}
}

func TestWaitForDeviceApprovalDenied(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/tokens" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"denied"}`))
	}))
	defer server.Close()

	client, err := httpclient.New(server.URL, "default", session.BackendFile, session.NewManager(t.TempDir()))
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = waitForDeviceApproval(
		context.Background(),
		client,
		"device-code",
		time.Millisecond,
		time.Second,
	)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Fatalf("error = %q, want denied message", err.Error())
	}
}
