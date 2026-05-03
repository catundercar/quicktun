# quicktun Service CRUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ServiceService` (List/Get/Create/Update/Delete) — the third level of the resource hierarchy (`projects/{p}/sites/{s}/services/{svc}`). Includes per-service relay port allocation from each project's `relay_port_range`. Bundle Plan 4 final-review cleanup as Task 0.

**Architecture:** Services are nested under sites, which are nested under projects. Three-segment resource names. ServiceService delegates to a `resolveSite` helper (extracted from SiteService) so auth ordering and project-scope filtering remain consistent. Each Service is assigned a unique `relay_port` within its project's port range at create time; allocation scans for the lowest-free port in O(n) (n = ports already in use, capped at the range size).

**Tech Stack:** Existing — Go, GORM, gRPC, grpc-gateway, buf, audit/resource/auth packages from Plans 1-4.

---

## File Structure

### New files
```
internal/dao/
├── service.go                    ServiceDAO with relay-port allocator
├── service_test.go

internal/grpcsvc/
├── service_service.go            ServiceService impl (5 standard methods)
├── service_service_test.go

cmd/quicktun-server/
├── cmd_admin_service.go          admin service create/list/delete CLI
├── cmd_admin_service_test.go

api/quicktun/v1/
├── service.proto                 NEW
```

### Files modified
```
internal/resource/
├── name.go                       (Task 0: lower minSlugLen to 1; add ParseServiceName)
├── name_test.go                  (Task 0: relax slug tests; add Service tests)

internal/grpcsvc/
├── site_service.go               (Task 0: replace private parseSiteName/parseProjectParent with resource package calls)

internal/dao/
├── site.go                       (Task 0: log last_used_at update errors)

internal/server/
├── server.go                     (Task 0: thread RelayAddr through Config; Task 6: register ServiceService)
├── server_test.go                (Task 6: Service e2e test)

cmd/quicktun-server/
├── cmd_serve.go                  (Task 0: pass cfg.RelayAddr into server.Config)
├── cmd_admin.go                  (Task 6: register adminServiceCmd)

internal/config/
├── config.go                     (Task 0: add RelayAddr to ControlPlaneConfig)
├── config_test.go                (Task 0: test RelayAddr default)

internal/grpcsvc/
├── site_service.go               (Task 0: read RelayAddr from injected config; install command uses it)

scripts/
├── smoke.sh                      (Task 6: extend with service flow)
```

---

## Task 0: Plan 4 Final-Review Cleanup

The Plan 4 reviewer flagged 2 Important + 4 Minor items. Address upfront.

### Step 1: Lower `minSlugLen` to 1; ParseServiceName + tests

Edit `/Users/tulip/project/repos/quicktun/internal/resource/name.go`. Change:

```go
// Before:
const (
    minSlugLen = 3
    maxSlugLen = 64
)

// After:
const (
    minSlugLen = 1
    maxSlugLen = 64
)
```

This unifies the test-fixtures-vs-production-validation split. Single-character slugs are valid (e.g., `a`); the 3-char minimum was arbitrary.

Append to the same file:

```go

const collectionServices = "services"

// ServiceName carries the parsed (project_slug, site_slug, service_slug) tuple.
type ServiceName struct {
    Project string
    Site    string
    Service string
}

// FormatServiceName returns "projects/{p}/sites/{s}/services/{svc}".
func FormatServiceName(projectSlug, siteSlug, serviceSlug string) string {
    return collectionProjects + "/" + projectSlug + "/" +
        collectionSites + "/" + siteSlug + "/" +
        collectionServices + "/" + serviceSlug
}

// ParseServiceName parses "projects/{p}/sites/{s}/services/{svc}".
func ParseServiceName(name string) (ServiceName, error) {
    parts := strings.Split(name, "/")
    if len(parts) != 6 ||
        parts[0] != collectionProjects ||
        parts[2] != collectionSites ||
        parts[4] != collectionServices {
        return ServiceName{}, errors.New(`resource: service name must be "projects/{p}/sites/{s}/services/{svc}"`)
    }
    if err := ValidateSlug(parts[1]); err != nil {
        return ServiceName{}, err
    }
    if err := ValidateSlug(parts[3]); err != nil {
        return ServiceName{}, err
    }
    if err := ValidateSlug(parts[5]); err != nil {
        return ServiceName{}, err
    }
    return ServiceName{Project: parts[1], Site: parts[3], Service: parts[5]}, nil
}

// FormatSiteParent returns "projects/{p}/sites/{s}" used as a List parent.
func FormatSiteParent(projectSlug, siteSlug string) string {
    return collectionProjects + "/" + projectSlug + "/" + collectionSites + "/" + siteSlug
}

// ParseSiteParent parses "projects/{p}/sites/{s}" used as a List request parent.
func ParseSiteParent(parent string) (SiteName, error) {
    return ParseSiteName(parent)
}
```

Update `/Users/tulip/project/repos/quicktun/internal/resource/name_test.go`. Update `TestValidateSlug` to reflect minSlugLen=1:

```go
// Before: invalid := []string{"", "ab", ...}
// After:
invalid := []string{"", "Abc", "abc_def", "abc def", "abc-", "-abc", "a--b", strings.Repeat("a", 65)}
// Note: "ab" is now valid (2 chars). "" (0 chars) still invalid.
```

Append:

```go

func TestFormatServiceName(t *testing.T) {
    require.Equal(t, "projects/p/sites/s/services/ssh",
        resource.FormatServiceName("p", "s", "ssh"))
}

func TestParseServiceName(t *testing.T) {
    n, err := resource.ParseServiceName("projects/clinic/sites/bastion-1/services/ssh")
    require.NoError(t, err)
    require.Equal(t, "clinic", n.Project)
    require.Equal(t, "bastion-1", n.Site)
    require.Equal(t, "ssh", n.Service)
}

func TestParseServiceNameRejects(t *testing.T) {
    cases := []string{
        "",
        "projects/p/sites/s",
        "projects/p/sites/s/services/",
        "projects/p/sites/s/services/x/extra",
        "sites/p/sites/s/services/x",
        "projects/Bad/sites/s/services/x",
        "projects/p/sites/Bad/services/x",
        "projects/p/sites/s/services/Bad",
    }
    for _, name := range cases {
        _, err := resource.ParseServiceName(name)
        require.Error(t, err, "expected error for %q", name)
    }
}
```

Run: `go test -count=3 ./internal/resource/...` — expected: 10 tests pass (4 prior + 3 service + 3 from Task slug-len adjustments).

### Step 2: Replace private parsers in site_service.go with resource package

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service.go`. Find the private `parseSiteName` and `parseProjectParent` functions (the duplicates flagged by review). Delete them.

Update the callers in the file:
- `parseSiteName(req.GetName())` → `resource.ParseSiteName(req.GetName())`
- `parseProjectParent(parent)` → `resource.ParseProjectParent(parent)`

If signatures match (they should — both return SiteName / string + error), no further changes needed. The `resource` package is already imported.

Run: `go test -count=3 ./internal/grpcsvc/...` — expected: all 48 tests still pass.

### Step 3: Log last_used_at update errors

Edit `/Users/tulip/project/repos/quicktun/internal/dao/site.go`. Find:

```go
now := time.Now().UTC()
d.db.WithContext(ctx).Model(&model.SiteAgentToken{}).
    Where("id = ?", rec.ID).Update("last_used_at", &now)
