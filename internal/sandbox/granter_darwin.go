//go:build darwin

// Gated on plain `darwin`, deliberately, rather than on a custom `masbuild`
// tag that would limit DarwinKit to the App Store build.
//
// `go mod tidy` resolves imports across GOOS/GOARCH but NOT across custom build
// tags, so a tag-gated import is invisible to it: the next `go mod tidy` run on
// any machine would drop DarwinKit from go.mod and break the macOS build with
// no local signal. Taking the dependency on every macOS build costs some
// compile time and binary size in the Developer ID, Setapp and MDM builds,
// where these code paths are inert because Enabled() is false. That is a much
// cheaper problem than a dependency that deletes itself.
//
// IMPORTANT: this file has never been compiled. The development container it
// was written in has no macOS cross-toolchain (`clang: unsupported option
// '-arch'`), and CGO_ENABLED=0 cannot type-check it because go-duckdb fails
// without cgo. Signatures were taken from DarwinKit v0.5.0's generated source,
// but the first real build is on a Mac. See docs/mas-sandbox-audit.md.

package sandbox

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/progrium/darwinkit/dispatch"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
)

// bookmarkGranter negotiates access to paths outside the App Sandbox container
// using security-scoped bookmarks.
type bookmarkGranter struct {
	store *BookmarkStore
}

func newPlatformGranter(store *BookmarkStore) Granter {
	return &bookmarkGranter{store: store}
}

// Granted reports whether a stored bookmark covers path. It deliberately does
// not resolve the bookmark: this is the cheap check the UI uses to decide
// whether to offer a "connect your AWS config" affordance, and resolving on
// every call would consume security-scoped resources for a question that only
// needs a lookup.
func (g *bookmarkGranter) Granted(path string) bool {
	_, _, ok := g.store.Lookup(path)
	return ok
}

// Ensure makes path reachable, prompting once if there is no usable grant.
func (g *bookmarkGranter) Ensure(ctx context.Context, path string) (Release, error) {
	if err := ctx.Err(); err != nil {
		return noRelease, err
	}

	if dir, bookmark, ok := g.store.Lookup(path); ok {
		release, stale, err := startAccess(bookmark)
		switch {
		case err == nil && !stale:
			return release, nil
		case err == nil && stale:
			// The bookmark still resolved, so access works right now; but the
			// target moved and the bookmark should be rewritten before it
			// stops resolving entirely. Refresh it opportunistically and keep
			// the working grant either way.
			g.refresh(dir)
			return release, nil
		}
		// A grant that no longer resolves is worse than no grant: it would
		// make every later Ensure fail the same way. Drop it and re-prompt.
		_ = g.store.Delete(dir)
	}

	return g.prompt(ctx, path)
}

// prompt asks the user to grant access to the directory containing path, then
// records a bookmark for it.
func (g *bookmarkGranter) prompt(ctx context.Context, path string) (Release, error) {
	dir := grantDirFor(path)

	url, ok, err := runOpenPanel(ctx, dir)
	if err != nil {
		return noRelease, err
	}
	if !ok {
		return noRelease, fmt.Errorf("sandbox: access to %s was not granted", dir)
	}

	// Use the path the panel returned, not the one we asked for. The user may
	// have navigated elsewhere, and macOS hands back a fully resolved path
	// (symlinks and firmlinks collapsed) which is what later lookups must key
	// on to match.
	granted := url.Path()

	bookmark := url.BookmarkDataWithOptionsIncludingResourceValuesForKeysRelativeToURLError(
		foundation.URLBookmarkCreationWithSecurityScope, nil, nil, nil)
	if len(bookmark) == 0 {
		return noRelease, fmt.Errorf("sandbox: could not create a security-scoped bookmark for %s", granted)
	}
	if err := g.store.Put(granted, bookmark); err != nil {
		return noRelease, err
	}

	release, _, err := startAccess(bookmark)
	if err != nil {
		return noRelease, err
	}
	return release, nil
}

// refresh re-creates a stale bookmark for dir without prompting. Best effort:
// the caller already holds working access, so a failure here is not fatal.
func (g *bookmarkGranter) refresh(dir string) {
	url := foundation.URL_FileURLWithPathIsDirectory(dir, true)
	bookmark := url.BookmarkDataWithOptionsIncludingResourceValuesForKeysRelativeToURLError(
		foundation.URLBookmarkCreationWithSecurityScope, nil, nil, nil)
	if len(bookmark) > 0 {
		_ = g.store.Put(dir, bookmark)
	}
}

// startAccess resolves a bookmark and begins security-scoped access to it.
func startAccess(bookmark []byte) (Release, bool, error) {
	var stale bool
	url := foundation.URL_URLByResolvingBookmarkDataOptionsRelativeToURLBookmarkDataIsStaleError(
		bookmark, foundation.URLBookmarkResolutionWithSecurityScope, nil, &stale, nil)

	if url.Path() == "" {
		return noRelease, stale, fmt.Errorf("sandbox: bookmark no longer resolves")
	}
	if !url.StartAccessingSecurityScopedResource() {
		return noRelease, stale, fmt.Errorf("sandbox: could not start access to %s", url.Path())
	}

	// Security-scoped resources are a limited kernel resource, so the stop must
	// be idempotent — a double Release that stopped access twice would unbalance
	// the retain count and revoke a grant another caller still holds.
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		url.StopAccessingSecurityScopedResource()
	}, stale, nil
}

// runOpenPanel shows a directory picker rooted at dir and returns the chosen
// URL.
//
// AppKit requires this on the main thread. Godot owns the NSApplication and
// calls into Go from its main thread during UI callbacks, so we are usually
// already there — and dispatching *synchronously* to the main queue from the
// main thread deadlocks. Hence the explicit check rather than an unconditional
// DispatchSync.
func runOpenPanel(ctx context.Context, dir string) (foundation.URL, bool, error) {
	var (
		chosen foundation.URL
		ok     bool
	)

	show := func() {
		panel := appkit.OpenPanel_OpenPanel()
		panel.SetCanChooseDirectories(true)
		panel.SetCanChooseFiles(false)
		panel.SetAllowsMultipleSelection(false)
		panel.SetResolvesAliases(true)
		panel.SetMessage(fmt.Sprintf(
			"Bufflehead needs access to %s to read your existing credentials.", dir))
		panel.SetPrompt("Grant Access")
		panel.SetDirectoryURL(foundation.URL_FileURLWithPathIsDirectory(dir, true))

		if panel.RunModal() != appkit.ModalResponseOK {
			return
		}
		urls := panel.URLs()
		if len(urls) == 0 {
			return
		}
		chosen, ok = urls[0], true
	}

	if foundation.Thread_CurrentThread().IsMainThread() {
		show()
	} else {
		dispatch.MainQueue().DispatchSync(show)
	}

	if err := ctx.Err(); err != nil {
		return chosen, false, err
	}
	return chosen, ok, nil
}

// grantDirFor returns the directory a grant should be requested for.
//
// Bookmarks are requested on directories, not files: a bookmark for
// ~/.aws/config would not cover ~/.aws/sso/cache, and the user would be
// prompted twice for what they think of as one thing.
func grantDirFor(path string) string {
	if fi, err := statPath(path); err == nil && fi.IsDir() {
		return path
	}
	return filepath.Dir(path)
}
