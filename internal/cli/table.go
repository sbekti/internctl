package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func printTable(cmd *cobra.Command, headers []string, rows [][]string) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(writer, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func addOutputFlag(cmd *cobra.Command, output *string) {
	cmd.Flags().StringVar(output, "output", "table", "Output format: table or json")
}

func validateOutputFormat(output string) error {
	switch strings.TrimSpace(output) {
	case "", "table", "json":
		return nil
	default:
		return fmt.Errorf("unsupported output format %q (expected table or json)", output)
	}
}

func printJSON(cmd *cobra.Command, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = cmd.OutOrStdout().Write(encoded)
	return err
}
