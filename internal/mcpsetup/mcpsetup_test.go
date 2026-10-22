package mcpsetup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakeEnv(goos, exe string, vars map[string]string) Env {
	return Env{
		GOOS:         goos,
		Executable:   func() (string, error) { return exe, nil },
		Getenv:       func(k string) string { return vars[k] },
		EvalSymlinks: func(p string) (string, error) { return p, nil },
	}
}

func TestLaunchCommand(t *testing.T) {
	tests := []struct {
		name, goos, exe string
		vars            map[string]string
		wantPath        string
		wantWarning     bool
	}{
		{"macOS app bundle", "darwin", "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead", nil,
			"/Applications/Bufflehead.app/Contents/MacOS/Bufflehead", false},
		{"macOS dev build", "darwin", "/Users/me/.local/bin/godot", nil, "/Users/me/.local/bin/godot", true},
		{"Windows exe", "windows", `C:\Program Files\Bufflehead\Bufflehead.exe`, nil,
			`C:\Program Files\Bufflehead\Bufflehead.exe`, false},
		{"Linux AppImage ignores the mount", "linux", "/tmp/.mount_BuffleXYZ/usr/bin/Bufflehead",
			map[string]string{"APPIMAGE": "/home/me/Apps/Bufflehead-x86_64.AppImage"},
			"/home/me/Apps/Bufflehead-x86_64.AppImage", false},
		{"Linux without AppImage", "linux", "/opt/bufflehead/Bufflehead", nil, "/opt/bufflehead/Bufflehead", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := LaunchCommand(fakeEnv(tt.goos, tt.exe, tt.vars))
			if err != nil {
				t.Fatal(err)
			}
			if cmd.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", cmd.Path, tt.wantPath)
			}
			if !reflect.DeepEqual(cmd.Args, []string{"--headless", "--", "--mcp"}) {
				t.Errorf("Args = %q", cmd.Args)
			}
			if (cmd.Warning != "") != tt.wantWarning {
				t.Errorf("Warning = %q, want warning: %v", cmd.Warning, tt.wantWarning)
			}
		})
	}
}

func TestLaunchCommandResolvesSymlinks(t *testing.T) {
	env := fakeEnv("darwin", "/usr/local/bin/bufflehead", nil)
	env.EvalSymlinks = func(string) (string, error) {
		return "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead", nil
	}
	cmd, err := LaunchCommand(env)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead" || cmd.Warning != "" {
		t.Errorf("got %+v", cmd)
	}
}

func TestLaunchCommandExecutableError(t *testing.T) {
	env := fakeEnv("darwin", "", nil)
	env.Executable = func() (string, error) { return "", errors.New("boom") }
	if _, err := LaunchCommand(env); err == nil {
		t.Fatal("want error")
	}
}

func TestClaudeCodeCommand(t *testing.T) {
	c := Command{Path: "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead", Args: Args}
	want := `claude mcp add --scope user bufflehead -- "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead" --headless -- --mcp`
	if got := c.ClaudeCodeCommand("darwin"); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	c.Path = `/Users/me/$weird "dir"/Bufflehead`
	if got := c.ClaudeCodeCommand("linux"); !strings.Contains(got, `"/Users/me/\$weird \"dir\"/Bufflehead"`) {
		t.Errorf("unescaped: %s", got)
	}
	c.Path = `C:\Program Files\Bufflehead\Bufflehead.exe`
	if got := c.ClaudeCodeCommand("windows"); !strings.Contains(got, `"C:\Program Files\Bufflehead\Bufflehead.exe"`) {
		t.Errorf("windows: %s", got)
	}
}

func TestClaudeDesktopConfigPath(t *testing.T) {
	p, _ := ClaudeDesktopConfigPath(fakeEnv("darwin", "", map[string]string{"HOME": "/Users/me"}))
	if p != filepath.Join("/Users/me", "Library", "Application Support", "Claude", "claude_desktop_config.json") {
		t.Errorf("darwin: %s", p)
	}
	p, _ = ClaudeDesktopConfigPath(fakeEnv("windows", "", map[string]string{"APPDATA": `C:\Users\me\AppData\Roaming`}))
	if p != filepath.Join(`C:\Users\me\AppData\Roaming`, "Claude", "claude_desktop_config.json") {
		t.Errorf("windows: %s", p)
	}
	if _, err := ClaudeDesktopConfigPath(fakeEnv("linux", "", nil)); err == nil {
		t.Error("linux: want error")
	}
	p, _ = ClaudeDesktopConfigPath(fakeEnv("linux", "", map[string]string{"BUFFLEHEAD_CLAUDE_DESKTOP_CONFIG": "/tmp/x.json"}))
	if p != "/tmp/x.json" {
		t.Errorf("override: %s", p)
	}
}

