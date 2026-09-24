//go:build !windows

package main

import (
	"syscall"
)

// processAlive reports whether pid exists. Signal 0 checks existence without
// delivering anything; EPERM means it exists but belongs to someone else.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
