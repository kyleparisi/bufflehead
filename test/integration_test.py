"""Integration tests — drives the app via the control API.

Requires the app to be running headless. The control server port is dynamic;
set CONTROL_PORT env var or default to 9900 for manual runs.
Run via: test/integration_test.sh (which builds, launches Godot, then runs pytest)
"""
import os
import re
import time
from pathlib import Path

import requests

_PORT = os.environ.get("CONTROL_PORT", "9900")
BASE_URL = f"http://127.0.0.1:{_PORT}"
TESTDATA = Path(__file__).resolve().parent.parent / "testdata"
SAMPLE = str(TESTDATA / "sample.parquet")
CITIES = str(TESTDATA / "cities.parquet")
CSV = str(TESTDATA / "sales.csv")
JSONF = str(TESTDATA / "events.json")
TSV = str(TESTDATA / "metrics.tsv")
CORRUPT = str(TESTDATA / "corrupted.parquet")
DUCKDB = str(TESTDATA / "test.duckdb")


# ── Helpers ──────────────────────────────────────────────────────────────

def state():
    return requests.get(f"{BASE_URL}/state").json()


def post(endpoint, data=None):
    r = requests.post(f"{BASE_URL}/{endpoint}", json=data)
    return r.json()


def wait():
    time.sleep(0.5)


def close_all_tabs():
    while state()["tabCount"] != 0:
        post("close-tab")
        time.sleep(0.1)


def close_all_connections():
    """Close every non-memory connection (index 0 is the in-memory DuckDB)."""
    while state().get("connectionCount", 1) > 1:
        post("close-connection", {"index": 1})
        time.sleep(0.1)


def open_file(path):
    result = post("open", {"path": path})
    wait()
    return result


# ── .tscn parser ─────────────────────────────────────────────────────────

def parse_tscn(text):
    """Parse .tscn text into a list of node dicts with path, type, props."""
    nodes = []
    current = None
    for line in text.splitlines():
        line = line.strip()
        m = re.match(r'^\[node\s+(.*)\]$', line)
        if m:
            attrs = {am.group(1): am.group(2) for am in re.finditer(r'(\w+)="([^"]*)"', m.group(1))}
            name = attrs.get("name", "")
            parent = attrs.get("parent")
            if parent is None:
                full_path = name
            elif parent == ".":
                full_path = name
            else:
                full_path = parent + "/" + name
            current = {"path": full_path, "type": attrs.get("type", ""), "props": {}}
            nodes.append(current)
            continue
        if current is not None and "=" in line and not line.startswith("["):
            key, _, val = line.partition("=")
            current["props"][key.strip()] = val.strip()
    return nodes


def find_node(tscn_text, cls, ancestor=None):
    """Find first node of `cls` type, optionally under an `ancestor` type."""
    nodes = parse_tscn(tscn_text)
    for n in nodes:
        if n["type"] != cls:
            continue
        if ancestor:
            has_ancestor = any(
                a["type"] == ancestor and n["path"].startswith(a["path"] + "/")
                for a in nodes if a is not n
            )
            if not has_ancestor:
                continue
        return n
    return None


def has_node_named(tscn_text, name):
    """True if any node in the scene tree has the given node name."""
    return any(n["path"].split("/")[-1] == name for n in parse_tscn(tscn_text))


def count_nodes_named(tscn_text, name):
    """Count nodes whose leaf name equals `name` (siblings get unique names,
    so each type chip keeps its own parent and stays 'TypeChip')."""
    return sum(1 for n in parse_tscn(tscn_text) if n["path"].split("/")[-1] == name)


def ui_tree(width=None, height=None, scale=None):
    params = {}
    if width:
        params["width"] = width
    if height:
        params["height"] = height
    if scale:
        params["scale"] = scale
    r = requests.get(f"{BASE_URL}/ui-tree", params=params)
    return r.text


# ── Tests ────────────────────────────────────────────────────────────────

class TestInitialState:
    def test_no_file_loaded(self):
        s = state()
        assert s["filePath"] == ""

    def test_empty_sql(self):
        s = state()
        assert s["userSQL"] == ""

    def test_zero_rows(self):
        s = state()
        assert s["rowCount"] == 0


class TestOpenFile:
    def test_open_parquet(self):
        result = open_file(SAMPLE)
        assert result["ok"] is True
        s = state()
        assert s["filePath"] == SAMPLE
        assert s["rowCount"] == 500
        assert len(s["columns"]) == 8
        assert s["sortColumn"] == ""
        assert s["sortDir"] == 0


