package db

import (
	"encoding/json"
	"strings"
)

// FromTableName parses sql with DuckDB's SQL AST (json_serialize_sql) and
// returns the name of the first plain base table (or file path) referenced in
// the top-level statement's FROM clause. It returns "" when there is no simple
// base table to name — e.g. no FROM, a subquery/CTE/derived table, a values
// clause, or a parse error — so callers can fall back to a default title.
//
// This is used to label tabs by the table/file they query. Using the real
// parser avoids the fragility of string-scanning the FROM clause (quoting,
// schemas, joins, comments, CTEs, etc.).
func FromTableName(d *DB, sql string) string {
	if d == nil || strings.TrimSpace(sql) == "" {
		return ""
	}

	// json_serialize_sql is a macro that requires a literal string argument.
	lit := "'" + strings.ReplaceAll(sql, "'", "''") + "'"
	var js string
	// Cast to VARCHAR so the driver returns a string rather than a JSON map.
	row := d.conn.QueryRow("SELECT json_serialize_sql(" + lit + ")::VARCHAR")
	if err := row.Scan(&js); err != nil {
		return ""
	}

	var parsed struct {
		Error      bool `json:"error"`
		Statements []struct {
			Node struct {
				FromTable json.RawMessage `json:"from_table"`
			} `json:"node"`
		} `json:"statements"`
	}
	if err := json.Unmarshal([]byte(js), &parsed); err != nil {
		return ""
	}
	if parsed.Error || len(parsed.Statements) == 0 {
		return ""
	}
	return firstBaseTable(parsed.Statements[0].Node.FromTable)
}

// firstBaseTable walks a from_table AST node and returns the name of the first
// BASE_TABLE it finds (schema-qualified when a schema is present), descending
// into the left side of JOINs. Returns "" for subqueries, empty FROMs, etc.
func firstBaseTable(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var node struct {
		Type       string          `json:"type"`
		TableName  string          `json:"table_name"`
		SchemaName string          `json:"schema_name"`
		Left       json.RawMessage `json:"left"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	switch node.Type {
	case "BASE_TABLE":
		if node.TableName == "" {
			return ""
		}
		if node.SchemaName != "" && node.SchemaName != "main" {
			return node.SchemaName + "." + node.TableName
		}
		return node.TableName
	case "JOIN":
		// Name the tab after the first (left-most) table in the join tree.
		return firstBaseTable(node.Left)
	default:
		// SUBQUERY, EMPTY, TABLE_FUNCTION, EXPRESSION_LIST, etc. — no plain name.
		return ""
	}
}
