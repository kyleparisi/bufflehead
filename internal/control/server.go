package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
)

// Command represents an action to execute on the main thread.
type Command struct {
	Action string          `json:"action"` // open, sort, query, page, reset_sort
	Data   json.RawMessage `json:"data,omitempty"`
	result chan Result
}

// Result is returned after the main thread processes a command.
type Result struct {
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	RawBytes []byte          `json:"-"` // for binary responses like screenshots
}

// OpenData is the payload for the "open" action.
type OpenData struct {
	Path string `json:"path"`
}

// SortData is the payload for the "sort" action.
type SortData struct {
	Column int `json:"column"`
}

// QueryData is the payload for the "query" action.
type QueryData struct {
	SQL string `json:"sql"`
}

// PageData is the payload for the "page" action.
type PageData struct {
	Offset int `json:"offset"`
}

// ResizeData is the payload for the "ui_tree" action (optional resize before capture).
type ResizeData struct {
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	Scale  float64 `json:"scale,omitempty"`
}

// SQLRequest is the payload for the direct /sql endpoint.
type SQLRequest struct {
	SQL        string `json:"sql"`
	Connection string `json:"connection,omitempty"` // connection name (default: active connection)
	Limit      int    `json:"limit,omitempty"`      // max rows (default: 100)
}

// SQLResult is the response from the /sql endpoint.
type SQLResult struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
	Total   int64      `json:"total"`
	Error   string     `json:"error,omitempty"`
}

// SQLExecutor runs a SQL query against a named connection and returns results.
// The context is derived from the HTTP request so the query cancels if the client disconnects.
type SQLExecutor func(ctx context.Context, connName, sql string, limit int) (*SQLResult, error)

// S3GetObjectRequest is the payload for the /s3/get-object endpoint.
type S3GetObjectRequest struct {
	Bucket     string `json:"bucket"`
	Key        string `json:"key"`
	Region     string `json:"region,omitempty"`     // override region (default: gateway region)
	Connection string `json:"connection,omitempty"` // connection name (default: active connection)
	MaxBytes   int64  `json:"max_bytes,omitempty"`  // max bytes to read (default: 10MB)
}

// S3GetObjectResult is the response from the /s3/get-object endpoint.
type S3GetObjectResult struct {
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Truncated   bool   `json:"truncated"`
	Error       string `json:"error,omitempty"`
}

// S3Executor fetches an S3 object using credentials from a named connection.
type S3Executor func(req S3GetObjectRequest) (*S3GetObjectResult, error)

// StateProvider returns the current app state as JSON.
type StateProvider func() (json.RawMessage, error)

// Server is the HTTP control server.
type Server struct {
	commands      chan *Command
	stateProvider StateProvider
	sqlExecutor   SQLExecutor
	s3Executor    S3Executor
	port          int
	mu            sync.Mutex
}

// New creates a control server on the given port.
func New(port int) *Server {
	return &Server{
		commands: make(chan *Command, 16),
		port:     port,
	}
}

// SetStateProvider sets the callback for GET /state.
func (s *Server) SetStateProvider(fn StateProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateProvider = fn
}

// SetSQLExecutor sets the callback for POST /sql.
func (s *Server) SetSQLExecutor(fn SQLExecutor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sqlExecutor = fn
}

// SetS3Executor sets the callback for POST /s3/get-object.
func (s *Server) SetS3Executor(fn S3Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s3Executor = fn
}

// Commands returns the channel the main loop reads from.
func (s *Server) Commands() <-chan *Command {
	return s.commands
}

// Respond sends a result back to the HTTP handler.
func (c *Command) Respond(r Result) {
	c.result <- r
}

