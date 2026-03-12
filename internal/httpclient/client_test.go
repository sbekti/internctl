package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/session"
)

func TestClientGetProfileRefreshesOn401(t *testing.T) {
	t.Parallel()

	manager := session.NewManager(t.TempDir())
	if _, err := manager.Save("default", session.BackendFile, session.Data{
		AccessToken:  "stale-access",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	var profileCalls int32
	var refreshCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/profile":
			atomic.AddInt32(&profileCalls, 1)
			if r.Header.Get("Authorization") != "Bearer fresh-access" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"unauthorized","message":"invalid token"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"username":"alice","name":"Alice Example","email":"alice@example.com","groups":["Users"],"is_admin":false}`))
		case "/api/v1/auth/tokens/refresh":
			atomic.AddInt32(&refreshCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"fresh-access","token_type":"Bearer","expires_in_seconds":900,"refresh_token":"fresh-refresh"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "default", session.BackendFile, manager)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	profile, err := client.GetProfile(context.Background())
	if err != nil {
		t.Fatalf("GetProfile returned error: %v", err)
	}
	if profile.Username != "alice" {
		t.Fatalf("profile username = %q, want %q", profile.Username, "alice")
	}
	if got := atomic.LoadInt32(&profileCalls); got != 2 {
		t.Fatalf("profile call count = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 1 {
		t.Fatalf("refresh call count = %d, want 1", got)
	}

	loaded, _, err := manager.Load("default", session.BackendFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.RefreshToken != "fresh-refresh" {
		t.Fatalf("refresh token = %q, want %q", loaded.RefreshToken, "fresh-refresh")
	}
}

func TestClientGetProfileClearsSessionOnRefreshUnauthorized(t *testing.T) {
	t.Parallel()

	manager := session.NewManager(t.TempDir())
	if _, err := manager.Save("default", session.BackendFile, session.Data{
		AccessToken:  "stale-access",
		RefreshToken: "revoked-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/profile":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthorized","message":"invalid token"}`))
		case "/api/v1/auth/tokens/refresh":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthorized","message":"refresh token is invalid"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "default", session.BackendFile, manager)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = client.GetProfile(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("GetProfile error = %v, want ErrUnauthorized", err)
	}

	_, _, err = manager.Load("default", session.BackendFile)
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("Load error = %v, want ErrSessionNotFound", err)
	}
}

func TestClientGetProfileDoesNotRefreshOnNon401(t *testing.T) {
	t.Parallel()

	manager := session.NewManager(t.TempDir())
	if _, err := manager.Save("default", session.BackendFile, session.Data{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	var refreshCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/profile":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"forbidden","message":"not allowed"}`))
		case "/api/v1/auth/tokens/refresh":
			atomic.AddInt32(&refreshCalls, 1)
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "default", session.BackendFile, manager)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = client.GetProfile(context.Background())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if got := atomic.LoadInt32(&refreshCalls); got != 0 {
		t.Fatalf("refresh call count = %d, want 0", got)
	}
}

func TestTokenResponseToSessionData(t *testing.T) {
	t.Parallel()

	data := TokenResponseToSessionData(&api.TokenResponse{
		AccessToken:      "access",
		RefreshToken:     "refresh",
		TokenType:        "Bearer",
		ExpiresInSeconds: 60,
	})
	if data.AccessToken != "access" || data.RefreshToken != "refresh" || data.TokenType != "Bearer" {
		t.Fatalf("unexpected session data: %+v", data)
	}
	if time.Until(data.ExpiresAt) <= 0 {
		t.Fatalf("expected future expiry, got %v", data.ExpiresAt)
	}
}
