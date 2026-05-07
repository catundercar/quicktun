package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/clicred"
)

func newLoginCmd() *cobra.Command {
	var (
		endpoint  string
		email     string
		password  string
		authProxy string
		insecure  bool
		readPwd   bool
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate against the control plane and store a session token.",
		Long: "Calls AuthService.Login over gRPC, then writes the returned access " +
			"token to credentials.yaml (mode 0o600) so subsequent CLI commands " +
			"can authenticate without re-prompting.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if endpoint == "" || email == "" {
				return fmt.Errorf("--endpoint and --email are required")
			}
			if password == "" && readPwd {
				pw, err := promptPassword()
				if err != nil {
					return err
				}
				password = pw
			}
			if password == "" {
				return fmt.Errorf("--password (or --password-stdin) is required")
			}

			// Build a temporary creds (no token yet) just to dial. We
			// reuse dialControl so the TLS / insecure logic stays in
			// one place and matches what later commands do.
			creds := &clicred.Credentials{
				Endpoint:    endpoint,
				TLSInsecure: insecure,
			}
			conn, err := dialControl(creds)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			authClient := quicktunv1.NewAuthServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			resp, err := authClient.Login(ctx, &quicktunv1.LoginRequest{
				Email:    email,
				Password: password,
			})
			if err != nil {
				return fmt.Errorf("login: %w", err)
			}

			creds.SessionToken = resp.GetAccessToken()
			creds.AuthProxyEndpoint = authProxy
			creds.OperatorEmail = email

			path, _ := cmd.Root().PersistentFlags().GetString("config")
			if path == "" {
				p, err := clicred.DefaultPath()
				if err != nil {
					return err
				}
				path = p
			}
			if err := clicred.Save(path, creds); err != nil {
				return fmt.Errorf("save credentials: %w", err)
			}
			fmt.Printf("Logged in as %s. Token saved to %s\n", email, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "",
		"control plane gRPC address (host:port)")
	cmd.Flags().StringVar(&email, "email", "", "operator email")
	cmd.Flags().StringVar(&password, "password", "",
		"operator password (use --password-stdin for security)")
	cmd.Flags().BoolVar(&readPwd, "password-stdin", false,
		"read password from stdin (TTY prompts if attached, else reads piped input)")
	cmd.Flags().StringVar(&authProxy, "auth-proxy", "",
		"auth-proxy public address (host:port)")
	cmd.Flags().BoolVar(&insecure, "insecure", false, "skip TLS (dev only)")
	return cmd
}

// promptPassword reads a password from stdin. If stdin is a TTY we hide
// the typed characters via term.ReadPassword; otherwise we drain stdin
// (e.g. `echo pw | quicktun login --password-stdin`). In both cases we
// trim trailing newline / whitespace so a piped `echo "pw"` works.
func promptPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
