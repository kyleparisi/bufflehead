package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"parquet-viewer/internal/control"
	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"

	"graphics.gd/classdb"
	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CheckBox"
	"graphics.gd/classdb/CodeEdit"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventKey"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/Tree"
	"graphics.gd/classdb/TreeItem"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
	"graphics.gd/variant/Vector2i"
)

// ── Title bar ──────────────────────────────────────────────────────────────

type TitleBar struct {
	PanelContainer.Extension[TitleBar] `gd:"ParquetTitleBar"`

	infoLabel  Label.Instance
	NavBackBtn Button.Instance
	NavFwdBtn  Button.Instance
}

func (t *TitleBar) GuiInput(event InputEvent.Instance) {
	if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
		if mb.ButtonIndex() == Input.MouseButtonLeft && mb.AsInputEvent().IsPressed() {
			DisplayServer.WindowStartDrag(0)
		}
	}
}

func (t *TitleBar) Ready() {
	applyTitleBarTheme(t.AsControl())
	t.AsControl().SetMouseFilter(Control.MouseFilterStop)
	t.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	margin := MarginContainer.New()
	margin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	margin.AsControl().AddThemeConstantOverride("margin_top", 6)
	margin.AsControl().AddThemeConstantOverride("margin_left", 78) // clear macOS traffic lights
	margin.AsControl().AddThemeConstantOverride("margin_right", 8)
	margin.AsControl().AddThemeConstantOverride("margin_bottom", 6)

	row := HBoxContainer.New()
	row.AsControl().AddThemeConstantOverride("separation", 6)

	// Nav buttons
	t.NavBackBtn = Button.New()
	t.NavBackBtn.SetText("◀")
	t.NavBackBtn.AsControl().AddThemeFontSizeOverride("font_size", 11)
	t.NavBackBtn.AsControl().SetCustomMinimumSize(Vector2.New(24, 0))
	applySecondaryButtonTheme(t.NavBackBtn.AsControl())
	t.NavBackBtn.AsBaseButton().SetDisabled(true)
	t.NavBackBtn.AsControl().SetMouseFilter(Control.MouseFilterStop)

	t.NavFwdBtn = Button.New()
	t.NavFwdBtn.SetText("▶")
	t.NavFwdBtn.AsControl().AddThemeFontSizeOverride("font_size", 11)
	t.NavFwdBtn.AsControl().SetCustomMinimumSize(Vector2.New(24, 0))
	applySecondaryButtonTheme(t.NavFwdBtn.AsControl())
	t.NavFwdBtn.AsBaseButton().SetDisabled(true)
	t.NavFwdBtn.AsControl().SetMouseFilter(Control.MouseFilterStop)

	// Spacer after nav buttons
	leftSpacer := Control.New()
	leftSpacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)
	leftSpacer.AsControl().SetSizeFlagsStretchRatio(1)

	// Connection info pill (centered, 50%)
	pill := PanelContainer.New()
	applyPillTheme(pill.AsControl())
	pill.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	pill.AsControl().AsControl().SetSizeFlagsStretchRatio(2)
	t.infoLabel = Label.New()
	t.infoLabel.SetText("DuckDB  ·  In-Memory  ·  No file loaded")
	t.infoLabel.AsControl().AddThemeColorOverride("font_color", colorText)
	t.infoLabel.AsControl().AddThemeFontSizeOverride("font_size", 13)
	t.infoLabel.SetHorizontalAlignment(1) // center
	pill.AsNode().AddChild(t.infoLabel.AsNode())

	// Right spacer (25%)
	rightSpacer := Control.New()
	rightSpacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)
	rightSpacer.AsControl().SetSizeFlagsStretchRatio(1)

	// Let all children pass mouse events through to the title bar for dragging
	margin.AsControl().SetMouseFilter(Control.MouseFilterPass)
	row.AsControl().SetMouseFilter(Control.MouseFilterPass)
	leftSpacer.SetMouseFilter(Control.MouseFilterPass)
	pill.AsControl().SetMouseFilter(Control.MouseFilterPass)
	t.infoLabel.AsControl().SetMouseFilter(Control.MouseFilterPass)
	rightSpacer.SetMouseFilter(Control.MouseFilterPass)

	row.AsNode().AddChild(t.NavBackBtn.AsNode())
	row.AsNode().AddChild(t.NavFwdBtn.AsNode())
	row.AsNode().AddChild(leftSpacer.AsNode())
	row.AsNode().AddChild(pill.AsNode())
	row.AsNode().AddChild(rightSpacer.AsNode())

	margin.AsNode().AddChild(row.AsNode())
	t.AsNode().AddChild(margin.AsNode())
}

func (t *TitleBar) SetFileInfo(path string) {
	if path == "" {
		t.infoLabel.SetText("DuckDB  ·  In-Memory")
		return
	}
	// Extract filename
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			name = path[i+1:]
			break
		}
	}
	// Detect type
	ext := ""
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		ext = strings.ToLower(name[dot:])
	}
	switch ext {
	case ".duckdb", ".db", ".ddb":
		t.infoLabel.SetText("DuckDB  ·  " + name)
	case ".parquet":
		t.infoLabel.SetText("DuckDB  ·  In-Memory  ·  " + name)
	case ".csv":
		t.infoLabel.SetText("DuckDB  ·  CSV  ·  " + name)
	case ".json":
		t.infoLabel.SetText("DuckDB  ·  JSON  ·  " + name)
	case ".tsv":
		t.infoLabel.SetText("DuckDB  ·  TSV  ·  " + name)
	default:
		t.infoLabel.SetText("DuckDB  ·  " + name)
	}
}

