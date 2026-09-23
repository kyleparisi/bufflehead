package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"bufflehead/internal/updater"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CenterContainer"
	"graphics.gd/classdb/ColorRect"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventKey"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/OS"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/RichTextLabel"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/TextServer"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Color"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
)

const updateModalWidth = 440

// Update flow: events (menu item, button presses, finished goroutines) mutate
// a.update, the updater.State; renderUpdate projects that state onto the main
// window. Network work runs off the main thread and reports back through
// a.updateEvents, which Process drains before rendering.

// autoCheckForUpdates runs the startup check unless the app is headless (the
// integration harness), opted out, or unversioned.
func (a *App) autoCheckForUpdates() {
	if updater.FinishInstall() {
		log.Printf("[update] now running %s; removed the previous version", a.Version)
	}
	if DisplayServer.GetName() == "headless" || os.Getenv("BUFFLEHEAD_NO_UPDATE_CHECK") != "" {
		return
	}
	a.checkForUpdates(false)
}

func (a *App) checkForUpdates(manual bool) {
	if !a.update.StartCheck(manual) {
		return
	}
	current := a.Version
	go func() {
		var u *updater.Update
		var err error
		if current == "" {
			err = fmt.Errorf("this build has no version number")
		} else {
			u, err = updater.Client{}.Check(context.Background(), current, runtime.GOOS, runtime.GOARCH)
		}
		if err != nil {
			log.Printf("[update] check from %s: %v", current, err)
		}
		a.updateEvents <- func(s *updater.State) { s.CheckDone(u, err) }
	}()
}

func (a *App) downloadUpdate() {
	if !a.update.StartDownload() {
		return
	}
	asset := a.update.Update.Asset
	go func() {
		path, blocker := "", ""
		dir, err := updater.StageDir()
		if err == nil {
			path, err = updater.Client{}.StageNamed(context.Background(), asset, dir)
		}
		if err != nil {
			log.Printf("[update] download %s: %v", asset.Name, err)
		} else {
			log.Printf("[update] staged %s", path)
			if blocker = updater.InstallBlocker(context.Background()); blocker != "" && updater.InstallSupported() {
				log.Printf("[update] can't install in place: %s", blocker)
			}
		}
		a.updateEvents <- func(s *updater.State) { s.DownloadDone(path, blocker, err) }
	}()
}

// installUpdate verifies and copies the new app off the main thread, then
// launches the swap helper. Process quits the app once the state reaches
// Restarting.
func (a *App) installUpdate() {
	if !a.update.StartInstall() {
		return
	}
	dmg, tag := a.update.Staged, a.update.Update.Release.Tag
	go func() {
		p, err := updater.PrepareInstall(context.Background(), dmg, tag)
		if err == nil {
			err = p.Launch(os.Getpid())
		}
		if err != nil {
			log.Printf("[update] install %s: %v", tag, err)
		} else {
			log.Printf("[update] installing %s over %s; quitting", tag, p.App)
		}
		a.updateEvents <- func(s *updater.State) { s.InstallDone(err) }
	}()
}

// quitForUpdate exits so the swap helper can replace the bundle. It is the
// same teardown as closing the main window.
func (a *App) quitForUpdate() {
	if a.quittingForUpdate {
		return
	}
	a.quittingForUpdate = true
	a.stopGatewayTunnels()
	a.stopAllWorkers()
	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		tree.Quit()
	}
}

// drainUpdateEvents applies finished background work on the main thread.
func (a *App) drainUpdateEvents() {
	for {
		select {
		case ev := <-a.updateEvents:
			ev(&a.update)
		default:
			return
		}
	}
}

