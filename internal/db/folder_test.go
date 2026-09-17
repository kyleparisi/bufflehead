package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeTestFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("../../testdata/cities.parquet")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMatchDataSuffix(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"a.parquet", ".parquet", true},
		{"a.csv", ".csv", true},
		{"a.csv.gz", ".csv.gz", true}, // longest suffix wins over ".gz" alone
		{"a.PARQUET", ".PARQUET", true},
		{"a.Csv.Gz", ".Csv.Gz", true},
		{"a.txt", "", false},
		{"a.gz", "", false}, // bare .gz says nothing about the format
		{"parquet", "", false},
		{"a.parquet.bak", "", false},
	}
	for _, c := range cases {
		got, ok := matchDataSuffix(c.name)
		if ok != c.wantOK || got != c.want {
			t.Errorf("matchDataSuffix(%q) = (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestScanFolderOrdersByCountThenFormat(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.csv", "b.csv", "c.csv"} {
		writeTestFile(t, dir, n)
	}
	for _, n := range []string{"x.parquet", "y.parquet", "z.parquet"} {
		writeTestFile(t, dir, n)
	}
	writeTestFile(t, dir, "one.json")
	writeTestFile(t, dir, "notes.txt")
	writeTestFile(t, dir, ".DS_Store")
	writeTestFile(t, dir, "._sidecar.parquet")
	if err := os.MkdirAll(filepath.Join(dir, "nested.parquet"), 0o755); err != nil {
		t.Fatal(err)
	}

	pats, err := ScanFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []FolderPattern{
		{Glob: "*.parquet", Suffix: ".parquet", Count: 3}, // ties with csv on count, parquet ranks first
		{Glob: "*.csv", Suffix: ".csv", Count: 3},
		{Glob: "*.json", Suffix: ".json", Count: 1},
	}
	if len(pats) != len(want) {
		t.Fatalf("got %d patterns %v, want %d", len(pats), pats, len(want))
	}
	for i := range want {
		if pats[i] != want[i] {
			t.Errorf("pattern %d = %+v, want %+v", i, pats[i], want[i])
		}
	}
}

func TestScanFolderEmptyAndMissing(t *testing.T) {
	empty := t.TempDir()
	pats, err := ScanFolder(empty)
	if err != nil {
		t.Fatalf("empty dir: %v", err)
	}
	if len(pats) != 0 {
		t.Errorf("empty dir: got %v, want none", pats)
	}

	if _, err := ScanFolder(filepath.Join(empty, "nope")); err == nil {
		t.Error("missing dir: want error, got nil")
	}
}

func TestFolderQueryQuoting(t *testing.T) {
	if got, want := FolderQuery("/data/sales", "*.parquet"), `SELECT * FROM '/data/sales/*.parquet'`; got != want {
		t.Errorf("FolderQuery = %q, want %q", got, want)
	}
	// A trailing separator must not double up.
	if got, want := FolderQuery("/data/sales/", "*.csv"), `SELECT * FROM '/data/sales/*.csv'`; got != want {
		t.Errorf("FolderQuery trailing sep = %q, want %q", got, want)
	}
	// A quote in the folder name must not break out of the literal.
	if got, want := FolderQuery("/data/o'brien", "*.csv"), `SELECT * FROM '/data/o''brien/*.csv'`; got != want {
		t.Errorf("FolderQuery apostrophe = %q, want %q", got, want)
	}
	// Glob metacharacters in the folder name are escaped; the glob itself is not.
	if got, want := FolderGlobPath("/data/q[1]", "*.parquet"), "/data/q[[]1]/*.parquet"; got != want {
		t.Errorf("FolderGlobPath bracket = %q, want %q", got, want)
	}
	if got, want := FolderGlobPath("/data/a*b", "*.parquet"), "/data/a[*]b/*.parquet"; got != want {
		t.Errorf("FolderGlobPath star = %q, want %q", got, want)
	}
}

// TestFolderQueryAgainstDuckDB is the point of the feature: the generated SQL
// must actually read the folder, including folders whose own names contain
// characters DuckDB's globber would otherwise interpret.
func TestFolderQueryAgainstDuckDB(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	base := t.TempDir()
	for _, dirName := range []string{"plain", "a*b", "q[1]", "o'brien"} {
		dir := filepath.Join(base, dirName)
		writeTestFile(t, dir, "one.parquet")
		writeTestFile(t, dir, "two.parquet")

		q := FolderQuery(dir, "*.parquet")
		cols, err := d.Schema(FolderGlobPath(dir, "*.parquet"))
		if err != nil {
			t.Errorf("Schema(%s): %v", dirName, err)
			continue
		}
		if len(cols) != 2 {
			t.Errorf("Schema(%s): got %d cols, want 2", dirName, len(cols))
		}
		res, err := d.Query(context.Background(), q, 0, 100)
		if err != nil {
			t.Errorf("Query(%s): %v", dirName, err)
			continue
		}
		// cities.parquet holds 3 rows; two copies is 6.
		if res.Total != 6 {
			t.Errorf("Query(%s): got %d rows, want 6", dirName, res.Total)
		}
	}
}
