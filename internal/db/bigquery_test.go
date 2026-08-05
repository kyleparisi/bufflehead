package db

import (
	"math/big"
	"testing"
	"time"
)

func TestBQPaged(t *testing.T) {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bqPaged(tt.sql, tt.offset, tt.limit); got != tt.want {
				t.Errorf("bqPaged() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsReadOnlyQuery(t *testing.T) {
	readOnly := []string{
		"SELECT 1",
		"  select * from t",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"EXPLAIN SELECT 1",
		"(SELECT 1 UNION ALL SELECT 2)",
		"-- a comment\nSELECT 1",
		"/* block */ SELECT 1",
		"\n\t SELECT 1",
	}
	for _, sql := range readOnly {
		if !isReadOnlyQuery(sql) {
			t.Errorf("isReadOnlyQuery(%q) = false, want true", sql)
		}
	}

	writes := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET x = 1",
		"DELETE FROM t",
		"MERGE t USING s ON t.id = s.id",
		"CREATE TABLE t AS SELECT 1",
		"DROP TABLE t",
		"TRUNCATE TABLE t",
		"EXPORT DATA OPTIONS() AS SELECT 1",
		"",
		"-- only a comment",
	}
	for _, sql := range writes {
		if isReadOnlyQuery(sql) {
			t.Errorf("isReadOnlyQuery(%q) = true, want false", sql)
		}
	}
}

func TestBQFormatValue(t *testing.T) {
	ts := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hi", "hi"},
		{"bytes", []byte("abc"), "abc"},
		{"time", ts, "2026-07-31T12:00:00Z"},
		{"numeric", big.NewRat(5, 2), "2.500000000"},
		{"int", int64(42), "42"},
		{"bool", true, "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bqFormatValue(tt.in); got != tt.want {
				t.Errorf("bqFormatValue(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestQuoteBigQueryName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"dataset.table", "`dataset`.`table`"},
		{"proj.dataset.table", "`proj`.`dataset`.`table`"},
		{"table", "`table`"},
		{"weird`name", "`weird\\`name`"},
	}
	for _, tt := range tests {
		if got := QuoteBigQueryName(tt.in); got != tt.want {
			t.Errorf("QuoteBigQueryName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitDatasetTable(t *testing.T) {
	b := &BigQueryDB{defaultDataset: "analytics"}
	tests := []struct {
		in, wantDS, wantTbl string
	}{
		{"sales.orders", "sales", "orders"},
		{"orders", "analytics", "orders"},
	}
	for _, tt := range tests {
		ds, tbl := b.splitDatasetTable(tt.in)
		if ds != tt.wantDS || tbl != tt.wantTbl {
			t.Errorf("splitDatasetTable(%q) = (%q, %q), want (%q, %q)", tt.in, ds, tbl, tt.wantDS, tt.wantTbl)
		}
	}
}
