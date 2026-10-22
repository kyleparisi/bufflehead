package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"time"

	"bufflehead/internal/mcpsetup"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CenterContainer"
	"graphics.gd/classdb/ColorRect"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventKey"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/RichTextLabel"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/TextServer"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Color"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
)

const mcpSetupModalWidth = 560

// "Connect an AI Assistant…" (Help menu): registers the headless MCP server
// mode (internal/mcpmode) with AI clients. Events mutate a.mcpSetup through
// a.mcpSetupEvent, which then calls renderMCPSetup; rendering only reads it.

type mcpSetupPhase int

const (
	mcpSetupClosed  mcpSetupPhase = iota
	mcpSetupList                  // one row per client
	mcpSetupConfirm               // the exact Claude Desktop change, before writing
	mcpSetupDone                  // written; restart Claude Desktop
)

func (p mcpSetupPhase) String() string {
	return [...]string{"closed", "list", "confirm", "done"}[p]
}

type mcpSetupState struct {
	Phase   mcpSetupPhase
	Cmd     mcpsetup.Command
	CmdErr  string
	Desktop bool           // show the Claude Desktop row
	Plan    *mcpsetup.Plan // nil if PlanErr
	PlanErr string
	Backup  string // backup written by the last Apply
	Err     string // Apply failure, shown on the confirm screen
	Copied  string // "code" or "json" after a copy, for the button label
}

// mcpLaunchCommand is the executable and args this install should be launched
// with in MCP mode.
func mcpLaunchCommand() (mcpsetup.Command, error) {
	return mcpsetup.LaunchCommand(mcpsetup.OSEnv(runtime.GOOS))
}

// mcpDesktopSupported: Claude Desktop has no official Linux build, and the Mac
// App Store sandbox can't write outside its container.
func mcpDesktopSupported() bool {
	return runtime.GOOS != "linux" && !storeBuild
}

func (a *App) mcpSetupEvent(f func(s *mcpSetupState)) {
	f(&a.mcpSetup)
	a.renderMCPSetup()
}

func (a *App) openMCPSetup() {
	a.mcpSetupEvent(func(s *mcpSetupState) {
		*s = mcpSetupState{Phase: mcpSetupList, Desktop: mcpDesktopSupported()}
		cmd, err := mcpLaunchCommand()
		if err != nil {
			s.CmdErr = err.Error()
			return
		}
		s.Cmd = cmd
		if s.Desktop {
			s.replan()
		}
	})
}

// replan re-reads Claude Desktop's config. It runs on open and after a write,
// so the row reflects the file as it is now.
func (s *mcpSetupState) replan() {
	s.Plan, s.PlanErr = nil, ""
	path, err := mcpsetup.ClaudeDesktopConfigPath(mcpsetup.OSEnv(runtime.GOOS))
	if err == nil {
		s.Plan, err = mcpsetup.PlanClaudeDesktop(path, s.Cmd)
	}
	if err != nil {
		s.PlanErr = err.Error()
	}
}

func (a *App) mcpSetupReview() {
	a.mcpSetupEvent(func(s *mcpSetupState) {
		if s.Phase != mcpSetupList || s.Plan == nil || s.Plan.Status == mcpsetup.Current {
			return
		}
		s.replan() // the file may have changed while the dialog was open
		if s.Plan != nil {
			s.Phase, s.Err = mcpSetupConfirm, ""
		}
	})
}

func (a *App) mcpSetupApply() {
	a.mcpSetupEvent(func(s *mcpSetupState) {
		if s.Phase != mcpSetupConfirm || s.Plan == nil {
			return
		}
		backup, err := s.Plan.Apply(time.Now())
		if err != nil {
			log.Printf("[mcp-setup] write %s: %v", s.Plan.Path, err)
			s.Err = err.Error()
			return
		}
		log.Printf("[mcp-setup] wrote %s (backup %q)", s.Plan.Path, backup)
		s.Backup = backup
		s.Phase = mcpSetupDone
		s.replan()
	})
}

func (a *App) mcpSetupBack() {
	a.mcpSetupEvent(func(s *mcpSetupState) {
		if s.Phase != mcpSetupClosed {
			s.Phase, s.Err = mcpSetupList, ""
		}
	})
}

