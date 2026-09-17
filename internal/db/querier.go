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

// LocalQuerier is implemented by backends that read from this machine — DuckDB
// (in-memory, a database file, or a dropped folder's files) and SQLite. Nothing
// between the app and the data can go away mid-session, so the UI skips the
// liveness checks and connect timeouts that exist for tunnelled connections.
type LocalQuerier interface {
	IsLocal() bool
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

// Remote backends deliberately do NOT implement LocalQuerier: Postgres and
// MySQL reach a host (often through an SSM tunnel that can drop), and BigQuery
// is a network service, so both keep the liveness check and connect deadlines.
var (
	_ Querier = (*PostgresDB)(nil)
	_ Querier = (*MySQLDB)(nil)
	_ Querier = (*BigQueryDB)(nil)
)
