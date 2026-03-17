package ui

import (
	"fmt"
	"time"

	"bufflehead/internal/db"
	"bufflehead/internal/models"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/TabBar"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Vector2"
	"graphics.gd/variant/Vector2i"
)

// Connection represents an open database connection.
type Connection struct {
	Name   string
	Path   string
	DB     *db.DB
	Tables []db.TableInfo
	button Button.Instance
}

// AppWindow represents a single viewer window (main or secondary).
type AppWindow struct {
	window    Window.Instance // zero for main window (uses root viewport)
	isMain    bool
	duck      *db.DB // in-memory DuckDB for file queries
	history   *models.QueryHistory

	titleBar   *TitleBar
	// toolbar removed
	statusBar  *StatusBar
	tabBar     TabBar.Instance
	tabBarWrap MarginContainer.Instance
	split        HSplitContainer.Instance
	sidebarCol   VBoxContainer.Instance // left side of split, holds per-tab sidebars
	contentCol   VBoxContainer.Instance // right side of split, holds tabbar + per-tab content
	emptyView    VBoxContainer.Instance

	// Connection rail
	connRail      VBoxContainer.Instance
	connRailWrap  PanelContainer.Instance
	connections   []*Connection
	activeConnIdx int

	tabs      []*tabState
	activeTab int

	navWired bool

	// Callbacks
	onNewWindow func()
}

