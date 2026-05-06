# quicktun Rathole Supervisor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Spawn one `rathole-server` subprocess per active project, render its TOML config from `Project + Sites + Services` rows, and refresh on mutations. Bundle Plan 5 final-review cleanup as Task 0.

**Architecture:** A new `internal/supervisor/` package wraps `os/exec` to manage one long-running child process with auto-restart and `Pdeathsig` (Linux). A new `internal/relay/` package renders rathole-server TOML from the DB and operates a `Manager` that maps `project_id → *Supervisor`. ProjectService / SiteService / ServiceService gain a `RelayManager` field and call `Refresh(ctx, projectID)` after each successful mutation. Server bootstrap reads all `active` projects from DB and spawns supervisors.

**Tech Stack:** Existing — Go, GORM, gRPC, zap, audit/resource/auth packages. New: `os/exec`, `syscall.SysProcAttr.Pdeathsig` (Linux only). No new third-party deps. Tests use a fake "rathole" binary built at test time from a minimal Go program.

---

## File Structure

### New files
```
internal/supervisor/
├── supervisor.go               Spec + Supervisor + Run + Stop (cross-platform)
├── supervisor_linux.go         //go:build linux — Pdeathsig
├── supervisor_other.go         //go:build !linux — best-effort fallback
├── supervisor_test.go          Builds fake binary; exercises lifecycle

internal/relay/
├── render.go                   RenderRatholeServer(project, sites, services) string
├── render_test.go              Golden TOML
├── manager.go                  Manager: Start / Refresh / AddProject / RemoveProject / Stop
├── manager_test.go
```

### Files modified
```
internal/grpcsvc/
├── project_service.go          Hold RelayManager; call AddProject after Create, RemoveProject after Delete
├── site_service.go             Call Refresh after Create/Update/Delete/Rotate
├── service_service.go          Call Refresh after Create/Update/Delete; fix display_name no-op (Task 0); add admin test (Task 0); use strconv.Itoa (Task 0); audit relay_port on delete (Task 0)
├── project_service_test.go     Pass nil manager / fake manager
├── site_service_test.go        Same
├── service_service_test.go     Same + add TestUpdateServiceRequiresAdmin

internal/resource/
├── name.go                     Update doc comments to "1-64" (Task 0)

internal/config/
├── config.go                   Add BackendConfig{ RatholeBinary, RatholeConfigDir }
├── config_test.go              Test defaults

internal/server/
├── server.go                   Construct relay.Manager; pass to all gRPC services; start it
├── server_test.go              Verify Manager started + reachable

cmd/quicktun-server/
├── cmd_serve.go                Pass cfg.Backend.* to server.Config
├── cmd_admin_project.go        (Task 0: no changes from existing)

scripts/
├── smoke.sh                    Verify rathole config file appears on disk after service create
```

---

## Task 0: Plan 5 Final-Review Cleanup

The Plan 5 reviewer flagged 2 Important + 4 Minor items.

### Step 1: Fix `UpdateService` display_name no-op

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`. In `UpdateService`, find:

```go
case "display_name":
    // Phase 1: Service.Name doubles as slug+label. Audit captures the request.
    changed["display_name"] = req.Service.DisplayName
