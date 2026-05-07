#!/usr/bin/env bash
# scripts/smoke-cli.sh — end-to-end smoke for the quicktun operator CLI.
#
# Spins up server + auth-proxy + a fake "service backend" listening on the
# allocated relay_port, then drives the new operator CLI:
#   1) login (saves a session token to a temp credentials file)
#   2) project list --json (verifies the seeded project is visible)
#   3) forward (auth-proxy -> service backend) and dial through it.
#
# Project / site / service seeding still goes through the gRPC-gateway HTTP
# endpoint (no admin "create" verbs in the operator CLI yet) — but every
# operator-facing assertion uses the new CLI binary, never curl.
set -euo pipefail

cd "$(dirname "$0")/.."

WORKDIR=$(mktemp -d)
SERVER_PID=""
AUTHPROXY_PID=""
NC_PID=""
FORWARD_PID=""

cleanup() {
    set +e
    if [ -n "$FORWARD_PID" ]; then
        kill "$FORWARD_PID" 2>/dev/null
        wait "$FORWARD_PID" 2>/dev/null
    fi
    if [ -n "$AUTHPROXY_PID" ]; then
        kill "$AUTHPROXY_PID" 2>/dev/null
        wait "$AUTHPROXY_PID" 2>/dev/null
    fi
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" 2>/dev/null
        wait "$SERVER_PID" 2>/dev/null
    fi
    if [ -n "$NC_PID" ]; then
        kill "$NC_PID" 2>/dev/null
        wait "$NC_PID" 2>/dev/null
    fi
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

# Distinct ports from other smoke scripts so they can run concurrently in CI.
GRPC_PORT=19663
HTTP_PORT=19200
AUTHPROXY_PORT=18445
LOCAL_PORT=12222
# Project relay_port_range minP. The fake backend binds here; the auth-proxy
# enforces target == 127.0.0.1:<svcPort>, and ssh's relay_port is the first
# allocation = minP.
RELAY_MIN=22000
HTTP_BASE="http://127.0.0.1:${HTTP_PORT}"

# 1. Server config.
cat > "$WORKDIR/server.yaml" <<EOF
control_plane:
  grpc_listen: 127.0.0.1:${GRPC_PORT}
  http_listen: 127.0.0.1:${HTTP_PORT}
  relay_addr: 127.0.0.1
database:
  driver: sqlite
  dsn: ${WORKDIR}/quicktun.db?_foreign_keys=on
session:
  default_ttl: 8h
log:
  path: ""
  level: info
backend:
  rathole_binary: ""
  rathole_config_dir: ${WORKDIR}/relays
  auth_proxy_public_addr: 127.0.0.1:${AUTHPROXY_PORT}
EOF

make build > /dev/null
./bin/quicktun-server migrate --config "$WORKDIR/server.yaml"
./bin/quicktun-server admin create-operator --config "$WORKDIR/server.yaml" \
    --email=admin@x.com --password=testpass --admin
echo "seed: PASS"

# 2. Start the server.
./bin/quicktun-server serve --config "$WORKDIR/server.yaml" >"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

# Wait for HTTP gateway (proxy for "server is ready").
for i in $(seq 1 40); do
    if curl -s -o /dev/null "${HTTP_BASE}/v1/auth:whoami"; then break; fi
    sleep 0.2
done

# 3. Seed project / site / service via HTTP gateway (admin token).
ADMIN_TOKEN=$(curl -sS -X POST "${HTTP_BASE}/v1/auth:login" \
    -H 'Content-Type: application/json' \
    -d '{"email":"admin@x.com","password":"testpass"}' \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["accessToken"])')
[ -n "$ADMIN_TOKEN" ] || { echo "FAIL: admin login" >&2; exit 1; }

curl -sS -X POST "${HTTP_BASE}/v1/projects?project_id=cli-test" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
    -d "{\"displayName\":\"CLI Test\",\"relayPortRange\":\"${RELAY_MIN}-22099\"}" >/dev/null

curl -sS -X POST "${HTTP_BASE}/v1/projects/cli-test/sites?site_id=bastion" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
    -d '{"displayName":"Bastion"}' >/dev/null

SVC_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects/cli-test/sites/bastion/services?service_id=ssh" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
    -d '{"displayName":"ssh","targetAddr":"127.0.0.1","targetPort":22,"proto":"PROTO_TCP"}')
