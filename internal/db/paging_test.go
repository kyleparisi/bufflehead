package db

import "testing"

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
