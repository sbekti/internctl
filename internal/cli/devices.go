package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newDevicesCommand(options *RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Manage network devices",
	}
	cmd.AddCommand(newDevicesListCommand(options))
	cmd.AddCommand(newDevicesCreateCommand(options))
	cmd.AddCommand(newDevicesUpdateCommand(options))
	cmd.AddCommand(newDevicesDeleteCommand(options))
	return cmd
}

func newDevicesListCommand(options *RootOptions) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List network devices",
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

			if output == "json" {
				return printJSON(cmd, devices)
			}

			rows := make([][]string, 0, len(devices))
			for _, device := range devices {
				rows = append(rows, []string{
					device.Id.String(),
					device.DisplayName,
					device.MacAddress,
					device.Vlan.Name,
				})
			}

			if err := printTable(cmd, []string{"ID", "NAME", "MAC ADDRESS", "VLAN"}, rows); err != nil {
				return fmt.Errorf("render device table: %w", err)
			}
			return nil
		},
	}

	addOutputFlag(cmd, &output)
	return cmd
}

func newDevicesCreateCommand(options *RootOptions) *cobra.Command {
	var name string
	var macAddress string
	var vlanID int64

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a network device",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			if strings.TrimSpace(macAddress) == "" {
				return errors.New("--mac-address is required")
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

			device, err := client.CreateNetworkDevice(cmd.Context(), api.NetworkDeviceWrite{
				DisplayName: name,
				MacAddress:  macAddress,
				VlanId:      vlanID,
			})
			if err != nil {
				return mapDeviceMutationError(err, "create")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created device %s (%s).\n", device.DisplayName, device.Id.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Device display name")
	cmd.Flags().StringVar(&macAddress, "mac-address", "", "Device MAC address")
	cmd.Flags().Int64Var(&vlanID, "vlan-id", 0, "Assigned VLAN record ID")

	return cmd
}

func newDevicesUpdateCommand(options *RootOptions) *cobra.Command {
	var name string
	var macAddress string
	var vlanID int64

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a network device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			patch := api.NetworkDevicePatch{}
			changed := false

			if cmd.Flags().Changed("name") {
				patch.DisplayName = &name
				changed = true
			}
			if cmd.Flags().Changed("mac-address") {
				if strings.TrimSpace(macAddress) == "" {
					return errors.New("--mac-address must not be empty")
				}
				patch.MacAddress = &macAddress
				changed = true
			}
			if cmd.Flags().Changed("vlan-id") {
				if vlanID <= 0 {
					return errors.New("--vlan-id must be greater than zero")
				}
				patch.VlanId = &vlanID
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

			device, err := client.UpdateNetworkDevice(cmd.Context(), args[0], patch)
			if err != nil {
				return mapDeviceMutationError(err, "update")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated device %s (%s).\n", device.DisplayName, device.Id.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Device display name")
	cmd.Flags().StringVar(&macAddress, "mac-address", "", "Device MAC address")
	cmd.Flags().Int64Var(&vlanID, "vlan-id", 0, "Assigned VLAN record ID")

	return cmd
}

func newDevicesDeleteCommand(options *RootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a network device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			if err := client.DeleteNetworkDevice(cmd.Context(), args[0]); err != nil {
				return mapDeviceMutationError(err, "delete")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted device %s.\n", args[0])
			return nil
		},
	}
}

func mapDeviceMutationError(err error, action string) error {
	if errors.Is(err, session.ErrSessionNotFound) {
		return errors.New("not logged in")
	}
	if errors.Is(err, httpclient.ErrUnauthorized) {
		return errors.New("authentication failed; run `internctl login` again")
	}
	if errors.Is(err, httpclient.ErrForbidden) {
		return fmt.Errorf("admin access is required to %s devices", action)
	}
	return err
}
