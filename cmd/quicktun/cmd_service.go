package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage services exposed by a site.",
	}
	cmd.AddCommand(newServiceListCmd())
	cmd.AddCommand(newServiceGetCmd())
	cmd.AddCommand(newServiceCreateCmd())
	cmd.AddCommand(newServiceDeleteCmd())
	return cmd
}

func newServiceListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list <site>",
		Short: "List services within a site (accepts p/s or projects/p/sites/s).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent, err := canonicalSiteName(args[0])
			if err != nil {
				return err
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewServiceServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			resp, err := client.ListServices(ctx, &quicktunv1.ListServicesRequest{Parent: parent})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, resp.GetServices(), func() {
				rows := make([][]string, 0, len(resp.GetServices()))
				for _, s := range resp.GetServices() {
					rows = append(rows, serviceRow(s))
				}
				printTable(serviceHeaders(), rows)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newServiceGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get a single service (accepts p/s/svc or full resource name).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := canonicalServiceName(args[0])
			if err != nil {
				return err
			}
			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewServiceServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			s, err := client.GetService(ctx, &quicktunv1.GetServiceRequest{Name: name})
			if err != nil {
				return err
			}

			return renderOrJSON(asJSON, s, func() {
				printTable(serviceHeaders(), [][]string{serviceRow(s)})
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func newServiceCreateCmd() *cobra.Command {
	var (
		displayName string
		targetAddr  string
		targetPort  uint32
		protoStr    string
		asJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "create <site> <slug>",
		Short: "Create a service within a site.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent, err := canonicalSiteName(args[0])
			if err != nil {
				return err
			}
			proto, err := parseProto(protoStr)
			if err != nil {
				return err
			}

			_, conn, err := loadAndDial(cmd)
			if err != nil {
				return err
			}
			defer conn.Close() //nolint:errcheck

			client := quicktunv1.NewServiceServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			s, err := client.CreateService(ctx, &quicktunv1.CreateServiceRequest{
				Parent:    parent,
				ServiceId: args[1],
				Service: &quicktunv1.Service{
					DisplayName: displayName,
					TargetAddr:  targetAddr,
					TargetPort:  targetPort,
					Proto:       proto,
				},
			})
			if err != nil {
				return err
			}
			return renderOrJSON(asJSON, s, func() {
				fmt.Printf("Created %s (relay_port=%d)\n", s.GetName(), s.GetRelayPort())
			})
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "human-readable name")
	cmd.Flags().StringVar(&targetAddr, "target-addr", "", "target address reachable by the bastion (required)")
	cmd.Flags().Uint32Var(&targetPort, "target-port", 0, "target port (required)")
	cmd.Flags().StringVar(&protoStr, "proto", "tcp", "protocol: tcp (default) or udp")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	_ = cmd.MarkFlagRequired("target-addr")
	_ = cmd.MarkFlagRequired("target-port")
	return cmd
}

func newServiceDeleteCmd() *cobra.Command {
	var yesFlag bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a service (accepts p/s/svc or full resource name).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := canonicalServiceName(args[0])
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

			client := quicktunv1.NewServiceServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			if _, err := client.DeleteService(ctx, &quicktunv1.DeleteServiceRequest{Name: name}); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yesFlag, "yes", "y", false, "skip interactive confirmation")
	return cmd
}

// parseProto maps the friendly --proto flag value to the generated enum.
func parseProto(s string) (quicktunv1.Proto, error) {
	switch strings.ToLower(s) {
	case "", "tcp":
		return quicktunv1.Proto_PROTO_TCP, nil
	case "udp":
		return quicktunv1.Proto_PROTO_UDP, nil
	}
	return quicktunv1.Proto_PROTO_UNSPECIFIED, fmt.Errorf("invalid --proto %q (want tcp or udp)", s)
}

func serviceHeaders() []string {
	return []string{"NAME", "DISPLAY", "TARGET", "PROTO", "RELAY_PORT"}
}

// serviceRow extracts the columns shown in the table view. Sharing this
// between list and get keeps the two views aligned by construction —
// adding a column to one always adds it to the other.
func serviceRow(s *quicktunv1.Service) []string {
	target := fmt.Sprintf("%s:%d", s.GetTargetAddr(), s.GetTargetPort())
	return []string{
		s.GetName(),
		s.GetDisplayName(),
		target,
		s.GetProto().String(),
		strconv.FormatUint(uint64(s.GetRelayPort()), 10),
	}
}

