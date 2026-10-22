// Package connect opens saved database connections: it builds the SSH or SSM
// tunnel a bookmark needs, dials the database through it and loads the schema.
//
// It holds no UI state and does not import graphics.gd, so the window code
// (internal/ui) and the headless MCP mode (internal/mcpmode) share one
// connection pipeline.
package connect

import (
	"context"
	"fmt"
	"os"
	"time"

	bfaws "bufflehead/internal/aws"
	"bufflehead/internal/db"
	"bufflehead/internal/models"
	"bufflehead/internal/sshtun"

	"github.com/aws/aws-sdk-go-v2/aws"
	"google.golang.org/api/option"
)

// Conn is an open connection and the tunnels it runs through.
type Conn struct {
	DB     db.Querier
	Tables []db.TableInfo
	// Entry is the config the connection was opened with, carrying the local
	// end of any tunnel in LocalPort.
	Entry models.GatewayEntry
	SSH   *sshtun.Tunnel       // nil unless the entry uses an SSH jump host
	SSM   *bfaws.TunnelManager // nil unless the entry is an AWS gateway
}

// Close closes the database and stops any tunnel under it.
func (c *Conn) Close() {
	if c.DB != nil {
		c.DB.Close()
	}
	c.SSH.Stop() // nil-safe
	if c.SSM != nil {
		c.SSM.Stop()
	}
}

// Open connects to entry of any kind — BigQuery, direct Postgres/MySQL (with
// an optional SSH jump host) or an AWS gateway over SSM — and loads its tables
// and schemas. source labels BigQuery jobs (e.g. "ui", "mcp"). logf, if
// non-nil, receives progress messages.
func Open(ctx context.Context, entry models.GatewayEntry, source string, logf func(string)) (*Conn, error) {
	if entry.IsBigQuery() {
		bq, tables, err := BigQuery(entry, source)
		if err != nil {
			return nil, err
		}
		return &Conn{DB: bq, Tables: tables, Entry: entry}, nil
	}

	if entry.IsDirect() || entry.IsMySQL() {
		tunnel, updated, err := SSHTunnel(ctx, entry, logf)
		if err != nil {
			return nil, err
		}
		dbConn, tables, err := Direct(updated)
		if err != nil {
			tunnel.Stop()
			return nil, err
		}
		return &Conn{DB: dbConn, Tables: tables, Entry: updated, SSH: tunnel}, nil
	}

	// AWS gateway: load credentials first so an expired SSO login surfaces
	// before the tunnel is attempted.
	auth := bfaws.NewAuthManager(entry.AWSProfile, entry.AWSRegion)
	credCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := auth.EnsureConfig(credCtx)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("AWS credentials: %w", err)
	}
	var awsCfg *aws.Config
	if entry.UseIAMAuth() {
		cfg := auth.Config()
		awsCfg = &cfg
	}

	tunnel, updated, err := SSMTunnel(ctx, auth, entry, logf)
	if err != nil {
		return nil, err
	}
	rdsEndpoint := fmt.Sprintf("%s:%d", updated.RDSHost, updated.RDSPort)
	pgConn, tables, err := GatewayPostgres("127.0.0.1", updated.LocalPort, rdsEndpoint,
		updated.DBName, updated.DBUser, updated.ResolvePassword(), awsCfg)
	if err != nil {
		tunnel.Stop()
		return nil, err
	}
	return &Conn{DB: pgConn, Tables: tables, Entry: updated, SSM: tunnel}, nil
}

// SSHTunnel opens an SSH port-forward for a direct connection whose entry
// names a jump host, and returns the entry updated with the local end of the
// forward in LocalPort — so entry.DialTarget() then points the database driver
// at 127.0.0.1 instead of the unreachable database host.
//
// Entries with no tunnel pass through untouched (nil tunnel, nil error), so
// every direct-connect path can call this unconditionally.
func SSHTunnel(ctx context.Context, entry models.GatewayEntry, logf func(string)) (*sshtun.Tunnel, models.GatewayEntry, error) {
	if !entry.UsesSSH() {
		return nil, entry, nil
	}
	if err := entry.SSH.Validate(); err != nil {
		return nil, entry, err
	}

	cfg := sshtun.Config{
		Host:       entry.SSH.Host,
		Port:       entry.SSH.EffectivePort(),
		User:       entry.SSH.EffectiveUser(),
		Method:     string(entry.SSH.AuthMethod),
		KeyPath:    entry.SSH.KeyPath,
		Secret:     entry.ResolveSSHSecret(),
		RemoteHost: entry.RDSHost,
		RemotePort: entry.RDSPort,
	}

	tunnel, err := sshtun.Start(ctx, cfg, logf)
	if err != nil {
		return nil, entry, fmt.Errorf("SSH tunnel: %w", err)
	}
	entry.LocalPort = tunnel.LocalPort()
	return tunnel, entry, nil
}

