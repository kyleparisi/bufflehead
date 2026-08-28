package db

import (
	"fmt"
	"regexp"
	"strings"
)

// maxResultRows is a hard ceiling on the number of rows the row-based backends
// (DuckDB, Postgres, MySQL) materialize from a single Query, regardless of a
// user-supplied LIMIT. It bounds memory and response size so an agent can't
// stream millions of rows through /sql. Each of those backends' row loops stops
// accumulating at this count even when the user wrote a larger LIMIT. The grid
// pages in blocks of 100, far below this, so normal use is never truncated.
//
// BigQuery is deliberately exempt: it is bounded by bytes scanned
// (MaxBytesBilled), which is its real cost lever — a row cap there is meaningless
// since a LIMIT doesn't reduce bytes scanned.
const maxResultRows = 10000

// sqlTrailingLimitRe matches a LIMIT clause at the very end of a statement
// (after whitespace/semicolons are trimmed). It covers the standard
// `LIMIT n [OFFSET m]` form and MySQL's `LIMIT m, n` form.
var sqlTrailingLimitRe = regexp.MustCompile(`(?i)\bLIMIT\s+\d+(\s*,\s*\d+)?(\s+OFFSET\s+\d+)?$`)

// trimSQL strips trailing semicolons and surrounding whitespace so a statement
// can be safely embedded in a subquery or have paging appended.
func trimSQL(sql string) string {
	return strings.TrimRight(strings.TrimSpace(sql), "; \t\r\n")
}

// hasTrailingLimit reports whether sql already ends with its own LIMIT clause.
func hasTrailingLimit(sql string) bool {
	return sqlTrailingLimitRe.MatchString(sql)
}

// paginate appends LIMIT/OFFSET to sql for server-side pagination — unless sql
// already carries its own trailing LIMIT, in which case the caller's query
// controls the row count and it is returned untouched. This avoids emitting an
// invalid double clause like "... LIMIT 5 LIMIT 100 OFFSET 0". A trailing ';'
// and surrounding whitespace are stripped first.
//
// The grid path never hits the untouched branch: AppState.VirtualSQL wraps the
// user query in a subquery, so any user LIMIT is nested and the page window is
// appended as usual. The /sql control path passes raw user SQL, so an agent that
// writes its own LIMIT now works instead of erroring.
func paginate(sql string, offset, limit int) string {
	s := trimSQL(sql)
	if hasTrailingLimit(s) {
		return s
	}
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", s, limit, offset)
}
