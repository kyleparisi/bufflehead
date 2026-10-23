#!/bin/bash
# Integration test — builds the dylib, launches Godot headless, runs pytest.
# Usage: ./test/integration_test.sh
set -e

# Locate Godot: respect $GODOT, then check $GDPATH/~/gd, then $PATH.
if [ -z "$GODOT" ]; then
    GDPATH="${GDPATH:-$HOME/gd}"
    if [ -x "$GDPATH/bin/godot.app/Contents/MacOS/godot" ]; then
        GODOT="$GDPATH/bin/godot.app/Contents/MacOS/godot"
    elif command -v godot >/dev/null 2>&1; then
        GODOT="$(command -v godot)"
    else
        echo "Error: Godot not found. Set GODOT or install via 'gd run'." >&2
        exit 1
    fi
fi

# Each run owns only the Godot PID it launches; never kill other sessions.

# Build the dylib
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
PATH="$PATH:$(go env GOPATH)/bin"
echo "Building shared library..."
case "$(uname -s)" in
    Darwin)
        go build -buildmode=c-shared -o "$ROOT/graphics/darwin_amd64.dylib" .
        cp "$ROOT/graphics/darwin_amd64.dylib" "$ROOT/graphics/darwin_universal.dylib"
        if command -v codesign >/dev/null 2>&1; then
            codesign --force --sign - "$ROOT/graphics/darwin_universal.dylib"
        fi
        ;;
    Linux)
        go build -buildmode=c-shared -o "$ROOT/graphics/linux_amd64.so" .
        ;;
esac
echo "Build OK"

# The control server requires a temporary bearer key. Pin a known value so the
# app and the pytest harness share it (the app reads BUFFLEHEAD_CONTROL_KEY at
# startup; the harness sends it as the Authorization header).
export BUFFLEHEAD_CONTROL_KEY="integration-test-key"

# "Connect an AI Assistant…" writes Claude Desktop's config; aim it at a
# scratch file so tests never touch the real one.
SCRATCH_DIR=$(mktemp -d)
export BUFFLEHEAD_CLAUDE_DESKTOP_CONFIG="$SCRATCH_DIR/claude_desktop_config.json"
export XDG_CONFIG_HOME="$SCRATCH_DIR/config"
export XDG_DATA_HOME="$SCRATCH_DIR/data"

# Start headless and capture stdout to parse the dynamic port
LOGFILE=$(mktemp)
GODOT_ENV=()
if [ "$(uname -s)" = "Linux" ]; then
    # Match the shipped Linux launcher: allocate the Go/DuckDB static TLS at
    # process startup rather than trying to fit it during Godot's dlopen.
    GODOT_ENV=(env "LD_PRELOAD=$ROOT/graphics/linux_$(go env GOARCH).so${LD_PRELOAD:+:$LD_PRELOAD}")
fi
cd "$ROOT/graphics"
# A clean checkout has no extension cache. Import before starting the app.
if ! "${GODOT_ENV[@]}" "$GODOT" --headless --editor --import --quit > "$LOGFILE" 2>&1; then
    cat "$LOGFILE"
    rm -f "$LOGFILE"
    rm -rf "$SCRATCH_DIR"
    exit 1
fi
"${GODOT_ENV[@]}" "$GODOT" --headless > "$LOGFILE" 2>&1 &
PID=$!

cleanup() {
    kill $PID 2>/dev/null || true
    wait $PID 2>/dev/null || true
    rm -f "$LOGFILE"
    rm -rf "$SCRATCH_DIR"
}
trap cleanup EXIT

# Wait for the control server to print its port
for i in $(seq 1 30); do
    if grep -q "Control server:" "$LOGFILE" 2>/dev/null; then
        break
    fi
    sleep 1
done

PORT=$(grep "Control server:" "$LOGFILE" | sed 's/.*127.0.0.1:\([0-9]*\).*/\1/')
if [ -z "$PORT" ]; then
    echo "Error: could not detect control server port" >&2
    cat "$LOGFILE"
    exit 1
fi
echo "Control server on port $PORT"

# Run pytest with the dynamic port
cd "$ROOT"
CONTROL_PORT="$PORT" BUFFLEHEAD_CONTROL_KEY="$BUFFLEHEAD_CONTROL_KEY" \
    python3 -m pytest test/integration_test.py -v "$@"
