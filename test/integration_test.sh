#!/bin/bash
# Integration test — builds the dylib, launches Godot headless, runs pytest.
# Usage: ./test/integration_test.sh
set -e

PORT=9900

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

# Kill any stale Godot processes holding port 9900
pkill -9 -if godot 2>/dev/null || true
for i in $(seq 1 10); do
    curl -s http://127.0.0.1:$PORT/state >/dev/null 2>&1 || break
    sleep 1
done

# Build the dylib
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
PATH="$PATH:$(go env GOPATH)/bin"
echo "Building shared library..."
case "$(uname -s)" in
    Darwin)
        go build -buildmode=c-shared -o "$ROOT/graphics/darwin_amd64.dylib" ./cmd/viewer
        cp "$ROOT/graphics/darwin_amd64.dylib" "$ROOT/graphics/darwin_universal.dylib"
        if command -v codesign >/dev/null 2>&1; then
            codesign --force --sign - "$ROOT/graphics/darwin_universal.dylib"
        fi
        ;;
    Linux)
        go build -buildmode=c-shared -o "$ROOT/graphics/linux_amd64.so" ./cmd/viewer
        ;;
esac
echo "Build OK"

# Start headless
cd "$ROOT/graphics"
$GODOT --headless &
PID=$!
sleep 3

cleanup() {
    kill $PID 2>/dev/null || true
    wait $PID 2>/dev/null || true
}
trap cleanup EXIT

# Run pytest
cd "$ROOT"
python3 -m pytest test/integration_test.py -v "$@"
