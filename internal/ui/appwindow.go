package ui

import (
	"fmt"
	"strings"
	"time"

	bfaws "bufflehead/internal/aws"
	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"bufflehead/internal/models"

	"graphics.gd/variant/Color"

	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/ConfirmationDialog"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/PopupMenu"
	"graphics.gd/classdb/TabBar"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/VSplitContainer"
	"graphics.gd/classdb/Window"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
	"graphics.gd/variant/Vector2i"
)

// Connection represents an open database connection.
type Connection struct {
	Name    string
	Path    string
	DB      db.Querier
	Tables  []db.TableInfo
	button  Button.Instance
	worker  *ConnWorker
	Gateway *GatewayConnection // nil for local connections
}

// GatewayConnection holds the gateway-specific state for a remote connection.
type GatewayConnection struct {
	Config        models.GatewayEntry
	Auth          *bfaws.AuthManager
	Tunnel        *bfaws.TunnelManager
	LastTunnelMsg string // tracks last displayed tunnel status to avoid redundant updates
}

var nextTabID uint64

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
	connList      VBoxContainer.Instance // holds the connection buttons (mem + added)
	connRailWrap  PanelContainer.Instance
	connections   []*Connection
	activeConnIdx int

	tabs      []*tabState
	activeTab int
	results   chan DBResult
	skipPoll  bool // skip one frame of result polling so "Running…" renders

	navWired bool

	// Gateway
	pendingGateway      *GatewayConnection
	gatewayLoadingLabel Label.Instance
	gatewayTracker      *stepTracker // connection-status step tracker on the loading screen
	gatewayLoadingMsg   string       // set by background goroutine, read by Process

	// Extension install/load result, set by a background goroutine and applied
	// on the main thread in Process (mirrors the gatewayLoadingMsg pattern).
	extActionMsg *extActionResult
	extActionTab *tabState

	// Control server address for AI prompt
	controlAddr string

	// Callbacks
	onNewWindow     func()
	onReLogin       func() // opens the gateway/SSO screen to re-authenticate
	onNewConnection func() // opens the gateway screen to add a new connection

	reLoginDialogOpen bool // guards against stacking re-login dialogs
}

