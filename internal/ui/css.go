package ui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/Node"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/StyleBoxFlat"
	"graphics.gd/classdb/SystemFont"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Color"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
)

// CSSStyle holds parsed CSS declarations for one rule block.
type CSSStyle struct {
	Props map[string]string
}

// ParseCSSBlock parses a block of CSS declarations (property: value; pairs).
// Accepts the format you'd paste from browser dev tools.
func ParseCSSBlock(css string) CSSStyle {
	style := CSSStyle{Props: make(map[string]string)}
	// Strip braces if present
	css = strings.TrimSpace(css)
	css = strings.TrimPrefix(css, "{")
	css = strings.TrimSuffix(css, "}")

	for _, line := range strings.Split(css, ";") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		prop := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Skip webkit/vendor prefixes
		if strings.HasPrefix(prop, "-webkit-") || strings.HasPrefix(prop, "-moz-") {
			continue
		}
		style.Props[prop] = val
	}
	return style
}

// ApplyToControl applies parsed CSS properties to a Godot Control.
func (s CSSStyle) ApplyToControl(ctrl Control.Instance) {
	// -- Size --
	_, hasW := s.Props["width"]
	_, hasH := s.Props["height"]
	if hasW {
		ms := ctrl.CustomMinimumSize()
		ctrl.SetCustomMinimumSize(Vector2.New(cssLength(s.Props["width"]), ms.Y))
	}
	if hasH {
		ms := ctrl.CustomMinimumSize()
		ctrl.SetCustomMinimumSize(Vector2.New(ms.X, cssLength(s.Props["height"])))
	}
	// -- Typography --
	if fs, ok := s.Props["font-size"]; ok {
		ctrl.AddThemeFontSizeOverride("font_size", int(cssLength(fs)))
	}
	if fw, ok := s.Props["font-weight"]; ok {
		weight := cssFontWeight(fw)
		sf := getOrCreateSystemFont(ctrl)
		sf.SetFontWeight(weight)
		ctrl.AddThemeFontOverride("font", sf.AsFont())
	}
	if ff, ok := s.Props["font-family"]; ok {
		sf := getOrCreateSystemFont(ctrl)
		sf.SetFontNames(cssFontFamily(ff))
		ctrl.AddThemeFontOverride("font", sf.AsFont())
	}
	if c, ok := s.Props["color"]; ok {
		ctrl.AddThemeColorOverride("font_color", cssColor(c))
		ctrl.AddThemeColorOverride("font_hover_color", cssColor(c))
	}

	// -- Background & Border --
	sbf := getOrCreateStyleBox(ctrl)
	if sbf != (StyleBoxFlat.Instance{}) {
		if bg, ok := s.Props["background"]; ok {
			sbf.SetBgColor(cssColor(bg))
		}
		if bg, ok := s.Props["background-color"]; ok {
			sbf.SetBgColor(cssColor(bg))
		}
		if br, ok := s.Props["border-radius"]; ok {
			vals := cssLengthMulti(br)
			switch len(vals) {
			case 1:
				sbf.SetCornerRadiusAll(int(vals[0]))
			case 4:
				sbf.SetCornerRadiusTopLeft(int(vals[0]))
				sbf.SetCornerRadiusTopRight(int(vals[1]))
				sbf.SetCornerRadiusBottomRight(int(vals[2]))
				sbf.SetCornerRadiusBottomLeft(int(vals[3]))
			}
		}
		if v, ok := s.Props["border-top-left-radius"]; ok {
			sbf.SetCornerRadiusTopLeft(int(cssLength(v)))
		}
		if v, ok := s.Props["border-top-right-radius"]; ok {
			sbf.SetCornerRadiusTopRight(int(cssLength(v)))
		}
		if v, ok := s.Props["border-bottom-right-radius"]; ok {
			sbf.SetCornerRadiusBottomRight(int(cssLength(v)))
		}
		if v, ok := s.Props["border-bottom-left-radius"]; ok {
			sbf.SetCornerRadiusBottomLeft(int(cssLength(v)))
		}
		if b, ok := s.Props["border"]; ok {
			parseCSSBorder(b, sbf)
		}
		if b, ok := s.Props["border-top"]; ok {
			w, c := parseCSSBorderSide(b)
			sbf.SetBorderWidthTop(w)
			if c.A > 0 {
				sbf.SetBorderColor(c)
			}
		}
		if b, ok := s.Props["border-right"]; ok {
			w, c := parseCSSBorderSide(b)
			sbf.SetBorderWidthRight(w)
			if c.A > 0 {
				sbf.SetBorderColor(c)
			}
		}
		if b, ok := s.Props["border-bottom"]; ok {
			w, c := parseCSSBorderSide(b)
			sbf.SetBorderWidthBottom(w)
			if c.A > 0 {
				sbf.SetBorderColor(c)
			}
		}
		if b, ok := s.Props["border-left"]; ok {
			w, c := parseCSSBorderSide(b)
			sbf.SetBorderWidthLeft(w)
			if c.A > 0 {
				sbf.SetBorderColor(c)
			}
		}
		if v, ok := s.Props["border-color"]; ok {
			sbf.SetBorderColor(cssColor(v))
		}
		if v, ok := s.Props["border-width"]; ok {
			sbf.SetBorderWidthAll(int(cssLength(v)))
		}
		if o, ok := s.Props["outline"]; ok {
			parseCSSOutline(o, sbf)
		}

		// Padding (shorthand and individual)
		if p, ok := s.Props["padding"]; ok {
			vals := cssLengthMulti(p)
			switch len(vals) {
			case 1:
				sbf.AsStyleBox().SetContentMarginAll(vals[0])
			case 2:
				sbf.AsStyleBox().SetContentMarginTop(vals[0])
				sbf.AsStyleBox().SetContentMarginBottom(vals[0])
				sbf.AsStyleBox().SetContentMarginLeft(vals[1])
				sbf.AsStyleBox().SetContentMarginRight(vals[1])
			case 4:
				sbf.AsStyleBox().SetContentMarginTop(vals[0])
				sbf.AsStyleBox().SetContentMarginRight(vals[1])
				sbf.AsStyleBox().SetContentMarginBottom(vals[2])
				sbf.AsStyleBox().SetContentMarginLeft(vals[3])
			}
		}
		if v, ok := s.Props["padding-top"]; ok {
			sbf.AsStyleBox().SetContentMarginTop(cssLength(v))
		}
		if v, ok := s.Props["padding-left"]; ok {
			sbf.AsStyleBox().SetContentMarginLeft(cssLength(v))
		}
		if v, ok := s.Props["padding-right"]; ok {
			sbf.AsStyleBox().SetContentMarginRight(cssLength(v))
		}
		if v, ok := s.Props["padding-bottom"]; ok {
			sbf.AsStyleBox().SetContentMarginBottom(cssLength(v))
		}
	}

	// -- Margin (for MarginContainers) --
	if _, ok := Object.As[MarginContainer.Instance](ctrl); ok {
		if m, ok := s.Props["margin"]; ok {
			vals := cssLengthMulti(m)
			switch len(vals) {
			case 1:
				v := int(vals[0])
				ctrl.AddThemeConstantOverride("margin_top", v)
				ctrl.AddThemeConstantOverride("margin_right", v)
				ctrl.AddThemeConstantOverride("margin_bottom", v)
				ctrl.AddThemeConstantOverride("margin_left", v)
			case 2:
				ctrl.AddThemeConstantOverride("margin_top", int(vals[0]))
				ctrl.AddThemeConstantOverride("margin_bottom", int(vals[0]))
				ctrl.AddThemeConstantOverride("margin_left", int(vals[1]))
				ctrl.AddThemeConstantOverride("margin_right", int(vals[1]))
			case 4:
				ctrl.AddThemeConstantOverride("margin_top", int(vals[0]))
				ctrl.AddThemeConstantOverride("margin_right", int(vals[1]))
				ctrl.AddThemeConstantOverride("margin_bottom", int(vals[2]))
				ctrl.AddThemeConstantOverride("margin_left", int(vals[3]))
			}
		}
	}

	// -- Gap (for BoxContainers) --
	if gap, ok := s.Props["gap"]; ok {
		ctrl.AddThemeConstantOverride("separation", int(cssLength(gap)))
	}

	// -- Flex alignment → BoxContainer alignment --
	if _, ok := Object.As[VBoxContainer.Instance](ctrl); ok {
		applyFlexAlignment(ctrl, s)
	} else if _, ok := Object.As[HBoxContainer.Instance](ctrl); ok {
		applyFlexAlignment(ctrl, s)
	}

	// -- Size flags --
	if v, ok := s.Props["h-sizing"]; ok {
		ctrl.SetSizeFlagsHorizontal(parseSizeFlag(v))
	}
	if v, ok := s.Props["v-sizing"]; ok {
		ctrl.SetSizeFlagsVertical(parseSizeFlag(v))
	}

	// -- Opacity --
	if op, ok := s.Props["opacity"]; ok {
		v, _ := strconv.ParseFloat(op, 32)
		ctrl.AsCanvasItem().SetModulate(Color.RGBA{R: 1, G: 1, B: 1, A: float32(v)})
	}
}

