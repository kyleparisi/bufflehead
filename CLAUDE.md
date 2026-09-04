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

Integration tests use an HTTP control API to drive the app programmatically. The control server binds to a random available port (printed to stdout on startup as `Control server: http://127.0.0.1:<port>`). It also mints a temporary bearer key — printed as `Control key: <key>` and required on **every** request as an `Authorization: Bearer <key>` header — so other local software can't drive the control server by blindly POSTing to the port. The key is ephemeral (rotates each launch, never written to disk) and is baked into the AI prompt Bufflehead copies to the clipboard. Set `BUFFLEHEAD_CONTROL_KEY` to pin a known key (the integration harness does this so the app and pytest share one). The test suite is in `test/integration_test.py` (pytest). You can also interact with the control API manually — check stdout for the port and key:

```bash
# stdout prints e.g. "Control server: http://127.0.0.1:54321" and "Control key: <key>"
curl -X POST http://localhost:<port>/open \
  -H "Authorization: Bearer <key>" -d '{"path":"testdata/sample.parquet"}'
curl -H "Authorization: Bearer <key>" http://localhost:<port>/state
```

Test data lives in `testdata/` (parquet, CSV, JSON, TSV, .duckdb files).

## Architecture

**Entry point**: `cmd/viewer/main.go` — initializes the Godot scene tree, creates a DuckDB instance, starts the control server, registers UI classes, and creates the main window.

**Key packages**:

- `internal/db/` — DuckDB wrapper. `New()` creates in-memory DB, `OpenDB()` opens .duckdb files read-only. Handles schema inspection, paginated queries, and parquet metadata extraction.
- `internal/models/` — `AppState` is the single source of truth per tab. Holds query text, schema, results, sort/pagination params, and a navigation stack (back/forward). `QueryHistory` persists query history to JSON in the user config dir.
- `internal/ui/` — All Godot UI nodes implemented as Go extensions. `app.go` is the root node managing windows, menus, and keyboard shortcuts. `appwindow.go` manages tabs, sidebar, SQL panel, data grid, and row detail panel.
- `internal/control/` — HTTP server (dynamic port, printed to stdout) exposing endpoints for programmatic control (`/open`, `/query`, `/sort`, `/page`, `/state`, `/screenshot`, `/ui-tree`, etc.). Every request is gated by a temporary bearer key minted in `control.New` (`Authorization: Bearer <key>`); `requireAuth` wraps the mux in `Start`. Primarily used by integration tests and the copy-to-clipboard AI prompt.

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

## UI Rendering: Treat It As A State Machine, Not Procedural Mutations

The UI (tabs, connections, sidebar, rail, title/status bars) is a **projection of state**. Model changes as a state machine, not as ad-hoc, scattered node mutations. This is the single most important convention for `internal/ui/`.

**Rules:**

1. **Separate state from rendering.** Keep the authoritative state in plain Go fields/structs (e.g. `AppWindow.connections`, `AppWindow.tabs`, `activeConnIdx`, per-connection active tab). Godot nodes are a *view* derived from that state — never a second source of truth.

2. **Events mutate state only.** User/system actions (select connection, select tab, new tab, close tab, open/close connection) should change state fields and nothing else. They must NOT poke visibility, highlights, or the TabBar directly.

3. **One `render()`/projection function updates the nodes.** After any event mutates state, call a single idempotent render pass that reconstructs the visible nodes from state: which TabBar entries exist and which is selected, which sidebar/content pane is visible, which rail tile is highlighted, title/status/AI-prompt text. Rendering is a pure function of state and must be safe to call repeatedly.

4. **Rendering must never mutate state.** A render step that writes back into state (e.g. `switchTab` setting `activeConnIdx = ts.connIdx`) is the classic bug — it couples views to each other and causes cross-talk (clicking a tab jumps connections). If you find a render path writing state, that's the root cause to fix.

