package ui

import (
	"strings"

	"bufflehead/internal/models"

	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/StyleBoxFlat"
	"graphics.gd/variant/Color"
)

// Data-type chip accent colors, keyed by category. Chips render the accent as
// text over a low-opacity tint of the same hue (Pro-Grade Data System).
var (
	colorTypeInt   = Color.RGBA{R: 0.5373, G: 0.8078, B: 1.0, A: 1}    // #89CEFF — blue
	colorTypeFloat = Color.RGBA{R: 0.3059, G: 0.8706, B: 0.6392, A: 1} // #4EDEA3 — green
	colorTypeBool  = Color.RGBA{R: 0.7647, G: 0.7529, B: 1.0, A: 1}    // #C3C0FF — lavender
	colorTypeTime  = Color.RGBA{R: 0.878, G: 0.702, B: 0.255, A: 1}    // #E0B341 — amber
	colorTypeJSON  = Color.RGBA{R: 0.353, G: 0.824, B: 0.769, A: 1}    // #5AD2C4 — teal
	colorTypeEnum  = Color.RGBA{R: 0.702, G: 0.616, B: 1.0, A: 1}      // #B39DFF — violet
	colorTypeText  = Color.RGBA{R: 0.7804, G: 0.7686, B: 0.8471, A: 1} // #C7C4D8 — neutral
)

// typeChipColor returns the accent color for a SQL data type's chip.
func typeChipColor(dataType string) Color.RGBA {
	switch models.TypeCategory(dataType) {
	case models.TypeInt:
		return colorTypeInt
	case models.TypeFloat:
		return colorTypeFloat
	case models.TypeBool:
		return colorTypeBool
	case models.TypeTime:
		return colorTypeTime
	case models.TypeJSON:
		return colorTypeJSON
	case models.TypeEnum:
		return colorTypeEnum
	case models.TypeText:
		return colorTypeText
	default:
		return colorTextDim
	}
}

// typeChipLabel condenses a data type into a short uppercase chip label:
// strips a nullable "?" and type parameters, and keeps only the leading token
// (e.g. "TIMESTAMP WITH TIME ZONE" → "TIMESTAMP", "DECIMAL(10,2)" → "DECIMAL").
func typeChipLabel(dataType string) string {
	t := strings.ToUpper(strings.TrimSpace(dataType))
	t = strings.TrimSuffix(t, "?")
	arr := strings.HasSuffix(t, "[]")
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if f := strings.Fields(t); len(f) > 0 {
		t = f[0]
	}
	if arr && !strings.HasSuffix(t, "[]") {
		t += "[]"
	}
	if t == "" {
		return "—"
	}
	return t
}

// makeAccentChip builds a small rounded chip: accent-colored uppercase text
// over a 15%-opacity tint of the same hue. `nodeName` names the root node so it
// is identifiable in the control-server ui-tree.
func makeAccentChip(text string, accent Color.RGBA, nodeName string) PanelContainer.Instance {
	tint := accent
	tint.A = 0.15

	chip := PanelContainer.New()
	chip.AsNode().SetName(nodeName)
	chip.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)

	sb := StyleBoxFlat.New()
	sb.SetBgColor(tint)
	sb.SetCornerRadiusAll(3)
	sb.AsStyleBox().SetContentMarginLeft(4)
	sb.AsStyleBox().SetContentMarginRight(4)
	sb.AsStyleBox().SetContentMarginTop(1)
	sb.AsStyleBox().SetContentMarginBottom(1)
	chip.AsControl().AddThemeStyleboxOverride("panel", sb.AsStyleBox())

	lbl := Label.New()
	lbl.SetText(text)
	lbl.AsControl().AddThemeColorOverride("font_color", accent)
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(9))
	chip.AsNode().AddChild(lbl.AsNode())

	return chip
}

// makeTypeChip builds a small rounded, color-coded data-type chip. The node is
// named "TypeChip" so it is identifiable in the control-server ui-tree.
func makeTypeChip(dataType string) PanelContainer.Instance {
	chip := makeAccentChip(typeChipLabel(dataType), typeChipColor(dataType), "TypeChip")
	chip.AsControl().SetTooltipText(strings.ToUpper(strings.TrimSpace(dataType)))
	return chip
}
