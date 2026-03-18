package ui

import (
	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"time"
)

// DBRequestKind identifies the type of database request.
type DBRequestKind int

const (
	ReqQuery    DBRequestKind = iota // Execute a paginated query
	ReqOpenFile                      // Open a file: Schema + Metadata + Query
	ReqOpenDB                        // Open a .duckdb file: OpenDB + Tables + TableSchema
)

// DBRequest is sent to a ConnWorker for processing.
type DBRequest struct {
	Kind       DBRequestKind
	VirtualSQL string
	UserSQL    string
	FilePath   string
	DBPath     string
	Offset     int
	Limit      int
	TabID      uint64
	Generation uint64
	Navigating bool
	ControlCmd *control.Command
}

// DBResult is sent back from a ConnWorker after processing a request.
type DBResult struct {
	Kind       DBRequestKind
	TabID      uint64
	Generation uint64
	Navigating bool
	Elapsed    time.Duration
	Query      *db.QueryResult
	Schema     []db.Column
	Metadata   map[string]string
	DB         *db.DB
	Tables     []db.TableInfo
	Err        error
	ControlCmd *control.Command
	FilePath   string
	DBPath     string
	UserSQL    string
	VirtualSQL string
}

// ConnWorker owns a *db.DB handle and processes requests sequentially.
type ConnWorker struct {
	db       *db.DB
	requests chan DBRequest
	results  chan DBResult
	done     chan struct{}
}

// NewConnWorker creates a worker for the given database connection.
// Results are sent to the shared results channel.
func NewConnWorker(database *db.DB, results chan DBResult) *ConnWorker {
	return &ConnWorker{
		db:       database,
		requests: make(chan DBRequest, 16),
		results:  results,
		done:     make(chan struct{}),
	}
}

// Start launches the worker goroutine.
func (cw *ConnWorker) Start() {
	go cw.loop()
}

// Stop signals the worker to shut down and waits for it to finish.
func (cw *ConnWorker) Stop() {
	close(cw.requests)
	<-cw.done
}

// Send enqueues a request for the worker.
func (cw *ConnWorker) Send(req DBRequest) {
	cw.requests <- req
}

func (cw *ConnWorker) loop() {
	defer close(cw.done)
	for req := range cw.requests {
		cw.handle(req)
	}
}

func (cw *ConnWorker) handle(req DBRequest) {
	switch req.Kind {
	case ReqQuery:
		cw.handleQuery(req)
	case ReqOpenFile:
		cw.handleOpenFile(req)
	}
}

func (cw *ConnWorker) handleQuery(req DBRequest) {
	start := time.Now()
	result, err := cw.db.Query(req.VirtualSQL, req.Offset, req.Limit)
	elapsed := time.Since(start)

	cw.results <- DBResult{
		Kind:       ReqQuery,
		TabID:      req.TabID,
		Generation: req.Generation,
		Navigating: req.Navigating,
		Elapsed:    elapsed,
		Query:      result,
		Err:        err,
		ControlCmd: req.ControlCmd,
		VirtualSQL: req.VirtualSQL,
		FilePath:   req.FilePath,
		UserSQL:    req.UserSQL,
	}
}

func (cw *ConnWorker) handleOpenFile(req DBRequest) {
	// Step 1: Schema
	cols, err := cw.db.Schema(req.FilePath)
	if err != nil {
		cw.results <- DBResult{
			Kind:       ReqOpenFile,
			TabID:      req.TabID,
			Generation: req.Generation,
			Err:        err,
			ControlCmd: req.ControlCmd,
			FilePath:   req.FilePath,
			UserSQL:    req.UserSQL,
		}
		return
	}

	// Step 2: Metadata (optional — continue on error)
	meta, _ := cw.db.Metadata(req.FilePath)

	// Step 3: Query
	start := time.Now()
	result, queryErr := cw.db.Query(req.VirtualSQL, req.Offset, req.Limit)
	elapsed := time.Since(start)

	cw.results <- DBResult{
		Kind:       ReqOpenFile,
		TabID:      req.TabID,
		Generation: req.Generation,
		Navigating: req.Navigating,
		Elapsed:    elapsed,
		Query:      result,
		Schema:     cols,
		Metadata:   meta,
		Err:        queryErr,
		ControlCmd: req.ControlCmd,
		FilePath:   req.FilePath,
		UserSQL:    req.UserSQL,
		VirtualSQL: req.VirtualSQL,
	}
}

// RunOpenDB executes the OpenDB workflow in a one-shot goroutine,
// sending the result to the provided results channel.
// This is used for .duckdb files where no worker exists yet.
func RunOpenDB(dbPath string, tabID, generation uint64, cmd *control.Command, results chan DBResult) {
	go func() {
		dbConn, err := db.OpenDB(dbPath)
		if err != nil {
			results <- DBResult{
				Kind:       ReqOpenDB,
				TabID:      tabID,
				Generation: generation,
				Err:        err,
				ControlCmd: cmd,
				DBPath:     dbPath,
			}
			return
		}

		tables, err := dbConn.Tables()
		if err != nil {
			dbConn.Close()
			results <- DBResult{
				Kind:       ReqOpenDB,
				TabID:      tabID,
				Generation: generation,
				Err:        err,
				ControlCmd: cmd,
				DBPath:     dbPath,
			}
			return
		}

		for i := range tables {
			cols, _ := dbConn.TableSchema(tables[i].Name)
			tables[i].Columns = cols
		}

		results <- DBResult{
			Kind:       ReqOpenDB,
			TabID:      tabID,
			Generation: generation,
			DB:         dbConn,
			Tables:     tables,
			ControlCmd: cmd,
			DBPath:     dbPath,
		}
	}()
}
