package models

import "testing"

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