// Direct opens a direct (non-AWS, non-BigQuery) database connection and loads
// its tables and schemas, picking the engine from the entry's Kind. The entry
// must already carry LocalPort when an SSH tunnel is in play — DialTarget reads
// it — so callers run SSHTunnel first.
func Direct(entry models.GatewayEntry) (db.Querier, []db.TableInfo, error) {
	if entry.IsMySQL() {
		return DirectMySQL(entry)
	}
	return DirectPostgres(entry)
}

// SSMTunnel allocates a local port, resolves the bastion instance, and starts
// + waits for an SSM tunnel for the given gateway entry. It returns the running
// tunnel and the entry updated with the assigned LocalPort. Cancelling ctx
// aborts the attempt and stops the tunnel.
//
// logf, if non-nil, receives human-readable progress messages. This is shared
// by the initial gateway connect flow and the reconnect flow so both build the
// tunnel identically (including spot-instance resolution on reconnect).
func SSMTunnel(ctx context.Context, auth *bfaws.AuthManager, entry models.GatewayEntry, logf func(string)) (*bfaws.TunnelManager, models.GatewayEntry, error) {
	log := func(msg string) {
		if logf != nil {
			logf(msg)
		}
	}

	log("Allocating port...")
	localPort, err := bfaws.FindFreePort()
	if err != nil {
		return nil, entry, fmt.Errorf("port allocation: %w", err)
	}
	entry.LocalPort = localPort

	log("Resolving bastion instance...")
	instanceID, err := auth.ResolveInstanceID(entry.InstanceID, entry.InstanceTags)
	if err != nil {
		return nil, entry, fmt.Errorf("instance resolution: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, entry, err
	}

	log("Starting SSM tunnel...")
	tunnel := bfaws.NewTunnelManager(func(status bfaws.TunnelStatus, msg string) {
		if status == bfaws.TunnelConnecting {
			log(msg)
		}
	})

	tunnelCfg := bfaws.TunnelConfig{
		InstanceID: instanceID,
		RDSHost:    entry.RDSHost,
		RDSPort:    entry.RDSPort,
		LocalPort:  localPort,
		AWSProfile: entry.AWSProfile,
		AWSRegion:  entry.AWSRegion,
	}

	// Instance resolver so reconnects can find a new bastion when spot
	// instances rotate.
	if len(entry.InstanceTags) > 0 {
		tunnelCfg.InstanceResolver = func() (string, error) {
			return auth.ResolveInstanceID("", entry.InstanceTags)
		}
	} else {
		tunnelCfg.InstanceResolver = func() (string, error) {
			instances, err := auth.ListSSMInstances()
			if err != nil {
				return "", err
			}
			switch len(instances) {
			case 0:
				return "", fmt.Errorf("no online SSM instances found")
			case 1:
				return instances[0].InstanceID, nil
			default:
				// Ambiguous — only the user can pick the right bastion, so
				// retrying is pointless. Fail the tunnel immediately.
				return "", &bfaws.PermanentError{Err: fmt.Errorf("%d SSM instances online — cannot auto-select bastion", len(instances))}
			}
		}
	}

	if err := tunnel.Start(tunnelCfg); err != nil {
		return nil, entry, fmt.Errorf("start tunnel: %w", err)
	}

	// Tear the tunnel down (which also halts its reconnect loop) as soon as
	// the caller cancels — e.g. the user hit Cancel on the connection card.
	stopOnCancel := context.AfterFunc(ctx, func() { tunnel.Stop() })
	defer stopOnCancel()

	log("Waiting for tunnel...")
	if err := tunnel.WaitReady(ctx, 60*time.Second); err != nil {
		tunnel.Stop()
		return nil, entry, fmt.Errorf("tunnel not ready: %w", err)
	}

	log("Tunnel connected")
	return tunnel, entry, nil
}

// GatewayPostgres opens a Postgres gateway connection and loads its tables and
// schemas, synchronously. It mirrors ui.RunOpenGateway's connect logic but
// returns directly instead of posting to a channel, so it can be used inside a
// caller's own goroutine (the reconnect flow).
func GatewayPostgres(host string, port int, rdsEndpoint, dbName, user, password string, awsCfg *aws.Config) (*db.PostgresDB, []db.TableInfo, error) {
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
		return nil, nil, fmt.Errorf("connection timed out after 30s — tunnel may not be forwarding data")
	}
	if err != nil {
		return nil, nil, err
	}

	tables, err := pgConn.Tables()
	if err != nil {
		pgConn.Close()
		return nil, nil, fmt.Errorf("load tables: %w", err)
	}
	if err := pgConn.AllTableSchemas(tables); err != nil {
		pgConn.Close()
		return nil, nil, fmt.Errorf("load schemas: %w", err)
	}
	return pgConn, tables, nil
}