func (a *App) mcpSetupCopy(kind string) {
	a.mcpSetupEvent(func(s *mcpSetupState) {
		if s.Phase != mcpSetupList || s.CmdErr != "" {
			return
		}
		switch kind {
		case "code":
			DisplayServer.ClipboardSet(s.Cmd.ClaudeCodeCommand(runtime.GOOS))
		case "json":
			DisplayServer.ClipboardSet(s.Cmd.EntryJSON())
		default:
			return
		}
		s.Copied = kind
	})
}

func (a *App) closeMCPSetup() {
	a.mcpSetupEvent(func(s *mcpSetupState) { *s = mcpSetupState{} })
}

// snapshot is the dialog state for the control API's /state.
func (s mcpSetupState) snapshot() map[string]any {
	m := map[string]any{"phase": s.Phase.String(), "command": s.Cmd.Path, "desktop": s.Desktop, "copied": s.Copied}
	if s.CmdErr != "" {
		m["error"] = s.CmdErr
	}
	if s.Plan != nil {
		m["configPath"] = s.Plan.Path
		m["desktopStatus"] = desktopStatusName(s.Plan.Status)
	}
	if s.PlanErr != "" {
		m["planError"] = s.PlanErr
	}
	if s.Err != "" {
		m["applyError"] = s.Err
	}
	if s.Backup != "" {
		m["backup"] = s.Backup
	}
	return m
}

func desktopStatusName(st mcpsetup.Status) string {
	return [...]string{"absent", "current", "changed"}[st]
}

// renderMCPSetup rebuilds the overlay when the visible state changes. It reads
// a.mcpSetup and never writes it.
func (a *App) renderMCPSetup() {
	w := a.mainWin
	s := a.mcpSetup
	key := ""
	if s.Phase != mcpSetupClosed && w != nil && w.rootPanel != (PanelContainer.Instance{}) {
		status := -1
		if s.Plan != nil {
			status = int(s.Plan.Status)
		}
		key = fmt.Sprintf("%v|%s|%d|%s|%s|%s|%s", s.Phase, s.Cmd.Path, status, s.PlanErr, s.Err, s.Backup, s.Copied)
	}
	if key == a.mcpSetupRenderKey {
		return
	}
	a.mcpSetupRenderKey = key
	if a.mcpSetupOverlay != (Control.Instance{}) {
		a.mcpSetupOverlay.AsNode().QueueFree()
		a.mcpSetupOverlay = Control.Instance{}
	}
	if key == "" {
		return
	}
	a.mcpSetupOverlay = a.buildMCPSetupModal(s)
	w.rootPanel.AsNode().AddChild(a.mcpSetupOverlay.AsNode())
	a.mcpSetupOverlay.GrabFocus()
}

