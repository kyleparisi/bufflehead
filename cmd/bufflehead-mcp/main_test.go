package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"bufflehead/internal/control"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// fakeApp is a control server with a state provider, served over httptest.
func fakeApp(t *testing.T) (*control.Server, *httptest.Server) {
	t.Helper()
	s := control.New(0)
	s.SetStateProvider(func() (json.RawMessage, error) { return json.RawMessage(`{}`), nil })
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestResolveTarget_EnvWins(t *testing.T) {
	s, ts := fakeApp(t)
	dir := t.TempDir() // no discovery file at all
	c, err := resolveTarget(context.Background(), env(map[string]string{
		"BUFFLEHEAD_CONTROL_ADDR": strings.TrimPrefix(ts.URL, "http://"),
		"BUFFLEHEAD_CONTROL_KEY":  s.APIKey(),
	}), dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != ts.URL {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

func TestResolveTarget_DiscoveryFile(t *testing.T) {
	s, ts := fakeApp(t)
	dir := t.TempDir()
	if err := control.WriteDiscovery(dir, control.Discovery{
		Addr: strings.TrimPrefix(ts.URL, "http://"), Key: s.APIKey(), PID: os.Getpid(), Version: "x",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveTarget(context.Background(), env(nil), dir); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTarget_Missing(t *testing.T) {
	_, err := resolveTarget(context.Background(), env(nil), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveTarget_StalePID(t *testing.T) {
	_, ts := fakeApp(t)
	dir := t.TempDir()
	// A pid that cannot be alive: pid_max is far below this on every platform.
	control.WriteDiscovery(dir, control.Discovery{Addr: strings.TrimPrefix(ts.URL, "http://"), Key: "k", PID: 1 << 30})
	_, err := resolveTarget(context.Background(), env(nil), dir)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveTarget_WrongKey(t *testing.T) {
	_, ts := fakeApp(t)
	dir := t.TempDir()
	control.WriteDiscovery(dir, control.Discovery{Addr: strings.TrimPrefix(ts.URL, "http://"), Key: "wrong", PID: os.Getpid()})
	_, err := resolveTarget(context.Background(), env(nil), dir)
	if err == nil || !strings.Contains(err.Error(), "rejected the control key") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveTarget_Unreachable(t *testing.T) {
	dir := t.TempDir()
	control.WriteDiscovery(dir, control.Discovery{Addr: "127.0.0.1:1", Key: "k", PID: os.Getpid()})
	_, err := resolveTarget(context.Background(), env(nil), dir)
	if err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Fatalf("err = %v", err)
	}
}

// TestLazyBackend_SurvivesAppRestart drives one lazyBackend across an app
// restart: app A serves, exits (port and key gone, discovery file removed),
// then app B starts with a fresh port and key. The same backend must fail
// clearly in the gap and succeed against B without any reconstruction.
func TestLazyBackend_SurvivesAppRestart(t *testing.T) {
	dir := t.TempDir()
	backend := &lazyBackend{getenv: env(nil), dir: dir}
	ctx := context.Background()

	serveConnections := func(s *control.Server, name string) {
		go func() {
			for cmd := range s.Commands() {
				data, _ := json.Marshal([]control.ConnectionInfo{{Name: name, Kind: "memory", Active: true}})
				cmd.Respond(control.Result{OK: true, Data: data})
			}
		}()
	}
	writeDiscovery := func(s *control.Server, ts *httptest.Server) {
		t.Helper()
		err := control.WriteDiscovery(dir, control.Discovery{
			Addr: strings.TrimPrefix(ts.URL, "http://"), Key: s.APIKey(), PID: os.Getpid(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	sA, tsA := fakeApp(t)
	serveConnections(sA, "A")
	writeDiscovery(sA, tsA)
	conns, err := backend.Connections(ctx, false)
	if err != nil || len(conns) != 1 || conns[0].Name != "A" {
		t.Fatalf("app A: conns=%v err=%v", conns, err)
	}

	tsA.Close()
	os.Remove(control.DiscoveryPath(dir))
	if _, err := backend.Connections(ctx, false); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("after app A exit: err=%v", err)
	}

	sB, tsB := fakeApp(t)
	serveConnections(sB, "B")
	writeDiscovery(sB, tsB)
	conns, err = backend.Connections(ctx, false)
	if err != nil || len(conns) != 1 || conns[0].Name != "B" {
		t.Fatalf("app B: conns=%v err=%v", conns, err)
	}
}

func TestConfigSnippet(t *testing.T) {
	var cfg struct {
		Servers map[string]struct{ Command string } `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(configSnippet()), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Servers["bufflehead"].Command == "" {
		t.Errorf("snippet = %s", configSnippet())
	}
}
