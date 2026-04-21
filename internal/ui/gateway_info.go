package ui

import (
	"fmt"

	"bufflehead/internal/db"
	"bufflehead/internal/models"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Vector2"
)

// GatewayInfoPanel shows connection details and copyable snippets for other tools.
type GatewayInfoPanel struct {
	VBoxContainer.Extension[GatewayInfoPanel] `gd:"GatewayInfoPanel"`

	entry        models.GatewayEntry
	tables       []db.TableInfo
	statusLabel  Label.Instance
	uptimeLabel  Label.Instance
	aiSnippetBox VBoxContainer.Instance // container for AI prompt snippet, populated after tables load
	OnDisconnect func()
}

func (p *GatewayInfoPanel) SetEntry(entry models.GatewayEntry) {
	p.entry = entry
}

func (p *GatewayInfoPanel) SetTables(tables []db.TableInfo) {
	p.tables = tables
	if p.aiSnippetBox.AsNode().GetChildCount() > 0 {
		// Clear and rebuild
		for p.aiSnippetBox.AsNode().GetChildCount() > 0 {
			child := p.aiSnippetBox.AsNode().GetChild(0)
			p.aiSnippetBox.AsNode().RemoveChild(child)
			child.QueueFree()
		}
	}
	prompt := p.buildAIPrompt()
	// Truncate display to keep it readable
	display := prompt
	if len(display) > 600 {
		display = display[:600] + "\n..."
	}
	snippet := p.makeSnippet("AI Prompt", display, prompt)
	p.aiSnippetBox.AsNode().AddChild(snippet.AsNode())
}

func (p *GatewayInfoPanel) Ready() {
	p.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.AsControl().AddThemeConstantOverride("separation", 12)

	scroll := ScrollContainer.New()
	scroll.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	scroll.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)

	content := VBoxContainer.New()
	content.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	content.AsControl().AddThemeConstantOverride("separation", 12)

	entry := p.entry
	password := entry.ResolvePassword()
	localPort := entry.LocalPort

	// Header
	header := Label.New()
	header.SetText("Gateway: " + entry.Name)
	header.AsControl().AddThemeFontSizeOverride("font_size", fontSize(14))
	header.AsControl().AddThemeColorOverride("font_color", colorText)
	content.AsNode().AddChild(header.AsNode())

	// Status info
	p.statusLabel = Label.New()
	p.statusLabel.SetText("Status: Connected")
	p.statusLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	p.statusLabel.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
	content.AsNode().AddChild(p.statusLabel.AsNode())

	// Connection details
	details := []struct{ label, value string }{
		{"Endpoint", fmt.Sprintf("localhost:%d", localPort)},
		{"Database", entry.DBName},
		{"User", entry.DBUser},
	}
	for _, d := range details {
		row := p.makeDetailRow(d.label, d.value)
		content.AsNode().AddChild(row.AsNode())
	}

	// Separator
	sepLabel := Label.New()
	sepLabel.SetText("-- Connect with other tools --")
	sepLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	sepLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	sepLabel.SetHorizontalAlignment(1)
	content.AsNode().AddChild(sepLabel.AsNode())

	// psql snippet
	psqlCmd := fmt.Sprintf("psql -h localhost -p %d -U %s -d %s", localPort, entry.DBUser, entry.DBName)
	psqlCopy := psqlCmd
	if password != "" {
		psqlCopy = fmt.Sprintf("PGPASSWORD='%s' %s", password, psqlCmd)
	}
	content.AsNode().AddChild(p.makeSnippet("psql", psqlCmd, psqlCopy).AsNode())

	// Connection URL
	connURL := fmt.Sprintf("postgresql://%s:@localhost:%d/%s", entry.DBUser, localPort, entry.DBName)
	connURLCopy := connURL
	if password != "" {
		connURLCopy = fmt.Sprintf("postgresql://%s:%s@localhost:%d/%s", entry.DBUser, password, localPort, entry.DBName)
	}
	content.AsNode().AddChild(p.makeSnippet("Connection URL", connURL, connURLCopy).AsNode())

	// Python snippet
	pythonSnippet := fmt.Sprintf(`engine = create_engine("%s")`, connURL)
	pythonCopy := fmt.Sprintf(`engine = create_engine("%s")`, connURLCopy)
	content.AsNode().AddChild(p.makeSnippet("Python", pythonSnippet, pythonCopy).AsNode())

	// Claude MCP config
	mcpConfig := fmt.Sprintf(`{"mcpServers":{"pg":{"command":"npx","args":["-y","@modelcontextprotocol/server-postgres","%s"]}}}`, connURLCopy)
	mcpDisplay := fmt.Sprintf(`{"mcpServers":{"pg":{"command":"npx","args":["-y","@modelcontextprotocol/server-postgres","%s"]}}}`, connURL)
	content.AsNode().AddChild(p.makeSnippet("Claude MCP config", mcpDisplay, mcpConfig).AsNode())

	// AI Prompt snippet (placeholder — populated when SetTables is called)
	p.aiSnippetBox = VBoxContainer.New()
	p.aiSnippetBox.AsControl().AddThemeConstantOverride("separation", 4)
	content.AsNode().AddChild(p.aiSnippetBox.AsNode())

	// If tables are already set, build the snippet now
	if len(p.tables) > 0 {
		prompt := p.buildAIPrompt()
		display := prompt
		if len(display) > 600 {
			display = display[:600] + "\n..."
		}
		snippet := p.makeSnippet("AI Prompt", display, prompt)
		p.aiSnippetBox.AsNode().AddChild(snippet.AsNode())
	}

	// Disconnect button
	disconnectBtn := Button.New()
	disconnectBtn.SetText("Disconnect")
	disconnectBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	applySecondaryButtonTheme(disconnectBtn.AsControl())
	disconnectBtn.AsBaseButton().OnPressed(func() {
		if p.OnDisconnect != nil {
			p.OnDisconnect()
		}
	})
	content.AsNode().AddChild(disconnectBtn.AsNode())

	scroll.AsNode().AddChild(content.AsNode())
	p.AsNode().AddChild(scroll.AsNode())
}