// ── Toolbar ────────────────────────────────────────────────────────────────

type Toolbar struct {
	HBoxContainer.Extension[Toolbar] `gd:"ParquetToolbar"`

	fileLabel LineEdit.Instance

	OnFileOpened func(path string)
}

func (t *Toolbar) Ready() {
	t.AsControl().AddThemeConstantOverride("separation", 6)

	openBtn := Button.New()
	openBtn.SetText("Open…")
	applyButtonTheme(openBtn.AsControl())
	openBtn.AsBaseButton().OnPressed(func() {
		DisplayServer.FileDialogShow(
			"Open Parquet File",
			"",
			"",
			false,
			DisplayServer.FileDialogModeOpenFile,
			[]string{"*.parquet ; Parquet Files"},
			func(status bool, selectedPaths []string, selectedFilterIndex int) {
				if status && len(selectedPaths) > 0 {
					path := selectedPaths[0]
					t.fileLabel.SetText(path)
					if t.OnFileOpened != nil {
						t.OnFileOpened(path)
					}
				}
			},
			0,
		)
	})

	t.fileLabel = LineEdit.New()
	t.fileLabel.SetPlaceholderText("No file loaded")
	t.fileLabel.SetEditable(false)
	t.fileLabel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyInputTheme(t.fileLabel.AsControl())

	t.AsNode().AddChild(openBtn.AsNode())
	t.AsNode().AddChild(t.fileLabel.AsNode())
}

// ── Schema sidebar ─────────────────────────────────────────────────────────

type SchemaPanel struct {
	VBoxContainer.Extension[SchemaPanel] `gd:"ParquetSchemaPanel"`

	searchBox        LineEdit.Instance
	tree             Tree.Instance
	OnTableClicked   func(tableName string)
	OnColumnsChanged func(selected []string)
	allCols          []db.Column
	allTables        []db.TableInfo
	checkMode        bool
	checkBoxes       []CheckBox.Instance
	checkRows        []HBoxContainer.Instance
}

func (s *SchemaPanel) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 4)

	// Search input
	s.searchBox = LineEdit.New()
	s.searchBox.SetPlaceholderText("Search items…")
	s.searchBox.AsControl().AddThemeFontSizeOverride("font_size", 12)
	applyInputTheme(s.searchBox.AsControl())
	s.searchBox.OnTextChanged(func(text string) {
		if len(s.allTables) > 0 {
			s.filterTables(text)
		} else {
			s.filterCols(text)
		}
	})

	s.tree = Tree.New()
	s.tree.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	s.tree.SetHideRoot(true)
	applySidebarTreeTheme(s.tree.AsControl())

	s.tree.OnItemActivated(func() {
		selected := s.tree.GetSelected()
		if selected == (TreeItem.Instance{}) || s.OnTableClicked == nil {
			return
		}
		tableName := selected.GetTooltipText(0)
		if tableName != "" {
			s.OnTableClicked(tableName)
		}
	})

	s.AsNode().AddChild(s.searchBox.AsNode())
	s.AsNode().AddChild(s.tree.AsNode())
}

func (s *SchemaPanel) SetSchema(cols []db.Column) {
	s.allCols = cols
	s.allTables = nil
	s.checkMode = true
	s.searchBox.SetText("")
	s.tree.AsCanvasItem().SetVisible(false)
	s.filterCols("")
}

// SetCheckedColumns updates checkbox state from external source (e.g. state restore).
func (s *SchemaPanel) SetCheckedColumns(selected []string) {
	if !s.checkMode {
		return
	}
	selSet := make(map[string]bool, len(selected))
	for _, c := range selected {
		selSet[c] = true
	}
	for _, cb := range s.checkBoxes {
		name := cb.AsControl().GetTooltip()
		checked := len(selected) == 0 || selSet[name]
		cb.AsBaseButton().SetButtonPressed(checked)
	}
}

func (s *SchemaPanel) selectOnly(target CheckBox.Instance) {
	for _, cb := range s.checkBoxes {
		cb.AsBaseButton().SetButtonPressed(false)
	}
	target.AsBaseButton().SetButtonPressed(true)
	if s.OnColumnsChanged != nil {
		s.OnColumnsChanged(s.getCheckedColumns())
	}
}

func (s *SchemaPanel) getCheckedColumns() []string {
	var cols []string
	for _, cb := range s.checkBoxes {
		if cb.AsBaseButton().ButtonPressed() {
			cols = append(cols, cb.AsControl().GetTooltip())
		}
	}
	return cols
}

