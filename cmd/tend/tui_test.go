//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// The TUI is tested by running it, because almost everything that can go wrong
// with it only happens against a real terminal: raw mode, the alternate
// screen, escape sequences arriving in pieces. So tend's own pty runs tend's
// own binary, and tend's own terminal emulator reads what it drew.
//
// That makes this the one test where the whole stack is exercised at once —
// and the one that fails when any layer of it stops agreeing with another.

// buildBinary compiles the CLI once per test run.
func buildBinary(t *testing.T) string {
	t.Helper()
	return buildBinaryWith(t)
}

// buildBinaryWith builds the client, optionally with the race detector. The
// tests run the real binary in another process, so the detector in the test
// process sees nothing of what it does: a test about the client's own
// goroutines has to ask for a binary that is watching itself.
func buildBinaryWith(t *testing.T, flags ...string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tend")
	args := append([]string{"build"}, flags...)
	cmd := exec.Command("go", append(args, "-o", bin, ".")...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the binary: %v\n%s", err, out)
	}
	return bin
}

// raceBinary is what the next session should run, when a test needs the
// client to be watching its own goroutines.
var raceBinary bool

// configOverride is the settings file the next session should use, for the
// tests that are about settings. withConfig sets it for one test.
var configOverride string

func withConfig(t *testing.T, path string) {
	t.Helper()
	configOverride = path
	t.Cleanup(func() { configOverride = "" })
}

// attached is a running TUI and a terminal reading it.
type attached struct {
	pty      *pty.Pty
	mu       sync.Mutex
	screen   *vt.Screen
	rawBytes strings.Builder
}

func (a *attached) send(t *testing.T, keys string) {
	t.Helper()
	if _, err := a.pty.Write([]byte(keys)); err != nil {
		t.Fatalf("sending %q: %v", keys, err)
	}
}

// lines returns what the TUI has drawn.
func (a *attached) lines() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	g := a.screen.Grid()
	out := make([]string, g.Rows())
	for y := 0; y < g.Rows(); y++ {
		out[y] = g.Line(y).Text()
	}
	return out
}

func (a *attached) text() string { return strings.Join(a.lines(), "\n") }

// raw is every byte the client has written, for the tests about sequences the
// emulator does not keep — an image is drawn by the terminal, not stored in
// its grid.
func (a *attached) raw() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rawBytes.String()
}

// Write feeds the TUI's output into the terminal emulator under a lock, since
// the test goroutine reads it while this one writes.
func (a *attached) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rawBytes.Write(p)
	return a.screen.Write(p)
}

// waitForScreen polls until the drawn screen satisfies cond.
func (a *attached) waitForScreen(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond(a.text()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; the screen was:\n%s", what, a.text())
}

// startSession runs a server in this process and attaches the real binary to
// it over a pty.
func startSession(t *testing.T, cols, rows int) *attached {
	t.Helper()
	// The same build as the client, so the notice about an older server stays
	// out of the way of tests that are not about it. An empty build is what a
	// genuinely old server reports, and it is a case worth keeping distinct.
	return startSessionBuilt(t, cols, rows, version)
}

// startSessionBuilt runs the session's server as a given build, so a client
// meeting a server older than itself can be tested.
func startSessionBuilt(t *testing.T, cols, rows int, build string) *attached {
	t.Helper()
	return startSessionWith(t, cols, rows, build, "")
}

// startSessionOlder runs the session's server as one that has never heard of
// a method this client uses, which is what being behind actually means.
func startSessionOlder(t *testing.T, cols, rows int, build string) *attached {
	t.Helper()
	var without []string
	for _, m := range server.Methods {
		if m != proto.MethodPaneText {
			without = append(without, m)
		}
	}
	return startSessionWith(t, cols, rows, build, "", without...)
}

// startSessionIn runs the session's server rooted at a directory, which is
// what a workspace with none of its own takes.
// startSessionEnv is a session whose client is told something extra about the
// terminal it is drawing to.
func startSessionEnv(t *testing.T, cols, rows int, env ...string) *attached {
	t.Helper()
	extraClientEnv = env
	t.Cleanup(func() { extraClientEnv = nil })
	return startSessionIn(t, cols, rows, t.TempDir())
}

// extraClientEnv is added to the client's environment by the next session.
var extraClientEnv []string

func startSessionIn(t *testing.T, cols, rows int, dir string) *attached {
	t.Helper()
	return startSessionWith(t, cols, rows, version, dir)
}

func startSessionWith(t *testing.T, cols, rows int, build, dir string, advertise ...string) *attached {
	t.Helper()
	return startSessionConfigured(t, cols, rows, server.Config{Build: build, Dir: dir, Advertise: advertise})
}

// startSessionConfigured runs the session's server from a given configuration,
// for the tests that need a server unlike the one this build would start.
func startSessionConfigured(t *testing.T, cols, rows int, cfg server.Config) *attached {
	t.Helper()

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	// A file of the defaults (quietConfig), so the tests see them rather than
	// whatever settings the machine running them happens to have. A test
	// about settings names its own file instead.
	configPath := configOverride
	if configPath == "" {
		configPath = quietConfig(t)
	}
	t.Setenv("TEND_CONFIG", configPath)

	path, err := transport.SocketPath("tui")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DetectInterval = 20 * time.Millisecond
	cfg.DefaultSize = pty.Size{Cols: uint16(cols), Rows: uint16(rows)}
	cfg.ShutdownGrace = time.Second
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()

	bin := buildBinary(t)
	if raceBinary {
		bin = buildBinaryWith(t, "-race")
	}
	p, err := pty.Start(bin, []string{"attach", "-s", "tui"}, pty.Options{
		Size: pty.Size{Cols: uint16(cols), Rows: uint16(rows)},
		Env: append(append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"TEND_CONFIG="+configPath,
			// A predictable shell, so what a new pane runs does not depend on
			// whoever is running the tests.
			"SHELL=/bin/sh",
			"TERM=xterm-256color",
		), extraClientEnv...),
	})
	if err != nil {
		t.Fatalf("attaching: %v", err)
	}

	a := &attached{pty: p, screen: vt.NewScreen(cols, rows, 200)}
	go func() { _, _ = io.Copy(a, p) }()

	t.Cleanup(func() {
		_ = p.Close()
		_ = srv.Close()
		_ = ln.Close()
		<-served

		// A test that makes the client replace the server leaves one behind
		// that this harness never started and so would never stop: its binary
		// is in a temporary directory that is about to be deleted, and the
		// process outlives the run. Whatever is answering on the socket gets
		// shut down, which is nothing at all in the ordinary case.
		stopSession(t, "tui")
	})
	return a
}

// dividerColumn returns the screen column where two panes meet, counted in
// cells rather than bytes.
//
// The box-drawing characters are three bytes each, so strings.Index gives an
// offset that is not a column. Comparing two such offsets happens to work;
// clicking at one does not.
func (a *attached) dividerColumn() int {
	for _, line := range a.lines() {
		col := 0
		var prev rune
		for _, r := range line {
			if prev == '┐' && r == '┌' {
				return col - 1
			}
			prev = r
			col++
		}
	}
	return -1
}

// stopSession shuts a session's server down and waits for it to be gone.
//
// Waiting is the point. A server told to stop writes the session down on its
// way out, and a test that deletes its temporary directory while that is
// happening fails on a directory that is not empty — for a reason that has
// nothing to do with what the test was about.
func stopSession(t *testing.T, name string) {
	t.Helper()
	c, err := connect(name, nil)
	if err != nil {
		return // nothing is running, which is the ordinary case
	}
	_ = c.Shutdown()
	_ = c.Close()
	if path, err := transport.SocketPath(name); err == nil {
		waitForSocketGone(path)
	}
}

// waitForSocketGone returns once a server has removed its socket, which it
// does last of all, or after a bound so a hung server fails the test that
// follows rather than hanging the run.
func waitForSocketGone(path string) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAttachDrawsAPane is the whole stack in one assertion: a server starts a
// shell on a pty, renders its screen to ANSI, sends it over a socket, and the
// client parses and draws it inside a bordered frame.
func TestAttachDrawsAPane(t *testing.T) {
	a := startSession(t, 80, 20)

	a.waitForScreen(t, "a bordered pane", func(s string) bool {
		return strings.Contains(s, "┌") && strings.Contains(s, "┘")
	})
	a.waitForScreen(t, "the status bar", func(s string) bool {
		return strings.Contains(s, "tui")
	})

	// A shell is running in it, so a command typed into the pane comes back.
	a.send(t, "printf tend-was-here\n")
	a.waitForScreen(t, "the shell's output", func(s string) bool {
		return strings.Contains(s, "tend-was-here")
	})
}

// TestAttachSplitsPanes covers the prefix key reaching the client rather than
// the pane, and the layout changing as a result.
func TestAttachSplitsPanes(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "the first pane", func(s string) bool {
		return strings.Contains(s, "┌")
	})

	countBorders := func(s string) int { return strings.Count(s, "┌") }
	if got := countBorders(a.text()); got != 1 {
		t.Fatalf("started with %d panes, want 1", got)
	}

	a.send(t, "\x02|")
	a.waitForScreen(t, "a second pane beside the first", func(s string) bool {
		return countBorders(s) == 2
	})

	a.send(t, "\x02-")
	a.waitForScreen(t, "a third pane below", func(s string) bool {
		return countBorders(s) == 3
	})

	// The panes must tile: no two top-left corners in the same column on the
	// same row unless the layout really is a grid.
	if got := countBorders(a.text()); got != 3 {
		t.Errorf("got %d panes, want 3", got)
	}
}

// TestAttachClosesAPane checks the other direction, and that the client keeps
// drawing afterwards rather than being left pointing at something gone.
func TestAttachClosesAPane(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "the first pane", func(s string) bool {
		return strings.Contains(s, "┌")
	})

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	a.send(t, "\x02x")
	a.waitForScreen(t, "one pane again", func(s string) bool {
		return strings.Count(s, "┌") == 1
	})
}

// TestAttachPrefixIsNotForwarded: the prefix key belongs to the client, and a
// pane must never see it. An armed prefix is also shown, so the user is not
// guessing about the mode they are in.
func TestAttachPrefixIsNotForwarded(t *testing.T) {
	a := startSession(t, 80, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02")
	a.waitForScreen(t, "the prefix indicator", func(s string) bool {
		return strings.Contains(s, "PREFIX")
	})

	// Cancel it with an unbound key, which is forwarded instead.
	a.send(t, "Z")
	a.waitForScreen(t, "the prefix to clear", func(s string) bool {
		return !strings.Contains(s, "PREFIX")
	})
}

// TestAttachShowsHelp keeps the discoverability promise: the keys are one
// keystroke away rather than in the manual.
func TestAttachShowsHelp(t *testing.T) {
	a := startSession(t, 110, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02?")
	a.waitForScreen(t, "the key help", func(s string) bool {
		return strings.Contains(s, "detach")
	})
}

// TestAttachDetachLeavesTheSessionRunning is the point of the whole
// client/server split: the interface goes away, the agents do not.
func TestAttachDetachLeavesTheSessionRunning(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	path, err := transport.SocketPath("detach")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv, err := server.New(server.Config{
		Build:          version,
		DetectInterval: 20 * time.Millisecond,
		ShutdownGrace:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go func() { _ = srv.Serve(ln) }()

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "detach"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env:  append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh"),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02d")
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the client did not exit after detaching")
	}

	// The pane is still there, and a fresh connection can see it.
	c, err := connect("detach", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	snap, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 1 {
		t.Fatalf("after detaching, the session has %d panes, want 1", len(snap.Panes))
	}
	if !snap.Panes[0].Running {
		t.Error("the pane's process should still be running")
	}
}

// TestAttachRefusesWithoutATerminal: drawing into a pipe produces escape
// sequences nobody asked for, so it is refused with a pointer to the command
// that does work there.
func TestAttachRefusesWithoutATerminal(t *testing.T) {
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())
	bin := buildBinary(t)

	cmd := exec.Command(bin, "attach")
	cmd.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+os.Getenv("TEND_RUNTIME_DIR"))
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("attaching without a terminal should fail")
	}
	if !strings.Contains(string(out), "terminal") {
		t.Errorf("error = %q, want it to explain the terminal requirement", out)
	}
}

// TestBareTendStartsItsOwnServer is the shape of the thing: one command, no
// server running, and you are in a session.
//
// Needing a daemon is tend's problem rather than the user's, and this is the
// test that keeps it that way — it starts from an empty runtime directory with
// nothing listening.
func TestBareTendStartsItsOwnServer(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	bin := buildBinary(t)
	p, err := pty.Start(bin, nil, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"SHELL=/bin/sh",
		),
	})
	if err != nil {
		t.Fatalf("running tend with no arguments: %v", err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()

	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "default")
	})

	a.waitForScreen(t, "a session drawn from nothing", func(s string) bool {
		return strings.Contains(s, "┌") && strings.Contains(s, "default")
	})

	// A shell really is running in it.
	a.send(t, "printf started-from-nothing\n")
	a.waitForScreen(t, "the shell's output", func(s string) bool {
		return strings.Contains(s, "started-from-nothing")
	})
}

