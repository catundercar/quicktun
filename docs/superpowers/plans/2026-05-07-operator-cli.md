# quicktun Operator CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A single `quicktun` CLI binary for operators. Phase 1 functionality:
1. `quicktun login` — authenticate against the control plane, persist a session token locally.
2. `quicktun project|site|service [list|get|create|delete]` — gRPC-based CRUD wrappers.
3. `quicktun forward <service-name> --local-port N` — open a local TCP listener that tunnels through the auth-proxy to the named service's relay port.

**Out of scope (deferred):**
- mTLS / cert pinning for the gRPC client (Plan 11).
- Configuration profiles / multi-environment (Phase 1 ships single profile).
- TUI dashboards.
- Operator service-account tokens (only interactive login for now).
- Windows agent (Plan 7.5).

---

## Key design decisions

### Token contract for operator CLI vs auth-proxy

Phase 1 ships **two token types** that the auth-proxy accepts:
1. **Site agent token** (Plan 6.5 contract): raw → sha256 → `site_agent_tokens.token_hash`. Routes to that site's project's rathole **control port**. Used by agents.
2. **Operator session token** (already issued by `AuthService.CreateSession`): raw token already validated by `auth.NewUnaryInterceptor` against `operator_sessions.token_hash`. Routes to a **specific service relay port** within a project the operator has access to.

The auth-proxy resolves token type by **trying agent path first, then operator path**. Each path validates and routes differently. The CONNECT host:port matters only for operator (target service relay port); agents send a dummy host.

### Forward request shape

operator CLI sends `CONNECT 127.0.0.1:<service.relay_port> HTTP/1.1` + `Authorization: Bearer <session_token>`. Auth-proxy:
1. Validates session token via `dao.OperatorSessionDAO.ValidateRaw` (or equivalent — verify in code).
2. Looks up the service whose `relay_port == <port>`. Resolves project. 
3. Verifies operator has access to that project (`operator_project_access` table).
4. Forwards to `127.0.0.1:<port>`.

### Local config storage

```
~/.config/quicktun/credentials.yaml
```

Format:
```yaml
endpoint: control.example.com:9090       # gRPC endpoint
auth_proxy_endpoint: relay.example.com:8443
session_token: <opaque>
operator_email: user@x.com
tls_insecure: true                        # dev default; flip in prod
```

`auth_proxy_endpoint` is captured at login-time from a server response (Phase 2: have AuthService.CreateSession echo it back; Phase 1: pass via CLI flag during `login` and persist).

CLI commands read this file. `--config` flag overrides path.

---

## File Structure

### New
```
cmd/quicktun/
├── main.go                       cobra root + persistent flags
├── cmd_login.go                  quicktun login
├── cmd_project.go                quicktun project [list|get|create|delete]
├── cmd_site.go                   quicktun site [list|get|create|delete]
├── cmd_service.go                quicktun service [list|get|create|delete]
├── cmd_forward.go                quicktun forward <name> --local-port N
├── cmd_version.go

internal/clicred/
├── store.go                      Load/Save credentials.yaml
├── store_test.go

scripts/
├── smoke-cli.sh                  end-to-end with the new CLI
```

### Modified
```
internal/authproxy/
├── router.go                     Route accepts (rawToken, target host:port);
                                  tries agent path then operator path
├── router_test.go                add operator-token path tests
├── server.go                     pass req.Host to router
├── server_test.go                update RouteFunc signature in tests

Makefile                          add quicktun binary target

scripts/
├── smoke-authproxy.sh            update Python CONNECT client if Router signature shift breaks the existing token path

internal/dao/operator.go (or wherever)
                                  add ValidateSessionRaw if not present;
                                  must return OperatorID + error
```

---

## Task 0: CLI framework + `login` + credentials store

### Step 1: `internal/clicred/store.go`

Public API:
```go
type Credentials struct {
    Endpoint          string `yaml:"endpoint"`
    AuthProxyEndpoint string `yaml:"auth_proxy_endpoint"`
    SessionToken      string `yaml:"session_token"`
    OperatorEmail     string `yaml:"operator_email"`
    TLSInsecure       bool   `yaml:"tls_insecure"`
}

// DefaultPath returns the resolved credentials path.
// Honors $QUICKTUN_CONFIG, falls back to $XDG_CONFIG_HOME/quicktun/credentials.yaml,
// finally ~/.config/quicktun/credentials.yaml.
func DefaultPath() (string, error)

func Load(path string) (*Credentials, error)   // reads + yaml unmarshal
func Save(path string, c *Credentials) error   // creates parent dir; mode 0o600
```

Tests in `store_test.go`: round-trip, default-path resolution under different env vars (use `t.Setenv`), file-not-found error, file-mode preservation (saved file is 0o600, parent dir 0o700).

