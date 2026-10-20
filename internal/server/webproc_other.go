//go:build !unix

package server

import (
	"errors"
	"os/exec"
)

// The phone view is not started from a server here yet.
func detachWeb(*exec.Cmd)   {}
func isWebProcess(int) bool { return false }
func stopWeb(int) error     { return errors.New("not supported on this system") }