// TestStartedServerOutlivesTheClient: the server tend starts is detached, so
// closing the client leaves the session running rather than taking it down.
func TestStartedServerOutlivesTheClient(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "auto"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env:  append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh"),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()

	t.Cleanup(func() {
		stopSession(t, "auto")
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02d")
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the client did not exit after detaching")
	}

	c, err := connect("auto", nil)
	if err != nil {
		t.Fatalf("the server should still be running: %v", err)
	}
	defer c.Close()
	snap, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 1 {
		t.Errorf("the session has %d panes, want 1", len(snap.Panes))
	}
}

// TestCommandsThatInspectDoNotCreate: listing a session that does not exist
// must say so. Creating one on the way to listing it would report an empty
// session rather than the absence of one.
func TestCommandsThatInspectDoNotCreate(t *testing.T) {
	runtimeDir := t.TempDir()
	bin := buildBinary(t)

	for _, args := range [][]string{
		{"ls", "-s", "ghost"},
		{"kill", "-s", "ghost", "1"},
	} {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("%v succeeded against a session that does not exist", args)
		}
		if !strings.Contains(string(out), "no session") {
			t.Errorf("%v said %q, want it to report the missing session", args, out)
		}
	}

	if entries, err := os.ReadDir(runtimeDir); err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("inspecting created %v", names)
	}
}

// TestAttachZoomsAPane: a zoomed pane fills the area and the others go away,
// and zooming again puts them back.
func TestAttachZoomsAPane(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	a.send(t, "\x02z")
	a.waitForScreen(t, "one pane filling the screen", func(s string) bool {
		return strings.Count(s, "┌") == 1 && strings.Contains(s, "zoom")
	})

	a.send(t, "\x02z")
	a.waitForScreen(t, "both panes back", func(s string) bool {
		return strings.Count(s, "┌") == 2 && !strings.Contains(s, "zoom")
	})
}

// TestAttachResizesASplit checks the divider actually moves, by watching the
// column the second pane starts at.
func TestAttachResizesASplit(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	start := a.dividerColumn()
	if start < 0 {
		t.Fatalf("could not find the divider:\n%s", a.text())
	}

	// Focus is on the new right-hand pane, so growing it leftwards moves the
	// divider left. Resizing is herdr's resize mode: prefix+r, then h/j/k/l
	// as many times as wanted without the prefix, and escape.
	a.send(t, "\x02r")
	a.waitForScreen(t, "resize mode", func(s string) bool { return strings.Contains(s, "resize") })
	a.send(t, "h")
	a.waitForScreen(t, "the divider to move left", func(string) bool {
		d := a.dividerColumn()
		return d >= 0 && d < start
	})
	moved := a.dividerColumn()
	a.send(t, "h")
	a.waitForScreen(t, "a second press to move it again, with no prefix", func(string) bool {
		d := a.dividerColumn()
		return d >= 0 && d < moved
	})
	a.send(t, "\x1b")
	a.waitForScreen(t, "resize mode to end", func(s string) bool { return !strings.Contains(s, "resize") })
}

// TestAttachShowsTabs: the bar is there from the first tab, because it holds
// the button that makes the next one. A button that appears only once you have
// what it creates is a button nobody finds.
func TestAttachShowsTabs(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.waitForScreen(t, "the tab bar", func(string) bool {
		first := a.lines()[0]
		return strings.Contains(first, "tab 1") && strings.Contains(first, "+")
	})

	a.send(t, "\x02c")
	a.waitForScreen(t, "a second tab", func(string) bool {
		first := a.lines()[0]
		return strings.Contains(first, "tab 1") && strings.Contains(first, "tab 2")
	})
}

// TestAttachScrollsBack: a pane's history is reachable without stopping it.
func TestAttachScrollsBack(t *testing.T) {
	a := startSession(t, 80, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// More output than fits, so the early lines are only in the scrollback.
	a.send(t, "for i in $(seq 1 40); do echo line-$i; done\n")
	a.waitForScreen(t, "the last line", func(s string) bool {
		return strings.Contains(s, "line-40")
	})
	if strings.Contains(a.text(), "line-1\n") {
		t.Fatal("line-1 should have scrolled off already")
	}

	// prefix+[ is copy mode now, which is the scroll view with a cursor, as
	// in tmux and herdr. Paging up moves the cursor and the view goes with it.
	a.send(t, "\x02[")
	a.waitForScreen(t, "the copy-mode indicator", func(s string) bool {
		return strings.Contains(s, "copy ")
	})

	// Page back until the earliest line appears.
	for i := 0; i < 10 && !strings.Contains(a.text(), "line-2 "); i++ {
		a.send(t, "\x1b[5~")
		time.Sleep(60 * time.Millisecond)
	}
	if !strings.Contains(a.text(), "line-2") {
		t.Errorf("scrolling back did not reach the early output:\n%s", a.text())
	}

	// Leaving the view returns to the live screen.
	a.send(t, "q")
	a.waitForScreen(t, "the live screen", func(s string) bool {
		return !strings.Contains(s, "copy ") && strings.Contains(s, "line-40")
	})
}

// TestAttachUsesTheConfiguredPrefix: a user who rebinds the prefix gets the
// new key, and the old one reaches the pane like any other keystroke.
func TestAttachUsesTheConfiguredPrefix(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[keys]\nprefix = \"ctrl+a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)

	p, err := pty.Start(bin, []string{"attach", "-s", "cfg"}, pty.Options{
		Size: pty.Size{Cols: 90, Rows: 20},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"TEND_CONFIG="+cfgPath,
			"SHELL=/bin/sh",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(90, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "cfg")
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// The configured prefix works.
	a.send(t, "\x01|")
	a.waitForScreen(t, "a split from ctrl+a", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})
}

// TestAttachRejectsABrokenConfig: running with settings the user did not
// write, and not saying so, is worse than refusing to start.
func TestAttachRejectsABrokenConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[keys]\nprefix = \"banana\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := buildBinary(t)
	cmd := exec.Command(bin, "attach")
	cmd.Env = append(os.Environ(),
		"TEND_RUNTIME_DIR="+t.TempDir(),
		"TEND_CONFIG="+cfgPath,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("a broken config should stop tend from starting")
	}
	if !strings.Contains(string(out), "prefix") {
		t.Errorf("error = %q, want it to name the setting", out)
	}
}

// TestConfigCommand covers the path people are pointed at when something is
// wrong with their settings.
func TestConfigCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	bin := buildBinary(t)

	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "TEND_CONFIG="+cfgPath)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("config")
	if err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	if !strings.Contains(out, cfgPath) || !strings.Contains(out, "none") {
		t.Errorf("output = %q, want the path and that there is no file", out)
	}

	if out, err := run("config", "-init"); err != nil {
		t.Fatalf("config -init: %v\n%s", err, out)
	}
	out, err = run("config")
	if err != nil {
		t.Fatalf("config after init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "loads cleanly") {
		t.Errorf("output = %q, want it to load", out)
	}

	// A second -init must not quietly overwrite what the user has written.
	if _, err := run("config", "-init"); err == nil {
		t.Error("a second -init should refuse without -force")
	}
}

// TestVersionIsStamped: a binary that cannot say what built it is one nobody
// can report a bug against.
func TestVersionIsStamped(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tend")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v9.9.9-test", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "v9.9.9-test") {
		t.Errorf("version = %q, want the stamped value", out)
	}
}

