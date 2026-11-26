// Package core is the desktop client's view of a session, with no Qt in it:
// the connection, each pane's terminal state, and the translation of keys
// into the bytes a terminal sends. It is kept apart from the Qt code so it
// can be tested by `go test` on a machine with no display, and so the
// QML side only ever draws what this package has already decided.
package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/transport"
)

// ErrNoTend is a desktop with no terminal tend to start a server with. The
// server is the tend binary's (`tend serve`), not the desktop's: one server
// serves both clients, and the desktop does not carry a second copy of it.
var ErrNoTend = errors.New("tend is not installed: the desktop needs the tend binary to start a session (make install)")

// FindTend is the tend binary the desktop starts servers with: the one given,
// else the one beside the desktop's own binary, else the one on PATH.
//
// Beside first, because a desktop and a tend installed together should be
// the same version; a tend found on PATH may be an older one, whose server
// would then be talking to a newer client.
func FindTend(given string) (string, error) {
	if given != "" {
		return given, nil
	}
	if self, err := os.Executable(); err == nil {
		beside := filepath.Join(filepath.Dir(self), "tend")
		if info, err := os.Stat(beside); err == nil && !info.IsDir() {
			return beside, nil
		}
	}
	if p, err := exec.LookPath("tend"); err == nil {
		return p, nil
	}
	return "", ErrNoTend
}

// OpenSession connects to the named session, starting its server with the
// tend binary at tendPath when there is none.
//
// This is cmd/tend's openSessionOn for the local case, written again rather
// than shared because that one lives in package main and starts its own
// binary. The behaviour is the same: dial, start a server only when nothing
// is listening, and wait for it, since two clients starting one at once is
// resolved by the second server failing to take the socket.
func OpenSession(name, tendPath string, handler client.Handler) (*client.Client, error) {
	path, err := transport.SocketPath(name)
	if err != nil {
		return nil, err
	}
	c, err := client.Dial(path, handler)
	if err == nil {
		return c, nil
	}
	if !notRunning(err) {
		return nil, err
	}
	if err := startServer(tendPath, name, path); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := client.Dial(path, handler); err == nil {
			return c, nil
		} else if !notRunning(err) {
			return nil, err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("started a server for %q but it never came up; see %s", name, serverLog(path))
}

// serverLog is cmd/tend's serverLog: the socket's name with .log for .sock.
func serverLog(socket string) string {
	return strings.TrimSuffix(socket, ".sock") + ".log"
}

func notRunning(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// startServer runs `tend serve -s name` in a session of its own, so closing
// the desktop window does not take the server, and with it every pane, along.
// Its output goes to the same log file cmd/tend's server writes, which is
// the only record of why a session failed to start.
func startServer(tendPath, name, socket string) error {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return fmt.Errorf("creating the runtime directory: %w", err)
	}
	log, err := os.OpenFile(serverLog(socket), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening the server log: %w", err)
	}
	defer log.Close()
	cmd := exec.Command(tendPath, "serve", "-s", name)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting a server for %q with %s: %w", name, tendPath, err)
	}
	return cmd.Process.Release()
}
