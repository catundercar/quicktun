# quicktun Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap the quicktun repository with Go module, project structure, GORM data models, SQL migrations, config loading, and structured logging — producing a working `quicktun-server migrate` command that materializes the full Phase 1 schema in SQLite.

**Architecture:** Single Go module rooted at `github.com/tulip/quicktun`. Layered structure: `cmd/` for binaries, `internal/` for non-public packages (config, logger, model, dao, migration), `migrations/` for SQL files. SQLite via GORM with WAL mode. Migrations run via `golang-migrate/migrate` library (not the CLI) so they bake into the server binary. Logging via Uber zap with lumberjack rotation. Config loading via Viper from a YAML file.

**Tech Stack:**
- Go 1.22+
- GORM v2 (gorm.io/gorm + gorm.io/driver/sqlite)
- golang-migrate/migrate v4 (with file source + sqlite3 driver)
- Uber zap + natefinch/lumberjack
- spf13/cobra + spf13/viper
- testify (assertions)

---

## File Structure

Created in this plan:

```
quicktun/
├── .gitignore
├── .editorconfig
├── go.mod
├── go.sum
├── Makefile
├── cmd/
│   └── quicktun-server/
│       ├── main.go                 # entrypoint, root cobra command
│       ├── cmd_migrate.go          # `quicktun-server migrate` subcommand
│       └── cmd_version.go          # `quicktun-server version` subcommand
├── internal/
│   ├── config/
│   │   ├── config.go               # Config struct + Load()
│   │   └── config_test.go
│   ├── logger/
│   │   ├── logger.go               # zap+lumberjack init
│   │   └── logger_test.go
│   ├── model/
│   │   ├── base.go                 # Base struct, common types
│   │   ├── operator.go             # Operator + OperatorSession
│   │   ├── project.go              # Project + OperatorProjectAccess
│   │   ├── site.go                 # Site + SiteAgentToken
│   │   ├── service.go              # Service
│   │   ├── audit.go                # AuditLog
│   │   └── all.go                  # AllModels() helper for AutoMigrate / tests
│   ├── dao/
│   │   ├── db.go                   # OpenDB(dsn) helper, GORM config
│   │   └── db_test.go
│   └── migration/
│       ├── migrate.go              # Run(dsn) wraps golang-migrate
│       ├── migrate_test.go
│       └── embed.go                # //go:embed migrations/*.sql
├── migrations/
│   ├── 0001_init.up.sql
│   └── 0001_init.down.sql
├── etc/
│   └── server.example.yaml         # example config
└── docs/                           # (already exists)
```

The Go module path will be `github.com/tulip/quicktun` (placeholder org `tulip` — adjust to actual GitHub org when registering).

---

## Task 0: Repository Bootstrap

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/.gitignore`
- Create: `/Users/tulip/project/repos/quicktun/.editorconfig`
- Create: `/Users/tulip/project/repos/quicktun/Makefile`

- [ ] **Step 1: Initialize git repository**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
git init
git config --local commit.gpgsign false
```

Expected: `Initialized empty Git repository in /Users/tulip/project/repos/quicktun/.git/`

- [ ] **Step 2: Create `.gitignore`**

```gitignore
# binaries
/bin/
/dist/
*.exe

# data
*.db
*.db-journal
*.db-wal
*.db-shm

# logs
*.log
/var/

# Go
/vendor/
*.test
*.out
coverage.txt

# editors
.vscode/
.idea/
*.swp
.DS_Store

# secrets
.env
etc/server.yaml
etc/tls/*
!etc/tls/.gitkeep

# generated
gen/
```

- [ ] **Step 3: Create `.editorconfig`**

```editorconfig
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true
indent_style = space
indent_size = 4

[*.go]
indent_style = tab

[Makefile]
indent_style = tab

[*.{yml,yaml,proto,md}]
indent_size = 2
```

- [ ] **Step 4: Create `Makefile`**

```makefile
.PHONY: all build test lint clean migrate

GO ?= go
BINDIR := bin
SERVER_BIN := $(BINDIR)/quicktun-server

all: build

build: $(SERVER_BIN)

$(SERVER_BIN):
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-server

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...

clean:
	rm -rf $(BINDIR) coverage.txt

migrate: build
	$(SERVER_BIN) migrate --config etc/server.yaml
```

- [ ] **Step 5: Initialize Go module**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go mod init github.com/tulip/quicktun
```

Expected: Creates `go.mod` with `module github.com/tulip/quicktun` and a Go version line.

- [ ] **Step 6: Commit bootstrap**

```bash
git add .gitignore .editorconfig Makefile go.mod docs/
git commit -m "chore: bootstrap repository structure"
```

---

## Task 1: GORM Model — Base + Common Types

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/model/base.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/model/all.go`

- [ ] **Step 1: Add GORM dependency**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go get gorm.io/gorm@v1.25.12
go get gorm.io/driver/sqlite@v1.5.6
```

Expected: `go.mod` updated with two new requires; `go.sum` populated.

- [ ] **Step 2: Create `internal/model/base.go`**

```go
package model

import (
	"time"

	"gorm.io/gorm"
)

// Base provides common ID + timestamps + soft delete used by most models.
type Base struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
```

- [ ] **Step 3: Create `internal/model/all.go` (placeholder, populated as models land)**

```go
package model

// AllModels returns every concrete model type registered in this package.
// Used by AutoMigrate for tests and by introspection tooling.
//
// Production migrations are SQL files under /migrations and should remain the
// source of truth — AutoMigrate is for in-memory test fixtures only.
func AllModels() []any {
	return []any{
		// populated as model files are added
	}
}
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/model/base.go internal/model/all.go
git commit -m "feat(model): add Base struct and AllModels registry"
```

---

## Task 2: Operator + Session Models

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/model/operator.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/model/operator_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/all.go`

- [ ] **Step 1: Write failing test `internal/model/operator_test.go`**

