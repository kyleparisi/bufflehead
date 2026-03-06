package ui

import (
	"encoding/json"
	"fmt"

	"parquet-viewer/internal/control"
	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"

	"graphics.gd/classdb"
	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CodeEdit"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/TabBar"
	"graphics.gd/classdb/Tree"
	"graphics.gd/classdb/TreeItem"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Object"
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

	// Legacy accessor — points to active tab's state
	State *models.AppState `gd:"-"`

	titleBar  *TitleBar
	toolbar   *Toolbar
	statusBar *StatusBar
	appMenu   *AppMenu
	tabBar    TabBar.Instance

	tabs      []*tabState `gd:"-"`
	activeTab int         `gd:"-"`

	// The split container holds tab content
	split     HSplitContainer.Instance
	emptyView VBoxContainer.Instance
	tabBarWrap MarginContainer.Instance
}

func (a *App) Ready() {
	a.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)

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

	// ── Tab bar ──────────────────────────────────────────────────────
	a.tabBarWrap = MarginContainer.New()
	a.tabBarWrap.AsControl().AddThemeConstantOverride("margin_left", 8)
	a.tabBarWrap.AsControl().AddThemeConstantOverride("margin_right", 8)
	a.tabBarWrap.AsControl().AddThemeConstantOverride("margin_top", 0)
	a.tabBarWrap.AsControl().AddThemeConstantOverride("margin_bottom", 0)

	a.tabBar = TabBar.New()
	a.tabBar.SetTabCloseDisplayPolicy(TabBar.CloseButtonShowActiveOnly)
	a.tabBar.SetClipTabs(true)
	a.tabBar.SetMaxTabWidth(200)
	a.tabBar.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyTabBarTheme(a.tabBar.AsControl())

	a.tabBar.OnTabChanged(func(tab int) {
		a.switchTab(tab)
	})
	a.tabBar.OnTabClosePressed(func(tab int) {
		a.closeTab(tab)
	})
	a.tabBarWrap.AsNode().AddChild(a.tabBar.AsNode())

	// ── Main split container ─────────────────────────────────────────
	a.split = HSplitContainer.New()
	a.split.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	a.split.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	a.split.AsSplitContainer().SetSplitOffset(180)
	a.split.AsControl().AddThemeConstantOverride("separation", 1)

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

	// ── Empty state view ─────────────────────────────────────────────
	a.emptyView = VBoxContainer.New()
	a.emptyView.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	a.emptyView.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	a.emptyView.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	emptyCenter := VBoxContainer.New()
	emptyCenter.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	emptyCenter.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	emptyCenter.AsControl().AddThemeConstantOverride("separation", 16)
	emptyCenter.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	emptyIcon := Label.New()
	emptyIcon.SetText("⬡")
	emptyIcon.AsControl().AddThemeFontSizeOverride("font_size", 48)
	emptyIcon.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	emptyIcon.SetHorizontalAlignment(1) // center

	emptyTitle := Label.New()
	emptyTitle.SetText("Parquet Viewer")
	emptyTitle.AsControl().AddThemeFontSizeOverride("font_size", 18)
	emptyTitle.AsControl().AddThemeColorOverride("font_color", colorText)
	emptyTitle.SetHorizontalAlignment(1)

	emptyHint := Label.New()
	emptyHint.SetText("⌘T  New Tab   ·   ⌘O  Open File   ·   Drop .parquet here")
	emptyHint.AsControl().AddThemeFontSizeOverride("font_size", 12)
	emptyHint.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	emptyHint.SetHorizontalAlignment(1)

	emptyCenter.AsNode().AddChild(emptyIcon.AsNode())
	emptyCenter.AsNode().AddChild(emptyTitle.AsNode())
	emptyCenter.AsNode().AddChild(emptyHint.AsNode())
	a.emptyView.AsNode().AddChild(emptyCenter.AsNode())

	// ── Assemble ─────────────────────────────────────────────────────
	outerVBox.AsNode().AddChild(a.titleBar.AsNode())
	outerVBox.AsNode().AddChild(toolbarWrap.AsNode())
	outerVBox.AsNode().AddChild(a.tabBarWrap.AsNode())
	outerVBox.AsNode().AddChild(a.split.AsNode())
	outerVBox.AsNode().AddChild(a.emptyView.AsNode())
	outerVBox.AsNode().AddChild(statusWrap.AsNode())

	bg.AsNode().AddChild(outerVBox.AsNode())
	a.AsNode().AddChild(bg.AsNode())

	// Create first tab (visible)
	a.addNewTab()

	// Setup file drag & drop
	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		if root := tree.Root(); root != Window.Nil {
			root.OnFilesDropped(func(files []string) {
				for _, f := range files {
					if len(f) > 8 && f[len(f)-8:] == ".parquet" {
						a.onFileSelected(f)
						a.toolbar.fileLabel.SetText(f)
						return
					}
				}
				if len(files) > 0 {
					a.onFileSelected(files[0])
					a.toolbar.fileLabel.SetText(files[0])
				}
			})
		}
	}

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
		OnNewTab: func() {
			a.addNewTab()
		},
		OnCloseTab: func() {
			a.closeTab(a.activeTab)
		},
	}
	a.appMenu.Setup()

	// Wire up control server state provider
	if a.ControlServer != nil {
		a.ControlServer.SetStateProvider(func() (json.RawMessage, error) {
			state := map[string]any{
				"tabCount":  len(a.tabs),
				"activeTab": a.activeTab,
			}
			if a.State != nil {
				state["filePath"] = a.State.FilePath
				state["userSQL"] = a.State.UserSQL
				state["sortColumn"] = a.State.SortColumn
				state["sortDir"] = int(a.State.SortDir)
				state["pageOffset"] = a.State.PageOffset
				state["pageSize"] = a.State.PageSize
				state["rowCount"] = 0
				state["columns"] = []string{}
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
			}
			return json.Marshal(state)
		})
	}
}

