# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Bufflehead is a native cross-platform Parquet viewer built with Go + DuckDB + graphics.gd (Godot 4.6). The entire UI is built in Go using Godot as a rendering engine — there are no `.gd` scripts.

## Build & Run

Requires Go 1.26+ and the `gd` CLI (`go install graphics.gd/cmd/gd@release`).

```bash
# Run in dev mode (downloads Godot 4.6 on first run)
gd run ./cmd/viewer

# Build for macOS
GOOS=macos gd build ./cmd/viewer
```

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (builds dylib, launches Godot headless, runs pytest)
./test/integration_test.sh
```

Integration tests use an HTTP control API to drive the app programmatically. The control server binds to a random available port (printed to stdout on startup as `Control server: http://127.0.0.1:<port>`). The test suite is in `test/integration_test.py` (pytest). You can also interact with the control API manually — check stdout for the port:

```bash
# port is printed to stdout, e.g. "Control server: http://127.0.0.1:54321"
curl -X POST http://localhost:<port>/open -d '{"path":"testdata/sample.parquet"}'
curl http://localhost:<port>/state
```

Test data lives in `testdata/` (parquet, CSV, JSON, TSV, .duckdb files).

## Architecture

**Entry point**: `cmd/viewer/main.go` — initializes the Godot scene tree, creates a DuckDB instance, starts the control server, registers UI classes, and creates the main window.

**Key packages**:

- `internal/db/` — DuckDB wrapper. `New()` creates in-memory DB, `OpenDB()` opens .duckdb files read-only. Handles schema inspection, paginated queries, and parquet metadata extraction.
- `internal/models/` — `AppState` is the single source of truth per tab. Holds query text, schema, results, sort/pagination params, and a navigation stack (back/forward). `QueryHistory` persists query history to JSON in the user config dir.
- `internal/ui/` — All Godot UI nodes implemented as Go extensions. `app.go` is the root node managing windows, menus, and keyboard shortcuts. `appwindow.go` manages tabs, sidebar, SQL panel, data grid, and row detail panel.
- `internal/control/` — HTTP server (dynamic port, printed to stdout) exposing endpoints for programmatic control (`/open`, `/query`, `/sort`, `/page`, `/state`, `/screenshot`, `/ui-tree`, etc.). Primarily used by integration tests.

**UI extension pattern** (graphics.gd):
```go
type MyComponent struct {
    Container.Extension[MyComponent] `gd:"ComponentName"`
}
func (c *MyComponent) Ready() { /* init */ }
```

Components expose public callback functions (e.g., `OnColumnsChanged`, `OnColumnClicked`) that parent components set during initialization.

**Window layout** (per AppWindow):
- TitleBar → TabBar → HSplitContainer(sidebar | content) → StatusBar
- Sidebar holds SchemaPanel (columns/tables) or HistoryPanel
- Content holds SQLPanel + DataGrid, with an optional RowDetailPanel in a nested HSplit
- A Connection Rail on the far left lists open database connections

**Query flow**: File opened → schema loaded via `duck.Schema()` → default query set → `execQuery()` builds virtual SQL with sort/pagination via `state.VirtualSQL()` → DuckDB executes → results populate DataGrid → state pushed to nav stack.

## MCP Tools

If the `gopls` and `godoc` MCP servers are available, use them for Go workspace navigation, symbol search, file context, diagnostics, and package documentation. Prefer these over manually reading source files when exploring unfamiliar code.

## Creating Releases

Build the app and create a styled DMG, then publish a GitHub release:
```bash
./bin/release-dmg
gh release create vX.Y.Z releases/Bufflehead.dmg --title "vX.Y.Z" --notes "..."
```
