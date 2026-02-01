-- Migration 001: Initial schema
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY
);

INSERT OR IGNORE INTO schema_version (version) VALUES (1);

CREATE TABLE IF NOT EXISTS setup_state (
    key TEXT PRIMARY KEY,
    value TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    branch TEXT NOT NULL DEFAULT 'main',
    project_type TEXT NOT NULL DEFAULT 'node',
    build_command TEXT,
    output_dir TEXT,
    local_port INTEGER,
    env_vars TEXT DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deploys (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    commit_hash TEXT,
    commit_message TEXT,
    commit_author TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    exit_code INTEGER DEFAULT -1,
    log_path TEXT,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS nginx_sites (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    domain TEXT NOT NULL,
    config_path TEXT,
    is_active INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS tunnel_config (
    id TEXT PRIMARY KEY,
    tunnel_id TEXT,
    tunnel_name TEXT,
    domain TEXT,
    api_token_hash TEXT,
    status TEXT DEFAULT 'inactive',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    command TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    exit_code INTEGER DEFAULT -1,
    log_path TEXT
);

CREATE TABLE IF NOT EXISTS dashboard_auth (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    password_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
