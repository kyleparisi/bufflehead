package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Session keeps scoped access alive for asynchronous queries and later SQL.
// Callers serialize Open/Close on the UI thread; Close runs at app shutdown.
type Session struct {
	granter  Granter
	releases map[string]Release
}

func NewSession(g Granter) *Session { return &Session{granter: g, releases: map[string]Release{}} }
func (s *Session) Open(path string, prompt bool) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	key := s.scopeKey(path)
	if _, ok := s.releases[key]; ok {
		return nil
	}
	// Restore even if a native picker has temporarily made the path readable.
	// A saved bookmark must stay active for the application's async workers.
	var release Release
	if s.granter.Granted(path) {
		release, err = s.granter.Restore(path)
		if err != nil && prompt {
			release, err = s.granter.Ensure(context.Background(), path)
		}
	} else if prompt {
		// A file picker may confer temporary access; still persist a folder grant.
		release, err = s.granter.Ensure(context.Background(), path)
	} else {
		f, openErr := os.Open(path)
		if openErr == nil {
			f.Close()
			return nil
		}
		return fmt.Errorf("sandbox: open this folder in Bufflehead first to grant access: %w", openErr)
	}

	if err != nil {
		return err
	}
	s.releases[s.scopeKey(path)] = release
	return nil
}
func (s *Session) Close() {
	for path, release := range s.releases {
		release()
		delete(s.releases, path)
	}
}

func (s *Session) scopeKey(path string) string {
	if g, ok := s.granter.(interface{ scopeKey(string) string }); ok {
		return g.scopeKey(path)
	}
	return path
}
