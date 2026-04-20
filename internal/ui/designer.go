package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/Font"
	"graphics.gd/classdb/GUI"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/Node"
	"graphics.gd/classdb/OptionButton"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/ColorPicker"
	"graphics.gd/classdb/StyleBoxFlat"
	"graphics.gd/classdb/SystemFont"
	"graphics.gd/classdb/TextEdit"
	"graphics.gd/classdb/Tree"
	"graphics.gd/classdb/TreeItem"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/VSplitContainer"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Color"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
	"graphics.gd/variant/Vector2i"
)

// DesignMode provides a live layout inspector. Toggle with Cmd+Shift+D.
type DesignMode struct {
	active    bool
	inspector Window.Instance
	propsList VBoxContainer.Instance
	nameLabel Label.Instance
	typeLabel Label.Instance
	pathLabel Label.Instance

	selected   Control.Instance
	highlight  PanelContainer.Instance // blue border on selected
	hoverHL    PanelContainer.Instance // subtle border on hover
	rootNode      Node.Instance          // tree root for hit-testing
	overlayParent Node.Instance         // node to attach overlays to
	scaleFactor   float32               // window content scale factor

	// Node tree view
	nodeTree     Tree.Instance
	treeNodeMap  map[string]Control.Instance // tree item path → control

	// Alt+scroll: all nodes under cursor, from shallowest to deepest
	hoverStack   []Control.Instance
	hoverDepth   int              // current index into hoverStack
	hoverCurrent Control.Instance // the node currently highlighted by hover/scroll

	copySelector   string // formatted selector string for clipboard
	overrides      map[string]map[string]any
	inSelectNode   bool   // guard against re-entrant selectNode calls
}

// IsOverInspector returns true if the screen-space position is inside the inspector window.
func (d *DesignMode) IsOverInspector(screenPos Vector2.XY) bool {
	if !d.active || d.inspector == (Window.Instance{}) {
		return false
	}
	wpos := d.inspector.Position()
	wsize := d.inspector.Size()
	return screenPos.X >= float32(wpos.X) && screenPos.X <= float32(wpos.X+wsize.X) &&
		screenPos.Y >= float32(wpos.Y) && screenPos.Y <= float32(wpos.Y+wsize.Y)
}

func NewDesignMode() *DesignMode {
	return &DesignMode{
		overrides: make(map[string]map[string]any),
	}
}

func (d *DesignMode) Toggle(root Control.Instance) {
	if d.active {
		d.Close()
	} else {
		d.Open(root.AsNode())
	}
}

func (d *DesignMode) ToggleFromWindow(win Window.Instance) {
	if d.active {
		d.Close()
	} else {
		d.Open(win.AsNode())
	}
}

func (d *DesignMode) IsActive() bool { return d.active }