// buildUI creates the full UI tree and returns the root node.
// For the main window, this is added to the App Extension.
// For secondary windows, this is added to a Window node.
func (w *AppWindow) buildUI() PanelContainer.Instance {
	bg := PanelContainer.New()
	bg.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	bg.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	bg.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyPanelBg(bg.AsControl(), colorBg)

	outerVBox := VBoxContainer.New()
	outerVBox.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	outerVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	outerVBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	outerVBox.AsControl().AddThemeConstantOverride("separation", 0)

	// Title bar
	w.titleBar = new(TitleBar)

	// (toolbar removed)

	// Tab bar
	w.tabBarWrap = MarginContainer.New()
	w.tabBarWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.tabBarWrap.AsControl().AddThemeConstantOverride("margin_left", 8)
	w.tabBarWrap.AsControl().AddThemeConstantOverride("margin_right", 8)
	w.tabBarWrap.AsControl().AddThemeConstantOverride("margin_top", 0)
	w.tabBarWrap.AsControl().AddThemeConstantOverride("margin_bottom", 0)

	w.tabBar = TabBar.New()
	w.tabBar.SetTabCloseDisplayPolicy(TabBar.CloseButtonShowActiveOnly)
	w.tabBar.SetClipTabs(true)
	w.tabBar.SetMaxTabWidth(200)
	w.tabBar.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyTabBarTheme(w.tabBar.AsControl())

	w.tabBar.OnTabChanged(func(tab int) { w.switchTab(tab) })
	w.tabBar.OnTabClosePressed(func(tab int) { w.closeTab(tab) })
	w.tabBarWrap.AsNode().AddChild(w.tabBar.AsNode())

	// Split
	w.split = HSplitContainer.New()
	w.split.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.split.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.split.AsSplitContainer().SetSplitOffset(220)
	w.split.AsControl().AddThemeConstantOverride("separation", 1)
	w.split.AsControl().SetClipContents(true)

	// Content column (tab bar + per-tab content) — right side of outer split
	w.contentCol = VBoxContainer.New()
	w.contentCol.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.contentCol.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.contentCol.AsControl().AddThemeConstantOverride("separation", 0)
	w.contentCol.AsControl().SetClipContents(true)
	w.contentCol.AsNode().AddChild(w.tabBarWrap.AsNode())

	// Sidebar column: holds per-tab sidebars (show/hide)
	w.sidebarCol = VBoxContainer.New()
	w.sidebarCol.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.sidebarCol.AsControl().SetCustomMinimumSize(Vector2.New(100, 0))
	w.sidebarCol.AsControl().SetClipContents(true)

	// Split: sidebar (left) | content column (right)
	w.split.AsNode().AddChild(w.sidebarCol.AsNode())
	w.split.AsNode().AddChild(w.contentCol.AsNode())

	// Status bar
	statusWrap := PanelContainer.New()
	applyPanelBg(statusWrap.AsControl(), colorBgSidebar)
	statusMargin := MarginContainer.New()
	statusMargin.AsControl().AddThemeConstantOverride("margin_top", 4)
	statusMargin.AsControl().AddThemeConstantOverride("margin_left", 8)
	statusMargin.AsControl().AddThemeConstantOverride("margin_right", 8)
	statusMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	w.statusBar = new(StatusBar)
	w.statusBar.OnPrevPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.PageOffset-ts.State.PageSize >= 0 {
			ts.State.PageOffset -= ts.State.PageSize
			w.runCurrentQuery()
		}
	}
	w.statusBar.OnNextPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.Result != nil && ts.State.PageOffset+ts.State.PageSize < int(ts.State.Result.Total) {
			ts.State.PageOffset += ts.State.PageSize
			w.runCurrentQuery()
		}
	}
	w.statusBar.OnToggleLeftPane = func() {
		ts := w.currentTab()
		if ts == nil {
			return
		}
		visible := ts.sidebarWrap.AsCanvasItem().Visible()
		ts.sidebarWrap.AsCanvasItem().SetVisible(!visible)
		// Also toggle connection rail
		if len(w.connections) > 0 {
			w.connRailWrap.AsCanvasItem().SetVisible(!visible)
		}
	}
	w.statusBar.OnToggleRightPane = func() {
		ts := w.currentTab()
		if ts == nil {
			return
		}
		visible := ts.detailWrap.AsCanvasItem().Visible()
		ts.detailWrap.AsCanvasItem().SetVisible(!visible)
	}
	statusMargin.AsNode().AddChild(w.statusBar.AsNode())
	statusWrap.AsNode().AddChild(statusMargin.AsNode())

	// Empty view
	w.emptyView = VBoxContainer.New()
	w.emptyView.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.emptyView.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.emptyView.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	emptyCenter := VBoxContainer.New()
	emptyCenter.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	emptyCenter.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	emptyCenter.AsControl().AddThemeConstantOverride("separation", 16)
	emptyCenter.AsBoxContainer().SetAlignment(BoxContainer.AlignmentCenter)

	emptyIcon := Label.New()
	emptyIcon.SetText("⬡")
	emptyIcon.AsControl().AddThemeFontSizeOverride("font_size", 48)
	emptyIcon.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	emptyIcon.SetHorizontalAlignment(1)

	emptyTitle := Label.New()
	emptyTitle.SetText("Bufflehead")
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
	w.emptyView.AsNode().AddChild(emptyCenter.AsNode())

	// Connection rail (far-left column)
	w.connRailWrap = PanelContainer.New()
	w.connRailWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.connRailWrap.AsControl().SetCustomMinimumSize(Vector2.New(36, 0))
	applyPanelBg(w.connRailWrap.AsControl(), colorBgDarker)

	railMargin := MarginContainer.New()
	railMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	railMargin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	railMargin.AsControl().AddThemeConstantOverride("margin_top", 4)
	railMargin.AsControl().AddThemeConstantOverride("margin_left", 4)
	railMargin.AsControl().AddThemeConstantOverride("margin_right", 4)
	railMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	w.connRail = VBoxContainer.New()
	w.connRail.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.connRail.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.connRail.AsControl().AddThemeConstantOverride("separation", 4)

	railMargin.AsNode().AddChild(w.connRail.AsNode())
	w.connRailWrap.AsNode().AddChild(railMargin.AsNode())

	// Memory connection is always index 0
	memBtn := Button.New()
	memBtn.SetText("mem")
	memBtn.AsControl().AddThemeFontSizeOverride("font_size", 10)
	memBtn.AsControl().SetCustomMinimumSize(Vector2.New(36, 36))
	memBtn.SetClipText(true)
	applyActiveButtonTheme(memBtn.AsControl()) // active by default
	memConn := &Connection{
		Name:   "Memory",
		Path:   ":memory:",
		DB:     w.duck,
		Tables: nil,
		button: memBtn,
	}
	w.connections = append(w.connections, memConn)
	w.connRail.AsNode().AddChild(memBtn.AsNode())
	w.activeConnIdx = 0
	memBtn.AsBaseButton().OnPressed(func() {
		w.selectConnection(0)
	})

	// Content area (rail | main)
	contentHBox := HBoxContainer.New()
	contentHBox.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	contentHBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	contentHBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	contentHBox.AsControl().AddThemeConstantOverride("separation", 0)
	contentHBox.AsNode().AddChild(w.connRailWrap.AsNode())
	contentHBox.AsNode().AddChild(w.split.AsNode())
	contentHBox.AsNode().AddChild(w.emptyView.AsNode())

	// Assemble — tab bar is added per-tab inside rightPanel now
	outerVBox.AsNode().AddChild(w.titleBar.AsNode())
	outerVBox.AsNode().AddChild(contentHBox.AsNode())
	outerVBox.AsNode().AddChild(statusWrap.AsNode())

	bg.AsNode().AddChild(outerVBox.AsNode())

	// Wire nav buttons (buttons created in TitleBar.Ready, wired here)
	// These will be connected after the node enters the scene tree,
	// so we defer the connection to the first addNewTab call via a flag.
	w.navWired = false

	// Don't create tabs here — caller must call addNewTab() after adding bg to tree
	return bg
}

