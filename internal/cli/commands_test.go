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

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/config"
	"github.com/sbekti/internctl/internal/session"
)

func writeLoggedInProfile(t *testing.T, configDir string, serverURL string) *session.Manager {
	t.Helper()

	cfg := config.File{
		Profiles: map[string]config.Profile{
			config.DefaultProfile: {
				ServerURL:    serverURL,
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

	return manager
}

func TestLoginPersistsProfileAndSession(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	profileName := "login-test"
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
			_, _ = w.Write([]byte(`{"device_code":"device-code","user_code":"ABCD-EFGH","verification_uri":"https://intern.corp.example.com/auth/device","verification_uri_complete":"https://intern.corp.example.com/auth/device?user_code=ABCD-EFGH","expires_in":60,"interval":1}`))
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
		"--profile", profileName,
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
	if !strings.Contains(stdout.String(), "Verification URL: https://intern.corp.example.com/auth/device?user_code=ABCD-EFGH") {
		t.Fatalf("stdout missing derived verification URL: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	cfg, err := config.Load(configDir)
	if err != nil {
		t.Fatalf("Load config returned error: %v", err)
	}
	profile := config.GetProfile(cfg, profileName)
	if profile.ServerURL != server.URL {
		t.Fatalf("server URL = %q, want %q", profile.ServerURL, server.URL)
	}
	if profile.TokenBackend != "file" {
		t.Fatalf("token backend = %q, want %q", profile.TokenBackend, "file")
	}

	manager := session.NewManager(configDir)
	data, actualBackend, err := manager.Load(profileName, session.BackendFile)
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

func TestVlansListPrintsTable(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/vlans" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("Authorization header = %q, want %q", got, "Bearer access-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"guest","vlan_id":10,"description":"Guest devices","is_active":true,"created_at":"2026-03-13T00:00:00Z","updated_at":"2026-03-13T00:00:00Z"}]}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"vlans", "list", "--config-dir", configDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "ID") || !strings.Contains(output, "VLAN ID") || !strings.Contains(output, "ACTIVE") {
		t.Fatalf("stdout missing table headers: %s", output)
	}
	if !strings.Contains(output, "guest") || !strings.Contains(output, "10") || !strings.Contains(output, "yes") {
		t.Fatalf("stdout missing VLAN row: %s", output)
	}
}

func TestDevicesListRequiresAdmin(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/devices" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden","message":"admin access required"}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"devices", "list", "--config-dir", configDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "admin access is required to list devices") {
		t.Fatalf("error = %q, want admin access message", err.Error())
	}
}

func TestVlansListJSONOutput(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/vlans" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":1,"name":"guest","vlan_id":10,"description":"Guest devices","is_active":true,"created_at":"2026-03-13T00:00:00Z","updated_at":"2026-03-13T00:00:00Z"}]}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"vlans", "list", "--config-dir", configDir, "--output", "json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"name": "guest"`) || !strings.Contains(stdout.String(), `"vlan_id": 10`) {
		t.Fatalf("stdout missing JSON VLAN fields: %s", stdout.String())
	}
}

func TestVlansCreatePrintsCreatedMessage(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	var request api.VlanWrite
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/vlans" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42,"name":"iot","vlan_id":20,"description":"IoT devices","is_active":true,"created_at":"2026-03-13T00:00:00Z","updated_at":"2026-03-13T00:00:00Z"}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"vlans", "create",
		"--config-dir", configDir,
		"--name", "iot",
		"--vlan-id", "20",
		"--description", "IoT devices",
		"--active",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if request.Name != "iot" || request.VlanId != 20 {
		t.Fatalf("unexpected create request: %+v", request)
	}
	if request.Description == nil || *request.Description != "IoT devices" {
		t.Fatalf("description = %+v, want IoT devices", request.Description)
	}
	if request.IsActive == nil || !*request.IsActive {
		t.Fatalf("is_active = %+v, want true", request.IsActive)
	}
	if !strings.Contains(stdout.String(), "Created VLAN 42 (iot).") {
		t.Fatalf("stdout missing create confirmation: %s", stdout.String())
	}
}

func TestVlansDeleteRequiresAdmin(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/vlans/42" || r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden","message":"admin access required"}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"vlans", "delete", "42", "--config-dir", configDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "admin access is required to delete VLANs") {
		t.Fatalf("error = %q, want admin access message", err.Error())
	}
}

func TestDevicesCreatePrintsCreatedMessage(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	var request api.NetworkDeviceWrite
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/devices" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000123","display_name":"Kitchen TV","mac_address":"aa:bb:cc:dd:ee:ff","vlan":{"id":1,"name":"trusted","vlan_id":1},"created_at":"2026-03-13T00:00:00Z","updated_at":"2026-03-13T00:00:00Z"}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"devices", "create",
		"--config-dir", configDir,
		"--name", "Kitchen TV",
		"--mac-address", "aa:bb:cc:dd:ee:ff",
		"--vlan-id", "1",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if request.DisplayName != "Kitchen TV" || request.MacAddress != "aa:bb:cc:dd:ee:ff" || request.VlanId != 1 {
		t.Fatalf("unexpected create request: %+v", request)
	}
	if !strings.Contains(stdout.String(), "Created device Kitchen TV (00000000-0000-0000-0000-000000000123).") {
		t.Fatalf("stdout missing create confirmation: %s", stdout.String())
	}
}

func TestDevicesListPrintsIDAndSupportsJSONOutput(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/devices" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"00000000-0000-0000-0000-000000000123","display_name":"Kitchen TV","mac_address":"aa:bb:cc:dd:ee:ff","vlan":{"id":1,"name":"trusted","vlan_id":1},"created_at":"2026-03-13T00:00:00Z","updated_at":"2026-03-13T00:00:00Z"}]}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	tableCmd := NewRootCommand()
	var tableOut bytes.Buffer
	tableCmd.SetOut(&tableOut)
	tableCmd.SetErr(new(bytes.Buffer))
	tableCmd.SetArgs([]string{"devices", "list", "--config-dir", configDir})

	if err := tableCmd.Execute(); err != nil {
		t.Fatalf("table Execute returned error: %v", err)
	}
	if !strings.Contains(tableOut.String(), "00000000-0000-0000-0000-000000000123") || !strings.Contains(tableOut.String(), "ID") {
		t.Fatalf("table output missing device id: %s", tableOut.String())
	}

	jsonCmd := NewRootCommand()
	var jsonOut bytes.Buffer
	jsonCmd.SetOut(&jsonOut)
	jsonCmd.SetErr(new(bytes.Buffer))
	jsonCmd.SetArgs([]string{"devices", "list", "--config-dir", configDir, "--output", "json"})

	if err := jsonCmd.Execute(); err != nil {
		t.Fatalf("json Execute returned error: %v", err)
	}
	if !strings.Contains(jsonOut.String(), `"id": "00000000-0000-0000-0000-000000000123"`) || !strings.Contains(jsonOut.String(), `"display_name": "Kitchen TV"`) {
		t.Fatalf("json output missing device fields: %s", jsonOut.String())
	}
}

func TestDevicesDeleteRequiresAdmin(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/networks/devices/00000000-0000-0000-0000-000000000123" || r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden","message":"admin access required"}`))
	}))
	defer server.Close()

	writeLoggedInProfile(t, configDir, server.URL)

	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"devices", "delete", "00000000-0000-0000-0000-000000000123", "--config-dir", configDir})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "admin access is required to delete devices") {
		t.Fatalf("error = %q, want admin access message", err.Error())
	}
}
