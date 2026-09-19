package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"graphics.gd/classdb/Control"
	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/MenuBar"
	"graphics.gd/classdb/NativeMenu"
	"graphics.gd/classdb/PanelContainer"
	"graphics.gd/classdb/PopupMenu"
	"graphics.gd/variant/RID"
)

const maxRecentFiles = 10

// AppMenu owns the application menu. The menu is described once by spec() and
// rendered two ways:
//
//   - macOS: into the native global menu bar (NativeMenu), once per app.
//   - Windows: there is no global menu, so each AppWindow gets an in-window
//     MenuBar row above its title bar (like Sublime Text on Windows).
//
// Actions receive the window the menu was used from. The native menu has no
// owning window, so it targets ActiveWindow().
type AppMenu struct {
	nativeRecent RID.NativeMenu // Open Recent submenu; valid when hasNative
	hasNative    bool
	recentPaths  []string

	ActiveWindow    func() *AppWindow
	OnOpenFile      func(w *AppWindow)              // triggers native file dialog
	OnOpenRecent    func(w *AppWindow, path string) // opens a specific recent file
	OnNewTab        func(w *AppWindow)              // creates new tab (⌘T)
	OnCloseTab      func(w *AppWindow)              // closes current tab (⌘W)
	OnNewWindow     func()                          // creates new window (⌘N)
	OnOpenGateway   func()                          // shows gateway connection screen
	OnCopyMCPConfig func()                          // copies the Claude Desktop MCP config snippet
	OnCheckUpdates  func()                          // manual "Check for Updates…"
	OnQuit          func()                          // quits the app (⌘Q)
}

// menuRole places an item that platforms file in different spots.
type menuRole int

const (
	roleNone    menuRole = iota
	roleAppMenu          // macOS: lives in the application (Bufflehead) menu instead
	roleQuit             // macOS: omitted — the application menu provides Quit
)

type menuItem struct {
	label  string
	key    Input.Key // shortcut key, shown with ⌘ (macOS) or Ctrl; 0 = none
	action func(w *AppWindow)
	sep    bool
	recent bool // the "Open Recent" submenu, rendered from recentPaths
	role   menuRole
}

type menuDef struct {
	title string
	items []menuItem
}

// spec is the single description of the menu. The shortcut keys shown here are
// display-only in both renderers: App.Process polls ⌘/Ctrl shortcuts for every
// window (handleShortcut), so keep the two in sync.
func (m *AppMenu) spec() []menuDef {
	global := func(f func()) func(*AppWindow) {
		return func(*AppWindow) {
			if f != nil {
				f()
			}
		}
	}
	perWindow := func(f func(*AppWindow)) func(*AppWindow) {
		return func(w *AppWindow) {
			if f != nil && w != nil {
				f(w)
			}
		}
	}
	return []menuDef{
		{title: "File", items: []menuItem{
			{label: "New Window", key: Input.KeyN, action: global(m.OnNewWindow)},
			{label: "New Tab", key: Input.KeyT, action: perWindow(m.OnNewTab)},
			{label: "Open…", key: Input.KeyO, action: perWindow(m.OnOpenFile)},
			{label: "Connect to Gateway…", key: Input.KeyG, action: global(m.OnOpenGateway)},
			{label: "Copy Claude MCP Config", action: global(m.OnCopyMCPConfig)},
			{sep: true},
			{label: "Close Tab", key: Input.KeyW, action: perWindow(m.OnCloseTab)},
			{sep: true},
			{label: "Open Recent", recent: true},
			{sep: true, role: roleQuit},
			{label: "Exit", key: Input.KeyQ, action: global(m.OnQuit), role: roleQuit},
		}},
		{title: "Help", items: []menuItem{
			{label: "Check for Updates…", action: global(m.OnCheckUpdates), role: roleAppMenu},
		}},
	}
}

// useInWindowMenu reports whether windows should draw their own menu bar: on
// Windows, or when BUFFLEHEAD_INWINDOW_MENU=1 forces it (to develop and test
// the Windows menu on macOS).
func useInWindowMenu() bool {
	return runtime.GOOS == "windows" || os.Getenv("BUFFLEHEAD_INWINDOW_MENU") == "1"
}

func (m *AppMenu) Setup() {
	m.loadRecent()
	if !useInWindowMenu() {
		m.setupNative()
	}
}