// DirectPostgres opens a direct Postgres connection (no tunnel) and loads its
// tables and schemas synchronously. Mirrors GatewayPostgres for the direct
// Postgres connection type.
func DirectPostgres(entry models.GatewayEntry) (*db.PostgresDB, []db.TableInfo, error) {
	type pgResult struct {
		conn *db.PostgresDB
		err  error
	}
	host, port, err := entry.DialTarget()
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan pgResult, 1)
	go func() {
		pgConn, err := db.NewPostgresDirect(host, port,
			entry.DBName, entry.DBUser, entry.ResolvePassword(), entry.EffectiveSSLMode())
		ch <- pgResult{pgConn, err}
	}()

	var pgConn *db.PostgresDB
	select {
	case r := <-ch:
		pgConn, err = r.conn, r.err
	case <-time.After(30 * time.Second):
		return nil, nil, directTimeoutErr(entry)
	}
	if err != nil {
		return nil, nil, err
	}

	tables, err := pgConn.Tables()
	if err != nil {
		pgConn.Close()
		return nil, nil, fmt.Errorf("load tables: %w", err)
	}
	if err := pgConn.AllTableSchemas(tables); err != nil {
		pgConn.Close()
		return nil, nil, fmt.Errorf("load schemas: %w", err)
	}
	return pgConn, tables, nil
}

// DirectMySQL opens a direct MySQL connection (no tunnel) and loads its tables
// and schemas synchronously. Mirrors DirectPostgres for the direct MySQL
// connection type.
func DirectMySQL(entry models.GatewayEntry) (*db.MySQLDB, []db.TableInfo, error) {
	type myResult struct {
		conn *db.MySQLDB
		err  error
	}
	host, port, err := entry.DialTarget()
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan myResult, 1)
	go func() {
		myConn, err := db.NewMySQLDirect(host, port,
			entry.DBName, entry.DBUser, entry.ResolvePassword(), entry.EffectiveTLSMode())
		ch <- myResult{myConn, err}
	}()

	var myConn *db.MySQLDB
	select {
	case r := <-ch:
		myConn, err = r.conn, r.err
	case <-time.After(30 * time.Second):
		return nil, nil, directTimeoutErr(entry)
	}
	if err != nil {
		return nil, nil, err
	}

	tables, err := myConn.Tables()
	if err != nil {
		myConn.Close()
		return nil, nil, fmt.Errorf("load tables: %w", err)
	}
	if err := myConn.AllTableSchemas(tables); err != nil {
		myConn.Close()
		return nil, nil, fmt.Errorf("load schemas: %w", err)
	}
	return myConn, tables, nil
}

// directTimeoutErr explains a stalled direct connection, pointing at the SSH
// jump host when the dial went through one — the database can be listening and
// still be unreachable if the jump host cannot see it.
func directTimeoutErr(entry models.GatewayEntry) error {
	if entry.UsesSSH() {
		return fmt.Errorf("connection timed out after 30s — the SSH tunnel is open, but %s:%d is not answering from %s",
			entry.RDSHost, entry.RDSPort, entry.SSH.Describe())
	}
	return fmt.Errorf("connection timed out after 30s — host may be unreachable")
}

// BigQuery opens a BigQuery connection (no tunnel, no AWS) and loads the
// default dataset's tables and schemas synchronously. Mirrors DirectPostgres.
// Auth is ADC unless the entry names a credentials file. source becomes the
// jobs' "source" label so billing can tell the UI from other callers.
func BigQuery(entry models.GatewayEntry, source string) (*db.BigQueryDB, []db.TableInfo, error) {
	cfg := db.BigQueryConfig{
		Project:        entry.GCPProject,
		DefaultDataset: entry.DefaultDataset,
		MaxBytesBilled: entry.EffectiveMaxBytesBilled(),
		Labels:         map[string]string{"source": source},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var opts []option.ClientOption
	if entry.CredentialsPath != "" {
		data, err := os.ReadFile(entry.CredentialsPath)
		if err != nil {
			return nil, nil, fmt.Errorf("read credentials: %w", err)
		}
		opt, err := db.CredentialsOption(ctx, data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse credentials: %w", err)
		}
		opts = append(opts, opt)
	}

	bq, err := db.NewBigQuery(ctx, cfg, opts...)
	if err != nil {
		return nil, nil, err
	}
	tables, err := bq.Tables()
	if err != nil {
		bq.Close()
		return nil, nil, fmt.Errorf("load tables: %w", err)
	}
	if err := bq.AllTableSchemas(tables); err != nil {
		bq.Close()
		return nil, nil, fmt.Errorf("load schemas: %w", err)
	}
	return bq, tables, nil
}