func (w *AppWindow) currentTab() *tabState {
	if w.activeTab >= 0 && w.activeTab < len(w.tabs) {
		return w.tabs[w.activeTab]
	}
	return nil
}

func (w *AppWindow) currentState() *models.AppState {
	if ts := w.currentTab(); ts != nil {
		return ts.State
	}
	return nil
}

func (w *AppWindow) addNewTab() {
	// Wire nav buttons on first tab creation (after TitleBar.Ready has run)
	if !w.navWired {
		w.titleBar.NavBackBtn.AsBaseButton().OnPressed(func() { w.navBack() })
		w.titleBar.NavFwdBtn.AsBaseButton().OnPressed(func() { w.navForward() })
		w.navWired = true
	}

	ts := &tabState{State: models.NewAppState(), connIdx: 0} // default to Memory connection

	// Sidebar
	ts.sidebarWrap = PanelContainer.New()
	ts.sidebarWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.sidebarWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.sidebarWrap.AsControl().SetCustomMinimumSize(Vector2.New(100, 0))
	ts.sidebarWrap.AsControl().SetClipContents(true)
	applyPanelBg(ts.sidebarWrap.AsControl(), colorBgSidebar)
	sidebarMargin := MarginContainer.New()
	sidebarMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sidebarMargin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_top", 6)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_left", 6)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_right", 4)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	sidebarVBox := VBoxContainer.New()
	sidebarVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sidebarVBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	sidebarVBox.AsControl().AddThemeConstantOverride("separation", 4)

	// Tab selector: Items | History (TablePlus-style)
	selectorRow := HBoxContainer.New()
	selectorRow.AsControl().AddThemeConstantOverride("separation", 0)

	schemaBtn := Button.New()
	schemaBtn.SetText("Items")
	schemaBtn.AsControl().AddThemeFontSizeOverride("font_size", 11)
	schemaBtn.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applySidebarTabTheme(schemaBtn.AsControl(), true)

	historyBtn := Button.New()
	historyBtn.SetText("History")
	historyBtn.AsControl().AddThemeFontSizeOverride("font_size", 11)
	historyBtn.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applySidebarTabTheme(historyBtn.AsControl(), false)

	selectorRow.AsNode().AddChild(schemaBtn.AsNode())
	selectorRow.AsNode().AddChild(historyBtn.AsNode())

	ts.schema = new(SchemaPanel)
	ts.schema.OnColumnsChanged = func(selected []string) {
		ts.State.SelectedCols = selected
		ts.State.PageOffset = 0
		w.runCurrentQuery()
	}
	ts.historyPanel = new(HistoryPanel)
	ts.historyPanel.OnReplay = func(sql string) {
		ts.State.UserSQL = sql
		ts.State.PageOffset = 0
		ts.State.SortColumn = ""
		ts.State.SortDir = models.SortNone
		ts.sqlPanel.SetSQL(sql)
		w.execQuery()
	}

	// Start showing schema, hide history
	ts.historyPanel.AsCanvasItem().SetVisible(false)

	schemaBtn.AsBaseButton().OnPressed(func() {
		ts.schema.AsCanvasItem().SetVisible(true)
		ts.historyPanel.AsCanvasItem().SetVisible(false)
		applySidebarTabTheme(schemaBtn.AsControl(), true)
		applySidebarTabTheme(historyBtn.AsControl(), false)
	})
	historyBtn.AsBaseButton().OnPressed(func() {
		ts.schema.AsCanvasItem().SetVisible(false)
		ts.historyPanel.AsCanvasItem().SetVisible(true)
		applySidebarTabTheme(schemaBtn.AsControl(), false)
		applySidebarTabTheme(historyBtn.AsControl(), true)
		if w.history != nil {
			ts.historyPanel.SetHistory(w.history.All())
		}
	})

	sidebarVBox.AsNode().AddChild(selectorRow.AsNode())
	sidebarVBox.AsNode().AddChild(ts.schema.AsNode())
	sidebarVBox.AsNode().AddChild(ts.historyPanel.AsNode())
	sidebarMargin.AsNode().AddChild(sidebarVBox.AsNode())
	ts.sidebarWrap.AsNode().AddChild(sidebarMargin.AsNode())

	// Right panel
	ts.rightPanel = VBoxContainer.New()
	ts.rightPanel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.rightPanel.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.rightPanel.AsControl().AddThemeConstantOverride("separation", 1)
	ts.rightPanel.AsControl().SetCustomMinimumSize(Vector2.New(200, 0)) // min width for data grid
	ts.rightPanel.AsControl().SetClipContents(true)

	ts.sqlPanel = new(SQLPanel)
	ts.sqlPanel.OnRunQuery = func(sql string) {
		ts.State.UserSQL = sql
		ts.State.PageOffset = 0
		ts.State.SortColumn = ""
		ts.State.SortDir = models.SortNone
		w.runCurrentQuery()
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
		w.runCurrentQuery()
	}

	ts.dataGrid.OnRowSelected = func(rowIndex int) {
		if rowIndex < len(ts.dataGrid.rows) {
			ts.detailPanel.SetRow(ts.dataGrid.columns, ts.dataGrid.rows[rowIndex])
			ts.detailWrap.AsCanvasItem().SetVisible(true)
		}
	}

	// Detail panel (third column)
	ts.detailPanel = new(RowDetailPanel)
	ts.detailWrap = PanelContainer.New()
	applyPanelBg(ts.detailWrap.AsControl(), colorBgSidebar)
	ts.detailWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.detailWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.detailWrap.AsControl().SetCustomMinimumSize(Vector2.New(150, 0)) // min width for detail
	detailMargin := MarginContainer.New()
	detailMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	detailMargin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	detailMargin.AsControl().AddThemeConstantOverride("margin_top", 6)
	detailMargin.AsControl().AddThemeConstantOverride("margin_left", 6)
	detailMargin.AsControl().AddThemeConstantOverride("margin_right", 6)
	detailMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)
	detailMargin.AsNode().AddChild(ts.detailPanel.AsNode())
	ts.detailWrap.AsNode().AddChild(detailMargin.AsNode())
	ts.detailWrap.AsCanvasItem().SetVisible(false) // hidden until row clicked

	ts.rightPanel.AsNode().AddChild(sqlWrap.AsNode())
	ts.rightPanel.AsNode().AddChild(ts.dataGrid.AsNode())

	// Inner split: content | detail
	ts.outerWrap = HSplitContainer.New()
	ts.outerWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.outerWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.outerWrap.AsControl().AddThemeConstantOverride("separation", 1)
	ts.outerWrap.AsControl().SetClipContents(true)
	ts.outerWrap.AsSplitContainer().SetSplitOffset(-200) // negative = from right edge
	ts.outerWrap.AsNode().AddChild(ts.rightPanel.AsNode())
	ts.outerWrap.AsNode().AddChild(ts.detailWrap.AsNode())

	// Sidebar goes into sidebarCol, content goes into contentCol
	w.sidebarCol.AsNode().AddChild(ts.sidebarWrap.AsNode())
	w.contentCol.AsNode().AddChild(ts.outerWrap.AsNode())

	w.showTabView()

	idx := len(w.tabs)
	w.tabs = append(w.tabs, ts)
	w.tabBar.AddTab()
	w.tabBar.SetTabTitle(idx, "Untitled")
	w.tabBar.SetCurrentTab(idx)
	w.switchTab(idx)
}

