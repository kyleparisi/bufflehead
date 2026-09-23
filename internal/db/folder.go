package db

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dataFileSuffixes are the file suffixes DuckDB can read directly from a path,
// longest first so ".csv.gz" wins over ".gz" when both could match. The order
// within a compression family also sets the tie-break order in ScanFolder.
var dataFileSuffixes = []string{
	".parquet",
	".csv", ".csv.gz", ".csv.zst",
	".tsv", ".tsv.gz", ".tsv.zst",
	".json", ".json.gz", ".json.zst",
	".jsonl", ".jsonl.gz", ".jsonl.zst",
	".ndjson", ".ndjson.gz", ".ndjson.zst",
}

// suffixRank orders patterns that have the same file count, so the dominant
// pattern of a mixed folder is stable and parquet-first.
var suffixRank = func() map[string]int {
	m := make(map[string]int, len(dataFileSuffixes))
	for i, s := range dataFileSuffixes {
		m[s] = i
	}
	return m
}()

// FolderPattern is one glob available in a dropped folder: every file directly
// inside it that shares a data-file suffix, counted.
type FolderPattern struct {
	Glob   string // "*.parquet", relative to the folder
	Suffix string // ".parquet" — the exact spelling found on disk
	Count  int
}

// matchDataSuffix returns the longest data-file suffix that name ends with,
// preserving the spelling on disk: globs are case-sensitive in DuckDB, so a
// folder of ".PARQUET" files must be offered as "*.PARQUET" to match anything.
func matchDataSuffix(name string) (string, bool) {
	var best string
	lower := strings.ToLower(name)
	for _, s := range dataFileSuffixes {
		if len(s) > len(best) && strings.HasSuffix(lower, s) {
			best = s
		}
	}
	if best == "" {
		return "", false
	}
	return name[len(name)-len(best):], true
}

// ScanFolder lists the data-file globs directly inside dir, most files first.
// Subdirectories are not descended into and are never themselves candidates.
// An empty result means the folder holds nothing DuckDB can read.
func ScanFolder(dir string) ([]FolderPattern, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan folder: %w", err)
	}

	counts := make(map[string]int)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// A dotfile is never data the user meant to query (.DS_Store and
		// friends), and macOS AppleDouble sidecars (._foo.parquet) carry a
		// real suffix but are not readable files.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if suffix, ok := matchDataSuffix(e.Name()); ok {
			counts[suffix]++
		}
	}

	pats := make([]FolderPattern, 0, len(counts))
	for suffix, n := range counts {
		pats = append(pats, FolderPattern{Glob: "*" + suffix, Suffix: suffix, Count: n})
	}
	sort.Slice(pats, func(i, j int) bool {
		if pats[i].Count != pats[j].Count {
			return pats[i].Count > pats[j].Count
		}
		ri, oki := suffixRank[strings.ToLower(pats[i].Suffix)]
		rj, okj := suffixRank[strings.ToLower(pats[j].Suffix)]
		if oki && okj && ri != rj {
			return ri < rj
		}
		return pats[i].Glob < pats[j].Glob
	})
	return pats, nil
}

// globMetaChars are the characters DuckDB's globber treats specially. A folder
// whose own path contains one would otherwise turn into a pattern that matches
// the wrong files (or nothing).
const globMetaChars = `*?[`

// escapeGlobLiteral quotes glob metacharacters in a literal path segment using
// single-character classes, which DuckDB matches literally.
func escapeGlobLiteral(s string) string {
	if !strings.ContainsAny(s, globMetaChars) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		if strings.ContainsRune(globMetaChars, r) {
			b.WriteString("[" + string(r) + "]")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FolderGlobPath joins a folder with one of its globs into the path DuckDB
// should read. The directory part is escaped as a literal; the glob is not.
func FolderGlobPath(dir, glob string) string {
	// DuckDB accepts forward slashes on Windows too. Normalize before trimming
	// so both native paths and paths already using slashes join consistently.
	dir = filepath.ToSlash(dir)
	return escapeGlobLiteral(strings.TrimRight(dir, "/")) + "/" + glob
}

// FolderQuery is the default query for one glob in a dropped folder.
func FolderQuery(dir, glob string) string {
	return fmt.Sprintf("SELECT * FROM %s", QuotePathLiteral(FolderGlobPath(dir, glob)))
}

// QuotePathLiteral renders a filesystem path as a DuckDB string literal. Paths
// reach SQL by interpolation, and an apostrophe in a folder or file name
// ("O'Brien/sales.parquet") would otherwise close the literal early — the rest
// of the path then parses as SQL, which fails confusingly or, with a crafted
// name, runs.
func QuotePathLiteral(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}
