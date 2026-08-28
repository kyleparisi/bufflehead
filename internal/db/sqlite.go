package db

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"database/sql"

	_ "modernc.org/sqlite"
)

// SQLiteDB wraps a SQLite database file opened read-only via the pure-Go
// modernc.org/sqlite driver (no cgo). It mirrors the Postgres/MySQL backends:
// the connection is read-only, the pool is pinned to a single connection, and
// schema introspection goes through SQLite's own catalog (sqlite_master +
// pragma_table_info) rather than a DuckDB extension.
type SQLiteDB struct {
	conn *sql.DB
	path string
}

// OpenSQLiteNative opens a SQLite database file read-only. mode=ro opens the
// file without write access at the OS level, and query_only=1 rejects writes at
// the SQL level as a second layer — matching the read-only guarantee of every
// other Bufflehead backend.
func OpenSQLiteNative(path string) (*SQLiteDB, error) {
	conn, err := sql.Open("sqlite", sqliteReadOnlyDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A SQLite file is single-writer; pinning to one connection keeps behavior
	// predictable and matches the other backends.
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	return &SQLiteDB{conn: conn, path: path}, nil
}

// sqliteReadOnlyDSN builds a read-only SQLite DSN as a file: URI. mode=ro opens
// the file without OS-level write access; query_only=1 rejects writes at the SQL
// level as a second layer.
//
// The path must become a valid file URI on every platform. On Windows an
// absolute path is like `C:\dir\db`; SQLite's URI parser needs forward slashes
// and a leading slash before the drive letter (`file:///C:/dir/db`), otherwise
// it reads `C:` as a URI authority and fails. Backslashes are converted and a
// leading slash added; on Unix the path already starts with `/` and has no
// backslashes, so it is unchanged. Separator handling is done with plain string
// ops (not path/filepath) so it is identical regardless of the host OS.
func sqliteReadOnlyDSN(path string) string {
	p := strings.ReplaceAll(path, `\`, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // C:/dir/db  →  /C:/dir/db  →  file:///C:/dir/db
	}
	u := url.URL{Scheme: "file", Path: p}
	q := url.Values{}
	q.Set("mode", "ro")
	q.Set("_pragma", "query_only(1)")
	u.RawQuery = q.Encode()
	return u.String()
}

// Ping verifies the connection is alive.
func (s *SQLiteDB) Ping(ctx context.Context) error {
	return s.conn.PingContext(ctx)
}

// Close releases the connection.
func (s *SQLiteDB) Close() error {
	return s.conn.Close()
}

// Tables lists the tables and views in the database, hiding SQLite's internal
// objects (sqlite_*). Names are bare — a SQLite file is a single database.
func (s *SQLiteDB) Tables() ([]TableInfo, error) {
	rows, err := s.conn.Query(`
		SELECT name, type
		FROM sqlite_master
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		// sqlite_master.type is already 'table' or 'view', matching TableInfo.Type.
		if err := rows.Scan(&t.Name, &t.Type); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// TableSchema returns column info for a table via the pragma_table_info
// table-valued function, which (unlike a bare PRAGMA) accepts a bound argument.
func (s *SQLiteDB) TableSchema(tableName string) ([]Column, error) {
	rows, err := s.conn.Query(`
		SELECT name, type, "notnull"
		FROM pragma_table_info(?)`, tableName)
	if err != nil {
		return nil, fmt.Errorf("table schema: %w", err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		var notNull int
		if err := rows.Scan(&c.Name, &c.DataType, &notNull); err != nil {
			return nil, err
		}
		c.Nullable = notNull == 0
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// AllTableSchemas fetches column info for every table in one query by joining
// sqlite_master against pragma_table_info, avoiding N+1 PRAGMA round trips.
func (s *SQLiteDB) AllTableSchemas(tables []TableInfo) error {
	if len(tables) == 0 {
		return nil
	}

	rows, err := s.conn.Query(`
		SELECT m.name, p.name, p.type, p."notnull"
		FROM sqlite_master m
		JOIN pragma_table_info(m.name) p
		WHERE m.type IN ('table', 'view') AND m.name NOT LIKE 'sqlite_%'
		ORDER BY m.name, p.cid`)
	if err != nil {
		return fmt.Errorf("all table schemas: %w", err)
	}
	defer rows.Close()

	colsByTable := make(map[string][]Column)
	for rows.Next() {
		var tbl, colName, dataType string
		var notNull int
		if err := rows.Scan(&tbl, &colName, &dataType, &notNull); err != nil {
			return err
		}
		colsByTable[tbl] = append(colsByTable[tbl], Column{
			Name:     colName,
			DataType: dataType,
			Nullable: notNull == 0,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range tables {
		tables[i].Columns = colsByTable[tables[i].Name]
	}
	return nil
}

// Query runs a paginated query. Same approach as the other SQL backends: wrap
// with COUNT(*) for the total, then apply the shared paging (which respects a
// user-supplied LIMIT), and cap rows at maxResultRows.
func (s *SQLiteDB) Query(ctx context.Context, virtualSQL string, offset, limit int) (*QueryResult, error) {
	virtualSQL = trimSQL(virtualSQL)
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM (%s) _c", virtualSQL)
	var total int64
	if err := s.conn.QueryRowContext(ctx, countQ).Scan(&total); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	pagedQ := paginate(virtualSQL, offset, limit)
	rows, err := s.conn.QueryContext(ctx, pagedQ)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &QueryResult{
		Columns: colNames,
		Total:   total,
	}

	for rows.Next() {
		if len(result.Rows) >= maxResultRows {
			break // hard row ceiling; total still reflects the true count
		}
		vals := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(colNames))
		for i, v := range vals {
			row[i] = formatSQLiteValue(v)
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

// formatSQLiteValue renders a scanned value as display text. Like the MySQL
// formatter it treats []byte (SQLite BLOB/TEXT) as a plain string and never
// applies the DuckDB 16-byte→UUID heuristic.
func formatSQLiteValue(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// Verify SQLiteDB implements Querier at compile time.
var _ Querier = (*SQLiteDB)(nil)
