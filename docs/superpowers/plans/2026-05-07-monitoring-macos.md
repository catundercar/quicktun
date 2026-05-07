# quicktun Self-Monitoring + macOS Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two related improvements:

1. **Service self-monitoring** — three pieces:
   - Site liveness sweeper (control plane background goroutine that flips stale `online` sites to `offline`).
   - `/healthz` HTTP endpoints on each binary so systemd / launchd / nginx / k8s can probe health.
   - `AdminService.GetSystemStatus` RPC + `quicktun status` CLI for at-a-glance operational visibility.

2. **macOS agent deployment** — `quicktun-agent` running as a `launchd` LaunchDaemon. Linux already supported (Plan 10).

**Out of scope:**
- Prometheus metrics / Grafana dashboards (Plan 12).
- External webhooks / Slack alerts on supervisor crash loops (Plan 12).
- macOS control plane (Phase 1 expects Linux for server + auth-proxy).
- macOS LaunchAgent (per-user); we ship LaunchDaemon (system-wide).
- Windows agent (Plan 7.5).

---

## Design

### Site liveness sweeper

A goroutine in the control plane runs every `sweeper_interval` (default 30s). It:
1. Reads sites where `status = 'online' AND last_seen_at < now() - site_offline_after` (default 90s).
2. Updates each row's `status = 'offline'` + emits an audit log entry per flipped site.

Configuration (new):
```yaml
backend:
  sweeper_interval: 30s
  site_offline_after: 90s
```

Empty / zero values disable the sweeper (useful for tests).

**Heartbeat handling unchanged**: Plan 7's `AgentService.Heartbeat` continues to set `status = online + last_seen_at = now`. The sweeper is the OPPOSITE direction (online → offline).

### `/healthz` HTTP endpoints

Each binary serves a JSON `/healthz`:
- `200 OK` `{"status": "ok"}` when healthy.
- `503 Service Unavailable` `{"status": "degraded", "reasons": ["..."]}` otherwise.

**Server**: piggyback on the existing grpc-gateway HTTP listen. Register `/healthz` directly on the http.ServeMux. Healthy when DB ping succeeds.

**Auth-proxy**: add new optional config `health_listen_addr` (default `127.0.0.1:8444`). Runs a separate `http.ListenAndServe` for `/healthz`. Healthy when DB ping succeeds.

**Agent**: add new optional config `health_listen_addr` (default empty = disabled). Runs `http.ListenAndServe` for `/healthz`. Healthy when:
- last successful Bootstrap was less than 2 × heartbeat_interval ago, AND
- supervisor child is running (or render-only mode is configured)

### `AdminService.GetSystemStatus`

New gRPC method on a NEW `AdminService`. (No existing AdminService — create it.)

```proto
service AdminService {
  rpc GetSystemStatus(GetSystemStatusRequest) returns (GetSystemStatusResponse) {
    option (google.api.http) = {
      get: "/v1/admin:status"
    };
  }
}

message GetSystemStatusResponse {
  uint32 operator_count = 1;
  uint32 project_count_active = 2;
  uint32 project_count_disabled = 3;
  uint32 site_count_online = 4;
  uint32 site_count_offline = 5;
  uint32 site_count_pending = 6;
  uint32 service_count = 7;
  uint32 supervisor_running_count = 8;
  google.protobuf.Timestamp now = 9;
  // Stale: online sites where last_seen > heartbeat_seconds × 2 ago.
  // Surfaces "about to be flipped offline" before the sweeper runs.
  repeated SiteHealthSummary stale_sites = 10;
}

message SiteHealthSummary {
  string name = 1;
  google.protobuf.Timestamp last_seen_at = 2;
  string status = 3;
  string hostname = 4;
}
```

Auth: admin-only (existing `IsAdmin` check via the operator interceptor).

### `quicktun status` CLI

New CLI subcommand `quicktun status` that calls `AdminService.GetSystemStatus` and renders a small dashboard:

```
$ quicktun status
quicktun control plane @ control.example.com:9090

operators:        3 (admins: 1)
projects:         5 active, 1 disabled
sites:           12 online, 3 offline, 1 pending
services:        37
supervisors:      5 (one per active project)

stale sites (no heartbeat in last 30s):
  projects/clinic-net/sites/bastion-3   last_seen=12s ago  hostname=bastion-3.lan
```

