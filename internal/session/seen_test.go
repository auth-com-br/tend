package session

import (
	"testing"

	"github.com/auth-com-br/tend/internal/detect"
)

// TestAnAgentThatFinishesUnwatchedIsUnseen holds herdr's rule for "done": an
// agent that goes from working or blocked to idle while its tab is not in
// view is unseen until the tab is looked at or it starts again; in view, it
// is seen at once. If it regresses, an agent that finished in another tab
// looks exactly like one that has been idle for an hour.
func TestAnAgentThatFinishesUnwatchedIsUnseen(t *testing.T) {
	s, tab, a, _, _ := threePanes(t)
	pane := func() *Pane { p, _ := s.Pane(a); return p }

	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, 0)
	seq := pane().StateSeq
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, 0)
	if !pane().Unseen {
		t.Error("finishing while its tab is not in view should be unseen")
	}
	if pane().StateSeq <= seq {
		t.Error("a change of state should move the sequence on")
	}
	if !s.MarkTabSeen(tab.ID) || pane().Unseen {
		t.Error("looking at the tab should see it")
	}
	if s.MarkTabSeen(tab.ID) {
		t.Error("a second look changes nothing")
	}

	_ = s.SetPaneStateWatched(a, "claude", detect.StateBlocked, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, tab.ID)
	if pane().Unseen {
		t.Error("finishing in the tab in view is seen")
	}

	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, 0)
	if pane().Unseen {
		t.Error("starting again clears it")
	}
	// Unknown to idle is not a finish.
	_ = s.SetPaneStateWatched(a, "claude", detect.StateUnknown, 0)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, 0)
	if pane().Unseen {
		t.Error("unknown to idle is not a finish")
	}
}

// TestAnAgentsNameIsKeptInTheSnapshot: the name a script gave an agent is
// how it addresses it; losing it on a restart or a handoff breaks the script
// on its next call.
func TestAnAgentsNameIsKeptInTheSnapshot(t *testing.T) {
	s, _, a, _, _ := threePanes(t)
	p, _ := s.Pane(a)
	p.AgentName = "reviewer"
	restored, err := Restore(s.Snapshot(nil))
	if err != nil {
		t.Fatal(err)
	}
	if q, _ := restored.Pane(a); q == nil || q.AgentName != "reviewer" {
		t.Errorf("restored pane = %+v, want the name kept", q)
	}
}

// TestAnAgentMarkedUnreadStaysUntilItsTabIsArrivedAt: an agent marked unread
// by hand is a reminder to come back to it, so what clears "unseen" — it
// working again, the tab being looked at as focus moves within it or as the
// window comes back — leaves the mark; arriving at its tab from another
// clears it; and a restart keeps it. If it regresses, glancing at an agent
// marked to come back to loses it from the list — what herdr's users asked
// the mark to prevent (herdr discussion #864).
func TestAnAgentMarkedUnreadStaysUntilItsTabIsArrivedAt(t *testing.T) {
	s, tab, a, _, _ := threePanes(t)
	pane := func(s *Session) *Pane { p, _ := s.Pane(a); return p }

	if err := s.SetMarked(a, true); err != nil {
		t.Fatal(err)
	}
	_ = s.SetPaneStateWatched(a, "claude", detect.StateWorking, tab.ID)
	_ = s.SetPaneStateWatched(a, "claude", detect.StateIdle, tab.ID)
	s.MarkTabSeen(tab.ID)
	if !pane(s).Marked {
		t.Fatal("working again or the tab being looked at should not take the mark off")
	}

	restored, err := Restore(s.Snapshot(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !pane(restored).Marked {
		t.Error("a restart should keep the mark")
	}

	if !s.MarkTabArrived(tab.ID) || pane(s).Marked {
		t.Error("arriving at its tab should take the mark off")
	}
	if err := s.SetMarked(a, true); err != nil || !pane(s).Marked {
		t.Fatal("marking again")
	}
	if err := s.SetMarked(a, false); err != nil || pane(s).Marked {
		t.Error("marking read takes it off")
	}
	if err := s.SetMarked(9999, true); err == nil {
		t.Error("no such pane should be an error")
	}
}
