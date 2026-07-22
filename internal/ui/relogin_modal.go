package ui

import (
	"strings"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CenterContainer"
	"graphics.gd/classdb/ColorRect"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventKey"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/RichTextLabel"
	"graphics.gd/classdb/TextServer"
	"graphics.gd/classdb/TextureRect"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Color"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
)

// reLoginModalWidth is the modal card width in logical points (max-w-md, scaled
// down for this IDE's compact type scale).
const reLoginModalWidth = 380

// svgReLoginWarning is the header glyph: a red alert-triangle (Lucide) inside a
// soft red-tinted circle, matching the Stitch "Session Expired" design.
const svgReLoginWarning = `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32">` +
	`<circle cx="16" cy="16" r="16" fill="#ef4444" fill-opacity="0.16"/>` +
	`<g transform="translate(4,4)" fill="none" stroke="#ef4444" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` +
	`<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/>` +
	`<path d="M12 9v4"/><path d="M12 17h.01"/></g></svg>`

// connDBName resolves the human-facing database name for a tab's connection,
// preferring the gateway's configured DB name and falling back to the
// connection label.
func (w *AppWindow) connDBName(ts *tabState) string {
	if ts == nil || ts.connIdx < 0 || ts.connIdx >= len(w.connections) {
		return ""
	}
	conn := w.connections[ts.connIdx]
	if conn.Gateway != nil && conn.Gateway.Config.DBName != "" {
		return conn.Gateway.Config.DBName
	}
	return conn.Name
}

// reLoginDetail renders a one-line technical detail for the modal's error box
// from a connection error.
func reLoginDetail(err error) string {
	if err == nil {
		return ""
	}
	return reLoginDetailString(err.Error())
}

// reLoginDetailString collapses a message to a single trimmed line and prefixes
// it with "Error:" for the mono detail box (unless it already reads as one).
func reLoginDetailString(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "Error: authentication token expired or revoked"
	}
	if !strings.HasPrefix(strings.ToLower(msg), "error") {
		msg = "Error: " + msg
	}
	return msg
}