// ── Native (macOS) renderer ────────────────────────────────────────────────

func (m *AppMenu) setupNative() {
	m.hasNative = true
	mainMenu := NativeMenu.GetSystemMenu(NativeMenu.MainMenuId)
	appMenu := NativeMenu.GetSystemMenu(NativeMenu.ApplicationMenuId)

	appIdx := 0
	for _, def := range m.spec() {
		menu := NativeMenu.CreateMenu()
		for _, it := range def.items {
			if it.role == roleQuit {
				continue
			}
			if it.role == roleAppMenu {
				// Insert near the top of the app menu (after "About").
				appIdx++
				NativeMenu.AddItemOptions(appMenu, it.label, m.nativeCallback(it), nil, nil, 0, appIdx)
				continue
			}
			switch {
			case it.sep:
				NativeMenu.AddSeparator(menu)
			case it.recent:
				m.nativeRecent = NativeMenu.CreateMenu()
				m.rebuildNativeRecent()
				NativeMenu.AddSubmenuItem(menu, it.label, m.nativeRecent, nil)
			default:
				var accel Input.Key
				if it.key != 0 {
					accel = Input.Key(Input.KeyMaskMeta) | it.key
				}
				NativeMenu.AddItem(menu, it.label, m.nativeCallback(it), nil, nil, accel)
			}
		}
		if NativeMenu.GetItemCount(menu) == 0 {
			NativeMenu.FreeMenu(menu)
			continue
		}
		NativeMenu.AddSubmenuItem(mainMenu, def.title, menu, nil)
	}
}

func (m *AppMenu) nativeCallback(it menuItem) func(tag any) {
	return func(tag any) {
		fmt.Println("[menu]", it.label, "triggered")
		it.action(m.activeWindow())
	}
}

func (m *AppMenu) activeWindow() *AppWindow {
	if m.ActiveWindow == nil {
		return nil
	}
	return m.ActiveWindow()
}

// rebuildNativeRecent re-renders the native Open Recent submenu from recentPaths.
func (m *AppMenu) rebuildNativeRecent() {
	if !m.hasNative {
		return
	}
	for NativeMenu.GetItemCount(m.nativeRecent) > 0 {
		NativeMenu.RemoveItem(m.nativeRecent, 0)
	}

	if len(m.recentPaths) == 0 {
		NativeMenu.AddItem(m.nativeRecent, "No Recent Files", nil, nil, nil, 0)
		return
	}

	for _, p := range m.recentPaths {
		path := p // capture
		NativeMenu.AddItem(m.nativeRecent, filepath.Base(path), func(tag any) {
			if w := m.activeWindow(); w != nil && m.OnOpenRecent != nil {
				m.OnOpenRecent(w, path)
			}
		}, nil, nil, 0)
	}

	NativeMenu.AddSeparator(m.nativeRecent)
	NativeMenu.AddItem(m.nativeRecent, "Clear Recent", func(tag any) {
		m.clearRecent()
	}, nil, nil, 0)
}

// ── In-window (Windows) renderer ─────────────────────────────────────

// BuildMenuBar renders spec() into a MenuBar row whose actions target w. The
// Open Recent submenu is re-rendered from recentPaths each time it opens, so
// the bar never holds a stale copy of that state. It returns the row to place
// in the window and the MenuBar inside it.
func (m *AppMenu) BuildMenuBar(w *AppWindow) (Control.Instance, MenuBar.Instance) {
	wrap := PanelContainer.New()
	wrap.AsNode().SetName("MenuBarRow")
	applyMenuBarRowTheme(wrap.AsControl())

	bar := MenuBar.New()
	bar.AsNode().SetName("MenuBar")
	bar.SetPreferGlobalMenu(false)
	bar.SetFlat(true)
	bar.SetSwitchOnHover(true)
	// Shortcuts are polled for every window by App.Process; letting the bar
	// handle them too would fire each one twice.
	bar.SetDisableShortcuts(true)
	applyMenuBarTheme(bar.AsControl())

	for _, def := range m.spec() {
		popup := PopupMenu.New()
		popup.AsNode().SetName(def.title) // MenuBar titles each menu by its node name
		applyPopupMenuTheme(popup)
		items := def.items
		for i, it := range items {
			switch {
			case it.sep:
				popup.AddSeparator()
			case it.recent:
				popup.AddSubmenuNodeItem(it.label, m.buildRecentPopup(w))
			default:
				popup.AddItem(it.label)
				if it.key != 0 {
					popup.SetItemAccelerator(i, Input.Key(Input.KeyMaskCmdOrCtrl)|it.key)
				}
			}
		}
		popup.OnIndexPressed(func(index int) { runMenuItem(items, index, w) })
		bar.AsNode().AddChild(popup.AsNode())
	}

	wrap.AsNode().AddChild(bar.AsNode())
	return wrap.AsControl(), bar
}

