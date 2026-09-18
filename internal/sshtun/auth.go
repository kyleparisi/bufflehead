package sshtun

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Auth method identifiers. They match models.SSHAuthMethod's values so the UI
// layer can pass its stored setting straight through.
const (
	// AuthAgent uses the running ssh-agent, falling back to the default
	// identity files. This is the zero value, so an unconfigured tunnel
	// behaves like plain `ssh host`.
	AuthAgent = ""
	// AuthKey uses one named private key file.
	AuthKey = "key"
	// AuthPassword uses a login password for the SSH user.
	AuthPassword = "password"
)

// defaultIdentityFiles are the key files OpenSSH tries by default, in the same
// order. Used for AuthAgent when no agent is reachable (or it holds no usable
// key) so a plain key-on-disk setup still works.
var defaultIdentityFiles = []string{
	"id_ed25519",
	"id_ecdsa",
	"id_rsa",
}

// authMethods builds the ssh.AuthMethod list for a config. Several methods may
// be returned: the SSH protocol tries each in turn, so an agent that holds the
// wrong key still falls through to the on-disk identities.
func (c Config) authMethods() ([]ssh.AuthMethod, error) {
	switch c.Method {
	case AuthPassword:
		if c.Secret == "" {
			return nil, fmt.Errorf("SSH password authentication selected but no password was provided")
		}
		// Many servers offer only keyboard-interactive for passwords, so
		// answer its prompts with the same secret.
		return []ssh.AuthMethod{
			ssh.Password(c.Secret),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = c.Secret
				}
				return answers, nil
			}),
		}, nil

	case AuthKey:
		signer, err := loadKeyFile(c.KeyPath, c.Secret)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil

	case AuthAgent:
		var methods []ssh.AuthMethod
		if m, err := agentAuth(); err == nil {
			methods = append(methods, m)
		}
		if signers := defaultIdentitySigners(c.Secret); len(signers) > 0 {
			methods = append(methods, ssh.PublicKeys(signers...))
		}
		if len(methods) == 0 {
			return nil, fmt.Errorf("no SSH agent is running (SSH_AUTH_SOCK is unset or unreachable) and no usable key was found in ~/.ssh — " +
				"start an agent with `ssh-add`, or choose Key File authentication")
		}
		return methods, nil

	default:
		return nil, fmt.Errorf("unknown SSH auth method %q", c.Method)
	}
}

// agentAuth connects to the running ssh-agent over SSH_AUTH_SOCK. On Windows
// the agent is a named pipe that this dial cannot reach, so the caller treats a
// failure here as "no agent" and falls back to on-disk identities.
func agentAuth() (ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK is not set")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("dial ssh-agent: %w", err)
	}
	// The connection is intentionally left open for the life of the process:
	// Signers is called lazily during the handshake.
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers), nil
}

// defaultIdentitySigners loads whichever of the default ~/.ssh identity files
// exist and can be decrypted. Unreadable or passphrase-protected keys are
// skipped rather than failing the whole attempt — another identity may work.
func defaultIdentitySigners(passphrase string) []ssh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var signers []ssh.Signer
	for _, name := range defaultIdentityFiles {
		signer, err := loadKeyFile(filepath.Join(home, ".ssh", name), passphrase)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers
}

// loadKeyFile reads and parses a private key, decrypting it with passphrase
// when the key is encrypted. A "~" prefix is expanded so a path typed into the
// connection form works as the user wrote it.
func loadKeyFile(path, passphrase string) (ssh.Signer, error) {
	expanded, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("read SSH key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err == nil {
		return signer, nil
	}

	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("parse SSH key %s: %w", expanded, err)
	}
	if passphrase == "" {
		return nil, fmt.Errorf("SSH key %s is encrypted — enter its passphrase in the connection form", expanded)
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("decrypt SSH key %s: %w", expanded, err)
	}
	return signer, nil
}

// ExpandPath resolves a leading "~" to the user's home directory. Paths without
// one are returned unchanged.
func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve ~: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
