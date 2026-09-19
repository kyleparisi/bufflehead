package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"bufflehead/internal/control"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioEndToEnd builds the bridge, points it at a fake app via the
// discovery file, and drives it over stdio exactly as Claude Desktop would.
func TestStdioEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin := filepath.Join(t.TempDir(), "bufflehead-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	s := control.New(0)
	s.SetStateProvider(func() (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	s.SetSQLExecutor(func(ctx context.Context, conn, sql string, limit int) (*control.SQLResult, error) {
		return &control.SQLResult{Columns: []string{"n"}, Rows: [][]string{{"42"}}, Total: 1}, nil
	})
	go func() {
		for cmd := range s.Commands() {
			data, _ := json.Marshal([]control.ConnectionInfo{{Name: "Memory", Kind: "memory", Active: true, Dialect: "duckdb"}})
			cmd.Respond(control.Result{OK: true, Data: data})
		}
	}()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	dir := t.TempDir()
	if err := control.WriteDiscovery(dir, control.Discovery{
		Addr: strings.TrimPrefix(ts.URL, "http://"), Key: s.APIKey(), PID: os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "BUFFLEHEAD_CONFIG_DIR="+dir, "BUFFLEHEAD_CONTROL_ADDR=", "BUFFLEHEAD_CONTROL_KEY=")
	cmd.Stderr = os.Stderr
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).
		Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer cs.Close()

	if cs.InitializeResult().ServerInfo.Name != "bufflehead" {
		t.Errorf("server info = %+v", cs.InitializeResult().ServerInfo)
	}
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 6 {
		t.Fatalf("tools: %d %v", len(tools.Tools), err)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "run_sql", Arguments: map[string]any{"sql": "select 42"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "| 42 |") {
		t.Errorf("run_sql: isError=%v content=%+v", res.IsError, res.Content[0])
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_connections"})
	if res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "Memory (memory") {
		t.Errorf("list_connections: %+v", res.Content[0])
	}
}
