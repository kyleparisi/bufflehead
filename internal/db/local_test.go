package db

import "testing"

// TestLocalQuerierClassification pins which backends are treated as local. The
// worker skips liveness checks and timeouts for these, so a backend that
// reaches the network must never appear here.
func TestLocalQuerierClassification(t *testing.T) {
	local := []struct {
		name string
		q    Querier
	}{
		{"duckdb", (*DB)(nil)},
		{"sqlite", (*SQLiteDB)(nil)},
	}
	for _, c := range local {
		lq, ok := c.q.(LocalQuerier)
		if !ok {
			t.Errorf("%s: does not implement LocalQuerier", c.name)
			continue
		}
		if !lq.IsLocal() {
			t.Errorf("%s: IsLocal() = false, want true", c.name)
		}
	}

	remote := []struct {
		name string
		q    Querier
	}{
		{"postgres", (*PostgresDB)(nil)},
		{"mysql", (*MySQLDB)(nil)},
		{"bigquery", (*BigQueryDB)(nil)},
	}
	for _, c := range remote {
		if _, ok := c.q.(LocalQuerier); ok {
			t.Errorf("%s: must not be treated as local — it reaches the network", c.name)
		}
	}
}