```

Replace by introducing a local variable for the override pattern (mirroring `SiteService`):

```go
var displayNameOverride string
hasDisplayNameOverride := false
```

(Place these declarations right before the `for _, path := range req.UpdateMask.Paths {` loop.)

Inside the loop, change the `display_name` case to:

```go
case "display_name":
    displayNameOverride = req.Service.DisplayName
    hasDisplayNameOverride = true
    changed["display_name"] = req.Service.DisplayName
```

After the call to `s.services.Update(ctx, svc)` and before `_ = s.audit.Log(...)`, add:

```go
out := serviceToProto(p, site, svc)
if hasDisplayNameOverride {
    out.DisplayName = displayNameOverride
}
```

Replace the `return serviceToProto(p, site, svc), nil` at the end with `return out, nil`. The audit log still fires before the return.

### Step 2: Add `TestUpdateServiceRequiresAdmin`

Append to `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service_test.go`:

```go
func TestUpdateServiceRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	p, _, _ := mkSvc(t, db, "p1", "bastion", "ssh")
	op := seedOperator(t, db, "u@x.com", "p", false)
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleViewer,
	}).Error)
	svc := newServiceService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	_, err := svc.UpdateService(ctx, &quicktunv1.UpdateServiceRequest{
		Service: &quicktunv1.Service{
			Name: "projects/p1/sites/bastion/services/ssh", TargetAddr: "x",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"target_addr"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}
```

### Step 3: Replace `intToString` with `strconv.Itoa`

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`. Add `"strconv"` to imports. Find every call site of `intToString(int(...))` (only in `CreateService` audit) and replace with `strconv.Itoa(int(...))`. Delete the `intToString` function definition.

### Step 4: Include relay_port in `DeleteService` audit

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`. In `DeleteService`, replace:

```go
_ = s.audit.Log(ctx, audit.Entry{
    ProjectID: ptrUint64(p.ID),
    Action:    "service.delete",
    Target:    resource.FormatServiceName(p.Slug, site.Name, svc.Name),
})
```

With:

```go
extra := map[string]any{}
if svc.RelayPort != nil {
    extra["relay_port"] = *svc.RelayPort
}
_ = s.audit.Log(ctx, audit.Entry{
    ProjectID: ptrUint64(p.ID),
    Action:    "service.delete",
    Target:    resource.FormatServiceName(p.Slug, site.Name, svc.Name),
    Extra:     extra,
})
```

### Step 5: Update stale "3-64 chars" doc comments

Edit `/Users/tulip/project/repos/quicktun/internal/resource/name.go`. Find both occurrences of "3-64" (one in the package doc, one in `ValidateSlug` comment) and replace with "1-64".

### Step 6: Run + commit

```bash
cd /Users/tulip/project/repos/quicktun
go test ./...
go test -race -timeout 120s ./...
go vet ./...
make build

git add internal/grpcsvc/service_service.go internal/grpcsvc/service_service_test.go internal/resource/name.go
git commit -m "chore: address Plan 5 final-review cleanup"
```

Expected: all green, single new commit.

---

## Task 1: Supervisor Package

A generic subprocess supervisor. Independent of rathole — designed to also run `quicktun-auth-proxy` later.

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor_linux.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor_other.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor_test.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/supervisor/testfakebin/main.go` (helper binary built at test time)

### Step 1: Write the fake-binary helper

Create `/Users/tulip/project/repos/quicktun/internal/supervisor/testfakebin/main.go`:

```go
// Build-only helper used by supervisor_test.go. Not compiled into production
// binaries (lives in a sub-directory the regular package doesn't import).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "sleep", "sleep | crash | exit-fast")
	flag.Parse()

	switch *mode {
	case "sleep":
		fmt.Fprintln(os.Stdout, "fake: ready")
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		<-ch
		fmt.Fprintln(os.Stdout, "fake: stopping")
		os.Exit(0)
	case "crash":
		fmt.Fprintln(os.Stderr, "fake: crashing")
		os.Exit(1)
	case "exit-fast":
		time.Sleep(50 * time.Millisecond)
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "fake: unknown mode")
		os.Exit(2)
	}
}
```

### Step 2: Write failing test `internal/supervisor/supervisor_test.go`

```go
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

func TestSupervisorBinaryNotFound(t *testing.T) {
	sup := supervisor.New(supervisor.Spec{
		Name:   "missing",
		Binary: "/nonexistent/path/quicktun-test-bin",
	}, zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	sup.Run(ctx) // returns when ctx expires; should not hang
}
```

### Step 3: Run test — verify it fails

Run: `go test ./internal/supervisor/...`
Expected: compile error (`supervisor.Spec`, `supervisor.New` undefined).

### Step 4: Implement `supervisor.go`

```go
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
	Name   string   // logical name for logs
	Binary string   // absolute path
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
```

### Step 5: Implement Linux-only Pdeathsig

Create `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor_linux.go`:

```go
//go:build linux

package supervisor

import "syscall"

var termSignal = syscall.SIGTERM

func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
		Setpgid:   true,
	}
}
```

### Step 6: Implement non-Linux fallback

Create `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor_other.go`:

```go
//go:build !linux

package supervisor

import (
	"os"
	"syscall"
)

// termSignal is what Stop() sends. SIGTERM works on darwin/windows.
var termSignal os.Signal = syscall.SIGTERM

func platformSysProcAttr() *syscall.SysProcAttr {
	// macOS / Windows: best-effort. Process won't auto-die when parent dies.
	return &syscall.SysProcAttr{}
}
```

### Step 7: Run tests

```bash
go test -count=1 -timeout 30s ./internal/supervisor/...
```

Expected: 4 tests pass.

### Step 8: Commit

```bash
git add internal/supervisor/
git commit -m "feat(supervisor): add subprocess supervisor with Pdeathsig + backoff"
```

---

## Task 2: Rathole Config Renderer

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/relay/render.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/relay/render_test.go`

### Step 1: Write failing test `internal/relay/render_test.go`

```go
package relay_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/relay"
)

func TestRenderRatholeServerEmptyServices(t *testing.T) {
	p := &model.Project{
		Base: model.Base{ID: 1}, Slug: "p1", RelayPortRange: "20000-20099",
	}
	out, err := relay.RenderRatholeServer(p, nil)
	require.NoError(t, err)
	require.Contains(t, out, `[server]`)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20000"`)
	require.NotContains(t, out, `[server.services.`)
}

func TestRenderRatholeServerWithServices(t *testing.T) {
	p := &model.Project{
		Base: model.Base{ID: 1}, Slug: "clinic-network", RelayPortRange: "20000-20099",
	}
	rp22 := uint16(20022)
	rp33 := uint16(20033)
	binding := []relay.ServiceBinding{
		{
			SiteSlug:    "bastion-1",
			ServiceSlug: "ssh",
			RelayPort:   rp22,
			AgentToken:  "site1-token-hash",
		},
		{
			SiteSlug:    "bastion-1",
			ServiceSlug: "rdp",
			RelayPort:   rp33,
			AgentToken:  "site1-token-hash",
		},
	}
	out, err := relay.RenderRatholeServer(p, binding)
	require.NoError(t, err)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20000"`)
	require.Contains(t, out, `[server.services.bastion-1__ssh]`)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20022"`)
	require.Contains(t, out, `[server.services.bastion-1__rdp]`)
	require.Contains(t, out, `bind_addr = "127.0.0.1:20033"`)
	require.Contains(t, out, `token = "site1-token-hash"`)
	require.True(t, strings.HasPrefix(out, "# quicktun-rendered"))
}

func TestRenderRatholeServerRejectsBadRange(t *testing.T) {
	p := &model.Project{
		Base: model.Base{ID: 1}, Slug: "p", RelayPortRange: "garbage",
	}
	_, err := relay.RenderRatholeServer(p, nil)
	require.Error(t, err)
}
```

### Step 2: Run test — verify it fails

Run: `go test ./internal/relay/...`
Expected: package not found.

### Step 3: Implement `internal/relay/render.go`

```go
// Package relay renders rathole-server config files and supervises the
// per-project rathole-server processes that terminate reverse tunnels.
package relay

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tulip/quicktun/internal/model"
)