// ── CSS value parsers ──

var rgbRe = regexp.MustCompile(`rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)`)
var rgbaRe = regexp.MustCompile(`rgba\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*,\s*([0-9.]+)\s*\)`)

func cssColor(val string) Color.RGBA {
	val = strings.TrimSpace(val)

	// rgb(r, g, b)
	if m := rgbRe.FindStringSubmatch(val); len(m) == 4 {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		return Color.RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: 1}
	}
	// rgba(r, g, b, a)
	if m := rgbaRe.FindStringSubmatch(val); len(m) == 5 {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		a, _ := strconv.ParseFloat(m[4], 32)
		return Color.RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: float32(a)}
	}
	// #hex
	if strings.HasPrefix(val, "#") {
		return parseHexColor(val)
	}
	// "none" / "transparent"
	if val == "none" || val == "transparent" {
		return Color.RGBA{R: 0, G: 0, B: 0, A: 0}
	}
	return Color.RGBA{R: 1, G: 1, B: 1, A: 1}
}

func cssLength(val string) float32 {
	val = strings.TrimSpace(val)
	val = strings.TrimSuffix(val, "px")
	val = strings.TrimSuffix(val, "em")
	val = strings.TrimSuffix(val, "rem")
	val = strings.TrimSuffix(val, "%")
	f, _ := strconv.ParseFloat(val, 32)
	return float32(f)
}

