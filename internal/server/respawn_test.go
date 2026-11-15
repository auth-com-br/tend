package server

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/session"
)

// TestRespawningAPaneRunsANewProgramInTheSamePane is tmux's respawn-pane
// -k, which Claude Code's agent teams start each teammate with: the pane it
// split, running cat, is given the teammate. If it regresses, the teammate
// opens in a new pane while an empty one sits where Claude Code expects it,
// or the pane id Claude Code holds no longer names it.
func TestRespawningAPaneRunsANewProgramInTheSamePane(t *testing.T) {
	s := newServer(t)
	tab, pane := openTab(t, s, "echo before; exec cat")
	waitFor(t, "the first program", func() bool { return screenHas(s, pane, "before") })
	before := statusOf(t, s, pane).Pid

	if err := s.RespawnPane(pane, PaneSpec{Command: []string{"/bin/sh", "-c", "echo after; exec cat"}, CloseOnExit: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the new program", func() bool { return screenHas(s, pane, "after") })
	st := statusOf(t, s, pane)
	if !st.Running || st.Pid == before {
		t.Fatalf("after respawning: %+v (first pid %d)", st, before)
	}
	if !screenHas(s, pane, "before") {
		t.Fatal("the screen the first program left was not kept above the new one")
	}
	snap := s.Snapshot()
	var panes []uint64
	for _, ws := range snap.Workspaces {
		for _, tb := range ws.Tabs {
			if session.TabID(tb.ID) == tab {
				panes = tb.Panes
			}
		}
	}
	if len(panes) != 1 || session.PaneID(panes[0]) != pane {
		t.Fatalf("the tab holds %v, want just pane %d", panes, pane)
	}
}

// TestRespawningAPaneThatIsGoneSaysSo: Claude Code reports the failure
// rather than waiting on a teammate that never started.
func TestRespawningAPaneThatIsGoneSaysSo(t *testing.T) {
	s := newServer(t)
	if err := s.RespawnPane(999, PaneSpec{Command: []string{"/bin/true"}}); err == nil {
		t.Fatal("respawning a pane that does not exist succeeded")
	}
}

// TestAPaneInADirectoryThatIsGoneSaysTheDirectory: the start fails with the
// directory named, not as "fork/exec /bin/sh: no such file or directory",
// which blamed a shell that was there all along. Found opening a pane in a
// folder that had been cleaned away.
func TestAPaneInADirectoryThatIsGoneSaysTheDirectory(t *testing.T) {
	s := newServer(t)
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	gone := t.TempDir() + "/not-here"
	spec := shell("true")
	spec.Dir = gone
	_, _, err = s.NewTab(ws, "x", spec)
	if err == nil || !strings.Contains(err.Error(), "the directory "+gone+" does not exist") {
		t.Fatalf("err = %v", err)
	}
}
