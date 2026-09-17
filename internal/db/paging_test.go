package db

import (
	"context"
	"fmt"
	"testing"
)

func TestPaginate(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		offset, limit int
		want          string
	}{
		{"basic", "SELECT * FROM t", 0, 100, "SELECT * FROM t LIMIT 100 OFFSET 0"},
		{"offset", "SELECT * FROM t", 200, 50, "SELECT * FROM t LIMIT 50 OFFSET 200"},
		{"trailing semicolon", "SELECT * FROM t;", 0, 10, "SELECT * FROM t LIMIT 10 OFFSET 0"},
		{"trailing space", "SELECT * FROM t  ", 0, 10, "SELECT * FROM t LIMIT 10 OFFSET 0"},
		// A user-supplied trailing LIMIT is respected — no double clause.
		{"user limit", "SELECT * FROM t LIMIT 5", 0, 100, "SELECT * FROM t LIMIT 5"},
		{"user limit offset", "SELECT * FROM t LIMIT 5 OFFSET 10", 0, 100, "SELECT * FROM t LIMIT 5 OFFSET 10"},
		{"user limit mysql comma", "SELECT * FROM t LIMIT 10, 5", 0, 100, "SELECT * FROM t LIMIT 10, 5"},
		{"user limit trailing semicolon", "SELECT * FROM t LIMIT 5;", 0, 100, "SELECT * FROM t LIMIT 5"},
		{"user limit lowercase", "select * from t limit 5", 0, 100, "select * from t limit 5"},
		// LIMIT nested in a subquery is not a trailing limit — page the outer query.
		{"nested limit", "SELECT * FROM (SELECT * FROM t LIMIT 5) x", 0, 100,
			"SELECT * FROM (SELECT * FROM t LIMIT 5) x LIMIT 100 OFFSET 0"},
		// limit <= 0 means "no limit": emit no LIMIT clause at all, so a local
		// query returns everything rather than being silently truncated.
		{"no limit", "SELECT * FROM t", 0, 0, "SELECT * FROM t"},
		{"no limit negative", "SELECT * FROM t", 0, -1, "SELECT * FROM t"},
		{"no limit with offset", "SELECT * FROM t", 200, 0, "SELECT * FROM t OFFSET 200"},
		{"no limit trailing semicolon", "SELECT * FROM t;", 0, 0, "SELECT * FROM t"},
		{"no limit respects user limit", "SELECT * FROM t LIMIT 5", 0, 0, "SELECT * FROM t LIMIT 5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paginate(tt.sql, tt.offset, tt.limit); got != tt.want {
				t.Errorf("paginate(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestHasTrailingLimit(t *testing.T) {
	yes := []string{
		"SELECT * FROM t LIMIT 5",
		"SELECT * FROM t LIMIT 5 OFFSET 10",
		"SELECT * FROM t LIMIT 10, 5",
		"select 1 limit 1",
	}
	no := []string{
		"SELECT * FROM t",
		"SELECT * FROM (SELECT 1 LIMIT 5) x",
		"SELECT 'text with LIMIT 5 inside'",
		"SELECT * FROM t ORDER BY id",
	}
	for _, s := range yes {
		if !hasTrailingLimit(s) {
			t.Errorf("hasTrailingLimit(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if hasTrailingLimit(s) {
			t.Errorf("hasTrailingLimit(%q) = true, want false", s)
		}
	}
}

// TestQueryWithoutLimitReturnsEverything covers the local-connection default:
// no limit means no truncation, up to the maxResultRows ceiling.
func TestQueryWithoutLimitReturnsEverything(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	res, err := d.Query(context.Background(), "SELECT * FROM range(250)", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 250 {
		t.Errorf("Total = %d, want 250", res.Total)
	}
	if len(res.Rows) != 250 {
		t.Errorf("got %d rows, want all 250 — an unlimited query must not be paged", len(res.Rows))
	}

	// The ceiling still applies: it bounds memory no matter what was asked for.
	res, err = d.Query(context.Background(), fmt.Sprintf("SELECT * FROM range(%d)", maxResultRows+500), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != int64(maxResultRows+500) {
		t.Errorf("Total = %d, want %d — the count is not truncated", res.Total, maxResultRows+500)
	}
	if len(res.Rows) != maxResultRows {
		t.Errorf("got %d rows, want the %d-row ceiling", len(res.Rows), maxResultRows)
	}
}
