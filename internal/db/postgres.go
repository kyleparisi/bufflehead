package db

import (
	"context"
	"database/sql"
	"fmt"
	"os/user"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	rdsauth "github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresDB wraps a Postgres connection pool.
type PostgresDB struct {
	conn   *sql.DB
	dbName string
}

// NewPostgres connects to a Postgres database via the pgx stdlib driver.
// The session is set to read-only for safety.
func NewPostgres(host string, port int, dbName, user, password string) (*PostgresDB, error) {
	return newPostgres(host, port, dbName, user, password, "disable")
}

// NewPostgresIAM connects to a Postgres database using RDS IAM authentication.
// The token is generated for rdsEndpoint (the real RDS host:port), but the
// connection is made to connectHost:connectPort (typically localhost via SSM tunnel).
// RDS requires SSL for IAM auth connections.
func NewPostgresIAM(connectHost string, connectPort int, rdsEndpoint, dbName, user string, cfg aws.Config) (*PostgresDB, error) {
	token, err := rdsauth.BuildAuthToken(context.Background(), rdsEndpoint, cfg.Region, user, cfg.Credentials)
	if err != nil {
		return nil, fmt.Errorf("build rds auth token: %w", err)
	}
	return newPostgres(connectHost, connectPort, dbName, user, token, "require")
}

func newPostgres(host string, port int, dbName, dbUser, password, sslMode string) (*PostgresDB, error) {
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		host, port, dbName, dbUser, password, sslMode)
	if u, err := user.Current(); err == nil {
		dsn += " application_name=" + u.Username
	}

	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Limit to one connection — multiple TLS connections through an SSM
	// tunnel's smux session cause MAC errors from interleaved streams.
	conn.SetMaxOpenConns(1)

	// Close idle connections before RDS drops them (default RDS idle
	// timeout is ~5 min). This lets database/sql transparently reconnect.
	conn.SetConnMaxIdleTime(3 * time.Minute)
	conn.SetConnMaxLifetime(5 * time.Minute)

	// Verify connectivity
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	// Enforce read-only at the session level
	if _, err := conn.Exec("SET default_transaction_read_only = on"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set read-only: %w", err)
	}

	return &PostgresDB{conn: conn, dbName: dbName}, nil
}

// Ping verifies the connection is alive.
func (p *PostgresDB) Ping(ctx context.Context) error {
	return p.conn.PingContext(ctx)
}

// ResetPool forces the pool to close all idle connections. The next
// operation will open a fresh TCP+TLS connection. This is needed after
// an SSM tunnel reconnect to avoid TLS session cache issues.
func (p *PostgresDB) ResetPool() {
	p.conn.SetMaxIdleConns(0)
	p.conn.SetMaxIdleConns(1)
}

// Close releases the connection pool.
func (p *PostgresDB) Close() error {
	return p.conn.Close()
}

// Tables lists tables and views from information_schema, filtered to relevant schemas.
// Excludes pg_catalog, information_schema, and other system schemas.
func (p *PostgresDB) Tables() ([]TableInfo, error) {
	rows, err := p.conn.Query(`
		SELECT table_schema || '.' || table_name, table_type
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY table_schema, table_type, table_name`)
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

// TableSchema returns column info for a table from information_schema.columns.
// The tableName may be schema-qualified (schema.table).
func (p *PostgresDB) TableSchema(tableName string) ([]Column, error) {
	schema, table := splitSchemaTable(tableName)

	rows, err := p.conn.Query(`
		SELECT column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
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

// SchemaNames returns available schema names (excluding system schemas).
func (p *PostgresDB) SchemaNames() ([]string, error) {
	rows, err := p.conn.Query(`
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name`)
	if err != nil {
		return nil, fmt.Errorf("schema names: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// Query runs a paginated query. Same approach as DB.Query():
// wraps with COUNT(*) for total, then LIMIT/OFFSET for the page.
func (p *PostgresDB) Query(ctx context.Context, virtualSQL string, offset, limit int) (*QueryResult, error) {
	// Count total rows
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM (%s) _c", virtualSQL)
	var total int64
	if err := p.conn.QueryRowContext(ctx, countQ).Scan(&total); err != nil {
		return nil, fmt.Errorf("count: %w", err)
	}

	pagedQ := fmt.Sprintf("%s LIMIT %d OFFSET %d", virtualSQL, limit, offset)
	rows, err := p.conn.QueryContext(ctx, pagedQ)
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
			row[i] = formatValue(v)
		}
		result.Rows = append(result.Rows, row)
	}
	return result, rows.Err()
}

// splitSchemaTable splits "schema.table" into its parts.
// If no dot is present, defaults to "public".
func splitSchemaTable(name string) (string, string) {
	for i := range name {
		if name[i] == '.' {
			return name[:i], name[i+1:]
		}
	}
	return "public", name
}

// AllTableSchemas fetches column info for all given tables in a single query.
// This avoids N+1 round trips over high-latency connections (e.g. SSM tunnels).
// Retries once on failure to handle transient TLS errors over SSM tunnels.
func (p *PostgresDB) AllTableSchemas(tables []TableInfo) error {
	if len(tables) == 0 {
		return nil
	}

	err := p.allTableSchemas(tables)
	if err == nil {
		return nil
	}

	// Transient TLS errors (e.g. "bad record MAC") can occur over SSM
	// tunnels during the initial data burst. Force the pool to close the
	// broken connection and retry once with a fresh one.
	p.conn.SetMaxIdleConns(0)
	p.conn.SetMaxIdleConns(1)
	return p.allTableSchemas(tables)
}

func (p *PostgresDB) allTableSchemas(tables []TableInfo) error {
	rows, err := p.conn.Query(`
		SELECT table_schema || '.' || table_name, column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY table_schema, table_name, ordinal_position`)
	if err != nil {
		return fmt.Errorf("all table schemas: %w", err)
	}
	defer rows.Close()

	// Build a map for quick lookup
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

// Verify PostgresDB implements Querier at compile time.
var _ Querier = (*PostgresDB)(nil)
