package main

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// twoMachines is a client on this machine with one other saved, whose
// session has an agent waiting in pane 1 — the same number as a shell here.
func twoMachines() (*tui, *endpoint) {
	tu := &tui{config: config.Defaults(), toasts: "tend", notices: map[uint64]paneNotice{}}
	tu.snap = proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{{ID: 1, Name: "main", Tabs: []proto.TabInfo{{ID: 1, Name: "t", Panes: []uint64{1, 2}}}}},
		Panes: []proto.PaneInfo{
			{ID: 1, Running: true},
			{ID: 2, Agent: "codex", State: "working", Running: true, StateSeq: 9},
		},
	}
	local := &endpoint{id: localEndpoint, label: "Local", status: ui.MachineOnline, notices: map[uint64]paneNotice{}}
	far := &endpoint{id: "m_far", label: "farbox", status: ui.MachineOnline, have: true, notices: map[uint64]paneNotice{}}
	far.snap = proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{{ID: 1, Name: "farspace", Tabs: []proto.TabInfo{{ID: 1, Name: "t", Panes: []uint64{1}}}}},
		Panes:      []proto.PaneInfo{{ID: 1, Agent: "claude", State: "blocked", Running: true, StateSeq: 1}},
	}
	tu.machines = &machinesState{
		endpoints: []*endpoint{local, far}, active: localEndpoint,
		folded: map[string]bool{}, remoteFolded: map[string]map[string]bool{},
	}
	return tu, far
}

// TestEveryMachinesAgentsAreListedNamingTheirMachine is herdr's aggregate
// agent list: the other machine's agents are in it, each row naming its
// machine — this one's as "Local" — and pointing at the machine it is on; in
// priority order the one waiting comes first wherever it is, and a machine
// that cannot be reached goes last. If it regresses, an agent waiting on the
// server is invisible from the laptop.
func TestEveryMachinesAgentsAreListedNamingTheirMachine(t *testing.T) {
	tu, far := twoMachines()
	text := func(r ui.SidebarRow) string {
		var b strings.Builder
		for _, line := range r.Lines {
			for _, tok := range line {
				b.WriteString(tok.Text + " ")
			}
		}
		return b.String()
	}

	rows := tu.agentRowsLocked()
	if len(rows) != 2 {
		t.Fatalf("rows %+v, want one agent from each machine", rows)
	}
	if rows[0].Machine != "" || rows[0].Pane != 2 || !strings.Contains(text(rows[0]), "Local") {
		t.Errorf("this machine's agent: %+v %q", rows[0], text(rows[0]))
	}
	if rows[1].Machine != far.id || rows[1].Pane != 1 || !strings.Contains(text(rows[1]), "farbox") {
		t.Errorf("the other machine's agent: %+v %q", rows[1], text(rows[1]))
	}
	if rows[1].Active {
		t.Error("an agent on another machine is never the one in view")
	}

	tu.config.UI.AgentPanelSort = "priority"
	if rows := tu.agentRowsLocked(); rows[0].Machine != far.id {
		t.Errorf("the waiting agent should lead the queue, wherever it is: %+v", rows[0])
	}
	far.status = ui.MachineReconnecting
	if rows := tu.agentRowsLocked(); rows[0].Machine != "" || !rows[1].Stale {
		t.Errorf("an unreachable machine's agents go last, dimmed: %+v", rows)
	}
}

// TestAnotherMachinesAgentIsAnnouncedAndItsCardKnowsWhere: herdr's client
// takes notifications from every machine. The other machine's waiting agent
// gets a card that names the machine and remembers it, so a click goes
// there; its record is kept apart from this machine's, and news of this
// machine's pane 1 does not replace the card about the other's. If it
// regresses, the agent that needs you on the server says nothing, or its
// card opens a pane here.
func TestAnotherMachinesAgentIsAnnouncedAndItsCardKnowsWhere(t *testing.T) {
	tu, far := twoMachines()
	tu.announceFrom(far, proto.Event{Kind: proto.EventPaneState, Pane: 1, State: "blocked", Agent: "claude"})

	if tu.toast == nil || tu.toast.machine != far.id || tu.toast.pane != 1 {
		t.Fatalf("card %+v, want one about the other machine's pane 1", tu.toast)
	}
	if !strings.Contains(tu.toast.toast.Title, "claude needs attention") || !strings.Contains(tu.toast.toast.Body, "farbox") {
		t.Errorf("the card should say who and where: %+v", tu.toast.toast)
	}
	if len(tu.notices) != 0 || far.notices[1].state == 0 {
		t.Errorf("the record belongs to its machine: here %v, there %v", tu.notices, far.notices)
	}
	if tu.lastNoticeMachine != far.id || tu.lastNotice != 1 {
		t.Errorf("open-notification should go to the other machine: %q %d", tu.lastNoticeMachine, tu.lastNotice)
	}

	tu.pushToast(ui.ToastFinished, "here finished", "", 1)
	if tu.toast.machine != far.id || len(tu.toastQueue) != 1 {
		t.Errorf("news of this machine's pane 1 replaced the other's card: shown %+v, queued %d", tu.toast, len(tu.toastQueue))
	}
}

// TestNextAgentCrossesMachines is herdr's online_agent_targets: next agent
// from this machine's goes to the other machine's, and a machine that cannot
// be reached is stepped over. If it regresses, the key that finds the agent
// waiting never leaves the laptop.
func TestNextAgentCrossesMachines(t *testing.T) {
	tu, far := twoMachines()
	if machine, pane := tu.agentStep(2, 1); machine != far.id || pane != 1 {
		t.Errorf("next from here = %q %d, want the other machine's pane 1", machine, pane)
	}
	far.status = ui.MachineReconnecting
	if machine, pane := tu.agentStep(2, 1); machine != "" || pane != 2 {
		t.Errorf("with the other machine unreachable, next = %q %d, want this one's again", machine, pane)
	}
}

// TestAMutedAgentIsShownButNotAnnounced: an agent muted from its menu
// raises no card, no system notification and no sound when it stops for
// you or finishes; unmuted, it does again. If it regresses, a worker that
// finishes every minute buzzes every minute, which is what herdr's users
// asked to be spared (discussions #2414, #2529).
func TestAMutedAgentIsShownButNotAnnounced(t *testing.T) {
	tu, far := twoMachines()
	for i := range far.snap.Panes {
		if far.snap.Panes[i].ID == 1 {
			far.snap.Panes[i].Muted = true
		}
	}
	tu.announceFrom(far, proto.Event{Kind: proto.EventPaneState, Pane: 1, State: "blocked", Agent: "claude"})
	if tu.toast != nil || len(tu.pendingNotices) != 0 {
		t.Fatalf("a muted agent was announced: %+v", tu.toast)
	}
	for i := range far.snap.Panes {
		far.snap.Panes[i].Muted = false
	}
	far.notices = map[uint64]paneNotice{}
	tu.announceFrom(far, proto.Event{Kind: proto.EventPaneState, Pane: 1, State: "blocked", Agent: "claude"})
	if tu.toast == nil {
		t.Error("unmuted, it should be announced again")
	}
}