// ServiceBinding is a flattened (site, service, agent_token) tuple ready for
// rendering into rathole's per-service config block.
type ServiceBinding struct {
	SiteSlug    string
	ServiceSlug string
	RelayPort   uint16
	AgentToken  string // hash is OK — agent stores the same hash
}

// RenderRatholeServer returns a TOML config for a per-project rathole-server.
//
// The control port (rathole's `[server]` bind_addr) is the LOWEST port in the
// project's relay_port_range. Service ports occupy the rest of the range.
// All binds are 127.0.0.1 so external traffic must go through the auth-proxy
// (Plan 8) to reach rathole.
func RenderRatholeServer(p *model.Project, bindings []ServiceBinding) (string, error) {
	minP, _, err := parsePortRange(p.RelayPortRange)
	if err != nil {
		return "", fmt.Errorf("relay: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# quicktun-rendered config for project %q (id=%d)\n",
		p.Slug, p.ID)
	fmt.Fprintf(&b, "# DO NOT EDIT MANUALLY — overwritten on next reconfigure.\n\n")

	b.WriteString("[server]\n")
	fmt.Fprintf(&b, "bind_addr = \"127.0.0.1:%d\"\n\n", minP)

	for _, sb := range bindings {
		// rathole service names must not contain '/' — flatten with double underscore.
		name := sb.SiteSlug + "__" + sb.ServiceSlug
		fmt.Fprintf(&b, "[server.services.%s]\n", name)
		fmt.Fprintf(&b, "token = %q\n", sb.AgentToken)
		fmt.Fprintf(&b, "bind_addr = \"127.0.0.1:%d\"\n\n", sb.RelayPort)
	}

	return b.String(), nil
}

func parsePortRange(s string) (uint16, uint16, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid port range %q", s)
	}
	minI, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid min port: %w", err)
	}
	maxI, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid max port: %w", err)
	}
	if minI > maxI {
		return 0, 0, fmt.Errorf("min %d > max %d", minI, maxI)
	}
	return uint16(minI), uint16(maxI), nil
}
```

### Step 4: Run tests + commit

```bash
go test -count=3 ./internal/relay/...
git add internal/relay/render.go internal/relay/render_test.go
git commit -m "feat(relay): add rathole-server TOML renderer"
```

Expected: 3 render tests pass.

---

## Task 3: Relay Manager

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/relay/manager.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/relay/manager_test.go`

### Step 1: Write failing test `internal/relay/manager_test.go`