func (s *SchemaPanel) filterCols(query string) {
	q := strings.ToLower(query)
	// Clear existing rows
	for _, row := range s.checkRows {
		s.Super().AsNode().RemoveChild(row.AsNode())
		row.AsNode().QueueFree()
	}
	s.checkBoxes = nil
	s.checkRows = nil

	for _, col := range s.allCols {
		if q != "" && !strings.Contains(strings.ToLower(col.Name), q) {
			continue
		}
		typeSuffix := col.DataType
		if col.Nullable {
			typeSuffix += "?"
		}
		row := HBoxContainer.New()
		row.AsControl().AddThemeConstantOverride("separation", 4)
		row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		row.AsControl().SetMouseFilter(Control.MouseFilterStop)

		cb := CheckBox.New()
		cb.AsBaseButton().SetButtonPressed(true)
		cb.AsControl().AddThemeFontSizeOverride("font_size", 12)
		cb.AsControl().SetTooltipText(col.Name)
		cb.AsBaseButton().OnToggled(func(pressed bool) {
			if s.OnColumnsChanged != nil {
				s.OnColumnsChanged(s.getCheckedColumns())
			}
		})

		nameLabel := Label.New()
		nameLabel.SetText(col.Name)
		nameLabel.AsControl().AddThemeFontSizeOverride("font_size", 12)
		nameLabel.AsControl().AddThemeColorOverride("font_color", colorText)

		typeLabel := Label.New()
		typeLabel.SetText(typeSuffix)
		typeLabel.AsControl().AddThemeFontSizeOverride("font_size", 10)
		typeLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)

		// Spacer pushes "only" to the right
		spacer := Control.New()
		spacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)
		spacer.SetMouseFilter(Control.MouseFilterIgnore)

		// "only" button — hidden until hover
		onlyBtn := Button.New()
		onlyBtn.SetText("only")
		onlyBtn.AsControl().AddThemeFontSizeOverride("font_size", 10)
		onlyBtn.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
		applySecondaryButtonTheme(onlyBtn.AsControl())
		onlyBtn.AsCanvasItem().SetVisible(false)
		onlyBtn.AsControl().SetMouseFilter(Control.MouseFilterStop)

		// Capture index for "only" click
		cbRef := cb
		onlyBtn.AsBaseButton().OnPressed(func() {
			s.selectOnly(cbRef)
		})

		// Hover: show "only" + highlight row
		hoverBg := makeStyleBox(colorSelected, 0, 0, colorSelected)
		normalBg := makeStyleBox(colorBgSidebar, 0, 0, colorBgSidebar)
		row.AsControl().AddThemeStyleboxOverride("panel", normalBg.AsStyleBox())

		onlyBtnRef := onlyBtn
		rowRef := row
		row.AsControl().OnMouseEntered(func() {
			onlyBtnRef.AsCanvasItem().SetVisible(true)
			rowRef.AsControl().AddThemeStyleboxOverride("panel", hoverBg.AsStyleBox())
		})
		row.AsControl().OnMouseExited(func() {
			onlyBtnRef.AsCanvasItem().SetVisible(false)
			rowRef.AsControl().AddThemeStyleboxOverride("panel", normalBg.AsStyleBox())
		})

		row.AsNode().AddChild(cb.AsNode())
		row.AsNode().AddChild(nameLabel.AsNode())
		row.AsNode().AddChild(typeLabel.AsNode())
		row.AsNode().AddChild(spacer.AsNode())
		row.AsNode().AddChild(onlyBtn.AsNode())
		s.Super().AsNode().AddChild(row.AsNode())
		s.checkBoxes = append(s.checkBoxes, cb)
		s.checkRows = append(s.checkRows, row)
	}
}

func (s *SchemaPanel) SetTables(tables []db.TableInfo) {
	s.allTables = tables
	s.allCols = nil
	s.checkMode = false
	s.searchBox.SetText("")
	// Clear checkbox rows
	for _, row := range s.checkRows {
		s.Super().AsNode().RemoveChild(row.AsNode())
		row.AsNode().QueueFree()
	}
	s.checkBoxes = nil
	s.checkRows = nil
	s.tree.AsCanvasItem().SetVisible(true)
	s.filterTables("")
}

func (s *SchemaPanel) filterTables(query string) {
	q := strings.ToLower(query)
	s.tree.Clear()
	s.tree.SetColumns(2)
	root := s.tree.CreateItem()

	// Group: Tables
	var tableItems, viewItems []db.TableInfo
	for _, t := range s.allTables {
		if q != "" && !strings.Contains(strings.ToLower(t.Name), q) {
			continue
		}
		if t.Type == "VIEW" {
			viewItems = append(viewItems, t)
		} else {
			tableItems = append(tableItems, t)
		}
	}

	if len(tableItems) > 0 {
		group := s.tree.MoreArgs().CreateItem(root, -1)
		group.SetText(0, fmt.Sprintf("Tables (%d)", len(tableItems)))
		group.SetSelectable(0, false)
		group.SetSelectable(1, false)
		for _, t := range tableItems {
			s.addTableItem(group, t)
		}
	}

	if len(viewItems) > 0 {
		group := s.tree.MoreArgs().CreateItem(root, -1)
		group.SetText(0, fmt.Sprintf("Views (%d)", len(viewItems)))
		group.SetSelectable(0, false)
		group.SetSelectable(1, false)
		for _, t := range viewItems {
			s.addTableItem(group, t)
		}
	}
}

func (s *SchemaPanel) addTableItem(parent TreeItem.Instance, t db.TableInfo) {
	tableItem := s.tree.MoreArgs().CreateItem(parent, -1)
	tableItem.SetText(0, "  "+t.Name)
	tableItem.SetText(1, "")
	tableItem.SetSelectable(0, true)
	tableItem.SetSelectable(1, false)
	tableItem.SetTooltipText(0, t.Name)

	for _, col := range t.Columns {
		colItem := s.tree.MoreArgs().CreateItem(tableItem, -1)
		typeSuffix := col.DataType
		if col.Nullable {
			typeSuffix += "?"
		}
		colItem.SetText(0, "    "+col.Name)
		colItem.SetText(1, typeSuffix)
		colItem.SetSelectable(0, false)
		colItem.SetSelectable(1, false)
	}
	tableItem.SetCollapsed(true)
}

