package db

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/bigquery"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// BigQueryDB wraps a BigQuery client scoped to a project and (optionally) a
// default dataset. It implements Querier so the UI layer treats it like any
// other backend.
//
// Cost safety is central to this backend because BigQuery bills by bytes
// scanned. Every query job carries a maximumBytesBilled cap (a hard guarantee: a
// job that would exceed it fails before scanning, and a failed job isn't
// billed) and labels for FinOps attribution. Cancellation is best-effort on top
// of that: cancelling the Go context stops us waiting for results, but we also
// call job.Cancel so the server-side job actually stops.
type BigQueryDB struct {
	client         *bigquery.Client
	projectID      string
	defaultDataset string
	maxBytesBilled int64
	labels         map[string]string

	// running holds the in-flight query job so an out-of-band cancel (the
	// /sql/cancel endpoint, or a UI Cancel button) can reach it. The worker is
	// sequential, so at most one query runs per connection at a time.
	mu      sync.Mutex
	running *bigquery.Job
}

// BigQueryConfig configures a BigQuery connection.
type BigQueryConfig struct {
	Project        string
	DefaultDataset string
	// MaxBytesBilled caps bytes scanned per query. Zero means no cap (not
	// recommended); callers should pass a sane default (see the connection
	// model). This is the primary spend guardrail.
	MaxBytesBilled int64
	// Labels are attached to every job. Defaults to {"app":"bufflehead"} merged
	// with whatever the caller supplies (e.g. source=agent|ui).
	Labels map[string]string
}

// NewBigQuery constructs a BigQuery-backed Querier.
//
// Auth: with no credential options it uses Application Default Credentials — the
// file gcloud wrote at ~/.config/gcloud/application_default_credentials.json.
// The gcloud binary is never invoked. Extra option.ClientOption values (a
// credentials file, or an emulator endpoint + WithoutAuthentication for tests)
// are appended, so this constructor covers production, tests, and future
// auth sources without changing shape.
//
// If BIGQUERY_EMULATOR_HOST is set, the client is pointed at it with auth
// disabled. This is how integration tests drive goccy/bigquery-emulator.
func NewBigQuery(ctx context.Context, cfg BigQueryConfig, opts ...option.ClientOption) (*BigQueryDB, error) {
	if host := os.Getenv("BIGQUERY_EMULATOR_HOST"); host != "" {
		opts = append(opts, option.WithEndpoint(host), option.WithoutAuthentication())
	}

	client, err := bigquery.NewClient(ctx, cfg.Project, opts...)
	if err != nil {
		return nil, fmt.Errorf("bigquery client: %w", err)
	}

	labels := map[string]string{"app": "bufflehead"}
	for k, v := range cfg.Labels {
		labels[k] = v
	}

	return &BigQueryDB{
		client:         client,
		projectID:      cfg.Project,
		defaultDataset: cfg.DefaultDataset,
		maxBytesBilled: cfg.MaxBytesBilled,
		labels:         labels,
	}, nil
}

// CredentialsOption builds a ClientOption from service-account or user
// credentials JSON, scoped for BigQuery. Keeps the SDK-specific scope + the
// WithCredentials construction in one place, so callers just hand over the JSON
// bytes. Empty data is a caller error — use ADC (no option) instead.
//
// CredentialsFromJSON carries a deprecation note about accepting credential
// configs from untrusted sources; here the JSON is a user-selected file on the
// user's own machine (trusted input), and ADC — not this path — is the primary
// auth story, so the advisory doesn't apply.
func CredentialsOption(ctx context.Context, jsonData []byte) (option.ClientOption, error) {
	creds, err := google.CredentialsFromJSON(ctx, jsonData, bigquery.Scope)
	if err != nil {
		return nil, err
	}
	return option.WithCredentials(creds), nil
}

// Ping verifies connectivity by listing datasets (cheap metadata, and
// emulator-friendly — unlike a dry-run, which the emulator doesn't implement).
func (b *BigQueryDB) Ping(ctx context.Context) error {
	it := b.client.Datasets(ctx)
	_, err := it.Next()
	if err == iterator.Done {
		return nil // reachable, just no datasets
	}
	return err
}

// Close releases the client.
func (b *BigQueryDB) Close() error {
	return b.client.Close()
}

// Datasets lists dataset IDs in the project, sorted. Used by the dataset
// switcher. A project can have a very large number of datasets, so the caller
// (UI) is responsible for filtering/capping how many it renders.
func (b *BigQueryDB) Datasets() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	it := b.client.Datasets(ctx)
	var out []string
	for {
		ds, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list datasets: %w", err)
		}
		out = append(out, ds.DatasetID)
	}
	sort.Strings(out)
	return out, nil
}

