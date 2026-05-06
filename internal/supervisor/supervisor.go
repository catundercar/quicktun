// Package supervisor runs and supervises a single child process with
// auto-restart and exponential backoff. Used by relay.Manager to host
// rathole-server, and (later) by auth-proxy.
package supervisor

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Spec describes a supervised process.
type Spec struct {
	Name   string // logical name for logs
	Binary string // absolute path
	Args   []string
	Env    []string

	// OnLog receives each line of stdout/stderr.
	OnLog func(line, source string)
	// OnExit is invoked once per process exit (after each restart attempt).
	OnExit func(err error)
}

// Supervisor manages one child process.
type Supervisor struct {
	spec Spec
	lg   *zap.Logger

	mu     sync.Mutex
	cmd    *exec.Cmd
	stopCh chan struct{}
}

// New constructs a Supervisor.
func New(spec Spec, lg *zap.Logger) *Supervisor {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Supervisor{spec: spec, lg: lg, stopCh: make(chan struct{})}
}

// Pid returns the running child's PID, or 0 if not running.
func (s *Supervisor) Pid() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Run launches the child and restarts it on exit, until ctx is cancelled.
// On context cancel, Run sends SIGTERM and waits up to 5s for graceful exit.
// Blocks until the supervisor is fully stopped.
func (s *Supervisor) Run(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := s.runOnce(ctx)
		if s.spec.OnExit != nil {
			s.spec.OnExit(err)
		}
		if ctx.Err() != nil {
			return
		}

		s.lg.Warn("supervisor: child exited",
			zap.String("name", s.spec.Name),
			zap.Error(err),
			zap.Duration("backoff", backoff))

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// Stop is a convenience for callers that don't pass a ctx.WithCancel.
// It closes a sentinel; Run will exit on the next iteration after seeing it.
// Most callers should use ctx.WithCancel and pass the cancel-able ctx to Run.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(termSignal)
	}
}

func (s *Supervisor) runOnce(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.spec.Binary, s.spec.Args...)
	cmd.Env = s.spec.Env
	cmd.SysProcAttr = platformSysProcAttr()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.pipe(stdout, "stdout") }()
	go func() { defer wg.Done(); s.pipe(stderr, "stderr") }()

	waitErr := cmd.Wait()
	wg.Wait()

	s.mu.Lock()
	s.cmd = nil
	s.mu.Unlock()

	if waitErr != nil && errors.Is(waitErr, context.Canceled) {
		return nil
	}
	return waitErr
}

func (s *Supervisor) pipe(r io.Reader, src string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if s.spec.OnLog != nil {
			s.spec.OnLog(line, src)
		}
		s.lg.Info("supervisor: child log",
			zap.String("name", s.spec.Name),
			zap.String("source", src),
			zap.String("line", line))
	}
}
