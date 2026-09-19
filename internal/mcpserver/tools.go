package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bufflehead/internal/control"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Caps on the text rendering of run_sql. The structured result carries the
// whole page; the text is what most clients show the model first.
const (
	maxTextRows     = 200
	maxTextBytes    = 8 * 1024
	maxSchemaTables = 200
)

// ListConnectionsInput has no fields; the tool takes no arguments.
type ListConnectionsInput struct{}

// ListConnectionsOutput wraps the list so the output schema is an object.
type ListConnectionsOutput struct {
	Connections []control.ConnectionInfo `json:"connections"`
}

// GetSchemaInput selects a connection and optionally one table within it.
type GetSchemaInput struct {
	Connection string `json:"connection,omitempty" jsonschema:"Connection name from list_connections; omit for the active connection"`
	Table      string `json:"table,omitempty" jsonschema:"Only this table, view, glob pattern or file path (as listed by get_schema); omit for all"`
}

// GetSchemaOutput is the connection with its tables and columns.
type GetSchemaOutput struct {
	Connection control.ConnectionInfo `json:"connection"`
}

// RunSQLInput is one query against one connection.
type RunSQLInput struct {
	SQL        string `json:"sql" jsonschema:"The SQL to run, in the connection's dialect"`
	Connection string `json:"connection,omitempty" jsonschema:"Connection name from list_connections; omit for the active connection"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max rows to return; 0 uses the connection's default (100 for remote, all rows up to 10000 for local)"`
}

// ConnectionInput names a connection (or none for the active one).
type ConnectionInput struct {
	Connection string `json:"connection,omitempty" jsonschema:"Connection name from list_connections; omit for the active connection"`
}

// OKOutput is a bare success flag.
type OKOutput struct {
	OK bool `json:"ok"`
}

// GetS3ObjectInput fetches one object with a connection's AWS credentials.
type GetS3ObjectInput struct {
	Bucket     string `json:"bucket" jsonschema:"S3 bucket name"`
	Key        string `json:"key" jsonschema:"Object key"`
	Connection string `json:"connection,omitempty" jsonschema:"AWS gateway connection whose credentials to use; omit for the active connection"`
	Region     string `json:"region,omitempty" jsonschema:"Override the bucket region (default: the connection's region)"`
	MaxBytes   int64  `json:"max_bytes,omitempty" jsonschema:"Max bytes to read (default 10 MB); larger objects are truncated"`
}