// buildUI creates the full UI tree and returns the root node.
// For the main window, this is added to the App Extension.
// For secondary windows, this is added to a Window node.
func (w *AppWindow) buildUI() PanelContainer.Instance {
	w.results = make(chan DBResult, 16)

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

	// Tab bar - slightly more breathing room
	w.tabBarWrap = MarginContainer.New()
	w.tabBarWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.tabBarWrap.AsControl().AddThemeConstantOverride("margin_left", 12)
	w.tabBarWrap.AsControl().AddThemeConstantOverride("margin_right", 12)
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

	// Tab row: [tabs ............] [+ new tab] [⧉ new window]
	// The +/⧉ buttons are visible, cross-platform replacements for the
	// macOS-only "New Tab" / "New Window" menu items.
	tabRow := HBoxContainer.New()
	tabRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	tabRow.AsControl().AddThemeConstantOverride("separation", 4)
	tabRow.AsNode().AddChild(w.tabBar.AsNode())

	newTabBtn := Button.New()
	newTabBtn.AsNode().SetName("NewTabButton")
	newTabBtn.SetText("+")
	newTabBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(16))
	newTabBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(26), 0))
	newTabBtn.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	newTabBtn.AsControl().SetTooltipText("New Tab")
	applySecondaryButtonTheme(newTabBtn.AsControl())
	newTabBtn.AsBaseButton().OnPressed(func() { w.addNewTab() })
	tabRow.AsNode().AddChild(newTabBtn.AsNode())

	newWindowBtn := Button.New()
	newWindowBtn.AsNode().SetName("NewWindowButton")
	newWindowBtn.SetText("⧉")
	newWindowBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(14))
	newWindowBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(26), 0))
	newWindowBtn.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	newWindowBtn.AsControl().SetTooltipText("New Window")
	applySecondaryButtonTheme(newWindowBtn.AsControl())
	newWindowBtn.AsBaseButton().OnPressed(func() {
		if w.onNewWindow != nil {
			w.onNewWindow()
		}
	})
	tabRow.AsNode().AddChild(newWindowBtn.AsNode())

	w.tabBarWrap.AsNode().AddChild(tabRow.AsNode())

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
	w.sidebarCol.AsControl().SetCustomMinimumSize(Vector2.New(scaled(100), 0))
	w.sidebarCol.AsControl().SetClipContents(true)

	// Split: sidebar (left) | content column (right)
	w.split.AsNode().AddChild(w.sidebarCol.AsNode())
	w.split.AsNode().AddChild(w.contentCol.AsNode())

	// Status bar - more padding for breathing room
	statusWrap := PanelContainer.New()
	// Darker footer surface with a thin top border (surface-container-lowest).
	statusSB := makeStyleBox(colorBgDarker, 0, 0, colorBgDarker)
	statusSB.SetBorderWidthTop(1)
	statusSB.SetBorderColor(colorBorder)
	statusWrap.AsControl().AddThemeStyleboxOverride("panel", statusSB.AsStyleBox())
	statusMargin := MarginContainer.New()
	statusMargin.AsControl().AddThemeConstantOverride("margin_top", 4)
	statusMargin.AsControl().AddThemeConstantOverride("margin_left", 10)
	statusMargin.AsControl().AddThemeConstantOverride("margin_right", 10)
	statusMargin.AsControl().AddThemeConstantOverride("margin_bottom", 4)

	w.statusBar = new(StatusBar)
	w.statusBar.OnPrevPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.PageOffset-ts.State.PageSize >= 0 {
			ts.State.PageOffset -= ts.State.PageSize
			w.runCurrentQuery(nil)
		}
	}
	w.statusBar.OnNextPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.Result != nil && ts.State.PageOffset+ts.State.PageSize < int(ts.State.Result.Total) {
			ts.State.PageOffset += ts.State.PageSize
			w.runCurrentQuery(nil)
		}
	}
	w.statusBar.OnFirstPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.PageOffset > 0 {
			ts.State.PageOffset = 0
			w.runCurrentQuery(nil)
		}
	}
	w.statusBar.OnLastPage = func() {
		ts := w.currentTab()
		if ts != nil && ts.State.Result != nil && ts.State.PageSize > 0 {
			last := ((int(ts.State.Result.Total) - 1) / ts.State.PageSize) * ts.State.PageSize
			if last < 0 {
				last = 0
			}
			if last != ts.State.PageOffset {
				ts.State.PageOffset = last
				w.runCurrentQuery(nil)
			}
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
	emptyIcon.AsControl().AddThemeFontSizeOverride("font_size", fontSize(48))
	emptyIcon.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	emptyIcon.SetHorizontalAlignment(1)

	emptyTitle := Label.New()
	emptyTitle.SetText("Bufflehead")
	emptyTitle.AsControl().AddThemeFontSizeOverride("font_size", fontSize(18))
	emptyTitle.AsControl().AddThemeColorOverride("font_color", colorText)
	emptyTitle.SetHorizontalAlignment(1)

	emptyHint := Label.New()
	emptyHint.SetText("＋  New Connection (left rail)   ·   Open a file   ·   Drop .parquet here")
	emptyHint.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	emptyHint.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	emptyHint.SetHorizontalAlignment(1)

	emptyCenter.AsNode().AddChild(emptyIcon.AsNode())
	emptyCenter.AsNode().AddChild(emptyTitle.AsNode())
	emptyCenter.AsNode().AddChild(emptyHint.AsNode())
	w.emptyView.AsNode().AddChild(emptyCenter.AsNode())

	// Connection rail (far-left column)
	w.connRailWrap = PanelContainer.New()
	w.connRailWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	w.connRailWrap.AsControl().SetCustomMinimumSize(Vector2.New(scaled(48), 0))
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

	// connList holds the connection buttons; a bottom-pinned "+" adds new ones.
	w.connList = VBoxContainer.New()
	w.connList.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	w.connList.AsControl().AddThemeConstantOverride("separation", 4)

	railMargin.AsNode().AddChild(w.connRail.AsNode())
	w.connRailWrap.AsNode().AddChild(railMargin.AsNode())
	w.connRail.AsNode().AddChild(w.connList.AsNode())

	// Memory connection is always index 0
	memBtn := Button.New()
	memBtn.SetText(models.ConnBadge("Memory"))
	memBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(40), scaled(40)))
	memBtn.SetClipText(true)
	applyConnTileTheme(memBtn.AsControl(), true) // active by default
	memWorker := NewConnWorker(w.duck, w.results)
	memWorker.Start()
	memConn := &Connection{
		Name:   "Memory",
		Path:   ":memory:",
		DB:     w.duck,
		Tables: nil,
		button: memBtn,
		worker: memWorker,
	}
	w.connections = append(w.connections, memConn)
	w.connList.AsNode().AddChild(memBtn.AsNode())
	w.activeConnIdx = 0
	w.wireConnButton(memBtn, 0)

	// Spacer pushes the "New Connection" button to the bottom of the rail.
	railSpacer := Control.New()
	railSpacer.SetSizeFlagsVertical(Control.SizeExpandFill)
	w.connRail.AsNode().AddChild(railSpacer.AsNode())

	// New Connection ("+") — visible, cross-platform replacement for the
	// macOS-only "Connect to Gateway…" / "Open…" menu items.
	newConnBtn := Button.New()
	newConnBtn.AsNode().SetName("NewConnectionButton")
	newConnBtn.SetText("+")
	newConnBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(18))
	newConnBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(36), scaled(36)))
	newConnBtn.AsControl().SetTooltipText("New Connection")
	applySecondaryButtonTheme(newConnBtn.AsControl())
	newConnBtn.AsBaseButton().OnPressed(func() {
		w.showNewConnectionMenu(newConnBtn)
	})
	w.connRail.AsNode().AddChild(newConnBtn.AsNode())

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
		w.titleBar.OnRefresh = func() {
			w.refreshConnection(w.activeConnIdx)
		}
		w.titleBar.OnRun = func() {
			if ts := w.currentTab(); ts != nil && ts.sqlPanel != nil {
				ts.sqlPanel.Run()
			}
		}
		w.navWired = true
	}

	tid := nextTabID
	nextTabID++
	ts := &tabState{State: models.NewAppState(), connIdx: 0, tabID: tid} // default to Memory connection

	// Sidebar - 12px horizontal, 8px vertical padding
	ts.sidebarWrap = PanelContainer.New()
	ts.sidebarWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.sidebarWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.sidebarWrap.AsControl().SetCustomMinimumSize(Vector2.New(scaled(100), 0))
	ts.sidebarWrap.AsControl().SetClipContents(true)
	applyPanelBg(ts.sidebarWrap.AsControl(), colorBgSidebar)
	sidebarMargin := MarginContainer.New()
	sidebarMargin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sidebarMargin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_top", 8)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_left", 12)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_right", 12)
	sidebarMargin.AsControl().AddThemeConstantOverride("margin_bottom", 8)

	sidebarVBox := VBoxContainer.New()
	sidebarVBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sidebarVBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	sidebarVBox.AsControl().AddThemeConstantOverride("separation", 8)

	// Tab selector: Items | History (TablePlus-style)
	selectorRow := HBoxContainer.New()
	selectorRow.AsControl().AddThemeConstantOverride("separation", 0)

	schemaBtn := Button.New()
	schemaBtn.SetText("Schema")
	schemaBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	schemaBtn.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applySidebarTabTheme(schemaBtn.AsControl(), true)
	ts.schemaBtn = schemaBtn

	historyBtn := Button.New()
	historyBtn.SetText("History")
	historyBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	historyBtn.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applySidebarTabTheme(historyBtn.AsControl(), false)
	ts.historyBtn = historyBtn

	extBtn := Button.New()
	extBtn.SetText("Extensions")
	extBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	extBtn.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applySidebarTabTheme(extBtn.AsControl(), false)
	ts.extBtn = extBtn

	selectorRow.AsNode().AddChild(schemaBtn.AsNode())
	selectorRow.AsNode().AddChild(historyBtn.AsNode())
	selectorRow.AsNode().AddChild(extBtn.AsNode())

	ts.schema = new(SchemaPanel)
	ts.schema.OnColumnsChanged = func(selected []string) {
		ts.State.SelectedCols = selected
		ts.State.PageOffset = 0
		w.runCurrentQuery(nil)
	}
	ts.historyPanel = new(HistoryPanel)
	ts.historyPanel.OnReplay = func(sql string) {
		ts.State.UserSQL = sql
		ts.State.PageOffset = 0
		ts.State.SortColumn = ""
		ts.State.SortDir = models.SortNone
		ts.sqlPanel.SetSQL(sql)
		w.runCurrentQuery(nil)
	}
	ts.extPanel = new(ExtensionsPanel)
	ts.extPanel.OnAction = func(name string, install bool) {
		w.handleExtAction(ts, name, install)
	}

	// Start showing schema, hide the others
	ts.historyPanel.AsCanvasItem().SetVisible(false)
	ts.extPanel.AsCanvasItem().SetVisible(false)

	schemaBtn.AsBaseButton().OnPressed(func() { w.showSchemaSidebar(ts) })
	historyBtn.AsBaseButton().OnPressed(func() { w.showHistorySidebar(ts) })
	extBtn.AsBaseButton().OnPressed(func() { w.showExtensionsSidebar(ts) })

	sidebarVBox.AsNode().AddChild(selectorRow.AsNode())
	sidebarVBox.AsNode().AddChild(ts.schema.AsNode())
	sidebarVBox.AsNode().AddChild(ts.historyPanel.AsNode())
	sidebarVBox.AsNode().AddChild(ts.extPanel.AsNode())
	sidebarMargin.AsNode().AddChild(sidebarVBox.AsNode())
	ts.sidebarWrap.AsNode().AddChild(sidebarMargin.AsNode())

	// Right panel (VSplitContainer so SQL panel and data grid are resizable)
	ts.rightPanel = VSplitContainer.New()
	ts.rightPanel.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.rightPanel.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.rightPanel.AsControl().AddThemeConstantOverride("separation", 1)
	ts.rightPanel.AsControl().SetCustomMinimumSize(Vector2.New(scaled(200), 0)) // min width for data grid
	ts.rightPanel.AsControl().SetClipContents(true)

	ts.sqlPanel = new(SQLPanel)
	ts.sqlPanel.OnRunQuery = func(sql string) {
		ts.State.UserSQL = sql
		ts.State.PageOffset = 0
		ts.State.SortColumn = ""
		ts.State.SortDir = models.SortNone
		w.runCurrentQuery(nil)
	}
	sqlWrap := MarginContainer.New()
	sqlWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_top", 6)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_left", 8)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_right", 8)
	sqlWrap.AsControl().AddThemeConstantOverride("margin_bottom", 4)
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
		w.runCurrentQuery(nil)
	}

	ts.dataGrid.OnRowsSelected = func(rowIndices []int) {
		if len(rowIndices) == 0 {
			return
		}
		// Collect row data for all selected indices
		var rows [][]string
		for _, idx := range rowIndices {
			if idx < len(ts.dataGrid.rows) {
				rows = append(rows, ts.dataGrid.rows[idx])
			}
		}
		if len(rows) == 0 {
			return
		}
		ts.detailPanel.SetRows(ts.dataGrid.columns, ts.dataGrid.colTypes, rows)
		if !ts.detailWrap.AsCanvasItem().Visible() {
			// Open at 25% width
			totalWidth := ts.outerWrap.AsControl().Size().X
			ts.outerWrap.AsSplitContainer().SetSplitOffset(int(totalWidth * 0.75))
			ts.detailWrap.AsCanvasItem().SetVisible(true)
			w.statusBar.SetRightPaneActive(true)
		}
	}

	ts.dataGrid.OnSelectionCleared = func() {
		ts.detailPanel.Clear()
	}

	// Detail panel (third column)
	ts.detailPanel = new(RowDetailPanel)
	ts.detailWrap = PanelContainer.New()
	applyPanelBg(ts.detailWrap.AsControl(), colorBgSidebar)
	ts.detailWrap.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	ts.detailWrap.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	ts.detailWrap.AsControl().SetCustomMinimumSize(Vector2.New(scaled(150), 0)) // min width for detail
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
			applyConnTileTheme(c.button.AsControl(), i == ts.connIdx)
		}
		w.titleBar.SetFileInfo(w.connections[ts.connIdx].Path)
	}

	if ts.State.FilePath != "" {
		w.titleBar.SetFileInfo(ts.State.FilePath)
	} else if ts.connIdx == 0 {
		w.titleBar.SetFileInfo("")
	}

	// Sync right pane toggle with the active tab's detail panel visibility
	w.statusBar.SetRightPaneActive(ts.detailWrap.AsCanvasItem().Visible())

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
	w.onFileSelectedWithCmd(path, nil)
}

