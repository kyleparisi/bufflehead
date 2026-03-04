package ui

import (
	"fmt"

	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"

	"graphics.gd/classdb"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/FileDialog"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/TextEdit"
	"graphics.gd/classdb/Tree"
	"graphics.gd/classdb/TreeItem"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Vector2"

)

// ── Toolbar ────────────────────────────────────────────────────────────────

type Toolbar struct {
	HBoxContainer.Extension[Toolbar] `gd:"ParquetToolbar"`

	fileLabel  LineEdit.Instance
	fileDialog FileDialog.Instance

	OnFileOpened func(path string)
}

func (t *Toolbar) Ready() {
	t.AsControl().AddThemeConstantOverride("separation", 6)

	openBtn := Button.New()
	openBtn.SetText("Open…")
	applyButtonTheme(openBtn.AsControl())
	openBtn.AsBaseButton().OnPressed(func() {
		t.fileDialog.PopupFileDialog()
	})

	t.fileLabel = LineEdit.New()
	t.fileLabel.SetPlaceholderText("No file loaded")
	t.fileLabel.SetEditable(false)
	t.fileLabel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyInputTheme(t.fileLabel.AsControl())

	t.AsNode().AddChild(openBtn.AsNode())
	t.AsNode().AddChild(t.fileLabel.AsNode())

	t.fileDialog = FileDialog.New()
	t.fileDialog.SetFileMode(FileDialog.FileModeOpenFile)
	t.fileDialog.AddFilter("*.parquet")
	t.fileDialog.OnFileSelected(func(path string) {
		t.fileLabel.SetText(path)
		if t.OnFileOpened != nil {
			t.OnFileOpened(path)
		}
	})
	t.AsNode().AddChild(t.fileDialog.AsNode())
}

// ── Schema sidebar ─────────────────────────────────────────────────────────

type SchemaPanel struct {
	VBoxContainer.Extension[SchemaPanel] `gd:"ParquetSchemaPanel"`

	tree Tree.Instance
}

func (s *SchemaPanel) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 4)

	header := Label.New()
	header.SetText("Schema")
	applyLabelTheme(header.AsControl(), true)

	s.tree = Tree.New()
	s.tree.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	s.tree.SetHideRoot(true)
	applySidebarTreeTheme(s.tree.AsControl())

	s.AsNode().AddChild(header.AsNode())
	s.AsNode().AddChild(s.tree.AsNode())
}

func (s *SchemaPanel) SetSchema(cols []db.Column) {
	s.tree.Clear()
	s.tree.SetColumns(2)
	root := s.tree.CreateItem()
	for _, col := range cols {
		item := s.tree.MoreArgs().CreateItem(root, -1)
		typeSuffix := col.DataType
		if col.Nullable {
			typeSuffix += "?"
		}
		item.SetText(0, col.Name)
		item.SetText(1, typeSuffix)
	}
}

// ── SQL editor ─────────────────────────────────────────────────────────────

type SQLPanel struct {
	VBoxContainer.Extension[SQLPanel] `gd:"ParquetSQLPanel"`

	editor     TextEdit.Instance
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
			s.OnRunQuery(s.editor.Text())
		}
	})

	row.AsNode().AddChild(label.AsNode())
	row.AsNode().AddChild(runBtn.AsNode())

	s.editor = TextEdit.New()
	s.editor.AsControl().SetCustomMinimumSize(Vector2.New(0, 80))
	applyTextEditTheme(s.editor.AsControl())

	s.AsNode().AddChild(row.AsNode())
	s.AsNode().AddChild(s.editor.AsNode())
}

func (s *SQLPanel) SetSQL(sql string) {
	s.editor.SetText(sql)
}

// ── Data grid ──────────────────────────────────────────────────────────────

type DataGrid struct {
	Tree.Extension[DataGrid] `gd:"ParquetDataGrid"`
}

func (d *DataGrid) Ready() {
	d.Super().SetColumns(1)
	d.Super().SetColumnTitlesVisible(true)
	d.Super().SetHideRoot(true)
	d.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyTreeTheme(d.AsControl())
}

func (d *DataGrid) SetResult(r *db.QueryResult) {
	if r == nil {
		return
	}
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

// ── Status bar ─────────────────────────────────────────────────────────────

type StatusBar struct {
	HBoxContainer.Extension[StatusBar] `gd:"ParquetStatusBar"`

	rowCount Label.Instance
}

func (s *StatusBar) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 8)

	// Left: Data tab indicator
	dataTab := Label.New()
	dataTab.SetText("Data")
	dataTab.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	dataTab.AsControl().AddThemeFontSizeOverride("font_size", 10)

	structTab := Label.New()
	structTab.SetText("Structure")
	applyLabelTheme(structTab.AsControl(), true)

	// Spacer
	spacer := Control.New()
	spacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)

	// Right: row count
	s.rowCount = Label.New()
	s.rowCount.SetText("Ready")
	applyStatusBarTheme(s.rowCount.AsControl())

	s.AsNode().AddChild(dataTab.AsNode())
	s.AsNode().AddChild(structTab.AsNode())
	s.AsNode().AddChild(spacer.AsNode())
	s.AsNode().AddChild(s.rowCount.AsNode())
}

