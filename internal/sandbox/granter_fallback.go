//go:build !darwin

package sandbox

// This is the granter for every non-macOS platform. There is no App Sandbox to
// negotiate with, so nothing has to be granted.
//
// macOS builds use granter_darwin.go instead — including the unsandboxed ones
// (Developer ID, Setapp, MDM, `gd run`), which never reach it because New()
// returns passthrough when Enabled() is false. New()
// already returns passthrough when Enabled() is false; this exists so the
// package still compiles — and so that a build which somehow reports a
// container without the DarwinKit support compiled in degrades to "everything
// is reachable" rather than failing to build or blocking access.
func newPlatformGranter(*BookmarkStore) Granter { return passthrough{} }
