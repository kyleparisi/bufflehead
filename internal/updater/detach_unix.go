//go:build !windows && !mas

package updater

import (
	"os/exec"
	"syscall"
)

// detach puts the helper in its own session so it outlives the app.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