func (d *DesignMode) Open(rootNode Node.Instance) {
	d.active = true
	d.rootNode = rootNode
	d.overlayParent = rootNode
	// Read the actual content scale factor from the window
	if win, ok := Object.As[Window.Instance](rootNode); ok {
		d.scaleFactor = float32(win.ContentScaleFactor())
	} else {
		d.scaleFactor = 1.0
	}
	fmt.Println("[design] content scale factor:", d.scaleFactor)

	d.inspector = Window.New()
	d.inspector.AsNode().SetName("__design_inspector__")
	d.inspector.SetTitle("Layout Inspector")
	d.inspector.SetSize(Vector2i.New(800, 900))
	d.inspector.SetMinSize(Vector2i.New(600, 500))

	bg := PanelContainer.New()
	bg.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	applyPanelBg(bg.AsControl(), colorBgSidebar)

	margin := MarginContainer.New()
	margin.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	margin.AsControl().AddThemeConstantOverride("margin_top", 8)
	margin.AsControl().AddThemeConstantOverride("margin_left", 8)
	margin.AsControl().AddThemeConstantOverride("margin_right", 8)
	margin.AsControl().AddThemeConstantOverride("margin_bottom", 8)

	outerVBox := VBoxContainer.New()
	outerVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	outerVBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	outerVBox.AsControl().AddThemeConstantOverride("separation", 4)

	// ── Node tree ──
	treeLabel := Label.New()
	treeLabel.SetText("NODE TREE")
	treeLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	treeLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)

	d.nodeTree = Tree.New()
	d.nodeTree.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	d.nodeTree.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	d.nodeTree.AsControl().SetSizeFlagsStretchRatio(1)
	d.nodeTree.SetHideRoot(true)
	d.nodeTree.SetColumns(2)
	d.nodeTree.SetColumnTitlesVisible(false)
	d.nodeTree.SetColumnExpand(0, true)
	d.nodeTree.SetColumnExpand(1, false)
	d.nodeTree.SetColumnCustomMinimumWidth(1, 80)
	d.nodeTree.SetColumnTitleAlignment(1, GUI.HorizontalAlignmentRight)
	applySidebarTreeTheme(d.nodeTree.AsControl())
	d.nodeTree.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))

	d.treeNodeMap = make(map[string]Control.Instance)
	d.buildNodeTree()

	// Click tree item → select that node
	d.nodeTree.OnItemSelected(func() {
		sel := d.nodeTree.GetSelected()
		if sel == (TreeItem.Instance{}) {
			return
		}
		path := sel.GetTooltipText(0)
		if ctrl, ok := d.treeNodeMap[path]; ok {
			d.selectNode(ctrl)
		}
	})

	sep1 := PanelContainer.New()
	sep1.AsControl().SetCustomMinimumSize(Vector2.New(0, 1))
	applyPanelBg(sep1.AsControl(), colorBorder)

	// ── Selection info ──
	d.nameLabel = Label.New()
	d.nameLabel.SetText("No selection")
	d.nameLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	d.nameLabel.AsControl().AddThemeColorOverride("font_color", colorText)

	d.typeLabel = Label.New()
	d.typeLabel.SetText("")
	d.typeLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	d.typeLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)

	pathRow := HBoxContainer.New()
	pathRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	pathRow.AsControl().AddThemeConstantOverride("separation", 4)

	d.pathLabel = Label.New()
	d.pathLabel.SetText("")
	d.pathLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	d.pathLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	d.pathLabel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	d.pathLabel.SetAutowrapMode(3)

	copyBtn := Button.New()
	copyBtn.SetText("Copy")
	applyCompactSecondaryButtonTheme(copyBtn.AsControl())
	copyBtn.AsBaseButton().OnPressed(func() {
		if d.copySelector != "" {
			DisplayServer.ClipboardSet(d.copySelector)
		}
	})

	pathRow.AsNode().AddChild(d.pathLabel.AsNode())
	pathRow.AsNode().AddChild(copyBtn.AsNode())

	sep2 := PanelContainer.New()
	sep2.AsControl().SetCustomMinimumSize(Vector2.New(0, 1))
	applyPanelBg(sep2.AsControl(), colorBorder)

	// ── Properties ──
	propsLabel := Label.New()
	propsLabel.SetText("PROPERTIES")
	propsLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	propsLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)

	propsScroll := ScrollContainer.New()
	propsScroll.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	propsScroll.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	propsScroll.AsControl().SetSizeFlagsStretchRatio(1)
	propsScroll.SetHorizontalScrollMode(ScrollContainer.ScrollModeDisabled)

	d.propsList = VBoxContainer.New()
	d.propsList.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	d.propsList.AsControl().AddThemeConstantOverride("separation", 4)
	propsScroll.AsNode().AddChild(d.propsList.AsNode())

	// ── CSS paste area ──
	sep3 := PanelContainer.New()
	sep3.AsControl().SetCustomMinimumSize(Vector2.New(0, 1))
	applyPanelBg(sep3.AsControl(), colorBorder)

	cssLabel := Label.New()
	cssLabel.SetText("CSS")
	cssLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	cssLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)

	cssEdit := TextEdit.New()
	cssEdit.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	cssEdit.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(100)))
	cssEdit.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	cssEdit.SetPlaceholderText("CSS:\n  font-size: 12px;\n  background: #4c8dff;\nHTML:\n  <div style=\"gap: 8px\">\n    <button>Click</button>\n  </div>")
	applyTextEditTheme(cssEdit.AsControl())

	cssApplyBtn := Button.New()
	cssApplyBtn.SetText("Apply")
	applyButtonTheme(cssApplyBtn.AsControl())
	cssApplyBtn.AsBaseButton().OnPressed(func() {
		text := strings.TrimSpace(cssEdit.Text())
		if text == "" {
			return
		}

		// Auto-detect HTML vs CSS
		if strings.Contains(text, "<") {
			// HTML mode — build nodes under selected node (or root)
			parent := d.rootNode
			if d.selected != (Control.Instance{}) {
				parent = d.selected.AsNode()
			}
			created := BuildFromHTML(text, parent)
			// Track all created named nodes for saving
			for _, ctrl := range created {
				d.trackNodeTree(ctrl.AsNode())
			}
			fmt.Printf("[html] Created %d nodes\n", len(created))
			d.rebuildNodeTree()
		} else {
			// CSS mode — apply to selected node
			if d.selected == (Control.Instance{}) {
				return
			}
			style := ParseCSSBlock(text)
			style.ApplyToControl(d.selected)
			path := d.selected.AsNode().GetPath()
			for prop, val := range style.Props {
				d.setOverride(path, prop, val)
			}
			d.buildProps(d.selected, path)
			d.positionOverlay(d.highlight, d.selected)
		}
	})

	resetBtn := Button.New()
	resetBtn.SetText("Reset")
	applySecondaryButtonTheme(resetBtn.AsControl())
	resetBtn.AsBaseButton().OnPressed(func() {
		d.overrides = make(map[string]map[string]any)
		ApplyStylesheetToTree(d.rootNode)
		if d.selected != (Control.Instance{}) {
			d.buildProps(d.selected, d.selected.AsNode().GetPath())
		}
		fmt.Println("[design] Reset unsaved changes")
	})

	saveBtn := Button.New()
	saveBtn.SetText("Save Layout")
	applyButtonTheme(saveBtn.AsControl())
	saveBtn.AsBaseButton().OnPressed(func() { SaveStylesheet(d.rootNode, d.overrides) })

	outerVBox.AsNode().AddChild(treeLabel.AsNode())
	outerVBox.AsNode().AddChild(d.nodeTree.AsNode())
	outerVBox.AsNode().AddChild(sep1.AsNode())
	outerVBox.AsNode().AddChild(d.nameLabel.AsNode())
	outerVBox.AsNode().AddChild(d.typeLabel.AsNode())
	outerVBox.AsNode().AddChild(pathRow.AsNode())
	outerVBox.AsNode().AddChild(sep2.AsNode())
	outerVBox.AsNode().AddChild(propsLabel.AsNode())
	outerVBox.AsNode().AddChild(propsScroll.AsNode())
	outerVBox.AsNode().AddChild(sep3.AsNode())
	outerVBox.AsNode().AddChild(cssLabel.AsNode())
	outerVBox.AsNode().AddChild(cssEdit.AsNode())
	outerVBox.AsNode().AddChild(cssApplyBtn.AsNode())
	outerVBox.AsNode().AddChild(resetBtn.AsNode())
	outerVBox.AsNode().AddChild(saveBtn.AsNode())

	margin.AsNode().AddChild(outerVBox.AsNode())
	bg.AsNode().AddChild(margin.AsNode())
	d.inspector.AsNode().AddChild(bg.AsNode())

	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		tree.Root().AsNode().AddChild(d.inspector.AsNode())
	}
	d.inspector.Show()
	d.inspector.MoveToCenter()

	// Hover overlay (subtle tint)
	d.hoverHL = PanelContainer.New()
	d.hoverHL.AsNode().SetName("__design_hover__")
	hoverStyle := StyleBoxFlat.New()
	hoverStyle.SetBgColor(Color.RGBA{R: 0.298, G: 0.553, B: 1.0, A: 0.1})
	d.hoverHL.AsControl().AddThemeStyleboxOverride("panel", hoverStyle.AsStyleBox())
	d.hoverHL.AsControl().SetMouseFilter(Control.MouseFilterIgnore)
	d.hoverHL.AsCanvasItem().SetVisible(false)
	d.overlayParent.AddChild(d.hoverHL.AsNode())

	// Selection highlight (stronger tint)
	d.highlight = PanelContainer.New()
	d.highlight.AsNode().SetName("__design_highlight__")
	highlightStyle := StyleBoxFlat.New()
	highlightStyle.SetBgColor(Color.RGBA{R: 0.298, G: 0.553, B: 1.0, A: 0.2})
	d.highlight.AsControl().AddThemeStyleboxOverride("panel", highlightStyle.AsStyleBox())
	d.highlight.AsControl().SetMouseFilter(Control.MouseFilterIgnore)
	d.highlight.AsCanvasItem().SetVisible(false)
	d.overlayParent.AddChild(d.highlight.AsNode())

	d.inspector.OnCloseRequested(func() { d.Close() })
	fmt.Println("[design] Inspector opened — click elements to inspect")
}

func (d *DesignMode) Close() {
	d.active = false
	if d.hoverHL != (PanelContainer.Instance{}) {
		d.hoverHL.AsNode().QueueFree()
		d.hoverHL = PanelContainer.Instance{}
	}
	if d.highlight != (PanelContainer.Instance{}) {
		d.highlight.AsNode().QueueFree()
		d.highlight = PanelContainer.Instance{}
	}
	if d.inspector != (Window.Instance{}) {
		d.inspector.AsNode().QueueFree()
		d.inspector = Window.Instance{}
	}
	d.selected = Control.Instance{}
	fmt.Println("[design] Inspector closed")
}

