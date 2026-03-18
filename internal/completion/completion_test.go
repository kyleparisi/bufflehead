package completion

import (
	"strings"
	"testing"

	"bufflehead/internal/db"
)

// helper: find all items of a given kind
func itemsOfKind(items []Item, kind Kind) []Item {
	var out []Item
	for _, item := range items {
		if item.Kind == kind {
			out = append(out, item)
		}
	}
	return out
}

// helper: find an item by exact insert text
func findByInsert(items []Item, insert string) *Item {
	for _, item := range items {
		if item.InsertText == insert {
			return &item
		}
	}
	return nil
}

// ── Schema column completions ──────────────────────────────────────────────

func TestBuildSchemaColumns(t *testing.T) {
	cols := []db.Column{
		{Name: "user_id", DataType: "INTEGER"},
		{Name: "username", DataType: "VARCHAR"},
		{Name: "email", DataType: "VARCHAR"},
	}

	items := Build("user", cols, nil)

	vars := itemsOfKind(items, KindVariable)
	if len(vars) < 2 {
		t.Fatalf("expected at least 2 column matches for 'user', got %d", len(vars))
	}

	// Check display format: "column_name  DATA_TYPE"
	uid := findByInsert(items, `"user_id"`)
	if uid == nil {
		t.Fatal("expected user_id in completions")
	}
	if uid.Display != "user_id  INTEGER" {
		t.Errorf("display = %q, want %q", uid.Display, "user_id  INTEGER")
	}
	if uid.Kind != KindVariable {
		t.Errorf("kind = %d, want KindVariable (%d)", uid.Kind, KindVariable)
	}

	uname := findByInsert(items, `"username"`)
	if uname == nil {
		t.Fatal("expected username in completions")
	}
	if uname.Display != "username  VARCHAR" {
		t.Errorf("display = %q, want %q", uname.Display, "username  VARCHAR")
	}
}

