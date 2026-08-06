// Package sandbox handles reaching files outside the macOS App Sandbox
// container.
//
// A sandboxed app can only touch its own container plus whatever the user has
// explicitly granted. Bufflehead needs paths that live well outside it — the
// user's ~/.aws for SSO config and token cache, and ~/.config/gcloud for
// BigQuery Application Default Credentials. There is no entitlement for those;
// the only sanctioned route is a *security-scoped bookmark*: the user picks the
// directory once through a native open panel, and the app persists a bookmark
// it can re-resolve on every later launch.
//
// The subtlety this package exists to contain is that outside the sandbox none
// of that applies. Unsandboxed builds — Developer ID, Setapp, MDM, `gd run`,
// and every non-macOS platform — reach those paths directly and must never
// prompt. So callers ask for access unconditionally and this package decides
// whether that means "do nothing" or "resolve a bookmark".
package sandbox

import (
	"context"
	"io/fs"
	"os"
)

// containerEnv is set by macOS for processes running inside an App Sandbox
// container. Its presence is the signal that access has to be negotiated.
const containerEnv = "APP_SANDBOX_CONTAINER_ID"

// Enabled reports whether this process is running inside an App Sandbox
// container.
//
// It is deliberately not a build-tag check. The same MAS binary is what App
// Review runs and what `gd run` loads during development, and a developer
// running an unsandboxed copy of a sandbox-configured build must not be
// prompted for access it already has.
func Enabled() bool {
	_, ok := os.LookupEnv(containerEnv)
	return ok
}

// Release undoes an access grant. It is always safe to call, and safe to call
// more than once.
//
// Security-scoped resources are a limited kernel resource, so every successful
// Ensure must be paired with its Release — leaking them eventually causes new
// grants to fail with no useful error.
type Release func()

// noRelease is the Release for a grant that consumed nothing.
func noRelease() {}

// Granter makes paths outside the container reachable.
type Granter interface {
	// Ensure makes path reachable, returning a Release that must be called
	// when the caller is done with it.
	//
	// If no grant exists yet and prompting is allowed, Ensure may present a
	// native open panel. It must be called from a context where blocking on
	// user interaction is acceptable, and it honours ctx cancellation.
	Ensure(ctx context.Context, path string) (Release, error)

	// Granted reports whether path is reachable right now with no prompting.
	// Use it to decide whether to show "connect your AWS config" UI rather
	// than surprising the user with a panel mid-flow.
	Granted(path string) bool
}

// New returns the Granter appropriate to this process: a real bookmark-backed
// one when sandboxed on macOS, otherwise a pass-through that grants everything
// without prompting.
func New(store *BookmarkStore) Granter {
	if !Enabled() {
		return passthrough{}
	}
	return newPlatformGranter(store)
}

// statPath is os.Stat behind a variable so the darwin granter's directory
// detection can be exercised without touching the real filesystem.
var statPath = func(p string) (fs.FileInfo, error) { return os.Stat(p) }

// passthrough grants everything. It is what runs outside the sandbox, where
// the filesystem is already reachable and prompting would be user-hostile.
type passthrough struct{}

func (passthrough) Ensure(context.Context, string) (Release, error) {
	return noRelease, nil
}

func (passthrough) Granted(string) bool { return true }
