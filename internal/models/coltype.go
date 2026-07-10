package models

import "strings"

// ColumnTypeCategory is a small visual bucket for a SQL/DuckDB data type,
// used to color type chips (schema panel, row inspector) and grid cells.
type ColumnTypeCategory string

const (
	TypeInt   ColumnTypeCategory = "int"
	TypeFloat ColumnTypeCategory = "float"
	TypeBool  ColumnTypeCategory = "bool"
	TypeTime  ColumnTypeCategory = "time"
	TypeJSON  ColumnTypeCategory = "json"
	TypeEnum  ColumnTypeCategory = "enum"
	TypeText  ColumnTypeCategory = "text"
	TypeOther ColumnTypeCategory = "other"
)

// TypeCategory classifies a DuckDB data type name. It is case-insensitive,
// ignores a trailing nullable "?" and any type parameters (e.g. DECIMAL(10,2),
// TIMESTAMP WITH TIME ZONE), and treats array/list/struct types as JSON-like.
//
// Order matters: collection and temporal checks run before the numeric checks
// because names like INTERVAL and some nested types contain "INT".
func TypeCategory(dataType string) ColumnTypeCategory {
	t := strings.ToUpper(strings.TrimSpace(dataType))
	t = strings.TrimSuffix(t, "?")
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	switch {
	case t == "":
		return TypeOther
	case strings.HasSuffix(t, "[]"):
		return TypeJSON
	case strings.HasPrefix(t, "ENUM"):
		return TypeEnum
	case containsAnyType(t, "STRUCT", "MAP", "LIST", "ARRAY", "JSON", "UNION"):
		return TypeJSON
	case containsAnyType(t, "TIMESTAMP", "DATETIME", "DATE", "TIME", "INTERVAL"):
		return TypeTime
	case strings.Contains(t, "BOOL"):
		return TypeBool
	case containsAnyType(t, "FLOAT", "DOUBLE", "DECIMAL", "NUMERIC", "REAL"):
		return TypeFloat
	case containsAnyType(t, "INT", "SERIAL"):
		return TypeInt
	case containsAnyType(t, "CHAR", "TEXT", "STRING", "VARCHAR", "UUID", "BLOB", "BYTEA", "BIT"):
		return TypeText
	default:
		return TypeOther
	}
}

func containsAnyType(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
