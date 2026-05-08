-- 0003_service_account_tokens.up.sql
-- Long-lived bearer tokens scoped to a specific operator, for CI/CD and
-- automation. Distinct from operator_sessions (which are login-derived and
-- expire on a TTL); SA tokens may have NULL expires_at and are issued/revoked
-- explicitly via OperatorService.

CREATE TABLE service_account_tokens (
    id            INTEGER  PRIMARY KEY AUTOINCREMENT,
    operator_id   INTEGER  NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
    token_hash    TEXT     NOT NULL UNIQUE,        -- sha256 hex
    description   TEXT     NOT NULL DEFAULT '',
    expires_at    DATETIME,                        -- NULL = no expiry
    last_used_at  DATETIME,
    revoked_at    DATETIME,
    created_at    DATETIME NOT NULL,
    deleted_at    DATETIME                          -- gorm soft delete
);
CREATE INDEX idx_sat_operator_id ON service_account_tokens(operator_id);
CREATE INDEX idx_sat_token_hash  ON service_account_tokens(token_hash);
CREATE INDEX idx_sat_deleted_at  ON service_account_tokens(deleted_at);
