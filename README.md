# parquet-viewer

A native cross-platform Parquet viewer built with Go + DuckDB + graphics.gd (Godot 4.6).

## Features

- Open any `.parquet` file via file dialog
- Schema inspector (column names, types, nullability)
- Virtual-scrolled data grid (handles large files via DuckDB paging)
- SQL query editor — run any DuckDB SQL against your file
- File-level Parquet metadata viewer

## Prerequisites

```bash
# 1. Install Go 1.25+
https://go.dev/dl/

# 2. Install the gd command (graphics.gd build tool)
go install graphics.gd/cmd/gd@release

# 3. Make sure $GOPATH/bin is in your $PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

## Run

```bash
gd run ./cmd/viewer
```

This will download Godot 4.6 automatically on first run and open the editor/app.

## Build for distribution

```bash
# macOS
GOOS=macos gd build ./cmd/viewer

# Windows
GOOS=windows gd build ./cmd/viewer

# Linux
GOOS=linux gd build ./cmd/viewer

# Android
GOOS=android GOARCH=arm64 gd build ./cmd/viewer

# Web (WASM)
GOOS=web gd build ./cmd/viewer
```

## Project Structure

```
parquet-viewer/
├── cmd/viewer/
│   └── main.go          # Entrypoint
├── internal/
│   ├── db/
│   │   └── duck.go      # DuckDB wrapper (schema, query, metadata)
│   ├── models/
│   │   └── state.go     # Shared app state
│   └── ui/
│       └── app.go       # Godot UI built in Go via graphics.gd
├── go.mod
└── README.md
```

## Roadmap

- [ ] Row group / file metadata panel
- [ ] Multi-file JOIN support (drag & drop multiple parquets)
- [ ] S3/remote parquet support (`read_parquet('s3://...')`)
- [ ] Column statistics (min/max/null count per column)
- [ ] Export query results to CSV/JSON
- [ ] Query history
- [ ] Virtual scrolling pagination controls (next/prev page)
