//go:build !windows

package updater

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstalledBundle(t *testing.T) {
	for _, tc := range []struct{ exe, app, err string }{
		{"/Applications/Bufflehead.app/Contents/MacOS/Bufflehead", "/Applications/Bufflehead.app", ""},
		{"/Users/me/Apps/Bufflehead.app/Contents/MacOS/Bufflehead", "/Users/me/Apps/Bufflehead.app", ""},
		{"/usr/local/bin/bufflehead", "", "isn't an installed app"},
		{"/Volumes/Bufflehead/Bufflehead.app/Contents/MacOS/Bufflehead", "", "disk image"},
		{"/private/var/folders/x/T/AppTranslocation/ABC/d/Bufflehead.app/Contents/MacOS/Bufflehead", "", "disk image"},
	} {
		app, err := installedBundle(tc.exe)
		if app != tc.app || (tc.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: got %q, %v", tc.exe, app, err)
		}
	}
}

// fakeTools stands in for plutil/hdiutil/codesign/spctl/ditto.
type fakeTools struct {
	ids, versions map[string]string // by bundle path
	failCodesign  bool
	calls         []string
}

func (f *fakeTools) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name)
	last := args[len(args)-1]
	switch name {
	case "plutil":
		app := filepath.Dir(filepath.Dir(last))
		if args[1] == "CFBundleIdentifier" {
			return []byte(f.ids[filepath.Base(app)]), nil
		}
		return []byte(f.versions[filepath.Base(app)]), nil
	case "hdiutil":
		if args[0] == "attach" {
			return nil, os.MkdirAll(filepath.Join(last, appName), 0o700)
		}
		return nil, os.RemoveAll(filepath.Join(last, appName))
	case "codesign":
		if f.failCodesign {
			return nil, errors.New("code object is not signed at all")
		}
	case "ditto":
		return nil, os.MkdirAll(last, 0o700)
	}
	return nil, nil
}

func withTools(t *testing.T, f *fakeTools) {
	prev := command
	command = f.run
	t.Cleanup(func() { command = prev })
}

func goodTools() *fakeTools {
	id := map[string]string{"Bufflehead.app": bundleID, ".Bufflehead-update.app": bundleID}
	v := map[string]string{"Bufflehead.app": "0.30.0", ".Bufflehead-update.app": "0.30.0"}
	return &fakeTools{ids: id, versions: v}
}

func TestPrepareInstall(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, appName)
	f := goodTools()
	withTools(t, f)
	p, err := prepareInstall(context.Background(), app, filepath.Join(dir, "x.dmg"), "v0.30.0")
	if err != nil {
		t.Fatal(err)
	}
	if p.New != filepath.Join(dir, ".Bufflehead-update.app") || p.Backup != filepath.Join(dir, ".Bufflehead-backup.app") {
		t.Fatalf("got %+v", p)
	}
	if _, err := os.Stat(p.New); err != nil {
		t.Fatal("copy missing")
	}
	if got := strings.Join(f.calls, " "); !strings.Contains(got, "hdiutil ditto codesign spctl") || !strings.HasSuffix(got, "hdiutil") {
		t.Fatalf("tool sequence %s", got)
	}
}

func TestPrepareInstallRejects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakeTools)
		want   string
	}{
		{"unsigned", func(f *fakeTools) { f.failCodesign = true }, "signature"},
		{"wrong version", func(f *fakeTools) { f.versions[".Bufflehead-update.app"] = "0.29.0" }, "version"},
		{"not bufflehead installed", func(f *fakeTools) { f.ids["Bufflehead.app"] = "org.godotengine.godot" }, "isn't an installed app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := goodTools()
			tc.mutate(f)
			withTools(t, f)
			_, err := prepareInstall(context.Background(), filepath.Join(dir, appName), filepath.Join(dir, "x.dmg"), "v0.30.0")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, ".Bufflehead-update.app")); err == nil {
				t.Fatal("left a copy behind")
			}
		})
	}
}

func TestSwapScript(t *testing.T) {
	dir := t.TempDir()
	app, newApp, bak := filepath.Join(dir, "Bufflehead.app"), filepath.Join(dir, ".new.app"), filepath.Join(dir, ".bak.app")
	os.MkdirAll(app, 0o700)
	os.WriteFile(filepath.Join(app, "v"), []byte("old"), 0o600)
	os.MkdirAll(newApp, 0o700)
	os.WriteFile(filepath.Join(newApp, "v"), []byte("new"), 0o600)
	script := filepath.Join(dir, "swap.sh")
	os.WriteFile(script, []byte(swapScript), 0o600)

	waiter := exec.Command("sleep", "0.5") // stands in for the exiting app
	waiter.Start()
	go waiter.Wait()
	out, err := exec.Command("/bin/sh", script, strconv.Itoa(waiter.Process.Pid), app, newApp, bak, "true").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got, _ := os.ReadFile(filepath.Join(app, "v")); string(got) != "new" {
		t.Fatalf("installed %q; log: %s", got, out)
	}
	if got, _ := os.ReadFile(filepath.Join(bak, "v")); string(got) != "old" {
		t.Fatal("backup missing")
	}
	if _, err := os.Stat(script); err == nil {
		t.Fatal("script not removed")
	}
}

// TestPrepareInstallRealDMG runs the real tools against a downloaded release:
// BUFFLEHEAD_TEST_DMG=path/Bufflehead-0.29.0-macOS.dmg BUFFLEHEAD_TEST_TAG=v0.29.0
func TestPrepareInstallRealDMG(t *testing.T) {
	dmg, tag := os.Getenv("BUFFLEHEAD_TEST_DMG"), os.Getenv("BUFFLEHEAD_TEST_TAG")
	if dmg == "" || !InstallSupported() {
		t.Skip("set BUFFLEHEAD_TEST_DMG and BUFFLEHEAD_TEST_TAG on macOS")
	}
	dir := t.TempDir()
	app := filepath.Join(dir, appName)
	os.MkdirAll(filepath.Join(app, "Contents"), 0o700)
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + bundleID + `</string></dict></plist>`
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o600)
	p, err := prepareInstall(context.Background(), app, dmg, tag)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.New, "Contents", "MacOS", "Bufflehead")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareInstall(context.Background(), app, dmg, "v9.9.9"); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("wrong tag accepted: %v", err)
	}
}
