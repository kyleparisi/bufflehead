package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	bfaws "bufflehead/internal/aws"
	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"bufflehead/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// establishTunnel allocates a local port, resolves the bastion instance, and
// starts + waits for an SSM tunnel for the given gateway entry. It returns the
// running tunnel and the entry updated with the assigned LocalPort.
//
// logf, if non-nil, receives human-readable progress messages. This is shared
// by the initial gateway connect flow and the reconnect flow so both build the
// tunnel identically (including spot-instance resolution on reconnect).
func establishTunnel(auth *bfaws.AuthManager, entry models.GatewayEntry, logf func(string)) (*bfaws.TunnelManager, models.GatewayEntry, error) {
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
				return "", fmt.Errorf("%d SSM instances online — cannot auto-select bastion", len(instances))
			}
		}
	}

	if err := tunnel.Start(tunnelCfg); err != nil {
		return nil, entry, fmt.Errorf("start tunnel: %w", err)
	}

	log("Waiting for tunnel...")
	if err := tunnel.WaitReady(60 * time.Second); err != nil {
		tunnel.Stop()
		return nil, entry, fmt.Errorf("tunnel not ready: %w", err)
	}

	log("Tunnel connected")
	return tunnel, entry, nil
}

// reconnectConnection tears down a gateway connection (cancelling any running
// queries, closing the DB pool, and stopping the SSM tunnel) and then rebuilds
// it from the stored config. It records the outcome of each phase so callers
// (e.g. an AI agent via /reconnect) get granular feedback about what failed.
//
// The teardown runs synchronously on the main thread; the rebuild runs in a
// background goroutine and delivers a ReqReconnect result that swaps the new
// resources into the Connection on the main thread. If cmd is non-nil, the
// control command is answered from the result handler with a ReconnectResult.
func (w *AppWindow) reconnectConnection(idx int, cmd *control.Command) {
	if idx == 0 {
		// Index 0 is the in-memory DuckDB connection, not a gateway.
		if cmd != nil {
			cmd.Respond(control.Result{Error: "the in-memory connection is not a remote gateway; nothing to reconnect"})
		}
		return
	}
	if idx < 0 || idx >= len(w.connections) {
		if cmd != nil {
			cmd.Respond(control.Result{Error: "invalid connection index"})
		}
		return
	}
	conn := w.connections[idx]
	if conn.Gateway == nil {
		if cmd != nil {
			cmd.Respond(control.Result{Error: "connection is not a remote gateway; nothing to reconnect"})
		}
		return
	}

	name := conn.Name
	entry := conn.Gateway.Config
	auth := conn.Gateway.Auth
	oldTunnel := conn.Gateway.Tunnel

	var steps []control.ReconnectStep
	addStep := func(step string, err error) {
		s := control.ReconnectStep{Step: step, OK: err == nil}
		if err != nil {
			s.Error = err.Error()
		}
		steps = append(steps, s)
	}

	w.statusBar.SetStatus("Reconnecting " + name + "...")

	// ── Teardown (main thread) ──────────────────────────────────────────────
	// Stop the worker: cancels its context, aborting any in-flight queries.
	if conn.worker != nil {
		conn.worker.Stop()
		conn.worker = nil
	}
	addStep("cancel_queries", nil)

	// Close the DB pool.
	if conn.DB != nil {
		if err := conn.DB.Close(); err != nil {
			addStep("close_db", err) // non-fatal; keep going
		} else {
			addStep("close_db", nil)
		}
		conn.DB = nil
	}

	// Stop the old tunnel.
	if oldTunnel != nil {
		if err := oldTunnel.Stop(); err != nil {
			addStep("stop_tunnel", err) // non-fatal; keep going
		} else {
			addStep("stop_tunnel", nil)
		}
	}

	// ── Rebuild (background goroutine) ──────────────────────────────────────
	go func() {
		outcome := &ReconnectOutcome{ConnIdx: idx, Steps: steps}
		finish := func() { w.results <- DBResult{Kind: ReqReconnect, ControlCmd: cmd, Reconnect: outcome} }

		// Refresh AWS credentials/config for IAM auth so an expired SSO login
		// surfaces here (and a fresh login is picked up) before we build a token.
		var awsCfg *aws.Config
		if entry.UseIAMAuth() && auth != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := auth.EnsureConfig(ctx)
			cancel()
			if err != nil {
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{
					Step: "refresh_credentials", OK: false, Error: err.Error(),
				})
				finish()
				return
			}
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "refresh_credentials", OK: true})
			cfg := auth.Config()
			awsCfg = &cfg
		}

		// Re-establish the SSM tunnel.
		tunnel, updated, err := establishTunnel(auth, entry, nil)
		if err != nil {
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{
				Step: "start_tunnel", OK: false, Error: err.Error(),
			})
			finish()
			return
		}
		outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "start_tunnel", OK: true})
		outcome.Tunnel = tunnel

		// Reconnect the database + reload schema.
		rdsEndpoint := fmt.Sprintf("%s:%d", updated.RDSHost, updated.RDSPort)
		pgConn, tables, err := openGatewayDB("127.0.0.1", updated.LocalPort, rdsEndpoint,
			updated.DBName, updated.DBUser, updated.ResolvePassword(), awsCfg)
		if err != nil {
			tunnel.Stop()
			outcome.Tunnel = nil
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{
				Step: "connect_db", OK: false, Error: err.Error(),
			})
			finish()
			return
		}
		outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: true})
		outcome.Querier = pgConn
		outcome.Tables = tables
		finish()
	}()
}

