package models

import "testing"

func TestQueryHistoryScopedByConnection(t *testing.T) {
	h := &QueryHistory{maxSize: 500}
	h.Entries = []HistoryEntry{
		{SQL: "a1", Conn: "connA"},
		{SQL: "b1", Conn: "connB"},
		{SQL: "a2", Conn: "connA"},
		{SQL: "g1", Conn: ""}, // legacy global entry, no connection
	}

	a := h.AllFor("connA")
	if len(a) != 2 {
		t.Fatalf("connA: got %d entries, want 2", len(a))
	}
	// Newest first.
	if a[0].SQL != "a2" || a[1].SQL != "a1" {
		t.Errorf("connA order = %q,%q; want a2,a1", a[0].SQL, a[1].SQL)
	}

	if b := h.AllFor("connB"); len(b) != 1 || b[0].SQL != "b1" {
		t.Errorf("connB = %+v; want single b1", b)
	}

	// An unknown connection sees nothing (legacy global entries stay hidden).
	if u := h.AllFor("connC"); len(u) != 0 {
		t.Errorf("connC = %+v; want empty", u)
	}

	// RecentFor caps the result count.
	if r := h.RecentFor("connA", 1); len(r) != 1 || r[0].SQL != "a2" {
		t.Errorf("RecentFor connA n=1 = %+v; want [a2]", r)
	}
}
