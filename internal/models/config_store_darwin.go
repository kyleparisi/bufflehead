//go:build darwin && mas

package models

import (
	"github.com/progrium/darwinkit/macos/foundation"
	"path/filepath"
)

func nativeStoreConfigDir() string {
	paths := foundation.FileManager_DefaultManager().URLsForDirectoryInDomains(foundation.ApplicationSupportDirectory, foundation.UserDomainMask)
	if len(paths) == 0 {
		return ""
	}
	return filepath.Join(paths[0].Path(), "Bufflehead")
}