```go
package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

func openMemDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	return db
}

func TestOperatorRoundTrip(t *testing.T) {
	db := openMemDB(t)

	op := model.Operator{
		Email:        "alice@example.com",
		PasswordHash: "$2a$12$...",
		IsAdmin:      true,
	}
	require.NoError(t, db.Create(&op).Error)
	require.NotZero(t, op.ID)

	var got model.Operator
	require.NoError(t, db.First(&got, op.ID).Error)
	require.Equal(t, "alice@example.com", got.Email)
	require.True(t, got.IsAdmin)
}

func TestOperatorEmailUnique(t *testing.T) {
	db := openMemDB(t)
	require.NoError(t, db.Create(&model.Operator{Email: "a@x.com", PasswordHash: "x"}).Error)
	err := db.Create(&model.Operator{Email: "a@x.com", PasswordHash: "y"}).Error
	require.Error(t, err)
}

func TestOperatorSessionExpires(t *testing.T) {
	db := openMemDB(t)
	op := model.Operator{Email: "b@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)

	now := time.Now().UTC()
	sess := model.OperatorSession{
		OperatorID: op.ID,
		TokenHash:  "deadbeef",
		IssuedAt:   now,
		ExpiresAt:  now.Add(8 * time.Hour),
		UserAgent:  "quicktun-cli/0.1",
		SourceIP:   "203.0.113.7",
	}
	require.NoError(t, db.Create(&sess).Error)
	require.NotZero(t, sess.ID)
}
```

- [ ] **Step 2: Add testify dependency**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go get github.com/stretchr/testify@v1.10.0
```

- [ ] **Step 3: Run test to verify it fails (model not defined)**

Run: `go test ./internal/model/...`
Expected: compile error referring to `undefined: model.Operator` and `undefined: model.OperatorSession`.

- [ ] **Step 4: Create `internal/model/operator.go`**

```go
package model

import "time"

// Operator is a control-plane user (you / your ops team).
type Operator struct {
	Base
	Email        string `gorm:"uniqueIndex;not null;size:255" json:"email"`
	PasswordHash string `gorm:"not null;size:128" json:"-"`
	IsAdmin      bool   `gorm:"not null;default:false" json:"is_admin"`

	Sessions      []OperatorSession       `gorm:"foreignKey:OperatorID" json:"-"`
	ProjectAccess []OperatorProjectAccess `gorm:"foreignKey:OperatorID" json:"-"`
}

// OperatorSession is one logged-in bearer token (8h default TTL).
// TokenHash stores SHA-256 of the raw token; the raw value is shown
// to the client only at issue time.
type OperatorSession struct {
	Base
	OperatorID uint64     `gorm:"index;not null" json:"operator_id"`
	TokenHash  string     `gorm:"uniqueIndex;not null;size:128" json:"-"`
	IssuedAt   time.Time  `gorm:"not null" json:"issued_at"`
	ExpiresAt  time.Time  `gorm:"index;not null" json:"expires_at"`
	RevokedAt  *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	UserAgent  string     `gorm:"size:255" json:"user_agent"`
	SourceIP   string     `gorm:"size:45" json:"source_ip"`
}
```

- [ ] **Step 5: Update `internal/model/all.go`**

Replace the entire file with:

```go
package model

