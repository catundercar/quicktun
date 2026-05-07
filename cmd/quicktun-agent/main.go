// Package main is the quicktun-agent binary: a long-running daemon that runs
// on bastion hosts, registers with the control plane, and supervises a
// rathole-client subprocess to expose local services through the relay.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "quicktun-agent",
		Short: "quicktun site agent",
		Long:  "Long-running agent that runs on a bastion host and tunnels services to the quicktun control plane.",
	}

	root.PersistentFlags().StringP("config", "c", "", "path to agent.yaml (required)")

	root.AddCommand(newRunCmd())
	root.AddCommand(newVersionCmd())
	if c := newServiceRunCmd(); c != nil {
		root.AddCommand(c)
	}

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
