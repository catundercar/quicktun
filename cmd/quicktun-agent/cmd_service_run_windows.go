//go:build windows

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/tulip/quicktun/internal/agent"
)

const serviceName = "quicktun-agent"

type winService struct {
	cfg *agent.Config
	lg  *zap.Logger
}

// Execute implements svc.Handler. Called by the SCM when the service is
// started. It blocks until the service is stopped.
func (s *winService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := agent.New(s.cfg, s.lg)
	if err != nil {
		s.lg.Error("agent: New failed", zap.Error(err))
		return false, 1
	}
	defer rt.Close() //nolint:errcheck

	runErr := make(chan error, 1)
	go func() { runErr <- rt.Run(ctx) }()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case req := <-r:
			switch req.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-runErr
				status <- svc.Status{State: svc.Stopped}
				return false, 0
			case svc.Interrogate:
				status <- req.CurrentStatus
			}
		case err := <-runErr:
			if err != nil {
				s.lg.Error("agent: Run returned error", zap.Error(err))
				status <- svc.Status{State: svc.Stopped}
				return true, 1
			}
			status <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}

func newServiceRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "service-run",
		Short: "Run as a Windows service (called by SCM; not for manual use)",
		Long: `Integrates with the Windows Service Control Manager (SCM).
This subcommand is invoked by the MSI-installed Windows service entry.
Use 'run' instead for interactive / NSSM-based operation.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Root().PersistentFlags().GetString("config")
			if configPath == "" {
				return fmt.Errorf("--config is required")
			}

			cfg, err := agent.Load(configPath)
			if err != nil {
				return err
			}

			// Log to a file — the SCM doesn't have a terminal.
			logPath := os.Getenv("ProgramData") + `\quicktun\logs\agent.log`
			lg, err := newFileLogger(logPath)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer lg.Sync() //nolint:errcheck

			interactive, err := svc.IsAnInteractiveSession()
			if err != nil {
				return fmt.Errorf("svc.IsAnInteractiveSession: %w", err)
			}

			runner := svc.Run
			if interactive {
				// Running from a shell (debug mode): behave like a foreground process
				// but still exercise the SCM handler code path.
				runner = debug.Run
			}

			return runner(serviceName, &winService{cfg: cfg, lg: lg})
		},
	}
}

func newFileLogger(path string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{path}
	cfg.ErrorOutputPaths = []string{path}
	return cfg.Build()
}

// init registers the event-log source so Windows Event Viewer can decode our
// log entries. Best-effort: ignore failures (e.g. running without admin rights).
func init() {
	_ = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
}