func AllModels() []any {
	return []any{
		&Operator{},
		&OperatorSession{},
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/model/...`
Expected: PASS.

Note: `OperatorProjectAccess` is referenced from `Operator.ProjectAccess`. GORM tolerates unknown referenced structs at struct-build time, but `AutoMigrate` would fail. We avoid that by not registering `Operator` ProjectAccess relation in test until Project models exist. **If the test fails because of `OperatorProjectAccess` reference**, comment out the `ProjectAccess` field temporarily and add a `// TODO: re-enable in Task 3` note. Re-add it in Task 3.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/model/operator.go internal/model/operator_test.go internal/model/all.go
git commit -m "feat(model): add Operator and OperatorSession"
```

---

## Task 3: Project + OperatorProjectAccess Models

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/model/project.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/model/project_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/all.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/operator.go` (re-enable `ProjectAccess` if commented in Task 2)

- [ ] **Step 1: Write failing test `internal/model/project_test.go`**

```go
package model_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestProjectCreate(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{
		Slug:           "clinic-network",
		Name:           "Clinic Network",
		DefaultMode:    model.SiteModeEndpoint,
		Backend:        model.BackendRathole,
		RelayPortRange: "20000-20999",
		Status:         model.ProjectStatusActive,
	}
	require.NoError(t, db.Create(&p).Error)
	require.NotZero(t, p.ID)
}

func TestProjectSlugUnique(t *testing.T) {
	db := openMemDB(t)
	require.NoError(t, db.Create(&model.Project{Slug: "x", Name: "X", RelayPortRange: "20000-20099"}).Error)
	err := db.Create(&model.Project{Slug: "x", Name: "X2", RelayPortRange: "20100-20199"}).Error
	require.Error(t, err)
}

func TestOperatorProjectAccess(t *testing.T) {
	db := openMemDB(t)

	op := model.Operator{Email: "ops@x.com", PasswordHash: "x"}
	require.NoError(t, db.Create(&op).Error)

	p := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)

	access := model.OperatorProjectAccess{
		OperatorID: op.ID,
		ProjectID:  p.ID,
		Role:       model.RoleOperator,
	}
	require.NoError(t, db.Create(&access).Error)

	// uniqueness: same operator+project combo cannot duplicate
	dup := model.OperatorProjectAccess{OperatorID: op.ID, ProjectID: p.ID, Role: model.RoleViewer}
	require.Error(t, db.Create(&dup).Error)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/...`
Expected: compile error `undefined: model.Project` etc.

- [ ] **Step 3: Create `internal/model/project.go`**

```go
package model

// ProjectStatus enumerates lifecycle states of a Project.
type ProjectStatus string

const (
	ProjectStatusActive   ProjectStatus = "active"
	ProjectStatusDisabled ProjectStatus = "disabled"
)

// Backend selects which relay implementation drives this project.
type Backend string

const (
	BackendRathole Backend = "rathole"
	BackendNetbird Backend = "netbird" // Phase 2
)

// SiteMode controls how a Site exposes services. Phase 1 only ships endpoint.
type SiteMode string

const (
	SiteModeEndpoint SiteMode = "endpoint"
	SiteModeSubnet   SiteMode = "subnet" // Phase 2
)

// ProjectRole grants a level of access to an operator within a project.
type ProjectRole string

const (
	RoleOwner    ProjectRole = "owner"
	RoleOperator ProjectRole = "operator"
	RoleViewer   ProjectRole = "viewer"
)

// Project is a tenancy boundary inside the single-instance control plane.
// Each project gets its own rathole-server process and its own port range.
type Project struct {
	Base
	Slug           string        `gorm:"uniqueIndex;not null;size:64" json:"slug"`
	Name           string        `gorm:"not null;size:255" json:"name"`
	DefaultMode    SiteMode      `gorm:"not null;default:'endpoint';size:32" json:"default_mode"`
	Backend        Backend       `gorm:"not null;default:'rathole';size:32" json:"backend"`
	RelayPortRange string        `gorm:"not null;size:32" json:"relay_port_range"`
	Status         ProjectStatus `gorm:"not null;default:'active';size:32" json:"status"`

	Sites        []Site                  `gorm:"foreignKey:ProjectID" json:"-"`
	AccessGrants []OperatorProjectAccess `gorm:"foreignKey:ProjectID" json:"-"`
}

// OperatorProjectAccess is the many-to-many between operators and projects.
// Composite uniqueness on (operator_id, project_id) is enforced at DB level.
type OperatorProjectAccess struct {
	Base
	OperatorID uint64      `gorm:"uniqueIndex:uk_operator_project;not null" json:"operator_id"`
	ProjectID  uint64      `gorm:"uniqueIndex:uk_operator_project;not null" json:"project_id"`
	Role       ProjectRole `gorm:"not null;default:'operator';size:32" json:"role"`
}
```

- [ ] **Step 4: Update `internal/model/all.go`**

Replace with:

```go
package model

func AllModels() []any {
	return []any{
		&Operator{},
		&OperatorSession{},
		&Project{},
		&OperatorProjectAccess{},
	}
}
```

- [ ] **Step 5: If Operator's `ProjectAccess` field was commented out in Task 2, re-enable it now**

Verify `internal/model/operator.go` has the `ProjectAccess []OperatorProjectAccess` field uncommented.

- [ ] **Step 6: Run tests**

Run: `go test ./internal/model/...`
Expected: PASS.

Note: `Site` is referenced by `Project.Sites` but `Site` doesn't exist yet. Comment out `Sites []Site ...` line in `project.go` with `// TODO: re-enable in Task 4` — re-enable in Task 4. (Same pattern as Task 2 → Task 3.)

- [ ] **Step 7: Commit**

```bash
git add internal/model/project.go internal/model/project_test.go internal/model/all.go internal/model/operator.go
git commit -m "feat(model): add Project and OperatorProjectAccess"
```

---

## Task 4: Site + SiteAgentToken Models

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/model/site.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/model/site_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/all.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/project.go` (re-enable `Sites` if commented)

- [ ] **Step 1: Write failing test `internal/model/site_test.go`**

```go
package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestSiteCreate(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)

	s := model.Site{
		ProjectID:    p.ID,
		Name:         "hospital-shanghai",
		LanCidrsJSON: `["192.168.10.0/24"]`,
		Mode:         model.SiteModeEndpoint,
		Status:       model.SiteStatusPending,
	}
	require.NoError(t, db.Create(&s).Error)
	require.NotZero(t, s.ID)
}

func TestSiteNameUniquePerProject(t *testing.T) {
	db := openMemDB(t)

	p1 := model.Project{Slug: "p1", Name: "P1", RelayPortRange: "20000-20099"}
	p2 := model.Project{Slug: "p2", Name: "P2", RelayPortRange: "20100-20199"}
	require.NoError(t, db.Create(&p1).Error)
	require.NoError(t, db.Create(&p2).Error)

	require.NoError(t, db.Create(&model.Site{ProjectID: p1.ID, Name: "bastion"}).Error)
	// same name in different project = OK
	require.NoError(t, db.Create(&model.Site{ProjectID: p2.ID, Name: "bastion"}).Error)
	// duplicate in same project = fails
	err := db.Create(&model.Site{ProjectID: p1.ID, Name: "bastion"}).Error
	require.Error(t, err)
}

func TestSiteAgentTokenOnePerSite(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b1"}
	require.NoError(t, db.Create(&s).Error)

	exp := time.Now().UTC().Add(24 * time.Hour)
	tok := model.SiteAgentToken{SiteID: s.ID, TokenHash: "h1", ExpiresAt: &exp}
	require.NoError(t, db.Create(&tok).Error)

	dup := model.SiteAgentToken{SiteID: s.ID, TokenHash: "h2"}
	err := db.Create(&dup).Error
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/...`
Expected: compile error `undefined: model.Site` etc.

- [ ] **Step 3: Create `internal/model/site.go`**

```go
package model

import "time"

// SiteStatus tracks runtime liveness of a site (bastion machine).
type SiteStatus string

const (
	SiteStatusPending SiteStatus = "pending" // record exists, agent has not yet registered
	SiteStatusOnline  SiteStatus = "online"  // recent heartbeat
	SiteStatusOffline SiteStatus = "offline" // missed heartbeats past threshold
)

// Site is one bastion machine inside a project. Phase 1 only ships endpoint mode
// (services explicitly listed). Subnet mode (mesh routing peer) is Phase 2.
type Site struct {
	Base
	ProjectID    uint64     `gorm:"index;uniqueIndex:uk_project_site_name;not null" json:"project_id"`
	Name         string     `gorm:"uniqueIndex:uk_project_site_name;not null;size:64" json:"name"`
	LanCidrsJSON string     `gorm:"type:text" json:"lan_cidrs_json"`
	Mode         SiteMode   `gorm:"not null;default:'endpoint';size:32" json:"mode"`
	Backend      Backend    `gorm:"size:32" json:"backend"` // empty = inherit from project
	Status       SiteStatus `gorm:"not null;default:'pending';size:32" json:"status"`
	LastSeenAt   *time.Time `gorm:"index" json:"last_seen_at,omitempty"`
	Hostname     string     `gorm:"size:255" json:"hostname"`
	OS           string     `gorm:"size:32" json:"os"`
	AgentVersion string     `gorm:"size:32" json:"agent_version"`

	Project    Project        `gorm:"foreignKey:ProjectID" json:"-"`
	Services   []Service      `gorm:"foreignKey:SiteID" json:"-"`
	AgentToken SiteAgentToken `gorm:"foreignKey:SiteID" json:"-"`
}

// SiteAgentToken is the long-lived credential each site agent uses to call
// the control plane. One per site (uniqueIndex on SiteID).
type SiteAgentToken struct {
	Base
	SiteID     uint64     `gorm:"uniqueIndex;not null" json:"site_id"`
	TokenHash  string     `gorm:"uniqueIndex;not null;size:128" json:"-"`
	ExpiresAt  *time.Time `gorm:"index" json:"expires_at,omitempty"`
	LastUsedAt *time.Time `gorm:"index" json:"last_used_at,omitempty"`
}
```

- [ ] **Step 4: If `Project.Sites` was commented in Task 3, re-enable it now**

- [ ] **Step 5: Update `internal/model/all.go`**

```go
package model

func AllModels() []any {
	return []any{
		&Operator{},
		&OperatorSession{},
		&Project{},
		&OperatorProjectAccess{},
		&Site{},
		&SiteAgentToken{},
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/model/...`
Expected: PASS.

Note: `Service` is referenced by `Site.Services` but doesn't exist yet. Comment out the `Services []Service` line with `// TODO: re-enable in Task 5` — re-enable in Task 5.

- [ ] **Step 7: Commit**

```bash
git add internal/model/site.go internal/model/site_test.go internal/model/all.go internal/model/project.go
git commit -m "feat(model): add Site and SiteAgentToken"
```

---

## Task 5: Service Model

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/model/service.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/model/service_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/all.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/site.go` (re-enable `Services` if commented)

- [ ] **Step 1: Write failing test `internal/model/service_test.go`**

```go
package model_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestServiceCreate(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "bastion"}
	require.NoError(t, db.Create(&s).Error)

	svc := model.Service{
		SiteID:     s.ID,
		Name:       "ssh",
		TargetAddr: "127.0.0.1",
		TargetPort: 22,
		Proto:      model.ProtoTCP,
		RelayPort:  20022,
	}
	require.NoError(t, db.Create(&svc).Error)
	require.NotZero(t, svc.ID)
}

func TestServiceNameUniquePerSite(t *testing.T) {
	db := openMemDB(t)

	p := model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"}
	require.NoError(t, db.Create(&p).Error)
	s := model.Site{ProjectID: p.ID, Name: "b"}
	require.NoError(t, db.Create(&s).Error)

	require.NoError(t, db.Create(&model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 22, Proto: model.ProtoTCP}).Error)
	err := db.Create(&model.Service{SiteID: s.ID, Name: "ssh", TargetAddr: "127.0.0.1", TargetPort: 23, Proto: model.ProtoTCP}).Error
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/...`
Expected: compile error `undefined: model.Service`.

- [ ] **Step 3: Create `internal/model/service.go`**

```go
package model

// Proto is the wire protocol of a Service. Phase 1 ships TCP only.
type Proto string

const (
	ProtoTCP Proto = "tcp"
	ProtoUDP Proto = "udp" // Phase 2
)

// Service is a TCP target reachable via the bastion. target_addr can be
// 127.0.0.1 (the bastion itself) or any LAN IP the bastion can reach.
//
// RelayPort is allocated by the control plane within the project's port range.
type Service struct {
	Base
	SiteID     uint64 `gorm:"index;uniqueIndex:uk_site_svc_name;not null" json:"site_id"`
	Name       string `gorm:"uniqueIndex:uk_site_svc_name;not null;size:64" json:"name"`
	TargetAddr string `gorm:"not null;size:64" json:"target_addr"`
	TargetPort uint16 `gorm:"not null" json:"target_port"`
	Proto      Proto  `gorm:"not null;default:'tcp';size:8" json:"proto"`
	RelayPort  uint16 `gorm:"index" json:"relay_port"`
}
```

- [ ] **Step 4: If `Site.Services` was commented in Task 4, re-enable it now**

- [ ] **Step 5: Update `internal/model/all.go`**

```go
package model

func AllModels() []any {
	return []any{
		&Operator{},
		&OperatorSession{},
		&Project{},
		&OperatorProjectAccess{},
		&Site{},
		&SiteAgentToken{},
		&Service{},
	}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/model/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/model/service.go internal/model/service_test.go internal/model/all.go internal/model/site.go
git commit -m "feat(model): add Service"
```

---

## Task 6: Audit Log Model

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/model/audit.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/model/audit_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/model/all.go`

- [ ] **Step 1: Write failing test `internal/model/audit_test.go`**

```go
package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/model"
)

func TestAuditLogCreate(t *testing.T) {
	db := openMemDB(t)

	opID := uint64(1)
	pID := uint64(7)
	entry := model.AuditLog{
		Ts:         time.Now().UTC(),
		ProjectID:  &pID,
		OperatorID: &opID,
		Action:     "site.create",
		Target:     "projects/p1/sites/bastion-01",
		SourceIP:   "203.0.113.7",
		ExtraJSON:  `{"foo":"bar"}`,
	}
	require.NoError(t, db.Create(&entry).Error)
	require.NotZero(t, entry.ID)
}

func TestAuditLogAllowsNullActor(t *testing.T) {
	db := openMemDB(t)

	entry := model.AuditLog{
		Ts:     time.Now().UTC(),
		Action: "system.startup",
	}
	require.NoError(t, db.Create(&entry).Error)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/...`
Expected: compile error `undefined: model.AuditLog`.

- [ ] **Step 3: Create `internal/model/audit.go`**

```go
package model

import "time"

// AuditLog records control-plane operator actions and notable system events.
// Append-only; updates and deletes should never happen at the application layer.
//
// ProjectID is nullable for cross-project actions. OperatorID is nullable for
// system-originated entries (e.g., scheduled cleanup).
type AuditLog struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Ts         time.Time `gorm:"index;not null" json:"ts"`
	ProjectID  *uint64   `gorm:"index" json:"project_id,omitempty"`
	OperatorID *uint64   `gorm:"index" json:"operator_id,omitempty"`
	Action     string    `gorm:"index;not null;size:64" json:"action"`
	Target     string    `gorm:"size:255" json:"target"`
	SourceIP   string    `gorm:"size:45" json:"source_ip"`
	ExtraJSON  string    `gorm:"type:text" json:"extra_json"`
}
```

- [ ] **Step 4: Update `internal/model/all.go`**

```go
package model

func AllModels() []any {
	return []any{
		&Operator{},
		&OperatorSession{},
		&Project{},
		&OperatorProjectAccess{},
		&Site{},
		&SiteAgentToken{},
		&Service{},
		&AuditLog{},
	}
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/model/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/model/audit.go internal/model/audit_test.go internal/model/all.go
git commit -m "feat(model): add AuditLog"
```

---

## Task 7: Database Open Helper

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/dao/db.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/dao/db_test.go`

- [ ] **Step 1: Write failing test `internal/dao/db_test.go`**

```go
package dao_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
)

func TestOpenInMemory(t *testing.T) {
	db, err := dao.Open("file::memory:?cache=shared")
	require.NoError(t, err)
	require.NotNil(t, db)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Ping())
}

func TestOpenFileWithWAL(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test.db")
	dsn := "file:" + tmp + "?_journal_mode=WAL&_busy_timeout=5000"

	db, err := dao.Open(dsn)
	require.NoError(t, err)

	var mode string
	require.NoError(t, db.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	require.Equal(t, "wal", mode)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dao/...`
Expected: compile error — package does not exist.

- [ ] **Step 3: Create `internal/dao/db.go`**

```go
package dao

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open establishes a GORM connection to a SQLite database.
//
// Recommended DSN flags:
//
//	file:/path/to/quicktun.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on
//
// WAL gives better read/write concurrency. busy_timeout avoids "database is locked"
// errors under contention. foreign_keys enforces FK constraints (off by default in
// SQLite).
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("dao: open %q: %w", dsn, err)
	}
	return db, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/dao/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dao/db.go internal/dao/db_test.go
git commit -m "feat(dao): add SQLite connection helper"
```

---

## Task 8: SQL Migration Files (0001_init)

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/migrations/0001_init.up.sql`
- Create: `/Users/tulip/project/repos/quicktun/migrations/0001_init.down.sql`

- [ ] **Step 1: Create `migrations/0001_init.up.sql`**

```sql
-- 0001_init.up.sql
-- Phase 1 schema for quicktun control plane.

CREATE TABLE operators (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    email           TEXT    NOT NULL,
    password_hash   TEXT    NOT NULL,
    is_admin        INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    deleted_at      DATETIME
);
CREATE UNIQUE INDEX idx_operators_email   ON operators(email)      WHERE deleted_at IS NULL;
CREATE INDEX        idx_operators_deleted ON operators(deleted_at);

CREATE TABLE operator_sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    operator_id     INTEGER NOT NULL REFERENCES operators(id),
    token_hash      TEXT    NOT NULL,
    issued_at       DATETIME NOT NULL,
    expires_at      DATETIME NOT NULL,
    revoked_at      DATETIME,
    user_agent      TEXT,
    source_ip       TEXT,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    deleted_at      DATETIME
);
CREATE UNIQUE INDEX idx_op_sessions_token   ON operator_sessions(token_hash);
CREATE INDEX        idx_op_sessions_op      ON operator_sessions(operator_id);
CREATE INDEX        idx_op_sessions_expires ON operator_sessions(expires_at);
CREATE INDEX        idx_op_sessions_revoked ON operator_sessions(revoked_at);

CREATE TABLE projects (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    slug              TEXT    NOT NULL,
    name              TEXT    NOT NULL,
    default_mode      TEXT    NOT NULL DEFAULT 'endpoint',
    backend           TEXT    NOT NULL DEFAULT 'rathole',
    relay_port_range  TEXT    NOT NULL,
    status            TEXT    NOT NULL DEFAULT 'active',
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL,
    deleted_at        DATETIME
);
CREATE UNIQUE INDEX idx_projects_slug ON projects(slug) WHERE deleted_at IS NULL;

CREATE TABLE operator_project_access (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    operator_id  INTEGER NOT NULL REFERENCES operators(id),
    project_id   INTEGER NOT NULL REFERENCES projects(id),
    role         TEXT    NOT NULL DEFAULT 'operator',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);
CREATE UNIQUE INDEX uk_operator_project ON operator_project_access(operator_id, project_id) WHERE deleted_at IS NULL;

CREATE TABLE sites (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id       INTEGER NOT NULL REFERENCES projects(id),
    name             TEXT    NOT NULL,
    lan_cidrs_json   TEXT,
    mode             TEXT    NOT NULL DEFAULT 'endpoint',
    backend          TEXT,
    status           TEXT    NOT NULL DEFAULT 'pending',
    last_seen_at     DATETIME,
    hostname         TEXT,
    os               TEXT,
    agent_version    TEXT,
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    deleted_at       DATETIME
);
CREATE UNIQUE INDEX uk_project_site_name ON sites(project_id, name) WHERE deleted_at IS NULL;
CREATE INDEX        idx_sites_last_seen  ON sites(last_seen_at);

CREATE TABLE services (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id      INTEGER NOT NULL REFERENCES sites(id),
    name         TEXT    NOT NULL,
    target_addr  TEXT    NOT NULL,
    target_port  INTEGER NOT NULL,
    proto        TEXT    NOT NULL DEFAULT 'tcp',
    relay_port   INTEGER,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);
CREATE UNIQUE INDEX uk_site_svc_name ON services(site_id, name) WHERE deleted_at IS NULL;
CREATE INDEX        idx_services_relay ON services(relay_port);

CREATE TABLE site_agent_tokens (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id       INTEGER NOT NULL REFERENCES sites(id),
    token_hash    TEXT    NOT NULL,
    expires_at    DATETIME,
    last_used_at  DATETIME,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    deleted_at    DATETIME
);
CREATE UNIQUE INDEX uk_site_agent_site  ON site_agent_tokens(site_id)    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uk_site_agent_token ON site_agent_tokens(token_hash) WHERE deleted_at IS NULL;
CREATE INDEX        idx_site_agent_exp  ON site_agent_tokens(expires_at);
CREATE INDEX        idx_site_agent_used ON site_agent_tokens(last_used_at);

CREATE TABLE audit_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            DATETIME NOT NULL,
    project_id    INTEGER,
    operator_id   INTEGER,
    action        TEXT    NOT NULL,
    target        TEXT,
    source_ip     TEXT,
    extra_json    TEXT
);
CREATE INDEX idx_audit_ts        ON audit_logs(ts);
CREATE INDEX idx_audit_project   ON audit_logs(project_id);
CREATE INDEX idx_audit_operator  ON audit_logs(operator_id);
CREATE INDEX idx_audit_action    ON audit_logs(action);
```

- [ ] **Step 2: Create `migrations/0001_init.down.sql`**

```sql
-- 0001_init.down.sql
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS site_agent_tokens;
DROP TABLE IF EXISTS services;
DROP TABLE IF EXISTS sites;
DROP TABLE IF EXISTS operator_project_access;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS operator_sessions;
DROP TABLE IF EXISTS operators;
```

- [ ] **Step 3: Commit**

```bash
git add migrations/0001_init.up.sql migrations/0001_init.down.sql
git commit -m "feat(migration): add 0001 initial schema"
```

---

## Task 9: Migration Runner (Embedded)

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/migration/embed.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/migration/migrate.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/migration/migrate_test.go`

- [ ] **Step 1: Add migration dependency**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go get github.com/golang-migrate/migrate/v4@v4.18.1
go get github.com/golang-migrate/migrate/v4/database/sqlite3@v4.18.1
go get github.com/golang-migrate/migrate/v4/source/iofs@v4.18.1
```

- [ ] **Step 2: Write failing test `internal/migration/migrate_test.go`**

```go
package migration_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/migration"
)

func TestUpAppliesAllTables(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "qt.db")
	dsn := "file:" + tmp + "?_foreign_keys=on"

	require.NoError(t, migration.Up(dsn))

	db, err := dao.Open(dsn)
	require.NoError(t, err)

	wanted := []string{
		"operators", "operator_sessions",
		"projects", "operator_project_access",
		"sites", "services", "site_agent_tokens",
		"audit_logs",
	}
	for _, table := range wanted {
		var count int
		require.NoError(t, db.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count).Error)
		require.Equal(t, 1, count, "expected table %q to exist", table)
	}
}

func TestUpIsIdempotent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "qt.db")
	dsn := "file:" + tmp

	require.NoError(t, migration.Up(dsn))
	require.NoError(t, migration.Up(dsn)) // second call should be no-op
}

func TestDownDropsAllTables(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "qt.db")
	dsn := "file:" + tmp

	require.NoError(t, migration.Up(dsn))
	require.NoError(t, migration.Down(dsn))

	db, err := dao.Open(dsn)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='operators'",
	).Scan(&count).Error)
	require.Equal(t, 0, count)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/migration/...`
Expected: compile error — package does not exist.

- [ ] **Step 4: Create `internal/migration/embed.go`**

```go
package migration

import "embed"

//go:embed all:files
var migrationFS embed.FS
```

- [ ] **Step 5: Create the embed mirror directory**

`go:embed` cannot reach outside the package directory, so we mirror the migration files into `internal/migration/files/`.

Run:
```bash
cd /Users/tulip/project/repos/quicktun
mkdir -p internal/migration/files
cp migrations/*.sql internal/migration/files/
```

Add a Makefile target so this stays in sync (modify `Makefile`):

Insert after the `lint:` target:

```makefile
sync-migrations:
	@cp migrations/*.sql internal/migration/files/
```

And add it as a build dependency:

```makefile
$(SERVER_BIN): sync-migrations
	@mkdir -p $(BINDIR)
	$(GO) build -o $@ ./cmd/quicktun-server
```

(So `make build` always copies fresh SQL into the embed mirror.)

- [ ] **Step 6: Create `internal/migration/migrate.go`**

```go
// Package migration runs SQL schema migrations against the control plane DB.
//
// Migration files live at /migrations/*.sql in the repo root. They are mirrored
// into internal/migration/files/ at build time so the binary can embed them
// (//go:embed cannot reach outside the package directory).
package migration

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Up applies every pending migration against the SQLite database at dsn.
// Idempotent: returns nil if no migrations are pending.
//
// dsn is in golang-migrate sqlite3 form, e.g. "file:/path/to/qt.db" — without
// gorm's PRAGMA query string. Translation from a gorm DSN happens in the caller.
func Up(dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration: up: %w", err)
	}
	return nil
}

// Down rolls back every applied migration, leaving an empty database.
// Intended for tests and disaster recovery; never called in normal operation.
func Down(dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration: down: %w", err)
	}
	return nil
}

func newMigrator(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrationFS, "files")
	if err != nil {
		return nil, fmt.Errorf("migration: load source: %w", err)
	}
	// golang-migrate expects the sqlite3:// URL scheme.
	target := "sqlite3://" + dsn
	m, err := migrate.NewWithSourceInstance("iofs", src, target)
	if err != nil {
		return nil, fmt.Errorf("migration: connect %q: %w", target, err)
	}
	return m, nil
}

func closeMigrator(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	_ = srcErr
	_ = dbErr
}
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/migration/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum Makefile internal/migration/
git commit -m "feat(migration): embed SQL files and run via golang-migrate"
```

---

## Task 10: Config Loader

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/config/config.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/config/config_test.go`
- Create: `/Users/tulip/project/repos/quicktun/etc/server.example.yaml`

- [ ] **Step 1: Add Viper dependency**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go get github.com/spf13/viper@v1.20.0
```

- [ ] **Step 2: Write failing test `internal/config/config_test.go`**

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/config"
)

func TestLoadFromYAML(t *testing.T) {
	yaml := `
control_plane:
  grpc_listen: 0.0.0.0:9443
  http_listen: 0.0.0.0:9080

database:
  driver: sqlite
  dsn: /tmp/qt.db?_journal_mode=WAL

session:
  default_ttl: 8h

log:
  path: /tmp/qt.log
  level: info
  max_size_mb: 100
  max_age_days: 30
  max_backups: 7
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9443", cfg.ControlPlane.GRPCListen)
	require.Equal(t, "sqlite", cfg.Database.Driver)
	require.Equal(t, "/tmp/qt.db?_journal_mode=WAL", cfg.Database.DSN)
	require.Equal(t, "8h", cfg.Session.DefaultTTL.String())
	require.Equal(t, "info", cfg.Log.Level)
}

func TestLoadAppliesDefaults(t *testing.T) {
	// Minimal config — only DSN required.
	yaml := `
database:
  dsn: ":memory:"
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9443", cfg.ControlPlane.GRPCListen)
	require.Equal(t, "info", cfg.Log.Level)
	require.Equal(t, "8h", cfg.Session.DefaultTTL.String())
}

func TestLoadFailsOnMissingFile(t *testing.T) {
	_, err := config.Load("/no/such/file.yaml")
	require.Error(t, err)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: compile error.

- [ ] **Step 4: Create `internal/config/config.go`**

```go
// Package config loads quicktun-server configuration from YAML.
//
// Config schema is defined by the Config struct; example:
//
//	control_plane:
//	  grpc_listen: 0.0.0.0:9443
//	  http_listen: 0.0.0.0:9080
//	database:
//	  driver: sqlite
//	  dsn: /opt/quicktun/var/quicktun.db?_journal_mode=WAL
//	session:
//	  default_ttl: 8h
//	log:
//	  path: /opt/quicktun/var/log/server.log
//	  level: info
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config is the full server configuration tree.
type Config struct {
	ControlPlane ControlPlaneConfig `mapstructure:"control_plane"`
	Database     DatabaseConfig     `mapstructure:"database"`
	Session      SessionConfig      `mapstructure:"session"`
	Log          LogConfig          `mapstructure:"log"`
}

// ControlPlaneConfig holds gRPC + grpc-gateway listener settings.
type ControlPlaneConfig struct {
	GRPCListen string `mapstructure:"grpc_listen"`
	HTTPListen string `mapstructure:"http_listen"`
}

// DatabaseConfig describes the persistence layer.
// Phase 1 only ships sqlite; Postgres support is Phase 3+.
type DatabaseConfig struct {
	Driver string `mapstructure:"driver"`
	DSN    string `mapstructure:"dsn"`
}

// SessionConfig controls operator login token lifetime.
type SessionConfig struct {
	DefaultTTL time.Duration `mapstructure:"default_ttl"`
}

// LogConfig sets up zap output destination and rotation policy.
type LogConfig struct {
	Path       string `mapstructure:"path"`
	Level      string `mapstructure:"level"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	MaxBackups int    `mapstructure:"max_backups"`
}

// Load reads YAML from path and applies defaults for missing keys.
// Returns an error if the file cannot be read or parsed.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("control_plane.grpc_listen", "0.0.0.0:9443")
	v.SetDefault("control_plane.http_listen", "0.0.0.0:9080")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("session.default_ttl", "8h")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.max_backups", 7)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/config/...`
Expected: PASS.

- [ ] **Step 6: Create `etc/server.example.yaml`**

```yaml
# quicktun-server example configuration.
# Copy to etc/server.yaml and edit for your environment.

control_plane:
  grpc_listen: 0.0.0.0:9443
  http_listen: 0.0.0.0:9080

database:
  driver: sqlite
  dsn: /opt/quicktun/var/quicktun.db?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on

session:
  default_ttl: 8h

log:
  path: /opt/quicktun/var/log/control-plane.log
  level: info
  max_size_mb: 100
  max_age_days: 30
  max_backups: 7
```

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/config/ etc/server.example.yaml
git commit -m "feat(config): add YAML config loader with defaults"
```

---

## Task 11: Logger (zap + lumberjack)

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/logger/logger.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/logger/logger_test.go`

- [ ] **Step 1: Add deps**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go get go.uber.org/zap@v1.27.0
go get gopkg.in/natefinch/lumberjack.v2@v2.2.1
```

- [ ] **Step 2: Write failing test `internal/logger/logger_test.go`**

```go
package logger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/logger"
)

func TestNewWritesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	lg, err := logger.New(config.LogConfig{
		Path:       path,
		Level:      "info",
		MaxSizeMB:  10,
		MaxAgeDays: 1,
		MaxBackups: 1,
	})
	require.NoError(t, err)

	lg.Info("hello", logger.String("k", "v"))
	require.NoError(t, lg.Sync())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"hello"`)
	require.Contains(t, out, `"k":"v"`)
}

func TestNewWithStdoutWhenPathEmpty(t *testing.T) {
	lg, err := logger.New(config.LogConfig{Level: "debug"})
	require.NoError(t, err)
	require.NotNil(t, lg)
	require.True(t, lg.Core().Enabled(-1)) // debug level
	require.False(t, strings.Contains("", "no-op"))
}

func TestNewRejectsBadLevel(t *testing.T) {
	_, err := logger.New(config.LogConfig{Level: "screaming"})
	require.Error(t, err)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/logger/...`
Expected: compile error.

- [ ] **Step 4: Create `internal/logger/logger.go`**

```go
// Package logger constructs a zap.Logger using the application's LogConfig.
//
// When LogConfig.Path is set, output is rotated by lumberjack. When empty,
// logs go to stdout (useful in tests and dev).
package logger

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/tulip/quicktun/internal/config"
)

// String wraps zap.String so callers don't import zap directly. Add more
// helpers (Int, Error, Duration, ...) as the surface grows.
func String(k, v string) zap.Field { return zap.String(k, v) }

// New builds a production zap.Logger from the given LogConfig.
func New(cfg config.LogConfig) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("logger: parse level %q: %w", cfg.Level, err)
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	enc := zapcore.NewJSONEncoder(encCfg)

	var sink zapcore.WriteSyncer
	if cfg.Path == "" {
		sink = zapcore.AddSync(os.Stdout)
	} else {
		sink = zapcore.AddSync(&lumberjack.Logger{
			Filename:   cfg.Path,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   true,
		})
	}

	core := zapcore.NewCore(enc, sink, level)
	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/logger/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/logger/
git commit -m "feat(logger): add zap+lumberjack initialiser"
```

---

## Task 12: Server Binary — Cobra Skeleton + Version

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/main.go`
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_version.go`

- [ ] **Step 1: Add Cobra dependency**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
go get github.com/spf13/cobra@v1.8.1
```

- [ ] **Step 2: Create `cmd/quicktun-server/main.go`**

```go
// quicktun-server is the control-plane binary.
//
// Subcommands:
//
//	migrate   apply pending SQL migrations
//	version   print build version and exit
//
// Future subcommands (Phase 1 milestones):
//
//	serve     run the gRPC + grpc-gateway server
//	admin     create / list operators
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "quicktun-server",
		Short: "quicktun control plane server",
	}

	root.PersistentFlags().String("config", "etc/server.yaml", "path to YAML config")

	root.AddCommand(versionCmd())
	root.AddCommand(migrateCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Create `cmd/quicktun-server/cmd_version.go`**

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("quicktun-server", version)
		},
	}
}
```

- [ ] **Step 4: Stub `migrateCmd()` (will be implemented in Task 13)**

Edit `cmd/quicktun-server/main.go` is fine, but to keep Task 12 self-contained, also add a temporary file:

Create `cmd/quicktun-server/cmd_migrate.go`:

```go
package main

