package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newDevicesCommand(options *RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Manage network devices",
	}
	cmd.AddCommand(newDevicesListCommand(options))
	return cmd
}

func newDevicesListCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List network devices",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			devices, err := client.ListNetworkDevices(cmd.Context())
			if err != nil {
				if errors.Is(err, session.ErrSessionNotFound) {
					return errors.New("not logged in")
				}
				if errors.Is(err, httpclient.ErrUnauthorized) {
					return errors.New("authentication failed; run `internctl login` again")
				}
				if errors.Is(err, httpclient.ErrForbidden) {
					return errors.New("admin access is required to list devices")
				}
				return err
			}

			rows := make([][]string, 0, len(devices))
			for _, device := range devices {
				rows = append(rows, []string{
					device.DisplayName,
					device.MacAddress,
					device.Vlan.Name,
				})
			}

			if err := printTable(cmd, []string{"NAME", "MAC ADDRESS", "VLAN"}, rows); err != nil {
				return fmt.Errorf("render device table: %w", err)
			}
			return nil
		},
	}
}
