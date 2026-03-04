package models

import "testing"

func TestNewAppState(t *testing.T) {
	s := NewAppState()
	if s == nil {
		t.Fatal("expected non-nil AppState")
	}
	if s.PageSize != 100 {
		t.Errorf("PageSize = %d, want 100", s.PageSize)
	}
	if s.PageOffset != 0 {
		t.Errorf("PageOffset = %d, want 0", s.PageOffset)
	}
	if s.FilePath != "" {
		t.Errorf("FilePath = %q, want empty", s.FilePath)
	}
}