import "github.com/spf13/cobra"

// migrateCmd is fleshed out in Task 13.
func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending SQL migrations",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println("not yet implemented")
		},
	}
}
```

- [ ] **Step 5: Build and run version**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
make build
./bin/quicktun-server version
```

Expected output: `quicktun-server dev`

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum cmd/quicktun-server/
git commit -m "feat(cmd): add quicktun-server cobra skeleton with version"
```

---

## Task 13: `quicktun-server migrate` Subcommand

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_migrate.go`
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_migrate_test.go`

- [ ] **Step 1: Write failing test `cmd/quicktun-server/cmd_migrate_test.go`**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
)

func TestMigrateCmdAppliesSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qt.db")
	cfgPath := filepath.Join(dir, "server.yaml")

	yaml := `
database:
  driver: sqlite
  dsn: ` + dbPath + `?_foreign_keys=on
log:
  level: error
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o600))

	// Wire migrate under a fake root so it inherits the persistent --config flag
	// the same way it does in production.
	root := &cobra.Command{Use: "test-root"}
	root.PersistentFlags().String("config", cfgPath, "path to YAML config")
	cmd := migrateCmd()
	root.AddCommand(cmd)

	require.NoError(t, cmd.RunE(cmd, nil))

	db, err := dao.Open(dbPath)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='operators'",
	).Scan(&n).Error)
	require.Equal(t, 1, n)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/quicktun-server/...`
Expected: test fails (current `migrateCmd` is a stub printing "not yet implemented" and lacks `RunE`).

- [ ] **Step 3: Replace `cmd/quicktun-server/cmd_migrate.go`**

```go
package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/migration"
)

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending SQL migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reads the persistent --config flag inherited from the root command.
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("migrate: load config: %w", err)
			}
			if cfg.Database.Driver != "sqlite" {
				return fmt.Errorf("migrate: only sqlite driver supported in Phase 1, got %q", cfg.Database.Driver)
			}
			if err := migration.Up(cfg.Database.DSN); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/quicktun-server/...`
Expected: PASS.

- [ ] **Step 5: End-to-end smoke test from a shell**

Run:
```bash
cd /Users/tulip/project/repos/quicktun
make build
mkdir -p var
cat > etc/server.yaml <<'EOF'
database:
  driver: sqlite
  dsn: var/quicktun.db?_journal_mode=WAL&_foreign_keys=on
