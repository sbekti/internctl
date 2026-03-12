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

func newLogoutCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear the saved session and revoke the refresh token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			profileCfg := runtime.ProfileConfig()
			selectedBackend, err := session.ParseBackend(profileCfg.TokenBackend)
			if err != nil {
				selectedBackend = session.BackendAuto
			}

			currentSession, actualBackend, err := runtime.Sessions.Load(runtime.Profile, selectedBackend)
			if err != nil {
				if errors.Is(err, session.ErrSessionNotFound) {
					fmt.Fprintln(cmd.OutOrStdout(), "No saved session.")
					return nil
				}
				return err
			}

			serverURL := strings.TrimSpace(profileCfg.ServerURL)
			if serverURL == "" {
				serverURL = config.DefaultServerURL
			}

			client, err := httpclient.New(serverURL, runtime.Profile, actualBackend, runtime.Sessions)
			if err != nil {
				return err
			}

			logoutErr := client.Logout(cmd.Context(), currentSession.RefreshToken)
			deleteErr := runtime.Sessions.Delete(runtime.Profile, actualBackend)
			if deleteErr != nil && !errors.Is(deleteErr, session.ErrSessionNotFound) {
				return deleteErr
			}

			if logoutErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Remote logout failed; local session cleared: %v\n", logoutErr)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Signed out.")
			return nil
		},
	}
}