`--json` flag for machine output. Admin-only.

### macOS launchd

`com.tulip.quicktun-agent.plist` runs as a system-wide LaunchDaemon under `/Library/LaunchDaemons/`. KeepAlive=true gives Restart-on-crash. RunAtLoad=true autostarts at boot. Logs to `/var/log/quicktun-agent/agent.log`.

`install-agent.sh` detects the OS (`uname -s`):
- `Linux` → existing systemd path.
- `Darwin` → install plist into `/Library/LaunchDaemons/`, `launchctl load -w` to enable + start.

System user creation differs:
- Linux: `useradd --system`
- macOS: `dscl . -create /Users/_quicktun-agent` (or run as `root` for Phase 1 simplicity — easier and the agent's filesystem footprint is small).

For Phase 1 macOS simplicity: **run as `root`** under launchd. Document the trade-off; Phase 2 can add proper unprivileged user via `dscl`.

---

## File Structure

### New
```
internal/sweeper/
├── sweeper.go              Sweeper{} with Start/Stop; flips stale sites
├── sweeper_test.go

internal/health/
├── health.go               http.Handler /healthz + Checker abstraction
├── health_test.go

internal/grpcsvc/
├── admin_service.go        AdminService server impl (GetSystemStatus)
├── admin_service_test.go

cmd/quicktun/
├── cmd_status.go           `quicktun status` (+ tests)

api/quicktun/v1/
├── admin.proto             AdminService + GetSystemStatusRequest/Response

deploy/launchd/
└── com.tulip.quicktun-agent.plist

scripts/
└── smoke-monitor.sh        Optional new smoke OR extend an existing one.
                            Tentative; may extend smoke-cli.sh instead.
```

### Modified
```
internal/config/config.go        BackendConfig.SweeperInterval + SiteOfflineAfter
internal/authproxy/config.go     HealthListenAddr (optional)
internal/agent/config.go         HealthListenAddr (optional)

internal/server/server.go        Start sweeper goroutine; register /healthz on HTTP mux; register AdminService

internal/authproxy/server.go     OR cmd/quicktun-authproxy/cmd_run.go: spin up health endpoint listener
internal/agent/runtime.go        Spin up health endpoint if configured

internal/dao/site.go             MarkStaleOffline(ctx, threshold time.Time) (int64, error)

cmd/quicktun-server/cmd_serve.go Pass new config knobs

deploy/install-agent.sh          OS detection branch
deploy/etc/agent.yaml.example    health_listen_addr commented hint
deploy/etc/server.yaml.example   sweeper config
deploy/etc/authproxy.yaml.example health_listen_addr

cmd/quicktun/main.go             register newStatusCmd
```

---

## Task 0: Site liveness sweeper

### Step 1: Add config

`internal/config/config.go::BackendConfig`:
```go
type BackendConfig struct {
    // ... existing fields ...
    SweeperInterval   time.Duration `mapstructure:"sweeper_interval"`
    SiteOfflineAfter  time.Duration `mapstructure:"site_offline_after"`
}
```

In setDefaults:
```go
v.SetDefault("backend.sweeper_interval", "30s")
v.SetDefault("backend.site_offline_after", "90s")
```

Update `TestBackendDefaults` to assert both.

### Step 2: DAO method

`internal/dao/site.go`:
```go
// MarkStaleOffline updates every site whose status is online and last_seen_at
// is before threshold to status=offline. Returns the count of rows updated.
// Skipping rows with status=pending preserves "never seen" sites for the
// human eye.
func (d *SiteDAO) MarkStaleOffline(ctx context.Context, threshold time.Time) (int64, error) {
    res := d.db.WithContext(ctx).Model(&model.Site{}).
        Where("status = ? AND last_seen_at IS NOT NULL AND last_seen_at < ?",
            model.SiteStatusOnline, threshold).
        Update("status", model.SiteStatusOffline)
    return res.RowsAffected, res.Error
}
```

Add a unit test that seeds 3 sites (one fresh, one stale, one pending) and asserts only the stale one flips.

### Step 3: Sweeper package

`internal/sweeper/sweeper.go`:
```go
// Package sweeper periodically flips online sites whose last_seen_at has
// gone stale to status=offline. Without this, sites that crash hard without
// graceful shutdown stay online forever.
package sweeper

import (
    "context"
    "time"

    "go.uber.org/zap"

    "github.com/tulip/quicktun/internal/dao"
)

type Config struct {
    Interval     time.Duration
    OfflineAfter time.Duration
}

type Sweeper struct {
    sites *dao.SiteDAO
    cfg   Config
    lg    *zap.Logger
}

func New(sites *dao.SiteDAO, cfg Config, lg *zap.Logger) *Sweeper {
    if lg == nil { lg = zap.NewNop() }
    return &Sweeper{sites: sites, cfg: cfg, lg: lg}
}

// Run blocks until ctx is cancelled. It runs Tick() every Interval.
// If Interval <= 0 or OfflineAfter <= 0, Run returns immediately (disabled).
func (s *Sweeper) Run(ctx context.Context) {
    if s.cfg.Interval <= 0 || s.cfg.OfflineAfter <= 0 {
        s.lg.Info("sweeper disabled (interval or offline_after <= 0)")
        return
    }
    s.lg.Info("sweeper starting",
        zap.Duration("interval", s.cfg.Interval),
        zap.Duration("offline_after", s.cfg.OfflineAfter))

    t := time.NewTicker(s.cfg.Interval)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            if err := s.Tick(ctx); err != nil {
                s.lg.Warn("sweeper tick failed", zap.Error(err))
            }
        }
    }
}

// Tick performs a single sweep. Exposed for tests.
func (s *Sweeper) Tick(ctx context.Context) error {
    threshold := time.Now().UTC().Add(-s.cfg.OfflineAfter)
    n, err := s.sites.MarkStaleOffline(ctx, threshold)
    if err != nil { return err }
    if n > 0 {
        s.lg.Info("sweeper marked sites offline", zap.Int64("count", n))
    }
    return nil
}
```

Tests:
- `TestSweeperTickFlipsStale`
- `TestSweeperRunDisabledWhenIntervalZero`
- `TestSweeperRunStopsOnContextCancel`

### Step 4: Wire into server

In `internal/server/server.go::Run`, after `s.relay.Start(ctx)` (or alongside it), spawn the sweeper:

```go
sw := sweeper.New(dao.NewSiteDAO(s.cfg.DB), sweeper.Config{
    Interval:     s.cfg.SweeperInterval,
    OfflineAfter: s.cfg.SiteOfflineAfter,
}, s.cfg.Logger)
go sw.Run(ctx)
```

Add `SweeperInterval`, `SiteOfflineAfter` to `server.Config`. Pass through `cmd_serve.go`.

### Step 5: Verify + commit

```bash
go test -count=1 ./internal/sweeper/... ./internal/dao/... ./internal/config/...
go test ./...
go vet ./...
make build
./scripts/smoke.sh   # confirm no regression
git add internal/sweeper/ internal/dao/site.go internal/dao/site_test.go internal/config/ internal/server/server.go cmd/quicktun-server/cmd_serve.go
git commit -m "feat(sweeper): periodically flip stale sites to offline"
```

---

## Task 1: `/healthz` endpoints

### Step 1: Shared `internal/health/`

```go
// Package health provides a tiny http.Handler for /healthz.
package health

import (
    "encoding/json"
    "net/http"
)

// Checker reports a service's health. Reasons is empty when healthy.
type Checker func() (ok bool, reasons []string)

// Handler returns an http.Handler that runs check on every request and
// renders {status, reasons} JSON. 200 if ok else 503.
func Handler(check Checker) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        ok, reasons := check()
        body := map[string]any{"status": "ok"}
        status := http.StatusOK
        if !ok {
            body["status"] = "degraded"
            body["reasons"] = reasons
            status = http.StatusServiceUnavailable
        }
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        _ = json.NewEncoder(w).Encode(body)
    })
}
```

Tests: 2 cases (ok and degraded).

### Step 2: Server `/healthz`

In `internal/server/server.go`'s gateway HTTP setup, register `/healthz` BEFORE the catch-all gateway mux. Checker:

```go
healthCheck := func() (bool, []string) {
    sql, err := s.cfg.DB.DB()
    if err != nil { return false, []string{"db: " + err.Error()} }
    if err := sql.Ping(); err != nil { return false, []string{"db ping: " + err.Error()} }
    return true, nil
}
mux.Handle("/healthz", health.Handler(healthCheck))
```

### Step 3: Auth-proxy `/healthz`

Add `HealthListenAddr string \`yaml:"health_listen_addr"\`` to `authproxy.Config`. Default `127.0.0.1:8444`.

In `cmd/quicktun-authproxy/cmd_run.go`, after the auth-proxy server starts, also spin up:

```go
if cfg.HealthListenAddr != "" {
    go func() {
        check := func() (bool, []string) {
            sql, err := db.DB()
            if err != nil { return false, []string{err.Error()} }
            return sql.PingContext(ctx) == nil, nil
        }
        mux := http.NewServeMux()
        mux.Handle("/healthz", health.Handler(check))
        srv := &http.Server{Addr: cfg.HealthListenAddr, Handler: mux}
        go func() { <-ctx.Done(); srv.Shutdown(context.Background()) }()
        _ = srv.ListenAndServe()
    }()
}
```

### Step 4: Agent `/healthz`

Same shape, in `internal/agent/runtime.go`. Default empty (disabled). Health check:

```go
check := func() (bool, []string) {
    var reasons []string
    r.mu.Lock()
    lastBoot := r.lastBootstrapAt
    sup := r.supervisor
    r.mu.Unlock()

    if lastBoot.IsZero() {
        reasons = append(reasons, "agent has never bootstrapped")
    } else if time.Since(lastBoot) > time.Duration(r.heartbeatSec)*2*time.Second {
        reasons = append(reasons, "stale bootstrap")
    }
    if sup != nil && sup.Pid() == 0 && r.cfg.RatholeBinary != "" {
        reasons = append(reasons, "supervisor not running")
    }
    return len(reasons) == 0, reasons
}
```

Add `lastBootstrapAt time.Time` field on Runtime. Update inside applyBootstrap.

### Step 5: Tests

- Unit tests for `health.Handler`.
- Integration: spin up server, hit `/healthz`, expect 200 + `"status":"ok"`.
- For agent: render-only mode + start runtime; hit healthz; expect ok after first bootstrap.

### Step 6: Verify + commit

```bash
go test -count=1 ./internal/health/... ./...
go vet ./...
make build
./scripts/smoke.sh
git add internal/health/ internal/server/server.go internal/authproxy/ internal/agent/ cmd/quicktun-authproxy/cmd_run.go cmd/quicktun-server/
git commit -m "feat(health): /healthz endpoints on server, authproxy, agent"
```

---

## Task 2: `AdminService.GetSystemStatus` + `quicktun status` CLI

### Step 1: `api/quicktun/v1/admin.proto`

```proto
syntax = "proto3";
package quicktun.v1;

import "google/api/annotations.proto";
import "google/protobuf/timestamp.proto";

option go_package = "github.com/tulip/quicktun/gen/go/quicktun/v1;quicktunv1";

service AdminService {
  rpc GetSystemStatus(GetSystemStatusRequest) returns (GetSystemStatusResponse) {
    option (google.api.http) = { get: "/v1/admin:status" };
  }
}

message GetSystemStatusRequest {}

message GetSystemStatusResponse {
  uint32 operator_count = 1;
  uint32 project_count_active = 2;
  uint32 project_count_disabled = 3;
  uint32 site_count_online = 4;
  uint32 site_count_offline = 5;
  uint32 site_count_pending = 6;
  uint32 service_count = 7;
  uint32 supervisor_running_count = 8;
  google.protobuf.Timestamp now = 9;
  repeated SiteHealthSummary stale_sites = 10;
}

message SiteHealthSummary {
  string name = 1;
  google.protobuf.Timestamp last_seen_at = 2;
  string status = 3;
  string hostname = 4;
}
```

Run `make proto-gen`.

### Step 2: `internal/grpcsvc/admin_service.go`

Implementation: aggregates COUNT(*) queries per status / table, plus `relay.Manager.SupervisorCount()`.

```go
type AdminService struct {
    quicktunv1.UnimplementedAdminServiceServer
    db    *gorm.DB
    relay RelayManager  // existing interface; reuse SupervisorCount
}

func NewAdminService(db *gorm.DB, relay RelayManager) *AdminService {
    return &AdminService{db: db, relay: relay}
}

func (a *AdminService) GetSystemStatus(ctx context.Context, _ *quicktunv1.GetSystemStatusRequest) (*quicktunv1.GetSystemStatusResponse, error) {
    op, ok := auth.OperatorFromContext(ctx)
    if !ok || !op.IsAdmin {
        return nil, status.Error(codes.PermissionDenied, "admin only")
    }

    var resp quicktunv1.GetSystemStatusResponse
    var n int64

    if err := a.db.WithContext(ctx).Model(&model.Operator{}).Count(&n).Error; err != nil { return nil, status.Errorf(codes.Internal, "internal error") }
    resp.OperatorCount = uint32(n)

    // ... same shape for project_active, project_disabled, sites by status, services
    // ... supervisor_running_count = uint32(a.relay.SupervisorCount())

    // stale_sites: online with last_seen older than threshold
    threshold := time.Now().UTC().Add(-30 * time.Second)
    var stale []model.Site
    if err := a.db.WithContext(ctx).
        Where("status = ? AND last_seen_at < ?", model.SiteStatusOnline, threshold).
        Find(&stale).Error; err != nil { return nil, status.Errorf(codes.Internal, "internal error") }
    for _, s := range stale {
        resp.StaleSites = append(resp.StaleSites, &quicktunv1.SiteHealthSummary{
            Name:       resource.FormatSiteName(/* project slug */, s.Name),
            LastSeenAt: timestamppb.New(*s.LastSeenAt),
            Status:     string(s.Status),
            Hostname:   s.Hostname,
        })
    }
    resp.Now = timestamppb.Now()
    return &resp, nil
}
```

Confirm:
- `auth.OperatorFromContext` (exact name; read interceptor.go)
- `RelayManager` interface (already in grpcsvc)
- `resource.FormatSiteName` to build the canonical name (need project slug — preload or look up)

For stale_sites needing project slug: do a JOIN or prefetch projects. Simplest: query `sites` with a join to projects:

```go
type staleRow struct {
    SiteName    string
    ProjectSlug string
    LastSeenAt  *time.Time
    Status      string
    Hostname    string
}
var rows []staleRow
err := a.db.WithContext(ctx).
    Table("sites").
    Select("sites.name as site_name, projects.slug as project_slug, sites.last_seen_at, sites.status, sites.hostname").
    Joins("JOIN projects ON projects.id = sites.project_id").
    Where("sites.status = ? AND sites.last_seen_at < ?", model.SiteStatusOnline, threshold).
    Find(&rows).Error
```

Tests:
- `TestAdminGetSystemStatusReturnsCounts`
- `TestAdminGetSystemStatusRequiresAdmin`
- `TestAdminGetSystemStatusReportsStaleSites`

### Step 3: Wire into `server.New`

```go
adminSvc := grpcsvc.NewAdminService(cfg.DB, mgr)
quicktunv1.RegisterAdminServiceServer(gs, adminSvc)
```

Update operator interceptor's allowlist if `/quicktun.v1.AdminService/*` needs auth (probably yes — admin only).

### Step 4: `cmd/quicktun/cmd_status.go`

```go
func newStatusCmd() *cobra.Command {
    var asJSON bool
    cmd := &cobra.Command{
        Use:   "status",
        Short: "Show control plane status (admin only).",
        RunE: func(cmd *cobra.Command, args []string) error {
            _, conn, err := loadAndDial(cmd)
            if err != nil { return err }
            defer conn.Close()

            client := quicktunv1.NewAdminServiceClient(conn)
            ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
            defer cancel()
            resp, err := client.GetSystemStatus(ctx, &quicktunv1.GetSystemStatusRequest{})
            if err != nil { return err }

            if asJSON { return printJSON(resp) }

            // Human renderer
            fmt.Printf("operators:        %d\n", resp.GetOperatorCount())
            fmt.Printf("projects:         %d active, %d disabled\n",
                resp.GetProjectCountActive(), resp.GetProjectCountDisabled())
            fmt.Printf("sites:            %d online, %d offline, %d pending\n",
                resp.GetSiteCountOnline(), resp.GetSiteCountOffline(), resp.GetSiteCountPending())
            fmt.Printf("services:         %d\n", resp.GetServiceCount())
            fmt.Printf("supervisors:      %d\n", resp.GetSupervisorRunningCount())
            if len(resp.GetStaleSites()) > 0 {
                fmt.Println("\nstale sites (no recent heartbeat):")
                for _, s := range resp.GetStaleSites() {
                    fmt.Printf("  %s  last_seen=%s  hostname=%s\n",
                        s.GetName(), s.GetLastSeenAt().AsTime().Format(time.RFC3339), s.GetHostname())
                }
            }
            return nil
        },
    }
    cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
    return cmd
}
```

Register in `main.go`: `cmd.AddCommand(newStatusCmd())`.

### Step 5: Verify + commit

```bash
make proto-gen
go test ./...
go vet ./...
make build
./scripts/smoke-cli.sh
git add api/ gen/ internal/grpcsvc/admin_service.go internal/grpcsvc/admin_service_test.go internal/server/server.go cmd/quicktun/cmd_status.go cmd/quicktun/main.go
git commit -m "feat(admin): GetSystemStatus RPC + quicktun status CLI"
```

---

## Task 3: macOS launchd

### Step 1: Plist file

`deploy/launchd/com.tulip.quicktun-agent.plist`:
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.tulip.quicktun-agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/quicktun-agent</string>
        <string>run</string>
        <string>--config</string>
        <string>/etc/quicktun/agent.yaml</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>StandardOutPath</key>
    <string>/var/log/quicktun-agent/agent.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/quicktun-agent/agent.log</string>
    <key>ThrottleInterval</key>
    <integer>10</integer>
</dict>
</plist>
```

### Step 2: install-agent.sh OS detection

Refactor:
```bash
OS=$(uname -s)
case "$OS" in
    Linux) install_linux ;;
    Darwin) install_darwin ;;
    *) fail "unsupported OS: $OS" ;;