// ── Tab management ─────────────────────────────────────────────────────────

func (a *App) currentTab() *tabState {
	if a.activeTab >= 0 && a.activeTab < len(a.tabs) {
		return a.tabs[a.activeTab]
	}
	return nil
}

func (a *App) addNewTab() {
	ts := &tabState{
		State: models.NewAppState(),
	}

	// Build sidebar
	ts.sidebarWrap = PanelContainer.New()
	ts.sidebarWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyPanelBg(ts.sidebarWrap.AsControl(), colorBgSidebar)
	sidebarMargin := MarginContainer.New()
	sidebarMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sidebarMargin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_top", 6)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_left", 6)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_right", 4)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	ts.schema = new(SchemaPanel)
	sidebarMargin.AsNode().AddChild(ts.schema.AsNode())
	ts.sidebarWrap.AsNode().AddChild(sidebarMargin.AsNode())

	// Build right panel
	ts.rightPanel = VBoxContainer.New()
	ts.rightPanel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.rightPanel.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.rightPanel.AsControl().AddThemeConstantOverride("separation", 1)

	ts.sqlPanel = new(SQLPanel)
	ts.sqlPanel.OnRunQuery = func(sql string) {
		ts.State.UserSQL = sql
		ts.State.PageOffset = 0
		ts.State.SortColumn = ""
		ts.State.SortDir = models.SortNone
		a.execQuery()
	}
	sqlWrap := MarginContainer.New()
	sqlWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_top", 4)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_left", 6)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_right", 6)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_bottom", 2)
	sqlWrap.AsNode().AddChild(ts.sqlPanel.AsNode())

	ts.dataGrid = new(DataGrid)
	ts.dataGrid.OnColumnClicked = func(column int) {
		if ts.State.Result == nil || column >= len(ts.State.Result.Columns) {
			return
		}
		colName := ts.State.Result.Columns[column]
		if ts.State.SortColumn == colName {
			switch ts.State.SortDir {
			case models.SortAsc:
				ts.State.SortDir = models.SortDesc
			case models.SortDesc:
				ts.State.SortColumn = ""
				ts.State.SortDir = models.SortNone
			default:
				ts.State.SortDir = models.SortAsc
			}
		} else {
			ts.State.SortColumn = colName
			ts.State.SortDir = models.SortAsc
		}
		ts.State.PageOffset = 0
		a.execQuery()
	}

	ts.rightPanel.AsNode().AddChild(sqlWrap.AsNode())
	ts.rightPanel.AsNode().AddChild(ts.dataGrid.AsNode())

	// Add to split (hidden initially if not first tab)
	a.split.AsNode().AddChild(ts.sidebarWrap.AsNode())
	a.split.AsNode().AddChild(ts.rightPanel.AsNode())

	// Show tab view (in case we were in empty state)
	a.showTabView()

	// Add tab bar entry
	idx := len(a.tabs)
	a.tabs = append(a.tabs, ts)
	a.tabBar.AddTab()
	a.tabBar.SetTabTitle(idx, "Untitled")

	// Switch to the new tab
	a.tabBar.SetCurrentTab(idx)
	a.switchTab(idx)
}

func (a *App) switchTab(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}

	// Hide all tab content
	for _, ts := range a.tabs {
		ts.sidebarWrap.AsCanvasItem().SetVisible(false)
		ts.rightPanel.AsCanvasItem().SetVisible(false)
	}

	// Show active tab
	a.activeTab = idx
	ts := a.tabs[idx]
	ts.sidebarWrap.AsCanvasItem().SetVisible(true)
	ts.rightPanel.AsCanvasItem().SetVisible(true)
	a.State = ts.State

	// Update toolbar and title bar
	if ts.State.FilePath != "" {
		a.toolbar.fileLabel.SetText(ts.State.FilePath)
		a.titleBar.SetFileInfo(ts.State.FilePath)
	} else {
		a.toolbar.fileLabel.SetText("")
		a.titleBar.SetFileInfo("")
	}

	// Update status bar
	if ts.State.Result != nil {
		start := ts.State.PageOffset + 1
		end := ts.State.PageOffset + len(ts.State.Result.Rows)
		a.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, ts.State.Result.Total))
		page := (ts.State.PageOffset / ts.State.PageSize) + 1
		totalPages := (int(ts.State.Result.Total) + ts.State.PageSize - 1) / ts.State.PageSize
		a.statusBar.SetPage(page, totalPages)
	} else {
		a.statusBar.SetStatus("Ready")
		a.statusBar.SetPage(1, 1)
	}
}

