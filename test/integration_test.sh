#!/bin/bash
# Integration test — runs the app headless and drives it via the control API.
# Usage: ./test/integration_test.sh
set -e

GODOT="/Users/openclaw/gd/bin/Godot.app/Contents/MacOS/Godot"
PROJECT_DIR="$(cd "$(dirname "$0")/../graphics" && pwd)"
SAMPLE="$(cd "$(dirname "$0")/../testdata" && pwd)/sample.parquet"
PORT=9900
URL="http://127.0.0.1:$PORT"
PASS=0
FAIL=0

# Build the dylib first
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
PATH="$PATH:/Users/openclaw/go/bin"
echo "Building dylib..."
go build -buildmode=c-shared -o "$ROOT/graphics/darwin_amd64.dylib" ./cmd/viewer
echo "Build OK"

# Start headless
cd "$PROJECT_DIR"
$GODOT --headless &
PID=$!
sleep 3

cleanup() {
    kill $PID 2>/dev/null || true
    wait $PID 2>/dev/null || true
    echo ""
    echo "Results: $PASS passed, $FAIL failed"
    [ $FAIL -eq 0 ] && exit 0 || exit 1
}
trap cleanup EXIT

assert_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        echo "  ✓ $desc"
        PASS=$((PASS + 1))
    else
        echo "  ✗ $desc (expected: $expected, got: $actual)"
        FAIL=$((FAIL + 1))
    fi
}

json_get() {
    python3 -c "import sys,json; d=json.load(sys.stdin); print(d$1)"
}

# ── Test: Initial state ──────────────────────────────────────────────────
echo "Test: Initial state"
STATE=$(curl -s "$URL/state")
assert_eq "no file loaded" "" "$(echo "$STATE" | json_get '["filePath"]')"
assert_eq "empty SQL" "" "$(echo "$STATE" | json_get '["userSQL"]')"
assert_eq "zero rows" "0" "$(echo "$STATE" | json_get '["rowCount"]')"

# ── Test: Open file ──────────────────────────────────────────────────────
echo "Test: Open file"
RESULT=$(curl -s -X POST "$URL/open" -d "{\"path\":\"$SAMPLE\"}")
assert_eq "open ok" "True" "$(echo "$RESULT" | json_get '["ok"]')"
sleep 0.5

STATE=$(curl -s "$URL/state")
assert_eq "file path set" "$SAMPLE" "$(echo "$STATE" | json_get '["filePath"]')"
assert_eq "500 rows" "500" "$(echo "$STATE" | json_get '["rowCount"]')"
assert_eq "8 columns" "8" "$(echo "$STATE" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['columns']))")"
assert_eq "no sort" "" "$(echo "$STATE" | json_get '["sortColumn"]')"
assert_eq "sort dir none" "0" "$(echo "$STATE" | json_get '["sortDir"]')"

# ── Test: Sort ascending ─────────────────────────────────────────────────
echo "Test: Sort ascending (score)"
curl -s -X POST "$URL/sort" -d '{"column":2}' >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "sort column" "score" "$(echo "$STATE" | json_get '["sortColumn"]')"
assert_eq "sort asc" "1" "$(echo "$STATE" | json_get '["sortDir"]')"

# ── Test: Sort descending ────────────────────────────────────────────────
echo "Test: Sort descending (score)"
curl -s -X POST "$URL/sort" -d '{"column":2}' >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "sort column" "score" "$(echo "$STATE" | json_get '["sortColumn"]')"
assert_eq "sort desc" "2" "$(echo "$STATE" | json_get '["sortDir"]')"

# ── Test: Sort reset ─────────────────────────────────────────────────────
echo "Test: Sort reset (score)"
curl -s -X POST "$URL/sort" -d '{"column":2}' >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "sort cleared" "" "$(echo "$STATE" | json_get '["sortColumn"]')"
assert_eq "sort none" "0" "$(echo "$STATE" | json_get '["sortDir"]')"

# ── Test: Custom query ───────────────────────────────────────────────────
echo "Test: Custom query"
curl -s -X POST "$URL/query" -d "{\"sql\":\"SELECT id, name FROM '$SAMPLE' WHERE id <= 10\"}" >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "10 rows" "10" "$(echo "$STATE" | json_get '["rowCount"]')"
assert_eq "2 columns" "2" "$(echo "$STATE" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['columns']))")"

