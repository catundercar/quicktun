#!/usr/bin/env bash
# scripts/smoke-agent.sh — end-to-end smoke for quicktun-agent.
# Spins up the server + agent (render-only mode) against a temp DB and
# verifies the agent registers, renders rathole-client.toml with the
# correct sha256 token, and updates Site.hostname / last_seen_time.
set -euo pipefail

cd "$(dirname "$0")/.."

WORKDIR=$(mktemp -d)
SERVE_PID=""
AGENT_PID=""
cleanup() {
  if [ -n "${AGENT_PID}" ]; then
    kill "${AGENT_PID}" 2>/dev/null || true
    wait "${AGENT_PID}" 2>/dev/null || true
  fi
  if [ -n "${SERVE_PID}" ]; then
    kill "${SERVE_PID}" 2>/dev/null || true
    wait "${SERVE_PID}" 2>/dev/null || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

DB="$WORKDIR/qt.db"
CFG="$WORKDIR/server.yaml"
GRPC_PORT=19543
HTTP_PORT=19180
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

# Create project.
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

# Create service so Bootstrap returns a tunnel binding.
SVC_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion/services?service_id=ssh" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"SSH","targetAddr":"127.0.0.1","targetPort":22,"proto":"PROTO_TCP"}')
if ! echo "$SVC_RESP" | grep -q '"name":"projects/smoke-test/sites/smoke-bastion/services/ssh"'; then
  echo "FAIL: service create failed: $SVC_RESP" >&2
  exit 1
fi
echo "service: PASS"

# Rotate the agent token; capture the raw value.
ROTATE_RESP=$(curl -sS -X POST "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion:rotateAgentToken" \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' -d '{}')
RAW_TOKEN=$(echo "$ROTATE_RESP" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("token",""))')
if [ -z "$RAW_TOKEN" ]; then
  echo "FAIL: could not capture raw agent token: $ROTATE_RESP" >&2
  exit 1
fi
echo "rotate: PASS"

# Write the agent config (render-only mode).
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

# Run the agent in the background.
./bin/quicktun-agent run --config "$AGENT_CFG" >"$WORKDIR/agent.log" 2>&1 &
AGENT_PID=$!

# Poll for the rendered config file (up to ~10s).
RATHOLE_CFG="$WORKDIR/agent-state/rathole-client.toml"
for i in $(seq 1 40); do
  if [ -f "$RATHOLE_CFG" ]; then break; fi
  # Detect early agent crash so we don't wait the full 10s for nothing.
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
echo "agent render: PASS"

# Verify content.
EXPECTED_TOKEN_HEX=$(printf '%s' "$RAW_TOKEN" | shasum -a 256 | awk '{print $1}')
EXPECTED_REMOTE_ADDR="127.0.0.1:20000"

if ! grep -qF '[client]' "$RATHOLE_CFG"; then
  echo "FAIL: rathole config missing [client] block" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
if ! grep -qF "remote_addr = \"$EXPECTED_REMOTE_ADDR\"" "$RATHOLE_CFG"; then
  echo "FAIL: rathole config remote_addr != $EXPECTED_REMOTE_ADDR" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
if ! grep -qF "default_token = \"$EXPECTED_TOKEN_HEX\"" "$RATHOLE_CFG"; then
  echo "FAIL: rathole config missing expected sha256 token" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
if ! grep -qF '[client.services.smoke-bastion__ssh]' "$RATHOLE_CFG"; then
  echo "FAIL: rathole config missing service block [client.services.smoke-bastion__ssh]" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
if ! grep -qE '^local_addr = "127\.0\.0\.1:22"' "$RATHOLE_CFG"; then
  echo "FAIL: rathole config missing/incorrect local_addr" >&2
  cat "$RATHOLE_CFG" >&2
  exit 1
fi
echo "agent toml content: PASS"

# Bootstrap also runs UpdateAgentMeta, so site.hostname + last_seen_time are
# updated within milliseconds of the agent dialing — no need to wait for the
# 15s heartbeat tick. Re-fetch to confirm.
SITE_JSON=$(curl -sS -H "Authorization: Bearer $ADMIN_TOKEN" \
  "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion")
HOSTNAME_FROM_DB=$(echo "$SITE_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("hostname",""))')
if [ "$HOSTNAME_FROM_DB" != "smoke-host" ]; then
  echo "FAIL: site.hostname not updated by agent (got: '$HOSTNAME_FROM_DB')" >&2
  echo "$SITE_JSON" >&2
  exit 1
fi
LAST_SEEN=$(echo "$SITE_JSON" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("lastSeenTime",""))')
if [ -z "$LAST_SEEN" ]; then
  echo "FAIL: site.lastSeenTime not set by agent" >&2
  echo "$SITE_JSON" >&2
  exit 1
fi
echo "agent registration: PASS"

# Stop the agent before cleanup so it doesn't see deletes mid-flight.
kill "$AGENT_PID" 2>/dev/null || true
wait "$AGENT_PID" 2>/dev/null || true
AGENT_PID=""

# Cleanup: delete service, site, project.
curl -sS -X DELETE "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion/services/ssh" \
  -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null
curl -sS -X DELETE "${HTTP_BASE}/v1/projects/smoke-test/sites/smoke-bastion" \
  -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null
curl -sS -X DELETE "${HTTP_BASE}/v1/projects/smoke-test" \
  -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null

GET_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
  "${HTTP_BASE}/v1/projects/smoke-test" \
  -H "Authorization: Bearer $ADMIN_TOKEN")
if [ "$GET_CODE" != "404" ]; then
  echo "FAIL: get-after-delete returned $GET_CODE, expected 404" >&2
  exit 1
fi

echo "PASS: end-to-end agent registration + tunnel render"
