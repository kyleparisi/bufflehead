package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *BookmarkStore {
	t.Helper()
	return NewBookmarkStoreAt(filepath.Join(t.TempDir(), "grants.json"))
}

func TestBookmarkStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.json")
	s := NewBookmarkStoreAt(path)

	grant := filepath.Join(t.TempDir(), "aws")
	want := []byte{0x01, 0x02, 0xFF, 0x00}
	if err := s.Put(grant, want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A fresh store must see it — the grant has to survive relaunch, which is
	// the entire point of a bookmark.
	reopened := NewBookmarkStoreAt(path)
	dir, got, ok := reopened.Lookup(grant)
	if !ok {
		t.Fatal("Lookup after reopen = not found")
	}
	if dir != grant {
		t.Errorf("dir = %q", dir)
	}
	if string(got) != string(want) {
		t.Errorf("bookmark = %v, want %v", got, want)
	}
}

// A grant on a directory must cover everything beneath it, or the app would
// re-prompt for every file inside a directory the user already granted.
func TestBookmarkStoreCoversDescendants(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("/Users/me/.aws", []byte{0x01}); err != nil {
		t.Fatal(err)
	}

	covered := []string{
		"/Users/me/.aws",
		"/Users/me/.aws/config",
		"/Users/me/.aws/sso/cache/token.json",
		"/Users/me/.aws/",
	}
	for _, p := range covered {
		if _, _, ok := s.Lookup(p); !ok {
			t.Errorf("Lookup(%q) = not found, want covered", p)
		}
	}
}

// The bug this guards: a plain prefix test makes "/Users/me/.aws" appear to
// cover "/Users/me/.awsbackup", silently granting a sibling directory.
func TestBookmarkStoreDoesNotCoverSiblings(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("/Users/me/.aws", []byte{0x01}); err != nil {
		t.Fatal(err)
	}

	notCovered := []string{
		"/Users/me/.awsbackup",
		"/Users/me/.aws-old/config",
		"/Users/me",
		"/Users/other/.aws",
		"/",
	}
	for _, p := range notCovered {
		if dir, _, ok := s.Lookup(p); ok {
			t.Errorf("Lookup(%q) = covered by %q, want not found", p, dir)
		}
	}
}

// With overlapping grants the narrowest one must win, so the app starts access
// on the most specific scope the user actually granted.
func TestBookmarkStoreLongestMatchWins(t *testing.T) {
	s := newTestStore(t)
	home := t.TempDir()
	aws := filepath.Join(home, ".aws")
	if err := s.Put(home, []byte{0xAA}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(aws, []byte{0xBB}); err != nil {
		t.Fatal(err)
	}

	dir, got, ok := s.Lookup(filepath.Join(aws, "config"))
	if !ok {
		t.Fatal("not found")
	}
	if dir != aws {
		t.Errorf("dir = %q, want the narrower grant", dir)
	}
	if got[0] != 0xBB {
		t.Errorf("bookmark = %v, want the narrower grant's", got)
	}

	// A path only the broad grant covers still resolves to the broad one.
	if dir, _, ok := s.Lookup(filepath.Join(home, "Documents")); !ok || dir != home {
		t.Errorf("Lookup(Documents) = %q,%v; want %q,true", dir, ok, home)
	}
}

func TestBookmarkStorePathNormalisation(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("/Users/me/.aws/", []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/Users/me/.aws",
		"/Users/me/./.aws",
		"/Users/me/x/../.aws/config",
	} {
		if _, _, ok := s.Lookup(p); !ok {
			t.Errorf("Lookup(%q) = not found", p)
		}
	}
}

func TestBookmarkStoreDelete(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("/Users/me/.aws", []byte{0x01}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("/Users/me/.aws"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, ok := s.Lookup("/Users/me/.aws/config"); ok {
		t.Error("grant survived Delete")
	}
	if err := s.Delete("/not/granted"); err != nil {
		t.Errorf("Delete of a missing grant = %v, want nil", err)
	}
}

func TestBookmarkStoreRejectsEmptyBookmark(t *testing.T) {
	s := newTestStore(t)
	if err := s.Put("/Users/me/.aws", nil); err == nil {
		t.Error("Put accepted an empty bookmark; that would record a grant that cannot resolve")
	}
	if _, _, ok := s.Lookup("/Users/me/.aws"); ok {
		t.Error("empty bookmark was stored anyway")
	}
}

func TestBookmarkStoreDirs(t *testing.T) {
	s := newTestStore(t)
	root := t.TempDir()
	for _, d := range []string{filepath.Join(root, "b"), filepath.Join(root, "a"), filepath.Join(root, "c")} {
		if err := s.Put(d, []byte{0x01}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.Dirs()
	want := []string{filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")}
	if len(got) != len(want) {
		t.Fatalf("Dirs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Dirs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A corrupt grant file must cost one re-grant, not a broken app.
func TestBookmarkStoreSurvivesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewBookmarkStoreAt(path)
	if len(s.Dirs()) != 0 {
		t.Error("corrupt file produced grants")
	}
	if err := s.Put("/Users/me/.aws", []byte{0x01}); err != nil {
		t.Errorf("Put after corrupt load = %v", err)
	}
}

// Outside the sandbox nothing may prompt, and everything must be reachable.
func TestPassthroughGrantsEverything(t *testing.T) {
	t.Setenv(containerEnv, "")
	os.Unsetenv(containerEnv)
	if Enabled() {
		t.Fatal("this passthrough test requires an unsandboxed test binary")
	}

	g := New(newTestStore(t))
	if !g.Granted("/anywhere/at/all") {
		t.Error("Granted() = false outside the sandbox")
	}
	release, err := g.Ensure(context.Background(), "/Users/me/.aws")
	if err != nil {
		t.Fatalf("Ensure outside the sandbox = %v, want nil", err)
	}
	if release == nil {
		t.Fatal("Ensure returned a nil Release")
	}
	release()
	release() // must be safe twice
}

func TestEnabledDetectionIgnoresEnvironment(t *testing.T) {
	before := Enabled()
	t.Setenv(containerEnv, "forged-container-id")
	if Enabled() != before {
		t.Fatal("environment must not override signed sandbox entitlement")
	}
	t.Setenv(containerEnv, "")
	os.Unsetenv(containerEnv)
	if Enabled() != before {
		t.Fatal("missing environment must not hide sandbox entitlement")
	}
}
