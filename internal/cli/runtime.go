package cli

import (
	"fmt"

	"github.com/sbekti/internctl/internal/config"
	"github.com/sbekti/internctl/internal/session"
)

type Runtime struct {
	ConfigDir string
	Profile   string
	Config    config.File
	Sessions  *session.Manager
}

func resolveRuntime(options *RootOptions) (*Runtime, error) {
	profile, err := config.NormalizeProfileName(options.Profile)
	if err != nil {
		return nil, err
	}

	configDir, err := config.ResolveConfigDir(options.ConfigDir)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(configDir)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		ConfigDir: configDir,
		Profile:   profile,
		Config:    cfg,
		Sessions:  session.NewManager(configDir),
	}, nil
}

func (r *Runtime) ProfileConfig() config.Profile {
	return config.GetProfile(r.Config, r.Profile)
}

func (r *Runtime) SaveProfile(profile config.Profile) error {
	config.SetProfile(&r.Config, r.Profile, profile)
	if err := config.Save(r.ConfigDir, r.Config); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
