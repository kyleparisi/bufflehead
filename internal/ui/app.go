package ui

import (
	"encoding/json"
	"fmt"

	"parquet-viewer/internal/control"
	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"

	"graphics.gd/classdb"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Object"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/TextEdit"
	"graphics.gd/classdb/Tree"
	"graphics.gd/classdb/TreeItem"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/variant/Vector2"

)

// ── Title bar ──────────────────────────────────────────────────────────────

type TitleBar struct {
	PanelContainer.Extension[TitleBar] `gd:"ParquetTitleBar"`

	infoLabel Label.Instance
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
	row.AsControl().AddThemeConstantOverride("separation", 0)

	// Left spacer (25%)
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

	row.AsNode().AddChild(leftSpacer.AsNode())
	row.AsNode().AddChild(pill.AsNode())
	row.AsNode().AddChild(rightSpacer.AsNode())

	margin.AsNode().AddChild(row.AsNode())
	t.AsNode().AddChild(margin.AsNode())
}

func (t *TitleBar) SetFileInfo(path string) {
	t.infoLabel.SetText("DuckDB  ·  In-Memory  ·  " + path)
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

	OnColumnClicked func(column int)
	columns         []string // track current column names
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

func (d *DataGrid) SetResult(r *db.QueryResult) {
	if r == nil {
		return
	}
	d.columns = r.Columns
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

	rowCount  Label.Instance
	pageLabel Label.Instance

	OnPrevPage func()
	OnNextPage func()
}

func (s *StatusBar) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 8)

	// Left: Data tab indicator
	dataTab := Label.New()
	dataTab.SetText("Data")
	dataTab.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	dataTab.AsControl().AddThemeFontSizeOverride("font_size", 13)

	structTab := Label.New()
	structTab.SetText("Structure")
	applyLabelTheme(structTab.AsControl(), true)

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

	s.AsNode().AddChild(dataTab.AsNode())
	s.AsNode().AddChild(structTab.AsNode())
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

// ── App root ───────────────────────────────────────────────────────────────

type App struct {
	MarginContainer.Extension[App] `gd:"ParquetViewer"`

	Duck          *db.DB            `gd:"-"`
	State         *models.AppState  `gd:"-"`
	ControlServer *control.Server   `gd:"-"`

	titleBar  *TitleBar
	toolbar   *Toolbar
	schema    *SchemaPanel
	sqlPanel  *SQLPanel
	dataGrid  *DataGrid
	statusBar *StatusBar
	appMenu   *AppMenu
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

	// ── Title bar ────────────────────────────────────────────────────
	a.titleBar = new(TitleBar)

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
		a.State.UserSQL = sql
		a.State.PageOffset = 0 // reset pagination on new query
		a.State.SortColumn = ""
		a.State.SortDir = models.SortNone
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
	a.dataGrid.OnColumnClicked = func(column int) {
		if a.State.Result == nil || column >= len(a.State.Result.Columns) {
			return
		}
		colName := a.State.Result.Columns[column]
		if a.State.SortColumn == colName {
			// Cycle: asc → desc → none
			switch a.State.SortDir {
			case models.SortAsc:
				a.State.SortDir = models.SortDesc
			case models.SortDesc:
				a.State.SortColumn = ""
				a.State.SortDir = models.SortNone
			default:
				a.State.SortDir = models.SortAsc
			}
		} else {
			a.State.SortColumn = colName
			a.State.SortDir = models.SortAsc
		}
		a.State.PageOffset = 0
		a.execQuery()
	}

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
	a.statusBar.OnPrevPage = func() {
		if a.State.PageOffset-a.State.PageSize >= 0 {
			a.State.PageOffset -= a.State.PageSize
			a.execQuery()
		}
	}
	a.statusBar.OnNextPage = func() {
		if a.State.Result != nil && a.State.PageOffset+a.State.PageSize < int(a.State.Result.Total) {
			a.State.PageOffset += a.State.PageSize
			a.execQuery()
		}
	}
	statusMargin.AsNode().AddChild(a.statusBar.AsNode())
	statusWrap.AsNode().AddChild(statusMargin.AsNode())

	// ── Assemble ─────────────────────────────────────────────────────
	outerVBox.AsNode().AddChild(a.titleBar.AsNode())
	outerVBox.AsNode().AddChild(toolbarWrap.AsNode())
	outerVBox.AsNode().AddChild(split.AsNode())
	outerVBox.AsNode().AddChild(statusWrap.AsNode())

	bg.AsNode().AddChild(outerVBox.AsNode())
	a.AsNode().AddChild(bg.AsNode())

	// Setup native menu bar
	a.appMenu = &AppMenu{
		OnOpenFile: func() {
			DisplayServer.FileDialogShow(
				"Open Parquet File",
				"",
				"",
				false,
				DisplayServer.FileDialogModeOpenFile,
				[]string{"*.parquet ; Parquet Files"},
				func(status bool, selectedPaths []string, selectedFilterIndex int) {
					if status && len(selectedPaths) > 0 {
						a.onFileSelected(selectedPaths[0])
						a.toolbar.fileLabel.SetText(selectedPaths[0])
					}
				},
				0,
			)
		},
		OnOpenRecent: func(path string) {
			a.onFileSelected(path)
			a.toolbar.fileLabel.SetText(path)
		},
	}
	a.appMenu.Setup()

	// Wire up control server state provider
	if a.ControlServer != nil {
		a.ControlServer.SetStateProvider(func() (json.RawMessage, error) {
			state := map[string]any{
				"filePath":   a.State.FilePath,
				"userSQL":    a.State.UserSQL,
				"sortColumn": a.State.SortColumn,
				"sortDir":    int(a.State.SortDir),
				"pageOffset": a.State.PageOffset,
				"pageSize":   a.State.PageSize,
				"rowCount":   0,
				"columns":    []string{},
			}
			if a.State.Result != nil {
				state["rowCount"] = a.State.Result.Total
				state["columns"] = a.State.Result.Columns
			}
			if a.State.Schema != nil {
				schema := make([]map[string]any, len(a.State.Schema))
				for i, c := range a.State.Schema {
					schema[i] = map[string]any{
						"name": c.Name, "type": c.DataType, "nullable": c.Nullable,
					}
				}
				state["schema"] = schema
			}
			return json.Marshal(state)
		})
	}
}