log:
  level: info
EOF
./bin/quicktun-server migrate --config etc/server.yaml
ls -la var/quicktun.db
sqlite3 var/quicktun.db ".tables"
```

Expected: `var/quicktun.db` exists; `.tables` lists `operators projects sites services ...` plus `schema_migrations`.

- [ ] **Step 6: Commit**

```bash
git add cmd/quicktun-server/cmd_migrate.go cmd/quicktun-server/cmd_migrate_test.go
git commit -m "feat(cmd): wire migrate subcommand to migration.Up"
```

---

## Task 14: Smoke `make test` + `go vet`

**Files:** none new.

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 2: Run race detector**

Run: `go test -race ./...`
Expected: PASS.

- [ ] **Step 3: Run `go vet`**

Run: `go vet ./...`
Expected: no diagnostics.

- [ ] **Step 4: Verify migrations stay in sync with embed mirror**

Run:
```bash
make sync-migrations
git diff --exit-code internal/migration/files
```

Expected: exit 0 (no diff). If diff appears, commit the embed mirror update.

- [ ] **Step 5: Final commit if anything was missed**

```bash
git status
# if clean, no commit needed
```

---

## Task 15: README Stub

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/README.md`

- [ ] **Step 1: Create `README.md`**

```markdown
# quicktun

Multi-site remote access control plane. Replaces TeamViewer/ToDesk-style screen sharing for managing many small project networks; each network gets a bastion machine running the quicktun agent and is reachable via SSH / RDP / AI tools through a central relay.

> Phase 1 is bastion + reverse-proxy (rathole). Phase 2 adds NetBird mesh. See `docs/`.

## Status

Active development. Phase 1 foundation complete.

## Quick start (developers)

```bash
git clone <repo>
cd quicktun
make build
mkdir -p var
cp etc/server.example.yaml etc/server.yaml
# edit etc/server.yaml — at minimum set database.dsn
./bin/quicktun-server migrate --config etc/server.yaml
```

## Design docs

| Doc | Topic |
|-----|-------|
| [docs/00-overview.md](docs/00-overview.md) | Product framing + key decisions |
| [docs/01-data-model.md](docs/01-data-model.md) | GORM models + ER + migrations |
| [docs/02-grpc-api.md](docs/02-grpc-api.md) | gRPC + grpc-gateway (Google AIP) |
| [docs/03-agent-protocol.md](docs/03-agent-protocol.md) | Site agent ↔ control plane |
| [docs/04-security.md](docs/04-security.md) | Network admission + auth-proxy |
| [docs/05-process-supervisor.md](docs/05-process-supervisor.md) | rathole / auth-proxy lifecycle |
| [docs/06-cli.md](docs/06-cli.md) | Operator CLI |
| [docs/07-deployment.md](docs/07-deployment.md) | Server + agent deployment |
| [docs/08-roadmap.md](docs/08-roadmap.md) | Milestones |

## License

TBD.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README"
```

---

## Self-Review

After all tasks land, verify:

1. `make build` produces `bin/quicktun-server` with no warnings.
2. `make test` is green.
3. `make test-race` is green.
4. `bin/quicktun-server version` prints `quicktun-server dev`.
5. `bin/quicktun-server migrate --config etc/server.yaml` materializes a SQLite DB with all 8 application tables + `schema_migrations`.
6. `git log --oneline` shows ≥15 small commits, one per logical step.

If any of these fail, the offending task's "Run" step should have caught it — diagnose and patch in place rather than stacking new tasks.
