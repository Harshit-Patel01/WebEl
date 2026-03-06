package state

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	conn.SetMaxOpenConns(1) // SQLite handles one writer at a time

	db := &DB{conn: conn}
	if err := db.runMigrations(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) runMigrations() error {
	// Create schema_version table if not exists
	_, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	// Get current version
	var currentVersion int
	row := db.conn.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	// Run migrations sequentially
	migrations := []string{migration001, migration002, migration003, migration004, migration005, migration006, migration007, migration008, migration009, migration010, migration011, migration012}

	for i, m := range migrations {
		version := i + 1
		if version <= currentVersion {
			continue
		}
		if _, err := db.conn.Exec(m); err != nil {
			return fmt.Errorf("executing migration %d: %w", version, err)
		}
	}

	return nil
}

const migration001 = `
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

INSERT OR REPLACE INTO schema_version (version) VALUES (1);
`

const migration002 = `
CREATE TABLE IF NOT EXISTS env_variables (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	key TEXT NOT NULL,
	value TEXT NOT NULL,
	is_secret INTEGER DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_env_project_key ON env_variables(project_id, key);

INSERT OR REPLACE INTO schema_version (version) VALUES (2);
`

const migration003 = `
CREATE TABLE IF NOT EXISTS saved_wifi_networks (
	id TEXT PRIMARY KEY,
	ssid TEXT NOT NULL UNIQUE,
	password TEXT,
	security TEXT,
	last_connected_at DATETIME,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR REPLACE INTO schema_version (version) VALUES (3);
`

const migration004 = `
-- Update tunnel_config table with new fields
ALTER TABLE tunnel_config ADD COLUMN tunnel_token TEXT;
ALTER TABLE tunnel_config ADD COLUMN account_id TEXT;
ALTER TABLE tunnel_config ADD COLUMN zone_id TEXT;
ALTER TABLE tunnel_config ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP;

INSERT OR REPLACE INTO schema_version (version) VALUES (4);
`

const migration005 = `
-- Create tunnel_routes table for ingress route management
CREATE TABLE IF NOT EXISTS tunnel_routes (
	id TEXT PRIMARY KEY,
	tunnel_id TEXT NOT NULL,
	hostname TEXT NOT NULL,
	zone_id TEXT NOT NULL,
	dns_record_id TEXT,
	local_scheme TEXT NOT NULL DEFAULT 'http',
	local_port INTEGER NOT NULL,
	path_prefix TEXT,
	sort_order INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (tunnel_id) REFERENCES tunnel_config(tunnel_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tunnel_routes_tunnel_id ON tunnel_routes(tunnel_id);
CREATE INDEX IF NOT EXISTS idx_tunnel_routes_sort_order ON tunnel_routes(tunnel_id, sort_order);

INSERT OR REPLACE INTO schema_version (version) VALUES (5);
`

const migration006 = `
-- Add encrypted API token storage to tunnel_config
ALTER TABLE tunnel_config ADD COLUMN api_token_encrypted TEXT;

INSERT OR REPLACE INTO schema_version (version) VALUES (6);
`

const migration007 = `
-- Remove api_token_encrypted column - API key now passed as header, never stored
-- SQLite doesn't support DROP COLUMN directly, so we need to recreate the table

-- Create new table without api_token_encrypted
CREATE TABLE tunnel_config_new (
    id TEXT PRIMARY KEY,
    tunnel_id TEXT NOT NULL UNIQUE,
    tunnel_name TEXT NOT NULL,
    account_id TEXT NOT NULL,
    zone_id TEXT NOT NULL,
    domain TEXT NOT NULL,
    tunnel_token TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);

-- Copy data from old table
INSERT INTO tunnel_config_new (id, tunnel_id, tunnel_name, account_id, zone_id, domain, tunnel_token, created_at, updated_at)
SELECT id, tunnel_id, tunnel_name, account_id, zone_id, domain, tunnel_token, created_at, updated_at
FROM tunnel_config;

-- Drop old table
DROP TABLE tunnel_config;

-- Rename new table
ALTER TABLE tunnel_config_new RENAME TO tunnel_config;

INSERT OR REPLACE INTO schema_version (version) VALUES (7);
`

const migration008 = `
-- Add status column back to tunnel_config table
ALTER TABLE tunnel_config ADD COLUMN status TEXT DEFAULT 'inactive';

INSERT OR REPLACE INTO schema_version (version) VALUES (8);
`

const migration009 = `
-- Add working_directory to projects table
ALTER TABLE projects ADD COLUMN working_directory TEXT DEFAULT '';

-- Create containers table for Docker container management
CREATE TABLE IF NOT EXISTS containers (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	name TEXT NOT NULL,
	image TEXT NOT NULL,
	container_id TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'stopped',
	port_mappings TEXT DEFAULT '{}',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_containers_project_id ON containers(project_id);

INSERT OR REPLACE INTO schema_version (version) VALUES (9);
`

const migration010 = `
-- Create deploy_logs table for storing deployment logs
CREATE TABLE IF NOT EXISTS deploy_logs (
	id TEXT PRIMARY KEY,
	deploy_id TEXT NOT NULL,
	log_timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	stream TEXT NOT NULL,
	message TEXT NOT NULL,
	FOREIGN KEY (deploy_id) REFERENCES deploys(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deploy_logs_deploy_id ON deploy_logs(deploy_id);
CREATE INDEX IF NOT EXISTS idx_deploy_logs_timestamp ON deploy_logs(deploy_id, log_timestamp);

INSERT OR REPLACE INTO schema_version (version) VALUES (10);
`

const migration011 = `
-- Add install_command and start_command columns to projects table
ALTER TABLE projects ADD COLUMN install_command TEXT;
ALTER TABLE projects ADD COLUMN start_command TEXT;

INSERT OR REPLACE INTO schema_version (version) VALUES (11);
`

const migration012 = `
-- Add output_path, framework, is_backend, build_duration columns to deploys table
ALTER TABLE deploys ADD COLUMN output_path TEXT DEFAULT '';
ALTER TABLE deploys ADD COLUMN framework TEXT DEFAULT '';
ALTER TABLE deploys ADD COLUMN is_backend INTEGER DEFAULT 0;
ALTER TABLE deploys ADD COLUMN build_duration REAL DEFAULT 0;

INSERT OR REPLACE INTO schema_version (version) VALUES (12);
`
