package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sbekti/internctl/internal/api"
	"github.com/sbekti/internctl/internal/httpclient"
	"github.com/sbekti/internctl/internal/session"
)

func newSessionCommand(options *RootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage API client sessions",
	}
	cmd.AddCommand(newSessionListCommand(options))
	cmd.AddCommand(newSessionRevokeCommand(options))
	cmd.AddCommand(newSessionRevokeAllCommand(options))
	return cmd
}

func newSessionListCommand(options *RootOptions) *cobra.Command {
	var output string
	var all bool
	var page int32
	var pageSize int32

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active client sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateOutputFormat(output); err != nil {
				return err
			}
			if page <= 0 {
				return errors.New("--page must be greater than zero")
			}
			if pageSize <= 0 {
				return errors.New("--page-size must be greater than zero")
			}

			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			offset := (page - 1) * pageSize
			sessionPage, err := client.ListSessions(cmd.Context(), all, pageSize, offset)
			if err != nil {
				return mapSessionError(err, all, "list")
			}

			if output == "json" {
				return printJSON(cmd, sessionPage)
			}

			if err := printSessionTable(cmd, sessionPage, all); err != nil {
				return err
			}
			return printSessionPageSummary(cmd, page, sessionPage.Pagination)
		},
	}

	addOutputFlag(cmd, &output)
	cmd.Flags().BoolVar(&all, "all", false, "List client sessions across all users (admin only)")
	cmd.Flags().Int32Var(&page, "page", 1, "Page number (1-based)")
	cmd.Flags().Int32Var(&pageSize, "page-size", 20, "Number of sessions per page")

	return cmd
}

func newSessionRevokeCommand(options *RootOptions) *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a client session",
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

			if err := client.RevokeSession(cmd.Context(), args[0], all); err != nil {
				return mapSessionError(err, all, "revoke")
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Revoked session %s.\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Revoke a client session across all users (admin only)")
	return cmd
}

func newSessionRevokeAllCommand(options *RootOptions) *cobra.Command {
	var all bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "revoke-all",
		Short: "Revoke multiple client sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes {
				message := "Revoke all of your other client sessions?"
				if all {
					message = "Revoke all client sessions across all users? This may sign out your current CLI session."
				}
				ok, err := confirmAction(cmd, message)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("aborted")
				}
			}

			runtime, err := resolveRuntime(options)
			if err != nil {
				return err
			}

			client, err := runtime.NewAuthenticatedClient()
			if err != nil {
				return err
			}

			if err := client.RevokeAllSessions(cmd.Context(), all); err != nil {
				return mapSessionError(err, all, "revoke")
			}

			if all {
				fmt.Fprintln(cmd.OutOrStdout(), "Revoked all client sessions across all users.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Revoked all other client sessions.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Revoke client sessions across all users (admin only)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	return cmd
}

func printSessionTable(cmd *cobra.Command, sessionPage *api.AuthSessionPage, all bool) error {
	rows := make([][]string, 0, len(sessionPage.Items))
	for _, item := range sessionPage.Items {
		row := []string{
			item.Id.String(),
		}
		if all {
			row = append(row, item.Username)
		}
		row = append(row,
			currentLabel(item.IsCurrent),
			item.ClientName,
			formatTime(item.LastUsedAt),
			item.IdleExpiresAt.Format(time.RFC3339),
			item.ExpiresAt.Format(time.RFC3339),
		)
		rows = append(rows, row)
	}

	headers := []string{"ID"}
	if all {
		headers = append(headers, "USERNAME")
	}
	headers = append(headers, "CURRENT", "CLIENT", "LAST USED", "IDLE EXPIRY", "ABSOLUTE EXPIRY")
	return printTable(cmd, headers, rows)
}

func printSessionPageSummary(cmd *cobra.Command, page int32, pagination api.AuthSessionPagination) error {
	totalPages := int64(0)
	if pagination.Limit > 0 {
		totalPages = int64(math.Ceil(float64(pagination.Total) / float64(pagination.Limit)))
	}
	if totalPages == 0 {
		totalPages = 1
	}
	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"Page %d of %d (%d per page, %d total)\n",
		page,
		totalPages,
		pagination.Limit,
		pagination.Total,
	)
	return err
}

func mapSessionError(err error, all bool, action string) error {
	if errors.Is(err, session.ErrSessionNotFound) {
		return errors.New("not logged in")
	}
	if errors.Is(err, httpclient.ErrUnauthorized) {
		return errors.New("authentication failed; run `internctl login` again")
	}
	if errors.Is(err, httpclient.ErrForbidden) {
		if all {
			return fmt.Errorf("admin access is required to %s all-user sessions", action)
		}
		return fmt.Errorf("admin access is required to %s these sessions", action)
	}
	return err
}

func currentLabel(current bool) string {
	if current {
		return "yes"
	}
	return "no"
}

func formatTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func confirmAction(cmd *cobra.Command, message string) (bool, error) {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", message); err != nil {
		return false, err
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}
