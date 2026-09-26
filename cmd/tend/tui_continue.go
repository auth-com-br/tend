package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/proto"
)

// Handing a task from one agent to another (docs/MEMORY.md, #27): "continue
// in…" on an agent's pane. The server reads the conversation the pane's
// agent is in — the one its hook reported — and the state of the project
// it left; this writes a note from it, without asking a model to, starts
// the chosen agent in a new tab of the same space, where the conversation
// was held, and puts the note in the context, where it is looked over and
// sent to the new agent — typed, not submitted — once that is ready.

// continueAgents are the agents a task can be handed to, by name.
func continueAgents() []string {
	out := make([]string, 0, len(issueAgents))
	for name := range issueAgents {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

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
	argv, ok := issueAgents[agent]
	if !ok {
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
	note := handoffNote(h)
	_, err = t.client.ContextAdd(proto.ContextItem{
		Kind: capture.KindText, Source: "handoff", Title: "handoff: " + orDash(h.Title), Text: note,
	})
	if err != nil {
		t.setMessage("could not put the note in the context: "+err.Error(), true)
		return
	}
	tab, _, err := t.client.NewTab(ws, agent+" ← "+h.Agent, proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", resumeScript, "tend-continue"}, argv...),
		Dir:     h.Dir,
		DirOf:   pane,
		Agent:   agent,
	})
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

// handoffNote is what the next agent is told: the task, what was asked, the
// files written, where the last one stopped, and the project's state, with
// where to read the whole conversation.
func handoffNote(h proto.AgentHandoff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are taking over a task another agent (%s) was working on in this project. ", orDash(h.Agent))
	b.WriteString("Read this, look at the files it changed and the uncommitted work, and carry on from where it stopped. ")
	b.WriteString("Do not redo what it already did; ask if something is unclear.\n\n")
	if h.Title != "" {
		fmt.Fprintf(&b, "Task: %s\n", h.Title)
	}
	if h.First != "" {
		fmt.Fprintf(&b, "First request: %s\n", h.First)
	}
	if len(h.Recent) > 0 {
		b.WriteString("Latest requests, oldest first:\n")
		for _, r := range h.Recent {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	if len(h.Files) > 0 {
		b.WriteString("Files it changed:\n")
		for _, f := range h.Files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if h.Last != "" {
		fmt.Fprintf(&b, "Where it stopped — its last message:\n%s\n", h.Last)
	}
	if h.Branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n", h.Branch)
	}
	if h.Status != "" {
		fmt.Fprintf(&b, "Uncommitted changes (git status --short):\n%s\n", h.Status)
	}
	if h.Path != "" {
		fmt.Fprintf(&b, "The whole conversation, if you need more: %s\n", h.Path)
	}
	return strings.TrimRight(b.String(), "\n")
}