// HandleHover rebuilds the hover stack and highlights the deepest node.
func (d *DesignMode) HandleHover(pos Vector2.XY) {
	if !d.active {
		return
	}
	d.hoverStack = d.hoverStack[:0]
	d.collectHits(d.rootNode, pos)
	d.hoverDepth = len(d.hoverStack) - 1 // default to deepest

	if d.hoverDepth >= 0 {
		d.hoverCurrent = d.hoverStack[d.hoverDepth]
	} else {
		d.hoverCurrent = Control.Instance{}
	}
	d.updateHoverHighlight()
}

// HandleScroll moves up/down the hover stack. dir > 0 = scroll up (toward parent),
// dir < 0 = scroll down (toward child).
func (d *DesignMode) HandleScroll(dir int) {
	if !d.active || len(d.hoverStack) == 0 {
		return
	}
	d.hoverDepth -= dir // scroll up = shallower = lower index
	if d.hoverDepth < 0 {
		d.hoverDepth = 0
	}
	if d.hoverDepth >= len(d.hoverStack) {
		d.hoverDepth = len(d.hoverStack) - 1
	}
	d.hoverCurrent = d.hoverStack[d.hoverDepth]
	d.updateHoverHighlight()
}

// HandleClick selects whatever node the hover highlight is currently on.
func (d *DesignMode) HandleClick(pos Vector2.XY) bool {
	if !d.active {
		return false
	}
	if d.hoverCurrent != (Control.Instance{}) {
		d.selectNode(d.hoverCurrent)
		return true
	}
	return false
}

func (d *DesignMode) positionOverlay(overlay PanelContainer.Instance, ctrl Control.Instance) {
	pos := ctrl.GlobalPosition()
	size := ctrl.Size()
	if size.X <= 0 || size.Y <= 0 {
		overlay.AsCanvasItem().SetVisible(false)
		return
	}
	overlay.AsControl().SetGlobalPosition(Vector2.New(pos.X, pos.Y))
	overlay.AsControl().SetSize(Vector2.New(size.X, size.Y))
}

func (d *DesignMode) updateHoverHighlight() {
	if d.hoverCurrent != (Control.Instance{}) && d.hoverCurrent != d.selected {
		d.positionOverlay(d.hoverHL, d.hoverCurrent)
		d.hoverHL.AsCanvasItem().SetVisible(true)
	} else {
		d.hoverHL.AsCanvasItem().SetVisible(false)
	}
}

// collectHits walks the tree and appends all controls containing pos (shallowest first).
func (d *DesignMode) collectHits(node Node.Instance, pos Vector2.XY) {
	for i := 0; i < int(node.GetChildCount()); i++ {
		child := node.GetChild(i)
		// Skip all designer-owned nodes (overlays, inspector window)
		if strings.HasPrefix(child.Name(), "__design_") {
			continue
		}
		if ctrl, ok := Object.As[Control.Instance](child); ok {
			if !ctrl.AsCanvasItem().Visible() {
				continue
			}
			rect := ctrl.GetGlobalRect()
			if rect.Size.X > 0 && rect.Size.Y > 0 &&
				pos.X >= rect.Position.X && pos.X <= rect.Position.X+rect.Size.X &&
				pos.Y >= rect.Position.Y && pos.Y <= rect.Position.Y+rect.Size.Y {
				d.hoverStack = append(d.hoverStack, ctrl)
			}
			d.collectHits(child, pos)
		} else {
			d.collectHits(child, pos)
		}
	}
}

func (d *DesignMode) selectNode(ctrl Control.Instance) {
	// Guard against re-entry from the tree's OnItemSelected callback
	if d.inSelectNode {
		return
	}
	d.inSelectNode = true
	defer func() { d.inSelectNode = false }()

	d.selected = ctrl

	d.positionOverlay(d.highlight, ctrl)
	d.highlight.AsCanvasItem().SetVisible(true)
	d.hoverHL.AsCanvasItem().SetVisible(false)

	name := ctrl.AsNode().Name()
	path := ctrl.AsNode().GetPath()

	// Highlight in tree view
	d.highlightTreeItem(path)

	typeName := d.detectTypeName(ctrl)
	d.nameLabel.SetText(name)
	d.typeLabel.SetText(typeName)
	d.pathLabel.SetText(path)

	// Update the copy button closure
	d.copySelector = fmt.Sprintf("%s %q @ %s", typeName, name, path)

	d.buildProps(ctrl, path)
}

// buildNodeTree populates the tree view with the UI node hierarchy.
// trackNodeTree marks all named nodes in a subtree for CSS saving.
func (d *DesignMode) trackNodeTree(node Node.Instance) {
	name := node.Name()
	if name != "" && !strings.HasPrefix(name, "@") {
		path := node.GetPath()
		if d.overrides[path] == nil {
			d.overrides[path] = make(map[string]any)
		}
		d.overrides[path]["_html"] = true // marker so save picks it up
	}
	for i := 0; i < node.GetChildCount(); i++ {
		d.trackNodeTree(node.GetChild(i))
	}
}

// rebuildNodeTree refreshes the inspector's node tree view.
func (d *DesignMode) rebuildNodeTree() {
	d.buildNodeTree()
}

func (d *DesignMode) buildNodeTree() {
	d.nodeTree.Clear()
	d.treeNodeMap = make(map[string]Control.Instance)
	root := d.nodeTree.CreateItem()
	d.addNodeToTree(d.rootNode, root, 0)
}

func (d *DesignMode) addNodeToTree(node Node.Instance, parent TreeItem.Instance, depth int) {
	if depth > 20 {
		return // safety limit
	}
	for i := 0; i < int(node.GetChildCount()); i++ {
		child := node.GetChild(i)
		ctrl, isCtrl := Object.As[Control.Instance](child)

		// Detect type name
		typeName := "Node"
		if isCtrl {
			typeName = d.detectTypeName(ctrl)
		}

		name := child.Name()
		if name == "" {
			name = typeName
		}

		// Skip our own overlays
		if isCtrl && (ctrl == d.highlight.AsControl() || ctrl == d.hoverHL.AsControl()) {
			continue
		}

		item := d.nodeTree.MoreArgs().CreateItem(parent, -1)
		item.SetText(0, name)
		item.SetText(1, typeName)

		if isCtrl {
			path := ctrl.AsNode().GetPath()
			item.SetTooltipText(0, path)
			d.treeNodeMap[path] = ctrl
			// Muted type column
			item.SetCustomColor(1, colorTextDim)
		} else {
			item.SetCustomColor(0, colorTextDim)
			item.SetCustomColor(1, colorTextDim)
			item.SetSelectable(0, false)
			item.SetSelectable(1, false)
		}

		// Recurse, but collapse by default if deep
		d.addNodeToTree(child, item, depth+1)
		if depth > 1 {
			item.SetCollapsed(true)
		}
	}
}

