package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/migration"
)

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending SQL migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reads the persistent --config flag inherited from the root command.
			cfgPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("migrate: load config: %w", err)
			}
			if cfg.Database.Driver != "sqlite" {
				return fmt.Errorf("migrate: only sqlite driver supported in Phase 1, got %q", cfg.Database.Driver)
			}
			if err := migration.Up(cfg.Database.DSN); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}
