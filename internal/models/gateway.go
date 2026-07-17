package models

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConnKind identifies the transport/topology of a database connection.
type ConnKind string

const (
	// KindAWSGateway reaches a Postgres/RDS database through an AWS SSO login
	// and an SSM tunnel from a bastion instance. This is the historical
	// default; an empty Kind is treated as KindAWSGateway for backward
	// compatibility with existing saved configs.
	KindAWSGateway ConnKind = ""
	// KindPostgres connects directly to a reachable Postgres host (local, over
	// a VPN, or a public endpoint). No AWS credentials or tunnel are used.
	KindPostgres ConnKind = "postgres"
	// KindSSHPostgres reaches a Postgres host through an SSH tunnel. Reserved
	// for a future release.
	KindSSHPostgres ConnKind = "ssh_postgres"
)

// GatewayConfig is the top-level gateway configuration file structure.
type GatewayConfig struct {
	SSOStartURL string         `yaml:"sso_start_url,omitempty"`
	SSORegion   string         `yaml:"sso_region,omitempty"`
	Gateways    []GatewayEntry `yaml:"gateways"`
}

// GatewayEntry describes a single database connection. Despite the name it now
// covers both AWS-gateway connections and direct Postgres connections; the Kind
// field discriminates between them (empty == AWS gateway).
type GatewayEntry struct {
	Name          string            `yaml:"name"`
	Kind          ConnKind          `yaml:"kind,omitempty"`
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
	SSLMode       string            `yaml:"ssl_mode,omitempty"`  // direct Postgres: prefer|require|disable
	SecretKind    SecretKind        `yaml:"secret_kind,omitempty"`
}

// IsDirect reports whether this entry is a direct Postgres connection (no AWS
// SSM tunnel).
func (g *GatewayEntry) IsDirect() bool {
	return g.Kind == KindPostgres
}

// UseIAMAuth returns true if this entry uses RDS IAM authentication.
func (g *GatewayEntry) UseIAMAuth() bool {
	return g.AuthMode == "iam"
}

// ResolvePassword returns the database password by checking, in order:
//  1. db_password field directly (in-memory, never persisted for direct conns)
//  2. the OS keychain, when SecretKind is keychain
//  3. the db_password_env environment variable
//  4. Empty string if none apply
func (g *GatewayEntry) ResolvePassword() string {
	if g.DBPassword != "" {
		return g.DBPassword
	}
	if g.SecretKind == SecretKeychain {
		if s, err := GetSecret(g.Name); err == nil && s != "" {
			return s
		}
	}
	if g.DBPasswordEnv != "" {
		return os.Getenv(g.DBPasswordEnv)
	}
	return ""
}

// EffectiveSSLMode returns the SSL mode for a direct Postgres connection,
// defaulting to "prefer" when unset.
func (g *GatewayEntry) EffectiveSSLMode() string {
	if g.SSLMode == "" {
		return "prefer"
	}
	return g.SSLMode
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
	return filepath.Join(ConfigDir(), "gateway.yaml")
}
