package ui

import (
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Image"
	"graphics.gd/classdb/ImageTexture"
	"graphics.gd/classdb/StyleBoxEmpty"
	"graphics.gd/classdb/StyleBoxFlat"
	"graphics.gd/classdb/SystemFont"
	"graphics.gd/classdb/Texture2D"
	"graphics.gd/variant/Color"
)

// uiScale is the multiplier applied to all font sizes and layout dimensions.
// It is computed dynamically from the screen's physical pixel height so
// that the UI remains readable on high-resolution displays.
//
// Using discrete scale factors inspired by the Godot 3D resolution-scaling
// table, the physical pixel height maps to:
//
//	≤1080  (Full HD)     → 1.0
//	≤1440  (QHD)         → 1.33
//	≤2160  (Retina / 4K) → 1.5
//	≤2880  (5K)          → 2.0
//	>2880  (8K+)         → 2.0
var uiScale float64 = 1.0

// initScale computes uiScale from the primary screen's physical pixel
// height. It should be called once during startup, after the Godot
// DisplayServer is available.
func initScale() {
	screenSize := DisplayServer.ScreenGetSize()
	physH := float64(screenSize.Y)
	if physH <= 0 {
		return // fallback: keep 1.0
	}

	var scale float64
	switch {
	case physH <= 1080:
		scale = 1.0
	case physH <= 1440:
		scale = 1.33
	case physH <= 2160:
		scale = 2.0
	default:
		scale = 2.0
	}

	uiScale = scale
}

// fontSize returns a scaled font size in logical points.
func fontSize(base int) int {
	return int(float64(base) * uiScale)
}

// scaled returns a scaled layout dimension in logical points.
func scaled(base float32) float32 {
	return base * float32(uiScale)
}

// Dark theme palette — cool-neutral with blue undertone
var (
	colorBg         = Color.RGBA{R: 0.102, G: 0.106, B: 0.118, A: 1} // #1a1b1e — window body
	colorBgSidebar  = Color.RGBA{R: 0.125, G: 0.129, B: 0.141, A: 1} // #202124 — sidebars, panels
	colorBgDarker   = Color.RGBA{R: 0.125, G: 0.129, B: 0.141, A: 1} // #202124 — connection rail
	colorBgPanel    = Color.RGBA{R: 0.149, G: 0.157, B: 0.173, A: 1} // #26282c — inputs, tabs (bgElev)
	colorBgInput    = Color.RGBA{R: 0.149, G: 0.157, B: 0.173, A: 1} // #26282c — input fields
	colorBgHeader   = Color.RGBA{R: 0.125, G: 0.129, B: 0.141, A: 1} // #202124 — table headers
	colorBgRaised   = Color.RGBA{R: 0.180, G: 0.188, B: 0.212, A: 1} // #2e3036 — hover/selected
	colorRowOdd     = Color.RGBA{R: 0.102, G: 0.106, B: 0.118, A: 1} // #1a1b1e
	colorRowEven    = Color.RGBA{R: 0.110, G: 0.114, B: 0.129, A: 1} // #1c1d21
	colorBorder     = Color.RGBA{R: 0.188, G: 0.196, B: 0.212, A: 1} // #303236 — dividers (line)
	colorBorderDim  = Color.RGBA{R: 0.188, G: 0.196, B: 0.212, A: 1} // #303236
	colorBorderStrong = Color.RGBA{R: 0.227, G: 0.235, B: 0.259, A: 1} // #3a3c42 — input borders
	colorText       = Color.RGBA{R: 0.910, G: 0.918, B: 0.929, A: 1} // #e8eaed — primary text
	colorTextBright = Color.RGBA{R: 0.910, G: 0.918, B: 0.929, A: 1} // #e8eaed
	colorTextDim    = Color.RGBA{R: 0.420, G: 0.439, B: 0.463, A: 1} // #6b7076 — tertiary (textSubtle)
	colorTextMuted  = Color.RGBA{R: 0.604, G: 0.627, B: 0.651, A: 1} // #9aa0a6 — secondary
	colorAccent     = Color.RGBA{R: 0.298, G: 0.553, B: 1.0, A: 1}   // #4c8dff — oklch blue
	colorAccentHover = Color.RGBA{R: 0.373, G: 0.608, B: 1.0, A: 1}  // #5f9bff
	colorSelected   = Color.RGBA{R: 0.298, G: 0.553, B: 1.0, A: 0.16} // accent at 16% for selection
	colorSuccess    = Color.RGBA{R: 0.290, G: 0.871, B: 0.502, A: 1}  // #4ade80 — connected/ready
	colorBtnNormal  = Color.RGBA{R: 0.149, G: 0.157, B: 0.173, A: 1} // #26282c (bgElev)
	colorBtnHover   = Color.RGBA{R: 0.180, G: 0.188, B: 0.212, A: 1} // #2e3036 (bgRaised)

	// SQL syntax highlighting
	colorSQLKeyword  = Color.RGBA{R: 0.78, G: 0.57, B: 0.92, A: 1}  // #c792ea — purple keywords
	colorSQLString   = Color.RGBA{R: 0.65, G: 0.84, B: 0.65, A: 1}  // #a5d6a7 — green strings
	colorSQLNumber   = Color.RGBA{R: 1.0, G: 0.72, B: 0.42, A: 1}   // #ffb86c — orange numbers
	colorSQLComment  = Color.RGBA{R: 0.42, G: 0.44, B: 0.463, A: 1} // #6b7076 — subtle
	colorSQLSymbol   = Color.RGBA{R: 0.604, G: 0.627, B: 0.651, A: 1} // #9aa0a6
	colorSQLFunction = Color.RGBA{R: 0.85, G: 0.75, B: 0.50, A: 1}  // #d9bf80 — gold
)

