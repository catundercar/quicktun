package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/supervisor"
)

const (
	defaultBootstrapRetry = 5 * time.Second
	maxBootstrapBackoff   = 1 * time.Minute
	// maxBootstrapAttempts caps the retry loop so a permanently misconfigured
	// agent (wrong endpoint, wrong token, server permanently down) eventually
	// exits with an error instead of spinning forever — letting systemd report
	// service failure and operators notice.
	maxBootstrapAttempts = 10
	defaultHeartbeat     = 15 * time.Second
)

// AgentVersion is the version string the agent reports in Bootstrap and
// Heartbeat. Override at link time with -ldflags="-X 'github.com/tulip/quicktun/internal/agent.AgentVersion=X.Y.Z'".
var AgentVersion = "dev"

// Runtime is the long-running agent. Construct with New, drive with Run,
// release resources with Close. Run blocks until ctx is cancelled.
type Runtime struct {
	// cfg is read-only after New returns — Run, heartbeat, applyBootstrap,
	// startSupervisor all read it without locks. Do not mutate.
	cfg *Config
	lg  *zap.Logger

	conn *grpc.ClientConn
	cli  quicktunv1.AgentServiceClient

	// supMu guards the supervisor lifecycle fields (cancel + done) and
	// curVersion. Heartbeat reads curVersion, applyBootstrap writes both.
	// Invariant: at most one supervisor goroutine is alive at a time.
	// startSupervisor publishes (supCancel, supDone) under the lock;
	// stopSupervisor swaps both to nil under the lock and then waits on
	// the snapshotted done channel after releasing it (so the goroutine
	// can finish without blocking on supMu).
	supMu      sync.Mutex
	supCancel  context.CancelFunc
	supDone    chan struct{}
	curVersion string
}

// New dials the control plane and constructs a Runtime ready to Run. The
// supplied config must already be validated (use Load).
func New(cfg *Config, lg *zap.Logger) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("agent: nil config")
	}
	if lg == nil {
		lg = zap.NewNop()
	}
	// MkdirAll is idempotent and surfaces permission errors, but it does
	// NOT verify the directory is writable when it already exists with the
	// wrong owner/mode. A future hardening pass could add an explicit
	// writability probe (e.g. open a temp file) here.
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("agent: mkdir state dir: %w", err)
	}

	var transport credentials.TransportCredentials
	if cfg.TLSInsecure {
		transport = insecure.NewCredentials()
	} else {
		// TLS 1.3 is the secure default; if a deployment ever needs 1.2
		// (e.g. legacy load balancer in front of the control plane) we'll
		// add a config opt-in rather than weakening the default.
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13})
	}

	conn, err := grpc.NewClient(cfg.ControlEndpoint,
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(bearerCreds{token: cfg.Token, allowInsecure: cfg.TLSInsecure}),
	)
	if err != nil {
		return nil, fmt.Errorf("agent: dial: %w", err)
	}

	return &Runtime{
		cfg:  cfg,
		lg:   lg,
		conn: conn,
		cli:  quicktunv1.NewAgentServiceClient(conn),
	}, nil
}

// Close stops any running supervisor and closes the gRPC connection. Safe
// to call after Run returns.
func (r *Runtime) Close() error {
	r.stopSupervisor()
	if r.conn != nil {
		err := r.conn.Close()
		r.conn = nil
		return err
	}
	return nil
}

// Run drives the agent: bootstrap (with retry), apply, heartbeat loop,
// rebootstrap on signal. Returns when ctx is cancelled.
func (r *Runtime) Run(ctx context.Context) error {
	boot, err := r.bootstrapWithRetry(ctx)
	if err != nil {
		return err
	}
	if err := r.applyBootstrap(boot); err != nil {
		return err
	}

	hbInterval := time.Duration(boot.GetHeartbeatSeconds()) * time.Second
	if hbInterval <= 0 {
		hbInterval = defaultHeartbeat
	}
	tick := time.NewTicker(hbInterval)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			r.stopSupervisor()
			return nil
		case <-tick.C:
			resp, err := r.heartbeat(ctx)
			if err != nil {
				if ctx.Err() != nil {
					r.stopSupervisor()
					return nil
				}
				// Future: escalate after N consecutive failures (e.g. log at
				// Error level, or surface to a healthcheck endpoint). For
				// now we log every failure at Warn and keep ticking — the
				// supervisor keeps running on the last applied config.
				r.lg.Warn("agent: heartbeat failed", zap.Error(err))
				continue
			}
			if resp.GetShouldRebootstrap() {
				r.lg.Info("agent: server requested rebootstrap")
				newBoot, err := r.bootstrapWithRetry(ctx)
				if err != nil {
					return err
				}
				if err := r.applyBootstrap(newBoot); err != nil {
					r.lg.Warn("agent: applyBootstrap after rebootstrap failed",
						zap.Error(err))
				}
			}
		}
	}
}