func (a *App) buildMCPSetupModal(s mcpSetupState) Control.Instance {
	m := newModalShell(mcpSetupModalWidth, a.closeMCPSetup)
	switch s.Phase {
	case mcpSetupList:
		m.title("Connect an AI Assistant")
		if s.CmdErr != "" {
			m.para("Bufflehead couldn't work out how to launch itself for an AI assistant.")
			m.mono(s.CmdErr)
			m.button("Close", true, true, a.closeMCPSetup)
			break
		}
		m.para("Let Claude and other MCP clients list your saved connections, read their schemas and run read-only SQL. " +
			"They start this copy of Bufflehead in the background; the window doesn't need to be open.")
		if s.Cmd.Warning != "" {
			m.warn(s.Cmd.Warning)
		}
		if s.Desktop {
			a.desktopRow(m, s)
		}
		copyLabel := func(kind, label string) string {
			if s.Copied == kind {
				return "Copied"
			}
			return label
		}
		m.row("Claude Code", "Run this in a terminal:", s.Cmd.ClaudeCodeCommand(runtime.GOOS),
			copyLabel("code", "Copy Command"), true, func() { a.mcpSetupCopy("code") })
		entry, _ := json.Marshal(s.Cmd) // one line here; the copy is indented
		m.row("Other MCP clients", "Add this server entry to the client's config:", string(entry),
			copyLabel("json", "Copy JSON"), true, func() { a.mcpSetupCopy("json") })
		m.button("Done", true, true, a.closeMCPSetup)

	case mcpSetupConfirm:
		p := s.Plan
		verb := "Add"
		if p.Status == mcpsetup.Changed {
			verb = "Update"
		}
		m.title(verb + " Bufflehead in Claude Desktop?")
		m.para("This sets [b]mcpServers → bufflehead[/b] in:")
		m.mono(p.Path)
		if p.Status == mcpsetup.Changed {
			m.label("Current entry (runs a different copy of Bufflehead):")
			m.mono(p.Existing)
			m.label("New entry:")
		}
		m.mono(p.Proposed)
		if len(p.Original) > 0 {
			m.para("Every other setting is kept (the file is re-indented). The current file is backed up next to it first.")
		} else {
			m.para("The file doesn't exist yet and will be created.")
		}
		if s.Err != "" {
			m.warn(s.Err)
		}
		m.button("Cancel", false, true, a.mcpSetupBack)
		m.button(verb, true, true, a.mcpSetupApply)

	case mcpSetupDone:
		m.title("Added to Claude Desktop")
		quit := "File → Exit"
		if runtime.GOOS == "darwin" {
			quit = "⌘Q"
		}
		m.para("Quit Claude Desktop completely (" + quit + ") and reopen it. Then ask in a new chat: " +
			"[i]\"What Bufflehead bookmarks do I have?\"[/i]")
		if s.Backup != "" {
			m.label("Your previous config was backed up to:")
			m.mono(s.Backup)
		}
		m.button("Back", false, true, a.mcpSetupBack)
		m.button("Done", true, true, a.closeMCPSetup)
	}
	return m.overlay
}

func (a *App) desktopRow(m *modalShell, s mcpSetupState) {
	if s.PlanErr != "" {
		m.row("Claude Desktop", "Can't read the config file: "+s.PlanErr, "", "Add Automatically", false, nil)
		return
	}
	switch s.Plan.Status {
	case mcpsetup.Current:
		m.row("Claude Desktop", "Set up. Restart Claude Desktop if it doesn't show Bufflehead's tools yet.", "", "Added", false, nil)
	case mcpsetup.Changed:
		m.row("Claude Desktop", "Set up for a different copy of Bufflehead (the app may have moved).", "", "Update…", true, a.mcpSetupReview)
	default:
		m.row("Claude Desktop", "Adds Bufflehead to Claude Desktop's config file for you.", "", "Add Automatically…", true, a.mcpSetupReview)
	}
}

// modalShell is the card layout shared by Bufflehead's modals (see the update
// and re-login modals): dimmer, header, body and an action row. Backdrop
// clicks and Escape call dismiss.
type modalShell struct {
	overlay Control.Instance
	header  Control.Instance
	content VBoxContainer.Instance
	actions HBoxContainer.Instance
}

