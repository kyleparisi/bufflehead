package db

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New()
	if err != nil {
		t.Fatalf("failed to create DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// createTestParquet creates a small parquet file via DuckDB and returns the path.
func createTestParquet(t *testing.T, db *DB) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.parquet")

	_, err := db.conn.Exec(`
		COPY (
			SELECT 1 AS id, 'alice' AS name, 30 AS age
			UNION ALL
			SELECT 2, 'bob', 25
			UNION ALL
			SELECT 3, NULL, 40
		) TO '` + path + `' (FORMAT PARQUET)
	`)
	if err != nil {
		t.Fatalf("failed to create test parquet: %v", err)
	}
	return path
}

func TestNew(t *testing.T) {
	db := setupTestDB(t)
	if db == nil {
		t.Fatal("expected non-nil DB")
	}
}

func TestSchema(t *testing.T) {
	db := setupTestDB(t)
	path := createTestParquet(t, db)

	cols, err := db.Schema(path)
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}

	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}

	expected := []struct {
		name     string
		dataType string
	}{
		{"id", "INTEGER"},
		{"name", "VARCHAR"},
		{"age", "INTEGER"},
	}

	for i, e := range expected {
		if cols[i].Name != e.name {
			t.Errorf("col[%d].Name = %q, want %q", i, cols[i].Name, e.name)
		}
		if cols[i].DataType != e.dataType {
			t.Errorf("col[%d].DataType = %q, want %q", i, cols[i].DataType, e.dataType)
		}
	}
}

func TestQuery(t *testing.T) {
	db := setupTestDB(t)
	path := createTestParquet(t, db)

	result, err := db.Query(DefaultQuery(path), 0, 100)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}

	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if len(result.Rows) != 3 {
		t.Errorf("len(Rows) = %d, want 3", len(result.Rows))
	}
	if len(result.Columns) != 3 {
		t.Errorf("len(Columns) = %d, want 3", len(result.Columns))
	}

	// Check first row
	if result.Rows[0][0] != "1" {
		t.Errorf("row[0][0] = %q, want \"1\"", result.Rows[0][0])
	}
	if result.Rows[0][1] != "alice" {
		t.Errorf("row[0][1] = %q, want \"alice\"", result.Rows[0][1])
	}
}

func TestQueryPagination(t *testing.T) {
	db := setupTestDB(t)
	path := createTestParquet(t, db)

	// Page size 2, offset 0
	result, err := db.Query(DefaultQuery(path), 0, 2)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if len(result.Rows) != 2 {
		t.Errorf("len(Rows) = %d, want 2", len(result.Rows))
	}

	// Page size 2, offset 2
	result, err = db.Query(DefaultQuery(path), 2, 2)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Errorf("len(Rows) = %d, want 1", len(result.Rows))
	}
}

func TestQueryCustomSQL(t *testing.T) {
	db := setupTestDB(t)
	path := createTestParquet(t, db)

	sql := "SELECT name, age FROM '" + path + "' WHERE age > 26"
	result, err := db.Query(sql, 0, 100)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}

	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	if len(result.Columns) != 2 {
		t.Errorf("len(Columns) = %d, want 2", len(result.Columns))
	}
}

func TestQueryBadSQL(t *testing.T) {
	db := setupTestDB(t)

	_, err := db.Query("SELECT * FROM nonexistent_table", 0, 100)
	if err == nil {
		t.Error("expected error for bad SQL, got nil")
	}
}

func TestMetadata(t *testing.T) {
	db := setupTestDB(t)
	path := createTestParquet(t, db)

	meta, err := db.Metadata(path)
	if err != nil {
		t.Fatalf("Metadata() error: %v", err)
	}
	if len(meta) == 0 {
		t.Error("expected non-empty metadata")
	}
}

func TestSchemaFileNotFound(t *testing.T) {
	db := setupTestDB(t)
	_, err := db.Schema("/nonexistent/file.parquet")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestDefaultQuery(t *testing.T) {
	q := DefaultQuery("/tmp/test.parquet")
	if q != "SELECT * FROM '/tmp/test.parquet'" {
		t.Errorf("DefaultQuery = %q, unexpected", q)
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"uuid bytes", []byte{0x69, 0xb8, 0x4c, 0xa5, 0x93, 0x2b, 0x48, 0xa5, 0x85, 0xa2, 0xf5, 0x62, 0xad, 0xbf, 0xe3, 0x73}, "69b84ca5-932b-48a5-85a2-f562adbfe373"},
		{"zero uuid", make([]byte, 16), "00000000-0000-0000-0000-000000000000"},
		{"text bytes", []byte("hello"), "hello"},
		{"empty bytes", []byte{}, ""},
		{"int", 42, "42"},
		{"string", "foo", "foo"},
		{"nil", nil, "<nil>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.in)
			if got != tt.want {
				t.Errorf("formatValue(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestQueryUUID(t *testing.T) {
	db := setupTestDB(t)

	result, err := db.Query("SELECT uuid() AS id", 0, 1)
	if err != nil {
		t.Fatalf("Query() error: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	id := result.Rows[0][0]
	// UUID format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx (36 chars)
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Errorf("expected UUID format, got %q", id)
	}
}

func TestSchemaNotParquet(t *testing.T) {
	db := setupTestDB(t)

	// Create a non-parquet file
	dir := t.TempDir()
	path := filepath.Join(dir, "notparquet.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	_, err := db.Schema(path)
	if err == nil {
		t.Error("expected error for non-parquet file, got nil")
	}
}
