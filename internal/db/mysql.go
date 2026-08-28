package db

import (
	"context"
	"database/sql"
	"fmt"
	"os/user"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// MySQLDB wraps a MySQL connection pool. It mirrors PostgresDB: the session is
// forced read-only, the pool is pinned to a single connection, and schema
// introspection goes through information_schema.
type MySQLDB struct {
	conn   *sql.DB
	dbName string
}

// mysqlSystemSchemas are the built-in databases hidden from the table list and
// flagged as system entries in the database switcher.
var mysqlSystemSchemas = map[string]bool{
	"information_schema": true,
	"mysql":              true,
	"performance_schema": true,
	"sys":                true,
}

// NewMySQLDirect connects directly to a reachable MySQL host with an explicit
// TLS mode ("false", "true", "skip-verify", or "preferred"). Used for the direct
// MySQL connection type (local, VPN, or public endpoints). The session is set
// read-only for safety, matching all other Bufflehead connections.
func NewMySQLDirect(host string, port int, dbName, dbUser, password, tlsMode string) (*MySQLDB, error) {
	if tlsMode == "" {
		tlsMode = "preferred"
	}

	cfg := mysql.NewConfig()
	cfg.User = dbUser
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = dbName
	cfg.TLSConfig = tlsMode
	cfg.InterpolateParams = true
	cfg.Timeout = 30 * time.Second
	// transaction_read_only is a real MySQL session system variable; passing it
	// as a DSN param means the driver re-applies "SET transaction_read_only=ON"
	// on every new connection, so read-only survives a pool reconnect (unlike a
	// one-shot SET after Open). This is the primary write guardrail.
	cfg.Params = map[string]string{"transaction_read_only": "ON"}
	// Identify the client to the server via a connection attribute (MySQL has no
	// settable "application name" system variable like Postgres).
	if u, err := user.Current(); err == nil {
		cfg.ConnectionAttributes = "program_name:" + u.Username
	}

	conn, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	// Pin to one connection to match the Postgres backend: keeps session state
	// (read-only) consistent and avoids interleaved streams over tunnels.
	conn.SetMaxOpenConns(1)
	conn.SetConnMaxIdleTime(3 * time.Minute)
	conn.SetConnMaxLifetime(5 * time.Minute)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &MySQLDB{conn: conn, dbName: dbName}, nil
}

// Ping verifies the connection is alive.
func (m *MySQLDB) Ping(ctx context.Context) error {
	return m.conn.PingContext(ctx)
}

// ResetPool forces the pool to close all idle connections so the next operation
// opens a fresh one. Mirrors PostgresDB.ResetPool.
func (m *MySQLDB) ResetPool() {
	m.conn.SetMaxIdleConns(0)
	m.conn.SetMaxIdleConns(1)
}

// Close releases the connection pool.
func (m *MySQLDB) Close() error {
	return m.conn.Close()
}

// DBName returns the database this connection is bound to.
func (m *MySQLDB) DBName() string {
	return m.dbName
}

// Tables lists tables and views in the connected database. Names are bare (no
// schema qualifier) because a MySQL connection is bound to one database; the
// database switcher reconnects to change databases.
func (m *MySQLDB) Tables() ([]TableInfo, error) {
	rows, err := m.conn.Query(`
		SELECT table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = ?
		ORDER BY table_type, table_name`, m.dbName)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		var tableType string
		if err := rows.Scan(&t.Name, &tableType); err != nil {
			return nil, err
		}
		switch tableType {
		case "BASE TABLE":
			t.Type = "table"
		case "VIEW":
			t.Type = "view"
		default:
			t.Type = tableType
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// TableSchema returns column info for a table in the connected database.
func (m *MySQLDB) TableSchema(tableName string) ([]Column, error) {
	rows, err := m.conn.Query(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = ? AND table_name = ?
		ORDER BY ordinal_position`, m.dbName, tableName)
	if err != nil {
		return nil, fmt.Errorf("table schema: %w", err)
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		var nullable string
		if err := rows.Scan(&c.Name, &c.DataType, &nullable); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// AllTableSchemas fetches column info for all tables in the connected database
// in a single query, avoiding N+1 round trips. Retries once on failure to match
// the Postgres backend's resilience over flaky links.
func (m *MySQLDB) AllTableSchemas(tables []TableInfo) error {
	if len(tables) == 0 {
		return nil
	}

	err := m.allTableSchemas(tables)
	if err == nil {
		return nil
	}

	m.conn.SetMaxIdleConns(0)
	m.conn.SetMaxIdleConns(1)
	return m.allTableSchemas(tables)
}

func (m *MySQLDB) allTableSchemas(tables []TableInfo) error {
	rows, err := m.conn.Query(`
		SELECT table_name, column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = ?
		ORDER BY table_name, ordinal_position`, m.dbName)
	if err != nil {
		return fmt.Errorf("all table schemas: %w", err)
	}
	defer rows.Close()

	colsByTable := make(map[string][]Column)
	for rows.Next() {
		var tableName, colName, dataType, nullable string
		if err := rows.Scan(&tableName, &colName, &dataType, &nullable); err != nil {
			return err
		}
		colsByTable[tableName] = append(colsByTable[tableName], Column{
			Name:     colName,
			DataType: dataType,
			Nullable: nullable == "YES",
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range tables {
		tables[i].Columns = colsByTable[tables[i].Name]
	}
	return nil
}

// Databases lists databases (schemas) on the server for the switcher UI.
func (m *MySQLDB) Databases() ([]DatabaseInfo, error) {
	rows, err := m.conn.Query(`
		SELECT schema_name
		FROM information_schema.schemata
		ORDER BY schema_name`)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()

	var out []DatabaseInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, DatabaseInfo{Name: name, IsSystem: mysqlSystemSchemas[name]})
	}
	return out, rows.Err()
}

// Query runs a paginated query. Same approach as PostgresDB.Query: wrap with
// COUNT(*) for the total, then LIMIT/OFFSET for the page.
func (m *MySQLDB) Query(ctx context.Context, virtualSQL string, offset, limit int) (*QueryResult, error) {
	virtualSQL = trimSQL(virtualSQL)
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM (%s) _c", virtualSQL)
	var total int64
	if err := m.conn.QueryRowContext(ctx, countQ).Scan(&total); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	pagedQ := paginate(virtualSQL, offset, limit)
	rows, err := m.conn.QueryContext(ctx, pagedQ)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &QueryResult{
		Columns: colNames,
		Total:   total,
	}

	for rows.Next() {
		if len(result.Rows) >= maxResultRows {
			break // hard row ceiling; total still reflects the true count
		}
		vals := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, len(colNames))
		for i, v := range vals {
			row[i] = formatMySQLValue(v)
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

// formatMySQLValue renders a scanned value as a display string. Unlike the
// shared formatValue, it never applies the 16-byte UUID heuristic: the MySQL
// driver returns text/blob columns as []byte, and a 16-character string must
// not be mangled into UUID form.
func formatMySQLValue(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// QuoteMySQLName backtick-quotes each dot-separated segment of an identifier so
// a name like `mydb.orders` becomes `` `mydb`.`orders` ``. Embedded backticks
// are escaped by doubling.
func QuoteMySQLName(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = "`" + strings.ReplaceAll(p, "`", "``") + "`"
	}
	return strings.Join(parts, ".")
}

// Verify MySQLDB implements the shared interfaces at compile time.
var (
	_ Querier          = (*MySQLDB)(nil)
	_ PoolResetter     = (*MySQLDB)(nil)
	_ DatabaseSwitcher = (*MySQLDB)(nil)
)
