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

	// Map physical pixel height to a discrete scale factor.
	var scale float64
	switch {
	case physH <= 1080: // 1080p
		scale = 1.0
	case physH <= 1440: // QHD
		scale = 1.33
	case physH <= 2160: // Retina laptops, 4K
		scale = 1.5
	default: // 5K, 8K+
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

// Dark theme palette (TablePlus-inspired)
var (
	colorBg         = Color.RGBA{R: 0.11, G: 0.11, B: 0.12, A: 1}  // #1C1C1E
	colorBgSidebar  = Color.RGBA{R: 0.14, G: 0.14, B: 0.16, A: 1}  // #242428
	colorBgDarker   = Color.RGBA{R: 0.094, G: 0.094, B: 0.10, A: 1} // #18181A
	colorBgPanel    = Color.RGBA{R: 0.165, G: 0.165, B: 0.18, A: 1} // #2A2A2E
	colorBgInput    = Color.RGBA{R: 0.165, G: 0.165, B: 0.18, A: 1} // #2A2A2E
	colorBgHeader   = Color.RGBA{R: 0.145, G: 0.145, B: 0.16, A: 1} // #252528
	colorRowOdd     = Color.RGBA{R: 0.11, G: 0.11, B: 0.12, A: 1}  // #1C1C1E
	colorRowEven    = Color.RGBA{R: 0.133, G: 0.133, B: 0.15, A: 1} // #222226
	colorBorder     = Color.RGBA{R: 0.227, G: 0.227, B: 0.243, A: 1} // #3A3A3E
	colorBorderDim  = Color.RGBA{R: 0.18, G: 0.18, B: 0.20, A: 1}   // #2E2E32
	colorText       = Color.RGBA{R: 0.90, G: 0.90, B: 0.90, A: 1}  // #E5E5E5
	colorTextBright = Color.RGBA{R: 1.0, G: 1.0, B: 1.0, A: 1}     // #FFFFFF
	colorTextDim    = Color.RGBA{R: 0.333, G: 0.333, B: 0.345, A: 1} // #555558
	colorTextMuted  = Color.RGBA{R: 0.557, G: 0.557, B: 0.576, A: 1} // #8E8E93
	colorAccent     = Color.RGBA{R: 0.29, G: 0.494, B: 0.969, A: 1} // #4A7EF7
	colorSelected   = Color.RGBA{R: 0.118, G: 0.227, B: 0.435, A: 1} // #1E3A6F
	colorBtnNormal  = Color.RGBA{R: 0.165, G: 0.165, B: 0.18, A: 1} // #2A2A2E
	colorBtnHover   = Color.RGBA{R: 0.22, G: 0.22, B: 0.235, A: 1} // #38383C

	// SQL syntax highlighting
	colorSQLKeyword  = Color.RGBA{R: 0.40, G: 0.60, B: 1.0, A: 1}  // #6699FF — brighter blue
	colorSQLString   = Color.RGBA{R: 0.80, G: 0.58, B: 0.38, A: 1} // #CC9460 — warm orange
	colorSQLNumber   = Color.RGBA{R: 0.70, G: 0.85, B: 0.55, A: 1} // #B3D98C — green
	colorSQLComment  = Color.RGBA{R: 0.42, G: 0.42, B: 0.435, A: 1} // #6B6B6F — muted tone
	colorSQLSymbol   = Color.RGBA{R: 0.65, G: 0.65, B: 0.667, A: 1} // #A6A6AA — blue-tinted gray
	colorSQLFunction = Color.RGBA{R: 0.85, G: 0.75, B: 0.50, A: 1} // #D9BF80 — gold
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
	// Premium CTA button with vivid accent blue
	normal := makeStyleBox(colorAccent, 6, 0, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(14)
	normal.AsStyleBox().SetContentMarginRight(14)
	normal.AsStyleBox().SetContentMarginTop(8)
	normal.AsStyleBox().SetContentMarginBottom(8)

	hover := makeStyleBox(Color.RGBA{R: 0.353, G: 0.557, B: 1.0, A: 1}, 6, 0, colorBorder) // #5A8EFF
	hover.AsStyleBox().SetContentMarginLeft(14)
	hover.AsStyleBox().SetContentMarginRight(14)
	hover.AsStyleBox().SetContentMarginTop(8)
	hover.AsStyleBox().SetContentMarginBottom(8)

	pressed := makeStyleBox(Color.RGBA{R: 0.227, G: 0.431, B: 0.906, A: 1}, 6, 0, colorBorder) // #3A6EE7
	pressed.AsStyleBox().SetContentMarginLeft(14)
	pressed.AsStyleBox().SetContentMarginRight(14)
	pressed.AsStyleBox().SetContentMarginTop(8)
	pressed.AsStyleBox().SetContentMarginBottom(8)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", pressed.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextBright)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
}

func applySecondaryButtonTheme(c Control.Instance) {
	// Secondary button with 6px radius
	normal := makeStyleBox(colorBtnNormal, 6, 1, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(14)
	normal.AsStyleBox().SetContentMarginRight(14)
	normal.AsStyleBox().SetContentMarginTop(8)
	normal.AsStyleBox().SetContentMarginBottom(8)

	hover := makeStyleBox(colorBtnHover, 6, 1, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(14)
	hover.AsStyleBox().SetContentMarginRight(14)
	hover.AsStyleBox().SetContentMarginTop(8)
	hover.AsStyleBox().SetContentMarginBottom(8)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
}

func applySidebarTabTheme(c Control.Instance, active bool) {
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	if active {
		bg := makeStyleBox(transparent, 0, 0, transparent)
		bg.SetBorderWidthBottom(2)
		bg.SetBorderColor(colorAccent)
		sb := bg.AsStyleBox()
		sb.SetContentMarginTop(6)
		sb.SetContentMarginBottom(6)
		sb.SetContentMarginLeft(8)
		sb.SetContentMarginRight(8)
		c.AddThemeStyleboxOverride("normal", sb)
		c.AddThemeStyleboxOverride("hover", sb)
		c.AddThemeStyleboxOverride("pressed", sb)
		c.AddThemeColorOverride("font_color", colorTextBright)
		c.AddThemeColorOverride("font_hover_color", colorTextBright)
	} else {
		bg := makeStyleBox(transparent, 0, 0, transparent)
		sb := bg.AsStyleBox()
		sb.SetContentMarginTop(6)
		sb.SetContentMarginBottom(8)
		sb.SetContentMarginLeft(8)
		sb.SetContentMarginRight(8)
		c.AddThemeStyleboxOverride("normal", sb)
		c.AddThemeStyleboxOverride("hover", sb)
		c.AddThemeStyleboxOverride("pressed", sb)
		c.AddThemeColorOverride("font_color", colorTextDim)
		c.AddThemeColorOverride("font_hover_color", colorTextMuted)
	}
}

func applyToggleButtonTheme(c Control.Instance, active bool) {
	if active {
		bg := makeStyleBoxPadded(colorBtnHover, 3, 1, colorBorder, 3)
		c.AddThemeStyleboxOverride("normal", bg.AsStyleBox())
		c.AddThemeStyleboxOverride("hover", bg.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", bg.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextBright)
	} else {
		bg := makeStyleBoxPadded(colorBtnNormal, 3, 0, colorBtnNormal, 3)
		c.AddThemeStyleboxOverride("normal", bg.AsStyleBox())
		hover := makeStyleBoxPadded(colorBtnHover, 3, 0, colorBtnHover, 3)
		c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextDim)
	}
}

func applyActiveButtonTheme(c Control.Instance) {
	active := makeStyleBoxPadded(colorAccent, 3, 1, colorAccent, 5)
	c.AddThemeStyleboxOverride("normal", active.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", active.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", active.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorBg)
	c.AddThemeColorOverride("font_hover_color", colorBg)
}

func applyInputTheme(c Control.Instance) {
	normal := makeStyleBoxPadded(colorBgInput, 3, 1, colorBorder, 5)
	focus := makeStyleBoxPadded(colorBgInput, 3, 1, colorAccent, 5)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("focus", focus.AsStyleBox())
	c.AddThemeStyleboxOverride("read_only", normal.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("font_placeholder_color", colorTextDim)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
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

	// Row hover - vivid blue tint (#1E3050)
	hover := makeStyleBox(Color.RGBA{R: 0.118, G: 0.188, B: 0.314, A: 1}, 0, 0, colorBorder)
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
	normal := makeStyleBoxPadded(colorBgInput, 3, 1, colorBorder, 6)
	focus := makeStyleBoxPadded(colorBgInput, 3, 1, colorAccent, 6)
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
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
}

// Title bar colors
var (
	colorTitleBar  = Color.RGBA{R: 0.11, G: 0.11, B: 0.12, A: 1}  // #1C1C1E (same as main bg)
	colorTitlePill = Color.RGBA{R: 0.22, G: 0.22, B: 0.235, A: 1} // #38383C
)

func applyTitleBarTheme(c Control.Instance) {
	applyPanelBg(c, colorTitleBar)
}

func applyPillTheme(c Control.Instance) {
	pill := makeStyleBoxPadded(colorTitlePill, 6, 0, colorBorder, 6)
	c.AddThemeStyleboxOverride("panel", pill.AsStyleBox())
}

func applyPanelBg(c Control.Instance, bg Color.RGBA) {
	sb := makeStyleBox(bg, 0, 0, bg)
	c.AddThemeStyleboxOverride("panel", sb.AsStyleBox())
}

func applyTabBarTheme(c Control.Instance) {
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
	c.AddThemeColorOverride("font_selected_color", colorTextBright)
	c.AddThemeColorOverride("font_unselected_color", colorTextDim)
	c.AddThemeColorOverride("font_hovered_color", colorText)

	// Mono system font
	mono := SystemFont.New()
	mono.SetFontNames([]string{"SF Mono", "Menlo", "monospace"})
	c.AddThemeFontOverride("font", mono.AsFont())

	// Tighter tab padding
	active := StyleBoxFlat.New()
	active.SetBgColor(colorBgSidebar)
	active.SetCornerRadiusAll(3)
	active.AsStyleBox().SetContentMarginLeft(6)
	active.AsStyleBox().SetContentMarginTop(2)
	active.AsStyleBox().SetContentMarginRight(6)
	active.AsStyleBox().SetContentMarginBottom(2)
	c.AddThemeStyleboxOverride("tab_selected", active.AsStyleBox())

	inactive := StyleBoxFlat.New()
	inactive.SetBgColor(colorBg)
	inactive.SetCornerRadiusAll(3)
	inactive.AsStyleBox().SetContentMarginLeft(6)
	inactive.AsStyleBox().SetContentMarginTop(2)
	inactive.AsStyleBox().SetContentMarginRight(6)
	inactive.AsStyleBox().SetContentMarginBottom(2)
	c.AddThemeStyleboxOverride("tab_unselected", inactive.AsStyleBox())

	hovered := StyleBoxFlat.New()
	hovered.SetBgColor(colorBtnHover)
	hovered.SetCornerRadiusAll(3)
	hovered.AsStyleBox().SetContentMarginLeft(6)
	hovered.AsStyleBox().SetContentMarginTop(2)
	hovered.AsStyleBox().SetContentMarginRight(6)
	hovered.AsStyleBox().SetContentMarginBottom(2)
	c.AddThemeStyleboxOverride("tab_hovered", hovered.AsStyleBox())

	// Spacing
	c.AddThemeConstantOverride("h_separation", 4)

	// Small close icon (8x8 X at 80% opacity)
	// Close icon from SVG (Lucide "x" icon) at 80% opacity
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
