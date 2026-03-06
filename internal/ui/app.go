package ui

import (
	"encoding/json"
	"fmt"

	"parquet-viewer/internal/control"
	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"

	"graphics.gd/classdb"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CodeEdit"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/SceneTree"
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

// ── Tab state ──────────────────────────────────────────────────────────────

type tabState struct {
	State    *models.AppState
	schema   *SchemaPanel
	sqlPanel *SQLPanel
	dataGrid *DataGrid

	// Container nodes for show/hide on tab switch
	sidebarWrap PanelContainer.Instance
	rightPanel  VBoxContainer.Instance
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
	a.mainWin = &AppWindow{
		isMain: true,
		duck:   a.Duck,
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
	aw := createSecondaryWindow(a.Duck, func() { a.newWindow() })
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
		aw.addNewTab()
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
		w.execQuery()
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
		w.execQuery()
		cmd.Respond(control.Result{OK: true})

	case "page":
		var d control.PageData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		if s := w.currentState(); s != nil {
			s.PageOffset = d.Offset
		}
		w.execQuery()
		cmd.Respond(control.Result{OK: true})

	case "reset_sort":
		if s := w.currentState(); s != nil {
			s.SortColumn = ""
			s.SortDir = models.SortNone
			s.PageOffset = 0
		}
		w.execQuery()
		cmd.Respond(control.Result{OK: true})

	case "new_tab":
		w.addNewTab()
		cmd.Respond(control.Result{OK: true})

	case "close_tab":
		w.closeTab(w.activeTab)
		cmd.Respond(control.Result{OK: true})

	case "new_window":
		a.newWindow()
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
	classdb.Register[StatusBar]()
	classdb.Register[App]()
}

var _ TreeItem.Instance
