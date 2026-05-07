// Package supervisor runs and supervises a single child process with
// auto-restart and exponential backoff. Used by relay.Manager to host
// rathole-server, and (later) by auth-proxy.
package supervisor

import (
	"bufio"
	"context"
	"io"
	"os"
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
	// Env is appended to the parent's environment. nil/empty means inherit only.
	Env []string

	// OnLog receives each line of stdout/stderr.
	OnLog func(line, source string)
	// OnExit is invoked once per process exit (after each restart attempt).
	OnExit func(err error)
}

// Supervisor manages one child process.
type Supervisor struct {
	spec Spec
	lg   *zap.Logger

	mu  sync.Mutex
	cmd *exec.Cmd
}

// New constructs a Supervisor.
func New(spec Spec, lg *zap.Logger) *Supervisor {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Supervisor{spec: spec, lg: lg}
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

		start := time.Now()
		err := s.runOnce(ctx)
		if s.spec.OnExit != nil {
			s.spec.OnExit(err)
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > 60*time.Second {
			backoff = time.Second
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

func (s *Supervisor) runOnce(ctx context.Context) error {
	cmd := exec.Command(s.spec.Binary, s.spec.Args...)
	if len(s.spec.Env) > 0 {
		cmd.Env = append(os.Environ(), s.spec.Env...)
	}
	// else: leave nil so child inherits parent env
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
	if err := platformAfterStart(cmd); err != nil {
		s.lg.Warn("supervisor: post-start hook failed",
			zap.String("name", s.spec.Name), zap.Error(err))
	}

	s.mu.Lock()
	s.cmd = cmd
	s.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.pipe(stdout, "stdout") }()
	go func() { defer wg.Done(); s.pipe(stderr, "stderr") }()

	// On ctx cancel: SIGTERM, wait up to 5s for graceful exit, then SIGKILL.
	waitDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Signal(termSignal)
				select {
				case <-waitDone:
					// child exited within grace window
				case <-time.After(5 * time.Second):
					_ = cmd.Process.Kill()
				}
			}
		case <-waitDone:
		}
	}()

	waitErr := cmd.Wait()
	close(waitDone)
	wg.Wait()

	s.mu.Lock()
	s.cmd = nil
	s.mu.Unlock()

	if ctx.Err() != nil {
		return nil
	}
	return waitErr
}

func (s *Supervisor) pipe(r io.Reader, src string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
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
