package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	bfaws "bufflehead/internal/aws"
	"bufflehead/internal/completion"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"bufflehead/internal/control"
	"bufflehead/internal/db"
	"bufflehead/internal/models"

	"graphics.gd/classdb"
	"graphics.gd/classdb/BoxContainer"
	"graphics.gd/classdb/Button"
	"graphics.gd/classdb/CanvasItem"
	"graphics.gd/classdb/CheckBox"
	"graphics.gd/classdb/CodeEdit"
	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/DisplayServer"
	"graphics.gd/classdb/Engine"
	"graphics.gd/classdb/GUI"
	"graphics.gd/classdb/HBoxContainer"
	"graphics.gd/classdb/HSplitContainer"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/InputEvent"
	"graphics.gd/classdb/InputEventKey"
	"graphics.gd/classdb/InputEventMouseButton"
	"graphics.gd/classdb/InputEventMouseMotion"
	"graphics.gd/classdb/Label"
	"graphics.gd/classdb/LineEdit"
	"graphics.gd/classdb/MarginContainer"
	"graphics.gd/classdb/TextureRect"
	"graphics.gd/classdb/Node"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/PopupMenu"
	"graphics.gd/classdb/SceneTree"
	"graphics.gd/classdb/StyleBoxEmpty"
	"graphics.gd/classdb/ScrollContainer"
	"graphics.gd/classdb/TextServer"
	"graphics.gd/classdb/SplitContainer"
	"graphics.gd/classdb/Tree"
	"graphics.gd/classdb/TreeItem"
	"graphics.gd/classdb/VBoxContainer"
	"graphics.gd/classdb/VSplitContainer"
	"graphics.gd/classdb/Window"

	"graphics.gd/variant/Color"
	"graphics.gd/variant/Float"
	"graphics.gd/variant/Object"
	"graphics.gd/variant/Vector2"
	"graphics.gd/variant/Vector2i"
)

// ── Title bar ──────────────────────────────────────────────────────────────

type TitleBar struct {
	PanelContainer.Extension[TitleBar] `gd:"TitleBar"`

	infoLabel      Label.Instance
	dbBtn          Button.Instance // clickable database segment (opens switcher)
	copyBtn        Button.Instance
	refreshBtn     Button.Instance
	runBtn         Button.Instance
	aiPrompt       string
	NavBackBtn     Button.Instance
	NavFwdBtn      Button.Instance
	OnRefresh      func() // called when refresh button is pressed
	OnRun          func() // called when the Run button is pressed
	OnPickDatabase func() // called when the database breadcrumb segment is clicked
	WindowID       int
	resetCopyText  bool // set by goroutine, read by Process on main thread
}

func (t *TitleBar) GuiInput(event InputEvent.Instance) {
	if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
		if mb.ButtonIndex() == Input.MouseButtonLeft && mb.AsInputEvent().IsPressed() {
			DisplayServer.WindowStartDrag(DisplayServer.Window(t.WindowID))
		}
	}
}

func (t *TitleBar) Ready() {
	applyTitleBarTheme(t.AsControl())
	t.AsControl().SetMouseFilter(Control.MouseFilterStop)
	t.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	t.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(42)))

	margin := MarginContainer.New()
	margin.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	margin.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	margin.AsControl().AddThemeConstantOverride("margin_top", 6)
	margin.AsControl().AddThemeConstantOverride("margin_left", 78) // clear macOS traffic lights
	margin.AsControl().AddThemeConstantOverride("margin_right", 8)
	margin.AsControl().AddThemeConstantOverride("margin_bottom", 6)

	row := HBoxContainer.New()
	row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	row.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	row.AsControl().AddThemeConstantOverride("separation", 6)

	// ── Left: brand + nav ───────────────────────────────────────────────
	brand := Label.New()
	brand.SetText("Bufflehead")
	brand.AsControl().AddThemeColorOverride("font_color", colorTextBright)
	brand.AsControl().AddThemeFontSizeOverride("font_size", fontSize(14))
	brand.AsControl().SetMouseFilter(Control.MouseFilterPass)
	brand.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)

	brandDiv := footerDivider()
	brandDiv.AsControl().SetMouseFilter(Control.MouseFilterPass)

	// Ghost nav buttons
	t.NavBackBtn = Button.New()
	t.NavBackBtn.SetText("◀")
	t.NavBackBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(24), 0))
	applyFooterGhostButton(t.NavBackBtn.AsControl())
	t.NavBackBtn.AsBaseButton().SetDisabled(true)
	t.NavBackBtn.AsControl().SetMouseFilter(Control.MouseFilterStop)

	t.NavFwdBtn = Button.New()
	t.NavFwdBtn.SetText("▶")
	t.NavFwdBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(24), 0))
	applyFooterGhostButton(t.NavFwdBtn.AsControl())
	t.NavFwdBtn.AsBaseButton().SetDisabled(true)
	t.NavFwdBtn.AsControl().SetMouseFilter(Control.MouseFilterStop)

	// Spacer (draggable area)
	spacer := Control.New()
	spacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)
	spacer.SetMouseFilter(Control.MouseFilterPass)

	// ── Right: breadcrumb pill (db icon + connection) + AI + reconnect ──
	pill := PanelContainer.New()
	pill.AsControl().AddThemeStyleboxOverride("panel", titlePill().AsStyleBox())
	pill.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	pill.AsControl().SetMouseFilter(Control.MouseFilterPass)

	pillRow := HBoxContainer.New()
	pillRow.AsControl().AddThemeConstantOverride("separation", 6)
	pillRow.AsControl().SetMouseFilter(Control.MouseFilterPass)

	dbIcon := TextureRect.New()
	dbIcon.SetTexture(loadSVGTexture(svgDatabase))
	dbIcon.SetStretchMode(TextureRect.StretchKeepAspectCentered)
	dbIcon.AsControl().SetCustomMinimumSize(Vector2.New(scaled(14), scaled(14)))
	dbIcon.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	dbIcon.AsControl().SetMouseFilter(Control.MouseFilterPass)

	t.infoLabel = Label.New()
	t.infoLabel.SetText("DuckDB  ›  In-Memory  ›  No file loaded")
	t.infoLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
	t.infoLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	t.infoLabel.AsControl().AddThemeFontOverride("font", monoFont())
	t.infoLabel.AsControl().SetMouseFilter(Control.MouseFilterPass)

	// Clickable database segment — only shown for Postgres connections. Opens
	// the database switcher popup.
	t.dbBtn = Button.New()
	t.dbBtn.SetText("")
	t.dbBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	t.dbBtn.AsControl().AddThemeFontOverride("font", monoFont())
	applyBreadcrumbSegmentTheme(t.dbBtn.AsControl())
	t.dbBtn.AsControl().SetTooltipText("Switch database")
	t.dbBtn.AsCanvasItem().SetVisible(false)
	t.dbBtn.AsBaseButton().OnPressed(func() {
		if t.OnPickDatabase != nil {
			t.OnPickDatabase()
		}
	})

	t.copyBtn = Button.New()
	t.copyBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	applyFooterGhostButton(t.copyBtn.AsControl())
	t.copyBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(62), 0))
	t.copyBtn.AsControl().SetTooltipText("Copy an AI prompt describing this connection")
	t.copyBtn.AsCanvasItem().SetVisible(false)
	t.setAIButtonDefault()
	t.copyBtn.AsBaseButton().OnPressed(func() {
		if t.aiPrompt != "" {
			DisplayServer.ClipboardSet(t.aiPrompt)
			t.setAIButtonCopied()
			go func() {
				<-time.After(1500 * time.Millisecond)
				t.resetCopyText = true
			}()
		}
	})

	t.refreshBtn = Button.New()
	t.refreshBtn.SetText("↻")
	t.refreshBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	applyFooterGhostButton(t.refreshBtn.AsControl())
	t.refreshBtn.AsControl().SetCustomMinimumSize(Vector2.New(scaled(28), 0))
	t.refreshBtn.AsCanvasItem().SetVisible(false)
	t.refreshBtn.AsControl().SetTooltipText("Reconnect (rebuild tunnel + connection)")
	t.refreshBtn.AsBaseButton().OnPressed(func() {
		if t.OnRefresh != nil {
			t.OnRefresh()
		}
	})

	pillRow.AsNode().AddChild(dbIcon.AsNode())
	pillRow.AsNode().AddChild(t.infoLabel.AsNode())
	pillRow.AsNode().AddChild(t.dbBtn.AsNode())
	pill.AsNode().AddChild(pillRow.AsNode())

	// Prominent Run button (indigo CTA)
	t.runBtn = Button.New()
	t.runBtn.SetText("▶ Run")
	applyButtonTheme(t.runBtn.AsControl())
	t.runBtn.AsControl().SetTooltipText("Run query")
	t.runBtn.AsControl().SetMouseFilter(Control.MouseFilterStop)
	t.runBtn.AsBaseButton().OnPressed(func() {
		if t.OnRun != nil {
			t.OnRun()
		}
	})

	// Let container children pass mouse events through for window dragging.
	margin.AsControl().SetMouseFilter(Control.MouseFilterPass)
	row.AsControl().SetMouseFilter(Control.MouseFilterPass)

	row.AsNode().AddChild(brand.AsNode())
	row.AsNode().AddChild(brandDiv.AsNode())
	row.AsNode().AddChild(t.NavBackBtn.AsNode())
	row.AsNode().AddChild(t.NavFwdBtn.AsNode())
	row.AsNode().AddChild(spacer.AsNode())
	row.AsNode().AddChild(pill.AsNode())
	row.AsNode().AddChild(t.refreshBtn.AsNode())
	row.AsNode().AddChild(t.copyBtn.AsNode())
	row.AsNode().AddChild(t.runBtn.AsNode())

	margin.AsNode().AddChild(row.AsNode())
	t.AsNode().AddChild(margin.AsNode())
}

// setAIButtonDefault shows the AI copy button in its resting state (a lavender
// "✦ Ask AI").
func (t *TitleBar) setAIButtonDefault() {
	t.copyBtn.SetText("✦ Ask AI")
	t.copyBtn.AsControl().AddThemeColorOverride("font_color", colorAccent)
}

// setAIButtonCopied shows the green "✓ Copied!" confirmation state.
func (t *TitleBar) setAIButtonCopied() {
	t.copyBtn.SetText("✓ Copied!")
	t.copyBtn.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
}

func (t *TitleBar) Process(delta Float.X) {
	if t.resetCopyText {
		t.resetCopyText = false
		t.setAIButtonDefault()
	}
}

func (t *TitleBar) SetConnectionInfo(driver, name, dbName string) {
	// The database name is rendered as a separate clickable segment (the
	// switcher), so the label carries only the driver › connection prefix.
	t.infoLabel.SetText(driver + "  ›  " + name + "  ›  ")
	t.dbBtn.SetText(dbName + "  ▾")
	t.dbBtn.AsCanvasItem().SetVisible(true)
}

func (t *TitleBar) SetAIPrompt(prompt string) {
	t.aiPrompt = prompt
	t.copyBtn.AsCanvasItem().SetVisible(prompt != "")
	t.setAIButtonDefault()
	t.refreshBtn.AsCanvasItem().SetVisible(prompt != "")
}

func (t *TitleBar) SetFileInfo(path string) {
	// The database switcher segment only applies to Postgres connections.
	t.dbBtn.AsCanvasItem().SetVisible(false)
	if path == "" {
		t.infoLabel.SetText("DuckDB  ›  In-Memory")
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
		t.infoLabel.SetText("DuckDB  ›  " + name)
	case ".parquet":
		t.infoLabel.SetText("DuckDB  ›  In-Memory  ›  " + name)
	case ".csv":
		t.infoLabel.SetText("DuckDB  ›  CSV  ›  " + name)
	case ".json":
		t.infoLabel.SetText("DuckDB  ›  JSON  ›  " + name)
	case ".tsv":
		t.infoLabel.SetText("DuckDB  ›  TSV  ›  " + name)
	default:
		t.infoLabel.SetText("DuckDB  ›  " + name)
	}
}

// ── Toolbar ────────────────────────────────────────────────────────────────

type Toolbar struct {
	HBoxContainer.Extension[Toolbar] `gd:"Toolbar"`

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
	VBoxContainer.Extension[SchemaPanel] `gd:"SchemaPanel"`

	searchBox        LineEdit.Instance
	tree             Tree.Instance
	scrollBox        ScrollContainer.Instance
	colsList         VBoxContainer.Instance
	OnTableClicked   func(tableName string)
	OnColumnsChanged func(selected []string)
	allCols          []db.Column
	allTables        []db.TableInfo
	checkMode        bool
	selectAllRow     HBoxContainer.Instance
	selectAllDivider PanelContainer.Instance
	selectAllCb      CheckBox.Instance
	checkBoxes       []CheckBox.Instance
	checkCols        []string // column name per checkbox (parallel to checkBoxes)
	checkRows        []HBoxContainer.Instance
}

