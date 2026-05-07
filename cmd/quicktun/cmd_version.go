package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is overridable at build time via -ldflags
// `-X main.Version=<git sha>`.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("quicktun", Version)
		},
	}
}