func cssLengthMulti(val string) []float32 {
	parts := strings.Fields(val)
	var result []float32
	for _, p := range parts {
		result = append(result, cssLength(p))
	}
	return result
}

func cssFontWeight(val string) int {
	val = strings.TrimSpace(val)
	switch val {
	case "normal":
		return 400
	case "bold":
		return 700
	case "lighter":
		return 300
	case "bolder":
		return 800
	default:
		w, _ := strconv.Atoi(val)
		if w == 0 {
			return 400
		}
		return w
	}
}

func cssFontFamily(val string) []string {
	var families []string
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, `"'`)
		if part != "" {
			families = append(families, part)
		}
	}
	return families
}

func parseCSSBorder(val string, sbf StyleBoxFlat.Instance) {
	val = strings.TrimSpace(val)
	if val == "none" || val == "0" {
		sbf.SetBorderWidthAll(0)
		return
	}
	// Parse "1px solid #303236" or "2px solid rgb(139, 92, 246)"
	// Find the width (first number+px)
	parts := strings.Fields(val)
	if len(parts) >= 1 {
		sbf.SetBorderWidthAll(int(cssLength(parts[0])))
	}
	// Find color (last part, or rgb(...) which may span multiple parts)
	colorStr := strings.Join(parts, " ")
	if idx := strings.Index(colorStr, "#"); idx >= 0 {
		sbf.SetBorderColor(cssColor(colorStr[idx:]))
	} else if idx := strings.Index(colorStr, "rgb"); idx >= 0 {
		sbf.SetBorderColor(cssColor(colorStr[idx:]))
	}
}