func (s *SchemaPanel) Ready() {
	s.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	s.AsControl().AddThemeConstantOverride("separation", 4)

	// Search input
	s.searchBox = LineEdit.New()
	s.searchBox.SetPlaceholderText("Search items…")
	s.searchBox.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	applyInputTheme(s.searchBox.AsControl())
	s.searchBox.OnTextChanged(func(text string) {
		if len(s.allTables) > 0 {
			s.filterTables(text)
		} else {
			s.filterCols(text)
		}
	})

	s.tree = Tree.New()
	s.tree.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
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

	// Scrollable column list (for parquet check-mode)
	s.scrollBox = ScrollContainer.New()
	s.scrollBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	s.scrollBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	s.scrollBox.AsCanvasItem().SetVisible(false)

	s.colsList = VBoxContainer.New()
	s.colsList.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	s.colsList.AsControl().AddThemeConstantOverride("separation", 4)
	s.scrollBox.AsNode().AddChild(s.colsList.AsNode())

	s.AsNode().AddChild(s.searchBox.AsNode())
	s.AsNode().AddChild(s.scrollBox.AsNode())
	s.AsNode().AddChild(s.tree.AsNode())
}

func (s *SchemaPanel) SetSchema(cols []db.Column) {
	s.allCols = cols
	s.allTables = nil
	s.checkMode = true
	s.searchBox.SetText("")
	s.tree.AsCanvasItem().SetVisible(false)
	s.scrollBox.AsCanvasItem().SetVisible(true)

	// Remove old select-all row + divider if exists
	if s.selectAllRow != (HBoxContainer.Instance{}) {
		s.colsList.AsNode().RemoveChild(s.selectAllRow.AsNode())
		s.selectAllRow.AsNode().QueueFree()
	}
	if s.selectAllDivider != (PanelContainer.Instance{}) {
		s.colsList.AsNode().RemoveChild(s.selectAllDivider.AsNode())
		s.selectAllDivider.AsNode().QueueFree()
	}

	// Select-all header row
	s.selectAllRow = HBoxContainer.New()
	s.selectAllRow.AsControl().AddThemeConstantOverride("separation", 4)
	s.selectAllRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	s.selectAllRow.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(24)))

	s.selectAllCb = CheckBox.New()
	s.selectAllCb.AsBaseButton().SetButtonPressed(true)
	applyCheckboxTheme(s.selectAllCb.AsControl())
	s.selectAllCb.AsBaseButton().OnToggled(func(pressed bool) {
		for _, cb := range s.checkBoxes {
			cb.AsBaseButton().SetPressedNoSignal(pressed)
		}
		if s.OnColumnsChanged != nil {
			s.OnColumnsChanged(s.getCheckedColumns())
		}
	})

	allLabel := Label.New()
	allLabel.SetText("Label")
	allLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	allLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)

	s.selectAllRow.AsNode().AddChild(s.selectAllCb.AsNode())
	s.selectAllRow.AsNode().AddChild(allLabel.AsNode())

	// Divider below header
	s.selectAllDivider = PanelContainer.New()
	s.selectAllDivider.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(1)))
	s.selectAllDivider.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	applyPanelBg(s.selectAllDivider.AsControl(), colorBorder)

	// Insert header and divider at the top of the scrollable column list
	s.colsList.AsNode().AddChild(s.selectAllRow.AsNode())
	s.colsList.AsNode().AddChild(s.selectAllDivider.AsNode())

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
	for i, cb := range s.checkBoxes {
		name := ""
		if i < len(s.checkCols) {
			name = s.checkCols[i]
		}
		checked := len(selected) == 0 || selSet[name]
		cb.AsBaseButton().SetPressedNoSignal(checked)
	}
}

func (s *SchemaPanel) selectOnly(target CheckBox.Instance) {
	for _, cb := range s.checkBoxes {
		cb.AsBaseButton().SetPressedNoSignal(false)
	}
	target.AsBaseButton().SetPressedNoSignal(true)
	s.selectAllCb.AsBaseButton().SetPressedNoSignal(false)
	if s.OnColumnsChanged != nil {
		s.OnColumnsChanged(s.getCheckedColumns())
	}
}

func (s *SchemaPanel) getCheckedColumns() []string {
	var cols []string
	for i, cb := range s.checkBoxes {
		if cb.AsBaseButton().ButtonPressed() && i < len(s.checkCols) {
			cols = append(cols, s.checkCols[i])
		}
	}
	return cols
}

func (s *SchemaPanel) filterCols(query string) {
	q := strings.ToLower(query)
	// Clear existing rows
	for _, row := range s.checkRows {
		s.colsList.AsNode().RemoveChild(row.AsNode())
		row.AsNode().QueueFree()
	}
	s.checkBoxes = nil
	s.checkCols = nil
	s.checkRows = nil

	for _, col := range s.allCols {
		if q != "" && !strings.Contains(strings.ToLower(col.Name), q) {
			continue
		}
		row := HBoxContainer.New()
		row.AsControl().AddThemeConstantOverride("separation", 4)
		row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		row.AsControl().SetMouseFilter(Control.MouseFilterStop)

		cb := CheckBox.New()
		cb.AsBaseButton().SetButtonPressed(true)
		applyCheckboxTheme(cb.AsControl())
		cb.AsControl().SetTooltipText(col.Name)
		// CheckBox keeps default MouseFilterStop for click handling
		cb.AsBaseButton().OnToggled(func(pressed bool) {
			if s.OnColumnsChanged != nil {
				s.OnColumnsChanged(s.getCheckedColumns())
			}
		})

		nameLabel := Label.New()
		nameLabel.SetText(col.Name)
		nameLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
		nameLabel.AsControl().AddThemeColorOverride("font_color", colorText)
		nameLabel.AsControl().SetMouseFilter(Control.MouseFilterPass)

		// Color-coded data-type chip (blue ints, green floats, amber time, …)
		typeChip := makeTypeChip(col.DataType)
		typeChip.AsControl().SetMouseFilter(Control.MouseFilterPass)

		// Spacer pushes the type chip to the right edge of the row.
		typeSpacer := Control.New()
		typeSpacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)
		typeSpacer.SetMouseFilter(Control.MouseFilterPass)

		// "only" link — hidden until hover
		onlyLabel := Label.New()
		onlyLabel.SetText("only")
		onlyLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
		onlyLabel.AsControl().AddThemeColorOverride("font_color", colorTextMuted)
		onlyLabel.AsControl().SetMouseFilter(Control.MouseFilterIgnore)
		onlyLabel.AsCanvasItem().SetVisible(false)

		// Capture for click on row — detect if over "only" label area
		cbRef := cb
		onlyLabelRef := onlyLabel

		// Hover: show "only" label
		row.AsControl().OnMouseEntered(func() {
			onlyLabelRef.AsCanvasItem().SetVisible(true)
		})
		row.AsControl().OnMouseExited(func() {
			onlyLabelRef.AsCanvasItem().SetVisible(false)
		})

		// Click on row: if over "only" label area, trigger selectOnly
		row.AsControl().OnGuiInput(func(event InputEvent.Instance) {
			if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
				if mb.AsInputEvent().IsPressed() && mb.ButtonIndex() == Input.MouseButtonLeft {
					// Check if click is within the "only" label's rect
					localPos := mb.AsInputEventMouse().Position()
					labelRect := onlyLabelRef.AsControl().GetRect()
					if localPos.X >= labelRect.Position.X && localPos.X <= labelRect.Position.X+labelRect.Size.X {
						s.selectOnly(cbRef)
					}
				}
			}
		})

		row.AsNode().AddChild(cb.AsNode())
		row.AsNode().AddChild(nameLabel.AsNode())
		row.AsNode().AddChild(typeSpacer.AsNode())
		row.AsNode().AddChild(onlyLabel.AsNode())
		row.AsNode().AddChild(typeChip.AsNode())
		s.colsList.AsNode().AddChild(row.AsNode())
		s.checkBoxes = append(s.checkBoxes, cb)
		s.checkCols = append(s.checkCols, col.Name)
		s.checkRows = append(s.checkRows, row)
	}
}

func (s *SchemaPanel) SetTables(tables []db.TableInfo) {
	s.allTables = tables
	s.allCols = nil
	s.checkMode = false
	s.searchBox.SetText("")
	// Clear checkbox rows + select-all
	if s.selectAllRow != (HBoxContainer.Instance{}) {
		s.colsList.AsNode().RemoveChild(s.selectAllRow.AsNode())
		s.selectAllRow.AsNode().QueueFree()
		s.selectAllRow = HBoxContainer.Instance{}
	}
	if s.selectAllDivider != (PanelContainer.Instance{}) {
		s.colsList.AsNode().RemoveChild(s.selectAllDivider.AsNode())
		s.selectAllDivider.AsNode().QueueFree()
		s.selectAllDivider = PanelContainer.Instance{}
	}
	for _, row := range s.checkRows {
		s.colsList.AsNode().RemoveChild(row.AsNode())
		row.AsNode().QueueFree()
	}
	s.checkBoxes = nil
	s.checkRows = nil
	s.scrollBox.AsCanvasItem().SetVisible(false)
	s.tree.AsCanvasItem().SetVisible(true)
	s.filterTables("")
}

func (s *SchemaPanel) filterTables(query string) {
	q := strings.ToLower(query)
	s.tree.Clear()
	s.tree.SetColumns(2)
	s.tree.SetColumnExpand(0, true)
	s.tree.SetColumnExpand(1, false)
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
	VBoxContainer.Extension[HistoryPanel] `gd:"HistoryPanel"`

	searchBox LineEdit.Instance
	scrollBox ScrollContainer.Instance
	rowsList  VBoxContainer.Instance
	entries   []models.HistoryEntry
	OnReplay  func(sql string) // callback when user replays a history entry
}

func (h *HistoryPanel) Ready() {
	h.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	h.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	h.AsControl().AddThemeConstantOverride("separation", 4)

	h.searchBox = LineEdit.New()
	h.searchBox.SetPlaceholderText("Filter history…")
	h.searchBox.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	applyInputTheme(h.searchBox.AsControl())
	h.searchBox.OnTextChanged(func(text string) { h.rebuild(text) })

	h.scrollBox = ScrollContainer.New()
	h.scrollBox.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	h.scrollBox.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	h.scrollBox.SetHorizontalScrollMode(ScrollContainer.ScrollModeDisabled)

	h.rowsList = VBoxContainer.New()
	h.rowsList.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	h.rowsList.AsControl().AddThemeConstantOverride("separation", 4)
	h.scrollBox.AsNode().AddChild(h.rowsList.AsNode())

	h.AsNode().AddChild(h.searchBox.AsNode())
	h.AsNode().AddChild(h.scrollBox.AsNode())
}

func (h *HistoryPanel) SetHistory(entries []models.HistoryEntry) {
	h.entries = entries
	h.searchBox.SetText("")
	h.rebuild("")
}

func (h *HistoryPanel) rebuild(query string) {
	for h.rowsList.AsNode().GetChildCount() > 0 {
		child := h.rowsList.AsNode().GetChild(0)
		h.rowsList.AsNode().RemoveChild(child)
		child.QueueFree()
	}
	q := strings.ToLower(query)
	now := time.Now()
	const maxRows = 100 // entries are newest-first; cap for performance
	shown := 0
	for _, e := range h.entries {
		if q != "" && !strings.Contains(strings.ToLower(e.SQL), q) {
			continue
		}
		h.rowsList.AsNode().AddChild(h.makeRow(e, now))
		if shown++; shown >= maxRows {
			break
		}
	}
}

