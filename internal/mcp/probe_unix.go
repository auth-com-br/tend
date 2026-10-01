//go:build unix

package mcp

import (
	"os/exec"
	"syscall"
)

// setGroup gives a server started for a test a process group of its own:
// npx and uvx start the real server as a child, which killing npx alone
// would leave running.
func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup ends the server and everything it started.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) != nil {
		_ = cmd.Process.Kill()
	}
}