// ── History panel ──────────────────────────────────────────────────────────

type HistoryPanel struct {
	VBoxContainer.Extension[HistoryPanel] `gd:"ParquetHistoryPanel"`

	tree      Tree.Instance
	OnReplay  func(sql string) // callback when user clicks a history entry
}

func (h *HistoryPanel) Ready() {
	h.AsControl().AddThemeConstantOverride("separation", 4)

	header := Label.New()
	header.SetText("Query History")
	applyLabelTheme(header.AsControl(), true)

	h.tree = Tree.New()
	h.tree.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	h.tree.SetHideRoot(true)
	h.tree.SetColumns(1)
	applySidebarTreeTheme(h.tree.AsControl())

	h.tree.OnItemActivated(func() {
		selected := h.tree.GetSelected()
		if selected == (TreeItem.Instance{}) || h.OnReplay == nil {
			return
		}
		sql := selected.GetTooltipText(0)
		if sql != "" {
			h.OnReplay(sql)
		}
	})

	h.AsNode().AddChild(header.AsNode())
	h.AsNode().AddChild(h.tree.AsNode())
}

func (h *HistoryPanel) SetHistory(entries []models.HistoryEntry) {
	h.tree.Clear()
	root := h.tree.CreateItem()
	for _, e := range entries {
		item := h.tree.MoreArgs().CreateItem(root, -1)
		// Show truncated SQL + timestamp
		display := e.SQL
		if len(display) > 60 {
			display = display[:57] + "..."
		}
		ts := e.Timestamp.Local().Format("15:04:05")
		item.SetText(0, ts+"  "+display)
		item.SetTooltipText(0, e.SQL) // full SQL in tooltip for replay
	}
}

// ── SQL editor ─────────────────────────────────────────────────────────────

type SQLPanel struct {
	VBoxContainer.Extension[SQLPanel] `gd:"ParquetSQLPanel"`

	editor     CodeEdit.Instance
	OnRunQuery func(sql string)
}

func (s *SQLPanel) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 4)

	// Top row: label + run button
	row := HBoxContainer.New()
	row.AsControl().AddThemeConstantOverride("separation", 6)

	label := Label.New()
	label.SetText("SQL")
	applyLabelTheme(label.AsControl(), true)
	label.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	runBtn := Button.New()
	runBtn.SetText("▶ Run")
	applyButtonTheme(runBtn.AsControl())
	runBtn.AsBaseButton().OnPressed(func() {
		if s.OnRunQuery != nil {
			s.OnRunQuery(s.editor.AsTextEdit().Text())
		}
	})

	row.AsNode().AddChild(label.AsNode())
	row.AsNode().AddChild(runBtn.AsNode())

	s.editor = CodeEdit.New()
	s.editor.AsControl().SetCustomMinimumSize(Vector2.New(0, 80))
	s.editor.SetGuttersDrawExecutingLines(false)
	s.editor.SetGuttersDrawLineNumbers(false)
	s.editor.SetGuttersDrawBreakpointsGutter(false)
	s.editor.SetGuttersDrawBookmarks(false)
	applyTextEditTheme(s.editor.AsControl())

	// SQL syntax highlighting
	setupSQLHighlighter(s.editor)

	s.AsNode().AddChild(row.AsNode())
	s.AsNode().AddChild(s.editor.AsNode())
}

func (s *SQLPanel) SetSQL(sql string) {
	s.editor.AsTextEdit().SetText(sql)
}

// ── Data grid ──────────────────────────────────────────────────────────────

type DataGrid struct {
	Tree.Extension[DataGrid] `gd:"ParquetDataGrid"`

	OnColumnClicked func(column int)
	OnRowSelected   func(rowIndex int)
	columns         []string // track current column names
	rows            [][]string
}

func (d *DataGrid) Ready() {
	d.Super().SetColumns(1)
	d.Super().SetColumnTitlesVisible(true)
	d.Super().SetHideRoot(true)
	d.Super().SetSelectMode(Tree.SelectRow)
	d.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyTreeTheme(d.AsControl())

	d.Super().OnColumnTitleClicked(func(column int, mouseButton int) {
		if d.OnColumnClicked != nil {
			d.OnColumnClicked(column)
		}
	})

	d.Super().OnItemSelected(func() {
		if d.OnRowSelected == nil {
			return
		}
		selected := d.Super().GetSelected()
		if selected == (TreeItem.Instance{}) {
			return
		}
		// Find row index by walking tree items
		root := d.Super().GetRoot()
		child := root.GetFirstChild()
		idx := 0
		for child != (TreeItem.Instance{}) {
			if child == selected {
				d.OnRowSelected(idx)
				return
			}
			child = child.GetNext()
			idx++
		}
	})
}

func (d *DataGrid) UpdateColumnTitles(columns []string, sortCol string, sortDir models.SortDirection) {
	t := d.Super()
	for i, col := range columns {
		title := col
		if col == sortCol {
			switch sortDir {
			case models.SortAsc:
				title += " ▲"
			case models.SortDesc:
				title += " ▼"
			}
		}
		t.SetColumnTitle(i, title)
	}
}