// flattenSQL collapses whitespace/newlines so a query fits on one line.
func flattenSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// makeRow builds one history card: SQL text + status chip, a metadata line
// (relative time · duration · rows), and a re-run button revealed on hover.
func (h *HistoryPanel) makeRow(e models.HistoryEntry, now time.Time) Node.Instance {
	card := PanelContainer.New()
	card.AsNode().SetName("HistoryRow")
	card.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	cardBg := makeStyleBoxPadded(colorBgPanel, 4, 1, colorBorderDim, 6)
	card.AsControl().AddThemeStyleboxOverride("panel", cardBg.AsStyleBox())
	card.AsControl().SetTooltipText(e.SQL)

	box := VBoxContainer.New()
	box.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	box.AsControl().AddThemeConstantOverride("separation", 3)

	// Top line: SQL (monospace, truncated) + status chip
	topRow := HBoxContainer.New()
	topRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	topRow.AsControl().AddThemeConstantOverride("separation", 6)

	sqlLbl := Label.New()
	sqlLbl.SetText(flattenSQL(e.SQL))
	sqlLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(11))
	sqlLbl.AsControl().AddThemeColorOverride("font_color", colorText)
	sqlLbl.SetTextOverrunBehavior(TextServer.OverrunTrimEllipsis)
	sqlLbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	sqlLbl.SetClipText(true)
	sqlLbl.AsControl().AddThemeFontOverride("font", monoFont())

	statusText, statusColor := "SUCCESS", colorStatusGreen
	if e.Failed() {
		statusText, statusColor = "ERROR", colorStatusRed
	}
	statusChip := makeAccentChip(statusText, statusColor, "HistoryStatus")

	topRow.AsNode().AddChild(sqlLbl.AsNode())
	topRow.AsNode().AddChild(statusChip.AsNode())

	// Meta line: relative time · duration · rows + re-run (on hover)
	metaRow := HBoxContainer.New()
	metaRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	metaRow.AsControl().AddThemeConstantOverride("separation", 6)

	meta := models.RelativeTime(e.Timestamp, now) + "  ·  " + fmt.Sprintf("%dms", e.DurationMs)
	if !e.Failed() {
		meta += "  ·  " + fmt.Sprintf("%d rows", e.RowCount)
	}
	metaLbl := Label.New()
	metaLbl.SetText(meta)
	metaLbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))
	metaLbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	metaLbl.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	rerunBtn := Button.New()
	rerunBtn.SetText("↻")
	rerunBtn.AsControl().SetTooltipText("Re-run query")
	rerunBtn.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	applyNavButtonTheme(rerunBtn.AsControl())
	rerunBtn.AsCanvasItem().SetVisible(false)
	sql := e.SQL
	rerunBtn.AsBaseButton().OnPressed(func() {
		if h.OnReplay != nil {
			h.OnReplay(sql)
		}
	})

	metaRow.AsNode().AddChild(metaLbl.AsNode())
	metaRow.AsNode().AddChild(rerunBtn.AsNode())

	box.AsNode().AddChild(topRow.AsNode())
	box.AsNode().AddChild(metaRow.AsNode())
	card.AsNode().AddChild(box.AsNode())

	// Reveal re-run on hover; click anywhere on the card replays the query.
	card.AsControl().OnMouseEntered(func() { rerunBtn.AsCanvasItem().SetVisible(true) })
	card.AsControl().OnMouseExited(func() { rerunBtn.AsCanvasItem().SetVisible(false) })
	card.AsControl().OnGuiInput(func(event InputEvent.Instance) {
		if mb, ok := Object.As[InputEventMouseButton.Instance](event); ok {
			if mb.AsInputEvent().IsPressed() && mb.ButtonIndex() == Input.MouseButtonLeft {
				if h.OnReplay != nil {
					h.OnReplay(sql)
				}
			}
		}
	})

	return card.AsNode()
}

// ── SQL editor ─────────────────────────────────────────────────────────────

type SQLPanel struct {
	VBoxContainer.Extension[SQLPanel] `gd:"SQLPanel"`

	editor     CodeEdit.Instance
	OnRunQuery func(sql string)

	// Autocomplete data
	columns []db.Column   // current schema columns
	tables  []db.TableInfo // current database tables (for .duckdb files)
}

func (s *SQLPanel) Ready() {
	s.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	s.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	s.AsControl().AddThemeConstantOverride("separation", 4)

	// Top row: label + run button
	row := HBoxContainer.New()
	row.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	row.AsControl().AddThemeConstantOverride("separation", 6)

	label := Label.New()
	label.SetText("SQL")
	applyLabelTheme(label.AsControl(), true)
	label.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)

	// Run lives in the title bar; a "⌘↵ / Ctrl+↵" hint keeps the shortcut discoverable.
	hint := Label.New()
	hint.SetText("⌘↵ to run")
	hint.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	hint.AsControl().AddThemeFontSizeOverride("font_size", fontSize(10))

	row.AsNode().AddChild(label.AsNode())
	row.AsNode().AddChild(hint.AsNode())

	s.editor = CodeEdit.New()
	s.editor.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	s.editor.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	s.editor.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(80)))
	s.editor.SetGuttersDrawExecutingLines(false)
	s.editor.SetGuttersDrawLineNumbers(true)
	s.editor.SetGuttersDrawBreakpointsGutter(false)
	s.editor.SetGuttersDrawBookmarks(false)
	applyTextEditTheme(s.editor.AsControl())

	// SQL syntax highlighting
	setupSQLHighlighter(s.editor)

	// Code completion
	s.editor.SetCodeCompletionEnabled(true)
	s.editor.OnCodeCompletionRequested(func() {
		s.populateCompletions()
	})
	// Trigger completion on every text change while typing a word
	s.editor.AsTextEdit().OnTextChanged(func() {
		prefix := s.currentWordPrefix()
		if len(prefix) >= 1 {
			s.editor.RequestCodeCompletion()
		}
	})

	// ⌘↵ / Ctrl+↵ runs the query (Run itself lives in the title bar).
	s.editor.AsControl().OnGuiInput(func(event InputEvent.Instance) {
		ke, ok := Object.As[InputEventKey.Instance](event)
		if !ok || !ke.AsInputEvent().IsPressed() {
			return
		}
		if ke.Keycode() == Input.KeyEnter && ke.AsInputEventWithModifiers().IsCommandOrControlPressed() {
			s.editor.AsControl().AcceptEvent() // don't insert a newline
			s.Run()
		}
	})

	s.AsNode().AddChild(row.AsNode())
	s.AsNode().AddChild(s.editor.AsNode())
}

// Run executes the current editor contents via OnRunQuery.
func (s *SQLPanel) Run() {
	if s.OnRunQuery != nil {
		s.OnRunQuery(s.editor.AsTextEdit().Text())
	}
}

func (s *SQLPanel) SetSQL(sql string) {
	s.editor.AsTextEdit().SetText(sql)
}

// SetCompletionSchema updates the column list used for autocomplete.
func (s *SQLPanel) SetCompletionSchema(cols []db.Column) {
	s.columns = cols
}

// SetCompletionTables updates the table list used for autocomplete.
func (s *SQLPanel) SetCompletionTables(tables []db.TableInfo) {
	s.tables = tables
}

// populateCompletions adds completion options based on the current word prefix.
func (s *SQLPanel) populateCompletions() {
	prefix := s.currentWordPrefix()
	items := completion.Build(prefix, s.columns, s.tables)
	if len(items) == 0 {
		return
	}
	for _, item := range items {
		s.editor.AddCodeCompletionOption(
			CodeEdit.CodeCompletionKind(item.Kind), item.Display, item.InsertText,
		)
	}
	s.editor.UpdateCodeCompletionOptions(true)
}

// currentWordPrefix returns the text of the word currently being typed at the cursor.
func (s *SQLPanel) currentWordPrefix() string {
	te := s.editor.AsTextEdit()
	line := te.GetLine(te.GetCaretLine())
	col := te.GetCaretColumn()
	return completion.WordPrefixAt(line, int(col))
}

// ── Data grid ──────────────────────────────────────────────────────────────

type DataGrid struct {
	Tree.Extension[DataGrid] `gd:"DataGrid"`

	OnColumnClicked    func(column int)
	OnRowSelected      func(rowIndex int)
	OnRowsSelected     func(rowIndices []int)
	OnSelectionCleared func()
	columns          []string // track current column names
	rows             [][]string
	colTypes         []string // data types for alignment
	colWidthCache    map[string][]int // query hash → column widths
	dragging         bool
	dragCol          int
	dragStartX       float32
	dragStartWidth   int
	skipSort         bool              // set during resize to suppress column sort
	selectedItem     TreeItem.Instance  // previously selected item (for clearing cell border)
	selectedCol      int               // previously selected column
	cellEdit         LineEdit.Instance  // overlay for copying cell text
	contextMenu      PopupMenu.Instance // right-click context menu
	selectedRows     map[int]bool       // set of selected row indices for multi-select
	lastSelectedRow  int               // anchor for shift-click range selection
	mouseHandled     bool              // suppress OnItemSelected after mouse click handling
}

