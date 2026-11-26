// Package transport carries protocol frames between a client and the server.
//
// It is a Unix domain socket. The socket is a door into a process that runs
// commands on the user's behalf, so it is created private and kept private:
// anyone who can connect can start a process as this user.
package transport

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ErrAlreadyRunning means a live server already holds the socket.
var ErrAlreadyRunning = errors.New("transport: a server is already listening")

// DefaultSessionName is the session used when none is given.
const DefaultSessionName = "default"

// dirPerm and socketPerm keep the socket reachable only by its owner. This is
// the access control: there is no authentication beyond the file system, so
// the permissions are the whole of it.
const (
	dirPerm    os.FileMode = 0o700
	socketPerm os.FileMode = 0o600
)

// checkPathLength rejects a socket path the kernel cannot bind.
//
// A Unix socket address is a fixed-size struct: the path lives in sun_path,
// which is 108 bytes on Linux and 104 on the BSDs including macOS. Exceeding
// it fails with a bare "invalid argument" that says nothing about why, so the
// check is here to turn that into an explanation and a way out.
func checkPathLength(path string) error {
	if len(path) < maxSocketPathLen {
		return nil
	}
	return fmt.Errorf(
		"transport: socket path is %d bytes, the limit is %d: %s\n"+
			"set TEND_RUNTIME_DIR to a shorter directory",
		len(path), maxSocketPathLen-1, path)
}

// SocketPath returns the socket for a named session.
//
// A session name is part of a file path, so it is restricted to characters
// that cannot walk out of the directory. Rejecting them is the point: a name
// arriving from a flag must not be able to place a socket somewhere else.
func SocketPath(name string) (string, error) {
	if name == "" {
		name = DefaultSessionName
	}
	if err := validSessionName(name); err != nil {
		return "", err
	}
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".sock")
	if err := checkPathLength(path); err != nil {
		return "", err
	}
	return path, nil
}

// ValidSessionName is whether a name can be a session's.
func ValidSessionName(name string) error { return validSessionName(name) }

// RuntimeDir is the directory the sessions' sockets are in. A program of
// tend's with a socket of its own (tend-whatsapp) puts it in a directory
// under this one, never beside the sessions' sockets: sessions are found by
// listing *.sock here, and another socket would be taken for one.
func RuntimeDir() (string, error) { return runtimeDir() }

// CheckSocketPath is the length check Listen makes, for a socket path made
// outside this package, so a too-long one is explained before it is used.
func CheckSocketPath(path string) error { return checkPathLength(path) }

func validSessionName(name string) error {
	if len(name) > 64 {
		return fmt.Errorf("transport: session name is too long")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("transport: session name %q contains %q; use letters, digits, dash, underscore or dot", name, r)
		}
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return fmt.Errorf("transport: session name %q is not allowed", name)
	}
	return nil
}

// APISocketPath is where a session's automation socket lives.
//
// A directory of its own rather than a second name beside the first: sessions
// are found by listing sockets, and a session must not appear twice, nor a
// session called "work.api" be mistaken for the automation half of "work".
func APISocketPath(name string) (string, error) {
	if name == "" {
		name = DefaultSessionName
	}
	if err := validSessionName(name); err != nil {
		return "", err
	}
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "api", name+".sock")
	if err := checkPathLength(path); err != nil {
		return "", err
	}
	return path, nil
}

// Listen creates the socket for a session and starts accepting on it.
//
// A socket left behind by a crashed server would otherwise block every later
// start, so a stale one is removed — but only after checking that nothing is
// listening on it. Removing a live server's socket would silently orphan it,
// leaving its panes running and unreachable.
func Listen(path string) (net.Listener, error) {
	if err := checkPathLength(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("transport: creating %s: %w", filepath.Dir(path), err)
	}

	if _, err := os.Stat(path); err == nil {
		switch state := probe(path); state {
		case socketLive:
			return nil, fmt.Errorf("%w at %s", ErrAlreadyRunning, path)
		case socketStale:
			if err := os.Remove(path); err != nil {
				return nil, fmt.Errorf("transport: removing stale socket: %w", err)
			}
		default:
			// Neither answer. Taking the socket on a maybe is how one loaded
			// machine ends up with two servers on one session and the first
			// one's clients talking to a file nobody is reading.
			return nil, fmt.Errorf("transport: cannot tell whether a server holds %s", path)
		}
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("transport: listening on %s: %w", path, err)
	}
	// Narrow the socket before anyone can reach it. A window where it is
	// world-writable is a window where anyone can run a command as this user.
	if err := os.Chmod(path, socketPerm); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("transport: securing %s: %w", path, err)
	}
	return ln, nil
}

// Dial connects to a server.
func Dial(path string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("transport: connecting to %s: %w", path, err)
	}
	return conn, nil
}

// socketState is what a probe of an existing socket file found.
type socketState uint8

const (
	// socketUnknown is the answer when the probe failed for a reason that is
	// not "nobody is listening" — a timeout, a permission, an interruption.
	socketUnknown socketState = iota
	// socketLive means something accepted the connection.
	socketLive
	// socketStale means the file is there and nothing is behind it.
	socketStale
)

// probeTimeout bounds the connect. It is generous because it is not what
// decides the answer: a refused connection comes back at once whatever the
// timeout, and a timeout means the probe failed, not that the socket is dead.
const probeTimeout = 3 * time.Second

// probe asks whether a server holds the socket.
//
// The distinction it draws is the whole point. Connecting to a Unix socket
// with no listener is refused immediately by the kernel, which is a definite
// answer; anything else — a slow machine, a denied permission — is not an
// answer at all. Reading "I did not hear back in time" as "nothing is there"
// is what lets a busy machine delete a running server's socket.
func probe(path string) socketState {
	conn, err := net.DialTimeout("unix", path, probeTimeout)
	if err == nil {
		_ = conn.Close()
		return socketLive
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
		return socketStale
	}
	return socketUnknown
}

// Sessions lists the sessions that currently have a socket, whether or not a
// server is still behind it.
func Sessions() ([]string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".sock")
		if !ok {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