func (d *DataGrid) ShowError(msg string) {
	d.columns = nil
	d.rows = nil
	t := d.Super()
	t.Clear()
	t.SetColumns(1)
	t.SetColumnTitle(0, "Error")
	root := t.CreateItem()
	item := t.MoreArgs().CreateItem(root, -1)
	item.SetText(0, msg)
}

func (d *DataGrid) SetResult(r *db.QueryResult) {
	if r == nil {
		return
	}
	d.columns = r.Columns
	d.rows = r.Rows
	t := d.Super()
	t.Clear()
	t.SetColumns(len(r.Columns))
	for i, col := range r.Columns {
		t.SetColumnTitle(i, col)
	}
	root := t.CreateItem()
	for _, row := range r.Rows {
		item := t.MoreArgs().CreateItem(root, -1)
		for i, cell := range row {
			item.SetText(i, cell)
		}
	}
}

func debugLog(msg string) {
	f, _ := os.OpenFile("/tmp/pv-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		fmt.Fprintln(f, msg)
		f.Close()
	}
}

// ── Row detail panel ───────────────────────────────────────────────────────

type RowDetailPanel struct {
	VBoxContainer.Extension[RowDetailPanel] `gd:"ParquetRowDetail"`

	searchBox    LineEdit.Instance
	scrollBox    ScrollContainer.Instance
	fieldsList   VBoxContainer.Instance
	placeholder  VBoxContainer.Instance
	columns      []string
	values       []string
}

func (p *RowDetailPanel) Ready() {
	p.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.AsControl().AddThemeConstantOverride("separation", 0)
	p.AsControl().SetClipContents(true)

	// Search input
	p.searchBox = LineEdit.New()
	p.searchBox.SetPlaceholderText("Search fields…")
	p.searchBox.AsControl().AddThemeFontSizeOverride("font_size", 12)
	applyInputTheme(p.searchBox.AsControl())
	p.searchBox.OnTextChanged(func(text string) {
		p.filterFields(text)
	})

	searchWrap := MarginContainer.New()
	searchWrap.AsControl().AddThemeConstantOverride("margin_top", 4)
	searchWrap.AsControl().AddThemeConstantOverride("margin_left", 6)
	searchWrap.AsControl().AddThemeConstantOverride("margin_right", 6)
	searchWrap.AsControl().AddThemeConstantOverride("margin_bottom", 4)
	searchWrap.AsNode().AddChild(p.searchBox.AsNode())

	// Placeholder: "No row selected"
	p.placeholder = VBoxContainer.New()
	p.placeholder.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.placeholder.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.placeholder.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	phIcon := Label.New()
	phIcon.SetText("☰")
	phIcon.AsControl().AddThemeFontSizeOverride("font_size", 32)
	phIcon.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	phIcon.SetHorizontalAlignment(1)

	phText := Label.New()
	phText.SetText("No row selected")
	phText.AsControl().AddThemeFontSizeOverride("font_size", 12)
	phText.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	phText.SetHorizontalAlignment(1)

	p.placeholder.AsNode().AddChild(phIcon.AsNode())
	p.placeholder.AsNode().AddChild(phText.AsNode())

	// Scrollable form area
	p.scrollBox = ScrollContainer.New()
	p.scrollBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.scrollBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.scrollBox.SetHorizontalScrollMode(ScrollContainer.ScrollModeDisabled)
	p.scrollBox.AsCanvasItem().SetVisible(false)

	p.fieldsList = VBoxContainer.New()
	p.fieldsList.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.fieldsList.AsControl().AddThemeConstantOverride("separation", 12)

	fieldsMargin := MarginContainer.New()
	fieldsMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	fieldsMargin.AsControl().AddThemeConstantOverride("margin_top", 4)
	fieldsMargin.AsControl().AddThemeConstantOverride("margin_left", 6)
	fieldsMargin.AsControl().AddThemeConstantOverride("margin_right", 6)
	fieldsMargin.AsControl().AddThemeConstantOverride("margin_bottom", 6)
	fieldsMargin.AsNode().AddChild(p.fieldsList.AsNode())
	p.scrollBox.AsNode().AddChild(fieldsMargin.AsNode())

	// Separator line between search and fields
	sep := PanelContainer.New()
	sep.AsControl().SetCustomMinimumSize(Vector2.New(0, 1))
	applyPanelBg(sep.AsControl(), colorBorder)

	p.AsNode().AddChild(searchWrap.AsNode())
	p.AsNode().AddChild(sep.AsNode())
	p.AsNode().AddChild(p.placeholder.AsNode())
	p.AsNode().AddChild(p.scrollBox.AsNode())
}

func (p *RowDetailPanel) SetRow(columns []string, values []string) {
	p.columns = columns
	p.values = values
	p.searchBox.SetText("")
	p.placeholder.AsCanvasItem().SetVisible(false)
	p.scrollBox.AsCanvasItem().SetVisible(true)
	p.filterFields("")
}

func (p *RowDetailPanel) Clear() {
	p.columns = nil
	p.values = nil
	p.clearFields()
}

func (p *RowDetailPanel) clearFields() {
	for p.fieldsList.AsNode().GetChildCount() > 0 {
		child := p.fieldsList.AsNode().GetChild(0)
		p.fieldsList.AsNode().RemoveChild(child)
		child.QueueFree()
	}
}