return rec.SiteID, nil
```

Replace with:

```go
now := time.Now().UTC()
if err := d.db.WithContext(ctx).Model(&model.SiteAgentToken{}).
    Where("id = ?", rec.ID).Update("last_used_at", &now).Error; err != nil {
    // Best-effort stat field; log to stderr but don't fail the validate.
    fmt.Fprintf(os.Stderr, "dao: site token last_used_at update: %v\n", err)
}
return rec.SiteID, nil
```

Add `os` to imports.

Run: `go test ./internal/dao/...` — expected: all 25 tests pass.

### Step 4: Thread RelayAddr through config + server + site service

Edit `/Users/tulip/project/repos/quicktun/internal/config/config.go`. Add field to `ControlPlaneConfig`:

```go
type ControlPlaneConfig struct {
    GRPCListen string `mapstructure:"grpc_listen"`
    HTTPListen string `mapstructure:"http_listen"`
    // RelayAddr is the public address operators paste into agent install
    // commands; e.g., "relay.example.com:443".
    RelayAddr string `mapstructure:"relay_addr"`
}
```

In `setDefaults`, add:

```go
v.SetDefault("control_plane.relay_addr", "relay.example.com:443")
```

Edit `/Users/tulip/project/repos/quicktun/internal/config/config_test.go`. Append:

```go
func TestRelayAddrDefault(t *testing.T) {
    yaml := `
database:
  dsn: ":memory:"
`
    path := filepath.Join(t.TempDir(), "server.yaml")
    require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
    cfg, err := config.Load(path)
    require.NoError(t, err)
    require.Equal(t, "relay.example.com:443", cfg.ControlPlane.RelayAddr)
}
```

Edit `/Users/tulip/project/repos/quicktun/internal/server/server.go`. Add to `Config`:

```go
type Config struct {
    DB         *gorm.DB
    Logger     *zap.Logger
    GRPCListen string
    HTTPListen string
    RelayAddr  string  // ★ NEW
    SessionTTL time.Duration
}
```

In `New`, when constructing `SiteService`, pass the relay addr — but SiteService currently doesn't take one. Update the SiteService constructor in Step 5.

### Step 5: Make SiteService accept RelayAddr

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service.go`. Change the constructor:

```go
// Before:
func NewSiteService(projects *dao.ProjectDAO, sites *dao.SiteDAO, tokens *dao.SiteAgentTokenDAO, audit *audit.Writer) *SiteService {
    return &SiteService{projects: projects, sites: sites, tokens: tokens, audit: audit}
}

// After:
func NewSiteService(projects *dao.ProjectDAO, sites *dao.SiteDAO, tokens *dao.SiteAgentTokenDAO, audit *audit.Writer, relayAddr string) *SiteService {
    if relayAddr == "" {
        relayAddr = "relay.example.com:443"
    }
    return &SiteService{
        projects: projects, sites: sites, tokens: tokens, audit: audit,
        relayAddr: relayAddr,
    }
}
```

Add the field to the struct:

```go
type SiteService struct {
    quicktunv1.UnimplementedSiteServiceServer
    projects  *dao.ProjectDAO
    sites     *dao.SiteDAO
    tokens    *dao.SiteAgentTokenDAO
    audit     *audit.Writer
    relayAddr string
}
```

Update `GetSiteInstallCommand` to use `s.relayAddr` instead of the hardcoded value. Find the `cmd = "curl ..." + raw + ... "relay.example.com:443" ...` lines and use `s.relayAddr`:

```go
switch osKind {
case "linux":
    cmd = "curl -fsSL https://" + s.relayAddr + "/install/agent.sh | " +
        "QT_TOKEN=" + raw + " QT_ENDPOINT=" + s.relayAddr + " bash"
case "windows":
    cmd = `$env:QT_TOKEN="` + raw + `"; ` +
        `$env:QT_ENDPOINT="` + s.relayAddr + `"; ` +
        `iwr -useb https://` + s.relayAddr + `/install/agent.ps1 | iex`
}
```

Update `internal/server/server.go` `New()`:

```go
siteSvc := grpcsvc.NewSiteService(
    dao.NewProjectDAO(cfg.DB),
    dao.NewSiteDAO(cfg.DB),
    dao.NewSiteAgentTokenDAO(cfg.DB),
    auditWriter,
    cfg.RelayAddr,  // ★ NEW
)
```

Update `cmd/quicktun-server/cmd_serve.go` to pass the value:

```go
srv, err := server.New(server.Config{
    DB:         db,
    Logger:     lg,
    GRPCListen: cfg.ControlPlane.GRPCListen,
    HTTPListen: cfg.ControlPlane.HTTPListen,
    RelayAddr:  cfg.ControlPlane.RelayAddr,  // ★ NEW
    SessionTTL: cfg.Session.DefaultTTL,
})
```

Update test callers of `NewSiteService` in `internal/grpcsvc/site_service_test.go` (if any direct construction exists, pass `""` so the default kicks in). Find `newSiteService(t, db)` helper and update:

```go
func newSiteService(t *testing.T, db *gorm.DB) *grpcsvc.SiteService {
    return grpcsvc.NewSiteService(
        dao.NewProjectDAO(db),
        dao.NewSiteDAO(db),
        dao.NewSiteAgentTokenDAO(db),
        audit.NewWriter(db),
        "test-relay.example.com:443",  // explicit test value
    )
}
```

Update the install-command tests to assert against `test-relay.example.com:443` instead of `relay.example.com:443`.

### Step 6: Add tests for previously-uncovered UNSPECIFIED + bad-os paths

Append to `/Users/tulip/project/repos/quicktun/internal/grpcsvc/site_service_test.go`:

```go
func TestUpdateSiteRejectsUnspecifiedMode(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "u-mode")
    svc := newSiteService(t, db)

    _, err := svc.UpdateSite(adminCtx(t, db), &quicktunv1.UpdateSiteRequest{
        Site: &quicktunv1.Site{
            Name: "projects/p1/sites/u-mode",
            Mode: quicktunv1.SiteMode_SITE_MODE_UNSPECIFIED,
        },
        UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"mode"}},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetSiteInstallCommandRejectsBadOS(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "bad-os-target")
    svc := newSiteService(t, db)

    _, err := svc.GetSiteInstallCommand(adminCtx(t, db), &quicktunv1.GetSiteInstallCommandRequest{
        Name: "projects/p1/sites/bad-os-target",
        Os:   "freebsd",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.InvalidArgument, st.Code())
}
```

### Step 7: Run smoke + commit

```bash
cd /Users/tulip/project/repos/quicktun
go test ./...
go test -race -timeout 120s ./...
go vet ./...
make build

git add internal/resource/ internal/grpcsvc/site_service.go internal/grpcsvc/site_service_test.go internal/dao/site.go internal/config/ internal/server/server.go cmd/quicktun-server/cmd_serve.go
git commit -m "chore: address Plan 4 final-review cleanup"
```

Expected: all green. Single new commit.

---

## Task 1: service.proto + Codegen

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/api/quicktun/v1/service.proto`

### Step 1: Create `api/quicktun/v1/service.proto`

```proto
// ServiceService — CRUD for services (TCP forwarding rules under a site).
syntax = "proto3";

package quicktun.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/timestamp.proto";

import "quicktun/v1/common.proto";

option go_package = "github.com/tulip/quicktun/gen/go/quicktun/v1;quicktunv1";

service ServiceService {
  rpc ListServices(ListServicesRequest) returns (ListServicesResponse) {
    option (google.api.http) = {
      get: "/v1/{parent=projects/*/sites/*}/services"
    };
  }

  rpc GetService(GetServiceRequest) returns (Service) {
    option (google.api.http) = {
      get: "/v1/{name=projects/*/sites/*/services/*}"
    };
  }

  rpc CreateService(CreateServiceRequest) returns (Service) {
    option (google.api.http) = {
      post: "/v1/{parent=projects/*/sites/*}/services"
      body: "service"
    };
  }

  rpc UpdateService(UpdateServiceRequest) returns (Service) {
    option (google.api.http) = {
      patch: "/v1/{service.name=projects/*/sites/*/services/*}"
      body: "service"
    };
  }

  rpc DeleteService(DeleteServiceRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/v1/{name=projects/*/sites/*/services/*}"
    };
  }
}

enum Proto {
  PROTO_UNSPECIFIED = 0;
  PROTO_TCP         = 1;
  PROTO_UDP         = 2;  // Phase 2
}

message Service {
  // Resource name. Format: projects/{p}/sites/{s}/services/{svc}
  string name = 1;

  string service_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];

  string display_name = 10 [(google.api.field_behavior) = REQUIRED];
  // target_addr is "127.0.0.1" (bastion's own SSH/etc) or a LAN IP the
  // bastion can reach (e.g., "192.168.10.50").
  string target_addr  = 11 [(google.api.field_behavior) = REQUIRED];
  uint32 target_port  = 12 [(google.api.field_behavior) = REQUIRED];
  Proto  proto        = 13;

  // Output only: relay-side port allocated by the control plane.
  uint32 relay_port   = 20 [(google.api.field_behavior) = OUTPUT_ONLY];
}

message ListServicesRequest {
  string parent = 1 [(google.api.field_behavior) = REQUIRED];
  PageRequest page = 2;
}

message ListServicesResponse {
  repeated Service services = 1;
  PageResponse page = 2;
}

message GetServiceRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
}

message CreateServiceRequest {
  string parent = 1 [(google.api.field_behavior) = REQUIRED];
  string service_id = 2 [(google.api.field_behavior) = REQUIRED];
  Service service = 3 [(google.api.field_behavior) = REQUIRED];
}

message UpdateServiceRequest {
  Service service = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2 [(google.api.field_behavior) = REQUIRED];
}

message DeleteServiceRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
}
```

