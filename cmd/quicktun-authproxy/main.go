// Package main is the quicktun-authproxy binary: a public TCP gateway that
// authenticates HTTP CONNECT requests via Bearer tokens and forwards each
// connection to the agent's project's loopback rathole-server.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "quicktun-authproxy",
		Short: "quicktun auth-proxy",
		Long:  "Public TCP gateway: HTTP CONNECT + Bearer auth, forwarding to per-project rathole-server.",
	}

	root.PersistentFlags().StringP("config", "c", "", "path to authproxy.yaml (required)")

	root.AddCommand(newRunCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
