package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

// rpcTimeout bounds every CRUD RPC. The control plane is local to the
// operator's network in production so 10s is generous; long enough to
// absorb a TLS handshake + DB round-trip but short enough that a wedged
// server surfaces quickly in CI.
const rpcTimeout = 10 * time.Second

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects (tenant boundaries).",
	}
	cmd.AddCommand(newProjectListCmd())
	cmd.AddCommand(newProjectGetCmd())
	cmd.AddCommand(newProjectCreateCmd())
	cmd.AddCommand(newProjectDeleteCmd())
	return cmd
}

func newProjectListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewProjectServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.ListProjects(ctx, &quicktunv1.ListProjectsRequest{})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, resp.GetProjects(), func() {
				rows := make([][]string, 0, len(resp.GetProjects()))
				for _, p := range resp.GetProjects() {
					rows = append(rows, []string{
						p.GetName(),
						p.GetDisplayName(),
						p.GetStatus().String(),
						p.GetRelayPortRange(),
					})
				}
				printTable([]string{"NAME", "DISPLAY", "STATUS", "PORT_RANGE"}, rows)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newProjectGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <slug-or-name>",
		Short: "Get a single project by slug or full resource name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalProjectName(args[0])
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewProjectServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			p, err := client.GetProject(ctx, &quicktunv1.GetProjectRequest{Name: name})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, p, func() {
				rows := [][]string{{
					p.GetName(),
					p.GetDisplayName(),
					p.GetStatus().String(),
					p.GetRelayPortRange(),
				}}
				printTable([]string{"NAME", "DISPLAY", "STATUS", "PORT_RANGE"}, rows)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newProjectCreateCmd() *cobra.Command {
	var (
		displayName string
		portRange   string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewProjectServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			p, err := client.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
				ProjectId: args[0],
				Project: &quicktunv1.Project{
					DisplayName:    displayName,
					RelayPortRange: portRange,
				},
			})
			if err != nil {
				return err
			}
			return renderOrJSON(asJSON, p, func() {
				fmt.Printf("Created %s\n", p.GetName())
			})
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "human-readable name")
	cmd.Flags().StringVar(&portRange, "port-range", "", "relay port range (e.g. 20000-20099, required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	_ = cmd.MarkFlagRequired("port-range")
	return cmd
}

func newProjectDeleteCmd() *cobra.Command {
	var (
		yesFlag bool
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "delete <slug-or-name>",
		Short: "Delete a project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := canonicalProjectName(args[0])
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

			client := quicktunv1.NewProjectServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.DeleteProject(ctx, &quicktunv1.DeleteProjectRequest{
				Name:  name,
				Force: force,
			}); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "skip interactive confirmation")
	cmd.Flags().BoolVar(&force, "force", false, "cascade-delete sites and services (default: refuse if non-empty)")
	return cmd
}

// confirmYes prompts the operator on stderr and returns true if the
// answer starts with "y" or "Y". Centralised so every destructive
// command has identical UX without each one re-implementing it.
func confirmYes(prompt string) bool {
	fmt.Fprintf(errStream(), "%s [y/N] ", prompt)
	var ans string
	_, _ = fmt.Scanln(&ans)
	return strings.EqualFold(strings.TrimSpace(ans), "y")
}
