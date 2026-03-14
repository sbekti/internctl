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
					strconv.FormatInt(int64(vlan.VlanId), 10),
					vlan.Name,
					vlan.Description,
				})
			}

			if err := printTable(cmd, []string{"VLAN ID", "NAME", "DESCRIPTION"}, rows); err != nil {
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

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a VLAN",
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			vlan, err := client.CreateVlan(cmd.Context(), body)
			if err != nil {
				return mapVlanMutationError(err, "create")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created VLAN %d (%s).\n", vlan.VlanId, vlan.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "VLAN display name")
	cmd.Flags().Int32Var(&vlanID, "vlan-id", 0, "Network VLAN ID")
	cmd.Flags().StringVar(&description, "description", "", "VLAN description")

	return cmd
}

func newVlansUpdateCommand(options *RootOptions) *cobra.Command {
	var name string
	var vlanID int32
	var description string

	cmd := &cobra.Command{
		Use:   "update <vlan-id>",
		Short: "Update a VLAN",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			currentVlanID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil || currentVlanID <= 0 {
				return errors.New("vlan id must be a positive integer")
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

			vlan, err := client.UpdateVlan(cmd.Context(), int32(currentVlanID), patch)
			if err != nil {
				return mapVlanMutationError(err, "update")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated VLAN %d (%s).\n", vlan.VlanId, vlan.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "VLAN display name")
	cmd.Flags().Int32Var(&vlanID, "vlan-id", 0, "Network VLAN ID")
	cmd.Flags().StringVar(&description, "description", "", "VLAN description")

	return cmd
}

func newVlansDeleteCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <vlan-id>",
		Short: "Delete a VLAN",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vlanID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil || vlanID <= 0 {
				return errors.New("vlan id must be a positive integer")
			}

			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			if err := client.DeleteVlan(cmd.Context(), int32(vlanID)); err != nil {
				return mapVlanMutationError(err, "delete")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted VLAN %d.\n", vlanID)
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
