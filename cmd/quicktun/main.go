// Package main is the quicktun operator CLI: a single binary that
// authenticates against the control plane and drives operator workflows
// (project / site / service CRUD, agent token rotation, etc.).
//
// State lives in ~/.config/quicktun/credentials.yaml (mode 0o600). See
// internal/clicred for the on-disk schema.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quicktun",
		Short: "quicktun operator CLI",
		Long:  "Operator CLI for managing quicktun projects, sites, services, and forwarding tunnels.",
	}
	var configPath string
	cmd.PersistentFlags().StringVar(&configPath, "config", "",
		"path to credentials.yaml (default: $QUICKTUN_CONFIG or ~/.config/quicktun/credentials.yaml)")
	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newForwardCmd())
	return cmd
}