func newModalShell(width float32, dismiss func()) *modalShell {
	m := &modalShell{}
	overlay := Control.New()
	overlay.AsNode().SetName("MCPSetupModal")
	overlay.SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	overlay.SetMouseFilter(Control.MouseFilterStop)
	overlay.SetFocusMode(Control.FocusAll)
	m.overlay = overlay

	dimmer := ColorRect.New()
	dimmer.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	dimmer.AsControl().SetMouseFilter(Control.MouseFilterIgnore)
	dimmer.SetColor(Color.RGBA{R: 0, G: 0, B: 0, A: 0.6})
	overlay.AsNode().AddChild(dimmer.AsNode())

	center := CenterContainer.New()
	center.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	center.AsControl().SetMouseFilter(Control.MouseFilterIgnore)
	overlay.AsNode().AddChild(center.AsNode())

	modal := PanelContainer.New()
	modalBg := makeStyleBox(colorBgPanel, 8, 1, colorBorder)
	modalBg.SetShadowColor(Color.RGBA{R: 0, G: 0, B: 0, A: 0.5})
	modalBg.SetShadowSize(int(scaled(24)))
	modal.AsControl().AddThemeStyleboxOverride("panel", modalBg.AsStyleBox())
	modal.AsControl().SetCustomMinimumSize(Vector2.New(scaled(width), 0))
	modal.AsControl().SetMouseFilter(Control.MouseFilterStop)
	center.AsNode().AddChild(modal.AsNode())

	card := VBoxContainer.New()
	card.AsControl().AddThemeConstantOverride("separation", 0)
	modal.AsNode().AddChild(card.AsNode())

	headerBar := PanelContainer.New()
	headerSB := makeStyleBox(colorBg, 0, 0, colorBg)
	headerSB.SetCornerRadiusTopLeft(8)
	headerSB.SetCornerRadiusTopRight(8)
	headerSB.SetBorderWidthBottom(1)
	headerSB.SetBorderColor(colorBorder)
	headerBar.AsControl().AddThemeStyleboxOverride("panel", headerSB.AsStyleBox())
	card.AsNode().AddChild(headerBar.AsNode())
	headerMargin := paddedMargin(20, 14, 20, 14)
	headerBar.AsNode().AddChild(headerMargin.AsNode())
	m.header = headerMargin.AsControl()

	// The body scrolls once it is taller than the window allows. A
	// ScrollContainer doesn't size to its content, so fitBody sets its
	// height: the content's, capped at what's left after the header and
	// actions.
	scroll := ScrollContainer.New()
	scroll.SetHorizontalScrollMode(ScrollContainer.ScrollModeDisabled)
	card.AsNode().AddChild(scroll.AsNode())
	bodyMargin := paddedMargin(20, 18, 20, 18)
	bodyMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	scroll.AsNode().AddChild(bodyMargin.AsNode())
	m.content = VBoxContainer.New()
	m.content.AsControl().AddThemeConstantOverride("separation", 12)
	bodyMargin.AsNode().AddChild(m.content.AsNode())
	fitBody := func() {
		want := bodyMargin.AsControl().GetCombinedMinimumSize().Y
		room := overlay.Size().Y - scaled(170)
		if room < scaled(120) {
			room = scaled(120)
		}
		if want > room {
			want = room
		}
		scroll.AsControl().SetCustomMinimumSize(Vector2.New(0, want))
	}
	bodyMargin.AsControl().OnMinimumSizeChanged(fitBody)
	overlay.OnResized(fitBody)

	actionsBar := PanelContainer.New()
	actionsSB := makeStyleBox(colorBg, 0, 0, colorBg)
	actionsSB.SetCornerRadiusBottomLeft(8)
	actionsSB.SetCornerRadiusBottomRight(8)
	actionsSB.SetBorderWidthTop(1)
	actionsSB.SetBorderColor(colorBorder)
	actionsBar.AsControl().AddThemeStyleboxOverride("panel", actionsSB.AsStyleBox())
	card.AsNode().AddChild(actionsBar.AsNode())
	actionsMargin := paddedMargin(20, 14, 20, 14)
	actionsBar.AsNode().AddChild(actionsMargin.AsNode())
	m.actions = HBoxContainer.New()
	m.actions.AsControl().AddThemeConstantOverride("separation", 10)
	m.actions.AsBoxContainer().SetAlignment(BoxContainer.AlignmentEnd)
	actionsMargin.AsNode().AddChild(m.actions.AsNode())

	overlay.OnGuiInput(func(event InputEvent.Instance) {
		if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
			if mb.AsInputEvent().IsPressed() && mb.ButtonIndex() == Input.MouseButtonLeft {
				dismiss()
			}
			return
		}
		if kb, ok := Object.As[InputEventKey.Instance](event); ok {
			if kb.AsInputEvent().IsPressed() && kb.Keycode() == Input.KeyEscape {
				dismiss()
			}
		}
	})
	return m
}

func (m *modalShell) title(text string) {
	lbl := Label.New()
	lbl.SetText(text)
	lbl.AsControl().AddThemeFontOverride("font", boldFont())
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(15))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	m.header.AsNode().AddChild(lbl.AsNode())
}