### Step 2: Lint, generate, commit

```bash
cd /Users/tulip/project/repos/quicktun
make proto-lint
make proto-gen
go mod tidy
go build ./gen/...
git add api/quicktun/v1/service.proto go.mod go.sum
git commit -m "feat(proto): add ServiceService"
```

Expected: clean lint, generates `service.pb.go` / `service_grpc.pb.go` / `service.pb.gw.go`.

---

## Task 2: ServiceDAO with Port Allocator

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/dao/service.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/dao/service_test.go`

### Step 1: Write failing test `internal/dao/service_test.go`

```go
package dao_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"

    "github.com/tulip/quicktun/internal/dao"
    "github.com/tulip/quicktun/internal/model"
)

func TestServiceCreateAndFind(t *testing.T) {
    db := openWithModels(t)
    p := mkProject(t, db, "p1")
    s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
        ProjectID: p.ID, Name: "bastion",
    })
    store := dao.NewServiceDAO(db)
    ctx := context.Background()

    relayPort := uint16(20100)
    svc, err := store.Create(ctx, &model.Service{
        SiteID: s.ID, Name: "ssh",
        TargetAddr: "127.0.0.1", TargetPort: 22,
        Proto: model.ProtoTCP, RelayPort: &relayPort,
    })
    require.NoError(t, err)
    require.NotZero(t, svc.ID)

    got, err := store.FindByName(ctx, s.ID, "ssh")
    require.NoError(t, err)
    require.Equal(t, svc.ID, got.ID)
    require.NotNil(t, got.RelayPort)
    require.Equal(t, uint16(20100), *got.RelayPort)
}

func TestServiceListBySite(t *testing.T) {
    db := openWithModels(t)
    p := mkProject(t, db, "p1")
    s1, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b1"})
    s2, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b2"})
    store := dao.NewServiceDAO(db)
    ctx := context.Background()

    _, _ = store.Create(ctx, &model.Service{SiteID: s1.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP})
    _, _ = store.Create(ctx, &model.Service{SiteID: s1.ID, Name: "rdp", TargetAddr: "192.168.10.50", TargetPort: 3389, Proto: model.ProtoTCP})
    _, _ = store.Create(ctx, &model.Service{SiteID: s2.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP})

    got, err := store.ListBySite(ctx, s1.ID, 100, "")
    require.NoError(t, err)
    require.Len(t, got, 2)
}

func TestServiceDelete(t *testing.T) {
    db := openWithModels(t)
    p := mkProject(t, db, "p1")
    s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
    store := dao.NewServiceDAO(db)
    ctx := context.Background()

    svc, _ := store.Create(ctx, &model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP})
    require.NoError(t, store.Delete(ctx, svc.ID))
    _, err := store.FindByName(ctx, s.ID, "ssh")
    require.True(t, dao.IsNotFound(err))
}

func TestAllocateRelayPortAssignsLowestFree(t *testing.T) {
    db := openWithModels(t)
    p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "p", Name: "P", RelayPortRange: "20000-20003",
    })
    s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
    store := dao.NewServiceDAO(db)
    ctx := context.Background()

    // Pre-fill 20000 and 20002 with services.
    rp1 := uint16(20000)
    rp2 := uint16(20002)
    _, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "a", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP, RelayPort: &rp1})
    _, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "b", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP, RelayPort: &rp2})

    port, err := store.AllocateRelayPort(ctx, p)
    require.NoError(t, err)
    require.Equal(t, uint16(20001), port) // lowest free in [20000-20003]
}

func TestAllocateRelayPortExhausted(t *testing.T) {
    db := openWithModels(t)
    p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "p", Name: "P", RelayPortRange: "20000-20001",
    })
    s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
    store := dao.NewServiceDAO(db)
    ctx := context.Background()

    a := uint16(20000)
    b := uint16(20001)
    _, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "x", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP, RelayPort: &a})
    _, _ = store.Create(ctx, &model.Service{SiteID: s.ID, Name: "y", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP, RelayPort: &b})

    _, err := store.AllocateRelayPort(ctx, p)
    require.Error(t, err)
    require.ErrorIs(t, err, dao.ErrPortRangeExhausted)
}

func TestAllocateRelayPortBadRange(t *testing.T) {
    db := openWithModels(t)
    p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "p", Name: "P", RelayPortRange: "garbage",
    })
    store := dao.NewServiceDAO(db)
    _, err := store.AllocateRelayPort(context.Background(), p)
    require.Error(t, err)
}
```

### Step 2: Run test — verify it fails

Run: `go test ./internal/dao/...`
Expected: compile error: `dao.NewServiceDAO`, `dao.ErrPortRangeExhausted` undefined.

### Step 3: Implement `internal/dao/service.go`

```go
package dao

import (
    "context"
    "errors"
    "fmt"
    "strconv"
    "strings"

    "gorm.io/gorm"

    "github.com/tulip/quicktun/internal/model"
)

// ErrPortRangeExhausted is returned by AllocateRelayPort when no free port
// remains in the project's relay_port_range.
var ErrPortRangeExhausted = errors.New("dao: relay port range exhausted")

// ServiceDAO encapsulates queries against the services table (site-scoped).
type ServiceDAO struct{ db *gorm.DB }

// NewServiceDAO constructs a ServiceDAO bound to db.
func NewServiceDAO(db *gorm.DB) *ServiceDAO { return &ServiceDAO{db: db} }

// Create inserts a new service. Caller must populate SiteID, Name,
// TargetAddr, TargetPort. RelayPort may be nil (unassigned) or set.
func (d *ServiceDAO) Create(ctx context.Context, s *model.Service) (*model.Service, error) {
    if s.Proto == "" {
        s.Proto = model.ProtoTCP
    }
    if err := d.db.WithContext(ctx).Create(s).Error; err != nil {
        return nil, fmt.Errorf("dao: create service: %w", err)
    }
    return s, nil
}

// FindByName returns the live service with (siteID, name).
func (d *ServiceDAO) FindByName(ctx context.Context, siteID uint64, name string) (*model.Service, error) {
    var s model.Service
    err := d.db.WithContext(ctx).
        Where("site_id = ? AND name = ?", siteID, name).
        First(&s).Error
    if err != nil {
        return nil, fmt.Errorf("dao: find service by name: %w", err)
    }
    return &s, nil
}

// ListBySite returns up to pageSize services in siteID, paged by ID cursor.
func (d *ServiceDAO) ListBySite(ctx context.Context, siteID uint64, pageSize int, pageToken string) ([]model.Service, error) {
    if pageSize <= 0 {
        pageSize = 50
    }
    if pageSize > 1000 {
        pageSize = 1000
    }
    q := d.db.WithContext(ctx).
        Where("site_id = ?", siteID).
        Order("id ASC").
        Limit(pageSize)
    if pageToken != "" {
        afterID, err := strconv.ParseUint(pageToken, 10, 64)
        if err != nil {
            return nil, fmt.Errorf("%w: %v", ErrInvalidPageToken, err)
        }
        q = q.Where("id > ?", afterID)
    }
    var out []model.Service
    if err := q.Find(&out).Error; err != nil {
        return nil, fmt.Errorf("dao: list services: %w", err)
    }
    return out, nil
}

// NextServicePageToken returns the page-token for fetching the page after `page`.
func NextServicePageToken(page []model.Service) string {
    if len(page) == 0 {
        return ""
    }
    return strconv.FormatUint(page[len(page)-1].ID, 10)
}

// Update writes mutable fields back. Caller must already have fetched the service.
func (d *ServiceDAO) Update(ctx context.Context, s *model.Service) error {
    if s.ID == 0 {
        return fmt.Errorf("dao: update service: missing ID")
    }
    res := d.db.WithContext(ctx).Save(s)
    if res.Error != nil {
        return fmt.Errorf("dao: update service: %w", res.Error)
    }
    return nil
}