RELAY_PORT=$(echo "$SVC_RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin)["relayPort"])')
[ -n "$RELAY_PORT" ] || { echo "FAIL: capture relay_port: $SVC_RESP" >&2; exit 1; }

echo "seed projects: PASS (relay_port=$RELAY_PORT)"

# 4. Stand up the fake service backend on RELAY_PORT.
# Accepts every connection: sends "BACKEND_HELLO\n", drains, closes.
python3 -u -c "
import socket, threading, time
srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(('127.0.0.1', $RELAY_PORT))
srv.listen(8)
def serve():
    while True:
        try:
            c, _ = srv.accept()
        except OSError:
            return
        try:
            c.send(b'BACKEND_HELLO\n')
            c.settimeout(0.5)
            try:
                c.recv(4096)
            except Exception:
                pass
        finally:
            c.close()
threading.Thread(target=serve, daemon=True).start()
time.sleep(60)
" >"$WORKDIR/fake-backend.log" 2>&1 &
NC_PID=$!
sleep 0.3

# 5. Auth-proxy config + start.
cat > "$WORKDIR/authproxy.yaml" <<EOF
listen_addr: 127.0.0.1:${AUTHPROXY_PORT}
database:
  dsn: ${WORKDIR}/quicktun.db?_foreign_keys=on
log:
  level: info
EOF
./bin/quicktun-authproxy run --config "$WORKDIR/authproxy.yaml" >"$WORKDIR/authproxy.log" 2>&1 &
AUTHPROXY_PID=$!
for i in $(seq 1 30); do
    if (echo > /dev/tcp/127.0.0.1/${AUTHPROXY_PORT}) 2>/dev/null; then break; fi
    sleep 0.2
done
if ! (echo > /dev/tcp/127.0.0.1/${AUTHPROXY_PORT}) 2>/dev/null; then
    echo "FAIL: auth-proxy never bound on 127.0.0.1:${AUTHPROXY_PORT}" >&2
    cat "$WORKDIR/authproxy.log" >&2 || true
    exit 1
fi
echo "authproxy started: PASS"

# 6. CLI login -> writes credentials.yaml to a temp path.
CRED_FILE="$WORKDIR/quicktun-creds.yaml"
./bin/quicktun --config "$CRED_FILE" login \
    --endpoint 127.0.0.1:${GRPC_PORT} \
    --email admin@x.com \
    --password testpass \
    --auth-proxy 127.0.0.1:${AUTHPROXY_PORT} \
    --insecure >"$WORKDIR/login.log" 2>&1
[ -f "$CRED_FILE" ] || { echo "FAIL: credentials not saved at $CRED_FILE" >&2; cat "$WORKDIR/login.log" >&2; exit 1; }
echo "login: PASS"

# 7. CLI project list --json -> must include projects/cli-test.
LIST_OUT=$(./bin/quicktun --config "$CRED_FILE" project list --json)
if ! echo "$LIST_OUT" | python3 -c '
import json, sys
items = json.load(sys.stdin)
sys.exit(0 if any(p.get("name") == "projects/cli-test" for p in items) else 1)
'; then
    echo "FAIL: project list missing projects/cli-test" >&2
    echo "$LIST_OUT" >&2
    exit 1
fi
echo "project list: PASS"

# 8. CLI forward in the background, then dial 127.0.0.1:LOCAL_PORT through it.
./bin/quicktun --config "$CRED_FILE" forward cli-test/bastion/ssh \
    --local-port ${LOCAL_PORT} >"$WORKDIR/forward.log" 2>&1 &
FORWARD_PID=$!

for i in $(seq 1 30); do
    if (echo > /dev/tcp/127.0.0.1/${LOCAL_PORT}) 2>/dev/null; then break; fi
    if ! kill -0 "$FORWARD_PID" 2>/dev/null; then
        echo "FAIL: forward exited before listener was ready" >&2
        cat "$WORKDIR/forward.log" >&2 || true
        exit 1
    fi
    sleep 0.2
done

RESULT=$(LOCAL_PORT=$LOCAL_PORT python3 -c "
import os, socket
s = socket.socket()
s.settimeout(5)
s.connect(('127.0.0.1', int(os.environ['LOCAL_PORT'])))
data = b''
s.settimeout(2)
try:
    while True:
        chunk = s.recv(64)
        if not chunk: break
        data += chunk
except socket.timeout:
    pass
print(data.decode(errors='replace'))
")
if echo "$RESULT" | grep -q 'BACKEND_HELLO'; then
    echo "forward: PASS"
else
    echo "FAIL: forward did not deliver backend payload; got: $RESULT" >&2
    echo '--- forward.log ---' >&2; cat "$WORKDIR/forward.log" >&2 || true
    echo '--- authproxy.log ---' >&2; cat "$WORKDIR/authproxy.log" >&2 || true
    echo '--- fake-backend.log ---' >&2; cat "$WORKDIR/fake-backend.log" >&2 || true
    exit 1
fi

echo "PASS: end-to-end CLI login + list + forward"
