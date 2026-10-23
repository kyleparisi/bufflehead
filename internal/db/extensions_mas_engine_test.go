//go:build duckdb_mas_engine

package db

import (
	"context"
	"testing"
	"time"
)

// Run only against the custom static MAS engine, never the default libraries.
func TestMASEngineRejectsExternalExtensions(t *testing.T) {
	db := setupTestDB(t)
	var version string
	if err := db.conn.QueryRow("SELECT version()").Scan(&version); err != nil || version != "v2.0.0" {
		t.Fatalf("expected release engine v2.0.0, got %q, %v", version, err)
	}
	for _, query := range []string{"LOAD httpfs", "INSTALL httpfs"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := db.conn.ExecContext(ctx, query)
		timedOut := ctx.Err() != nil
		cancel()
		if err == nil {
			t.Fatalf("restricted engine accepted %s", query)
		}
		if timedOut {
			t.Fatalf("%s timed out instead of rejecting external extension code: %v", query, err)
		}
		t.Logf("%s rejected: %v", query, err)
	}
	var got int
	if err := db.conn.QueryRow(`SELECT json_extract('{"a": 42}', '$.a')::INTEGER`).Scan(&got); err != nil || got != 42 {
		t.Fatalf("statically linked JSON unavailable after refusal: %d, %v", got, err)
	}
}
