package updater

import (
	"bufio"
	"bytes"
	"strings"
)

// PresetVersion extracts application/short_version from a Godot
// export_presets.cfg, the single place releases bump the version.
func PresetVersion(cfg []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(cfg))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && k == "application/short_version" {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}