class TestSort:
    def test_sort_ascending(self):
        post("sort", {"column": 2})
        wait()
        s = state()
        assert s["sortColumn"] == "score"
        assert s["sortDir"] == 1

    def test_sort_descending(self):
        post("sort", {"column": 2})
        wait()
        s = state()
        assert s["sortColumn"] == "score"
        assert s["sortDir"] == 2

    def test_sort_reset(self):
        post("sort", {"column": 2})
        wait()
        s = state()
        assert s["sortColumn"] == ""
        assert s["sortDir"] == 0


class TestQuery:
    def test_custom_query(self):
        post("query", {"sql": f"SELECT id, name FROM '{SAMPLE}' WHERE id <= 10"})
        wait()
        s = state()
        assert s["rowCount"] == 10
        assert len(s["columns"]) == 2


class TestPagination:
    def test_page_offset(self):
        open_file(SAMPLE)
        post("page", {"offset": 100})
        wait()
        s = state()
        assert s["pageOffset"] == 100
        assert s["rowCount"] == 500


class TestTabs:
    def test_open_second_file_creates_tab(self):
        close_all_tabs()
        open_file(SAMPLE)
        s = state()
        assert s["rowCount"] == 500
        tabs_before = s["tabCount"]

        open_file(CITIES)
        s = state()
        assert s["rowCount"] == 3
        assert s["tabCount"] == tabs_before + 1
        assert s["filePath"] == CITIES
        assert len(s["columns"]) == 2

    def test_open_uses_empty_tab(self):
        post("new-tab")
        time.sleep(0.3)
        s = state()
        tab_count = s["tabCount"]
        assert s["filePath"] == ""

        open_file(SAMPLE)
        s = state()
        assert s["filePath"] == SAMPLE
        assert s["rowCount"] == 500
        assert s["tabCount"] == tab_count

    def test_close_all_then_open(self):
        close_all_tabs()
        s = state()
        assert s["tabCount"] == 0

        open_file(CITIES)
        s = state()
        assert s["tabCount"] == 1
        assert s["filePath"] == CITIES
        assert s["rowCount"] == 3


class TestRowDetail:
    def test_select_row(self):
        open_file(SAMPLE)
        result = post("select-row", {"row": 0})
        assert result["ok"] is True

    def test_select_row_out_of_range(self):
        result = post("select-row", {"row": 999})
        assert result.get("ok") is not True

    def test_search_detail(self):
        post("select-row", {"row": 0})
        time.sleep(0.3)
        result = post("search-detail", {"query": "tags"})
        assert result["ok"] is True

    def test_detail_opens_at_25_percent(self):
        close_all_tabs()
        open_file(SAMPLE)
        s = state()
        assert s["detailVisible"] is False
        assert s["detailToggleActive"] is False

        post("select-row", {"row": 0})
        time.sleep(0.3)
        s = state()
        assert s["detailVisible"] is True
        assert s["detailToggleActive"] is True
        assert 0.20 <= s["detailWidthRatio"] <= 0.30, (
            f"detail panel should open at ~25%, got {s['detailWidthRatio']:.0%}"
        )

    def test_detail_keeps_size_when_already_open(self):
        """Selecting another row should not resize the detail panel."""
        s = state()
        ratio_before = s["detailWidthRatio"]

        post("select-row", {"row": 1})
        time.sleep(0.3)
        s = state()
        assert s["detailVisible"] is True
        assert s["detailWidthRatio"] == ratio_before


class TestFileFormats:
    def test_csv(self):
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)
        post("query", {"sql": f"SELECT * FROM '{CSV}'"})
        wait()
        s = state()
        assert s["rowCount"] == 5
        assert len(s["columns"]) == 4
        assert s["columns"] == ["product", "region", "quantity", "price"]

    def test_json(self):
        post("query", {"sql": f"SELECT * FROM '{JSONF}'"})
        wait()
        s = state()
        assert s["rowCount"] == 4
        assert "event" in s["columns"]
        assert "user" in s["columns"]

    def test_tsv(self):
        post("query", {"sql": f"SELECT * FROM read_csv('{TSV}', delim='\\t', header=true)"})
        wait()
        s = state()
        assert s["rowCount"] == 4
        assert "host" in s["columns"]
        assert "metric" in s["columns"]


class TestErrorHandling:
    def test_corrupted_parquet(self):
        result = post("query", {"sql": f"SELECT * FROM '{CORRUPT}'"})
        assert result.get("ok") is not True

    def test_invalid_sql(self):
        result = post("query", {"sql": "SELEKT * FORM nowhere"})
        assert result.get("ok") is not True


