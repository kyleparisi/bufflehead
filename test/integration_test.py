"""Integration tests — drives the app via the control API.

Requires the app to be running headless on port 9900.
Run via: test/integration_test.sh (which builds, launches Godot, then runs pytest)
"""
import json
import re
import time
from pathlib import Path

import requests

BASE_URL = "http://127.0.0.1:9900"
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


def find_node_by_name(tscn_text, name):
    """Find a node by its name (last path segment)."""
    nodes = parse_tscn(tscn_text)
    for n in nodes:
        node_name = n["path"].split("/")[-1] if "/" in n["path"] else n["path"]
        if node_name == name:
            return n
    return None


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


class TestDesigner:
    """Tests for the layout designer / inspector."""

    def setup_method(self):
        close_all_tabs()
        open_file(SAMPLE)
        post("designer-open")
        time.sleep(0.3)

    def teardown_method(self):
        post("designer-close")
        time.sleep(0.1)

    def _designer_state(self):
        r = requests.get(f"{BASE_URL}/designer-state")
        return r.json()

    def _designer_find(self, name):
        return post("designer-find", {"name": name})

    def _designer_click(self, x, y):
        return post("designer-click", {"x": x, "y": y})

    def test_designer_opens(self):
        """Designer can be toggled on via control API."""
        s = self._designer_state()
        assert s["ok"] is True
        # No selection yet — data should be null
        assert s.get("data") is None

    def test_find_node_by_name(self):
        """Can find and select a node by its Godot name."""
        # The ui-tree should have Button nodes; find the first one
        tree = ui_tree()
        nodes = parse_tscn(tree)
        buttons = [n for n in nodes if n["type"] == "Button"]
        assert len(buttons) > 0, "Should have at least one Button in the tree"
        # Use the first button's name
        btn_name = buttons[0]["path"].split("/")[-1]
        result = self._designer_find(btn_name)
        assert result["ok"] is True
        info = result["data"]
        assert info["name"] == btn_name
        assert info["type"] == "Button"

    def test_button_rect_is_reasonable(self):
        """A button's rect should be small, not the size of the whole window."""
        tree = ui_tree()
        nodes = parse_tscn(tree)
        buttons = [n for n in nodes if n["type"] == "Button"]
        assert len(buttons) > 0

        btn_name = buttons[0]["path"].split("/")[-1]
        result = self._designer_find(btn_name)
        assert result["ok"] is True
        info = result["data"]

        # A button should NOT be larger than 300x300 (generous upper bound)
        assert info["sizeW"] < 300, (
            f"Button {btn_name!r} width {info['sizeW']} is too large — "
            f"should be a small button, not a container"
        )
        assert info["sizeH"] < 300, (
            f"Button {btn_name!r} height {info['sizeH']} is too large — "
            f"should be a small button, not a container"
        )

    def test_size_matches_global_rect(self):
        """Size() and GetGlobalRect().Size should agree."""
        tree = ui_tree()
        nodes = parse_tscn(tree)
        buttons = [n for n in nodes if n["type"] == "Button"]
        assert len(buttons) > 0

        btn_name = buttons[0]["path"].split("/")[-1]
        result = self._designer_find(btn_name)
        assert result["ok"] is True
        info = result["data"]

        # Size() should equal GetGlobalRect().Size
        assert abs(info["sizeW"] - info["rectSizeW"]) < 1, (
            f"Size().X={info['sizeW']} != GetGlobalRect().Size.X={info['rectSizeW']}"
        )
        assert abs(info["sizeH"] - info["rectSizeH"]) < 1, (
            f"Size().Y={info['sizeH']} != GetGlobalRect().Size.Y={info['rectSizeH']}"
        )

    def test_global_position_matches_rect(self):
        """GlobalPosition() and GetGlobalRect().Position should agree."""
        tree = ui_tree()
        nodes = parse_tscn(tree)
        buttons = [n for n in nodes if n["type"] == "Button"]
        assert len(buttons) > 0

        btn_name = buttons[0]["path"].split("/")[-1]
        result = self._designer_find(btn_name)
        assert result["ok"] is True
        info = result["data"]

        assert abs(info["globalPosX"] - info["rectPosX"]) < 1, (
            f"GlobalPosition.X={info['globalPosX']} != GetGlobalRect.Position.X={info['rectPosX']}"
        )
        assert abs(info["globalPosY"] - info["rectPosY"]) < 1, (
            f"GlobalPosition.Y={info['globalPosY']} != GetGlobalRect.Position.Y={info['rectPosY']}"
        )

    def test_click_at_button_position_selects_button(self):
        """Clicking at a button's center should select it (or a deeper child)."""
        tree = ui_tree()
        nodes = parse_tscn(tree)
        buttons = [n for n in nodes if n["type"] == "Button"]
        assert len(buttons) > 0

        btn_name = buttons[0]["path"].split("/")[-1]
        find_result = self._designer_find(btn_name)
        assert find_result["ok"] is True
        info = find_result["data"]

        # Click at the center of the button
        cx = info["globalPosX"] + info["sizeW"] / 2
        cy = info["globalPosY"] + info["sizeH"] / 2
        click_result = self._designer_click(cx, cy)
        assert click_result["ok"] is True
        click_info = click_result["data"]

        # The clicked node should be at this position with a reasonable size
        assert click_info["sizeW"] < 300, (
            f"Clicked node {click_info['name']!r} width {click_info['sizeW']} is too large"
        )
        assert click_info["sizeH"] < 300, (
            f"Clicked node {click_info['name']!r} height {click_info['sizeH']} is too large"
        )

    def test_tree_selection_highlights_correct_node(self):
        """Selecting a node in the inspector tree positions the highlight overlay
        exactly over that node's rect in the main view."""
        tree = ui_tree()
        nodes = parse_tscn(tree)

        # Test several node types to cover containers, buttons, and panels
        targets = []
        for cls in ("Button", "VBoxContainer", "PanelContainer", "Label"):
            found = [n for n in nodes if n["type"] == cls]
            if found:
                targets.append(found[0]["path"].split("/")[-1])
        assert len(targets) >= 2, "Need at least 2 different node types to test"

        for name in targets:
            result = self._designer_find(name)
            assert result["ok"] is True, f"Could not find node {name!r}"
            info = result["data"]

            # Highlight must be visible after selection
            assert info["highlightVisible"] is True, (
                f"Highlight should be visible after selecting {name!r}"
            )

            # Highlight position must match selected node's GlobalPosition
            assert abs(info["highlightX"] - info["globalPosX"]) < 1, (
                f"Node {name!r}: highlight X={info['highlightX']} != "
                f"node globalPosX={info['globalPosX']}"
            )
            assert abs(info["highlightY"] - info["globalPosY"]) < 1, (
                f"Node {name!r}: highlight Y={info['highlightY']} != "
                f"node globalPosY={info['globalPosY']}"
            )

            # Highlight size must match selected node's Size
            assert abs(info["highlightW"] - info["sizeW"]) < 1, (
                f"Node {name!r}: highlight W={info['highlightW']} != "
                f"node sizeW={info['sizeW']}"
            )
            assert abs(info["highlightH"] - info["sizeH"]) < 1, (
                f"Node {name!r}: highlight H={info['highlightH']} != "
                f"node sizeH={info['sizeH']}"
            )