// Tables lists tables and views in the default dataset via INFORMATION_SCHEMA.
// Names are returned dataset-qualified ("dataset.table") to match how the rest
// of the app qualifies non-DuckDB tables. A default dataset is required — a
// whole-project listing would be too large to feed the schema tree or the AI
// prompt (see docs/bigquery-connection.md).
func (b *BigQueryDB) Tables() ([]TableInfo, error) {
	if b.defaultDataset == "" {
		return nil, fmt.Errorf("bigquery: no default dataset set for connection")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sql := fmt.Sprintf(
		"SELECT table_name, table_type FROM `%s`.`%s`.INFORMATION_SCHEMA.TABLES ORDER BY table_name",
		b.projectID, b.defaultDataset)

	rows, _, err := b.readAll(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}

	var tables []TableInfo
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		name, _ := r[0].(string)
		tt, _ := r[1].(string)
		t := TableInfo{Name: b.defaultDataset + "." + name}
		switch tt {
		case "VIEW", "MATERIALIZED VIEW":
			t.Type = "view"
		default:
			t.Type = "table"
		}
		tables = append(tables, t)
	}
	return tables, nil
}

// TableSchema returns column info for one dataset-qualified table.
func (b *BigQueryDB) TableSchema(name string) ([]Column, error) {
	dataset, table := b.splitDatasetTable(name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sql := fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable FROM `%s`.`%s`.INFORMATION_SCHEMA.COLUMNS "+
			"WHERE table_name = '%s' ORDER BY ordinal_position",
		b.projectID, dataset, table)

	rows, _, err := b.readAll(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("table schema: %w", err)
	}

	var cols []Column
	for _, r := range rows {
		if len(r) < 3 {
			continue
		}
		colName, _ := r[0].(string)
		dataType, _ := r[1].(string)
		nullable, _ := r[2].(string)
		cols = append(cols, Column{Name: colName, DataType: dataType, Nullable: nullable == "YES"})
	}
	return cols, nil
}

// AllTableSchemas fetches columns for every table in the default dataset in a
// single INFORMATION_SCHEMA query, avoiding a per-table N+1 (a dataset can have
// thousands of tables). Mirrors PostgresDB.AllTableSchemas.
func (b *BigQueryDB) AllTableSchemas(tables []TableInfo) error {
	if len(tables) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sql := fmt.Sprintf(
		"SELECT table_name, column_name, data_type, is_nullable FROM `%s`.`%s`.INFORMATION_SCHEMA.COLUMNS "+
			"ORDER BY table_name, ordinal_position",
		b.projectID, b.defaultDataset)

	rows, _, err := b.readAll(ctx, sql)
	if err != nil {
		return fmt.Errorf("all table schemas: %w", err)
	}

	colsByTable := make(map[string][]Column)
	for _, r := range rows {
		if len(r) < 4 {
			continue
		}
		tableName, _ := r[0].(string)
		colName, _ := r[1].(string)
		dataType, _ := r[2].(string)
		nullable, _ := r[3].(string)
		key := b.defaultDataset + "." + tableName
		colsByTable[key] = append(colsByTable[key], Column{
			Name: colName, DataType: dataType, Nullable: nullable == "YES",
		})
	}

	for i := range tables {
		tables[i].Columns = colsByTable[tables[i].Name]
	}
	return nil
}

// Query runs a paginated query. Unlike the Postgres backend it does NOT issue a
// separate COUNT(*) — on BigQuery that's a full extra scan (i.e. extra cost).
// Total is reported as a lower bound (offset + rows returned); the interactive
// grid treats a full page as "there may be more". The agent path (offset 0)
// gets an exact count of the returned rows.
//
// maximumBytesBilled and job labels are applied to every job, and the job is
// cancelled server-side if the context is cancelled (client disconnect, timeout,
// or an explicit /sql/cancel).
func (b *BigQueryDB) Query(ctx context.Context, sql string, offset, limit int) (*QueryResult, error) {
	if !isReadOnlyQuery(sql) {
		return nil, fmt.Errorf("only read-only queries (SELECT/WITH) are allowed")
	}
	if limit <= 0 {
		limit = 100
	}

	q := b.client.Query(bqPaged(sql, offset, limit))
	if b.defaultDataset != "" {
		q.DefaultProjectID = b.projectID
		q.DefaultDatasetID = b.defaultDataset
	}
	if b.maxBytesBilled > 0 {
		q.MaxBytesBilled = b.maxBytesBilled
	}
	q.Labels = b.labels

	job, err := q.Run(ctx)
	if err != nil {
		return nil, err
	}
	b.setRunning(job)
	defer b.clearRunning()

	it, err := job.Read(ctx)
	if err != nil {
		b.cancelIfCtxDone(ctx, job)
		return nil, err
	}

	result := &QueryResult{}
	for {
		var row []bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			b.cancelIfCtxDone(ctx, job)
			return nil, err
		}
		strs := make([]string, len(row))
		for i, v := range row {
			strs[i] = bqFormatValue(v)
		}
		result.Rows = append(result.Rows, strs)
	}

	for _, f := range it.Schema {
		result.Columns = append(result.Columns, f.Name)
	}
	result.Total = int64(offset) + int64(len(result.Rows))

	// Report bytes scanned so a cost-aware agent sees what the query cost.
	if status, serr := job.Status(ctx); serr == nil && status.Statistics != nil {
		result.BytesProcessed = status.Statistics.TotalBytesProcessed
	}
	return result, nil
}