func (w *AppWindow) switchTab(idx int) {
	if idx < 0 || idx >= len(w.tabs) {
		return
	}
	for _, ts := range w.tabs {
		ts.sidebarWrap.AsCanvasItem().SetVisible(false)
		ts.outerWrap.AsCanvasItem().SetVisible(false)
	}
	w.activeTab = idx
	ts := w.tabs[idx]
	ts.sidebarWrap.AsCanvasItem().SetVisible(true)
	ts.outerWrap.AsCanvasItem().SetVisible(true)

	// Update connection rail highlight
	if ts.connIdx >= 0 && ts.connIdx < len(w.connections) {
		w.activeConnIdx = ts.connIdx
		for i, c := range w.connections {
			if i == ts.connIdx {
				applyActiveButtonTheme(c.button.AsControl())
			} else {
				applySecondaryButtonTheme(c.button.AsControl())
			}
		}
		w.titleBar.SetFileInfo(w.connections[ts.connIdx].Path)
	}

	if ts.State.FilePath != "" {
		w.titleBar.SetFileInfo(ts.State.FilePath)
	} else if ts.connIdx == 0 {
		w.titleBar.SetFileInfo("")
	}

	if ts.State.Result != nil {
		start := ts.State.PageOffset + 1
		end := ts.State.PageOffset + len(ts.State.Result.Rows)
		w.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, ts.State.Result.Total))
		page := (ts.State.PageOffset / ts.State.PageSize) + 1
		totalPages := (int(ts.State.Result.Total) + ts.State.PageSize - 1) / ts.State.PageSize
		w.statusBar.SetPage(page, totalPages)
	} else {
		w.statusBar.SetStatus("Ready")
		w.statusBar.SetPage(1, 1)
	}
}

