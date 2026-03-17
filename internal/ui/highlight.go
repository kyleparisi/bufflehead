package ui

import (
	"strings"

	"bufflehead/internal/completion"

	"graphics.gd/classdb/CodeEdit"
	"graphics.gd/classdb/CodeHighlighter"
)

func setupSQLHighlighter(editor CodeEdit.Instance) {
	hl := CodeHighlighter.New()
	hl.SetNumberColor(colorSQLNumber)
	hl.SetSymbolColor(colorSQLSymbol)
	hl.SetFunctionColor(colorSQLFunction)

	for _, kw := range completion.SQLKeywords {
		hl.AddKeywordColor(kw, colorSQLKeyword)
		hl.AddKeywordColor(strings.ToLower(kw), colorSQLKeyword)
	}

	// String highlighting
	hl.AddColorRegion("'", "'", colorSQLString)

	// Comment highlighting
	hl.MoreArgs().AddColorRegion("--", "", colorSQLComment, true)
	hl.AddColorRegion("/*", "*/", colorSQLComment)

	hl.AsSyntaxHighlighter().Set(editor.AsTextEdit())
}
