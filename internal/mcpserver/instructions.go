package mcpserver

// instructions is sent to every MCP client at initialize. It condenses the
// per-connection guidance the app's "Ask AI" prompts carry (see
// internal/ui/appwindow.go buildAIPrompt and friends).
const instructions = `Bufflehead is a desktop data viewer. These tools query whatever the user currently has open in it; Bufflehead owns every credential, tunnel and connection.

Start with list_connections. Every other tool takes an optional "connection" name from that list (omit it for the active connection).

How to reference data, by connection kind:
- memory (flat files: Parquet, CSV, JSON, TSV) and folder: DuckDB SQL. Reference a file by its single-quoted path in FROM, e.g. SELECT * FROM '/path/data.parquet'. A folder connection's tables are glob patterns; a glob reads every matching file as one table, and a single file or narrower glob works the same way. get_schema lists the paths and their columns.
- duckdb / sqlite (database files): query tables by name.
- postgres / aws-postgres / mysql: query tables by name. Use indexed columns in WHERE, avoid full table scans, keep queries targeted. Results default to 100 rows; pass "limit" to change it.
- bigquery: GoogleSQL. Tables are backtick-quoted ` + "`project.dataset.table`" + `. BigQuery bills by bytes SCANNED, not rows: filter on the partition column, select only needed columns, never SELECT * on wide tables; a LIMIT does not reduce bytes scanned. Each query is capped by a byte budget and fails if it would exceed it — narrow it and retry. run_sql reports bytes_processed; cancel_sql works here.

Row limits: local connections (memory, folder, duckdb, sqlite) return every row up to a 10,000-row ceiling unless you pass "limit" or your own LIMIT. Remote connections default to 100 rows. There is no server-side timeout; the query is cancelled if you stop waiting.

If two queries are already running, run_sql reports busy — wait a few seconds and retry, or cancel_sql.

Some columns hold JSON with S3 pointers such as {"s3_bucket": "...", "s3_key": "..."}; fetch those with get_s3_object (AWS gateway connections only).

If queries start failing with connection errors (timeouts, health-check failures, expired credentials, a broken tunnel), call reconnect: it cancels running queries, tears down the tunnel and pool, and rebuilds them, reporting each step. If a step mentions expired SSO, the user must run "aws sso login" before a reconnect can succeed.`
