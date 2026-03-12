package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/config"
	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newWhoamiCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the currently authenticated user",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			profileCfg := runtime.ProfileConfig()
			serverURL := strings.TrimSpace(profileCfg.ServerURL)
			if serverURL == "" {
				serverURL = config.DefaultServerURL
			}

			selectedBackend, err := session.ParseBackend(profileCfg.TokenBackend)
			if err != nil {
				selectedBackend = session.BackendAuto
			}

			client, err := httpclient.New(serverURL, runtime.Profile, selectedBackend, runtime.Sessions)
			if err != nil {
				return err
			}

			profile, err := client.GetProfile(cmd.Context())
			if err != nil {
				if errors.Is(err, session.ErrSessionNotFound) {
					return errors.New("not logged in")
				}
				if errors.Is(err, httpclient.ErrUnauthorized) {
					return errors.New("authentication failed; run `internctl login` again")
				}
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Profile: %s\n", runtime.Profile)
			fmt.Fprintf(cmd.OutOrStdout(), "Server: %s\n", serverURL)
			fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", profile.Username)
			fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\n", profile.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Email: %s\n", string(profile.Email))
			fmt.Fprintf(cmd.OutOrStdout(), "Admin: %t\n", profile.IsAdmin)
			fmt.Fprintf(cmd.OutOrStdout(), "Groups: %s\n", strings.Join(profile.Groups, ", "))

			return nil
		},
	}
}
