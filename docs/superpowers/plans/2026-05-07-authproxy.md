# quicktun Auth-Proxy (HTTP CONNECT Gateway) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single public ingress that token-authenticates agents before forwarding their tunnel TCP to the right project's loopback rathole-server. Replaces today's "agent dials rathole control port directly" with "agent dials auth-proxy, sends CONNECT + Bearer, gets a tunnel."

**Design choice (locked):** HTTP CONNECT (RFC 7231 §4.3.6). Agent sends `CONNECT relay:443 HTTP/1.1\r\nAuthorization: Bearer <raw_token>\r\n\r\n`; auth-proxy validates the token via `dao.SiteAgentTokenDAO.ValidateRaw`, looks up site → project → minP-of-relay-port-range, opens TCP to `127.0.0.1:<minP>`, returns `HTTP/1.1 200 OK\r\n\r\n`, then bidirectional `io.Copy`. The CONNECT host field is ignored — token alone determines routing.

**Why not TLS in this plan:** TLS termination is a deployment concern. Plan 10 will front auth-proxy with nginx/Caddy + Let's Encrypt. Phase 1 dev uses plain HTTP/TCP. The CONNECT preamble is ASCII-readable, easy to debug.

**Token contract recap (Plan 6.5):** Operator sees raw token. Agent presents raw as `Authorization: Bearer <raw>` to control plane API AND to auth-proxy. Inside the rathole tunnel, agent presents `sha256_hex(raw)` (the same hash the rathole-server config has). Two presentations of one secret.

**Out of scope:**
- TLS (Plan 10).
- Rate limiting / abuse mitigation (Plan 11+).
- Per-tunnel ACL (today's model: any valid agent token can reach its own project's rathole).
- Operator CLI use of auth-proxy (Plan 9 may add operator CONNECT for `quicktun forward`).

---

## Architecture

```
[Bastion]                     [Public ingress]            [Control plane host]
+-------------+               +-----------------+         +----------------------+
| agent       |               |                 |         |                      |
|             | CONNECT       |  auth-proxy     |  TCP    |  rathole-server (p1) |
|  bridge ────┼───────────────┼─►:8443──token──►├────────►│  127.0.0.1:20000     |
|  rathole-cli│  + Bearer     |  (port 8443     │         │                      |
|             |               |   plain HTTP)   │         │  rathole-server (p2) |
+-------------+               +-----------------+         │  127.0.0.1:21000     |
                                      ▲                   +----------------------+
                                      │
                                  shares DB with control plane
                                  (read-only: site_agent_tokens, sites, projects)
```

The agent's runtime gains a small in-process **CONNECT bridge** that listens on `127.0.0.1:<random>`. rathole-client connects there in plain TCP. The bridge accepts each connection, dials auth-proxy, sends the CONNECT preamble + Bearer, then `io.Copy` both directions. rathole's protocol is opaque to the bridge — once `200 OK` arrives, bytes flow freely.

auth-proxy runs as its own binary `quicktun-authproxy`, sharing the SQLite DB file with the control plane (read-only). For prod with high-availability, Plan 11 can switch to a gRPC token-validate API.

---

## File Structure

### New
```
internal/authproxy/
├── server.go              CONNECT server: parse, auth, route, forward
├── server_test.go         table-driven: bad CONNECT, bad token, ok flow
├── router.go              token → site → project → backend addr
├── router_test.go

cmd/quicktun-authproxy/
├── main.go                cobra root
├── cmd_run.go             `quicktun-authproxy run --config <path>`

internal/agent/
├── bridge.go              CONNECT bridge listener (in-process)
├── bridge_test.go

scripts/
├── smoke-authproxy.sh     end-to-end
```