// renderUpdate rebuilds the modal when the visible state changes. It reads
// a.update and never writes it; handlers call it after mutating.
func (a *App) renderUpdate() {
	w := a.mainWin
	s := a.update
	key := ""
	if s.Open && w != nil && w.rootPanel != (PanelContainer.Instance{}) {
		key = fmt.Sprintf("%v|%s|%s", s.Phase, s.Err, s.Staged)
	}
	if key == a.updateRenderKey {
		return
	}
	a.updateRenderKey = key
	if a.updateOverlay != (Control.Instance{}) {
		a.updateOverlay.AsNode().QueueFree()
		a.updateOverlay = Control.Instance{}
	}
	if key == "" {
		return
	}
	a.updateOverlay = a.buildUpdateModal(s)
	w.rootPanel.AsNode().AddChild(a.updateOverlay.AsNode())
	a.updateOverlay.GrabFocus()
}

func (a *App) dismissUpdate() {
	a.update.Dismiss()
	a.renderUpdate()
}

type updateAction struct {
	label   string
	primary bool
	enabled bool
	press   func()
}

func (a *App) buildUpdateModal(s updater.State) Control.Instance {
	title, body, detail := "", "", ""
	notes := ""
	later := updateAction{label: "Later", enabled: true, press: a.dismissUpdate}
	var actions []updateAction
	switch s.Phase {
	case updater.Checking:
		title, body = "Checking for Updates…", "Looking up the latest Bufflehead release on GitHub."
		actions = []updateAction{{label: "Close", enabled: true, press: a.dismissUpdate}}
	case updater.UpToDate:
		title = "You're Up to Date"
		body = "[b]Bufflehead " + escapeBBCode(a.Version) + "[/b] is the latest version."
		actions = []updateAction{{label: "OK", primary: true, enabled: true, press: a.dismissUpdate}}
	case updater.Available, updater.Downloading:
		u := s.Update
		title = "Update Available"
		body = "[color=#c3c0ff][b]Bufflehead " + escapeBBCode(strings.TrimPrefix(u.Release.Tag, "v")) +
			"[/b][/color] is available — you have " + escapeBBCode(a.Version) + "."
		notes = u.Release.Notes
		dl := updateAction{label: fmt.Sprintf("Download (%s)", humanBytes(u.Asset.Size)), primary: true, enabled: true, press: a.downloadUpdate}
		if s.Phase == updater.Downloading {
			dl.label, dl.enabled = "Downloading…", false
		}
		actions = []updateAction{later, a.releasePageAction(u), dl}
	case updater.Ready:
		title = "Update Downloaded"
		body = "Verified [b]" + escapeBBCode(s.Update.Asset.Name) + "[/b] (SHA-256 checked). "
		detail = s.Staged
		actions = []updateAction{later, {label: "Show in Folder", enabled: true, press: func() { OS.ShellShowInFileManager(s.Staged) }}}
		switch {
		case s.Blocker == "":
			body += installHint()
			actions = append(actions, updateAction{label: "Install and Restart", primary: true, enabled: true, press: a.installUpdate})
		case updater.InstallSupported():
			body += escapeBBCode(s.Blocker)
			actions = append(actions, a.openInstallerAction(s.Staged, true))
		default:
			body += installHint()
			if runtime.GOOS != "linux" {
				actions = append(actions, a.openInstallerAction(s.Staged, true))
			}
		}
	case updater.Installing, updater.Restarting:
		title = "Installing Update…"
		body = "Checking the signature of [b]Bufflehead " + escapeBBCode(strings.TrimPrefix(s.Update.Release.Tag, "v")) +
			"[/b] and copying it into place. Bufflehead will then quit and reopen."
		label := "Installing…"
		if s.Phase == updater.Restarting {
			title, label = "Restarting…", "Restarting…"
		}
		actions = []updateAction{{label: label, primary: true}}
	case updater.Failed:
		title, body = "Update Failed", "Bufflehead couldn't complete the update."
		if s.Staged != "" {
			body = "Bufflehead couldn't install the update. The verified download is ready; open the installer to finish by hand."
		}
		detail = reLoginDetailString(s.Err)
		actions = []updateAction{{label: "Close", enabled: true, press: a.dismissUpdate}}
		switch {
		case s.Staged != "":
			// The verified download is still on disk: hand it to the user.
			if runtime.GOOS != "linux" {
				actions = append(actions, a.openInstallerAction(s.Staged, true))
			} else {
				actions = append(actions, updateAction{label: "Show in Folder", primary: true, enabled: true, press: func() { OS.ShellShowInFileManager(s.Staged) }})
			}
		case s.Update != nil:
			actions = append(actions, a.releasePageAction(s.Update),
				updateAction{label: "Retry Download", primary: true, enabled: true, press: a.downloadUpdate})
		}
	}
	return a.updateModalNodes(title, body, notes, detail, actions)
}

