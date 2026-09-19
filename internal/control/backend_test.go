package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// answerCommands answers main-thread commands the way the app would, from a
// goroutine, for the duration of a test.
func answerCommands(t *testing.T, s *Server, handle func(*Command) Result) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case cmd := <-s.commands:
				cmd.Respond(handle(cmd))
			case <-done:
				return
			}
		}
	}()
	t.Cleanup(func() { close(done) })
}

func TestConnections_RoundTripsSnapshot(t *testing.T) {
	s := New(0)
	want := []ConnectionInfo{{Name: "Memory", Kind: "memory", Active: true, Dialect: "duckdb",
		Tables: []TableInfo{{Name: "/x.parquet", Type: "file", Columns: []ColumnInfo{{Name: "id", DataType: "BIGINT"}}}}}}
	var gotIncludeCols bool
	answerCommands(t, s, func(cmd *Command) Result {
		if cmd.Action != "connections" {
			return Result{Error: "unexpected " + cmd.Action}
		}
		var d ConnectionsData
		json.Unmarshal(cmd.Data, &d)
		gotIncludeCols = d.IncludeColumns
		data, _ := json.Marshal(want)
		return Result{OK: true, Data: data}
	})

	handler := s.requireAuth(buildMux(s))
	req := httptest.NewRequest("GET", "/connections?columns=1", nil)
	req.Header.Set("Authorization", "Bearer "+s.APIKey())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var got []ConnectionInfo
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !gotIncludeCols || len(got) != 1 || got[0].Tables[0].Columns[0].Name != "id" {
		t.Errorf("includeCols=%v got=%+v", gotIncludeCols, got)
	}
}

func TestDispatch_HonoursContext(t *testing.T) {
	s := New(0) // nobody drains commands
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Fill the buffer so the send itself must block.
	for i := 0; i < cap(s.commands); i++ {
		s.commands <- &Command{}
	}
	if _, err := s.Connections(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestExecSQL_ThirdConcurrentCallIsBusy(t *testing.T) {
	s := New(0)
	release := make(chan struct{})
	var started sync.WaitGroup
	s.SetSQLExecutor(func(ctx context.Context, conn, sql string, limit int) (*SQLResult, error) {
		started.Done()
		<-release
		return &SQLResult{Columns: []string{"x"}, Rows: [][]string{{"1"}}, Total: 1}, nil
	})
	started.Add(2)
	for i := 0; i < 2; i++ {
		go s.ExecSQL(context.Background(), SQLRequest{SQL: "select 1"})
	}
	started.Wait()

	if _, err := s.ExecSQL(context.Background(), SQLRequest{SQL: "select 1"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("third call err = %v, want ErrBusy", err)
	}

	// /sql surfaces it as 429 with Retry-After.
	handler := s.requireAuth(buildMux(s))
	req := httptest.NewRequest("POST", "/sql", strings.NewReader(`{"sql":"select 1"}`))
	req.Header.Set("Authorization", "Bearer "+s.APIKey())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 429 || w.Header().Get("Retry-After") != "5" {
		t.Fatalf("status %d retry-after %q", w.Code, w.Header().Get("Retry-After"))
	}
	close(release)
}

func TestMCP_RequiresKeyAnd503WhenUnset(t *testing.T) {
	s := New(0)
	handler := s.requireAuth(buildMux(s))

	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no key: status %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+s.APIKey())
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unset handler: status %d", w.Code)
	}

	s.SetMCPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req = httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+s.APIKey())
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTeapot {
		t.Fatalf("mounted handler: status %d", w.Code)
	}
}

