#!/usr/bin/env bash
# Installs quicktun-agent on a bastion (agent) host.
#
# Usage:
#   sudo ./install-agent.sh \
#       --token            <RAW_TOKEN>          \
#       --control-endpoint <host:port>          \
#       [--auth-proxy      <host:port>]         \
#       [--tls-insecure]
#
# Get RAW_TOKEN from `quicktun site get-install-command` (or
# `quicktun site rotate-agent-token`). --auth-proxy is currently
# informational — Phase 1 reads the auth-proxy address from the control
# plane's BootstrapResponse, not from agent.yaml. Pass it anyway so the
# operator records the value alongside the install.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

require_root
require_linux

TOKEN=
CONTROL_ENDPOINT=
AUTH_PROXY=
TLS_INSECURE=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --token)            TOKEN=$2;            shift 2 ;;
        --control-endpoint) CONTROL_ENDPOINT=$2; shift 2 ;;
        --auth-proxy)       AUTH_PROXY=$2;       shift 2 ;;
        --tls-insecure)     TLS_INSECURE=true;   shift   ;;
        *) fail "unknown arg: $1" ;;
    esac
done

[[ -n "$TOKEN" && -n "$CONTROL_ENDPOINT" ]] || \
    fail "--token and --control-endpoint are required"

REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENT_BIN="$REPO_ROOT/bin/quicktun-agent"
[[ -x "$AGENT_BIN" ]] || fail "missing $AGENT_BIN — run 'make build' first"

ensure_user quicktun-agent /var/lib/quicktun-agent
ensure_dir /etc/quicktun            quicktun-agent 0750
ensure_dir /var/lib/quicktun-agent  quicktun-agent 0750
ensure_dir /var/log/quicktun-agent  quicktun-agent 0750

install -m 0755 "$AGENT_BIN" /usr/local/bin/quicktun-agent
install -m 0644 "$SCRIPT_DIR/systemd/quicktun-agent.service" /etc/systemd/system/
systemctl daemon-reload

log "writing /etc/quicktun/agent.yaml"
{
    cat <<EOF
control_endpoint: $CONTROL_ENDPOINT
token: $TOKEN
state_dir: /var/lib/quicktun-agent
rathole_binary: /usr/local/bin/rathole
rathole_args: ["--client"]
tls_insecure: $TLS_INSECURE
EOF
    if [[ -n "$AUTH_PROXY" ]]; then
        # Auth-proxy address (informational; agent reads it from the
        # control plane's BootstrapResponse).
        printf '# auth_proxy_endpoint: %s  # informational, ignored by agent\n' "$AUTH_PROXY"
    fi
} > /etc/quicktun/agent.yaml
chown quicktun-agent:quicktun-agent /etc/quicktun/agent.yaml
chmod 0600 /etc/quicktun/agent.yaml

systemctl enable --now quicktun-agent.service
log "Done. Check: systemctl status quicktun-agent && journalctl -u quicktun-agent -f"