func runMenuItem(items []menuItem, index int, w *AppWindow) {
	if index >= 0 && index < len(items) && items[index].action != nil {
		items[index].action(w)
	}
}

// ActivateInWindow presses the item labelled item in bar's menu titled menu,
// as a click would. Used by the control API. The label is resolved against
// spec(), then the rendered popup must show a real item at that index, so this
// fails if the bar doesn't actually draw it. (Labels aren't compared with the
// popup's text: graphics.gd truncates non-ASCII strings read back from Godot,
// so "Open…" reads back as "Open\xe2".)
func (m *AppMenu) ActivateInWindow(w *AppWindow, bar MenuBar.Instance, menu, item string) error {
	for _, def := range m.spec() {
		if def.title != menu {
			continue
		}
		for i, it := range def.items {
			if it.sep || it.label != item {
				continue
			}
			for b := 0; b < bar.GetMenuCount(); b++ {
				if bar.GetMenuTitle(b) != menu {
					continue
				}
				popup := bar.GetMenuPopup(b)
				if i >= popup.ItemCount() || popup.IsItemSeparator(i) {
					return fmt.Errorf("menu %q does not render %q", menu, item)
				}
				runMenuItem(def.items, i, w)
				return nil
			}
			return fmt.Errorf("menu bar has no %q menu", menu)
		}
		return fmt.Errorf("menu %q has no item %q", menu, item)
	}
	return fmt.Errorf("no menu %q", menu)
}

func (m *AppMenu) buildRecentPopup(w *AppWindow) PopupMenu.Instance {
	popup := PopupMenu.New()
	applyPopupMenuTheme(popup)
	var shown []string // the paths the current rendering lists, by item index
	popup.AsWindow().OnAboutToPopup(func() {
		popup.Clear()
		shown = append(shown[:0], m.recentPaths...)
		if len(shown) == 0 {
			popup.AddItem("No Recent Files")
			popup.SetItemDisabled(0, true)
			return
		}
		for _, p := range shown {
			popup.AddItem(filepath.Base(p))
		}
		popup.AddSeparator()
		popup.AddItem("Clear Recent")
	})
	popup.OnIndexPressed(func(index int) {
		switch {
		case index < len(shown):
			if m.OnOpenRecent != nil {
				m.OnOpenRecent(w, shown[index])
			}
		case index == len(shown)+1: // after the separator
			m.clearRecent()
		}
	})
	return popup
}

// ── Recent files state ─────────────────────────────────────────────────────

func (m *AppMenu) AddRecentFile(path string) {
	// Remove if already exists
	filtered := make([]string, 0, len(m.recentPaths))
	for _, p := range m.recentPaths {
		if p != path {
			filtered = append(filtered, p)
		}
	}
	// Prepend
	m.recentPaths = append([]string{path}, filtered...)
	if len(m.recentPaths) > maxRecentFiles {
		m.recentPaths = m.recentPaths[:maxRecentFiles]
	}
	m.saveRecent()
	m.rebuildNativeRecent()
}

func (m *AppMenu) clearRecent() {
	m.recentPaths = nil
	m.saveRecent()
	m.rebuildNativeRecent()
}

func (m *AppMenu) recentFilePath() string {
	// macOS: ~/Library/Application Support/Bufflehead/
	// Linux: ~/.local/share/Bufflehead/
	// Windows: %APPDATA%/Bufflehead/
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, _ = os.UserHomeDir()
	}
	appDir := filepath.Join(dir, "Bufflehead")
	os.MkdirAll(appDir, 0755)
	return filepath.Join(appDir, "recent.json")
}

func (m *AppMenu) loadRecent() {
	data, err := os.ReadFile(m.recentFilePath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &m.recentPaths)
}

func (m *AppMenu) saveRecent() {
	data, _ := json.Marshal(m.recentPaths)
	os.WriteFile(m.recentFilePath(), data, 0644)
}
