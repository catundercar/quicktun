#!/usr/bin/env bash
# scripts/smoke.sh — end-to-end auth + project + site verification.
# Spins up the server with a temp DB and verifies login + whoami via HTTP.
set -euo pipefail

cd "$(dirname "$0")/.."

WORKDIR=$(mktemp -d)
trap 'kill ${SERVE_PID:-0} 2>/dev/null; rm -rf "$WORKDIR"' EXIT

DB="$WORKDIR/qt.db"
CFG="$WORKDIR/server.yaml"
GRPC_PORT=19443
HTTP_PORT=19080

cat > "$CFG" <<EOF
control_plane:
  grpc_listen: 127.0.0.1:${GRPC_PORT}
  http_listen: 127.0.0.1:${HTTP_PORT}
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

./bin/quicktun-server serve --config "$CFG" &
SERVE_PID=$!

# Wait up to 5s for HTTP listener.
for i in $(seq 1 25); do
  if curl -s -o /dev/null "http://127.0.0.1:${HTTP_PORT}/v1/auth:whoami"; then break; fi
  sleep 0.2
done

# Login, capture token.
TOKEN=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/auth:login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@x.com","password":"hunter2"}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["accessToken"])')

if [ -z "${TOKEN:-}" ]; then
  echo "FAIL: did not receive access_token" >&2
  exit 1
fi

# WhoAmI with bearer.
WHOAMI=$(curl -sS "http://127.0.0.1:${HTTP_PORT}/v1/auth:whoami" -H "Authorization: Bearer $TOKEN")
echo "whoami: $WHOAMI"
if ! echo "$WHOAMI" | grep -q '"email":"test@x.com"'; then
  echo "FAIL: whoami did not return seeded operator" >&2
  exit 1
fi

# Logout.
curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/auth:logout" \
  -H "Authorization: Bearer $TOKEN" -d '{}' -H 'Content-Type: application/json' > /dev/null

# After logout, whoami should 401.
CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${HTTP_PORT}/v1/auth:whoami" -H "Authorization: Bearer $TOKEN")
if [ "$CODE" != "401" ]; then
  echo "FAIL: whoami after logout returned $CODE, expected 401" >&2
  exit 1
fi

echo "auth: PASS"

# Re-login since we logged out earlier in the script.
LOGIN2=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/auth:login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@x.com","password":"hunter2"}')
TOKEN=$(echo "$LOGIN2" | python3 -c 'import sys,json; print(json.load(sys.stdin)["accessToken"])')

# Create a project via gRPC gateway.
CREATE_RESP=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/projects?project_id=smoke-test" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Smoke","relayPortRange":"20000-20099"}')
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

echo "project: PASS"

# Site flow: create site under project, list, rotate token, delete.
SITE_RESP=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites?site_id=smoke-bastion" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"displayName":"Smoke Bastion"}')
echo "site create: $SITE_RESP"
if ! echo "$SITE_RESP" | grep -q '"name":"projects/smoke-test/sites/smoke-bastion"'; then
  echo "FAIL: site create response missing expected name" >&2
  exit 1
fi

LIST_SITES=$(curl -sS "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites" \
  -H "Authorization: Bearer $TOKEN")
echo "site list: $LIST_SITES"
if ! echo "$LIST_SITES" | grep -q '"name":"projects/smoke-test/sites/smoke-bastion"'; then
  echo "FAIL: site list missing the created site" >&2
  exit 1
fi

ROTATE_RESP=$(curl -sS -X POST "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites/smoke-bastion:rotateAgentToken" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}')
echo "rotate: $ROTATE_RESP"
if ! echo "$ROTATE_RESP" | grep -q '"token":'; then
  echo "FAIL: rotate response missing token" >&2
  exit 1
fi

echo "site: PASS"

# Service flow: create -> list -> delete
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

echo "service: PASS"

# Verify the rathole config file was rendered to disk by relay.Manager.
RELAY_CFG="${WORKDIR}/relays/smoke-test.toml"
if [ ! -f "$RELAY_CFG" ]; then
  echo "FAIL: rathole config $RELAY_CFG was not rendered" >&2
  exit 1
fi
if ! grep -q 'smoke-bastion__ssh' "$RELAY_CFG"; then
  echo "FAIL: rathole config does not mention smoke-bastion__ssh" >&2
  cat "$RELAY_CFG" >&2
  exit 1
fi
echo "relay: PASS"

curl -sS -X DELETE "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites/smoke-bastion/services/ssh" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

curl -sS -X DELETE "http://127.0.0.1:${HTTP_PORT}/v1/projects/smoke-test/sites/smoke-bastion" \
  -H "Authorization: Bearer $TOKEN" > /dev/null

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

echo "PASS: end-to-end auth + project + site + service + relay flow"
