// Package mcpsetup registers Bufflehead's MCP server mode with AI clients.
//
// It works out the command this install should be launched with, and adds a
// "bufflehead" entry to Claude Desktop's config file without disturbing the
// rest of it. It has no graphics.gd import, so it is unit-testable; the
// "Connect an AI Assistant…" dialog in internal/ui drives it.
package mcpsetup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServerName is the key Bufflehead registers under in every client.
const ServerName = "bufflehead"

// Args are the arguments that start the executable in MCP mode: Godot's
// --headless, then "--" to end Godot's arguments, then Bufflehead's --mcp.
var Args = []string{"--headless", "--", "--mcp"}

// Command is how an MCP client should launch this install.
type Command struct {
	Path string   `json:"command"`
	Args []string `json:"args"`
	// Warning explains why Path may not work from a client (a dev build, an
	// AppImage that wasn't launched as one). Empty when it looks right.
	Warning string `json:"-"`
}

// Env is the process information LaunchCommand reads, injectable for tests.
type Env struct {
	GOOS       string
	Executable func() (string, error)
	Getenv     func(string) string
	// EvalSymlinks resolves the executable path; nil means filepath.EvalSymlinks.
	EvalSymlinks func(string) (string, error)
}

// OSEnv is the running process.
func OSEnv(goos string) Env {
	return Env{GOOS: goos, Executable: os.Executable, Getenv: os.Getenv, EvalSymlinks: filepath.EvalSymlinks}
}

// LaunchCommand returns the executable and args a client should run.
//
//   - macOS: the binary inside the .app (os.Executable, symlinks resolved).
//   - Windows: the .exe.
//   - Linux: $APPIMAGE, the AppImage file itself. os.Executable points into
//     the temporary /tmp/.mount_* folder the AppImage runtime mounts, which
//     is gone once the app quits.
func LaunchCommand(env Env) (Command, error) {
	cmd := Command{Args: append([]string(nil), Args...)}
	if env.GOOS == "linux" {
		if p := env.Getenv("APPIMAGE"); p != "" {
			cmd.Path = p
			return cmd, nil
		}
	}
	exe, err := env.Executable()
	if err != nil {
		return Command{}, fmt.Errorf("find the Bufflehead executable: %w", err)
	}
	eval := env.EvalSymlinks
	if eval == nil {
		eval = filepath.EvalSymlinks
	}
	if resolved, err := eval(exe); err == nil {
		exe = resolved
	}
	cmd.Path = exe
	switch env.GOOS {
	case "linux":
		cmd.Warning = "Bufflehead isn't running from an AppImage, so this path may not start it outside this session."
	case "darwin":
		if !strings.Contains(exe, ".app/Contents/MacOS/") {
			cmd.Warning = "This doesn't look like an installed Bufflehead.app (a development build?), so a client may not be able to start it."
		}
	}
	return cmd, nil
}

// EntryJSON is the {"command": ..., "args": [...]} entry, indented, as other
// MCP clients take it.
func (c Command) EntryJSON() string {
	b, _ := json.MarshalIndent(c, "", "  ")
	return string(b)
}

// ClaudeCodeCommand is the `claude mcp add` line for this install. It uses
// user scope so the server is available in every project, not only the
// directory the command was run from.
func (c Command) ClaudeCodeCommand(goos string) string {
	parts := []string{"claude", "mcp", "add", "--scope", "user", ServerName, "--", shellQuote(c.Path, goos)}
	parts = append(parts, c.Args...)
	return strings.Join(parts, " ")
}