func TestClient_RoundTrips(t *testing.T) {
	s := New(0)
	s.SetStateProvider(func() (json.RawMessage, error) { return json.RawMessage(`{"tabCount":1}`), nil })
	s.SetSQLExecutor(func(ctx context.Context, conn, sql string, limit int) (*SQLResult, error) {
		if sql == "bad" {
			return nil, errors.New("Parser Error: bad")
		}
		return &SQLResult{Columns: []string{"c"}, Rows: [][]string{{conn}}, Total: 1}, nil
	})
	s.SetCancelExecutor(func(conn string) error { return errors.New("nope " + conn) })
	s.SetS3Executor(func(req S3GetObjectRequest) (*S3GetObjectResult, error) {
		return &S3GetObjectResult{Content: req.Bucket + "/" + req.Key, Size: 3}, nil
	})
	answerCommands(t, s, func(cmd *Command) Result {
		switch cmd.Action {
		case "connections":
			data, _ := json.Marshal([]ConnectionInfo{{Name: "Memory", Kind: "memory"}})
			return Result{OK: true, Data: data}
		case "reconnect":
			data, _ := json.Marshal(ReconnectResult{Connection: "prod", OK: false,
				Steps: []ReconnectStep{{Step: "start_tunnel", OK: false, Error: "boom"}}})
			return Result{OK: false, Error: "reconnect failed", Data: data}
		}
		return Result{Error: "unexpected"}
	})
	ts := httptest.NewServer(s.requireAuth(buildMux(s)))
	defer ts.Close()

	ctx := context.Background()
	c := NewClient(strings.TrimPrefix(ts.URL, "http://"), s.APIKey())
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	conns, err := c.Connections(ctx, false)
	if err != nil || len(conns) != 1 || conns[0].Name != "Memory" {
		t.Fatalf("connections: %+v %v", conns, err)
	}
	res, err := c.ExecSQL(ctx, SQLRequest{SQL: "select 1", Connection: "x"})
	if err != nil || res.Rows[0][0] != "x" {
		t.Fatalf("sql: %+v %v", res, err)
	}
	if _, err := c.ExecSQL(ctx, SQLRequest{SQL: "bad"}); err == nil || !strings.Contains(err.Error(), "Parser Error") {
		t.Fatalf("sql error: %v", err)
	}
	if err := c.CancelSQL(ctx, "p"); err == nil || err.Error() != "nope p" {
		t.Fatalf("cancel: %v", err)
	}
	obj, err := c.GetS3Object(ctx, S3GetObjectRequest{Bucket: "b", Key: "k"})
	if err != nil || obj.Content != "b/k" {
		t.Fatalf("s3: %+v %v", obj, err)
	}
	rr, err := c.Reconnect(ctx, "prod")
	if err != nil || rr.OK || len(rr.Steps) != 1 || rr.Steps[0].Error != "boom" {
		t.Fatalf("reconnect: %+v %v", rr, err)
	}

	bad := NewClient(strings.TrimPrefix(ts.URL, "http://"), "wrong")
	if err := bad.Ping(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong key: %v", err)
	}
	dead := NewClient("127.0.0.1:1", "k")
	if err := dead.Ping(ctx); err == nil || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("dead server: %v", err)
	}
}

func TestClient_BusyMapsTo429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"busy"}`))
	}))
	defer ts.Close()
	c := NewClient(ts.URL, "k")
	if _, err := c.ExecSQL(context.Background(), SQLRequest{SQL: "x"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscovery_WriteReadRemove(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "Bufflehead")
	d := Discovery{Addr: "127.0.0.1:1234", Key: "k", PID: 42, Version: "1.0.0"}
	if err := WriteDiscovery(dir, d); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(DiscoveryPath(dir))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("perm = %o, want 600", st.Mode().Perm())
		}
	}
	got, err := ReadDiscovery(dir)
	if err != nil || *got != d {
		t.Fatalf("read: %+v %v", got, err)
	}
	// A different pid must not remove it.
	if err := RemoveDiscoveryIfOwned(dir, 43); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DiscoveryPath(dir)); err != nil {
		t.Fatal("file removed by non-owner")
	}
	if err := RemoveDiscoveryIfOwned(dir, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDiscovery(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("after remove: %v", err)
	}
	// Removing when absent is fine.
	if err := RemoveDiscoveryIfOwned(dir, 42); err != nil {
		t.Fatal(err)
	}
	// Corrupt file is reported, not silently treated as absent.
	os.WriteFile(DiscoveryPath(dir), []byte("{"), 0o600)
	if _, err := ReadDiscovery(dir); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt: %v", err)
	}
	if err := WriteDiscovery(dir, Discovery{}); err == nil {
		t.Fatal("empty discovery accepted")
	}
}