### Step 2: `cmd/quicktun/main.go` and `cmd_login.go`

Match existing `cmd/quicktun-agent/` style. Persistent flags on root:
- `--config string` — path to credentials file (default: `clicred.DefaultPath()`)

Subcommand `login`:
- Args: `--endpoint <addr>` (required first time), `--email <addr>`, `--password <-/stdin>`, `--auth-proxy <addr>`, `--insecure`
- Behavior: dial `--endpoint` via gRPC; call `AuthService.CreateSession(email, password)`; store token + endpoint + auth-proxy in credentials.

Read `internal/grpcsvc/auth_service.go` to confirm the actual RPC name and request/response shape. Pattern after `scripts/smoke.sh`'s login flow (it does it via curl; we do it via gRPC client).

### Step 3: Wire dial helpers

Add `cmd/quicktun/dial.go` (small helper):
```go
// dialControl returns a gRPC client conn using the supplied creds.
// Adds Bearer interceptor if creds.SessionToken is non-empty.
func dialControl(c *clicred.Credentials) (*grpc.ClientConn, error)
```

Reuse the bearer per-RPC creds pattern from `internal/agent/runtime.go::bearerCreds`. Or just write a small `grpc.WithPerRPCCredentials(...)` inline.

### Step 4: Makefile

Add `CLI_BIN := $(BINDIR)/quicktun`. Add to `build` target. Add the build rule (no migrations dep).

### Step 5: Verify + commit

```bash
cd /Users/tulip/project/repos/quicktun
go test -count=1 ./internal/clicred/... ./...
go vet ./...
make build
./bin/quicktun --help
./bin/quicktun login --help
./scripts/smoke.sh           # ensure no regression
git add cmd/quicktun/ internal/clicred/ Makefile
git commit -m "feat(cli): add quicktun CLI scaffold + login command"
```

---

## Task 1: Management subcommands (project / site / service)

For each resource, support `list`, `get`, `create`, `delete`. Phase 1 doesn't need `update` (operators can re-create). Output: human-readable table for `list`/`get`, success message for mutations. Add `--json` flag for machine-readable.

### Step 1: Generic helpers

`cmd/quicktun/output.go`:
```go
// printTable renders rows with column headers; aligned via tabwriter.
func printTable(headers []string, rows [][]string)
// printJSON marshals to indented JSON.
func printJSON(v any)
```

### Step 2: One file per resource family

`cmd_project.go`:
- `project list` — calls `ProjectService.ListProjects` (existing RPC). Renders slug, status, port range, created_at.
- `project get <slug>` — `GetProject`.
- `project create <slug> --display-name X --port-range MIN-MAX` — `CreateProject`.
- `project delete <slug>` — `DeleteProject` (interactive confirm unless `--yes`).

Mirror pattern for `cmd_site.go` and `cmd_service.go`. Resource hierarchy uses google.aip naming (`projects/X/sites/Y/services/Z`).

### Step 3: Resource name convenience

Operators don't want to type `projects/<slug>` every time. Accept either:
- bare slug (`my-project`) — auto-prefix with `projects/`
- full name (`projects/my-project`) — pass through

Add a small helper `internal/clicred/names.go` (or `cmd/quicktun/names.go`):
```go
// canonicalize prepends prefix if name has only k or fewer slashes.
// canonicalize("p1", "projects/")            -> "projects/p1"
// canonicalize("projects/p1", "projects/")   -> "projects/p1"
// canonicalize("p1/s1", "projects/")         -> "projects/p1/s1" (depends on prefix logic)
```

Site/service take longer names; pattern accordingly.

### Step 4: Tests

