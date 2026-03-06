package state

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// --- Setup State ---

func (db *DB) GetSetupState(key string) (string, error) {
	var value string
	err := db.conn.QueryRow("SELECT value FROM setup_state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (db *DB) SetSetupState(key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO setup_state (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now(),
	)
	return err
}

func (db *DB) GetAllSetupStates() (map[string]string, error) {
	rows, err := db.conn.Query("SELECT key, value FROM setup_state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		states[k] = v
	}
	return states, rows.Err()
}

// --- Projects ---

func (db *DB) CreateProject(p *Project) error {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	_, err := db.conn.Exec(
		`INSERT INTO projects (id, name, repo_url, branch, project_type, build_command, install_command, start_command, output_dir, working_directory, local_port, env_vars, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.RepoURL, p.Branch, p.ProjectType, p.BuildCommand, p.InstallCommand, p.StartCommand, p.OutputDir, p.WorkingDirectory, p.LocalPort, p.EnvVars, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (db *DB) GetProject(id string) (*Project, error) {
	p := &Project{}
	err := db.conn.QueryRow(
		"SELECT id, name, repo_url, branch, project_type, build_command, install_command, start_command, output_dir, working_directory, local_port, env_vars, created_at, updated_at FROM projects WHERE id = ?", id,
	).Scan(&p.ID, &p.Name, &p.RepoURL, &p.Branch, &p.ProjectType, &p.BuildCommand, &p.InstallCommand, &p.StartCommand, &p.OutputDir, &p.WorkingDirectory, &p.LocalPort, &p.EnvVars, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.conn.Query("SELECT id, name, repo_url, branch, project_type, build_command, install_command, start_command, output_dir, working_directory, local_port, env_vars, created_at, updated_at FROM projects ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoURL, &p.Branch, &p.ProjectType, &p.BuildCommand, &p.InstallCommand, &p.StartCommand, &p.OutputDir, &p.WorkingDirectory, &p.LocalPort, &p.EnvVars, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (db *DB) UpdateProject(p *Project) error {
	p.UpdatedAt = time.Now()
	_, err := db.conn.Exec(
		`UPDATE projects SET name=?, repo_url=?, branch=?, project_type=?, build_command=?, install_command=?, start_command=?, output_dir=?, working_directory=?, local_port=?, env_vars=?, updated_at=? WHERE id=?`,
		p.Name, p.RepoURL, p.Branch, p.ProjectType, p.BuildCommand, p.InstallCommand, p.StartCommand, p.OutputDir, p.WorkingDirectory, p.LocalPort, p.EnvVars, p.UpdatedAt, p.ID,
	)
	return err
}

func (db *DB) DeleteProject(id string) error {
	_, err := db.conn.Exec("DELETE FROM projects WHERE id = ?", id)
	return err
}

func (db *DB) GetProjectByRepoAndBranch(repoURL, branch string) (*Project, error) {
	p := &Project{}
	err := db.conn.QueryRow(
		"SELECT id, name, repo_url, branch, project_type, build_command, install_command, start_command, output_dir, working_directory, local_port, env_vars, created_at, updated_at FROM projects WHERE repo_url = ? AND branch = ?",
		repoURL, branch,
	).Scan(&p.ID, &p.Name, &p.RepoURL, &p.Branch, &p.ProjectType, &p.BuildCommand, &p.InstallCommand, &p.StartCommand, &p.OutputDir, &p.WorkingDirectory, &p.LocalPort, &p.EnvVars, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// --- Deploys ---

func (db *DB) CreateDeploy(d *Deploy) error {
	if d.ID == "" {
		d.ID = uuid.New().String()
	}
	d.StartedAt = time.Now()

	_, err := db.conn.Exec(
		`INSERT INTO deploys (id, project_id, status, commit_hash, commit_message, commit_author, started_at, exit_code, log_path, output_path, framework, is_backend, build_duration)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ProjectID, d.Status, d.CommitHash, d.CommitMessage, d.CommitAuthor, d.StartedAt, d.ExitCode, d.LogPath, d.OutputPath, d.Framework, boolToInt(d.IsBackend), d.BuildDuration,
	)
	return err
}

