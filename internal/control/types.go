package control

import "errors"

// ErrBusy is returned by ExecSQL when both SQL slots are taken. /sql maps it to
// HTTP 429 and the MCP run_sql tool turns it into a "retry shortly" message, so
// agents back off instead of flooding the worker queue and starving the UI.
var ErrBusy = errors.New("too many concurrent SQL requests, retry later")

// ColumnInfo mirrors db.Column as plain JSON. The control package must stay free
// of internal/db (cgo DuckDB) so the MCP bridge can link it.
type ColumnInfo struct {
	Name     string `json:"name"`
	DataType string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// TableInfo mirrors db.TableInfo: a table, view, folder glob pattern, or — for
// the in-memory connection — an open data file keyed by its path.
type TableInfo struct {
	Name    string       `json:"name"`
	Type    string       `json:"type"`             // table, view, PATTERN, file
	Detail  string       `json:"detail,omitempty"` // sidebar right-column text
	Columns []ColumnInfo `json:"columns,omitempty"`
}

// ConnectionInfo describes one open connection for GET /connections and the
// MCP list_connections / get_schema tools. Kind is one of: memory, duckdb,
// sqlite, folder, aws-postgres, postgres, mysql, bigquery.
type ConnectionInfo struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Path   string `json:"path,omitempty"` // file/folder/db path; empty for remote
	Active bool   `json:"active"`
	// Dialect is the SQL flavour to write: duckdb, sqlite, postgres, mysql, bigquery.
	Dialect string `json:"dialect"`
	// DefaultLimit is the row cap applied when a query names none (0 = none;
	// local connections return everything up to the backend ceiling).
	DefaultLimit   int         `json:"default_limit"`
	SupportsCancel bool        `json:"supports_cancel"`
	SupportsS3     bool        `json:"supports_s3"`
	Tables         []TableInfo `json:"tables"`
}

// ConnectionsData is the payload for the "connections" command.
type ConnectionsData struct {
	IncludeColumns bool `json:"include_columns"`
}