// shellQuote double-quotes p for the user's shell. POSIX shells still expand
// $, ` and \ inside double quotes, so those (and ") are escaped there; cmd and
// PowerShell take a double-quoted Windows path as is.
func shellQuote(p, goos string) string {
	if goos == "windows" {
		return `"` + p + `"`
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range p {
		if strings.ContainsRune("\"$`\\", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// ClaudeDesktopConfigPath is where Claude Desktop keeps its config:
// ~/Library/Application Support/Claude on macOS, %APPDATA%\Claude on Windows.
// $BUFFLEHEAD_CLAUDE_DESKTOP_CONFIG overrides it (tests, and trying the merge
// against a scratch copy). Other platforms have no official Claude Desktop.
func ClaudeDesktopConfigPath(env Env) (string, error) {
	if p := env.Getenv("BUFFLEHEAD_CLAUDE_DESKTOP_CONFIG"); p != "" {
		return p, nil
	}
	switch env.GOOS {
	case "darwin":
		home := env.Getenv("HOME")
		if home == "" {
			return "", errors.New("$HOME is not set")
		}
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		appData := env.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("%APPDATA% is not set")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
	}
	return "", fmt.Errorf("Claude Desktop is not available on %s", env.GOOS)
}

// Status is how the config's bufflehead entry compares to this install.
type Status int

const (
	Absent  Status = iota // no entry (or no config file): Add
	Current               // entry already runs this install: nothing to do
	Changed               // entry runs something else (the app moved): Update
)

// Plan is a proposed change to Claude Desktop's config, computed by
// PlanClaudeDesktop and carried out by Apply.
type Plan struct {
	Path     string
	Status   Status
	Existing string // the current bufflehead entry, indented; "" if Absent
	Proposed string // the entry that will be written, indented
	// Content is the whole new file. Original is the file as read ("" if it
	// didn't exist); Apply refuses if the file no longer matches it.
	Content  []byte
	Original []byte
	exists   bool
}

// PlanClaudeDesktop reads the config at path and works out the change that
// makes its bufflehead entry run cmd. Every other key is kept. A missing file
// is treated as empty; a file that isn't a JSON object is an error, since
// rewriting it would lose whatever is in it.
func PlanClaudeDesktop(path string, cmd Command) (*Plan, error) {
	p := &Plan{Path: path, Proposed: cmd.EntryJSON()}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		p.exists = true
		p.Original = raw
	}

	top := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &top); err != nil {
			return nil, fmt.Errorf("%s is not a JSON object, so Bufflehead won't rewrite it: %w", path, err)
		}
	}
	servers := map[string]json.RawMessage{}
	if s, ok := top["mcpServers"]; ok && !isNull(s) {
		if err := json.Unmarshal(s, &servers); err != nil {
			return nil, fmt.Errorf(`"mcpServers" in %s is not an object: %w`, path, err)
		}
	}

	if old, ok := servers[ServerName]; ok {
		p.Existing = indent(old)
		p.Status = Changed
		var cur Command
		if json.Unmarshal(old, &cur) == nil && sameCommand(cur, cmd) {
			p.Status = Current
		}
	}

	entry, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	servers[ServerName] = entry
	if top["mcpServers"], err = json.Marshal(servers); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, err
	}
	p.Content = append(out, '\n')
	return p, nil
}

func sameCommand(a, b Command) bool {
	if a.Path != b.Path || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
}

func isNull(m json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(m), []byte("null")) }

func indent(m json.RawMessage) string {
	var b bytes.Buffer
	if json.Indent(&b, m, "", "  ") != nil {
		return string(m)
	}
	return b.String()
}

// Apply writes p.Content to p.Path. An existing file is first copied to a
// timestamped backup next to it, whose path is returned. The new file is
// written to a temporary file in the same folder and renamed over the old
// one, so a crash never leaves a half-written config. Apply refuses if the
// file changed since p was planned.
func (p *Plan) Apply(now time.Time) (backup string, err error) {
	cur, err := os.ReadFile(p.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if p.exists {
			return "", fmt.Errorf("%s was removed since it was read; try again", p.Path)
		}
	case err != nil:
		return "", err
	case !p.exists || !bytes.Equal(cur, p.Original):
		return "", fmt.Errorf("%s changed since it was read; try again", p.Path)
	}

	dir := filepath.Dir(p.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	mode := os.FileMode(0o644)
	if p.exists {
		if fi, err := os.Stat(p.Path); err == nil {
			mode = fi.Mode().Perm()
		}
		backup = fmt.Sprintf("%s.bufflehead-backup-%s", p.Path, now.Format("20060102-150405"))
		if err := os.WriteFile(backup, p.Original, mode); err != nil {
			return "", fmt.Errorf("write backup: %w", err)
		}
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(p.Path)+".tmp-*")
	if err != nil {
		return backup, err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(p.Content); err != nil {
		tmp.Close()
		return backup, err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return backup, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return backup, err
	}
	if err := tmp.Close(); err != nil {
		return backup, err
	}
	if err := os.Rename(tmp.Name(), p.Path); err != nil {
		return backup, err
	}
	return backup, nil
}
