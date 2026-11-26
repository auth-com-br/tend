package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/ui"
)

// These tests drive Session against a real tend server, built from this
// checkout, in a runtime and state directory of their own: what the window
// does is what the server is asked, and only the real server says whether
// it was asked right.

var tendBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tend-desktop-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tendBin = filepath.Join(dir, "tend")
	// Built in the repository's root, by its own go.mod: the desktop's
	// module needs only the packages it imports, not all of tend's.
	build := exec.Command("go", "build", "-o", tendBin, "./cmd/tend")
	build.Dir = filepath.Join("..", "..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building tend: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var sessionCount atomic.Int32

// openTest opens a session of its own on a server of its own, stopped when
// the test ends.
func openTest(t *testing.T) *Session {
	t.Helper()
	// A short directory: a unix socket's path is limited to about a
	// hundred bytes, and t.TempDir's are long.
	run, err := os.MkdirTemp("/tmp", "tdt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	name := fmt.Sprintf("t%d", sessionCount.Add(1))
	s, err := Open(name, tendBin, config.Defaults().UI)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		_ = exec.Command(tendBin, "kill", "-s", name, "-server").Run()
		_ = os.RemoveAll(run)
	})
	return s
}

// waitFor polls until cond holds, or fails the test.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// screenText is what a pane's terminal here shows, live.
func screenText(s *Session, pane uint64) string {
	f, ok := s.Frame(pane, nil)
	if !ok {
		return ""
	}
	var b strings.Builder
	for i, c := range f.Cells {
		if i > 0 && i%f.Cols == 0 {
			b.WriteByte('\n')
		}
		if c.Width != 0 {
			b.WriteRune(rune(c.Ch))
		}
	}
	return b.String()
}

// TestTheWindowShowsASessionsPaneAtItsSize: a new session has a shell, laid
// out over the window's area, and what the shell prints reaches the pane's
// terminal here. If it regresses, the window opens empty, as its first
// version did on Wayland.
func TestTheWindowShowsASessionsPaneAtItsSize(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	if len(st.Panes) != 1 || st.Panes[0].Cols != 100 || st.Panes[0].Rows != 40 {
		t.Fatalf("panes %+v, want one of 100x40", st.Panes)
	}
	pane := st.Panes[0].Pane
	if err := s.ResizePane(pane, 98, 38); err != nil {
		t.Fatal(err)
	}
	for _, k := range "echo hello-desktop\r" {
		if err := s.Key(pane, int(k), 0, string(k)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the shell's output", func() bool {
		return strings.Count(screenText(s, pane), "hello-desktop") >= 2
	})
	if len(st.Spaces) != 1 || len(st.Tabs) != 1 {
		t.Errorf("sidebar %d spaces, %d tabs; want one each", len(st.Spaces), len(st.Tabs))
	}
}

// TestSplitsAndDividersAreTheServers: a split adds a pane beside the one in
// focus and gives it the focus; dragging the divider moves it by whole
// cells. If it regresses, the window's splits and the TUI's disagree.
func TestSplitsAndDividersAreTheServers(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	first := s.Focused()
	if err := s.Split("columns"); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	if len(st.Panes) != 2 {
		t.Fatalf("%d panes after a split, want 2", len(st.Panes))
	}
	if s.Focused() == first {
		t.Error("the new pane did not get the focus")
	}
	var before int
	for _, p := range st.Panes {
		if p.Pane == first {
			before = p.Cols
		}
	}
	if err := s.AdjustSplit(first, "right", 7); err != nil {
		t.Fatal(err)
	}
	for _, p := range s.State().Panes {
		if p.Pane == first && p.Cols != before+7 {
			t.Errorf("the divider moved the pane from %d to %d columns, want %d", before, p.Cols, before+7)
		}
	}
}

// TestATabOpensAndIsShown: + opens a tab in the space shown and goes to it.
func TestATabOpensAndIsShown(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(80, 24); err != nil {
		t.Fatal(err)
	}
	if err := s.NewTab(); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	if len(st.Tabs) != 2 || !st.Tabs[1].Active {
		t.Fatalf("tabs %+v, want two with the new one shown", st.Tabs)
	}
	if len(st.Panes) != 1 {
		t.Errorf("the new tab shows %d panes, want its one", len(st.Panes))
	}
}

// TestExitClosesThePane: a shell that exits takes its pane, as herdr's do,
// and the window shows what is left.
func TestExitClosesThePane(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	if err := s.Split("rows"); err != nil {
		t.Fatal(err)
	}
	gone := s.Focused()
	for _, k := range "exit\r" {
		_ = s.Key(gone, int(k), 0, string(k))
	}
	waitFor(t, "the pane to close", func() bool {
		for _, p := range s.State().Panes {
			if p.Pane == gone {
				return false
			}
		}
		return len(s.State().Panes) == 1
	})
}

// TestTheWheelScrollsAShellsHistory: in a pane whose program did not ask
// for the mouse, the wheel looks back through tend's history, and typing
// returns to the present.
func TestTheWheelScrollsAShellsHistory(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(80, 24); err != nil {
		t.Fatal(err)
	}
	pane := s.Focused()
	if err := s.ResizePane(pane, 80, 24); err != nil {
		t.Fatal(err)
	}
	for _, k := range "seq 1 200\r" {
		_ = s.Key(pane, int(k), 0, string(k))
	}
	waitFor(t, "the output", func() bool { return strings.Contains(screenText(s, pane), "200") })
	waitFor(t, "the history to be scrollable", func() bool {
		_ = s.Mouse(pane, ui.MouseEvent{Kind: ui.MouseWheelUp})
		for _, p := range s.State().Panes {
			if p.Pane == pane && p.Scroll > 0 {
				return true
			}
		}
		return false
	})
	_ = s.Key(pane, 'x', 0, "x")
	for _, p := range s.State().Panes {
		if p.Pane == pane && p.Scroll != 0 {
			t.Errorf("typing left the pane %d lines back", p.Scroll)
		}
	}
}