# ── Test: Pagination ─────────────────────────────────────────────────────
echo "Test: Pagination"
curl -s -X POST "$URL/open" -d "{\"path\":\"$SAMPLE\"}" >/dev/null
sleep 0.5
curl -s -X POST "$URL/page" -d '{"offset":100}' >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "offset 100" "100" "$(echo "$STATE" | json_get '["pageOffset"]')"
assert_eq "still 500 total" "500" "$(echo "$STATE" | json_get '["rowCount"]')"

# ── Test: Open replaces current tab ──────────────────────────────────────
CITIES="$(dirname "$SAMPLE")/cities.parquet"
# Reset: close all tabs, start fresh
echo "Test: Open second file opens new tab"
while [ "$(curl -s "$URL/state" | json_get '["tabCount"]')" != "0" ]; do
    curl -s -X POST "$URL/close-tab" >/dev/null; sleep 0.1
done
curl -s -X POST "$URL/open" -d "{\"path\":\"$SAMPLE\"}" >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "sample loaded" "500" "$(echo "$STATE" | json_get '["rowCount"]')"
TABS_BEFORE=$(echo "$STATE" | json_get '["tabCount"]')

curl -s -X POST "$URL/open" -d "{\"path\":\"$CITIES\"}" >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "cities loaded" "3" "$(echo "$STATE" | json_get '["rowCount"]')"
assert_eq "tab count +1" "$((TABS_BEFORE + 1))" "$(echo "$STATE" | json_get '["tabCount"]')"
assert_eq "cities file path" "$CITIES" "$(echo "$STATE" | json_get '["filePath"]')"
assert_eq "cities 2 cols" "2" "$(echo "$STATE" | python3 -c "import sys,json; print(len(json.load(sys.stdin)['columns']))")"

# ── Test: New tab then open file ─────────────────────────────────────────
echo "Test: New tab then open file (uses empty tab)"
curl -s -X POST "$URL/new-tab" >/dev/null
sleep 0.3
STATE=$(curl -s "$URL/state")
NEW_TABS=$(echo "$STATE" | json_get '["tabCount"]')
assert_eq "new tab empty" "" "$(echo "$STATE" | json_get '["filePath"]')"

curl -s -X POST "$URL/open" -d "{\"path\":\"$SAMPLE\"}" >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "sample in empty tab" "$SAMPLE" "$(echo "$STATE" | json_get '["filePath"]')"
assert_eq "500 rows" "500" "$(echo "$STATE" | json_get '["rowCount"]')"
assert_eq "no extra tab" "$NEW_TABS" "$(echo "$STATE" | json_get '["tabCount"]')"

# ── Test: Close all tabs then open file ──────────────────────────────────
echo "Test: Close all tabs then open file"
# Close all tabs
for i in $(seq 1 $NEW_TABS); do
    curl -s -X POST "$URL/close-tab" >/dev/null
    sleep 0.2
done
STATE=$(curl -s "$URL/state")
assert_eq "zero tabs" "0" "$(echo "$STATE" | json_get '["tabCount"]')"

curl -s -X POST "$URL/open" -d "{\"path\":\"$CITIES\"}" >/dev/null
sleep 0.5
STATE=$(curl -s "$URL/state")
assert_eq "auto-created tab" "1" "$(echo "$STATE" | json_get '["tabCount"]')"
assert_eq "cities file" "$CITIES" "$(echo "$STATE" | json_get '["filePath"]')"
assert_eq "3 rows" "3" "$(echo "$STATE" | json_get '["rowCount"]')"

# ── Test: Select row populates detail ────────────────────────────────────
echo "Test: Select row"
curl -s -X POST "$URL/open" -d "{\"path\":\"$SAMPLE\"}" >/dev/null
sleep 0.5
RESULT=$(curl -s -X POST "$URL/select-row" -d '{"row":0}')
assert_eq "select row ok" "True" "$(echo "$RESULT" | json_get '["ok"]')"
RESULT=$(curl -s -X POST "$URL/select-row" -d '{"row":999}')
assert_eq "out of range error" "False" "$(echo "$RESULT" | json_get '.get("ok", False)')"

# ── Test: Search detail ──────────────────────────────────────────────────
echo "Test: Search detail"
curl -s -X POST "$URL/select-row" -d '{"row":0}' >/dev/null
sleep 0.3
RESULT=$(curl -s -X POST "$URL/search-detail" -d '{"query":"tags"}')
assert_eq "search detail ok" "True" "$(echo "$RESULT" | json_get '["ok"]')"