func (d *DataGrid) Ready() {
	d.Super().SetColumns(1)
	d.Super().SetColumnTitlesVisible(true)
	d.Super().SetHideRoot(true)
	d.Super().SetSelectMode(Tree.SelectRow)
	d.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	applyTreeTheme(d.AsControl())

	d.Super().OnColumnTitleClicked(func(column int, mouseButton int) {
		if d.skipSort {
			d.skipSort = false
			return
		}
		if d.OnColumnClicked != nil {
			d.OnColumnClicked(column)
		}
	})

	d.selectedCol = -1
	d.selectedRows = make(map[int]bool)
	d.lastSelectedRow = -1

	d.Super().OnItemSelected(func() {
		// Mouse clicks handle multi-select in GuiInput; skip duplicate processing
		if d.mouseHandled {
			d.mouseHandled = false
			return
		}
		// Clear cell highlight when navigating with arrow keys
		d.clearCellHighlight()
		selected := d.Super().GetSelected()
		if selected == (TreeItem.Instance{}) {
			return
		}
		idx := d.treeItemIndex(selected)
		if idx < 0 {
			return
		}
		// Shift+arrow key extends the selection range from the anchor
		if Input.IsKeyPressed(Input.KeyShift) && d.lastSelectedRow >= 0 {
			d.clearRowHighlights()
			d.selectedRows = make(map[int]bool)
			lo, hi := d.lastSelectedRow, idx
			if lo > hi {
				lo, hi = hi, lo
			}
			for i := lo; i <= hi; i++ {
				d.selectedRows[i] = true
			}
			d.applyRowHighlights()
			d.notifyRowsSelected()
			return
		}
		// Plain arrow key: single-select
		d.clearRowHighlights()
		d.selectedRows = make(map[int]bool)
		d.selectedRows[idx] = true
		d.lastSelectedRow = idx
		d.applyRowHighlights()
		d.notifyRowsSelected()
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
	d.dismissCellEdit()
	d.selectedItem = TreeItem.Instance{}
	d.selectedCol = -1
	d.selectedRows = make(map[int]bool)
	d.lastSelectedRow = -1
	// Model the error as a single-cell result so the standard cell-copy paths
	// (double-click overlay, right-click Copy) work on the error text.
	d.columns = []string{"Error"}
	d.rows = [][]string{{msg}}
	d.colTypes = []string{"VARCHAR"}
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
	d.dismissCellEdit()
	d.selectedItem = TreeItem.Instance{}
	d.selectedCol = -1
	d.selectedRows = make(map[int]bool)
	d.lastSelectedRow = -1
	d.columns = r.Columns
	d.rows = r.Rows
	t := d.Super()
	t.Clear()
	t.SetColumns(len(r.Columns))
	for i, col := range r.Columns {
		t.SetColumnTitle(i, col)
		t.SetColumnClipContent(i, true)
	}
	// Set column title alignment for numeric types
	for i := range r.Columns {
		if d.isNumericCol(i) {
			t.SetColumnTitleAlignment(i, GUI.HorizontalAlignmentRight) // right align
		}
	}

	root := t.CreateItem()
	for _, row := range r.Rows {
		item := t.MoreArgs().CreateItem(root, -1)
		for i, cell := range row {
			item.SetText(i, cell)
			item.SetTextOverrunBehavior(i, TextServer.OverrunTrimEllipsis)
			if d.isNumericCol(i) {
				item.SetTextAlignment(i, GUI.HorizontalAlignmentRight) // right align
			}
		}
	}
	d.autoSizeColumns(r)
}

func (d *DataGrid) isNumericCol(i int) bool {
	if i >= len(d.colTypes) {
		return false
	}
	t := strings.ToUpper(d.colTypes[i])
	return strings.Contains(t, "INT") || strings.Contains(t, "FLOAT") ||
		strings.Contains(t, "DOUBLE") || strings.Contains(t, "DECIMAL") ||
		strings.Contains(t, "NUMERIC") || strings.Contains(t, "REAL") ||
		t == "BIGINT" || t == "SMALLINT" || t == "TINYINT" ||
		t == "HUGEINT" || t == "UBIGINT" || t == "UINTEGER" ||
		t == "USMALLINT" || t == "UTINYINT"
}

func (d *DataGrid) queryKey() string {
	// Simple key from column names
	return strings.Join(d.columns, "|")
}

func (d *DataGrid) autoSizeColumns(r *db.QueryResult) {
	t := d.Super()
	numCols := len(r.Columns)
	if numCols == 0 {
		return
	}

	// Check cache
	key := d.queryKey()
	if d.colWidthCache != nil {
		if cached, ok := d.colWidthCache[key]; ok && len(cached) == numCols {
			for i, w := range cached {
				t.SetColumnExpand(i, false)
				t.SetColumnCustomMinimumWidth(i, w)
			}
			// Last column expands to fill
			t.SetColumnExpand(numCols-1, true)
			return
		}
	}

	// Compute minimum header widths from the actual displayed title (includes sort indicator).
	// Reserve extra space so a sort indicator (" ▲") can be appended without wrapping.
	// 8px per char at 13px font + 8+8 padding + 1 border + margin = 25px overhead.
	headerWidths := make([]int, numCols)
	for i, col := range r.Columns {
		// Use raw column name + 2 extra chars for potential sort indicator
		headerWidths[i] = (len(col)+2)*8 + 25
	}

	// Estimate widths from data content (first 50 rows)
	contentWidths := make([]int, numCols)
	sampleRows := len(r.Rows)
	if sampleRows > 50 {
		sampleRows = 50
	}
	for _, row := range r.Rows[:sampleRows] {
		for i, cell := range row {
			if len(cell) > contentWidths[i] {
				contentWidths[i] = len(cell)
			}
		}
	}

	// Use the larger of header or content width for each column
	widths := make([]int, numCols)
	for i := range widths {
		cw := contentWidths[i]*8 + 24
		w := headerWidths[i]
		if cw > w {
			w = cw
		}
		if w < 60 {
			w = 60
		}
		if w > 400 {
			w = 400
		}
		widths[i] = w
		t.SetColumnExpand(i, false)
		t.SetColumnCustomMinimumWidth(i, w)
	}
	// Last column expands to fill remaining space
	t.SetColumnExpand(numCols-1, true)

	// Cache
	if d.colWidthCache == nil {
		d.colWidthCache = make(map[string][]int)
	}
	d.colWidthCache[key] = widths
}

// headerHeight returns the height of the column title row in pixels.
func (d *DataGrid) headerHeight() float32 {
	// font size + stylebox padding (3 top + 3 bottom) + border (1) + internal margin
	return float32(fontSize(13) + 12)
}

func (d *DataGrid) colBorderHit(pos Vector2.XY) int {
	// Only activate in the header area
	if float32(pos.Y) > d.headerHeight() {
		return -1
	}
	// Check if x is near a column border (within 8px for easier targeting)
	t := d.Super()
	x := float32(pos.X)
	offset := 0
	for i := 0; i < len(d.columns)-1; i++ {
		offset += t.GetColumnWidth(i)
		if x >= float32(offset-8) && x <= float32(offset+8) {
			return i
		}
	}
	return -1
}

// autoFitColumn resizes a column to fit its content (header + all visible rows).
func (d *DataGrid) autoFitColumn(col int) {
	if col < 0 || col >= len(d.columns) {
		return
	}
	// Header needs room for column name + sort indicator
	headerChars := len(d.columns[col]) + 2
	maxChars := headerChars
	for _, row := range d.rows {
		if col < len(row) && len(row[col]) > maxChars {
			maxChars = len(row[col])
		}
	}
	w := maxChars*8 + 25
	if w < 60 {
		w = 60
	}
	if w > 600 {
		w = 600
	}
	d.Super().SetColumnCustomMinimumWidth(col, w)
}

// saveWidthsToCache stores the current column widths into the cache.
func (d *DataGrid) saveWidthsToCache() {
	key := d.queryKey()
	if d.colWidthCache == nil {
		d.colWidthCache = make(map[string][]int)
	}
	widths := make([]int, len(d.columns))
	for i := range d.columns {
		widths[i] = d.Super().GetColumnWidth(i)
	}
	d.colWidthCache[key] = widths
}

func (d *DataGrid) GuiInput(event InputEvent.Instance) {
	mb, isMouse := Object.As[InputEventMouseButton.Instance](event)
	if isMouse {
		if mb.ButtonIndex() == Input.MouseButtonLeft {
			if mb.AsInputEvent().IsPressed() {
				col := d.colBorderHit(mb.AsInputEventMouse().Position())
				if col >= 0 {
					if mb.DoubleClick() {
						// Double-click on column border: auto-fit column width to content
						d.autoFitColumn(col)
						d.saveWidthsToCache()
					} else {
						d.dragging = true
						d.dragCol = col
						d.dragStartX = mb.AsInputEventMouse().Position().X
						d.dragStartWidth = d.Super().GetColumnWidth(col)
					}
					d.skipSort = true           // suppress the column-title-click sort
					d.AsControl().AcceptEvent()
				} else {
					// Cell click handling
					pos := mb.AsInputEventMouse().Position()
					clickCol := d.Super().GetColumnAtPosition(pos)
					clickItem := d.Super().GetItemAtPosition(pos)
					if clickItem != (TreeItem.Instance{}) && clickCol >= 0 && clickCol < len(d.columns) {
						if mb.DoubleClick() {
							// Double-click on cell: show copyable overlay
							d.showCellEdit(clickItem, clickCol)
							d.AsControl().AcceptEvent()
						} else {
							d.dismissCellEdit()

							// Multi-row selection (skip duplicate in OnItemSelected)
							d.mouseHandled = true
							clickedRow := d.treeItemIndex(clickItem)
							if clickedRow >= 0 {
								cmdHeld := Input.IsKeyPressed(Input.KeyMeta) || Input.IsKeyPressed(Input.KeyCtrl)
								shiftHeld := Input.IsKeyPressed(Input.KeyShift)

								if shiftHeld && d.lastSelectedRow >= 0 {
									// Shift+click: select range from anchor to clicked row
									d.clearCellHighlight()
									d.clearRowHighlights()
									d.selectedRows = make(map[int]bool)
									lo, hi := d.lastSelectedRow, clickedRow
									if lo > hi {
										lo, hi = hi, lo
									}
									for i := lo; i <= hi; i++ {
										d.selectedRows[i] = true
									}
									d.applyRowHighlights()
									d.notifyRowsSelected()
								} else if cmdHeld {
									// Cmd/Ctrl+click: toggle individual row
									d.clearCellHighlight()
									d.clearRowHighlights()
									if d.selectedRows[clickedRow] {
										delete(d.selectedRows, clickedRow)
									} else {
										d.selectedRows[clickedRow] = true
									}
									d.lastSelectedRow = clickedRow
									d.applyRowHighlights()
									d.notifyRowsSelected()
								} else {
									// Plain click: select single row, highlight clicked cell
									d.clearCellHighlight()
									d.clearRowHighlights()
									d.selectedRows = make(map[int]bool)
									d.selectedRows[clickedRow] = true
									d.lastSelectedRow = clickedRow
									d.applyRowHighlights()
									d.notifyRowsSelected()
									// Cell border highlight for single click
									border := makeStyleBox(colorSelected, 0, 2, colorTextMuted)
									clickItem.SetCustomStylebox(clickCol, border.AsStyleBox())
									d.selectedItem = clickItem
									d.selectedCol = clickCol
								}
							}
						}
					}
				}
			} else {
				if d.dragging {
					d.dragging = false
					d.saveWidthsToCache()
					d.AsControl().SetMouseDefaultCursorShape(Control.CursorArrow)
				}
			}
		}
		if mb.ButtonIndex() == Input.MouseButtonRight && mb.AsInputEvent().IsPressed() {
			pos := mb.AsInputEventMouse().Position()
			clickCol := d.Super().GetColumnAtPosition(pos)
			clickItem := d.Super().GetItemAtPosition(pos)
			if clickItem != (TreeItem.Instance{}) && clickCol >= 0 && clickCol < len(d.columns) {
				clickedRow := d.treeItemIndex(clickItem)
				if clickedRow >= 0 && !d.selectedRows[clickedRow] {
					// Right-clicked a row outside the current selection: select just this row
					d.clearCellHighlight()
					d.clearRowHighlights()
					d.selectedRows = make(map[int]bool)
					d.selectedRows[clickedRow] = true
					d.lastSelectedRow = clickedRow
					d.applyRowHighlights()
					d.notifyRowsSelected()
				}
			}
			if len(d.selectedRows) > 0 {
				d.showContextMenu(clickCol)
			}
			d.AsControl().AcceptEvent()
		}
		return
	}
	kb, isKey := Object.As[InputEventKey.Instance](event)
	if isKey && kb.AsInputEvent().IsPressed() && kb.Keycode() == Input.KeyEscape {
		if len(d.selectedRows) > 0 {
			d.clearCellHighlight()
			d.clearRowHighlights()
			d.selectedRows = make(map[int]bool)
			d.lastSelectedRow = -1
			d.Super().DeselectAll()
			if d.OnSelectionCleared != nil {
				d.OnSelectionCleared()
			}
			d.AsControl().AcceptEvent()
		}
		return
	}
	mm, isMotion := Object.As[InputEventMouseMotion.Instance](event)
	if isMotion {
		if d.dragging {
			delta := mm.AsInputEventMouse().Position().X - d.dragStartX
			newWidth := d.dragStartWidth + int(delta)
			if newWidth < 40 {
				newWidth = 40
			}
			d.Super().SetColumnCustomMinimumWidth(d.dragCol, newWidth)
		} else {
			// Show resize cursor when hovering near column borders in the header
			col := d.colBorderHit(mm.AsInputEventMouse().Position())
			if col >= 0 {
				d.AsControl().SetMouseDefaultCursorShape(Control.CursorHsize)
			} else {
				d.AsControl().SetMouseDefaultCursorShape(Control.CursorArrow)
			}
		}
	}
}

func (d *DataGrid) clearCellHighlight() {
	if d.selectedItem != (TreeItem.Instance{}) && d.selectedCol >= 0 {
		clear := StyleBoxEmpty.New()
		d.selectedItem.SetCustomStylebox(d.selectedCol, clear.AsStyleBox())
		d.selectedItem = TreeItem.Instance{}
		d.selectedCol = -1
	}
	d.dismissCellEdit()
}

func (d *DataGrid) dismissCellEdit() {
	if d.cellEdit != (LineEdit.Instance{}) {
		d.cellEdit.AsNode().QueueFree()
		d.cellEdit = LineEdit.Instance{}
	}
}

func (d *DataGrid) showCellEdit(item TreeItem.Instance, col int) {
	d.dismissCellEdit()

	rect := d.Super().MoreArgs().GetItemAreaRect(item, col, -1)
	scroll := d.Super().GetScroll()
	rect.Position.Y -= scroll.Y

	edit := LineEdit.New()
	edit.SetText(item.GetText(col))
	edit.SetEditable(false)
	edit.AsControl().SetPosition(Vector2.New(rect.Position.X, rect.Position.Y))
	edit.AsControl().SetSize(Vector2.New(rect.Size.X, rect.Size.Y))
	edit.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
	applyInputTheme(edit.AsControl())
	d.AsNode().AddChild(edit.AsNode())
	edit.AsControl().GrabFocus()
	edit.SelectAll()

	edit.AsControl().OnFocusExited(func() {
		d.dismissCellEdit()
	})
	edit.AsControl().OnGuiInput(func(event InputEvent.Instance) {
		if kb, ok := Object.As[InputEventKey.Instance](event); ok {
			if kb.AsInputEvent().IsPressed() && kb.Keycode() == Input.KeyEscape {
				d.dismissCellEdit()
			}
		}
	})

	d.cellEdit = edit
}

const (
	copyTSVWithHeaders = 0
	copyTSV            = 1
	copyColumnValues   = 2
)

func (d *DataGrid) dismissContextMenu() {
	if d.contextMenu != (PopupMenu.Instance{}) {
		d.contextMenu.AsNode().QueueFree()
		d.contextMenu = PopupMenu.Instance{}
	}
}

func (d *DataGrid) showContextMenu(col int) {
	d.dismissContextMenu()

	copyMenu := PopupMenu.New()
	copyMenu.AddItem("TSV with Headers")
	copyMenu.AddItem("TSV")
	copyMenu.AddItem("Column Values")
	copyMenu.OnIndexPressed(func(index int) {
		switch index {
		case copyTSVWithHeaders:
			d.copySelectedRows(true)
		case copyTSV:
			d.copySelectedRows(false)
		case copyColumnValues:
			d.copyColumnValues(col)
		}
		d.dismissContextMenu()
	})

	popup := PopupMenu.New()
	popup.AddSubmenuNodeItem("Copy", copyMenu)
	d.AsNode().AddChild(popup.AsNode())

	popup.AsWindow().OnCloseRequested(func() {
		d.dismissContextMenu()
	})

	// Position at mouse cursor in screen coordinates
	popup.AsWindow().SetPosition(DisplayServer.MouseGetPosition())
	popup.AsWindow().Popup()
	d.contextMenu = popup
}

func (d *DataGrid) copySelectedRows(withHeaders bool) {
	indices := d.sortedSelectedRows()
	if len(indices) == 0 {
		return
	}
	rows := make([][]string, 0, len(indices))
	for _, idx := range indices {
		if idx < len(d.rows) {
			rows = append(rows, d.rows[idx])
		}
	}
	DisplayServer.ClipboardSet(models.FormatRowsTSV(d.columns, rows, withHeaders))
}

func (d *DataGrid) copyColumnValues(col int) {
	if col < 0 || col >= len(d.columns) {
		return
	}
	indices := d.sortedSelectedRows()
	if len(indices) == 0 {
		return
	}
	rows := make([][]string, 0, len(indices))
	for _, idx := range indices {
		if idx < len(d.rows) {
			rows = append(rows, d.rows[idx])
		}
	}
	DisplayServer.ClipboardSet(models.FormatColumnValues(rows, col, d.isNumericCol(col)))
}

// treeItemIndex returns the row index for a TreeItem, or -1 if not found.
func (d *DataGrid) treeItemIndex(item TreeItem.Instance) int {
	root := d.Super().GetRoot()
	if root == (TreeItem.Instance{}) {
		return -1
	}
	child := root.GetFirstChild()
	idx := 0
	for child != (TreeItem.Instance{}) {
		if child == item {
			return idx
		}
		child = child.GetNext()
		idx++
	}
	return -1
}

// treeItemAtIndex returns the TreeItem at the given row index, or zero value if out of range.
func (d *DataGrid) treeItemAtIndex(idx int) TreeItem.Instance {
	root := d.Super().GetRoot()
	if root == (TreeItem.Instance{}) {
		return TreeItem.Instance{}
	}
	child := root.GetFirstChild()
	for i := 0; child != (TreeItem.Instance{}); i++ {
		if i == idx {
			return child
		}
		child = child.GetNext()
	}
	return TreeItem.Instance{}
}

// clearRowHighlights removes the custom background color from all selected rows.
func (d *DataGrid) clearRowHighlights() {
	for idx := range d.selectedRows {
		item := d.treeItemAtIndex(idx)
		if item != (TreeItem.Instance{}) {
			for c := 0; c < len(d.columns); c++ {
				item.ClearCustomBgColor(c)
			}
		}
	}
}

// applyRowHighlights sets the custom background color on all selected rows.
func (d *DataGrid) applyRowHighlights() {
	for idx := range d.selectedRows {
		item := d.treeItemAtIndex(idx)
		if item != (TreeItem.Instance{}) {
			for c := 0; c < len(d.columns); c++ {
				item.SetCustomBgColor(c, colorSelected)
			}
		}
	}
}

// sortedSelectedRows returns the selected row indices in ascending order.
func (d *DataGrid) sortedSelectedRows() []int {
	rows := make([]int, 0, len(d.selectedRows))
	for idx := range d.selectedRows {
		rows = append(rows, idx)
	}
	sort.Ints(rows)
	return rows
}

// notifyRowsSelected calls the appropriate callback with the current selection.
func (d *DataGrid) notifyRowsSelected() {
	if d.OnRowsSelected != nil {
		d.OnRowsSelected(d.sortedSelectedRows())
	} else if d.OnRowSelected != nil && len(d.selectedRows) == 1 {
		for idx := range d.selectedRows {
			d.OnRowSelected(idx)
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
	VBoxContainer.Extension[RowDetailPanel] `gd:"RowDetail"`

	searchBox    LineEdit.Instance
	scrollBox    ScrollContainer.Instance
	fieldsList   VBoxContainer.Instance
	placeholder  VBoxContainer.Instance
	columns      []string
	colTypes     []string   // parallel to columns; drives the type chips
	values       []string
	multiRows    [][]string // all selected rows for multi-select display
}

func (p *RowDetailPanel) Ready() {
	p.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
	p.AsControl().SetSizeFlagsVertical(Control.SizeExpandFill)
	p.AsControl().AddThemeConstantOverride("separation", 0)
	p.AsControl().SetClipContents(true)

	// Search input
	p.searchBox = LineEdit.New()
	p.searchBox.SetPlaceholderText("Search fields…")
	p.searchBox.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
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
	phIcon.AsControl().AddThemeFontSizeOverride("font_size", fontSize(32))
	phIcon.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	phIcon.SetHorizontalAlignment(1)

	phText := Label.New()
	phText.SetText("No row selected")
	phText.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
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
	sep.AsControl().SetCustomMinimumSize(Vector2.New(0, scaled(1)))
	applyPanelBg(sep.AsControl(), colorBorder)

	p.AsNode().AddChild(searchWrap.AsNode())
	p.AsNode().AddChild(sep.AsNode())
	p.AsNode().AddChild(p.placeholder.AsNode())
	p.AsNode().AddChild(p.scrollBox.AsNode())
}

func (p *RowDetailPanel) SetRow(columns, colTypes, values []string) {
	p.columns = columns
	p.colTypes = colTypes
	p.values = values
	p.multiRows = nil
	p.searchBox.SetText("")
	p.placeholder.AsCanvasItem().SetVisible(false)
	p.scrollBox.AsCanvasItem().SetVisible(true)
	p.filterFields("")
}

// SetRows displays detail for multiple selected rows. If all rows share
// the same value for a column, that value is shown; otherwise "—" is displayed.
func (p *RowDetailPanel) SetRows(columns, colTypes []string, rows [][]string) {
	if len(rows) == 1 {
		p.SetRow(columns, colTypes, rows[0])
		return
	}
	p.columns = columns
	p.colTypes = colTypes
	p.values = nil
	p.multiRows = rows
	p.searchBox.SetText("")
	p.placeholder.AsCanvasItem().SetVisible(false)
	p.scrollBox.AsCanvasItem().SetVisible(true)
	p.filterFields("")
}

func (p *RowDetailPanel) Clear() {
	p.columns = nil
	p.values = nil
	p.multiRows = nil
	p.clearFields()
	p.searchBox.SetText("")
	p.scrollBox.AsCanvasItem().SetVisible(false)
	p.placeholder.AsCanvasItem().SetVisible(true)
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
		val := p.resolveValue(i)
		if query != "" && !strings.Contains(strings.ToLower(col), query) && !strings.Contains(strings.ToLower(val), query) {
			continue
		}
		// Field group: label + value input
		group := VBoxContainer.New()
		group.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		group.AsControl().AddThemeConstantOverride("separation", 2)

		// Label row: field name + right-aligned color-coded type chip
		lblRow := HBoxContainer.New()
		lblRow.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		lblRow.AsControl().AddThemeConstantOverride("separation", 4)

		lbl := Label.New()
		lbl.SetText(col)
		lbl.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
		lbl.AsControl().AddThemeColorOverride("font_color", colorTextDim)

		lblSpacer := Control.New()
		lblSpacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)

		lblRow.AsNode().AddChild(lbl.AsNode())
		lblRow.AsNode().AddChild(lblSpacer.AsNode())
		if i < len(p.colTypes) && p.colTypes[i] != "" {
			lblRow.AsNode().AddChild(makeTypeChip(p.colTypes[i]).AsNode())
		}

		// Value (read-only input for copyable text)
		valInput := LineEdit.New()
		valInput.SetText(val)
		valInput.SetEditable(false)
		valInput.AsControl().AddThemeFontSizeOverride("font_size", fontSize(13))
		valInput.AsControl().SetSizeFlagsHorizontal(Control.SizeExpandFill)
		applyInputTheme(valInput.AsControl())

		group.AsNode().AddChild(lblRow.AsNode())
		group.AsNode().AddChild(valInput.AsNode())
		p.fieldsList.AsNode().AddChild(group.AsNode())
	}
}

// resolveValue returns the display value for column i. For single-row selection
// it returns the value directly. For multi-row, it returns the value if all rows
// agree, or "—" if they differ.
func (p *RowDetailPanel) resolveValue(i int) string {
	if p.multiRows == nil {
		return models.ResolveDetailValue(i, p.values, nil)
	}
	return models.ResolveDetailValue(i, nil, p.multiRows)
}

// ── Status bar ─────────────────────────────────────────────────────────────

type StatusBar struct {
	HBoxContainer.Extension[StatusBar] `gd:"StatusBar"`

	statusLabel Label.Instance
	pageLabel   Label.Instance
	connDot     Label.Instance
	connLabel   Label.Instance
	memPill     Label.Instance
	leftBtn     Button.Instance
	bottomBtn   Button.Instance
	rightBtn    Button.Instance

	OnPrevPage         func()
	OnNextPage         func()
	OnFirstPage        func()
	OnLastPage         func()
	OnToggleLeftPane   func()
	OnToggleRightPane  func()
	OnToggleBottomPane func()

	leftPaneVisible   bool
	rightPaneVisible  bool
	bottomPaneVisible bool
	memFrame          int
}

// footerDivider returns a thin vertical rule separating status-bar segments.
func footerDivider() PanelContainer.Instance {
	d := PanelContainer.New()
	d.AsControl().SetCustomMinimumSize(Vector2.New(scaled(1), scaled(12)))
	d.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	applyPanelBg(d.AsControl(), colorBorder)
	return d
}

func (s *StatusBar) footerIconButton(glyph, tooltip string, onPress func()) Button.Instance {
	b := Button.New()
	b.SetText(glyph)
	b.AsControl().SetTooltipText(tooltip)
	b.AsControl().SetCustomMinimumSize(Vector2.New(scaled(22), scaled(20)))
	applyFooterGhostButton(b.AsControl())
	if onPress != nil {
		b.AsBaseButton().OnPressed(onPress)
	}
	return b
}

func (s *StatusBar) footerText(text string, c Color.RGBA) Label.Instance {
	l := Label.New()
	l.SetText(text)
	l.AsControl().AddThemeColorOverride("font_color", c)
	l.AsControl().AddThemeFontSizeOverride("font_size", fontSize(navFontBase))
	l.AsControl().AddThemeFontOverride("font", monoFont())
	l.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	return l
}

func (s *StatusBar) Ready() {
	s.AsControl().AddThemeConstantOverride("separation", 8)
	s.leftPaneVisible = true
	s.rightPaneVisible = false

	// ── Left: pane toggles ──────────────────────────────────────────────
	s.leftBtn = s.footerIconButton("◧", "Toggle Left Pane", func() {
		s.leftPaneVisible = !s.leftPaneVisible
		if s.OnToggleLeftPane != nil {
			s.OnToggleLeftPane()
		}
	})
	s.bottomBtn = s.footerIconButton("▤", "Toggle Console (bottom pane)", func() {
		s.bottomPaneVisible = !s.bottomPaneVisible
		if s.OnToggleBottomPane != nil {
			s.OnToggleBottomPane()
		}
	})
	s.rightBtn = s.footerIconButton("◨", "Toggle Right Pane", func() {
		s.rightPaneVisible = !s.rightPaneVisible
		if s.OnToggleRightPane != nil {
			s.OnToggleRightPane()
		}
	})

	// ── Connection status: green dot + name ─────────────────────────────
	s.connDot = Label.New()
	s.connDot.SetText("●")
	s.connDot.AsControl().AddThemeColorOverride("font_color", colorStatusGreen)
	s.connDot.AsControl().AddThemeFontSizeOverride("font_size", fontSize(8))
	s.connDot.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	s.connLabel = s.footerText("in-memory", colorTypeInt) // #89CEFF blue

	// ── Query metrics / transient status ────────────────────────────────
	s.statusLabel = s.footerText("Ready", colorTextMuted)

	// Spacer pushes pagination + mem pill to the right.
	spacer := Control.New()
	spacer.SetSizeFlagsHorizontal(Control.SizeExpandFill)

	// ── Right: pagination ───────────────────────────────────────────────
	firstBtn := s.footerIconButton("«", "First Page", func() {
		if s.OnFirstPage != nil {
			s.OnFirstPage()
		}
	})
	prevBtn := s.footerIconButton("‹", "Previous Page", func() {
		if s.OnPrevPage != nil {
			s.OnPrevPage()
		}
	})
	s.pageLabel = s.footerText("Page 1 / 1", colorTextMuted)
	nextBtn := s.footerIconButton("›", "Next Page", func() {
		if s.OnNextPage != nil {
			s.OnNextPage()
		}
	})
	lastBtn := s.footerIconButton("»", "Last Page", func() {
		if s.OnLastPage != nil {
			s.OnLastPage()
		}
	})

	// ── Memory pill ─────────────────────────────────────────────────────
	pill := PanelContainer.New()
	pill.AsControl().AddThemeStyleboxOverride("panel", footerPill().AsStyleBox())
	pill.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	s.memPill = s.footerText("MEM —", colorTextMuted)
	pill.AsNode().AddChild(s.memPill.AsNode())

	// ── Assemble ────────────────────────────────────────────────────────
	s.AsNode().AddChild(s.leftBtn.AsNode())
	s.AsNode().AddChild(s.bottomBtn.AsNode())
	s.AsNode().AddChild(s.rightBtn.AsNode())
	s.AsNode().AddChild(footerDivider().AsNode())
	s.AsNode().AddChild(s.connDot.AsNode())
	s.AsNode().AddChild(s.connLabel.AsNode())
	s.AsNode().AddChild(footerDivider().AsNode())
	s.AsNode().AddChild(s.statusLabel.AsNode())
	s.AsNode().AddChild(spacer.AsNode())
	s.AsNode().AddChild(firstBtn.AsNode())
	s.AsNode().AddChild(prevBtn.AsNode())
	s.AsNode().AddChild(s.pageLabel.AsNode())
	s.AsNode().AddChild(nextBtn.AsNode())
	s.AsNode().AddChild(lastBtn.AsNode())
	s.AsNode().AddChild(footerDivider().AsNode())
	s.AsNode().AddChild(pill.AsNode())
}

// Process refreshes the memory pill roughly once per second.
func (s *StatusBar) Process(delta Float.X) {
	s.memFrame++
	if s.memFrame < 60 {
		return
	}
	s.memFrame = 0
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s.memPill.SetText(fmt.Sprintf("MEM %dMB", m.HeapAlloc/(1024*1024)))
}

func (s *StatusBar) SetRightPaneActive(active bool) {
	s.rightPaneVisible = active
}

// SetConnection sets the footer's connection name (green-dot segment).
func (s *StatusBar) SetConnection(name string) {
	if name == "" {
		name = "in-memory"
	}
	s.connLabel.SetText(name)
}

// SetConnectionDot sets the color of the footer's connection status dot
// (green = connected, amber = reconnecting, red = error).
func (s *StatusBar) SetConnectionDot(c Color.RGBA) {
	s.connDot.AsControl().AddThemeColorOverride("font_color", c)
}

func (s *StatusBar) SetStatus(msg string) {
	s.statusLabel.SetText(msg)
}

func (s *StatusBar) SetPage(page, totalPages int) {
	s.pageLabel.SetText(fmt.Sprintf("Page %d / %d", page, totalPages))
}

// ── Tab state ──────────────────────────────────────────────────────────────

type tabState struct {
	State        *models.AppState
	schema       *SchemaPanel
	historyPanel *HistoryPanel
	extPanel     *ExtensionsPanel
	schemaBtn    Button.Instance // "Schema" sidebar tab
	historyBtn   Button.Instance // "History" sidebar tab
	extBtn       Button.Instance // "Extensions" sidebar tab
	sqlPanel     *SQLPanel
	dataGrid     *DataGrid
	detailPanel  *RowDetailPanel
	connIdx      int    // index into AppWindow.connections (-1 = in-memory)
	navigating   bool   // true during back/forward nav — skip history+nav recording
	tabID        uint64 // unique ID for matching async results
	generation   uint64 // incremented on each new query to discard stale results

	// Container nodes for show/hide on tab switch
	sidebarWrap PanelContainer.Instance
	outerWrap   HSplitContainer.Instance // content | detail
	rightPanel  VSplitContainer.Instance  // SQL + data grid (resizable)
	detailWrap  PanelContainer.Instance
}

// ── App root ───────────────────────────────────────────────────────────────

type App struct {
	MarginContainer.Extension[App] `gd:"Bufflehead"`

	Duck          *db.DB          `gd:"-"`
	ControlServer *control.Server `gd:"-"`
	GatewayConfig *models.GatewayConfig `gd:"-"`
	BookmarkStore *models.BookmarkStore  `gd:"-"`

	// Legacy accessor — points to active window's active tab state
	State *models.AppState `gd:"-"`

	mainWin      *AppWindow              `gd:"-"`
	secondWins   []*AppWindow             `gd:"-"`
	appMenu      *AppMenu                 `gd:"-"`
	history      *models.QueryHistory     `gd:"-"`
	pendingInit  bool                     `gd:"-"`
	prevKeys     map[Input.Key]bool       `gd:"-"`
	escPrev      bool                     `gd:"-"` // Escape key state for the connection screen
	cachedState  json.RawMessage          `gd:"-"` // updated on main thread each frame
}

func (a *App) activeWindow() *AppWindow {
	if a.mainWin != nil {
		return a.mainWin
	}
	if len(a.secondWins) > 0 {
		return a.secondWins[0]
	}
	return nil
}

func (a *App) Ready() {
	a.history = models.NewQueryHistory()
	a.pendingInit = true
}

func (a *App) initMainWindow() {
	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		rootWin := tree.Root().AsWindow()
		a.mainWin = createMainWindowFromRoot(rootWin, a.Duck, a.history, func() { a.newWindow() })
		a.mainWin.onReLogin = func() { a.openGatewayScreen() }
		a.mainWin.onNewConnection = func() { a.openGatewayScreen() }
		if a.ControlServer != nil {
			a.mainWin.controlAddr = a.ControlServer.Addr()
		}
		a.mainWin.titleBar.WindowID = rootWin.GetWindowId()

		a.mainWin.addNewTab()
		rootWin.MoveToCenter()

		// Handle close — quit the app when main window is closed
		rootWin.OnCloseRequested(func() {
			// Stop any gateway tunnels
			a.stopGatewayTunnels()
			tree.Quit()
		})
	}

	// Setup native menu bar
	a.appMenu = &AppMenu{
		OnOpenFile: func() {
			w := a.activeWindow()
			if w == nil {
				return
			}
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
			if w := a.activeWindow(); w != nil {
				w.onFileSelected(path)
			}
		},
		OnNewTab: func() {
			if w := a.activeWindow(); w != nil {
				w.addNewTab()
			}
		},
		OnCloseTab: func() {
			if w := a.activeWindow(); w != nil {
				w.closeTab(w.activeTab)
			}
		},
		OnNewWindow: func() {
			a.newWindow()
		},
		OnOpenGateway: func() {
			a.openGatewayScreen()
		},
	}
	a.appMenu.Setup()

	// Wire up control server state provider — returns cached state
	// computed on the main thread each frame (avoids Godot thread-safety errors).
	if a.ControlServer != nil {
		a.ControlServer.SetStateProvider(func() (json.RawMessage, error) {
			if a.cachedState != nil {
				return a.cachedState, nil
			}
			return json.Marshal(map[string]any{"tabCount": 0})
		})

		a.ControlServer.SetSQLExecutor(func(ctx context.Context, connName, sql string, limit int) (*control.SQLResult, error) {
			w := a.activeWindow()
			if w == nil {
				return nil, fmt.Errorf("no active window")
			}

			// Find connection by name (empty = active connection)
			var conn *Connection
			if connName == "" {
				if w.activeConnIdx >= 0 && w.activeConnIdx < len(w.connections) {
					conn = w.connections[w.activeConnIdx]
				}
			} else {
				for _, c := range w.connections {
					if c.Name == connName {
						conn = c
						break
					}
				}
			}
			if conn == nil {
				return nil, fmt.Errorf("connection %q not found", connName)
			}
			if conn.worker == nil {
				return nil, fmt.Errorf("connection %q has no worker", conn.Name)
			}

			// Route through the connection's worker so all queries are serialized
			// on a single goroutine — no concurrent access to the underlying sql.DB.
			reply := make(chan SQLReply, 1)
			conn.worker.Send(DBRequest{
				Kind:     ReqSQL,
				UserSQL:  sql,
				Limit:    limit,
				SQLReply: reply,
				Ctx:      ctx,
			})
			// Wait for the query result or the client disconnecting.
			var res SQLReply
			select {
			case res = <-reply:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if res.Err != nil {
				return nil, res.Err
			}
			columns := res.Result.Columns
			if columns == nil {
				columns = []string{}
			}
			rows := res.Result.Rows
			if rows == nil {
				rows = [][]string{}
			}
			return &control.SQLResult{
				Columns: columns,
				Rows:    rows,
				Total:   res.Result.Total,
			}, nil
		})

		a.ControlServer.SetS3Executor(func(req control.S3GetObjectRequest) (*control.S3GetObjectResult, error) {
			w := a.activeWindow()
			if w == nil {
				return nil, fmt.Errorf("no active window")
			}

			// Find connection by name (empty = active connection)
			var conn *Connection
			if req.Connection == "" {
				if w.activeConnIdx >= 0 && w.activeConnIdx < len(w.connections) {
					conn = w.connections[w.activeConnIdx]
				}
			} else {
				for _, c := range w.connections {
					if c.Name == req.Connection {
						conn = c
						break
					}
				}
			}
			if conn == nil {
				return nil, fmt.Errorf("connection %q not found", req.Connection)
			}
			if conn.Gateway == nil || conn.Gateway.Auth == nil {
				return nil, fmt.Errorf("connection %q has no AWS credentials", conn.Name)
			}

			cfg := conn.Gateway.Auth.Config()

			// Allow region override from request, fall back to gateway config
			if req.Region != "" {
				cfg.Region = req.Region
			}

			const defaultMaxBytes int64 = 10 * 1024 * 1024 // 10MB
			maxBytes := defaultMaxBytes
			if req.MaxBytes > 0 {
				maxBytes = req.MaxBytes
			}

			client := s3.NewFromConfig(cfg)
			ctx := context.Background()

			// Set Range header to limit download size
			rangeHeader := fmt.Sprintf("bytes=0-%d", maxBytes-1)
			out, err := client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(req.Bucket),
				Key:    aws.String(req.Key),
				Range:  aws.String(rangeHeader),
			})
			if err != nil {
				return nil, fmt.Errorf("s3 GetObject: %w", err)
			}
			defer out.Body.Close()

			data, err := io.ReadAll(out.Body)
			if err != nil {
				return nil, fmt.Errorf("reading s3 object: %w", err)
			}

			contentType := ""
			if out.ContentType != nil {
				contentType = *out.ContentType
			}

			// Determine full object size and whether we truncated
			var fullSize int64
			truncated := false
			if out.ContentRange != nil {
				// Range response: "bytes 0-N/total"
				var start, end, total int64
				if _, err := fmt.Sscanf(*out.ContentRange, "bytes %d-%d/%d", &start, &end, &total); err == nil {
					fullSize = total
					truncated = int64(len(data)) < total
				}
			} else if out.ContentLength != nil {
				fullSize = *out.ContentLength
			} else {
				fullSize = int64(len(data))
			}

			return &control.S3GetObjectResult{
				Content:     string(data),
				ContentType: contentType,
				Size:        fullSize,
				Truncated:   truncated,
			}, nil
		})
	}
}

