package models

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns the base directory for Bufflehead config/data files.
// Uses os.UserConfigDir first (the OS-native config location), falling back
// to os.UserHomeDir, then os.TempDir.
//
// Results per platform:
//   - macOS:   ~/Library/Application Support/Bufflehead
//   - Windows: %AppData%/Bufflehead
//   - Linux:   $XDG_CONFIG_HOME/bufflehead or ~/.config/bufflehead
func ConfigDir() string {
	name := "Bufflehead"
	if runtime.GOOS == "linux" {
		name = "bufflehead"
	}

	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		return filepath.Join(configDir, name)
	}
	fmt.Fprintf(os.Stderr, "bufflehead: UserConfigDir failed, trying UserHomeDir\n")

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join(home, "Library", "Application Support", name)
		case "windows":
			return filepath.Join(home, "AppData", "Roaming", name)
		default:
			return filepath.Join(home, ".config", name)
		}
	}
	fmt.Fprintf(os.Stderr, "bufflehead: UserHomeDir failed, using temp dir\n")

	return filepath.Join(os.TempDir(), name)
}
