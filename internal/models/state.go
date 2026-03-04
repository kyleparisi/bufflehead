package models

import "parquet-viewer/internal/db"

// AppState is the central shared state passed between UI components.
type AppState struct {
	FilePath    string
	CurrentSQL  string
	Schema      []db.Column
	Result      *db.QueryResult
	Metadata    map[string]string
	Error       string
	Loading     bool
	PageOffset  int
	PageSize    int // rows per page, e.g. 100
}

// NewAppState returns defaults.
func NewAppState() *AppState {
	return &AppState{
		PageSize: 100,
	}
}