func (a *App) showGatewayScreen() {
	w := a.mainWin
	screen := new(GatewayScreen)
	screen.SetConfig(a.GatewayConfig)
	screen.SetBookmarks(a.BookmarkStore)
	screen.OnConnect = func(entry models.GatewayEntry, auth *bfaws.AuthManager, tunnel *bfaws.TunnelManager) {
		// Replace gateway screen with loading indicator
		w.gatewayScreenOpen = false
		screen.AsCanvasItem().SetVisible(false)
		a.showGatewayLoading(entry.Name)
		a.onGatewayConnected(entry, auth, tunnel)
	}
	screen.OnCancel = func() {
		w.exitGatewayScreen()
	}
	w.gatewayScreenOpen = true
	// Add gateway screen to emptyView (which is already visible)
	// Clear default empty view children and add gateway screen
	for w.emptyView.AsNode().GetChildCount() > 0 {
		child := w.emptyView.AsNode().GetChild(0)
		w.emptyView.AsNode().RemoveChild(child)
		child.QueueFree()
	}
	w.emptyView.AsNode().AddChild(screen.AsNode())
}

func (a *App) showGatewayLoading(name string) {
	w := a.mainWin
	// Clear emptyView children (gateway screen is hidden but still a child)
	for w.emptyView.AsNode().GetChildCount() > 0 {
		child := w.emptyView.AsNode().GetChild(0)
		if !child.IsInsideTree() {
			break
		}
		w.emptyView.AsNode().RemoveChild(child)
		child.QueueFree()
	}

	loadingBox := VBoxContainer.New()
	loadingBox.AsControl().SetSizeFlagsHorizontal(Control.SizeShrinkCenter)
	loadingBox.AsControl().SetSizeFlagsVertical(Control.SizeShrinkCenter)
	loadingBox.AsControl().AddThemeConstantOverride("separation", 12)

	titleLabel := Label.New()
	titleLabel.SetText("Connecting to " + name + "...")
	titleLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(16))
	titleLabel.AsControl().AddThemeColorOverride("font_color", colorText)
	titleLabel.SetHorizontalAlignment(1)

	// Connection-status step tracker. Auth + tunnel are already established by
	// the time we reach the loading screen, so seed the active stage at
	// "Connect to database"; the Process loop advances it from progress messages.
	w.gatewayTracker = newStepTracker(control.ConnectStageLabels)
	w.gatewayTracker.setActive(int(control.StageConnectDB), false)
	w.gatewayTracker.root.AsControl().SetSizeFlagsHorizontal(Control.SizeShrinkCenter)

	statusLabel := Label.New()
	statusLabel.SetText("Connecting to database…")
	statusLabel.AsControl().AddThemeFontSizeOverride("font_size", fontSize(12))
	statusLabel.AsControl().AddThemeColorOverride("font_color", colorTextDim)
	statusLabel.SetHorizontalAlignment(1)

	loadingBox.AsNode().AddChild(titleLabel.AsNode())
	loadingBox.AsNode().AddChild(w.gatewayTracker.root.AsNode())
	loadingBox.AsNode().AddChild(statusLabel.AsNode())
	w.emptyView.AsNode().AddChild(loadingBox.AsNode())
	w.gatewayLoadingLabel = statusLabel

	w.statusBar.SetStatus("Connecting to " + name + "...")
}

