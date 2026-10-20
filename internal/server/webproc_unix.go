//go:build unix

package server

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// detachWeb gives the phone view a session of its own.
func detachWeb(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// isWebProcess reports whether pid is alive and, where the system says,
// a tend web: a pid in a file left behind may since be somebody else's.
func isWebProcess(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if runtime.GOOS != "linux" {
		return true
	}
	argv, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	return bytes.Contains(argv, []byte("\x00web\x00")) || bytes.HasSuffix(argv, []byte("\x00web"))
}

// stopWeb asks the view to end, and makes it after a moment.
func stopWeb(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !isWebProcess(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
