package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	keyring "github.com/zalando/go-keyring"

	"github.com/sbekti/internctl/internal/config"
)

const (
	ServiceName            = "internctl"
	BackendAuto    Backend = "auto"
	BackendKeyring Backend = "keyring"
	BackendFile    Backend = "file"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidBackend  = errors.New("invalid token backend")
)

type Backend string

type Data struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type keyringClient interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type osKeyring struct{}

type Manager struct {
	configDir   string
	serviceName string
	keyring     keyringClient
}

func ParseBackend(value string) (Backend, error) {
	switch strings.TrimSpace(value) {
	case "", string(BackendAuto):
		return BackendAuto, nil
	case string(BackendKeyring):
		return BackendKeyring, nil
	case string(BackendFile):
		return BackendFile, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidBackend, value)
	}
}

func NewManager(configDir string) *Manager {
	return &Manager{
		configDir:   configDir,
		serviceName: ServiceName,
		keyring:     osKeyring{},
	}
}

func (m *Manager) Load(profile string, backend Backend) (Data, Backend, error) {
	normalizedProfile, err := config.NormalizeProfileName(profile)
	if err != nil {
		return Data{}, "", err
	}

	switch backend {
	case BackendAuto:
		data, err := m.loadFromKeyring(normalizedProfile)
		if err == nil {
			return data, BackendKeyring, nil
		}
		if !errors.Is(err, ErrSessionNotFound) {
			data, fileErr := m.loadFromFile(normalizedProfile)
			if fileErr == nil {
				return data, BackendFile, nil
			}
			return Data{}, "", err
		}

		data, err = m.loadFromFile(normalizedProfile)
		if err == nil {
			return data, BackendFile, nil
		}
		return Data{}, "", err
	case BackendKeyring:
		data, err := m.loadFromKeyring(normalizedProfile)
		return data, BackendKeyring, err
	case BackendFile:
		data, err := m.loadFromFile(normalizedProfile)
		return data, BackendFile, err
	default:
		return Data{}, "", fmt.Errorf("%w: %q", ErrInvalidBackend, backend)
	}
}

func (m *Manager) Save(profile string, preferred Backend, data Data) (Backend, error) {
	normalizedProfile, err := config.NormalizeProfileName(profile)
	if err != nil {
		return "", err
	}

	switch preferred {
	case BackendAuto:
		if err := m.saveToKeyring(normalizedProfile, data); err == nil {
			return BackendKeyring, nil
		}
		if err := m.saveToFile(normalizedProfile, data); err != nil {
			return "", err
		}
		return BackendFile, nil
	case BackendKeyring:
		if err := m.saveToKeyring(normalizedProfile, data); err != nil {
			return "", err
		}
		return BackendKeyring, nil
	case BackendFile:
		if err := m.saveToFile(normalizedProfile, data); err != nil {
			return "", err
		}
		return BackendFile, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidBackend, preferred)
	}
}

func (m *Manager) Delete(profile string, backend Backend) error {
	normalizedProfile, err := config.NormalizeProfileName(profile)
	if err != nil {
		return err
	}

	switch backend {
	case BackendAuto:
		keyringErr := m.deleteFromKeyring(normalizedProfile)
		fileErr := m.deleteFromFile(normalizedProfile)
		if keyringErr == nil || errors.Is(keyringErr, ErrSessionNotFound) {
			keyringErr = nil
		}
		if fileErr == nil || errors.Is(fileErr, ErrSessionNotFound) {
			fileErr = nil
		}
		if keyringErr != nil {
			return keyringErr
		}
		return fileErr
	case BackendKeyring:
		return m.deleteFromKeyring(normalizedProfile)
	case BackendFile:
		return m.deleteFromFile(normalizedProfile)
	default:
		return fmt.Errorf("%w: %q", ErrInvalidBackend, backend)
	}
}

func (m *Manager) Exists(profile string) (bool, error) {
	normalizedProfile, err := config.NormalizeProfileName(profile)
	if err != nil {
		return false, err
	}

	if _, err := m.keyring.Get(m.serviceName, normalizedProfile); err == nil {
		return true, nil
	} else if !errors.Is(err, keyring.ErrNotFound) {
		// Ignore transient keyring availability errors so file-backed workflows
		// still function on hosts without a running secret-service daemon.
	}

	if _, err := os.Stat(m.sessionPath(normalizedProfile)); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, fmt.Errorf("stat session file: %w", err)
	}

	return false, nil
}

func (m *Manager) loadFromKeyring(profile string) (Data, error) {
	secret, err := m.keyring.Get(m.serviceName, profile)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return Data{}, ErrSessionNotFound
		}
		return Data{}, fmt.Errorf("read keyring session: %w", err)
	}

	var data Data
	if err := json.Unmarshal([]byte(secret), &data); err != nil {
		return Data{}, fmt.Errorf("decode keyring session: %w", err)
	}

	return data, nil
}

func (m *Manager) saveToKeyring(profile string, data Data) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode keyring session: %w", err)
	}

	if err := m.keyring.Set(m.serviceName, profile, string(encoded)); err != nil {
		return fmt.Errorf("write keyring session: %w", err)
	}

	return nil
}

func (m *Manager) deleteFromKeyring(profile string) error {
	if err := m.keyring.Delete(m.serviceName, profile); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("delete keyring session: %w", err)
	}

	return nil
}

func (m *Manager) loadFromFile(profile string) (Data, error) {
	path := m.sessionPath(profile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Data{}, ErrSessionNotFound
		}
		return Data{}, fmt.Errorf("read session file: %w", err)
	}

	var data Data
	if err := json.Unmarshal(raw, &data); err != nil {
		return Data{}, fmt.Errorf("decode session file: %w", err)
	}

	return data, nil
}

func (m *Manager) saveToFile(profile string, data Data) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session file: %w", err)
	}
	encoded = append(encoded, '\n')

	if err := os.MkdirAll(filepath.Dir(m.sessionPath(profile)), 0o700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	return writeFileAtomically(m.sessionPath(profile), encoded, 0o600)
}

func (m *Manager) deleteFromFile(profile string) error {
	path := m.sessionPath(profile)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("delete session file: %w", err)
	}
	return nil
}

func (m *Manager) sessionPath(profile string) string {
	return filepath.Join(m.configDir, "profiles", profile, "session.json")
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	defer cleanup()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}

	return nil
}

func (osKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (osKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (osKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}