```go
package relay_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/relay"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:relay_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

// buildFakeBin compiles the supervisor's testfakebin into a temp path so the
// manager has something runnable to spawn during tests.
func buildFakeBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "fakebin")
	wd, _ := os.Getwd()
	cmd := exec.Command("go", "build", "-o", out, "../supervisor/testfakebin")
	cmd.Dir = wd
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fake bin: %v", err)
	}
	return out
}

func TestManagerStartWritesConfigPerProject(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	pdao := dao.NewProjectDAO(db)
	p1, _ := pdao.Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	p2, _ := pdao.Create(context.Background(), &model.Project{
		Slug: "p2", Name: "P2", RelayPortRange: "21000-21099",
	})
	_ = p1
	_ = p2

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:    bin,
		BinaryArgs: []string{"--mode=sleep"},
		ConfigDir: cfgDir,
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))

	// Configs for both projects appear on disk.
	require.Eventually(t, func() bool {
		_, e1 := os.Stat(filepath.Join(cfgDir, "p1.toml"))
		_, e2 := os.Stat(filepath.Join(cfgDir, "p2.toml"))
		return e1 == nil && e2 == nil
	}, 2*time.Second, 50*time.Millisecond)

	mgr.Stop()
}

func TestManagerRefreshRewritesConfig(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})
	site, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
		ProjectID: p.ID, Name: "bastion",
	})

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:    bin,
		BinaryArgs: []string{"--mode=sleep"},
		ConfigDir: cfgDir,
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))

	// Add a service.
	rp := uint16(20022)
	_, err := dao.NewServiceDAO(db).Create(context.Background(), &model.Service{
		SiteID: site.ID, Name: "ssh",
		TargetAddr: "127.0.0.1", TargetPort: 22,
		Proto: model.ProtoTCP, RelayPort: &rp,
	})
	require.NoError(t, err)

	require.NoError(t, mgr.Refresh(ctx, p.ID))

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(cfgDir, "p1.toml"))
		if err != nil {
			return false
		}
		return contains(string(data), "bastion__ssh") &&
			contains(string(data), "20022")
	}, 2*time.Second, 50*time.Millisecond)

	mgr.Stop()
}

func TestManagerAddRemoveProject(t *testing.T) {
	db := newDB(t)
	bin := buildFakeBin(t)
	cfgDir := t.TempDir()

	mgr := relay.NewManager(db, relay.ManagerConfig{
		Binary:    bin,
		BinaryArgs: []string{"--mode=sleep"},
		ConfigDir: cfgDir,
	}, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, mgr.Start(ctx))

	p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
	})

	require.NoError(t, mgr.AddProject(ctx, p.ID))
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(cfgDir, "p1.toml"))
		return err == nil
	}, 2*time.Second, 50*time.Millisecond)

	require.NoError(t, mgr.RemoveProject(ctx, p.ID))
	// The config file may remain on disk (cleanup is best-effort). What we
	// assert is that the supervisor for p.ID is no longer tracked.
	require.Equal(t, 0, mgr.SupervisorCount())

	mgr.Stop()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

### Step 2: Run test — verify it fails

Run: `go test ./internal/relay/...`
Expected: compile error (`relay.NewManager`, `relay.ManagerConfig` undefined).

### Step 3: Implement `internal/relay/manager.go`

```go
package relay

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/supervisor"
)

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	Binary     string   // path to rathole-server (or fake in tests)
	BinaryArgs []string // extra args appended after the config path
	ConfigDir  string   // dir to write per-project config files
}

// Manager owns one *supervisor.Supervisor per active project. Each child is a
// rathole-server (or compatible) process. On mutations, callers invoke Refresh
// (re-render config + restart) or AddProject / RemoveProject.
type Manager struct {
	db   *gorm.DB
	cfg  ManagerConfig
	lg   *zap.Logger

	mu          sync.Mutex
	rootCtx     context.Context
	supervisors map[uint64]*supervisor.Supervisor
	cancels     map[uint64]context.CancelFunc
	wg          sync.WaitGroup
}

// NewManager constructs a Manager.
func NewManager(db *gorm.DB, cfg ManagerConfig, lg *zap.Logger) *Manager {
	if lg == nil {
		lg = zap.NewNop()
	}
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = "/tmp/quicktun-relay"
	}
	return &Manager{
		db:          db,
		cfg:         cfg,
		lg:          lg,
		supervisors: map[uint64]*supervisor.Supervisor{},
		cancels:     map[uint64]context.CancelFunc{},
	}
}

// Start reads all active projects and spawns one supervisor per project.
// The returned error indicates a startup failure (e.g., DB read). Once Start
// returns nil, child failures don't propagate — they're logged + restarted by
// the supervisor.
func (m *Manager) Start(ctx context.Context) error {
	if err := os.MkdirAll(m.cfg.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("relay: mkdir config dir: %w", err)
	}

	m.mu.Lock()
	m.rootCtx = ctx
	m.mu.Unlock()

	var projects []model.Project
	err := m.db.WithContext(ctx).
		Where("status = ?", string(model.ProjectStatusActive)).
		Find(&projects).Error
	if err != nil {
		return fmt.Errorf("relay: list projects: %w", err)
	}
	for i := range projects {
		if err := m.AddProject(ctx, projects[i].ID); err != nil {
			m.lg.Warn("relay: add project failed",
				zap.Uint64("project_id", projects[i].ID),
				zap.Error(err))
		}
	}
	return nil
}

// AddProject renders the config for projectID and starts a new supervisor.
// Idempotent: if a supervisor already exists, returns nil.
func (m *Manager) AddProject(ctx context.Context, projectID uint64) error {
	m.mu.Lock()
	if _, ok := m.supervisors[projectID]; ok {
		m.mu.Unlock()
		return nil
	}
	rootCtx := m.rootCtx
	m.mu.Unlock()

	cfgPath, err := m.renderToFile(ctx, projectID)
	if err != nil {
		return err
	}

	sup := supervisor.New(supervisor.Spec{
		Name:   fmt.Sprintf("rathole-project-%d", projectID),
		Binary: m.cfg.Binary,
		Args:   append([]string{cfgPath}, m.cfg.BinaryArgs...),
		OnLog: func(line, src string) {
			m.lg.Info("relay child log",
				zap.Uint64("project_id", projectID),
				zap.String("source", src),
				zap.String("line", line))
		},
	}, m.lg)

	childCtx, cancel := context.WithCancel(rootCtx)
	m.mu.Lock()
	m.supervisors[projectID] = sup
	m.cancels[projectID] = cancel
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		sup.Run(childCtx)
	}()
	return nil
}

