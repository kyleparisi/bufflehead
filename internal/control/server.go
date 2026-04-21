package control

import (
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
type SQLExecutor func(connName, sql string, limit int) (*SQLResult, error)

// StateProvider returns the current app state as JSON.
type StateProvider func() (json.RawMessage, error)

// Server is the HTTP control server.
type Server struct {
	commands      chan *Command
	stateProvider StateProvider
	sqlExecutor   SQLExecutor
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

	mux.HandleFunc("POST /sql", func(w http.ResponseWriter, r *http.Request) {
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

		result, err := executor(req.Connection, req.SQL, req.Limit)
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