func (w *AppWindow) onFileSelectedWithCmd(path string, cmd *control.Command) {
	if w.isDuckDBFile(path) {
		w.onDatabaseOpenedWithCmd(path, cmd)
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
		if cmd != nil {
			cmd.Respond(control.Result{Error: "no active tab"})
		}
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
	w.statusBar.SetStatus("Loading…")
	ts.dataGrid.AsCanvasItem().SetModulate(Color.RGBA{R: 1, G: 1, B: 1, A: 0.3})
	w.skipPoll = true

	ts.generation++
	conn := w.connections[0] // memory connection for file queries
	conn.worker.Send(DBRequest{
		Kind:       ReqOpenFile,
		FilePath:   path,
		UserSQL:    ts.State.UserSQL,
		VirtualSQL: ts.State.VirtualSQL(),
		Offset:     ts.State.PageOffset,
		Limit:      ts.State.PageSize,
		TabID:      ts.tabID,
		Generation: ts.generation,
		ControlCmd: cmd,
	})
}

// runCurrentQuery sends a query request to the appropriate worker.
// The optional cmd will receive a response when the result arrives.
func (w *AppWindow) runCurrentQuery(cmd *control.Command) {
	ts := w.currentTab()
	if ts == nil {
		if cmd != nil {
			cmd.Respond(control.Result{Error: "no active tab"})
		}
		return
	}
	ts.generation++
	w.statusBar.SetStatus("Running…")
	ts.dataGrid.AsCanvasItem().SetModulate(Color.RGBA{R: 1, G: 1, B: 1, A: 0.3})
	w.skipPoll = true

	var worker *ConnWorker
	if ts.connIdx >= 0 && ts.connIdx < len(w.connections) {
		worker = w.connections[ts.connIdx].worker
	}
	if worker == nil {
		if cmd != nil {
			cmd.Respond(control.Result{Error: "no worker for connection"})
		}
		return
	}
	worker.Send(DBRequest{
		Kind:       ReqQuery,
		VirtualSQL: ts.State.VirtualSQL(),
		UserSQL:    ts.State.UserSQL,
		FilePath:   ts.State.FilePath,
		Offset:     ts.State.PageOffset,
		Limit:      ts.State.PageSize,
		TabID:      ts.tabID,
		Generation: ts.generation,
		Navigating: ts.navigating,
		ControlCmd: cmd,
	})
}

func (w *AppWindow) onDatabaseOpened(path string) {
	w.onDatabaseOpenedWithCmd(path, nil)
}

func (w *AppWindow) onDatabaseOpenedWithCmd(path string, cmd *control.Command) {
	w.statusBar.SetStatus("Opening database…")
	// Use a placeholder tabID for the one-shot goroutine
	// (the actual tab doesn't exist yet; handleOpenDBResult will create it)
	tid := nextTabID
	nextTabID++
	RunOpenDB(path, tid, 0, cmd, w.results)
}

// handleOpenDBResult processes the result of an async OpenDB operation.
func (w *AppWindow) handleOpenDBResult(res DBResult) {
	if res.Err != nil {
		w.statusBar.SetStatus("Error: " + res.Err.Error())
		if ts := w.currentTab(); ts != nil {
			ts.dataGrid.ShowError(res.Err.Error())
		}
		if res.ControlCmd != nil {
			res.ControlCmd.Respond(control.Result{Error: res.Err.Error()})
		}
		return
	}

	path := res.DBPath
	tables := res.Tables
	dbConn := res.Querier

	// Extract short name from path
	name := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			name = path[i+1:]
			break
		}
	}

	// Create worker for this connection
	dbWorker := NewConnWorker(dbConn, w.results)
	dbWorker.Start()

	// Create connection
	conn := &Connection{
		Name:   name,
		Path:   path,
		DB:     dbConn,
		Tables: tables,
		worker: dbWorker,
	}

	// Add button to rail
	btn := Button.New()
	btn.SetText(models.ConnBadge(name))
	btn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(40), scaled(40)))
	btn.SetClipText(true)
	applyConnTileTheme(btn.AsControl(), false)
	conn.button = btn

	dbIdx := len(w.connections)
	w.connections = append(w.connections, conn)
	w.connList.AsNode().AddChild(btn.AsNode())
	w.connRailWrap.AsCanvasItem().SetVisible(true)

	w.wireConnButton(btn, dbIdx)

	// Create a new tab bound to this connection
	w.addNewTab()
	ts := w.currentTab()
	if ts != nil {
		ts.State.IsDatabase = true
		ts.connIdx = dbIdx
		ts.schema.SetTables(conn.Tables)
		ts.sqlPanel.SetCompletionTables(conn.Tables)
		ts.schema.OnTableClicked = func(tableName string) {
			ts.State.ActiveTable = tableName
			ts.State.UserSQL = fmt.Sprintf("SELECT * FROM \"%s\"", tableName)
			ts.State.PageOffset = 0
			ts.State.SortColumn = ""
			ts.State.SortDir = models.SortNone
			ts.sqlPanel.SetSQL(ts.State.UserSQL)
			w.runCurrentQuery(nil)
		}
	}

	// Highlight this connection in the rail
	w.selectConnection(dbIdx)

	w.statusBar.SetStatus(fmt.Sprintf("Connected: %s (%d tables/views)", name, len(tables)))
	if res.ControlCmd != nil {
		res.ControlCmd.Respond(control.Result{OK: true})
	}
}