// buildMux creates the HTTP handler with all routes registered.
func buildMux(s *Server) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		sp := s.stateProvider
		s.mu.Unlock()

		if sp == nil {
			http.Error(w, "no state provider", 500)
			return
		}
		data, err := sp()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	mux.HandleFunc("POST /open", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "open")
	})

	mux.HandleFunc("POST /sort", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "sort")
	})

	mux.HandleFunc("POST /query", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "query")
	})

	mux.HandleFunc("POST /page", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "page")
	})

	mux.HandleFunc("POST /reset-sort", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "reset_sort")
	})

	mux.HandleFunc("POST /new-tab", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "new_tab")
	})

	mux.HandleFunc("POST /close-tab", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "close_tab")
	})

	mux.HandleFunc("POST /new-window", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "new_window")
	})

	mux.HandleFunc("POST /select-row", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "select_row")
	})

	mux.HandleFunc("POST /search-detail", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "search_detail")
	})

	mux.HandleFunc("POST /deselect-all", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "deselect_all")
	})

	mux.HandleFunc("POST /nav-back", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "nav_back")
	})

	mux.HandleFunc("POST /nav-forward", func(w http.ResponseWriter, r *http.Request) {
		s.handleCommand(w, r, "nav_forward")
	})

	mux.HandleFunc("GET /screenshot", func(w http.ResponseWriter, r *http.Request) {
		cmd := &Command{
			Action: "screenshot",
			result: make(chan Result, 1),
		}
		s.commands <- cmd
		res := <-cmd.result
		if !res.OK {
			http.Error(w, res.Error, 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(res.RawBytes)
	})

	mux.HandleFunc("GET /ui-tree", func(w http.ResponseWriter, r *http.Request) {
		var rd ResizeData
		if ws := r.URL.Query().Get("width"); ws != "" {
			rd.Width, _ = strconv.Atoi(ws)
		}
		if hs := r.URL.Query().Get("height"); hs != "" {
			rd.Height, _ = strconv.Atoi(hs)
		}
		if ss := r.URL.Query().Get("scale"); ss != "" {
			rd.Scale, _ = strconv.ParseFloat(ss, 64)
		}
		var data json.RawMessage
		if rd.Width > 0 || rd.Height > 0 || rd.Scale > 0 {
			data, _ = json.Marshal(rd)
		}
		cmd := &Command{
			Action: "ui_tree",
			Data:   data,
			result: make(chan Result, 1),
		}
		s.commands <- cmd
		res := <-cmd.result
		if !res.OK {
			http.Error(w, res.Error, 500)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(res.RawBytes)
	})

	mux.HandleFunc("POST /s3/get-object", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		executor := s.s3Executor
		s.mu.Unlock()

		if executor == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(S3GetObjectResult{Error: "no s3 executor configured"})
			return
		}

		var req S3GetObjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(S3GetObjectResult{Error: "bad json: " + err.Error()})
			return
		}
		if req.Bucket == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(S3GetObjectResult{Error: "bucket is required"})
			return
		}
		if req.Key == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(S3GetObjectResult{Error: "key is required"})
			return
		}

		result, err := executor(req)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(S3GetObjectResult{Error: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})

	// Limit to 2 concurrent /sql requests (1 processing + 1 queued).
	// Additional requests get 429 so agents back off instead of
	// flooding the worker queue and starving UI operations.
	sqlSem := make(chan struct{}, 2)

	mux.HandleFunc("POST /sql", func(w http.ResponseWriter, r *http.Request) {
		select {
		case sqlSem <- struct{}{}:
			defer func() { <-sqlSem }()
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(429)
			json.NewEncoder(w).Encode(SQLResult{Error: "too many concurrent SQL requests, retry later"})
			return
		}

		s.mu.Lock()
		executor := s.sqlExecutor
		s.mu.Unlock()

		if executor == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(SQLResult{Error: "no sql executor configured"})
			return
		}

		var req SQLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(SQLResult{Error: "bad json: " + err.Error()})
			return
		}
		if req.SQL == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(SQLResult{Error: "sql is required"})
			return
		}
		if req.Limit <= 0 {
			req.Limit = 100
		}

		// No server-side timeout — the agent manages its own timeouts
		// by disconnecting, which baseCtx detects and cancels the query.
		result, err := executor(r.Context(), req.Connection, req.SQL, req.Limit)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(SQLResult{Error: err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})

	return mux
}

// Start launches the HTTP server in a goroutine.
func (s *Server) Start() {
	mux := buildMux(s)
	go func() {
		addr := fmt.Sprintf("127.0.0.1:%d", s.port)
		fmt.Printf("Control server: http://%s\n", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			fmt.Printf("Control server error: %v\n", err)
		}
	}()
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request, action string) {
	var body json.RawMessage
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), 400)
			return
		}
	}

	cmd := &Command{
		Action: action,
		Data:   body,
		result: make(chan Result, 1),
	}

	s.commands <- cmd
	res := <-cmd.result

	w.Header().Set("Content-Type", "application/json")
	if !res.OK {
		w.WriteHeader(400)
	}
	json.NewEncoder(w).Encode(res)
}
