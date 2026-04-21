package ui

import (
	"fmt"
	"time"

	"bufflehead/internal/control"
	"bufflehead/internal/db"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// DBRequestKind identifies the type of database request.
type DBRequestKind int

const (
	ReqQuery       DBRequestKind = iota // Execute a paginated query
	ReqOpenFile                         // Open a file: Schema + Metadata + Query
	ReqOpenDB                           // Open a .duckdb file: OpenDB + Tables + TableSchema
	ReqOpenGateway                      // Open a Postgres gateway connection
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
	Querier    db.Querier
	Tables     []db.TableInfo
	Err        error
	ControlCmd *control.Command
	FilePath   string
	DBPath     string
	UserSQL    string
	VirtualSQL string
}

// ConnWorker owns a db.Querier handle and processes requests sequentially.
type ConnWorker struct {
	db       db.Querier
	requests chan DBRequest
	results  chan DBResult
	done     chan struct{}
}

// NewConnWorker creates a worker for the given database connection.
// Results are sent to the shared results channel.
func NewConnWorker(database db.Querier, results chan DBResult) *ConnWorker {
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
	// handleOpenFile requires DuckDB-specific methods (Schema, Metadata).
	duck, ok := cw.db.(*db.DB)
	if !ok {
		cw.results <- DBResult{
			Kind:       ReqOpenFile,
			TabID:      req.TabID,
			Generation: req.Generation,
			Err:        fmt.Errorf("open file requires DuckDB connection"),
			ControlCmd: req.ControlCmd,
			FilePath:   req.FilePath,
			UserSQL:    req.UserSQL,
		}
		return
	}

	// Step 1: Schema
	cols, err := duck.Schema(req.FilePath)
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
	meta, _ := duck.Metadata(req.FilePath)

	// Step 3: Query
	start := time.Now()
	result, queryErr := duck.Query(req.VirtualSQL, req.Offset, req.Limit)
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
			Querier:    dbConn,
			Tables:     tables,
			ControlCmd: cmd,
			DBPath:     dbPath,
		}
	}()
}

// RunOpenGateway connects to a Postgres database in a one-shot goroutine,
// listing tables and schemas, then sending the result.
// If awsCfg is non-nil, IAM auth is used (rdsEndpoint is the real RDS host:port
// for token generation, while host:port is the local tunnel endpoint).
// statusFunc is an optional callback for progress updates (may be nil).
func RunOpenGateway(host string, port int, rdsEndpoint, dbName, user, password string,
	awsCfg *aws.Config, tabID, generation uint64, results chan DBResult, statusFunc func(string)) {
	go func() {
		if statusFunc != nil {
			statusFunc("Connecting to database...")
		}

		// Use a timeout so we don't hang forever if the tunnel isn't relaying data
		type pgResult struct {
			conn *db.PostgresDB
			err  error
		}
		ch := make(chan pgResult, 1)
		go func() {
			var pgConn *db.PostgresDB
			var err error
			if awsCfg != nil {
				pgConn, err = db.NewPostgresIAM(host, port, rdsEndpoint, dbName, user, *awsCfg)
			} else {
				pgConn, err = db.NewPostgres(host, port, dbName, user, password)
			}
			ch <- pgResult{pgConn, err}
		}()

		var pgConn *db.PostgresDB
		var err error
		select {
		case r := <-ch:
			pgConn, err = r.conn, r.err
		case <-time.After(30 * time.Second):
			err = fmt.Errorf("connection timed out after 30s — tunnel may not be forwarding data")
		}

		if err != nil {
			results <- DBResult{
				Kind:       ReqOpenGateway,
				TabID:      tabID,
				Generation: generation,
				Err:        err,
			}
			return
		}

		if statusFunc != nil {
			statusFunc("Loading tables...")
		}

		tables, err := pgConn.Tables()
		if err != nil {
			pgConn.Close()
			results <- DBResult{
				Kind:       ReqOpenGateway,
				TabID:      tabID,
				Generation: generation,
				Err:        err,
			}
			return
		}

		if statusFunc != nil {
			statusFunc(fmt.Sprintf("Loading schema for %d tables...", len(tables)))
		}

		for i := range tables {
			cols, _ := pgConn.TableSchema(tables[i].Name)
			tables[i].Columns = cols
		}

		results <- DBResult{
			Kind:       ReqOpenGateway,
			TabID:      tabID,
			Generation: generation,
			Querier:    pgConn,
			Tables:     tables,
		}
	}()
}
