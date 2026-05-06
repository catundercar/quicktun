# quicktun Supervisor Polish (Plan 6.5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address production-readiness items from Plan 6's final review before Plan 7 (agent) starts. Lock in the token contract, validate inputs, surface failures fast.

**Why now:** Plan 7's agent code depends on the token contract (Critical from Plan 6 review). Operational issues (silent crash-loops on missing binary, port-range overlap, disabled projects still spawning rathole) compound with every project added — easier to fix once with no live data than retroactively.

**Out of scope:** `Site.GetAgentBootstrap` RPC (Plan 7 Task 0), supervisor health metric (Plan 8 + auth-proxy), `BinaryArgs`-as-yaml (vs Args-as-list).

---

## Token Contract Decision

The renderer at `internal/relay/render.go:20` writes `SiteAgentToken.TokenHash` (sha256 hex) into rathole's `token = ...` field. Operators receive the *raw* token from `RotateSiteAgentToken` / `GetSiteInstallCommand`. The DB only stores the hash.

**Decision: agents transform raw → sha256 hex on send.** When the Plan 7 agent connects to rathole-server, it computes `sha256_hex(raw_token)` and presents that as rathole's token. The raw never leaves the agent.

This means:
- No schema change.
- The "token" in the install command IS the raw — operators paste it as-is into the agent config.
- The agent has one token, but presents it differently to two endpoints: raw (Bearer) for the control-plane API, hashed for rathole.
- Future split (separate rathole-token from agent-token) is possible without a wire-format change.

Task 0 captures this in code comments + a new docs file.

---

## File Structure

```
internal/grpcsvc/
├── project_service.go          (Task 1: overlap check; Task 3: disabled→Remove)
├── project_service_test.go     (Task 1: overlap test; Task 3: disabled test)

internal/config/
├── config.go                   (Task 2: BackendConfig.RatholeArgs)
├── config_test.go              (Task 2: defaults updated)

internal/server/
├── server.go                   (Task 2: thread RatholeArgs)

internal/relay/
├── manager.go                  (Task 2: LookPath preflight; Task 0: doc updates)
├── render.go                   (Task 0: doc update)
├── manager_test.go             (Task 2: LookPath test)

internal/supervisor/
├── supervisor.go               (Task 4: SIGTERM grace)
├── supervisor_test.go          (Task 4: graceful exit test)

cmd/quicktun-server/
├── cmd_serve.go                (Task 2: pass RatholeArgs)

docs/
├── 07-token-contract.md        NEW (Task 0)
```

---

## Task 0: Token Contract — Document Option A

### Step 1: Create `docs/07-token-contract.md`

```markdown
# Site Agent Token Contract

## Storage
- The control plane generates a 32-byte random token and stores `sha256_hex(token)` in `site_agent_tokens.token_hash`.
- The raw token is returned exactly once via `RotateSiteAgentToken` / `GetSiteInstallCommand` and discarded.

## Two consumers, two presentations

The agent uses ONE token, presented two ways:

1. **Control plane API (Plan 7+)**: agent sends `Authorization: Bearer <raw_token>` for heartbeat, config sync, etc. The server hashes on receipt and looks up `token_hash`.

2. **rathole-server**: agent's rathole-client.toml has `token = "<sha256_hex(raw)>"`. The server's rathole-server.toml is rendered (in `internal/relay/render.go`) with the same hex hash, so rathole sees matching shared secrets without ever holding the raw.

## Why this design

- No schema change. The DB only ever holds the hash.
- The "install command" output is directly usable by an operator (no transformation required on their end).
- Future split (e.g., separate `rathole_token` from `agent_token`) is a render-side change; no wire-format break.

## Implementation notes for Plan 7 agent author

- On first boot, the agent reads the raw token from a config file or env var (operator pastes it).
- Compute `rathole_token = sha256_hex(raw_token)` once at startup; pass to rathole-client.toml.
- Use the raw for Bearer auth against the control plane.
```

### Step 2: Update `internal/relay/render.go:20`

Change the comment on `ServiceBinding.AgentToken` from:
```
AgentToken  string // hash is OK — agent stores the same hash
```
To:
```
AgentToken  string // sha256_hex of the raw token — agent computes the same hex
                   // before presenting to rathole. See docs/07-token-contract.md.
```

### Step 3: Commit

```bash
cd /Users/tulip/project/repos/quicktun
git add docs/07-token-contract.md internal/relay/render.go
git commit -m "docs: capture site agent token contract (option A: agent hashes raw)"
```

---

## Task 1: Project port-range overlap validation

When two projects share `relay_port_range`, the second rathole-server crash-loops silently. Reject overlap at create/update time.

