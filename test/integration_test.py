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


class TestTabTitleFromQuery:
    """Tabs on a DB connection are titled by the table in the query's FROM
    clause (parsed via the DuckDB AST), so they're easy to tell apart."""

    def setup_method(self):
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

    def test_tab_title_reflects_from_table(self):
        open_file(DUCKDB)
        time.sleep(0.2)

        # A plain table query titles the tab after the table.
        post("query", {"sql": "SELECT * FROM users"})
        wait()
        assert state()["activeTabTitle"] == "users"

        # Switching the query updates the title.
        post("query", {"sql": "SELECT * FROM orders WHERE id > 0"})
        wait()
        assert state()["activeTabTitle"] == "orders"

        # A join titles after the first (left-most) table.
        post("query", {"sql": "SELECT * FROM users u JOIN orders o ON u.id = o.user_id"})
        wait()
        assert state()["activeTabTitle"] == "users"

    def test_tab_title_falls_back_without_base_table(self):
        open_file(DUCKDB)
        time.sleep(0.2)
        conn_name = state()["activeTabTitle"]  # default DB query titles by table

        # A query with no plain base table (constant select) falls back to the
        # connection name rather than showing a confusing title.
        post("query", {"sql": "SELECT 1 AS n"})
        wait()
        title = state()["activeTabTitle"]
        # test.duckdb connection is named after the file (without extension logic
        # aside, it's the file's base name). Just assert it's NOT a bogus title.
        assert title not in ("1", "n", ""), f"unexpected fallback title: {title!r}"


class TestColumnSelection:
    """The schema sidebar for file-based tabs shows a checkbox per column; the
    checked set drives which columns the query projects (via SelectedCols →
    VirtualSQL). /select-columns mirrors clicking those checkboxes."""

    def setup_method(self):
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

    def test_selecting_subset_projects_only_those_columns(self):
        open_file(SAMPLE)
        s = state()
        all_cols = s["columns"]
        assert len(all_cols) > 2, "sample should have several columns"

        # Select a subset → only those columns show, in the given order.
        post("select-columns", {"columns": ["name", "role"]})
        wait()
        assert state()["columns"] == ["name", "role"]

        # A different subset updates the projection.
        post("select-columns", {"columns": ["id", "score"]})
        wait()
        assert state()["columns"] == ["id", "score"]

        # Empty selection restores all columns.
        post("select-columns", {"columns": []})
        wait()
        assert state()["columns"] == all_cols

    def test_column_selection_applies_over_custom_query(self):
        open_file(SAMPLE)
        s = state()
        assert "id" in s["columns"] and "score" in s["columns"]

        # Run a custom query, then restrict columns — projection must still apply
        # (wrapping the user's SQL), not silently show everything.
        post("query", {"sql": f"SELECT * FROM '{SAMPLE}' WHERE score > 0"})
        wait()
        post("select-columns", {"columns": ["id", "score"]})
        wait()
        assert state()["columns"] == ["id", "score"]


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


