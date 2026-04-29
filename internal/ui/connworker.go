package ui

import (
	"context"
	"fmt"
	"log"
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
	ReqSQL                              // Raw SQL via /sql endpoint (synchronous response)
	ReqRefresh                          // Re-fetch tables and schemas
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
	ConnIdx    int // connection index (for ReqRefresh)
	Navigating bool
	ControlCmd *control.Command
	SQLReply   chan SQLReply    // for ReqSQL: synchronous response channel
	Ctx        context.Context // for ReqSQL: cancels when HTTP client disconnects
}

// SQLReply is the synchronous response for ReqSQL requests.
type SQLReply struct {
	Result *db.QueryResult
	Err    error
}

// DBResult is sent back from a ConnWorker after processing a request.
type DBResult struct {
	Kind       DBRequestKind
	TabID      uint64
	Generation uint64
	ConnIdx    int // connection index (for ReqRefresh)
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
// UI operations (query, open, refresh) have priority over /sql requests.
type ConnWorker struct {
	db      db.Querier
	ctx     context.Context
	cancel  context.CancelFunc
	uiReqs  chan DBRequest // high-priority: ReqQuery, ReqOpenFile, ReqRefresh
	sqlReqs chan DBRequest // low-priority: ReqSQL
	results chan DBResult
	done    chan struct{}
}

// NewConnWorker creates a worker for the given database connection.
// Results are sent to the shared results channel.
func NewConnWorker(database db.Querier, results chan DBResult) *ConnWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &ConnWorker{
		db:      database,
		ctx:     ctx,
		cancel:  cancel,
		uiReqs:  make(chan DBRequest, 16),
		sqlReqs: make(chan DBRequest, 4),
		results: results,
		done:    make(chan struct{}),
	}
}

// Start launches the worker goroutine.
func (cw *ConnWorker) Start() {
	go cw.loop()
}

// Stop cancels in-flight operations, signals the worker to shut down,
// and waits briefly for it to finish. This prevents the app from
// hanging on quit when a long-running query is in progress.
func (cw *ConnWorker) Stop() {
	cw.cancel()
	select {
	case <-cw.done:
	case <-time.After(3 * time.Second):
	}
}

// Send enqueues a request for the worker. UI operations (query, open,
// refresh) go to the high-priority channel; /sql requests go to the
// low-priority channel.
func (cw *ConnWorker) Send(req DBRequest) {
	if req.Kind == ReqSQL {
		cw.sqlReqs <- req
	} else {
		cw.uiReqs <- req
	}
}

func (cw *ConnWorker) loop() {
	defer close(cw.done)
	for {
		// Priority 1: always check UI requests first.
		select {
		case req := <-cw.uiReqs:
			cw.handle(req)
			continue
		default:
		}

		// Priority 2: take from either channel, preferring UI.
		select {
		case req := <-cw.uiReqs:
			cw.handle(req)
		case req := <-cw.sqlReqs:
			// Skip if the HTTP client already disconnected.
			if req.Ctx != nil {
				select {
				case <-req.Ctx.Done():
					log.Println("sql: skipping queued request, client already disconnected")
					req.SQLReply <- SQLReply{Err: req.Ctx.Err()}
					continue
				default:
				}
			}
			cw.handle(req)
		case <-cw.ctx.Done():
			return
		}
	}
}

// baseCtx returns a context that cancels when either the worker shuts down
// or, for ReqSQL, the HTTP client disconnects.
func (cw *ConnWorker) baseCtx(req DBRequest) (context.Context, context.CancelFunc) {
	if req.Kind == ReqSQL && req.Ctx != nil {
		ctx, cancel := context.WithCancel(cw.ctx)
		go func() {
			select {
			case <-req.Ctx.Done():
				log.Println("sql: client disconnected, cancelling request")
				cancel()
			case <-ctx.Done():
			}
		}()
		return ctx, cancel
	}
	return cw.ctx, func() {}
}