func (p *RowDetailPanel) filterFields(query string) {
	p.clearFields()
	query = strings.ToLower(query)
	for i, col := range p.columns {
		val := ""
		if i < len(p.values) {
			val = p.values[i]
		}
		if query != "" && !strings.Contains(strings.ToLower(col), query) && !strings.Contains(strings.ToLower(val), query) {
			continue
		}
		// Field group: label + value input
		group := VBoxContainer.New()
		group.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		group.AsControl().AddThemeConstantOverride("separation", 2)

		// Label (field name + type dim)
		lbl := Label.New()
		lbl.SetText(col)
		lbl.AsControl().AddThemeFontSizeOverride("font_size", 11)
		lbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)

		// Value (read-only input for copyable text)
		valInput := LineEdit.New()
		valInput.SetText(val)
		valInput.SetEditable(false)
		valInput.AsControl().AddThemeFontSizeOverride("font_size", 13)
		valInput.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		applyInputTheme(valInput.AsControl())

		group.AsNode().AddChild(lbl.AsNode())
		group.AsNode().AddChild(valInput.AsNode())
		p.fieldsList.AsNode().AddChild(group.AsNode())
	}
}

// ── Status bar ─────────────────────────────────────────────────────────────

type StatusBar struct {
	HBoxContainer.Extension[StatusBar] `gd:"ParquetStatusBar"`

	rowCount  Label.Instance
	pageLabel Label.Instance
	leftBtn   Button.Instance
	rightBtn  Button.Instance

	OnPrevPage        func()
	OnNextPage        func()
	OnToggleLeftPane  func()
	OnToggleRightPane func()

	leftPaneVisible  bool
	rightPaneVisible bool
}

func (s *StatusBar) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 8)

	// Left: pane toggle buttons with SVG icons
	s.leftPaneVisible = true
	s.rightPaneVisible = false

	s.leftBtn = Button.New()
	s.leftBtn.AsControl().SetTooltipText("Toggle Left Pane")
	s.leftBtn.SetText("◧")
	s.leftBtn.AsControl().SetCustomMinimumSize(Vector2.New(28, 22))
	applyToggleButtonTheme(s.leftBtn.AsControl(), true)
	s.leftBtn.AsBaseButton().OnPressed(func() {
		s.leftPaneVisible = !s.leftPaneVisible
		applyToggleButtonTheme(s.leftBtn.AsControl(), s.leftPaneVisible)
		if s.OnToggleLeftPane != nil {
			s.OnToggleLeftPane()
		}
	})

	s.rightBtn = Button.New()
	s.rightBtn.AsControl().SetTooltipText("Toggle Right Pane")
	s.rightBtn.SetText("◨")
	s.rightBtn.AsControl().SetCustomMinimumSize(Vector2.New(28, 22))
	applyToggleButtonTheme(s.rightBtn.AsControl(), false)

	// Spacer
	spacer := Control.New()
	spacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)

	// Pagination controls
	prevBtn := Button.New()
	prevBtn.SetText("◀")
	applySecondaryButtonTheme(prevBtn.AsControl())
	prevBtn.AsBaseButton().OnPressed(func() {
		if s.OnPrevPage != nil {
			s.OnPrevPage()
		}
	})

	s.pageLabel = Label.New()
	s.pageLabel.SetText("Page 1")
	applyStatusBarTheme(s.pageLabel.AsControl())

	nextBtn := Button.New()
	nextBtn.SetText("▶")
	applySecondaryButtonTheme(nextBtn.AsControl())
	nextBtn.AsBaseButton().OnPressed(func() {
		if s.OnNextPage != nil {
			s.OnNextPage()
		}
	})

	sep := Label.New()
	sep.SetText("·")
	applyStatusBarTheme(sep.AsControl())

	// Right: row count
	s.rowCount = Label.New()
	s.rowCount.SetText("Ready")
	applyStatusBarTheme(s.rowCount.AsControl())

	s.rightBtn.AsBaseButton().OnPressed(func() {
		s.rightPaneVisible = !s.rightPaneVisible
		applyToggleButtonTheme(s.rightBtn.AsControl(), s.rightPaneVisible)
		if s.OnToggleRightPane != nil {
			s.OnToggleRightPane()
		}
	})

	s.AsNode().AddChild(s.leftBtn.AsNode())
	s.AsNode().AddChild(s.rightBtn.AsNode())
	s.AsNode().AddChild(spacer.AsNode())
	s.AsNode().AddChild(prevBtn.AsNode())
	s.AsNode().AddChild(s.pageLabel.AsNode())
	s.AsNode().AddChild(nextBtn.AsNode())
	s.AsNode().AddChild(sep.AsNode())
	s.AsNode().AddChild(s.rowCount.AsNode())
}

func (s *StatusBar) SetStatus(msg string) {
	s.rowCount.SetText(msg)
}

func (s *StatusBar) SetPage(page, totalPages int) {
	s.pageLabel.SetText(fmt.Sprintf("Page %d / %d", page, totalPages))
}

// ── Tab state ──────────────────────────────────────────────────────────────

type tabState struct {
	State        *models.AppState
	schema       *SchemaPanel
	historyPanel *HistoryPanel
	sqlPanel     *SQLPanel
	dataGrid     *DataGrid
	detailPanel  *RowDetailPanel
	connIdx      int  // index into AppWindow.connections (-1 = in-memory)
	navigating   bool // true during back/forward nav — skip history+nav recording

	// Container nodes for show/hide on tab switch
	sidebarWrap PanelContainer.Instance
	outerWrap   HSplitContainer.Instance // content | detail
	rightPanel  VBoxContainer.Instance   // SQL + data grid
	detailWrap  PanelContainer.Instance
}