// TestAttachClickFocusesAPane: clicking is the plainest way to say "I mean
// that one", and it has to work without the keyboard.
func TestAttachClickFocusesAPane(t *testing.T) {
	a := startSession(t, 86, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	// Focus follows a split, so it is on the right-hand pane. Clicking the
	// left one and typing must put the text there. The sidebar owns the
	// leftmost columns, so the click aims between it and the divider.
	divider := a.dividerColumn()
	if divider < 0 {
		t.Fatalf("no divider:\n%s", a.text())
	}
	a.clickAt(t, (ui.SidebarWidth+divider)/2, 5)
	a.send(t, "printf clicked\n")

	a.waitForScreen(t, "the click to move focus", func(string) bool {
		for _, line := range a.lines() {
			if i := columnOfString(line, "clicked"); i >= 0 && i < divider {
				return true // it landed in the left pane
			}
		}
		return false
	})
}

// TestAttachDragResizesASplit: grabbing a border is the other half of the
// mouse being useful, and it must not be confused with clicking the pane.
func TestAttachDragResizesASplit(t *testing.T) {
	a := startSession(t, 86, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	start := a.dividerColumn()
	if start < 0 {
		t.Fatalf("no divider found:\n%s", a.text())
	}

	// Press on the border, move ten columns left, release. Mouse reports
	// count from one, so the column is the cell plus one.
	a.send(t, "\x1b[<0;"+itoa(start+1)+";4M")
	time.Sleep(100 * time.Millisecond)
	a.send(t, "\x1b[<32;"+itoa(start-9)+";4M")

	a.waitForScreen(t, "the divider to follow the drag", func(string) bool {
		d := a.dividerColumn()
		return d >= 0 && d < start-5
	})
	a.send(t, "\x1b[<0;"+itoa(start-9)+";4m")
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// sendUntil retypes a line until the screen shows what it should produce.
func (a *attached) sendUntil(t *testing.T, keys, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.send(t, keys)
		for i := 0; i < 8; i++ {
			if cond(a.text()) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Fatalf("timed out waiting for %s; the screen was:\n%s", what, a.text())
}

// TestAttachReconnects: a server restarting is not the client's failure, and
// exiting when it happens loses the user's place for a reason that had
// nothing to do with them.
func TestAttachReconnects(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	configPath := quietConfig(t)
	t.Setenv("TEND_CONFIG", configPath)
	bin := buildBinary(t)

	p, err := pty.Start(bin, []string{"attach", "-s", "again"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 14},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"TEND_CONFIG="+configPath,
			"SHELL=/bin/sh",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 14, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "again")
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Mark the old session, so the screen after the reconnect can be told
	// from the one still on the terminal when the server went away.
	a.sendUntil(t, "printf OLD-SESSION\n", "the marker", func(s string) bool {
		return strings.Contains(s, "OLD-SESSION")
	})

	// Stop the server out from under it.
	c, err := connect("again", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	// The client says so rather than vanishing, then finds its way back: it
	// starts a server itself, exactly as it did the first time, and that
	// server comes back to the session the old one wrote down. "reconnected"
	// is what says this is the new server rather than the last frame of the
	// old one, and the marker still being there is what says nothing was lost.
	a.waitForScreen(t, "the session again", func(s string) bool {
		return strings.Contains(s, "reconnected") &&
			strings.Contains(s, "┌") &&
			strings.Contains(s, "OLD-SESSION")
	})

	// The pane is drawn as soon as it exists, which is before the shell
	// inside it has read anything, so the first line typed can land before
	// anything is listening. Say it again until it is heard.
	a.sendUntil(t, "printf back-again\n", "the new session to work", func(s string) bool {
		return strings.Contains(s, "back-again")
	})
}

// TestAttachTabsAreScopedToTheirSpace is the bug this set of features started
// from: a bar listing every tab in the session puts tabs the user cannot
// reach next to ones they can.
func TestAttachTabsAreScopedToTheirSpace(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Two tabs in the first space.
	a.send(t, "\x02c")
	a.waitForScreen(t, "a tab bar", func(string) bool {
		return strings.Contains(a.lines()[0], "tab 2")
	})

	// A new space starts with one tab of its own, and the first space's tabs
	// must not appear in its bar.
	a.send(t, "\x02N")
	a.waitForScreen(t, "the new space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})
	if first := a.lines()[0]; strings.Contains(first, "tab 2") {
		t.Errorf("the tab bar shows another space's tabs: %q", first)
	}

	// Going back finds the first space's tabs again.
	a.send(t, "\x02(")
	a.waitForScreen(t, "the first space", func(string) bool {
		return strings.Contains(a.lines()[0], "tab 2")
	})
}

// TestAttachAgentListGroupsEverything: the point of seeing every agent at
// once is reaching the one that stopped, wherever it lives.
func TestAttachAgentListGroupsEverything(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})

	// Every space is listed, not only the one being looked at.
	text := a.text()
	if !strings.Contains(text, "spaces") || !strings.Contains(text, "agents") {
		t.Errorf("both sections should be shown:\n%s", text)
	}
	if !strings.Contains(text, "main") || !strings.Contains(text, "space 2") {
		t.Errorf("the list should show every space:\n%s", text)
	}

	// Closing it gives the columns back: the pane returns to the left edge.
	a.send(t, "\x02a")
	a.waitForScreen(t, "the list to close", func(string) bool {
		return a.paneStartsAtLeftEdge()
	})

	a.send(t, "\x02a")
	a.waitForScreen(t, "the list to come back", func(s string) bool {
		return strings.Contains(s, "spaces")
	})
}

// paneStartsAtLeftEdge reports whether the panes have the left of the screen,
// which they do only when the sidebar is not taking those columns.
func (a *attached) paneStartsAtLeftEdge() bool {
	// The tab bar is skipped: it is drawn on its own row.
	lines := a.lines()
	if len(lines) < 2 {
		return false
	}
	// "The edge" is the gutter, not column zero. Two columns stay behind for
	// the handle that brings the sidebar back, or a sidebar put away with the
	// mouse could not be recovered with it.
	for _, r := range []rune(lines[1]) {
		if r == ' ' {
			continue
		}
		return r == '┌' || r == '│' || r == '└'
	}
	return false
}

// TestAttachNavigateJumpsAcrossSpaces covers the reason the list exists.
func TestAttachNavigateJumpsAcrossSpaces(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Mark the first space's pane so it can be recognised after the jump.
	a.send(t, "printf FIRST-SPACE\n")
	a.waitForScreen(t, "the marker", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE")
	})

	a.send(t, "\x02N")
	a.waitForScreen(t, "the second space", func(s string) bool {
		return strings.Contains(s, "space 2") && !strings.Contains(s, "FIRST-SPACE")
	})

	// Walk the list back to the first space and jump to it.
	a.send(t, "\x02w")
	a.waitForScreen(t, "navigate mode", func(s string) bool {
		return strings.Contains(s, "NAVIGATE")
	})
	a.send(t, "k\r")

	a.waitForScreen(t, "the jump to land", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE") && !strings.Contains(s, "NAVIGATE")
	})
}

func TestAttachSelectsTabByNumber(t *testing.T) {
	a := startSession(t, 100, 14)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "printf TAB-ONE\n")
	a.waitForScreen(t, "the marker", func(s string) bool {
		return strings.Contains(s, "TAB-ONE")
	})
	a.send(t, "\x02c")
	a.waitForScreen(t, "a second tab", func(s string) bool {
		return !strings.Contains(s, "TAB-ONE")
	})

	a.send(t, "\x021")
	a.waitForScreen(t, "the first tab", func(s string) bool {
		return strings.Contains(s, "TAB-ONE")
	})
}

// TestAttachRenamesATab: the seeded name starts selected, so typing replaces
// it rather than appending to it.
func TestAttachRenamesATab(t *testing.T) {
	a := startSession(t, 100, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02,")
	a.waitForScreen(t, "the prompt", func(s string) bool {
		return strings.Contains(s, "rename tab")
	})
	a.send(t, "builder")
	a.waitForScreen(t, "the typed name", func(s string) bool {
		return strings.Contains(s, "builder") && !strings.Contains(s, "tab 1builder")
	})

	a.send(t, "\r")
	a.waitForScreen(t, "the new name", func(s string) bool {
		return strings.Contains(s, "· builder")
	})
}

// TestAttachRenameCanBeCancelled: escape must leave the name as it was.
func TestAttachRenameCanBeCancelled(t *testing.T) {
	a := startSession(t, 100, 12)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02,")
	a.waitForScreen(t, "the prompt", func(s string) bool {
		return strings.Contains(s, "rename tab")
	})
	a.send(t, "discarded\x1b")
	a.waitForScreen(t, "the prompt to close", func(s string) bool {
		return !strings.Contains(s, "rename tab")
	})
	if strings.Contains(a.text(), "discarded") {
		t.Errorf("a cancelled rename took effect:\n%s", a.text())
	}
}

// TestAttachNamesNewSpacesAndTabs: an unnamed space shows as a dash and
// vanishes from the status bar, making the thing just created the hardest to
// find.
func TestAttachNamesNewSpacesAndTabs(t *testing.T) {
	a := startSession(t, 100, 14)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02N")
	a.waitForScreen(t, "the new space named", func(s string) bool {
		return strings.Contains(s, "· space 2 ·")
	})

	a.send(t, "\x02c")
	a.waitForScreen(t, "the new tab named", func(s string) bool {
		return strings.Contains(s, "· tab 2")
	})
}

// clickAt sends a press and release at a column and row, counted from one as
// the terminal reports them.
func (a *attached) clickAt(t *testing.T, col, row int) {
	t.Helper()
	seq := "\x1b[<0;" + itoa(col) + ";" + itoa(row) + "M" +
		"\x1b[<0;" + itoa(col) + ";" + itoa(row) + "m"
	a.send(t, seq)
	time.Sleep(120 * time.Millisecond)
}

// columnOfString returns the cell column where a substring starts, or -1.
func columnOfString(line, want string) int {
	i := strings.Index(line, want)
	if i < 0 {
		return -1
	}
	return len([]rune(line[:i]))
}

// columnOf returns the cell column of a rune in a line, counting cells rather
// than bytes — the box-drawing characters are three bytes each.
func columnOf(line string, want rune) int {
	col := 0
	for _, r := range line {
		if r == want {
			return col
		}
		col++
	}
	return -1
}

// TestAttachClickCreatesATab covers the plus button, which is how a tab gets
// made by someone who does not know the keys.
func TestAttachClickCreatesATab(t *testing.T) {
	a := startSession(t, 90, 14)
	a.waitForScreen(t, "the tab bar", func(string) bool {
		return strings.Contains(a.lines()[0], "+")
	})

	plus := columnOf(a.lines()[0], '+')
	if plus < 0 {
		t.Fatalf("no plus button in %q", a.lines()[0])
	}
	a.clickAt(t, plus+1, 1)

	a.waitForScreen(t, "a second tab", func(string) bool {
		return strings.Contains(a.lines()[0], "tab 2")
	})
	a.waitForScreen(t, "the new tab to be the one shown", func(s string) bool {
		return strings.Contains(s, "· tab 2")
	})
}

// TestAttachClickSelectsATab: navigating with the mouse means clicking the
// one you want, not cycling until it appears.
func TestAttachClickSelectsATab(t *testing.T) {
	a := startSession(t, 90, 14)
	a.waitForScreen(t, "the tab bar", func(string) bool {
		return strings.Contains(a.lines()[0], "+")
	})

	a.send(t, "printf TAB-ONE\n")
	a.waitForScreen(t, "the marker", func(s string) bool {
		return strings.Contains(s, "TAB-ONE")
	})
	a.send(t, "\x02c")
	// Waiting only for the marker to go is satisfied by a redraw that has
	// momentarily blanked the screen, and the bar is not there yet. What this
	// test needs is two tabs drawn, so that is what it waits for.
	a.waitForScreen(t, "a second tab", func(string) bool {
		return strings.Count(a.lines()[0], "tab ") == 2 &&
			!strings.Contains(a.text(), "TAB-ONE")
	})

	first := columnOf(a.lines()[0], 't')
	if first < 0 {
		t.Fatalf("no tab label in %q", a.lines()[0])
	}
	a.clickAt(t, first+1, 1)

	a.waitForScreen(t, "the first tab", func(s string) bool {
		return strings.Contains(s, "TAB-ONE")
	})
}

// TestAttachClickSidebarHeading: a line that looks clickable and is not is
// worse than one that is not drawn, so the group headings go somewhere too.
func TestAttachClickSidebarHeading(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "printf FIRST-SPACE\n")
	a.waitForScreen(t, "the marker", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE")
	})
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})
	a.waitForScreen(t, "the space list", func(s string) bool {
		return strings.Contains(s, "main")
	})

	row := -1
	for i, line := range a.lines() {
		// A space with no agent in it is marked "·", herdr's mark for
		// nothing known; the one in view is the band, not the mark.
		if strings.Contains(line, "· main") {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("no heading for the first space:\n%s", a.text())
	}
	a.clickAt(t, 2, row+1)

	a.waitForScreen(t, "the first space", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE")
	})
}

// fakeAgentBin writes an executable with the given name. Linux takes a
// process's name from the file that was executed, so a script is enough.
func fakeAgentBin(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// lineContaining returns the screen row holding a substring, counted from one
// as the mouse reports rows.
func (a *attached) lineContaining(t *testing.T, want string) int {
	t.Helper()
	for i, line := range a.lines() {
		if strings.Contains(line, want) {
			return i + 1
		}
	}
	t.Fatalf("no line containing %q:\n%s", want, a.text())
	return 0
}

// TestAttachSidebarIsShownByDefault: the list of what needs attention is the
// reason to run tend, and a list behind a keystroke is one nobody presses.
func TestAttachSidebarIsShownByDefault(t *testing.T) {
	a := startSession(t, 90, 14)
	a.waitForScreen(t, "the sidebar", func(s string) bool {
		return strings.Contains(s, "spaces") && strings.Contains(s, "agents")
	})
	if a.paneStartsAtLeftEdge() {
		t.Errorf("the sidebar should own the leftmost columns:\n%s", a.text())
	}
}

// TestAttachSidebarShowsTheBranch covers what tells two spaces on the same
// repository apart.
//
// In a repository of its own, on a branch of its own. It used to run in the
// test's checkout and look for "master" or "main", so it failed in every
// worktree on an issue branch — which is where tend makes the work happen.
func TestAttachSidebarShowsTheBranch(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{{"init", "-q", "-b", "side-branch"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	a := startSessionIn(t, 90, 14, repo)
	a.waitForScreen(t, "the branch", func(s string) bool {
		return strings.Contains(s, "side-branch")
	})
}

// TestAttachTogglesGroupedByClicking: the toggle sits in the heading, and
// clicking it is the only way to reach it with the mouse.
func TestAttachTogglesGroupedByClicking(t *testing.T) {
	a := startSession(t, 90, 14)
	a.waitForScreen(t, "the agents heading", func(s string) bool {
		return strings.Contains(s, "flat")
	})

	a.clickAt(t, ui.SidebarWidth-4, a.lineContaining(t, "agents"))
	a.waitForScreen(t, "the list to group", func(s string) bool {
		return strings.Contains(s, "grouped")
	})

	a.clickAt(t, ui.SidebarWidth-4, a.lineContaining(t, "agents"))
	a.waitForScreen(t, "the list to flatten", func(s string) bool {
		return strings.Contains(s, "flat")
	})
}

// TestAttachClickNewSpace: the row says "new", so clicking it has to make one.
func TestAttachClickNewSpace(t *testing.T) {
	a := startSession(t, 90, 14)
	a.waitForScreen(t, "the new row", func(s string) bool {
		return strings.Contains(s, "new")
	})

	a.clickAt(t, 2, a.lineContaining(t, "new"))
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})
}

// sidebarText is only the sidebar's columns, so an assertion about the list
// cannot be satisfied by whatever a pane happens to be showing.
//
// The status bar is left out with them: it spans the full width, so its own
// mention of the current space sits in the sidebar's columns and would answer
// for the list.
func (a *attached) sidebarText() string {
	var b strings.Builder
	lines := a.lines()
	if len(lines) > ui.StatusRows {
		lines = lines[:len(lines)-ui.StatusRows]
	}
	for _, line := range lines {
		r := []rune(line)
		if len(r) > ui.SidebarWidth {
			r = r[:ui.SidebarWidth]
		}
		b.WriteString(strings.TrimRight(string(r), " "))
		b.WriteString("\n")
	}
	return b.String()
}

// openMenuOn right-clicks a point until the menu it should open is there.
//
// A right-click lands wherever the screen happens to be at that instant, and
// after a reconnect or a redraw that can be before the row it is aiming at has
// been drawn. Insisting is what a person does; waiting a fixed time and hoping
// is what makes a test flake.
func (a *attached) openMenuOn(t *testing.T, col, row int, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.rightClickAt(t, col, row)
		for i := 0; i < 6; i++ {
			if strings.Contains(a.text(), want) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Fatalf("no menu with %q after right-clicking %d,%d:\n%s", want, col, row, a.text())
}

// rightClickAt opens a context menu where a right-click would.
func (a *attached) rightClickAt(t *testing.T, col, row int) {
	t.Helper()
	seq := "\x1b[<2;" + itoa(col) + ";" + itoa(row) + "M" +
		"\x1b[<2;" + itoa(col) + ";" + itoa(row) + "m"
	a.send(t, seq)
	time.Sleep(150 * time.Millisecond)
}

// TestAttachListsAnAgentStartedInAShell is the bug this was found by: almost
// nobody opens a pane by naming an agent, they open a shell and type its name.
// The agent list stayed empty because a pane's agent lives in the session and
// the client only re-read the session when the shape of it changed.
func TestAttachListsAnAgentStartedInAShell(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, fakeAgentBin(t, "claude", "sleep 30")+"\n")
	a.waitForScreen(t, "the agent to be listed", func(string) bool {
		return strings.Contains(a.sidebarText(), "claude")
	})
}

// TestAttachMenuClosesAPane covers the menu end to end: it is opened on the
// thing it acts on, and what it says it will do is what happens.
func TestAttachMenuClosesAPane(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})

	a.openMenuOn(t, 50, 6, "close pane")

	a.clickAt(t, 52, a.lineContaining(t, "close pane"))
	a.waitForScreen(t, "the pane to close", func(s string) bool {
		return strings.Count(s, "┌") == 1 && !strings.Contains(s, "close pane")
	})
}