These are mostly integration: stand up a real server in-process (via `internal/server.New` against in-memory SQLite, like Plan 7's runtime_test.go does), seed an admin operator + session, run subcommand `Execute()` programmatically, assert JSON output.

Add `cmd/quicktun/cli_test.go` with table-driven tests for at least `project list` and `project create`.

If the test infra is too painful, defer with one-line happy-path tests that just check `cmd.Help()` doesn't panic. The smoke (Task 4) will exercise real CRUD.

### Step 5: Verify + commit

```bash
go test ./...
go vet ./...
make build
./bin/quicktun project list   # without login: expect a friendly error
git add cmd/quicktun/
git commit -m "feat(cli): add project/site/service CRUD subcommands"
```

---

## Task 2: Auth-proxy operator token support

Currently `internal/authproxy/Router.Route(ctx, rawToken) (string, error)` only validates against site agent tokens and routes to the project's rathole control port (minP). Extend it to also accept operator session tokens.

### Step 1: Identify the operator session validation path

Read existing `internal/auth/interceptor.go` (operator interceptor) to find how it validates the session token. There's likely a DAO method or inline lookup against `operator_sessions.token_hash`. Trace it.

If the existing path is `auth.SessionFromContext` populated by interceptor, that's runtime ctx — not callable from authproxy. Need a DAO helper.

If `internal/dao/operator.go` (or `session.go`) doesn't have `ValidateSessionRaw(ctx, raw) (operatorID uint64, err error)`, ADD ONE that mirrors `SiteAgentTokenDAO.ValidateRaw`:
- Hash `raw` via `auth.HashToken`
- Look up by `token_hash`
- Validate expiry
- Return `OperatorID` + nil, or `gorm.ErrRecordNotFound` for invalid/expired

Add a unit test for the new DAO method.

### Step 2: Extend `Router.Route` signature

```go
// Route validates rawToken and decides which loopback backend to forward to,
// based on the type of token + the requested target.
//
// For site agent tokens: target is ignored; backend = "127.0.0.1:<minP-of-project>".
// For operator session tokens: target must be "127.0.0.1:<port>" where <port>
// is a service.relay_port within a project the operator has access to;
// backend = target.
func (r *Router) Route(ctx context.Context, rawToken, target string) (string, error)
```

Update `RouteFunc` type alias accordingly.

Implementation:
1. Try agent path: `SiteAgentTokenDAO.ValidateRaw(ctx, raw)`. On success → existing behavior (ignore target, return minP).
2. Try operator path: `OperatorSessionDAO.ValidateSessionRaw(ctx, raw)`. On success:
   - Parse `target` as `127.0.0.1:<port>`. Reject if not loopback.
   - `db.Where("relay_port = ?", port).First(&service)`. Reject if not found.
   - Walk to project. Reject if disabled.
   - Verify operator has access: `db.Where("operator_id = ? AND project_id = ?", operatorID, projectID).First(&access)`. Reject if not found.
   - Return `target` verbatim.
3. Both fail → ErrUnauthenticated.

### Step 3: Update Server to pass req.Host to Route

In `internal/authproxy/server.go::handle`:
```go
// req.Host is "host:port" from the CONNECT line (e.g. "relay:443" for agents,
// "127.0.0.1:20022" for operator forward).
backend, err := s.route(ctx, rawToken, req.Host)
```

### Step 4: Tests

Add to `router_test.go`:
- `TestRouterOperatorTokenForwardsToServicePort` — happy path.
- `TestRouterOperatorTokenRejectsForeignProject` — operator has no access to that project.
- `TestRouterOperatorTokenRejectsNonLoopbackTarget` — target like `evil.example.com:80` rejected.
- `TestRouterOperatorTokenRejectsUnknownPort` — port not matching any service.
- `TestRouterAgentTokenIgnoresTarget` — agent token with arbitrary target still routes to minP.

Update `server_test.go` `RouteFunc` signature in stubs.

### Step 5: smoke-authproxy.sh adjustment

The existing smoke uses an agent token + dummy `CONNECT relay:443`. With the new signature, the agent path still works (target ignored). No script change needed — confirm by running.

### Step 6: Verify + commit

```bash
go test ./internal/authproxy/... ./internal/dao/...
go test ./...
go vet ./...
make build
./scripts/smoke.sh
./scripts/smoke-agent.sh
./scripts/smoke-authproxy.sh
git add internal/authproxy/ internal/dao/
git commit -m "feat(authproxy): accept operator session tokens for service forwarding"
```

---

## Task 3: `quicktun forward` subcommand

### Step 1: Resolve target service via gRPC

`cmd_forward.go::run`:
1. Load credentials.
2. Dial gRPC; `GetService(ctx, &GetServiceRequest{Name: "projects/p1/sites/s1/services/svc"})`.
3. Extract `relay_port` and `display_name`.
4. Resolve target := `127.0.0.1:<relay_port>`.
5. Bind local listener on `--local-port` (or random if 0). Print "forwarding 127.0.0.1:<localPort> -> <service display name>".
6. For each accepted local conn, spawn a handler that:
   - Dials the auth-proxy (`creds.AuthProxyEndpoint`).
   - Sends `CONNECT 127.0.0.1:<relay_port> HTTP/1.1\r\nHost: 127.0.0.1:<relay_port>\r\nAuthorization: Bearer <session>\r\n\r\n`.
   - Reads `200 OK`. On non-200, log + close local.
   - `io.Copy` both directions.
7. Trap SIGTERM/SIGINT; close listener; drain in-flight; exit cleanly.

This logic is essentially `internal/agent/bridge.go` but as a one-shot CLI command. Consider extracting the CONNECT-handshake helper to a shared package (`internal/connectbridge/`?) — or just copy the handler. Phase 1: copy. Refactor opportunity later.

### Step 2: Tests

Stand up the full stack (server + auth-proxy + a fake "service backend") in a single test, run forward as a goroutine, verify a TCP roundtrip works. Pattern after smoke-authproxy.sh's structure but inside `cmd/quicktun/cli_forward_test.go`.

If too complex: just have an integration test that exercises the CONNECT handler logic directly (without spinning up auth-proxy). The auth-proxy already has its own tests.

### Step 3: Verify + commit

```bash
go test ./...
go vet ./...
make build
git add cmd/quicktun/ internal/connectbridge/
# (only include connectbridge if you extracted it)
git commit -m "feat(cli): add quicktun forward subcommand"
```

---

## Task 4: `scripts/smoke-cli.sh`

End-to-end through the new CLI. Use the binary, not curl.

```bash
#!/usr/bin/env bash
set -euo pipefail
# ... same setup as smoke.sh: server, project/site/service, agent token rotation ...

# Login.
./bin/quicktun login --endpoint 127.0.0.1:$GRPC_PORT --email admin@x.com \
    --password testpass --auth-proxy 127.0.0.1:$AUTHPROXY_PORT --insecure \
    --config "$WORKDIR/quicktun-creds.yaml"
echo "login: PASS"

# List projects.
./bin/quicktun project list --config "$WORKDIR/quicktun-creds.yaml" --json | grep -q smoke-test
echo "project list: PASS"

# Forward to ssh service.
# Stand up a fake service backend on 127.0.0.1:<service.relay_port> so the
# tunnel has somewhere to land. Service was created with target 127.0.0.1:22
# but for the smoke we override the relay_port to a known value, OR we read it.
RELAY_PORT=$(./bin/quicktun service get \
    projects/smoke-test/sites/smoke-bastion/services/ssh \
    --config "$WORKDIR/quicktun-creds.yaml" --json | python3 -c "import json, sys; print(json.load(sys.stdin)['relayPort'])")

# fake rathole service backend on RELAY_PORT
python3 -c "
import socket, threading, time
srv = socket.socket(); srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(('127.0.0.1', $RELAY_PORT)); srv.listen(1)
def serve():
    c, _ = srv.accept(); c.send(b'BACKEND_HELLO'); c.close()
threading.Thread(target=serve, daemon=True).start()
time.sleep(60)
" &
NC_PID=$!
sleep 0.3

# Run forward in background.
./bin/quicktun forward projects/smoke-test/sites/smoke-bastion/services/ssh \
    --local-port 12222 --config "$WORKDIR/quicktun-creds.yaml" \
    > "$WORKDIR/forward.log" 2>&1 &
FORWARD_PID=$!
sleep 0.5

# Connect to the forward; expect to receive BACKEND_HELLO from the fake service.
RESULT=$(python3 -c "
import socket
s = socket.socket(); s.connect(('127.0.0.1', 12222))
print(s.recv(64).decode())
")
if echo "$RESULT" | grep -q 'BACKEND_HELLO'; then
    echo "forward: PASS"
else
    echo "FAIL: forward did not relay" >&2
    cat "$WORKDIR/forward.log" >&2
    exit 1
fi

echo "PASS: end-to-end CLI login + list + forward"
```

Cleanup all 4-5 PIDs in trap.

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
GOOS=linux GOARCH=amd64 go vet ./...
```

All green.

---

## Self-review

| Plan-9 requirement | Implemented in |
|---|---|
| `quicktun` binary scaffold + persistent flags | Task 0 |
| `login` storing local credentials at 0o600 | Task 0 |
| project / site / service CRUD subcommands | Task 1 |
| auth-proxy accepts operator session tokens | Task 2 |
| `forward <name>` opens local tunnel via auth-proxy | Task 3 |
| End-to-end smoke | Task 4 |

**Deferred to later plans:**
- Operator service-account tokens (Plan 11+ — for CI/CD).
- Multi-profile credentials (Plan 11+).
- mTLS cert pinning (Plan 11).
- TUI / interactive selection.
- Update RPCs (CLI relies on delete + recreate; Plan 9.5 if needed).

**Open questions for Plan 10/11:**
- Once nginx terminates TLS in front of auth-proxy (Plan 10), how does the operator CLI know to dial https vs plain TCP? Phase 1 default: `tls_insecure: true` and plain HTTP CONNECT. Plan 10 will add proper TLS config to credentials.yaml.
- Should `forward` cache the resolved relay_port to avoid a gRPC round-trip on every reconnection? Phase 1: re-fetch each time, simpler. Watch heartbeat-driven config changes — operator must re-run `forward` if relay_port changes.