// Delete soft-deletes the service.
func (d *ServiceDAO) Delete(ctx context.Context, id uint64) error {
    res := d.db.WithContext(ctx).Delete(&model.Service{}, id)
    if res.Error != nil {
        return fmt.Errorf("dao: delete service: %w", res.Error)
    }
    return nil
}

// AllocateRelayPort returns the lowest unused port in the project's
// relay_port_range. Returns ErrPortRangeExhausted if all ports are taken.
func (d *ServiceDAO) AllocateRelayPort(ctx context.Context, project *model.Project) (uint16, error) {
    minP, maxP, err := parsePortRange(project.RelayPortRange)
    if err != nil {
        return 0, fmt.Errorf("dao: allocate relay port: %w", err)
    }

    // Get all in-use ports for live services across all sites in this project.
    var used []uint16
    err = d.db.WithContext(ctx).
        Model(&model.Service{}).
        Joins("JOIN sites ON sites.id = services.site_id AND sites.deleted_at IS NULL").
        Where("sites.project_id = ? AND services.relay_port IS NOT NULL", project.ID).
        Pluck("services.relay_port", &used).Error
    if err != nil {
        return 0, fmt.Errorf("dao: lookup used relay ports: %w", err)
    }

    usedSet := make(map[uint16]struct{}, len(used))
    for _, p := range used {
        usedSet[p] = struct{}{}
    }

    for p := minP; p <= maxP; p++ {
        if _, taken := usedSet[p]; !taken {
            return p, nil
        }
    }
    return 0, ErrPortRangeExhausted
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
go test -count=3 ./internal/dao/...
git add internal/dao/service.go internal/dao/service_test.go
git commit -m "feat(dao): add ServiceDAO with relay port allocator"
```

Expected: 25 prior + 6 new = 31 dao tests pass.

---

## Task 3: ServiceService — Get + List

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service_test.go`

### Step 1: Write failing test `internal/grpcsvc/service_service_test.go`

```go
package grpcsvc_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/require"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "gorm.io/gorm"

    quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
    "github.com/tulip/quicktun/internal/audit"
    "github.com/tulip/quicktun/internal/auth"
    "github.com/tulip/quicktun/internal/dao"
    "github.com/tulip/quicktun/internal/grpcsvc"
    "github.com/tulip/quicktun/internal/model"
)

func newServiceService(t *testing.T, db *gorm.DB) *grpcsvc.ServiceService {
    return grpcsvc.NewServiceService(
        dao.NewProjectDAO(db),
        dao.NewSiteDAO(db),
        dao.NewServiceDAO(db),
        audit.NewWriter(db),
    )
}

func mkSvc(t *testing.T, db *gorm.DB, projSlug, siteName, svcName string) (*model.Project, *model.Site, *model.Service) {
    t.Helper()
    p, s := mkProjAndSite(t, db, projSlug, siteName)
    relay := uint16(20100)
    svc, err := dao.NewServiceDAO(db).Create(context.Background(), &model.Service{
        SiteID: s.ID, Name: svcName,
        TargetAddr: "127.0.0.1", TargetPort: 22,
        Proto: model.ProtoTCP, RelayPort: &relay,
    })
    require.NoError(t, err)
    return p, s, svc
}

func TestGetServiceByName(t *testing.T) {
    db := openTestDB(t)
    mkSvc(t, db, "p1", "bastion", "ssh")
    svc := newServiceService(t, db)

    resp, err := svc.GetService(adminCtx(t, db), &quicktunv1.GetServiceRequest{
        Name: "projects/p1/sites/bastion/services/ssh",
    })
    require.NoError(t, err)
    require.Equal(t, "projects/p1/sites/bastion/services/ssh", resp.Name)
    require.Equal(t, uint32(20100), resp.RelayPort)
}

func TestGetServiceNotFound(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "bastion")
    svc := newServiceService(t, db)

    _, err := svc.GetService(adminCtx(t, db), &quicktunv1.GetServiceRequest{
        Name: "projects/p1/sites/bastion/services/missing",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.NotFound, st.Code())
}

func TestGetServiceInvalidName(t *testing.T) {
    db := openTestDB(t)
    svc := newServiceService(t, db)
    _, err := svc.GetService(adminCtx(t, db), &quicktunv1.GetServiceRequest{Name: "garbage"})
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetServiceRequiresAuth(t *testing.T) {
    db := openTestDB(t)
    svc := newServiceService(t, db)
    _, err := svc.GetService(context.Background(), &quicktunv1.GetServiceRequest{
        Name: "projects/p1/sites/b/services/ssh",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListServicesAdminSeesAll(t *testing.T) {
    db := openTestDB(t)
    p, s := mkProjAndSite(t, db, "p1", "bastion")
    sd := dao.NewServiceDAO(db)
    rp1 := uint16(20100)
    rp2 := uint16(20101)
    sd.Create(context.Background(), &model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP, RelayPort: &rp1})
    sd.Create(context.Background(), &model.Service{SiteID: s.ID, Name: "rdp", TargetAddr: "192.168.10.50", TargetPort: 3389, Proto: model.ProtoTCP, RelayPort: &rp2})
    _ = p
    svc := newServiceService(t, db)

    resp, err := svc.ListServices(adminCtx(t, db), &quicktunv1.ListServicesRequest{
        Parent: "projects/p1/sites/bastion",
    })
    require.NoError(t, err)
    require.Len(t, resp.Services, 2)
}

func TestListServicesNonAdminWithoutAccessDenied(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "bastion")
    op := seedOperator(t, db, "noaccess@x.com", "p", false)
    svc := newServiceService(t, db)

    ctx := auth.WithOperator(context.Background(), op)
    _, err := svc.ListServices(ctx, &quicktunv1.ListServicesRequest{
        Parent: "projects/p1/sites/bastion",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.NotFound, st.Code())
}
```

### Step 2: Run test — verify it fails

Run: `go test ./internal/grpcsvc/...`
Expected: compile error: `grpcsvc.NewServiceService` undefined.

### Step 3: Implement `internal/grpcsvc/service_service.go`

```go
package grpcsvc

import (
    "context"
    "errors"

    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
    "google.golang.org/protobuf/types/known/timestamppb"

    quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
    "github.com/tulip/quicktun/internal/audit"
    "github.com/tulip/quicktun/internal/auth"
    "github.com/tulip/quicktun/internal/dao"
    "github.com/tulip/quicktun/internal/model"
    "github.com/tulip/quicktun/internal/resource"
)

// ServiceService implements quicktunv1.ServiceServiceServer.
type ServiceService struct {
    quicktunv1.UnimplementedServiceServiceServer
    projects *dao.ProjectDAO
    sites    *dao.SiteDAO
    services *dao.ServiceDAO
    audit    *audit.Writer
}

// NewServiceService constructs a ServiceService.
func NewServiceService(projects *dao.ProjectDAO, sites *dao.SiteDAO, services *dao.ServiceDAO, audit *audit.Writer) *ServiceService {
    return &ServiceService{projects: projects, sites: sites, services: services, audit: audit}
}

// resolveSiteFromParent parses a "projects/{p}/sites/{s}" parent and returns
// the project + site, performing access checks.
func (s *ServiceService) resolveSiteFromParent(ctx context.Context, parent string) (*model.Project, *model.Site, error) {
    op := auth.OperatorFromContext(ctx)
    if op == nil {
        return nil, nil, status.Error(codes.Unauthenticated, "not authenticated")
    }
    n, err := resource.ParseSiteParent(parent)
    if err != nil {
        return nil, nil, status.Error(codes.InvalidArgument, err.Error())
    }
    p, err := s.projects.FindBySlug(ctx, n.Project)
    if err != nil {
        if dao.IsNotFound(err) {
            return nil, nil, status.Error(codes.NotFound, "project not found")
        }
        return nil, nil, status.Error(codes.Internal, "lookup failed")
    }
    if !op.IsAdmin && !s.hasProjectAccess(ctx, op.ID, p.ID) {
        return nil, nil, status.Error(codes.NotFound, "project not found")
    }
    site, err := s.sites.FindByName(ctx, p.ID, n.Site)
    if err != nil {
        if dao.IsNotFound(err) {
            return nil, nil, status.Error(codes.NotFound, "site not found")
        }
        return nil, nil, status.Error(codes.Internal, "lookup failed")
    }
    return p, site, nil
}

// resolveService parses a "projects/{p}/sites/{s}/services/{svc}" name and
// returns project + site + service, performing access checks.
func (s *ServiceService) resolveService(ctx context.Context, name string) (*model.Project, *model.Site, *model.Service, error) {
    op := auth.OperatorFromContext(ctx)
    if op == nil {
        return nil, nil, nil, status.Error(codes.Unauthenticated, "not authenticated")
    }
    n, err := resource.ParseServiceName(name)
    if err != nil {
        return nil, nil, nil, status.Error(codes.InvalidArgument, err.Error())
    }
    p, err := s.projects.FindBySlug(ctx, n.Project)
    if err != nil {
        if dao.IsNotFound(err) {
            return nil, nil, nil, status.Error(codes.NotFound, "project not found")
        }
        return nil, nil, nil, status.Error(codes.Internal, "lookup failed")
    }
    if !op.IsAdmin && !s.hasProjectAccess(ctx, op.ID, p.ID) {
        return nil, nil, nil, status.Error(codes.NotFound, "project not found")
    }
    site, err := s.sites.FindByName(ctx, p.ID, n.Site)
    if err != nil {
        if dao.IsNotFound(err) {
            return nil, nil, nil, status.Error(codes.NotFound, "site not found")
        }
        return nil, nil, nil, status.Error(codes.Internal, "lookup failed")
    }
    svc, err := s.services.FindByName(ctx, site.ID, n.Service)
    if err != nil {
        if dao.IsNotFound(err) {
            return nil, nil, nil, status.Error(codes.NotFound, "service not found")
        }
        return nil, nil, nil, status.Error(codes.Internal, "lookup failed")
    }
    return p, site, svc, nil
}

func (s *ServiceService) hasProjectAccess(ctx context.Context, operatorID, projectID uint64) bool {
    var count int64
    err := s.projects.Db().WithContext(ctx).
        Model(&model.OperatorProjectAccess{}).
        Where("operator_id = ? AND project_id = ?", operatorID, projectID).
        Count(&count).Error
    return err == nil && count > 0
}

// GetService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) GetService(ctx context.Context, req *quicktunv1.GetServiceRequest) (*quicktunv1.Service, error) {
    p, site, svc, err := s.resolveService(ctx, req.GetName())
    if err != nil {
        return nil, err
    }
    return serviceToProto(p, site, svc), nil
}

// ListServices implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) ListServices(ctx context.Context, req *quicktunv1.ListServicesRequest) (*quicktunv1.ListServicesResponse, error) {
    p, site, err := s.resolveSiteFromParent(ctx, req.GetParent())
    if err != nil {
        return nil, err
    }
    pageSize := int(req.GetPage().GetPageSize())
    pageToken := req.GetPage().GetPageToken()
    rows, err := s.services.ListBySite(ctx, site.ID, pageSize, pageToken)
    if err != nil {
        if errors.Is(err, dao.ErrInvalidPageToken) {
            return nil, status.Error(codes.InvalidArgument, "invalid page_token")
        }
        return nil, status.Error(codes.Internal, "list failed")
    }
    out := &quicktunv1.ListServicesResponse{
        Services: make([]*quicktunv1.Service, len(rows)),
        Page:     &quicktunv1.PageResponse{NextPageToken: dao.NextServicePageToken(rows)},
    }
    for i := range rows {
        out.Services[i] = serviceToProto(p, site, &rows[i])
    }
    return out, nil
}

func serviceToProto(p *model.Project, site *model.Site, svc *model.Service) *quicktunv1.Service {
    out := &quicktunv1.Service{
        Name:        resource.FormatServiceName(p.Slug, site.Name, svc.Name),
        ServiceId:   svc.Name,
        DisplayName: svc.Name,
        TargetAddr:  svc.TargetAddr,
        TargetPort:  uint32(svc.TargetPort),
        Proto:       protoFromModel(svc.Proto),
        CreateTime:  timestamppb.New(svc.CreatedAt),
        UpdateTime:  timestamppb.New(svc.UpdatedAt),
    }
    if svc.RelayPort != nil {
        out.RelayPort = uint32(*svc.RelayPort)
    }
    return out
}

func protoFromModel(p model.Proto) quicktunv1.Proto {
    switch p {
    case model.ProtoTCP:
        return quicktunv1.Proto_PROTO_TCP
    case model.ProtoUDP:
        return quicktunv1.Proto_PROTO_UDP
    }
    return quicktunv1.Proto_PROTO_UNSPECIFIED
}

func protoFromProto(p quicktunv1.Proto) model.Proto {
    switch p {
    case quicktunv1.Proto_PROTO_TCP:
        return model.ProtoTCP
    case quicktunv1.Proto_PROTO_UDP:
        return model.ProtoUDP
    }
    return ""
}
```

### Step 4: Run tests + commit

```bash
go test -count=3 ./internal/grpcsvc/...
git add internal/grpcsvc/service_service.go internal/grpcsvc/service_service_test.go
git commit -m "feat(grpcsvc): add ServiceService Get + List"
```

Expected: prior + 6 new ServiceService tests pass.

---

## Task 4: ServiceService — Create

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service_test.go`

### Step 1: Append failing tests

```go
func TestCreateServiceSuccess(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "bastion")
    svc := newServiceService(t, db)

    resp, err := svc.CreateService(adminCtx(t, db), &quicktunv1.CreateServiceRequest{
        Parent:    "projects/p1/sites/bastion",
        ServiceId: "ssh",
        Service: &quicktunv1.Service{
            DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22,
            Proto: quicktunv1.Proto_PROTO_TCP,
        },
    })
    require.NoError(t, err)
    require.Equal(t, "projects/p1/sites/bastion/services/ssh", resp.Name)
    require.NotZero(t, resp.RelayPort)
    require.GreaterOrEqual(t, resp.RelayPort, uint32(20000))
    require.LessOrEqual(t, resp.RelayPort, uint32(20099))

    var audits []model.AuditLog
    require.NoError(t, db.Where("action = ?", "service.create").Find(&audits).Error)
    require.Len(t, audits, 1)
}

