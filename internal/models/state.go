package models

import "parquet-viewer/internal/db"

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
}

// NewAppState returns defaults.
func NewAppState() *AppState {
	return &AppState{
		PageSize: 100,
	}
}

// VirtualSQL wraps the user's query with sorting and pagination.
func (s *AppState) VirtualSQL() string {
	q := "SELECT * FROM (" + s.UserSQL + ") _q"
	if s.SortColumn != "" && s.SortDir != SortNone {
		dir := "ASC"
		if s.SortDir == SortDesc {
			dir = "DESC"
		}
		q += " ORDER BY \"" + s.SortColumn + "\" " + dir
	}
	return q
}
