package ui

import (
	"strings"

	"graphics.gd/classdb/CodeEdit"
	"graphics.gd/classdb/CodeHighlighter"
)

var sqlKeywords = []string{
	"SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "IS", "NULL",
	"AS", "ON", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "FULL", "CROSS",
	"GROUP", "BY", "ORDER", "ASC", "DESC", "HAVING", "LIMIT", "OFFSET",
	"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "DROP", "CREATE",
	"TABLE", "INDEX", "VIEW", "ALTER", "ADD", "COLUMN", "PRIMARY", "KEY",
	"FOREIGN", "REFERENCES", "UNIQUE", "CHECK", "DEFAULT", "CONSTRAINT",
	"UNION", "ALL", "DISTINCT", "BETWEEN", "LIKE", "ILIKE", "EXISTS",
	"CASE", "WHEN", "THEN", "ELSE", "END", "CAST", "COALESCE", "NULLIF",
	"TRUE", "FALSE", "WITH", "RECURSIVE", "EXPLAIN", "ANALYZE",
	"COUNT", "SUM", "AVG", "MIN", "MAX", "OVER", "PARTITION", "WINDOW",
	"ROW_NUMBER", "RANK", "DENSE_RANK", "FIRST_VALUE", "LAST_VALUE",
	"COPY", "FORMAT", "PARQUET", "CSV", "JSON", "READ_PARQUET", "READ_CSV",
}

func setupSQLHighlighter(editor CodeEdit.Instance) {
	hl := CodeHighlighter.New()
	hl.SetNumberColor(colorSQLNumber)
	hl.SetSymbolColor(colorSQLSymbol)
	hl.SetFunctionColor(colorSQLFunction)

	for _, kw := range sqlKeywords {
		hl.AddKeywordColor(kw, colorSQLKeyword)
		hl.AddKeywordColor(strings.ToLower(kw), colorSQLKeyword)
	}

	// String highlighting
	hl.AddColorRegion("'", "'", colorSQLString)

	// Comment highlighting
	hl.MoreArgs().AddColorRegion("--", "", colorSQLComment, true)
	hl.AddColorRegion("/*", "*/", colorSQLComment)

	// Use patched Instance-level SetSyntaxHighlighter
	editor.AsTextEdit().SetSyntaxHighlighter(hl)
}
