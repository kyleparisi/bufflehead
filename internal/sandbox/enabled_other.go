//go:build !darwin

package sandbox

func sandboxEnabled() bool { return false }