func (w *AppWindow) selectConnection(idx int) {
	if idx < 0 || idx >= len(w.connections) {
		return
	}
	w.activeConnIdx = idx

	// Highlight active tile
	for i, c := range w.connections {
		applyConnTileTheme(c.button.AsControl(), i == idx)
	}

	// Show/hide AI prompt copy button based on connection type
	conn := w.connections[idx]
	if conn.Gateway != nil {
		w.titleBar.SetAIPrompt(buildAIPrompt(conn.Gateway.Config, conn.Tables, w.controlAddr))
	} else {
		w.titleBar.SetAIPrompt("")
	}

	// Reflect the active connection in the footer, including its health dot.
	connName := conn.Name
	if conn.Path == ":memory:" {
		connName = "in-memory"
	}
	w.statusBar.SetConnection(connName)
	w.statusBar.SetConnectionDot(connHealthColor(conn))

	// Find the first tab bound to this connection and switch to it
	for i, ts := range w.tabs {
		if ts.connIdx == idx {
			w.tabBar.SetCurrentTab(i)
			return
		}
	}
}

type sidebarView int

const (
	viewSchema sidebarView = iota
	viewHistory
	viewExtensions
)

// setSidebarView shows exactly one of the sidebar panels and highlights its tab.
func (w *AppWindow) setSidebarView(ts *tabState, v sidebarView) {
	ts.schema.AsCanvasItem().SetVisible(v == viewSchema)
	ts.historyPanel.AsCanvasItem().SetVisible(v == viewHistory)
	ts.extPanel.AsCanvasItem().SetVisible(v == viewExtensions)
	applySidebarTabTheme(ts.schemaBtn.AsControl(), v == viewSchema)
	applySidebarTabTheme(ts.historyBtn.AsControl(), v == viewHistory)
	applySidebarTabTheme(ts.extBtn.AsControl(), v == viewExtensions)

	switch v {
	case viewHistory:
		if w.history != nil {
			ts.historyPanel.SetHistory(w.history.All())
		}
	case viewExtensions:
		w.loadExtensions(ts)
	}
}

