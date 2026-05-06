package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/agent"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the agent version.",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("quicktun-agent", agent.AgentVersion)
		},
	}
}