func (a *App) closeTab(idx int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("closeTab recovered:", r)
		}
	}()

	if idx < 0 || idx >= len(a.tabs) {
		return
	}

	ts := a.tabs[idx]

	// Hide first, then remove
	ts.sidebarWrap.AsCanvasItem().SetVisible(false)
	ts.rightPanel.AsCanvasItem().SetVisible(false)

	// Remove nodes from split
	a.split.AsNode().RemoveChild(ts.sidebarWrap.AsNode())
	a.split.AsNode().RemoveChild(ts.rightPanel.AsNode())
	ts.sidebarWrap.AsNode().QueueFree()
	ts.rightPanel.AsNode().QueueFree()

	// Remove from tabs slice
	a.tabs = append(a.tabs[:idx], a.tabs[idx+1:]...)
	a.tabBar.RemoveTab(idx)

	if len(a.tabs) == 0 {
		// No tabs left — show empty view
		a.activeTab = -1
		a.State = nil
		a.showEmptyView()
		return
	}

	// Adjust active tab
	if a.activeTab >= len(a.tabs) {
		a.activeTab = len(a.tabs) - 1
	}
	a.tabBar.SetCurrentTab(a.activeTab)
	a.switchTab(a.activeTab)
}

func (a *App) showEmptyView() {
	a.split.AsCanvasItem().SetVisible(false)
	a.tabBarWrap.AsCanvasItem().SetVisible(false)
	a.emptyView.AsCanvasItem().SetVisible(true)
	a.toolbar.fileLabel.SetText("")
	a.titleBar.SetFileInfo("")
	a.statusBar.SetStatus("No tabs open")
	a.statusBar.SetPage(0, 0)
}

func (a *App) showTabView() {
	a.emptyView.AsCanvasItem().SetVisible(false)
	a.split.AsCanvasItem().SetVisible(true)
	a.tabBarWrap.AsCanvasItem().SetVisible(true)
}

func (a *App) updateTabTitle(idx int) {
	if idx < 0 || idx >= len(a.tabs) {
		return
	}
	ts := a.tabs[idx]
	if ts.State.FilePath != "" {
		// Just the filename
		name := ts.State.FilePath
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '/' {
				name = name[i+1:]
				break
			}
		}
		a.tabBar.SetTabTitle(idx, name)
	} else {
		a.tabBar.SetTabTitle(idx, "Untitled")
	}
}

// ── Process + control commands ─────────────────────────────────────────────

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
		ts := a.currentTab()
		if ts != nil {
			ts.State.UserSQL = d.SQL
			ts.State.PageOffset = 0
			ts.State.SortColumn = ""
			ts.State.SortDir = models.SortNone
			ts.sqlPanel.SetSQL(d.SQL)
		}
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
		if a.State != nil {
			a.State.SortColumn = ""
			a.State.SortDir = models.SortNone
			a.State.PageOffset = 0
			a.execQuery()
		}
		cmd.Respond(control.Result{OK: true})

	case "new_tab":
		a.addNewTab()
		cmd.Respond(control.Result{OK: true})

	case "close_tab":
		a.closeTab(a.activeTab)
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

// ── File + query logic ─────────────────────────────────────────────────────

func (a *App) onFileSelected(path string) {
	if len(a.tabs) == 0 {
		// Re-create a tab from empty state
		a.addNewTab()
	}
	ts := a.currentTab()
	if ts == nil {
		return
	}
	ts.State.FilePath = path
	ts.State.UserSQL = db.DefaultQuery(path)
	if a.appMenu != nil {
		a.appMenu.AddRecentFile(path)
	}
	ts.State.PageOffset = 0
	ts.State.SortColumn = ""
	ts.State.SortDir = models.SortNone
	ts.sqlPanel.SetSQL(ts.State.UserSQL)
	a.titleBar.SetFileInfo(path)
	a.updateTabTitle(a.activeTab)

	cols, err := a.Duck.Schema(path)
	if err != nil {
		a.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	ts.State.Schema = cols
	ts.schema.SetSchema(cols)

	meta, _ := a.Duck.Metadata(path)
	ts.State.Metadata = meta

	a.execQuery()
}

func (a *App) execQuery() {
	ts := a.currentTab()
	if ts == nil {
		return
	}
	a.statusBar.SetStatus("Running…")
	result, err := a.Duck.Query(ts.State.VirtualSQL(), ts.State.PageOffset, ts.State.PageSize)
	if err != nil {
		a.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	ts.State.Result = result
	ts.dataGrid.SetResult(result)
	ts.dataGrid.UpdateColumnTitles(result.Columns, ts.State.SortColumn, ts.State.SortDir)
	start := ts.State.PageOffset + 1
	end := ts.State.PageOffset + len(result.Rows)
	a.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, result.Total))

	page := (ts.State.PageOffset / ts.State.PageSize) + 1
	totalPages := (int(result.Total) + ts.State.PageSize - 1) / ts.State.PageSize
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
