package supervisor_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/tulip/quicktun/internal/supervisor"
)

// buildFakeBin compiles the helper binary into a temp dir and returns its path.
func buildFakeBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "fakebin")
	cmd := exec.Command("go", "build", "-o", out, "./testfakebin")
	cmd.Dir, _ = os.Getwd() // internal/supervisor
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fake bin: %v", err)
	}
	return out
}

func TestSupervisorRunsToCompletion(t *testing.T) {
	bin := buildFakeBin(t)

	logCh := make(chan string, 16)
	sup := supervisor.New(supervisor.Spec{
		Name:   "fake",
		Binary: bin,
		Args:   []string{"--mode=sleep"},
		OnLog:  func(line, src string) { logCh <- line },
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sup.Run(ctx); close(done) }()

	// Wait for "fake: ready".
	deadline := time.After(2 * time.Second)
	for {
		select {
		case line := <-logCh:
			if line == "fake: ready" {
				goto ready
			}
		case <-deadline:
			t.Fatal("never saw 'ready'")
		}
	}
ready:
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop after cancel")
	}
}

func TestSupervisorRestartsOnCrash(t *testing.T) {
	bin := buildFakeBin(t)

	var mu sync.Mutex
	startCount := 0
	logCh := make(chan string, 32)

	sup := supervisor.New(supervisor.Spec{
		Name:   "fake-crash",
		Binary: bin,
		Args:   []string{"--mode=exit-fast"},
		OnLog:  func(line, src string) { logCh <- line },
		OnExit: func(err error) {
			mu.Lock()
			startCount++
			mu.Unlock()
		},
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { sup.Run(ctx); close(done) }()

	// exit-fast exits after ~50ms. Wait long enough for >= 2 restarts.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return startCount >= 2
	}, 5*time.Second, 100*time.Millisecond)

	cancel()
	<-done
}

func TestSupervisorBackoff(t *testing.T) {
	bin := buildFakeBin(t)
	t.Logf("fake bin at %s", bin)
	// We don't measure timing exactly (CI variance); just check it doesn't
	// thrash too fast: 5 second test should produce no more than ~25 restarts
	// for a binary that exits in 50ms.
	var mu sync.Mutex
	startCount := 0

	sup := supervisor.New(supervisor.Spec{
		Name:   "fake-bo",
		Binary: bin,
		Args:   []string{"--mode=exit-fast"},
		OnExit: func(err error) {
			mu.Lock()
			startCount++
			mu.Unlock()
		},
	}, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sup.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	require.Less(t, startCount, 30, "backoff didn't slow restarts (startCount=%d)", startCount)
	require.Greater(t, startCount, 1, "expected at least 2 restarts (startCount=%d)", startCount)
}

func TestSupervisorSendsSIGTERMOnCancel(t *testing.T) {
	bin := buildFakeBin(t)

	logCh := make(chan string, 16)
	sup := supervisor.New(supervisor.Spec{
		Name:   "fake-graceful",
		Binary: bin,
		Args:   []string{"--mode=sleep"},
		OnLog:  func(line, src string) { logCh <- line },
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sup.Run(ctx); close(done) }()

	// Wait for "fake: ready" so we know the child is running.
	deadline := time.After(2 * time.Second)
readyLoop:
	for {
		select {
		case line := <-logCh:
			if line == "fake: ready" {
				break readyLoop
			}
		case <-deadline:
			t.Fatal("never saw 'ready'")
		}
	}

	cancel()

	// fake binary writes "fake: stopping" on SIGTERM. If we get there, SIGTERM
	// arrived and the grace window worked.
	deadline = time.After(3 * time.Second)
	for {
		select {
		case line := <-logCh:
			if line == "fake: stopping" {
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("supervisor goroutine did not exit after graceful child stop")
				}
				return
			}
		case <-deadline:
			t.Fatal("never saw 'stopping' — SIGTERM not delivered or grace period broken")
		}
	}
}

func TestSupervisorBinaryNotFound(t *testing.T) {
	sup := supervisor.New(supervisor.Spec{
		Name:   "missing",
		Binary: "/nonexistent/path/quicktun-test-bin",
	}, zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	sup.Run(ctx) // returns when ctx expires; should not hang
}