// ── App root ───────────────────────────────────────────────────────────────

type App struct {
	MarginContainer.Extension[App] `gd:"ParquetViewer"`

	Duck          *db.DB          `gd:"-"`
	ControlServer *control.Server `gd:"-"`

	// Legacy accessor — points to active window's active tab state
	State *models.AppState `gd:"-"`

	mainWin    *AppWindow     `gd:"-"`
	secondWins []*AppWindow   `gd:"-"`
	appMenu    *AppMenu       `gd:"-"`
}

func (a *App) activeWindow() *AppWindow {
	return a.mainWin // TODO: track focused window
}

func (a *App) Ready() {
	a.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)

	// Build main window UI
	history := models.NewQueryHistory()
	a.mainWin = &AppWindow{
		isMain:  true,
		duck:    a.Duck,
		history: history,
	}
	a.mainWin.onNewWindow = func() { a.newWindow() }

	ui := a.mainWin.buildUI()
	a.AsNode().AddChild(ui.AsNode())

	// Now create the initial tab (after UI is in tree so Ready() has been called)
	a.mainWin.addNewTab()

	// Setup file drag & drop on main window
	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		if root := tree.Root(); root != Window.Nil {
			root.OnFilesDropped(func(files []string) {
				for _, f := range files {
					if len(f) > 8 && f[len(f)-8:] == ".parquet" {
						a.mainWin.onFileSelected(f)
						return
					}
				}
				if len(files) > 0 {
					a.mainWin.onFileSelected(files[0])
				}
			})
		}
	}

	// Setup native menu bar
	a.appMenu = &AppMenu{
		OnOpenFile: func() {
			w := a.activeWindow()
			DisplayServer.FileDialogShow(
				"Open Parquet File",
				"",
				"",
				false,
				DisplayServer.FileDialogModeOpenFile,
				[]string{"*.parquet ; Parquet Files"},
				func(status bool, selectedPaths []string, selectedFilterIndex int) {
					if status && len(selectedPaths) > 0 {
						w.onFileSelected(selectedPaths[0])
					}
				},
				0,
			)
		},
		OnOpenRecent: func(path string) {
			a.activeWindow().onFileSelected(path)
		},
		OnNewTab: func() {
			a.activeWindow().addNewTab()
		},
		OnCloseTab: func() {
			w := a.activeWindow()
			w.closeTab(w.activeTab)
		},
		OnNewWindow: func() {
			a.newWindow()
		},
	}
	a.appMenu.Setup()

	// Update State pointer for control server compatibility
	a.State = a.mainWin.currentState()

	// Wire up control server state provider
	if a.ControlServer != nil {
		a.ControlServer.SetStateProvider(func() (json.RawMessage, error) {
			w := a.activeWindow()
			state := map[string]any{
				"tabCount":    len(w.tabs),
				"activeTab":   w.activeTab,
				"windowCount": 1 + len(a.secondWins),
			}
			if s := w.currentState(); s != nil {
				state["filePath"] = s.FilePath
				state["userSQL"] = s.UserSQL
				state["sortColumn"] = s.SortColumn
				state["sortDir"] = int(s.SortDir)
				state["pageOffset"] = s.PageOffset
				state["pageSize"] = s.PageSize
				state["rowCount"] = 0
				state["columns"] = []string{}
				if s.Result != nil {
					state["rowCount"] = s.Result.Total
					state["columns"] = s.Result.Columns
				}
				if s.Schema != nil {
					schema := make([]map[string]any, len(s.Schema))
					for i, c := range s.Schema {
						schema[i] = map[string]any{
							"name": c.Name, "type": c.DataType, "nullable": c.Nullable,
						}
					}
					state["schema"] = schema
				}
			}
			return json.Marshal(state)
		})
	}
}

func (a *App) newWindow() {
	aw := createSecondaryWindow(a.Duck, a.mainWin.history, func() { a.newWindow() })
	a.secondWins = append(a.secondWins, aw)

	// Add the window to the scene tree
	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		root := tree.Root()
		root.AsNode().AddChild(aw.window.AsNode())
		aw.window.Show()
		aw.window.MoveToCenter()
		// Cascade offset so windows don't stack exactly
		pos := aw.window.Position()
		offset := int32(len(a.secondWins) * 30)
		aw.window.SetPosition(Vector2i.New(int(pos.X+offset), int(pos.Y+offset)))
		aw.window.GrabFocus()
		aw.window.MoveToForeground()
		aw.addNewTab()
	}
}

func (a *App) UnhandledKeyInput(event InputEvent.Instance) {
	key, ok := Object.As[InputEventKey.Instance](event)
	if !ok || !key.AsInputEvent().IsPressed() {
		return
	}
	if !key.AsInputEventWithModifiers().IsCommandOrControlPressed() {
		return
	}
	switch key.Keycode() {
	case Input.KeyN:
		fmt.Println("[input] Cmd+N → new window")
		a.newWindow()
	case Input.KeyT:
		fmt.Println("[input] Cmd+T → new tab")
		a.activeWindow().addNewTab()
	case Input.KeyW:
		fmt.Println("[input] Cmd+W → close tab")
		w := a.activeWindow()
		w.closeTab(w.activeTab)
	case Input.KeyO:
		fmt.Println("[input] Cmd+O → open file")
		if a.appMenu != nil && a.appMenu.OnOpenFile != nil {
			a.appMenu.OnOpenFile()
		}
	case Input.KeyBracketleft:
		a.activeWindow().navBack()
	case Input.KeyBracketright:
		a.activeWindow().navForward()
	}
}