// highlightTreeItem finds and selects the tree item matching the given node path.
func (d *DesignMode) highlightTreeItem(path string) {
	d.walkTreeItems(d.nodeTree.GetRoot(), path)
}

func (d *DesignMode) walkTreeItems(item TreeItem.Instance, path string) bool {
	if item == (TreeItem.Instance{}) {
		return false
	}
	if item.GetTooltipText(0) == path {
		item.Select(0)
		// Uncollapse parents so it's visible
		p := item.GetParent()
		for p != (TreeItem.Instance{}) {
			p.SetCollapsed(false)
			p = p.GetParent()
		}
		return true
	}
	child := item.GetFirstChild()
	for child != (TreeItem.Instance{}) {
		if d.walkTreeItems(child, path) {
			return true
		}
		child = child.GetNext()
	}
	return false
}

func (d *DesignMode) detectTypeName(ctrl Control.Instance) string {
	if _, ok := Object.As[PanelContainer.Instance](ctrl); ok {
		return "PanelContainer"
	} else if _, ok := Object.As[MarginContainer.Instance](ctrl); ok {
		return "MarginContainer"
	} else if _, ok := Object.As[ScrollContainer.Instance](ctrl); ok {
		return "ScrollContainer"
	} else if _, ok := Object.As[VBoxContainer.Instance](ctrl); ok {
		return "VBoxContainer"
	} else if _, ok := Object.As[HBoxContainer.Instance](ctrl); ok {
		return "HBoxContainer"
	} else if _, ok := Object.As[HSplitContainer.Instance](ctrl); ok {
		return "HSplitContainer"
	} else if _, ok := Object.As[VSplitContainer.Instance](ctrl); ok {
		return "VSplitContainer"
	} else if _, ok := Object.As[Tree.Instance](ctrl); ok {
		return "Tree"
	} else if _, ok := Object.As[Label.Instance](ctrl); ok {
		return "Label"
	} else if _, ok := Object.As[Button.Instance](ctrl); ok {
		return "Button"
	} else if _, ok := Object.As[LineEdit.Instance](ctrl); ok {
		return "LineEdit"
	}
	return "Control"
}

