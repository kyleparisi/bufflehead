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

func TestFormatRowsTSV(t *testing.T) {
	tests := []struct {
		name        string
		columns     []string
		rows        [][]string
		withHeaders bool
		want        string
	}{
		{
			name:        "single row without headers",
			columns:     []string{"id", "name"},
			rows:        [][]string{{"1", "alice"}},
			withHeaders: false,
			want:        "1\talice",
		},
		{
			name:        "single row with headers",
			columns:     []string{"id", "name"},
			rows:        [][]string{{"1", "alice"}},
			withHeaders: true,
			want:        "id\tname\n1\talice",
		},
		{
			name:        "multiple rows with headers",
			columns:     []string{"id", "name", "score"},
			rows:        [][]string{{"1", "alice", "90"}, {"2", "bob", "85"}},
			withHeaders: true,
			want:        "id\tname\tscore\n1\talice\t90\n2\tbob\t85",
		},
		{
			name:        "multiple rows without headers",
			columns:     []string{"a", "b"},
			rows:        [][]string{{"x", "y"}, {"z", "w"}},
			withHeaders: false,
			want:        "x\ty\nz\tw",
		},
		{
			name:        "empty rows",
			columns:     []string{"id"},
			rows:        [][]string{},
			withHeaders: true,
			want:        "id\n",
		},
		{
			name:        "empty rows no headers",
			columns:     []string{"id"},
			rows:        [][]string{},
			withHeaders: false,
			want:        "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatRowsTSV(tt.columns, tt.rows, tt.withHeaders)
			if got != tt.want {
				t.Errorf("FormatRowsTSV() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatColumnValues(t *testing.T) {
	tests := []struct {
		name    string
		rows    [][]string
		col     int
		numeric bool
		want    string
	}{
		{
			name:    "numeric values",
			rows:    [][]string{{"1", "alice"}, {"2", "bob"}, {"3", "carol"}},
			col:     0,
			numeric: true,
			want:    "1, 2, 3",
		},
		{
			name:    "string values",
			rows:    [][]string{{"1", "alice"}, {"2", "bob"}},
			col:     1,
			numeric: false,
			want:    "'alice', 'bob'",
		},
		{
			name:    "string with single quotes escaped",
			rows:    [][]string{{"1", "it's"}, {"2", "they're"}},
			col:     1,
			numeric: false,
			want:    "'it''s', 'they''re'",
		},
		{
			name:    "single row numeric",
			rows:    [][]string{{"42", "x"}},
			col:     0,
			numeric: true,
			want:    "42",
		},
		{
			name:    "single row string",
			rows:    [][]string{{"42", "x"}},
			col:     1,
			numeric: false,
			want:    "'x'",
		},
		{
			name:    "empty rows",
			rows:    [][]string{},
			col:     0,
			numeric: true,
			want:    "",
		},
		{
			name:    "col out of range skipped",
			rows:    [][]string{{"a"}, {"b"}},
			col:     5,
			numeric: false,
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatColumnValues(tt.rows, tt.col, tt.numeric)
			if got != tt.want {
				t.Errorf("FormatColumnValues() = %q, want %q", got, tt.want)
			}
		})
	}
}