func (w *AppWindow) closeTab(idx int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("closeTab recovered:", r)
		}
	}()

	if idx < 0 || idx >= len(w.tabs) {
		return
	}

	ts := w.tabs[idx]
	ts.sidebarWrap.AsCanvasItem().SetVisible(false)
	ts.outerWrap.AsCanvasItem().SetVisible(false)
	w.sidebarCol.AsNode().RemoveChild(ts.sidebarWrap.AsNode())
	w.contentCol.AsNode().RemoveChild(ts.outerWrap.AsNode())
	ts.sidebarWrap.AsNode().QueueFree()
	ts.outerWrap.AsNode().QueueFree()

	w.tabs = append(w.tabs[:idx], w.tabs[idx+1:]...)
	w.tabBar.RemoveTab(idx)

	if len(w.tabs) == 0 {
		w.activeTab = -1
		w.showEmptyView()
		return
	}

	if w.activeTab >= len(w.tabs) {
		w.activeTab = len(w.tabs) - 1
	}
	w.tabBar.SetCurrentTab(w.activeTab)
	w.switchTab(w.activeTab)
}

func (w *AppWindow) updateTabTitle(idx int) {
	if idx < 0 || idx >= len(w.tabs) {
		return
	}
	ts := w.tabs[idx]
	if ts.State.FilePath != "" {
		name := ts.State.FilePath
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '/' {
				name = name[i+1:]
				break
			}
		}
		w.tabBar.SetTabTitle(idx, name)
	} else if ts.connIdx > 0 && ts.connIdx < len(w.connections) {
		w.tabBar.SetTabTitle(idx, w.connections[ts.connIdx].Name)
	} else {
		w.tabBar.SetTabTitle(idx, "Untitled")
	}
}

