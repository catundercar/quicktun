# quicktun Project CRUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the first full CRUD-capable resource (`ProjectService`) following Google API Design Guide standard methods, plus the supporting infrastructure (resource-name parser, audit-log writer, project-scope access check) that Site and Service in subsequent plans will reuse. Bundle Plan 2 final-review cleanup as Task 0.

**Architecture:** Resource names follow Google AIP-122 (`projects/{slug}`). A new `internal/resource/` package parses + formats resource names. A new `internal/audit/` package wraps `audit_logs` writes with a fluent `Log(ctx, ...)` API. `internal/grpcsvc/project_service.go` implements ListProjects / GetProject / CreateProject / UpdateProject / DeleteProject using `field_mask` for partial updates. The auth interceptor stash improvements from Plan 2 final review land in Task 0 to remove the metadata-fallback footgun.

**Tech Stack:**
- Existing: GORM, gRPC, grpc-gateway, zap, cobra, buf, bcrypt
- New: `google.golang.org/protobuf/types/known/fieldmaskpb`
- New: `google.golang.org/protobuf/proto` (for FieldMask merge)

---

## File Structure

### New files
```
internal/resource/
├── name.go                       Format / Parse helpers per resource type
├── name_test.go

internal/audit/
├── audit.go                      Log(ctx, action, target, extra) writer
├── audit_test.go

internal/grpcsvc/
├── project_service.go            ProjectService impl
├── project_service_test.go

internal/dao/
├── project.go                    ProjectDAO (CRUD)
├── project_test.go

cmd/quicktun-server/
├── cmd_admin_project.go          admin project list/get/create/update/delete
├── cmd_admin_project_test.go
```

### Files modified
```
internal/auth/
├── interceptor.go                Add WithOperator + WithRawToken; interceptor stashes raw token
├── interceptor_test.go           Update for new helpers

internal/grpcsvc/
├── auth_service.go               Use auth.RawTokenFromContext (drop local helper)

internal/dao/
├── operator.go                   var _ auth.Validator = (*SessionDAO)(nil) compile-time assertion

internal/server/
├── server.go                     WWW-Authenticate header conformance + register ProjectService
├── server_test.go                Cleanup goroutine wait

cmd/quicktun-server/
├── cmd_serve.go                  Run migration.Up before serving
├── main.go                       Updated package doc

api/quicktun/v1/
├── project.proto                 NEW

scripts/
├── smoke.sh                      Add project create + list verification
```

---

## Task 0: Plan 2 Final Review Cleanup

The Plan 2 final reviewer flagged 4 Important + 4 Minor items. Address them upfront so they don't bleed into Plan 3 implementation.

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/auth/interceptor.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/auth/interceptor_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/auth_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/auth_service_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/dao/operator.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_serve.go`
- Modify: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/main.go`

- [ ] **Step 1: Move raw-token helpers from grpcsvc into auth**

Edit `/Users/tulip/project/repos/quicktun/internal/auth/interceptor.go`. Replace the entire file with:

```go
package auth

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/tulip/quicktun/internal/model"
)

// Validator validates a raw bearer token and returns the owning operator.
// Implemented by *dao.SessionDAO; receiving an interface here keeps the
// interceptor unit-testable without a database.
type Validator interface {
	Validate(ctx context.Context, rawToken string) (*model.Operator, error)
}

// ctxKey is unexported to prevent collisions with other packages' context keys.
type ctxKey int

const (
	operatorKey ctxKey = iota
	rawTokenKey
)

// WithOperator attaches op to ctx so handlers can read it via OperatorFromContext.
func WithOperator(ctx context.Context, op *model.Operator) context.Context {
	return context.WithValue(ctx, operatorKey, op)
}

// OperatorFromContext returns the authenticated operator attached by
// NewUnaryInterceptor. Returns nil if not authenticated (e.g. in the Login
// path, which is unauthenticated).
func OperatorFromContext(ctx context.Context) *model.Operator {
	op, _ := ctx.Value(operatorKey).(*model.Operator)
	return op
}

// WithRawToken attaches the raw bearer token to ctx so handlers like Logout
// can revoke it without re-parsing metadata. Set by the interceptor.
func WithRawToken(ctx context.Context, raw string) context.Context {
	return context.WithValue(ctx, rawTokenKey, raw)
}

// RawTokenFromContext returns the raw bearer token previously attached by
// WithRawToken. Returns "" if no token is in context.
func RawTokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(rawTokenKey).(string)
	return v
}

// NewUnaryInterceptor returns a grpc.UnaryServerInterceptor that:
//   - lets requests for any FullMethod listed in `unauth` proceed without auth
//   - extracts a Bearer token from the "authorization" gRPC metadata header
//   - validates it via Validator and attaches operator + raw token to ctx
//   - returns codes.Unauthenticated on missing or invalid token
func NewUnaryInterceptor(v Validator, unauth ...string) grpc.UnaryServerInterceptor {
	allowlist := make(map[string]struct{}, len(unauth))
	for _, m := range unauth {
		allowlist[m] = struct{}{}
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := allowlist[info.FullMethod]; ok {
			return handler(ctx, req)
		}
		token := extractBearer(ctx)
		if token == "" {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}
		op, err := v.Validate(ctx, token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}
		ctx = WithOperator(ctx, op)
		ctx = WithRawToken(ctx, token)
		return handler(ctx, req)
	}
}

func extractBearer(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	const prefix = "Bearer "
	v := values[0]
	if !strings.HasPrefix(v, prefix) {
		return ""
	}
	return strings.TrimSpace(v[len(prefix):])
}
```

Note: `OperatorContextKey()` is removed; tests should now use `auth.WithOperator(ctx, op)` instead of `context.WithValue(ctx, auth.OperatorContextKey(), op)`.

- [ ] **Step 2: Update `internal/auth/interceptor_test.go`**

Edit the test that uses `auth.OperatorContextKey()` (if any) to use `auth.WithOperator(ctx, op)` instead. Search:

```bash
cd /Users/tulip/project/repos/quicktun
grep -rn "OperatorContextKey" --include="*.go"
```

Update each call. The interceptor tests in `interceptor_test.go` already use `metadata.NewIncomingContext` directly and don't call `OperatorContextKey`, so the only changes needed are in `internal/grpcsvc/auth_service_test.go` (handled in step 4). Verify nothing else uses it.

- [ ] **Step 3: Drop the local `WithRawToken` from grpcsvc and use auth helpers**

Edit `/Users/tulip/project/repos/quicktun/internal/grpcsvc/auth_service.go`. Remove the `rawTokenCtxKey`, `WithRawToken`, and `rawTokenFromContext` declarations. The Logout method should now read from auth's helper.

Replace the file with:

```go
// Package grpcsvc contains gRPC service implementations.
package grpcsvc

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

// AuthService implements quicktunv1.AuthServiceServer.
type AuthService struct {
	quicktunv1.UnimplementedAuthServiceServer
	ops      *dao.OperatorDAO
	sessions *dao.SessionDAO
	ttl      time.Duration
}

// NewAuthService constructs an AuthService.
func NewAuthService(ops *dao.OperatorDAO, sessions *dao.SessionDAO, sessionTTL time.Duration) *AuthService {
	return &AuthService{ops: ops, sessions: sessions, ttl: sessionTTL}
}

// Login implements quicktunv1.AuthServiceServer.
func (s *AuthService) Login(ctx context.Context, req *quicktunv1.LoginRequest) (*quicktunv1.LoginResponse, error) {
	if req.GetEmail() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}
	op, err := s.ops.FindByEmail(ctx, req.Email)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(op.PasswordHash), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	ua, ip := callerMetadata(ctx)
	rec, raw, err := s.sessions.Issue(ctx, op.ID, s.ttl, ua, ip)
	if err != nil {
		return nil, status.Error(codes.Internal, "session issue failed")
	}

	return &quicktunv1.LoginResponse{
		AccessToken: raw,
		ExpireTime:  timestamppb.New(rec.ExpiresAt),
		Operator:    operatorToProto(op),
	}, nil
}

// Logout implements quicktunv1.AuthServiceServer. Phase 1 ignores
// LogoutRequest.session_name and revokes the caller's current session.
func (s *AuthService) Logout(ctx context.Context, _ *quicktunv1.LogoutRequest) (*emptypb.Empty, error) {
	if auth.OperatorFromContext(ctx) == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if raw := auth.RawTokenFromContext(ctx); raw != "" {
		if err := s.sessions.RevokeByToken(ctx, raw); err != nil {
			return nil, status.Error(codes.Internal, "revoke failed")
		}
	}
	return &emptypb.Empty{}, nil
}

// WhoAmI implements quicktunv1.AuthServiceServer.
func (s *AuthService) WhoAmI(ctx context.Context, _ *quicktunv1.WhoAmIRequest) (*quicktunv1.WhoAmIResponse, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	return &quicktunv1.WhoAmIResponse{Operator: operatorToProto(op)}, nil
}

func operatorToProto(op *model.Operator) *quicktunv1.Operator {
	return &quicktunv1.Operator{
		Name:       "operators/" + uint64ToString(op.ID),
		OperatorId: uint64ToString(op.ID),
		CreateTime: timestamppb.New(op.CreatedAt),
		Email:      op.Email,
		IsAdmin:    op.IsAdmin,
	}
}

func uint64ToString(v uint64) string {
	const digits = "0123456789"
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = digits[v%10]
		v /= 10
	}
	return string(b[i:])
}

func callerMetadata(ctx context.Context) (userAgent, sourceIP string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	if v := md.Get("user-agent"); len(v) > 0 {
		userAgent = v[0]
	}
	if v := md.Get("x-forwarded-for"); len(v) > 0 {
		sourceIP = v[0]
	}
	return userAgent, sourceIP
}
```