func (d *DesignMode) buildProps(ctrl Control.Instance, path string) {
	for d.propsList.AsNode().GetChildCount() > 0 {
		child := d.propsList.AsNode().GetChild(0)
		d.propsList.AsNode().RemoveChild(child)
		child.QueueFree()
	}

	// ── TYPOGRAPHY (for Label and Button) ──
	if lbl, ok := Object.As[Label.Instance](ctrl); ok {
		d.addSectionLabel("TYPOGRAPHY")
		d.addFontFamilyProp(ctrl, path)
		fs := float32(ctrl.GetThemeFontSize("font_size"))
		d.addEditableProp("Size", fs, path, "font_size", func(v float64) {
			ctrl.AddThemeFontSizeOverride("font_size", int(v))
		})
		d.addFontWeightProp(ctrl, path)
		d.addColorPropSaved("Color", ctrl.GetThemeColor("font_color"), path, "font_color", func(c Color.RGBA) {
			ctrl.AddThemeColorOverride("font_color", c)
		})
		d.addReadOnlyProp("Align", alignName(lbl.HorizontalAlignment()))
		d.addReadOnlyProp("Text", lbl.Text())
	} else if _, ok := Object.As[Button.Instance](ctrl); ok {
		d.addSectionLabel("TYPOGRAPHY")
		d.addFontFamilyProp(ctrl, path)
		fs := float32(ctrl.GetThemeFontSize("font_size"))
		d.addEditableProp("Size", fs, path, "font_size", func(v float64) {
			ctrl.AddThemeFontSizeOverride("font_size", int(v))
		})
		d.addFontWeightProp(ctrl, path)
		d.addColorPropSaved("Color", ctrl.GetThemeColor("font_color"), path, "font_color", func(c Color.RGBA) {
			ctrl.AddThemeColorOverride("font_color", c)
			ctrl.AddThemeColorOverride("font_hover_color", c)
		})
	}

	// ── SIZE ──
	d.addSectionLabel("SIZE")
	size := ctrl.Size()
	minSize := ctrl.CustomMinimumSize()
	// Width: show actual, edit sets min size
	widthVal := minSize.X
	if widthVal == 0 {
		widthVal = size.X
	}
	d.addEditableProp("Width", widthVal, path, "min_width", func(v float64) {
		ms := ctrl.CustomMinimumSize()
		ctrl.SetCustomMinimumSize(Vector2.New(float32(v), ms.Y))
	})
	// Height: show actual, edit sets min size
	heightVal := minSize.Y
	if heightVal == 0 {
		heightVal = size.Y
	}
	d.addEditableProp("Height", heightVal, path, "min_height", func(v float64) {
		ms := ctrl.CustomMinimumSize()
		ctrl.SetCustomMinimumSize(Vector2.New(ms.X, float32(v)))
	})

	// ── LAYOUT (for containers) ──
	isVBox := false
	isHBox := false
	if _, ok := Object.As[VBoxContainer.Instance](ctrl); ok {
		isVBox = true
	}
	if _, ok := Object.As[HBoxContainer.Instance](ctrl); ok {
		isHBox = true
	}
	if isVBox || isHBox {
		d.addSectionLabel("LAYOUT")
		val := float32(ctrl.GetThemeConstant("separation"))
		d.addEditableProp("Gap", val, path, "separation", func(v float64) {
			ctrl.AddThemeConstantOverride("separation", int(v))
		})
		if isVBox {
			d.addReadOnlyProp("Direction", "column")
		} else {
			d.addReadOnlyProp("Direction", "row")
		}
		// Size flags
		d.addReadOnlyProp("H Flags", sizeFlagName(ctrl.SizeFlagsHorizontal()))
		d.addReadOnlyProp("V Flags", sizeFlagName(ctrl.SizeFlagsVertical()))
	}

	// MarginContainer margins
	if _, ok := Object.As[MarginContainer.Instance](ctrl); ok {
		d.addSectionLabel("MARGINS")
		for _, prop := range []string{"margin_top", "margin_left", "margin_right", "margin_bottom"} {
			p := prop
			val := float32(ctrl.GetThemeConstant(p))
			d.addEditableProp(p, val, path, p, func(v float64) {
				ctrl.AddThemeConstantOverride(p, int(v))
			})
		}
	}

	// SplitContainer offset
	if sc, ok := Object.As[HSplitContainer.Instance](ctrl); ok {
		d.addSectionLabel("SPLIT")
		val := float32(sc.AsSplitContainer().SplitOffset())
		d.addEditableProp("split_offset", val, path, "split_offset", func(v float64) {
			sc.AsSplitContainer().SetSplitOffset(int(v))
		})
	} else if sc, ok := Object.As[VSplitContainer.Instance](ctrl); ok {
		d.addSectionLabel("SPLIT")
		val := float32(sc.AsSplitContainer().SplitOffset())
		d.addEditableProp("split_offset", val, path, "split_offset", func(v float64) {
			sc.AsSplitContainer().SetSplitOffset(int(v))
		})
	}

	// ── BOX (PanelContainer stylebox) ──
	if _, ok := Object.As[PanelContainer.Instance](ctrl); ok {
		if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("panel")); ok {
			d.addSectionLabel("BOX")
			d.addColorPropSaved("Background", sbf.BgColor(), path, "panel_bg_color", func(c Color.RGBA) {
				sbf.SetBgColor(c)
			})
			for _, side := range []struct {
				name   string
				getter func() float32
				setter func(float32)
			}{
				{"Padding Top", sbf.AsStyleBox().ContentMarginTop, func(v float32) { sbf.AsStyleBox().SetContentMarginTop(v) }},
				{"Padding Left", sbf.AsStyleBox().ContentMarginLeft, func(v float32) { sbf.AsStyleBox().SetContentMarginLeft(v) }},
				{"Padding Right", sbf.AsStyleBox().ContentMarginRight, func(v float32) { sbf.AsStyleBox().SetContentMarginRight(v) }},
				{"Padding Bottom", sbf.AsStyleBox().ContentMarginBottom, func(v float32) { sbf.AsStyleBox().SetContentMarginBottom(v) }},
			} {
				s := side
				val := s.getter()
				d.addEditableProp(s.name, val, path, s.name, func(v float64) { s.setter(float32(v)) })
			}
			d.addEditableProp("Border", float32(sbf.BorderWidthTop()), path, "border_width", func(v float64) {
				sbf.SetBorderWidthAll(int(v))
			})
			d.addColorPropSaved("Border Color", sbf.BorderColor(), path, "panel_border_color", func(c Color.RGBA) {
				sbf.SetBorderColor(c)
			})
			d.addEditableProp("Radius", float32(sbf.CornerRadiusTopLeft()), path, "corner_radius", func(v float64) {
				sbf.SetCornerRadiusAll(int(v))
			})
		}
	}

	// ── BOX (Button stylebox) ──
	if _, ok := Object.As[Button.Instance](ctrl); ok {
		if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("normal")); ok {
			d.addSectionLabel("BOX")

			// Background color (with picker)
			d.addColorPropSaved("Background", sbf.BgColor(), path, "btn_bg_color", func(c Color.RGBA) {
				sbf.SetBgColor(c)
				if hoverSb, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("hover")); ok {
					hoverSb.SetBgColor(Color.RGBA{R: c.R * 1.1, G: c.G * 1.1, B: c.B * 1.1, A: c.A})
				}
				if pressedSb, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("pressed")); ok {
					pressedSb.SetBgColor(Color.RGBA{R: c.R * 0.9, G: c.G * 0.9, B: c.B * 0.9, A: c.A})
				}
			})

			d.addColorPropSaved("Font Color", ctrl.GetThemeColor("font_color"), path, "font_color", func(c Color.RGBA) {
				ctrl.AddThemeColorOverride("font_color", c)
				ctrl.AddThemeColorOverride("font_hover_color", c)
			})

			// Padding
			for _, side := range []struct {
				name   string
				getter func() float32
				setter func(float32)
			}{
				{"Padding Top", sbf.AsStyleBox().ContentMarginTop, func(v float32) { sbf.AsStyleBox().SetContentMarginTop(v) }},
				{"Padding Left", sbf.AsStyleBox().ContentMarginLeft, func(v float32) { sbf.AsStyleBox().SetContentMarginLeft(v) }},
				{"Padding Right", sbf.AsStyleBox().ContentMarginRight, func(v float32) { sbf.AsStyleBox().SetContentMarginRight(v) }},
				{"Padding Bottom", sbf.AsStyleBox().ContentMarginBottom, func(v float32) { sbf.AsStyleBox().SetContentMarginBottom(v) }},
			} {
				s := side
				val := s.getter()
				d.addEditableProp(s.name, val, path, s.name, func(v float64) { s.setter(float32(v)) })
			}

			// Border
			d.addEditableProp("Border", float32(sbf.BorderWidthTop()), path, "border_width", func(v float64) {
				sbf.SetBorderWidthAll(int(v))
			})
			d.addColorPropSaved("Border Color", sbf.BorderColor(), path, "btn_border_color", func(c Color.RGBA) {
				sbf.SetBorderColor(c)
			})

			// Radius
			d.addEditableProp("Radius", float32(sbf.CornerRadiusTopLeft()), path, "corner_radius", func(v float64) {
				sbf.SetCornerRadiusAll(int(v))
			})
		}
	}
}

func alignName(a GUI.HorizontalAlignment) string {
	switch a {
	case GUI.HorizontalAlignmentLeft:
		return "left"
	case GUI.HorizontalAlignmentCenter:
		return "center"
	case GUI.HorizontalAlignmentRight:
		return "right"
	default:
		return "fill"
	}
}

func sizeFlagName(f Control.SizeFlags) string {
	switch {
	case f&Control.SizeExpandFill == Control.SizeExpandFill:
		return "expand+fill"
	case f&Control.SizeExpandFill == Control.SizeExpand:
		return "expand"
	case f&Control.SizeShrinkCenter == Control.SizeShrinkCenter:
		return "shrink-center"
	case f&Control.SizeFill == Control.SizeFill:
		return "fill"
	default:
		return fmt.Sprintf("%d", int(f))
	}
}

func (d *DesignMode) addSectionLabel(text string) {
	lbl := Label.New()
	lbl.SetText(text)
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	d.propsList.AsNode().AddChild(lbl.AsNode())
}

func (d *DesignMode) addReadOnlyProp(label string, value string) {
	row := HBoxContainer.New()
	row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	row.AsControl().AddThemeConstantOverride("separation", 8)

	lbl := Label.New()
	lbl.SetText(label)
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	lbl.AsControl().SetCustomMinimumSize(Vector2.New(scaled(110), 0))

	val := Label.New()
	val.SetText(value)
	val.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	val.AsControl().AddThemeColorOverride("font_color", colorText)

	row.AsNode().AddChild(lbl.AsNode())
	row.AsNode().AddChild(val.AsNode())
	d.propsList.AsNode().AddChild(row.AsNode())
}