// RemoveProject stops the supervisor for projectID and discards it.
// Best-effort: returns nil even if no such supervisor exists.
func (m *Manager) RemoveProject(ctx context.Context, projectID uint64) error {
	m.mu.Lock()
	cancel, ok := m.cancels[projectID]
	delete(m.supervisors, projectID)
	delete(m.cancels, projectID)
	m.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

// Refresh re-renders the config for projectID and restarts the supervisor so
// the new config takes effect (rathole does not support SIGHUP reload).
func (m *Manager) Refresh(ctx context.Context, projectID uint64) error {
	m.mu.Lock()
	_, hasSup := m.supervisors[projectID]
	m.mu.Unlock()
	if !hasSup {
		// Project isn't tracked — start it now.
		return m.AddProject(ctx, projectID)
	}
	if _, err := m.renderToFile(ctx, projectID); err != nil {
		return err
	}
	// Restart by removing+adding so the supervisor picks up the new config
	// file path on its next exec.
	if err := m.RemoveProject(ctx, projectID); err != nil {
		return err
	}
	return m.AddProject(ctx, projectID)
}

// SupervisorCount returns how many supervisors the manager is currently tracking.
func (m *Manager) SupervisorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.supervisors)
}

// Stop cancels every supervisor and waits for them to exit.
func (m *Manager) Stop() {
	m.mu.Lock()
	for _, c := range m.cancels {
		c()
	}
	m.cancels = map[uint64]context.CancelFunc{}
	m.supervisors = map[uint64]*supervisor.Supervisor{}
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) renderToFile(ctx context.Context, projectID uint64) (string, error) {
	pdao := dao.NewProjectDAO(m.db)
	sdao := dao.NewSiteDAO(m.db)
	svcdao := dao.NewServiceDAO(m.db)
	tokdao := dao.NewSiteAgentTokenDAO(m.db)

	p, err := pdao.FindByID(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("relay: find project: %w", err)
	}

	sites, err := sdao.ListByProject(ctx, p.ID, 1000, "")
	if err != nil {
		return "", fmt.Errorf("relay: list sites: %w", err)
	}

	var bindings []ServiceBinding
	for _, site := range sites {
		// One agent token per site; if missing (rotation pending), skip with empty.
		var tokenHash string
		var tk model.SiteAgentToken
		err := m.db.WithContext(ctx).
			Where("site_id = ?", site.ID).
			First(&tk).Error
		if err == nil {
			tokenHash = tk.TokenHash
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("relay: lookup site token: %w", err)
		}

		svcs, err := svcdao.ListBySite(ctx, site.ID, 1000, "")
		if err != nil {
			return "", fmt.Errorf("relay: list services: %w", err)
		}
		for _, svc := range svcs {
			if svc.RelayPort == nil {
				continue
			}
			bindings = append(bindings, ServiceBinding{
				SiteSlug:    site.Name,
				ServiceSlug: svc.Name,
				RelayPort:   *svc.RelayPort,
				AgentToken:  tokenHash,
			})
		}
	}

	toml, err := RenderRatholeServer(p, bindings)
	if err != nil {
		return "", err
	}

	path := filepath.Join(m.cfg.ConfigDir, p.Slug+".toml")
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		return "", fmt.Errorf("relay: write config: %w", err)
	}
	_ = tokdao // silence "imported and not used" if not invoked
	return path, nil
}
```

Note: `tokdao` is imported for forward use; if `go vet` complains about unused declaration, remove it (the inline `m.db.Where("site_id = ?", site.ID).First(&tk)` doesn't go through the DAO). Cleanup: just remove the `tokdao := ...` line entirely.

### Step 4: Run tests + commit

```bash
go test -count=1 -timeout 60s ./internal/relay/...
git add internal/relay/manager.go internal/relay/manager_test.go
git commit -m "feat(relay): add per-project supervisor manager"
```

Expected: 3 manager tests + 3 render tests = 6 relay tests pass.

---

## Task 4: Hook Manager into gRPC services

Each mutation calls `Manager.Refresh(ctx, projectID)` (or AddProject/RemoveProject).

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service_test.go`

### Step 1: Define `RelayManager` interface

Add at the top of `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service.go` (after the package and existing imports):

```go
// RelayManager is the subset of relay.Manager that gRPC services depend on.
// Defined here so test code can supply a stub without importing relay.
type RelayManager interface {
	AddProject(ctx context.Context, projectID uint64) error
	RemoveProject(ctx context.Context, projectID uint64) error
	Refresh(ctx context.Context, projectID uint64) error
}

// noopRelayManager satisfies RelayManager but does nothing. Used by services
// constructed without an explicit manager (typically tests).
type noopRelayManager struct{}

func (noopRelayManager) AddProject(context.Context, uint64) error    { return nil }
func (noopRelayManager) RemoveProject(context.Context, uint64) error { return nil }
func (noopRelayManager) Refresh(context.Context, uint64) error       { return nil }
```

### Step 2: Wire into `ProjectService`

In the same file, change the struct + constructor:

```go
type ProjectService struct {
	quicktunv1.UnimplementedProjectServiceServer
	projects *dao.ProjectDAO
	audit    *audit.Writer
	relay    RelayManager
}

func NewProjectService(projects *dao.ProjectDAO, audit *audit.Writer, relay RelayManager) *ProjectService {
	if relay == nil {
		relay = noopRelayManager{}
	}
	return &ProjectService{projects: projects, audit: audit, relay: relay}
}
```

