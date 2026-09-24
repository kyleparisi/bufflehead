package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"bufflehead/internal/control"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBackend records calls and returns canned data.
type fakeBackend struct {
	conns     []control.ConnectionInfo
	sqlErr    error
	sqlResult *control.SQLResult
	s3Called  bool
	lastSQL   control.SQLRequest
}

func (f *fakeBackend) Connections(_ context.Context, _ bool) ([]control.ConnectionInfo, error) {
	return f.conns, nil
}
func (f *fakeBackend) ExecSQL(_ context.Context, req control.SQLRequest) (*control.SQLResult, error) {
	f.lastSQL = req
	if f.sqlErr != nil {
		return nil, f.sqlErr
	}
	return f.sqlResult, nil
}
func (f *fakeBackend) CancelSQL(_ context.Context, conn string) error {
	return errors.New("connection \"" + conn + "\" does not support query cancellation")
}
func (f *fakeBackend) GetS3Object(_ context.Context, _ control.S3GetObjectRequest) (*control.S3GetObjectResult, error) {
	f.s3Called = true
	return &control.S3GetObjectResult{Content: "hello", ContentType: "text/plain", Size: 5}, nil
}
func (f *fakeBackend) Reconnect(_ context.Context, conn string) (*control.ReconnectResult, error) {
	return &control.ReconnectResult{Connection: conn, OK: false, Steps: []control.ReconnectStep{
		{Step: "cancel_queries", OK: true},
		{Step: "start_tunnel", OK: false, Error: "ssm: timeout"},
	}}, nil
}

func newFake() *fakeBackend {
	return &fakeBackend{
		conns: []control.ConnectionInfo{
			{Name: "Memory", Kind: "memory", Path: ":memory:", Active: true, Dialect: "duckdb",
				Tables: []control.TableInfo{{Name: "/tmp/a.parquet", Type: "file",
					Columns: []control.ColumnInfo{{Name: "id", DataType: "BIGINT"}, {Name: "name", DataType: "VARCHAR", Nullable: true}}}}},
			{Name: "prod", Kind: "aws-postgres", Dialect: "postgres", DefaultLimit: 100, SupportsS3: true,
				Tables: []control.TableInfo{{Name: "users", Type: "table"}}},
		},
		sqlResult: &control.SQLResult{Columns: []string{"id", "name"}, Rows: [][]string{{"1", "a|b"}, {"2", "c"}}, Total: 50, BytesProcessed: 1234},
	}
}

// connect wires a client to the server over in-memory transports.
func connect(t *testing.T, b Backend) *mcp.ClientSession {
	t.Helper()
	srv := New(b, "test")
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestListsSixTools(t *testing.T) {
	cs := connect(t, newFake())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"list_connections": true, "get_schema": true, "run_sql": true, "cancel_sql": true, "get_s3_object": true, "reconnect": true}
	if len(res.Tools) != len(want) {
		t.Fatalf("got %d tools, want %d", len(res.Tools), len(want))
	}
	for _, tl := range res.Tools {
		if !want[tl.Name] {
			t.Errorf("unexpected tool %q", tl.Name)
		}
	}
	if cs.InitializeResult().Instructions == "" {
		t.Error("server sent no instructions")
	}
}

func TestListConnectionsOmitsTables(t *testing.T) {
	cs := connect(t, newFake())
	res := call(t, cs, "list_connections", nil)
	if res.IsError {
		t.Fatalf("error: %s", text(res))
	}
	if !strings.Contains(text(res), "Memory (memory, :memory:, active)") {
		t.Errorf("text = %q", text(res))
	}
	var out ListConnectionsOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Connections) != 2 || out.Connections[0].Tables != nil {
		t.Errorf("structured = %+v", out)
	}
}

func TestGetSchemaMemoryListsFiles(t *testing.T) {
	cs := connect(t, newFake())
	res := call(t, cs, "get_schema", nil) // active = Memory
	if res.IsError {
		t.Fatalf("error: %s", text(res))
	}
	got := text(res)
	for _, want := range []string{"single-quoted path", "/tmp/a.parquet", "id BIGINT", "name VARCHAR"} {
		if !strings.Contains(got, want) {
			t.Errorf("schema text missing %q:\n%s", want, got)
		}
	}
	res = call(t, cs, "get_schema", map[string]any{"connection": "prod", "table": "nope"})
	if !res.IsError || !strings.Contains(text(res), `no table "nope"`) {
		t.Errorf("unknown table: isError=%v text=%q", res.IsError, text(res))
	}
	res = call(t, cs, "get_schema", map[string]any{"connection": "ghost"})
	if !res.IsError || !strings.Contains(text(res), "open connections: Memory, prod") {
		t.Errorf("unknown connection: isError=%v text=%q", res.IsError, text(res))
	}
}