// TestAttachMenuClosesASpace: closing a space has to take its panes with it
// and leave the client somewhere real.
func TestAttachMenuClosesASpace(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})

	a.openMenuOn(t, 6, a.lineContaining(t, "space 2"), "close space")
	a.clickAt(t, 8, a.lineContaining(t, "close space"))

	a.waitForScreen(t, "the space to go", func(string) bool {
		return !strings.Contains(a.sidebarText(), "space 2")
	})
	// What is left is still a usable session rather than a blank screen.
	a.waitForScreen(t, "a pane to remain", func(s string) bool {
		return strings.Contains(s, "┌")
	})
}

// TestAttachMenuButtonAndEscape: the sidebar's "menu" button opens herdr's
// global menu — settings, keybinds, reload config, detach — and escape
// leaves it without doing anything. The space's own menu is on a
// right-click on the space. If it regresses, the menu that holds the
// release notes cannot be reached, or escaping it acts.
func TestAttachMenuButtonAndEscape(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "the new row", func(s string) bool {
		return strings.Contains(s, "menu")
	})

	row := a.lineContaining(t, "new")
	a.clickAt(t, ui.SidebarWidth-3, row)
	a.waitForScreen(t, "the global menu", func(s string) bool {
		return strings.Contains(s, "reload config") && strings.Contains(s, "keybinds") && !strings.Contains(s, "close space")
	})

	a.send(t, "\x1b")
	a.waitForScreen(t, "the menu to close", func(s string) bool {
		return !strings.Contains(s, "reload config")
	})
	// Escaping is not a decision: the space is still there.
	if !strings.Contains(a.sidebarText(), "main") {
		t.Errorf("escaping should change nothing:\n%s", a.sidebarText())
	}
}

// groupSpace puts the space named in the sidebar into a group, through the
// menu, the way a user would.
func (a *attached) groupSpace(t *testing.T, space, group string) {
	t.Helper()
	a.openMenuOn(t, 6, a.lineContaining(t, space), "new group...")
	a.clickAt(t, 8, a.lineContaining(t, "new group..."))
	a.waitForScreen(t, "the group prompt", func(s string) bool {
		return strings.Contains(s, "new group — name")
	})
	a.send(t, group+"\r")
	time.Sleep(300 * time.Millisecond)
}

// TestAttachGroupsSpacesIntoATree covers the sidebar's shape: spaces kept
// together read as a tree, and folding one hides its members without hiding
// that they are there.
func TestAttachGroupsSpacesIntoATree(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})

	a.groupSpace(t, "space 2", "clients")
	a.waitForScreen(t, "the group", func(string) bool {
		return strings.Contains(a.sidebarText(), "▼ clients")
	})
	// The space is still listed, under its heading.
	if !strings.Contains(a.sidebarText(), "space 2") {
		t.Errorf("the member should still be listed:\n%s", a.sidebarText())
	}

	a.clickAt(t, 3, a.lineContaining(t, "clients"))
	a.waitForScreen(t, "the group to fold", func(string) bool {
		side := a.sidebarText()
		return strings.Contains(side, "▶ clients") && !strings.Contains(side, "space 2")
	})

	a.clickAt(t, 3, a.lineContaining(t, "clients"))
	a.waitForScreen(t, "the group to open", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 2")
	})
}

// TestAttachUngroupsFromTheGroupMenu: ungrouping keeps the spaces and drops
// only the heading.
func TestAttachUngroupsFromTheGroupMenu(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})
	a.groupSpace(t, "space 2", "clients")
	a.waitForScreen(t, "the group", func(string) bool {
		return strings.Contains(a.sidebarText(), "clients")
	})

	a.openMenuOn(t, 4, a.lineContaining(t, "clients"), "ungroup")
	a.clickAt(t, 6, a.lineContaining(t, "ungroup"))

	a.waitForScreen(t, "the group to go", func(string) bool {
		side := a.sidebarText()
		return !strings.Contains(side, "clients") && strings.Contains(side, "space 2")
	})
}

// TestASpaceMovesIntoAGroupPickedFromTheMenu: "move to group..." lists the
// groups there are, and picking one puts the space in it. If it regresses,
// joining a group means typing its exact name again.
func TestASpaceMovesIntoAGroupPickedFromTheMenu(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(string) bool { return strings.Contains(a.sidebarText(), "space 2") })
	a.groupSpace(t, "space 2", "clients")
	a.waitForScreen(t, "the group", func(string) bool { return strings.Contains(a.sidebarText(), "▼ clients") })

	a.openMenuOn(t, 6, a.lineContaining(t, "main"), "move to group...")
	a.clickAt(t, 8, a.lineContaining(t, "move to group..."))
	a.waitForScreen(t, "the list of groups", func(s string) bool { return strings.Contains(s, "new group...") })
	// The menu's item, not the sidebar's heading, which has the fold marker.
	item := 0
	for i, line := range a.lines() {
		if strings.Contains(line, "clients") && !strings.Contains(line, "▼ clients") {
			item = i + 1
		}
	}
	if item == 0 {
		t.Fatalf("clients is not offered:\n%s", a.text())
	}
	col := strings.Index(string([]rune(a.lines()[item-1])), "clients")
	a.clickAt(t, len([]rune(a.lines()[item-1][:col]))+2, item)
	a.waitForScreen(t, "main under clients", func(string) bool {
		side := a.sidebarText()
		return strings.Index(side, "▼ clients") >= 0 && strings.Index(side, "▼ clients") < strings.Index(side, "main")
	})
}

// TestASpaceDroppedOnAGroupJoinsIt: dragging a space onto a group's heading
// puts it in that group. If it regresses, the only way into a group is the
// menu.
func TestASpaceDroppedOnAGroupJoinsIt(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(string) bool { return strings.Contains(a.sidebarText(), "space 2") })
	a.groupSpace(t, "space 2", "clients")
	a.waitForScreen(t, "the group", func(string) bool { return strings.Contains(a.sidebarText(), "▼ clients") })

	a.dragFromTo(t, 0, 6, a.lineContaining(t, "main"), 4, a.lineContaining(t, "clients"))
	a.waitForScreen(t, "main under clients", func(string) bool {
		side := a.sidebarText()
		return strings.Index(side, "▼ clients") >= 0 && strings.Index(side, "▼ clients") < strings.Index(side, "main")
	})
}

// TestAttachNavigatesIntoAFoldedGroup: whatever the mouse can reach, the
// keyboard can. A folded group would otherwise be a dead end.
func TestAttachNavigatesIntoAFoldedGroup(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf FIRST-SPACE\n", "the marker", func(s string) bool {
		return strings.Contains(s, "FIRST-SPACE")
	})
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(s string) bool {
		return strings.Contains(s, "space 2")
	})
	a.groupSpace(t, "space 2", "clients")
	a.clickAt(t, 3, a.lineContaining(t, "clients"))
	a.waitForScreen(t, "the group to fold", func(string) bool {
		return strings.Contains(a.sidebarText(), "▶ clients")
	})

	// Walk to the heading and open it from the keyboard.
	a.send(t, "\x02w")
	a.waitForScreen(t, "navigate mode", func(s string) bool {
		return strings.Contains(s, "NAVIGATE")
	})
	a.send(t, "j\r")
	a.waitForScreen(t, "the group to open", func(string) bool {
		return strings.Contains(a.sidebarText(), "▼ clients")
	})
}

// wheelAt sends one notch of the wheel at a point, counted from one.
func (a *attached) wheelAt(t *testing.T, col, row int, up bool) {
	t.Helper()
	button := "65"
	if up {
		button = "64"
	}
	a.send(t, "\x1b[<"+button+";"+itoa(col)+";"+itoa(row)+"M")
	time.Sleep(120 * time.Millisecond)
}

// TestAttachScrollsTheSidebar is the bug this was found by: with enough
// spaces the list filled the column and everything under it — the button that
// makes a space, the whole agents section — was simply gone.
func TestAttachScrollsTheSidebar(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	for i := 0; i < 8; i++ {
		a.send(t, "\x02N")
		time.Sleep(150 * time.Millisecond)
	}
	a.waitForScreen(t, "a long list", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 9")
	})
	if strings.Contains(a.sidebarText(), "agents") {
		t.Skipf("the window is tall enough to show everything:\n%s", a.sidebarText())
	}

	for i := 0; i < 15; i++ {
		a.wheelAt(t, 5, 6, false)
		if strings.Contains(a.sidebarText(), "agents") {
			break
		}
	}
	if !strings.Contains(a.sidebarText(), "agents") {
		t.Fatalf("scrolling down should reach the agents section:\n%s", a.sidebarText())
	}

	for i := 0; i < 20; i++ {
		a.wheelAt(t, 5, 6, true)
		if strings.Contains(a.sidebarText(), "spaces") {
			break
		}
	}
	if !strings.Contains(a.sidebarText(), "spaces") {
		t.Errorf("scrolling back up should reach the top:\n%s", a.sidebarText())
	}
}

// TestAttachClosingTheLastSpaceLeavesAnEmptySession: closing the last space
// used to hand back a fresh one immediately, which from the outside is
// indistinguishable from the close having failed.
func TestAttachClosingTheLastSpaceLeavesAnEmptySession(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.openMenuOn(t, 6, a.lineContaining(t, "main"), "close space")
	a.clickAt(t, 8, a.lineContaining(t, "close space"))

	a.waitForScreen(t, "the session to empty", func(s string) bool {
		return !strings.Contains(s, "┌") && !strings.Contains(a.sidebarText(), "main")
	})
	// And it is not a dead end: the button that makes one is still there.
	a.clickAt(t, 2, a.lineContaining(t, "new"))
	a.waitForScreen(t, "a space again", func(s string) bool {
		return strings.Contains(s, "┌")
	})
}

// TestAttachWheelReachesAProgramThatAskedForIt: an agent has its own idea of
// what is above the screen, and showing tend's scrollback instead means the
// wheel does nothing the user recognises.
func TestAttachWheelReachesAProgramThatAskedForIt(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// A program that asks for mouse reporting and prints what it is sent.
	mouser := fakeAgentBin(t, "mouser", "printf '\\033[?1002h\\033[?1006h'; cat")
	a.sendUntil(t, mouser+"\n", "the program to start", func(s string) bool {
		return strings.Contains(s, "mouser")
	})
	time.Sleep(400 * time.Millisecond)

	a.wheelAt(t, 50, 6, true)
	a.waitForScreen(t, "the wheel to reach the program", func(s string) bool {
		return strings.Contains(s, "[<64;")
	})
	// And tend did not take it for itself.
	if strings.Contains(a.text(), "scroll ") {
		t.Errorf("tend should not have entered its own scroll view:\n%s", a.text())
	}
}

// TestAttachOffersToRestartAnOlderServer: the server outlives the client, so
// upgrading tend and reattaching leaves the old one running. A notice naming a
// command to run in another terminal is one step further than most people go
// while something is already in front of them, so the restart is offered where
// the problem is.
func TestAttachOffersToRestartAnOlderServer(t *testing.T) {
	a := startSessionOlder(t, 100, 20, "0.0.1-ancient")
	a.waitForScreen(t, "the notice", func(s string) bool {
		return strings.Contains(s, "older server") && strings.Contains(s, "0.0.1-ancient")
	})
	// The command for elsewhere has to be the one that works: without
	// -server, "tend kill -s <name>" closes panes by number and leaves the
	// server where it was.
	if !strings.Contains(a.text(), "tend kill -s tui -server") {
		t.Errorf("the notice must show the whole command:\n%s", a.text())
	}

	a.send(t, "r")
	a.waitForScreen(t, "the new server", func(s string) bool {
		return strings.Contains(s, "reconnected")
	})

	// And the thing that could not be done before can be done now, which is
	// the only proof that the replacement is this binary.
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.openMenuOn(t, 6, a.lineContaining(t, "main"), "close space")
	a.clickAt(t, 8, a.lineContaining(t, "close space"))
	a.waitForScreen(t, "the space to close", func(s string) bool {
		return !strings.Contains(a.sidebarText(), "main")
	})
}