func TestCreateServiceRejectsDuplicate(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "bastion")
    svc := newServiceService(t, db)
    ctx := adminCtx(t, db)

    req := &quicktunv1.CreateServiceRequest{
        Parent: "projects/p1/sites/bastion", ServiceId: "ssh",
        Service: &quicktunv1.Service{
            DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: quicktunv1.Proto_PROTO_TCP,
        },
    }
    _, err := svc.CreateService(ctx, req)
    require.NoError(t, err)
    _, err = svc.CreateService(ctx, req)
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.AlreadyExists, st.Code())
}

func TestCreateServiceRejectsBadTarget(t *testing.T) {
    db := openTestDB(t)
    mkProjAndSite(t, db, "p1", "bastion")
    svc := newServiceService(t, db)

    _, err := svc.CreateService(adminCtx(t, db), &quicktunv1.CreateServiceRequest{
        Parent: "projects/p1/sites/bastion", ServiceId: "x",
        Service: &quicktunv1.Service{
            DisplayName: "X",
            // missing TargetAddr / TargetPort
        },
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateServicePortRangeExhausted(t *testing.T) {
    db := openTestDB(t)
    p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "tiny", Name: "T", RelayPortRange: "20500-20500", // only one slot
    })
    s, _ := dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "b"})
    _ = s
    svc := newServiceService(t, db)
    ctx := adminCtx(t, db)

    // First create succeeds.
    _, err := svc.CreateService(ctx, &quicktunv1.CreateServiceRequest{
        Parent: "projects/tiny/sites/b", ServiceId: "a",
        Service: &quicktunv1.Service{DisplayName: "A", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: quicktunv1.Proto_PROTO_TCP},
    })
    require.NoError(t, err)
    // Second exhausts.
    _, err = svc.CreateService(ctx, &quicktunv1.CreateServiceRequest{
        Parent: "projects/tiny/sites/b", ServiceId: "b",
        Service: &quicktunv1.Service{DisplayName: "B", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: quicktunv1.Proto_PROTO_TCP},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.ResourceExhausted, st.Code())
}

