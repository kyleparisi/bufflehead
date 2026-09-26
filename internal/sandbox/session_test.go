package sandbox

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type testGranter struct {
	saved                       bool
	prompts, restores, releases int
	err                         error
}

func (g *testGranter) Granted(string) bool { return g.saved }
func (g *testGranter) Ensure(context.Context, string) (Release, error) {
	g.prompts++
	return func() { g.releases++ }, g.err
}
func (g *testGranter) Restore(string) (Release, error) {
	g.restores++
	return func() { g.releases++ }, g.err
}
func TestSessionRetainsRestoredGrantUntilClose(t *testing.T) {
	g := &testGranter{saved: true}
	s := NewSession(g)
	p := filepath.Join(t.TempDir(), "file.csv")
	for range 2 {
		if err := s.Open(p, false); err != nil {
			t.Fatal(err)
		}
	}
	if g.restores != 1 || g.prompts != 0 || g.releases != 0 {
		t.Fatalf("unexpected lifecycle: %+v", g)
	}
	s.Close()
	s.Close()
	if g.releases != 1 {
		t.Fatal(g.releases)
	}
}
func TestSessionAPINeverPrompts(t *testing.T) {
	for _, saved := range []bool{false, true} {
		g := &testGranter{saved: saved, err: errors.New("stale grant")}
		s := NewSession(g)
		if s.Open(filepath.Join(t.TempDir(), "missing.csv"), false) == nil {
			t.Fatal("expected failure")
		}
		if g.prompts != 0 {
			t.Fatal("API prompted")
		}
	}
}
func TestSessionUserOpenPrompts(t *testing.T) {
	g := &testGranter{}
	s := NewSession(g)
	if err := s.Open(filepath.Join(t.TempDir(), "data.csv"), true); err != nil {
		t.Fatal(err)
	}
	if g.prompts != 1 {
		t.Fatal("no prompt")
	}
	s.Close()
}

type folderGranter struct{ testGranter }

func (g *folderGranter) scopeKey(path string) string { return filepath.Dir(path) }
func TestSessionSharesOneScopeAcrossFolderFiles(t *testing.T) {
	g := &folderGranter{testGranter: testGranter{saved: true}}
	s := NewSession(g)
	dir := t.TempDir()
	for _, name := range []string{"a.csv", "b.csv"} {
		if err := s.Open(filepath.Join(dir, name), false); err != nil {
			t.Fatal(err)
		}
	}
	if g.restores != 1 {
		t.Fatalf("restored %d times", g.restores)
	}
	s.Close()
	if g.releases != 1 {
		t.Fatal(g.releases)
	}
}