// TestAttachKeepsAnOlderServerWhenAsked: the notice is a question, and the
// answer "carry on" has to leave a working session.
func TestAttachKeepsAnOlderServerWhenAsked(t *testing.T) {
	a := startSessionOlder(t, 100, 20, "0.0.1-ancient")
	a.waitForScreen(t, "the notice", func(s string) bool {
		return strings.Contains(s, "older server")
	})

	a.send(t, "\r")
	a.waitForScreen(t, "the notice to go", func(s string) bool {
		return !strings.Contains(s, "older server")
	})
	a.sendUntil(t, "printf STILL-HERE\n", "the pane to work", func(s string) bool {
		return strings.Contains(s, "STILL-HERE")
	})

	// The keystroke that answered the notice is not also typed into the pane
	// behind it: it was aimed at something the user could not see.
	if strings.Count(a.text(), "STILL-HERE") > 2 {
		t.Errorf("the pane got more than it was sent:\n%s", a.text())
	}
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool {
		return strings.Count(s, "┌") == 2
	})
}

// dividerRow is the screen row the sidebar's divider is drawn on, counted
// from zero, or -1.
func (a *attached) sidebarDividerRow() int {
	for i, line := range a.lines() {
		if strings.HasPrefix(line, "──") {
			return i
		}
	}
	return -1
}

// TestAttachDividesTheSidebar: the two lists answer different questions, and
// one sharing the other's space meant a long list of spaces hid every agent.
func TestAttachDividesTheSidebar(t *testing.T) {
	a := startSession(t, 100, 22)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	for i := 0; i < 6; i++ {
		a.send(t, "\x02N")
		time.Sleep(150 * time.Millisecond)
	}
	a.waitForScreen(t, "a long list of spaces", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 7")
	})

	// However many spaces there are, the agents section keeps its own room.
	side := a.sidebarText()
	if !strings.Contains(side, "spaces") || !strings.Contains(side, "agents") {
		t.Errorf("both sections should be on screen:\n%s", side)
	}
	if a.sidebarDividerRow() < 0 {
		t.Errorf("the lists should be divided by a rule:\n%s", side)
	}
}

// TestAttachDragsTheSidebarDivider: a press on the line is a grab, and the
// line follows the pointer.
func TestAttachDragsTheSidebarDivider(t *testing.T) {
	a := startSession(t, 100, 22)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	for i := 0; i < 5; i++ {
		a.send(t, "\x02N")
		time.Sleep(150 * time.Millisecond)
	}
	a.waitForScreen(t, "a divider", func(string) bool { return a.sidebarDividerRow() > 0 })

	from := a.sidebarDividerRow()
	to := from - 3
	if to < 3 {
		t.Skipf("no room to drag: divider at %d", from)
	}
	// Press on the line, move, release — the same gesture as resizing a split.
	a.send(t, "\x1b[<0;5;"+itoa(from+1)+"M")
	time.Sleep(120 * time.Millisecond)
	a.send(t, "\x1b[<32;5;"+itoa(to+1)+"M")
	time.Sleep(200 * time.Millisecond)
	a.send(t, "\x1b[<0;5;"+itoa(to+1)+"m")

	a.waitForScreen(t, "the divider to move", func(string) bool {
		return a.sidebarDividerRow() == to
	})
	// Both lists survive the move: the clamp leaves room either side.
	side := a.sidebarText()
	if !strings.Contains(side, "spaces") || !strings.Contains(side, "agents") {
		t.Errorf("dragging must not squeeze a list out:\n%s", side)
	}
}

// TestAttachScrollsTheSpacesByTheirScrollbar: a list with more spaces than
// fit has herdr's scrollbar; a press at the bottom of its track shows the
// last space, and the thumb dragged back to the top shows the first, main. If it
// regresses, a long list can only be scrolled with a wheel, which a trackpad
// or a terminal that does not report one cannot give.
func TestAttachScrollsTheSpacesByTheirScrollbar(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	for i := 0; i < 8; i++ {
		a.send(t, "\x02N")
		time.Sleep(150 * time.Millisecond)
	}
	a.waitForScreen(t, "nine spaces", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 9")
	})
	// A new space is shown when it is made, so the list is scrolled to its
	// end now; the wheel takes it back to the top first.
	for i := 0; i < 6; i++ {
		a.send(t, "\x1b[<64;5;4M")
	}
	a.waitForScreen(t, "the top of the list", func(string) bool {
		side := a.sidebarText()
		return strings.Contains(side, "main")
	})

	// The track is the column of ▕ beside the entries, and the last line of
	// it is where a press scrolls to the end.
	track := func() (col, top, bottom int) {
		col, top, bottom = -1, -1, -1
		for y, line := range a.lines() {
			r := []rune(line)
			for x := 0; x < min(len(r), ui.SidebarWidth); x++ {
				if r[x] == '▕' || r[x] == '▐' {
					if top < 0 {
						top = y
					}
					col, bottom = x, y
				}
			}
		}
		return col, top, bottom
	}
	col, top, bottom := track()
	if col < 0 {
		t.Fatalf("an overflowing list should have a scrollbar:\n%s", a.sidebarText())
	}
	a.clickAt(t, col+1, bottom+1)
	a.waitForScreen(t, "the end of the list", func(string) bool {
		side := a.sidebarText()
		return strings.Contains(side, "space 9") && !strings.Contains(side, "main")
	})

	// The thumb is at the bottom now: held there and dragged to the top of
	// the track, it takes the list back to its start.
	a.send(t, "\x1b[<0;"+itoa(col+1)+";"+itoa(bottom+1)+"M")
	time.Sleep(120 * time.Millisecond)
	a.send(t, "\x1b[<32;"+itoa(col+1)+";"+itoa(top+1)+"M")
	time.Sleep(200 * time.Millisecond)
	a.send(t, "\x1b[<0;"+itoa(col+1)+";"+itoa(top+1)+"m")
	a.waitForScreen(t, "the start of the list", func(string) bool {
		side := a.sidebarText()
		return strings.Contains(side, "main") && !strings.Contains(side, "space 9")
	})
}

// TestAttachRenamesInAModal: the status bar is where tend says things, not
// where the user says them. A field down there competes with the session name
// for one row and puts what is being typed furthest from the eye.
func TestAttachRenamesInAModal(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02,")
	a.waitForScreen(t, "the field", func(s string) bool {
		return strings.Contains(s, "rename tab") && strings.Contains(s, "enter · esc")
	})

	// It is a box over the middle, not a line at the bottom.
	row := a.lineContaining(t, "rename tab") - 1
	if row < 2 || row > 14 {
		t.Errorf("the field should be over the middle, found at row %d:\n%s", row, a.text())
	}
	if last := a.lines()[len(a.lines())-1]; strings.Contains(last, "rename tab") {
		t.Errorf("the field should not be on the status line:\n%q", last)
	}

	a.send(t, "backend\r")
	a.waitForScreen(t, "the new name", func(s string) bool {
		return strings.Contains(s, "backend") && !strings.Contains(s, "enter · esc")
	})
}

// TestWaitingCountsBlockedAgentsAnywhere: anywhere, not here — the reason to
// run tend is that the one needing you is usually not the one on screen.
func TestWaitingCountsBlockedAgentsAnywhere(t *testing.T) {
	tt := &tui{}
	tt.snap = proto.SessionSnapshot{Panes: []proto.PaneInfo{
		{ID: 1, Agent: "claude", Running: true, State: "blocked"},
		{ID: 2, Agent: "codex", Running: true, State: "working"},
		{ID: 3, Agent: "claude", Running: true, State: "blocked"},
		// A shell at a prompt is not waiting for anybody.
		{ID: 4, Running: true, State: "blocked"},
		// Nor is an agent whose process has ended.
		{ID: 5, Agent: "claude", Running: false, State: "blocked"},
	}}
	if got := tt.waitingLocked(); got != 2 {
		t.Errorf("waiting = %d, want 2", got)
	}

	tt.snap = proto.SessionSnapshot{}
	if got := tt.waitingLocked(); got != 0 {
		t.Errorf("an empty session has nothing waiting, got %d", got)
	}
}

// TestAttachHidesTheSidebarFromItsHandle: found the sidebar with the mouse,
// dismiss it with the mouse.
func TestAttachHidesTheSidebarFromItsHandle(t *testing.T) {
	a := startSession(t, 100, 18)
	a.waitForScreen(t, "the sidebar", func(s string) bool {
		return strings.Contains(s, "spaces") && strings.Contains(s, "«")
	})

	handle := -1
	for i, line := range a.lines() {
		if strings.Contains(line, "«") {
			handle = i
		}
	}
	if handle < 0 {
		t.Fatalf("no handle:\n%s", a.text())
	}
	a.clickAt(t, ui.SidebarWidth-1, handle+1)

	a.waitForScreen(t, "the sidebar to go", func(s string) bool {
		return !strings.Contains(s, "spaces") && strings.Contains(s, "»")
	})
	// The panes get the columns back, bar the gutter the handle needs.
	a.waitForScreen(t, "the panes to widen", func(string) bool {
		for _, line := range a.lines() {
			if at := columnOf(line, '┌'); at >= 0 && at < ui.SidebarWidth/2 {
				return true
			}
		}
		return false
	})

	a.clickAt(t, 1, 1)
	a.waitForScreen(t, "the sidebar to come back", func(s string) bool {
		return strings.Contains(s, "spaces")
	})
}

// TestAttachPinsTheSpaceButtonsAboveTheDivider: the buttons act on the spaces
// list, so they sit at its edge rather than floating after its last entry,
// where every new space would move them.
func TestAttachPinsTheSpaceButtonsAboveTheDivider(t *testing.T) {
	a := startSession(t, 100, 24)
	a.waitForScreen(t, "the sidebar", func(s string) bool {
		return strings.Contains(s, "new") && strings.Contains(s, "menu")
	})

	where := func() (buttons, divider int) {
		return a.lineContaining(t, "new") - 1, a.sidebarDividerRow()
	}
	buttons, divider := where()
	if divider < 0 || buttons != divider-1 {
		t.Fatalf("buttons at %d, divider at %d: they should touch:\n%s", buttons, divider, a.sidebarText())
	}

	// Adding spaces does not move them.
	for i := 0; i < 4; i++ {
		a.send(t, "\x02N")
		time.Sleep(150 * time.Millisecond)
	}
	a.waitForScreen(t, "more spaces", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 5")
	})
	if moved, _ := where(); moved != buttons {
		t.Errorf("the buttons moved from %d to %d when the list grew:\n%s", buttons, moved, a.sidebarText())
	}

	// And they still work where they are.
	a.clickAt(t, 2, buttons+1)
	a.waitForScreen(t, "a new space", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 6")
	})
}

// TestAttachShowsHowFarASpaceHasDrifted: the branch alone does not say whether
// there is anything to push, which is most of what a checkout's state means.
func TestAttachShowsHowFarASpaceHasDrifted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin")
	clone := filepath.Join(dir, "clone")
	git := func(at string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = at
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(dir, "init", "-q", origin)
	git(origin, "commit", "-q", "--allow-empty", "-m", "one")
	git(dir, "clone", "-q", origin, clone)
	git(clone, "commit", "-q", "--allow-empty", "-m", "two")

	// The server roots a workspace at its own directory, so it is started in
	// the clone.
	a := startSessionIn(t, 100, 20, clone)
	a.waitForScreen(t, "the drift", func(string) bool {
		return strings.Contains(a.sidebarText(), "↑1")
	})
}

// moveTo sends a pointer movement with no button held, which arrives only
// while motion reporting is on.
func (a *attached) moveTo(t *testing.T, col, row int) {
	t.Helper()
	a.send(t, "\x1b[<35;"+itoa(col)+";"+itoa(row)+"M")
	time.Sleep(150 * time.Millisecond)
}

