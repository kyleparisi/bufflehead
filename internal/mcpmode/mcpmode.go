// Package mcpmode runs Bufflehead as a headless MCP server over stdio.
//
// Claude Desktop (and other MCP clients) launch local servers as subprocesses
// and speak JSON-RPC over stdin/stdout. Rather than shipping a second binary,
// the app's own executable serves MCP when started as
//
//	Bufflehead --headless -- --mcp
//
// In this mode no window, UI or control server is created: the process reads
// the saved bookmarks directly, connects to them on first use, and exits when
// the client closes stdin. Because the client owns both ends of the pipe, no
// port, key or discovery file is involved.
package mcpmode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"bufflehead/internal/db"
	"bufflehead/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Flag is the user argument (after Godot's "--") that selects MCP mode.
const Flag = "--mcp"

// Row limits for run_sql. Clients hand the structured result to the model
// as-is, so the cap applies to the rows themselves, not only the text.
const (
	defaultRowLimit = 100
	maxRowLimit     = 1000
	maxTextBytes    = 8 * 1024
)

// Requested reports whether args contain Flag.
func Requested(args []string) bool {
	for _, a := range args {
		if a == Flag {
			return true
		}
	}
	return false
}

// TakeStdout reserves the process's stdout for the MCP transport and points
// fd 1 (and os.Stdout) at stderr, so any later print — from Go, Godot or a
// C library — lands on stderr instead of corrupting the JSON-RPC stream.
// Call it as early as possible.
func TakeStdout() (*os.File, error) {
	return takeStdout()
}

// Run serves MCP on stdin and out until the client closes stdin, then closes
// every connection it opened.
func Run(ctx context.Context, out *os.File, version string) error {
	restoreSSHAgent()
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "bufflehead", Title: "Bufflehead", Version: version},
		nil,
	)
	cs := newConns()
	defer cs.closeAll()
	registerTools(srv, cs)
	return srv.Run(ctx, &mcp.IOTransport{Reader: os.Stdin, Writer: out})
}

// BookmarkInfo is the non-secret view of a saved connection.
type BookmarkInfo struct {
	Label     string `json:"label"`
	Kind      string `json:"kind"`
	Env       string `json:"env,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Database  string `json:"database,omitempty"`
	User      string `json:"user,omitempty"`
	Project   string `json:"project,omitempty"`
	SSH       bool   `json:"ssh,omitempty"`
	Connected bool   `json:"connected"`
}

// ListBookmarksInput has no fields; the tool takes no arguments.
type ListBookmarksInput struct{}

// ListBookmarksOutput wraps the list so the output schema is an object.
type ListBookmarksOutput struct {
	Bookmarks []BookmarkInfo `json:"bookmarks"`
}

// GetSchemaInput names a bookmark and optionally one table in it.
type GetSchemaInput struct {
	Bookmark string `json:"bookmark" jsonschema:"Bookmark label from list_bookmarks"`
	Table    string `json:"table,omitempty" jsonschema:"Only this table, with its columns; omit to list every table without columns"`
}

// TableOut is one table or view, with columns when a single table was asked for.
type TableOut struct {
	Name    string      `json:"name"`
	Type    string      `json:"type,omitempty"`
	Columns []ColumnOut `json:"columns,omitempty"`
}

// ColumnOut is one column of a table.
type ColumnOut struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// GetSchemaOutput is a bookmark's tables.
type GetSchemaOutput struct {
	Bookmark string     `json:"bookmark"`
	Tables   []TableOut `json:"tables"`
}

// RunSQLInput is one query against one bookmark.
type RunSQLInput struct {
	Bookmark string `json:"bookmark" jsonschema:"Bookmark label from list_bookmarks"`
	SQL      string `json:"sql" jsonschema:"A single SELECT query in the bookmark's dialect"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Max rows to return (default 100, max 1000)"`
}

// RunSQLOutput is one page of a query's result.
type RunSQLOutput struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	// Total is the query's full row count; more rows exist when it exceeds len(Rows).
	Total          int64 `json:"total"`
	BytesProcessed int64 `json:"bytes_processed,omitempty"`
}

