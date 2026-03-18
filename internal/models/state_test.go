package models

import (
	"testing"
)


func TestVirtualSQL_NoSort(t *testing.T) {
	s := NewAppState()
	s.UserSQL = "SELECT * FROM 'test.parquet'"
	want := `SELECT * FROM (SELECT * FROM 'test.parquet') _q`
	if got := s.VirtualSQL(); got != want {
		t.Errorf("VirtualSQL() = %q, want %q", got, want)
	}
}

func TestVirtualSQL_SortAsc(t *testing.T) {
	s := NewAppState()
	s.UserSQL = "SELECT * FROM 'test.parquet'"
	s.SortColumn = "score"
	s.SortDir = SortAsc
	want := `SELECT * FROM (SELECT * FROM 'test.parquet') _q ORDER BY "score" ASC`
	if got := s.VirtualSQL(); got != want {
		t.Errorf("VirtualSQL() = %q, want %q", got, want)
	}
}

func TestVirtualSQL_SortDesc(t *testing.T) {
	s := NewAppState()
	s.UserSQL = "SELECT * FROM 'test.parquet'"
	s.SortColumn = "name"
	s.SortDir = SortDesc
	want := `SELECT * FROM (SELECT * FROM 'test.parquet') _q ORDER BY "name" DESC`
	if got := s.VirtualSQL(); got != want {
		t.Errorf("VirtualSQL() = %q, want %q", got, want)
	}
}

func TestSortCycle(t *testing.T) {
	s := NewAppState()
	// New column → asc
	s.SortColumn = "id"
	s.SortDir = SortAsc
	if s.SortDir != SortAsc {
		t.Fatal("expected SortAsc")
	}
	// Same column again → desc
	s.SortDir = SortDesc
	if s.SortDir != SortDesc {
		t.Fatal("expected SortDesc")
	}
	// Same column again → none
	s.SortColumn = ""
	s.SortDir = SortNone
	if s.SortDir != SortNone {
		t.Fatal("expected SortNone")
	}
}

func TestResolveDetailValue(t *testing.T) {
	tests := []struct {
		name      string
		col       int
		singleRow []string
		multiRows [][]string
		want      string
	}{
		{
			name:      "single row returns value",
			col:       1,
			singleRow: []string{"a", "b", "c"},
			want:      "b",
		},
		{
			name:      "single row out of range returns empty",
			col:       5,
			singleRow: []string{"a", "b"},
			want:      "",
		},
		{
			name:      "single row nil returns empty",
			col:       0,
			singleRow: nil,
			want:      "",
		},
		{
			name:      "multi rows all same returns value",
			col:       0,
			multiRows: [][]string{{"x", "1"}, {"x", "2"}, {"x", "3"}},
			want:      "x",
		},
		{
			name:      "multi rows differ returns dash",
			col:       1,
			multiRows: [][]string{{"x", "1"}, {"x", "2"}, {"x", "1"}},
			want:      "—",
		},
		{
			name:      "multi rows single row delegates",
			col:       0,
			multiRows: [][]string{{"only"}},
			want:      "only",
		},
		{
			name:      "multi rows empty returns empty",
			col:       0,
			multiRows: [][]string{},
			want:      "",
		},
		{
			name:      "multi rows col out of range returns empty for all",
			col:       5,
			multiRows: [][]string{{"a"}, {"b"}},
			want:      "",
		},
		{
			name:      "multi rows col out of range for some returns dash",
			col:       1,
			multiRows: [][]string{{"a", "b"}, {"c"}},
			want:      "—",
		},
		{
			name:      "multi rows all empty strings returns empty",
			col:       0,
			multiRows: [][]string{{"", "x"}, {"", "y"}},
			want:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDetailValue(tt.col, tt.singleRow, tt.multiRows)
			if got != tt.want {
				t.Errorf("ResolveDetailValue(%d) = %q, want %q", tt.col, got, tt.want)
			}
		})
	}
}
