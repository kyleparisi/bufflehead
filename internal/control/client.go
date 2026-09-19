package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client drives a running Bufflehead's control server over HTTP. It has the
// same method set as *Server so the MCP tool set (internal/mcpserver) can run
// either in-process or from the stdio bridge without adapters.
type Client struct {
	BaseURL string // e.g. http://127.0.0.1:54321
	Key     string
	HTTP    *http.Client
}

// NewClient returns a client for the control server at addr (host:port) that
// presents key on every request.
func NewClient(addr, key string) *Client {
	base := addr
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	return &Client{
		BaseURL: strings.TrimRight(base, "/"),
		Key:     key,
		// No overall timeout: queries have none server-side either; callers
		// bound them with ctx, which cancels the query on disconnect.
		HTTP: &http.Client{},
	}
}

// ErrUnauthorized is returned when the server rejects the bearer key.
var ErrUnauthorized = errors.New("unauthorized: control key rejected")

// do issues one request and decodes a JSON body into out (when out != nil).
// Error bodies are surfaced by their "error" field.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusTooManyRequests:
		return ErrBusy
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

// Ping checks the server is up and the key is accepted, bounded by a short
// timeout so a stale discovery file fails fast.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := c.State(ctx)
	return err
}

// State returns the raw /state snapshot.
func (c *Client) State(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/state", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Connections lists open connections (GET /connections).
func (c *Client) Connections(ctx context.Context, includeColumns bool) ([]ConnectionInfo, error) {
	path := "/connections"
	if includeColumns {
		path += "?columns=1"
	}
	var conns []ConnectionInfo
	if err := c.do(ctx, http.MethodGet, path, nil, &conns); err != nil {
		return nil, err
	}
	if conns == nil {
		conns = []ConnectionInfo{}
	}
	return conns, nil
}

// ExecSQL runs a query (POST /sql). HTTP 429 comes back as ErrBusy.
func (c *Client) ExecSQL(ctx context.Context, req SQLRequest) (*SQLResult, error) {
	var res SQLResult
	if err := c.do(ctx, http.MethodPost, "/sql", req, &res); err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return &res, nil
}

// CancelSQL cancels the running query on a connection (POST /sql/cancel).
func (c *Client) CancelSQL(ctx context.Context, conn string) error {
	return c.do(ctx, http.MethodPost, "/sql/cancel", CancelRequest{Connection: conn}, nil)
}

// GetS3Object fetches an object with a connection's credentials (POST /s3/get-object).
func (c *Client) GetS3Object(ctx context.Context, req S3GetObjectRequest) (*S3GetObjectResult, error) {
	var res S3GetObjectResult
	if err := c.do(ctx, http.MethodPost, "/s3/get-object", req, &res); err != nil {
		return nil, err
	}
	if res.Error != "" {
		return nil, errors.New(res.Error)
	}
	return &res, nil
}

// Reconnect rebuilds a connection (POST /reconnect) and returns the step report.
func (c *Client) Reconnect(ctx context.Context, conn string) (*ReconnectResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/reconnect",
		bytes.NewReader(mustJSON(ReconnectData{Connection: conn})))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	// /reconnect answers with the Result envelope and 400 when any step failed,
	// but the step report in Data is still the useful payload — decode it either way.
	var res Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("POST /reconnect: decode: %w", err)
	}
	var rr ReconnectResult
	if len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &rr); err != nil {
			return nil, fmt.Errorf("POST /reconnect: decode steps: %w", err)
		}
	}
	if !res.OK && rr.Connection == "" {
		return nil, errors.New(res.Error)
	}
	return &rr, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
