//go:build !windows

package mcpmode

import (
	"os"

	"golang.org/x/sys/unix"
)

func takeStdout() (*os.File, error) {
	fd, err := unix.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return nil, err
	}
	if err := unix.Dup2(int(os.Stderr.Fd()), int(os.Stdout.Fd())); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "mcp-stdout"), nil
}