// parseCSSBorderSide parses "1px solid #color" and returns width + color.
func parseCSSBorderSide(val string) (int, Color.RGBA) {
	val = strings.TrimSpace(val)
	if val == "none" || val == "0" {
		return 0, Color.RGBA{}
	}
	parts := strings.Fields(val)
	width := 0
	if len(parts) >= 1 {
		width = int(cssLength(parts[0]))
	}
	colorStr := strings.Join(parts, " ")
	var c Color.RGBA
	if idx := strings.Index(colorStr, "#"); idx >= 0 {
		c = cssColor(colorStr[idx:])
	} else if idx := strings.Index(colorStr, "rgb"); idx >= 0 {
		c = cssColor(colorStr[idx:])
	}
	return width, c
}

func parseSizeFlag(val string) Control.SizeFlags {
	switch strings.TrimSpace(val) {
	case "expand-fill", "fill":
		return Control.SizeExpandFill
	case "shrink-center", "center":
		return Control.SizeShrinkCenter
	case "shrink-begin", "begin":
		return Control.SizeShrinkBegin
	case "shrink-end", "end":
		return Control.SizeShrinkEnd
	case "expand":
		return Control.SizeExpand
	default:
		return Control.SizeFill
	}
}

func sizeFlagToCSS(f Control.SizeFlags) string {
	switch {
	case f&Control.SizeExpandFill == Control.SizeExpandFill:
		return "expand-fill"
	case f&Control.SizeExpand != 0:
		return "expand"
	case f&Control.SizeShrinkCenter != 0:
		return "shrink-center"
	case f&Control.SizeShrinkEnd != 0:
		return "shrink-end"
	case f&Control.SizeFill != 0:
		return "fill"
	default:
		return "shrink-begin"
	}
}

func parseCSSOutline(val string, sbf StyleBoxFlat.Instance) {
	// Treat outline like border for Godot purposes
	parseCSSBorder(val, sbf)
}

func getOrCreateSystemFont(ctrl Control.Instance) SystemFont.Instance {
	font := ctrl.GetThemeFont("font")
	if sf, ok := Object.As[SystemFont.Instance](font); ok {
		return sf
	}
	sf := SystemFont.New()
	sf.SetFontNames([]string{"-apple-system", "BlinkMacSystemFont", "sans-serif"})
	return sf
}

func getOrCreateStyleBox(ctrl Control.Instance) StyleBoxFlat.Instance {
	// Try "panel" (PanelContainer) first, then "normal" (Button)
	if _, ok := Object.As[PanelContainer.Instance](ctrl); ok {
		if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("panel")); ok {
			return sbf
		}
		// Create one
		sbf := StyleBoxFlat.New()
		ctrl.AddThemeStyleboxOverride("panel", sbf.AsStyleBox())
		return sbf
	}
	if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("normal")); ok {
		return sbf
	}
	return StyleBoxFlat.Instance{}
}