func (w *AppWindow) showSchemaSidebar(ts *tabState)     { w.setSidebarView(ts, viewSchema) }
func (w *AppWindow) showHistorySidebar(ts *tabState)    { w.setSidebarView(ts, viewHistory) }
func (w *AppWindow) showExtensionsSidebar(ts *tabState) { w.setSidebarView(ts, viewExtensions) }

// loadExtensions queries the DuckDB extension list and populates the panel.
func (w *AppWindow) loadExtensions(ts *tabState) {
	if w.duck == nil {
		return
	}
	exts, err := w.duck.Extensions()
	if err != nil {
		w.statusBar.SetStatus("Extensions error: " + err.Error())
		return
	}
	ts.extPanel.SetExtensions(exts)
}

// extActionResult carries the outcome of a background install/load back to the
// main thread.
type extActionResult struct {
	name string
	err  error
}

// handleExtAction installs (and loads) or loads an extension in the background,
// then refreshes the panel. INSTALL may hit the network, so it must not run on
// the main thread.
func (w *AppWindow) handleExtAction(ts *tabState, name string, install bool) {
	if w.duck == nil {
		return
	}
	verb := "Loading"
	if install {
		verb = "Installing"
	}
	w.statusBar.SetStatus(verb + " " + name + "…")
	go func() {
		var err error
		if install {
			err = w.duck.InstallExtension(name)
		}
		if err == nil {
			err = w.duck.LoadExtension(name)
		}
		w.extActionTab = ts
		w.extActionMsg = &extActionResult{name: name, err: err}
	}()
}

// wireConnButton sets up left-click (select) and right-click (context menu) on a rail button.
// connHealthColor returns the footer status-dot color for a connection: green
// when connected (all local connections, or a gateway with a live tunnel),
// amber while a gateway tunnel is reconnecting, red on tunnel error.
func connHealthColor(conn *Connection) Color.RGBA {
	if conn == nil {
		return colorStatusGray
	}
	if conn.Gateway == nil || conn.Gateway.Tunnel == nil {
		return colorStatusGreen
	}
	switch conn.Gateway.Tunnel.Status() {
	case bfaws.TunnelError:
		return colorStatusRed
	case bfaws.TunnelConnecting:
		return colorStatusYellow
	default:
		return colorStatusGreen
	}
}

func (w *AppWindow) wireConnButton(btn Button.Instance, idx int) {
	if idx >= 0 && idx < len(w.connections) {
		btn.AsControl().SetTooltipText(w.connections[idx].Name)
	}
	btn.AsBaseButton().OnPressed(func() {
		w.selectConnection(idx)
	})
	btn.AsControl().OnGuiInput(func(event InputEvent.Instance) {
		mb, ok := Object.As[InputEventMouseButton.Instance](event)
		if !ok {
			return
		}
		if mb.ButtonIndex() == Input.MouseButtonRight && mb.AsInputEvent().IsPressed() {
			w.showConnContextMenu(idx)
		}
	})
}

func (w *AppWindow) showConnContextMenu(idx int) {
	if idx < 0 || idx >= len(w.connections) {
		return
	}
	conn := w.connections[idx]

	popup := PopupMenu.New()
	popup.AddItem("Refresh")
	popup.AddItem("Close " + conn.Name)

	popup.OnIdPressed(func(id int) {
		switch id {
		case 0:
			w.refreshConnection(idx)
		case 1:
			w.closeConnection(idx)
		}
		popup.AsNode().QueueFree()
	})
	popup.AsWindow().OnCloseRequested(func() {
		popup.AsNode().QueueFree()
	})

	w.connRail.AsNode().AddChild(popup.AsNode())
	popup.AsWindow().SetPosition(DisplayServer.MouseGetPosition())
	popup.AsWindow().Popup()
}

// showNewConnectionMenu opens the "+" rail menu offering the two ways to create
// a connection. This is the cross-platform (Windows/Linux) entry point for
// actions that previously lived only in the macOS native menu bar.
func (w *AppWindow) showNewConnectionMenu(anchor Button.Instance) {
	popup := PopupMenu.New()
	popup.AddItem("Open File…")
	popup.AddItem("Connect to Gateway…")

	popup.OnIdPressed(func(id int) {
		switch id {
		case 0:
			w.showOpenFileDialog()
		case 1:
			if w.onNewConnection != nil {
				w.onNewConnection()
			}
		}
		popup.AsNode().QueueFree()
	})
	popup.AsWindow().OnCloseRequested(func() {
		popup.AsNode().QueueFree()
	})

	w.connRail.AsNode().AddChild(popup.AsNode())
	popup.AsWindow().SetPosition(DisplayServer.MouseGetPosition())
	popup.AsWindow().Popup()
}

// showOpenFileDialog presents the native file picker for the file types
// Bufflehead can open and loads the selection into the active tab.
func (w *AppWindow) showOpenFileDialog() {
	DisplayServer.FileDialogShow(
		"Open Data File",
		"",
		"",
		false,
		DisplayServer.FileDialogModeOpenFile,
		[]string{"*.parquet,*.duckdb,*.db,*.ddb,*.csv,*.json,*.tsv ; Data Files"},
		func(status bool, selectedPaths []string, selectedFilterIndex int) {
			if status && len(selectedPaths) > 0 {
				w.onFileSelected(selectedPaths[0])
			}
		},
		0,
	)
}

// refreshConnection refreshes a connection. For remote gateway connections it
// performs a full teardown/reconnect (cancel queries, rebuild tunnel + DB) so a
// broken tunnel or expired credentials are recovered, and it reports each step
// in the data grid. For local connections (in-memory DuckDB, .duckdb files)
// there is nothing to reconnect, so it just re-fetches tables and schemas.
func (w *AppWindow) refreshConnection(idx int) {
	if idx < 0 || idx >= len(w.connections) {
		return
	}
	conn := w.connections[idx]

	if conn.Gateway != nil {
		// Full reconnect; results (including step detail) handled in
		// handleReconnectResult. cmd is nil → UI-initiated.
		w.reconnectConnection(idx, nil)
		return
	}

	if conn.worker == nil {
		return
	}
	w.statusBar.SetStatus("Refreshing " + conn.Name + "...")
	conn.worker.Send(DBRequest{
		Kind:    ReqRefresh,
		ConnIdx: idx,
	})
}