// CancelRunning cancels the in-flight query job, if any. Used by the
// /sql/cancel control endpoint and the UI Cancel action.
func (b *BigQueryDB) CancelRunning() {
	b.mu.Lock()
	job := b.running
	b.mu.Unlock()
	if job != nil {
		// Best-effort; use a fresh context since the request context may be gone.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = job.Cancel(ctx)
	}
}

func (b *BigQueryDB) setRunning(job *bigquery.Job) {
	b.mu.Lock()
	b.running = job
	b.mu.Unlock()
}

func (b *BigQueryDB) clearRunning() {
	b.mu.Lock()
	b.running = nil
	b.mu.Unlock()
}

// cancelIfCtxDone kills the server-side job when the request context was
// cancelled — cancelling the context alone only stops us waiting, not the job.
func (b *BigQueryDB) cancelIfCtxDone(ctx context.Context, job *bigquery.Job) {
	if ctx.Err() == nil {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = job.Cancel(cctx)
}

// readAll runs a query and collects every row. Used for metadata queries
// (INFORMATION_SCHEMA), which are cheap and small. No maximumBytesBilled cap is
// applied — metadata scans are effectively free and must not be blocked by a
// user's tight cap.
func (b *BigQueryDB) readAll(ctx context.Context, sql string) ([][]bigquery.Value, bigquery.Schema, error) {
	q := b.client.Query(sql)
	q.Labels = b.labels
	it, err := q.Read(ctx)
	if err != nil {
		return nil, nil, err
	}
	var rows [][]bigquery.Value
	for {
		var row []bigquery.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
	return rows, it.Schema, nil
}

func (b *BigQueryDB) splitDatasetTable(name string) (dataset, table string) {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return b.defaultDataset, name
}

// bqPaged appends LIMIT/OFFSET to the user's SQL. Mirrors PostgresDB.Query's
// paging: the query must not already carry its own LIMIT (the AI prompt tells
// the agent not to add one — pagination is applied here).
func bqPaged(sql string, offset, limit int) string {
	s := strings.TrimRight(strings.TrimSpace(sql), ";")
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", s, limit, offset)
}

// bqFormatValue renders a single BigQuery value as display text.
func bqFormatValue(v bigquery.Value) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339)
	case *big.Rat:
		return val.FloatString(9) // NUMERIC/BIGNUMERIC
	default:
		return fmt.Sprintf("%v", val)
	}
}

// isReadOnlyQuery reports whether sql is a read-only statement. BigQuery has no
// session-level read-only mode (unlike Postgres), so we gate it here to keep the
// viewer read-only. Conservative: allow SELECT/WITH/EXPLAIN and a leading paren;
// reject DML/DDL. Leading comments and whitespace are ignored.
func isReadOnlyQuery(sql string) bool {
	s := stripLeadingSQLNoise(sql)
	up := strings.ToUpper(s)
	return strings.HasPrefix(up, "SELECT") ||
		strings.HasPrefix(up, "WITH") ||
		strings.HasPrefix(up, "EXPLAIN") ||
		strings.HasPrefix(up, "(")
}

// stripLeadingSQLNoise removes leading whitespace, -- line comments, and
// /* */ block comments so statement-type detection sees the first keyword.
func stripLeadingSQLNoise(sql string) string {
	s := sql
	for {
		s = strings.TrimLeft(s, " \t\r\n")
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
			} else {
				return ""
			}
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
			} else {
				return ""
			}
		default:
			return s
		}
	}
}

// QuoteBigQueryName backtick-quotes each dot-separated segment of an identifier
// (project.dataset.table). BigQuery's GoogleSQL uses backticks, not the
// double-quotes Postgres/DuckDB use — this is the BigQuery counterpart to
// QuoteQualifiedName. Embedded backticks are escaped by backslash.
func QuoteBigQueryName(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = "`" + strings.ReplaceAll(p, "`", "\\`") + "`"
	}
	return strings.Join(parts, ".")
}

// Verify BigQueryDB implements Querier at compile time.
var _ Querier = (*BigQueryDB)(nil)