var testCmd = Command{Path: "/Applications/Bufflehead.app/Contents/MacOS/Bufflehead", Args: Args}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude_desktop_config.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return m
}

func TestPlanAndApplyMergesIntoExistingConfig(t *testing.T) {
	path := writeConfig(t, `{
  "globalShortcut": "Alt+Space",
  "mcpServers": {"other": {"command": "/bin/other", "args": ["x"], "env": {"K": "V"}}},
  "preferences": {"nested": [1, 2, {"deep": true}]}
}`)
	plan, err := PlanClaudeDesktop(path, testCmd)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != Absent || plan.Existing != "" {
		t.Fatalf("status %v existing %q", plan.Status, plan.Existing)
	}
	now := time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)
	backup, err := plan.Apply(now)
	if err != nil {
		t.Fatal(err)
	}

	got := readJSON(t, path)
	if got["globalShortcut"] != "Alt+Space" {
		t.Errorf("lost globalShortcut: %v", got)
	}
	if !reflect.DeepEqual(got["preferences"], map[string]any{"nested": []any{1.0, 2.0, map[string]any{"deep": true}}}) {
		t.Errorf("lost preferences: %v", got["preferences"])
	}
	servers := got["mcpServers"].(map[string]any)
	if !reflect.DeepEqual(servers["other"], map[string]any{"command": "/bin/other", "args": []any{"x"}, "env": map[string]any{"K": "V"}}) {
		t.Errorf("lost other server: %v", servers["other"])
	}
	want := map[string]any{"command": testCmd.Path, "args": []any{"--headless", "--", "--mcp"}}
	if !reflect.DeepEqual(servers["bufflehead"], want) {
		t.Errorf("bufflehead = %v", servers["bufflehead"])
	}

	if !strings.HasSuffix(backup, ".bufflehead-backup-20261022-093000") {
		t.Errorf("backup name %q", backup)
	}
	if b, _ := os.ReadFile(backup); string(b) != string(plan.Original) {
		t.Errorf("backup content differs")
	}
	// Windows has no Unix permission bits: Stat reports 0666 for any
	// writable file, so only check the mode is kept elsewhere.
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600 kept", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 2 {
		t.Errorf("left temp files behind: %v", entries)
	}

	// Planning again finds it up to date.
	plan, err = PlanClaudeDesktop(path, testCmd)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != Current {
		t.Errorf("status %v, want Current", plan.Status)
	}
}

func TestPlanMissingFileCreatesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Claude", "claude_desktop_config.json")
	plan, err := PlanClaudeDesktop(path, testCmd)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := plan.Apply(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if backup != "" {
		t.Errorf("backup %q for a new file", backup)
	}
	got := readJSON(t, path)
	if _, ok := got["mcpServers"].(map[string]any)["bufflehead"]; !ok {
		t.Errorf("no entry: %v", got)
	}
}

func TestPlanEmptyAndNullServers(t *testing.T) {
	for _, content := range []string{"  \n", `{"mcpServers": null}`, `{}`} {
		path := writeConfig(t, content)
		if content == "  \n" {
			os.WriteFile(path, []byte(content), 0o644)
		}
		plan, err := PlanClaudeDesktop(path, testCmd)
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if plan.Status != Absent {
			t.Errorf("%q: status %v", content, plan.Status)
		}
	}
}

func TestPlanDetectsMovedApp(t *testing.T) {
	path := writeConfig(t, `{"mcpServers":{"bufflehead":{"command":"/Users/me/Downloads/Bufflehead.app/Contents/MacOS/Bufflehead","args":["--headless","--","--mcp"]}}}`)
	plan, err := PlanClaudeDesktop(path, testCmd)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != Changed {
		t.Fatalf("status %v, want Changed", plan.Status)
	}
	if !strings.Contains(plan.Existing, "Downloads") || !strings.Contains(plan.Proposed, "/Applications/") {
		t.Errorf("existing %q proposed %q", plan.Existing, plan.Proposed)
	}
}

func TestPlanRefusesInvalidJSON(t *testing.T) {
	for _, content := range []string{`{"mcpServers": {`, `[1,2]`, `{"mcpServers": []}`} {
		path := writeConfig(t, content)
		if _, err := PlanClaudeDesktop(path, testCmd); err == nil {
			t.Errorf("%q: want error", content)
		}
	}
}

func TestApplyRefusesIfFileChanged(t *testing.T) {
	path := writeConfig(t, `{}`)
	plan, err := PlanClaudeDesktop(path, testCmd)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"edited": true}`), 0o600)
	if _, err := plan.Apply(time.Now()); err == nil {
		t.Fatal("want error")
	}
	if got := readJSON(t, path); got["edited"] != true {
		t.Errorf("file was overwritten: %v", got)
	}
}