func (s *StatusBar) SetStatus(msg string) {
	s.rowCount.SetText(msg)
}

// ── App root ───────────────────────────────────────────────────────────────

type App struct {
	MarginContainer.Extension[App] `gd:"ParquetViewer"`

	Duck  *db.DB           `gd:"-"`
	State *models.AppState `gd:"-"`

	toolbar   *Toolbar
	schema    *SchemaPanel
	sqlPanel  *SQLPanel
	dataGrid  *DataGrid
	statusBar *StatusBar
}

func (a *App) Ready() {
	// Make this node fill the entire window
	a.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)

	// Dark background — as child of MarginContainer, it fills automatically
	bg := PanelContainer.New()
	bg.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	bg.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyPanelBg(bg.AsControl(), colorBg)

	outerVBox := VBoxContainer.New()
	outerVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	outerVBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	outerVBox.AsControl().AddThemeConstantOverride("separation", 0)

	// ── Toolbar strip ────────────────────────────────────────────────
	toolbarWrap := MarginContainer.New()
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_top", 6)
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_left", 8)
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_right", 8)
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	a.toolbar = new(Toolbar)
	a.toolbar.OnFileOpened = a.onFileSelected
	toolbarWrap.AsNode().AddChild(a.toolbar.AsNode())

	// ── Main split ───────────────────────────────────────────────────
	split := HSplitContainer.New()
	split.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	split.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	split.AsSplitContainer().SetSplitOffset(180)
	split.AsControl().AddThemeConstantOverride("separation", 1)

	// Left: sidebar with padding
	sidebarWrap := PanelContainer.New()
	sidebarWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyPanelBg(sidebarWrap.AsControl(), colorBgSidebar)
	sidebarMargin := MarginContainer.New()
	sidebarMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sidebarMargin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_top", 6)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_left", 6)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_right", 4)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	a.schema = new(SchemaPanel)
	sidebarMargin.AsNode().AddChild(a.schema.AsNode())
	sidebarWrap.AsNode().AddChild(sidebarMargin.AsNode())

	// Right: sql + grid
	rightPanel := VBoxContainer.New()
	rightPanel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	rightPanel.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	rightPanel.AsControl().AddThemeConstantOverride("separation", 1)

	a.sqlPanel = new(SQLPanel)
	a.sqlPanel.OnRunQuery = func(sql string) {
		a.State.CurrentSQL = sql
		a.execQuery()
	}

	sqlWrap := MarginContainer.New()
	sqlWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_top", 4)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_left", 6)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_right", 6)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_bottom", 2)
	sqlWrap.AsNode().AddChild(a.sqlPanel.AsNode())

	a.dataGrid = new(DataGrid)

	rightPanel.AsNode().AddChild(sqlWrap.AsNode())
	rightPanel.AsNode().AddChild(a.dataGrid.AsNode())

	split.AsNode().AddChild(sidebarWrap.AsNode())
	split.AsNode().AddChild(rightPanel.AsNode())

	// ── Bottom status bar ────────────────────────────────────────────
	statusWrap := PanelContainer.New()
	applyPanelBg(statusWrap.AsControl(), colorBgSidebar)
	statusMargin := MarginContainer.New()
	statusMargin.AsControl().AddThemeConstantOverride("margin_top", 4)
	statusMargin.AsControl().AddThemeConstantOverride("margin_left", 8)
	statusMargin.AsControl().AddThemeConstantOverride("margin_right", 8)
	statusMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	a.statusBar = new(StatusBar)
	statusMargin.AsNode().AddChild(a.statusBar.AsNode())
	statusWrap.AsNode().AddChild(statusMargin.AsNode())

	// ── Assemble ─────────────────────────────────────────────────────
	outerVBox.AsNode().AddChild(toolbarWrap.AsNode())
	outerVBox.AsNode().AddChild(split.AsNode())
	outerVBox.AsNode().AddChild(statusWrap.AsNode())

	bg.AsNode().AddChild(outerVBox.AsNode())
	a.AsNode().AddChild(bg.AsNode())
}

func (a *App) onFileSelected(path string) {
	a.State.FilePath = path
	a.State.CurrentSQL = db.DefaultQuery(path)
	a.sqlPanel.SetSQL(a.State.CurrentSQL)

	cols, err := a.Duck.Schema(path)
	if err != nil {
		a.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	a.State.Schema = cols
	a.schema.SetSchema(cols)

	meta, _ := a.Duck.Metadata(path)
	a.State.Metadata = meta

	a.execQuery()
}

func (a *App) execQuery() {
	a.statusBar.SetStatus("Running…")
	result, err := a.Duck.Query(a.State.CurrentSQL, a.State.PageOffset, a.State.PageSize)
	if err != nil {
		a.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	a.State.Result = result
	a.dataGrid.SetResult(result)
	a.statusBar.SetStatus(fmt.Sprintf("1–%d of %d rows", len(result.Rows), result.Total))
}

// RegisterAll registers all custom classes with the engine.
func RegisterAll() {
	classdb.Register[Toolbar]()
	classdb.Register[SchemaPanel]()
	classdb.Register[SQLPanel]()
	classdb.Register[DataGrid]()
	classdb.Register[StatusBar]()
	classdb.Register[App]()
}

var _ TreeItem.Instance
