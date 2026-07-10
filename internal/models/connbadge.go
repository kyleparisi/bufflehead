package models

import "strings"

// ConnBadge derives a short (≤3 char) rail badge from a connection name:
// "mem" for the in-memory DuckDB, otherwise up-to-two uppercase initials taken
// from the name's words (e.g. "analytics_warehouse" → "AW", "prod" → "PR").
func ConnBadge(name string) string {
	n := strings.TrimSpace(name)
	switch strings.ToLower(n) {
	case "", "memory", ":memory:", "mem", "in-memory":
		return "mem"
	}
	// Drop a file extension so "sales.parquet" → "sales".
	if i := strings.LastIndexByte(n, '.'); i > 0 {
		n = n[:i]
	}
	parts := strings.FieldsFunc(n, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '.' || r == '/'
	})
	switch len(parts) {
	case 0:
		return strings.ToUpper(firstN(n, 2))
	case 1:
		return strings.ToUpper(firstN(parts[0], 2))
	default:
		return strings.ToUpper(firstN(parts[0], 1) + firstN(parts[1], 1))
	}
}

// firstN returns the first n bytes of s (fewer if s is shorter).
func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