func (a *App) onGatewayConnected(entry models.GatewayEntry, auth *bfaws.AuthManager, tunnel *bfaws.TunnelManager) {
	w := a.mainWin
	password := entry.ResolvePassword()

	if entry.IsDirect() {
		// Direct Postgres: dial the real host:port with no tunnel or AWS auth.
		RunOpenPostgres(entry.RDSHost, entry.RDSPort, entry.DBName, entry.DBUser, password,
			entry.EffectiveSSLMode(), nextTabID, 0, w.results, func(msg string) {
				w.gatewayLoadingMsg = msg
			})
		nextTabID++

		w.pendingGateway = &GatewayConnection{Config: entry}
		return
	}

	// For IAM auth, pass the AWS config; for password auth, pass nil
	var awsCfg *aws.Config
	if entry.UseIAMAuth() {
		cfg := auth.Config()
		awsCfg = &cfg
	}

	// rdsEndpoint is the real RDS host:port (for IAM token generation)
	rdsEndpoint := fmt.Sprintf("%s:%d", entry.RDSHost, entry.RDSPort)

	// Connect to Postgres via the tunnel (127.0.0.1:localPort)
	RunOpenGateway("127.0.0.1", entry.LocalPort, rdsEndpoint, entry.DBName, entry.DBUser, password,
		awsCfg, nextTabID, 0, w.results, func(msg string) {
			w.gatewayLoadingMsg = msg
		})
	nextTabID++

	// Store gateway info for creating the connection after async result
	w.pendingGateway = &GatewayConnection{
		Config: entry,
		Auth:   auth,
		Tunnel: tunnel,
	}
}

