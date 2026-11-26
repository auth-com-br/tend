package core

import (
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
)

// TestTheTUIsFilesPanelIsLeftOutOfTheWindow: a tab the TUI gave its files
// panel (a pane named "files" running `tend files`, docked full height)
// shows in the window without it — the window has its own files tab — and
// the tab's other panes take the whole width; the panel is not closed, as
// it is the TUI's. If it regresses, the TUI's tree is drawn as a terminal
// beside the window's own.
func TestTheTUIsFilesPanelIsLeftOutOfTheWindow(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(120, 40); err != nil {
		t.Fatal(err)
	}
	shell := s.Focused()
	// The panel as the TUI opens it: a split to the side, named "files".
	pane, err := s.client.SplitPane(shell, "columns", proto.PaneSpec{Title: "files", Command: []string{"/bin/sh", "-c", "exec sleep 60"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if t.Failed() {
			for _, p := range s.State().Panes {
				t.Logf("pane %+v", p)
			}
			for _, p := range s.snap.Panes {
				t.Logf("snap %d title=%q named=%v", p.ID, p.Title, p.Named)
			}
		}
	}()
	waitFor(t, "the tab laid out without the panel", func() bool {
		st := s.State()
		if len(st.Panes) != 1 {
			return false
		}
		p := st.Panes[0]
		return p.Pane == shell && p.X == 0 && p.Cols == 120
	})
	if s.Focused() == pane {
		t.Error("the focus is on the panel the window does not draw")
	}
	alive := false
	for _, p := range s.snap.Panes {
		alive = alive || p.ID == pane
	}
	if !alive {
		t.Error("the TUI's panel was closed")
	}
}