// promptReLogin shows the in-scene "AWS SSO Session Expired" modal guiding the
// user to re-authenticate. It is a state-driven overlay (dimmer + centered
// card) rendered on top of the whole window, replacing the old native dialog so
// it matches the app theme on every platform. Confirming opens the connection/
// SSO screen. Guarded so only one instance shows at a time.
func (w *AppWindow) promptReLogin(dbName, detail string) {
	if w.reLoginDialogOpen || w.rootPanel == (PanelContainer.Instance{}) {
		return
	}
	w.reLoginDialogOpen = true

	// ── Overlay: full-window dimmer that blocks input to the app beneath ──
	overlay := Control.New()
	overlay.SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	overlay.SetMouseFilter(Control.MouseFilterStop)
	overlay.SetFocusMode(Control.FocusAll)

	dimmer := ColorRect.New()
	dimmer.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	dimmer.AsControl().SetMouseFilter(Control.MouseFilterStop)
	dimmer.SetColor(Color.RGBA{R: 0, G: 0, B: 0, A: 0.6})
	overlay.AsNode().AddChild(dimmer.AsNode())

	center := CenterContainer.New()
	center.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	center.AsControl().SetMouseFilter(Control.MouseFilterIgnore)
	overlay.AsNode().AddChild(center.AsNode())

	// ── Modal card ──
	modal := PanelContainer.New()
	modalBg := makeStyleBox(colorBgPanel, 8, 1, colorBorder)
	modalBg.SetShadowColor(Color.RGBA{R: 0, G: 0, B: 0, A: 0.5})
	modalBg.SetShadowSize(int(scaled(24)))
	modal.AsControl().AddThemeStyleboxOverride("panel", modalBg.AsStyleBox())
	modal.AsControl().SetCustomMinimumSize(Vector2.New(scaled(reLoginModalWidth), 0))
	modal.AsControl().SetMouseFilter(Control.MouseFilterStop) // clicks on the card don't dismiss
	center.AsNode().AddChild(modal.AsNode())

	card := VBoxContainer.New()
	card.AsControl().AddThemeConstantOverride("separation", 0)
	card.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	modal.AsNode().AddChild(card.AsNode())

	// ── Header: warning glyph + title, over a subtle bar with a bottom rule ──
	headerBar := PanelContainer.New()
	headerSB := makeStyleBox(colorBg, 0, 0, colorBg)
	headerSB.SetCornerRadiusTopLeft(8)
	headerSB.SetCornerRadiusTopRight(8)
	headerSB.SetBorderWidthBottom(1)
	headerSB.SetBorderColor(colorBorder)
	headerBar.AsControl().AddThemeStyleboxOverride("panel", headerSB.AsStyleBox())
	headerBar.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	card.AsNode().AddChild(headerBar.AsNode())

	headerMargin := paddedMargin(20, 14, 20, 14)
	headerBar.AsNode().AddChild(headerMargin.AsNode())

	headerRow := HBoxContainer.New()
	headerRow.AsControl().AddThemeConstantOverride("separation", 12)
	headerRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	headerMargin.AsNode().AddChild(headerRow.AsNode())

	icon := TextureRect.New()
	icon.SetTexture(loadSVGTexture(svgReLoginWarning))
	icon.SetStretchMode(TextureRect.StretchKeepCentered)
	icon.AsControl().SetCustomMinimumSize(Vector2.New(scaled(32), scaled(32)))
	icon.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	headerRow.AsNode().AddChild(icon.AsNode())

	title := Label.New()
	title.SetText("AWS SSO Session Expired")
	title.AsControl().AddThemeFontOverride("font", boldFont())
	title.AsControl().AddThemeFontSizeOverride("font_size", fontSize(15))
	title.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	title.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	title.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	headerRow.AsNode().AddChild(title.AsNode())

	// ── Body: explanatory paragraph + technical error detail box ──
	bodyMargin := paddedMargin(20, 18, 20, 18)
	card.AsNode().AddChild(bodyMargin.AsNode())

	body := VBoxContainer.New()
	body.AsControl().AddThemeConstantOverride("separation", 14)
	body.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	bodyMargin.AsNode().AddChild(body.AsNode())

	para := RichTextLabel.New()
	para.SetBbcodeEnabled(true)
	para.SetFitContent(true)
	para.SetScrollActive(false)
	para.SetAutowrapMode(TextServer.AutowrapWord)
	para.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	para.AsControl().AddThemeColorOverride("default_color", colorTextMuted)
	para.AsControl().AddThemeFontSizeOverride("normal_font_size", fontSize(12))
	para.AsControl().AddThemeFontSizeOverride("bold_font_size", fontSize(12))
	para.SetText(reLoginBody(dbName))
	body.AsNode().AddChild(para.AsNode())

	if detail != "" {
		errBox := PanelContainer.New()
		errSB := makeStyleBox(colorBgDarker, 4, 1, colorBorder)
		errBox.AsControl().AddThemeStyleboxOverride("panel", errSB.AsStyleBox())
		errBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		body.AsNode().AddChild(errBox.AsNode())

		errMargin := paddedMargin(10, 8, 10, 8)
		errBox.AsNode().AddChild(errMargin.AsNode())

		errLbl := Label.New()
		errLbl.SetText(detail)
		errLbl.SetAutowrapMode(TextServer.AutowrapWordSmart)
		errLbl.AsControl().AddThemeFontOverride("font", monoFont())
		errLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
		errLbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
		errLbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		errMargin.AsNode().AddChild(errLbl.AsNode())
	}

	// ── Actions: [ Not now ] [ Log in again ] over a subtle bar with a top rule ──
	actionsBar := PanelContainer.New()
	actionsSB := makeStyleBox(colorBg, 0, 0, colorBg)
	actionsSB.SetCornerRadiusBottomLeft(8)
	actionsSB.SetCornerRadiusBottomRight(8)
	actionsSB.SetBorderWidthTop(1)
	actionsSB.SetBorderColor(colorBorder)
	actionsBar.AsControl().AddThemeStyleboxOverride("panel", actionsSB.AsStyleBox())
	actionsBar.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	card.AsNode().AddChild(actionsBar.AsNode())

	actionsMargin := paddedMargin(20, 14, 20, 14)
	actionsBar.AsNode().AddChild(actionsMargin.AsNode())

	actions := HBoxContainer.New()
	actions.AsControl().AddThemeConstantOverride("separation", 10)
	actions.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	actionsMargin.AsNode().AddChild(actions.AsNode())

	notNow := Button.New()
	notNow.SetText("Not now")
	applySecondaryButtonTheme(notNow.AsControl())
	notNow.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(30)))
	notNow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	notNow.AsBaseButton().OnPressed(func() { w.dismissReLogin() })
	actions.AsNode().AddChild(notNow.AsNode())

	login := Button.New()
	login.SetText("Log in again")
	applyButtonTheme(login.AsControl())
	login.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(30)))
	login.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	login.AsBaseButton().OnPressed(func() {
		w.dismissReLogin()
		if w.onReLogin != nil {
			w.onReLogin()
		}
	})
	actions.AsNode().AddChild(login.AsNode())

	// Dismiss (= "Not now") on backdrop click or Escape.
	overlay.OnGuiInput(func(event InputEvent.Instance) {
		if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
			if mb.AsInputEvent().IsPressed() && mb.ButtonIndex() == Input.MouseButtonLeft {
				w.dismissReLogin()
			}
			return
		}
		if kb, ok := Object.As[InputEventKey.Instance](event); ok {
			if kb.AsInputEvent().IsPressed() && kb.Keycode() == Input.KeyEscape {
				w.dismissReLogin()
			}
		}
	})

	w.reLoginOverlay = overlay
	w.rootPanel.AsNode().AddChild(overlay.AsNode())
	overlay.GrabFocus()
}

