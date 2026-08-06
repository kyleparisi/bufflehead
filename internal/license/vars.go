package license

import (
	"path/filepath"
	"strings"
)

// Build-time configuration, injected with -ldflags at release time so that the
// same source tree produces channel-specific binaries without code edits:
//
//	go build -ldflags "\
//	  -X bufflehead/internal/license.internalKeyHashes=<sha256hex>,<sha256hex> \
//	  -X bufflehead/internal/license.defaultValidateURL=https://api.example.com/validate"
//
// internalKeyHashes holds SHA-256 digests, not keys. Shipping the keys
// themselves would put working coworker credentials in every customer's copy,
// recoverable with `strings`. Digests are safe because internal keys are
// high-entropy random strings, so there is no useful preimage attack.
var (
	defaultBundleID        = "com.kyleparisi.bufflehead"
	defaultBundleVersion   = ""
	defaultValidateURL     = ""
	defaultLicenseFilePath = "/Library/Application Support/bufflehead/license.plist"

	internalKeyHashes    = ""
	internalEmailDomains = ""
)

// splitList parses a comma-separated ldflags value into a trimmed, non-empty
// slice. Values are lowercased so hex digests and email domains compare
// case-insensitively.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// bundleRootFor returns the .app bundle directory containing exe — the parent
// of Contents/ — or "" when exe is not inside a macOS bundle.
//
// A bundled executable lives at Some.app/Contents/MacOS/exe, so the bundle root
// is three levels up. Godot's macOS export uses that same layout.
func bundleRootFor(exe string) string {
	macOS := filepath.Dir(exe)      // Contents/MacOS
	contents := filepath.Dir(macOS) // Contents
	root := filepath.Dir(contents)  // Some.app
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" {
		return ""
	}
	if !strings.EqualFold(filepath.Ext(root), ".app") {
		return ""
	}
	return root
}