// reversedOn returns the text drawn reversed on a screen row, which is how a
// marked menu item shows.
func (a *attached) reversedOn(row int) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	line := a.screen.Grid().Line(row)
	if line == nil {
		return ""
	}
	var b strings.Builder
	for x := 0; x < line.Len(); x++ {
		if c := line.Cell(x); c.Style.Has(vt.AttrReverse) && c.Width > 0 {
			r := c.R
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// TestAttachMarksTheMenuItemUnderThePointer: what the pointer is on has to be
// obvious before the click, not after it.
func TestAttachMarksTheMenuItemUnderThePointer(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.openMenuOn(t, 50, 6, "zoom")

	// Nothing is marked to begin with: the pointer is on the thing the menu
	// was opened on, not on a choice.
	zoom := a.lineContaining(t, "zoom")
	if got := a.reversedOn(zoom - 1); got != "" {
		t.Errorf("nothing should be marked before the pointer moves, got %q", got)
	}

	a.moveTo(t, 52, zoom)
	a.waitForScreen(t, "zoom to be marked", func(string) bool {
		return a.reversedOn(zoom-1) == "zoom"
	})

	down := a.lineContaining(t, "split down")
	a.moveTo(t, 52, down)
	a.waitForScreen(t, "the mark to follow", func(string) bool {
		return a.reversedOn(down-1) == "split down" && a.reversedOn(zoom-1) == ""
	})

	// Off the menu, nothing is marked: there is nothing there to click.
	a.moveTo(t, 90, 3)
	a.waitForScreen(t, "the mark to go", func(string) bool {
		return a.reversedOn(down-1) == ""
	})

	// And with nothing marked, choosing closes rather than running whichever
	// item happens to be first.
	a.send(t, "\r")
	a.waitForScreen(t, "the menu to close", func(s string) bool {
		return !strings.Contains(s, "split right")
	})
	if strings.Count(a.text(), "┌") != 1 {
		t.Errorf("no item was marked, so nothing should have run:\n%s", a.text())
	}
}

// dragFromTo presses at one point, drags to another and releases, with the
// given modifier bits folded into the button number.
func (a *attached) dragFromTo(t *testing.T, mods, fromCol, fromRow, toCol, toRow int) {
	t.Helper()
	a.send(t, "\x1b[<"+itoa(mods)+";"+itoa(fromCol)+";"+itoa(fromRow)+"M")
	time.Sleep(120 * time.Millisecond)
	a.send(t, "\x1b[<"+itoa(32+mods)+";"+itoa(toCol)+";"+itoa(toRow)+"M")
	time.Sleep(150 * time.Millisecond)
	a.send(t, "\x1b[<"+itoa(mods)+";"+itoa(toCol)+";"+itoa(toRow)+"m")
	time.Sleep(200 * time.Millisecond)
}

// writeLines puts known text in the focused pane and returns the screen row
// and column its first line starts at, counted from one.
func (a *attached) writeLines(t *testing.T, first, second string) (row, col int) {
	t.Helper()
	a.sendUntil(t, "printf '"+first+"\\n"+second+"\\n'\n", "the text", func(s string) bool {
		return strings.Contains(s, second)
	})
	time.Sleep(250 * time.Millisecond)
	row = a.lineContaining(t, first)
	return row, columnOfString(a.lines()[row-1], first) + 1
}

// TestAttachSelectsTextWithTheMouse is the thing a multiplexer takes away:
// asking the terminal for mouse reporting is what makes panes clickable and
// what stops the terminal doing its own selection.
func TestAttachSelectsTextWithTheMouse(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	row, col := a.writeLines(t, "alpha bravo charlie", "delta echo foxtrot")

	a.dragFromTo(t, 0, col, row, col+10, row)
	a.waitForScreen(t, "the mark", func(string) bool {
		return strings.Contains(a.reversedOn(row-1), "alpha bravo")
	})
	a.waitForScreen(t, "the copy", func(s string) bool {
		return strings.Contains(s, "copied 11 characters")
	})

	// Across two lines it counts lines, not characters.
	a.dragFromTo(t, 0, col, row, col+11, row+1)
	a.waitForScreen(t, "two lines copied", func(s string) bool {
		return strings.Contains(s, "copied 2 lines")
	})

	// A click is not a selection: a stray one must not leave a mark behind.
	a.clickAt(t, col+3, row)
	a.waitForScreen(t, "the mark to go", func(string) bool {
		return !strings.Contains(a.reversedOn(row-1), "alpha")
	})
}

// TestAttachSelectsARectangle: an agent draws its own panels, so a run
// spanning three lines takes the whole width of the middle one and everything
// beside it. Alt takes the columns instead, which is the convention every
// terminal already uses for this.
func TestAttachSelectsARectangle(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'AAA  ppp\\nBBB  qqq\\nCCC  rrr\\n'\n", "the text", func(s string) bool {
		return strings.Contains(s, "CCC  rrr")
	})
	time.Sleep(300 * time.Millisecond)

	// The last row holding it, so the command that produced it is skipped.
	bottom := 0
	for i, line := range a.lines() {
		if strings.Contains(line, "CCC  rrr") {
			bottom = i + 1
		}
	}
	if bottom == 0 {
		t.Fatalf("no output row:\n%s", a.text())
	}
	top := bottom - 2
	col := columnOfString(a.lines()[top-1], "AAA") + 1

	// A run takes the whole of the middle line.
	a.dragFromTo(t, 0, col, top, col+2, bottom)
	a.waitForScreen(t, "the run", func(string) bool {
		return strings.TrimSpace(a.reversedOn(top)) == "BBB  qqq"
	})

	// A rectangle takes the columns dragged over, on every line.
	const alt = 8
	a.dragFromTo(t, alt, col, top, col+2, bottom)
	a.waitForScreen(t, "the rectangle", func(string) bool {
		return strings.TrimSpace(a.reversedOn(top)) == "BBB"
	})
	for i, want := range []string{"AAA", "BBB", "CCC"} {
		if got := strings.TrimSpace(a.reversedOn(top - 1 + i)); got != want {
			t.Errorf("row %d marked %q, want %q", i, got, want)
		}
	}
	a.waitForScreen(t, "the copy to say so", func(s string) bool {
		return strings.Contains(s, "(block)")
	})
}

// TestAttachScrollsWhileSelecting is the reason the view follows the pointer:
// a selection that stops at the edge of the screen can only ever take what
// happens to be on it.
func TestAttachScrollsWhileSelecting(t *testing.T) {
	const rows = 14
	a := startSession(t, 100, rows)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "for i in $(seq 1 40); do echo LINE-$i; done\n", "the output", func(s string) bool {
		return strings.Contains(s, "LINE-40")
	})
	time.Sleep(300 * time.Millisecond)

	row := a.lineContaining(t, "LINE-40")
	col := columnOfString(a.lines()[row-1], "LINE-40") + 1

	// Press on the last line, drag to the top edge, and hold there. Reports
	// stop arriving once the pointer stops moving, so what happens next is
	// the draw loop's doing.
	a.send(t, "\x1b[<0;"+itoa(col+6)+";"+itoa(row)+"M")
	time.Sleep(120 * time.Millisecond)
	a.send(t, "\x1b[<32;"+itoa(col)+";2M")

	a.waitForScreen(t, "the view to follow", func(s string) bool {
		return strings.Contains(s, "LINE-1 ") || strings.Contains(s, "LINE-2 ")
	})
	a.send(t, "\x1b[<0;"+itoa(col)+";2m")

	a.waitForScreen(t, "the copy", func(s string) bool {
		return strings.Contains(s, "copied ") && strings.Contains(s, "lines")
	})
	// More than the pane is tall, which is the whole point.
	status := a.lines()[len(a.lines())-1]
	at := strings.Index(status, "copied ")
	var n int
	for _, r := range status[at+len("copied "):] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if n <= rows {
		t.Errorf("copied %d lines from a %d-row pane; the selection should have reached past it:\n%s",
			n, rows, status)
	}
}

// TestAttachDoesNotScrollWhileSelectingInside: the view follows the pointer at
// the edge and nowhere else, or a careful drag in the middle would slide the
// text out from under it.
func TestAttachDoesNotScrollWhileSelectingInside(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "for i in $(seq 1 30); do echo LINE-$i; done\n", "the output", func(s string) bool {
		return strings.Contains(s, "LINE-30")
	})
	time.Sleep(300 * time.Millisecond)

	row := a.lineContaining(t, "LINE-30")
	col := columnOfString(a.lines()[row-1], "LINE-30") + 1
	before := a.text()

	// A drag that ends two rows short of the top.
	a.dragFromTo(t, 0, col, row, col, row-3)
	time.Sleep(600 * time.Millisecond)

	if strings.Contains(a.text(), "scroll ") {
		t.Errorf("a drag inside the pane should not have moved the view:\n%s", a.text())
	}
	if !strings.Contains(a.text(), "LINE-30") || !strings.Contains(before, "LINE-30") {
		t.Errorf("the view should be where it was:\n%s", a.text())
	}
}

// TestAttachSaysWhenTheClientIsTheOldHalf: the server is respawned by
// whichever binary is on disk, so after an upgrade the running client is
// usually the one behind. It cannot replace itself, and restarting a server
// that is already ahead would cost the panes for nothing.
func TestAttachSaysWhenTheClientIsTheOldHalf(t *testing.T) {
	// A server offering a method this client has never heard of.
	newer := append(append([]string{}, server.Methods...), "pane.teleport")
	a := startSessionWith(t, 100, 20, "9.9.9-newer", "", newer...)

	a.waitForScreen(t, "the notice", func(s string) bool {
		return strings.Contains(s, "newer than this client")
	})
	if text := a.text(); strings.Contains(text, "restart it now") {
		t.Errorf("restarting the server is not the answer here:\n%s", text)
	}
	if !strings.Contains(a.text(), "Detach and run tend again") {
		t.Errorf("the notice should say what does help:\n%s", a.text())
	}

	// "r" is not a restart here; it dismisses like any other key.
	a.send(t, "r")
	a.waitForScreen(t, "the notice to go", func(s string) bool {
		return !strings.Contains(s, "newer than this client")
	})
	a.sendUntil(t, "printf STILL-HERE\n", "the pane to work", func(s string) bool {
		return strings.Contains(s, "STILL-HERE")
	})
	if strings.Contains(a.text(), "reconnected") {
		t.Errorf("nothing should have been restarted:\n%s", a.text())
	}
}

// TestAttachSaysNothingWhenTheBuildsAgree: a notice on every attach is one
// nobody reads, and two halves that can do the same things are not a problem
// however their version strings were stamped.
func TestAttachSaysNothingWhenTheBuildsAgree(t *testing.T) {
	a := startSessionBuilt(t, 100, 20, "some-other-stamp")
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	time.Sleep(400 * time.Millisecond)
	if text := a.text(); strings.Contains(text, "older server") || strings.Contains(text, "newer than") {
		t.Errorf("differing build strings alone are not a mismatch:\n%s", text)
	}
}

// TestAttachScrollsFromTheEdgeOfTheText: the edge is the first and last line
// of text, not the border around them. A trigger one row further out is a
// single cell of border that nobody aims at, and the first version of this
// had exactly that and so never fired in ordinary use.
func TestAttachScrollsFromTheEdgeOfTheText(t *testing.T) {
	a := startSession(t, 100, 14)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "for i in $(seq 1 40); do echo LINE-$i; done\n", "the output", func(s string) bool {
		return strings.Contains(s, "LINE-40")
	})
	time.Sleep(300 * time.Millisecond)

	border := -1
	for i, line := range a.lines() {
		if strings.Contains(line, "┌") {
			border = i
			break
		}
	}
	if border < 0 {
		t.Fatalf("no pane border:\n%s", a.text())
	}
	row := a.lineContaining(t, "LINE-40")
	col := columnOfString(a.lines()[row-1], "LINE-40") + 1

	// The first row of text, one below the border, counted from one.
	top := border + 2
	a.send(t, "\x1b[<0;"+itoa(col+6)+";"+itoa(row)+"M")
	time.Sleep(120 * time.Millisecond)
	a.send(t, "\x1b[<32;"+itoa(col)+";"+itoa(top)+"M")

	a.waitForScreen(t, "the view to follow", func(s string) bool {
		return strings.Contains(s, "scroll ")
	})
	a.send(t, "\x1b[<0;"+itoa(col)+";"+itoa(top)+"m")
	a.waitForScreen(t, "the copy", func(s string) bool {
		return strings.Contains(s, "copied ")
	})
}

// TestAttachRefusesToCopyBlankness: saying "copied" for a selection of blank
// lines is worse than saying nothing, because the user pastes and finds the
// last real thing they copied replaced by empty lines.
func TestAttachRefusesToCopyBlankness(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf MARKER\n", "the marker", func(s string) bool {
		return strings.Contains(s, "MARKER")
	})
	time.Sleep(300 * time.Millisecond)

	// Empty rows well below the prompt.
	row := a.lineContaining(t, "MARKER") + 4
	a.dragFromTo(t, 0, 40, row, 60, row+2)

	a.waitForScreen(t, "the refusal", func(s string) bool {
		return strings.Contains(s, "nothing to copy")
	})
	if strings.Contains(a.text(), "copied ") {
		t.Errorf("blank lines should not be reported as copied:\n%s", a.text())
	}
}