// openInstallerAction hands the verified download to the OS (mounts the DMG,
// runs Setup.exe) for the user to install by hand.
func (a *App) openInstallerAction(path string, primary bool) updateAction {
	return updateAction{label: "Open Installer", primary: primary, enabled: true, press: func() {
		if err := OS.ShellOpen(path); err != nil {
			log.Printf("[update] open %s: %v", path, err)
		}
		a.dismissUpdate()
	}}
}

func (a *App) releasePageAction(u *updater.Update) updateAction {
	return updateAction{label: "Release Notes", enabled: true, press: func() { OS.ShellOpen(u.Release.URL) }}
}

func installHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Bufflehead will quit, replace itself and reopen; open tabs and connections will close."
	case "windows":
		return "The installer will close Bufflehead and replace the installed copy."
	default:
		return "Quit Bufflehead and replace your existing AppImage with this one."
	}
}

// markdownToBBCode renders the subset of Markdown that release notes use:
// headings, bullets, **bold** and `code`. Everything else passes through as text.
func markdownToBBCode(md string) string {
	var out []string
	for _, line := range strings.Split(escapeBBCode(md), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "#"):
			line = "[b][color=#e4e4e7]" + strings.TrimSpace(strings.TrimLeft(t, "#")) + "[/color][/b]"
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
			line = "  • " + t[2:]
		}
		line = mdPairs(line, "**", "[b]", "[/b]")
		line = mdPairs(line, "`", "[code]", "[/code]")
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// mdPairs replaces matched delimiter pairs; an unmatched trailing one is kept.
func mdPairs(s, delim, open, close string) string {
	parts := strings.Split(s, delim)
	if len(parts) < 3 {
		return s
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			switch {
			case i == len(parts)-1 && i%2 == 1:
				b.WriteString(delim)
			case i%2 == 1:
				b.WriteString(open)
			default:
				b.WriteString(close)
			}
		}
		b.WriteString(p)
	}
	return b.String()
}

func humanBytes(n int64) string {
	return fmt.Sprintf("%.0f MB", float64(n)/1e6)
}

