package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

// newTokenCmd is the CLI surface for service-account tokens — long-lived
// bearer credentials scoped to a specific operator, intended for CI/CD.
//
//	quicktun token list <operator-name>           (admin only)
//	quicktun token create <operator-name> --description=... [--ttl=...]
//	quicktun token revoke <id>
//
// Operator-name accepts the bare numeric id ("42") or the full resource
// name ("operators/42"); canonicalOperatorName normalises both. When
// omitted from list/create, the caller's own operator id is resolved via
// AuthService.WhoAmI so admins can self-mint tokens for their own login
// without first looking up their id.
func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage service-account tokens (admin only).",
		Long: "Service-account tokens are long-lived bearer credentials bound to " +
			"a specific operator, designed for CI/CD automation. They are " +
			"distinct from session tokens issued by `quicktun login`.",
	}
	cmd.AddCommand(newTokenListCmd())
	cmd.AddCommand(newTokenCreateCmd())
	cmd.AddCommand(newTokenRevokeCmd())
	return cmd
}

// resolveOperatorName returns the canonical "operators/{id}" name for the
// supplied argument, or — when arg is empty — the caller's own operator
// id (resolved via AuthService.WhoAmI). The latter lets `quicktun token
// list` and `quicktun token create` work without forcing the operator to
// look up their id.
func resolveOperatorName(cmd *cobra.Command, arg string, fallbackToSelf bool) (string, error) {
	if arg != "" {
		return canonicalOperatorName(arg), nil
	}
	if !fallbackToSelf {
		return "", fmt.Errorf("operator name is required")
	}
	_, conn, err := loadAndDial(cmd)
	if err != nil {
		return "", err
	}
	defer conn.Close() //nolint:errcheck
	client := quicktunv1.NewAuthServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	resp, err := client.WhoAmI(ctx, &quicktunv1.WhoAmIRequest{})
	if err != nil {
		return "", fmt.Errorf("resolve self via WhoAmI: %w", err)
	}
	return resp.GetOperator().GetName(), nil
}

func newTokenListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list [operator]",
		Short: "List service-account tokens for an operator (default: self).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			name, err := resolveOperatorName(cmd, arg, true)
			if err != nil {
				return err
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewServiceAccountServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.ListServiceAccountTokens(ctx, &quicktunv1.ListServiceAccountTokensRequest{
				Operator: name,
			})
			if err != nil {
				return err
			}
			return renderOrJSON(asJSON, resp.GetTokens(), func() {
				rows := make([][]string, 0, len(resp.GetTokens()))
				for _, t := range resp.GetTokens() {
					status := "active"
					if t.GetRevokeTime() != nil && t.GetRevokeTime().IsValid() {
						status = "revoked"
					}
					expire := "-"
					if t.GetExpireTime() != nil && t.GetExpireTime().IsValid() {
						expire = t.GetExpireTime().AsTime().UTC().Format(time.RFC3339)
					}
					used := "-"
					if t.GetLastUsedTime() != nil && t.GetLastUsedTime().IsValid() {
						used = t.GetLastUsedTime().AsTime().UTC().Format(time.RFC3339)
					}
					rows = append(rows, []string{
						fmt.Sprintf("%d", t.GetId()),
						t.GetOperator(),
						t.GetDescription(),
						status,
						expire,
						used,
					})
				}
				printTable([]string{"ID", "OPERATOR", "DESCRIPTION", "STATUS", "EXPIRES", "LAST_USED"}, rows)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newTokenCreateCmd() *cobra.Command {
	var (
		description string
		ttl         time.Duration
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "create [operator]",
		Short: "Issue a new service-account token (default operator: self).",
		Long: "Issues a new long-lived bearer token bound to the named operator. " +
			"The raw token is shown ONCE — copy it immediately; the server only " +
			"stores its hash and cannot recover the value later.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			name, err := resolveOperatorName(cmd, arg, true)
			if err != nil {
				return err
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewServiceAccountServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			req := &quicktunv1.CreateServiceAccountTokenRequest{
				Operator:    name,
				Description: description,
				TtlSeconds:  int64(ttl.Seconds()),
			}
			resp, err := client.CreateServiceAccountToken(ctx, req)
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(resp)
			}
			// Human output: print the raw token prominently with a
			// "shown once" warning. Stderr carries the warning so a
			// shell user piping `... | head -1` still gets the token.
			fmt.Fprintln(errStream(), strings.Repeat("=", 60))
			fmt.Fprintln(errStream(), "Service-account token created")
			fmt.Fprintln(errStream(), strings.Repeat("=", 60))
			fmt.Fprintf(errStream(), "id:          %d\n", resp.GetToken().GetId())
			fmt.Fprintf(errStream(), "operator:    %s\n", resp.GetToken().GetOperator())
			fmt.Fprintf(errStream(), "description: %s\n", resp.GetToken().GetDescription())
			if exp := resp.GetToken().GetExpireTime(); exp != nil && exp.IsValid() {
				fmt.Fprintf(errStream(), "expires:     %s\n", exp.AsTime().UTC().Format(time.RFC3339))
			} else {
				fmt.Fprintln(errStream(), "expires:     never")
			}
			fmt.Fprintln(errStream(), strings.Repeat("-", 60))
			fmt.Fprintln(errStream(), "WARNING: 此 token 仅显示一次，请妥善保存")
			fmt.Fprintln(errStream(), "WARNING: this token is shown only once — save it now")
			fmt.Fprintln(errStream(), strings.Repeat("-", 60))
			fmt.Println(resp.GetRaw())
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "human-readable label (e.g. \"ci-deploy\")")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "token lifetime (e.g. 720h); 0 = no expiry")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON (raw token included; redirect carefully)")
	return cmd
}

func newTokenRevokeCmd() *cobra.Command {
	var yesFlag bool
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a service-account token by id (idempotent).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseUintArg(args[0])
			if err != nil {
				return fmt.Errorf("invalid token id %q: %w", args[0], err)
			}
			if !yesFlag {
				if !confirmYes(fmt.Sprintf("Revoke service-account token %d?", id)) {
					return fmt.Errorf("aborted")
				}
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewServiceAccountServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.RevokeServiceAccountToken(ctx, &quicktunv1.RevokeServiceAccountTokenRequest{
				Id: id,
			}); err != nil {
				return err
			}
			fmt.Printf("Revoked service-account token %d\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "skip interactive confirmation")
	return cmd
}

// parseUintArg parses a non-negative integer from a CLI argument. Wraps
// strconv to give the error a friendlier shape ("invalid token id ...").
func parseUintArg(s string) (uint64, error) {
	var n uint64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}