func makeStyleBox(bg Color.RGBA, radius int, border int, borderColor Color.RGBA) StyleBoxFlat.Instance {
	sb := StyleBoxFlat.New()
	sb.SetBgColor(bg)
	sb.SetCornerRadiusAll(radius)
	if border > 0 {
		sb.SetBorderWidthAll(border)
		sb.SetBorderColor(borderColor)
	}
	return sb
}

func makeStyleBoxPadded(bg Color.RGBA, radius int, border int, borderColor Color.RGBA, pad float32) StyleBoxFlat.Instance {
	sb := makeStyleBox(bg, radius, border, borderColor)
	sb.AsStyleBox().SetContentMarginAll(pad)
	return sb
}

func applyButtonTheme(c Control.Instance) {
	normal := makeStyleBox(colorAccent, 5, 0, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(12)
	normal.AsStyleBox().SetContentMarginRight(12)
	normal.AsStyleBox().SetContentMarginTop(5)
	normal.AsStyleBox().SetContentMarginBottom(5)

	hover := makeStyleBox(colorAccentHover, 5, 0, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(12)
	hover.AsStyleBox().SetContentMarginRight(12)
	hover.AsStyleBox().SetContentMarginTop(5)
	hover.AsStyleBox().SetContentMarginBottom(5)

	pressed := makeStyleBox(Color.RGBA{R: 0.247, G: 0.490, B: 0.941, A: 1}, 5, 0, colorBorder)
	pressed.AsStyleBox().SetContentMarginLeft(12)
	pressed.AsStyleBox().SetContentMarginRight(12)
	pressed.AsStyleBox().SetContentMarginTop(5)
	pressed.AsStyleBox().SetContentMarginBottom(5)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", pressed.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextBright)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

func applySecondaryButtonTheme(c Control.Instance) {
	normal := makeStyleBox(colorBtnNormal, 5, 1, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(10)
	normal.AsStyleBox().SetContentMarginRight(10)
	normal.AsStyleBox().SetContentMarginTop(4)
	normal.AsStyleBox().SetContentMarginBottom(4)

	hover := makeStyleBox(colorBtnHover, 5, 1, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(10)
	hover.AsStyleBox().SetContentMarginRight(10)
	hover.AsStyleBox().SetContentMarginTop(4)
	hover.AsStyleBox().SetContentMarginBottom(4)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

func applySidebarTabTheme(c Control.Instance, active bool) {
	if active {
		bg := makeStyleBox(colorBgRaised, 5, 0, colorBgRaised)
		sb := bg.AsStyleBox()
		sb.SetContentMarginTop(4)
		sb.SetContentMarginBottom(4)
		sb.SetContentMarginLeft(12)
		sb.SetContentMarginRight(12)
		c.AddThemeStyleboxOverride("normal", sb)
		c.AddThemeStyleboxOverride("hover", sb)
		c.AddThemeStyleboxOverride("pressed", sb)
		c.AddThemeColorOverride("font_color", colorText)
		c.AddThemeColorOverride("font_hover_color", colorText)
	} else {
		transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
		bg := makeStyleBox(transparent, 5, 0, transparent)
		sb := bg.AsStyleBox()
		sb.SetContentMarginTop(4)
		sb.SetContentMarginBottom(4)
		sb.SetContentMarginLeft(12)
		sb.SetContentMarginRight(12)
		c.AddThemeStyleboxOverride("normal", sb)
		c.AddThemeStyleboxOverride("hover", sb)
		c.AddThemeStyleboxOverride("pressed", sb)
		c.AddThemeColorOverride("font_color", colorTextMuted)
		c.AddThemeColorOverride("font_hover_color", colorTextMuted)
	}
}

func applyToggleButtonTheme(c Control.Instance, active bool) {
	if active {
		bg := makeStyleBoxPadded(colorBtnHover, 3, 1, colorBorder, 2)
		c.AddThemeStyleboxOverride("normal", bg.AsStyleBox())
		c.AddThemeStyleboxOverride("hover", bg.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", bg.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextBright)
	} else {
		bg := makeStyleBoxPadded(colorBtnNormal, 3, 0, colorBtnNormal, 2)
		c.AddThemeStyleboxOverride("normal", bg.AsStyleBox())
		hover := makeStyleBoxPadded(colorBtnHover, 3, 0, colorBtnHover, 2)
		c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextDim)
	}
}

func applyActiveButtonTheme(c Control.Instance) {
	active := makeStyleBoxPadded(colorAccent, 3, 1, colorAccent, 2)
	c.AddThemeStyleboxOverride("normal", active.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", active.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", active.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorBg)
	c.AddThemeColorOverride("font_hover_color", colorBg)
}

func applyInputTheme(c Control.Instance) {
	normal := makeStyleBoxPadded(colorBgPanel, 6, 1, colorBorder, 0)
	normal.AsStyleBox().SetContentMarginLeft(10)
	normal.AsStyleBox().SetContentMarginRight(10)
	normal.AsStyleBox().SetContentMarginTop(6)
	normal.AsStyleBox().SetContentMarginBottom(6)
	focus := makeStyleBoxPadded(colorBgPanel, 6, 1, colorAccent, 0)
	focus.AsStyleBox().SetContentMarginLeft(10)
	focus.AsStyleBox().SetContentMarginRight(10)
	focus.AsStyleBox().SetContentMarginTop(6)
	focus.AsStyleBox().SetContentMarginBottom(6)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("focus", focus.AsStyleBox())
	c.AddThemeStyleboxOverride("read_only", normal.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("font_placeholder_color", colorTextDim)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

func applyTreeTheme(c Control.Instance) {
	panel := makeStyleBox(colorBg, 0, 0, colorBg)
	c.AddThemeStyleboxOverride("panel", panel.AsStyleBox())

	// Compact row height (~32px) with 4px vertical padding
	selected := makeStyleBox(colorSelected, 0, 0, colorBorder)
	selected.AsStyleBox().SetContentMarginAll(4)
	c.AddThemeStyleboxOverride("selected", selected.AsStyleBox())
	c.AddThemeStyleboxOverride("selected_focus", selected.AsStyleBox())

	// Title button (column headers) — right border acts as always-visible resize separator
	titleBtn := makeStyleBox(colorBgHeader, 0, 0, colorBorder)
	titleBtn.SetBorderWidthBottom(1)
	titleBtn.SetBorderWidthRight(1)
	titleBtn.SetBorderColor(colorBorder)
	titleBtn.AsStyleBox().SetContentMarginTop(3)
	titleBtn.AsStyleBox().SetContentMarginBottom(3)
	titleBtn.AsStyleBox().SetContentMarginLeft(8)
	titleBtn.AsStyleBox().SetContentMarginRight(8)
	c.AddThemeStyleboxOverride("title_button_normal", titleBtn.AsStyleBox())
	c.AddThemeStyleboxOverride("title_button_hover", titleBtn.AsStyleBox())
	c.AddThemeStyleboxOverride("title_button_pressed", titleBtn.AsStyleBox())

	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("title_button_color", colorTextMuted)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))

	// Row hover — subtle blue tint
	hover := makeStyleBox(Color.RGBA{R: 0.298, G: 0.553, B: 1.0, A: 0.08}, 0, 0, colorBorder)
	hover.AsStyleBox().SetContentMarginAll(4)
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("hovered", hover.AsStyleBox())

	// Minimal scrollbar
	empty := StyleBoxEmpty.New()
	c.AddThemeStyleboxOverride("scroll_focus", empty.AsStyleBox())
}

func applySidebarTreeTheme(c Control.Instance) {
	panel := makeStyleBox(colorBgSidebar, 0, 0, colorBgSidebar)
	c.AddThemeStyleboxOverride("panel", panel.AsStyleBox())

	selected := makeStyleBox(colorSelected, 2, 0, colorBorder)
	selected.AsStyleBox().SetContentMarginAll(1)
	c.AddThemeStyleboxOverride("selected", selected.AsStyleBox())
	c.AddThemeStyleboxOverride("selected_focus", selected.AsStyleBox())

	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
}

func applyTextEditTheme(c Control.Instance) {
	normal := makeStyleBoxPadded(colorBg, 0, 0, colorBg, 0)
	focus := makeStyleBoxPadded(colorBg, 0, 0, colorBg, 0)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("focus", focus.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
}

func applyLabelTheme(c Control.Instance, dim bool) {
	if dim {
		c.AddThemeColorOverride("font_color", colorTextMuted)
	} else {
		c.AddThemeColorOverride("font_color", colorText)
	}
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
}

func applyStatusBarTheme(c Control.Instance) {
	c.AddThemeColorOverride("font_color", colorTextDim)
	c.AddThemeFontSizeOverride("font_size", fontSize(11))
}

// Toolbar colors
var (
	colorTitleBar  = Color.RGBA{R: 0.165, G: 0.169, B: 0.184, A: 1} // #2a2b2f
	colorTitlePill = Color.RGBA{R: 0.149, G: 0.157, B: 0.173, A: 1} // #26282c (bgElev)
)

func applyTitleBarTheme(c Control.Instance) {
	sb := makeStyleBox(colorTitleBar, 0, 0, colorTitleBar)
	sb.SetBorderWidthBottom(1)
	sb.SetBorderColor(colorBorder)
	c.AddThemeStyleboxOverride("panel", sb.AsStyleBox())
}

// applyPrimaryCompactButtonTheme is a smaller variant of applyButtonTheme
// used for inline CTAs like "Run" inside editor headers.
func applyPrimaryCompactButtonTheme(c Control.Instance) {
	normal := makeStyleBox(colorAccent, 5, 0, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(12)
	normal.AsStyleBox().SetContentMarginRight(12)
	normal.AsStyleBox().SetContentMarginTop(5)
	normal.AsStyleBox().SetContentMarginBottom(5)

	hover := makeStyleBox(Color.RGBA{R: 0.353, G: 0.557, B: 1.0, A: 1}, 5, 0, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(12)
	hover.AsStyleBox().SetContentMarginRight(12)
	hover.AsStyleBox().SetContentMarginTop(5)
	hover.AsStyleBox().SetContentMarginBottom(5)

	pressed := makeStyleBox(Color.RGBA{R: 0.227, G: 0.431, B: 0.906, A: 1}, 5, 0, colorBorder)
	pressed.AsStyleBox().SetContentMarginLeft(12)
	pressed.AsStyleBox().SetContentMarginRight(12)
	pressed.AsStyleBox().SetContentMarginTop(5)
	pressed.AsStyleBox().SetContentMarginBottom(5)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", pressed.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextBright)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

// applyCompactSecondaryButtonTheme — pagination/status-bar buttons.
func applyCompactSecondaryButtonTheme(c Control.Instance) {
	normal := makeStyleBox(colorBtnNormal, 3, 1, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(6)
	normal.AsStyleBox().SetContentMarginRight(6)
	normal.AsStyleBox().SetContentMarginTop(1)
	normal.AsStyleBox().SetContentMarginBottom(1)
	hover := makeStyleBox(colorBtnHover, 3, 1, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(6)
	hover.AsStyleBox().SetContentMarginRight(6)
	hover.AsStyleBox().SetContentMarginTop(1)
	hover.AsStyleBox().SetContentMarginBottom(1)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(11))
}

// applySegmentedControlTrack wraps the Items/History tab row.
func applySegmentedControlTrack(c Control.Instance) {
	sb := makeStyleBox(colorBgInput, 6, 1, colorBorder)
	sb.AsStyleBox().SetContentMarginAll(2)
	c.AddThemeStyleboxOverride("panel", sb.AsStyleBox())
}

// applySegmentedTab replaces applySidebarTabTheme for segmented controls.
func applySegmentedTab(c Control.Instance, active bool) {
	if active {
		sb := makeStyleBox(colorBgRaised, 5, 0, colorBgRaised)
		sb.AsStyleBox().SetContentMarginTop(4)
		sb.AsStyleBox().SetContentMarginBottom(4)
		sb.AsStyleBox().SetContentMarginLeft(12)
		sb.AsStyleBox().SetContentMarginRight(12)
		c.AddThemeStyleboxOverride("normal", sb.AsStyleBox())
		c.AddThemeStyleboxOverride("hover", sb.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", sb.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextBright)
		c.AddThemeColorOverride("font_hover_color", colorTextBright)
	} else {
		empty := StyleBoxEmpty.New()
		empty.AsStyleBox().SetContentMarginTop(4)
		empty.AsStyleBox().SetContentMarginBottom(4)
		empty.AsStyleBox().SetContentMarginLeft(12)
		empty.AsStyleBox().SetContentMarginRight(12)
		c.AddThemeStyleboxOverride("normal", empty.AsStyleBox())
		c.AddThemeStyleboxOverride("hover", empty.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", empty.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextMuted)
		c.AddThemeColorOverride("font_hover_color", colorText)
	}
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

func applyPillTheme(c Control.Instance) {
	pill := makeStyleBoxPadded(colorTitlePill, 6, 1, colorBorder, 6)
	c.AddThemeStyleboxOverride("panel", pill.AsStyleBox())
}

// applyConnectionPillTheme styles a button as a clickable connection picker pill.
func applyConnectionPillTheme(c Control.Instance) {
	normal := makeStyleBox(colorBgPanel, 6, 1, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(12)
	normal.AsStyleBox().SetContentMarginRight(12)
	normal.AsStyleBox().SetContentMarginTop(5)
	normal.AsStyleBox().SetContentMarginBottom(5)

	hover := makeStyleBox(colorBgRaised, 6, 1, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(12)
	hover.AsStyleBox().SetContentMarginRight(12)
	hover.AsStyleBox().SetContentMarginTop(5)
	hover.AsStyleBox().SetContentMarginBottom(5)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

// applyFlatButtonTheme styles a button as flat text (no background).
func applyFlatButtonTheme(c Control.Instance) {
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	normal := makeStyleBox(transparent, 0, 0, transparent)
	normal.AsStyleBox().SetContentMarginLeft(6)
	normal.AsStyleBox().SetContentMarginRight(6)
	normal.AsStyleBox().SetContentMarginTop(2)
	normal.AsStyleBox().SetContentMarginBottom(2)

	hover := makeStyleBox(colorBtnHover, 3, 0, colorBtnHover)
	hover.AsStyleBox().SetContentMarginLeft(6)
	hover.AsStyleBox().SetContentMarginRight(6)
	hover.AsStyleBox().SetContentMarginTop(2)
	hover.AsStyleBox().SetContentMarginBottom(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

// applyIconButtonTheme styles a small transparent icon button (26x26).
func applyIconButtonTheme(c Control.Instance) {
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	normal := makeStyleBox(transparent, 3, 0, transparent)
	normal.AsStyleBox().SetContentMarginAll(2)

	hover := makeStyleBox(colorBtnHover, 3, 0, colorBtnHover)
	hover.AsStyleBox().SetContentMarginAll(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(11))
}

// applyDashedButtonTheme styles a button with a dashed border (for "+" add affordance).
func applyDashedButtonTheme(c Control.Instance) {
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	normal := makeStyleBox(transparent, 4, 1, colorBorder)
	normal.AsStyleBox().SetContentMarginAll(2)

	hover := makeStyleBox(colorBtnHover, 4, 1, colorBorderStrong)
	hover.AsStyleBox().SetContentMarginAll(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
}

func applyPanelBg(c Control.Instance, bg Color.RGBA) {
	sb := makeStyleBox(bg, 0, 0, bg)
	c.AddThemeStyleboxOverride("panel", sb.AsStyleBox())
}

func applyTabBarTheme(c Control.Instance) {
	c.AddThemeFontSizeOverride("font_size", fontSize(12))
	c.AddThemeColorOverride("font_selected_color", colorText)
	c.AddThemeColorOverride("font_unselected_color", colorTextMuted)
	c.AddThemeColorOverride("font_hovered_color", colorText)

	// Mono system font
	mono := SystemFont.New()
	mono.SetFontNames([]string{"SF Mono", "Menlo", "monospace"})
	c.AddThemeFontOverride("font", mono.AsFont())

	// Active tab: accent bottom border
	active := StyleBoxFlat.New()
	active.SetBgColor(colorBg)
	active.SetCornerRadiusAll(0)
	active.SetBorderWidthBottom(2)
	active.SetBorderColor(colorAccent)
	active.AsStyleBox().SetContentMarginLeft(10)
	active.AsStyleBox().SetContentMarginTop(6)
	active.AsStyleBox().SetContentMarginRight(10)
	active.AsStyleBox().SetContentMarginBottom(6)
	c.AddThemeStyleboxOverride("tab_selected", active.AsStyleBox())

	// Inactive tab
	inactive := StyleBoxFlat.New()
	inactive.SetBgColor(Color.RGBA{R: 0, G: 0, B: 0, A: 0})
	inactive.SetCornerRadiusAll(0)
	inactive.AsStyleBox().SetContentMarginLeft(10)
	inactive.AsStyleBox().SetContentMarginTop(6)
	inactive.AsStyleBox().SetContentMarginRight(10)
	inactive.AsStyleBox().SetContentMarginBottom(6)
	c.AddThemeStyleboxOverride("tab_unselected", inactive.AsStyleBox())

	// Hovered tab
	hovered := StyleBoxFlat.New()
	hovered.SetBgColor(colorBtnHover)
	hovered.SetCornerRadiusAll(0)
	hovered.AsStyleBox().SetContentMarginLeft(10)
	hovered.AsStyleBox().SetContentMarginTop(6)
	hovered.AsStyleBox().SetContentMarginRight(10)
	hovered.AsStyleBox().SetContentMarginBottom(6)
	c.AddThemeStyleboxOverride("tab_hovered", hovered.AsStyleBox())

	// Spacing
	c.AddThemeConstantOverride("h_separation", 0)

	closeIcon := makeCloseIconSVG()
	c.AddThemeIconOverride("close", closeIcon.AsTexture2D())
}

// SVG icon strings (Lucide icons)
const closeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#d9d9d9" stroke-opacity="0.8" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`

// Sidebar left: panel-left icon
const svgSidebarLeft = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#c8c8c8" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/></svg>`

// Sidebar right: panel-right icon
const svgSidebarRight = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#c8c8c8" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/></svg>`

func loadSVGTexture(svgStr string) Texture2D.Instance {
	img := Image.New()
	if err := img.LoadSvgFromString(svgStr); err != nil {
		return Texture2D.Instance{}
	}
	tex := ImageTexture.CreateFromImage(img)
	return tex.AsTexture2D()
}

func makeCloseIconSVG() ImageTexture.Instance {
	img := Image.New()
	if err := img.LoadSvgFromString(closeSVG); err != nil {
		// Fallback: simple 12x12 image
		img = Image.Create(12, 12, false, Image.FormatRgba8)
		img.Fill(Color.RGBA{R: 0.85, G: 0.85, B: 0.85, A: 0.8})
	}
	return ImageTexture.CreateFromImage(img)
}
