"""Integration tests — drives the app via the control API.

Requires the app to be running headless on port 9900.
Run via: test/integration_test.sh (which builds, launches Godot, then runs pytest)
"""
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

    def test_tree_at_custom_size(self):
        tree = ui_tree(width=800, height=600)
        assert find_node(tree, "Window") is not None