class TestDuckDB:
    def test_open_duckdb(self):
        close_all_tabs()
        result = open_file(DUCKDB)
        assert result["ok"] is True
        s = state()
        assert s["rowCount"] == 0

    def test_query_table(self):
        post("query", {"sql": "SELECT * FROM users"})
        wait()
        s = state()
        assert s["rowCount"] == 3
        assert "name" in s["columns"]

    def test_query_view(self):
        post("query", {"sql": "SELECT * FROM user_orders"})
        wait()
        s = state()
        assert s["rowCount"] == 3
        assert "amount" in s["columns"]


class TestNavigation:
    def test_back_forward(self):
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)
        post("query", {"sql": f"SELECT * FROM '{SAMPLE}'"})
        time.sleep(0.3)
        post("query", {"sql": f"SELECT id, name FROM '{SAMPLE}' LIMIT 10"})
        time.sleep(0.3)
        s = state()
        assert s["rowCount"] == 10

        post("nav-back")
        time.sleep(0.3)
        s = state()
        assert s["rowCount"] == 500

        post("nav-forward")
        time.sleep(0.3)
        s = state()
        assert s["rowCount"] == 10


class TestUITree:
    def test_schema_panel_exists(self):
        close_all_tabs()
        open_file(SAMPLE)
        tree = ui_tree()
        assert find_node(tree, "SchemaPanel") is not None

    def test_scroll_container_in_schema(self):
        tree = ui_tree()
        sc = find_node(tree, "ScrollContainer", ancestor="SchemaPanel")
        assert sc is not None
        assert sc["props"]["horizontal_scroll_mode"] == "1"
        assert sc["props"]["vertical_scroll_mode"] == "1"

    def test_sql_data_split_resizable(self):
        """SQL panel and data grid are in a VSplitContainer (user-resizable)."""
        tree = ui_tree()
        vsplit = find_node(tree, "VSplitContainer")
        assert vsplit is not None, "VSplitContainer should exist for SQL/data split"
        # Both SQLPanel and DataGrid should be descendants of the VSplitContainer
        sql = find_node(tree, "SQLPanel", ancestor="VSplitContainer")
        assert sql is not None, "SQLPanel should be inside VSplitContainer"
        grid = find_node(tree, "DataGrid", ancestor="VSplitContainer")
        assert grid is not None, "DataGrid should be inside VSplitContainer"

    def test_tree_at_custom_size(self):
        tree = ui_tree(width=800, height=600)
        assert find_node(tree, "Window") is not None


class TestMultiRowSelect:
    """Tests for multi-row selection and detail panel value resolution."""

    def setup_method(self):
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)
        # Use sales.csv: 5 rows with known data
        # row0: Widget, North, 150, 9.99
        # row1: Gadget, South, 85, 24.5
        # row2: Widget, East, 200, 9.99
        # row3: Doohickey, West, 42, 49.99
        # row4: Gadget, North, 110, 24.5
        post("query", {"sql": f"SELECT * FROM '{CSV}'"})
        wait()

    def test_single_row_select(self):
        """Selecting a single row exposes it in state."""
        post("select-row", {"row": 0})
        time.sleep(0.3)
        s = state()
        assert s["selectedRows"] == [0]
        assert s["detailValues"]["product"] == "Widget"
        assert s["detailValues"]["region"] == "North"

    def test_multi_row_select(self):
        """Selecting multiple rows exposes all indices in state."""
        post("select-row", {"rows": [0, 2]})
        time.sleep(0.3)
        s = state()
        assert s["selectedRows"] == [0, 2]

    def test_multi_row_same_value(self):
        """When all selected rows agree on a column, detail shows that value."""
        # rows 0 and 2 are both Widget with price 9.99
        post("select-row", {"rows": [0, 2]})
        time.sleep(0.3)
        s = state()
        assert s["detailValues"]["product"] == "Widget"
        assert s["detailValues"]["price"] == "9.99"

    def test_multi_row_different_value(self):
        """When selected rows disagree on a column, detail shows dash."""
        # rows 0 and 2: same product (Widget) but different region (North vs East)
        post("select-row", {"rows": [0, 2]})
        time.sleep(0.3)
        s = state()
        assert s["detailValues"]["region"] == "\u2014"  # em dash

    def test_multi_row_all_different(self):
        """Selecting rows with no common values shows dashes everywhere."""
        # rows 0 (Widget/North) and 3 (Doohickey/West): all columns differ
        post("select-row", {"rows": [0, 3]})
        time.sleep(0.3)
        s = state()
        assert s["detailValues"]["product"] == "\u2014"
        assert s["detailValues"]["region"] == "\u2014"
        assert s["detailValues"]["quantity"] == "\u2014"
        assert s["detailValues"]["price"] == "\u2014"

    def test_deselect_all(self):
        """Deselecting clears selected rows and detail values."""
        post("select-row", {"row": 0})
        time.sleep(0.3)
        s = state()
        assert s["selectedRows"] == [0]

        post("deselect-all")
        time.sleep(0.3)
        s = state()
        assert s["selectedRows"] == []
        assert s.get("detailValues") is None

    def test_select_then_reselect_single(self):
        """Selecting multi then single replaces the selection."""
        post("select-row", {"rows": [0, 1, 2]})
        time.sleep(0.3)
        s = state()
        assert s["selectedRows"] == [0, 1, 2]

        post("select-row", {"row": 4})
        time.sleep(0.3)
        s = state()
        assert s["selectedRows"] == [4]
        assert s["detailValues"]["product"] == "Gadget"

    def test_select_row_out_of_range_multi(self):
        """Out-of-range index in multi-select returns an error."""
        result = post("select-row", {"rows": [0, 999]})
        assert result.get("ok") is not True