func registerTools(srv *mcp.Server, b Backend) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_connections",
		Description: "List the connections currently open in Bufflehead: name, kind (memory, duckdb, sqlite, folder, postgres, aws-postgres, mysql, bigquery), path, SQL dialect, default row limit, and whether cancel/S3 are supported. Call this first; other tools take a connection name from it.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ListConnectionsInput) (*mcp.CallToolResult, ListConnectionsOutput, error) {
		conns, err := b.Connections(ctx, false)
		if err != nil {
			return nil, ListConnectionsOutput{}, err
		}
		for i := range conns {
			conns[i].Tables = nil
		}
		var text strings.Builder
		if len(conns) == 0 {
			text.WriteString("No connections open in Bufflehead.")
		}
		for _, c := range conns {
			fmt.Fprintf(&text, "- %s (%s", c.Name, c.Kind)
			if c.Path != "" {
				fmt.Fprintf(&text, ", %s", c.Path)
			}
			if c.Active {
				text.WriteString(", active")
			}
			text.WriteString(")\n")
		}
		return textResult(text.String()), ListConnectionsOutput{Connections: conns}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_schema",
		Description: "Tables (or files/glob patterns for memory and folder connections) and their columns for one connection. Pass table to narrow to a single entry.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetSchemaInput) (*mcp.CallToolResult, GetSchemaOutput, error) {
		conn, err := findConnection(ctx, b, in.Connection, true)
		if err != nil {
			return nil, GetSchemaOutput{}, err
		}
		if in.Table != "" {
			var kept []control.TableInfo
			for _, t := range conn.Tables {
				if t.Name == in.Table {
					kept = append(kept, t)
				}
			}
			if kept == nil {
				return nil, GetSchemaOutput{}, fmt.Errorf("connection %q has no table %q (get_schema without table lists them)", conn.Name, in.Table)
			}
			conn.Tables = kept
		}
		return textResult(renderSchema(*conn)), GetSchemaOutput{Connection: *conn}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_sql",
		Description: "Run a SQL query on a connection and return columns and rows. Flat files are referenced by single-quoted path (SELECT * FROM '/path/file.parquet'); database connections by table name. Remote connections default to 100 rows unless limit is set.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RunSQLInput) (*mcp.CallToolResult, control.SQLResult, error) {
		if strings.TrimSpace(in.SQL) == "" {
			return nil, control.SQLResult{}, errors.New("sql is required")
		}
		res, err := b.ExecSQL(ctx, control.SQLRequest{SQL: in.SQL, Connection: in.Connection, Limit: in.Limit})
		if err != nil {
			if errors.Is(err, control.ErrBusy) {
				return nil, control.SQLResult{}, errors.New("Bufflehead is busy with two queries already; retry in ~5 seconds or call cancel_sql")
			}
			return nil, control.SQLResult{}, err
		}
		if res.Columns == nil {
			res.Columns = []string{}
		}
		if res.Rows == nil {
			res.Rows = [][]string{}
		}
		table, shown := renderTable(res.Columns, res.Rows, maxTextRows, maxTextBytes)
		var text strings.Builder
		text.WriteString(table)
		fmt.Fprintf(&text, "\n%s returned", plural(len(res.Rows), "row"))
		if res.Total > int64(len(res.Rows)) {
			fmt.Fprintf(&text, " of %d total", res.Total)
		}
		if shown < len(res.Rows) {
			fmt.Fprintf(&text, "; %d shown above (all rows are in the structured result)", shown)
		}
		if res.BytesProcessed > 0 {
			fmt.Fprintf(&text, "; bytes_processed=%d", res.BytesProcessed)
		}
		text.WriteString(".")
		return textResult(text.String()), *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cancel_sql",
		Description: "Cancel the query currently running on a connection. Only BigQuery supports explicit cancellation; other backends cancel when the caller stops waiting.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ConnectionInput) (*mcp.CallToolResult, OKOutput, error) {
		if err := b.CancelSQL(ctx, in.Connection); err != nil {
			return nil, OKOutput{}, err
		}
		return textResult("Cancelled."), OKOutput{OK: true}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_s3_object",
		Description: "Fetch an S3 object's contents using an AWS gateway connection's credentials. Use it for S3 pointers found in query results ({\"s3_bucket\": ..., \"s3_key\": ...}).",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetS3ObjectInput) (*mcp.CallToolResult, control.S3GetObjectResult, error) {
		if in.Bucket == "" || in.Key == "" {
			return nil, control.S3GetObjectResult{}, errors.New("bucket and key are required")
		}
		// Refuse before touching the backend when the connection has no AWS
		// credentials, with a message that points at the right connections.
		conn, err := findConnection(ctx, b, in.Connection, false)
		if err != nil {
			return nil, control.S3GetObjectResult{}, err
		}
		if !conn.SupportsS3 {
			return nil, control.S3GetObjectResult{}, fmt.Errorf("connection %q (%s) has no AWS credentials; get_s3_object needs an aws-postgres connection", conn.Name, conn.Kind)
		}
		res, err := b.GetS3Object(ctx, control.S3GetObjectRequest{
			Bucket: in.Bucket, Key: in.Key, Region: in.Region, Connection: conn.Name, MaxBytes: in.MaxBytes,
		})
		if err != nil {
			return nil, control.S3GetObjectResult{}, err
		}
		text := res.Content
		if res.Truncated {
			text += fmt.Sprintf("\n\n[truncated: showing %d of %d bytes]", len(res.Content), res.Size)
		}
		return textResult(text), *res, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "reconnect",
		Description: "Force a full reconnect of a remote connection: cancel queries, close the pool, stop and restart the tunnel, refresh credentials, reconnect, reload tables. Use when queries fail with connection, tunnel or credential errors.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ConnectionInput) (*mcp.CallToolResult, control.ReconnectResult, error) {
		rr, err := b.Reconnect(ctx, in.Connection)
		if err != nil {
			return nil, control.ReconnectResult{}, err
		}
		text := control.FormatReconnectSteps(rr.Connection, rr.OK, rr.Steps, nil)
		if rr.OK && rr.Tables > 0 {
			text += fmt.Sprintf("\n%s loaded.", plural(rr.Tables, "table"))
		}
		res := textResult(text)
		res.IsError = !rr.OK
		return res, *rr, nil
	})
}

// findConnection resolves a name (empty = active) against the backend's list.
func findConnection(ctx context.Context, b Backend, name string, includeColumns bool) (*control.ConnectionInfo, error) {
	conns, err := b.Connections(ctx, includeColumns)
	if err != nil {
		return nil, err
	}
	if len(conns) == 0 {
		return nil, errors.New("no connections are open in Bufflehead")
	}
	for i := range conns {
		if (name == "" && conns[i].Active) || (name != "" && conns[i].Name == name) {
			return &conns[i], nil
		}
	}
	if name == "" {
		return &conns[0], nil
	}
	names := make([]string, len(conns))
	for i, c := range conns {
		names[i] = c.Name
	}
	return nil, fmt.Errorf("connection %q not found; open connections: %s", name, strings.Join(names, ", "))
}

// renderSchema lists a connection's tables one per line with their columns,
// capped so a huge remote schema stays readable.
func renderSchema(c control.ConnectionInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s, dialect %s)\n", c.Name, c.Kind, c.Dialect)
	switch c.Kind {
	case "memory":
		b.WriteString("Reference each file by its single-quoted path in FROM.\n")
	case "folder":
		b.WriteString("Reference each entry by its single-quoted glob path in FROM; a narrower glob or single file works too.\n")
	}
	if len(c.Tables) == 0 {
		b.WriteString("(no tables)\n")
		return b.String()
	}
	for i, t := range c.Tables {
		if i >= maxSchemaTables {
			fmt.Fprintf(&b, "… %d more tables; pass table=<name> to fetch one.\n", len(c.Tables)-i)
			break
		}
		fmt.Fprintf(&b, "- %s", t.Name)
		if t.Type != "" && t.Type != "table" {
			fmt.Fprintf(&b, " (%s)", strings.ToLower(t.Type))
		}
		if t.Detail != "" {
			fmt.Fprintf(&b, " — %s", t.Detail)
		}
		if len(t.Columns) > 0 {
			cols := make([]string, len(t.Columns))
			for j, col := range t.Columns {
				cols[j] = col.Name + " " + col.DataType
			}
			fmt.Fprintf(&b, ": %s", strings.Join(cols, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}
