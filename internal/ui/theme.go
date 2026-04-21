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

// navFontBase is the base size for all navigation / chrome text.
const navFontBase = 10

// scaled returns a scaled layout dimension in logical points.
func scaled(base float32) float32 {
	return base * float32(uiScale)
}

// Studio Sonoma dark theme palette — near-black surfaces with blue-tinted accents.
var (
	colorBg         = Color.RGBA{R: 0.055, G: 0.055, B: 0.055, A: 1}  // #0E0E0E — surface
	colorBgSidebar  = Color.RGBA{R: 0.098, G: 0.102, B: 0.102, A: 1}  // #191A1A — surface-container
	colorBgDarker   = Color.RGBA{R: 0.0, G: 0.0, B: 0.0, A: 1}       // #000000 — surface-container-lowest
	colorBgPanel    = Color.RGBA{R: 0.075, G: 0.075, B: 0.075, A: 1}  // #131313 — surface-container-low
	colorBgInput    = Color.RGBA{R: 0.122, G: 0.125, B: 0.125, A: 1}  // #1F2020 — surface-container-high
	colorBgHeader   = Color.RGBA{R: 0.122, G: 0.125, B: 0.125, A: 1}  // #1F2020 — surface-container-high
	colorRowOdd     = Color.RGBA{R: 0.055, G: 0.055, B: 0.055, A: 1}  // #0E0E0E — surface
	colorRowEven    = Color.RGBA{R: 0.075, G: 0.075, B: 0.075, A: 1}  // #131313 — surface-container-low
	colorBorder     = Color.RGBA{R: 0.278, G: 0.282, B: 0.282, A: 0.4} // #474848 at 40% — outline-variant
	colorBorderDim  = Color.RGBA{R: 0.278, G: 0.282, B: 0.282, A: 0.15} // #474848 at 15% — ghost border
	colorText       = Color.RGBA{R: 0.902, G: 0.898, B: 0.898, A: 1}  // #E6E5E5 — on-surface
	colorTextBright = Color.RGBA{R: 1.0, G: 1.0, B: 1.0, A: 1}       // #FFFFFF
	colorTextDim    = Color.RGBA{R: 0.459, G: 0.459, B: 0.459, A: 1}  // #757575 — outline
	colorTextMuted  = Color.RGBA{R: 0.671, G: 0.671, B: 0.671, A: 1}  // #ABABAB — on-surface-variant
	colorAccent     = Color.RGBA{R: 0.678, G: 0.776, B: 1.0, A: 1}    // #ADC6FF — primary
	colorSelected   = Color.RGBA{R: 0.0, G: 0.267, B: 0.576, A: 1}    // #004493 — primary-container
	colorBtnNormal  = Color.RGBA{R: 0.122, G: 0.125, B: 0.125, A: 1}  // #1F2020 — surface-container-high
	colorBtnHover   = Color.RGBA{R: 0.145, G: 0.149, B: 0.149, A: 1}  // #252626 — surface-container-highest

	// SQL syntax highlighting — Studio Sonoma palette
	colorSQLKeyword  = Color.RGBA{R: 0.678, G: 0.776, B: 1.0, A: 1}    // #ADC6FF — primary
	colorSQLString   = Color.RGBA{R: 0.847, G: 0.827, B: 0.957, A: 1}  // #D8D3F4 — tertiary-container
	colorSQLNumber   = Color.RGBA{R: 0.70, G: 0.85, B: 0.55, A: 1}    // #B3D98C — green (kept)
	colorSQLComment  = Color.RGBA{R: 0.459, G: 0.459, B: 0.459, A: 1}  // #757575 — outline
	colorSQLSymbol   = Color.RGBA{R: 0.671, G: 0.671, B: 0.671, A: 1}  // #ABABAB — on-surface-variant
	colorSQLFunction = Color.RGBA{R: 0.792, G: 0.776, B: 0.902, A: 1}  // #CAC6E6 — tertiary-dim
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
	// CTA button — primary-container bg with light blue text
	normal := makeStyleBox(colorSelected, 4, 0, colorBorder) // #004493
	normal.AsStyleBox().SetContentMarginLeft(8)
	normal.AsStyleBox().SetContentMarginRight(8)
	normal.AsStyleBox().SetContentMarginTop(2)
	normal.AsStyleBox().SetContentMarginBottom(2)

	hover := makeStyleBox(Color.RGBA{R: 0.0, G: 0.345, B: 0.733, A: 1}, 4, 0, colorBorder) // #0058BB
	hover.AsStyleBox().SetContentMarginLeft(8)
	hover.AsStyleBox().SetContentMarginRight(8)
	hover.AsStyleBox().SetContentMarginTop(2)
	hover.AsStyleBox().SetContentMarginBottom(2)

	pressed := makeStyleBox(Color.RGBA{R: 0.0, G: 0.239, B: 0.529, A: 1}, 4, 0, colorBorder) // #003D87
	pressed.AsStyleBox().SetContentMarginLeft(8)
	pressed.AsStyleBox().SetContentMarginRight(8)
	pressed.AsStyleBox().SetContentMarginTop(2)
	pressed.AsStyleBox().SetContentMarginBottom(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", pressed.AsStyleBox())
	c.AddThemeColorOverride("font_color", Color.RGBA{R: 0.737, G: 0.816, B: 1.0, A: 1}) // #BCD0FF — on-primary-container
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

func applyNavButtonTheme(c Control.Instance) {
	// Compact icon button with contrast background for title bar nav
	normal := makeStyleBox(colorBgHeader, 4, 0, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(4)
	normal.AsStyleBox().SetContentMarginRight(4)
	normal.AsStyleBox().SetContentMarginTop(2)
	normal.AsStyleBox().SetContentMarginBottom(2)

	hover := makeStyleBox(colorBtnHover, 4, 0, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(4)
	hover.AsStyleBox().SetContentMarginRight(4)
	hover.AsStyleBox().SetContentMarginTop(2)
	hover.AsStyleBox().SetContentMarginBottom(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
}

func applySecondaryButtonTheme(c Control.Instance) {
	// Secondary button with 4px radius
	normal := makeStyleBox(colorBtnNormal, 4, 1, colorBorder)
	normal.AsStyleBox().SetContentMarginLeft(8)
	normal.AsStyleBox().SetContentMarginRight(8)
	normal.AsStyleBox().SetContentMarginTop(2)
	normal.AsStyleBox().SetContentMarginBottom(2)

	hover := makeStyleBox(colorBtnHover, 4, 1, colorBorder)
	hover.AsStyleBox().SetContentMarginLeft(8)
	hover.AsStyleBox().SetContentMarginRight(8)
	hover.AsStyleBox().SetContentMarginTop(2)
	hover.AsStyleBox().SetContentMarginBottom(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

func applySidebarTabTheme(c Control.Instance, active bool) {
	transparent := Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	if active {
		bg := makeStyleBox(transparent, 0, 0, transparent)
		bg.SetBorderWidthBottom(2)
		bg.SetBorderColor(colorAccent)
		sb := bg.AsStyleBox()
		sb.SetContentMarginTop(2)
		sb.SetContentMarginBottom(2)
		sb.SetContentMarginLeft(6)
		sb.SetContentMarginRight(6)
		c.AddThemeStyleboxOverride("normal", sb)
		c.AddThemeStyleboxOverride("hover", sb)
		c.AddThemeStyleboxOverride("pressed", sb)
		c.AddThemeColorOverride("font_color", colorTextBright)
		c.AddThemeColorOverride("font_hover_color", colorTextBright)
	} else {
		bg := makeStyleBox(transparent, 0, 0, transparent)
		sb := bg.AsStyleBox()
		sb.SetContentMarginTop(2)
		sb.SetContentMarginBottom(4)
		sb.SetContentMarginLeft(6)
		sb.SetContentMarginRight(6)
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
	active := makeStyleBoxPadded(colorSelected, 3, 1, colorSelected, 2) // primary-container
	c.AddThemeStyleboxOverride("normal", active.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", active.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", active.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextBright)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
}

func applyInputTheme(c Control.Instance) {
	normal := makeStyleBoxPadded(colorBgInput, 4, 1, colorBorder, 2)
	focus := makeStyleBoxPadded(colorBgInput, 4, 1, colorAccent, 2)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("focus", focus.AsStyleBox())
	c.AddThemeStyleboxOverride("read_only", normal.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("font_placeholder_color", colorTextDim)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

func applyTreeTheme(c Control.Instance) {
	panel := makeStyleBox(colorBg, 0, 0, colorBg)
	c.AddThemeStyleboxOverride("panel", panel.AsStyleBox())

	// Compact row height with 1px vertical padding
	selected := makeStyleBox(colorSelected, 0, 0, colorBorder)
	selected.AsStyleBox().SetContentMarginAll(1)
	c.AddThemeStyleboxOverride("selected", selected.AsStyleBox())
	c.AddThemeStyleboxOverride("selected_focus", selected.AsStyleBox())

	// Title button (column headers) — right border acts as always-visible resize separator
	titleBtn := makeStyleBox(colorBgHeader, 0, 0, colorBorder)
	titleBtn.SetBorderWidthBottom(1)
	titleBtn.SetBorderWidthRight(1)
	titleBtn.SetBorderColor(colorBorder)
	titleBtn.AsStyleBox().SetContentMarginTop(1)
	titleBtn.AsStyleBox().SetContentMarginBottom(1)
	titleBtn.AsStyleBox().SetContentMarginLeft(6)
	titleBtn.AsStyleBox().SetContentMarginRight(6)
	c.AddThemeStyleboxOverride("title_button_normal", titleBtn.AsStyleBox())
	c.AddThemeStyleboxOverride("title_button_hover", titleBtn.AsStyleBox())
	c.AddThemeStyleboxOverride("title_button_pressed", titleBtn.AsStyleBox())

	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("title_button_color", colorTextMuted)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))

	// Row hover — subtle surface-bright tint
	hover := makeStyleBox(Color.RGBA{R: 0.169, G: 0.173, B: 0.173, A: 0.6}, 0, 0, colorBorder) // #2B2C2C at 60%
	hover.AsStyleBox().SetContentMarginAll(1)
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
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

func applyTextEditTheme(c Control.Instance) {
	normal := makeStyleBoxPadded(colorBgInput, 4, 1, colorBorder, 2)
	focus := makeStyleBoxPadded(colorBgInput, 4, 1, colorAccent, 2)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("focus", focus.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

func applyLabelTheme(c Control.Instance, dim bool) {
	if dim {
		c.AddThemeColorOverride("font_color", colorTextMuted)
	} else {
		c.AddThemeColorOverride("font_color", colorText)
	}
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

func applyStatusBarTheme(c Control.Instance) {
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

// Title bar colors — Studio Sonoma
var (
	colorTitleBar  = Color.RGBA{R: 0.075, G: 0.075, B: 0.075, A: 1}  // #131313 — surface-container-low
	colorTitlePill = Color.RGBA{R: 0.122, G: 0.125, B: 0.125, A: 1}  // #1F2020 — surface-container-high
)

func applyTitleBarTheme(c Control.Instance) {
	applyPanelBg(c, colorTitleBar)
}

func applyPillTheme(c Control.Instance) {
	pill := makeStyleBoxPadded(colorTitlePill, 4, 0, colorBorder, 2)
	c.AddThemeStyleboxOverride("panel", pill.AsStyleBox())
}

func applyPanelBg(c Control.Instance, bg Color.RGBA) {
	sb := makeStyleBox(bg, 0, 0, bg)
	c.AddThemeStyleboxOverride("panel", sb.AsStyleBox())
}

func applyTabBarTheme(c Control.Instance) {
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
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
const closeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#ababab" stroke-opacity="0.8" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`

// Navigation chevrons (Lucide chevron-left / chevron-right) — solid white, 16px
const svgChevronLeft = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>`
const svgChevronRight = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>`

// Sidebar left: panel-left icon
const svgSidebarLeft = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ababab" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/></svg>`

// Sidebar right: panel-right icon
const svgSidebarRight = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ababab" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/></svg>`

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