// closeConnection closes a connection, removes its tabs, and cleans up resources.
// Index 0 (in-memory DuckDB) cannot be closed.
func (w *AppWindow) closeConnection(idx int) {
	if idx <= 0 || idx >= len(w.connections) {
		return
	}
	conn := w.connections[idx]

	// Close tabs bound to this connection (iterate backwards to avoid index shifts)
	for i := len(w.tabs) - 1; i >= 0; i-- {
		if w.tabs[i].connIdx == idx {
			w.closeTab(i)
		}
	}

	// Stop worker
	if conn.worker != nil {
		conn.worker.Stop()
	}

	// Stop tunnel
	if conn.Gateway != nil && conn.Gateway.Tunnel != nil {
		conn.Gateway.Tunnel.Stop()
	}

	// Close DB connection
	if conn.DB != nil {
		conn.DB.Close()
	}

	// Remove button from rail
	w.connRail.AsNode().RemoveChild(conn.button.AsNode())
	conn.button.AsNode().QueueFree()

	// Remove from connections slice
	w.connections = append(w.connections[:idx], w.connections[idx+1:]...)

	// Fix connIdx on remaining tabs (any pointing above idx need to shift down)
	for _, ts := range w.tabs {
		if ts.connIdx > idx {
			ts.connIdx--
		}
	}

	// Ensure at least one tab exists so the app stays usable
	if len(w.tabs) == 0 {
		w.addNewTab()
	}

	// Fix activeConnIdx
	if w.activeConnIdx == idx {
		w.activeConnIdx = 0
		w.selectConnection(0)
	} else if w.activeConnIdx > idx {
		w.activeConnIdx--
	}

	// Reset title bar to memory connection
	w.titleBar.SetFileInfo("")
	w.titleBar.SetAIPrompt("")

}

// findTab returns the tabState with the given tabID, or nil if not found.
func (w *AppWindow) findTab(tabID uint64) *tabState {
	for _, ts := range w.tabs {
		if ts.tabID == tabID {
			return ts
		}
	}
	return nil
}

// handleDBResult dispatches an async result by kind.
func (w *AppWindow) handleDBResult(res DBResult) {
	switch res.Kind {
	case ReqQuery:
		w.handleQueryResult(res)
	case ReqOpenFile:
		w.handleOpenFileResult(res)
	case ReqOpenDB:
		w.handleOpenDBResult(res)
	case ReqOpenGateway:
		w.handleOpenGatewayResult(res)
	case ReqRefresh:
		w.handleRefreshResult(res)
	case ReqReconnect:
		w.handleReconnectResult(res)
	}
}

// handleOpenGatewayResult processes the result of an async gateway (Postgres) open.
func (w *AppWindow) handleOpenGatewayResult(res DBResult) {
	if res.Err != nil {
		w.gatewayTracker = nil
		w.showConnError(w.currentTab(), res.Err, "Gateway error: ")
		return
	}

	gw := w.pendingGateway
	w.pendingGateway = nil
	w.gatewayTracker = nil
	if gw == nil {
		return
	}

	name := gw.Config.Name
	pgConn := res.Querier
	tables := res.Tables

	// Create worker
	pgWorker := NewConnWorker(pgConn, w.results)
	pgWorker.Start()

	conn := &Connection{
		Name:    name,
		Path:    fmt.Sprintf("postgresql://localhost:%d/%s", gw.Config.LocalPort, gw.Config.DBName),
		DB:      pgConn,
		Tables:  tables,
		worker:  pgWorker,
		Gateway: gw,
	}

	// Add button to rail
	btn := Button.New()
	btn.SetText(models.ConnBadge(name))
	btn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(40), scaled(40)))
	btn.SetClipText(true)
	applyConnTileTheme(btn.AsControl(), false)
	conn.button = btn

	gwIdx := len(w.connections)
	w.connections = append(w.connections, conn)
	w.connList.AsNode().AddChild(btn.AsNode())
	w.connRailWrap.AsCanvasItem().SetVisible(true)

	w.wireConnButton(btn, gwIdx)

	// Create a new tab bound to this connection
	w.addNewTab()
	ts := w.currentTab()
	if ts != nil {
		ts.State.IsDatabase = true
		ts.connIdx = gwIdx
		ts.schema.SetTables(conn.Tables)
		ts.sqlPanel.SetCompletionTables(conn.Tables)
		ts.schema.OnTableClicked = func(tableName string) {
			ts.State.ActiveTable = tableName
			ts.State.UserSQL = fmt.Sprintf("SELECT * FROM %s", tableName)
			ts.State.PageOffset = 0
			ts.State.SortColumn = ""
			ts.State.SortDir = models.SortNone
			ts.sqlPanel.SetSQL(ts.State.UserSQL)
			w.runCurrentQuery(nil)
		}

		// Update title bar for Postgres
		w.titleBar.SetConnectionInfo("PostgreSQL", name, gw.Config.DBName)

		// Build AI prompt with schema for the copy button
		w.titleBar.SetAIPrompt(buildAIPrompt(gw.Config, tables, w.controlAddr))
	}

	w.selectConnection(gwIdx)
	w.statusBar.SetStatus(fmt.Sprintf("Connected: %s (%d tables/views)", name, len(tables)))
}

// showConnError displays a connection/query error in the given tab's data grid
// and status bar. For expired-login errors it shows friendly guidance and pops
// a dialog offering to re-authenticate.
func (w *AppWindow) showConnError(ts *tabState, err error, statusPrefix string) {
	msg, isAuth := bfaws.FormatConnError(err)
	if ts != nil {
		ts.dataGrid.ShowError(msg)
	}
	if isAuth {
		w.statusBar.SetStatus("Login expired — log in again to reconnect")
		w.promptReLogin()
		return
	}
	w.statusBar.SetStatus(statusPrefix + err.Error())
}

