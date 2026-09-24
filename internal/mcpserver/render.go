package mcpserver

import (
	"fmt"
	"strings"
)

// renderTable formats rows as a markdown table for the model to read, capped
// at maxRows rows and roughly maxBytes characters. The structured result
// carries the full page; this is the human-readable summary beside it.
func renderTable(cols []string, rows [][]string, maxRows, maxBytes int) (string, int) {
	if len(cols) == 0 {
		return "(no columns)", 0
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("|")
		for i := range cols {
			v := ""
			if i < len(cells) {
				v = cells[i]
			}
			b.WriteString(" ")
			b.WriteString(escapeCell(v))
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	writeRow(cols)
	b.WriteString("|")
	for range cols {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	shown := 0
	for _, r := range rows {
		if shown >= maxRows || b.Len() >= maxBytes {
			break
		}
		writeRow(r)
		shown++
	}
	return b.String(), shown
}

// escapeCell keeps a value on one table row: pipes are escaped and newlines
// collapsed. Long values are clipped so one wide cell cannot eat the budget.
func escapeCell(v string) string {
	const maxCell = 200
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "|", "\\|")
	if len(v) > maxCell {
		v = v[:maxCell] + "…"
	}
	return v
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
