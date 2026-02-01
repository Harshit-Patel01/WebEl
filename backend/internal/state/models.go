package state

import "time"

type SetupState struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	RepoURL      string    `json:"repo_url"`
	Branch       string    `json:"branch"`
	ProjectType  string    `json:"project_type"`
	BuildCommand string    `json:"build_command"`
	OutputDir    string    `json:"output_dir"`
	LocalPort    int       `json:"local_port,omitempty"`
	EnvVars      string    `json:"env_vars"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Deploy struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Status        string     `json:"status"`
	CommitHash    string     `json:"commit_hash,omitempty"`
	CommitMessage string     `json:"commit_message,omitempty"`
	CommitAuthor  string     `json:"commit_author,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	ExitCode      int        `json:"exit_code"`
	LogPath       string     `json:"log_path,omitempty"`
}

type NginxSite struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Domain     string    `json:"domain"`
	ConfigPath string    `json:"config_path"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
}

type TunnelConfig struct {
	ID          string    `json:"id"`
	TunnelID    string    `json:"tunnel_id"`
	TunnelName  string    `json:"tunnel_name"`
	TunnelToken string    `json:"tunnel_token"` // Encrypted
	AccountID   string    `json:"account_id"`
	ZoneID      string    `json:"zone_id"`
	Domain      string    `json:"domain"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Job struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Status    string     `json:"status"`
	Command   string     `json:"command"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	ExitCode  int        `json:"exit_code"`
	LogPath   string     `json:"log_path,omitempty"`
}

type DashboardAuth struct {
	PasswordHash string `json:"password_hash"`
}

type EnvVariable struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	IsSecret  bool      `json:"is_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SavedWifiNetwork struct {
	ID              string     `json:"id"`
	SSID            string     `json:"ssid"`
	Password        string     `json:"password,omitempty"`
	Security        string     `json:"security"`
	LastConnectedAt *time.Time `json:"last_connected_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type TunnelRoute struct {
	ID           string    `json:"id"`
	TunnelID     string    `json:"tunnel_id"`
	Hostname     string    `json:"hostname"`
	ZoneID       string    `json:"zone_id"`
	DNSRecordID  string    `json:"dns_record_id,omitempty"`
	LocalScheme  string    `json:"local_scheme"`
	LocalPort    int       `json:"local_port"`
	PathPrefix   string    `json:"path_prefix,omitempty"`
	SortOrder    int       `json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