func (d *DesignMode) addEditableProp(label string, value float32, nodePath string, propName string, apply func(float64)) {
	row := HBoxContainer.New()
	row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	row.AsControl().AddThemeConstantOverride("separation", 4)
	row.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	lbl := Label.New()
	lbl.SetText(label)
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	lbl.AsControl().SetCustomMinimumSize(Vector2.New(scaled(110), 0))

	input := LineEdit.New()
	input.SetText(fmt.Sprintf("%.0f", value))
	input.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	input.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applyInputTheme(input.AsControl())

	minus := Button.New()
	minus.SetText("-")
	minus.AsControl().SetCustomMinimumSize(Vector2.New(scaled(22), 0))
	applyCompactSecondaryButtonTheme(minus.AsControl())

	plus := Button.New()
	plus.SetText("+")
	plus.AsControl().SetCustomMinimumSize(Vector2.New(scaled(22), 0))
	applyCompactSecondaryButtonTheme(plus.AsControl())

	pathRef := nodePath
	propRef := propName
	inputRef := input

	refreshHighlight := func() {
		if d.selected != (Control.Instance{}) {
			d.positionOverlay(d.highlight, d.selected)
		}
	}

	input.OnTextSubmitted(func(_ string) {
		v, err := strconv.ParseFloat(inputRef.Text(), 64)
		if err != nil {
			return
		}
		apply(v)
		d.setOverride(pathRef, propRef, v)
		refreshHighlight()
	})

	minus.AsBaseButton().OnPressed(func() {
		v, _ := strconv.ParseFloat(inputRef.Text(), 64)
		v--
		if v < 0 {
			v = 0
		}
		inputRef.SetText(fmt.Sprintf("%.0f", v))
		apply(v)
		d.setOverride(pathRef, propRef, v)
		refreshHighlight()
	})

	plus.AsBaseButton().OnPressed(func() {
		v, _ := strconv.ParseFloat(inputRef.Text(), 64)
		v++
		inputRef.SetText(fmt.Sprintf("%.0f", v))
		apply(v)
		d.setOverride(pathRef, propRef, v)
		refreshHighlight()
	})

	row.AsNode().AddChild(lbl.AsNode())
	row.AsNode().AddChild(minus.AsNode())
	row.AsNode().AddChild(input.AsNode())
	row.AsNode().AddChild(plus.AsNode())
	d.propsList.AsNode().AddChild(row.AsNode())
}

func (d *DesignMode) addColorProp(label string, current Color.RGBA, apply func(Color.RGBA)) {
	d.addColorPropSaved(label, current, "", "", apply)
}

func (d *DesignMode) addColorPropSaved(label string, current Color.RGBA, nodePath string, propName string, apply func(Color.RGBA)) {
	row := HBoxContainer.New()
	row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	row.AsControl().AddThemeConstantOverride("separation", 4)
	row.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	lbl := Label.New()
	lbl.SetText(label)
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	lbl.AsControl().SetCustomMinimumSize(Vector2.New(scaled(80), 0))

	swatch := PanelContainer.New()
	swatch.AsControl().SetCustomMinimumSize(Vector2.New(scaled(22), scaled(22)))
	swatchStyle := StyleBoxFlat.New()
	swatchStyle.SetBgColor(current)
	swatchStyle.SetCornerRadiusAll(3)
	swatchStyle.SetBorderWidthAll(1)
	swatchStyle.SetBorderColor(colorBorder)
	swatch.AsControl().AddThemeStyleboxOverride("panel", swatchStyle.AsStyleBox())

	hexStr := fmt.Sprintf("#%02x%02x%02x", int(current.R*255), int(current.G*255), int(current.B*255))
	hexInput := LineEdit.New()
	hexInput.SetText(hexStr)
	hexInput.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	hexInput.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applyInputTheme(hexInput.AsControl())

	swatchRef := swatchStyle
	pathRef := nodePath
	propRef := propName
	saveColor := func(c Color.RGBA) {
		if pathRef != "" && propRef != "" {
			d.setOverride(pathRef, propRef, fmt.Sprintf("#%02x%02x%02x", int(c.R*255), int(c.G*255), int(c.B*255)))
		}
	}

	hexInput.OnTextSubmitted(func(text string) {
		c := parseHexColor(text)
		apply(c)
		swatchRef.SetBgColor(c)
		saveColor(c)
	})

	pickerBtn := Button.New()
	pickerBtn.SetText("◉")
	applyCompactSecondaryButtonTheme(pickerBtn.AsControl())

	picker := ColorPicker.New()
	picker.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	picker.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(200)))
	picker.SetColor(current)
	picker.AsCanvasItem().SetVisible(false)

	hexInputRef := hexInput
	picker.OnColorChanged(func(c Color.RGBA) {
		apply(c)
		swatchRef.SetBgColor(c)
		hexInputRef.SetText(fmt.Sprintf("#%02x%02x%02x", int(c.R*255), int(c.G*255), int(c.B*255)))
		saveColor(c)
	})

	pickerRef := picker
	pickerBtn.AsBaseButton().OnPressed(func() {
		vis := pickerRef.AsCanvasItem().Visible()
		pickerRef.AsCanvasItem().SetVisible(!vis)
	})

	row.AsNode().AddChild(lbl.AsNode())
	row.AsNode().AddChild(swatch.AsNode())
	row.AsNode().AddChild(hexInput.AsNode())
	row.AsNode().AddChild(pickerBtn.AsNode())
	d.propsList.AsNode().AddChild(row.AsNode())
	d.propsList.AsNode().AddChild(picker.AsNode())
}

// System font families available in the dropdown.
var systemFonts = []struct {
	label string
	names []string
}{
	{"-apple-system", []string{"-apple-system", "BlinkMacSystemFont", "Segoe UI", "sans-serif"}},
	{"SF Pro", []string{"SF Pro Text", "SF Pro", "-apple-system", "sans-serif"}},
	{"SF Mono", []string{"SF Mono", "Menlo", "monospace"}},
	{"Helvetica Neue", []string{"Helvetica Neue", "Helvetica", "sans-serif"}},
	{"Inter", []string{"Inter", "-apple-system", "sans-serif"}},
	{"Menlo", []string{"Menlo", "SF Mono", "monospace"}},
	{"Monaco", []string{"Monaco", "Menlo", "monospace"}},
	{"Courier New", []string{"Courier New", "Courier", "monospace"}},
	{"Georgia", []string{"Georgia", "serif"}},
	{"Times New Roman", []string{"Times New Roman", "Times", "serif"}},
	{"Arial", []string{"Arial", "Helvetica", "sans-serif"}},
	{"Verdana", []string{"Verdana", "Geneva", "sans-serif"}},
	{"Trebuchet MS", []string{"Trebuchet MS", "sans-serif"}},
	{"Futura", []string{"Futura", "sans-serif"}},
	{"Avenir", []string{"Avenir", "Avenir Next", "sans-serif"}},
	{"JetBrains Mono", []string{"JetBrains Mono", "SF Mono", "monospace"}},
	{"Fira Code", []string{"Fira Code", "monospace"}},
}