func TestRunSQLRendersTable(t *testing.T) {
	fb := newFake()
	cs := connect(t, fb)
	res := call(t, cs, "run_sql", map[string]any{"sql": "select 1", "connection": "prod", "limit": 7})
	if res.IsError {
		t.Fatalf("error: %s", text(res))
	}
	if fb.lastSQL.Connection != "prod" || fb.lastSQL.Limit != 7 {
		t.Errorf("request = %+v", fb.lastSQL)
	}
	got := text(res)
	for _, want := range []string{"| id | name |", "| 1 | a\\|b |", "2 rows returned of 50 total", "bytes_processed=1234"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	var out control.SQLResult
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Rows) != 2 {
		t.Errorf("structured rows = %+v err=%v", out, err)
	}
}

func TestRunSQLErrors(t *testing.T) {
	fb := newFake()
	cs := connect(t, fb)

	res := call(t, cs, "run_sql", map[string]any{"sql": "  "})
	if !res.IsError || !strings.Contains(text(res), "sql is required") {
		t.Errorf("empty sql: isError=%v text=%q", res.IsError, text(res))
	}

	fb.sqlErr = control.ErrBusy
	res = call(t, cs, "run_sql", map[string]any{"sql": "select 1"})
	if !res.IsError || !strings.Contains(text(res), "retry") {
		t.Errorf("busy: isError=%v text=%q", res.IsError, text(res))
	}

	fb.sqlErr = errors.New("Parser Error: syntax error at end of input")
	res = call(t, cs, "run_sql", map[string]any{"sql": "select"})
	if !res.IsError || !strings.Contains(text(res), "Parser Error") {
		t.Errorf("backend error: isError=%v text=%q", res.IsError, text(res))
	}
}

func TestRunSQLTruncatesText(t *testing.T) {
	fb := newFake()
	rows := make([][]string, 500)
	for i := range rows {
		rows[i] = []string{"x", "y"}
	}
	fb.sqlResult = &control.SQLResult{Columns: []string{"a", "b"}, Rows: rows, Total: 500}
	cs := connect(t, fb)
	res := call(t, cs, "run_sql", map[string]any{"sql": "select 1"})
	got := text(res)
	if !strings.Contains(got, "500 rows returned; 200 shown above") {
		t.Errorf("truncation note missing:\n%s", got[len(got)-200:])
	}
}

func TestGetS3ObjectGuard(t *testing.T) {
	fb := newFake()
	cs := connect(t, fb)
	res := call(t, cs, "get_s3_object", map[string]any{"bucket": "b", "key": "k"}) // active = Memory
	if !res.IsError || !strings.Contains(text(res), "no AWS credentials") {
		t.Errorf("guard: isError=%v text=%q", res.IsError, text(res))
	}
	if fb.s3Called {
		t.Error("backend was called despite the guard")
	}
	res = call(t, cs, "get_s3_object", map[string]any{"bucket": "b", "key": "k", "connection": "prod"})
	if res.IsError || text(res) != "hello" || !fb.s3Called {
		t.Errorf("allowed: isError=%v text=%q called=%v", res.IsError, text(res), fb.s3Called)
	}
}

func TestCancelAndReconnect(t *testing.T) {
	cs := connect(t, newFake())
	res := call(t, cs, "cancel_sql", map[string]any{"connection": "prod"})
	if !res.IsError || !strings.Contains(text(res), "does not support") {
		t.Errorf("cancel: isError=%v text=%q", res.IsError, text(res))
	}
	res = call(t, cs, "reconnect", map[string]any{"connection": "prod"})
	if !res.IsError || !strings.Contains(text(res), "ssm: timeout") {
		t.Errorf("reconnect: isError=%v text=%q", res.IsError, text(res))
	}
}
