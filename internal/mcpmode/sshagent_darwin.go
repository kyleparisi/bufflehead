package mcpmode

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

// restoreSSHAgent puts SSH_AUTH_SOCK back when the MCP client started us
// without it. Claude Desktop launches servers with a trimmed environment, so
// the macOS ssh-agent socket that every GUI app inherits from launchd is
// missing, and SSH bookmarks that rely on the agent fail. launchd still knows
// the socket, so ask it.
func restoreSSHAgent() {
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		return
	}
	out, err := exec.Command("/bin/launchctl", "getenv", "SSH_AUTH_SOCK").Output()
	if err != nil {
		return
	}
	if sock := strings.TrimSpace(string(out)); sock != "" {
		os.Setenv("SSH_AUTH_SOCK", sock)
		log.Printf("SSH_AUTH_SOCK was unset; using launchd's agent socket")
	}
}
