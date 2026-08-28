package db

import "context"

// Querier is the interface implemented by the DuckDB, Postgres, MySQL, and
// BigQuery backends. The UI layer operates on this interface so it doesn't need
// to know which backend is active.
type Querier interface {
	Tables() ([]TableInfo, error)
	TableSchema(name string) ([]Column, error)
	Query(ctx context.Context, sql string, offset, limit int) (*QueryResult, error)
	Ping(ctx context.Context) error
	Close() error
}

// PoolResetter is implemented by pooled SQL backends (Postgres, MySQL) that can
// drop stale connections after a tunnel reconnect or a cancelled query. The UI
// asserts on this interface instead of a concrete type so every pooled backend
// benefits.
type PoolResetter interface {
	ResetPool()
}

// DatabaseSwitcher is implemented by backends whose server hosts multiple
// databases the user can switch between (Postgres, MySQL). The database switcher
// asserts on this interface to list the available databases.
type DatabaseSwitcher interface {
	Databases() ([]DatabaseInfo, error)
}