func (a *App) Process(delta Float.X) {
	// Update State pointer
	if w := a.activeWindow(); w != nil {
		a.State = w.currentState()
	}

	if a.ControlServer == nil {
		return
	}
	for {
		select {
		case cmd := <-a.ControlServer.Commands():
			a.handleControlCommand(cmd)
		default:
			return
		}
	}
}

func (a *App) handleControlCommand(cmd *control.Command) {
	defer func() {
		if r := recover(); r != nil {
			cmd.Respond(control.Result{Error: fmt.Sprintf("panic: %v", r)})
		}
	}()

	w := a.activeWindow()

	switch cmd.Action {
	case "open":
		var d control.OpenData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		w.onFileSelected(d.Path)
		cmd.Respond(control.Result{OK: true})

	case "sort":
		var d control.SortData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		s := w.currentState()
		if s == nil || s.Result == nil || d.Column >= len(s.Result.Columns) {
			cmd.Respond(control.Result{Error: "invalid column or no data loaded"})
			return
		}
		colName := s.Result.Columns[d.Column]
		if s.SortColumn == colName {
			switch s.SortDir {
			case models.SortAsc:
				s.SortDir = models.SortDesc
			case models.SortDesc:
				s.SortColumn = ""
				s.SortDir = models.SortNone
			default:
				s.SortDir = models.SortAsc
			}
		} else {
			s.SortColumn = colName
			s.SortDir = models.SortAsc
		}
		s.PageOffset = 0
		w.runCurrentQuery()
		cmd.Respond(control.Result{OK: true})

	case "query":
		var d control.QueryData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		ts := w.currentTab()
		if ts != nil {
			ts.State.UserSQL = d.SQL
			ts.State.PageOffset = 0
			ts.State.SortColumn = ""
			ts.State.SortDir = models.SortNone
			ts.sqlPanel.SetSQL(d.SQL)
		}
		if err := w.runCurrentQuery(); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
		} else {
			cmd.Respond(control.Result{OK: true})
		}

	case "page":
		var d control.PageData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		if s := w.currentState(); s != nil {
			s.PageOffset = d.Offset
		}
		w.runCurrentQuery()
		cmd.Respond(control.Result{OK: true})

	case "reset_sort":
		if s := w.currentState(); s != nil {
			s.SortColumn = ""
			s.SortDir = models.SortNone
			s.PageOffset = 0
		}
		w.runCurrentQuery()
		cmd.Respond(control.Result{OK: true})

	case "new_tab":
		w.addNewTab()
		cmd.Respond(control.Result{OK: true})

	case "close_tab":
		w.closeTab(w.activeTab)
		cmd.Respond(control.Result{OK: true})

	case "select_row":
		var d struct{ Row int `json:"row"` }
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		ts := w.currentTab()
		if ts == nil || ts.dataGrid == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		if d.Row < 0 || d.Row >= len(ts.dataGrid.rows) {
			cmd.Respond(control.Result{Error: "row index out of range"})
			return
		}
		ts.detailPanel.SetRow(ts.dataGrid.columns, ts.dataGrid.rows[d.Row])
		ts.detailWrap.AsCanvasItem().SetVisible(true)
		cmd.Respond(control.Result{OK: true})

	case "search_detail":
		var d struct{ Query string `json:"query"` }
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		ts := w.currentTab()
		if ts == nil || ts.detailPanel == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		ts.detailPanel.searchBox.SetText(d.Query)
		ts.detailPanel.filterFields(d.Query)
		cmd.Respond(control.Result{OK: true})

	case "new_window":
		a.newWindow()
		cmd.Respond(control.Result{OK: true})

	case "nav_back":
		w.navBack()
		cmd.Respond(control.Result{OK: true})

	case "nav_forward":
		w.navForward()
		cmd.Respond(control.Result{OK: true})

	case "screenshot":
		viewport := Engine.GetMainLoop()
		if tree, ok := Object.As[SceneTree.Instance](viewport); ok {
			root := tree.Root()
			tex := root.AsViewport().GetTexture()
			img := tex.AsTexture2D().GetImage()
			pngBytes := img.SavePngToBuffer()
			cmd.Respond(control.Result{OK: true, RawBytes: pngBytes})
		} else {
			cmd.Respond(control.Result{Error: "could not get viewport"})
		}

	default:
		cmd.Respond(control.Result{Error: "unknown action: " + cmd.Action})
	}
}

// RegisterAll registers all custom classes with the engine.
func RegisterAll() {
	classdb.Register[TitleBar]()
	classdb.Register[Toolbar]()
	classdb.Register[SchemaPanel]()
	classdb.Register[SQLPanel]()
	classdb.Register[DataGrid]()
	classdb.Register[HistoryPanel]()
	classdb.Register[RowDetailPanel]()
	classdb.Register[StatusBar]()
	classdb.Register[App]()
}

var _ TreeItem.Instance
