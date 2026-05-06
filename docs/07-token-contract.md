# Site Agent Token Contract

## Storage
- The control plane generates a 32-byte random token and stores `sha256_hex(token)` in `site_agent_tokens.token_hash`.
- The raw token is returned exactly once via `RotateSiteAgentToken` / `GetSiteInstallCommand` and discarded.

## Two consumers, two presentations

The agent uses ONE token, presented two ways:

1. **Control plane API (Plan 7+)**: agent sends `Authorization: Bearer <raw_token>` for heartbeat, config sync, etc. The server hashes on receipt and looks up `token_hash`.

2. **rathole-server**: agent's rathole-client.toml has `token = "<sha256_hex(raw)>"`. The server's rathole-server.toml is rendered (in `internal/relay/render.go`) with the same hex hash, so rathole sees matching shared secrets without ever holding the raw.

## Why this design

- No schema change. The DB only ever holds the hash.
- The "install command" output is directly usable by an operator (no transformation required on their end).
- Future split (e.g., separate `rathole_token` from `agent_token`) is a render-side change; no wire-format break.

## Implementation notes for Plan 7 agent author

- On first boot, the agent reads the raw token from a config file or env var (operator pastes it).
- Compute `rathole_token = sha256_hex(raw_token)` once at startup; pass to rathole-client.toml.
- Use the raw for Bearer auth against the control plane.