// dismissReLogin tears down the re-login overlay and clears the guard so it can
// be shown again on the next auth error.
func (w *AppWindow) dismissReLogin() {
	if !w.reLoginDialogOpen {
		return
	}
	w.reLoginDialogOpen = false
	if w.reLoginOverlay != (Control.Instance{}) {
		w.reLoginOverlay.AsNode().QueueFree()
		w.reLoginOverlay = Control.Instance{}
	}
}

// reLoginBody builds the BBCode paragraph, highlighting the app name and the
// affected database.
func reLoginBody(dbName string) string {
	db := strings.TrimSpace(dbName)
	if db == "" {
		return "Your AWS SSO session has expired. [b]Bufflehead[/b] has lost access to this " +
			"database and can no longer refresh connection tokens."
	}
	return "Your AWS SSO session has expired. [b]Bufflehead[/b] has lost access to the " +
		"[color=#c3c0ff][b]" + escapeBBCode(db) + "[/b][/color] database and can no longer " +
		"refresh connection tokens."
}

// escapeBBCode neutralizes any BBCode-significant brackets in dynamic text so a
// database name can't inject tags into the RichTextLabel.
func escapeBBCode(s string) string {
	return strings.ReplaceAll(s, "[", "[lb]")
}

// paddedMargin returns a MarginContainer with the given per-side padding in
// logical points (left, top, right, bottom).
func paddedMargin(left, top, right, bottom int) MarginContainer.Instance {
	m := MarginContainer.New()
	m.AsControl().AddThemeConstantOverride("margin_left", int(scaled(float32(left))))
	m.AsControl().AddThemeConstantOverride("margin_top", int(scaled(float32(top))))
	m.AsControl().AddThemeConstantOverride("margin_right", int(scaled(float32(right))))
	m.AsControl().AddThemeConstantOverride("margin_bottom", int(scaled(float32(bottom))))
	return m
}