func applyFlexAlignment(ctrl Control.Instance, s CSSStyle) {
	if ai, ok := s.Props["align-items"]; ok {
		switch ai {
		case "center":
			ctrl.SetSizeFlagsVertical(Control.SizeShrinkCenter)
		case "flex-start", "start":
			ctrl.SetSizeFlagsVertical(Control.SizeShrinkBegin)
		case "flex-end", "end":
			ctrl.SetSizeFlagsVertical(Control.SizeShrinkEnd)
		case "stretch":
			ctrl.SetSizeFlagsVertical(Control.SizeExpandFill)
		}
	}
	if jc, ok := s.Props["justify-content"]; ok {
		if bc, ok := Object.As[BoxContainer.Instance](ctrl); ok {
			switch jc {
			case "center":
				bc.SetAlignment(BoxContainer.AlignmentCenter)
			case "flex-end", "end":
				bc.SetAlignment(BoxContainer.AlignmentEnd)
			default:
				bc.SetAlignment(BoxContainer.AlignmentBegin)
			}
		}
	}
}

// ── Stylesheet file (selector → CSS block) ──

// CSSRule is a selector + style block.
type CSSRule struct {
	Selector string
	Style    CSSStyle
}

// ParseStylesheet parses a CSS-like stylesheet with selectors.
// Supports selectors by node name or type: Button, .TitleBar, #nodeName
func ParseStylesheet(css string) []CSSRule {
	var rules []CSSRule
	css = strings.TrimSpace(css)

	for len(css) > 0 {
		// Find selector (everything before {)
		braceIdx := strings.Index(css, "{")
		if braceIdx < 0 {
			break
		}
		selector := strings.TrimSpace(css[:braceIdx])

		// Find matching }
		closeIdx := strings.Index(css[braceIdx:], "}")
		if closeIdx < 0 {
			break
		}
		closeIdx += braceIdx

		block := css[braceIdx+1 : closeIdx]
		rules = append(rules, CSSRule{
			Selector: selector,
			Style:    ParseCSSBlock(block),
		})
		css = strings.TrimSpace(css[closeIdx+1:])
	}
	return rules
}

// ApplyStylesheetToTree loads style.css and applies matching rules to the node tree.
func ApplyStylesheetToTree(root Node.Instance) {
	data, err := os.ReadFile("style.css")
	if err != nil {
		return
	}
	rules := ParseStylesheet(string(data))
	if len(rules) == 0 {
		return
	}
	fmt.Printf("[css] Loaded %d rules from style.css\n", len(rules))
	applyRulesToNode(rules, root)
}

func applyRulesToNode(rules []CSSRule, node Node.Instance) {
	name := node.Name()
	if ctrl, ok := Object.As[Control.Instance](node); ok {
		for _, rule := range rules {
			if selectorMatches(rule.Selector, name, ctrl) {
				fmt.Printf("[css] Applying %s to %s\n", rule.Selector, node.GetPath())
				rule.Style.ApplyToControl(ctrl)
			}
		}
	}
	for i := 0; i < node.GetChildCount(); i++ {
		applyRulesToNode(rules, node.GetChild(i))
	}
}

func selectorMatches(selector, nodeName string, ctrl Control.Instance) bool {
	selector = strings.TrimSpace(selector)
	if strings.HasPrefix(selector, "#") {
		return selector[1:] == nodeName
	}
	className := Object.Instance(ctrl.AsObject()).ClassName()
	return selector == className
}

