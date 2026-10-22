package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	bfaws "bufflehead/internal/aws"
	"bufflehead/internal/connect"
	"bufflehead/internal/control"

	"github.com/aws/aws-sdk-go-v2/aws"
)

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
	oldSSH := conn.Gateway.SSH

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
	if oldSSH != nil {
		if err := oldSSH.Stop(); err != nil {
			addStep("stop_ssh_tunnel", err) // non-fatal; keep going
		} else {
			addStep("stop_ssh_tunnel", nil)
		}
	}

	// ── Rebuild (background goroutine) ──────────────────────────────────────
	go func() {
		outcome := &ReconnectOutcome{Conn: conn, Steps: steps}
		finish := func() { w.results <- DBResult{Kind: ReqReconnect, ControlCmd: cmd, Reconnect: outcome} }

		// BigQuery: no tunnel or AWS auth — just reopen the client.
		if entry.IsBigQuery() {
			bq, tables, err := connect.BigQuery(entry, "ui")
			if err != nil {
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{
					Step: "connect_db", OK: false, Error: err.Error(),
				})
				finish()
				return
			}
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: true})
			outcome.Querier = bq
			outcome.Tables = tables
			finish()
			return
		}

		// Direct Postgres/MySQL: no AWS auth. Rebuild the SSH tunnel first when
		// the connection uses one — the old one was just stopped, so the local
		// forward port is reassigned here.
		if entry.IsDirect() || entry.IsMySQL() {
			tunnel, updated, err := connect.SSHTunnel(context.Background(), entry, nil)
			if err != nil {
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{
					Step: "start_ssh_tunnel", OK: false, Error: err.Error(),
				})
				finish()
				return
			}
			if tunnel != nil {
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "start_ssh_tunnel", OK: true})
				outcome.SSH = tunnel
			}

			dbConn, tables, err := connect.Direct(updated)
			if err != nil {
				tunnel.Stop()
				outcome.SSH = nil
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{
					Step: "connect_db", OK: false, Error: err.Error(),
				})
				finish()
				return
			}
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: true})
			outcome.Querier = dbConn
			outcome.Tables = tables
			outcome.Entry = &updated // carries the rebuilt tunnel's LocalPort
			finish()
			return
		}

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
		tunnel, updated, err := connect.SSMTunnel(context.Background(), auth, entry, nil)
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
		pgConn, tables, err := connect.GatewayPostgres("127.0.0.1", updated.LocalPort, rdsEndpoint,
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

// switchDatabase re-points a connection at a different database on the same
// server. For Postgres it reuses the reconnect teardown+rebuild plumbing but
// KEEPS any tunnel, SSM or SSH (host:port is database-agnostic) — only the DB
// pool is swapped for one bound to the new dbname. For BigQuery it swaps the default
// dataset and reopens the client. The new schema/tables repopulate via the
// shared ReqReconnect result handler (which reads the updated Config).
func (w *AppWindow) switchDatabase(idx int, dbName string) {
	if idx <= 0 || idx >= len(w.connections) {
		return
	}
	conn := w.connections[idx]
	if conn.Gateway == nil {
		return
	}
	isBQ := conn.Gateway.Config.IsBigQuery()

	// ── State change: point the config at the new database/dataset. ──
	if isBQ {
		if conn.Gateway.Config.DefaultDataset == dbName {
			return // already on this dataset
		}
		conn.Gateway.Config.DefaultDataset = dbName
		w.statusBar.SetStatus(fmt.Sprintf("Switching %s to dataset %q...", conn.Name, dbName))
	} else {
		if conn.Gateway.Config.DBName == dbName {
			return // already on this database
		}
		conn.Gateway.Config.DBName = dbName
		w.statusBar.SetStatus(fmt.Sprintf("Switching %s to database %q...", conn.Name, dbName))
	}
	entry := conn.Gateway.Config

	// ── Teardown (main thread): stop worker + close DB pool, keep the tunnel. ──
	if conn.worker != nil {
		conn.worker.Stop()
		conn.worker = nil
	}
	if conn.DB != nil {
		conn.DB.Close()
		conn.DB = nil
	}

	steps := []control.ReconnectStep{{Step: "cancel_queries", OK: true}, {Step: "close_db", OK: true}}

	// ── Rebuild (goroutine): open a new pool/client for the new target. ──
	go func() {
		// Both tunnels are deliberately kept: host:port is database-agnostic, so
		// only the pool below them is swapped.
		outcome := &ReconnectOutcome{Conn: conn, Steps: steps, Tunnel: conn.Gateway.Tunnel, SSH: conn.Gateway.SSH}
		finish := func() { w.results <- DBResult{Kind: ReqReconnect, Reconnect: outcome} }

		if isBQ {
			bq, tables, err := connect.BigQuery(entry, "ui")
			if err != nil {
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: false, Error: err.Error()})
				finish()
				return
			}
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: true})
			outcome.Querier = bq
			outcome.Tables = tables
			finish()
			return
		}

		if entry.IsMySQL() || entry.IsDirect() {
			// The entry keeps its LocalPort, so a tunneled connection re-opens
			// through the SSH forward that is still standing.
			dbConn, tables, err := connect.Direct(entry)
			if err != nil {
				outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: false, Error: err.Error()})
				finish()
				return
			}
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: true})
			outcome.Querier = dbConn
			outcome.Tables = tables
			outcome.Entry = &entry // the new DBName, same tunnel
			finish()
			return
		}

		// AWS gateway: reopen through the SSM tunnel, which is still standing.
		var awsCfg *aws.Config
		if entry.UseIAMAuth() && conn.Gateway.Auth != nil {
			cfg := conn.Gateway.Auth.Config()
			awsCfg = &cfg
		}
		rdsEndpoint := fmt.Sprintf("%s:%d", entry.RDSHost, entry.RDSPort)
		pgConn, tables, err := connect.GatewayPostgres("127.0.0.1", entry.LocalPort, rdsEndpoint,
			entry.DBName, entry.DBUser, entry.ResolvePassword(), awsCfg)
		if err != nil {
			outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: false, Error: err.Error()})
			finish()
			return
		}
		outcome.Steps = append(outcome.Steps, control.ReconnectStep{Step: "connect_db", OK: true})
		outcome.Querier = pgConn
		outcome.Tables = tables
		finish()
	}()
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

	// Resolve by identity: the connection's index may have shifted, or it may
	// have been closed, while the rebuild ran.
	idx := w.connIndex(oc.Conn)
	success := oc.Querier != nil
	if idx < 0 && success {
		// Closed mid-reconnect: nothing owns the fresh resources, so release them.
		oc.Querier.Close()
		if oc.Tunnel != nil {
			oc.Tunnel.Stop()
		}
		oc.SSH.Stop() // nil-safe
	}
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
				conn.Gateway.SSH = oc.SSH
				if oc.Entry != nil {
					conn.Gateway.Config = *oc.Entry
				}
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
			if conn.Gateway != nil {
				// Keep the connection Path and breadcrumb in sync with the
				// (possibly changed) database name — a database switch reuses
				// this handler with a new Config.DBName.
				cfg := conn.Gateway.Config
				conn.Path = connPathFor(cfg)
				if idx == w.activeConnIdx {
					w.titleBar.SetAIPrompt(buildAIPrompt(cfg, conn.Tables, w.controlAddr, w.controlKey))
					w.titleBar.SetReconnectVisible(true)
					w.titleBar.SetConnectionInfo(connDisplayKind(cfg), conn.Name, connDBSegment(cfg))
				}
			}
			applyConnTileTheme(conn.button.AsControl(), idx == w.activeConnIdx)
			w.statusBar.SetStatus(fmt.Sprintf("Reconnected: %s (%d tables/views)", conn.Name, len(conn.Tables)))
		} else {
			applyConnTileErrorTheme(conn.button.AsControl())
			w.statusBar.SetStatus("Reconnect failed: " + conn.Name)
		}

		// Record the step-by-step detail in the console so it's available without
		// clobbering the data grid — a successful reconnect/database switch is not
		// an error and shouldn't render as one. Only a genuine failure escalates to
		// the data grid (and pops the console open), where the breakdown is prominent.
		if uiInitiated || !overallOK {
			detail := formatReconnectSteps(conn.Name, overallOK, oc.Steps)
			if !overallOK {
				if ts := w.currentTab(); ts != nil && ts.connIdx == idx {
					ts.dataGrid.ShowError(detail)
				}
				w.logConsole(detail)
			} else {
				LogConsole(detail)
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
