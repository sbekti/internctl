package cli

import (
	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/config"
)

type RootOptions struct {
	Profile   string
	ConfigDir string
}

func NewRootCommand() *cobra.Command {
	options := &RootOptions{}

	cmd := &cobra.Command{
		Use:           "internctl",
		Short:         "Command-line client for the internal management platform",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(
		&options.Profile,
		"profile",
		config.DefaultProfile,
		"Profile name to use",
	)
	cmd.PersistentFlags().StringVar(
		&options.ConfigDir,
		"config-dir",
		"",
		"Override the config directory (defaults to ~/.intern)",
	)

	cmd.AddCommand(newLoginCommand(options))
	cmd.AddCommand(newLogoutCommand(options))
	cmd.AddCommand(newWhoamiCommand(options))

	return cmd
}
