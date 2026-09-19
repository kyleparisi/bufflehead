package buildinfo

import (
	"os"
	"regexp"
	"testing"
)

// TestVersionMatchesExportPresets fails when someone bumps the release version
// in export_presets.cfg without updating Version (or vice versa).
func TestVersionMatchesExportPresets(t *testing.T) {
	data, err := os.ReadFile("../../graphics/export_presets.cfg")
	if err != nil {
		t.Fatalf("read export_presets.cfg: %v", err)
	}
	m := regexp.MustCompile(`application/short_version="([0-9]+(?:\.[0-9]+)+)"`).FindSubmatch(data)
	if m == nil {
		t.Fatal("application/short_version not found in export_presets.cfg")
	}
	if got := string(m[1]); got != Version {
		t.Fatalf("export_presets.cfg short_version = %q, buildinfo.Version = %q", got, Version)
	}
}
