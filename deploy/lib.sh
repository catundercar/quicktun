# deploy/lib.sh — shared bash helpers for install scripts.
# shellcheck shell=bash

# Callers source this file; they own `set -euo pipefail`.

log()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m==>\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m==>\033[0m %s\n' "$*" >&2; exit 1; }

require_root() {
    [[ $EUID -eq 0 ]] || fail "this script must run as root (use sudo)"
}

require_linux() {
    [[ "$(uname -s)" == "Linux" ]] || fail "this script only runs on Linux"
}

# ensure_user <name> <home>
# Creates a system user with /usr/sbin/nologin shell if missing. Idempotent.
ensure_user() {
    local user=$1 home=$2
    id -u "$user" >/dev/null 2>&1 && return 0
    log "creating system user $user"
    useradd --system --no-create-home --home "$home" --shell /usr/sbin/nologin "$user"
}

# ensure_dir <dir> <owner> <mode>
# mkdir -p, chown owner:owner, chmod mode. Idempotent.
ensure_dir() {
    local dir=$1 owner=$2 mode=$3
    mkdir -p "$dir"
    chown "$owner:$owner" "$dir"
    chmod "$mode" "$dir"
}

# ensure_file_unchanged_or_prompt <existing> <incoming>
# If <existing> exists, leave it alone (warn if it differs). Otherwise copy
# <incoming> into place. Use this for example configs that operators edit.
ensure_file_unchanged_or_prompt() {
    local existing=$1 incoming=$2
    if [[ -f "$existing" ]]; then
        if ! cmp -s "$existing" "$incoming"; then
            warn "$existing differs from packaged version; not overwriting"
        fi
        return 0
    fi
    cp "$incoming" "$existing"
}
