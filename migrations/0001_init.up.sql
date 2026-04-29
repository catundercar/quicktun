-- 0001_init.up.sql
-- Phase 1 schema for quicktun control plane.

CREATE TABLE operators (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    email           TEXT    NOT NULL,
    password_hash   TEXT    NOT NULL,
    is_admin        INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    deleted_at      DATETIME
);
CREATE UNIQUE INDEX uk_operators_email_active ON operators(email) WHERE deleted_at IS NULL;
CREATE INDEX        idx_operators_deleted      ON operators(deleted_at);

CREATE TABLE operator_sessions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    operator_id     INTEGER NOT NULL REFERENCES operators(id),
    token_hash      TEXT    NOT NULL,
    issued_at       DATETIME NOT NULL,
    expires_at      DATETIME NOT NULL,
    revoked_at      DATETIME,
    user_agent      TEXT    NOT NULL DEFAULT '',
    source_ip       TEXT    NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    deleted_at      DATETIME
);
CREATE UNIQUE INDEX uk_operator_sessions_token_active ON operator_sessions(token_hash) WHERE deleted_at IS NULL;
CREATE INDEX        idx_op_sessions_op      ON operator_sessions(operator_id);
CREATE INDEX        idx_op_sessions_expires ON operator_sessions(expires_at);
CREATE INDEX        idx_op_sessions_revoked ON operator_sessions(revoked_at);

CREATE TABLE projects (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    slug              TEXT    NOT NULL,
    name              TEXT    NOT NULL,
    default_mode      TEXT    NOT NULL DEFAULT 'endpoint',
    backend           TEXT    NOT NULL DEFAULT 'rathole',
    relay_port_range  TEXT    NOT NULL,
    status            TEXT    NOT NULL DEFAULT 'active',
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL,
    deleted_at        DATETIME
);
CREATE UNIQUE INDEX uk_projects_slug_active ON projects(slug) WHERE deleted_at IS NULL;

CREATE TABLE operator_project_access (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    operator_id  INTEGER NOT NULL REFERENCES operators(id),
    project_id   INTEGER NOT NULL REFERENCES projects(id),
    role         TEXT    NOT NULL DEFAULT 'operator',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);
CREATE UNIQUE INDEX uk_operator_project_active ON operator_project_access(operator_id, project_id) WHERE deleted_at IS NULL;

CREATE TABLE sites (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id       INTEGER NOT NULL REFERENCES projects(id),
    name             TEXT    NOT NULL,
    lan_cidrs_json   TEXT    NOT NULL DEFAULT '',
    mode             TEXT    NOT NULL DEFAULT 'endpoint',
    backend          TEXT    NOT NULL DEFAULT '',
    status           TEXT    NOT NULL DEFAULT 'pending',
    last_seen_at     DATETIME,
    hostname         TEXT    NOT NULL DEFAULT '',
    os               TEXT    NOT NULL DEFAULT '',
    agent_version    TEXT    NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    deleted_at       DATETIME
);
CREATE INDEX        idx_sites_project_id     ON sites(project_id);
CREATE UNIQUE INDEX uk_project_site_name_active ON sites(project_id, name) WHERE deleted_at IS NULL;
CREATE INDEX        idx_sites_last_seen      ON sites(last_seen_at);

CREATE TABLE services (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id      INTEGER NOT NULL REFERENCES sites(id),
    name         TEXT    NOT NULL,
    target_addr  TEXT    NOT NULL,
    target_port  INTEGER NOT NULL,
    proto        TEXT    NOT NULL DEFAULT 'tcp',
    relay_port   INTEGER,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);
CREATE INDEX        idx_services_site_id        ON services(site_id);
CREATE UNIQUE INDEX uk_site_svc_name_active     ON services(site_id, name) WHERE deleted_at IS NULL;
CREATE INDEX        idx_services_relay          ON services(relay_port);

CREATE TABLE site_agent_tokens (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id       INTEGER NOT NULL REFERENCES sites(id),
    token_hash    TEXT    NOT NULL,
    expires_at    DATETIME,
    last_used_at  DATETIME,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    deleted_at    DATETIME
);
CREATE UNIQUE INDEX uk_site_agent_site_active  ON site_agent_tokens(site_id)    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uk_site_agent_token_active ON site_agent_tokens(token_hash) WHERE deleted_at IS NULL;
CREATE INDEX        idx_site_agent_exp         ON site_agent_tokens(expires_at);
CREATE INDEX        idx_site_agent_used        ON site_agent_tokens(last_used_at);

-- audit_logs: append-only, no soft delete, no updated_at
CREATE TABLE audit_logs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    ts            DATETIME NOT NULL,
    project_id    INTEGER,
    operator_id   INTEGER,
    action        TEXT    NOT NULL,
    target        TEXT    NOT NULL DEFAULT '',
    source_ip     TEXT    NOT NULL DEFAULT '',
    extra_json    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_ts        ON audit_logs(ts);
CREATE INDEX idx_audit_project   ON audit_logs(project_id);
CREATE INDEX idx_audit_operator  ON audit_logs(operator_id);
CREATE INDEX idx_audit_action    ON audit_logs(action);
