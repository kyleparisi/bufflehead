package db

// Querier is the interface implemented by both DuckDB and Postgres backends.
// The UI layer operates on this interface so it doesn't need to know which
// backend is active.
type Querier interface {
	Tables() ([]TableInfo, error)
	TableSchema(name string) ([]Column, error)
	Query(sql string, offset, limit int) (*QueryResult, error)
	Close() error
}