esac
```

`install_linux()` is the existing flow (factor out into a function).

`install_darwin()`:
```bash
install_darwin() {
    log "installing for macOS (LaunchDaemon)"
    # Phase 1 simplification: no separate user; daemon runs as root.
    install -d -m 0755 /etc/quicktun
    install -d -m 0755 /var/lib/quicktun-agent
    install -d -m 0755 /var/log/quicktun-agent

    install -m 0755 "$AGENT_BIN" /usr/local/bin/quicktun-agent
    install -m 0644 "$SCRIPT_DIR/launchd/com.tulip.quicktun-agent.plist" /Library/LaunchDaemons/

    cat > /etc/quicktun/agent.yaml <<EOF
control_endpoint: $CONTROL_ENDPOINT
token: $TOKEN
state_dir: /var/lib/quicktun-agent
rathole_binary: /usr/local/bin/rathole
rathole_args: ["--client"]
tls_insecure: $TLS_INSECURE
# auth_proxy_endpoint is fetched from BootstrapResponse; uncomment to override:
# auth_proxy_endpoint: $AUTH_PROXY
EOF
    chmod 0600 /etc/quicktun/agent.yaml

    launchctl unload /Library/LaunchDaemons/com.tulip.quicktun-agent.plist 2>/dev/null || true
    launchctl load -w /Library/LaunchDaemons/com.tulip.quicktun-agent.plist
    log "Done. View logs: tail -f /var/log/quicktun-agent/agent.log"
    log "  launchctl list | grep quicktun-agent"
    log "  sudo launchctl unload /Library/LaunchDaemons/com.tulip.quicktun-agent.plist  (to stop)"
}
```

`require_linux()` becomes `require_supported_os()` (Linux or Darwin OK). Update `lib.sh`.

### Step 3: Update deploy/README.md

Add a "macOS agent installation" section that mirrors the Linux flow but uses `launchctl` commands.

### Step 4: Verify

```bash
bash -n deploy/install-agent.sh
shellcheck deploy/install-agent.sh deploy/lib.sh || true
./scripts/lint-deploy.sh
```

You can't actually run install-agent.sh on this dev macOS without root + clobbering /usr/local/bin. Manual test:
```bash
# Dry-run check
bash -x deploy/install-agent.sh --token TEST --control-endpoint x:9090 --auth-proxy x:443 --tls-insecure 2>&1 | head -20
```

(Expect it to bail at `require_root` early; that's fine.)

### Step 5: Commit

```bash
git add deploy/launchd/ deploy/install-agent.sh deploy/lib.sh deploy/README.md
git commit -m "feat(deploy): macOS LaunchDaemon support for agent"
```

---

## Task 4: smoke extension

Add ONE new section to `scripts/smoke-cli.sh` (or create `scripts/smoke-monitor.sh`):

```bash
# After the existing forward test passes:

# Verify /healthz on the server's HTTP gateway.
HEALTH=$(curl -sS -o /dev/null -w "%{http_code}" "http://127.0.0.1:$HTTP_PORT/healthz")
[[ "$HEALTH" == "200" ]] || { echo "FAIL: server /healthz got $HEALTH"; exit 1; }
echo "server /healthz: PASS"

# Verify quicktun status reports the seeded counts.
STATUS_JSON=$("$ROOT/bin/quicktun" status --json --config "$CRED_FILE")
ACTIVE=$(echo "$STATUS_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['projectCountActive'])")
[[ "$ACTIVE" -ge 1 ]] || { echo "FAIL: status active count $ACTIVE"; exit 1; }
echo "quicktun status: PASS"

# Verify the sweeper marks a stale site offline.
# (Set last_seen_at to 5 min ago via sqlite; wait for next sweeper tick; verify.)
sqlite3 "$WORKDIR/quicktun.db" "UPDATE sites SET status='online', last_seen_at=datetime('now', '-5 minutes') WHERE name='bastion';"
sleep 35  # sweeper interval default 30s
SITE_STATUS=$(sqlite3 "$WORKDIR/quicktun.db" "SELECT status FROM sites WHERE name='bastion';")
[[ "$SITE_STATUS" == "offline" ]] || { echo "FAIL: sweeper didn't flip site (still $SITE_STATUS)"; exit 1; }
echo "sweeper: PASS"
```

The 35s sleep makes smoke slower. To keep smoke under a minute, lower the sweeper interval via server config to e.g. 5s in the test:
```yaml
backend:
  sweeper_interval: 5s
  site_offline_after: 1s