func (db *DB) UpdateDeploy(d *Deploy) error {
	_, err := db.conn.Exec(
		`UPDATE deploys SET status=?, commit_hash=?, commit_message=?, commit_author=?, ended_at=?, exit_code=?, log_path=?, output_path=?, framework=?, is_backend=?, build_duration=? WHERE id=?`,
		d.Status, d.CommitHash, d.CommitMessage, d.CommitAuthor, d.EndedAt, d.ExitCode, d.LogPath, d.OutputPath, d.Framework, boolToInt(d.IsBackend), d.BuildDuration, d.ID,
	)
	return err
}

func (db *DB) GetDeploy(id string) (*Deploy, error) {
	d := &Deploy{}
	var isBackend int
	err := db.conn.QueryRow(
		"SELECT id, project_id, status, commit_hash, commit_message, commit_author, started_at, ended_at, exit_code, log_path, output_path, framework, is_backend, build_duration FROM deploys WHERE id = ?", id,
	).Scan(&d.ID, &d.ProjectID, &d.Status, &d.CommitHash, &d.CommitMessage, &d.CommitAuthor, &d.StartedAt, &d.EndedAt, &d.ExitCode, &d.LogPath, &d.OutputPath, &d.Framework, &isBackend, &d.BuildDuration)
	d.IsBackend = isBackend == 1
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

func (db *DB) ListDeploysByProject(projectID string) ([]Deploy, error) {
	rows, err := db.conn.Query(
		"SELECT id, project_id, status, commit_hash, commit_message, commit_author, started_at, ended_at, exit_code, log_path, output_path, framework, is_backend, build_duration FROM deploys WHERE project_id = ? ORDER BY started_at DESC", projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deploys []Deploy
	for rows.Next() {
		var d Deploy
		var isBackend int
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Status, &d.CommitHash, &d.CommitMessage, &d.CommitAuthor, &d.StartedAt, &d.EndedAt, &d.ExitCode, &d.LogPath, &d.OutputPath, &d.Framework, &isBackend, &d.BuildDuration); err != nil {
			return nil, err
		}
		d.IsBackend = isBackend == 1
		deploys = append(deploys, d)
	}
	return deploys, rows.Err()
}

// ListStaleRunningDeploys returns deploys stuck in "running" status older than the given duration
func (db *DB) ListStaleRunningDeploys(olderThan time.Duration) ([]Deploy, error) {
	cutoff := time.Now().Add(-olderThan)
	rows, err := db.conn.Query(
		"SELECT id, project_id, status, commit_hash, commit_message, commit_author, started_at, ended_at, exit_code, log_path, output_path, framework, is_backend, build_duration FROM deploys WHERE status = 'running' AND started_at < ? ORDER BY started_at ASC",
		cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deploys []Deploy
	for rows.Next() {
		var d Deploy
		var isBackend int
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Status, &d.CommitHash, &d.CommitMessage, &d.CommitAuthor, &d.StartedAt, &d.EndedAt, &d.ExitCode, &d.LogPath, &d.OutputPath, &d.Framework, &isBackend, &d.BuildDuration); err != nil {
			return nil, err
		}
		d.IsBackend = isBackend == 1
		deploys = append(deploys, d)
	}
	return deploys, rows.Err()
}

// --- Nginx Sites ---

func (db *DB) CreateNginxSite(s *NginxSite) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	s.CreatedAt = time.Now()

	_, err := db.conn.Exec(
		`INSERT INTO nginx_sites (id, project_id, domain, config_path, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.ProjectID, s.Domain, s.ConfigPath, boolToInt(s.IsActive), s.CreatedAt,
	)
	return err
}

func (db *DB) GetNginxSite(id string) (*NginxSite, error) {
	s := &NginxSite{}
	var isActive int
	err := db.conn.QueryRow(
		"SELECT id, project_id, domain, config_path, is_active, created_at FROM nginx_sites WHERE id = ?", id,
	).Scan(&s.ID, &s.ProjectID, &s.Domain, &s.ConfigPath, &isActive, &s.CreatedAt)
	s.IsActive = isActive == 1
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (db *DB) ListNginxSites() ([]NginxSite, error) {
	rows, err := db.conn.Query("SELECT id, project_id, domain, config_path, is_active, created_at FROM nginx_sites ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []NginxSite
	for rows.Next() {
		var s NginxSite
		var isActive int
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Domain, &s.ConfigPath, &isActive, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.IsActive = isActive == 1
		sites = append(sites, s)
	}
	return sites, rows.Err()
}

func (db *DB) UpdateNginxSite(s *NginxSite) error {
	_, err := db.conn.Exec(
		`UPDATE nginx_sites SET project_id=?, domain=?, config_path=?, is_active=? WHERE id=?`,
		s.ProjectID, s.Domain, s.ConfigPath, boolToInt(s.IsActive), s.ID,
	)
	return err
}

func (db *DB) DeleteNginxSite(id string) error {
	_, err := db.conn.Exec("DELETE FROM nginx_sites WHERE id = ?", id)
	return err
}

// --- Tunnel Config ---

func (db *DB) SaveTunnelConfig(t *TunnelConfig) error {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err := db.conn.Exec(
		`INSERT INTO tunnel_config (id, tunnel_id, tunnel_name, tunnel_token, account_id, zone_id, domain, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET tunnel_id=excluded.tunnel_id, tunnel_name=excluded.tunnel_name,
		 tunnel_token=excluded.tunnel_token, account_id=excluded.account_id, zone_id=excluded.zone_id,
		 domain=excluded.domain, status=excluded.status, updated_at=excluded.updated_at`,
		t.ID, t.TunnelID, t.TunnelName, t.TunnelToken, t.AccountID, t.ZoneID, t.Domain, t.Status, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (db *DB) GetTunnelConfig() (*TunnelConfig, error) {
	t := &TunnelConfig{}
	err := db.conn.QueryRow(
		"SELECT id, tunnel_id, tunnel_name, tunnel_token, account_id, zone_id, domain, status, created_at, updated_at FROM tunnel_config ORDER BY created_at DESC LIMIT 1",
	).Scan(&t.ID, &t.TunnelID, &t.TunnelName, &t.TunnelToken, &t.AccountID, &t.ZoneID, &t.Domain, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (db *DB) UpdateTunnelConfig(t *TunnelConfig) error {
	t.UpdatedAt = time.Now()
	_, err := db.conn.Exec(
		`UPDATE tunnel_config SET tunnel_id=?, tunnel_name=?, tunnel_token=?, account_id=?, zone_id=?, domain=?, status=?, updated_at=? WHERE id=?`,
		t.TunnelID, t.TunnelName, t.TunnelToken, t.AccountID, t.ZoneID, t.Domain, t.Status, t.UpdatedAt, t.ID,
	)
	return err
}

func (db *DB) DeleteTunnelConfig(id string) error {
	_, err := db.conn.Exec("DELETE FROM tunnel_config WHERE id = ?", id)
	return err
}

// --- Jobs ---

func (db *DB) CreateJob(j *Job) error {
	if j.ID == "" {
		j.ID = uuid.New().String()
	}
	j.StartedAt = time.Now()

	_, err := db.conn.Exec(
		`INSERT INTO jobs (id, type, status, command, started_at, exit_code, log_path) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Type, j.Status, j.Command, j.StartedAt, j.ExitCode, j.LogPath,
	)
	return err
}

func (db *DB) UpdateJob(j *Job) error {
	_, err := db.conn.Exec(
		`UPDATE jobs SET status=?, ended_at=?, exit_code=?, log_path=? WHERE id=?`,
		j.Status, j.EndedAt, j.ExitCode, j.LogPath, j.ID,
	)
	return err
}

func (db *DB) GetJob(id string) (*Job, error) {
	j := &Job{}
	err := db.conn.QueryRow(
		"SELECT id, type, status, command, started_at, ended_at, exit_code, log_path FROM jobs WHERE id = ?", id,
	).Scan(&j.ID, &j.Type, &j.Status, &j.Command, &j.StartedAt, &j.EndedAt, &j.ExitCode, &j.LogPath)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (db *DB) ListJobs(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.conn.Query("SELECT id, type, status, command, started_at, ended_at, exit_code, log_path FROM jobs ORDER BY started_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &j.Command, &j.StartedAt, &j.EndedAt, &j.ExitCode, &j.LogPath); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// --- Dashboard Auth ---

func (db *DB) GetPasswordHash() (string, error) {
	var hash string
	err := db.conn.QueryRow("SELECT password_hash FROM dashboard_auth WHERE id = 1").Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

func (db *DB) SetPasswordHash(hash string) error {
	_, err := db.conn.Exec(
		`INSERT INTO dashboard_auth (id, password_hash, updated_at) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash, updated_at = excluded.updated_at`,
		hash, time.Now(),
	)
	return err
}

// helpers

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Environment Variables ---

func (db *DB) CreateEnvVariable(e *EnvVariable) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now

	_, err := db.conn.Exec(
		`INSERT INTO env_variables (id, project_id, key, value, is_secret, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(project_id, key) DO UPDATE SET value = excluded.value, is_secret = excluded.is_secret, updated_at = excluded.updated_at`,
		e.ID, e.ProjectID, e.Key, e.Value, boolToInt(e.IsSecret), e.CreatedAt, e.UpdatedAt,
	)
	return err
}

func (db *DB) ListEnvVariables(projectID string) ([]EnvVariable, error) {
	rows, err := db.conn.Query(
		"SELECT id, project_id, key, value, is_secret, created_at, updated_at FROM env_variables WHERE project_id = ? ORDER BY key ASC",
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vars []EnvVariable
	for rows.Next() {
		var v EnvVariable
		var isSecret int
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Key, &v.Value, &isSecret, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.IsSecret = isSecret == 1
		vars = append(vars, v)
	}
	return vars, rows.Err()
}

func (db *DB) GetEnvVariable(id string) (*EnvVariable, error) {
	v := &EnvVariable{}
	var isSecret int
	err := db.conn.QueryRow(
		"SELECT id, project_id, key, value, is_secret, created_at, updated_at FROM env_variables WHERE id = ?", id,
	).Scan(&v.ID, &v.ProjectID, &v.Key, &v.Value, &isSecret, &v.CreatedAt, &v.UpdatedAt)
	v.IsSecret = isSecret == 1
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (db *DB) DeleteEnvVariable(id string) error {
	_, err := db.conn.Exec("DELETE FROM env_variables WHERE id = ?", id)
	return err
}

func (db *DB) DeleteEnvVariablesByProject(projectID string) error {
	_, err := db.conn.Exec("DELETE FROM env_variables WHERE project_id = ?", projectID)
	return err
}

// GetEnvMap returns env variables as a key->value map for a project (for passing to builds/services).
func (db *DB) GetEnvMap(projectID string) (map[string]string, error) {
	vars, err := db.ListEnvVariables(projectID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Key] = v.Value
	}
	return m, nil
}

// --- Saved WiFi Networks ---

func (db *DB) SaveWifiNetwork(n *SavedWifiNetwork) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	now := time.Now()
	n.CreatedAt = now
	n.UpdatedAt = now

	_, err := db.conn.Exec(
		`INSERT INTO saved_wifi_networks (id, ssid, password, security, last_connected_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(ssid) DO UPDATE SET password = excluded.password, security = excluded.security,
		 last_connected_at = excluded.last_connected_at, updated_at = excluded.updated_at`,
		n.ID, n.SSID, n.Password, n.Security, n.LastConnectedAt, n.CreatedAt, n.UpdatedAt,
	)
	return err
}

func (db *DB) GetSavedWifiNetwork(ssid string) (*SavedWifiNetwork, error) {
	n := &SavedWifiNetwork{}
	err := db.conn.QueryRow(
		"SELECT id, ssid, password, security, last_connected_at, created_at, updated_at FROM saved_wifi_networks WHERE ssid = ?", ssid,
	).Scan(&n.ID, &n.SSID, &n.Password, &n.Security, &n.LastConnectedAt, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return n, err
}

func (db *DB) ListSavedWifiNetworks() ([]SavedWifiNetwork, error) {
	rows, err := db.conn.Query(
		"SELECT id, ssid, password, security, last_connected_at, created_at, updated_at FROM saved_wifi_networks ORDER BY last_connected_at DESC NULLS LAST",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var networks []SavedWifiNetwork
	for rows.Next() {
		var n SavedWifiNetwork
		if err := rows.Scan(&n.ID, &n.SSID, &n.Password, &n.Security, &n.LastConnectedAt, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		networks = append(networks, n)
	}
	return networks, rows.Err()
}

func (db *DB) UpdateWifiPassword(ssid, password string) error {
	_, err := db.conn.Exec(
		`UPDATE saved_wifi_networks SET password = ?, updated_at = ? WHERE ssid = ?`,
		password, time.Now(), ssid,
	)
	return err
}

func (db *DB) DeleteSavedWifiNetwork(ssid string) error {
	_, err := db.conn.Exec("DELETE FROM saved_wifi_networks WHERE ssid = ?", ssid)
	return err
}

// --- Tunnel Routes ---

func (db *DB) CreateTunnelRoute(r *TunnelRoute) error {
	if r.ID == "" {
		r.ID = uuid.New().String()
	}
	now := time.Now()
	r.CreatedAt = now
	r.UpdatedAt = now

	_, err := db.conn.Exec(
		`INSERT INTO tunnel_routes (id, tunnel_id, hostname, zone_id, dns_record_id, local_scheme, local_port, path_prefix, sort_order, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TunnelID, r.Hostname, r.ZoneID, r.DNSRecordID, r.LocalScheme, r.LocalPort, r.PathPrefix, r.SortOrder, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

func (db *DB) GetTunnelRoute(id string) (*TunnelRoute, error) {
	r := &TunnelRoute{}
	err := db.conn.QueryRow(
		"SELECT id, tunnel_id, hostname, zone_id, dns_record_id, local_scheme, local_port, path_prefix, sort_order, created_at, updated_at FROM tunnel_routes WHERE id = ?", id,
	).Scan(&r.ID, &r.TunnelID, &r.Hostname, &r.ZoneID, &r.DNSRecordID, &r.LocalScheme, &r.LocalPort, &r.PathPrefix, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

func (db *DB) ListTunnelRoutes(tunnelID string) ([]TunnelRoute, error) {
	rows, err := db.conn.Query(
		"SELECT id, tunnel_id, hostname, zone_id, dns_record_id, local_scheme, local_port, path_prefix, sort_order, created_at, updated_at FROM tunnel_routes WHERE tunnel_id = ? ORDER BY sort_order ASC",
		tunnelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []TunnelRoute
	for rows.Next() {
		var r TunnelRoute
		if err := rows.Scan(&r.ID, &r.TunnelID, &r.Hostname, &r.ZoneID, &r.DNSRecordID, &r.LocalScheme, &r.LocalPort, &r.PathPrefix, &r.SortOrder, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	return routes, rows.Err()
}

func (db *DB) UpdateTunnelRoute(r *TunnelRoute) error {
	r.UpdatedAt = time.Now()
	_, err := db.conn.Exec(
		`UPDATE tunnel_routes SET hostname=?, zone_id=?, dns_record_id=?, local_scheme=?, local_port=?, path_prefix=?, sort_order=?, updated_at=? WHERE id=?`,
		r.Hostname, r.ZoneID, r.DNSRecordID, r.LocalScheme, r.LocalPort, r.PathPrefix, r.SortOrder, r.UpdatedAt, r.ID,
	)
	return err
}

func (db *DB) DeleteTunnelRoute(id string) error {
	_, err := db.conn.Exec("DELETE FROM tunnel_routes WHERE id = ?", id)
	return err
}

func (db *DB) UpdateTunnelRouteSortOrder(id string, sortOrder int) error {
	_, err := db.conn.Exec(
		`UPDATE tunnel_routes SET sort_order=?, updated_at=? WHERE id=?`,
		sortOrder, time.Now(), id,
	)
	return err
}

// --- Containers ---

func (db *DB) CreateContainer(c *Container) error {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	now := time.Now()
	c.CreatedAt = now
	c.UpdatedAt = now

	_, err := db.conn.Exec(
		`INSERT INTO containers (id, project_id, name, image, container_id, status, port_mappings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ProjectID, c.Name, c.Image, c.ContainerID, c.Status, c.PortMappings, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (db *DB) GetContainer(id string) (*Container, error) {
	c := &Container{}
	err := db.conn.QueryRow(
		"SELECT id, project_id, name, image, container_id, status, port_mappings, created_at, updated_at FROM containers WHERE id = ?", id,
	).Scan(&c.ID, &c.ProjectID, &c.Name, &c.Image, &c.ContainerID, &c.Status, &c.PortMappings, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (db *DB) GetContainerByProjectID(projectID string) (*Container, error) {
	c := &Container{}
	err := db.conn.QueryRow(
		"SELECT id, project_id, name, image, container_id, status, port_mappings, created_at, updated_at FROM containers WHERE project_id = ? ORDER BY created_at DESC LIMIT 1", projectID,
	).Scan(&c.ID, &c.ProjectID, &c.Name, &c.Image, &c.ContainerID, &c.Status, &c.PortMappings, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (db *DB) ListContainersByProject(projectID string) ([]Container, error) {
	query := "SELECT id, project_id, name, image, container_id, status, port_mappings, created_at, updated_at FROM containers"
	args := []interface{}{}

	if projectID != "" {
		query += " WHERE project_id = ?"
		args = append(args, projectID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []Container
	for rows.Next() {
		var c Container
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Image, &c.ContainerID, &c.Status, &c.PortMappings, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

func (db *DB) UpdateContainer(c *Container) error {
	c.UpdatedAt = time.Now()
	_, err := db.conn.Exec(
		`UPDATE containers SET name=?, image=?, container_id=?, status=?, port_mappings=?, updated_at=? WHERE id=?`,
		c.Name, c.Image, c.ContainerID, c.Status, c.PortMappings, c.UpdatedAt, c.ID,
	)
	return err
}

func (db *DB) DeleteContainer(id string) error {
	_, err := db.conn.Exec("DELETE FROM containers WHERE id = ?", id)
	return err
}

func (db *DB) DeleteContainersByProject(projectID string) error {
	_, err := db.conn.Exec("DELETE FROM containers WHERE project_id = ?", projectID)
	return err
}

// --- Deploy Logs ---

func (db *DB) CreateDeployLog(l *DeployLog) error {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	if l.LogTimestamp.IsZero() {
		l.LogTimestamp = time.Now()
	}

	_, err := db.conn.Exec(
		`INSERT INTO deploy_logs (id, deploy_id, log_timestamp, stream, message) VALUES (?, ?, ?, ?, ?)`,
		l.ID, l.DeployID, l.LogTimestamp, l.Stream, l.Message,
	)
	return err
}

func (db *DB) ListDeployLogs(deployID string, limit int, offset int) ([]DeployLog, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := db.conn.Query(
		"SELECT id, deploy_id, log_timestamp, stream, message FROM deploy_logs WHERE deploy_id = ? ORDER BY log_timestamp ASC LIMIT ? OFFSET ?",
		deployID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []DeployLog
	for rows.Next() {
		var l DeployLog
		if err := rows.Scan(&l.ID, &l.DeployID, &l.LogTimestamp, &l.Stream, &l.Message); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (db *DB) GetDeployLogsAfter(deployID string, after time.Time) ([]DeployLog, error) {
	rows, err := db.conn.Query(
		"SELECT id, deploy_id, log_timestamp, stream, message FROM deploy_logs WHERE deploy_id = ? AND log_timestamp > ? ORDER BY log_timestamp ASC",
		deployID, after,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []DeployLog
	for rows.Next() {
		var l DeployLog
		if err := rows.Scan(&l.ID, &l.DeployID, &l.LogTimestamp, &l.Stream, &l.Message); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func (db *DB) DeleteDeployLogs(deployID string) error {
	_, err := db.conn.Exec("DELETE FROM deploy_logs WHERE deploy_id = ?", deployID)
	return err
}

