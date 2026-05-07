package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/authproxy"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the auth-proxy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}

			cfg, err := authproxy.LoadConfig(configPath)
			if err != nil {
				return err
			}

			lg, err := zap.NewProduction()
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer lg.Sync() //nolint:errcheck

			db, err := gorm.Open(sqlite.Open(cfg.Database.DSN), &gorm.Config{})
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}

			router := authproxy.NewRouter(db)
			srv := authproxy.New(router.Route, lg)

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			lg.Info("authproxy: starting",
				zap.String("listen_addr", cfg.ListenAddr),
				zap.String("dsn", cfg.Database.DSN))

			if err := srv.Serve(ctx, cfg.ListenAddr); err != nil {
				return fmt.Errorf("authproxy: %w", err)
			}
			lg.Info("authproxy: stopped cleanly")
			return nil
		},
	}
}
