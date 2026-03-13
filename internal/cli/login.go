package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/config"
	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newLoginCommand(options *RootOptions) *cobra.Command {
	var serverURL string
	var tokenBackend string
	var clientName string
	var force bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a device code flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			profileCfg := runtime.ProfileConfig()

			resolvedServerURL := strings.TrimSpace(serverURL)
			if resolvedServerURL == "" {
				if profileCfg.ServerURL != "" {
					resolvedServerURL = profileCfg.ServerURL
				} else {
					resolvedServerURL = config.DefaultServerURL
				}
			}

			selectedBackend, err := selectLoginBackend(cmd, tokenBackend, profileCfg.TokenBackend)
			if err != nil {
				return err
			}

			resolvedClientName := strings.TrimSpace(clientName)
			if resolvedClientName == "" {
				resolvedClientName = detectHostname()
			}

			if !force {
				if _, _, err := runtime.Sessions.Load(runtime.Profile, session.BackendAuto); err == nil {
					return fmt.Errorf(
						"profile %q is already signed in; use --force to replace the existing session or run logout first",
						runtime.Profile,
					)
				} else if !errors.Is(err, session.ErrSessionNotFound) {
					return err
				}
			}

			client, err := httpclient.New(resolvedServerURL, runtime.Profile, selectedBackend, runtime.Sessions)
			if err != nil {
				return err
			}

			deviceCode, err := client.CreateDeviceCode(cmd.Context(), resolvedClientName)
			if err != nil {
				return err
			}
			verificationURL := strings.TrimSpace(deviceCode.VerificationUriComplete)
			if verificationURL == "" {
				verificationURL = strings.TrimSpace(deviceCode.VerificationUri)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Open the verification URL in a browser and approve:")
			fmt.Fprintf(cmd.OutOrStdout(), "Verification URL: %s\n", verificationURL)
			fmt.Fprintf(cmd.OutOrStdout(), "User code: %s\n", deviceCode.UserCode)
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"Waiting for approval (will timeout in %ds)...\n",
				deviceCode.ExpiresIn,
			)

			tokenResponse, err := waitForDeviceApproval(
				cmd.Context(),
				client,
				deviceCode.DeviceCode,
				time.Duration(deviceCode.Interval)*time.Second,
				time.Duration(deviceCode.ExpiresIn)*time.Second,
			)
			if err != nil {
				return err
			}

			sessionData := httpclient.TokenResponseToSessionData(tokenResponse)
			resolvedBackend, err := runtime.Sessions.Save(runtime.Profile, selectedBackend, sessionData)
			if err != nil {
				return err
			}

			profileCfg.ServerURL = resolvedServerURL
			profileCfg.TokenBackend = string(resolvedBackend)
			if err := runtime.SaveProfile(profileCfg); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Login successful.")
			fmt.Fprintf(cmd.OutOrStdout(), "Profile: %s\n", runtime.Profile)
			fmt.Fprintf(cmd.OutOrStdout(), "Server: %s\n", resolvedServerURL)
			fmt.Fprintf(cmd.OutOrStdout(), "Token backend: %s\n", resolvedBackend)

			return nil
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", "", "Server base URL")
	cmd.Flags().StringVar(
		&tokenBackend,
		"token-backend",
		string(session.BackendAuto),
		"Token storage backend: auto, keyring, or file",
	)
	cmd.Flags().StringVar(&clientName, "client-name", "", "Client name sent during device login")
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing saved session for the selected profile")

	return cmd
}

func waitForDeviceApproval(
	ctx context.Context,
	client *httpclient.Client,
	deviceCode string,
	pollInterval time.Duration,
	timeout time.Duration,
) (*api.TokenResponse, error) {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, errors.New("device login timed out")
		}

		tokenResponse, err := client.ExchangeDeviceCode(ctx, deviceCode)
		if err == nil {
			return tokenResponse, nil
		}

		var authErr httpclient.ClientAuthError
		if errors.As(err, &authErr) {
			switch authErr.Code {
			case "authorization_pending":
				if err := sleepContext(ctx, pollInterval); err != nil {
					return nil, err
				}
				continue
			case "access_denied":
				return nil, errors.New("device login was denied")
			case "expired_token":
				return nil, errors.New("device login expired")
			case "invalid_request":
				return nil, fmt.Errorf("device login failed: %s", authErr.Description)
			default:
				return nil, err
			}
		}

		var apiErr httpclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 429 {
			if err := sleepContext(ctx, pollInterval); err != nil {
				return nil, err
			}
			continue
		}

		return nil, err
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func detectHostname() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "unknown-host"
	}
	return strings.TrimSpace(host)
}

func selectLoginBackend(cmd *cobra.Command, flagValue string, savedValue string) (session.Backend, error) {
	if !cmd.Flags().Changed("token-backend") && strings.TrimSpace(savedValue) != "" {
		return session.ParseBackend(savedValue)
	}
	return session.ParseBackend(flagValue)
}