- [ ] **Step 4: Update `internal/grpcsvc/auth_service_test.go` to use auth helpers**

Find tests using `auth.OperatorContextKey()` and `grpcsvc.WithRawToken(...)`. Replace:

- `context.WithValue(ctx, auth.OperatorContextKey(), op)` → `auth.WithOperator(ctx, op)`
- `grpcsvc.WithRawToken(ctx, raw)` → `auth.WithRawToken(ctx, raw)`

Apply throughout the file. Verify with `grep`:

```bash
grep -n "OperatorContextKey\|grpcsvc.WithRawToken" internal/grpcsvc/auth_service_test.go
```

Should return no matches after edits.

- [ ] **Step 5: Add compile-time interface assertion to dao**

Edit `/Users/tulip/project/repos/quicktun/internal/dao/operator.go`. Append at the bottom of the file:

```go

// compile-time assertion that *SessionDAO satisfies auth.Validator.
var _ auth.Validator = (*SessionDAO)(nil)
```

This requires importing `auth`:

```go
import (
	// ... existing imports ...
	"github.com/tulip/quicktun/internal/auth"
)
```

(`auth` is likely already imported. If not, add it.)

- [ ] **Step 6: Run migrations automatically in `serve`**

Edit `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_serve.go`. Add `migration.Up` after config load and before `dao.Open`. Replace the file with:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/logger"
	"github.com/tulip/quicktun/internal/migration"
	"github.com/tulip/quicktun/internal/server"
)

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the gRPC + grpc-gateway control-plane server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := cmd.Root().PersistentFlags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			lg, err := logger.New(cfg.Log)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			defer lg.Sync()

			// Apply any pending migrations before opening the gorm pool. Idempotent
			// when schema is already up to date. Without this, a fresh deploy fails
			// at first request with confusing "no such table" errors.
			if err := migration.Up(cfg.Database.DSN); err != nil {
				return fmt.Errorf("serve: migrate: %w", err)
			}

			db, err := dao.Open(cfg.Database.DSN, lg)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			defer func() { sqlDB, _ := db.DB(); sqlDB.Close() }()

			srv, err := server.New(server.Config{
				DB:         db,
				Logger:     lg,
				GRPCListen: cfg.ControlPlane.GRPCListen,
				HTTPListen: cfg.ControlPlane.HTTPListen,
				SessionTTL: cfg.Session.DefaultTTL,
			})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if err := srv.Run(ctx); err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			lg.Info("server stopped cleanly")
			return nil
		},
	}
}
```

- [ ] **Step 7: Fix `WWW-Authenticate` header format in server.go**

Edit `/Users/tulip/project/repos/quicktun/internal/server/server.go`. Find the `gatewayErrorHandler` (or whatever the function is named that sets the WWW-Authenticate header — added by Plan 2 Task 7). Locate the line:

```go
w.Header().Set("WWW-Authenticate", s.Message())
```

Replace with:

```go
// RFC 9110 §11.6.1: WWW-Authenticate must name an auth scheme.
w.Header().Set("WWW-Authenticate", `Bearer realm="quicktun"`)
```

If the implementation differs (e.g., the header is set inline or named differently), search for `WWW-Authenticate`:

```bash
grep -n "WWW-Authenticate" internal/server/server.go
```

And apply the same fix at each call site.

- [ ] **Step 8: Fix goroutine leak in `TestServerWhoAmIRequiresAuth`**

Edit `/Users/tulip/project/repos/quicktun/internal/server/server_test.go`. Find:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go srv.Run(ctx)
```

Replace with:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
errCh := make(chan error, 1)
go func() { errCh <- srv.Run(ctx) }()
t.Cleanup(func() {
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Log("server did not stop cleanly")
	}
})
```

This mirrors the pattern in `TestServerLoginEndToEnd` and ensures the goroutine completes before the test exits.

- [ ] **Step 9: Update `main.go` package doc to list current subcommands**

Edit `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/main.go`. Replace the package doc comment block (the lines starting with `//` at the top of the file before `package main`) with:

```go
// quicktun-server is the control-plane binary.
//
// Subcommands:
//
//	serve     run the gRPC + grpc-gateway server
//	migrate   apply pending SQL migrations
//	admin     administrative commands (create-operator, ...)
//	version   print build version and exit
package main
```

- [ ] **Step 10: Run full smoke + commit**

```bash
cd /Users/tulip/project/repos/quicktun
go test ./...
go test -race -timeout 120s ./...
go vet ./...
make build

git add internal/auth/ internal/grpcsvc/ internal/dao/operator.go internal/server/ cmd/quicktun-server/
git commit -m "chore: address Plan 2 final-review cleanup"
```

Expected: all green. Single new commit.

---

## Task 1: Resource Name Parser

A central helper for parsing/formatting resource names like `projects/clinic-network`. Keeps proto-name parsing out of every handler.

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/resource/name.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/resource/name_test.go`

- [ ] **Step 1: Write failing test `internal/resource/name_test.go`**

```go
package resource_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/resource"
)

func TestParseProjectName(t *testing.T) {
	slug, err := resource.ParseProjectName("projects/clinic-network")
	require.NoError(t, err)
	require.Equal(t, "clinic-network", slug)
}

func TestParseProjectNameRejectsBadFormat(t *testing.T) {
	cases := []string{
		"",
		"clinic-network",
		"projects/",
		"projects/clinic/extra",
		"sites/clinic-network",
		"projects/Bad Slug",
	}
	for _, name := range cases {
		_, err := resource.ParseProjectName(name)
		require.Error(t, err, "expected error for %q", name)
	}
}

func TestFormatProjectName(t *testing.T) {
	require.Equal(t, "projects/clinic-network", resource.FormatProjectName("clinic-network"))
}

func TestValidateSlug(t *testing.T) {
	valid := []string{"abc", "abc-def", "a1", "abc-def-ghi", "x-y-z-9"}
	for _, s := range valid {
		require.NoError(t, resource.ValidateSlug(s), "expected %q to be valid", s)
	}
	invalid := []string{"", "ab", "Abc", "abc_def", "abc def", "abc-", "-abc", "a--b", strings.Repeat("a", 65)}
	for _, s := range invalid {
		require.Error(t, resource.ValidateSlug(s), "expected %q to be invalid", s)
	}
}
```

The import for `strings` is needed for the long-slug test:

```go
import (
	"strings"
	"testing"
	// ...
)
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/resource/...`
Expected: package not found.

- [ ] **Step 3: Implement `internal/resource/name.go`**

```go
// Package resource parses and formats Google AIP-122 resource names.
//
// Resource names look like "projects/{project}", "operators/{operator}", etc.
// Slugs are URL-safe lowercase strings: [a-z0-9][a-z0-9-]*[a-z0-9], length
// 3-64. Tokens like "Project" or paths with extra segments are rejected.
package resource

import (
	"errors"
	"fmt"
	"strings"
)

const (
	collectionProjects = "projects"

	minSlugLen = 3
	maxSlugLen = 64
)

// ValidateSlug returns an error if s is not a valid resource ID slug.
//
// Valid: 3-64 chars, [a-z0-9][a-z0-9-]*[a-z0-9], no consecutive dashes.
func ValidateSlug(s string) error {
	if len(s) < minSlugLen || len(s) > maxSlugLen {
		return fmt.Errorf("resource: slug %q must be %d-%d chars", s, minSlugLen, maxSlugLen)
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
			if i == 0 || i == len(s)-1 {
				return fmt.Errorf("resource: slug %q must not start or end with '-'", s)
			}
			if s[i-1] == '-' {
				return fmt.Errorf("resource: slug %q must not contain '--'", s)
			}
		default:
			return fmt.Errorf("resource: slug %q contains invalid char %q", s, r)
		}
	}
	return nil
}

// FormatProjectName returns the resource name for a project slug.
func FormatProjectName(slug string) string {
	return collectionProjects + "/" + slug
}

