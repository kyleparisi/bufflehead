package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"graphics.gd/classdb/Input"
	"graphics.gd/classdb/NativeMenu"
	"graphics.gd/variant/RID"
)

const maxRecentFiles = 10

// AppMenu manages the native macOS menu bar.
type AppMenu struct {
	fileMenu    RID.NativeMenu
	recentMenu  RID.NativeMenu
	recentPaths []string

	OnOpenFile   func()            // triggers native file dialog
	OnOpenRecent func(path string) // opens a specific recent file
	OnNewTab     func()            // creates new tab (⌘N)
	OnCloseTab   func()            // closes current tab (⌘W)
}

func (m *AppMenu) Setup() {
	m.loadRecent()

	// Get the main menu bar
	mainMenu := NativeMenu.GetSystemMenu(NativeMenu.MainMenuId)

	// Create File menu
	m.fileMenu = NativeMenu.CreateMenu()

	// New Tab (Cmd+N)
	NativeMenu.AddItem(m.fileMenu, "New Tab", func(tag any) {
		if m.OnNewTab != nil {
			m.OnNewTab()
		}
	}, nil, nil, Input.Key(Input.KeyMaskMeta)|Input.KeyN)

	// Open… (Cmd+O)
	NativeMenu.AddItem(m.fileMenu, "Open…", func(tag any) {
		if m.OnOpenFile != nil {
			m.OnOpenFile()
		}
	}, nil, nil, Input.Key(Input.KeyMaskMeta)|Input.KeyO)

	// Close Tab (Cmd+W)
	NativeMenu.AddItem(m.fileMenu, "Close Tab", func(tag any) {
		if m.OnCloseTab != nil {
			m.OnCloseTab()
		}
	}, nil, nil, Input.Key(Input.KeyMaskMeta)|Input.KeyW)

	// Open Recent submenu
	m.recentMenu = NativeMenu.CreateMenu()
	m.rebuildRecentMenu()
	NativeMenu.AddSubmenuItem(mainMenu, "File", m.fileMenu, nil)

	// Insert File menu at position 1 (after the app menu)
	// Actually, AddSubmenuItem already adds it. Let's add recent to file menu.
	NativeMenu.AddSeparator(m.fileMenu)
	NativeMenu.AddSubmenuItem(m.fileMenu, "Open Recent", m.recentMenu, nil)
}

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
	m.rebuildRecentMenu()
}

func (m *AppMenu) rebuildRecentMenu() {
	// Clear existing items
	for NativeMenu.GetItemCount(m.recentMenu) > 0 {
		NativeMenu.RemoveItem(m.recentMenu, 0)
	}

	if len(m.recentPaths) == 0 {
		NativeMenu.AddItem(m.recentMenu, "No Recent Files", nil, nil, nil, 0)
		return
	}

	for _, p := range m.recentPaths {
		path := p // capture
		label := filepath.Base(path)
		NativeMenu.AddItem(m.recentMenu, label, func(tag any) {
			if m.OnOpenRecent != nil {
				m.OnOpenRecent(path)
			}
		}, nil, nil, 0)
	}

	NativeMenu.AddSeparator(m.recentMenu)
	NativeMenu.AddItem(m.recentMenu, "Clear Recent", func(tag any) {
		m.recentPaths = nil
		m.saveRecent()
		m.rebuildRecentMenu()
	}, nil, nil, 0)
}

func (m *AppMenu) recentFilePath() string {
	// macOS: ~/Library/Application Support/ParquetViewer/
	// Linux: ~/.local/share/ParquetViewer/
	// Windows: %APPDATA%/ParquetViewer/
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, _ = os.UserHomeDir()
	}
	appDir := filepath.Join(dir, "ParquetViewer")
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
