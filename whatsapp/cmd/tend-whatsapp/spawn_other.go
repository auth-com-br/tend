//go:build !unix

package main

import "os/exec"

// detach does nothing where there is no setsid: the daemon is an ordinary
// child, which a Windows console does not take down with the parent.
func detach(cmd *exec.Cmd) {}