func (a *App) openGatewayScreen() {
	// Reload config from disk each time so edits are picked up
	cfg, err := models.LoadGatewayConfig()
	if err != nil {
		fmt.Printf("gateway config error: %v\n", err)
	}
	if cfg == nil {
		cfg = &models.GatewayConfig{}
	}

	a.GatewayConfig = cfg
	a.showGatewayScreen()

	// Show the empty view (which now contains the gateway screen)
	w := a.mainWin
	w.emptyView.AsCanvasItem().SetVisible(true)
	w.split.AsCanvasItem().SetVisible(false)
}

func (a *App) stopGatewayTunnels() {
	if a.mainWin == nil {
		return
	}
	for _, conn := range a.mainWin.connections {
		if conn.Gateway != nil && conn.Gateway.Tunnel != nil {
			conn.Gateway.Tunnel.Stop()
		}
	}
}

// updateCachedState computes the state snapshot on the main thread
// so the HTTP state provider can return it without touching Godot nodes.
func (a *App) updateCachedState() {
	w := a.activeWindow()
	if w == nil {
		a.cachedState, _ = json.Marshal(map[string]any{"tabCount": 0})
		return
	}
	state := map[string]any{
		"tabCount":        len(w.tabs),
		"activeTab":       w.activeTab,
		"windowCount":     len(a.secondWins),
		"connectionCount": len(w.connections),
	}
	if a.mainWin != nil {
		state["windowCount"] = 1 + len(a.secondWins)
	}
	state["activeConnIdx"] = w.activeConnIdx
	state["visibleTabCount"] = w.tabBar.TabCount()
	// Ordered tabIDs of the visible (connection-scoped) tab bar, plus the
	// active tab's position within it, so tests can verify ordering/selection.
	visIDs := make([]uint64, 0, len(w.tabs))
	visTitles := make([]string, 0, len(w.tabs))
	for _, ts := range w.visibleTabs() {
		visIDs = append(visIDs, ts.tabID)
		visTitles = append(visTitles, w.tabTitle(ts))
	}
	state["visibleTabIDs"] = visIDs
	state["visibleTabTitles"] = visTitles
	state["activeBarIndex"] = w.tabBar.CurrentTab()
	if ts := w.currentTab(); ts != nil {
		state["connIdx"] = ts.connIdx
		state["activeTabID"] = ts.tabID
		state["activeTabTitle"] = w.tabTitle(ts)
		if ts.schema != nil {
			state["schemaTableCount"] = len(ts.schema.allTables)
		}
		state["detailVisible"] = ts.detailWrap.AsCanvasItem().Visible()
		totalWidth := ts.outerWrap.AsControl().Size().X
		if totalWidth > 0 {
			offset := float64(ts.outerWrap.AsSplitContainer().SplitOffset())
			state["detailWidthRatio"] = 1.0 - offset/float64(totalWidth)
		}
		state["selectedRows"] = ts.dataGrid.sortedSelectedRows()
		// Detail panel values for testing
		if ts.detailPanel.columns != nil {
			detailValues := make(map[string]string, len(ts.detailPanel.columns))
			for i, col := range ts.detailPanel.columns {
				detailValues[col] = ts.detailPanel.resolveValue(i)
			}
			state["detailValues"] = detailValues
		}
	}
	state["detailToggleActive"] = w.statusBar.rightPaneVisible
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
	a.cachedState, _ = json.Marshal(state)
}

func (a *App) newWindow() {
	aw := createSecondaryWindow(a.Duck, a.history, func() { a.newWindow() })
	// A Window.New() secondary window defaults to content_scale_size {0,0}; on a
	// HiDPI (Retina) display that makes its UI render tiny compared to the root
	// window, which inherits the project viewport as its content-scale base.
	// Copy the root window's content-scale settings so new windows match it.
	if a.mainWin != nil {
		rw := a.mainWin.window
		aw.window.SetContentScaleMode(rw.ContentScaleMode())
		aw.window.SetContentScaleAspect(rw.ContentScaleAspect())
		aw.window.SetContentScaleSize(rw.ContentScaleSize())
		aw.window.SetContentScaleFactor(rw.ContentScaleFactor())
	}
	aw.onReLogin = func() { a.openGatewayScreen() }
	aw.onNewConnection = func() { a.openGatewayScreen() }
	if a.ControlServer != nil {
		aw.controlAddr = a.ControlServer.Addr()
	}
	a.secondWins = append(a.secondWins, aw)

	if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
		root := tree.Root()
		root.AsNode().AddChild(aw.window.AsNode())
		aw.window.Show()
		aw.window.MoveToCenter()
		pos := aw.window.Position()
		offset := int32(len(a.secondWins) * 30)
		aw.window.SetPosition(Vector2i.New(int(pos.X+offset), int(pos.Y+offset)))
		aw.titleBar.WindowID = aw.window.GetWindowId()
		aw.window.GrabFocus()
		aw.window.RequestAttention()
		aw.addNewTab()

		// Handle close — destroy window, app stays alive
		aw.window.OnCloseRequested(func() {
			for i, w := range a.secondWins {
				if w == aw {
					a.secondWins = append(a.secondWins[:i], a.secondWins[i+1:]...)
					break
				}
			}
			aw.window.AsNode().QueueFree()
		})
	}
}

// handleShortcut is called from input polling
func (a *App) handleShortcut(key Input.Key, w *AppWindow) {
	switch key {
	case Input.KeyQ:
		if tree, ok := Object.As[SceneTree.Instance](Engine.GetMainLoop()); ok {
			tree.Quit()
		}
	case Input.KeyN:
		a.newWindow()
	case Input.KeyT:
		if w != nil {
			w.addNewTab()
		}
	case Input.KeyW:
		if w != nil {
			if len(w.tabs) <= 1 && w != a.mainWin {
				// Close secondary windows when last tab is closed
				for i, sw := range a.secondWins {
					if sw == w {
						a.secondWins = append(a.secondWins[:i], a.secondWins[i+1:]...)
						break
					}
				}
				w.window.AsNode().QueueFree()
			} else if len(w.tabs) > 0 {
				w.closeTab(w.activeTab)
			}
		}
	case Input.KeyO:
		if a.appMenu != nil && a.appMenu.OnOpenFile != nil {
			a.appMenu.OnOpenFile()
		}
	case Input.KeyG:
		a.openGatewayScreen()
	case Input.KeyBracketleft:
		if w != nil {
			w.navBack()
		}
	case Input.KeyBracketright:
		if w != nil {
			w.navForward()
		}
	}
}

func (a *App) justPressed(key Input.Key) bool {
	pressed := Input.IsKeyPressed(key)
	was := a.prevKeys[key]
	a.prevKeys[key] = pressed
	return pressed && !was
}

func (a *App) stopAllWorkers() {
	if a.mainWin != nil {
		a.mainWin.stopWorkers()
	}
	for _, w := range a.secondWins {
		w.stopWorkers()
	}
}

func (a *App) Notification(what Object.Notification) {
	// Log all notifications above 2000 (application-level)
	if what >= 2000 {
		fmt.Println("[bufflehead] notification:", what, "mainWin:", a.mainWin != nil, "secondWins:", len(a.secondWins))
	}

	// Stop workers on quit
	const notificationWMCloseRequest Object.Notification = 1006
	if what == notificationWMCloseRequest {
		a.stopAllWorkers()
	}

	// macOS dock click: focus existing window
	const notificationApplicationFocusIn Object.Notification = 2016
	if what == notificationApplicationFocusIn {
		if w := a.activeWindow(); w != nil {
			// Un-minimize if needed
			wid := DisplayServer.Window(w.window.GetWindowId())
			if DisplayServer.WindowGetMode(wid) == DisplayServer.WindowModeMinimized {
				DisplayServer.WindowSetMode(DisplayServer.WindowModeWindowed, wid)
			}
			w.window.Show()
			w.window.GrabFocus()
		}
	}
}