func TestBuildSchemaColumnsSubstring(t *testing.T) {
	cols := []db.Column{
		{Name: "created_at", DataType: "TIMESTAMP"},
		{Name: "updated_at", DataType: "TIMESTAMP"},
		{Name: "name", DataType: "VARCHAR"},
	}

	// "ated" should match created_at and updated_at via substring
	items := Build("ated", cols, nil)
	vars := itemsOfKind(items, KindVariable)

	found := 0
	for _, v := range vars {
		if v.InsertText == `"created_at"` || v.InsertText == `"updated_at"` {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 column matches for 'ated', got %d", found)
	}
}

// ── Table name completions ─────────────────────────────────────────────────

func TestBuildTableNames(t *testing.T) {
	tables := []db.TableInfo{
		{Name: "users", Type: "table"},
		{Name: "orders", Type: "table"},
		{Name: "user_roles", Type: "table"},
	}

	items := Build("user", nil, tables)

	classes := itemsOfKind(items, KindClass)
	if len(classes) < 2 {
		t.Fatalf("expected at least 2 table matches for 'user', got %d", len(classes))
	}

	u := findByInsert(items, "users")
	if u == nil {
		t.Fatal("expected 'users' table in completions")
	}
	if u.Display != "users  table" {
		t.Errorf("display = %q, want %q", u.Display, "users  table")
	}
	if u.Kind != KindClass {
		t.Errorf("kind = %d, want KindClass (%d)", u.Kind, KindClass)
	}
}

// ── Table column completions (database mode) ──────────────────────────────

func TestBuildTableColumns(t *testing.T) {
	tables := []db.TableInfo{
		{
			Name: "orders",
			Type: "table",
			Columns: []db.Column{
				{Name: "order_id", DataType: "INTEGER"},
				{Name: "total", DataType: "DECIMAL"},
			},
		},
		{
			Name: "products",
			Type: "table",
			Columns: []db.Column{
				{Name: "product_id", DataType: "INTEGER"},
				{Name: "price", DataType: "DECIMAL"},
			},
		},
	}

	// "id" should match order_id and product_id from table columns
	items := Build("id", nil, tables)
	vars := itemsOfKind(items, KindVariable)

	found := map[string]bool{}
	for _, v := range vars {
		found[v.InsertText] = true
	}
	if !found[`"order_id"`] {
		t.Error("expected order_id in completions")
	}
	if !found[`"product_id"`] {
		t.Error("expected product_id in completions")
	}
}

// ── SQL function completions ───────────────────────────────────────────────

func TestBuildFunctions(t *testing.T) {
	items := Build("cou", nil, nil)

	fn := findByInsert(items, "count(")
	if fn == nil {
		t.Fatal("expected count( in completions")
	}
	if fn.Display != "count()" {
		t.Errorf("display = %q, want %q", fn.Display, "count()")
	}
	if fn.Kind != KindFunction {
		t.Errorf("kind = %d, want KindFunction (%d)", fn.Kind, KindFunction)
	}
}

func TestBuildFunctionsSubstring(t *testing.T) {
	// "replace" should match replace() and regexp_replace()
	items := Build("replace", nil, nil)
	fns := itemsOfKind(items, KindFunction)

	found := map[string]bool{}
	for _, f := range fns {
		found[f.InsertText] = true
	}
	if !found["replace("] {
		t.Error("expected replace( in completions")
	}
	if !found["regexp_replace("] {
		t.Error("expected regexp_replace( in completions")
	}
}

// ── SQL keyword completions ────────────────────────────────────────────────

func TestBuildKeywords(t *testing.T) {
	items := Build("sel", nil, nil)

	kw := findByInsert(items, "SELECT ")
	if kw == nil {
		t.Fatal("expected SELECT in completions")
	}
	if kw.Display != "SELECT" {
		t.Errorf("display = %q, want %q", kw.Display, "SELECT")
	}
	if kw.Kind != KindConstant {
		t.Errorf("kind = %d, want KindConstant (%d)", kw.Kind, KindConstant)
	}
	// Insert text should have trailing space
	if !strings.HasSuffix(kw.InsertText, " ") {
		t.Errorf("insert text %q should end with space", kw.InsertText)
	}
}

func TestBuildKeywordsUpperCase(t *testing.T) {
	items := Build("whe", nil, nil)

	kw := findByInsert(items, "WHERE ")
	if kw == nil {
		t.Fatal("expected WHERE in completions")
	}
	if kw.Display != "WHERE" {
		t.Errorf("display = %q, want uppercase %q", kw.Display, "WHERE")
	}
	if kw.InsertText != "WHERE " {
		t.Errorf("insert = %q, want %q", kw.InsertText, "WHERE ")
	}
}

func TestBuildKeywordsCaseInsensitive(t *testing.T) {
	// Typing uppercase should still match
	items := Build("SEL", nil, nil)

	kw := findByInsert(items, "SELECT ")
	if kw == nil {
		t.Fatal("expected SELECT in completions when typing SEL")
	}
}

// ── Deduplication ──────────────────────────────────────────────────────────

func TestBuildDeduplicatesColumnAndKeyword(t *testing.T) {
	// A column named "select" should appear as a column, not also as a keyword
	cols := []db.Column{
		{Name: "select", DataType: "VARCHAR"},
	}

	items := Build("sel", cols, nil)

	// The column should be present
	col := findByInsert(items, `"select"`)
	if col == nil {
		t.Fatal("expected column 'select' in completions")
	}
	if col.Kind != KindVariable {
		t.Errorf("kind = %d, want KindVariable", col.Kind)
	}

	// The keyword SELECT should NOT appear (deduped by lowercase key)
	kw := findByInsert(items, "SELECT ")
	if kw != nil {
		t.Error("keyword SELECT should be deduplicated when column 'select' exists")
	}
}

func TestBuildDeduplicatesColumnAndFunction(t *testing.T) {
	// A column named "count" should appear as column, not also as function
	cols := []db.Column{
		{Name: "count", DataType: "INTEGER"},
	}

	items := Build("cou", cols, nil)

	col := findByInsert(items, `"count"`)
	if col == nil {
		t.Fatal("expected column 'count' in completions")
	}
	if col.Kind != KindVariable {
		t.Errorf("kind = %d, want KindVariable", col.Kind)
	}

	fn := findByInsert(items, "count(")
	if fn != nil {
		t.Error("function count() should be deduplicated when column 'count' exists")
	}
}

func TestBuildDeduplicatesTableColumns(t *testing.T) {
	// Same column name in schema and table columns — should appear only once
	cols := []db.Column{
		{Name: "id", DataType: "INTEGER"},
	}
	tables := []db.TableInfo{
		{
			Name: "users",
			Columns: []db.Column{
				{Name: "id", DataType: "BIGINT"},
			},
		},
	}

	items := Build("id", cols, tables)

	vars := itemsOfKind(items, KindVariable)
	idCount := 0
	for _, v := range vars {
		if v.InsertText == `"id"` {
			idCount++
		}
	}
	if idCount != 1 {
		t.Errorf("expected exactly 1 'id' completion, got %d", idCount)
	}
}

func TestBuildDeduplicatesFunctionAndKeyword(t *testing.T) {
	// "count" exists in both SQLFunctions and SQLKeywords — should appear once as function
	items := Build("coun", nil, nil)

	fns := 0
	kws := 0
	for _, item := range items {
		insert := strings.TrimRight(item.InsertText, " (")
		if strings.EqualFold(insert, "count") {
			if item.Kind == KindFunction {
				fns++
			} else if item.Kind == KindConstant {
				kws++
			}
		}
	}
	if fns != 1 {
		t.Errorf("expected 1 function entry for count, got %d", fns)
	}
	if kws != 0 {
		t.Errorf("expected 0 keyword entries for count (deduped by function), got %d", kws)
	}
}

// ── Empty prefix ───────────────────────────────────────────────────────────

func TestBuildEmptyPrefix(t *testing.T) {
	cols := []db.Column{
		{Name: "id", DataType: "INTEGER"},
	}
	items := Build("", cols, nil)
	if len(items) != 0 {
		t.Errorf("expected no completions for empty prefix, got %d", len(items))
	}
}

// ── No matches ─────────────────────────────────────────────────────────────

func TestBuildNoMatches(t *testing.T) {
	cols := []db.Column{
		{Name: "id", DataType: "INTEGER"},
	}
	items := Build("zzzzz", cols, nil)
	if len(items) != 0 {
		t.Errorf("expected no completions for 'zzzzz', got %d", len(items))
	}
}

// ── WordPrefixAt ───────────────────────────────────────────────────────────

func TestWordPrefixAt(t *testing.T) {
	tests := []struct {
		name string
		line string
		col  int
		want string
	}{
		{"mid-word", "SELECT us", 9, "us"},
		{"after space", "SELECT ", 7, ""},
		{"start of line", "sel", 3, "sel"},
		{"after paren", "count(id", 8, "id"},
		{"after dot", "t.name", 6, "name"},
		{"col 0", "hello", 0, ""},
		{"col past end", "hi", 5, ""},
		{"empty line", "", 0, ""},
		{"underscore", "user_id", 7, "user_id"},
		{"mixed case", "SELECT myCol", 12, "myCol"},
		{"after comma", "a, b", 4, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WordPrefixAt(tt.line, tt.col)
			if got != tt.want {
				t.Errorf("WordPrefixAt(%q, %d) = %q, want %q", tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// ── IsWordChar ─────────────────────────────────────────────────────────────

func TestIsWordChar(t *testing.T) {
	for _, ch := range "abcxyzABCXYZ0189_" {
		if !IsWordChar(byte(ch)) {
			t.Errorf("expected IsWordChar(%q) = true", string(ch))
		}
	}
	for _, ch := range " .,()'\"!@#$%^&*-+=<>/" {
		if IsWordChar(byte(ch)) {
			t.Errorf("expected IsWordChar(%q) = false", string(ch))
		}
	}
}

// ── Priority ordering ──────────────────────────────────────────────────────

func TestBuildColumnsBeforeKeywords(t *testing.T) {
	// Column "select_flag" should appear before keyword SELECT
	cols := []db.Column{
		{Name: "select_flag", DataType: "BOOLEAN"},
	}

	items := Build("sel", cols, nil)
	if len(items) == 0 {
		t.Fatal("expected completions")
	}

	// First item should be the column
	if items[0].Kind != KindVariable {
		t.Errorf("first item kind = %d, want KindVariable; items[0] = %+v", items[0].Kind, items[0])
	}
	if items[0].InsertText != `"select_flag"` {
		t.Errorf("first item insert = %q, want %q", items[0].InsertText, `"select_flag"`)
	}
}
