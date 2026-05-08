package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

func newOperatorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Manage operators (admin-only) and per-project access grants.",
	}
	cmd.AddCommand(newOperatorListCmd())
	cmd.AddCommand(newOperatorGetCmd())
	cmd.AddCommand(newOperatorCreateCmd())
	cmd.AddCommand(newOperatorDeleteCmd())
	cmd.AddCommand(newOperatorGrantCmd())
	cmd.AddCommand(newOperatorRevokeCmd())
	cmd.AddCommand(newOperatorAccessCmd())
	cmd.AddCommand(newOperatorSetAdminCmd())
	cmd.AddCommand(newOperatorSetPasswordCmd())
	return cmd
}

// canonicalOperatorName accepts either a bare numeric id ("42") or the full
// resource name ("operators/42") and returns "operators/{id}". This mirrors
// the convention used by canonicalProjectName / canonicalSiteName.
func canonicalOperatorName(arg string) string {
	if strings.HasPrefix(arg, "operators/") {
		return arg
	}
	return "operators/" + arg
}

func newOperatorListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List operators (admin only).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.ListOperators(ctx, &quicktunv1.ListOperatorsRequest{})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, resp.GetOperators(), func() {
				rows := make([][]string, 0, len(resp.GetOperators()))
				for _, op := range resp.GetOperators() {
					admin := "no"
					if op.GetIsAdmin() {
						admin = "yes"
					}
					rows = append(rows, []string{op.GetName(), op.GetEmail(), admin})
				}
				printTable([]string{"NAME", "EMAIL", "ADMIN"}, rows)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newOperatorGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a single operator by id or full resource name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			op, err := client.GetOperator(ctx, &quicktunv1.GetOperatorRequest{Name: name})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, op, func() {
				admin := "no"
				if op.GetIsAdmin() {
					admin = "yes"
				}
				printTable(
					[]string{"NAME", "EMAIL", "ADMIN"},
					[][]string{{op.GetName(), op.GetEmail(), admin}},
				)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newOperatorCreateCmd() *cobra.Command {
	var (
		email    string
		password string
		isAdmin  bool
		asJSON   bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new operator (admin only).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" || password == "" {
				return fmt.Errorf("--email and --password are required")
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			op, err := client.CreateOperator(ctx, &quicktunv1.CreateOperatorRequest{
				Operator: &quicktunv1.Operator{Email: email, IsAdmin: isAdmin},
				Password: password,
			})
			if err != nil {
				return err
			}
			return renderOrJSON(asJSON, op, func() {
				fmt.Printf("Created %s (email=%s, admin=%v)\n", op.GetName(), op.GetEmail(), op.GetIsAdmin())
			})
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "operator email (required)")
	cmd.Flags().StringVar(&password, "password", "", "operator password (required)")
	cmd.Flags().BoolVar(&isAdmin, "admin", false, "grant admin role")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newOperatorDeleteCmd() *cobra.Command {
	var yesFlag bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an operator (admin only).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			if !yesFlag {
				if !confirmYes(fmt.Sprintf("Delete %s?", name)) {
					return fmt.Errorf("aborted")
				}
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.DeleteOperator(ctx, &quicktunv1.DeleteOperatorRequest{Name: name}); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "skip interactive confirmation")
	return cmd
}

func newOperatorSetAdminCmd() *cobra.Command {
	var (
		isAdmin bool
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "set-admin <name>",
		Short: "Toggle the is_admin flag on an operator.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			op, err := client.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
				Operator:   &quicktunv1.Operator{Name: name, IsAdmin: isAdmin},
				UpdateMask: "is_admin",
			})
			if err != nil {
				return err
			}
			return renderOrJSON(asJSON, op, func() {
				fmt.Printf("Updated %s (admin=%v)\n", op.GetName(), op.GetIsAdmin())
			})
		},
	}
	cmd.Flags().BoolVar(&isAdmin, "admin", false, "target value of is_admin")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newOperatorSetPasswordCmd() *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:   "set-password <name>",
		Short: "Rotate an operator's password (admin only).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			if password == "" {
				return fmt.Errorf("--password is required")
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.UpdateOperator(ctx, &quicktunv1.UpdateOperatorRequest{
				Operator:   &quicktunv1.Operator{Name: name},
				UpdateMask: "password",
				Password:   password,
			}); err != nil {
				return err
			}
			fmt.Printf("Rotated password for %s\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "new password (required)")
	return cmd
}

func newOperatorGrantCmd() *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "grant <operator> <project-slug>",
		Short: "Grant project access to an operator (admin only).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			projectSlug := args[1]
			if role == "" {
				return fmt.Errorf("--role is required (one of: viewer, operator, owner)")
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			access, err := client.GrantProjectAccess(ctx, &quicktunv1.GrantProjectAccessRequest{
				Operator:    name,
				ProjectSlug: projectSlug,
				Role:        role,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Granted %s access to %s on %s\n", access.GetRole(), access.GetOperator(), access.GetProjectSlug())
			return nil
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "role to grant: viewer | operator | owner")
	return cmd
}

func newOperatorRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <operator> <project-slug>",
		Short: "Revoke project access from an operator (admin only).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			projectSlug := args[1]
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.RevokeProjectAccess(ctx, &quicktunv1.RevokeProjectAccessRequest{
				Operator:    name,
				ProjectSlug: projectSlug,
			}); err != nil {
				return err
			}
			fmt.Printf("Revoked access from %s on %s\n", name, projectSlug)
			return nil
		},
	}
	return cmd
}

func newOperatorAccessCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "access <operator>",
		Short: "List per-project access grants for an operator.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalOperatorName(args[0])
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewOperatorServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.ListProjectAccess(ctx, &quicktunv1.ListProjectAccessRequest{Operator: name})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, resp.GetAccess(), func() {
				rows := make([][]string, 0, len(resp.GetAccess()))
				for _, a := range resp.GetAccess() {
					rows = append(rows, []string{a.GetOperator(), a.GetProjectSlug(), a.GetRole()})
				}
				printTable([]string{"OPERATOR", "PROJECT", "ROLE"}, rows)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