### Modified
```
api/quicktun/v1/
├── agent.proto            BootstrapResponse: rathole_control_addr → auth_proxy_endpoint

internal/config/
├── config.go              BackendConfig.AuthProxyPublicAddr
├── config_test.go

internal/server/
├── server.go              pass AuthProxyPublicAddr to AgentService

internal/grpcsvc/
├── agent_service.go       Bootstrap fills AuthProxyEndpoint
├── agent_service_test.go

internal/agent/
├── render.go              uses bridge's local addr (not auth-proxy directly)
├── render_test.go
├── runtime.go             starts bridge before applyBootstrap
├── runtime_test.go

cmd/quicktun-server/
├── cmd_serve.go           thread AuthProxyPublicAddr config

cmd/quicktun-agent/        (no changes)

Makefile                   build $(AUTHPROXY_BIN)
scripts/
├── smoke-agent.sh         optionally update if BootstrapResponse field rename
                            ripples through the agent toml grep
```

---

## Tech

- All existing deps. New: none. `net/http` for CONNECT parsing (we accept HTTP/1.1 CONNECT manually since it's a simple preamble).
- Tests: real DB (in-memory SQLite) + real `net.Listen("tcp", "127.0.0.1:0")` for bufconn-style integration.

---

## Task 0: Proto + config rename

Field rename: `BootstrapResponse.rathole_control_addr` → `auth_proxy_endpoint`. Server fills with new config field `Backend.AuthProxyPublicAddr` (fallback to `cfg.RelayAddr` if empty). Agent renderer reads the new field.

### Step 1: Edit `api/quicktun/v1/agent.proto`

Change message:
```proto
message BootstrapResponse {
  string site_name = 1;
  string project_slug = 2;
  string site_slug = 3;
  string auth_proxy_endpoint = 4;     // was rathole_control_addr
  repeated TunnelBinding tunnels = 5;
  int32 heartbeat_seconds = 6;
  string config_version = 7;
}
```

Run `make proto-gen`. The `gen/` regen will rename the Go field to `AuthProxyEndpoint`.

### Step 2: Edit `internal/config/config.go`

Add to `BackendConfig`:
```go
type BackendConfig struct {
    RatholeBinary       string   `mapstructure:"rathole_binary"`
    RatholeArgs         []string `mapstructure:"rathole_args"`
    RatholeConfigDir    string   `mapstructure:"rathole_config_dir"`
    AuthProxyPublicAddr string   `mapstructure:"auth_proxy_public_addr"`  // NEW
}
```

In setDefaults, leave `AuthProxyPublicAddr` empty by default (fallback to `RelayAddr` happens in agent_service).

### Step 3: Edit `internal/grpcsvc/agent_service.go`

Replace the `RatholeControlAddr` computation (which currently does `relayHost + ":" + minP`) with:

```go
endpoint := a.authProxyEndpoint
if endpoint == "" {
    // Fallback: use the legacy RelayAddr-based wiring. Agent will hit rathole
    // directly without going through auth-proxy.
    minP, _, err := resource.ParsePortRange(project.RelayPortRange)
    if err != nil {
        return nil, status.Errorf(codes.FailedPrecondition, "port range: %v", err)
    }
    endpoint = a.relayHost + ":" + strconv.Itoa(int(minP))
}
```

Update the `BootstrapResponse` field name. Update `NewAgentService` to accept an `authProxyEndpoint string` arg (after `relayHost`). Wire from `server.New`.

If `cfg.AuthProxyPublicAddr` is empty in `server.New`, pass `""` — agent_service falls back to the Plan 7 behavior.

### Step 4: Edit `internal/server/server.go` + `cmd/quicktun-server/cmd_serve.go`

- `server.Config`: add `AuthProxyPublicAddr string`.
- `cmd_serve.go`: pass `cfg.Backend.AuthProxyPublicAddr`.
- `server.New`: pass to `grpcsvc.NewAgentService(... , relayHost, cfg.AuthProxyPublicAddr)`.

### Step 5: Edit `internal/agent/render.go` + `runtime.go`

`render.go::RenderRatholeClient`: change `boot.GetRatholeControlAddr()` (or `RatholeControlAddr` field access) to `boot.GetAuthProxyEndpoint()` / `boot.AuthProxyEndpoint`.

For Task 0, this still acts as the rathole `remote_addr`. Task 3 will indirect it through the bridge. Don't change semantics yet — just rename the source field.

### Step 6: Update tests

- `internal/grpcsvc/agent_service_test.go`: any assertion on `RatholeControlAddr` → `AuthProxyEndpoint`. Add a test that sets `authProxyEndpoint` non-empty in fixtures and asserts it's returned verbatim (NOT computed from minP).
- `internal/agent/render_test.go`: rename field references.
- `internal/agent/runtime_test.go`: rename.
- `scripts/smoke-agent.sh`: any grep for `rathole_control_addr` (probably none — smoke greps the rendered toml, not the proto JSON).

### Step 7: Verify + commit

```bash
cd /Users/tulip/project/repos/quicktun
make proto-gen
go test -count=1 ./...
go vet ./...
make build
./scripts/smoke.sh
./scripts/smoke-agent.sh
GOOS=linux GOARCH=amd64 go vet ./...
git add api/ gen/ internal/config/ internal/grpcsvc/ internal/server/ internal/agent/ cmd/quicktun-server/
git commit -m "feat(api,grpcsvc): rename BootstrapResponse field to auth_proxy_endpoint"
```

---

## Task 1: `internal/authproxy/` package

Pure Go HTTP CONNECT server. No cobra, no signal handling — that's Task 2.

### Step 1: Router (`internal/authproxy/router.go`)

```go
// Package authproxy is a TCP gateway that authenticates incoming connections
// via HTTP CONNECT + Bearer tokens and forwards them to the appropriate
// per-project rathole-server backend on loopback.
package authproxy

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/model"
	"github.com/tulip/quicktun/internal/resource"
)

// Router resolves a Bearer token to the loopback backend address it maps to.
// Sharing the same DB file as the control plane (read-only) is fine for
// Phase 1.
type Router struct {
	db *gorm.DB
}

func NewRouter(db *gorm.DB) *Router {
	return &Router{db: db}
}

// Route validates the raw bearer token and returns the loopback backend
// address ("127.0.0.1:<port>") for the agent's project's rathole-server.
//
// Errors:
//   - ErrUnauthenticated for invalid / expired tokens
//   - ErrInternal for DB / config issues
var (
	ErrUnauthenticated = errors.New("authproxy: invalid or expired token")
	ErrInternal        = errors.New("authproxy: internal error")
)

func (r *Router) Route(ctx context.Context, rawToken string) (string, error) {
	if rawToken == "" {
		return "", ErrUnauthenticated
	}
	siteID, err := dao.NewSiteAgentTokenDAO(r.db).ValidateRaw(ctx, rawToken)
	if err != nil {
		// ValidateRaw returns generic errors; treat all as Unauthenticated
		// (Plan 7 Task 0 fix did the same for the gRPC interceptor).
		return "", ErrUnauthenticated
	}

	var site model.Site
	if err := r.db.WithContext(ctx).First(&site, siteID).Error; err != nil {
		return "", ErrUnauthenticated
	}

	var project model.Project
	if err := r.db.WithContext(ctx).First(&project, site.ProjectID).Error; err != nil {
		return "", ErrUnauthenticated
	}
	if project.Status != model.ProjectStatusActive {
		return "", ErrUnauthenticated
	}

	minP, _, err := resource.ParsePortRange(project.RelayPortRange)
	if err != nil {
		return "", fmt.Errorf("%w: parse port range: %v", ErrInternal, err)
	}
	return fmt.Sprintf("127.0.0.1:%d", minP), nil
}
```

### Step 2: Server (`internal/authproxy/server.go`)

```go
package authproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Server runs a single authproxy listener.
type Server struct {
	router *Router
	lg     *zap.Logger

	listener net.Listener
	wg       sync.WaitGroup
}

// New constructs a Server that will Route via the given router.
func New(router *Router, lg *zap.Logger) *Server {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Server{router: router, lg: lg}
}

// Serve listens on addr and serves until ctx is cancelled. Blocks.
func (s *Server) Serve(ctx context.Context, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("authproxy: listen %s: %w", addr, err)
	}
	s.listener = lis
	s.lg.Info("authproxy: listening", zap.String("addr", lis.Addr().String()))

	go func() {
		<-ctx.Done()
		_ = lis.Close()
	}()

	for {
		conn, err := lis.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.wg.Wait()
				return nil
			}
			s.lg.Warn("authproxy: accept", zap.Error(err))
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer conn.Close()
			s.handle(ctx, conn)
		}()
	}
}

// Addr returns the listener's bound address (for tests using port 0).
func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		s.lg.Warn("authproxy: read request", zap.Error(err))
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	if req.Method != http.MethodConnect {
		writeStatus(conn, "HTTP/1.1 405 Method Not Allowed", "method must be CONNECT")
		return
	}

	rawToken := bearerToken(req.Header.Get("Authorization"))
	backend, err := s.router.Route(ctx, rawToken)
	if err != nil {
		writeStatus(conn, "HTTP/1.1 401 Unauthorized", "")
		return
	}

	upstream, err := net.DialTimeout("tcp", backend, 5*time.Second)
	if err != nil {
		s.lg.Warn("authproxy: dial backend",
			zap.String("backend", backend), zap.Error(err))
		writeStatus(conn, "HTTP/1.1 502 Bad Gateway", "")
		return
	}
	defer upstream.Close()

	writeStatus(conn, "HTTP/1.1 200 OK", "")

	// If the client buffered any bytes after the CONNECT preamble, flush them.
	if br.Buffered() > 0 {
		buf, _ := br.Peek(br.Buffered())
		_, _ = upstream.Write(buf)
	}

	// Bidirectional copy.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, conn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(conn, upstream); done <- struct{}{} }()
	<-done
}

func writeStatus(conn net.Conn, status, body string) {
	var b strings.Builder
	b.WriteString(status + "\r\n")
	if body != "" {
		fmt.Fprintf(&b, "Content-Length: %d\r\nContent-Type: text/plain\r\n", len(body))
	} else {
		b.WriteString("Content-Length: 0\r\n")
	}
	b.WriteString("Connection: close\r\n\r\n")
	if body != "" {
		b.WriteString(body)
	}
	_, _ = conn.Write([]byte(b.String()))
}

func bearerToken(h string) string {
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
```

### Step 3: Tests

**`router_test.go`**: in-memory SQLite + DAO seed.
- valid token → `127.0.0.1:<minP>`
- invalid token → ErrUnauthenticated
- expired token → ErrUnauthenticated
- disabled project → ErrUnauthenticated
- bad port range → ErrInternal

**`server_test.go`**: integration. Stand up the Server on `127.0.0.1:0`, plus a fake backend on `127.0.0.1:0` that ECHOES bytes. Override the router so it always returns the fake backend addr (use a small interface seam — `Router.Route` is a method, so wrap it via an interface):

```go
// In server.go, accept an interface, not the concrete *Router:
type RouteFunc func(ctx context.Context, rawToken string) (string, error)

type Server struct {
    route RouteFunc
    ...
}

func New(route RouteFunc, lg *zap.Logger) *Server { ... }
```

Update Task 2 to pass a closure: `func(ctx, raw) (string, error) { return router.Route(ctx, raw) }`.

Tests:
- `TestServerForwardsAfterValidConnect` — valid token, echo backend, send "hello", get "hello".
- `TestServerRejectsBadMethod` — send `GET /` over conn, expect 405.
- `TestServerRejectsMissingBearer` — CONNECT but no Authorization header, expect 401.
- `TestServerReturns502OnBackendDial` — route returns a deliberately-bad backend, expect 502.
- `TestServerForwardsBufferedClientBytes` — write `CONNECT ...\r\n\r\nhello-after-preamble` in one Write; assert backend receives the trailing bytes.

### Step 4: Verify + commit

```bash
go test -count=1 -timeout 60s ./internal/authproxy/...
go test -race -timeout 120s ./internal/authproxy/...
go vet ./...
git add internal/authproxy/
git commit -m "feat(authproxy): add HTTP CONNECT token gateway"
```

---

## Task 2: `cmd/quicktun-authproxy/` binary

Cobra wrapper that loads a YAML config and runs the server.

### Step 1: Config file format

The auth-proxy needs:
- `listen_addr` (e.g. `:8443`)
- `database.dsn` (same path as the control plane's DB; opens read-only or normal)

Create a tiny `internal/authproxy/config.go` (NOT mixed with `internal/config/` which is server-specific):

```go
type Config struct {
    ListenAddr string `yaml:"listen_addr"`
    Database struct {
        DSN string `yaml:"dsn"`
    } `yaml:"database"`
    Log struct {
        Level string `yaml:"level"`
    } `yaml:"log"`
}

func Load(path string) (*Config, error) { /* yaml.v3 unmarshal */ }
```

Defaults:
- `listen_addr = ":8443"`
- `database.dsn = "file:/var/lib/quicktun/quicktun.db?cache=shared&mode=ro"` (read-only access; rejects writes)
- `log.level = "info"`

Validate that `database.dsn` is non-empty.

### Step 2: `cmd/quicktun-authproxy/main.go` + `cmd_run.go`

Match the style of `cmd/quicktun-agent/`. The `run` subcommand:
1. Load config.
2. Open `*gorm.DB` with the configured DSN.
3. Auto-migrate? No — proxy is read-only. Open with `&gorm.Config{DryRun: false}` and DON'T call AutoMigrate.
4. Construct router + server.
5. `signal.NotifyContext(SIGTERM, SIGINT)`.
6. `server.Serve(ctx, cfg.ListenAddr)`.

### Step 3: Makefile

Add `AUTHPROXY_BIN := $(BINDIR)/quicktun-authproxy`. Update `build` to depend on it. Add the build rule (no `sync-migrations` dep).

### Step 4: Verify + commit

```bash
make build
./bin/quicktun-authproxy --help
go vet ./...
git add cmd/quicktun-authproxy/ internal/authproxy/config.go internal/authproxy/config_test.go Makefile
git commit -m "feat(authproxy): add quicktun-authproxy binary"
```

(Add a `config_test.go` mirroring the agent's: load/defaults/required-fields tests.)

---

## Task 3: Agent-side CONNECT bridge

A small in-process listener that bridges rathole-client's plain TCP to the auth-proxy's CONNECT protocol. Started by `agent.Runtime` before `applyBootstrap`.

### Step 1: `internal/agent/bridge.go`

```go
package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
)

// bridge listens on 127.0.0.1:<random> and, for each accepted connection,
// dials the configured auth-proxy, sends an HTTP CONNECT preamble with the
// agent's Bearer token, then forwards bytes both directions.
//
// Used by Runtime to make rathole-client's plain-TCP connections look like
// authenticated CONNECT tunnels to the auth-proxy.
type bridge struct {
	authProxyAddr string
	bearerToken   string
	lg            *zap.Logger

	listener net.Listener
	wg       sync.WaitGroup
}

// startBridge listens on 127.0.0.1:0 and runs accept loop until ctx is done.
// Returns the bound local addr and an error if listen failed.
func startBridge(ctx context.Context, authProxyAddr, bearer string, lg *zap.Logger) (*bridge, error) {
	if authProxyAddr == "" {
		return nil, fmt.Errorf("agent: empty auth_proxy_endpoint")
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("agent: bridge listen: %w", err)
	}
	b := &bridge{
		authProxyAddr: authProxyAddr,
		bearerToken:   bearer,
		lg:            lg,
		listener:      lis,
	}
	go b.serve(ctx)
	return b, nil
}

func (b *bridge) localAddr() string { return b.listener.Addr().String() }

func (b *bridge) close() {
	_ = b.listener.Close()
	b.wg.Wait()
}

func (b *bridge) serve(ctx context.Context) {
	go func() {
		<-ctx.Done()
		_ = b.listener.Close()
	}()
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.lg.Warn("agent bridge: accept", zap.Error(err))
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			defer conn.Close()
			b.handle(ctx, conn)
		}()
	}
}

func (b *bridge) handle(ctx context.Context, local net.Conn) {
	upstream, err := net.DialTimeout("tcp", b.authProxyAddr, 5*time.Second)
	if err != nil {
		b.lg.Warn("agent bridge: dial auth-proxy",
			zap.String("addr", b.authProxyAddr), zap.Error(err))
		return
	}
	defer upstream.Close()

	// CONNECT preamble. Host is dummy — auth-proxy ignores it.
	host, _, _ := net.SplitHostPort(b.authProxyAddr)
	if host == "" {
		host = "relay"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodConnect,
		(&url.URL{Host: host}).String(), nil)
	req.Host = host + ":443"
	req.Header.Set("Authorization", "Bearer "+b.bearerToken)
	if err := req.Write(upstream); err != nil {
		b.lg.Warn("agent bridge: write CONNECT", zap.Error(err))
		return
	}

	br := bufio.NewReader(upstream)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		b.lg.Warn("agent bridge: read CONNECT response", zap.Error(err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		b.lg.Warn("agent bridge: auth-proxy rejected",
			zap.Int("status", resp.StatusCode))
		return
	}

	// If anything was buffered after 200 OK, push it to local first.
	if br.Buffered() > 0 {
		buf, _ := br.Peek(br.Buffered())
		_, _ = local.Write(buf)
	}

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, upstream); done <- struct{}{} }()
	<-done
}
```

### Step 2: Wire into runtime

In `internal/agent/runtime.go`:

1. Add `bridge *bridge` field on `Runtime`.
2. In `Run`, AFTER first successful Bootstrap, BEFORE `applyBootstrap`:
   ```go
   br, err := startBridge(ctx, boot.GetAuthProxyEndpoint(), r.cfg.Token, r.lg)
   if err != nil {
       return fmt.Errorf("agent: start bridge: %w", err)
   }
   r.bridge = br
   defer br.close()
   ```
3. On rebootstrap, reuse the existing bridge (same auth-proxy, same token). If `boot.GetAuthProxyEndpoint()` changes between bootstraps, log a warning but keep the old bridge running (Phase 1 simplification — Plan 9 / Plan 11 can handle dynamic re-target).
4. Pass `r.bridge.localAddr()` into `applyBootstrap` / `RenderRatholeClient` so the rathole-client.toml's `remote_addr` points at the bridge, not at the auth-proxy directly.

### Step 3: Update `RenderRatholeClient` signature

```go
func RenderRatholeClient(boot *quicktunv1.BootstrapResponse, rawToken, ratholeRemoteAddr string) (string, error) {
    // ... unchanged validation ...
    fmt.Fprintf(&b, "remote_addr = %q\n", ratholeRemoteAddr)
    // ... rest unchanged ...
}
```

The `boot.AuthProxyEndpoint` is no longer used directly inside the renderer — the runtime resolves it through the bridge. Update `render_test.go` to pass an explicit `ratholeRemoteAddr` arg.

### Step 4: Tests

**`bridge_test.go`** — integration with a fake auth-proxy:

```go
func TestBridgeForwardsThroughFakeAuthProxy(t *testing.T) {
    // 1. Stand up a tiny test "auth-proxy" that:
    //    - Accepts a CONNECT request
    //    - Validates Authorization: Bearer <expected_token>
    //    - Writes 200 OK
    //    - Echoes bytes (for testing)
    // 2. Call startBridge with that fake's addr and the expected token.
    // 3. Dial 127.0.0.1:<bridge.localAddr()>, write "hello", read echo.
    // 4. Cancel ctx; assert bridge closes cleanly.
}

func TestBridgeRejectedByAuthProxyClosesLocalConn(t *testing.T) {
    // Fake proxy returns 401. Local conn should close (read returns EOF).
}
```

**Update `runtime_test.go`** — the existing `TestRuntimeBootstrapAndRender` still works in render-only mode, but now needs:
- A fake auth-proxy in addition to the fake gRPC server (or `auth_proxy_endpoint=""` short-circuit)
- The rendered TOML's `remote_addr` should now be `127.0.0.1:<bridge_port>`, not `relay.test:20000`

Decision: when `boot.AuthProxyEndpoint == ""`, runtime SKIPS bridge startup and falls back to using the field value as a direct rathole address (Plan 7 behavior). Tests can keep the old shape by setting `auth_proxy_endpoint=""` (server-side: `cfg.AuthProxyPublicAddr=""` falls back to RelayAddr).

Add a NEW test:
```go
func TestRuntimeStartsBridgeWhenAuthProxyConfigured(t *testing.T) {
    // 1. Stand up a fake auth-proxy on 127.0.0.1:0 — returns 200 OK + echoes
    // 2. Stand up the gRPC server with cfg.AuthProxyPublicAddr=<fake_proxy_addr>
    // 3. Run agent runtime
    // 4. Assert rendered toml has remote_addr = 127.0.0.1:<some-port>  (the bridge)
    // 5. Optionally dial the bridge directly + verify it CONNECTS to the fake
}
```

### Step 5: Verify + commit

```bash
go test -count=1 -timeout 60s ./internal/agent/...
go test -race -timeout 120s ./internal/agent/...
go test ./...
go vet ./...
make build
GOOS=linux GOARCH=amd64 go vet ./internal/agent/...
git add internal/agent/bridge.go internal/agent/bridge_test.go internal/agent/render.go internal/agent/render_test.go internal/agent/runtime.go internal/agent/runtime_test.go
git commit -m "feat(agent): add CONNECT bridge between rathole-client and auth-proxy"
```

---

## Task 4: `scripts/smoke-authproxy.sh` end-to-end

Goal: server + auth-proxy + agent all running. Agent's bridge connects via auth-proxy. Verify CONNECT works.

Since rathole-client isn't actually being run (smoke uses render-only), the bridge is **not exercised** by smoke-agent.sh today. We need a smoke that:
1. Starts server.
2. Starts auth-proxy pointing at the same DB.
3. Starts agent with `auth_proxy_endpoint` set in YAML AND `rathole_binary=""` (render-only).
4. Verifies the rendered toml's `remote_addr` is the bridge's local addr (127.0.0.1:<port>).
5. Manually dials the bridge port + sends a small payload + asserts auth-proxy gets the CONNECT (auth-proxy logs visible in stderr).
6. Tear down.

Step 5 is the integration moment. Since we can't easily assert auth-proxy logs from bash, we can:
- Stand up a netcat/socat listener on the rathole control port (127.0.0.1:20000) BEFORE starting the agent, so the auth-proxy's TCP forward has somewhere to land.
- After the bridge connects, the netcat listener should see bytes.

Simpler approach: use `nc -l` to receive on 127.0.0.1:20000, dial the bridge port from a test client, send "ping", verify nc receives it.

Skip if too brittle. Alternative: just verify the rendered toml + check via `lsof` or `ss` that auth-proxy is listening + the agent's bridge port is bound. (lsof is non-portable; skip.)

Pragmatic minimum:
1. All three processes start without error.
2. Agent's rathole-client.toml has `remote_addr = "127.0.0.1:<bridge_port>"` (i.e. NOT the auth-proxy's address).
3. Hitting the auth-proxy port with `curl -X CONNECT` shows it responds to HTTP.
4. Doing a real CONNECT with the agent's raw token from the install command via curl returns 502 (because no rathole-server is listening) — but 502 means "auth passed, backend dial failed", which is what we want. Returns 401 means auth failed.

Yeah — the curl-CONNECT-with-real-token-expects-502 test is clean. Let's do that.

```bash
# After agent renders + we have RAW_TOKEN:
RESPONSE=$(curl -sS -X CONNECT \
    --proxy-header "Authorization: Bearer $RAW_TOKEN" \
    -o /dev/null -w "%{http_code}" \
    --connect-to "relay:443:127.0.0.1:$AUTHPROXY_PORT" \
    https://relay:443/  || true)
# Expect 502 (auth ok, backend dial fails — no rathole-server)
```

If curl's CONNECT semantics are too obscure, fall back to a tiny inline Python or netcat scriptlet that writes the CONNECT preamble manually.

Easiest approach: write a small inline Python (already used by smoke.sh per the implementer's note) that opens a TCP socket to `127.0.0.1:<authproxy_port>`, writes the CONNECT preamble, reads the status line, asserts:
- Status `200 OK` AND token valid AND backend listener present → success
- Status `401` if token bad
- Status `502` if no backend (which is the normal Phase 1 case since no real rathole)

For the smoke flow, we'll spin up a tiny netcat-style listener on 127.0.0.1:20000 (the project's minP) BEFORE the test, so the CONNECT 200 path works:

```bash
python3 -c "
import socket, threading, time, sys
srv = socket.socket(); srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(('127.0.0.1', 20000)); srv.listen(1)
def serve():
    c, _ = srv.accept()
    data = c.recv(4096)
    c.send(b'ECHO: ' + data)
    c.close()
threading.Thread(target=serve, daemon=True).start()
time.sleep(60)  # keep alive
" &
NC_PID=$!
trap "kill $NC_PID 2>/dev/null" EXIT
```

Then the smoke does:
```bash
RESULT=$(python3 -c "
import socket
s = socket.socket(); s.connect(('127.0.0.1', $AUTHPROXY_PORT))
s.send(b'CONNECT relay:443 HTTP/1.1\r\nAuthorization: Bearer $RAW_TOKEN\r\nHost: relay:443\r\n\r\n')
status = s.recv(64).decode().split('\r\n')[0]
if not status.startswith('HTTP/1.1 200'):
    print('FAIL', status); exit(1)
s.send(b'hello-tunnel')
data = s.recv(64)
print(data.decode())
")
if echo "$RESULT" | grep -q 'ECHO: hello-tunnel'; then
    echo 'authproxy: PASS'
else
    echo "FAIL: authproxy didn't forward correctly: $RESULT" >&2
    exit 1
fi
```

Done. This proves: token validation + project routing + bidirectional TCP forwarding all work end-to-end.

### Final smoke output:

```
PASS: end-to-end auth-proxy CONNECT + token + forwarding
```

### Commit
```bash
chmod +x scripts/smoke-authproxy.sh
./scripts/smoke-authproxy.sh
git add scripts/smoke-authproxy.sh
git commit -m "test(smoke): end-to-end auth-proxy CONNECT + forwarding"
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
GOOS=linux GOARCH=amd64 go vet ./...
```

All green.

---

## Self-Review

| Plan-8 requirement | Implemented in |
|---|---|
| Single public ingress on a configured port | Task 2 |
| Token authentication on ingress | Task 1 (router via SiteAgentTokenDAO.ValidateRaw) |
| Per-project routing to loopback rathole | Task 1 (router) |
| Bidirectional TCP forwarding | Task 1 (server.handle) |
| Bootstrap field rename | Task 0 |
| Agent transparently uses auth-proxy | Task 3 (bridge) |
| End-to-end smoke | Task 4 |

**Deferred to follow-ups:**
- TLS termination (Plan 10 — front with nginx/Caddy)
- Rate limiting / abuse mitigation (Plan 11)
- Per-tunnel ACL (Plan 11+)
- gRPC-based token validate (instead of shared DB) (Plan 11+)
- Operator CLI use of CONNECT (Plan 9 may extend)
- Dynamic re-target on rebootstrap when auth-proxy address changes mid-run (Plan 11+)

**Open question for Plan 9:**
- Should operators also CONNECT through auth-proxy when running `quicktun forward`? Probably yes — same code path. Plan 9 will add operator-token CONNECT support to the same auth-proxy (or operators tunnel through rathole's normal client path with no auth-proxy needed).