// DumpNodeCSS extracts the current visual properties of a Control as CSS declarations.
func DumpNodeCSS(ctrl Control.Instance) string {
	var buf strings.Builder

	// Font size
	fs := ctrl.GetThemeFontSize("font_size")
	if fs > 0 {
		buf.WriteString(fmt.Sprintf("  font-size: %dpx;\n", fs))
	}

	// Font color
	c := ctrl.GetThemeColor("font_color")
	if c.A > 0 {
		buf.WriteString(fmt.Sprintf("  color: %s;\n", colorToHex(c)))
	}

	// Custom minimum size
	ms := ctrl.CustomMinimumSize()
	if ms.X != 0 {
		buf.WriteString(fmt.Sprintf("  width: %.0fpx;\n", ms.X))
	}
	if ms.Y != 0 {
		buf.WriteString(fmt.Sprintf("  height: %.0fpx;\n", ms.Y))
	}

	// Gap (for BoxContainers)
	isVBox := false
	isHBox := false
	if _, ok := Object.As[VBoxContainer.Instance](ctrl); ok {
		isVBox = true
	}
	if _, ok := Object.As[HBoxContainer.Instance](ctrl); ok {
		isHBox = true
	}
	if isVBox || isHBox {
		gap := ctrl.GetThemeConstant("separation")
		buf.WriteString(fmt.Sprintf("  gap: %dpx;\n", gap))
	}

	// Flex alignment (for BoxContainers)
	if isVBox || isHBox {
		if bc, ok := Object.As[BoxContainer.Instance](ctrl); ok {
			switch bc.Alignment() {
			case BoxContainer.AlignmentCenter:
				buf.WriteString("  justify-content: center;\n")
			case BoxContainer.AlignmentEnd:
				buf.WriteString("  justify-content: flex-end;\n")
			}
		}
		vFlags := ctrl.SizeFlagsVertical()
		switch {
		case vFlags&Control.SizeShrinkCenter != 0:
			buf.WriteString("  align-items: center;\n")
		case vFlags&Control.SizeShrinkEnd != 0:
			buf.WriteString("  align-items: flex-end;\n")
		case vFlags&Control.SizeExpandFill != 0:
			buf.WriteString("  align-items: stretch;\n")
		}
	}

	// Margins (for MarginContainers)
	if _, ok := Object.As[MarginContainer.Instance](ctrl); ok {
		mt := ctrl.GetThemeConstant("margin_top")
		mr := ctrl.GetThemeConstant("margin_right")
		mb := ctrl.GetThemeConstant("margin_bottom")
		ml := ctrl.GetThemeConstant("margin_left")
		buf.WriteString(fmt.Sprintf("  margin: %dpx %dpx %dpx %dpx;\n", mt, mr, mb, ml))
	}

	// Stylebox — check for explicit overrides by type
	var sbf StyleBoxFlat.Instance
	var hasSbf bool
	if _, ok := Object.As[PanelContainer.Instance](ctrl); ok {
		if s, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("panel")); ok {
			sbf = s
			hasSbf = true
		}
	} else if _, ok := Object.As[Button.Instance](ctrl); ok {
		if s, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("normal")); ok {
			sbf = s
			hasSbf = true
		}
	}
	if hasSbf {
		bg := sbf.BgColor()
		buf.WriteString(fmt.Sprintf("  background-color: %s;\n", colorToHex(bg)))

		pt := sbf.AsStyleBox().ContentMarginTop()
		pr := sbf.AsStyleBox().ContentMarginRight()
		pb := sbf.AsStyleBox().ContentMarginBottom()
		pl := sbf.AsStyleBox().ContentMarginLeft()
		buf.WriteString(fmt.Sprintf("  padding: %.0fpx %.0fpx %.0fpx %.0fpx;\n", pt, pr, pb, pl))

		bt := sbf.BorderWidthTop()
		br := sbf.BorderWidthRight()
		bb := sbf.BorderWidthBottom()
		bl := sbf.BorderWidthLeft()
		bc := sbf.BorderColor()
		if bt == br && br == bb && bb == bl {
			if bt > 0 {
				buf.WriteString(fmt.Sprintf("  border: %dpx solid %s;\n", bt, colorToHex(bc)))
			}
		} else {
			if bt > 0 {
				buf.WriteString(fmt.Sprintf("  border-top: %dpx solid %s;\n", bt, colorToHex(bc)))
			}
			if br > 0 {
				buf.WriteString(fmt.Sprintf("  border-right: %dpx solid %s;\n", br, colorToHex(bc)))
			}
			if bb > 0 {
				buf.WriteString(fmt.Sprintf("  border-bottom: %dpx solid %s;\n", bb, colorToHex(bc)))
			}
			if bl > 0 {
				buf.WriteString(fmt.Sprintf("  border-left: %dpx solid %s;\n", bl, colorToHex(bc)))
			}
		}

		crtl := sbf.CornerRadiusTopLeft()
		crtr := sbf.CornerRadiusTopRight()
		crbr := sbf.CornerRadiusBottomRight()
		crbl := sbf.CornerRadiusBottomLeft()
		if crtl == crtr && crtr == crbr && crbr == crbl {
			if crtl > 0 {
				buf.WriteString(fmt.Sprintf("  border-radius: %dpx;\n", crtl))
			}
		} else {
			buf.WriteString(fmt.Sprintf("  border-radius: %dpx %dpx %dpx %dpx;\n", crtl, crtr, crbr, crbl))
		}
	}

	// Size flags
	hFlag := ctrl.SizeFlagsHorizontal()
	vFlag := ctrl.SizeFlagsVertical()
	buf.WriteString(fmt.Sprintf("  h-sizing: %s;\n", sizeFlagToCSS(hFlag)))
	buf.WriteString(fmt.Sprintf("  v-sizing: %s;\n", sizeFlagToCSS(vFlag)))

	// Opacity
	mod := ctrl.AsCanvasItem().Modulate()
	if mod.A < 1.0 {
		buf.WriteString(fmt.Sprintf("  opacity: %.2f;\n", mod.A))
	}

	return buf.String()
}

