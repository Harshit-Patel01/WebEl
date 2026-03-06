package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Database    DatabaseConfig    `yaml:"database"`
	Deploy      DeployConfig      `yaml:"deploy"`
	Nginx       NginxConfig       `yaml:"nginx"`
	Cloudflared CloudflaredConfig `yaml:"cloudflared"`
	Logging     LoggingConfig     `yaml:"logging"`
	Security    SecurityConfig    `yaml:"security"`
}

type ServerConfig struct {
	Port         int           `yaml:"port"`
	Host         string        `yaml:"host"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type DeployConfig struct {
	WorkspaceRoot       string        `yaml:"workspace_root"`
	MaxConcurrentBuilds int           `yaml:"max_concurrent_builds"`
	BuildTimeout        time.Duration `yaml:"build_timeout"`
	GitBinary           string        `yaml:"git_binary"`
	NodeBinary          string        `yaml:"node_binary"`
	NpmBinary           string        `yaml:"npm_binary"`
	PythonBinary        string        `yaml:"python_binary"`
	GoBinary            string        `yaml:"go_binary"`
	DockerBinary        string        `yaml:"docker_binary"`
	DockerEnabled       bool          `yaml:"docker_enabled"`
	DockerMemoryLimit   string        `yaml:"docker_memory_limit"`
	DockerCPULimit      string        `yaml:"docker_cpu_limit"`
	OutputRoot          string        `yaml:"output_root"`
	PortPoolStart       int           `yaml:"port_pool_start"`
	PortPoolEnd         int           `yaml:"port_pool_end"`
}

type NginxConfig struct {
	SitesAvailable string `yaml:"sites_available"`
	SitesEnabled   string `yaml:"sites_enabled"`
	LogPath        string `yaml:"log_path"`
}

type CloudflaredConfig struct {
	Binary         string `yaml:"binary"`
	ConfigPath     string `yaml:"config_path"`
	CredentialsDir string `yaml:"credentials_dir"`
}

type LoggingConfig struct {
	Level        string `yaml:"level"`
	LogDir       string `yaml:"log_dir"`
	MaxLogSizeMB int    `yaml:"max_log_size_mb"`
	MaxLogFiles  int    `yaml:"max_log_files"`
}

type SecurityConfig struct {
	SessionDuration time.Duration `yaml:"session_duration"`
	BcryptCost      int           `yaml:"bcrypt_cost"`
	LANOnly         bool          `yaml:"lan_only"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 3000
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 30 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 30 * time.Second
	}
	if c.Database.Path == "" {
		c.Database.Path = "/var/lib/opendeploy/state.db"
	}
	if c.Deploy.WorkspaceRoot == "" {
		c.Deploy.WorkspaceRoot = "/var/lib/opendeploy/projects"
	}
	if c.Deploy.MaxConcurrentBuilds == 0 {
		c.Deploy.MaxConcurrentBuilds = 1
	}
	if c.Deploy.BuildTimeout == 0 {
		c.Deploy.BuildTimeout = 30 * time.Minute
	}
	if c.Deploy.GitBinary == "" {
		c.Deploy.GitBinary = "/usr/bin/git"
	}
	if c.Deploy.NodeBinary == "" {
		c.Deploy.NodeBinary = "/usr/bin/node"
	}
	if c.Deploy.NpmBinary == "" {
		c.Deploy.NpmBinary = "/usr/bin/npm"
	}
	if c.Deploy.PythonBinary == "" {
		c.Deploy.PythonBinary = "/usr/bin/python3"
	}
	if c.Deploy.GoBinary == "" {
		c.Deploy.GoBinary = "/usr/local/go/bin/go"
	}
	if c.Deploy.DockerBinary == "" {
		c.Deploy.DockerBinary = "/usr/bin/docker"
	}
	if c.Deploy.DockerMemoryLimit == "" {
		c.Deploy.DockerMemoryLimit = "512m"
	}
	if c.Deploy.DockerCPULimit == "" {
		c.Deploy.DockerCPULimit = "1.0"
	}
	if c.Deploy.OutputRoot == "" {
		c.Deploy.OutputRoot = "/var/www/opendeploy"
	}
	if c.Deploy.PortPoolStart == 0 {
		c.Deploy.PortPoolStart = 8000
	}
	if c.Deploy.PortPoolEnd == 0 {
		c.Deploy.PortPoolEnd = 9000
	}
	if c.Nginx.SitesAvailable == "" {
		c.Nginx.SitesAvailable = "/etc/nginx/sites-available"
	}
	if c.Nginx.SitesEnabled == "" {
		c.Nginx.SitesEnabled = "/etc/nginx/sites-enabled"
	}
	if c.Nginx.LogPath == "" {
		c.Nginx.LogPath = "/var/log/nginx"
	}
	if c.Cloudflared.Binary == "" {
		c.Cloudflared.Binary = "/usr/local/bin/cloudflared"
	}
	if c.Cloudflared.ConfigPath == "" {
		c.Cloudflared.ConfigPath = "/etc/cloudflared/config.yml"
	}
	if c.Cloudflared.CredentialsDir == "" {
		c.Cloudflared.CredentialsDir = "/root/.cloudflared"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.LogDir == "" {
		c.Logging.LogDir = "/var/log/opendeploy"
	}
	if c.Logging.MaxLogSizeMB == 0 {
		c.Logging.MaxLogSizeMB = 50
	}
	if c.Logging.MaxLogFiles == 0 {
		c.Logging.MaxLogFiles = 5
	}
	if c.Security.SessionDuration == 0 {
		c.Security.SessionDuration = 24 * time.Hour
	}
	if c.Security.BcryptCost == 0 {
		c.Security.BcryptCost = 12
	}
}
