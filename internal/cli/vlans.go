package cli

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newVlansCommand(options *RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vlans",
		Short: "Manage VLANs",
	}
	cmd.AddCommand(newVlansListCommand(options))
	return cmd
}

func newVlansListCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List VLANs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			vlans, err := client.ListVlans(cmd.Context())
			if err != nil {
				if errors.Is(err, session.ErrSessionNotFound) {
					return errors.New("not logged in")
				}
				if errors.Is(err, httpclient.ErrUnauthorized) {
					return errors.New("authentication failed; run `internctl login` again")
				}
				if errors.Is(err, httpclient.ErrForbidden) {
					return errors.New("admin access is required to list VLANs")
				}
				return err
			}

			rows := make([][]string, 0, len(vlans))
			for _, vlan := range vlans {
				rows = append(rows, []string{
					strconv.FormatInt(vlan.Id, 10),
					vlan.Name,
					strconv.FormatInt(int64(vlan.VlanId), 10),
					vlan.Description,
					boolLabel(vlan.IsActive),
				})
			}

			if err := printTable(cmd, []string{"ID", "NAME", "VLAN ID", "DESCRIPTION", "ACTIVE"}, rows); err != nil {
				return fmt.Errorf("render VLAN table: %w", err)
			}
			return nil
		},
	}
}
