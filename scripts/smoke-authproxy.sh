#!/usr/bin/env bash
# scripts/smoke-authproxy.sh — end-to-end smoke for quicktun-authproxy.
# Spins up server + auth-proxy + agent + a fake rathole-server backend, then
# verifies CONNECT-based token forwarding and the agent's bridge wiring.
set -euo pipefail

cd "$(dirname "$0")/.."

WORKDIR=$(mktemp -d)
SERVE_PID=""
AUTHPROXY_PID=""
AGENT_PID=""
NC_PID=""

cleanup() {
  set +e
  if [ -n "${AGENT_PID}" ]; then
    kill "${AGENT_PID}" 2>/dev/null
    wait "${AGENT_PID}" 2>/dev/null
  fi
  if [ -n "${AUTHPROXY_PID}" ]; then
    kill "${AUTHPROXY_PID}" 2>/dev/null
    wait "${AUTHPROXY_PID}" 2>/dev/null
  fi
  if [ -n "${SERVE_PID}" ]; then
    kill "${SERVE_PID}" 2>/dev/null
    wait "${SERVE_PID}" 2>/dev/null
  fi
  if [ -n "${NC_PID}" ]; then
    kill "${NC_PID}" 2>/dev/null
    wait "${NC_PID}" 2>/dev/null
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

DB="$WORKDIR/qt.db"
CFG="$WORKDIR/server.yaml"
GRPC_PORT=19553
HTTP_PORT=19190
AUTHPROXY_PORT=18444
RATHOLE_CTRL_PORT=20000  # project's minP — auth-proxy will forward here
HTTP_BASE="http://127.0.0.1:${HTTP_PORT}"

cat > "$CFG" <<EOF
control_plane:
  grpc_listen: 127.0.0.1:${GRPC_PORT}
  http_listen: 127.0.0.1:${HTTP_PORT}
  relay_addr: 127.0.0.1
database:
  driver: sqlite
  dsn: ${DB}?_foreign_keys=on
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
./bin/quicktun-server migrate --config "$CFG"
./bin/quicktun-server admin create-operator --config "$CFG" --email=test@x.com --password=hunter2 --admin

./bin/quicktun-server serve --config "$CFG" >"$WORKDIR/server.log" 2>&1 &
SERVE_PID=$!

# Wait up to 5s for HTTP listener.
for i in $(seq 1 25); do
  if curl -s -o /dev/null "${HTTP_BASE}/v1/auth:whoami"; then break; fi
  sleep 0.2
done

# Login (operator).
ADMIN_TOKEN=$(curl -sS -X POST "${HTTP_BASE}/v1/auth:login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@x.com","password":"hunter2"}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["accessToken"])')

if [ -z "${ADMIN_TOKEN:-}" ]; then
  echo "FAIL: did not receive operator access token" >&2
  exit 1
fi

# Project with relay_port_range starting at 20000 (so minP == RATHOLE_CTRL_PORT).
CREATE_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects?project_id=smoke-test" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Smoke","relayPortRange":"20000-20099"}')
if ! echo "$CREATE_RESP" | grep -q '"name":"projects/smoke-test"'; then
  echo "FAIL: project create failed: $CREATE_RESP" >&2
  exit 1
fi
echo "project: PASS"

# Create site.
SITE_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects/smoke-test/sites?site_id=smoke-bastion" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Smoke Bastion"}')
if ! echo "$SITE_RESP" | grep -q '"name":"projects/smoke-test/sites/smoke-bastion"'; then
  echo "FAIL: site create failed: $SITE_RESP" >&2
  exit 1
fi
echo "site: PASS"

# Service so Bootstrap returns a tunnel binding.
SVC_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion/services?service_id=ssh" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"SSH","targetAddr":"127.0.0.1","targetPort":22,"proto":"PROTO_TCP"}')
if ! echo "$SVC_RESP" | grep -q '"name":"projects/smoke-test/sites/smoke-bastion/services/ssh"'; then
  echo "FAIL: service create failed: $SVC_RESP" >&2
  exit 1
fi
echo "service: PASS"

