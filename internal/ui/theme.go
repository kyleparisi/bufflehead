package ui

import (
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/Font"
	"graphics.gd/classdb/Image"
	"graphics.gd/classdb/ImageTexture"
	"graphics.gd/classdb/StyleBoxEmpty"
	"graphics.gd/classdb/StyleBoxFlat"
	"graphics.gd/classdb/SystemFont"
	"graphics.gd/classdb/Texture2D"
	"graphics.gd/variant/Color"
)

// fontSize returns a font size in logical points.
func fontSize(base int) int {
	return base
}

// navFontBase is the base size for all navigation / chrome text.
const navFontBase = 10

// Database switcher popover + breadcrumb tunables. Rendered at true pixel size
// now that popup windows inherit the root's HiDPI content scale.
const (
	popoverRowFont    = 14  // database name rows
	popoverHeaderFont = 12  // "SWITCH DATABASE" caption
	popoverFooterFont = 13  // "Refresh list"
	popoverBadgeFont  = 11  // count badge
	popoverRowPadX    = 12  // row horizontal padding
	popoverRowPadY    = 6   // row vertical padding
	pillMaxWidth      = 360 // connection breadcrumb max width
	pillLineHeight    = 17  // breadcrumb line box ≈ 1.5 × 11px font
)

// scaled returns a layout dimension in logical points.
func scaled(base float32) float32 {
	return base
}