class TestSchemaPersistsAcrossTabs:
    """Schema is scoped to the connection, not the tab. Closing the last tab
    bound to a still-open connection must not lose the schema — re-selecting the
    connection tile re-opens a tab and re-renders the schema from the retained
    connection tables."""

    def setup_method(self):
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

    def test_reselecting_connection_restores_schema_after_tab_close(self):
        # Open test.duckdb → creates connection index 1 with a bound tab whose
        # schema panel shows its tables/views.
        result = open_file(DUCKDB)
        assert result["ok"] is True
        s = state()
        assert s["connectionCount"] == 2
        assert s["connIdx"] == 1, "opened tab should be bound to the new connection"
        table_count = s["schemaTableCount"]
        assert table_count > 0, "schema panel should list the duckdb tables/views"

        # Close the active tab (the only one bound to connection 1).
        post("close-tab")
        time.sleep(0.3)

        # Connection must still be open (tile remains).
        s = state()
        assert s["connectionCount"] == 2, "closing a tab must not close the connection"

        # Re-select the connection tile → should open a fresh tab and restore
        # the schema from the connection's retained tables.
        result = post("select-connection", {"index": 1})
        assert result["ok"] is True
        time.sleep(0.3)

        s = state()
        assert s["connIdx"] == 1, "re-selecting the connection should bind a tab to it"
        assert s["schemaTableCount"] == table_count, (
            "schema should be restored from the still-open connection, not lost"
        )

    def test_closing_connection_removes_schema_and_tile(self):
        # Opening + then closing the connection should drop it entirely.
        open_file(DUCKDB)
        s = state()
        assert s["connectionCount"] == 2

        result = post("close-connection", {"index": 1})
        assert result["ok"] is True
        time.sleep(0.3)

        s = state()
        assert s["connectionCount"] == 1, "closing the connection removes its tile"
        # Falls back to the in-memory connection (index 0), which has no tables.
        assert s.get("schemaTableCount", 0) == 0