func (d *DesignMode) addFontFamilyProp(ctrl Control.Instance, path string) {
	// Detect current font family
	currentIdx := 0
	font := ctrl.GetThemeFont("font")
	if font != (Font.Instance{}) {
		if sf, ok := Object.As[SystemFont.Instance](font); ok {
			names := sf.FontNames()
			if len(names) > 0 {
				for i, f := range systemFonts {
					if len(f.names) > 0 && f.names[0] == names[0] {
						currentIdx = i
						break
					}
				}
			}
		}
	}

	row := HBoxContainer.New()
	row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	row.AsControl().AddThemeConstantOverride("separation", 4)
	row.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	lbl := Label.New()
	lbl.SetText("Font")
	lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	lbl.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	lbl.AsControl().SetCustomMinimumSize(Vector2.New(scaled(80), 0))

	dropdown := OptionButton.New()
	dropdown.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	dropdown.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	applySecondaryButtonTheme(dropdown.AsControl())

	for _, f := range systemFonts {
		dropdown.AddItem(f.label)
	}
	dropdown.AsOptionButton().Select(currentIdx)

	ctrlRef := ctrl
	pathRef := path
	dropdown.OnItemSelected(func(index int) {
		if index < 0 || index >= len(systemFonts) {
			return
		}
		sf := SystemFont.New()
		sf.SetFontNames(systemFonts[index].names)
		existingFont := ctrlRef.GetThemeFont("font")
		if existingFont != (Font.Instance{}) {
			w := existingFont.GetFontWeight()
			if w > 0 {
				sf.SetFontWeight(w)
			}
		}
		ctrlRef.AddThemeFontOverride("font", sf.AsFont())
		d.setOverride(pathRef, "font_family", systemFonts[index].label)
	})

	row.AsNode().AddChild(lbl.AsNode())
	row.AsNode().AddChild(dropdown.AsNode())
	d.propsList.AsNode().AddChild(row.AsNode())
}

func (d *DesignMode) addFontWeightProp(ctrl Control.Instance, path string) {
	// Get current weight from the font
	font := ctrl.GetThemeFont("font")
	weight := float32(400)
	if font != (Font.Instance{}) {
		w := font.GetFontWeight()
		if w > 0 {
			weight = float32(w)
		}
	}

	d.addEditableProp("Weight", weight, path, "font_weight", func(v float64) {
		// Create a SystemFont with the requested weight and override
		sf := SystemFont.New()
		sf.SetFontNames([]string{"-apple-system", "SF Pro Text", "Helvetica Neue", "sans-serif"})
		sf.SetFontWeight(int(v))
		ctrl.AddThemeFontOverride("font", sf.AsFont())
	})
}

// toFloat converts a JSON-decoded value to float64.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// getNodeStyleBox returns the StyleBoxFlat for a node (panel or normal).
func (d *DesignMode) getNodeStyleBox(ctrl Control.Instance) (StyleBoxFlat.Instance, bool) {
	if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("panel")); ok {
		return sbf, true
	}
	if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("normal")); ok {
		return sbf, true
	}
	return StyleBoxFlat.Instance{}, false
}

// NodeInfo holds coordinate data for a control, returned by the control API.
type NodeInfo struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Path         string  `json:"path"`
	GlobalPosX   float32 `json:"globalPosX"`
	GlobalPosY   float32 `json:"globalPosY"`
	SizeW        float32 `json:"sizeW"`
	SizeH        float32 `json:"sizeH"`
	RectPosX     float32 `json:"rectPosX"`
	RectPosY     float32 `json:"rectPosY"`
	RectSizeW    float32 `json:"rectSizeW"`
	RectSizeH    float32 `json:"rectSizeH"`
	HoverCount   int     `json:"hoverCount,omitempty"`
	// Highlight overlay actual position/size (what the user sees)
	HighlightX       float32 `json:"highlightX"`
	HighlightY       float32 `json:"highlightY"`
	HighlightW       float32 `json:"highlightW"`
	HighlightH       float32 `json:"highlightH"`
	HighlightVisible bool    `json:"highlightVisible"`
}

// SelectedInfo returns coordinate info for the currently selected node,
// including the highlight overlay's actual rect.
func (d *DesignMode) SelectedInfo() *NodeInfo {
	if !d.active || d.selected == (Control.Instance{}) {
		return nil
	}
	info := d.nodeInfo(d.selected)
	if d.highlight != (PanelContainer.Instance{}) {
		hpos := d.highlight.AsControl().GlobalPosition()
		hsz := d.highlight.AsControl().Size()
		info.HighlightX = hpos.X
		info.HighlightY = hpos.Y
		info.HighlightW = hsz.X
		info.HighlightH = hsz.Y
		info.HighlightVisible = d.highlight.AsCanvasItem().Visible()
	}
	return info
}

func (d *DesignMode) nodeInfo(ctrl Control.Instance) *NodeInfo {
	gpos := ctrl.GlobalPosition()
	size := ctrl.Size()
	rect := ctrl.GetGlobalRect()
	return &NodeInfo{
		Name:       ctrl.AsNode().Name(),
		Type:       d.detectTypeName(ctrl),
		Path:       ctrl.AsNode().GetPath(),
		GlobalPosX: gpos.X,
		GlobalPosY: gpos.Y,
		SizeW:      size.X,
		SizeH:      size.Y,
		RectPosX:   rect.Position.X,
		RectPosY:   rect.Position.Y,
		RectSizeW:  rect.Size.X,
		RectSizeH:  rect.Size.Y,
	}
}

// FindAndSelect walks the tree to find a node by name and selects it.
// Returns the node info, or nil if not found.
func (d *DesignMode) FindAndSelect(name string) *NodeInfo {
	if !d.active {
		return nil
	}
	ctrl := d.findByName(d.rootNode, name)
	if ctrl == (Control.Instance{}) {
		return nil
	}
	d.selectNode(ctrl)
	// Build info from ctrl directly (d.selected may have been changed
	// by the tree's OnItemSelected callback re-entering selectNode).
	info := d.nodeInfo(ctrl)
	if d.highlight != (PanelContainer.Instance{}) {
		hpos := d.highlight.AsControl().GlobalPosition()
		hsz := d.highlight.AsControl().Size()
		info.HighlightX = hpos.X
		info.HighlightY = hpos.Y
		info.HighlightW = hsz.X
		info.HighlightH = hsz.Y
		info.HighlightVisible = d.highlight.AsCanvasItem().Visible()
	}
	return info
}

