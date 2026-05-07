#!/usr/bin/env bash
# Installs quicktun-server + quicktun-authproxy on a Linux host.
# Usage: sudo ./install-server.sh
#
# Expects binaries under <repo>/bin (run `make build` first). Idempotent —
# safe to re-run for upgrades.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

require_root
require_linux

REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SERVER_BIN="$REPO_ROOT/bin/quicktun-server"
AUTHPROXY_BIN="$REPO_ROOT/bin/quicktun-authproxy"

[[ -x "$SERVER_BIN" ]] || fail "missing $SERVER_BIN — run 'make build' first"
[[ -x "$AUTHPROXY_BIN" ]] || fail "missing $AUTHPROXY_BIN — run 'make build' first"

# 1. System user + dirs.
ensure_user quicktun /var/lib/quicktun
ensure_dir /etc/quicktun            quicktun 0750
ensure_dir /var/lib/quicktun        quicktun 0750
ensure_dir /var/log/quicktun        quicktun 0750
ensure_dir /var/lib/quicktun/relays quicktun 0750

# 2. Install binaries.
log "installing binaries to /usr/local/bin"
install -m 0755 "$SERVER_BIN"    /usr/local/bin/quicktun-server
install -m 0755 "$AUTHPROXY_BIN" /usr/local/bin/quicktun-authproxy

# 3. Install systemd units.
log "installing systemd units"
install -m 0644 "$SCRIPT_DIR/systemd/quicktun-server.service"    /etc/systemd/system/
install -m 0644 "$SCRIPT_DIR/systemd/quicktun-authproxy.service" /etc/systemd/system/
systemctl daemon-reload

# 4. Install example configs (only if missing — never clobber operator edits).
log "installing example configs (if not present)"
ensure_file_unchanged_or_prompt /etc/quicktun/server.yaml    "$SCRIPT_DIR/etc/server.yaml.example"
ensure_file_unchanged_or_prompt /etc/quicktun/authproxy.yaml "$SCRIPT_DIR/etc/authproxy.yaml.example"
chown quicktun:quicktun /etc/quicktun/server.yaml /etc/quicktun/authproxy.yaml
chmod 0640 /etc/quicktun/server.yaml /etc/quicktun/authproxy.yaml

# 5. Run migrations (the server's ExecStartPre will too, but doing it here
#    surfaces config errors immediately).
log "running migrations"
sudo -u quicktun /usr/local/bin/quicktun-server migrate --config /etc/quicktun/server.yaml

# 6. Prompt to create admin operator if none exist yet.
if ! sudo -u quicktun /usr/local/bin/quicktun-server admin list-operators \
        --config /etc/quicktun/server.yaml 2>/dev/null | grep -q .; then
    log "no operators yet; create the first admin"
    read -rp "Admin email: " ADMIN_EMAIL
    read -rsp "Admin password: " ADMIN_PASSWORD; echo
    sudo -u quicktun /usr/local/bin/quicktun-server admin create-operator \
        --config /etc/quicktun/server.yaml \
        --email "$ADMIN_EMAIL" --password "$ADMIN_PASSWORD" --admin
fi

# 7. Enable + start services.
log "enabling + starting services"
systemctl enable --now quicktun-server.service
sleep 1
systemctl enable --now quicktun-authproxy.service

log "Done. Edit /etc/quicktun/server.yaml + authproxy.yaml as needed, then:"
log "  systemctl status quicktun-server quicktun-authproxy"
log "  journalctl -u quicktun-server -f"
log ""
log "Next: install nginx (see deploy/nginx/) and run certbot."
