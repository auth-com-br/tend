//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach lets the daemon outlive the command that started it, as tend's
// server does (cmd/tend/spawn_unix.go): a session of its own, so the ctrl+c
// meant for `tend whatsapp link` and the hang-up of its terminal do not end
// the one connection to WhatsApp.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