5. **Godot signals are events, not render triggers.** Translate a signal (e.g. TabBar `tab_changed` with a bar index) into a state event, then render. Because the visible TabBar is a filtered subset of `tabs`, a bar index is NOT a `tabs` index — key bar tabs to a stable ID via `SetTabMetadata`/`GetTabMetadata` and translate.

6. **Suppress signals at the render boundary, not with scattered guards.** If `render()` must call node setters that re-emit signals (`ClearTabs`, `AddTab`, `SetCurrentTab`), suppress those signals in one place around the render pass. Do NOT sprinkle re-entrancy flags across individual handlers — that's a symptom of missing separation.

**Smell checklist (stop and refactor toward state+render if you see these):**
- The same integer used as both a `tabs` index and a TabBar index (assumed 1:1 mapping).
- A re-entrancy/`rebuilding*` boolean guarding multiple handlers.
- A "switch"/"select" function updating unrelated view state (rail highlight, title bar) inline.
- Copy-pasted node-update sequences in `open`, `select`, `close`, and `refresh` paths.

**Derived state that belongs to the model, not the view:** e.g. "which tab is active for each connection" should live in state (per-connection active tab ID), so switching connections restores the right tab. Don't infer it from current node selection.

## MCP Tools

If the `gopls` and `godoc` MCP servers are available, use them for Go workspace navigation, symbol search, file context, diagnostics, and package documentation. Prefer these over manually reading source files when exploring unfamiliar code.

## Creating Releases

Bump the version in `graphics/export_presets.cfg` (macOS `application/short_version`
+ `application/version`, and the Windows presets' `file_version`/`product_version`).

**Signed + notarized DMG (Developer ID — this is what ships to users):**
`bin/sign-notarize` builds the app, deep-signs every nested Mach-O with the
hardened runtime, builds the styled DMG, then notarizes and staples it. It runs
either in CI or locally — same script, credentials come from a different place.

*Via CI (preferred — no dependency on one machine's keychain):* the `Build macOS`
workflow (`.github/workflows/build-macos.yml`) runs it on a macOS runner with the
signing credentials as repo secrets. Trigger it and download the DMG with:
```bash
./bin/release-dmg-ci
gh release upload vX.Y.Z releases/Bufflehead.dmg --clobber
```
Required repo secrets — `MACOS_CERT_P12_BASE64`, `MACOS_CERT_PASSWORD` (the
Developer ID cert as a base64 `.p12`), plus `APPLE_API_KEY_P8_BASE64`,
`APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID` (an App Store Connect API key, which
unlike an app-specific password is not tied to an Apple ID or 2FA). The workflow
checks all five are set before the build, and re-verifies the stapled ticket with
`stapler validate` + `spctl` before uploading the artifact. In CI the cert is
imported into a throwaway keychain that is deleted on exit.

*Locally* (credentials in your login keychain, per `bin/sign-notarize`'s header):
```bash
SIGN_IDENTITY="Developer ID Application: Kyle Parisi (63GMD6U4J2)" \
NOTARY_PROFILE="bufflehead-notary" ./bin/sign-notarize
gh release create vX.Y.Z releases/Bufflehead.dmg --title "vX.Y.Z" --notes "..."
```
Entitlements live in `packaging/macos/entitlements.plist`; `disable-library-validation`
is required so the hardened runtime can load DuckDB's downloaded extension dylibs.

**Unsigned DMG (local/dev only — Gatekeeper will block it on other Macs):**
```bash
./bin/release-dmg
```

**Linux AppImage:** the Linux build must run on Linux (glibc build + AppImage
tooling), so it goes through the `Build Linux` GitHub Actions workflow
(`.github/workflows/build-linux.yml`, on `ubuntu-22.04` for a low glibc floor).
The build logic lives in `bin/build-linux` (runnable directly on a Linux box):
it runs `gd build`, bundles `linux_amd64.so` next to the exported binary, and
packages a portable AppImage. DuckDB needs no special toolchain on Linux
(go-duckdb ships a static `libduckdb.a`; native ELF TLS avoids the Windows
gcc-14.2/emutls issue). From macOS, trigger CI and download the artifact with:
```bash
./bin/release-appimage
```
