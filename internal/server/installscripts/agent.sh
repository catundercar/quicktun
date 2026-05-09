#!/usr/bin/env bash
# quicktun-agent quick installer (curl-pipe-bash safe).
#
# Reads:
#   QT_TOKEN        - site agent raw token             (required)
#   QT_ENDPOINT     - control-plane gRPC host:port     (required)
#   QT_AGENT_URL    - (optional) where to fetch the quicktun-agent binary;
#                     defaults to a GitHub release URL derived from uname.
#   QT_TLS_INSECURE - "true" to skip TLS verification on the gRPC dial
#                     (default: false). Use only for dev / self-signed certs.
#
# Note: this installer does NOT auto-install the rathole binary. After the
# script finishes you must place a rathole executable at /usr/local/bin/rathole
# (download from https://github.com/rapiz1/rathole/releases) — the agent
# supervises it as a child process.
set -euo pipefail

[[ -n "${QT_TOKEN:-}" ]]    || { echo "QT_TOKEN required" >&2; exit 1; }
[[ -n "${QT_ENDPOINT:-}" ]] || { echo "QT_ENDPOINT required" >&2; exit 1; }

OS=$(uname -s)
case "$OS" in
    Linux|Darwin)
        ETC_DIR=/etc/quicktun
        DATA_DIR=/var/lib/quicktun-agent
        LOG_DIR=/var/log/quicktun-agent
        BIN_DIR=/usr/local/bin
        [[ $EUID -eq 0 ]] || { echo "run as root (sudo)" >&2; exit 1; }
        ;;
    *) echo "unsupported OS: $OS" >&2; exit 1 ;;
esac

mkdir -p "$ETC_DIR" "$DATA_DIR" "$LOG_DIR"

# Download agent binary if not already present.
if [[ ! -x "$BIN_DIR/quicktun-agent" ]]; then
    AGENT_URL="${QT_AGENT_URL:-https://github.com/catundercar/quicktun/releases/latest/download/quicktun-agent-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)}"
    echo "==> fetching quicktun-agent from $AGENT_URL"
    if ! curl -fsSL "$AGENT_URL" -o "$BIN_DIR/quicktun-agent.new"; then
        echo "failed to download. Place quicktun-agent at $BIN_DIR/quicktun-agent manually then rerun." >&2
        exit 1
    fi
    chmod +x "$BIN_DIR/quicktun-agent.new"
    mv "$BIN_DIR/quicktun-agent.new" "$BIN_DIR/quicktun-agent"
fi

# Write config.
cat > "$ETC_DIR/agent.yaml" <<EOF
control_endpoint: $QT_ENDPOINT
token: $QT_TOKEN
state_dir: $DATA_DIR
rathole_binary: $BIN_DIR/rathole
rathole_args:
  - --client
tls_insecure: ${QT_TLS_INSECURE:-false}
EOF
chmod 0600 "$ETC_DIR/agent.yaml"

# Linux: systemd unit
if [[ "$OS" == "Linux" ]]; then
    cat > /etc/systemd/system/quicktun-agent.service <<'EOF'
[Unit]
Description=quicktun site agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/quicktun-agent run --config /etc/quicktun/agent.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable --now quicktun-agent.service
    echo "==> systemctl status quicktun-agent"
    systemctl --no-pager status quicktun-agent || true
fi

# Darwin: launchd
if [[ "$OS" == "Darwin" ]]; then
    cat > /Library/LaunchDaemons/com.tulip.quicktun-agent.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>com.tulip.quicktun-agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>$BIN_DIR/quicktun-agent</string>
        <string>run</string>
        <string>--config</string>
        <string>$ETC_DIR/agent.yaml</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
    <key>StandardOutPath</key><string>$LOG_DIR/agent.log</string>
    <key>StandardErrorPath</key><string>$LOG_DIR/agent.log</string>
    <key>ThrottleInterval</key><integer>10</integer>
</dict>
</plist>
EOF
    launchctl unload /Library/LaunchDaemons/com.tulip.quicktun-agent.plist 2>/dev/null || true
    launchctl load -w /Library/LaunchDaemons/com.tulip.quicktun-agent.plist
fi

echo "==> done. Logs:"
echo "    Linux:  journalctl -u quicktun-agent -f"
echo "    macOS:  tail -f $LOG_DIR/agent.log"
echo
echo "Reminder: place a rathole binary at $BIN_DIR/rathole if you haven't already."
echo "  https://github.com/rapiz1/rathole/releases"
