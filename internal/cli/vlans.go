package cli

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newVlansCommand(options *RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "vlans",
		Short:  "Manage VLANs",
		Hidden: true,
	}
	cmd.AddCommand(newVlansListCommand(options))
	cmd.AddCommand(newVlansCreateCommand(options))
	cmd.AddCommand(newVlansUpdateCommand(options))
	cmd.AddCommand(newVlansDeleteCommand(options))
	return cmd
}

func newVlanCommand(options *RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vlan",
		Short: "Manage VLANs",
	}
	cmd.AddCommand(newVlansListCommand(options))
	cmd.AddCommand(newVlansCreateCommand(options))
	cmd.AddCommand(newVlansUpdateCommand(options))
	cmd.AddCommand(newVlansDeleteCommand(options))
	return cmd
}

func newVlansListCommand(options *RootOptions) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List VLANs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateOutputFormat(output); err != nil {
				return err
			}

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

			if output == "json" {
				return printJSON(cmd, vlans)
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

	addOutputFlag(cmd, &output)
	return cmd
}

func newVlansCreateCommand(options *RootOptions) *cobra.Command {
	var name string
	var vlanID int32
	var description string
	var active bool
	var inactive bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a VLAN",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if active && inactive {
				return errors.New("use only one of --active or --inactive")
			}
			if name == "" {
				return errors.New("--name is required")
			}
			if vlanID <= 0 {
				return errors.New("--vlan-id must be greater than zero")
			}

			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			body := api.VlanWrite{
				Name:   name,
				VlanId: vlanID,
			}
			if cmd.Flags().Changed("description") {
				body.Description = &description
			}
			if active || inactive {
				isActive := active && !inactive
				body.IsActive = &isActive
			}

			vlan, err := client.CreateVlan(cmd.Context(), body)
			if err != nil {
				return mapVlanMutationError(err, "create")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created VLAN %d (%s).\n", vlan.Id, vlan.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "VLAN display name")
	cmd.Flags().Int32Var(&vlanID, "vlan-id", 0, "Network VLAN ID")
	cmd.Flags().StringVar(&description, "description", "", "VLAN description")
	cmd.Flags().BoolVar(&active, "active", false, "Mark the VLAN as active")
	cmd.Flags().BoolVar(&inactive, "inactive", false, "Mark the VLAN as inactive")

	return cmd
}

func newVlansUpdateCommand(options *RootOptions) *cobra.Command {
	var name string
	var vlanID int32
	var description string
	var active bool
	var inactive bool

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a VLAN",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if active && inactive {
				return errors.New("use only one of --active or --inactive")
			}

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return errors.New("VLAN id must be a positive integer")
			}

			patch := api.VlanPatch{}
			changed := false
			if cmd.Flags().Changed("name") {
				patch.Name = &name
				changed = true
			}
			if cmd.Flags().Changed("vlan-id") {
				if vlanID <= 0 {
					return errors.New("--vlan-id must be greater than zero")
				}
				patch.VlanId = &vlanID
				changed = true
			}
			if cmd.Flags().Changed("description") {
				patch.Description = &description
				changed = true
			}
			if active || inactive {
				isActive := active && !inactive
				patch.IsActive = &isActive
				changed = true
			}
			if !changed {
				return errors.New("no changes requested")
			}

			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			vlan, err := client.UpdateVlan(cmd.Context(), id, patch)
			if err != nil {
				return mapVlanMutationError(err, "update")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated VLAN %d (%s).\n", vlan.Id, vlan.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "VLAN display name")
	cmd.Flags().Int32Var(&vlanID, "vlan-id", 0, "Network VLAN ID")
	cmd.Flags().StringVar(&description, "description", "", "VLAN description")
	cmd.Flags().BoolVar(&active, "active", false, "Mark the VLAN as active")
	cmd.Flags().BoolVar(&inactive, "inactive", false, "Mark the VLAN as inactive")

	return cmd
}

func newVlansDeleteCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a VLAN",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return errors.New("VLAN id must be a positive integer")
			}

			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			if err := client.DeleteVlan(cmd.Context(), id); err != nil {
				return mapVlanMutationError(err, "delete")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted VLAN %d.\n", id)
			return nil
		},
	}
}

func mapVlanMutationError(err error, action string) error {
	if errors.Is(err, session.ErrSessionNotFound) {
		return errors.New("not logged in")
	}
	if errors.Is(err, httpclient.ErrUnauthorized) {
		return errors.New("authentication failed; run `internctl login` again")
	}
	if errors.Is(err, httpclient.ErrForbidden) {
		return fmt.Errorf("admin access is required to %s VLANs", action)
	}
	return err
}
