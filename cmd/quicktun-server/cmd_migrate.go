package main

import "github.com/spf13/cobra"

// migrateCmd is fleshed out in Task 13.
func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending SQL migrations",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("not yet implemented")
		},
	}
}
