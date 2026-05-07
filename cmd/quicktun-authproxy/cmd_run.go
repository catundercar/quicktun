package main

import (
	"context"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/authproxy"
	"github.com/tulip/quicktun/internal/health"
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

			// Health endpoint runs on a dedicated HTTP listener (separate from
			// the CONNECT proxy which speaks raw TCP/HTTP-CONNECT). Disabled
			// when HealthListenAddr is empty. Goroutine exits on ctx cancel
			// via Shutdown.
			if cfg.HealthListenAddr != "" {
				go runHealthServer(ctx, lg, db, cfg.HealthListenAddr)
			}

			if err := srv.Serve(ctx, cfg.ListenAddr); err != nil {
				return fmt.Errorf("authproxy: %w", err)
			}
			lg.Info("authproxy: stopped cleanly")
			return nil
		},
	}
}

// runHealthServer hosts a tiny HTTP server with /healthz wired to a
// gorm.DB.PingContext probe. Returns when the listener errors or ctx is
// cancelled. Logs (but does not fail the parent command) on listener errors —
// the auth-proxy must continue serving traffic even if the health endpoint
// can't bind (e.g. port already in use).
func runHealthServer(ctx context.Context, lg *zap.Logger, db *gorm.DB, addr string) {
	check := func() (bool, []string) {
		sqlDB, err := db.DB()
		if err != nil {
			return false, []string{"db: " + err.Error()}
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			return false, []string{"db ping: " + err.Error()}
		}
		return true, nil
	}
	mux := http.NewServeMux()
	mux.Handle("/healthz", health.Handler(check))
	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	lg.Info("authproxy: health endpoint listening", zap.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		lg.Warn("authproxy: health endpoint stopped", zap.Error(err))
	}
}