class TestCloseConnection:
    """Test that closing a non-memory connection keeps the app alive."""

    def setup_method(self):
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

    def test_close_connection_keeps_memory_alive(self):
        """Closing a DuckDB connection should leave the memory connection usable."""
        s = state()
        assert s["connectionCount"] == 1

        result = open_file(DUCKDB)
        assert result["ok"] is True
        s = state()
        assert s["connectionCount"] == 2

        result = post("close-connection", {"index": 1})
        assert result["ok"] is True
        time.sleep(0.3)

        s = state()
        assert s["connectionCount"] == 1
        assert s["tabCount"] >= 1

        # Verify memory connection still works
        result = open_file(SAMPLE)
        assert result["ok"] is True
        s = state()
        assert s["rowCount"] == 500

    def test_sidebar_renders_after_close(self):
        """Side nav (schema panel, split container) is visible after closing a connection."""
        result = open_file(DUCKDB)
        assert result["ok"] is True

        post("close-connection", {"index": 1})
        time.sleep(0.3)

        tree = ui_tree()
        assert find_node(tree, "SchemaPanel") is not None, "SchemaPanel should exist after closing connection"

        split = find_node(tree, "HSplitContainer")
        assert split is not None, "HSplitContainer should exist after closing connection"
        assert split["props"].get("visible") != "false", "HSplitContainer should be visible"

    def test_close_memory_connection_rejected(self):
        """Index 0 (memory connection) cannot be closed."""
        result = post("close-connection", {"index": 0})
        assert result.get("ok") is not True

    def test_close_invalid_index_rejected(self):
        """Out-of-bounds index is rejected."""
        result = post("close-connection", {"index": 99})
        assert result.get("ok") is not True


class TestReconnect:
    """Test the /reconnect endpoint's graceful error paths.

    A full reconnect requires a live AWS gateway, which isn't available in CI,
    so these cover the validation/feedback paths that don't need AWS.
    """

    def setup_method(self):
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

    def test_reconnect_invalid_index_rejected(self):
        """Out-of-bounds index returns an error, not a crash."""
        result = post("reconnect", {"index": 99})
        assert result.get("ok") is not True
        assert "error" in result

    def test_reconnect_memory_connection_rejected(self):
        """Index 0 (in-memory DuckDB) is not a gateway; reconnect is rejected."""
        result = post("reconnect", {"index": 0})
        assert result.get("ok") is not True
        assert "gateway" in result.get("error", "").lower()

    def test_reconnect_unknown_connection_name(self):
        """An unknown connection name returns a clear not-found error."""
        result = post("reconnect", {"connection": "does-not-exist"})
        assert result.get("ok") is not True
        assert "not found" in result.get("error", "").lower()

    def test_reconnect_non_gateway_duckdb_rejected(self):
        """A local DuckDB connection can't be reconnected as a gateway."""
        result = open_file(DUCKDB)
        assert result["ok"] is True
        s = state()
        assert s["connectionCount"] == 2

        result = post("reconnect", {"index": 1})
        assert result.get("ok") is not True
        assert "gateway" in result.get("error", "").lower()

        # Clean up so the opened connection doesn't leak into later tests.
        post("close-connection", {"index": 1})
        time.sleep(0.2)


