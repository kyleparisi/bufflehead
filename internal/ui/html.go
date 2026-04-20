package ui

import (
	"fmt"
	"strings"

	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/Node"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/TextEdit"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Vector2"
)

// htmlNode is a lightweight parsed representation of an HTML element.
type htmlNode struct {
	Tag      string
	Attrs    map[string]string
	Text     string
	Children []*htmlNode
}

// BuildFromHTML parses an HTML string and builds Godot nodes under parent.
// Returns the top-level nodes created.
func BuildFromHTML(html string, parent Node.Instance) []Control.Instance {
	nodes := parseHTML(html)
	var created []Control.Instance
	for _, n := range nodes {
		if ctrl := buildNode(n, parent); ctrl != (Control.Instance{}) {
			created = append(created, ctrl)
		}
	}
	return created
}

func buildNode(n *htmlNode, parent Node.Instance) Control.Instance {
	// Transparent elements: just process children directly into parent
	if isTransparentElement(n.Tag) {
		var last Control.Instance
		for _, child := range n.Children {
			if c := buildNode(child, parent); c != (Control.Instance{}) {
				last = c
			}
		}
		return last
	}

	var ctrl Control.Instance

	switch n.Tag {
	case "div":
		style := n.Attrs["style"]
		if strings.Contains(style, "flex-direction") && strings.Contains(style, "row") {
			box := HBoxContainer.New()
			box.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
			ctrl = box.AsControl()
		} else {
			box := VBoxContainer.New()
			box.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
			ctrl = box.AsControl()
		}

	case "span", "p", "label":
		lbl := Label.New()
		lbl.SetText(n.Text)
		lbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		ctrl = lbl.AsControl()

	case "h1", "h2", "h3", "h4", "h5", "h6":
		lbl := Label.New()
		lbl.SetText(n.Text)
		sizes := map[string]int{"h1": 24, "h2": 20, "h3": 16, "h4": 14, "h5": 12, "h6": 10}
		lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(sizes[n.Tag]))
		ctrl = lbl.AsControl()

	case "button":
		btn := Button.New()
		btn.SetText(n.Text)
		applySecondaryButtonTheme(btn.AsControl())
		ctrl = btn.AsControl()

	case "input":
		le := LineEdit.New()
		if ph := n.Attrs["placeholder"]; ph != "" {
			le.SetPlaceholderText(ph)
		}
		if v := n.Attrs["value"]; v != "" {
			le.SetText(v)
		}
		le.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		applyInputTheme(le.AsControl())
		ctrl = le.AsControl()

	case "textarea":
		te := TextEdit.New()
		te.SetText(n.Text)
		te.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		te.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(80)))
		applyTextEditTheme(te.AsControl())
		ctrl = te.AsControl()

	case "hr":
		sep := PanelContainer.New()
		sep.AsControl().SetCustomMinimumSize(Vector2.New(0, 1))
		sep.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		applyPanelBg(sep.AsControl(), colorBorder)
		ctrl = sep.AsControl()

	case "br":
		spacer := Control.New()
		spacer.SetCustomMinimumSize(Vector2.New(0, scaled(8)))
		ctrl = spacer.AsControl()

	case "margin":
		mc := MarginContainer.New()
		mc.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		mc.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
		ctrl = mc.AsControl()

	default:
		// Unknown tag → treat as VBox
		box := VBoxContainer.New()
		box.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		ctrl = box.AsControl()
	}

	// Set name from id or class
	if id := n.Attrs["id"]; id != "" {
		ctrl.AsNode().SetName(id)
	} else if class := n.Attrs["class"]; class != "" {
		ctrl.AsNode().SetName(class)
	}

	// Apply inline style
	if style := n.Attrs["style"]; style != "" {
		css := ParseCSSBlock(style)
		css.ApplyToControl(ctrl)
	}

	// Add to parent
	parent.AddChild(ctrl.AsNode())

	// Recurse children
	for _, child := range n.Children {
		buildNode(child, ctrl.AsNode())
	}

	return ctrl
}

// ── Minimal HTML parser ──

func parseHTML(html string) []*htmlNode {
	html = strings.TrimSpace(html)
	var nodes []*htmlNode
	for len(html) > 0 {
		node, rest := parseElement(html)
		if node != nil {
			nodes = append(nodes, node)
		}
		if rest == html {
			break // no progress
		}
		html = strings.TrimSpace(rest)
	}
	return nodes
}

