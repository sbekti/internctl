package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sbekti/internctl/internal/config"
	"github.com/sbekti/internctl/internal/session"
)

func TestLoginPersistsProfileAndSession(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	var createRequest struct {
		ClientName string `json:"client_name"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/device_codes":
			if err := json.NewDecoder(r.Body).Decode(&createRequest); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"device_code":"device-code","user_code":"ABCD-EFGH","expires_in_seconds":60,"poll_interval_seconds":1}`))
		case "/api/v1/auth/tokens":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token","token_type":"Bearer","expires_in_seconds":900,"refresh_token":"refresh-token"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"login",
		"--config-dir", configDir,
		"--server", server.URL,
		"--token-backend", "file",
		"--client-name", "example-host",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if createRequest.ClientName != "example-host" {
		t.Fatalf("client_name = %q, want %q", createRequest.ClientName, "example-host")
	}
	if !strings.Contains(stdout.String(), "Login successful.") {
		t.Fatalf("stdout missing success message: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Verification URL: "+server.URL+"/auth/device?user_code=ABCD-EFGH") {
		t.Fatalf("stdout missing derived verification URL: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	cfg, err := config.Load(configDir)
	if err != nil {
		t.Fatalf("Load config returned error: %v", err)
	}
	profile := config.GetProfile(cfg, config.DefaultProfile)
	if profile.ServerURL != server.URL {
		t.Fatalf("server URL = %q, want %q", profile.ServerURL, server.URL)
	}
	if profile.TokenBackend != "file" {
		t.Fatalf("token backend = %q, want %q", profile.TokenBackend, "file")
	}

	manager := session.NewManager(configDir)
	data, actualBackend, err := manager.Load(config.DefaultProfile, session.BackendFile)
	if err != nil {
		t.Fatalf("Load session returned error: %v", err)
	}
	if actualBackend != session.BackendFile {
		t.Fatalf("backend = %q, want %q", actualBackend, session.BackendFile)
	}
	if data.RefreshToken != "refresh-token" {
		t.Fatalf("refresh token = %q, want %q", data.RefreshToken, "refresh-token")
	}
}

func TestLogoutClearsLocalSessionOnRemoteFailure(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	cfg := config.File{
		Profiles: map[string]config.Profile{
			config.DefaultProfile: {
				ServerURL:    "https://example.com",
				TokenBackend: "file",
			},
		},
	}
	if err := config.Save(configDir, cfg); err != nil {
		t.Fatalf("Save config returned error: %v", err)
	}

	manager := session.NewManager(configDir)
	if _, err := manager.Save(config.DefaultProfile, session.BackendFile, session.Data{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("Save session returned error: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/logout" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	profile := config.GetProfile(cfg, config.DefaultProfile)
	profile.ServerURL = server.URL
	cfg.Profiles[config.DefaultProfile] = profile
	if err := config.Save(configDir, cfg); err != nil {
		t.Fatalf("Save config returned error: %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"logout", "--config-dir", configDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "Signed out.") {
		t.Fatalf("stdout missing sign-out message: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Remote logout failed; local session cleared") {
		t.Fatalf("stderr missing warning: %s", stderr.String())
	}
	if _, _, err := manager.Load(config.DefaultProfile, session.BackendFile); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("session still present, err = %v", err)
	}
}

func TestLoginRefusesToReplaceExistingSessionWithoutForce(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	cfg := config.File{
		Profiles: map[string]config.Profile{
			config.DefaultProfile: {
				ServerURL:    "https://example.com",
				TokenBackend: "file",
			},
		},
	}
	if err := config.Save(configDir, cfg); err != nil {
		t.Fatalf("Save config returned error: %v", err)
	}

	manager := session.NewManager(configDir)
	if _, err := manager.Save(config.DefaultProfile, session.BackendFile, session.Data{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("Save session returned error: %v", err)
	}

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"login", "--config-dir", configDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), `profile "default" is already signed in`) {
		t.Fatalf("error = %q, want existing session message", err.Error())
	}
}