// openGatewayDB opens a Postgres gateway connection and loads its tables and
// schemas, synchronously. It mirrors RunOpenGateway's connect logic but returns
// directly instead of posting to a channel, so it can be used inside a caller's
// own goroutine (the reconnect flow).
func openGatewayDB(host string, port int, rdsEndpoint, dbName, user, password string, awsCfg *aws.Config) (*db.PostgresDB, []db.TableInfo, error) {
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

// handleReconnectResult swaps freshly-established resources into the Connection
// on the main thread and answers any waiting control command with a
// ReconnectResult describing every step.
func (w *AppWindow) handleReconnectResult(res DBResult) {
	oc := res.Reconnect
	if oc == nil {
		if res.ControlCmd != nil {
			res.ControlCmd.Respond(control.Result{Error: "reconnect: missing outcome"})
		}
		return
	}

	idx := oc.ConnIdx
	success := oc.Querier != nil
	// Determine overall OK: every recorded step succeeded and we have a DB.
	overallOK := success
	for _, s := range oc.Steps {
		if !s.OK {
			overallOK = false
		}
	}

	// UI-initiated reconnects (no control command) surface their step-by-step
	// detail directly in the data grid so the user can see where it failed.
	uiInitiated := res.ControlCmd == nil

	if idx > 0 && idx < len(w.connections) {
		conn := w.connections[idx]
		if success {
			conn.DB = oc.Querier
			conn.Tables = oc.Tables
			if conn.Gateway != nil {
				conn.Gateway.Tunnel = oc.Tunnel
				conn.Gateway.LastTunnelMsg = ""
			}
			worker := NewConnWorker(conn.DB, w.results)
			worker.Start()
			conn.worker = worker

			// Refresh any tabs bound to this connection.
			for _, ts := range w.tabs {
				if ts.connIdx == idx {
					ts.schema.SetTables(conn.Tables)
					ts.sqlPanel.SetCompletionTables(conn.Tables)
				}
			}
			if idx == w.activeConnIdx && conn.Gateway != nil {
				w.titleBar.SetAIPrompt(buildAIPrompt(conn.Gateway.Config, conn.Tables, w.controlAddr))
			}
			applySecondaryButtonTheme(conn.button.AsControl())
			if idx == w.activeConnIdx {
				applyActiveButtonTheme(conn.button.AsControl())
			}
			w.statusBar.SetStatus(fmt.Sprintf("Reconnected: %s (%d tables/views)", conn.Name, len(conn.Tables)))
		} else {
			applyErrorButtonTheme(conn.button.AsControl())
			w.statusBar.SetStatus("Reconnect failed: " + conn.Name)
		}

		// Show step detail in the grid when the user triggered this, or on any
		// failure so the breakdown is always visible.
		if uiInitiated || !overallOK {
			if ts := w.currentTab(); ts != nil && ts.connIdx == idx {
				ts.dataGrid.ShowError(formatReconnectSteps(conn.Name, overallOK, oc.Steps))
			}
		}
	}

	if res.ControlCmd != nil {
		name := ""
		if idx > 0 && idx < len(w.connections) {
			name = w.connections[idx].Name
		}
		payload := control.ReconnectResult{
			Connection: name,
			OK:         overallOK,
			Steps:      oc.Steps,
			Tables:     len(oc.Tables),
		}
		data, _ := json.Marshal(payload)
		res.ControlCmd.Respond(control.Result{OK: overallOK, Data: data})
	}
}

// formatReconnectSteps renders a reconnect outcome as a readable, multi-line
// summary for the data grid error panel. Auth (expired-login) errors get the
// friendly guidance treatment; other errors are shown verbatim.
func formatReconnectSteps(name string, ok bool, steps []control.ReconnectStep) string {
	return control.FormatReconnectSteps(name, ok, steps, func(msg string) (string, bool) {
		return bfaws.FormatConnError(fmt.Errorf("%s", msg))
	})
}