func parseElement(s string) (*htmlNode, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, ""
	}

	// Text node (before any tag)
	if s[0] != '<' {
		idx := strings.Index(s, "<")
		if idx < 0 {
			text := strings.TrimSpace(decodeHTMLEntities(s))
			if text == "" {
				return nil, ""
			}
			return &htmlNode{Tag: "span", Text: text}, ""
		}
		text := strings.TrimSpace(decodeHTMLEntities(s[:idx]))
		if text != "" {
			return &htmlNode{Tag: "span", Text: text}, s[idx:]
		}
		return parseElement(s[idx:])
	}

	// Skip comments
	if strings.HasPrefix(s, "<!--") {
		end := strings.Index(s, "-->")
		if end < 0 {
			return nil, ""
		}
		return nil, s[end+3:]
	}

	// Closing tag (shouldn't reach here, but skip it)
	if strings.HasPrefix(s, "</") {
		end := strings.Index(s, ">")
		if end < 0 {
			return nil, ""
		}
		return nil, s[end+1:]
	}

	// Opening tag
	end := strings.Index(s, ">")
	if end < 0 {
		return nil, ""
	}

	tagContent := s[1:end]
	selfClosing := strings.HasSuffix(tagContent, "/")
	if selfClosing {
		tagContent = tagContent[:len(tagContent)-1]
	}

	tag, attrs := parseTag(tagContent)
	tag = strings.ToLower(tag)
	rest := s[end+1:]

	node := &htmlNode{Tag: tag, Attrs: attrs}

	// Self-closing or void elements
	if selfClosing || isVoidElement(tag) {
		return node, rest
	}

	// Skip non-visual elements entirely (consume until closing tag)
	if isSkippedElement(tag) {
		closeTag := fmt.Sprintf("</%s>", tag)
		idx := strings.Index(strings.ToLower(rest), closeTag)
		if idx >= 0 {
			return nil, rest[idx+len(closeTag):]
		}
		return nil, ""
	}

	// Parse children until closing tag
	closeTag := fmt.Sprintf("</%s>", tag)
	for len(rest) > 0 {
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(strings.ToLower(rest), closeTag) {
			rest = rest[len(closeTag):]
			break
		}
		child, newRest := parseElement(rest)
		if child != nil {
			// Inline text nodes get merged into parent
			if child.Tag == "span" && child.Text != "" && len(node.Children) == 0 && node.Text == "" {
				node.Text = child.Text
			} else {
				node.Children = append(node.Children, child)
			}
		}
		if newRest == rest {
			break // no progress
		}
		rest = newRest
	}

	return node, rest
}

func parseTag(s string) (string, map[string]string) {
	s = strings.TrimSpace(s)
	attrs := make(map[string]string)

	// Tag name
	spaceIdx := strings.IndexAny(s, " \t\n\r")
	if spaceIdx < 0 {
		return s, attrs
	}
	tag := s[:spaceIdx]
	s = strings.TrimSpace(s[spaceIdx:])

	// Parse attributes
	for len(s) > 0 {
		s = strings.TrimSpace(s)
		if s == "" {
			break
		}
		eqIdx := strings.Index(s, "=")
		if eqIdx < 0 {
			// Boolean attribute
			spIdx := strings.IndexAny(s, " \t\n\r")
			if spIdx < 0 {
				attrs[s] = ""
				break
			}
			attrs[s[:spIdx]] = ""
			s = s[spIdx:]
			continue
		}
		name := strings.TrimSpace(s[:eqIdx])
		s = strings.TrimSpace(s[eqIdx+1:])
		if s == "" {
			break
		}
		var val string
		if s[0] == '"' || s[0] == '\'' {
			quote := s[0]
			endQ := strings.IndexByte(s[1:], quote)
			if endQ < 0 {
				val = s[1:]
				s = ""
			} else {
				val = s[1 : endQ+1]
				s = s[endQ+2:]
			}
		} else {
			spIdx := strings.IndexAny(s, " \t\n\r>")
			if spIdx < 0 {
				val = s
				s = ""
			} else {
				val = s[:spIdx]
				s = s[spIdx:]
			}
		}
		attrs[strings.ToLower(name)] = val
	}

	return tag, attrs
}

func isVoidElement(tag string) bool {
	switch tag {
	case "br", "hr", "img", "input", "meta", "link", "area", "base", "col", "embed", "source", "track", "wbr":
		return true
	}
	return false
}

func isSkippedElement(tag string) bool {
	switch tag {
	case "script", "style", "head", "meta", "link", "title", "noscript", "iframe", "object", "svg", "path", "template":
		return true
	}
	return false
}

func isTransparentElement(tag string) bool {
	switch tag {
	case "html", "body", "main", "article", "header", "footer", "section", "nav", "aside", "figure", "figcaption", "details", "summary", "dialog", "form", "fieldset", "legend", "ul", "ol", "li", "dl", "dt", "dd", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "colgroup", "col", "caption":
		return true
	}
	return false
}

func decodeHTMLEntities(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&apos;", "'",
		"&#39;", "'",
		"&nbsp;", " ",
		"&mdash;", "\u2014",
		"&ndash;", "\u2013",
		"&hellip;", "\u2026",
		"&bull;", "\u2022",
		"&copy;", "\u00A9",
		"&reg;", "\u00AE",
		"&trade;", "\u2122",
	)
	return r.Replace(s)
}
