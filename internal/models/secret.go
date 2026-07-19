package models

import (
	"errors"
	"os"

	"github.com/zalando/go-keyring"
)

// keychainService is the service name under which Bufflehead stores secrets in
// the OS credential store (macOS Keychain, Windows Credential Manager, or the
// Linux Secret Service).
const keychainService = "Bufflehead"

// SecretKind identifies where a connection's password is stored.
type SecretKind string

const (
	// SecretNone means no password is stored (e.g. trust auth or IAM).
	SecretNone SecretKind = ""
	// SecretKeychain stores the password in the OS credential store, keyed by
	// the bookmark label.
	SecretKeychain SecretKind = "keychain"
	// SecretEnv resolves the password from a named environment variable at
	// connect time. Kept for backward compatibility and headless/CI use.
	SecretEnv SecretKind = "env"
)

// ErrKeychainUnavailable is returned when the OS credential store cannot be
// reached (e.g. a headless Linux box with no Secret Service daemon).
var ErrKeychainUnavailable = errors.New("os keychain is unavailable")

// KeychainAvailable reports whether the OS credential store can be used. It
// performs a lightweight probe by writing and deleting a throwaway entry.
func KeychainAvailable() bool {
	const probeUser = "__bufflehead_probe__"
	if err := keyring.Set(keychainService, probeUser, "1"); err != nil {
		return false
	}
	_ = keyring.Delete(keychainService, probeUser)
	return true
}

// SetSecret stores a password for the given bookmark label in the OS keychain.
// An empty password deletes any existing entry.
func SetSecret(label, password string) error {
	if password == "" {
		return DeleteSecret(label)
	}
	if err := keyring.Set(keychainService, label, password); err != nil {
		return ErrKeychainUnavailable
	}
	return nil
}

// GetSecret retrieves a password for the given bookmark label from the OS
// keychain. It returns ("", nil) when no entry exists.
func GetSecret(label string) (string, error) {
	secret, err := keyring.Get(keychainService, label)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		return "", ErrKeychainUnavailable
	}
	return secret, nil
}

// DeleteSecret removes a stored password for the given bookmark label. Missing
// entries are treated as success.
func DeleteSecret(label string) error {
	err := keyring.Delete(keychainService, label)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return ErrKeychainUnavailable
	}
	return nil
}

// ResolveSecret returns the password for a connection given its storage kind.
//   - SecretKeychain: read from the OS keychain, keyed by label.
//   - SecretEnv: read from the named environment variable.
//   - SecretNone: empty string.
func ResolveSecret(kind SecretKind, label, envVar string) string {
	switch kind {
	case SecretKeychain:
		if s, err := GetSecret(label); err == nil {
			return s
		}
		return ""
	case SecretEnv:
		if envVar != "" {
			return os.Getenv(envVar)
		}
		return ""
	default:
		return ""
	}
}