class TestLayoutOverrides:
    """Verify that graphics/layout.json overrides are applied on boot."""

    LAYOUT_FILE = Path(__file__).resolve().parent.parent / "graphics" / "layout.json"

    def _load_layout(self):
        with open(self.LAYOUT_FILE) as f:
            return json.load(f)

    def _parse_vector2(self, val):
        """Parse 'Vector2(x, y)' string into (x, y) floats."""
        m = re.match(r"Vector2\(([^,]+),\s*([^)]+)\)", val)
        assert m, f"Cannot parse Vector2: {val}"
        return float(m.group(1)), float(m.group(2))

    def test_layout_file_exists(self):
        assert self.LAYOUT_FILE.exists(), "graphics/layout.json must exist"

    def test_layout_paths_resolve(self):
        """Every node path in layout.json should exist in the ui-tree."""
        layout = self._load_layout()
        tree = ui_tree()
        nodes = parse_tscn(tree)
        all_paths = set()
        for n in nodes:
            # Reconstruct full path: root name is the first node with no parent
            all_paths.add(n["path"])
        for path in layout:
            # layout.json uses /root/... but tscn paths are relative
            # strip leading /root/ to get relative path
            rel = path.lstrip("/root/") if path.startswith("/root/") else path
            # The root node itself has path "" in tscn, children use name or parent/name
            found = any(
                p == rel or p.endswith("/" + rel.split("/")[-1])
                for p in all_paths
            )
            node_name = path.split("/")[-1]
            node = find_node_by_name(tree, node_name)
            assert node is not None, (
                f"Node {node_name!r} from layout.json path {path!r} "
                f"not found in ui-tree"
            )

    def test_min_width_applied(self):
        """connRailWrap should have min_width from layout.json."""
        layout = self._load_layout()
        tree = ui_tree()
        for path, props in layout.items():
            if "min_width" not in props:
                continue
            node_name = path.split("/")[-1]
            node = find_node_by_name(tree, node_name)
            assert node is not None, f"Node {node_name!r} not found"
            assert "custom_minimum_size" in node["props"], (
                f"Node {node_name!r} missing custom_minimum_size in ui-tree"
            )
            x, _ = self._parse_vector2(node["props"]["custom_minimum_size"])
            expected = props["min_width"]
            assert x == expected, (
                f"Node {node_name!r}: min_width={x}, expected {expected}"
            )