func colorToHex(c Color.RGBA) string {
	r := int(c.R * 255)
	g := int(c.G * 255)
	b := int(c.B * 255)
	if r < 0 { r = 0 }
	if g < 0 { g = 0 }
	if b < 0 { b = 0 }
	if r > 255 { r = 255 }
	if g > 255 { g = 255 }
	if b > 255 { b = 255 }
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// SaveStylesheet saves the complete CSS for modified nodes to style.css.
// It merges existing style.css entries with newly modified nodes from the designer.
func SaveStylesheet(root Node.Instance, newMods map[string]map[string]any) {
	// Collect node paths: start with existing style.css entries
	nodePaths := make(map[string]bool)
	if data, err := os.ReadFile("style.css"); err == nil {
		rules := ParseStylesheet(string(data))
		for _, rule := range rules {
			if strings.HasPrefix(rule.Selector, "#") {
				name := rule.Selector[1:]
				walkNodes(root, func(n Node.Instance) bool {
					if n.Name() == name {
						nodePaths[n.GetPath()] = true
						return true
					}
					return false
				})
			}
		}
	}

	// Add newly modified nodes
	for path := range newMods {
		nodePaths[path] = true
	}

	// Write CSS for all tracked nodes
	var buf strings.Builder
	for path := range nodePaths {
		node, found := findNodeByPath(root, path)
		if !found {
			continue
		}
		ctrl, ok := Object.As[Control.Instance](node)
		if !ok {
			continue
		}
		name := node.Name()
		css := DumpNodeCSS(ctrl)
		if css != "" {
			buf.WriteString(fmt.Sprintf("#%s {\n%s}\n\n", name, css))
		}
	}
	content := buf.String()
	if content == "" {
		fmt.Println("[css] No nodes to save")
		return
	}
	if err := os.WriteFile("style.css", []byte(content), 0644); err != nil {
		fmt.Println("[css] write error:", err)
		return
	}
	fmt.Println("[css] Saved style.css")
}

func findNodeByPath(root Node.Instance, path string) (Node.Instance, bool) {
	var found Node.Instance
	ok := false
	walkNodes(root, func(n Node.Instance) bool {
		if n.GetPath() == path {
			found = n
			ok = true
			return true
		}
		return false
	})
	return found, ok
}

func walkNodes(node Node.Instance, fn func(Node.Instance) bool) bool {
	if fn(node) {
		return true
	}
	for i := 0; i < node.GetChildCount(); i++ {
		if walkNodes(node.GetChild(i), fn) {
			return true
		}
	}
	return false
}