// TestAttachDoesNotFreezeAPaneWithNoHistory is the bug behind "nothing to copy
// there": dragging to the edge of a pane with nothing to scroll back to left
// the client in the scroll view at an offset of zero. The pane looked live and
// was a still picture, so every selection after it was aimed at text that was
// no longer where the picture showed it, and the rows asked for held whatever
// the program had drawn there since — often nothing.
func TestAttachDoesNotFreezeAPaneWithNoHistory(t *testing.T) {
	a := startSession(t, 100, 14)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Like an agent: main screen, holds the mouse, wipes its scrollback and
	// redraws on its own.
	prog := fakeAgentBin(t, "redraws", `
printf '\033[?1002h\033[?1006h'
n=0
while true; do
  printf '\033[H\033[2J\033[3J'
  i=1
  while [ $i -lt 9 ]; do printf ' FRAME-%03d line %d\n' $n $i; i=$((i+1)); done
  n=$((n+1))
  sleep 0.4
done
`)
	a.send(t, prog+"\n")
	a.waitForScreen(t, "the program", func(s string) bool { return strings.Contains(s, "FRAME-") })
	time.Sleep(300 * time.Millisecond)

	border := -1
	for i, line := range a.lines() {
		if strings.Contains(line, "┌") {
			border = i
			break
		}
	}
	row := a.lineContaining(t, "line 8")

	// Drag to the top edge, hold, release — with the modifier, since a plain
	// drag in a pane that holds the mouse belongs to its program.
	a.send(t, "\x1b[<8;40;"+itoa(row)+"M")
	time.Sleep(100 * time.Millisecond)
	a.send(t, "\x1b[<40;40;"+itoa(border+2)+"M")
	time.Sleep(700 * time.Millisecond)
	a.send(t, "\x1b[<8;40;"+itoa(border+2)+"m")
	time.Sleep(300 * time.Millisecond)

	// The pane must still be showing the program as it runs.
	before := a.text()
	a.waitForScreen(t, "the pane to keep updating", func(s string) bool {
		return frameNumber(s) > frameNumber(before)
	})
	if strings.Contains(a.text(), "scroll ") {
		t.Errorf("a pane with no history should not be left in the scroll view:\n%s", a.text())
	}
}

// frameNumber reads the counter the redrawing fixture prints.
func frameNumber(screen string) int {
	at := strings.Index(screen, "FRAME-")
	if at < 0 {
		return -1
	}
	n := 0
	for _, r := range screen[at+len("FRAME-"):] {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// TestAttachGivesTheMouseToAProgramInItsOwnCoordinates: the program believes
// it has a terminal to itself whose top-left cell is 1,1. Handed the report as
// it arrived, it sees every click displaced by the sidebar and the border, and
// without the drags in between a press and a release it can select nothing.
func TestAttachGivesTheMouseToAProgramInItsOwnCoordinates(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Asks for the mouse and prints whatever it is sent.
	prog := fakeAgentBin(t, "mouser", "printf '\\033[?1002h\\033[?1006h'; printf 'READY\\n'; cat")
	a.sendUntil(t, prog+"\n", "the program", func(s string) bool {
		return strings.Contains(s, "READY")
	})
	time.Sleep(400 * time.Millisecond)

	// The pane's first cell of text, on the screen, counted from one.
	row := a.lineContaining(t, "READY")
	col := columnOfString(a.lines()[row-1], "READY") + 1

	// Press on it, drag four cells right and two down, release.
	a.dragFromTo(t, 0, col, row, col+4, row+2)

	// Where READY is in the pane's own terms: rows counted from the line
	// under the border, columns from the cell inside it.
	border := -1
	for i, line := range a.lines() {
		if strings.Contains(line, "┌") {
			border = i
			break
		}
	}
	paneRow := (row - 1) - border
	press := "[<0;1;" + itoa(paneRow) + "M"
	drag := "[<32;5;" + itoa(paneRow+2) + "M"
	release := "[<0;5;" + itoa(paneRow+2) + "m"

	a.waitForScreen(t, "the whole gesture, translated", func(s string) bool {
		return strings.Contains(s, press) && strings.Contains(s, drag) && strings.Contains(s, release)
	})
	// And tend did not select over the top of it.
	if strings.Contains(a.text(), "copied ") || strings.Contains(a.text(), "nothing to copy") {
		t.Errorf("a plain drag here belongs to the program:\n%s", a.text())
	}
}

// TestAttachCopiesWhatAProgramAsksToHaveCopied is how copying works in a pane
// that holds the mouse: the program does its own selecting, over its own
// scrollback, and hands the result over with OSC 52. Dropping that made every
// such copy fail without a word, which is what sent this client off trying to
// do the program's selecting for it.
func TestAttachCopiesWhatAProgramAsksToHaveCopied(t *testing.T) {
	if _, err := exec.LookPath("xclip"); err != nil {
		t.Skip("xclip is not installed")
	}
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// "copied by the program", as a program would send it.
	a.sendUntil(t, "printf '\\033]52;c;Y29waWVkIGJ5IHRoZSBwcm9ncmFt\\007'; echo SENT\n", "the write", func(s string) bool {
		return strings.Contains(s, "SENT")
	})
	a.waitForScreen(t, "the copy to be reported", func(s string) bool {
		return strings.Contains(s, "copied 21 characters")
	})

	out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	if err != nil {
		t.Skipf("reading the clipboard back: %v", err)
	}
	if got := string(out); got != "copied by the program" {
		t.Errorf("clipboard = %q", got)
	}
}

// TestAttachForcesItsOwnSelectionWithTheModifier: for a program that holds the
// mouse and does nothing useful with a drag, the modifier takes it back.
func TestAttachForcesItsOwnSelectionWithTheModifier(t *testing.T) {
	a := startSession(t, 100, 16)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	prog := fakeAgentBin(t, "mouser", "printf '\\033[?1002h\\033[?1006h'; printf 'alpha bravo charlie\\n'; cat")
	a.sendUntil(t, prog+"\n", "the program", func(s string) bool {
		return strings.Contains(s, "alpha bravo charlie")
	})
	time.Sleep(400 * time.Millisecond)
	row := a.lineContaining(t, "alpha bravo charlie")
	col := columnOfString(a.lines()[row-1], "alpha") + 1

	const alt = 8
	a.dragFromTo(t, alt, col, row, col+10, row)
	a.waitForScreen(t, "the copy", func(s string) bool {
		return strings.Contains(s, "copied 11 characters")
	})
	// The program heard none of it.
	if strings.Contains(a.text(), "[<") {
		t.Errorf("a forced selection should not reach the program:\n%s", a.text())
	}
}

// TestAttachNoticesAServerMissingAFeature: a change that adds no method is
// invisible in the method list. A client forwarding the mouse to a server that
// does not say which encoding the program wants, and waiting for a clipboard
// event that server has never heard of, fails without anything saying why.
func TestAttachNoticesAServerMissingAFeature(t *testing.T) {
	a := startSessionConfigured(t, 100, 20, server.Config{Build: "older", OmitFeatures: true})
	a.waitForScreen(t, "the notice", func(s string) bool {
		return strings.Contains(s, "older server") && strings.Contains(s, "restart it now")
	})
}

// TestAttachStillHandsOverTheMouseToAnOlderServer: what that server leaves out
// is read as the common case rather than as "no drags, legacy encoding", which
// would send a modern program reports it cannot parse.
func TestAttachStillHandsOverTheMouseToAnOlderServer(t *testing.T) {
	a := startSessionConfigured(t, 100, 16, server.Config{Build: version, OmitFeatures: true})
	a.waitForScreen(t, "the notice", func(s string) bool { return strings.Contains(s, "older server") })
	a.send(t, "\r") // carry on with it
	a.waitForScreen(t, "the notice to go", func(s string) bool { return !strings.Contains(s, "older server") })

	prog := fakeAgentBin(t, "mouser", "printf '\\033[?1002h\\033[?1006h'; printf 'READY\\n'; cat")
	a.sendUntil(t, prog+"\n", "the program", func(s string) bool { return strings.Contains(s, "READY") })
	time.Sleep(400 * time.Millisecond)

	row := a.lineContaining(t, "READY")
	col := columnOfString(a.lines()[row-1], "READY") + 1
	a.dragFromTo(t, 0, col, row, col+4, row)
	a.waitForScreen(t, "the drag, in the encoding the program reads", func(s string) bool {
		return strings.Contains(s, "[<32;5;")
	})
}

// TestAttachOverSSH covers a session on another machine end to end, with a
// local shell standing in for ssh: the client runs the command, the command
// runs `tend bridge` "there", and the ordinary protocol goes through it.
//
// The two sides get separate runtime directories, which is what makes it a
// test of remoteness rather than of a pipe: if the client fell back to its own
// socket the session would turn up in the wrong one.
func TestAttachOverSSH(t *testing.T) {
	bin := buildBinary(t)
	near, far := t.TempDir(), t.TempDir()
	configPath := quietConfig(t)

	// Stands in for ssh: drops the host and the word "tend", and runs the
	// rest with this build, as if on a machine with its own runtime directory.
	stand := filepath.Join(t.TempDir(), "fake-ssh")
	script := "#!/bin/sh\nshift; shift\nTEND_RUNTIME_DIR=" + far + " exec " + bin + " \"$@\"\n"
	if err := os.WriteFile(stand, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	p, err := pty.Start(bin, []string{"attach", "-ssh", "user@farhost", "-s", "far"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 16},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+near,
			"TEND_CONFIG="+configPath,
			"TEND_SSH="+stand,
			"SHELL=/bin/sh",
			"TERM=xterm-256color",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 16, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stop := exec.Command(bin, "kill", "-s", "far", "-server")
		stop.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+far)
		_ = stop.Run()
		waitForSocketGone(filepath.Join(far, "far.sock"))
	})

	a.waitForScreen(t, "a pane from the far side", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf REMOTE-OK\n", "the far shell to answer", func(s string) bool {
		return strings.Contains(s, "REMOTE-OK")
	})

	// The machine is named, because two windows showing the same session name
	// are otherwise identical and only one of them is production.
	if status := a.lines()[len(a.lines())-1]; !strings.Contains(status, "user@farhost:far") {
		t.Errorf("the status line should say where the session is: %q", status)
	}

	// And the session lives there, not here.
	if _, err := os.Stat(filepath.Join(far, "far.sock")); err != nil {
		t.Errorf("the far side should hold the socket: %v", err)
	}
	if _, err := os.Stat(filepath.Join(near, "far.sock")); err == nil {
		t.Error("the near side should not have started a server of its own")
	}
}

// TestAttachOverSSHSaysWhyItFailed: "connection closed" says nothing, and the
// reason — tend not installed there, a host key refused — is on stderr.
func TestAttachOverSSHSaysWhyItFailed(t *testing.T) {
	bin := buildBinary(t)
	stand := filepath.Join(t.TempDir(), "fake-ssh")
	script := "#!/bin/sh\necho 'sh: tend: command not found' >&2\nexit 127\n"
	if err := os.WriteFile(stand, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "ls", "-s", "far", "-ssh", "user@farhost")
	cmd.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+t.TempDir(), "TEND_SSH="+stand)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected a failure, got:\n%s", out)
	}
	if !strings.Contains(string(out), "command not found") {
		t.Errorf("the far side's complaint should be shown:\n%s", out)
	}
}

// TestAttachComesBackToTheSameLayoutAfterARestart is what used to cost the most
// time: replacing the server — to pick up a new build, or because it died —
// took every space, tab and split with it. The arrangement is written down now,
// and the server that starts next reads it back.
func TestAttachComesBackToTheSameLayoutAfterARestart(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	configPath := quietConfig(t)
	t.Setenv("TEND_CONFIG", configPath)
	bin := buildBinary(t)

	p, err := pty.Start(bin, []string{"attach", "-s", "keep"}, pty.Options{
		Size: pty.Size{Cols: 110, Rows: 20},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"TEND_CONFIG="+configPath,
			"SHELL=/bin/sh",
			"TERM=xterm-256color",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "keep")
	})

	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf BEFORE-THE-RESTART\n", "the marker", func(s string) bool {
		return strings.Contains(s, "BEFORE-THE-RESTART")
	})
	// The shell says who it is, so that afterwards "a pane answered" can be
	// told apart from "the old pane had not been killed yet".
	a.sendUntil(t, "printf 'OLD-SHELL-%s-\\n' $$\n", "the old shell's pid", func(s string) bool {
		return shellPid(s, "OLD-SHELL-") != ""
	})
	oldPid := shellPid(a.text(), "OLD-SHELL-")
	// A split, a second tab and a second space: some of everything.
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool { return strings.Count(s, "┌") == 2 })
	a.send(t, "\x02N")
	a.waitForScreen(t, "a second space", func(string) bool {
		return strings.Contains(a.sidebarText(), "space 2")
	})

	// Stop the server out from under the client, as a restart does. Not
	// stopSession: that waits for the socket to go, and here it never does,
	// because the client starts the replacement on the same path at once.
	c, err := connect("keep", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()

	// Nothing below waits for the "reconnected" notice. It is on screen for a
	// moment and then gone, and what it stands for is checked directly: the
	// old server is stopped, so a pane that answers is a restored one.
	// Both spaces are back.
	a.waitForScreen(t, "the spaces", func(string) bool {
		side := a.sidebarText()
		return strings.Contains(side, "main") && strings.Contains(side, "space 2")
	})
	// And in the first one, the split and what had been said in it.
	a.clickAt(t, 4, a.lineContaining(t, "main"))
	a.waitForScreen(t, "the split and its past", func(s string) bool {
		return strings.Count(s, "┌") == 2 && strings.Contains(s, "BEFORE-THE-RESTART")
	})

	// The panes are alive, not pictures of panes — and they are new processes
	// under the old output, not the old ones in the moment before they die.
	a.sendUntil(t, "printf 'NEW-SHELL-%s-\\n' $$\n", "a restored pane to answer", func(s string) bool {
		pid := shellPid(s, "NEW-SHELL-")
		return pid != "" && pid != oldPid
	})
	if !strings.Contains(a.text(), "OLD-SHELL-"+oldPid) {
		t.Errorf("the old shell's output should still be above the new one:\n%s", a.text())
	}
}