# Rotate token; capture raw value.
ROTATE_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion:rotateAgentToken" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' -d '{}')
RAW_TOKEN=$(echo "$ROTATE_RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
if [ -z "$RAW_TOKEN" ]; then
  echo "FAIL: could not capture raw agent token: $ROTATE_RESP" >&2
  exit 1
fi
echo "rotate: PASS"

# Stand up a fake rathole-server backend on 127.0.0.1:20000. It accepts ONE
# TCP connection, reads up to 4096 bytes, replies "ECHO: <bytes>", closes.
# Proves auth-proxy forwarded TCP correctly.
python3 -u -c "
import socket, threading, time, sys
srv = socket.socket()
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(('127.0.0.1', $RATHOLE_CTRL_PORT))
srv.listen(1)
def serve():
    c, _ = srv.accept()
    data = c.recv(4096)
    c.send(b'ECHO: ' + data)
    time.sleep(0.5)
    c.close()
threading.Thread(target=serve, daemon=True).start()
time.sleep(60)
" >"$WORKDIR/fake-rathole.log" 2>&1 &
NC_PID=$!
sleep 0.3  # let it bind

# Auth-proxy config + start.
cat > "$WORKDIR/authproxy.yaml" <<EOF
listen_addr: 127.0.0.1:${AUTHPROXY_PORT}
database:
  dsn: ${DB}?_foreign_keys=on
log:
  level: info
EOF

./bin/quicktun-authproxy run --config "$WORKDIR/authproxy.yaml" >"$WORKDIR/authproxy.log" 2>&1 &
AUTHPROXY_PID=$!

# Wait for auth-proxy to bind.
for i in $(seq 1 25); do
  if (echo > /dev/tcp/127.0.0.1/${AUTHPROXY_PORT}) 2>/dev/null; then break; fi
  sleep 0.2
done
if ! (echo > /dev/tcp/127.0.0.1/${AUTHPROXY_PORT}) 2>/dev/null; then
  echo "FAIL: auth-proxy never bound on 127.0.0.1:${AUTHPROXY_PORT}" >&2
  cat "$WORKDIR/authproxy.log" >&2 || true
  exit 1
fi
echo "authproxy started: PASS"

# CONNECT through auth-proxy with the real raw token. Expect 200 OK, then
# bytes get forwarded to the fake rathole listener and echoed back.
RESULT=$(RAW_TOKEN="$RAW_TOKEN" AUTHPROXY_PORT="$AUTHPROXY_PORT" python3 -c "
import os, socket, sys
raw_token = os.environ['RAW_TOKEN']
port = int(os.environ['AUTHPROXY_PORT'])
s = socket.socket()
s.settimeout(5)
s.connect(('127.0.0.1', port))
preamble = (
    b'CONNECT relay:443 HTTP/1.1\r\n'
    b'Host: relay:443\r\n'
    b'Authorization: Bearer ' + raw_token.encode() + b'\r\n'
    b'\r\n'
)
s.send(preamble)
status = b''
while not status.endswith(b'\r\n'):
    chunk = s.recv(1)
    if not chunk: break
    status += chunk
status_line = status.decode().rstrip()
if not status_line.startswith('HTTP/1.1 200'):
    print('STATUS:', status_line)
    sys.exit(1)
# Drain headers.
while True:
    line = b''
    while not line.endswith(b'\r\n'):
        ch = s.recv(1)
        if not ch: break
        line += ch
    if line == b'\r\n':
        break
s.send(b'tunnel-payload')
data = b''
s.settimeout(3)
try:
    while True:
        chunk = s.recv(64)
        if not chunk: break
        data += chunk
except socket.timeout:
    pass
print(data.decode(errors='replace'))
")

if echo "$RESULT" | grep -q 'ECHO: tunnel-payload'; then
  echo "authproxy CONNECT + forward: PASS"
else
  echo "FAIL: auth-proxy did not forward correctly. Result: $RESULT" >&2
  echo "--- authproxy.log ---" >&2
  cat "$WORKDIR/authproxy.log" >&2
  echo "--- fake-rathole.log ---" >&2
  cat "$WORKDIR/fake-rathole.log" >&2 || true
  exit 1
fi

# 401 path: bad token gets rejected.
BAD_RESULT=$(AUTHPROXY_PORT="$AUTHPROXY_PORT" python3 -c "
import os, socket, sys
port = int(os.environ['AUTHPROXY_PORT'])
s = socket.socket()
s.settimeout(5)
s.connect(('127.0.0.1', port))
s.send(b'CONNECT relay:443 HTTP/1.1\r\nHost: relay:443\r\nAuthorization: Bearer bogus-token\r\n\r\n')
status = b''
while not status.endswith(b'\r\n'):
    ch = s.recv(1)
    if not ch: break
    status += ch
print(status.decode(errors='replace').rstrip())
")
if echo "$BAD_RESULT" | grep -q '401'; then
  echo "authproxy 401 reject: PASS"
else
  echo "FAIL: bad token did not get 401. Got: $BAD_RESULT" >&2
  exit 1
fi

# Agent: should also be able to start with auth-proxy in front, render
# rathole-client.toml whose remote_addr is the agent's bridge (NOT auth-proxy
# directly). Skip the real rathole subprocess (rathole_binary: "") so this
# stays render-only.
mkdir -p "$WORKDIR/agent-state"
AGENT_CFG="$WORKDIR/agent.yaml"
cat > "$AGENT_CFG" <<EOF
control_endpoint: 127.0.0.1:${GRPC_PORT}
token: ${RAW_TOKEN}
state_dir: ${WORKDIR}/agent-state
rathole_binary: ""
tls_insecure: true
hostname_override: smoke-host
EOF

./bin/quicktun-agent run --config "$AGENT_CFG" >"$WORKDIR/agent.log" 2>&1 &
AGENT_PID=$!

# Wait for rathole-client.toml to render.
RATHOLE_CFG="$WORKDIR/agent-state/rathole-client.toml"
for i in $(seq 1 40); do
  if [ -f "$RATHOLE_CFG" ]; then break; fi
  if ! kill -0 "$AGENT_PID" 2>/dev/null; then
    echo "FAIL: quicktun-agent exited before rendering config" >&2
    cat "$WORKDIR/agent.log" >&2 || true
    exit 1
  fi
  sleep 0.25
done
if [ ! -f "$RATHOLE_CFG" ]; then
  echo "FAIL: rathole-client.toml never rendered at $RATHOLE_CFG" >&2
  cat "$WORKDIR/agent.log" >&2 || true
  exit 1
fi

# remote_addr should be 127.0.0.1:<bridge_port>, not the auth-proxy addr.
if ! grep -qE 'remote_addr = "127\.0\.0\.1:[0-9]+"' "$RATHOLE_CFG"; then
  echo "FAIL: remote_addr is not on 127.0.0.1" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
# And it should NOT be the auth-proxy addr (the bridge is in front of it).
if grep -q "remote_addr = \"127.0.0.1:${AUTHPROXY_PORT}\"" "$RATHOLE_CFG"; then
  echo "FAIL: remote_addr points at auth-proxy directly (bridge missing?)" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
echo "agent bridge in toml: PASS"

echo "PASS: end-to-end auth-proxy CONNECT + token + bridge"
