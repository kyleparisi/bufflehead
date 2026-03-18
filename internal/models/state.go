package models

import (
	"bufflehead/internal/db"
	"strings"
)

// SortDirection for column sorting.
type SortDirection int

const (
	SortNone SortDirection = iota
	SortAsc
	SortDesc
)

// AppState is the central shared state passed between UI components.
type AppState struct {
	FilePath   string
	UserSQL    string // The query the user writes/sees
	Schema     []db.Column
	Result     *db.QueryResult
	Metadata   map[string]string
	Error      string
	Loading    bool

	// Virtual query params — applied on top of UserSQL
	PageOffset int
	PageSize   int
	SortColumn string
	SortDir    SortDirection

	// Column selection (nil or empty = all columns)
	SelectedCols []string

	// Database mode (for .duckdb files)
	IsDatabase  bool
	ActiveTable string

	// Navigation stack (back/forward)
	navStack []NavEntry
	navPos   int
}

// NavEntry stores a snapshot for back/forward navigation.
type NavEntry struct {
	SQL        string
	SortColumn string
	SortDir    SortDirection
	PageOffset int
}

// NewAppState returns defaults.
func NewAppState() *AppState {
	return &AppState{
		PageSize: 100,
		navPos:   -1,
	}
}

// NavPush pushes a SELECT query onto the navigation stack.
// Non-SELECT queries are ignored.
func (s *AppState) NavPush(sql string) {
	if !isSelectQuery(sql) {
		return
	}
	entry := NavEntry{
		SQL:        sql,
		SortColumn: s.SortColumn,
		SortDir:    s.SortDir,
		PageOffset: s.PageOffset,
	}
	// Truncate forward history
	if s.navPos < len(s.navStack)-1 {
		s.navStack = s.navStack[:s.navPos+1]
	}
	s.navStack = append(s.navStack, entry)
	s.navPos = len(s.navStack) - 1
	// Cap at 100 entries
	if len(s.navStack) > 100 {
		s.navStack = s.navStack[len(s.navStack)-100:]
		s.navPos = len(s.navStack) - 1
	}
}

// NavBack moves back in navigation history. Returns the entry and true, or false if at start.
func (s *AppState) NavBack() (NavEntry, bool) {
	if s.navPos <= 0 {
		return NavEntry{}, false
	}
	s.navPos--
	return s.navStack[s.navPos], true
}

// NavForward moves forward in navigation history. Returns the entry and true, or false if at end.
func (s *AppState) NavForward() (NavEntry, bool) {
	if s.navPos >= len(s.navStack)-1 {
		return NavEntry{}, false
	}
	s.navPos++
	return s.navStack[s.navPos], true
}

// CanNavBack returns true if back navigation is possible.
func (s *AppState) CanNavBack() bool {
	return s.navPos > 0
}

// CanNavForward returns true if forward navigation is possible.
func (s *AppState) CanNavForward() bool {
	return s.navPos < len(s.navStack)-1
}

func isSelectQuery(sql string) bool {
	// Trim and check if it starts with SELECT (case-insensitive)
	trimmed := sql
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '\n' || trimmed[0] == '\r') {
		trimmed = trimmed[1:]
	}
	if len(trimmed) < 6 {
		return false
	}
	prefix := trimmed[:6]
	return prefix == "SELECT" || prefix == "select" || prefix == "Select"
}

// ResolveDetailValue returns the display value for a column when one or more rows
// are selected. For a single row, it returns the column value directly. For multiple
// rows, it returns the value if all rows agree, or "—" if they differ.
func ResolveDetailValue(col int, singleRow []string, multiRows [][]string) string {
	if multiRows == nil {
		if col < len(singleRow) {
			return singleRow[col]
		}
		return ""
	}
	if len(multiRows) == 0 {
		return ""
	}
	first := ""
	if col < len(multiRows[0]) {
		first = multiRows[0][col]
	}
	for _, row := range multiRows[1:] {
		v := ""
		if col < len(row) {
			v = row[col]
		}
		if v != first {
			return "—"
		}
	}
	return first
}

// VirtualSQL wraps the user's query with sorting and pagination.
func (s *AppState) VirtualSQL() string {
	cols := "*"
	if len(s.SelectedCols) > 0 && len(s.SelectedCols) < len(s.Schema) {
		quoted := make([]string, len(s.SelectedCols))
		for i, c := range s.SelectedCols {
			quoted[i] = "\"" + c + "\""
		}
		cols = strings.Join(quoted, ", ")
	}
	q := "SELECT " + cols + " FROM (" + s.UserSQL + ") _q"
	if s.SortColumn != "" && s.SortDir != SortNone {
		dir := "ASC"
		if s.SortDir == SortDesc {
			dir = "DESC"
		}
		q += " ORDER BY \"" + s.SortColumn + "\" " + dir
	}
	return q
}
