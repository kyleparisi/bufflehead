package models

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestStore builds a BookmarkStore backed by a temp file so the test never
// touches the real user config dir. Same-package access lets us set the path.
func newTestStore(t *testing.T) *BookmarkStore {
	t.Helper()
	return &BookmarkStore{path: filepath.Join(t.TempDir(), "bookmarks.json")}
}

func sampleBookmark(label string) Bookmark {
	return Bookmark{
		Label:      label,
		Env:        "prod",
		AWSProfile: "my-sso",
		AWSRegion:  "us-east-1",
		RDSHost:    "db.internal",
		RDSPort:    5432,
		DBName:     "app",
		DBUser:     "reader",
		AuthMode:   "password",
	}
}

// TestBookmarkStoreRoundtrip is the cross-platform (incl. Windows) check that a
// saved bookmark persists to disk and reloads with all fields intact.
func TestBookmarkStoreRoundtrip(t *testing.T) {
	bs := newTestStore(t)

	if err := bs.Add(sampleBookmark("prod-analytics")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := bs.Add(sampleBookmark("staging")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The file must actually exist on disk.
	if _, err := os.Stat(bs.Path()); err != nil {
		t.Fatalf("bookmarks file not written: %v", err)
	}

	// A fresh store reading the same file must see both bookmarks with fields intact.
	reloaded := &BookmarkStore{path: bs.Path()}
	reloaded.load()

	all := reloaded.All()
	if len(all) != 2 {
		t.Fatalf("reloaded %d bookmarks, want 2", len(all))
	}
	got := reloaded.FindByLabel("prod-analytics")
	if got == nil {
		t.Fatal("prod-analytics not found after reload")
	}
	if got.RDSHost != "db.internal" || got.RDSPort != 5432 || got.DBUser != "reader" || got.Env != "prod" {
		t.Errorf("reloaded bookmark fields mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("timestamps should be set on save")
	}
}

func TestBookmarkStoreUpdateAndRemove(t *testing.T) {
	bs := newTestStore(t)
	_ = bs.Add(sampleBookmark("prod"))

	// Adding the same label updates in place (no duplicate).
	upd := sampleBookmark("prod")
	upd.DBName = "changed"
	_ = bs.Add(upd)
	if len(bs.All()) != 1 {
		t.Fatalf("same-label Add should update, got %d bookmarks", len(bs.All()))
	}
	if bs.FindByLabel("prod").DBName != "changed" {
		t.Error("update did not persist new field")
	}

	// Remove, then confirm it's gone from disk too.
	if err := bs.Remove("prod"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	reloaded := &BookmarkStore{path: bs.Path()}
	reloaded.load()
	if len(reloaded.All()) != 0 {
		t.Errorf("bookmark still present after remove+reload")
	}
}

func TestBookmarkPathIsUnderConfigDir(t *testing.T) {
	p := bookmarkPath()
	if !filepath.IsAbs(p) {
		t.Errorf("bookmark path should be absolute, got %q", p)
	}
	if filepath.Base(p) != "bookmarks.json" {
		t.Errorf("bookmark path should end in bookmarks.json, got %q", p)
	}
}

func TestAddRejectsInvalidLabel(t *testing.T) {
	bs := newTestStore(t)
	if err := bs.Add(sampleBookmark("bad label!")); err == nil {
		t.Error("expected invalid label to be rejected")
	}
}