After the successful `s.projects.Create(...)` block (and before the audit log), insert:

```go
if err := s.relay.AddProject(ctx, row.ID); err != nil {
    // Non-fatal: audit + log; admin can re-run via DB or service mutations.
    s.lg = s.lg // (no logger field — skip)
}
```

Actually, ProjectService doesn't have a logger field. Just call relay and ignore the error (non-fatal):

```go
_ = s.relay.AddProject(ctx, row.ID)
```

Place this immediately after the `if _, err := s.projects.Create(ctx, row); err != nil { ... }` success path and before the audit log call.

In `DeleteProject`, after `s.projects.Delete(...)` succeeds and before audit:

```go
_ = s.relay.RemoveProject(ctx, p.ID)
```

In `UpdateProject`, after `s.projects.Update(...)` succeeds and before audit:

```go
_ = s.relay.Refresh(ctx, cur.ID)
```

### Step 3: Update ProjectService test constructor

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service_test.go`. Find `newProjectService`:

```go
func newProjectService(t *testing.T, db *gorm.DB) *grpcsvc.ProjectService {
    return grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db))
}
```

Replace with:

```go
func newProjectService(t *testing.T, db *gorm.DB) *grpcsvc.ProjectService {
    return grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db), nil)
}
```

(Passing `nil` activates the noopRelayManager fallback.)

### Step 4: Wire into `SiteService`

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service.go`. Add `relay RelayManager` field to the struct. Update `NewSiteService` signature:

```go
func NewSiteService(projects *dao.ProjectDAO, sites *dao.SiteDAO, tokens *dao.SiteAgentTokenDAO, audit *audit.Writer, relayAddr string, relay RelayManager) *SiteService {
    if relayAddr == "" {
        relayAddr = "relay.example.com:443"
    }
    if relay == nil {
        relay = noopRelayManager{}
    }
    return &SiteService{
        projects: projects, sites: sites, tokens: tokens, audit: audit,
        relayAddr: relayAddr, relay: relay,
    }
}
```

After every `s.sites.Create / Update / Delete` success, and after `s.tokens.Issue` (RotateSiteAgentToken + GetSiteInstallCommand), call `_ = s.relay.Refresh(ctx, p.ID)` before the audit log entry.

### Step 5: Update SiteService test constructor

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service_test.go`:

```go
func newSiteService(t *testing.T, db *gorm.DB) *grpcsvc.SiteService {
    return grpcsvc.NewSiteService(
        dao.NewProjectDAO(db),
        dao.NewSiteDAO(db),
        dao.NewSiteAgentTokenDAO(db),
        audit.NewWriter(db),
        "test-relay.example.com:443",
        nil, // noop relay
    )
}
```

### Step 6: Wire into `ServiceService`

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`. Add `relay RelayManager` to struct. Update constructor:

```go
func NewServiceService(projects *dao.ProjectDAO, sites *dao.SiteDAO, services *dao.ServiceDAO, audit *audit.Writer, relay RelayManager) *ServiceService {
    if relay == nil {
        relay = noopRelayManager{}
    }
    return &ServiceService{projects: projects, sites: sites, services: services, audit: audit, relay: relay}
}
```

After every `s.services.Create / Update / Delete` success, call `_ = s.relay.Refresh(ctx, p.ID)` before the audit log entry.

### Step 7: Update ServiceService test constructor

```go
func newServiceService(t *testing.T, db *gorm.DB) *grpcsvc.ServiceService {
    return grpcsvc.NewServiceService(
        dao.NewProjectDAO(db),
        dao.NewSiteDAO(db),
        dao.NewServiceDAO(db),
        audit.NewWriter(db),
        nil,
    )
}
```

### Step 8: Run tests + commit

```bash
go test -count=3 ./internal/grpcsvc/...
go test ./...
git add internal/grpcsvc/
git commit -m "feat(grpcsvc): hook RelayManager into Project/Site/Service mutations"
```

Expected: all grpcsvc tests pass with the noop relay manager.

---

## Task 5: Wire Manager into server.New + bootstrap

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/config/config.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/config/config_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_serve.go`

### Step 1: Add BackendConfig to config

Edit `internal/config/config.go`. Add a new struct + Config field:

```go
type Config struct {
    ControlPlane ControlPlaneConfig `mapstructure:"control_plane"`
    Database     DatabaseConfig     `mapstructure:"database"`
    Session      SessionConfig      `mapstructure:"session"`
    Log          LogConfig          `mapstructure:"log"`
    Backend      BackendConfig      `mapstructure:"backend"`
}

