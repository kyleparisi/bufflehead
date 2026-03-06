package control

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
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

// StateProvider returns the current app state as JSON.
type StateProvider func() (json.RawMessage, error)

// Server is the HTTP control server.
type Server struct {
	commands      chan *Command
	stateProvider StateProvider
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

// Commands returns the channel the main loop reads from.
func (s *Server) Commands() <-chan *Command {
	return s.commands
}

// Respond sends a result back to the HTTP handler.
func (c *Command) Respond(r Result) {
	c.result <- r
}

// Start launches the HTTP server in a goroutine.
func (s *Server) Start() {
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