// promptReLogin shows a dialog guiding the user to re-authenticate. Confirming
// opens the connection/SSO screen. Guarded so only one dialog shows at a time.
func (w *AppWindow) promptReLogin() {
	if w.reLoginDialogOpen {
		return
	}
	w.reLoginDialogOpen = true

	dlg := ConfirmationDialog.New()
	dlg.AsWindow().SetTitle("Login Expired")
	dlg.AsAcceptDialog().SetDialogText(
		"Your AWS SSO login has expired, so Bufflehead lost access to this database.\n\n" +
			"Would you like to log in again now?")
	dlg.AsAcceptDialog().SetOkButtonText("Log in again")
	dlg.SetCancelButtonText("Not now")

	dlg.AsAcceptDialog().OnConfirmed(func() {
		w.reLoginDialogOpen = false
		if w.onReLogin != nil {
			w.onReLogin()
		}
		dlg.AsNode().QueueFree()
	})
	dlg.AsWindow().OnCloseRequested(func() {
		w.reLoginDialogOpen = false
		dlg.AsNode().QueueFree()
	})

	// Attach the dialog inside the current window's scene tree so it centers
	// over the app. tabBarWrap is always part of that tree.
	w.tabBarWrap.AsNode().AddChild(dlg.AsNode())
	dlg.AsWindow().PopupCentered()
}

// handleRefreshResult updates a connection's table list and refreshes the sidebar.
func (w *AppWindow) handleRefreshResult(res DBResult) {
	idx := res.ConnIdx
	if idx < 0 || idx >= len(w.connections) {
		return
	}
	conn := w.connections[idx]

	if res.Err != nil {
		w.statusBar.SetStatus("Refresh error: " + res.Err.Error())
		return
	}

	conn.Tables = res.Tables

	// Update sidebar on any tabs bound to this connection
	for _, ts := range w.tabs {
		if ts.connIdx == idx {
			ts.schema.SetTables(conn.Tables)
			ts.sqlPanel.SetCompletionTables(conn.Tables)
		}
	}

	// Update AI prompt if this is the active connection
	if idx == w.activeConnIdx && conn.Gateway != nil {
		w.titleBar.SetAIPrompt(buildAIPrompt(conn.Gateway.Config, conn.Tables, w.controlAddr))
	}

	w.statusBar.SetStatus(fmt.Sprintf("Refreshed: %s (%d tables/views)", conn.Name, len(conn.Tables)))
}

func buildAIPrompt(entry models.GatewayEntry, tables []db.TableInfo, controlAddr string) string {
	connName := entry.Name

	var b strings.Builder
	b.WriteString("I have a PostgreSQL database you can query.\n")
	b.WriteString(fmt.Sprintf("Database: %s\n", entry.DBName))
	b.WriteString(fmt.Sprintf("\nRun queries via HTTP (no auth needed, Bufflehead manages the connection):\n"))
	b.WriteString(fmt.Sprintf("  curl -s -X POST http://%s/sql -d '{\"sql\":\"SELECT * FROM table LIMIT 10\",\"connection\":\"%s\"}'\n", controlAddr, connName))
	b.WriteString("\nResults are limited to 100 rows by default. Queries time out after 30 seconds.\n")
	b.WriteString("Use indexed columns in WHERE clauses, avoid full table scans, and keep queries targeted.\n")
	b.WriteString("\nResponse format: {\"columns\":[...],\"rows\":[[...],...],\"total\":N}\n")

	b.WriteString("\nYou can also fetch S3 objects via HTTP:\n")
	b.WriteString(fmt.Sprintf("  curl -s -X POST http://%s/s3/get-object -d '{\"bucket\":\"BUCKET\",\"key\":\"KEY\",\"connection\":\"%s\"}'\n", controlAddr, connName))
	b.WriteString("\nResponse format: {\"content\":\"...\",\"content_type\":\"...\",\"size\":N,\"truncated\":BOOL}\n")
	b.WriteString("\nSome columns may contain JSON with S3 pointers (e.g. {\"s3_key\": \"...\", \"s3_bucket\": \"...\"}).\n")
	b.WriteString("When you encounter these, extract s3_bucket and s3_key from the JSON and use the S3 endpoint to fetch the object contents.\n")

	b.WriteString("\nIf queries start failing with connection errors (timeouts, health check failures, expired credentials, or a broken SSM tunnel), you can force a full reconnect. This cancels any running queries, tears down the tunnel and database pool, and re-establishes them from scratch:\n")
	b.WriteString(fmt.Sprintf("  curl -s -X POST http://%s/reconnect -d '{\"connection\":\"%s\"}'\n", controlAddr, connName))
	b.WriteString("\nThe response reports each step so you can see where it failed:\n")
	b.WriteString("  {\"connection\":\"...\",\"ok\":BOOL,\"tables\":N,\"steps\":[{\"step\":\"cancel_queries\",\"ok\":true},{\"step\":\"start_tunnel\",\"ok\":false,\"error\":\"...\"},...]}\n")
	b.WriteString("Steps in order: cancel_queries, close_db, stop_tunnel, refresh_credentials (IAM only), start_tunnel, connect_db.\n")
	b.WriteString("If \"refresh_credentials\" or a step mentions expired SSO, the user must log in again (aws sso login) before a reconnect can succeed.\n")

	if len(tables) > 0 {
		b.WriteString("\nSchema:\n")
		for _, t := range tables {
			var cols []string
			for _, c := range t.Columns {
				cols = append(cols, fmt.Sprintf("%s %s", c.Name, c.DataType))
			}
			prefix := "- " + t.Name
			if t.Type == "view" {
				prefix = "- " + t.Name + " (view)"
			}
			b.WriteString(fmt.Sprintf("%s (%s)\n", prefix, strings.Join(cols, ", ")))
		}
	}

	return b.String()
}

