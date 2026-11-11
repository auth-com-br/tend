package main

import (
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// TestNextPendingGoesToWhatWaitsLongest: prefix+u is a queue — the blocked
// before the finished unseen and the marked unread, the one waiting longest
// first in each, never the pane already in view, across machines; an agent
// working or idle and seen waits for nothing. If it regresses, the key walks
// the list in its order and the person has to look at every agent to find
// the one that stopped.
func TestNextPendingGoesToWhatWaitsLongest(t *testing.T) {
	ranks := map[string]int{
		"blocked":         pendingRank(proto.PaneInfo{Running: true, State: "blocked"}),
		"done":            pendingRank(proto.PaneInfo{Running: true, State: "idle", Done: true}),
		"marked, working": pendingRank(proto.PaneInfo{Running: true, State: "working", Marked: true}),
		"working":         pendingRank(proto.PaneInfo{Running: true, State: "working"}),
		"idle":            pendingRank(proto.PaneInfo{Running: true, State: "idle"}),
		"asleep, blocked": pendingRank(proto.PaneInfo{Running: true, State: "blocked", Dormant: true}),
		"ended":           pendingRank(proto.PaneInfo{State: "blocked"}),
	}
	for name, want := range map[string]int{"blocked": 0, "done": 1, "marked, working": 1, "working": -1, "idle": -1, "asleep, blocked": -1, "ended": -1} {
		if ranks[name] != want {
			t.Errorf("%s: rank %d, want %d", name, ranks[name], want)
		}
	}

	agents := []pendingAgent{
		{pane: 1, rank: 1, seq: 5},                 // done, recent
		{pane: 2, rank: -1, seq: 1},                // working
		{pane: 3, rank: 1, seq: 2},                 // done, older
		{machine: "box", pane: 4, rank: 0, seq: 9}, // blocked on another machine
		{pane: 5, rank: 0, seq: 3},                 // blocked, the one in view
	}
	next, ok := nextPending(agents, "", 5)
	if !ok || next.machine != "box" || next.pane != 4 {
		t.Fatalf("got %+v, want the blocked one on box (the other blocked is in view)", next)
	}
	next, _ = nextPending(agents[:3], "", 0)
	if next.pane != 3 {
		t.Errorf("got pane %d, want 3, done longest ago", next.pane)
	}
	if _, ok := nextPending([]pendingAgent{{pane: 2, rank: -1}}, "", 0); ok {
		t.Error("nothing waiting should say so")
	}
}

// TestTheAgentMenuOffersToMarkUnreadOrRead: the agent's menu offers marking
// it unread, or read once it is marked, before "close pane".
func TestTheAgentMenuOffersToMarkUnreadOrRead(t *testing.T) {
	m := ui.WithMarkUnread(ui.AgentMenu(1, 2, 3, 0, 0), false)
	if got := m.Items[len(m.Items)-2]; got.Action != ui.MenuMarkUnread || got.Label != "mark unread" {
		t.Errorf("unmarked: %+v", got)
	}
	if last := m.Items[len(m.Items)-1]; last.Action != ui.MenuClose {
		t.Errorf("close pane should stay last, got %+v", last)
	}
	m = ui.WithMarkUnread(ui.AgentMenu(1, 2, 3, 0, 0), true)
	if got := m.Items[len(m.Items)-2]; got.Action != ui.MenuMarkRead || got.Label != "mark read" {
		t.Errorf("marked: %+v", got)
	}
}
