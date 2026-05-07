package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

func newSiteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "site",
		Short: "Manage sites within a project.",
	}
	cmd.AddCommand(newSiteListCmd())
	cmd.AddCommand(newSiteGetCmd())
	cmd.AddCommand(newSiteCreateCmd())
	cmd.AddCommand(newSiteDeleteCmd())
	return cmd
}

func newSiteListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list <project>",
		Short: "List sites within a project.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent := canonicalProjectName(args[0])
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewSiteServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.ListSites(ctx, &quicktunv1.ListSitesRequest{Parent: parent})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, resp.GetSites(), func() {
				rows := make([][]string, 0, len(resp.GetSites()))
				for _, s := range resp.GetSites() {
					rows = append(rows, []string{
						s.GetName(),
						s.GetDisplayName(),
						s.GetStatus().String(),
						s.GetMode().String(),
						formatTimestamp(s.GetLastSeenTime()),
						s.GetHostname(),
					})
				}
				printTable(
					[]string{"NAME", "DISPLAY", "STATUS", "MODE", "LAST_SEEN", "HOSTNAME"},
					rows,
				)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newSiteGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a single site (accepts p/s or projects/p/sites/s).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := canonicalSiteName(args[0])
			if err != nil {
				return err
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewSiteServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			s, err := client.GetSite(ctx, &quicktunv1.GetSiteRequest{Name: name})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, s, func() {
				rows := [][]string{{
					s.GetName(),
					s.GetDisplayName(),
					s.GetStatus().String(),
					s.GetMode().String(),
					formatTimestamp(s.GetLastSeenTime()),
					s.GetHostname(),
				}}
				printTable(
					[]string{"NAME", "DISPLAY", "STATUS", "MODE", "LAST_SEEN", "HOSTNAME"},
					rows,
				)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newSiteCreateCmd() *cobra.Command {
	var (
		displayName string
		mode        string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "create <project> <slug>",
		Short: "Create a site within a project.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent := canonicalProjectName(args[0])
			site := &quicktunv1.Site{
				DisplayName: displayName,
			}
			if mode != "" {
				m, err := parseSiteMode(mode)
				if err != nil {
					return err
				}
				site.Mode = m
			}

			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewSiteServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			s, err := client.CreateSite(ctx, &quicktunv1.CreateSiteRequest{
				Parent: parent,
				SiteId: args[1],
				Site:   site,
			})
			if err != nil {
				return err
			}
			return renderOrJSON(asJSON, s, func() {
				fmt.Printf("Created %s\n", s.GetName())
			})
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "human-readable name")
	cmd.Flags().StringVar(&mode, "mode", "", "site mode: endpoint or subnet (default: project default)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newSiteDeleteCmd() *cobra.Command {
	var (
		yesFlag bool
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a site (accepts p/s or projects/p/sites/s).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := canonicalSiteName(args[0])
			if err != nil {
				return err
			}
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

			client := quicktunv1.NewSiteServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.DeleteSite(ctx, &quicktunv1.DeleteSiteRequest{
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
	cmd.Flags().BoolVar(&force, "force", false, "cascade-delete services (default: refuse if non-empty)")
	return cmd
}

// parseSiteMode maps the operator-friendly --mode flag value to the
// generated enum. We accept just the suffix ("endpoint", "subnet") so
// operators don't have to type "SITE_MODE_ENDPOINT".
func parseSiteMode(s string) (quicktunv1.SiteMode, error) {
	switch strings.ToLower(s) {
	case "endpoint":
		return quicktunv1.SiteMode_SITE_MODE_ENDPOINT, nil
	case "subnet":
		return quicktunv1.SiteMode_SITE_MODE_SUBNET, nil
	}
	return quicktunv1.SiteMode_SITE_MODE_UNSPECIFIED,
		fmt.Errorf("invalid --mode %q (want endpoint or subnet)", s)
}

// formatTimestamp renders a *timestamppb.Timestamp for table output.
// Lives here rather than in output.go because it's only useful for
// site-shaped rows; service rows have nothing time-typed in the column
// set. Returns "-" for the zero / nil case so empty cells line up
// visually in the tabwriter output.
func formatTimestamp(ts *timestamppb.Timestamp) string {
	if ts == nil || !ts.IsValid() || (ts.GetSeconds() == 0 && ts.GetNanos() == 0) {
		return "-"
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}
