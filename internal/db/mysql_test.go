package db

import "testing"

func TestQuoteMySQLName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"table", "`table`"},
		{"mydb.orders", "`mydb`.`orders`"},
		{"weird`name", "`weird``name`"},
		{"a.b.c", "`a`.`b`.`c`"},
	}
	for _, tt := range tests {
		if got := QuoteMySQLName(tt.in); got != tt.want {
			t.Errorf("QuoteMySQLName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatMySQLValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string bytes", []byte("hello"), "hello"},
		// A 16-byte string must not be mangled into UUID form (unlike the
		// shared formatValue used by DuckDB).
		{"sixteen byte string", []byte("0123456789abcdef"), "0123456789abcdef"},
		{"int", int64(42), "42"},
		{"float", 3.5, "3.5"},
	}
	for _, tt := range tests {
		if got := formatMySQLValue(tt.in); got != tt.want {
			t.Errorf("%s: formatMySQLValue(%v) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