// updateModalNodes lays out the overlay: dimmer, card, header, body, optional
// scrollable notes and mono detail box, and an action row. Same visual language
// as the re-login modal.
func (a *App) updateModalNodes(title, body, notes, detail string, actions []updateAction) Control.Instance {
	overlay := Control.New()
	overlay.SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	overlay.SetMouseFilter(Control.MouseFilterStop)
	overlay.SetFocusMode(Control.FocusAll)

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
	modal.AsControl().SetCustomMinimumSize(Vector2.New(scaled(updateModalWidth), 0))
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
	titleLbl := Label.New()
	titleLbl.SetText(title)
	titleLbl.AsControl().AddThemeFontOverride("font", boldFont())
	titleLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(15))
	titleLbl.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	headerMargin.AsNode().AddChild(titleLbl.AsNode())

	bodyMargin := paddedMargin(20, 18, 20, 18)
	card.AsNode().AddChild(bodyMargin.AsNode())
	content := VBoxContainer.New()
	content.AsControl().AddThemeConstantOverride("separation", 14)
	bodyMargin.AsNode().AddChild(content.AsNode())

	para := RichTextLabel.New()
	para.SetBbcodeEnabled(true)
	para.SetFitContent(true)
	para.SetScrollActive(false)
	para.SetAutowrapMode(TextServer.AutowrapWord)
	para.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	para.AsControl().AddThemeColorOverride("default_color", colorTextMuted)
	para.AsControl().AddThemeFontSizeOverride("normal_font_size", fontSize(12))
	para.AsControl().AddThemeFontSizeOverride("bold_font_size", fontSize(12))
	para.SetText(body)
	content.AsNode().AddChild(para.AsNode())

	if notes = strings.TrimSpace(notes); notes != "" {
		box := PanelContainer.New()
		box.AsControl().AddThemeStyleboxOverride("panel", makeStyleBox(colorBgDarker, 4, 1, colorBorder).AsStyleBox())
		content.AsNode().AddChild(box.AsNode())
		scroll := ScrollContainer.New()
		scroll.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(160)))
		scroll.SetHorizontalScrollMode(ScrollContainer.ScrollModeDisabled)
		box.AsNode().AddChild(scroll.AsNode())
		m := paddedMargin(10, 8, 10, 8)
		m.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		scroll.AsNode().AddChild(m.AsNode())
		lbl := RichTextLabel.New()
		lbl.SetBbcodeEnabled(true)
		lbl.SetFitContent(true)
		lbl.SetScrollActive(false)
		lbl.SetAutowrapMode(TextServer.AutowrapWordSmart)
		lbl.AsControl().AddThemeFontSizeOverride("normal_font_size", fontSize(11))
		lbl.AsControl().AddThemeFontSizeOverride("bold_font_size", fontSize(11))
		lbl.AsControl().AddThemeFontOverride("mono_font", monoFont())
		lbl.AsControl().AddThemeColorOverride("default_color", colorTextDim)
		lbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		lbl.SetText(markdownToBBCode(notes))
		m.AsNode().AddChild(lbl.AsNode())
	}

	if detail != "" {
		box := PanelContainer.New()
		box.AsControl().AddThemeStyleboxOverride("panel", makeStyleBox(colorBgDarker, 4, 1, colorBorder).AsStyleBox())
		content.AsNode().AddChild(box.AsNode())
		m := paddedMargin(10, 8, 10, 8)
		box.AsNode().AddChild(m.AsNode())
		lbl := Label.New()
		lbl.SetText(detail)
		lbl.SetAutowrapMode(TextServer.AutowrapArbitrary)
		lbl.AsControl().AddThemeFontOverride("font", monoFont())
		lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
		lbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
		lbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		m.AsNode().AddChild(lbl.AsNode())
	}

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
	row := HBoxContainer.New()
	row.AsControl().AddThemeConstantOverride("separation", 10)
	actionsMargin.AsNode().AddChild(row.AsNode())
	for _, act := range actions {
		b := Button.New()
		b.SetText(act.label)
		if act.primary {
			applyButtonTheme(b.AsControl())
		} else {
			applySecondaryButtonTheme(b.AsControl())
		}
		b.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(30)))
		b.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		b.AsBaseButton().SetDisabled(!act.enabled)
		press := act.press
		b.AsBaseButton().OnPressed(func() { press(); a.renderUpdate() })
		row.AsNode().AddChild(b.AsNode())
	}

	// Backdrop click or Escape = dismiss; background work keeps running.
	overlay.OnGuiInput(func(event InputEvent.Instance) {
		if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
			if mb.AsInputEvent().IsPressed() && mb.ButtonIndex() == Input.MouseButtonLeft {
				a.dismissUpdate()
			}
			return
		}
		if kb, ok := Object.As[InputEventKey.Instance](event); ok {
			if kb.AsInputEvent().IsPressed() && kb.Keycode() == Input.KeyEscape {
				a.dismissUpdate()
			}
		}
	})
	return overlay
}