// Pro-Grade Data System — Modern IDE dark theme with an indigo/lavender primary,
// green + blue semantic accents, and low-contrast tonal layering (Stitch design tokens).
var (
	colorBg         = Color.RGBA{R: 0.0745, G: 0.0745, B: 0.0824, A: 1}   // #131315 — surface / background
	colorBgSidebar  = Color.RGBA{R: 0.1255, G: 0.1216, B: 0.1333, A: 1}   // #201F22 — surface-container
	colorBgDarker   = Color.RGBA{R: 0.0549, G: 0.0549, B: 0.0627, A: 1}   // #0E0E10 — surface-container-lowest
	colorBgPanel    = Color.RGBA{R: 0.1098, G: 0.1059, B: 0.1137, A: 1}   // #1C1B1D — surface-container-low
	colorBgInput    = Color.RGBA{R: 0.1647, G: 0.1647, B: 0.1725, A: 1}   // #2A2A2C — surface-container-high
	colorBgHeader   = Color.RGBA{R: 0.1647, G: 0.1647, B: 0.1725, A: 1}   // #2A2A2C — surface-container-high
	colorRowOdd     = Color.RGBA{R: 0.0745, G: 0.0745, B: 0.0824, A: 1}   // #131315 — surface (zebra light)
	colorRowEven    = Color.RGBA{R: 0.0549, G: 0.0549, B: 0.0627, A: 1}   // #0E0E10 — surface-container-lowest (zebra dark)
	colorBorder     = Color.RGBA{R: 0.2745, G: 0.2706, B: 0.3333, A: 0.5} // #464555 at 50% — outline-variant
	colorBorderDim  = Color.RGBA{R: 0.2745, G: 0.2706, B: 0.3333, A: 0.2} // #464555 at 20% — ghost border
	colorText       = Color.RGBA{R: 0.898, G: 0.8824, B: 0.8941, A: 1}    // #E5E1E4 — on-surface
	colorTextBright = Color.RGBA{R: 1.0, G: 1.0, B: 1.0, A: 1}            // #FFFFFF
	colorTextDim    = Color.RGBA{R: 0.5686, G: 0.5608, B: 0.6314, A: 1}   // #918FA1 — outline
	colorTextMuted  = Color.RGBA{R: 0.7804, G: 0.7686, B: 0.8471, A: 1}   // #C7C4D8 — on-surface-variant
	colorAccent     = Color.RGBA{R: 0.7647, G: 0.7529, B: 1.0, A: 1}      // #C3C0FF — primary
	colorSelected   = Color.RGBA{R: 0.3098, G: 0.2745, B: 0.898, A: 1}    // #4F46E5 — primary-container (indigo)
	colorBtnNormal  = Color.RGBA{R: 0.1647, G: 0.1647, B: 0.1725, A: 1}   // #2A2A2C — surface-container-high
	colorBtnHover   = Color.RGBA{R: 0.2078, G: 0.2039, B: 0.2157, A: 1}   // #353437 — surface-container-highest

	// SQL syntax highlighting — green keywords, blue strings (matches the design's editor)
	colorSQLKeyword  = Color.RGBA{R: 0.3059, G: 0.8706, B: 0.6392, A: 1} // #4EDEA3 — tertiary (green)
	colorSQLString   = Color.RGBA{R: 0.5373, G: 0.8078, B: 1.0, A: 1}    // #89CEFF — secondary (blue)
	colorSQLNumber   = Color.RGBA{R: 0.7647, G: 0.7529, B: 1.0, A: 1}    // #C3C0FF — primary (lavender)
	colorSQLComment  = Color.RGBA{R: 0.5686, G: 0.5608, B: 0.6314, A: 1} // #918FA1 — outline
	colorSQLSymbol   = Color.RGBA{R: 0.7804, G: 0.7686, B: 0.8471, A: 1} // #C7C4D8 — on-surface-variant
	colorSQLFunction = Color.RGBA{R: 0.4039, G: 0.9569, B: 0.7176, A: 1} // #67F4B7 — on-tertiary-container
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
	// CTA button — solid indigo primary-container with light lavender text
	normal := makeStyleBox(colorSelected, 4, 0, colorBorder) // #4F46E5
	normal.AsStyleBox().SetContentMarginLeft(8)
	normal.AsStyleBox().SetContentMarginRight(8)
	normal.AsStyleBox().SetContentMarginTop(2)
	normal.AsStyleBox().SetContentMarginBottom(2)

	hover := makeStyleBox(Color.RGBA{R: 0.3569, G: 0.3216, B: 0.9373, A: 1}, 4, 0, colorBorder) // #5B52EF
	hover.AsStyleBox().SetContentMarginLeft(8)
	hover.AsStyleBox().SetContentMarginRight(8)
	hover.AsStyleBox().SetContentMarginTop(2)
	hover.AsStyleBox().SetContentMarginBottom(2)

	pressed := makeStyleBox(Color.RGBA{R: 0.251, G: 0.2196, B: 0.7882, A: 1}, 4, 0, colorBorder) // #4038C9
	pressed.AsStyleBox().SetContentMarginLeft(8)
	pressed.AsStyleBox().SetContentMarginRight(8)
	pressed.AsStyleBox().SetContentMarginTop(2)
	pressed.AsStyleBox().SetContentMarginBottom(2)

	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", pressed.AsStyleBox())
	c.AddThemeColorOverride("font_color", Color.RGBA{R: 0.8549, G: 0.8431, B: 1.0, A: 1}) // #DAD7FF — on-primary-container
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

// applyConnTileTheme styles a connection-rail tile: a rounded 40px square that
// is solid indigo when active, and a subtle surface that brightens on hover
// when inactive.
func applyConnTileTheme(c Control.Instance, active bool) {
	if active {
		sb := makeStyleBox(colorSelected, 8, 0, colorSelected) // indigo, rounded-lg
		c.AddThemeStyleboxOverride("normal", sb.AsStyleBox())
		c.AddThemeStyleboxOverride("hover", sb.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", sb.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextBright)
		c.AddThemeColorOverride("font_hover_color", colorTextBright)
	} else {
		normal := makeStyleBox(colorBg, 8, 0, colorBg) // subtle raised surface
		hover := makeStyleBox(colorBgInput, 8, 1, colorBorder)
		c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
		c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
		c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
		c.AddThemeColorOverride("font_color", colorTextMuted)
		c.AddThemeColorOverride("font_hover_color", colorText)
	}
	c.AddThemeFontSizeOverride("font_size", fontSize(11))
}

// applyConnTileErrorTheme styles a rail tile to signal a tunnel/connection
// error (rounded red), keeping the rounded-tile shape.
func applyConnTileErrorTheme(c Control.Instance) {
	errorBg := Color.RGBA{R: 0.6, G: 0.1, B: 0.1, A: 1}
	sb := makeStyleBox(errorBg, 8, 0, errorBg)
	c.AddThemeStyleboxOverride("normal", sb.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", sb.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", sb.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextBright)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
	c.AddThemeFontSizeOverride("font_size", fontSize(11))
}

// applyBreadcrumbSegmentTheme styles the clickable database segment in the
// title-bar breadcrumb: mono text with a subtle indigo-tinted background that
// brightens on hover, signaling it opens the database switcher.
func applyBreadcrumbSegmentTheme(c Control.Instance) {
	tint := Color.RGBA{R: 0.3098, G: 0.2745, B: 0.898, A: 0.14}  // indigo @ 14%
	hover := Color.RGBA{R: 0.3098, G: 0.2745, B: 0.898, A: 0.28} // indigo @ 28%
	normal := makeStyleBox(tint, 4, 0, tint)
	normal.AsStyleBox().SetContentMarginLeft(6)
	normal.AsStyleBox().SetContentMarginRight(6)
	normal.AsStyleBox().SetContentMarginTop(1)
	normal.AsStyleBox().SetContentMarginBottom(1)
	hoverSB := makeStyleBox(hover, 4, 0, hover)
	hoverSB.AsStyleBox().SetContentMarginLeft(6)
	hoverSB.AsStyleBox().SetContentMarginRight(6)
	hoverSB.AsStyleBox().SetContentMarginTop(1)
	hoverSB.AsStyleBox().SetContentMarginBottom(1)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hoverSB.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hoverSB.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
}

// ── Database switcher popover ────────────────────────────────────────────────

// databasePopoverPanel styles the switcher's floating panel — surface-container-high
// with a soft drop shadow, matching the Stitch "Switch Database" popover.
func databasePopoverPanel() StyleBoxFlat.Instance {
	sb := makeStyleBox(colorBgInput, 8, 1, colorBorder)
	sb.SetShadowColor(Color.RGBA{R: 0, G: 0, B: 0, A: 0.5})
	sb.SetShadowSize(int(scaled(10)))
	return sb
}

// dbRowMargins gives a switcher row its inner padding (live-tunable).
func dbRowMargins(sb StyleBoxFlat.Instance) {
	sb.AsStyleBox().SetContentMarginLeft(popoverRowPadX)
	sb.AsStyleBox().SetContentMarginRight(popoverRowPadX)
	sb.AsStyleBox().SetContentMarginTop(popoverRowPadY)
	sb.AsStyleBox().SetContentMarginBottom(popoverRowPadY)
}

// applyDbRowSelectable styles a switchable database row: transparent at rest,
// surface-highest on hover.
func applyDbRowSelectable(c Control.Instance) {
	normal := makeStyleBox(Color.RGBA{}, 4, 0, colorBorder)
	dbRowMargins(normal)
	hover := makeStyleBox(colorBtnHover, 4, 0, colorBorder)
	dbRowMargins(hover)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeColorOverride("font_hover_color", colorTextBright)
	c.AddThemeFontOverride("font", monoFont())
	c.AddThemeFontSizeOverride("font_size", fontSize(popoverRowFont))
}

// applyDbRowCurrent styles the currently-connected database row: indigo tint with
// a 3px indigo left accent and lavender text. Disabled (you're already here).
func applyDbRowCurrent(c Control.Instance) {
	tint := Color.RGBA{R: 0.3098, G: 0.2745, B: 0.898, A: 0.16} // indigo @ 16%
	sb := makeStyleBox(tint, 4, 0, colorSelected)
	sb.SetBorderWidthLeft(3)
	sb.SetBorderColor(colorSelected)
	dbRowMargins(sb)
	for _, state := range []string{"normal", "hover", "pressed", "disabled"} {
		c.AddThemeStyleboxOverride(state, sb.AsStyleBox())
	}
	c.AddThemeColorOverride("font_color", colorAccent)
	c.AddThemeColorOverride("font_disabled_color", colorAccent)
	c.AddThemeFontOverride("font", monoFont())
	c.AddThemeFontSizeOverride("font_size", fontSize(popoverRowFont))
}

// applyDbRowSystem styles a system/maintenance database row: transparent and dim.
// Disabled (can't be opened as a working database).
func applyDbRowSystem(c Control.Instance) {
	empty := makeStyleBox(Color.RGBA{}, 4, 0, colorBorder)
	dbRowMargins(empty)
	c.AddThemeStyleboxOverride("normal", empty.AsStyleBox())
	c.AddThemeStyleboxOverride("disabled", empty.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextDim)
	c.AddThemeColorOverride("font_disabled_color", colorTextDim)
	c.AddThemeFontOverride("font", monoFont())
	c.AddThemeFontSizeOverride("font_size", fontSize(popoverRowFont))
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

// applyFooterGhostButton styles a borderless icon button for the status bar:
// no background, muted glyph that brightens on hover.
func applyFooterGhostButton(c Control.Instance) {
	transparent := Color.RGBA{}
	normal := makeStyleBoxPadded(transparent, 3, 0, transparent, 3)
	hover := makeStyleBoxPadded(colorBtnHover, 3, 0, colorBtnHover, 3)
	c.AddThemeStyleboxOverride("normal", normal.AsStyleBox())
	c.AddThemeStyleboxOverride("hover", hover.AsStyleBox())
	c.AddThemeStyleboxOverride("pressed", hover.AsStyleBox())
	c.AddThemeColorOverride("font_color", colorTextMuted)
	c.AddThemeColorOverride("font_hover_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
}

// footerPill styles a compact rounded "pill" panel (e.g. the memory indicator).
func footerPill() StyleBoxFlat.Instance {
	pill := makeStyleBoxPadded(colorBgInput, 8, 1, colorBorder, 2)
	pill.AsStyleBox().SetContentMarginLeft(6)
	pill.AsStyleBox().SetContentMarginRight(6)
	return pill
}

// Title bar colors — Pro-Grade Data System
var (
	colorTitleBar  = Color.RGBA{R: 0.1255, G: 0.1216, B: 0.1333, A: 1} // #201F22 — surface-container
	colorTitlePill = Color.RGBA{R: 0.1098, G: 0.1059, B: 0.1137, A: 1} // #1C1B1D — surface-container-low
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
const closeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#c7c4d8" stroke-opacity="0.8" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>`

// Navigation chevrons (Lucide chevron-left / chevron-right) — solid white, 16px
const svgChevronLeft = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>`
const svgChevronRight = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>`

// Sidebar left: panel-left icon
const svgSidebarLeft = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#c7c4d8" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/></svg>`

// Sidebar right: panel-right icon
const svgSidebarRight = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#c7c4d8" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="18" height="18" x="3" y="3" rx="2"/><path d="M15 3v18"/></svg>`

// Database icon (Lucide "database") — secondary blue, for the title-bar breadcrumb pill.
const svgDatabase = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#89ceff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M3 5V19A9 3 0 0 0 21 19V5"/><path d="M3 12A9 3 0 0 0 21 12"/></svg>`

// titlePill styles the title-bar breadcrumb pill (rounded, surface-container-high).
func titlePill() StyleBoxFlat.Instance {
	pill := makeStyleBoxPadded(colorBgInput, 8, 1, colorBorder, 2)
	pill.AsStyleBox().SetContentMarginLeft(8)
	pill.AsStyleBox().SetContentMarginRight(8)
	return pill
}

// monoFont returns a monospaced system font for code/SQL text.
func monoFont() Font.Instance {
	f := SystemFont.New()
	f.SetFontNames([]string{"SF Mono", "Menlo", "monospace"})
	return f.AsFont()
}

// boldFont returns a semibold UI system font for emphasized headings.
func boldFont() Font.Instance {
	f := SystemFont.New()
	f.SetFontNames([]string{"SF Pro Display", "Helvetica Neue", "Arial", "sans-serif"})
	f.SetFontWeight(600)
	return f.AsFont()
}

// Themed checkbox icons — subtle outlined square when off, indigo fill + white
// check when on (matches the design's custom checkbox, not Godot's default).
// svgCheckCircle — indigo filled circle with a white check, marks the current DB
// in the database switcher.
const svgCheckCircle = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="7" fill="#4f46e5"/><path d="M4.6 8.2 L6.9 10.6 L11.6 5.2" fill="none" stroke="#ffffff" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>`

// svgLock — dim padlock, marks system/maintenance databases that can't be opened.
const svgLock = `<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#918fa1" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>`

const svgCheckboxOff = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><rect x="1" y="1" width="14" height="14" rx="3" fill="none" stroke="#5a5968" stroke-width="1.5"/></svg>`
const svgCheckboxOn = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><rect x="1" y="1" width="14" height="14" rx="3" fill="#4f46e5"/><path d="M4.5 8.2 L6.8 10.5 L11.5 5.2" fill="none" stroke="#ffffff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>`

// applyCheckboxTheme replaces a CheckBox's default (bright) check glyphs with
// theme-matched indigo icons and removes the bright hover/press backgrounds.
func applyCheckboxTheme(c Control.Instance) {
	on := loadSVGTexture(svgCheckboxOn)
	off := loadSVGTexture(svgCheckboxOff)
	c.AddThemeIconOverride("checked", on)
	c.AddThemeIconOverride("unchecked", off)
	c.AddThemeIconOverride("checked_disabled", on)
	c.AddThemeIconOverride("unchecked_disabled", off)

	empty := StyleBoxEmpty.New().AsStyleBox()
	for _, state := range []string{"normal", "hover", "pressed", "focus", "hover_pressed", "disabled"} {
		c.AddThemeStyleboxOverride(state, empty)
	}
	c.AddThemeColorOverride("font_color", colorText)
	c.AddThemeFontSizeOverride("font_size", fontSize(13))
	c.AddThemeConstantOverride("h_separation", 4)
}

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