### Step 1: Add `OverlapsAny` to `dao.ProjectDAO`

Edit `/Users/tulip/project/repos/quicktun/internal/dao/project.go`. Add:

```go
// OverlapsAny returns true if minP..maxP overlaps any *other* project's range.
// excludeID is the project we're updating (pass 0 on create).
func (d *ProjectDAO) OverlapsAny(ctx context.Context, minP, maxP uint16, excludeID uint64) (bool, error) {
	var rows []model.Project
	q := d.db.WithContext(ctx)
	if excludeID != 0 {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return false, err
	}
	for _, p := range rows {
		oMin, oMax, err := resource.ParsePortRange(p.RelayPortRange)
		if err != nil {
			continue // drift; ignore
		}
		if minP <= oMax && oMin <= maxP {
			return true, nil
		}
	}
	return false, nil
}
```

(Add `"github.com/tulip/quicktun/internal/resource"` import if not present.)

### Step 2: Wire into ProjectService.CreateProject

In `internal/grpcsvc/project_service.go`, inside `CreateProject` AFTER the existing range parsing/validation but BEFORE `s.projects.Create(...)`, add:

```go
minP, maxP, err := resource.ParsePortRange(req.Project.RelayPortRange)
if err != nil {
    return nil, status.Errorf(codes.InvalidArgument, "relay_port_range: %v", err)
}
overlap, err := s.projects.OverlapsAny(ctx, minP, maxP, 0)
if err != nil {
    return nil, status.Errorf(codes.Internal, "overlap check: %v", err)
}
if overlap {
    return nil, status.Errorf(codes.FailedPrecondition,
        "relay_port_range %q overlaps another project's range", req.Project.RelayPortRange)
}
```

(If `ParsePortRange` is already called in CreateProject, reuse the result instead of parsing twice. Read the existing CreateProject end-to-end before pasting.)

### Step 3: Wire into ProjectService.UpdateProject

If `UpdateProject` allows changing `relay_port_range` (read the implementation; if the field is not in the update mask whitelist, skip this step). If it does:

```go
case "relay_port_range":
    newMin, newMax, err := resource.ParsePortRange(req.Project.RelayPortRange)
    if err != nil { return nil, status.Errorf(codes.InvalidArgument, "...") }
    overlap, err := s.projects.OverlapsAny(ctx, newMin, newMax, cur.ID)
    if err != nil { return nil, status.Errorf(codes.Internal, "...") }
    if overlap { return nil, status.Errorf(codes.FailedPrecondition, "...") }
    cur.RelayPortRange = req.Project.RelayPortRange
    changed["relay_port_range"] = req.Project.RelayPortRange
```

### Step 4: Test

In `internal/grpcsvc/project_service_test.go` add:

```go
func TestCreateProjectRejectsOverlappingRange(t *testing.T) {
    db := openTestDB(t)
    svc := newProjectService(t, db)
    ctx := adminCtx(t, db)

    _, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
        ProjectId: "p1",
        Project: &quicktunv1.Project{
            DisplayName: "P1", RelayPortRange: "20000-20099",
        },
    })
    require.NoError(t, err)

    _, err = svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
        ProjectId: "p2",
        Project: &quicktunv1.Project{
            DisplayName: "P2", RelayPortRange: "20050-20149",
        },
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.FailedPrecondition, st.Code())
}
```

### Step 5: Verify + commit

```bash
go test -count=1 ./internal/grpcsvc/...
go test ./...
go vet ./...
git add internal/dao/project.go internal/grpcsvc/project_service.go internal/grpcsvc/project_service_test.go
git commit -m "feat(grpcsvc): reject project relay_port_range overlap"
```

---

## Task 2: rathole_args + LookPath preflight

### Step 1: Add `RatholeArgs []string` to BackendConfig

Edit `/Users/tulip/project/repos/quicktun/internal/config/config.go`:

```go
type BackendConfig struct {
    RatholeBinary    string   `mapstructure:"rathole_binary"`
    RatholeArgs      []string `mapstructure:"rathole_args"`
    RatholeConfigDir string   `mapstructure:"rathole_config_dir"`
}
```

In setDefaults:
```go
v.SetDefault("backend.rathole_args", []string{"--server"})
```

(Most rathole versions accept `rathole --server <cfg>`; if that's wrong for the operator's version they can override in YAML.)

### Step 2: Update test

In `config_test.go::TestBackendDefaults`:

```go
require.Equal(t, []string{"--server"}, cfg.Backend.RatholeArgs)
```

### Step 3: Thread through server.New + cmd_serve.go

In `internal/server/server.go::Config`: add `RatholeArgs []string` field. Pass it into `relay.NewManager`:

```go
mgr := relay.NewManager(cfg.DB, relay.ManagerConfig{
    Binary:     cfg.RatholeBinary,
    BinaryArgs: cfg.RatholeArgs,
    ConfigDir:  cfg.RatholeConfigDir,
}, cfg.Logger)
```

In `cmd/quicktun-server/cmd_serve.go`: add `RatholeArgs: cfg.Backend.RatholeArgs,`.

### Step 4: LookPath preflight in Manager.Start

Edit `/Users/tulip/project/repos/quicktun/internal/relay/manager.go::Start`. After the mkdir and before the project iteration, add:

```go
if m.cfg.Binary != "" {
    resolved, err := exec.LookPath(m.cfg.Binary)
    if err != nil {
        m.lg.Warn("relay: rathole binary not found; supervisors will crash-loop",
            zap.String("binary", m.cfg.Binary), zap.Error(err))
        // Don't fail Start — operator may still want the API up. But log loudly.
    } else {
        m.lg.Info("relay: rathole binary resolved",
            zap.String("binary", m.cfg.Binary), zap.String("resolved", resolved))
    }
}
```

(Add `"os/exec"` to imports if not present.)

### Step 5: Test the LookPath warning is non-fatal

Add to `internal/relay/manager_test.go`:

```go
func TestManagerStartTolersBadBinary(t *testing.T) {
    db := newDB(t)
    cfgDir := t.TempDir()
    mgr := relay.NewManager(db, relay.ManagerConfig{
        Binary:    "/nonexistent/quicktun-test-rathole",
        ConfigDir: cfgDir,
    }, zap.NewNop())
    require.NoError(t, mgr.Start(context.Background()))
    mgr.Stop()
}
```

### Step 6: Verify + commit

```bash
go test -count=1 ./...
go vet ./...
make build
git add internal/config/ internal/server/server.go internal/relay/manager.go internal/relay/manager_test.go cmd/quicktun-server/cmd_serve.go
git commit -m "feat(config,relay): add rathole_args, LookPath preflight"
```

---

## Task 3: Disabled-project teardown

When `UpdateProject` flips `status` to `disabled`, the supervisor should be torn down (not refreshed). When it flips back to `active`, AddProject again.

### Step 1: Update ProjectService.UpdateProject

Edit `internal/grpcsvc/project_service.go`. Find the post-update relay call. Currently:

```go
if err := s.relay.Refresh(ctx, cur.ID); err != nil { /* warn */ }
```

Replace with:

```go
switch cur.Status {
case model.ProjectStatusActive:
    if err := s.relay.AddProject(ctx, cur.ID); err != nil {
        s.lg.Warn("relay add failed", zap.Uint64("project_id", cur.ID),
            zap.String("op", "project.update.activate"), zap.Error(err))
    } else if err := s.relay.Refresh(ctx, cur.ID); err != nil {
        // AddProject is idempotent and Refresh handles the renewal.
        s.lg.Warn("relay refresh failed", zap.Uint64("project_id", cur.ID),
            zap.String("op", "project.update"), zap.Error(err))
    }
case model.ProjectStatusDisabled:
    if err := s.relay.RemoveProject(ctx, cur.ID); err != nil {
        s.lg.Warn("relay remove failed", zap.Uint64("project_id", cur.ID),
            zap.String("op", "project.update.disable"), zap.Error(err))
    }
}
```