class TestConnectionControls:
    """The connection rail and tab row expose visible, cross-platform controls
    for actions that previously lived only in the macOS native menu bar
    (which does not render on Windows/Linux)."""

    def test_new_connection_button_visible(self):
        close_all_tabs()
        open_file(SAMPLE)
        tree = ui_tree()
        assert has_node_named(tree, "NewConnectionButton"), (
            "connection rail should have a visible '+' New Connection button"
        )

    def test_new_tab_button_visible(self):
        tree = ui_tree()
        assert has_node_named(tree, "NewTabButton"), (
            "tab row should have a visible '+' New Tab button"
        )

    def test_new_window_button_visible(self):
        tree = ui_tree()
        assert has_node_named(tree, "NewWindowButton"), (
            "tab row should have a visible New Window button"
        )

    def test_open_gateway_shows_gateway_screen(self):
        """The 'Connect to Gateway…' action opens the gateway screen — the same
        code path the connection rail '+' menu triggers."""
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

        result = post("open-gateway")
        assert result["ok"] is True
        time.sleep(0.3)

        tree = ui_tree()
        assert find_node(tree, "GatewayScreen") is not None, (
            "gateway screen should be shown after open-gateway"
        )

        # Restore a normal data view for subsequent tests.
        open_file(SAMPLE)


class TestNewWindow:
    def test_new_window_increments_count(self):
        close_all_tabs()
        open_file(SAMPLE)
        before = state()["windowCount"]

        result = post("new-window")
        assert result["ok"] is True
        time.sleep(0.3)

        assert state()["windowCount"] == before + 1


class TestTypeChips:
    """Color-coded data-type chips appear in the schema panel and, when a row is
    selected, in the row inspector (blue INT, green FLOAT, amber TIMESTAMP, …)."""

    def test_schema_panel_has_type_chips(self):
        close_all_tabs()
        open_file(SAMPLE)  # 8 columns
        tree = ui_tree()
        assert count_nodes_named(tree, "TypeChip") >= 8, (
            "schema panel should show a type chip per column"
        )

    def test_row_inspector_has_type_chips(self):
        open_file(SAMPLE)
        before = count_nodes_named(ui_tree(), "TypeChip")

        post("select-row", {"row": 0})
        time.sleep(0.3)
        after = count_nodes_named(ui_tree(), "TypeChip")

        assert after > before, (
            "row inspector should add a type chip per field when a row is selected"
        )


class TestConnectingTracker:
    """The connecting screen shows a step tracker (Authenticate → Establish
    tunnel → Connect database → Load schema). Previewed without a live gateway
    via the /preview-connecting hook."""

    def test_preview_shows_step_tracker(self):
        close_all_tabs()
        result = post("preview-connecting")
        assert result["ok"] is True
        time.sleep(0.3)

        tree = ui_tree()
        assert has_node_named(tree, "StepTracker"), (
            "connecting screen should render a StepTracker"
        )

        # Restore a normal data view for any later tests.
        open_file(SAMPLE)


class TestHistoryPanel:
    """The history panel renders rich cards: SQL + SUCCESS/ERROR status chip,
    relative time, duration, and row count. Shown via the /show-history hook."""

    def test_history_shows_rows_and_status_chips(self):
        close_all_tabs()
        open_file(SAMPLE)
        # A success and a failure so both chip states are recorded.
        post("query", {"sql": f"SELECT id FROM '{SAMPLE}' LIMIT 5"})
        time.sleep(0.3)
        post("query", {"sql": "SELEKT bad syntax"})
        time.sleep(0.3)

        result = post("show-history")
        assert result["ok"] is True
        time.sleep(0.3)

        tree = ui_tree()
        assert has_node_named(tree, "HistoryRow"), "history should render entry cards"
        assert has_node_named(tree, "HistoryStatus"), "history cards should show a status chip"


class TestExtensionsPanel:
    """The Extensions tab lists DuckDB extensions with status chips and
    Install/Load actions. Shown via the /show-extensions hook."""

    def test_extensions_panel_lists_extensions(self):
        close_all_tabs()
        open_file(SAMPLE)

        result = post("show-extensions")
        assert result["ok"] is True
        time.sleep(0.3)

        tree = ui_tree()
        assert has_node_named(tree, "ExtensionRow"), "extensions panel should list extension cards"
        assert has_node_named(tree, "ExtensionStatus"), "extension cards should show a status chip"
