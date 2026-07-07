package control

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// drainCommands consumes commands from the server and responds with the given
// result, so endpoints that dispatch to the "main thread" can be exercised in
// tests. It returns a function to fetch the last command the handler received.
func drainCommands(s *Server, resp Result) func() *Command {
	last := make(chan *Command, 1)
	go func() {
		cmd := <-s.commands
		last <- cmd
		cmd.Respond(resp)
	}()
	return func() *Command {
		select {
		case c := <-last:
			return c
		default:
			return nil
		}
	}
}

func TestReconnectEndpoint_DispatchesCommand(t *testing.T) {
	s := New(0)
	handler := buildMux(s)

	getLast := drainCommands(s, Result{OK: true})

	body := `{"connection":"prod"}`
	req := httptest.NewRequest("POST", "/reconnect", strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	cmd := getLast()
	if cmd == nil {
		t.Fatal("expected a command to be dispatched")
	}
	if cmd.Action != "reconnect" {
		t.Errorf("expected action 'reconnect', got %q", cmd.Action)
	}
	var d ReconnectData
	if err := json.Unmarshal(cmd.Data, &d); err != nil {
		t.Fatalf("unmarshal command data: %v", err)
	}
	if d.Connection != "prod" {
		t.Errorf("expected connection 'prod', got %q", d.Connection)
	}
}

func TestReconnectEndpoint_ReturnsSteps(t *testing.T) {
	s := New(0)
	handler := buildMux(s)

	// Simulate a failed reconnect: tunnel came up but the DB connect failed.
	payload := ReconnectResult{
		Connection: "prod",
		OK:         false,
		Steps: []ReconnectStep{
			{Step: "cancel_queries", OK: true},
			{Step: "close_db", OK: true},
			{Step: "stop_tunnel", OK: true},
			{Step: "start_tunnel", OK: true},
			{Step: "connect_db", OK: false, Error: "connection timed out after 30s"},
		},
	}
	data, _ := json.Marshal(payload)
	drainCommands(s, Result{OK: false, Data: data})

	req := httptest.NewRequest("POST", "/reconnect", strings.NewReader(`{"connection":"prod"}`))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// A failed reconnect maps to HTTP 400 but still returns the step detail.
	if w.Code != 400 {
		t.Fatalf("expected 400 for failed reconnect, got %d", w.Code)
	}
	var res Result
	if err := json.NewDecoder(w.Body).Decode(&res); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if res.OK {
		t.Error("expected OK=false")
	}
	var got ReconnectResult
	if err := json.Unmarshal(res.Data, &got); err != nil {
		t.Fatalf("unmarshal reconnect result: %v", err)
	}
	if len(got.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %d", len(got.Steps))
	}
	final := got.Steps[len(got.Steps)-1]
	if final.Step != "connect_db" || final.OK || !strings.Contains(final.Error, "timed out") {
		t.Errorf("unexpected final step: %+v", final)
	}
}

func TestFormatReconnectSteps_Success(t *testing.T) {
	steps := []ReconnectStep{
		{Step: "cancel_queries", OK: true},
		{Step: "start_tunnel", OK: true},
		{Step: "connect_db", OK: true},
	}
	out := FormatReconnectSteps("prod", true, steps, nil)

	if !strings.Contains(out, "Reconnected prod successfully") {
		t.Errorf("missing success header: %q", out)
	}
	// Machine step names are humanized.
	if !strings.Contains(out, "Connect to database") {
		t.Errorf("step label not humanized: %q", out)
	}
	if strings.Contains(out, "refresh again to retry") {
		t.Errorf("should not show retry hint on success: %q", out)
	}
}

func TestFormatReconnectSteps_Failure(t *testing.T) {
	steps := []ReconnectStep{
		{Step: "cancel_queries", OK: true},
		{Step: "connect_db", OK: false, Error: "connection timed out after 30s"},
	}
	out := FormatReconnectSteps("prod", false, steps, nil)

	if !strings.Contains(out, "Reconnecting prod failed") {
		t.Errorf("missing failure header: %q", out)
	}
	if !strings.Contains(out, "connection timed out after 30s") {
		t.Errorf("raw error not shown: %q", out)
	}
	if !strings.Contains(out, "refresh again to retry") {
		t.Errorf("missing retry hint on failure: %q", out)
	}
}

func TestFormatReconnectSteps_FriendlyAuthError(t *testing.T) {
	steps := []ReconnectStep{
		{Step: "refresh_credentials", OK: false, Error: "InvalidGrantException: token expired"},
	}
	// friendly recognizes the error and rewrites it.
	friendly := func(msg string) (string, bool) {
		return "Your login has expired. Log in again.", true
	}
	out := FormatReconnectSteps("prod", false, steps, friendly)

	if !strings.Contains(out, "Your login has expired") {
		t.Errorf("friendly message not used: %q", out)
	}
	if strings.Contains(out, "InvalidGrantException") {
		t.Errorf("raw error leaked despite friendly rewrite: %q", out)
	}
}

func TestFormatReconnectSteps_UnknownStepName(t *testing.T) {
	steps := []ReconnectStep{{Step: "mystery_step", OK: true}}
	out := FormatReconnectSteps("prod", true, steps, nil)
	// Falls back to the raw step name when no label exists.
	if !strings.Contains(out, "mystery_step") {
		t.Errorf("unknown step name not shown: %q", out)
	}
}

func TestReconnectEndpoint_EmptyBody(t *testing.T) {
	s := New(0)
	handler := buildMux(s)

	getLast := drainCommands(s, Result{OK: true})

	// No body → reconnect the active connection (index 0 semantics resolved
	// on the app side). Endpoint must still dispatch cleanly.
	req := httptest.NewRequest("POST", "/reconnect", strings.NewReader(""))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if cmd := getLast(); cmd == nil || cmd.Action != "reconnect" {
		t.Fatalf("expected reconnect command, got %+v", cmd)
	}
}
