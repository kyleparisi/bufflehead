package models

import "testing"

func TestTypeCategory(t *testing.T) {
	cases := map[string]ColumnTypeCategory{
		"BIGINT":                   TypeInt,
		"INTEGER":                  TypeInt,
		"integer":                  TypeInt,
		"UINTEGER":                 TypeInt,
		"HUGEINT":                  TypeInt,
		"TINYINT":                  TypeInt,
		"BIGINT?":                  TypeInt,
		"DOUBLE":                   TypeFloat,
		"DOUBLE PRECISION":         TypeFloat,
		"FLOAT":                    TypeFloat,
		"DECIMAL(10,2)":            TypeFloat,
		"NUMERIC":                  TypeFloat,
		"BOOLEAN":                  TypeBool,
		"TIMESTAMP":                TypeTime,
		"TIMESTAMP WITH TIME ZONE": TypeTime,
		"DATE":                     TypeTime,
		"TIME":                     TypeTime,
		"INTERVAL":                 TypeTime, // contains "INT" but must be time
		"VARCHAR":                  TypeText,
		"VARCHAR(255)":             TypeText,
		"TEXT":                     TypeText,
		"UUID":                     TypeText,
		"BLOB":                     TypeText,
		"JSON":                     TypeJSON,
		`STRUCT("name" VARCHAR)`:   TypeJSON,
		"MAP(VARCHAR, INTEGER)":    TypeJSON, // MAP(...) — params stripped, base MAP
		"INTEGER[]":                TypeJSON,
		"ENUM('a','b')":            TypeEnum,
		"":                         TypeOther,
		"GEOMETRY":                 TypeOther,
	}
	for in, want := range cases {
		if got := TypeCategory(in); got != want {
			t.Errorf("TypeCategory(%q) = %q, want %q", in, got, want)
		}
	}
}