func (w *AppWindow) showEmptyView() {
	w.split.AsCanvasItem().SetVisible(false)
	w.tabBarWrap.AsCanvasItem().SetVisible(false)
	w.emptyView.AsCanvasItem().SetVisible(true)
	w.titleBar.SetFileInfo("")
	w.statusBar.SetStatus("No tabs open")
	w.statusBar.SetPage(0, 0)
}

func (w *AppWindow) showTabView() {
	w.emptyView.AsCanvasItem().SetVisible(false)
	w.split.AsCanvasItem().SetVisible(true)
	w.tabBarWrap.AsCanvasItem().SetVisible(true)
}

func (w *AppWindow) isDuckDBFile(path string) bool {
	for _, ext := range []string{".duckdb", ".db", ".ddb"} {
		if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}
	return false
}

func (w *AppWindow) onFileSelected(path string) {
	if w.isDuckDBFile(path) {
		w.onDatabaseOpened(path)
		return
	}
	if len(w.tabs) == 0 {
		w.addNewTab()
	} else if ts := w.currentTab(); ts != nil && ts.State.FilePath != "" {
		// Current tab has a file — open in new tab
		w.addNewTab()
	}
	ts := w.currentTab()
	if ts == nil {
		return
	}
	ts.State.FilePath = path
	ts.State.UserSQL = db.DefaultQuery(path)
	ts.State.PageOffset = 0
	ts.State.SortColumn = ""
	ts.State.SortDir = models.SortNone
	ts.State.SelectedCols = nil
	ts.sqlPanel.SetSQL(ts.State.UserSQL)
	w.titleBar.SetFileInfo(path)
	w.updateTabTitle(w.activeTab)

	cols, err := w.duck.Schema(path)
	if err != nil {
		w.statusBar.SetStatus("Error: " + err.Error())
		ts.dataGrid.ShowError(err.Error())
		return
	}
	ts.State.Schema = cols
	ts.schema.SetSchema(cols)

	meta, _ := w.duck.Metadata(path)
	ts.State.Metadata = meta
	w.execQuery()
}

// runCurrentQuery uses the right DB connection for the active tab.
func (w *AppWindow) runCurrentQuery() error {
	ts := w.currentTab()
	if ts == nil {
		return fmt.Errorf("no active tab")
	}
	if ts.connIdx >= 0 && ts.connIdx < len(w.connections) {
		return w.execQueryWithConn(ts, w.connections[ts.connIdx].DB)
	}
	return w.execQuery()
}

func (w *AppWindow) onDatabaseOpened(path string) {
	// Open a read-only connection
	dbConn, err := db.OpenDB(path)
	if err != nil {
		w.statusBar.SetStatus("Error: " + err.Error())
		if ts := w.currentTab(); ts != nil {
			ts.dataGrid.ShowError(err.Error())
		}
		return
	}

	// Load tables
	tables, err := dbConn.Tables()
	if err != nil {
		dbConn.Close()
		w.statusBar.SetStatus("Error listing tables: " + err.Error())
		return
	}
	for i := range tables {
		cols, _ := dbConn.TableSchema(tables[i].Name)
		tables[i].Columns = cols
	}

	// Extract short name from path
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			name = path[i+1:]
			break
		}
	}

	// Create connection
	conn := &Connection{
		Name:   name,
		Path:   path,
		DB:     dbConn,
		Tables: tables,
	}

	// Add button to rail
	btn := Button.New()
	btn.SetText(name)
	btn.AsControl().AddThemeFontSizeOverride("font_size", 10)
	btn.AsControl().SetCustomMinimumSize(Vector2.New(36, 36))
	btn.SetClipText(true)
	applySecondaryButtonTheme(btn.AsControl())
	conn.button = btn

	idx := len(w.connections)
	w.connections = append(w.connections, conn)
	w.connRail.AsNode().AddChild(btn.AsNode())
	w.connRailWrap.AsCanvasItem().SetVisible(true)

	btn.AsBaseButton().OnPressed(func() {
		w.selectConnection(idx)
	})

	// Create a new tab bound to this connection
	w.addNewTab()
	ts := w.currentTab()
	if ts != nil {
		ts.State.IsDatabase = true
		ts.connIdx = idx
		ts.schema.SetTables(conn.Tables)
		ts.schema.OnTableClicked = func(tableName string) {
			ts.State.ActiveTable = tableName
			ts.State.UserSQL = fmt.Sprintf("SELECT * FROM \"%s\"", tableName)
			ts.State.PageOffset = 0
			ts.State.SortColumn = ""
			ts.State.SortDir = models.SortNone
			ts.sqlPanel.SetSQL(ts.State.UserSQL)
			w.runCurrentQuery()
		}
	}

	// Highlight this connection in the rail
	w.selectConnection(idx)

	w.statusBar.SetStatus(fmt.Sprintf("Connected: %s (%d tables/views)", name, len(tables)))
}

