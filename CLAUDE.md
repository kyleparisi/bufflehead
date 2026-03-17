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

Integration tests use an HTTP control API on port 9900 to drive the app programmatically. The test suite is in `test/integration_test.py` (pytest). You can also interact with the control API manually:

```bash
curl -X POST http://localhost:9900/open -d '{"path":"testdata/sample.parquet"}'
curl http://localhost:9900/state
```

Test data lives in `testdata/` (parquet, CSV, JSON, TSV, .duckdb files).

## Architecture

**Entry point**: `cmd/viewer/main.go` — initializes the Godot scene tree, creates a DuckDB instance, starts the control server, registers UI classes, and creates the main window.

**Key packages**:

- `internal/db/` — DuckDB wrapper. `New()` creates in-memory DB, `OpenDB()` opens .duckdb files read-only. Handles schema inspection, paginated queries, and parquet metadata extraction.
- `internal/models/` — `AppState` is the single source of truth per tab. Holds query text, schema, results, sort/pagination params, and a navigation stack (back/forward). `QueryHistory` persists query history to JSON in the user config dir.
- `internal/ui/` — All Godot UI nodes implemented as Go extensions. `app.go` is the root node managing windows, menus, and keyboard shortcuts. `appwindow.go` manages tabs, sidebar, SQL panel, data grid, and row detail panel.
- `internal/control/` — HTTP server (port 9900) exposing endpoints for programmatic control (`/open`, `/query`, `/sort`, `/page`, `/state`, `/screenshot`, `/ui-tree`, etc.). Primarily used by integration tests.

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

## Creating Releases

Build the app, then create a DMG and GitHub release:
```bash
GOOS=macos gd build ./cmd/viewer
hdiutil create -volname "Bufflehead" -srcfolder releases/darwin/universal/Bufflehead.app -ov -format UDZO /tmp/Bufflehead.dmg
gh release create vX.Y.Z /tmp/Bufflehead.dmg --title "vX.Y.Z" --notes "..."
```