func (d *DesignMode) findByName(node Node.Instance, name string) Control.Instance {
	for i := 0; i < int(node.GetChildCount()); i++ {
		child := node.GetChild(i)
		if child.Name() == name {
			if ctrl, ok := Object.As[Control.Instance](child); ok {
				return ctrl
			}
		}
		if found := d.findByName(child, name); found != (Control.Instance{}) {
			return found
		}
	}
	return Control.Instance{}
}

// ClickAt performs a hover + click at the given position and returns info about the selected node.
func (d *DesignMode) ClickAt(x, y float32) *NodeInfo {
	if !d.active {
		return nil
	}
	pos := Vector2.New(x, y)
	d.HandleHover(pos)
	d.HandleClick(pos)
	info := d.SelectedInfo()
	if info != nil {
		info.HoverCount = len(d.hoverStack)
	}
	return info
}

func parseHexColor(hex string) Color.RGBA {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return Color.RGBA{R: 1, G: 1, B: 1, A: 1}
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return Color.RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: 1}
}

func (d *DesignMode) setOverride(path, prop string, value any) {
	if d.overrides[path] == nil {
		d.overrides[path] = make(map[string]any)
	}
	d.overrides[path][prop] = value
}

func layoutConfigPath() string {
	return "layout.json"
}

func (d *DesignMode) SaveOverrides() {
	path := layoutConfigPath()
	data, err := json.MarshalIndent(d.overrides, "", "  ")
	if err != nil {
		fmt.Println("[design] save error:", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Println("[design] write error:", err)
		return
	}
	fmt.Println("[design] Layout saved to", path)
}

func (d *DesignMode) LoadOverrides() {
	path := layoutConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("[design] load error:", err)
		return
	}
	if err := json.Unmarshal(data, &d.overrides); err != nil {
		fmt.Println("[design] parse error:", err)
	}
}

// ApplyOverrides walks the tree and applies any saved layout overrides.
func (d *DesignMode) ApplyOverrides(root Node.Instance) {
	d.LoadOverrides()
	if len(d.overrides) == 0 {
		return
	}
	d.applyToNode(root)
}

func (d *DesignMode) applyToNode(node Node.Instance) {
	path := node.GetPath()
	if props, ok := d.overrides[path]; ok {
		if ctrl, ok := Object.As[Control.Instance](node); ok {
			for prop, rawVal := range props {
				switch prop {
				case "min_width":
					ms := ctrl.CustomMinimumSize()
					ctrl.SetCustomMinimumSize(Vector2.New(float32(toFloat(rawVal)), ms.Y))
				case "min_height":
					ms := ctrl.CustomMinimumSize()
					ctrl.SetCustomMinimumSize(Vector2.New(ms.X, float32(toFloat(rawVal))))
				case "margin_top", "margin_left", "margin_right", "margin_bottom", "separation":
					ctrl.AddThemeConstantOverride(prop, int(toFloat(rawVal)))
				case "font_size":
					ctrl.AddThemeFontSizeOverride("font_size", int(toFloat(rawVal)))
				case "font_weight":
					sf := SystemFont.New()
					sf.SetFontNames([]string{"-apple-system", "BlinkMacSystemFont", "sans-serif"})
					sf.SetFontWeight(int(toFloat(rawVal)))
					// Check if there's also a font_family override to use
					if famVal, ok := props["font_family"]; ok {
						if famStr, ok := famVal.(string); ok {
							for _, f := range systemFonts {
								if f.label == famStr {
									sf.SetFontNames(f.names)
									break
								}
							}
						}
					}
					ctrl.AddThemeFontOverride("font", sf.AsFont())
				case "font_family":
					// Handled together with font_weight above; apply standalone if no weight
					if _, hasWeight := props["font_weight"]; !hasWeight {
						if famStr, ok := rawVal.(string); ok {
							for _, f := range systemFonts {
								if f.label == famStr {
									sf := SystemFont.New()
									sf.SetFontNames(f.names)
									ctrl.AddThemeFontOverride("font", sf.AsFont())
									break
								}
							}
						}
					}
				case "font_color":
					if hexStr, ok := rawVal.(string); ok {
						c := parseHexColor(hexStr)
						ctrl.AddThemeColorOverride("font_color", c)
						ctrl.AddThemeColorOverride("font_hover_color", c)
					}
				case "split_offset":
					if sc, ok := Object.As[HSplitContainer.Instance](node); ok {
						sc.AsSplitContainer().SetSplitOffset(int(toFloat(rawVal)))
					} else if sc, ok := Object.As[VSplitContainer.Instance](node); ok {
						sc.AsSplitContainer().SetSplitOffset(int(toFloat(rawVal)))
					}
				case "border_width":
					if sbf, ok := d.getNodeStyleBox(ctrl); ok {
						sbf.SetBorderWidthAll(int(toFloat(rawVal)))
					}
				case "corner_radius":
					if sbf, ok := d.getNodeStyleBox(ctrl); ok {
						sbf.SetCornerRadiusAll(int(toFloat(rawVal)))
					}
				case "panel_bg_color":
					if hexStr, ok := rawVal.(string); ok {
						if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("panel")); ok {
							sbf.SetBgColor(parseHexColor(hexStr))
						}
					}
				case "panel_border_color":
					if hexStr, ok := rawVal.(string); ok {
						if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("panel")); ok {
							sbf.SetBorderColor(parseHexColor(hexStr))
						}
					}
				case "btn_bg_color":
					if hexStr, ok := rawVal.(string); ok {
						c := parseHexColor(hexStr)
						if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("normal")); ok {
							sbf.SetBgColor(c)
						}
					}
				case "btn_border_color":
					if hexStr, ok := rawVal.(string); ok {
						c := parseHexColor(hexStr)
						if sbf, ok := Object.As[StyleBoxFlat.Instance](ctrl.GetThemeStylebox("normal")); ok {
							sbf.SetBorderColor(c)
						}
					}
				case "Padding Top", "Padding Left", "Padding Right", "Padding Bottom":
					if sbf, ok := d.getNodeStyleBox(ctrl); ok {
						v := float32(toFloat(rawVal))
						switch prop {
						case "Padding Top":
							sbf.AsStyleBox().SetContentMarginTop(v)
						case "Padding Left":
							sbf.AsStyleBox().SetContentMarginLeft(v)
						case "Padding Right":
							sbf.AsStyleBox().SetContentMarginRight(v)
						case "Padding Bottom":
							sbf.AsStyleBox().SetContentMarginBottom(v)
						}
					}
				}
			}
		}
	}
	for i := 0; i < int(node.GetChildCount()); i++ {
		d.applyToNode(node.GetChild(i))
	}
}