func TestCreateServiceRequiresAdmin(t *testing.T) {
    db := openTestDB(t)
    p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
    })
    dao.NewSiteDAO(db).Create(context.Background(), &model.Site{ProjectID: p.ID, Name: "bastion"})
    op := seedOperator(t, db, "u@x.com", "p", false)
    require.NoError(t, db.Create(&model.OperatorProjectAccess{
        OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleOperator,
    }).Error)
    svc := newServiceService(t, db)

    ctx := auth.WithOperator(context.Background(), op)
    _, err := svc.CreateService(ctx, &quicktunv1.CreateServiceRequest{
        Parent: "projects/p1/sites/bastion", ServiceId: "ssh",
        Service: &quicktunv1.Service{DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: quicktunv1.Proto_PROTO_TCP},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.PermissionDenied, st.Code())
}
```

### Step 2: Append CreateService implementation

```go

// CreateService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) CreateService(ctx context.Context, req *quicktunv1.CreateServiceRequest) (*quicktunv1.Service, error) {
    p, site, err := s.resolveSiteFromParent(ctx, req.GetParent())
    if err != nil {
        return nil, err
    }
    op := auth.OperatorFromContext(ctx)
    if !op.IsAdmin {
        return nil, status.Error(codes.PermissionDenied, "admin role required")
    }
    if req.GetService() == nil {
        return nil, status.Error(codes.InvalidArgument, "service body is required")
    }
    if err := resource.ValidateSlug(req.GetServiceId()); err != nil {
        return nil, status.Error(codes.InvalidArgument, err.Error())
    }
    if req.Service.GetTargetAddr() == "" {
        return nil, status.Error(codes.InvalidArgument, "service.target_addr is required")
    }
    if req.Service.GetTargetPort() == 0 || req.Service.GetTargetPort() > 65535 {
        return nil, status.Error(codes.InvalidArgument, "service.target_port must be 1-65535")
    }

    // Allocate a relay port from the project's range.
    relayPort, err := s.services.AllocateRelayPort(ctx, p)
    if err != nil {
        if errors.Is(err, dao.ErrPortRangeExhausted) {
            return nil, status.Error(codes.ResourceExhausted, "no relay ports available in project range")
        }
        return nil, status.Error(codes.Internal, "port allocation failed")
    }

    proto := protoFromProto(req.Service.Proto)
    if proto == "" {
        proto = model.ProtoTCP
    }

    row := &model.Service{
        SiteID:     site.ID,
        Name:       req.ServiceId,
        TargetAddr: req.Service.TargetAddr,
        TargetPort: uint16(req.Service.TargetPort),
        Proto:      proto,
        RelayPort:  &relayPort,
    }
    if _, err := s.services.Create(ctx, row); err != nil {
        if isUniqueConstraintErr(err) {
            return nil, status.Error(codes.AlreadyExists, "service already exists in site")
        }
        return nil, status.Error(codes.Internal, "create failed")
    }

    _ = s.audit.Log(ctx, audit.Entry{
        ProjectID: ptrUint64(p.ID),
        Action:    "service.create",
        Target:    resource.FormatServiceName(p.Slug, site.Name, row.Name),
        Extra: map[string]any{
            "target":     row.TargetAddr + ":" + intToString(int(row.TargetPort)),
            "relay_port": relayPort,
        },
    })

    return serviceToProto(p, site, row), nil
}

func intToString(n int) string {
    if n == 0 {
        return "0"
    }
    var b [10]byte
    i := len(b)
    for n > 0 {
        i--
        b[i] = byte('0' + n%10)
        n /= 10
    }
    return string(b[i:])
}
```

### Step 3: Run tests + commit

```bash
go test -count=3 ./internal/grpcsvc/...
git add internal/grpcsvc/service_service.go internal/grpcsvc/service_service_test.go
git commit -m "feat(grpcsvc): add ServiceService.CreateService with port allocation"
```

Expected: 5 new + prior tests pass.

---

## Task 5: ServiceService — Update + Delete

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/service_service_test.go`

### Step 1: Append failing tests

Add `fieldmaskpb` import (likely already there from project/site service tests). Append:

```go
func TestUpdateServiceTarget(t *testing.T) {
    db := openTestDB(t)
    mkSvc(t, db, "p1", "bastion", "ssh")
    svc := newServiceService(t, db)

    resp, err := svc.UpdateService(adminCtx(t, db), &quicktunv1.UpdateServiceRequest{
        Service: &quicktunv1.Service{
            Name:       "projects/p1/sites/bastion/services/ssh",
            TargetAddr: "192.168.10.50",
            TargetPort: 2222,
        },
        UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"target_addr", "target_port"}},
    })
    require.NoError(t, err)
    require.Equal(t, "192.168.10.50", resp.TargetAddr)
    require.Equal(t, uint32(2222), resp.TargetPort)
}

func TestUpdateServiceRequiresMask(t *testing.T) {
    db := openTestDB(t)
    mkSvc(t, db, "p1", "bastion", "ssh")
    svc := newServiceService(t, db)

    _, err := svc.UpdateService(adminCtx(t, db), &quicktunv1.UpdateServiceRequest{
        Service: &quicktunv1.Service{Name: "projects/p1/sites/bastion/services/ssh", TargetAddr: "x"},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateServiceRejectsRelayPort(t *testing.T) {
    db := openTestDB(t)
    mkSvc(t, db, "p1", "bastion", "ssh")
    svc := newServiceService(t, db)

    _, err := svc.UpdateService(adminCtx(t, db), &quicktunv1.UpdateServiceRequest{
        Service: &quicktunv1.Service{
            Name: "projects/p1/sites/bastion/services/ssh", RelayPort: 30000,
        },
        UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"relay_port"}},
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteServiceSuccess(t *testing.T) {
    db := openTestDB(t)
    mkSvc(t, db, "p1", "bastion", "ssh")
    svc := newServiceService(t, db)
    ctx := adminCtx(t, db)

    _, err := svc.DeleteService(ctx, &quicktunv1.DeleteServiceRequest{
        Name: "projects/p1/sites/bastion/services/ssh",
    })
    require.NoError(t, err)

    _, err = svc.GetService(ctx, &quicktunv1.GetServiceRequest{
        Name: "projects/p1/sites/bastion/services/ssh",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.NotFound, st.Code())
}

func TestDeleteServiceRequiresAdmin(t *testing.T) {
    db := openTestDB(t)
    p, site, _ := mkSvc(t, db, "p1", "bastion", "ssh")
    op := seedOperator(t, db, "u@x.com", "p", false)
    require.NoError(t, db.Create(&model.OperatorProjectAccess{
        OperatorID: op.ID, ProjectID: p.ID, Role: model.ProjectRoleViewer,
    }).Error)
    _ = site
    svc := newServiceService(t, db)

    ctx := auth.WithOperator(context.Background(), op)
    _, err := svc.DeleteService(ctx, &quicktunv1.DeleteServiceRequest{
        Name: "projects/p1/sites/bastion/services/ssh",
    })
    require.Error(t, err)
    st, _ := status.FromError(err)
    require.Equal(t, codes.PermissionDenied, st.Code())
}
```

### Step 2: Append Update + Delete implementations

