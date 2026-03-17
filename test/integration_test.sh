#!/bin/bash
# Integration test — builds the dylib, launches Godot headless, runs pytest.
# Usage: ./test/integration_test.sh
set -e

GODOT="/Users/openclaw/gd/bin/Godot.app/Contents/MacOS/Godot"
PROJECT_DIR="$(cd "$(dirname "$0")/../graphics" && pwd)"
PORT=9900

# Kill any stale Godot processes holding port 9900
pkill -9 -if godot 2>/dev/null || true
for i in $(seq 1 10); do
    curl -s http://127.0.0.1:$PORT/state >/dev/null 2>&1 || break
    sleep 1
done

# Build the dylib
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
PATH="$PATH:/Users/openclaw/go/bin"
echo "Building dylib..."
go build -buildmode=c-shared -o "$ROOT/graphics/darwin_amd64.dylib" ./cmd/viewer
cp "$ROOT/graphics/darwin_amd64.dylib" "$ROOT/graphics/darwin_universal.dylib"
echo "Build OK"

# Start headless
cd "$PROJECT_DIR"
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