// shellPid reads a pid a shell printed after a marker, or "" when it has not
// been printed whole yet. The trailing dash is what says it is whole.
func shellPid(screen, marker string) string {
	at := strings.LastIndex(screen, marker)
	if at < 0 {
		return ""
	}
	rest := screen[at+len(marker):]
	end := strings.IndexByte(rest, '-')
	if end <= 0 {
		return ""
	}
	for _, r := range rest[:end] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return rest[:end]
}

// TestBareTendStartsWhenTheRuntimeDirectoryIsGone is the first tend after a
// reboot. The runtime directory lives on a filesystem that is emptied at
// logout, and the log is opened before the server exists — so tend failed on
// its own log file, before it had started anything, every time the machine had
// been restarted.
func TestBareTendStartsWhenTheRuntimeDirectoryIsGone(t *testing.T) {
	// A path that does not exist yet, as /run/user/<uid>/tend does not.
	runtimeDir := filepath.Join(t.TempDir(), "not", "made", "yet")
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	configPath := quietConfig(t)
	t.Setenv("TEND_CONFIG", configPath)

	bin := buildBinary(t)
	p, err := pty.Start(bin, nil, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+runtimeDir,
			"TEND_CONFIG="+configPath,
			"SHELL=/bin/sh",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "default")
	})

	a.waitForScreen(t, "a session, from a directory that was not there", func(s string) bool {
		return strings.Contains(s, "┌")
	})
	// Made private, since the socket goes in it.
	info, err := os.Stat(runtimeDir)
	if err != nil {
		t.Fatalf("the runtime directory should have been made: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("the runtime directory is %o, want 700", perm)
	}
}

// TestANewTabOnAnotherMachineRunsThatMachinesShell: found on a real host, a
// client sent its own shell (/usr/bin/zsh) with every new pane, and on a
// machine without zsh every new tab failed. The server now picks its own
// shell when the client names none, which a remote client does. Here the
// client's shell does not exist at all and the far side's is /bin/sh.
func TestANewTabOnAnotherMachineRunsThatMachinesShell(t *testing.T) {
	bin := buildBinary(t)
	near, far := t.TempDir(), t.TempDir()
	stand := filepath.Join(t.TempDir(), "fake-ssh")
	script := "#!/bin/sh\nshift; shift\nSHELL=/bin/sh TEND_RUNTIME_DIR=" + far + " exec " + bin + " \"$@\"\n"
	if err := os.WriteFile(stand, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := pty.Start(bin, []string{"--remote", "user@farhost", "--session", "far"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 16},
		Env: append(os.Environ(),
			"TEND_RUNTIME_DIR="+near,
			"TEND_CONFIG="+quietConfig(t),
			"TEND_SSH="+stand,
			"SHELL=/nowhere/zsh",
			"TERM=xterm-256color",
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 16, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stop := exec.Command(bin, "kill", "-s", "far", "-server")
		stop.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+far)
		_ = stop.Run()
		waitForSocketGone(filepath.Join(far, "far.sock"))
	})

	a.waitForScreen(t, "the far session", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02c")
	a.waitForScreen(t, "a second tab", func(s string) bool { return strings.Contains(s, "tab 2") })
	a.sendUntil(t, "echo $((40+2))-there\n", "the new tab's shell", func(s string) bool {
		return strings.Contains(s, "42-there")
	})
}

// TestTheSidebarsEdgeDragsToAWidth: dragging the rule down the sidebar's
// right edge makes the sidebar that wide, within herdr's 18 to 36, with the
// panes beside it following; a double click on the edge puts it back. If it
// regresses, the sidebar is the one width whatever the names in it need.
func TestTheSidebarsEdgeDragsToAWidth(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	rule := func() int {
		line := []rune(a.lines()[6])
		for x, r := range line {
			if r == '│' {
				return x
			}
		}
		return -1
	}
	if got := rule(); got != ui.SidebarWidth-1 {
		t.Fatalf("the edge starts at %d", got)
	}

	// Counted from one, as the terminal reports: the rule is column 26.
	a.dragFromTo(t, 0, ui.SidebarWidth, 7, 34, 7)
	a.waitForScreen(t, "the sidebar 34 wide, the pane beside it", func(s string) bool {
		line := []rune(strings.Split(s, "\n")[6])
		return len(line) > 34 && line[33] == '│' && line[25] != '│'
	})

	a.dragFromTo(t, 0, 34, 7, 90, 7)
	a.waitForScreen(t, "no wider than 36", func(s string) bool {
		line := []rune(strings.Split(s, "\n")[6])
		return len(line) > 36 && line[35] == '│'
	})

	a.clickAt(t, 36, 7)
	a.clickAt(t, 36, 7)
	a.waitForScreen(t, "a double click puts it back", func(s string) bool {
		line := []rune(strings.Split(s, "\n")[6])
		return len(line) > 26 && line[25] == '│'
	})
}

// quietConfig is a settings file of the defaults but for the files panel
// opening by itself in every tab, which would put a panel beside every pane
// every test lays out and reads. The tests about that panel ask for it.
func quietConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(quietSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// quietSettings is quietConfig's file, and what a test's own settings add
// to stay out of the files panel's way.
const quietSettings = "[files]\nauto_open = false\n\n[ui.toolbar]\nenabled = false\n"

// TestTheToolbarsFilesToolTogglesThePanel: with the toolbar on, the tools
// are over the spaces; a click on Files opens the files panel as prefix+f
// does and a second one on it closes it, and the keyboard's walk of the
// sidebar reaches the tools. If it regresses, the toolbar is a picture of
// buttons that do nothing, or it covers the list under it.
func TestTheToolbarsFilesToolTogglesThePanel(t *testing.T) {
	t.Setenv("PATH", filepath.Dir(buildBinary(t))+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("[files]\nauto_open = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withConfig(t, cfg)
	a := startSession(t, 120, 30)
	a.waitForScreen(t, "the tools over the spaces", func(s string) bool {
		lines := strings.Split(s, "\n")
		return strings.Contains(lines[0], " F ") && strings.Contains(lines[0], " C ") && strings.Contains(lines[2], "spaces")
	})
	panel := func(s string) bool { return strings.Contains(s, " files ─") }

	// Counted from one: the Files tool is the second column's chip.
	a.clickAt(t, 3, 1)
	a.waitForScreen(t, "the files panel", panel)
	a.clickAt(t, 3, 1)
	a.waitForScreen(t, "the panel closed", func(s string) bool { return !panel(s) })

	// prefix+w, then up from the spaces to the tools: the last is Context,
	// whose name the status line says.
	a.send(t, "\x02w")
	time.Sleep(200 * time.Millisecond)
	a.send(t, "k")
	time.Sleep(200 * time.Millisecond)
	a.send(t, "k")
	a.waitForScreen(t, "a tool's name on the status line", func(s string) bool {
		return strings.Contains(s, "Context — captured") || strings.Contains(s, "Browser (next)")
	})
}

// TestTheAgentManagerInstallsInATabOfItsOwn: prefix+A lists the agents the
// server's machine has and has not; enter on one that is not asks, showing
// the command, and enter again runs it in a new tab, which says how it went.
// The network is a stand-in: curl here prints a harmless script, so the
// whole way is run and nothing is fetched. If it regresses, the manager
// installs without asking, or somewhere the user cannot see.
func TestTheAgentManagerInstallsInATabOfItsOwn(t *testing.T) {
	// A PATH of the stand-in curl, the system's own programs, and go, which
	// the session builds tend with: no agent the machine running this has
	// installed elsewhere, and no real curl.
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("needs go on the PATH")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\necho 'echo fake-install-ran'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin:"+filepath.Dir(goBin))
	a := startSession(t, 120, 40)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02A")
	a.waitForScreen(t, "the manager, with amp installable", func(s string) bool {
		return strings.Contains(s, "AGENT MANAGER") && strings.Contains(s, "○ Amp")
	})
	row, col := -1, -1
	for i, line := range a.lines() {
		if c := columnOfString(line, "○ Amp"); c >= 0 {
			row, col = i, c
		}
	}
	a.clickAt(t, col+3, row+1)
	a.clickAt(t, col+3, row+1)
	a.waitForScreen(t, "the command, asked about", func(s string) bool {
		return strings.Contains(s, "install: curl -fsSL https://ampcode.com/install.sh | bash")
	})
	a.send(t, "\r")
	a.waitForScreen(t, "the install run in its own tab", func(s string) bool {
		return strings.Contains(s, "install amp") && strings.Contains(s, "fake-install-ran") &&
			strings.Contains(s, "[tend] Amp installed") && !strings.Contains(s, "AGENT MANAGER")
	})
}

// TestTheAboutPanelSaysWhichTendIsRunning: the global menu opens with
// "about tend", dotted while the server is another build; it opens a panel
// with this build, the server's with the handoff that brings it along, who
// makes tend and where it lives; esc closes it; and ctrl+b ? has the
// version at its top. If it regresses, nothing in tend says which version
// is running, and a server left behind by an update goes unseen.
func TestTheAboutPanelSaysWhichTendIsRunning(t *testing.T) {
	a := startSessionConfigured(t, 110, 24, server.Config{Build: "v0.5.0"})
	a.waitForScreen(t, "the new row", func(s string) bool { return strings.Contains(s, "menu") })

	a.clickAt(t, ui.SidebarWidth-3, a.lineContaining(t, "new"))
	a.waitForScreen(t, "about first in the menu, dotted", func(s string) bool {
		return strings.Contains(s, "about tend ●") && strings.Contains(s, "reload config")
	})
	row := a.lineContaining(t, "about tend")
	a.clickAt(t, columnOfString(a.lines()[row-1], "about tend")+3, row)
	a.waitForScreen(t, "the about panel", func(s string) bool {
		return strings.Contains(s, "a terminal runtime for coding agents") &&
			strings.Contains(s, "development build") &&
			strings.Contains(s, "v0.5.0 — run tend handoff -s tui") &&
			strings.Contains(s, "Auth Tecnologia Ltda") && strings.Contains(s, "tend.auth.com.br") &&
			strings.Contains(s, "Apache-2.0")
	})
	a.send(t, "\x1b")
	a.waitForScreen(t, "the panel gone", func(s string) bool { return !strings.Contains(s, "a terminal runtime for coding agents") })

	a.send(t, "\x02?")
	a.waitForScreen(t, "the version atop the help", func(s string) bool {
		return strings.Contains(s, "tend development build · server v0.5.0") && strings.Contains(s, "ctrl+b then:")
	})
}

// TestAClickOnAPanelsCloseMarkClosesIt: the ✕ on a panel's top edge closes
// it, as esc does — here the keys' help and the sessions list. If it
// regresses, a panel can be closed from the keyboard alone.
func TestAClickOnAPanelsCloseMarkClosesIt(t *testing.T) {
	a := startSession(t, 110, 30)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	closeMark := func() (int, int) {
		for y, line := range a.lines() {
			if c := columnOfString(line, " ✕ "); c >= 0 {
				return c + 2, y + 1
			}
		}
		t.Fatalf("no ✕ on the screen:\n%s", a.text())
		return 0, 0
	}

	a.send(t, "\x02?")
	a.waitForScreen(t, "the help", func(s string) bool { return strings.Contains(s, "ctrl+b then:") })
	x, y := closeMark()
	a.clickAt(t, x, y)
	a.waitForScreen(t, "the help gone", func(s string) bool { return !strings.Contains(s, "ctrl+b then:") })

	a.send(t, "\x02S")
	a.waitForScreen(t, "the sessions", func(s string) bool { return strings.Contains(s, "AGENT SESSIONS") })
	x, y = closeMark()
	a.clickAt(t, x, y)
	a.waitForScreen(t, "the sessions gone", func(s string) bool { return !strings.Contains(s, "AGENT SESSIONS") })
}
