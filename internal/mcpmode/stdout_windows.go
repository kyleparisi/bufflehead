//go:build windows

package mcpmode

import (
	"os"

	"golang.org/x/sys/windows"
)

// takeStdout keeps the original stdout handle for MCP and makes stderr the
// process's standard output. Unverified on Windows: Godot's CRT may hold its
// own copy of the stdout handle.
func takeStdout() (*os.File, error) {
	out := os.Stdout
	if err := windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(os.Stderr.Fd())); err != nil {
		return nil, err
	}
	os.Stdout = os.Stderr
	return out, nil
}
