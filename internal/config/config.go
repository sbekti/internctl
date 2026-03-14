package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const DefaultProfile = "default"

var DefaultServerURL = "https://intern.corp.example.com"

var (
	ErrInvalidProfileName = errors.New("invalid profile name")

	profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type Profile struct {
	ServerURL    string `json:"server_url"`
	TokenBackend string `json:"token_backend,omitempty"`
}

type File struct {
	Profiles map[string]Profile `json:"profiles"`
}

func ResolveConfigDir(flagValue string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		return filepath.Clean(flagValue), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".intern"), nil
}

func NormalizeProfileName(value string) (string, error) {
	profile := strings.TrimSpace(value)
	if profile == "" {
		profile = DefaultProfile
	}

	if !profileNamePattern.MatchString(profile) {
		return "", fmt.Errorf("%w: %q", ErrInvalidProfileName, value)
	}

	return profile, nil
}

func Load(configDir string) (File, error) {
	path := filepath.Join(configDir, "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return File{Profiles: make(map[string]Profile)}, nil
		}
		return File{}, fmt.Errorf("read config: %w", err)
	}

	var cfg File
	if err := json.Unmarshal(data, &cfg); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	return cfg, nil
}

func Save(configDir string, cfg File) error {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}

	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(configDir, "config.json")
	return writeFileAtomically(path, data, 0o600)
}

func GetProfile(cfg File, name string) Profile {
	if cfg.Profiles == nil {
		return Profile{}
	}
	return cfg.Profiles[name]
}

func SetProfile(cfg *File, name string, profile Profile) {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	cfg.Profiles[name] = profile
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

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
