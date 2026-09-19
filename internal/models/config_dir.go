package models

import "bufflehead/internal/configdir"

// ConfigDir returns the base directory for Bufflehead config/data files.
// The lookup lives in the leaf package configdir so cgo-free binaries (the
// MCP bridge) can share it; see configdir.Dir for the per-platform result.
func ConfigDir() string {
	return configdir.Dir()
}