```go

// UpdateService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) UpdateService(ctx context.Context, req *quicktunv1.UpdateServiceRequest) (*quicktunv1.Service, error) {
    if req.GetService() == nil {
        return nil, status.Error(codes.InvalidArgument, "service body is required")
    }
    if req.GetUpdateMask() == nil || len(req.UpdateMask.Paths) == 0 {
        return nil, status.Error(codes.InvalidArgument, "update_mask is required")
    }
    p, site, svc, err := s.resolveService(ctx, req.Service.GetName())
    if err != nil {
        return nil, err
    }
    op := auth.OperatorFromContext(ctx)
    if !op.IsAdmin {
        return nil, status.Error(codes.PermissionDenied, "admin role required")
    }

    changed := map[string]any{}
    for _, path := range req.UpdateMask.Paths {
        switch path {
        case "display_name":
            // no-op at DB layer (Service.Name doubles as slug+label in Phase 1)
            changed["display_name"] = req.Service.DisplayName
        case "target_addr":
            if req.Service.TargetAddr == "" {
                return nil, status.Error(codes.InvalidArgument, "target_addr cannot be empty")
            }
            svc.TargetAddr = req.Service.TargetAddr
            changed["target_addr"] = req.Service.TargetAddr
        case "target_port":
            if req.Service.TargetPort == 0 || req.Service.TargetPort > 65535 {
                return nil, status.Error(codes.InvalidArgument, "target_port must be 1-65535")
            }
            svc.TargetPort = uint16(req.Service.TargetPort)
            changed["target_port"] = req.Service.TargetPort
        case "proto":
            pr := protoFromProto(req.Service.Proto)
            if pr == "" {
                return nil, status.Error(codes.InvalidArgument, "proto must be TCP or UDP")
            }
            svc.Proto = pr
            changed["proto"] = string(pr)
        case "relay_port":
            // relay_port is server-allocated; reject client attempts to change it.
            return nil, status.Error(codes.InvalidArgument, "relay_port is allocated by the server and cannot be updated")
        default:
            return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
        }
    }

    if err := s.services.Update(ctx, svc); err != nil {
        return nil, status.Error(codes.Internal, "update failed")
    }

    _ = s.audit.Log(ctx, audit.Entry{
        ProjectID: ptrUint64(p.ID),
        Action:    "service.update",
        Target:    resource.FormatServiceName(p.Slug, site.Name, svc.Name),
        Extra:     changed,
    })

    return serviceToProto(p, site, svc), nil
}

// DeleteService implements quicktunv1.ServiceServiceServer.
func (s *ServiceService) DeleteService(ctx context.Context, req *quicktunv1.DeleteServiceRequest) (*emptypb.Empty, error) {
    p, site, svc, err := s.resolveService(ctx, req.GetName())
    if err != nil {
        return nil, err
    }
    op := auth.OperatorFromContext(ctx)
    if !op.IsAdmin {
        return nil, status.Error(codes.PermissionDenied, "admin role required")
    }
    if err := s.services.Delete(ctx, svc.ID); err != nil {
        return nil, status.Error(codes.Internal, "delete failed")
    }
    _ = s.audit.Log(ctx, audit.Entry{
        ProjectID: ptrUint64(p.ID),
        Action:    "service.delete",
        Target:    resource.FormatServiceName(p.Slug, site.Name, svc.Name),
    })
    return &emptypb.Empty{}, nil
}
```

Add `emptypb` to the import block.

### Step 3: Run tests + commit

```bash
go test -count=3 ./internal/grpcsvc/...
git add internal/grpcsvc/service_service.go internal/grpcsvc/service_service_test.go
git commit -m "feat(grpcsvc): add ServiceService Update + Delete"
```

Expected: 5 new + prior tests pass.

---

## Task 6: Wire ServiceService + admin CLI + smoke + final verification

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server_test.go`
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_admin_service.go`
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_admin_service_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_admin.go`
- Modify: `/Users/tulip/project/repos/quicktun/scripts/smoke.sh`

### Step 1: Register ServiceService in server

Edit `/Users/tulip/project/repos/quicktun/internal/server/server.go`. In `New`, after the existing SiteService registration:

```go
    serviceSvc := grpcsvc.NewServiceService(
        dao.NewProjectDAO(cfg.DB),
        dao.NewSiteDAO(cfg.DB),
        dao.NewServiceDAO(cfg.DB),
        auditWriter,
    )
    quicktunv1.RegisterServiceServiceServer(gs, serviceSvc)
```

In `Run`, after the SiteService gateway registration:

```go
    if err := quicktunv1.RegisterServiceServiceHandlerFromEndpoint(ctx, gatewayMux, s.cfg.GRPCListen, dialOpts); err != nil {
        grpcLn.Close()
        return fmt.Errorf("server: register service gateway: %w", err)
    }
```

### Step 2: Append e2e test

```go
func TestServiceCreateEndToEnd(t *testing.T) {
    db := newDB(t)
    hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
    _, err := dao.NewOperatorDAO(db).Create(context.Background(), "admin@x.com", string(hash), true)
    require.NoError(t, err)
    p, _ := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "p1", Name: "P1", RelayPortRange: "20000-20099",
    })
    _, err = dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
        ProjectID: p.ID, Name: "bastion",
    })
    require.NoError(t, err)

    grpcAddr := freePort(t)
    httpAddr := freePort(t)
    srv, err := server.New(server.Config{
        DB: db, Logger: zap.NewNop(),
        GRPCListen: grpcAddr, HTTPListen: httpAddr,
        SessionTTL: time.Hour,
    })
    require.NoError(t, err)

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    errCh := make(chan error, 1)
    go func() { errCh <- srv.Run(ctx) }()
    t.Cleanup(func() {
        cancel()
        select {
        case <-errCh:
        case <-time.After(2 * time.Second):
        }
    })
    require.Eventually(t, func() bool {
        c, err := net.DialTimeout("tcp", grpcAddr, 100*time.Millisecond)
        if err != nil {
            return false
        }
        c.Close()
        return true
    }, 2*time.Second, 25*time.Millisecond)

    conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    require.NoError(t, err)
    defer conn.Close()

    authClient := quicktunv1.NewAuthServiceClient(conn)
    loginResp, err := authClient.Login(context.Background(), &quicktunv1.LoginRequest{
        Email: "admin@x.com", Password: "pw",
    })
    require.NoError(t, err)

    svcClient := quicktunv1.NewServiceServiceClient(conn)
    authedCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+loginResp.AccessToken)
    created, err := svcClient.CreateService(authedCtx, &quicktunv1.CreateServiceRequest{
        Parent:    "projects/p1/sites/bastion",
        ServiceId: "ssh",
        Service: &quicktunv1.Service{
            DisplayName: "SSH", TargetAddr: "127.0.0.1", TargetPort: 22,
            Proto: quicktunv1.Proto_PROTO_TCP,
        },
    })
    require.NoError(t, err)
    require.Equal(t, "projects/p1/sites/bastion/services/ssh", created.Name)
    require.NotZero(t, created.RelayPort)

    listed, err := svcClient.ListServices(authedCtx, &quicktunv1.ListServicesRequest{
        Parent: "projects/p1/sites/bastion",
    })
    require.NoError(t, err)
    require.Len(t, listed.Services, 1)
}
```

### Step 3: admin service CLI

Create `cmd/quicktun-server/cmd_admin_service.go`:

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/spf13/cobra"

    "github.com/tulip/quicktun/internal/dao"
    "github.com/tulip/quicktun/internal/model"
    "github.com/tulip/quicktun/internal/resource"
)

func adminServiceCmd() *cobra.Command {
    c := &cobra.Command{
        Use:   "service",
        Short: "Manage services (admin)",
    }
    c.AddCommand(adminServiceCreateCmd())
    c.AddCommand(adminServiceListCmd())
    c.AddCommand(adminServiceDeleteCmd())
    return c
}

func adminServiceCreateCmd() *cobra.Command {
    var (
        projectSlug string
        siteSlug    string
        serviceSlug string
        targetAddr  string
        targetPort  uint16
    )
    c := &cobra.Command{
        Use:   "create",
        Short: "Create a service",
        RunE: func(cmd *cobra.Command, _ []string) error {
            if projectSlug == "" || siteSlug == "" || serviceSlug == "" || targetAddr == "" || targetPort == 0 {
                return fmt.Errorf("admin service create: --project, --site, --slug, --target-addr, --target-port required")
            }
            for k, v := range map[string]string{"project": projectSlug, "site": siteSlug, "service": serviceSlug} {
                if err := resource.ValidateSlug(v); err != nil {
                    return fmt.Errorf("admin service create: %s: %w", k, err)
                }
            }
            db, err := openAdminDB(cmd)
            if err != nil {
                return err
            }
            defer func() { s, _ := db.DB(); s.Close() }()

            p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
            if err != nil {
                return fmt.Errorf("admin service create: %w", err)
            }
            site, err := dao.NewSiteDAO(db).FindByName(context.Background(), p.ID, siteSlug)
            if err != nil {
                return fmt.Errorf("admin service create: %w", err)
            }
            store := dao.NewServiceDAO(db)
            relayPort, err := store.AllocateRelayPort(context.Background(), p)
            if err != nil {
                return fmt.Errorf("admin service create: %w", err)
            }
            svc, err := store.Create(context.Background(), &model.Service{
                SiteID: site.ID, Name: serviceSlug,
                TargetAddr: targetAddr, TargetPort: targetPort,
                Proto: model.ProtoTCP, RelayPort: &relayPort,
            })
            if err != nil {
                return fmt.Errorf("admin service create: %w", err)
            }
            cmd.Printf("created service %q (id=%d, relay_port=%d)\n", svc.Name, svc.ID, relayPort)
            return nil
        },
    }
    c.Flags().StringVar(&projectSlug, "project", "", "project slug")
    c.Flags().StringVar(&siteSlug, "site", "", "site slug")
    c.Flags().StringVar(&serviceSlug, "slug", "", "service slug")
    c.Flags().StringVar(&targetAddr, "target-addr", "", "target IP (127.0.0.1 or LAN)")
    c.Flags().Uint16Var(&targetPort, "target-port", 0, "target TCP port")
    return c
}

func adminServiceListCmd() *cobra.Command {
    var (
        projectSlug string
        siteSlug    string
    )
    c := &cobra.Command{
        Use:   "list",
        Short: "List services in a site",
        RunE: func(cmd *cobra.Command, _ []string) error {
            if projectSlug == "" || siteSlug == "" {
                return fmt.Errorf("admin service list: --project and --site required")
            }
            db, err := openAdminDB(cmd)
            if err != nil {
                return err
            }
            defer func() { s, _ := db.DB(); s.Close() }()
            p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
            if err != nil {
                return fmt.Errorf("admin service list: %w", err)
            }
            site, err := dao.NewSiteDAO(db).FindByName(context.Background(), p.ID, siteSlug)
            if err != nil {
                return fmt.Errorf("admin service list: %w", err)
            }
            rows, err := dao.NewServiceDAO(db).ListBySite(context.Background(), site.ID, 1000, "")
            if err != nil {
                return fmt.Errorf("admin service list: %w", err)
            }
            enc := json.NewEncoder(os.Stdout)
            enc.SetIndent("", "  ")
            return enc.Encode(rows)
        },
    }
    c.Flags().StringVar(&projectSlug, "project", "", "project slug")
    c.Flags().StringVar(&siteSlug, "site", "", "site slug")
    return c
}

func adminServiceDeleteCmd() *cobra.Command {
    var (
        projectSlug string
        siteSlug    string
        serviceSlug string
    )
    c := &cobra.Command{
        Use:   "delete",
        Short: "Delete a service",
        RunE: func(cmd *cobra.Command, _ []string) error {
            if projectSlug == "" || siteSlug == "" || serviceSlug == "" {
                return fmt.Errorf("admin service delete: --project, --site, --slug required")
            }
            db, err := openAdminDB(cmd)
            if err != nil {
                return err
            }
            defer func() { s, _ := db.DB(); s.Close() }()
            p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), projectSlug)
            if err != nil {
                return fmt.Errorf("admin service delete: %w", err)
            }
            site, err := dao.NewSiteDAO(db).FindByName(context.Background(), p.ID, siteSlug)
            if err != nil {
                return fmt.Errorf("admin service delete: %w", err)
            }
            store := dao.NewServiceDAO(db)
            svc, err := store.FindByName(context.Background(), site.ID, serviceSlug)
            if err != nil {
                return fmt.Errorf("admin service delete: %w", err)
            }
            if err := store.Delete(context.Background(), svc.ID); err != nil {
                return fmt.Errorf("admin service delete: %w", err)
            }
            cmd.Printf("deleted service %q\n", svc.Name)
            return nil
        },
    }
    c.Flags().StringVar(&projectSlug, "project", "", "project slug")
    c.Flags().StringVar(&siteSlug, "site", "", "site slug")
    c.Flags().StringVar(&serviceSlug, "slug", "", "service slug")
    return c
}
```

