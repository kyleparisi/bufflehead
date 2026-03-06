package ui

import (
	"fmt"

	"parquet-viewer/internal/db"
	"parquet-viewer/internal/models"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Control"

	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/TabBar"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Vector2i"
)

// AppWindow represents a single viewer window (main or secondary).
type AppWindow struct {
	window    Window.Instance // zero for main window (uses root viewport)
	isMain    bool
	duck      *db.DB

	titleBar   *TitleBar
	toolbar    *Toolbar
	statusBar  *StatusBar
	tabBar     TabBar.Instance
	tabBarWrap MarginContainer.Instance
	split      HSplitContainer.Instance
	emptyView  VBoxContainer.Instance

	tabs      []*tabState
	activeTab int

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
	outerVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	outerVBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	outerVBox.AsControl().AddThemeConstantOverride("separation", 0)

	// Title bar
	w.titleBar = new(TitleBar)

	// Toolbar
	toolbarWrap := MarginContainer.New()
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_top", 6)
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_left", 8)
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_right", 8)
	toolbarWrap.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	w.toolbar = new(Toolbar)
	w.toolbar.OnFileOpened = w.onFileSelected
	toolbarWrap.AsNode().AddChild(w.toolbar.AsNode())

	// Tab bar
	w.tabBarWrap = MarginContainer.New()
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
	w.split.AsSplitContainer().SetSplitOffset(180)
	w.split.AsControl().AddThemeConstantOverride("separation", 1)

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
			w.execQuery()
		}
	}
	w.statusBar.OnNextPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.Result != nil && ts.State.PageOffset+ts.State.PageSize < int(ts.State.Result.Total) {
			ts.State.PageOffset += ts.State.PageSize
			w.execQuery()
		}
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
	w.emptyView.AsNode().AddChild(emptyCenter.AsNode())

	// Assemble
	outerVBox.AsNode().AddChild(w.titleBar.AsNode())
	outerVBox.AsNode().AddChild(toolbarWrap.AsNode())
	outerVBox.AsNode().AddChild(w.tabBarWrap.AsNode())
	outerVBox.AsNode().AddChild(w.split.AsNode())
	outerVBox.AsNode().AddChild(w.emptyView.AsNode())
	outerVBox.AsNode().AddChild(statusWrap.AsNode())

	bg.AsNode().AddChild(outerVBox.AsNode())

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
	ts := &tabState{State: models.NewAppState()}

	// Sidebar
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

	// Right panel
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
		w.execQuery()
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
		w.execQuery()
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
	ts.detailWrap.AsNode().AddChild(ts.detailPanel.AsNode())
	ts.detailWrap.AsCanvasItem().SetVisible(false) // hidden until row clicked

	ts.rightPanel.AsNode().AddChild(sqlWrap.AsNode())
	ts.rightPanel.AsNode().AddChild(ts.dataGrid.AsNode())

	w.split.AsNode().AddChild(ts.sidebarWrap.AsNode())
	w.split.AsNode().AddChild(ts.rightPanel.AsNode())
	w.split.AsNode().AddChild(ts.detailWrap.AsNode())

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
		ts.rightPanel.AsCanvasItem().SetVisible(false)
		ts.detailWrap.AsCanvasItem().SetVisible(false)
	}
	w.activeTab = idx
	ts := w.tabs[idx]
	ts.sidebarWrap.AsCanvasItem().SetVisible(true)
	ts.rightPanel.AsCanvasItem().SetVisible(true)
	// Only show detail if it has content
	if ts.detailPanel.columns != nil {
		ts.detailWrap.AsCanvasItem().SetVisible(true)
	}

	if ts.State.FilePath != "" {
		w.toolbar.fileLabel.SetText(ts.State.FilePath)
		w.titleBar.SetFileInfo(ts.State.FilePath)
	} else {
		w.toolbar.fileLabel.SetText("")
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
	ts.rightPanel.AsCanvasItem().SetVisible(false)
	ts.detailWrap.AsCanvasItem().SetVisible(false)
	w.split.AsNode().RemoveChild(ts.sidebarWrap.AsNode())
	w.split.AsNode().RemoveChild(ts.rightPanel.AsNode())
	w.split.AsNode().RemoveChild(ts.detailWrap.AsNode())
	ts.sidebarWrap.AsNode().QueueFree()
	ts.rightPanel.AsNode().QueueFree()
	ts.detailWrap.AsNode().QueueFree()

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
	} else {
		w.tabBar.SetTabTitle(idx, "Untitled")
	}
}

func (w *AppWindow) showEmptyView() {
	w.split.AsCanvasItem().SetVisible(false)
	w.tabBarWrap.AsCanvasItem().SetVisible(false)
	w.emptyView.AsCanvasItem().SetVisible(true)
	w.toolbar.fileLabel.SetText("")
	w.titleBar.SetFileInfo("")
	w.statusBar.SetStatus("No tabs open")
	w.statusBar.SetPage(0, 0)
}

func (w *AppWindow) showTabView() {
	w.emptyView.AsCanvasItem().SetVisible(false)
	w.split.AsCanvasItem().SetVisible(true)
	w.tabBarWrap.AsCanvasItem().SetVisible(true)
}

func (w *AppWindow) onFileSelected(path string) {
	if len(w.tabs) == 0 {
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
	ts.sqlPanel.SetSQL(ts.State.UserSQL)
	w.titleBar.SetFileInfo(path)
	w.updateTabTitle(w.activeTab)
	w.toolbar.fileLabel.SetText(path)

	cols, err := w.duck.Schema(path)
	if err != nil {
		w.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	ts.State.Schema = cols
	ts.schema.SetSchema(cols)

	meta, _ := w.duck.Metadata(path)
	ts.State.Metadata = meta
	w.execQuery()
}

func (w *AppWindow) execQuery() {
	ts := w.currentTab()
	if ts == nil {
		return
	}
	w.statusBar.SetStatus("Running…")
	result, err := w.duck.Query(ts.State.VirtualSQL(), ts.State.PageOffset, ts.State.PageSize)
	if err != nil {
		w.statusBar.SetStatus("Error: " + err.Error())
		return
	}
	ts.State.Result = result
	ts.dataGrid.SetResult(result)
	ts.dataGrid.UpdateColumnTitles(result.Columns, ts.State.SortColumn, ts.State.SortDir)
	start := ts.State.PageOffset + 1
	end := ts.State.PageOffset + len(result.Rows)
	w.statusBar.SetStatus(fmt.Sprintf("%d–%d of %d rows", start, end, result.Total))
	page := (ts.State.PageOffset / ts.State.PageSize) + 1
	totalPages := (int(result.Total) + ts.State.PageSize - 1) / ts.State.PageSize
	w.statusBar.SetPage(page, totalPages)
}

// createSecondaryWindow creates a new OS-level window with full UI.
func createSecondaryWindow(duck *db.DB, onNewWindow func()) *AppWindow {
	win := Window.New()
	win.SetTitle("Parquet Viewer")
	win.SetSize(Vector2i.New(1440, 900))
	win.SetContentScaleFactor(2.0)
	win.SetExtendToTitle(true)

	aw := &AppWindow{
		window:      win,
		isMain:      false,
		duck:        duck,
		onNewWindow: onNewWindow,
	}

	ui := aw.buildUI()
	// Need a Control root to anchor the UI
	root := Control.New()
	root.SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
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
