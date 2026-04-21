package models

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// GatewayConfig is the top-level gateway configuration file structure.
type GatewayConfig struct {
	SSOStartURL string         `yaml:"sso_start_url,omitempty"`
	SSORegion   string         `yaml:"sso_region,omitempty"`
	Gateways    []GatewayEntry `yaml:"gateways"`
}

// GatewayEntry describes a single remote database gateway.
type GatewayEntry struct {
	Name          string            `yaml:"name"`
	AWSProfile    string            `yaml:"aws_profile"`
	AWSRegion     string            `yaml:"aws_region"`
	InstanceID    string            `yaml:"instance_id,omitempty"`
	InstanceTags  map[string]string `yaml:"instance_tags,omitempty"`
	RDSHost       string            `yaml:"rds_host"`
	RDSPort       int               `yaml:"rds_port"`
	LocalPort     int               `yaml:"local_port"`
	DBName        string            `yaml:"db_name"`
	DBUser        string            `yaml:"db_user"`
	DBPassword    string            `yaml:"db_password,omitempty"`
	DBPasswordEnv string            `yaml:"db_password_env,omitempty"`
	AuthMode      string            `yaml:"auth_mode,omitempty"` // "password" (default) or "iam"
}

// UseIAMAuth returns true if this entry uses RDS IAM authentication.
func (g *GatewayEntry) UseIAMAuth() bool {
	return g.AuthMode == "iam"
}

// ResolvePassword returns the database password by checking:
//  1. db_password field directly
//  2. db_password_env environment variable
//  3. Empty string if neither is set
func (g *GatewayEntry) ResolvePassword() string {
	if g.DBPassword != "" {
		return g.DBPassword
	}
	if g.DBPasswordEnv != "" {
		return os.Getenv(g.DBPasswordEnv)
	}
	return ""
}

// LoadGatewayConfig reads the gateway config from the platform config directory.
// Returns nil (no error) if the config file doesn't exist.
func LoadGatewayConfig() (*GatewayConfig, error) {
	path := gatewayConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read gateway config: %w", err)
	}

	var cfg GatewayConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse gateway config: %w", err)
	}

	// Apply defaults
	for i := range cfg.Gateways {
		if cfg.Gateways[i].RDSPort == 0 {
			cfg.Gateways[i].RDSPort = 5432
		}
		if cfg.Gateways[i].LocalPort == 0 {
			cfg.Gateways[i].LocalPort = 5432
		}
	}

	return &cfg, nil
}

// SaveGatewayConfig writes the gateway config back to disk.
func SaveGatewayConfig(cfg *GatewayConfig) error {
	path := gatewayConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal gateway config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// GatewayConfigPath returns the full path to the gateway config file.
func GatewayConfigPath() string {
	return gatewayConfigPath()
}

func gatewayConfigPath() string {
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "Library", "Application Support", "Bufflehead")
	case "windows":
		base = filepath.Join(os.Getenv("APPDATA"), "Bufflehead")
	default:
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config", "bufflehead")
	}
	return filepath.Join(base, "gateway.yaml")
}