func (w *AppWindow) selectConnection(idx int) {
	if idx < 0 || idx >= len(w.connections) {
		return
	}
	w.activeConnIdx = idx

	// Highlight active button
	for i, c := range w.connections {
		if i == idx {
			applyActiveButtonTheme(c.button.AsControl())
		} else {
			applySecondaryButtonTheme(c.button.AsControl())
		}
	}

	// Find the first tab bound to this connection and switch to it
	for i, ts := range w.tabs {
		if ts.connIdx == idx {
			w.tabBar.SetCurrentTab(i)
			return
		}
	}
}

// execQueryWithConn runs a query using a specific database connection.
func (w *AppWindow) execQueryWithConn(ts *tabState, conn *db.DB) error {
	if ts == nil || conn == nil {
		return fmt.Errorf("no active tab or connection")
	}
	w.statusBar.SetStatus("Running…")
	queryStart := time.Now()
	result, err := conn.Query(ts.State.VirtualSQL(), ts.State.PageOffset, ts.State.PageSize)
	if err != nil {
		w.statusBar.SetStatus("Error: " + err.Error())
		ts.dataGrid.ShowError(err.Error())
		return err
	}
	elapsed := time.Since(queryStart)
	ts.State.Result = result

	if !ts.navigating {
		if w.history != nil {
			w.history.Add(models.HistoryEntry{
				SQL:        ts.State.VirtualSQL(),
				FilePath:   ts.State.FilePath,
				Timestamp:  time.Now(),
				RowCount:   result.Total,
				DurationMs: elapsed.Milliseconds(),
			})
		}
		ts.State.NavPush(ts.State.UserSQL)
	}
	ts.dataGrid.colTypes = schemaColTypes(ts.State.Schema, result.Columns)
	ts.dataGrid.SetResult(result)
	ts.dataGrid.UpdateColumnTitles(result.Columns, ts.State.SortColumn, ts.State.SortDir)
	start := ts.State.PageOffset + 1
	end := ts.State.PageOffset + len(result.Rows)
	w.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, result.Total))
	page := (ts.State.PageOffset / ts.State.PageSize) + 1
	totalPages := (int(result.Total) + ts.State.PageSize - 1) / ts.State.PageSize
	w.statusBar.SetPage(page, totalPages)
	w.updateNavButtons()
	return nil
}

func (w *AppWindow) execQuery() error {
	ts := w.currentTab()
	if ts == nil {
		return fmt.Errorf("no active tab")
	}
	w.statusBar.SetStatus("Running…")
	queryStart := time.Now()
	result, err := w.duck.Query(ts.State.VirtualSQL(), ts.State.PageOffset, ts.State.PageSize)
	if err != nil {
		w.statusBar.SetStatus("Error: " + err.Error())
		ts.dataGrid.ShowError(err.Error())
		return err
	}
	elapsed := time.Since(queryStart)
	ts.State.Result = result

	if !ts.navigating {
		if w.history != nil {
			w.history.Add(models.HistoryEntry{
				SQL:        ts.State.VirtualSQL(),
				FilePath:   ts.State.FilePath,
				Timestamp:  time.Now(),
				RowCount:   result.Total,
				DurationMs: elapsed.Milliseconds(),
			})
		}
		ts.State.NavPush(ts.State.UserSQL)
	}
	ts.dataGrid.colTypes = schemaColTypes(ts.State.Schema, result.Columns)
	ts.dataGrid.SetResult(result)
	ts.dataGrid.UpdateColumnTitles(result.Columns, ts.State.SortColumn, ts.State.SortDir)
	start := ts.State.PageOffset + 1
	end := ts.State.PageOffset + len(result.Rows)
	w.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, result.Total))
	page := (ts.State.PageOffset / ts.State.PageSize) + 1
	totalPages := (int(result.Total) + ts.State.PageSize - 1) / ts.State.PageSize
	w.statusBar.SetPage(page, totalPages)
	w.updateNavButtons()
	return nil
}

