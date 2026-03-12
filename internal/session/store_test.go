package session

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	keyring "github.com/zalando/go-keyring"
)

type fakeKeyring struct {
	values    map[string]string
	setErr    error
	getErr    error
	deleteErr error
}

func (f *fakeKeyring) Set(service, user, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = make(map[string]string)
	}
	f.values[service+"/"+user] = password
	return nil
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	value, ok := f.values[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.values[service+"/"+user]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.values, service+"/"+user)
	return nil
}

func TestManagerSaveAutoFallsBackToFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manager := &Manager{
		configDir:   dir,
		serviceName: ServiceName,
		keyring:     &fakeKeyring{setErr: errors.New("keyring unavailable")},
	}

	data := Data{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Unix(1700000000, 0).UTC(),
	}

	backend, err := manager.Save("default", BackendAuto, data)
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if backend != BackendFile {
		t.Fatalf("Save backend = %q, want %q", backend, BackendFile)
	}

	if _, err := filepath.Abs(filepath.Join(dir, "profiles", "default", "session.json")); err != nil {
		t.Fatalf("resolve session path: %v", err)
	}

	loaded, actualBackend, err := manager.Load("default", BackendAuto)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if actualBackend != BackendFile {
		t.Fatalf("Load backend = %q, want %q", actualBackend, BackendFile)
	}
	if loaded.RefreshToken != data.RefreshToken {
		t.Fatalf("loaded refresh token = %q, want %q", loaded.RefreshToken, data.RefreshToken)
	}
}

func TestManagerLoadKeepsExplicitFileBackend(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manager := &Manager{
		configDir:   dir,
		serviceName: ServiceName,
		keyring:     &fakeKeyring{},
	}

	data := Data{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Unix(1700000000, 0).UTC(),
	}

	if _, err := manager.Save("default", BackendFile, data); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, actualBackend, err := manager.Load("default", BackendFile)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if actualBackend != BackendFile {
		t.Fatalf("Load backend = %q, want %q", actualBackend, BackendFile)
	}
	if loaded.AccessToken != data.AccessToken {
		t.Fatalf("loaded access token = %q, want %q", loaded.AccessToken, data.AccessToken)
	}
}
