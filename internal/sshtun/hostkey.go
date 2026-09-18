package sshtun

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// DefaultKnownHostsPath returns the path OpenSSH uses, ~/.ssh/known_hosts.
func DefaultKnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// hostKeyCallback builds the host-key verifier for a config.
//
// Verification is always on: a jump host that can be impersonated is a jump
// host that can read every query and credential that crosses it. We deliberately
// do not offer an "ignore host key" switch — instead an unknown host produces an
// error that says exactly how to trust it, which is a one-time `ssh` away.
func (c Config) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if c.HostKeyCallback != nil {
		return c.HostKeyCallback, nil // tests, and callers with their own policy
	}

	path := c.KnownHostsPath
	if path == "" {
		p, err := DefaultKnownHostsPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	expanded, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(expanded); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no known_hosts file at %s — connect once from a terminal with `ssh %s` "+
				"to record the host key, then try again", expanded, c.addr())
		}
		return nil, fmt.Errorf("read known_hosts: %w", err)
	}

	verify, err := knownhosts.New(expanded)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", expanded, err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := verify(hostname, remote, key); err != nil {
			return describeHostKeyError(err, c.addr(), expanded, key)
		}
		return nil
	}, nil
}

// describeHostKeyError turns knownhosts' terse failures into something a user
// can act on: either "I've never seen this host" (with the command to fix it)
// or "the key changed", which is the one that deserves alarm.
func describeHostKeyError(err error, addr, knownHostsPath string, key ssh.PublicKey) error {
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return fmt.Errorf("verify host key for %s: %w", addr, err)
	}

	if len(keyErr.Want) > 0 {
		return fmt.Errorf("HOST KEY MISMATCH for %s.\n\n"+
			"The server presented a %s key that does not match the one recorded in %s. "+
			"This can mean the host was rebuilt — or that the connection is being intercepted. "+
			"Bufflehead will not connect until you resolve it.\n\n"+
			"If you know the host legitimately changed, remove the old entry with:\n"+
			"  ssh-keygen -R %q",
			addr, key.Type(), knownHostsPath, hostForKeygen(addr))
	}

	return fmt.Errorf("host key for %s is not in %s.\n\n"+
		"Bufflehead only connects to SSH hosts you have already trusted. Connect once from a terminal:\n"+
		"  ssh %s\n"+
		"accept the fingerprint, then try again.",
		addr, knownHostsPath, addr)
}

// hostForKeygen renders the host in the form `ssh-keygen -R` expects:
// "[host]:port" for a non-default port, bare host otherwise.
func hostForKeygen(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if port == "22" {
		return host
	}
	return fmt.Sprintf("[%s]:%s", host, port)
}