func (w *AppWindow) updateNavButtons() {
	ts := w.currentTab()
	if ts == nil {
		w.titleBar.NavBackBtn.AsBaseButton().SetDisabled(true)
		w.titleBar.NavFwdBtn.AsBaseButton().SetDisabled(true)
		return
	}
	w.titleBar.NavBackBtn.AsBaseButton().SetDisabled(!ts.State.CanNavBack())
	w.titleBar.NavFwdBtn.AsBaseButton().SetDisabled(!ts.State.CanNavForward())
}

func (w *AppWindow) navBack() {
	ts := w.currentTab()
	if ts == nil {
		return
	}
	entry, ok := ts.State.NavBack()
	if !ok {
		return
	}
	ts.navigating = true
	ts.State.UserSQL = entry.SQL
	ts.State.SortColumn = entry.SortColumn
	ts.State.SortDir = entry.SortDir
	ts.State.PageOffset = entry.PageOffset
	ts.sqlPanel.SetSQL(entry.SQL)
	w.runCurrentQuery()
	ts.navigating = false
	w.updateNavButtons()
}

func (w *AppWindow) navForward() {
	ts := w.currentTab()
	if ts == nil {
		return
	}
	entry, ok := ts.State.NavForward()
	if !ok {
		return
	}
	ts.navigating = true
	ts.State.UserSQL = entry.SQL
	ts.State.SortColumn = entry.SortColumn
	ts.State.SortDir = entry.SortDir
	ts.State.PageOffset = entry.PageOffset
	ts.sqlPanel.SetSQL(entry.SQL)
	w.runCurrentQuery()
	ts.navigating = false
	w.updateNavButtons()
}

// schemaColTypes maps result columns to their schema types.
func schemaColTypes(schema []db.Column, resultCols []string) []string {
	typeMap := make(map[string]string, len(schema))
	for _, c := range schema {
		typeMap[c.Name] = c.DataType
	}
	types := make([]string, len(resultCols))
	for i, col := range resultCols {
		types[i] = typeMap[col]
	}
	return types
}

// createSecondaryWindow creates a new OS-level window with full UI.
func createSecondaryWindow(duck *db.DB, history *models.QueryHistory, onNewWindow func()) *AppWindow {
	win := Window.New()
	win.SetTitle("Bufflehead")
	win.SetSize(Vector2i.New(1440, 900))
	win.SetMinSize(Vector2i.New(1100, 720))
	// Let Godot/macOS handle per-display DPI. Manually multiplying by
	// ScreenGetScale() makes the entire UI oversized on some displays
	// (for example "looks like 1920x1080" setups), which can push the
	// title pill off-screen and effectively clip the main content.
	win.SetContentScaleFactor(Float.X(1.0))
	// Custom title-bar extension behaves inconsistently across Macs.
	// Keep a normal native title bar for now and render our app chrome below it.
	win.SetExtendToTitle(false)

	aw := &AppWindow{
		window:      win,
		isMain:      false,
		duck:        duck,
		history:     history,
		onNewWindow: onNewWindow,
	}

	ui := aw.buildUI()
	// Need a Control root to anchor the UI
	root := Control.New()
	root.SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	ui.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	root.AsNode().AddChild(ui.AsNode())
	win.AsNode().AddChild(root.AsNode())



	// Note: caller must call aw.addNewTab() after adding window to scene tree

	// Setup drag & drop for this window
	win.OnFilesDropped(func(files []string) {
		for _, f := range files {
			if len(f) > 8 && f[len(f)-8:] == ".parquet" {
				aw.onFileSelected(f)
				return
			}
		}
		if len(files) > 0 {
			aw.onFileSelected(files[0])
		}
	})

	return aw
}
