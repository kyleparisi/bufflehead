//go:build !darwin

package mcpmode

// restoreSSHAgent is a no-op off macOS, where there is no launchd-managed
// agent socket to recover.
func restoreSSHAgent() {}