func (p *GatewayInfoPanel) buildAIPrompt() string {
	return buildAIPrompt(p.entry, p.tables)
}

func (p *GatewayInfoPanel) SetStatus(status string) {
	p.statusLabel.SetText("Status: " + status)
}

func (p *GatewayInfoPanel) makeDetailRow(label, value string) HBoxContainer.Instance {
	row := HBoxContainer.New()
	row.AsControl().AddThemeConstantOverride("separation", 8)

	lbl := Label.New()
	lbl.SetText(label + ":")
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	lbl.AsControl().SetCustomMinimumSize(Vector2.New(scaled(80), 0))

	val := Label.New()
	val.SetText(value)
	val.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	val.AsControl().AddThemeColorOverride("font_color", colorText)

	row.AsNode().AddChild(lbl.AsNode())
	row.AsNode().AddChild(val.AsNode())
	return row
}

func (p *GatewayInfoPanel) makeSnippet(title, display, copyText string) PanelContainer.Instance {
	panel := PanelContainer.New()
	bg := makeStyleBox(colorBgInput, 4, 1, colorBorderDim)
	bg.AsStyleBox().SetContentMarginAll(scaled(8))
	panel.AsControl().AddThemeStyleboxOverride("panel", bg.AsStyleBox())

	vbox := VBoxContainer.New()
	vbox.AsControl().AddThemeConstantOverride("separation", 4)

	titleRow := HBoxContainer.New()
	titleRow.AsControl().AddThemeConstantOverride("separation", 4)

	titleLabel := Label.New()
	titleLabel.SetText(title + ":")
	titleLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	titleLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	titleLabel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	copyBtn := Button.New()
	copyBtn.SetText("Copy")
	copyBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(9))
	applySecondaryButtonTheme(copyBtn.AsControl())
	copyBtn.AsBaseButton().OnPressed(func() {
		DisplayServer.ClipboardSet(copyText)
	})

	titleRow.AsNode().AddChild(titleLabel.AsNode())
	titleRow.AsNode().AddChild(copyBtn.AsNode())

	codeLabel := Label.New()
	codeLabel.SetText(display)
	codeLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	codeLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	codeLabel.SetAutowrapMode(3) // word wrap

	vbox.AsNode().AddChild(titleRow.AsNode())
	vbox.AsNode().AddChild(codeLabel.AsNode())
	panel.AsNode().AddChild(vbox.AsNode())

	return panel
}