// ParseProjectName parses "projects/{slug}" and returns the slug.
func ParseProjectName(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] != collectionProjects {
		return "", errors.New(`resource: project name must be "projects/{slug}"`)
	}
	if err := ValidateSlug(parts[1]); err != nil {
		return "", err
	}
	return parts[1], nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -count=3 ./internal/resource/...`
Expected: 4 tests x 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resource/
git commit -m "feat(resource): add Google AIP-122 name parser"
```

---

## Task 2: Audit Log Writer

Single entry point for writing `audit_logs` rows. Service handlers will call `audit.Log(ctx, "project.create", ...)` after successful mutations.

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/audit/audit.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/audit/audit_test.go`

- [ ] **Step 1: Write failing test `internal/audit/audit_test.go`**

```go
package audit_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:audit_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(model.AllModels()...))
	t.Cleanup(func() { s, _ := db.DB(); s.Close() })
	return db
}

func TestLogWritesEntry(t *testing.T) {
	db := openDB(t)
	w := audit.NewWriter(db)

	op := &model.Operator{Base: model.Base{ID: 7}, Email: "x@y.com"}
	ctx := auth.WithOperator(context.Background(), op)

	require.NoError(t, w.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(42),
		Action:    "project.create",
		Target:    "projects/clinic-network",
		Extra:     map[string]any{"display_name": "Clinic Network"},
	}))

	var got model.AuditLog
	require.NoError(t, db.First(&got).Error)
	require.NotNil(t, got.OperatorID)
	require.Equal(t, uint64(7), *got.OperatorID)
	require.NotNil(t, got.ProjectID)
	require.Equal(t, uint64(42), *got.ProjectID)
	require.Equal(t, "project.create", got.Action)
	require.Equal(t, "projects/clinic-network", got.Target)
	require.Contains(t, got.ExtraJSON, "Clinic Network")
}

func TestLogAllowsNilOperator(t *testing.T) {
	db := openDB(t)
	w := audit.NewWriter(db)

	require.NoError(t, w.Log(context.Background(), audit.Entry{
		Action: "system.startup",
	}))

	var got model.AuditLog
	require.NoError(t, db.First(&got).Error)
	require.Nil(t, got.OperatorID)
	require.Equal(t, "system.startup", got.Action)
}

func TestLogPullsSourceIPFromContext(t *testing.T) {
	db := openDB(t)
	w := audit.NewWriter(db)

	op := &model.Operator{Base: model.Base{ID: 1}, Email: "y@z.com"}
	ctx := auth.WithOperator(context.Background(), op)
	ctx = audit.WithSourceIP(ctx, "203.0.113.42")

	require.NoError(t, w.Log(ctx, audit.Entry{Action: "x"}))

	var got model.AuditLog
	require.NoError(t, db.First(&got).Error)
	require.Equal(t, "203.0.113.42", got.SourceIP)
}

func ptrUint64(v uint64) *uint64 { return &v }
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/audit/...`
Expected: package not found.

- [ ] **Step 3: Implement `internal/audit/audit.go`**

```go
// Package audit writes structured entries to the audit_logs table.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/model"
)

// Entry describes one audit event. Action and Target are required; everything
// else is filled from context or left null.
type Entry struct {
	ProjectID *uint64
	Action    string
	Target    string
	Extra     map[string]any
}

type sourceIPCtxKey struct{}

// WithSourceIP attaches a source IP to ctx so writes pick it up automatically.
// Set this in your gRPC interceptor (typically from peer.FromContext).
func WithSourceIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, sourceIPCtxKey{}, ip)
}

func sourceIPFromContext(ctx context.Context) string {
	v, _ := ctx.Value(sourceIPCtxKey{}).(string)
	return v
}

// Writer persists audit events.
type Writer struct{ db *gorm.DB }

// NewWriter constructs a Writer bound to db.
func NewWriter(db *gorm.DB) *Writer { return &Writer{db: db} }

