package db

import "testing"

func TestExtensions(t *testing.T) {
	db := setupTestDB(t)

	exts, err := db.Extensions()
	if err != nil {
		t.Fatalf("Extensions() error: %v", err)
	}
	if len(exts) == 0 {
		t.Fatal("expected at least one extension")
	}
	// Core extensions like parquet/json are always known to DuckDB.
	byName := map[string]Extension{}
	for _, e := range exts {
		byName[e.Name] = e
	}
	if _, ok := byName["parquet"]; !ok {
		if _, ok2 := byName["json"]; !ok2 {
			t.Errorf("expected a core extension (parquet/json) in %d extensions", len(exts))
		}
	}
}

func TestInstallExtension_RejectsInjection(t *testing.T) {
	db := setupTestDB(t)

	bad := []string{"httpfs; DROP TABLE x", "foo bar", "", "a-b", "x'y"}
	for _, name := range bad {
		if err := db.InstallExtension(name); err == nil {
			t.Errorf("InstallExtension(%q) should have been rejected", name)
		}
		if err := db.LoadExtension(name); err == nil {
			t.Errorf("LoadExtension(%q) should have been rejected", name)
		}
	}
}

func TestLoadExtension_CoreLoadsWithoutNetwork(t *testing.T) {
	db := setupTestDB(t)
	// json is statically available in the DuckDB build; LOAD should not need
	// network access. If this DuckDB build lacks it, skip rather than fail.
	if err := db.LoadExtension("json"); err != nil {
		t.Skipf("LOAD json unavailable in this build: %v", err)
	}
	exts, err := db.Extensions()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range exts {
		if e.Name == "json" && !e.Loaded {
			t.Errorf("json should be loaded after LOAD")
		}
	}
}