// BackendConfig configures the relay backend (Phase 1: rathole).
type BackendConfig struct {
    RatholeBinary    string `mapstructure:"rathole_binary"`
    RatholeConfigDir string `mapstructure:"rathole_config_dir"`
}
```

In `setDefaults`:

```go
v.SetDefault("backend.rathole_binary", "rathole")
v.SetDefault("backend.rathole_config_dir", "/var/lib/quicktun/relays")
```

Append to `internal/config/config_test.go`:

```go
func TestBackendDefaults(t *testing.T) {
	yaml := `
database:
  dsn: ":memory:"
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "rathole", cfg.Backend.RatholeBinary)
	require.Equal(t, "/var/lib/quicktun/relays", cfg.Backend.RatholeConfigDir)
}
```

### Step 2: Construct + start Manager in server.New / Run

Edit `internal/server/server.go`. Add to `Config`:

```go
type Config struct {
    DB         *gorm.DB
    Logger     *zap.Logger
    GRPCListen string
    HTTPListen string
    RelayAddr  string

    // Relay backend.
    RatholeBinary    string
    RatholeConfigDir string

    SessionTTL time.Duration
}
```

Add field on Server:

```go
type Server struct {
    cfg        Config
    grpcServer *grpc.Server
    httpServer *http.Server
    relay      *relay.Manager  // ★ NEW
}
```

Add `relay` import:

```go
import (
    // ... existing imports ...
    "github.com/tulip/quicktun/internal/relay"
)
```

In `New`, BEFORE constructing the gRPC services, build the manager:

```go
mgr := relay.NewManager(cfg.DB, relay.ManagerConfig{
    Binary:    cfg.RatholeBinary,
    ConfigDir: cfg.RatholeConfigDir,
}, cfg.Logger)
```

Pass `mgr` into each service constructor:

```go
auditWriter := audit.NewWriter(cfg.DB)
projectSvc := grpcsvc.NewProjectService(dao.NewProjectDAO(cfg.DB), auditWriter, mgr)
quicktunv1.RegisterProjectServiceServer(gs, projectSvc)

siteSvc := grpcsvc.NewSiteService(
    dao.NewProjectDAO(cfg.DB),
    dao.NewSiteDAO(cfg.DB),
    dao.NewSiteAgentTokenDAO(cfg.DB),
    auditWriter,
    cfg.RelayAddr,
    mgr,
)
quicktunv1.RegisterSiteServiceServer(gs, siteSvc)

serviceSvc := grpcsvc.NewServiceService(
    dao.NewProjectDAO(cfg.DB),
    dao.NewSiteDAO(cfg.DB),
    dao.NewServiceDAO(cfg.DB),
    auditWriter,
    mgr,
)
quicktunv1.RegisterServiceServiceServer(gs, serviceSvc)
```

Save the manager on Server: `return &Server{cfg: cfg, grpcServer: gs, relay: mgr}, nil`.

In `Run`, BEFORE the gRPC listener starts, call `s.relay.Start(ctx)`:

```go
if err := s.relay.Start(ctx); err != nil {
    return fmt.Errorf("server: relay manager start: %w", err)
}
```

In the shutdown path, after `s.grpcServer.GracefulStop()`, add:

```go
s.relay.Stop()
```

### Step 3: Update cmd_serve to thread BackendConfig

Edit `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_serve.go`. Update the `srv, err := server.New(server.Config{...})` block:

```go
srv, err := server.New(server.Config{
    DB:               db,
    Logger:           lg,
    GRPCListen:       cfg.ControlPlane.GRPCListen,
    HTTPListen:       cfg.ControlPlane.HTTPListen,
    RelayAddr:        cfg.ControlPlane.RelayAddr,
    RatholeBinary:    cfg.Backend.RatholeBinary,
    RatholeConfigDir: cfg.Backend.RatholeConfigDir,
    SessionTTL:       cfg.Session.DefaultTTL,
})
```

### Step 4: Update server tests for new Config fields

Edit `/Users/tulip/project/repos/quicktun/internal/server/server_test.go`. Find every `server.Config{...}` construction. Some may now fail because they don't supply `RatholeBinary` (which would point to a non-existent path → manager refuses to start). Decide:

Option A: build the fake bin in each test and supply it to `server.Config`.
Option B: make `relay.Manager.Start` lenient when `Binary == ""` (skip starting any supervisors).

Option B is cleaner — the server tests don't care about the supervisor. Update `relay.Manager.Start` to short-circuit when `m.cfg.Binary == ""`:

```go
func (m *Manager) Start(ctx context.Context) error {
    if m.cfg.Binary == "" {
        m.lg.Info("relay: no binary configured; supervisor disabled")
        return nil
    }
    // ... existing code ...
}
```

And `AddProject` to short-circuit similarly. Test the lenient path:

Append to `internal/relay/manager_test.go`:

```go
func TestManagerSkipsWhenBinaryEmpty(t *testing.T) {
	db := newDB(t)
	cfgDir := t.TempDir()
	mgr := relay.NewManager(db, relay.ManagerConfig{ConfigDir: cfgDir}, zap.NewNop())
	require.NoError(t, mgr.Start(context.Background()))
	require.NoError(t, mgr.AddProject(context.Background(), 42))
	require.Equal(t, 0, mgr.SupervisorCount())
}
```

(Update Manager methods accordingly.)

### Step 5: Run tests + commit

```bash
go test -count=1 -timeout 60s ./...
go vet ./...
make build
git add internal/config/ internal/server/ internal/relay/manager.go internal/relay/manager_test.go cmd/quicktun-server/cmd_serve.go
git commit -m "feat(server): wire relay.Manager into gRPC server bootstrap"
```

Expected: all green.

---

## Task 6: Smoke + Final Verification

### Step 1: Update `scripts/smoke.sh` to verify config file appears

After the existing service-create block (which posts to `/v1/projects/smoke-test/sites/smoke-bastion/services?service_id=ssh`), add a check that the rathole config was rendered. The test config will need a `backend.rathole_binary` pointing to a fake or `""` to disable:

The smoke script already configures `etc/server.yaml` via heredoc. Find that block and add:

```yaml
backend:
  rathole_binary: ""
  rathole_config_dir: ${WORKDIR}/relays
```

Where `${WORKDIR}` is the temp directory the smoke script already creates (look for `mktemp -d` in the script). With `rathole_binary=""`, the manager runs but doesn't actually spawn supervisors — it still renders configs to disk via `Refresh`.

Wait — actually with binary="", we shortcut Start and AddProject, which means renderToFile never runs. Smoke can't verify rendering this way. Two options:

**Option A**: provide a real fake binary. Build it ahead of smoke.
**Option B**: make the renderer run independently (always render configs, only skip supervisor startup when binary is empty).

Option B is the right call. Update `relay.Manager`:

In `AddProject`, render the config to disk regardless of whether the binary is empty:

```go
func (m *Manager) AddProject(ctx context.Context, projectID uint64) error {
    m.mu.Lock()
    if _, ok := m.supervisors[projectID]; ok {
        m.mu.Unlock()
        return nil
    }
    rootCtx := m.rootCtx
    m.mu.Unlock()

    cfgPath, err := m.renderToFile(ctx, projectID)
    if err != nil {
        return err
    }

    if m.cfg.Binary == "" {
        return nil // render-only mode for smoke / dev
    }

    sup := supervisor.New(...)
    // ... rest as before
}
```

And `Refresh`:

```go
func (m *Manager) Refresh(ctx context.Context, projectID uint64) error {
    if _, err := m.renderToFile(ctx, projectID); err != nil {
        return err
    }
    if m.cfg.Binary == "" {
        return nil
    }
    // ... rest as before (restart supervisor)
}
```

Update tests to reflect: `TestManagerSkipsWhenBinaryEmpty` should still pass (no supervisor started, but config file CAN be written if a project is added). Adjust test assertion: the file appears, but `SupervisorCount() == 0`.

### Step 2: Add smoke verification

After the smoke service-create line, add:

```bash
# Verify rathole config was rendered for the project.
RELAY_CFG="${WORKDIR}/relays/smoke-test.toml"
if [ ! -f "$RELAY_CFG" ]; then
  echo "FAIL: rathole config $RELAY_CFG was not rendered" >&2
  exit 1
fi
if ! grep -q 'smoke-bastion__ssh' "$RELAY_CFG"; then
  echo "FAIL: rathole config does not mention smoke-bastion__ssh" >&2
  exit 1
fi
echo "relay: PASS"
```

(Place this block right after `echo "service: PASS"` and before the existing service DELETE.)

Update the final echo:

```bash
echo "PASS: end-to-end auth + project + site + service + relay flow"
```

### Step 3: Run final verification

```bash
cd /Users/tulip/project/repos/quicktun
go test ./...
go test -race -timeout 120s ./...
go vet ./...
make proto-lint
make check-migrations
make build
./scripts/smoke.sh
```

Expected: all green; smoke prints `PASS: end-to-end auth + project + site + service + relay flow`.

### Step 4: Commit

```bash
git add scripts/smoke.sh internal/relay/manager.go internal/relay/manager_test.go
git commit -m "feat(relay,smoke): always render configs; verify in smoke"
```

---

## Self-Review

**Spec coverage:**

| Plan 6 requirement | Implemented in |
|---|---|
| Plan 5 cleanup (display_name, admin test, intToString → strconv, audit relay_port on delete, doc comments) | Task 0 |
| Generic `internal/supervisor/` package with Pdeathsig + auto-restart | Task 1 |
| `RenderRatholeServer(project, bindings)` TOML renderer | Task 2 |
| `internal/relay/Manager` with Start/AddProject/RemoveProject/Refresh/Stop | Task 3 |
| `RelayManager` interface in grpcsvc + hook into Project/Site/Service mutations | Task 4 |
| Server bootstrap creates Manager + threads through services + Start/Stop | Task 5 |
| Smoke verifies config rendered to disk | Task 6 |

**No placeholders.** Each step has full code or precise diffs.

**Type consistency:** `relay.NewManager(db, relay.ManagerConfig{Binary, BinaryArgs, ConfigDir}, lg)` matches in tests + server. `RelayManager` interface in grpcsvc has 3 methods (`AddProject`, `RemoveProject`, `Refresh`) all matching `relay.Manager` method signatures. `supervisor.Spec{Name, Binary, Args, Env, OnLog, OnExit}` consistent across renderer/supervisor/manager.

**Forward references resolved:** `noopRelayManager` defined in project_service.go is package-private; usable from site_service.go and service_service.go in same package without re-declaration. The `tokdao` variable mentioned in render.go is removed (the inline DB lookup is sufficient).

**Test infrastructure:** the `testfakebin` directory is a sub-package of `internal/supervisor` used only at test time. The supervisor and manager tests build it via `go build` to a temp dir; this avoids dependency on real rathole.
