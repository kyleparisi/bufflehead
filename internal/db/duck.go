package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/marcboeker/go-duckdb"
)

// DB wraps a DuckDB in-memory connection.
type DB struct {
	conn *sql.DB
}

// Column describes a single column in a parquet file.
type Column struct {
	Name     string
	DataType string
	Nullable bool
}

// QueryResult holds the columns and rows from a query.
type QueryResult struct {
	Columns []string
	Rows    [][]string
	Total   int64
}

// TableInfo describes a table or view in a database.
type TableInfo struct {
	Name    string
	Type    string // "table" or "view"
	Columns []Column
}

// New opens an in-memory DuckDB instance.
func New() (*DB, error) {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	return &DB{conn: conn}, nil
}

// OpenDB opens a DuckDB database file (read-only).
func OpenDB(path string) (*DB, error) {
	conn, err := sql.Open("duckdb", path+"?access_mode=read_only")
	if err != nil {
		return nil, fmt.Errorf("open duckdb db: %w", err)
	}
	return &DB{conn: conn}, nil
}

// Ping verifies the connection is alive.
func (d *DB) Ping(ctx context.Context) error {
	return d.conn.PingContext(ctx)
}

// Close releases the connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// Tables lists all tables and views in the database.
func (d *DB) Tables() ([]TableInfo, error) {
	rows, err := d.conn.Query(`
		SELECT table_name, table_type 
		FROM information_schema.tables 
		WHERE table_schema = 'main'
		ORDER BY table_type, table_name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Type); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// TableSchema returns column info for a table.
func (d *DB) TableSchema(tableName string) ([]Column, error) {
	rows, err := d.conn.Query(fmt.Sprintf(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'main' AND table_name = '%s'
		ORDER BY ordinal_position`, tableName))
	if err != nil {
		return nil, fmt.Errorf("table schema: %w", err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		var nullable string
		if err := rows.Scan(&c.Name, &c.DataType, &nullable); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// Schema returns column info for a parquet file.
func (d *DB) Schema(path string) ([]Column, error) {
	q := fmt.Sprintf(`DESCRIBE SELECT * FROM '%s'`, path)

	rows, err := d.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("schema query: %w", err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		var colType, isNull string
		var key, def, extra sql.NullString
		if err := rows.Scan(&c.Name, &colType, &isNull, &key, &def, &extra); err != nil {
			return nil, err
		}
		c.DataType = colType
		c.Nullable = isNull == "YES"
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// Query runs a virtual SQL query (already wrapped with ORDER BY if needed).
// offset/limit drive pagination.
func (d *DB) Query(virtualSQL string, offset, limit int) (*QueryResult, error) {
	// Count total rows.
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM (%s) _c", virtualSQL)
	var total int64
	if err := d.conn.QueryRow(countQ).Scan(&total); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	pagedQ := fmt.Sprintf("%s LIMIT %d OFFSET %d", virtualSQL, limit, offset)
	rows, err := d.conn.Query(pagedQ)
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
			row[i] = formatValue(v)
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

// formatValue converts a scanned database value to its display string.
func formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case []byte:
		if len(val) == 16 {
			// Format as UUID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
			h := hex.EncodeToString(val)
			return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
		}
		return string(val)
	case duckdb.Decimal:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// Metadata returns parquet file-level metadata.
func (d *DB) Metadata(path string) (map[string]string, error) {
	q := fmt.Sprintf(`SELECT * FROM parquet_metadata('%s') LIMIT 1`, path)
	rows, err := d.conn.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	meta := make(map[string]string)

	if rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, col := range cols {
			meta[col] = fmt.Sprintf("%v", vals[i])
		}
	}
	return meta, nil
}

// Verify DB implements Querier at compile time.
var _ Querier = (*DB)(nil)

// DefaultQuery builds a simple SELECT * for a given parquet path.
func DefaultQuery(path string) string {
	return fmt.Sprintf("SELECT * FROM '%s'", path)
}
