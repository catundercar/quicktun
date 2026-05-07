package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

// newStatusCmd implements `quicktun status`: a small operational dashboard
// that calls AdminService.GetSystemStatus and renders the counts. Admin-only
// (server-side enforces).
func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show control plane status (admin only).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewAdminServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
			if err != nil {
				return err
			}

			if asJSON {
				return printJSON(resp)
			}

			fmt.Printf("operators:        %d\n", resp.GetOperatorCount())
			fmt.Printf("projects:         %d active, %d disabled\n",
				resp.GetProjectCountActive(), resp.GetProjectCountDisabled())
			fmt.Printf("sites:            %d online, %d offline, %d pending\n",
				resp.GetSiteCountOnline(), resp.GetSiteCountOffline(), resp.GetSiteCountPending())
			fmt.Printf("services:         %d\n", resp.GetServiceCount())
			fmt.Printf("supervisors:      %d\n", resp.GetSupervisorRunningCount())
			if len(resp.GetStaleSites()) > 0 {
				fmt.Println("\nstale sites (no recent heartbeat):")
				for _, s := range resp.GetStaleSites() {
					last := "never"
					if ts := s.GetLastSeenAt(); ts != nil {
						last = ts.AsTime().Format(time.RFC3339)
					}
					fmt.Printf("  %s  last_seen=%s  hostname=%s\n",
						s.GetName(), last, s.GetHostname())
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
