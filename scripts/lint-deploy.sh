#!/usr/bin/env bash
# scripts/lint-deploy.sh — best-effort static checks for deploy/ artifacts.
#
# Runs:
#   - `bash -n` on every shell script (must pass)
#   - `shellcheck` (warn-only — won't fail the script)
#   - `systemd-analyze verify` on every .service file (if available)
#   - `nginx -t` on every nginx template (if available)
#
# Exits non-zero only if `bash -n` fails on a script. Everything else is
# best-effort because the lint target is most often macOS dev machines that
# don't have systemd or nginx installed.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

echo "=== bash -n ==="
shopt -s nullglob
# `deploy/*.sh` already includes lib.sh; no need to glob it twice.
for f in deploy/*.sh; do
    [[ -f "$f" ]] || continue
    bash -n "$f"
    echo "OK $f"
done

echo "=== shellcheck (warnings only) ==="
if command -v shellcheck >/dev/null 2>&1; then
    shellcheck deploy/*.sh || true
else
    echo "shellcheck not installed; skipping"
fi

echo "=== systemd-analyze verify ==="
if command -v systemd-analyze >/dev/null 2>&1; then
    for f in deploy/systemd/*.service; do
        # systemd-analyze can't fully verify a unit referencing missing
        # users / groups / binaries — accept warnings.
        systemd-analyze verify "$f" 2>&1 || true
    done
else
    echo "systemd-analyze not installed; skipping"
fi

echo "=== nginx -t (smoke parse only) ==="
if command -v nginx >/dev/null 2>&1; then
    # nginx -t needs a full config tree to fully validate. Best-effort
    # parse-only check.
    for f in deploy/nginx/*.conf; do
        nginx -T -c "$f" 2>&1 | head -1 || true
    done
else
    echo "nginx not installed; skipping"
fi

echo "=== done ==="
