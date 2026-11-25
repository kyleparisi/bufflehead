package db

import (
	"context"
	"database/sql"
	"fmt"
)

// MailboxSnapshot returns a session-local copy of a fully synced cache. The
// GUI and independent MCP processes can keep querying while another refreshes.
func MailboxSnapshot(ctx context.Context, path string) (Querier, error) {
	d, err := New()
	if err != nil {
		return nil, err
	}
	pathLiteral := QuotePathLiteral(path)
	for _, stmt := range []string{
		"ATTACH " + pathLiteral + " AS inbox_cache (READ_ONLY)",
		"CREATE TABLE messages AS SELECT * FROM inbox_cache.messages",
		"CREATE TABLE sync_status AS SELECT * FROM inbox_cache.sync_status",
		"DETACH inbox_cache",
		"SET enable_external_access = false",
		"SET lock_configuration = true",
	} {
		if _, err := d.conn.ExecContext(ctx, stmt); err != nil {
			d.Close()
			return nil, fmt.Errorf("open inbox snapshot: %w", err)
		}
	}
	return &inboxSnapshot{d}, nil
}

// OpenMailboxWriter opens the cache for sync under the caller's bookmark lock.
func OpenMailboxWriter(path string) (*sql.DB, error) {
	c, err := sql.Open("duckdb", path)
	if err == nil {
		c.SetMaxOpenConns(1)
	}
	return c, err
}

// The MCP accepts one SELECT. Validate it before DB.Query embeds it in the
// pagination SQL. DuckDB's serializer accepts only SELECT statements.
type inboxSnapshot struct{ *DB }

func (d *inboxSnapshot) Query(ctx context.Context, query string, offset, limit int) (*QueryResult, error) {
	var valid bool
	err := d.conn.QueryRowContext(ctx, `SELECT coalesce(
      (doc->>'error') = 'false' AND json_array_length(doc->'statements') = 1,
      false) FROM (SELECT json_serialize_sql(?) AS doc)`, query).Scan(&valid)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("an inbox query must be a single SELECT statement")
	}
	return d.DB.Query(ctx, query, offset, limit)
}