func registerTools(srv *mcp.Server, cs *conns) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_bookmarks",
		Description: "List the database connections saved as bookmarks in Bufflehead: label, kind, environment, host, database and whether it is connected. Secrets are never included. Other tools take a label from here.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ ListBookmarksInput) (*mcp.CallToolResult, ListBookmarksOutput, error) {
		out := ListBookmarksOutput{Bookmarks: []BookmarkInfo{}}
		var text strings.Builder
		for _, b := range bookmarks().All() {
			info := bookmarkInfo(b)
			info.Connected = cs.connected(b.Label)
			out.Bookmarks = append(out.Bookmarks, info)
			fmt.Fprintf(&text, "- %s (%s", info.Label, info.Kind)
			if info.Env != "" {
				fmt.Fprintf(&text, ", %s", info.Env)
			}
			if info.Host != "" {
				fmt.Fprintf(&text, ", %s/%s", info.Host, info.Database)
			}
			if info.Connected {
				text.WriteString(", connected")
			}
			text.WriteString(")\n")
		}
		if len(out.Bookmarks) == 0 {
			text.WriteString("No bookmarks saved in Bufflehead.")
		}
		return textResult(text.String()), out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_schema",
		Description: "Tables and views of a bookmarked database, connecting on first use. Without table it lists names only; pass table to get that table's columns.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in GetSchemaInput) (*mcp.CallToolResult, GetSchemaOutput, error) {
		conn, err := cs.get(ctx, in.Bookmark)
		if err != nil {
			return nil, GetSchemaOutput{}, err
		}
		out := GetSchemaOutput{Bookmark: in.Bookmark, Tables: []TableOut{}}
		var text strings.Builder
		for _, t := range conn.Tables {
			if in.Table != "" && t.Name != in.Table {
				continue
			}
			to := TableOut{Name: t.Name, Type: strings.ToLower(t.Type)}
			fmt.Fprintf(&text, "- %s", t.Name)
			if to.Type != "" && to.Type != "table" {
				fmt.Fprintf(&text, " (%s)", to.Type)
			}
			if in.Table != "" {
				to.Columns = columnsOut(t.Columns)
				for _, c := range to.Columns {
					fmt.Fprintf(&text, "\n    %s %s", c.Name, c.Type)
				}
			}
			text.WriteString("\n")
			out.Tables = append(out.Tables, to)
		}
		if in.Table != "" && len(out.Tables) == 0 {
			return nil, GetSchemaOutput{}, fmt.Errorf("bookmark %q has no table %q (get_schema without table lists them)", in.Bookmark, in.Table)
		}
		if len(out.Tables) == 0 {
			text.WriteString("(no tables)")
		}
		return textResult(text.String()), out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_sql",
		Description: "Run a SELECT query on a bookmarked database, connecting on first use, and return columns and rows. Returns at most limit rows (default 100, max 1000); total is the full row count.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in RunSQLInput) (*mcp.CallToolResult, RunSQLOutput, error) {
		if strings.TrimSpace(in.SQL) == "" {
			return nil, RunSQLOutput{}, errors.New("sql is required")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = defaultRowLimit
		}
		limit = min(limit, maxRowLimit)

		conn, err := cs.get(ctx, in.Bookmark)
		if err != nil {
			return nil, RunSQLOutput{}, err
		}
		res, err := conn.DB.Query(ctx, in.SQL, 0, limit)
		if err != nil {
			return nil, RunSQLOutput{}, err
		}
		out := RunSQLOutput{Columns: res.Columns, Rows: res.Rows, Total: res.Total, BytesProcessed: res.BytesProcessed}
		if out.Columns == nil {
			out.Columns = []string{}
		}
		if out.Rows == nil {
			out.Rows = [][]string{}
		}
		return textResult(renderRows(out)), out, nil
	})
}

func bookmarkInfo(b models.Bookmark) BookmarkInfo {
	kind := string(b.Kind)
	if b.Kind == models.KindAWSGateway {
		kind = "aws-gateway"
	}
	return BookmarkInfo{
		Label:    b.Label,
		Kind:     kind,
		Env:      b.Env,
		Host:     b.RDSHost,
		Port:     b.RDSPort,
		Database: b.DBName,
		User:     b.DBUser,
		Project:  b.GCPProject,
		SSH:      b.SSH != nil,
	}
}

func columnsOut(cols []db.Column) []ColumnOut {
	out := make([]ColumnOut, len(cols))
	for i, c := range cols {
		out[i] = ColumnOut{Name: c.Name, Type: c.DataType, Nullable: c.Nullable}
	}
	return out
}

// renderRows formats a result as tab-separated lines, stopping at
// maxTextBytes; the structured result always carries every returned row.
func renderRows(r RunSQLOutput) string {
	var b strings.Builder
	b.WriteString(strings.Join(r.Columns, "\t"))
	b.WriteString("\n")
	shown := 0
	for _, row := range r.Rows {
		line := strings.Join(row, "\t") + "\n"
		if b.Len()+len(line) > maxTextBytes {
			break
		}
		b.WriteString(line)
		shown++
	}
	fmt.Fprintf(&b, "%d of %d rows", len(r.Rows), r.Total)
	if shown < len(r.Rows) {
		fmt.Fprintf(&b, " (%d shown here; all are in the structured result)", shown)
	}
	if r.BytesProcessed > 0 {
		fmt.Fprintf(&b, "; %d bytes processed", r.BytesProcessed)
	}
	return b.String()
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}
