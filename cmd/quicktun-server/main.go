// quicktun-server is the control-plane binary.
//
// Subcommands:
//
//	migrate   apply pending SQL migrations
//	version   print build version and exit
//
// Future subcommands (Phase 1 milestones):
//
//	serve     run the gRPC + grpc-gateway server
//	admin     create / list operators
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "quicktun-server",
		Short: "quicktun control plane server",
	}

	root.PersistentFlags().String("config", "etc/server.yaml", "path to YAML config")

	root.AddCommand(versionCmd())
	root.AddCommand(migrateCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(adminCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