// bootstrapWithRetry calls Bootstrap with exponential backoff. It gives up
// after maxBootstrapAttempts so a misconfigured agent eventually exits and
// systemd surfaces the failure, and short-circuits immediately on
// codes.Unauthenticated since waiting won't fix a bad token.
func (r *Runtime) bootstrapWithRetry(ctx context.Context) (*quicktunv1.BootstrapResponse, error) {
	backoff := defaultBootstrapRetry
	var lastErr error
	for attempt := 1; attempt <= maxBootstrapAttempts; attempt++ {
		resp, err := r.cli.Bootstrap(ctx, &quicktunv1.BootstrapRequest{
			Hostname:     r.hostname(),
			Os:           runtime.GOOS,
			AgentVersion: AgentVersion,
		})
		if err == nil {
			return resp, nil
		}
		lastErr = err
		// A bad token won't get better — fail fast so the operator sees it.
		if status.Code(err) == codes.Unauthenticated {
			return nil, fmt.Errorf("agent: bootstrap rejected (token invalid?): %w", err)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		r.lg.Warn("agent: bootstrap failed; retrying",
			zap.Int("attempt", attempt),
			zap.Int("max_attempts", maxBootstrapAttempts),
			zap.Error(err), zap.Duration("backoff", backoff))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBootstrapBackoff {
			backoff = maxBootstrapBackoff
		}
	}
	return nil, fmt.Errorf("agent: bootstrap failed after %d attempts: %w", maxBootstrapAttempts, lastErr)
}

func (r *Runtime) heartbeat(ctx context.Context) (*quicktunv1.HeartbeatResponse, error) {
	r.supMu.Lock()
	cv := r.curVersion
	r.supMu.Unlock()
	return r.cli.Heartbeat(ctx, &quicktunv1.HeartbeatRequest{
		Hostname:      r.hostname(),
		Os:            runtime.GOOS,
		AgentVersion:  AgentVersion,
		ConfigVersion: cv,
	})
}

// applyBootstrap renders the rathole-client config, writes it to disk, and
// (re)starts the supervisor unless we are in render-only mode.
func (r *Runtime) applyBootstrap(boot *quicktunv1.BootstrapResponse) error {
	toml, err := RenderRatholeClient(boot, r.cfg.Token)
	if err != nil {
		return fmt.Errorf("agent: render: %w", err)
	}
	cfgPath := filepath.Join(r.cfg.StateDir, "rathole-client.toml")
	// 0o600 — the file embeds sha256(token), which is the cryptographic
	// material rathole presents to the server. Treat as a secret.
	if err := os.WriteFile(cfgPath, []byte(toml), 0o600); err != nil {
		return fmt.Errorf("agent: write config: %w", err)
	}
	r.lg.Info("agent: rendered rathole-client config",
		zap.String("path", cfgPath),
		zap.String("config_version", boot.GetConfigVersion()))

	r.supMu.Lock()
	r.curVersion = boot.GetConfigVersion()
	r.supMu.Unlock()

	// Render-only mode: skip subprocess spawn. Used in tests and smoke
	// checks where we just want to verify what the agent would render.
	if r.cfg.RatholeBinary == "" {
		r.lg.Info("agent: rathole_binary empty; render-only mode (no subprocess)")
		return nil
	}

	// Tear down the previous supervisor (if any) before spawning the new
	// one. Mirrors the relay.Manager Refresh pattern.
	r.stopSupervisor()
	r.startSupervisor(cfgPath)
	return nil
}

func (r *Runtime) startSupervisor(cfgPath string) {
	// rathole's CLI is `rathole [flags] <config-path>`; flags ("--client")
	// must come BEFORE the positional arg.
	args := append([]string{}, r.cfg.RatholeArgs...)
	args = append(args, cfgPath)

	sup := supervisor.New(supervisor.Spec{
		Name:   "rathole-client",
		Binary: r.cfg.RatholeBinary,
		Args:   args,
		OnLog: func(line, src string) {
			r.lg.Info("rathole client",
				zap.String("source", src), zap.String("line", line))
		},
	}, r.lg)

	supCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	r.supMu.Lock()
	r.supCancel = cancel
	r.supDone = done
	r.supMu.Unlock()

	go func() {
		defer close(done)
		sup.Run(supCtx)
	}()
}

func (r *Runtime) stopSupervisor() {
	r.supMu.Lock()
	cancel, done := r.supCancel, r.supDone
	r.supCancel = nil
	r.supDone = nil
	r.supMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *Runtime) hostname() string {
	if r.cfg.HostnameOverride != "" {
		return r.cfg.HostnameOverride
	}
	h, _ := os.Hostname()
	return h
}

// bearerCreds attaches `Authorization: Bearer <token>` to every RPC. gRPC
// canonicalizes the metadata key, so lowercase here is fine.
type bearerCreds struct {
	token         string
	allowInsecure bool
}

func (b bearerCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

// RequireTransportSecurity returns false only when the operator has
// explicitly opted into TLSInsecure mode (dev). In production this returns
// true, forcing gRPC to refuse to send the bearer token over plaintext.
func (b bearerCreds) RequireTransportSecurity() bool { return !b.allowInsecure }