// handleQueryResult applies a query result to the UI.
func (w *AppWindow) handleQueryResult(res DBResult) {
	ts := w.findTab(res.TabID)
	if ts == nil || ts.generation != res.Generation {
		// Stale result — respond OK to any waiting control command
		if res.ControlCmd != nil {
			res.ControlCmd.Respond(control.Result{OK: true})
		}
		return
	}

	ts.dataGrid.AsCanvasItem().SetModulate(Color.RGBA{R: 1, G: 1, B: 1, A: 1})

	if res.Err != nil {
		if !res.Navigating && w.history != nil {
			if sql := res.VirtualSQL; sql != "" {
				w.history.Add(models.HistoryEntry{
					SQL:        sql,
					FilePath:   ts.State.FilePath,
					Timestamp:  time.Now(),
					DurationMs: res.Elapsed.Milliseconds(),
					Error:      res.Err.Error(),
				})
			}
		}
		w.showConnError(ts, res.Err, "Error: ")
		ts.navigating = false
		if res.ControlCmd != nil {
			res.ControlCmd.Respond(control.Result{Error: res.Err.Error()})
		}
		return
	}

	result := res.Query
	ts.State.Result = result

	if !res.Navigating {
		if w.history != nil {
			w.history.Add(models.HistoryEntry{
				SQL:        res.VirtualSQL,
				FilePath:   ts.State.FilePath,
				Timestamp:  time.Now(),
				RowCount:   result.Total,
				DurationMs: res.Elapsed.Milliseconds(),
			})
		}
		ts.State.NavPush(ts.State.UserSQL)
	}
	ts.navigating = false
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

	if res.ControlCmd != nil {
		res.ControlCmd.Respond(control.Result{OK: true})
	}
}

// handleOpenFileResult applies the schema/metadata from an open-file operation,
// then delegates the query result.
func (w *AppWindow) handleOpenFileResult(res DBResult) {
	ts := w.findTab(res.TabID)
	if ts == nil || ts.generation != res.Generation {
		if res.ControlCmd != nil {
			res.ControlCmd.Respond(control.Result{OK: true})
		}
		return
	}

	if res.Schema == nil && res.Err != nil {
		// Schema failed — show error
		w.statusBar.SetStatus("Error: " + res.Err.Error())
		ts.dataGrid.ShowError(res.Err.Error())
		if res.ControlCmd != nil {
			res.ControlCmd.Respond(control.Result{Error: res.Err.Error()})
		}
		return
	}

	// Apply schema
	ts.State.Schema = res.Schema
	ts.schema.SetSchema(res.Schema)
	ts.sqlPanel.SetCompletionSchema(res.Schema)

	// Apply metadata (may be nil)
	ts.State.Metadata = res.Metadata

	// Delegate query result handling
	w.handleQueryResult(res)
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
	w.navBackWithCmd(nil)
}

func (w *AppWindow) navBackWithCmd(cmd *control.Command) {
	ts := w.currentTab()
	if ts == nil {
		if cmd != nil {
			cmd.Respond(control.Result{OK: true})
		}
		return
	}
	entry, ok := ts.State.NavBack()
	if !ok {
		if cmd != nil {
			cmd.Respond(control.Result{OK: true})
		}
		return
	}
	ts.navigating = true
	ts.State.UserSQL = entry.SQL
	ts.State.SortColumn = entry.SortColumn
	ts.State.SortDir = entry.SortDir
	ts.State.PageOffset = entry.PageOffset
	ts.sqlPanel.SetSQL(entry.SQL)
	w.runCurrentQuery(cmd)
	w.updateNavButtons()
}

func (w *AppWindow) navForward() {
	w.navForwardWithCmd(nil)
}

func (w *AppWindow) navForwardWithCmd(cmd *control.Command) {
	ts := w.currentTab()
	if ts == nil {
		if cmd != nil {
			cmd.Respond(control.Result{OK: true})
		}
		return
	}
	entry, ok := ts.State.NavForward()
	if !ok {
		if cmd != nil {
			cmd.Respond(control.Result{OK: true})
		}
		return
	}
	ts.navigating = true
	ts.State.UserSQL = entry.SQL
	ts.State.SortColumn = entry.SortColumn
	ts.State.SortDir = entry.SortDir
	ts.State.PageOffset = entry.PageOffset
	ts.sqlPanel.SetSQL(entry.SQL)
	w.runCurrentQuery(cmd)
	w.updateNavButtons()
}

// stopWorkers shuts down all connection workers for this window.
func (w *AppWindow) stopWorkers() {
	for _, conn := range w.connections {
		if conn.worker != nil {
			conn.worker.Stop()
		}
	}
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

// createMainWindowFromRoot builds the main AppWindow using the root viewport
// window, avoiding the creation of a secondary window that causes a visible
// flash on macOS.
func createMainWindowFromRoot(rootWin Window.Instance, duck *db.DB, history *models.QueryHistory, onNewWindow func()) *AppWindow {
	rootWin.SetTitle("Bufflehead")
	rootWin.SetSize(Vector2i.New(1440, 900))
	rootWin.SetMinSize(Vector2i.New(1100, 720))
	aw := &AppWindow{
		window:      rootWin,
		isMain:      true,
		duck:        duck,
		history:     history,
		onNewWindow: onNewWindow,
	}

	ui := aw.buildUI()
	root := Control.New()
	root.SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	ui.AsControl().SetAnchorsAndOffsetsPreset(Control.PresetFullRect)
	root.AsNode().AddChild(ui.AsNode())
	rootWin.AsNode().AddChild(root.AsNode())

	rootWin.OnFilesDropped(func(files []string) {
		for _, f := range files {
			aw.onFileSelected(f)
		}
	})

	return aw
}

// createSecondaryWindow creates a new OS-level window with full UI.
func createSecondaryWindow(duck *db.DB, history *models.QueryHistory, onNewWindow func()) *AppWindow {
	win := Window.New()
	win.SetTitle("Bufflehead")
	win.SetSize(Vector2i.New(1440, 900))
	win.SetMinSize(Vector2i.New(1100, 720))
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
			aw.onFileSelected(f)
		}
	})

	return aw
}
