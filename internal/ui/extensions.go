package ui

import (
	"fmt"
	"strings"

	"bufflehead/internal/db"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/Node"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/TextServer"
	"graphics.gd/classdb/VBoxContainer"
)

// ExtensionsPanel lists DuckDB extensions with their install/load state and
// lets the user install or load them. Mirrors the "DuckDB Extensions" screen
// from the Pro-Grade Data System design, laid out as compact sidebar cards.
type ExtensionsPanel struct {
	VBoxContainer.Extension[ExtensionsPanel] `gd:"ExtensionsPanel"`

	searchBox LineEdit.Instance
	countLbl  Label.Instance
	scrollBox ScrollContainer.Instance
	rowsList  VBoxContainer.Instance
	exts      []db.Extension

	// OnAction is called when the user acts on an extension. install=true means
	// INSTALL (download) then LOAD; install=false means LOAD an already-installed
	// extension.
	OnAction func(name string, install bool)
}

func (p *ExtensionsPanel) Ready() {
	p.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.AsControl().AddThemeConstantOverride("separation", 4)

	p.searchBox = LineEdit.New()
	p.searchBox.SetPlaceholderText("Filter extensions…")
	p.searchBox.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	applyInputTheme(p.searchBox.AsControl())
	p.searchBox.OnTextChanged(func(text string) { p.rebuild(text) })

	p.countLbl = Label.New()
	p.countLbl.SetText("")
	p.countLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	p.countLbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)

	p.scrollBox = ScrollContainer.New()
	p.scrollBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.scrollBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.scrollBox.SetHorizontalScrollMode(ScrollContainer.ScrollModeDisabled)

	p.rowsList = VBoxContainer.New()
	p.rowsList.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.rowsList.AsControl().AddThemeConstantOverride("separation", 4)
	p.scrollBox.AsNode().AddChild(p.rowsList.AsNode())

	p.AsNode().AddChild(p.searchBox.AsNode())
	p.AsNode().AddChild(p.countLbl.AsNode())
	p.AsNode().AddChild(p.scrollBox.AsNode())
}

// SetExtensions replaces the displayed extension list.
func (p *ExtensionsPanel) SetExtensions(exts []db.Extension) {
	p.exts = exts
	loaded, installed := 0, 0
	for _, e := range exts {
		if e.Loaded {
			loaded++
		}
		if e.Installed {
			installed++
		}
	}
	p.countLbl.SetText(fmt.Sprintf("%d loaded · %d installed · %d total", loaded, installed, len(exts)))
	p.rebuild(p.searchBox.Text())
}

func (p *ExtensionsPanel) rebuild(query string) {
	for p.rowsList.AsNode().GetChildCount() > 0 {
		child := p.rowsList.AsNode().GetChild(0)
		p.rowsList.AsNode().RemoveChild(child)
		child.QueueFree()
	}
	q := strings.ToLower(query)
	for _, e := range p.exts {
		if q != "" && !strings.Contains(strings.ToLower(e.Name), q) && !strings.Contains(strings.ToLower(e.Description), q) {
			continue
		}
		p.rowsList.AsNode().AddChild(p.makeRow(e))
	}
}

// makeRow builds one extension card: name + status chip, description, and an
// Install/Load action button (loaded extensions show no action).
func (p *ExtensionsPanel) makeRow(e db.Extension) Node.Instance {
	card := PanelContainer.New()
	card.AsNode().SetName("ExtensionRow")
	card.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	cardBg := makeStyleBoxPadded(colorBgPanel, 4, 1, colorBorderDim, 6)
	card.AsControl().AddThemeStyleboxOverride("panel", cardBg.AsStyleBox())
	if e.Description != "" {
		card.AsControl().SetTooltipText(e.Description)
	}

	box := VBoxContainer.New()
	box.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	box.AsControl().AddThemeConstantOverride("separation", 3)

	// Top: name (monospace) + status chip
	topRow := HBoxContainer.New()
	topRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	topRow.AsControl().AddThemeConstantOverride("separation", 6)

	nameLbl := Label.New()
	nameLbl.SetText(e.Name)
	nameLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	nameLbl.AsControl().AddThemeColorOverride("font_color", colorText)
	nameLbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	nameLbl.SetTextOverrunBehavior(TextServer.OverrunTrimEllipsis)
	nameLbl.SetClipText(true)
	nameLbl.AsControl().AddThemeFontOverride("font", monoFont())

	statusText, statusColor := "AVAILABLE", colorTextDim
	switch {
	case e.Loaded:
		statusText, statusColor = "LOADED", colorStatusGreen
	case e.Installed:
		statusText, statusColor = "INSTALLED", colorTypeInt
	}

	topRow.AsNode().AddChild(nameLbl.AsNode())
	topRow.AsNode().AddChild(makeAccentChip(statusText, statusColor, "ExtensionStatus").AsNode())

	box.AsNode().AddChild(topRow.AsNode())

	// Description (truncated)
	if e.Description != "" {
		descLbl := Label.New()
		descLbl.SetText(e.Description)
		descLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
		descLbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
		descLbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		descLbl.SetTextOverrunBehavior(TextServer.OverrunTrimEllipsis)
		descLbl.SetClipText(true)
		box.AsNode().AddChild(descLbl.AsNode())
	}

	// Action: Load (installed, not loaded) or Install (not installed).
	if !e.Loaded {
		actionRow := HBoxContainer.New()
		actionRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

		spacer := Control.New()
		spacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)
		actionRow.AsNode().AddChild(spacer.AsNode())

		btn := Button.New()
		install := !e.Installed
		if install {
			btn.SetText("Install")
		} else {
			btn.SetText("Load")
		}
		btn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
		applySecondaryButtonTheme(btn.AsControl())
		name := e.Name
		btn.AsBaseButton().OnPressed(func() {
			if p.OnAction != nil {
				p.OnAction(name, install)
			}
		})
		actionRow.AsNode().AddChild(btn.AsNode())
		box.AsNode().AddChild(actionRow.AsNode())
	}

	card.AsNode().AddChild(box.AsNode())
	return card.AsNode()
}
