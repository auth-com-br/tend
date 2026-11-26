package core

import (
	"slices"

	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The context screen: the server's context buffer, as the TUI's context
// panel (cmd/tend/tui_context.go) shows it. What a tool captured is
// listed, newest first; an item can be copied, sent to the agent being
// worked with — typed into its pane, not submitted, to be read over — or
// dropped, and the buffer cleared. It is read again whenever the server
// says the buffer changed, so a capture made while it is open shows.

// ContextState is the screen as QML draws it.
type ContextState struct {
	Items []ContextRow `json:"items"`
	// Target is the pane "send" types into, by number and name.
	Target     string `json:"target,omitempty"`
	Loading    bool   `json:"loading,omitempty"`
	Message    string `json:"message,omitempty"`
	ClearArmed bool   `json:"clearArmed,omitempty"`
}

// ContextRow is one item: a line, and its parts under headings.
type ContextRow struct {
	ID      uint64           `json:"id"`
	Kind    string           `json:"kind"`
	Summary string           `json:"summary"`
	Parts   []ui.ContextPart `json:"parts,omitempty"`
}

// ContextAction is what the screen asks for: Do "reload", "copy" (ID),
// "send" (ID), "send-all", "remove" (ID) or "clear" (twice: the first
// arms it).
type ContextAction struct {
	Do string `json:"do"`
	ID uint64 `json:"id"`
}

type contextScreen struct {
	state ContextState
	items []proto.ContextItem
}

// contextTargetLocked is the pane "send" types into, as the TUI decides it
// (tools.ContextTarget). The caller holds the lock.
func (s *Session) contextTargetLocked() (uint64, string) {
	shown := make([]uint64, 0, len(s.rects))
	for _, r := range s.rects {
		shown = append(shown, r.Pane)
	}
	var tabs [][]uint64
	if w, ok := workspaceIn(s.snap, s.workspace); ok {
		for _, t := range w.Tabs {
			tabs = append(tabs, t.Panes)
		}
	}
	return tools.ContextTarget(s.snap.Panes, s.focus, shown, tabs)
}

// loadContext reads the buffer into the screen.
func (s *Session) loadContext() {
	s.mu.Lock()
	c, v := s.client, s.tools.context
	s.mu.Unlock()
	if v == nil {
		return
	}
	if !slices.Contains(c.Server().Features, proto.FeatureContext) {
		s.contextSay("this server is older than the context panel; " + tools.HandoffCommand(s.name) + " moves it to this build")
		return
	}
	items, err := c.ContextList()
	tools.ContextNewestFirst(items)
	s.mu.Lock()
	if s.tools.context != v {
		s.mu.Unlock()
		return
	}
	v.state.Loading = false
	if err != nil {
		v.state.Message = "could not read the context: " + err.Error()
	} else {
		v.items = items
		v.state.Items = nil
		for _, it := range items {
			e := tools.ContextEntry(it)
			v.state.Items = append(v.state.Items, ContextRow{ID: it.ID, Kind: e.Kind, Summary: e.Summary, Parts: e.Parts})
		}
		_, v.state.Target = s.contextTargetLocked()
	}
	s.mu.Unlock()
	s.stateChanged()
}

func (s *Session) contextSay(msg string) {
	s.mu.Lock()
	if v := s.tools.context; v != nil {
		v.state.Message, v.state.Loading = msg, false
	}
	s.mu.Unlock()
	s.stateChanged()
}

// reloadContextIf reads the buffer again when its screen is open: the
// server said it changed.
func (s *Session) reloadContextIf() {
	s.mu.Lock()
	open := s.tools.context != nil
	s.mu.Unlock()
	if open {
		go s.loadContext()
	}
}

// Context does what the screen asks.
func (s *Session) Context(act ContextAction) error {
	s.mu.Lock()
	v, c := s.tools.context, s.client
	if v == nil {
		s.mu.Unlock()
		return nil
	}
	var item proto.ContextItem
	for _, it := range v.items {
		if it.ID == act.ID {
			item = it
		}
	}
	target, name := s.contextTargetLocked()
	armed := v.state.ClearArmed
	v.state.ClearArmed = false
	s.mu.Unlock()
	switch act.Do {
	case "reload":
		s.loadContext()
	case "copy":
		if item.ID != 0 && s.Copied != nil {
			s.Copied(capture.Format([]proto.ContextItem{item}))
			s.status("copied the context")
		}
	case "send", "send-all":
		if target == 0 {
			s.contextSay("no pane to send to in this tab")
			return nil
		}
		ids := []uint64{item.ID}
		if act.Do == "send-all" {
			// Oldest first, as they were captured: the browser's message
			// leads the elements it came with.
			ids = nil
		} else if item.ID == 0 {
			return nil
		}
		if err := c.ContextSend(target, ids); err != nil {
			s.contextSay("could not send it: " + err.Error())
			return nil
		}
		// To the agent, where it was typed, for it to be read over and
		// sent on.
		s.OpenPanel("")
		s.status("typed into " + name + " — read it over and press enter to send")
		return s.JumpToPane(target)
	case "remove":
		if item.ID == 0 {
			return nil
		}
		if err := c.ContextRemove([]uint64{item.ID}); err != nil {
			s.contextSay("could not remove it: " + err.Error())
		}
	case "clear":
		if !armed {
			s.mu.Lock()
			v.state.ClearArmed = true
			s.mu.Unlock()
			s.stateChanged()
			return nil
		}
		if err := c.ContextClear(); err != nil {
			s.contextSay("could not clear it: " + err.Error())
		}
	}
	s.stateChanged()
	return nil
}
