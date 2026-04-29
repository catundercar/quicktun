#!/usr/bin/env bash
# scripts/smoke.sh — end-to-end Plan 2 verification.
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

echo "PASS: end-to-end auth flow"