func (a *App) Process(delta Float.X) {
	// Deferred init — create main window after scene tree is ready
	if a.pendingInit {
		a.pendingInit = false
		a.prevKeys = make(map[Input.Key]bool)
		a.initMainWindow()
	}

	// Poll keyboard shortcuts (works across all windows)
	if Input.IsKeyPressed(Input.KeyMeta) || Input.IsKeyPressed(Input.KeyCtrl) {
		shortcuts := []Input.Key{Input.KeyQ, Input.KeyN, Input.KeyT, Input.KeyW, Input.KeyO, Input.KeyG, Input.KeyBracketleft, Input.KeyBracketright}
		for _, k := range shortcuts {
			if a.justPressed(k) {
				a.handleShortcut(k, a.activeWindow())
			}
		}
	} else {
		// Clear all tracked keys when cmd isn't held
		for k := range a.prevKeys {
			a.prevKeys[k] = false
		}
	}

	// Escape closes the connection screen (tracked separately from ⌘-shortcuts).
	escNow := Input.IsKeyPressed(Input.KeyEscape)
	if escNow && !a.escPrev {
		if w := a.activeWindow(); w != nil && w.gatewayScreenOpen {
			w.exitGatewayScreen()
		}
	}
	a.escPrev = escNow

	// Update State pointer
	if w := a.activeWindow(); w != nil {
		a.State = w.currentState()
	}

	// Poll async DB results from all windows
	a.pollResults()

	// Update cached state snapshot (safe to access Godot nodes here on the main thread)
	a.updateCachedState()

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

func (a *App) pollResults() {
	windows := make([]*AppWindow, 0, 1+len(a.secondWins))
	if a.mainWin != nil {
		windows = append(windows, a.mainWin)
	}
	windows = append(windows, a.secondWins...)

	for _, w := range windows {
		if w.results == nil {
			continue
		}
		if w.skipPoll {
			w.skipPoll = false
			continue
		}
		for {
			select {
			case res := <-w.results:
				w.handleDBResult(res)
			default:
				goto nextWindow
			}
		}
	nextWindow:
		// Apply a completed extension install/load (ran on a background goroutine).
		if w.extActionMsg != nil {
			res := w.extActionMsg
			w.extActionMsg = nil
			if res.err != nil {
				w.statusBar.SetStatus("Extension " + res.name + " failed: " + res.err.Error())
			} else {
				w.statusBar.SetStatus("Extension " + res.name + " ready")
			}
			if w.extActionTab != nil {
				w.loadExtensions(w.extActionTab)
			}
		}

		// Show the database switcher once its background list query completes.
		if w.dbListMsg != nil {
			res := w.dbListMsg
			w.dbListMsg = nil
			w.presentDatabaseSwitcher(res)
		}

		// Update gateway loading label + step tracker if background status changed
		if w.gatewayLoadingMsg != "" && w.pendingGateway != nil {
			w.gatewayLoadingLabel.SetText(w.gatewayLoadingMsg)
			if w.gatewayTracker != nil {
				w.gatewayTracker.setActive(int(control.ConnectStageFromMessage(w.gatewayLoadingMsg)), false)
			}
			w.gatewayLoadingMsg = ""
		}

		// Monitor active gateway tunnels for reconnection status
		for _, conn := range w.connections {
			if conn.Gateway == nil || conn.Gateway.Tunnel == nil {
				continue
			}
			tunnel := conn.Gateway.Tunnel
			isActive := w.activeConnIdx < len(w.connections) && w.connections[w.activeConnIdx] == conn
			var msg string
			switch tunnel.Status() {
			case bfaws.TunnelConnecting:
				msg = conn.Name + ": " + tunnel.StatusMsg()
				if isActive {
					w.statusBar.SetConnectionDot(colorStatusYellow)
				}
			case bfaws.TunnelError:
				if tunnel.IsAuthError() {
					msg = conn.Name + ": login expired — log in again to reconnect"
					// Prompt re-login once per error transition (guarded by
					// LastTunnelMsg dedup below and the dialog's own guard).
					if conn.Gateway.LastTunnelMsg != msg {
						w.promptReLogin()
					}
				} else {
					msg = conn.Name + ": Tunnel error — " + tunnel.LastError()
				}
				applyConnTileErrorTheme(conn.button.AsControl())
				if isActive {
					w.statusBar.SetConnectionDot(colorStatusRed)
				}
			case bfaws.TunnelConnected:
				// Clear any previous reconnect message
				if conn.Gateway.LastTunnelMsg != "" {
					conn.Gateway.LastTunnelMsg = ""
					w.statusBar.SetStatus("Reconnected")
					// Reset the Postgres pool so the next query uses a fresh
					// connection through the rebuilt tunnel instead of a stale one.
					if pgConn, ok := conn.DB.(*db.PostgresDB); ok {
						pgConn.ResetPool()
					}
					// Restore the rounded tile + green dot.
					applyConnTileTheme(conn.button.AsControl(), isActive)
					if isActive {
						w.statusBar.SetConnectionDot(colorStatusGreen)
					}
				}
				continue
			default:
				continue
			}
			if msg != conn.Gateway.LastTunnelMsg {
				conn.Gateway.LastTunnelMsg = msg
				w.statusBar.SetStatus(msg)
			}
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
	if w == nil {
		cmd.Respond(control.Result{Error: "no active window"})
		return
	}

	switch cmd.Action {
	case "open":
		var d control.OpenData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		w.onFileSelectedWithCmd(d.Path, cmd)
		// Response deferred to async result handler

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
		w.runCurrentQuery(cmd)
		// Response deferred to async result handler

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
		w.runCurrentQuery(cmd)
		// Response deferred to async result handler

	case "page":
		var d control.PageData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		if s := w.currentState(); s != nil {
			s.PageOffset = d.Offset
		}
		w.runCurrentQuery(cmd)
		// Response deferred to async result handler

	case "reset_sort":
		if s := w.currentState(); s != nil {
			s.SortColumn = ""
			s.SortDir = models.SortNone
			s.PageOffset = 0
		}
		w.runCurrentQuery(cmd)
		// Response deferred to async result handler

	case "new_tab":
		w.addNewTab()
		cmd.Respond(control.Result{OK: true})

	case "close_tab":
		w.closeTab(w.activeTab)
		cmd.Respond(control.Result{OK: true})

	case "close_connection":
		var d control.CloseConnectionData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		if d.Index <= 0 || d.Index >= len(w.connections) {
			cmd.Respond(control.Result{Error: "invalid connection index"})
			return
		}
		w.closeConnection(d.Index)
		cmd.Respond(control.Result{OK: true})

	case "select_connection":
		var d control.CloseConnectionData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		if d.Index < 0 || d.Index >= len(w.connections) {
			cmd.Respond(control.Result{Error: "invalid connection index"})
			return
		}
		w.selectConnection(d.Index)
		cmd.Respond(control.Result{OK: true})

	case "select_tab":
		// Select a tab by its position in the visible (connection-scoped) tab
		// bar. Mirrors clicking a tab in the UI.
		var d control.CloseConnectionData // reuse {index}
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		id, ok := w.tabIDAtBar(d.Index)
		if !ok {
			cmd.Respond(control.Result{Error: "invalid visible tab index"})
			return
		}
		w.selectTab(id)
		cmd.Respond(control.Result{OK: true})

	case "replay_history":
		// Replay a history entry by index (0 = most recent), mirroring clicking
		// it in the History panel.
		var d control.CloseConnectionData // reuse {index}
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		ts := w.currentTab()
		if ts == nil || ts.historyPanel == nil || w.history == nil {
			cmd.Respond(control.Result{Error: "no active tab/history"})
			return
		}
		entries := w.history.All()
		if d.Index < 0 || d.Index >= len(entries) {
			cmd.Respond(control.Result{Error: "invalid history index"})
			return
		}
		if ts.historyPanel.OnReplay != nil {
			ts.historyPanel.OnReplay(entries[d.Index].SQL)
		}
		cmd.Respond(control.Result{OK: true})

	case "select_columns":
		// Mirror clicking column checkboxes in the schema panel: set the visible
		// column set and re-run the query (projection applied via VirtualSQL).
		var d control.SelectColumnsData
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		ts := w.currentTab()
		if ts == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		ts.State.SelectedCols = d.Columns
		ts.State.PageOffset = 0
		if ts.schema != nil {
			ts.schema.SetCheckedColumns(d.Columns)
		}
		w.runCurrentQuery(cmd)

	case "reconnect":
		var d control.ReconnectData
		hasData := len(cmd.Data) > 0
		if hasData {
			if err := json.Unmarshal(cmd.Data, &d); err != nil {
				cmd.Respond(control.Result{Error: err.Error()})
				return
			}
		}
		var idx int
		switch {
		case d.Connection != "":
			idx = -1
			for i, c := range w.connections {
				if c.Name == d.Connection {
					idx = i
					break
				}
			}
			if idx < 0 {
				cmd.Respond(control.Result{Error: fmt.Sprintf("connection %q not found", d.Connection)})
				return
			}
		case !hasData:
			// No payload at all → default to the active connection.
			idx = w.activeConnIdx
		default:
			// An explicit index was provided (including 0).
			idx = d.Index
		}
		// reconnectConnection validates idx and answers cmd (async on success).
		w.reconnectConnection(idx, cmd)

	case "select_row":
		var d struct {
			Row  int   `json:"row"`
			Rows []int `json:"rows"`
		}
		if err := json.Unmarshal(cmd.Data, &d); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		ts := w.currentTab()
		if ts == nil || ts.dataGrid == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		// Support both single row and multi-row
		indices := d.Rows
		if len(indices) == 0 {
			indices = []int{d.Row}
		}
		var rows [][]string
		ts.dataGrid.clearRowHighlights()
		ts.dataGrid.selectedRows = make(map[int]bool)
		for _, idx := range indices {
			if idx < 0 || idx >= len(ts.dataGrid.rows) {
				cmd.Respond(control.Result{Error: "row index out of range"})
				return
			}
			rows = append(rows, ts.dataGrid.rows[idx])
			ts.dataGrid.selectedRows[idx] = true
		}
		if len(indices) > 0 {
			ts.dataGrid.lastSelectedRow = indices[len(indices)-1]
		}
		ts.dataGrid.applyRowHighlights()
		ts.detailPanel.SetRows(ts.dataGrid.columns, ts.dataGrid.colTypes, rows)
		if !ts.detailWrap.AsCanvasItem().Visible() {
			totalWidth := ts.outerWrap.AsControl().Size().X
			ts.outerWrap.AsSplitContainer().SetSplitOffset(int(totalWidth * 0.75))
			ts.detailWrap.AsCanvasItem().SetVisible(true)
			w.statusBar.SetRightPaneActive(true)
		}
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

	case "deselect_all":
		ts := w.currentTab()
		if ts == nil || ts.dataGrid == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		ts.dataGrid.clearCellHighlight()
		ts.dataGrid.clearRowHighlights()
		ts.dataGrid.selectedRows = make(map[int]bool)
		ts.dataGrid.lastSelectedRow = -1
		ts.dataGrid.Super().DeselectAll()
		ts.detailPanel.Clear()
		cmd.Respond(control.Result{OK: true})

	case "new_window":
		a.newWindow()
		cmd.Respond(control.Result{OK: true})

	case "open_gateway":
		a.openGatewayScreen()
		cmd.Respond(control.Result{OK: true})

	case "preview_connecting":
		// Test/preview hook: render the connecting screen (with its step
		// tracker) without a live AWS gateway. The tracker is seeded at the
		// "Connect to database" stage, matching a real connection in progress.
		// An optional {"failed": true} payload previews the failure state:
		// the active step's dot turns red and the status line is hidden.
		var pd struct {
			Failed bool `json:"failed"`
		}
		if len(cmd.Data) > 0 {
			_ = json.Unmarshal(cmd.Data, &pd)
		}
		a.showGatewayLoading("Preview")
		if pd.Failed && w.gatewayTracker != nil {
			w.gatewayTracker.markFailed()
			if w.gatewayLoadingLabel != (Label.Instance{}) {
				w.gatewayLoadingLabel.AsCanvasItem().SetVisible(false)
			}
		}
		a.mainWin.emptyView.AsCanvasItem().SetVisible(true)
		a.mainWin.split.AsCanvasItem().SetVisible(false)
		cmd.Respond(control.Result{OK: true})

	case "show_history":
		ts := w.currentTab()
		if ts == nil || ts.historyPanel == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		w.showHistorySidebar(ts)
		cmd.Respond(control.Result{OK: true})

	case "show_extensions":
		ts := w.currentTab()
		if ts == nil || ts.extPanel == nil {
			cmd.Respond(control.Result{Error: "no active tab"})
			return
		}
		w.showExtensionsSidebar(ts)
		cmd.Respond(control.Result{OK: true})

	case "close_gateway":
		w.exitGatewayScreen()
		cmd.Respond(control.Result{OK: true})

	case "create_test_bookmark":
		// Test hook: write a dummy bookmark so persistence can be verified
		// across restarts / platforms (esp. Windows). Idempotent — repeated
		// calls update the same "dummy-bookmark" entry.
		if a.BookmarkStore == nil {
			cmd.Respond(control.Result{Error: "no bookmark store"})
			return
		}
		bm := models.Bookmark{
			Label:      "dummy-bookmark",
			Env:        "test",
			AWSProfile: "test-profile",
			AWSRegion:  "us-east-1",
			RDSHost:    "dummy-db.example.com",
			RDSPort:    5432,
			DBName:     "testdb",
			DBUser:     "tester",
			AuthMode:   "password",
		}
		if err := a.BookmarkStore.Add(bm); err != nil {
			cmd.Respond(control.Result{Error: err.Error()})
			return
		}
		payload, _ := json.Marshal(map[string]any{
			"path":  a.BookmarkStore.Path(),
			"count": len(a.BookmarkStore.All()),
			"label": bm.Label,
		})
		cmd.Respond(control.Result{OK: true, Data: payload})

	case "nav_back":
		w.navBackWithCmd(cmd)
		// Response deferred to async result handler

	case "nav_forward":
		w.navForwardWithCmd(cmd)
		// Response deferred to async result handler

	case "ui_tree":
		// Optional resize before capturing the tree
		if len(cmd.Data) > 0 {
			var rd control.ResizeData
			if err := json.Unmarshal(cmd.Data, &rd); err == nil {
				if rd.Width > 0 && rd.Height > 0 {
					w.window.SetSize(Vector2i.New(rd.Width, rd.Height))
				}
				if rd.Scale > 0 {
					w.window.SetContentScaleFactor(Float.X(rd.Scale))
				}
			}
		}
		tscn := writeTSCN(w.window.AsNode())
		cmd.Respond(control.Result{OK: true, RawBytes: tscn})

	case "screenshot":
		if w.window != (Window.Nil) {
			tex := w.window.AsViewport().GetTexture()
			img := tex.AsTexture2D().GetImage()
			pngBytes := img.SavePngToBuffer()
			cmd.Respond(control.Result{OK: true, RawBytes: pngBytes})
		} else {
			cmd.Respond(control.Result{Error: "no active window"})
		}

	default:
		cmd.Respond(control.Result{Error: "unknown action: " + cmd.Action})
	}
}

// writeTSCN serializes a Godot node tree to .tscn text format.
func writeTSCN(root Node.Instance) []byte {
	var buf bytes.Buffer
	buf.WriteString("[gd_scene format=3]\n")
	walkNode(&buf, root, "")
	return buf.Bytes()
}

func walkNode(buf *bytes.Buffer, node Node.Instance, parentPath string) {
	name := node.Name()
	className := Object.Instance(node.AsObject()).ClassName()

	buf.WriteString("\n[node")
	fmt.Fprintf(buf, " name=%q type=%q", name, className)
	if parentPath != "" {
		fmt.Fprintf(buf, " parent=%q", parentPath)
	}
	buf.WriteString("]\n")

	// Emit properties for CanvasItem nodes
	if ci, ok := Object.As[CanvasItem.Instance](node); ok {
		fmt.Fprintf(buf, "visible = %v\n", ci.Visible())
	}
	if ctrl, ok := Object.As[Control.Instance](node); ok {
		size := ctrl.Size()
		fmt.Fprintf(buf, "size = Vector2(%v, %v)\n", size.X, size.Y)
	}

	// Emit type-specific properties
	if lbl, ok := Object.As[Label.Instance](node); ok {
		fmt.Fprintf(buf, "text = %q\n", lbl.Text())
		if lbl.AsControl().HasThemeColorOverride("font_color") {
			c := lbl.AsControl().GetThemeColor("font_color")
			fmt.Fprintf(buf, "font_color = Color(%v, %v, %v, %v)\n", c.R, c.G, c.B, c.A)
		}
	}
	if sc, ok := Object.As[ScrollContainer.Instance](node); ok {
		fmt.Fprintf(buf, "horizontal_scroll_mode = %d\n", int(sc.HorizontalScrollMode()))
		fmt.Fprintf(buf, "vertical_scroll_mode = %d\n", int(sc.VerticalScrollMode()))
	}
	if sc, ok := Object.As[SplitContainer.Instance](node); ok {
		fmt.Fprintf(buf, "split_offset = %d\n", sc.SplitOffset())
	}

	// Recurse children with correct parent path
	var childParent string
	switch parentPath {
	case "":
		childParent = "."
	case ".":
		childParent = name
	default:
		childParent = parentPath + "/" + name
	}

	for i := 0; i < node.GetChildCount(); i++ {
		walkNode(buf, node.GetChild(i), childParent)
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
	classdb.Register[ExtensionsPanel]()
	classdb.Register[RowDetailPanel]()
	classdb.Register[StatusBar]()
	classdb.Register[ConsolePanel]()
	classdb.Register[GatewayScreen]()
	classdb.Register[GatewayInfoPanel]()
	classdb.Register[App]()
}

var _ TreeItem.Instance
