package db

import (
	"database/sql"
	"fmt"

	_ "github.com/marcboeker/go-duckdb"
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

// New opens an in-memory DuckDB instance.
func New() (*DB, error) {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	return &DB{conn: conn}, nil
}

// Close releases the connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// Schema returns column info for a parquet file.
func (d *DB) Schema(path string) ([]Column, error) {
	q := fmt.Sprintf(`
		SELECT name, type, repetition_type
		FROM parquet_schema('%s')
		WHERE num_children IS NULL`, path)

	rows, err := d.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("schema query: %w", err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		var repType string
		if err := rows.Scan(&c.Name, &c.DataType, &repType); err != nil {
			return nil, err
		}
		c.Nullable = repType == "OPTIONAL"
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
			row[i] = fmt.Sprintf("%v", v)
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
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

// DefaultQuery builds a simple SELECT * for a given parquet path.
func DefaultQuery(path string) string {
	return fmt.Sprintf("SELECT * FROM '%s'", path)
}
