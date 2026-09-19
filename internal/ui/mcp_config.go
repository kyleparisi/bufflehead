package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// mcpBridgeName is the stdio bridge binary shipped next to the app executable
// (Bufflehead.app/Contents/MacOS on macOS, beside Bufflehead.exe / the AppImage
// payload elsewhere). See cmd/bufflehead-mcp.
func mcpBridgeName() string {
	if runtime.GOOS == "windows" {
		return "bufflehead-mcp.exe"
	}
	return "bufflehead-mcp"
}

// mcpBridgePath returns the absolute path an MCP client should launch. It
// prefers the bridge beside the running executable (a packaged install), then
// a dev build in the working directory (`go build -o bufflehead-mcp
// ./cmd/bufflehead-mcp` under `gd run`), and finally the canonical install
// path so the snippet is still useful when nothing is found.
func mcpBridgePath() string {
	name := mcpBridgeName()
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		if p := filepath.Join(filepath.Dir(exe), name); fileExists(p) {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		for _, dir := range []string{wd, filepath.Dir(wd)} { // gd run executes from graphics/
			if p := filepath.Join(dir, name); fileExists(p) {
				return p
			}
		}
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Applications/Bufflehead.app/Contents/MacOS/" + name
	case "windows":
		if pf := os.Getenv("LOCALAPPDATA"); pf != "" {
			return filepath.Join(pf, "Programs", "Bufflehead", name)
		}
		return name
	default:
		return "/usr/local/bin/" + name
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// mcpConfigSnippet is the claude_desktop_config.json entry for Bufflehead.
func mcpConfigSnippet() string {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"bufflehead": map[string]any{"command": mcpBridgePath()},
		},
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

// mcpClaudeCodeCommand registers the bridge with Claude Code.
func mcpClaudeCodeCommand() string {
	return "claude mcp add bufflehead -- " + shellQuote(mcpBridgePath())
}

// shellQuote single-quotes a path for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if strings.ContainsAny(s, " '\"$\\") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

// mcpPromptNote is appended to every "Ask AI" prompt so an agent that already
// has the Bufflehead MCP server configured uses it instead of curl.
func mcpPromptNote(connName string) string {
	return "\nIf you have the `bufflehead` MCP server configured, prefer its tools (list_connections, get_schema, run_sql, cancel_sql, get_s3_object, reconnect) over the curl commands above; this connection is named \"" + connName + "\".\n"
}