```

Then `sleep 8` is enough.

### Verify + commit

```bash
./scripts/smoke-cli.sh
git add scripts/smoke-cli.sh
git commit -m "test(smoke): cover /healthz, status RPC, sweeper"
```

---

## Task 5: Final verification

```bash
go test -count=1 -timeout 240s ./...
go test -race -timeout 360s ./...
go vet ./...
make proto-lint
make check-migrations
make build
./scripts/smoke.sh
./scripts/smoke-agent.sh
./scripts/smoke-authproxy.sh
./scripts/smoke-cli.sh
./scripts/lint-deploy.sh
GOOS=linux GOARCH=amd64 go vet ./...
GOOS=linux GOARCH=amd64 go build ./...
```

All green.

---

## Self-review

| Plan-11 requirement | Implemented in |
|---|---|
| Site offline sweeper | Task 0 |
| /healthz on server | Task 1 |
| /healthz on auth-proxy | Task 1 |
| /healthz on agent | Task 1 |
| AdminService.GetSystemStatus | Task 2 |
| quicktun status CLI | Task 2 |
| macOS LaunchDaemon for agent | Task 3 |
| install-agent.sh OS detection | Task 3 |
| Smoke covers all of the above | Task 4 |

**Deferred to Plan 12:**
- Prometheus `/metrics` (request counts, latencies, supervisor restarts).
- External webhook on supervisor crash loop (Slack/Discord).
- macOS server / auth-proxy LaunchDaemon (low value — control plane usually Linux).
- macOS unprivileged user via `dscl` (Phase 1 runs as root).
- Watchdog timers on agent that auto-restart the binary if it gets stuck.
