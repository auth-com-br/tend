package main

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
)

// Handing a task from one agent to another (docs/MEMORY.md, #27): "continue
// in…" on an agent's pane. The server reads the conversation the pane's
// agent is in — the one its hook reported — and the state of the project
// it left; this writes a note from it, without asking a model to, starts
// the chosen agent in a new tab of the same space, where the conversation
// was held, and puts the note in the context, where it is looked over and
// sent to the new agent — typed, not submitted — once that is ready.

// continueAgents are the agents a task can be handed to, by name.
var continueAgents = tools.ContinueAgents

// paneAgentsLocked is the agent each pane runs, by pane.
func (t *tui) paneAgentsLocked() map[uint64]string {
	out := make(map[uint64]string, len(t.snap.Panes))
	for _, p := range t.snap.Panes {
		if p.Agent != "" {
			out[p.ID] = p.Agent
		}
	}
	return out
}

// continueIn hands a task to agent: the one of the agent in pane, or, with
// no pane, the conversation named — chosen in the sessions list — started
// in the space being shown.
func (t *tui) continueIn(pane uint64, conversation, agent string) {
	if _, ok := issueAgents[agent]; !ok {
		t.setMessage("tend does not know how to start "+agent, true)
		return
	}
	if !t.client.Supports(proto.MethodAgentSessionHandoff) {
		t.setMessage("this server is older than handing tasks on; "+handoffCommand(t.session)+" moves it to this build", true)
		return
	}
	t.mu.Lock()
	var ws uint64
	for _, w := range t.snap.Workspaces {
		for _, tab := range w.Tabs {
			for _, id := range tab.Panes {
				if id == pane {
					ws = w.ID
				}
			}
		}
	}
	from := t.paneAgentsLocked()[pane]
	if pane == 0 {
		ws, from = t.workspace, "claude"
	}
	t.mu.Unlock()
	if ws == 0 {
		return
	}
	t.setMessage("reading what "+orDash(from)+" was doing…", false)
	h, err := t.client.AgentHandoff(pane, conversation)
	if err != nil {
		t.setMessage(err.Error(), true)
		return
	}
	_, err = t.client.ContextAdd(tools.HandoffItem(h))
	if err != nil {
		t.setMessage("could not put the note in the context: "+err.Error(), true)
		return
	}
	spec, _ := tools.ContinueSpec(agent, h, pane)
	tab, _, err := t.client.NewTab(ws, agent+" ← "+h.Agent, spec)
	if err != nil {
		t.setMessage(agent+" did not start: "+err.Error(), true)
		return
	}
	t.mu.Lock()
	t.rememberFocusLocked()
	t.workspace, t.tab, t.focus, t.zoom = ws, tab, 0, false
	t.mu.Unlock()
	if err := t.refresh(); err != nil {
		t.setMessage(err.Error(), true)
		return
	}
	_ = t.openContext()
	t.setMessage("the note is first in the context: once "+agent+" is ready, enter types it in for you to send", false)
}

// handoffNote is what the next agent is told (internal/tools).
var handoffNote = tools.HandoffNote