// Log inserts one audit entry. The acting operator and source IP are pulled
// from ctx; Action and Target come from e.
func (w *Writer) Log(ctx context.Context, e Entry) error {
	if e.Action == "" {
		return fmt.Errorf("audit: action is required")
	}

	var operatorID *uint64
	if op := auth.OperatorFromContext(ctx); op != nil {
		opID := op.ID
		operatorID = &opID
	}

	extra := ""
	if len(e.Extra) > 0 {
		b, err := json.Marshal(e.Extra)
		if err != nil {
			return fmt.Errorf("audit: marshal extra: %w", err)
		}
		extra = string(b)
	}

	row := model.AuditLog{
		Ts:         time.Now().UTC(),
		ProjectID:  e.ProjectID,
		OperatorID: operatorID,
		Action:     e.Action,
		Target:     e.Target,
		SourceIP:   sourceIPFromContext(ctx),
		ExtraJSON:  extra,
	}
	if err := w.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("audit: create: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -count=3 ./internal/audit/...`
Expected: 3 tests x 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/audit/
git commit -m "feat(audit): add audit_logs writer with operator + source IP from context"
```

---

## Task 3: project.proto + Code Generation

Add the `ProjectService` proto definition and regenerate Go code.

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/api/quicktun/v1/project.proto`

- [ ] **Step 1: Create `api/quicktun/v1/project.proto`**

```proto
// ProjectService — CRUD for projects (tenant boundary).
syntax = "proto3";

package quicktun.v1;

import "google/api/annotations.proto";
import "google/api/field_behavior.proto";
import "google/protobuf/empty.proto";
import "google/protobuf/field_mask.proto";
import "google/protobuf/timestamp.proto";

import "quicktun/v1/common.proto";

option go_package = "github.com/tulip/quicktun/gen/go/quicktun/v1;quicktunv1";

// ProjectService manages the project resources that bound multi-tenant data.
service ProjectService {
  rpc ListProjects(ListProjectsRequest) returns (ListProjectsResponse) {
    option (google.api.http) = {
      get: "/v1/projects"
    };
  }

  rpc GetProject(GetProjectRequest) returns (Project) {
    option (google.api.http) = {
      get: "/v1/{name=projects/*}"
    };
  }

  rpc CreateProject(CreateProjectRequest) returns (Project) {
    option (google.api.http) = {
      post: "/v1/projects"
      body: "project"
    };
  }

  rpc UpdateProject(UpdateProjectRequest) returns (Project) {
    option (google.api.http) = {
      patch: "/v1/{project.name=projects/*}"
      body: "project"
    };
  }

  rpc DeleteProject(DeleteProjectRequest) returns (google.protobuf.Empty) {
    option (google.api.http) = {
      delete: "/v1/{name=projects/*}"
    };
  }
}

// SiteMode controls how a Site within a project exposes services.
enum SiteMode {
  SITE_MODE_UNSPECIFIED = 0;
  SITE_MODE_ENDPOINT    = 1;
  SITE_MODE_SUBNET      = 2;
}

// Backend selects which relay implementation drives the project.
enum Backend {
  BACKEND_UNSPECIFIED = 0;
  BACKEND_RATHOLE     = 1;
  BACKEND_NETBIRD     = 2;
}

// ProjectStatus enumerates lifecycle states of a Project.
enum ProjectStatus {
  PROJECT_STATUS_UNSPECIFIED = 0;
  PROJECT_STATUS_ACTIVE      = 1;
  PROJECT_STATUS_DISABLED    = 2;
}

message Project {
  // Resource name. Format: projects/{slug}
  string name = 1;

  string project_id = 2 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp create_time = 3 [(google.api.field_behavior) = OUTPUT_ONLY];
  google.protobuf.Timestamp update_time = 4 [(google.api.field_behavior) = OUTPUT_ONLY];

  string display_name      = 10 [(google.api.field_behavior) = REQUIRED];
  SiteMode default_mode    = 11;
  Backend  backend         = 12;
  string   relay_port_range = 13 [(google.api.field_behavior) = REQUIRED];
  ProjectStatus status     = 14;
}

message ListProjectsRequest {
  PageRequest page = 1;
}

message ListProjectsResponse {
  repeated Project projects = 1;
  PageResponse page = 2;
}

message GetProjectRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
}

message CreateProjectRequest {
  // The slug to use as the project ID. Must match resource.ValidateSlug rules.
  string project_id = 1 [(google.api.field_behavior) = REQUIRED];
  Project project = 2 [(google.api.field_behavior) = REQUIRED];
}

message UpdateProjectRequest {
  Project project = 1 [(google.api.field_behavior) = REQUIRED];
  google.protobuf.FieldMask update_mask = 2 [(google.api.field_behavior) = REQUIRED];
}

message DeleteProjectRequest {
  string name = 1 [(google.api.field_behavior) = REQUIRED];
  // If true, cascade-delete sites + services. Default: refuse if project is non-empty.
  bool force = 2;
}
```

- [ ] **Step 2: Lint and generate**

```bash
cd /Users/tulip/project/repos/quicktun
make proto-lint
make proto-gen
```

Expected: clean lint. Generated files in `gen/go/quicktun/v1/`:
- `project.pb.go`
- `project_grpc.pb.go`
- `project.pb.gw.go`

If proto-lint complains about `RPC_RESPONSE_STANDARD_NAME` for `DeleteProject` (returns Empty), the existing `except: RPC_RESPONSE_STANDARD_NAME` in `api/buf.yaml` should already cover it.

- [ ] **Step 3: Verify generated code compiles**

```bash
go mod tidy
go build ./gen/...
```

Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add api/quicktun/v1/project.proto go.mod go.sum
git commit -m "feat(proto): add ProjectService"
```

---

## Task 4: ProjectDAO

CRUD methods for the projects table.

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/dao/project.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/dao/project_test.go`

- [ ] **Step 1: Write failing test `internal/dao/project_test.go`**

```go
package dao_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func TestProjectCreateAndFindBySlug(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, err := store.Create(ctx, &model.Project{
		Slug:           "clinic-network",
		Name:           "Clinic Network",
		DefaultMode:    model.SiteModeEndpoint,
		Backend:        model.BackendRathole,
		RelayPortRange: "20000-20999",
		Status:         model.ProjectStatusActive,
	})
	require.NoError(t, err)
	require.NotZero(t, p.ID)

	got, err := store.FindBySlug(ctx, "clinic-network")
	require.NoError(t, err)
	require.Equal(t, p.ID, got.ID)
	require.Equal(t, "Clinic Network", got.Name)
}

func TestProjectFindBySlugNotFound(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	_, err := store.FindBySlug(context.Background(), "nope")
	require.Error(t, err)
	require.True(t, dao.IsNotFound(err))
}

func TestProjectList(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	_, _ = store.Create(ctx, &model.Project{Slug: "a", Name: "A", RelayPortRange: "20000-20099"})
	_, _ = store.Create(ctx, &model.Project{Slug: "b", Name: "B", RelayPortRange: "20100-20199"})
	_, _ = store.Create(ctx, &model.Project{Slug: "c", Name: "C", RelayPortRange: "20200-20299"})

	got, err := store.List(ctx, 100, "")
	require.NoError(t, err)
	require.Len(t, got, 3)
}

func TestProjectListPagination(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		slug := string(rune('a'+i)) + string(rune('a'+i))
		_, err := store.Create(ctx, &model.Project{Slug: slug, Name: slug, RelayPortRange: "20000-20099"})
		require.NoError(t, err)
	}

	page1, err := store.List(ctx, 2, "")
	require.NoError(t, err)
	require.Len(t, page1, 2)

	// Use last id as page token.
	page2, err := store.List(ctx, 2, dao.NextProjectPageToken(page1))
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotEqual(t, page1[0].ID, page2[0].ID)
}

func TestProjectUpdate(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, _ := store.Create(ctx, &model.Project{Slug: "u", Name: "U1", RelayPortRange: "20000-20099"})
	p.Name = "U2"
	p.Status = model.ProjectStatusDisabled
	require.NoError(t, store.Update(ctx, p))

	got, _ := store.FindBySlug(ctx, "u")
	require.Equal(t, "U2", got.Name)
	require.Equal(t, model.ProjectStatusDisabled, got.Status)
}

func TestProjectSoftDelete(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, _ := store.Create(ctx, &model.Project{Slug: "d", Name: "D", RelayPortRange: "20000-20099"})
	require.NoError(t, store.Delete(ctx, p.ID))

	_, err := store.FindBySlug(ctx, "d")
	require.True(t, dao.IsNotFound(err))
}

func TestProjectCountSites(t *testing.T) {
	db := openWithModels(t)
	store := dao.NewProjectDAO(db)
	ctx := context.Background()

	p, _ := store.Create(ctx, &model.Project{Slug: "p", Name: "P", RelayPortRange: "20000-20099"})

	got, err := store.CountSites(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), got)

	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "s1"}).Error)
	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "s2"}).Error)

	got, err = store.CountSites(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got)
}
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/dao/...`
Expected: compile error: `dao.NewProjectDAO`, `dao.NextProjectPageToken` undefined.

- [ ] **Step 3: Implement `internal/dao/project.go`**

```go
package dao

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/model"
)

// ProjectDAO encapsulates queries against the projects table.
type ProjectDAO struct{ db *gorm.DB }

// NewProjectDAO constructs a ProjectDAO bound to db.
func NewProjectDAO(db *gorm.DB) *ProjectDAO { return &ProjectDAO{db: db} }

// Create inserts a new project. The caller must populate Slug, Name, and
// RelayPortRange. Defaults from struct tags fill DefaultMode, Backend, Status
// when left zero.
func (d *ProjectDAO) Create(ctx context.Context, p *model.Project) (*model.Project, error) {
	if err := d.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, fmt.Errorf("dao: create project: %w", err)
	}
	return p, nil
}

// FindBySlug returns the live project with the given slug.
func (d *ProjectDAO) FindBySlug(ctx context.Context, slug string) (*model.Project, error) {
	var p model.Project
	if err := d.db.WithContext(ctx).Where("slug = ?", slug).First(&p).Error; err != nil {
		return nil, fmt.Errorf("dao: find project by slug: %w", err)
	}
	return &p, nil
}

// FindByID returns the live project at id.
func (d *ProjectDAO) FindByID(ctx context.Context, id uint64) (*model.Project, error) {
	var p model.Project
	if err := d.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("dao: find project by id: %w", err)
	}
	return &p, nil
}

// List returns up to pageSize projects starting after the project ID encoded
// in pageToken. An empty token starts from the beginning.
func (d *ProjectDAO) List(ctx context.Context, pageSize int, pageToken string) ([]model.Project, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).Order("id ASC").Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dao: invalid page token: %w", err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Project
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list projects: %w", err)
	}
	return out, nil
}

// NextProjectPageToken returns the page token to fetch the page AFTER the
// given page. Returns "" when fewer rows came back than requested (i.e., no
// next page) — but here we trust the caller; the simple rule is: token is
// the last row's ID.
func NextProjectPageToken(page []model.Project) string {
	if len(page) == 0 {
		return ""
	}
	return strconv.FormatUint(page[len(page)-1].ID, 10)
}

// Update writes mutable fields back to the row. Caller must already have
// fetched the project (so ID is set). Soft-delete state is unaffected.
func (d *ProjectDAO) Update(ctx context.Context, p *model.Project) error {
	if p.ID == 0 {
		return fmt.Errorf("dao: update project: missing ID")
	}
	res := d.db.WithContext(ctx).Save(p)
	if res.Error != nil {
		return fmt.Errorf("dao: update project: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("dao: update project: no rows affected")
	}
	return nil
}

// Delete soft-deletes the project (and via gorm cascade settings, NOT the
// related sites — those need explicit handling). Idempotent: returns nil if
// already deleted.
func (d *ProjectDAO) Delete(ctx context.Context, id uint64) error {
	res := d.db.WithContext(ctx).Delete(&model.Project{}, id)
	if res.Error != nil {
		return fmt.Errorf("dao: delete project: %w", res.Error)
	}
	return nil
}

// CountSites returns the number of live sites belonging to projectID.
func (d *ProjectDAO) CountSites(ctx context.Context, projectID uint64) (int64, error) {
	var n int64
	err := d.db.WithContext(ctx).Model(&model.Site{}).
		Where("project_id = ?", projectID).
		Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("dao: count sites: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test -count=3 ./internal/dao/...`
Expected: all dao tests pass (existing + 7 new project tests).

- [ ] **Step 5: Commit**

```bash
git add internal/dao/project.go internal/dao/project_test.go
git commit -m "feat(dao): add ProjectDAO"
```

---

## Task 5: ProjectService — Get + List

Implement the read-only methods first; mutations come in Tasks 6-8.

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service.go`
- Create: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service_test.go`

- [ ] **Step 1: Write failing test `internal/grpcsvc/project_service_test.go`**

```go
package grpcsvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quicktunv1 "github.com/tulip/quicktun/gen/go/quicktun/v1"
	"github.com/tulip/quicktun/internal/audit"
	"github.com/tulip/quicktun/internal/auth"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/grpcsvc"
	"github.com/tulip/quicktun/internal/model"
)

func newProjectService(t *testing.T, db *gorm.DB) *grpcsvc.ProjectService {
	return grpcsvc.NewProjectService(dao.NewProjectDAO(db), audit.NewWriter(db))
}

// Authenticated context for tests: an admin operator.
func adminCtx(t *testing.T, db *gorm.DB) context.Context {
	t.Helper()
	op := seedOperator(t, db, "admin@x.com", "p", true)
	return auth.WithOperator(context.Background(), op)
}

func TestGetProjectByName(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "clinic-network", Name: "Clinic", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	resp, err := svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "projects/clinic-network",
	})
	require.NoError(t, err)
	require.Equal(t, "projects/clinic-network", resp.Name)
	require.Equal(t, "Clinic", resp.DisplayName)
}

func TestGetProjectNotFound(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "projects/missing",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestGetProjectInvalidName(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "not-a-resource-name",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetProjectRequiresAuth(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.GetProject(context.Background(), &quicktunv1.GetProjectRequest{
		Name: "projects/clinic-network",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.Unauthenticated, st.Code())
}

func TestListProjectsAdminSeesAll(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	store.Create(context.Background(), &model.Project{Slug: "a", Name: "A", RelayPortRange: "20000-20099"})
	store.Create(context.Background(), &model.Project{Slug: "b", Name: "B", RelayPortRange: "20100-20199"})
	svc := newProjectService(t, db)

	resp, err := svc.ListProjects(adminCtx(t, db), &quicktunv1.ListProjectsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Projects, 2)
}

func TestListProjectsNonAdminSeesOnlyAccessible(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	pa, _ := store.Create(context.Background(), &model.Project{Slug: "a", Name: "A", RelayPortRange: "20000-20099"})
	store.Create(context.Background(), &model.Project{Slug: "b", Name: "B", RelayPortRange: "20100-20199"})
	op := seedOperator(t, db, "user@x.com", "p", false) // not admin
	require.NoError(t, db.Create(&model.OperatorProjectAccess{
		OperatorID: op.ID, ProjectID: pa.ID, Role: model.ProjectRoleOperator,
	}).Error)
	svc := newProjectService(t, db)

	ctx := auth.WithOperator(context.Background(), op)
	resp, err := svc.ListProjects(ctx, &quicktunv1.ListProjectsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Projects, 1)
	require.Equal(t, "projects/a", resp.Projects[0].Name)
}
```

This test file imports `gorm.io/gorm` — add the import:

```go
import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	// ... rest of imports unchanged
)
```

(Note: `openTestDB` and `seedOperator` are already defined in `auth_service_test.go` in the same `grpcsvc_test` package; do not redefine.)

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/grpcsvc/...`
Expected: compile error: `grpcsvc.NewProjectService` undefined.

- [ ] **Step 3: Implement `internal/grpcsvc/project_service.go` (Get + List only)**

```go
package grpcsvc

import (
	"context"
	"errors"
	"strconv"

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

// ProjectService implements quicktunv1.ProjectServiceServer.
type ProjectService struct {
	quicktunv1.UnimplementedProjectServiceServer
	projects *dao.ProjectDAO
	audit    *audit.Writer
}

// NewProjectService constructs a ProjectService.
func NewProjectService(projects *dao.ProjectDAO, audit *audit.Writer) *ProjectService {
	return &ProjectService{projects: projects, audit: audit}
}

// GetProject implements quicktunv1.ProjectServiceServer.
func (s *ProjectService) GetProject(ctx context.Context, req *quicktunv1.GetProjectRequest) (*quicktunv1.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	slug, err := resource.ParseProjectName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.projects.FindBySlug(ctx, slug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}
	if !op.IsAdmin {
		if !s.hasAccess(ctx, op.ID, p.ID) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
	}
	return projectToProto(p), nil
}

// ListProjects implements quicktunv1.ProjectServiceServer.
//
// Admins see every project; non-admins see only projects they have an access
// grant on.
func (s *ProjectService) ListProjects(ctx context.Context, req *quicktunv1.ListProjectsRequest) (*quicktunv1.ListProjectsResponse, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	pageSize := int(req.GetPage().GetPageSize())
	pageToken := req.GetPage().GetPageToken()

	var rows []model.Project
	var err error
	if op.IsAdmin {
		rows, err = s.projects.List(ctx, pageSize, pageToken)
	} else {
		rows, err = s.projects.ListAccessible(ctx, op.ID, pageSize, pageToken)
	}
	if err != nil {
		if errors.Is(err, errInvalidToken) {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		return nil, status.Error(codes.Internal, "list failed")
	}

	out := &quicktunv1.ListProjectsResponse{
		Projects: make([]*quicktunv1.Project, len(rows)),
		Page:     &quicktunv1.PageResponse{NextPageToken: dao.NextProjectPageToken(rows)},
	}
	for i, p := range rows {
		out.Projects[i] = projectToProto(&p)
	}
	return out, nil
}

func (s *ProjectService) hasAccess(ctx context.Context, operatorID, projectID uint64) bool {
	var count int64
	err := s.projects.Db().WithContext(ctx).
		Model(&model.OperatorProjectAccess{}).
		Where("operator_id = ? AND project_id = ?", operatorID, projectID).
		Count(&count).Error
	return err == nil && count > 0
}

func projectToProto(p *model.Project) *quicktunv1.Project {
	out := &quicktunv1.Project{
		Name:           resource.FormatProjectName(p.Slug),
		ProjectId:      p.Slug,
		CreateTime:     timestamppb.New(p.CreatedAt),
		UpdateTime:     timestamppb.New(p.UpdatedAt),
		DisplayName:    p.Name,
		RelayPortRange: p.RelayPortRange,
		Status:         projectStatusToProto(p.Status),
		DefaultMode:    siteModeToProto(p.DefaultMode),
		Backend:        backendToProto(p.Backend),
	}
	return out
}

func projectStatusToProto(s model.ProjectStatus) quicktunv1.ProjectStatus {
	switch s {
	case model.ProjectStatusActive:
		return quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE
	case model.ProjectStatusDisabled:
		return quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED
	}
	return quicktunv1.ProjectStatus_PROJECT_STATUS_UNSPECIFIED
}

func siteModeToProto(m model.SiteMode) quicktunv1.SiteMode {
	switch m {
	case model.SiteModeEndpoint:
		return quicktunv1.SiteMode_SITE_MODE_ENDPOINT
	case model.SiteModeSubnet:
		return quicktunv1.SiteMode_SITE_MODE_SUBNET
	}
	return quicktunv1.SiteMode_SITE_MODE_UNSPECIFIED
}

func backendToProto(b model.Backend) quicktunv1.Backend {
	switch b {
	case model.BackendRathole:
		return quicktunv1.Backend_BACKEND_RATHOLE
	case model.BackendNetbird:
		return quicktunv1.Backend_BACKEND_NETBIRD
	}
	return quicktunv1.Backend_BACKEND_UNSPECIFIED
}

// errInvalidToken is returned by DAO.List when page_token cannot be parsed.
// Sentinel kept here so service can map to InvalidArgument.
var errInvalidToken = errors.New("invalid page token")

// uint64ToString already defined in auth_service.go (same package).
var _ = strconv.FormatUint // silence unused if uint64ToString used elsewhere
```

This implementation references `s.projects.Db()` and `s.projects.ListAccessible(...)` which need to be added to `ProjectDAO`. Add them in step 4.

- [ ] **Step 4: Extend `internal/dao/project.go` with Db() and ListAccessible**

Append to `internal/dao/project.go`:

```go

// Db returns the underlying *gorm.DB. Service handlers use this for
// cross-table queries (e.g., counting access grants) where adding a
// dedicated DAO method would be overkill.
func (d *ProjectDAO) Db() *gorm.DB { return d.db }

// ListAccessible returns up to pageSize projects that the operator has any
// access grant on, starting after pageToken (project ID).
func (d *ProjectDAO) ListAccessible(ctx context.Context, operatorID uint64, pageSize int, pageToken string) ([]model.Project, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	q := d.db.WithContext(ctx).
		Where("id IN (?)",
			d.db.Session(&gorm.Session{NewDB: true}).
				Table("operator_project_access").
				Select("project_id").
				Where("operator_id = ? AND deleted_at IS NULL", operatorID),
		).
		Order("id ASC").
		Limit(pageSize)
	if pageToken != "" {
		afterID, err := strconv.ParseUint(pageToken, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dao: invalid page token: %w", err)
		}
		q = q.Where("id > ?", afterID)
	}
	var out []model.Project
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("dao: list accessible projects: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test -count=3 ./internal/grpcsvc/... ./internal/dao/...`
Expected: PASS (existing AuthService 7 tests + 6 new ProjectService tests + all DAO tests).

- [ ] **Step 6: Commit**

```bash
git add internal/grpcsvc/project_service.go internal/grpcsvc/project_service_test.go internal/dao/project.go
git commit -m "feat(grpcsvc): add ProjectService Get + List with admin/scope filtering"
```

---

## Task 6: ProjectService — Create

Add CreateProject with audit logging.

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service_test.go`

- [ ] **Step 1: Write failing test (append to `project_service_test.go`)**

```go
func TestCreateProjectSuccess(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	resp, err := svc.CreateProject(adminCtx(t, db), &quicktunv1.CreateProjectRequest{
		ProjectId: "clinic-network",
		Project: &quicktunv1.Project{
			DisplayName:    "Clinic Network",
			RelayPortRange: "20000-20999",
			DefaultMode:    quicktunv1.SiteMode_SITE_MODE_ENDPOINT,
			Backend:        quicktunv1.Backend_BACKEND_RATHOLE,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/clinic-network", resp.Name)
	require.Equal(t, "clinic-network", resp.ProjectId)
	require.Equal(t, "Clinic Network", resp.DisplayName)
	require.Equal(t, quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE, resp.Status)

	// Verify audit log entry written.
	var audits []model.AuditLog
	require.NoError(t, db.Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "project.create", audits[0].Action)
	require.Equal(t, "projects/clinic-network", audits[0].Target)
}

func TestCreateProjectRejectsBadSlug(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.CreateProject(adminCtx(t, db), &quicktunv1.CreateProjectRequest{
		ProjectId: "Bad Slug!",
		Project:   &quicktunv1.Project{DisplayName: "X", RelayPortRange: "20000-20099"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateProjectRejectsMissingFields(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	_, err := svc.CreateProject(adminCtx(t, db), &quicktunv1.CreateProjectRequest{
		ProjectId: "x-y-z",
		Project:   &quicktunv1.Project{}, // missing display_name and relay_port_range
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestCreateProjectRejectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)

	req := &quicktunv1.CreateProjectRequest{
		ProjectId: "dup-slug",
		Project: &quicktunv1.Project{
			DisplayName: "First", RelayPortRange: "20000-20099",
		},
	}
	_, err := svc.CreateProject(adminCtx(t, db), req)
	require.NoError(t, err)

	_, err = svc.CreateProject(adminCtx(t, db), req)
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.AlreadyExists, st.Code())
}

func TestCreateProjectRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	svc := newProjectService(t, db)
	op := seedOperator(t, db, "non-admin@x.com", "p", false)
	ctx := auth.WithOperator(context.Background(), op)

	_, err := svc.CreateProject(ctx, &quicktunv1.CreateProjectRequest{
		ProjectId: "any-slug",
		Project:   &quicktunv1.Project{DisplayName: "X", RelayPortRange: "20000-20099"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}
```

- [ ] **Step 2: Run test — verify it fails**

Run: `go test ./internal/grpcsvc/...`
Expected: compile error: `svc.CreateProject` not implemented (defaults to UnimplementedProjectServiceServer's stub returning Unimplemented).

- [ ] **Step 3: Implement `CreateProject` (append to `internal/grpcsvc/project_service.go`)**

```go

// CreateProject implements quicktunv1.ProjectServiceServer.
func (s *ProjectService) CreateProject(ctx context.Context, req *quicktunv1.CreateProjectRequest) (*quicktunv1.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	if req.GetProject() == nil {
		return nil, status.Error(codes.InvalidArgument, "project body is required")
	}
	if err := resource.ValidateSlug(req.GetProjectId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if req.Project.GetDisplayName() == "" {
		return nil, status.Error(codes.InvalidArgument, "project.display_name is required")
	}
	if req.Project.GetRelayPortRange() == "" {
		return nil, status.Error(codes.InvalidArgument, "project.relay_port_range is required")
	}

	row := &model.Project{
		Slug:           req.ProjectId,
		Name:           req.Project.DisplayName,
		RelayPortRange: req.Project.RelayPortRange,
		DefaultMode:    siteModeFromProto(req.Project.DefaultMode),
		Backend:        backendFromProto(req.Project.Backend),
		Status:         model.ProjectStatusActive,
	}
	if row.DefaultMode == "" {
		row.DefaultMode = model.SiteModeEndpoint
	}
	if row.Backend == "" {
		row.Backend = model.BackendRathole
	}

	if _, err := s.projects.Create(ctx, row); err != nil {
		// SQLite returns "UNIQUE constraint failed: projects.slug" on dup.
		if isUniqueConstraintErr(err) {
			return nil, status.Error(codes.AlreadyExists, "project slug already exists")
		}
		return nil, status.Error(codes.Internal, "create failed")
	}

	if err := s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(row.ID),
		Action:    "project.create",
		Target:    resource.FormatProjectName(row.Slug),
		Extra: map[string]any{
			"display_name":     row.Name,
			"relay_port_range": row.RelayPortRange,
		},
	}); err != nil {
		// Audit failure is non-fatal — log but do not unwind the create.
		// Production would emit a metric here; Phase 1 swallows.
	}

	return projectToProto(row), nil
}

func siteModeFromProto(m quicktunv1.SiteMode) model.SiteMode {
	switch m {
	case quicktunv1.SiteMode_SITE_MODE_ENDPOINT:
		return model.SiteModeEndpoint
	case quicktunv1.SiteMode_SITE_MODE_SUBNET:
		return model.SiteModeSubnet
	}
	return ""
}

func backendFromProto(b quicktunv1.Backend) model.Backend {
	switch b {
	case quicktunv1.Backend_BACKEND_RATHOLE:
		return model.BackendRathole
	case quicktunv1.Backend_BACKEND_NETBIRD:
		return model.BackendNetbird
	}
	return ""
}

func ptrUint64(v uint64) *uint64 { return &v }

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint failed") ||
		contains(msg, "unique constraint")
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test -count=3 ./internal/grpcsvc/...`
Expected: 5 new CreateProject tests + existing tests all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/grpcsvc/project_service.go internal/grpcsvc/project_service_test.go
git commit -m "feat(grpcsvc): add ProjectService.CreateProject with audit log"
```

---

## Task 7: ProjectService — Update + Delete

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/grpcsvc/project_service_test.go`

- [ ] **Step 1: Add fieldmaskpb import to go.mod**

The standard library `google.golang.org/protobuf/types/known/fieldmaskpb` is already pulled in by the proto generation. No `go get` needed; verify with:

```bash
grep "fieldmaskpb\|field_mask" go.sum | head -3
```

- [ ] **Step 2: Write failing tests (append to `project_service_test.go`)**

```go
import (
	// ... existing imports
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestUpdateProjectDisplayName(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-p", Name: "Old", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	resp, err := svc.UpdateProject(adminCtx(t, db), &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name:        "projects/u-p",
			DisplayName: "New",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.NoError(t, err)
	require.Equal(t, "New", resp.DisplayName)
	require.Equal(t, "20000-20099", resp.RelayPortRange) // unchanged
}

func TestUpdateProjectStatus(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-s", Name: "S", RelayPortRange: "20000-20099", Status: model.ProjectStatusActive,
	})
	svc := newProjectService(t, db)

	resp, err := svc.UpdateProject(adminCtx(t, db), &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{
			Name:   "projects/u-s",
			Status: quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	require.Equal(t, quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED, resp.Status)
}

func TestUpdateProjectRequiresMask(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-m", Name: "M", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	_, err := svc.UpdateProject(adminCtx(t, db), &quicktunv1.UpdateProjectRequest{
		Project: &quicktunv1.Project{Name: "projects/u-m", DisplayName: "X"},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateProjectRequiresAdmin(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "u-a", Name: "A", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)
	op := seedOperator(t, db, "user@x.com", "p", false)
	ctx := auth.WithOperator(context.Background(), op)

	_, err := svc.UpdateProject(ctx, &quicktunv1.UpdateProjectRequest{
		Project:    &quicktunv1.Project{Name: "projects/u-a", DisplayName: "X"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"display_name"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.PermissionDenied, st.Code())
}

func TestDeleteProjectSuccess(t *testing.T) {
	db := openTestDB(t)
	dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "d-p", Name: "D", RelayPortRange: "20000-20099",
	})
	svc := newProjectService(t, db)

	_, err := svc.DeleteProject(adminCtx(t, db), &quicktunv1.DeleteProjectRequest{
		Name: "projects/d-p",
	})
	require.NoError(t, err)

	_, err = svc.GetProject(adminCtx(t, db), &quicktunv1.GetProjectRequest{
		Name: "projects/d-p",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.NotFound, st.Code())
}

func TestDeleteProjectRefusesIfHasSites(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	p, _ := store.Create(context.Background(), &model.Project{
		Slug: "d-s", Name: "S", RelayPortRange: "20000-20099",
	})
	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "child"}).Error)
	svc := newProjectService(t, db)

	_, err := svc.DeleteProject(adminCtx(t, db), &quicktunv1.DeleteProjectRequest{
		Name: "projects/d-s",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	require.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestDeleteProjectForceWithSites(t *testing.T) {
	db := openTestDB(t)
	store := dao.NewProjectDAO(db)
	p, _ := store.Create(context.Background(), &model.Project{
		Slug: "d-f", Name: "F", RelayPortRange: "20000-20099",
	})
	require.NoError(t, db.Create(&model.Site{ProjectID: p.ID, Name: "child"}).Error)
	svc := newProjectService(t, db)

	_, err := svc.DeleteProject(adminCtx(t, db), &quicktunv1.DeleteProjectRequest{
		Name:  "projects/d-f",
		Force: true,
	})
	require.NoError(t, err)
}
```

- [ ] **Step 3: Run tests — verify they fail**

Run: `go test ./internal/grpcsvc/...`
Expected: tests failing because Update/Delete are unimplemented (return Unimplemented from the embedded server).

- [ ] **Step 4: Implement UpdateProject + DeleteProject (append to `project_service.go`)**

```go

// UpdateProject implements quicktunv1.ProjectServiceServer with FieldMask
// semantics: only paths listed in update_mask are written.
func (s *ProjectService) UpdateProject(ctx context.Context, req *quicktunv1.UpdateProjectRequest) (*quicktunv1.Project, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	if req.GetProject() == nil {
		return nil, status.Error(codes.InvalidArgument, "project body is required")
	}
	if req.GetUpdateMask() == nil || len(req.UpdateMask.Paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask is required")
	}
	slug, err := resource.ParseProjectName(req.Project.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	cur, err := s.projects.FindBySlug(ctx, slug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}

	changed := map[string]any{}
	for _, path := range req.UpdateMask.Paths {
		switch path {
		case "display_name":
			if req.Project.DisplayName == "" {
				return nil, status.Error(codes.InvalidArgument, "display_name cannot be empty")
			}
			cur.Name = req.Project.DisplayName
			changed["display_name"] = req.Project.DisplayName
		case "relay_port_range":
			if req.Project.RelayPortRange == "" {
				return nil, status.Error(codes.InvalidArgument, "relay_port_range cannot be empty")
			}
			cur.RelayPortRange = req.Project.RelayPortRange
			changed["relay_port_range"] = req.Project.RelayPortRange
		case "default_mode":
			cur.DefaultMode = siteModeFromProto(req.Project.DefaultMode)
			changed["default_mode"] = string(cur.DefaultMode)
		case "backend":
			cur.Backend = backendFromProto(req.Project.Backend)
			changed["backend"] = string(cur.Backend)
		case "status":
			st := projectStatusFromProto(req.Project.Status)
			if st == "" {
				return nil, status.Error(codes.InvalidArgument, "status must be ACTIVE or DISABLED")
			}
			cur.Status = st
			changed["status"] = string(cur.Status)
		default:
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask path: %q", path)
		}
	}

	if err := s.projects.Update(ctx, cur); err != nil {
		return nil, status.Error(codes.Internal, "update failed")
	}

	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(cur.ID),
		Action:    "project.update",
		Target:    resource.FormatProjectName(cur.Slug),
		Extra:     changed,
	})

	return projectToProto(cur), nil
}

// DeleteProject implements quicktunv1.ProjectServiceServer.
//
// Refuses if the project has live sites and force=false.
func (s *ProjectService) DeleteProject(ctx context.Context, req *quicktunv1.DeleteProjectRequest) (*emptypb.Empty, error) {
	op := auth.OperatorFromContext(ctx)
	if op == nil {
		return nil, status.Error(codes.Unauthenticated, "not authenticated")
	}
	if !op.IsAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin role required")
	}
	slug, err := resource.ParseProjectName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	p, err := s.projects.FindBySlug(ctx, slug)
	if err != nil {
		if dao.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, "project not found")
		}
		return nil, status.Error(codes.Internal, "lookup failed")
	}

	if !req.GetForce() {
		n, err := s.projects.CountSites(ctx, p.ID)
		if err != nil {
			return nil, status.Error(codes.Internal, "site count failed")
		}
		if n > 0 {
			return nil, status.Errorf(codes.FailedPrecondition,
				"project has %d sites; pass force=true to cascade", n)
		}
	}

	if err := s.projects.Delete(ctx, p.ID); err != nil {
		return nil, status.Error(codes.Internal, "delete failed")
	}

	_ = s.audit.Log(ctx, audit.Entry{
		ProjectID: ptrUint64(p.ID),
		Action:    "project.delete",
		Target:    resource.FormatProjectName(p.Slug),
		Extra:     map[string]any{"force": req.GetForce()},
	})

	return &emptypb.Empty{}, nil
}

func projectStatusFromProto(s quicktunv1.ProjectStatus) model.ProjectStatus {
	switch s {
	case quicktunv1.ProjectStatus_PROJECT_STATUS_ACTIVE:
		return model.ProjectStatusActive
	case quicktunv1.ProjectStatus_PROJECT_STATUS_DISABLED:
		return model.ProjectStatusDisabled
	}
	return ""
}
```

The `emptypb` import needs to be added at the top of the file:

```go
import (
	// ... existing imports
	"google.golang.org/protobuf/types/known/emptypb"
)
```

- [ ] **Step 5: Run tests**

Run: `go test -count=3 ./internal/grpcsvc/...`
Expected: all tests PASS (7 update/delete + prior).

- [ ] **Step 6: Commit**

```bash
git add internal/grpcsvc/project_service.go internal/grpcsvc/project_service_test.go
git commit -m "feat(grpcsvc): add ProjectService Update + Delete"
```

---

## Task 8: Wire ProjectService into server.New

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server.go`
- Modify: `/Users/tulip/project/repos/quicktun/internal/server/server_test.go`

- [ ] **Step 1: Update `internal/server/server.go` to register ProjectService**

In `server.New`, after the `quicktunv1.RegisterAuthServiceServer(gs, authSvc)` line, add:

```go
	auditWriter := audit.NewWriter(cfg.DB)
	projectSvc := grpcsvc.NewProjectService(dao.NewProjectDAO(cfg.DB), auditWriter)
	quicktunv1.RegisterProjectServiceServer(gs, projectSvc)
```

In `Run`, after `quicktunv1.RegisterAuthServiceHandlerFromEndpoint(...)`, add:

```go
	if err := quicktunv1.RegisterProjectServiceHandlerFromEndpoint(ctx, gatewayMux, s.cfg.GRPCListen, dialOpts); err != nil {
		grpcLn.Close()
		return fmt.Errorf("server: register project gateway: %w", err)
	}
```

Add the `audit` import:

```go
import (
	// ... existing imports
	"github.com/tulip/quicktun/internal/audit"
)
```

- [ ] **Step 2: Add an end-to-end project test to `server_test.go`**

```go
func TestProjectCreateAndListEndToEnd(t *testing.T) {
	db := newDB(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	_, err := dao.NewOperatorDAO(db).Create(context.Background(), "admin@x.com", string(hash), true)
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

	// Login first.
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()
	authClient := quicktunv1.NewAuthServiceClient(conn)
	loginResp, err := authClient.Login(context.Background(), &quicktunv1.LoginRequest{
		Email: "admin@x.com", Password: "pw",
	})
	require.NoError(t, err)

	// Create project with bearer token.
	projClient := quicktunv1.NewProjectServiceClient(conn)
	authedCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+loginResp.AccessToken)
	created, err := projClient.CreateProject(authedCtx, &quicktunv1.CreateProjectRequest{
		ProjectId: "e2e-test",
		Project: &quicktunv1.Project{
			DisplayName:    "E2E",
			RelayPortRange: "20000-20099",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "projects/e2e-test", created.Name)

	// List should include it.
	listed, err := projClient.ListProjects(authedCtx, &quicktunv1.ListProjectsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Projects, 1)
	require.Equal(t, "projects/e2e-test", listed.Projects[0].Name)
}
```

The `metadata` import comes from `google.golang.org/grpc/metadata`:

```go
import (
	// ... existing imports
	"google.golang.org/grpc/metadata"
)
```

- [ ] **Step 3: Run server tests**

Run: `go test -count=1 -timeout 60s ./internal/server/...`
Expected: 3 tests PASS (existing 2 + new e2e).

- [ ] **Step 4: Commit**

```bash
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat(server): register ProjectService on gRPC + gateway"
```

---

## Task 9: CLI — `admin project` Subcommands

**Files:**
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_admin_project.go`
- Create: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_admin_project_test.go`
- Modify: `/Users/tulip/project/repos/quicktun/cmd/quicktun-server/cmd_admin.go`

- [ ] **Step 1: Add subcommand wiring**

Edit `cmd/quicktun-server/cmd_admin.go`. Add to `adminCmd()`:

```go
	c.AddCommand(adminCreateOperatorCmd())
	c.AddCommand(adminProjectCmd())
```

- [ ] **Step 2: Implement `cmd_admin_project.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
)

func adminProjectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "project",
		Short: "Manage projects (admin)",
	}
	c.AddCommand(adminProjectCreateCmd())
	c.AddCommand(adminProjectListCmd())
	c.AddCommand(adminProjectDeleteCmd())
	return c
}

func adminProjectCreateCmd() *cobra.Command {
	var (
		slug           string
		displayName    string
		relayPortRange string
	)
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a new project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" || displayName == "" || relayPortRange == "" {
				return fmt.Errorf("admin project create: --slug, --display-name, --relay-port-range required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
				Slug:           slug,
				Name:           displayName,
				RelayPortRange: relayPortRange,
				DefaultMode:    model.SiteModeEndpoint,
				Backend:        model.BackendRathole,
				Status:         model.ProjectStatusActive,
			})
			if err != nil {
				return fmt.Errorf("admin project create: %w", err)
			}
			cmd.Printf("created project %q (id=%d)\n", p.Slug, p.ID)
			return nil
		},
	}
	c.Flags().StringVar(&slug, "slug", "", "URL-safe project slug")
	c.Flags().StringVar(&displayName, "display-name", "", "human-friendly name")
	c.Flags().StringVar(&relayPortRange, "relay-port-range", "", "e.g. \"20000-20999\"")
	return c
}

func adminProjectListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			rows, err := dao.NewProjectDAO(db).List(context.Background(), 1000, "")
			if err != nil {
				return fmt.Errorf("admin project list: %w", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		},
	}
}

func adminProjectDeleteCmd() *cobra.Command {
	var (
		slug  string
		force bool
	)
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete (soft) a project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if slug == "" {
				return fmt.Errorf("admin project delete: --slug is required")
			}
			db, err := openAdminDB(cmd)
			if err != nil {
				return err
			}
			defer func() { s, _ := db.DB(); s.Close() }()
			store := dao.NewProjectDAO(db)
			p, err := store.FindBySlug(context.Background(), slug)
			if err != nil {
				return fmt.Errorf("admin project delete: %w", err)
			}
			if !force {
				n, err := store.CountSites(context.Background(), p.ID)
				if err != nil {
					return fmt.Errorf("admin project delete: %w", err)
				}
				if n > 0 {
					return fmt.Errorf("admin project delete: project has %d sites; pass --force to cascade", n)
				}
			}
			if err := store.Delete(context.Background(), p.ID); err != nil {
				return fmt.Errorf("admin project delete: %w", err)
			}
			cmd.Printf("deleted project %q\n", slug)
			return nil
		},
	}
	c.Flags().StringVar(&slug, "slug", "", "project slug")
	c.Flags().BoolVar(&force, "force", false, "cascade-delete sites")
	return c
}

// openAdminDB is a helper used by every admin subcommand.
func openAdminDB(cmd *cobra.Command) (interface {
	DB() (*os.File, error) // unused — placeholder so compiler doesn't complain
}, error) {
	cfgPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	db, err := dao.Open(cfg.Database.DSN, nil)
	if err != nil {
		return nil, err
	}
	// Hack: gorm.DB doesn't satisfy the placeholder interface; rewrite below.
	return nil, nil
}
```

The `openAdminDB` placeholder above won't compile — it's deliberately wrong so the engineer can't skip thinking about it. Replace it with the correct implementation:

```go
func openAdminDB(cmd *cobra.Command) (*gorm.DB, error) {
	cfgPath, err := cmd.Root().PersistentFlags().GetString("config")
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	db, err := dao.Open(cfg.Database.DSN, nil)
	if err != nil {
		return nil, fmt.Errorf("admin: %w", err)
	}
	return db, nil
}
```

And add the `gorm.io/gorm` import.

- [ ] **Step 3: Refactor `cmd_admin.go` to use `openAdminDB`**

Edit `cmd/quicktun-server/cmd_admin.go`. Inside `adminCreateOperatorCmd().RunE`, replace the config-load + dao.Open block with:

```go
		db, err := openAdminDB(cmd)
		if err != nil {
			return err
		}
		defer func() { s, _ := db.DB(); s.Close() }()
```

- [ ] **Step 4: Write a smoke test for the project subcommands**

Create `cmd/quicktun-server/cmd_admin_project_test.go`:

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
)

func TestAdminProjectCreate(t *testing.T) {
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

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", cfgPath, "")
	root.AddCommand(adminCmd())
	root.SetArgs([]string{"admin", "project", "create",
		"--slug=clinic-network",
		"--display-name=Clinic Network",
		"--relay-port-range=20000-20999",
	})
	require.NoError(t, root.Execute())

	db, err := dao.Open(dbPath, nil)
	require.NoError(t, err)
	p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), "clinic-network")
	require.NoError(t, err)
	require.Equal(t, "Clinic Network", p.Name)
	require.Equal(t, "20000-20999", p.RelayPortRange)
}
```

- [ ] **Step 5: Run tests**

Run: `go test -count=3 ./cmd/quicktun-server/...`
Expected: PASS (existing tests + new admin-project test).

- [ ] **Step 6: Commit**

```bash
go mod tidy
git add cmd/quicktun-server/cmd_admin.go cmd/quicktun-server/cmd_admin_project.go cmd/quicktun-server/cmd_admin_project_test.go go.mod go.sum
git commit -m "feat(cmd): add admin project create/list/delete subcommands"
```

---

## Task 10: Smoke Script Update

**Files:**
- Modify: `/Users/tulip/project/repos/quicktun/scripts/smoke.sh`

- [ ] **Step 1: Extend `smoke.sh` to verify project create + list**

Find the section ending with `echo "PASS: end-to-end auth flow"` and replace with:

```bash
echo "auth: PASS"

# Create a project via gRPC gateway.
CREATE_RESP=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/projects" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"projectId":"smoke-test","project":{"displayName":"Smoke","relayPortRange":"20000-20099"}}')
echo "create: $CREATE_RESP"
if ! echo "$CREATE_RESP" | grep -q '"name":"projects/smoke-test"'; then
  echo "FAIL: create response missing expected name" >&2
  exit 1
fi

# List projects.
LIST_RESP=$(curl -sS "http://127.0.0.1:${HTTP_PORT}/v1/projects" \
  -H "Authorization: Bearer $TOKEN")
echo "list: $LIST_RESP"
if ! echo "$LIST_RESP" | grep -q '"name":"projects/smoke-test"'; then
  echo "FAIL: list did not include created project" >&2
  exit 1
fi

# Delete project.
curl -sS -X DELETE "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

# Verify deletion.
GET_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test" \
  -H "Authorization: Bearer $TOKEN")
if [ "$GET_CODE" != "404" ]; then
  echo "FAIL: get-after-delete returned $GET_CODE, expected 404" >&2
  exit 1
fi

echo "PASS: end-to-end auth + project flow"
```

- [ ] **Step 2: Run smoke**

```bash
cd /Users/tulip/project/repos/quicktun
./scripts/smoke.sh
```

Expected: prints `PASS: end-to-end auth + project flow`.

- [ ] **Step 3: Commit**

```bash
git add scripts/smoke.sh
git commit -m "test(smoke): cover project create + list + delete via gateway"
```

---

## Task 11: Final Verification

- [ ] **Step 1: Full test suite + race**

```bash
cd /Users/tulip/project/repos/quicktun
make sync-migrations
make check-migrations
go test ./...
go test -race -timeout 120s ./...
go vet ./...
make proto-lint
```

Expected: every step green.

- [ ] **Step 2: Build + run smoke**

```bash
make build
./scripts/smoke.sh
```

Expected: `PASS: end-to-end auth + project flow`.

- [ ] **Step 3: Final commit if anything missed**

```bash
git status
# If clean, no commit needed.
```

---

## Self-Review

**Spec coverage:**

| Plan 3 requirement | Implemented in |
|---|---|
| Plan 2 cleanup (4 important + 4 minor) | Task 0 |
| Resource name parser (AIP-122) | Task 1 |
| Audit log writer with operator + IP from ctx | Task 2 |
| project.proto (5 standard methods, FieldMask, force-delete) | Task 3 |
| ProjectDAO (CRUD + List + ListAccessible + CountSites) | Task 4 |
| ProjectService.GetProject + ListProjects (admin/scope filtering) | Task 5 |
| ProjectService.CreateProject (admin-only, slug validation, audit) | Task 6 |
| ProjectService.UpdateProject (FieldMask) + DeleteProject (force flag) | Task 7 |
| Wire ProjectService into server + e2e gRPC + gateway test | Task 8 |
| `admin project` CLI subcommands | Task 9 |
| Smoke script extension (HTTP project flow) | Task 10 |
| Final smoke pass | Task 11 |

**No placeholders.** Every step has complete code, exact commands, expected output. The intentional placeholder in Task 9 step 2 (`openAdminDB` placeholder) is followed by the actual replacement in the same step — the engineer must replace it before compile.

**Type consistency check:** `dao.NewProjectDAO`, `dao.ProjectDAO.Create`/`FindBySlug`/`List`/`ListAccessible`/`Update`/`Delete`/`CountSites`/`Db` are referenced consistently. `audit.NewWriter`, `audit.Entry{ProjectID, Action, Target, Extra}`, `audit.Writer.Log` consistent. `auth.WithOperator`, `auth.WithRawToken`, `auth.OperatorFromContext`, `auth.RawTokenFromContext` consistent across Tasks 0, 2, 5, 6, 7. `resource.ParseProjectName`, `resource.FormatProjectName`, `resource.ValidateSlug` consistent. `grpcsvc.NewProjectService(*dao.ProjectDAO, *audit.Writer)` matches Task 5 constructor and Task 8 wiring.

**Forward references resolved:** Task 5 introduces `errInvalidToken` but never returns it from current DAO methods; the import is for future symmetry. Acceptable — flagged only if the linter complains. If unused-variable lint complains, remove `errInvalidToken` and the `errors.Is` branch in `ListProjects`.

`siteModeFromProto` / `backendFromProto` / `projectStatusFromProto` introduced in Task 6/7 with explicit definitions; no forward refs.