Edit `cmd/quicktun-server/cmd_admin.go`. In `adminCmd()`:

```go
    c.AddCommand(adminServiceCmd())
```

### Step 4: admin service CLI test

Create `cmd/quicktun-server/cmd_admin_service_test.go`:

```go
package main

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/spf13/cobra"
    "github.com/stretchr/testify/require"

    "github.com/tulip/quicktun/internal/dao"
    "github.com/tulip/quicktun/internal/migration"
    "github.com/tulip/quicktun/internal/model"
)

func TestAdminServiceCreate(t *testing.T) {
    dir := t.TempDir()
    dbPath := filepath.Join(dir, "qt.db")
    cfgPath := filepath.Join(dir, "server.yaml")
    yaml := `
control_plane:
  grpc_listen: 127.0.0.1:9443
database:
  driver: sqlite
  dsn: ` + dbPath + `?_foreign_keys=on
session:
  default_ttl: 1h
log:
  level: error
`
    require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o600))
    require.NoError(t, migration.Up(dbPath+"?_foreign_keys=on"))

    db, err := dao.Open(dbPath, nil)
    require.NoError(t, err)
    p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
        Slug: "clinic-network", Name: "Clinic", RelayPortRange: "20000-20099",
    })
    require.NoError(t, err)
    _, err = dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
        ProjectID: p.ID, Name: "bastion-1",
    })
    require.NoError(t, err)
    s, _ := db.DB()
    s.Close()

    root := &cobra.Command{Use: "root"}
    root.PersistentFlags().String("config", cfgPath, "")
    root.AddCommand(adminCmd())
    root.SetArgs([]string{"admin", "service", "create",
        "--project=clinic-network",
        "--site=bastion-1",
        "--slug=ssh",
        "--target-addr=127.0.0.1",
        "--target-port=22",
    })
    require.NoError(t, root.Execute())
}
```

### Step 5: Smoke script update

Edit `/Users/tulip/project/repos/quicktun/scripts/smoke.sh`. Find the line `echo "site: PASS"` and after it (BEFORE the existing site DELETE), insert service flow:

```bash
echo "site: PASS"

# Service flow
SVC_RESP=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites/smoke-bastion/services?service_id=ssh" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"SSH","targetAddr":"127.0.0.1","targetPort":22,"proto":"PROTO_TCP"}')
echo "service create: $SVC_RESP"
if ! echo "$SVC_RESP" | grep -q '"name":"projects/smoke-test/sites/smoke-bastion/services/ssh"'; then
  echo "FAIL: service create response missing expected name" >&2
  exit 1
fi
if ! echo "$SVC_RESP" | grep -q '"relayPort":'; then
  echo "FAIL: service create did not return relay_port" >&2
  exit 1
fi

LIST_SVCS=$(curl -sS "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites/smoke-bastion/services" \
  -H "Authorization: Bearer $TOKEN")
echo "service list: $LIST_SVCS"

curl -sS -X DELETE "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites/smoke-bastion/services/ssh" \
  -H "Authorization: Bearer $TOKEN" > /dev/null
echo "service: PASS"
```

Find the final `echo "PASS: end-to-end auth + project + site flow"` and change to:

```bash
echo "PASS: end-to-end auth + project + site + service flow"
```

### Step 6: Run final verification + commit

```bash
cd /Users/tulip/project/repos/quicktun
make sync-migrations
make check-migrations
go test ./...
go test -race -timeout 120s ./...
go vet ./...
make proto-lint
make build
./scripts/smoke.sh

git add internal/server/ cmd/quicktun-server/cmd_admin.go cmd/quicktun-server/cmd_admin_service.go cmd/quicktun-server/cmd_admin_service_test.go scripts/smoke.sh
git commit -m "feat(server,cmd,smoke): wire ServiceService + admin CLI + smoke flow"
```

Expected: all green; smoke prints `PASS: end-to-end auth + project + site + service flow`.

---

## Self-Review

**Spec coverage:**

| Plan 5 requirement | Implemented in |
|---|---|
| Plan 4 cleanup (parser unification + last_used_at + RelayAddr config + UNSPECIFIED tests + bad-os test) | Task 0 |
| service.proto with 5 standard methods | Task 1 |
| ServiceDAO + AllocateRelayPort with port range parser | Task 2 |
| ServiceService.GetService + ListServices with admin/scope | Task 3 |
| ServiceService.CreateService with port allocation, audit | Task 4 |
| ServiceService.UpdateService (FieldMask: target_addr/target_port/proto, rejects relay_port mutation), DeleteService | Task 5 |
| Wire ServiceService into server + e2e + admin CLI + smoke | Task 6 |

**No placeholders.** Each step has complete code.

**Type consistency:** `dao.NewServiceDAO`, `dao.ErrPortRangeExhausted`, `dao.NextServicePageToken`, `dao.AllocateRelayPort(*model.Project)` consistently referenced. `resource.ParseServiceName` returns `ServiceName{Project, Site, Service}`. `protoFromProto` and `protoFromModel` are inverse pairs. `serviceToProto(p, site, svc)` takes 3 args matching the resource hierarchy.

**Forward references resolved:** All types defined before use. `intToString` defined in Task 4 alongside CreateService; not reused elsewhere. `emptypb` import added in Task 5 for DeleteService.