func (a *App) Process(delta Float.X) {
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
	switch cmd.Action {
	case "open":
		var d control.OpenData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		a.onFileSelected(d.Path)
		a.toolbar.fileLabel.SetText(d.Path)
		cmd.Respond(control.Result{OK: true})

	case "sort":
		var d control.SortData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		if a.State.Result == nil || d.Column >= len(a.State.Result.Columns) {
			cmd.Respond(control.Result{Error: "invalid column or no data loaded"})
			return
		}
		colName := a.State.Result.Columns[d.Column]
		if a.State.SortColumn == colName {
			switch a.State.SortDir {
			case models.SortAsc:
				a.State.SortDir = models.SortDesc
			case models.SortDesc:
				a.State.SortColumn = ""
				a.State.SortDir = models.SortNone
			default:
				a.State.SortDir = models.SortAsc
			}
		} else {
			a.State.SortColumn = colName
			a.State.SortDir = models.SortAsc
		}
		a.State.PageOffset = 0
		a.execQuery()
		cmd.Respond(control.Result{OK: true})

	case "query":
		var d control.QueryData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		a.State.UserSQL = d.SQL
		a.State.PageOffset = 0
		a.State.SortColumn = ""
		a.State.SortDir = models.SortNone
		a.sqlPanel.SetSQL(d.SQL)
		a.execQuery()
		cmd.Respond(control.Result{OK: true})

	case "page":
		var d control.PageData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		a.State.PageOffset = d.Offset
		a.execQuery()
		cmd.Respond(control.Result{OK: true})

	case "reset_sort":
		a.State.SortColumn = ""
		a.State.SortDir = models.SortNone
		a.State.PageOffset = 0
		a.execQuery()
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

func (a *App) onFileSelected(path string) {
	a.State.FilePath = path
	a.State.UserSQL = db.DefaultQuery(path)
	if a.appMenu != nil {
		a.appMenu.AddRecentFile(path)
	}
	a.State.PageOffset = 0
	a.State.SortColumn = ""
	a.State.SortDir = models.SortNone
	a.sqlPanel.SetSQL(a.State.UserSQL)
	a.titleBar.SetFileInfo(path)

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
	result, err := a.Duck.Query(a.State.VirtualSQL(), a.State.PageOffset, a.State.PageSize)
	if err != nil {
		a.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	a.State.Result = result
	a.dataGrid.SetResult(result)
	a.dataGrid.UpdateColumnTitles(result.Columns, a.State.SortColumn, a.State.SortDir)
	start := a.State.PageOffset + 1
	end := a.State.PageOffset + len(result.Rows)
	a.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, result.Total))

	page := (a.State.PageOffset / a.State.PageSize) + 1
	totalPages := (int(result.Total) + a.State.PageSize - 1) / a.State.PageSize
	a.statusBar.SetPage(page, totalPages)
}

// RegisterAll registers all custom classes with the engine.
func RegisterAll() {
	classdb.Register[TitleBar]()
	classdb.Register[Toolbar]()
	classdb.Register[SchemaPanel]()
	classdb.Register[SQLPanel]()
	classdb.Register[DataGrid]()
	classdb.Register[StatusBar]()
	classdb.Register[App]()
}

var _ TreeItem.Instance
