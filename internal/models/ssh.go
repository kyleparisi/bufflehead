package models

import (
	"fmt"
	"os/user"
	"strings"
)

// SSHAuthMethod selects how Bufflehead authenticates to the SSH jump host.
type SSHAuthMethod string

const (
	// SSHAuthAgent authenticates with the keys held by the running ssh-agent,
	// falling back to the default identity files (~/.ssh/id_ed25519, id_rsa,
	// …) when no agent is reachable. This is the default because it matches
	// what `ssh host` already does for most people.
	SSHAuthAgent SSHAuthMethod = ""
	// SSHAuthKey authenticates with a specific private key file, decrypting it
	// with the stored passphrase when the key is encrypted.
	SSHAuthKey SSHAuthMethod = "key"
	// SSHAuthPassword authenticates with a login password for the SSH user.
	SSHAuthPassword SSHAuthMethod = "password"
)

// SSHTunnel describes an SSH jump host used to reach a database that is not
// directly routable. It is orthogonal to the connection Kind: any direct
// TCP-dialled engine (Postgres, MySQL) can be reached through one, so this
// rides along on the entry rather than multiplying ConnKind values.
//
// Secret holds either the private-key passphrase (SSHAuthKey) or the login
// password (SSHAuthPassword). Like the database password it is never written to
// disk — it lives in the OS keychain (SecretKind=keychain) or a named env var.
type SSHTunnel struct {
	Host       string        `yaml:"host" json:"host"`
	Port       int           `yaml:"port,omitempty" json:"port,omitempty"`
	User       string        `yaml:"user,omitempty" json:"user,omitempty"`
	AuthMethod SSHAuthMethod `yaml:"auth_method,omitempty" json:"auth_method,omitempty"`
	KeyPath    string        `yaml:"key_path,omitempty" json:"key_path,omitempty"`
	SecretKind SecretKind    `yaml:"secret_kind,omitempty" json:"secret_kind,omitempty"`
	SecretEnv  string        `yaml:"secret_env,omitempty" json:"secret_env,omitempty"`

	// Secret is the in-memory passphrase/password for this session only. The
	// yaml/json "-" tags keep it out of gateway.yaml and bookmarks.json.
	Secret string `yaml:"-" json:"-"`
}

// DefaultSSHPort is the port used when an SSH tunnel doesn't name one.
const DefaultSSHPort = 22

// EffectivePort returns the SSH port, defaulting to 22.
func (s *SSHTunnel) EffectivePort() int {
	if s.Port > 0 {
		return s.Port
	}
	return DefaultSSHPort
}

// EffectiveUser returns the SSH login user, defaulting to the OS user — the
// same default `ssh host` applies.
func (s *SSHTunnel) EffectiveUser() string {
	if s.User != "" {
		return s.User
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// Describe renders the jump host as "user@host:port" for status lines and the
// connection breadcrumb.
func (s *SSHTunnel) Describe() string {
	return fmt.Sprintf("%s@%s:%d", s.EffectiveUser(), s.Host, s.EffectivePort())
}

// Validate reports whether the tunnel is configured well enough to dial.
func (s *SSHTunnel) Validate() error {
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("SSH host is required")
	}
	if s.Port < 0 || s.Port > 65535 {
		return fmt.Errorf("SSH port %d is out of range", s.Port)
	}
	if s.AuthMethod == SSHAuthKey && strings.TrimSpace(s.KeyPath) == "" {
		return fmt.Errorf("SSH key file is required for key authentication")
	}
	if s.EffectiveUser() == "" {
		return fmt.Errorf("SSH user is required (could not determine the current OS user)")
	}
	return nil
}

// SSHSecretLabel returns the keychain account under which a connection's SSH
// passphrase/password is stored. It is deliberately distinct from the bookmark
// label used for the database password so the two never collide.
func SSHSecretLabel(label string) string {
	return label + "#ssh"
}

// ResolveSecret returns the SSH passphrase or login password, checking, in
// order: the in-memory value, the OS keychain, then the named env var.
func (s *SSHTunnel) ResolveSecret(label string) string {
	if s.Secret != "" {
		return s.Secret
	}
	return ResolveSecret(s.SecretKind, SSHSecretLabel(label), s.SecretEnv)
}

// Clone returns a deep copy, so a stored bookmark's tunnel and a live
// connection's tunnel never share the in-memory Secret.
func (s *SSHTunnel) Clone() *SSHTunnel {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// Redacted returns a copy with the in-memory secret cleared, for persistence.
func (s *SSHTunnel) Redacted() *SSHTunnel {
	c := s.Clone()
	if c != nil {
		c.Secret = ""
	}
	return c
}
