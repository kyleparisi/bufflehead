package ui

import (
	"strings"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/TextEdit"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Vector2"
)

// ConsolePanel is a read-only, copyable log/error console shown in a collapsible
// bottom pane. It drains the process-wide console log sink each frame and
// mirrors new lines into a selectable TextEdit so users can copy/paste error
// output for sharing.
type ConsolePanel struct {
	PanelContainer.Extension[ConsolePanel] `gd:"ConsolePanel"`

	output   TextEdit.Instance
	lastSeq  uint64
	autoTail bool
}

func (c *ConsolePanel) Ready() {
	c.autoTail = true

	bg := makeStyleBox(colorBgDarker, 0, 0, colorBgDarker)
	bg.SetBorderWidthTop(1)
	bg.SetBorderColor(colorBorder)
	c.AsControl().AddThemeStyleboxOverride("panel", bg.AsStyleBox())
	c.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	vbox := VBoxContainer.New()
	vbox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	vbox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	vbox.AsControl().AddThemeConstantOverride("separation", 2)

	// Header: title + Copy All + Clear
	header := HBoxContainer.New()
	header.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	header.AsControl().AddThemeConstantOverride("separation", 6)

	title := Label.New()
	title.SetText("CONSOLE")
	title.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	title.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	title.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	copyBtn := Button.New()
	copyBtn.SetText("Copy All")
	copyBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(9))
	applySecondaryButtonTheme(copyBtn.AsControl())
	copyBtn.AsBaseButton().OnPressed(func() {
		lines, _ := consoleLog.snapshot()
		DisplayServer.ClipboardSet(strings.Join(lines, "\n"))
	})

	clearBtn := Button.New()
	clearBtn.SetText("Clear")
	clearBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(9))
	applySecondaryButtonTheme(clearBtn.AsControl())
	clearBtn.AsBaseButton().OnPressed(func() {
		consoleLog.clear()
		c.output.SetText("")
		c.lastSeq = consoleLog.seqNum()
	})

	header.AsNode().AddChild(title.AsNode())
	header.AsNode().AddChild(copyBtn.AsNode())
	header.AsNode().AddChild(clearBtn.AsNode())

	// Read-only, selectable output.
	c.output = TextEdit.New()
	c.output.SetEditable(false)
	c.output.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	c.output.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	c.output.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(60)))
	applyTextEditTheme(c.output.AsControl())
	c.output.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))

	vbox.AsNode().AddChild(header.AsNode())
	vbox.AsNode().AddChild(c.output.AsNode())
	c.AsNode().AddChild(vbox.AsNode())

	// Seed with whatever has been logged before the panel existed.
	c.refresh()
}

// Process drains any new console lines into the output on the main thread.
func (c *ConsolePanel) Process(delta Float.X) {
	if consoleLog.seqNum() == c.lastSeq {
		return
	}
	c.refresh()
}

// refresh rebuilds the output text from the sink and tails to the newest line.
func (c *ConsolePanel) refresh() {
	lines, seq := consoleLog.snapshot()
	c.lastSeq = seq
	c.output.SetText(strings.Join(lines, "\n"))
	if c.autoTail {
		last := c.output.AsTextEdit().GetLineCount() - 1
		if last < 0 {
			last = 0
		}
		c.output.SetCaretLine(last)
		c.output.SetLineAsFirstVisible(last)
	}
}