class TestConnectionScopedTabs:
    """Tabs are scoped to connections. The visible tab bar shows ONLY the active
    connection's tabs; selecting a tab must not jump to another connection; and
    each connection remembers its own active tab."""

    def setup_method(self):
        close_all_connections()
        close_all_tabs()
        post("new-tab")
        time.sleep(0.3)

    def test_tab_bar_shows_only_active_connection_tabs(self):
        # Start on Memory (conn 0). Add a couple of Memory tabs.
        s = state()
        assert s["activeConnIdx"] == 0
        post("new-tab")
        post("new-tab")
        time.sleep(0.3)
        s = state()
        mem_visible = s["visibleTabCount"]
        assert mem_visible >= 3, "Memory should show its own tabs"

        # Open a duckdb connection (conn 1) → its own tab bar.
        open_file(DUCKDB)
        s = state()
        assert s["activeConnIdx"] == 1
        assert s["connIdx"] == 1
        # The visible tab bar now reflects connection 1, not the Memory tabs.
        assert s["visibleTabCount"] < s["tabCount"], (
            "visible tab bar should be a subset of all tabs (scoped to conn 1)"
        )
        conn1_visible = s["visibleTabCount"]

        # Switch back to Memory: its tab set (and count) returns.
        post("select-connection", {"index": 0})
        time.sleep(0.3)
        s = state()
        assert s["activeConnIdx"] == 0
        assert s["connIdx"] == 0, "selecting Memory activates a Memory tab"
        assert s["visibleTabCount"] == mem_visible, (
            "switching back to Memory restores its own tab bar"
        )

        # And back to conn 1.
        post("select-connection", {"index": 1})
        time.sleep(0.3)
        s = state()
        assert s["activeConnIdx"] == 1
        assert s["connIdx"] == 1
        assert s["visibleTabCount"] == conn1_visible

    def test_selecting_tab_does_not_change_connection(self):
        # Two Memory tabs; open a duckdb connection; return to Memory.
        post("new-tab")
        time.sleep(0.2)
        open_file(DUCKDB)
        time.sleep(0.2)
        post("select-connection", {"index": 0})
        time.sleep(0.2)

        s = state()
        assert s["activeConnIdx"] == 0
        before_conn = s["activeConnIdx"]

        # Selecting a visible tab (index 0 in the Memory-scoped bar) must keep us
        # on the same connection — no jump to conn 1.
        post("select-tab", {"index": 0})
        time.sleep(0.2)
        s = state()
        assert s["activeConnIdx"] == before_conn, (
            "clicking a tab must not switch the active connection"
        )
        assert s["connIdx"] == 0

    def test_connection_remembers_its_active_tab(self):
        # Memory: create a second tab and make it active.
        post("new-tab")
        time.sleep(0.2)
        s = state()
        mem_active_tab = s["activeTabID"]

        # Open duckdb connection (switches away from Memory).
        open_file(DUCKDB)
        time.sleep(0.2)
        s = state()
        assert s["activeConnIdx"] == 1
        conn1_active_tab = s["activeTabID"]

        # Back to Memory → the previously-active Memory tab is restored.
        post("select-connection", {"index": 0})
        time.sleep(0.2)
        s = state()
        assert s["activeConnIdx"] == 0
        assert s["activeTabID"] == mem_active_tab, (
            "connection should restore its own last-active tab"
        )

        # Back to conn 1 → its active tab is restored.
        post("select-connection", {"index": 1})
        time.sleep(0.2)
        s = state()
        assert s["activeConnIdx"] == 1
        assert s["activeTabID"] == conn1_active_tab

    def test_schema_stays_loaded_across_tabs_of_a_connection(self):
        # Open a duckdb connection → tab 1 shows its schema.
        open_file(DUCKDB)
        time.sleep(0.2)
        s = state()
        assert s["activeConnIdx"] == 1
        table_count = s["schemaTableCount"]
        assert table_count > 0, "first tab of the connection should show schema"

        # Add a second tab under the SAME connection via the "+" button.
        post("new-tab")
        time.sleep(0.2)
        s = state()
        assert s["connIdx"] == 1, "new tab should belong to the active connection"
        assert s["schemaTableCount"] == table_count, (
            "a new tab under a DB connection must also show the schema"
        )
        assert s["visibleTabCount"] >= 2

        # Click through the connection's tabs — schema must NOT unload.
        for barIdx in range(s["visibleTabCount"]):
            post("select-tab", {"index": barIdx})
            time.sleep(0.15)
            cur = state()
            assert cur["activeConnIdx"] == 1, "clicking tabs must not change connection"
            assert cur["schemaTableCount"] == table_count, (
                f"schema unloaded when selecting tab {barIdx}"
            )

    def test_close_tab_closes_the_active_tab_not_the_first(self):
        """Cmd+W (/close-tab) must close the currently-selected tab, not tab 0."""
        # Three Memory tabs. setup_method already created one, so add two more.
        post("new-tab")
        post("new-tab")
        time.sleep(0.3)
        s = state()
        assert s["visibleTabCount"] >= 3
        total_before = s["tabCount"]

        # Select the FIRST visible tab, remember its id, then re-select a middle
        # tab and make IT active.
        post("select-tab", {"index": 0})
        time.sleep(0.15)
        first_id = state()["activeTabID"]

        post("select-tab", {"index": 1})
        time.sleep(0.15)
        active_id = state()["activeTabID"]
        assert active_id != first_id

        # Close the active tab (mirrors Cmd+W). The first tab must survive; the
        # active tab must be gone.
        post("close-tab")
        time.sleep(0.2)
        s = state()
        assert s["tabCount"] == total_before - 1, "exactly one tab should close"
        assert s["activeTabID"] != active_id, "the closed tab must no longer be active"

        # The first tab must still exist: selecting it should succeed and make it
        # active.
        post("select-tab", {"index": 0})
        time.sleep(0.15)
        # first_id is still present among tabs (it wasn't the one closed).
        remaining_first = state()["activeTabID"]
        assert remaining_first == first_id, (
            "close-tab wrongly removed the first tab instead of the active one"
        )

    def test_closing_active_tab_keeps_order_and_activates_neighbor(self):
        """Closing the active tab must NOT reorder tabs or jump the selection to
        position 0 — it should activate the adjacent tab in place."""
        post("new-tab")
        post("new-tab")
        time.sleep(0.3)
        s = state()
        ids = s["visibleTabIDs"]
        assert len(ids) >= 3, "need at least 3 visible tabs"

        # Activate the LAST (rightmost) tab, like the screenshot.
        last_idx = len(ids) - 1
        post("select-tab", {"index": last_idx})
        time.sleep(0.15)
        s = state()
        assert s["activeTabID"] == ids[last_idx]
        assert s["activeBarIndex"] == last_idx

        # Close it. The remaining tabs keep their order (ids minus the last one),
        # and the new active tab is the neighbor to the left — still the last
        # position — NOT tab 0.
        post("close-tab")
        time.sleep(0.2)
        s = state()
        assert s["visibleTabIDs"] == ids[:-1], "tab order must be preserved"
        assert s["activeTabID"] == ids[last_idx - 1], (
            "closing the last tab should activate its left neighbor, not tab 0"
        )
        assert s["activeBarIndex"] == last_idx - 1, (
            "active selection must stay in place (rightmost), not jump to 0"
        )

        # Now close a MIDDLE tab and confirm the right neighbor slides in.
        ids2 = s["visibleTabIDs"]  # e.g. [a, b]
        post("select-tab", {"index": 0})
        time.sleep(0.15)
        assert state()["activeTabID"] == ids2[0]
        post("close-tab")
        time.sleep(0.2)
        s = state()
        assert s["visibleTabIDs"] == ids2[1:], "order preserved after closing tab 0"
        assert s["activeTabID"] == ids2[1], (
            "closing tab 0 should activate the right neighbor"
        )


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
        assert has_node_named(tree, "GatewayCloseButton"), (
            "gateway screen must have a visible Close button to exit"
        )

        # Closing the screen restores the normal view (bug fix: it used to have
        # no exit). Data view returns because a tab is open.
        result = post("close-gateway")
        assert result["ok"] is True
        time.sleep(0.3)
        tree = ui_tree()
        assert find_node(tree, "GatewayScreen") is None, (
            "gateway screen should be gone after close"
        )
        assert find_node(tree, "SchemaPanel") is not None, (
            "the normal data view should be restored after closing the gateway screen"
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

    def test_preview_failed_marks_dot_red_and_hides_status(self):
        """When the connection fails, the active step's dot turns red (filled)
        and the "Connecting to database…" status line is hidden."""
        close_all_tabs()
        result = post("preview-connecting", {"failed": True})
        assert result["ok"] is True
        time.sleep(0.3)

        tree = ui_tree()
        nodes = parse_tscn(tree)

        # Locate the StepTracker subtree.
        tracker = next(
            (n for n in nodes if n["path"].split("/")[-1] == "StepTracker"), None
        )
        assert tracker is not None, "connecting screen should render a StepTracker"

        # Collect the Label nodes under the tracker (dots + step labels). Each
        # step is an HBox row of [dot Label, text Label]; the dot glyph is a
        # multi-byte char that gets escaped in the .tscn dump, so we identify
        # dots as the labels whose text is not one of the step names.
        step_names = {
            '"Authenticate (SSO)"', '"Establish tunnel"',
            '"Connect to database"', '"Load schema"',
        }
        under_tracker = [
            n for n in nodes
            if n["type"] == "Label" and n["path"].startswith(tracker["path"] + "/")
        ]
        dots = [n for n in under_tracker if n["props"].get("text") not in step_names]
        # Error red is #FFB4AB → Color(1, 0.7059, 0.6706, 1).
        red = "Color(1, 0.7059, 0.6706, 1)"
        red_dots = [n for n in dots if n["props"].get("font_color") == red]
        assert len(red_dots) == 1, (
            "failed connection should render exactly one red dot on the active step; "
            f"got dots: {[(n['props'].get('text'), n['props'].get('font_color')) for n in dots]}"
        )
        # No dot should still be showing the yellow in-progress color.
        yellow = "Color(0.9, 0.75, 0.2, 1)"
        assert not any(
            n["props"].get("font_color") == yellow for n in dots
        ), "failed connection should have no yellow in-progress dot"

        # The "Connecting to database…" status line should be hidden.
        status_lines = [
            n for n in nodes
            if n["type"] == "Label"
            and "Connecting to database" in n["props"].get("text", "")
        ]
        for n in status_lines:
            assert n["props"].get("visible") == "false", (
                "status line should be hidden when the connection fails"
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