func richText(bbcode string, size int, color Color.RGBA) RichTextLabel.Instance {
	para := RichTextLabel.New()
	para.SetBbcodeEnabled(true)
	para.SetFitContent(true)
	para.SetScrollActive(false)
	para.SetAutowrapMode(TextServer.AutowrapWord)
	para.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	para.AsControl().AddThemeColorOverride("default_color", color)
	para.AsControl().AddThemeFontSizeOverride("normal_font_size", fontSize(size))
	para.AsControl().AddThemeFontSizeOverride("bold_font_size", fontSize(size))
	para.AsControl().AddThemeFontSizeOverride("italics_font_size", fontSize(size))
	para.SetText(bbcode)
	return para
}

func (m *modalShell) para(bbcode string) {
	m.content.AsNode().AddChild(richText(bbcode, 12, colorTextMuted).AsNode())
}

func (m *modalShell) label(text string) {
	m.content.AsNode().AddChild(richText(escapeBBCode(text), 11, colorTextDim).AsNode())
}

func (m *modalShell) warn(text string) {
	m.content.AsNode().AddChild(richText(escapeBBCode(text), 11, Color.RGBA{R: 1, G: 0.75, B: 0.4, A: 1}).AsNode())
}

func monoBox(text string) PanelContainer.Instance {
	box := PanelContainer.New()
	box.AsControl().AddThemeStyleboxOverride("panel", makeStyleBox(colorBgDarker, 4, 1, colorBorder).AsStyleBox())
	box.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	mg := paddedMargin(10, 8, 10, 8)
	box.AsNode().AddChild(mg.AsNode())
	lbl := Label.New()
	lbl.SetText(text)
	lbl.SetAutowrapMode(TextServer.AutowrapArbitrary)
	lbl.AsControl().AddThemeFontOverride("font", monoFont())
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	lbl.AsControl().AddThemeColorOverride("font_color", colorText)
	lbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	mg.AsNode().AddChild(lbl.AsNode())
	return box
}

func (m *modalShell) mono(text string) {
	m.content.AsNode().AddChild(monoBox(text).AsNode())
}

// row is one client: name and hint on the left, a button on the right, and an
// optional mono block (the command to copy) underneath.
func (m *modalShell) row(name, hint, code, btn string, enabled bool, press func()) {
	box := PanelContainer.New()
	box.AsControl().AddThemeStyleboxOverride("panel", makeStyleBox(colorBg, 6, 1, colorBorderDim).AsStyleBox())
	m.content.AsNode().AddChild(box.AsNode())
	mg := paddedMargin(12, 10, 12, 10)
	box.AsNode().AddChild(mg.AsNode())
	col := VBoxContainer.New()
	col.AsControl().AddThemeConstantOverride("separation", 8)
	mg.AsNode().AddChild(col.AsNode())

	top := HBoxContainer.New()
	top.AsControl().AddThemeConstantOverride("separation", 12)
	col.AsNode().AddChild(top.AsNode())
	text := VBoxContainer.New()
	text.AsControl().AddThemeConstantOverride("separation", 2)
	text.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	top.AsNode().AddChild(text.AsNode())
	title := Label.New()
	title.SetText(name)
	title.AsControl().AddThemeFontOverride("font", boldFont())
	title.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	title.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	text.AsNode().AddChild(title.AsNode())
	text.AsNode().AddChild(richText(escapeBBCode(hint), 11, colorTextDim).AsNode())

	b := Button.New()
	b.AsNode().SetName(name + " Button")
	b.SetText(btn)
	applySecondaryButtonTheme(b.AsControl())
	b.AsControl().SetCustomMinimumSize(Vector2.New(scaled(150), scaled(30)))
	b.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	b.AsBaseButton().SetDisabled(!enabled)
	if press != nil {
		b.AsBaseButton().OnPressed(press)
	}
	top.AsNode().AddChild(b.AsNode())

	if code != "" {
		col.AsNode().AddChild(monoBox(code).AsNode())
	}
}

func (m *modalShell) button(label string, primary, enabled bool, press func()) {
	b := Button.New()
	b.SetText(label)
	if primary {
		applyButtonTheme(b.AsControl())
	} else {
		applySecondaryButtonTheme(b.AsControl())
	}
	b.AsControl().SetCustomMinimumSize(Vector2.New(scaled(110), scaled(30)))
	b.AsBaseButton().SetDisabled(!enabled)
	b.AsBaseButton().OnPressed(press)
	m.actions.AsNode().AddChild(b.AsNode())
}
