package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// seedSQLite creates a SQLite file with a table, a view, and some rows, using a
// read-write connection, then closes it. Returns the file path.
func seedSQLite(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")

	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	defer rw.Close()

	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note TEXT)`,
		`INSERT INTO users (id, name, note) VALUES (1, 'alice', 'hi'), (2, 'bob', NULL), (3, 'carol', 'yo')`,
		`CREATE VIEW active AS SELECT id, name FROM users WHERE note IS NOT NULL`,
	}
	for _, s := range stmts {
		if _, err := rw.Exec(s); err != nil {
			t.Fatalf("seed exec %q: %v", s, err)
		}
	}
	return path
}

func TestSQLiteTablesAndSchema(t *testing.T) {
	db, err := OpenSQLiteNative(seedSQLite(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("tables: %v", err)
	}
	got := map[string]string{}
	for _, tbl := range tables {
		got[tbl.Name] = tbl.Type
	}
	if got["users"] != "table" || got["active"] != "view" {
		t.Fatalf("unexpected tables: %+v", got)
	}
	if _, ok := got["sqlite_sequence"]; ok {
		t.Errorf("internal sqlite_ table should be hidden")
	}

	cols, err := db.TableSchema("users")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("want 3 columns, got %d (%+v)", len(cols), cols)
	}
	// name is NOT NULL; note is nullable.
	byName := map[string]Column{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if byName["name"].Nullable {
		t.Errorf("name should be NOT NULL")
	}
	if !byName["note"].Nullable {
		t.Errorf("note should be nullable")
	}

	// Bulk schema should match per-table schema.
	if err := db.AllTableSchemas(tables); err != nil {
		t.Fatalf("all schemas: %v", err)
	}
	for _, tbl := range tables {
		if tbl.Name == "users" && len(tbl.Columns) != 3 {
			t.Errorf("bulk users columns = %d, want 3", len(tbl.Columns))
		}
	}
}

func TestSQLiteQuery(t *testing.T) {
	db, err := OpenSQLiteNative(seedSQLite(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// NULL renders as "", total reflects the full count.
	res, err := db.Query(context.Background(), "SELECT id, name, note FROM users ORDER BY id", 0, 100)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if res.Total != 3 {
		t.Errorf("total = %d, want 3", res.Total)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(res.Rows))
	}
	if res.Rows[1][2] != "" {
		t.Errorf("NULL note should render as empty string, got %q", res.Rows[1][2])
	}
	if res.Rows[0][1] != "alice" {
		t.Errorf("row 0 name = %q, want alice", res.Rows[0][1])
	}

	// A user-supplied LIMIT is respected (paging fix), not double-appended.
	res, err = db.Query(context.Background(), "SELECT id FROM users ORDER BY id LIMIT 2", 0, 100)
	if err != nil {
		t.Fatalf("query with limit: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Errorf("LIMIT 2 returned %d rows", len(res.Rows))
	}
}

// TestSQLiteValueRendering checks that each SQLite storage class renders as
// expected, and — the key difference from the old DuckDB-extension path — that a
// 16-byte BLOB is NOT mangled into UUID form.
func TestSQLiteValueRendering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "types.db")
	rw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open rw: %v", err)
	}
	if _, err := rw.Exec(`CREATE TABLE v (i INTEGER, r REAL, t TEXT, b BLOB, n TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	// A 16-char blob is the case DuckDB's formatter would turn into a UUID.
	if _, err := rw.Exec(`INSERT INTO v VALUES (42, 3.5, 'hello', CAST('0123456789abcdef' AS BLOB), NULL)`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	rw.Close()

	db, err := OpenSQLiteNative(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	res, err := db.Query(context.Background(), "SELECT i, r, t, b, n FROM v", 0, 100)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(res.Rows))
	}
	row := res.Rows[0]
	want := []string{"42", "3.5", "hello", "0123456789abcdef", ""}
	for i, w := range want {
		if row[i] != w {
			t.Errorf("col %d = %q, want %q", i, row[i], w)
		}
	}
}

func TestSQLiteReadOnly(t *testing.T) {
	path := seedSQLite(t)
	db, err := OpenSQLiteNative(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// A write through the read-only handle must fail. Use the raw pool so the
	// error is the driver's read-only rejection, not the COUNT(*) wrap in Query.
	if _, err := db.conn.Exec(`INSERT INTO users (id, name) VALUES (99, 'mallory')`); err == nil {
		t.Fatal("expected read-only connection to reject INSERT")
	}
}
