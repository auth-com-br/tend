package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/proto"
)

// Handing a task from one agent to another (docs/MEMORY.md, #27): "continue
// in…". The server reads the conversation the agent is in and the state of
// the project it left; a client writes a note from it, without asking a
// model to, starts the chosen agent in a new tab of the same space, where
// the conversation was held, and puts the note in the context, where it is
// looked over and sent to the new agent once that is ready.

// AgentArgv is how each agent is started with a prompt of its own: as the
// first argument for most, behind -i for Gemini and Antigravity, whose
// bare prompt runs once and exits (agy's -i is --prompt-interactive: the
// prompt, then the session goes on). An agent not here is started with
// nothing. It is also the agents a task can be handed to.
var AgentArgv = map[string][]string{
	"agy":    {"agy", "-i"},
	"claude": {"claude"},
	"codex":  {"codex"},
	"cursor": {"cursor-agent"},
	"gemini": {"gemini", "-i"},
}

// ContinueAgents are the agents a task can be handed to, by name.
func ContinueAgents() []string {
	out := make([]string, 0, len(AgentArgv))
	for name := range AgentArgv {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ContinueSpec is the pane the agent taking a task over starts in: where
// the conversation was held (h.Dir), else where the pane it came from is.
func ContinueSpec(agentName string, h proto.AgentHandoff, fromPane uint64) (proto.PaneSpec, bool) {
	argv, ok := AgentArgv[agentName]
	if !ok {
		return proto.PaneSpec{}, false
	}
	return proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", ResumeScript, "tend-continue"}, argv...),
		Dir:     h.Dir,
		DirOf:   fromPane,
		Agent:   agentName,
	}, true
}

// HandoffItem is the note as the context keeps it.
func HandoffItem(h proto.AgentHandoff) proto.ContextItem {
	return proto.ContextItem{
		Kind: capture.KindText, Source: "handoff", Title: "handoff: " + orDash(h.Title), Text: HandoffNote(h),
	}
}

// HandoffNote is what the next agent is told: the task, what was asked, the
// files written, where the last one stopped, and the project's state, with
// where to read the whole conversation.
func HandoffNote(h proto.AgentHandoff) string {
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

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
