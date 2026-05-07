package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/logger"
	"github.com/tulip/quicktun/internal/migration"
	"github.com/tulip/quicktun/internal/server"
)

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the gRPC + grpc-gateway control-plane server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			lg, err := logger.New(cfg.Log)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			defer lg.Sync()

			// Apply any pending migrations before opening the gorm pool. Idempotent
			// when schema is already up to date. Without this, a fresh deploy fails
			// at first request with confusing "no such table" errors.
			if err := migration.Up(cfg.Database.DSN); err != nil {
				return fmt.Errorf("serve: migrate: %w", err)
			}

			db, err := dao.Open(cfg.Database.DSN, lg)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			defer func() { sqlDB, _ := db.DB(); sqlDB.Close() }()

			srv, err := server.New(server.Config{
				DB:                  db,
				Logger:              lg,
				GRPCListen:          cfg.ControlPlane.GRPCListen,
				HTTPListen:          cfg.ControlPlane.HTTPListen,
				RelayAddr:           cfg.ControlPlane.RelayAddr,
				RatholeBinary:       cfg.Backend.RatholeBinary,
				RatholeArgs:         cfg.Backend.RatholeArgs,
				RatholeConfigDir:    cfg.Backend.RatholeConfigDir,
				AuthProxyPublicAddr: cfg.Backend.AuthProxyPublicAddr,
				SessionTTL:          cfg.Session.DefaultTTL,
			})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if err := srv.Run(ctx); err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			lg.Info("server stopped cleanly")
			return nil
		},
	}
}