func (cw *ConnWorker) handle(req DBRequest) {
	// Refresh skips the health check — Tables() will surface errors directly,
	// and the ping itself can hang on dead SSM tunnel connections.
	if req.Kind == ReqRefresh {
		cw.handleRefresh(req)
		return
	}

	// For ReqSQL, derive a context that also cancels when the HTTP client
	// disconnects. This prevents queued requests from blocking the worker
	// after the agent has given up.
	ctx, ctxCancel := cw.baseCtx(req)
	defer ctxCancel()

	// Health check before every operation to fail fast on dead connections.
	// SSM tunnels can drop and reconnect, so retry with backoff to give
	// the tunnel time to re-establish (tunnel backoff is 1s, 2s, 4s, 8s...).
	var err error
	const maxHealthRetries = 4
	for retries := 0; retries < maxHealthRetries; retries++ {
		if retries > 0 {
			if pgConn, ok := cw.db.(*db.PostgresDB); ok {
				pgConn.ResetPool()
			}
			// Back off to let the tunnel reconnect. 3s, 6s, 9s covers
			// the tunnel's first several reconnect attempts.
			wait := time.Duration(retries) * 3 * time.Second
			select {
			case <-time.After(wait):
			case <-ctx.Done():
			}
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
		}
		pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
		err = cw.db.Ping(pingCtx)
		pingCancel()
		if err == nil {
			break
		}
		log.Printf("sql: health check failed (attempt %d/%d): %v", retries+1, maxHealthRetries, err)
	}
	if err != nil {
		pingErr := fmt.Errorf("connection health check failed: %w", err)
		switch req.Kind {
		case ReqSQL:
			req.SQLReply <- SQLReply{Err: pingErr}
		default:
			cw.results <- DBResult{
				Kind:       req.Kind,
				TabID:      req.TabID,
				Generation: req.Generation,
				ConnIdx:    req.ConnIdx,
				Err:        pingErr,
				ControlCmd: req.ControlCmd,
				FilePath:   req.FilePath,
				DBPath:     req.DBPath,
				UserSQL:    req.UserSQL,
			}
		}
		return
	}

	switch req.Kind {
	case ReqQuery:
		cw.handleQuery(req)
	case ReqOpenFile:
		cw.handleOpenFile(req)
	case ReqSQL:
		cw.handleSQL(req, ctx)
	}
}

func (cw *ConnWorker) handleQuery(req DBRequest) {
	start := time.Now()
	result, err := cw.db.Query(cw.ctx, req.VirtualSQL, req.Offset, req.Limit)
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

func (cw *ConnWorker) handleSQL(req DBRequest, ctx context.Context) {
	result, err := cw.db.Query(ctx, req.UserSQL, 0, req.Limit)
	req.SQLReply <- SQLReply{Result: result, Err: err}

	// If the query was cancelled (HTTP timeout or client disconnect),
	// the connection may be in a dirty state. Reset the pool so the
	// next operation gets a clean connection.
	if err != nil && ctx.Err() != nil {
		if pgConn, ok := cw.db.(*db.PostgresDB); ok {
			log.Println("sql: resetting pool after cancelled query")
			pgConn.ResetPool()
		}
	}
}

func (cw *ConnWorker) handleRefresh(req DBRequest) {
	// Reset the pool so refresh gets a fresh connection, not a stale one
	// left over from a dead SSM tunnel.
	if pgConn, ok := cw.db.(*db.PostgresDB); ok {
		pgConn.ResetPool()
	}
	tables, err := cw.db.Tables()
	if err != nil {
		cw.results <- DBResult{
			Kind:    ReqRefresh,
			ConnIdx: req.ConnIdx,
			Err:     err,
		}
		return
	}

	// Bulk-load column schemas if the backend supports it (Postgres)
	if pgConn, ok := cw.db.(*db.PostgresDB); ok {
		if err := pgConn.AllTableSchemas(tables); err != nil {
			cw.results <- DBResult{
				Kind:    ReqRefresh,
				ConnIdx: req.ConnIdx,
				Err:     fmt.Errorf("load schemas: %w", err),
			}
			return
		}
	}

	cw.results <- DBResult{
		Kind:    ReqRefresh,
		ConnIdx: req.ConnIdx,
		Tables:  tables,
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
	result, queryErr := duck.Query(cw.ctx, req.VirtualSQL, req.Offset, req.Limit)
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

		if err := pgConn.AllTableSchemas(tables); err != nil {
			pgConn.Close()
			results <- DBResult{
				Kind:       ReqOpenGateway,
				TabID:      tabID,
				Generation: generation,
				Err:        fmt.Errorf("load schemas: %w", err),
			}
			return
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
