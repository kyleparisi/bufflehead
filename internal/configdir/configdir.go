// Package configdir locates Bufflehead's per-user config directory.
//
// It is a leaf package (stdlib only) so that binaries which must not link the
// cgo DuckDB driver — the MCP stdio bridge — can still find the directory the
// app writes its discovery file to. internal/models re-exports Dir as
// models.ConfigDir for the rest of the app.
package configdir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// EnvOverride, when set, replaces the platform lookup entirely. Tests use it
// to point the app and the bridge at a scratch directory.
const EnvOverride = "BUFFLEHEAD_CONFIG_DIR"

// Dir returns the base directory for Bufflehead config/data files.
// Uses os.UserConfigDir first (the OS-native config location), falling back
// to os.UserHomeDir, then os.TempDir.
//
// Results per platform:
//   - macOS:   ~/Library/Application Support/Bufflehead
//   - Windows: %AppData%/Bufflehead
//   - Linux:   $XDG_CONFIG_HOME/bufflehead or ~/.config/bufflehead
func Dir() string {
	if d := os.Getenv(EnvOverride); d != "" {
		return d
	}

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