(Note: `AddProject` is idempotent in `relay.Manager`. The dual-call sequence `AddProject → Refresh` handles the case where the project was previously disabled and the supervisor wasn't tracked.)

Actually simpler: just call `Refresh` for active (it falls through to AddProject if no supervisor exists), and `RemoveProject` for disabled:

```go
switch cur.Status {
case model.ProjectStatusActive:
    if err := s.relay.Refresh(ctx, cur.ID); err != nil {
        s.lg.Warn("relay refresh failed", zap.Uint64("project_id", cur.ID),
            zap.String("op", "project.update"), zap.Error(err))
    }
case model.ProjectStatusDisabled:
    if err := s.relay.RemoveProject(ctx, cur.ID); err != nil {
        s.lg.Warn("relay remove failed", zap.Uint64("project_id", cur.ID),
            zap.String("op", "project.update.disable"), zap.Error(err))
    }
}
```

(`Refresh` already handles "no existing supervisor → fall through to AddProject" per `manager.go:236-238`.)

### Step 2: Update Manager.Start to skip disabled projects

Confirm that `manager.go::Start` already filters `Where("status = ?", string(model.ProjectStatusActive))`. If yes, no change.

### Step 3: Test

Append to `project_service_test.go`:

```go
func TestUpdateProjectDisabledTearsDownSupervisor(t *testing.T) {
    db := openTestDB(t)
    rec := &recordingRelay{}
    svc := grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db), zap.NewNop(), rec)
    ctx := adminCtx(t, db)

    created, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
        ProjectId: "p1",
        Project: &quicktunv1.Project{
            DisplayName: "P1", RelayPortRange: "20000-20099",
        },
    })
    require.NoError(t, err)
    require.Len(t, rec.added, 1)

    _, err = svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
        Project: &quicktunv1.Project{
            Name: created.Name, Status: quicktunv1.Project_DISABLED,
        },
        UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
    })
    require.NoError(t, err)
    require.Len(t, rec.removed, 1)

    _, err = svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
        Project: &quicktunv1.Project{
            Name: created.Name, Status: quicktunv1.Project_ACTIVE,
        },
        UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
    })
    require.NoError(t, err)
    require.GreaterOrEqual(t, len(rec.refreshed), 1)
}
```

(Adjust the proto enum names to match the actual enum: read `gen/quicktun/v1/project.pb.go` for the right symbol — likely `Project_STATUS_ACTIVE` / `Project_STATUS_DISABLED` or similar.)

### Step 4: Verify + commit

```bash
go test -count=1 ./internal/grpcsvc/...
go vet ./...
git add internal/grpcsvc/project_service.go internal/grpcsvc/project_service_test.go
git commit -m "feat(grpcsvc): tear down supervisor when project disabled"
```

---

## Task 4: SIGTERM grace in supervisor

`exec.CommandContext` sends SIGKILL on context cancel. rathole has no chance to drain. Add a SIGTERM-then-SIGKILL grace period.

### Step 1: Modify supervisor.runOnce

Edit `/Users/tulip/project/repos/quicktun/internal/supervisor/supervisor.go::runOnce`. Currently:

```go
cmd := exec.CommandContext(ctx, s.spec.Binary, s.spec.Args...)
```

Replace the cmd construction + Wait pattern with manual context-watching:

```go
cmd := exec.Command(s.spec.Binary, s.spec.Args...)
if len(s.spec.Env) > 0 {
    cmd.Env = append(os.Environ(), s.spec.Env...)
}
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

// Watch context: on cancel, send SIGTERM, wait up to 5s, then SIGKILL.
waitDone := make(chan struct{})
go func() {
    select {
    case <-ctx.Done():
        if cmd.Process != nil {
            _ = cmd.Process.Signal(termSignal)
            select {
            case <-waitDone:
                // exited gracefully within window
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
```

### Step 2: Add a graceful-exit test

Edit `internal/supervisor/supervisor_test.go`. Add:

```go
func TestSupervisorSendsSIGTERMOnCancel(t *testing.T) {
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

	// Wait for ready.
	for {
		select {
		case line := <-logCh:
			if line == "fake: ready" {
				goto ready
			}
		case <-time.After(2 * time.Second):
			t.Fatal("never saw ready")
		}
	}
ready:
	cancel()

	// The fake binary writes "fake: stopping" on SIGTERM. Poll for it.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case line := <-logCh:
			if line == "fake: stopping" {
				<-done
				return
			}
		case <-deadline:
			t.Fatal("never saw 'stopping' — SIGTERM not delivered or grace period broken")
		}
	}
}
```

### Step 3: Verify + commit

```bash
go test -count=1 -timeout 60s ./internal/supervisor/...
go test -race -timeout 60s ./internal/supervisor/...
go test ./...
go vet ./...
GOOS=linux GOARCH=amd64 go vet ./internal/supervisor/...
git add internal/supervisor/supervisor.go internal/supervisor/supervisor_test.go
git commit -m "fix(supervisor): SIGTERM with 5s grace before SIGKILL"
```

---

## Task 5: Final verification + smoke

```bash
cd /Users/tulip/project/repos/quicktun
go test ./...
go test -race -timeout 180s ./...
go vet ./...
make proto-lint
make check-migrations
make build
./scripts/smoke.sh
GOOS=linux GOARCH=amd64 go vet ./...
```

All green. No new commit needed unless verification surfaces an issue.

---

## Self-review

| Plan 6 final-review item | Resolved by |
|---|---|
| C-1: Token contract | Task 0 |
| I-1: Project port-range overlap | Task 1 |
| I-2: rathole BinaryArgs knob | Task 2 |
| I-3: LookPath preflight | Task 2 |
| I-4: Disabled→RemoveProject | Task 3 |
| M-1: SIGTERM grace | Task 4 |

Deferred to Plan 7+: bootstrap RPC (I-6), supervisor health surface (I-5), narrow `dao.NewXxxDAO`-per-call cleanup (M-6), striped Refresh mutex (M-4 — not a correctness bug).
