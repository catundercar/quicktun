package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/tulip/quicktun/internal/agent"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the agent (default).",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}

			cfg, err := agent.Load(configPath)
			if err != nil {
				return err
			}

			lg, err := zap.NewProduction()
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer lg.Sync() //nolint:errcheck

			rt, err := agent.New(cfg, lg)
			if err != nil {
				return err
			}
			defer rt.Close() //nolint:errcheck

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			lg.Info("agent: starting",
				zap.String("control_endpoint", cfg.ControlEndpoint),
				zap.String("state_dir", cfg.StateDir))

			if err := rt.Run(ctx); err != nil {
				return fmt.Errorf("agent: %w", err)
			}

			lg.Info("agent: stopped cleanly")
			return nil
		},
	}
}
