package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"bufflehead/internal/models"
)

// bookmarkFile is the on-disk name of the grant store, kept in the app config
// directory (which is inside the container when sandboxed).
const bookmarkFile = "sandbox-grants.json"

// BookmarkStore persists security-scoped bookmarks by the directory they were
// granted for.
//
// A bookmark covers the directory it was created from *and everything beneath
// it*, so a grant on ~/.aws also reaches ~/.aws/sso/cache/token.json. Lookup is
// therefore an ancestor search, not an exact match — otherwise the app would
// re-prompt for every file inside a directory the user already granted.
//
// The stored bytes are opaque platform data. Nothing in this type interprets
// them, which is what keeps it testable on every platform.
type BookmarkStore struct {
	mu   sync.RWMutex
	path string
	// grants maps a cleaned absolute directory to its bookmark bytes.
	grants map[string][]byte
}

// NewBookmarkStore opens the grant store in the app config directory, creating
// it lazily on first write. A store that cannot be read starts empty rather
// than failing: a corrupt grant file should cost the user one re-grant, not a
// broken app.
func NewBookmarkStore() *BookmarkStore {
	return NewBookmarkStoreAt(filepath.Join(models.ConfigDir(), bookmarkFile))
}

// NewBookmarkStoreAt opens the grant store at an explicit path. Tests use this.
func NewBookmarkStoreAt(path string) *BookmarkStore {
	s := &BookmarkStore{path: path, grants: map[string][]byte{}}
	s.load()
	return s
}

func (s *BookmarkStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var raw map[string][]byte
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "bufflehead: sandbox grants unreadable, starting empty: %v\n", err)
		return
	}
	for k, v := range raw {
		if k == "" || len(v) == 0 {
			continue
		}
		s.grants[canonical(k)] = v
	}
}

// save writes the store atomically, so an interrupted write cannot leave a
// truncated file that loses every grant the user has made.
func (s *BookmarkStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("sandbox: create grant dir: %w", err)
	}
	data, err := json.MarshalIndent(s.grants, "", "  ")
	if err != nil {
		return fmt.Errorf("sandbox: encode grants: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".grants-*")
	if err != nil {
		return fmt.Errorf("sandbox: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("sandbox: write grants: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sandbox: close grants: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("sandbox: chmod grants: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("sandbox: replace grants: %w", err)
	}
	return nil
}

// Put records a bookmark for dir, replacing any existing grant for it.
func (s *BookmarkStore) Put(dir string, bookmark []byte) error {
	if len(bookmark) == 0 {
		return fmt.Errorf("sandbox: refusing to store an empty bookmark for %q", dir)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[canonical(dir)] = bookmark
	return s.save()
}

// Lookup returns the bookmark covering path — an exact grant on it, or a grant
// on the nearest granted ancestor. The returned dir is the directory the
// bookmark was created for, which is what the platform layer must resolve and
// start access on.
func (s *BookmarkStore) Lookup(path string) (dir string, bookmark []byte, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	want := canonical(path)
	// Longest match wins, so a narrow grant on ~/.aws/sso is preferred over a
	// broad one on ~ when both exist.
	best := ""
	for granted := range s.grants {
		if !covers(granted, want) {
			continue
		}
		if len(granted) > len(best) {
			best = granted
		}
	}
	if best == "" {
		return "", nil, false
	}
	return best, s.grants[best], true
}

// Delete removes the grant for dir. Missing grants are not an error.
func (s *BookmarkStore) Delete(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := canonical(dir)
	if _, exists := s.grants[key]; !exists {
		return nil
	}
	delete(s.grants, key)
	return s.save()
}

// Dirs lists every granted directory, sorted. Used by the UI to show the user
// what they have granted and let them revoke it.
func (s *BookmarkStore) Dirs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.grants))
	for dir := range s.grants {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// canonical normalises a path for comparison: absolute where possible, cleaned,
// and without a trailing separator.
//
// Note it does NOT resolve symlinks. On macOS the home directory is reachable
// as both /Users/x and /System/Volumes/Data/Users/x, so the platform layer
// should hand this store the path it actually got back from the open panel,
// which is already resolved.
func canonical(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	if len(p) > 1 {
		p = strings.TrimSuffix(p, string(filepath.Separator))
	}
	return p
}

// covers reports whether granted is want, or a directory containing it.
//
// The separator check is what stops "/a/b" from appearing to cover "/a/bc":
// a prefix test alone would match, and would silently hand out access to a
// sibling directory the user never granted.
func covers(granted, want string) bool {
	if granted == want {
		return true
	}
	if granted == string(filepath.Separator) {
		return strings.HasPrefix(want, granted)
	}
	return strings.HasPrefix(want, granted+string(filepath.Separator))
}
