package completion

import (
	"strings"

	"bufflehead/internal/db"
)

// Kind mirrors CodeEdit.CodeCompletionKind for testability.
type Kind int

const (
	KindClass    Kind = 0
	KindFunction Kind = 1
	KindVariable Kind = 3
	KindConstant Kind = 6
)

// Item represents a single autocomplete suggestion.
type Item struct {
	Kind       Kind
	Display    string
	InsertText string
}

// SQLKeywords lists SQL keywords for autocomplete and syntax highlighting.
var SQLKeywords = []string{
	"SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "IS", "NULL",
	"AS", "ON", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "FULL", "CROSS",
	"GROUP", "BY", "ORDER", "ASC", "DESC", "HAVING", "LIMIT", "OFFSET",
	"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "DROP", "CREATE",
	"TABLE", "INDEX", "VIEW", "ALTER", "ADD", "COLUMN", "PRIMARY", "KEY",
	"FOREIGN", "REFERENCES", "UNIQUE", "CHECK", "DEFAULT", "CONSTRAINT",
	"UNION", "ALL", "DISTINCT", "BETWEEN", "LIKE", "ILIKE", "EXISTS",
	"CASE", "WHEN", "THEN", "ELSE", "END", "CAST", "COALESCE", "NULLIF",
	"TRUE", "FALSE", "WITH", "RECURSIVE", "EXPLAIN", "ANALYZE",
	"COUNT", "SUM", "AVG", "MIN", "MAX", "OVER", "PARTITION", "WINDOW",
	"ROW_NUMBER", "RANK", "DENSE_RANK", "FIRST_VALUE", "LAST_VALUE",
	"COPY", "FORMAT", "PARQUET", "CSV", "JSON", "READ_PARQUET", "READ_CSV",
}

// SQLFunctions lists common DuckDB/SQL functions for autocomplete.
var SQLFunctions = []string{
	"COUNT", "SUM", "AVG", "MIN", "MAX",
	"COALESCE", "NULLIF", "CAST",
	"ROW_NUMBER", "RANK", "DENSE_RANK", "FIRST_VALUE", "LAST_VALUE",
	"READ_PARQUET", "READ_CSV", "READ_JSON",
	"ABS", "CEIL", "FLOOR", "ROUND", "SQRT", "POWER", "MOD", "LN", "LOG", "LOG2",
	"LENGTH", "LOWER", "UPPER", "TRIM", "LTRIM", "RTRIM",
	"SUBSTR", "SUBSTRING", "REPLACE", "REVERSE", "REPEAT",
	"CONCAT", "CONCAT_WS", "STARTS_WITH", "ENDS_WITH", "CONTAINS",
	"LEFT", "RIGHT", "LPAD", "RPAD", "SPLIT_PART",
	"NOW", "CURRENT_DATE", "CURRENT_TIMESTAMP",
	"DATE_PART", "DATE_TRUNC", "DATE_DIFF", "DATE_ADD",
	"EXTRACT", "EPOCH", "STRFTIME", "STRPTIME",
	"ARRAY_AGG", "LIST_AGG", "STRING_AGG", "GROUP_CONCAT",
	"UNNEST", "GENERATE_SERIES",
	"IF", "IIF", "IFNULL",
	"TYPEOF", "TRY_CAST",
	"REGEXP_MATCHES", "REGEXP_REPLACE", "REGEXP_EXTRACT",
	"LIST_VALUE", "STRUCT_PACK", "MAP",
	"GREATEST", "LEAST",
	"MD5", "SHA256",
}

// Build returns filtered, deduplicated completion items for the given
// prefix, schema columns, and database tables.
func Build(prefix string, columns []db.Column, tables []db.TableInfo) []Item {
	if len(prefix) == 0 {
		return nil
	}
	prefixLower := strings.ToLower(prefix)

	var items []Item
	seen := make(map[string]bool)

	// Column names from the current schema
	for _, col := range columns {
		colLower := strings.ToLower(col.Name)
		if strings.Contains(colLower, prefixLower) {
			display := col.Name + "  " + col.DataType
			items = append(items, Item{KindVariable, display, quoteIdentifier(col.Name)})
			seen[colLower] = true
		}
	}

	// Table names (for database connections)
	for _, tbl := range tables {
		tblLower := strings.ToLower(tbl.Name)
		if !seen[tblLower] && strings.Contains(tblLower, prefixLower) {
			display := tbl.Name + "  table"
			items = append(items, Item{KindClass, display, tbl.Name})
			seen[tblLower] = true
		}
		// Also suggest columns from all tables
		for _, col := range tbl.Columns {
			colLower := strings.ToLower(col.Name)
			if seen[colLower] {
				continue
			}
			if strings.Contains(colLower, prefixLower) {
				display := col.Name + "  " + col.DataType
				items = append(items, Item{KindVariable, display, quoteIdentifier(col.Name)})
				seen[colLower] = true
			}
		}
	}

	// SQL functions (before keywords so functions get the "()" display)
	for _, fn := range SQLFunctions {
		fnLower := strings.ToLower(fn)
		if seen[fnLower] {
			continue
		}
		if strings.Contains(fnLower, prefixLower) {
			items = append(items, Item{KindFunction, fnLower + "()", fnLower + "("})
			seen[fnLower] = true
		}
	}

	// SQL keywords (UPPERCASE)
	for _, kw := range SQLKeywords {
		kwLower := strings.ToLower(kw)
		if seen[kwLower] {
			continue
		}
		if strings.Contains(kwLower, prefixLower) {
			items = append(items, Item{KindConstant, strings.ToUpper(kw), strings.ToUpper(kw) + " "})
			seen[kwLower] = true
		}
	}

	return items
}

// WordPrefixAt extracts the word being typed at the given cursor column in line.
func WordPrefixAt(line string, col int) string {
	if col <= 0 || col > len(line) {
		return ""
	}
	start := col
	for start > 0 {
		ch := line[start-1]
		if IsWordChar(ch) {
			start--
		} else {
			break
		}
	}
	return line[start:col]
}

// quoteIdentifier wraps a column name in double quotes for safe SQL usage.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// IsWordChar returns true for characters that can be part of a SQL identifier.
func IsWordChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') || ch == '_'
}
