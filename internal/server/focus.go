package server

import (
	"fmt"

	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/session"
)

// A program can ask to be told when its terminal gains or loses focus (mode
// 1004): an editor dims its cursor, an agent stops animating, a shell reloads
// what changed while you were away. tend recorded the request and never sent
// anything, so every such program believed it had the focus for ever.
//
// Focus in tend is which pane the user is looking at, which is the client's
// business; the terminal that has to be told is the server's. So the client
// says which pane it focused, and the server tells the programs — the one that
// gained it and the one that lost it — but only those that asked.

// Focus sequences, as every terminal sends them.
const (
	focusGained = "\x1b[I"
	focusLost   = "\x1b[O"
)

// FocusPane tells panes that focus moved from one to another, and everything
// listening that it did. Either may be zero: nothing had it, or nothing has it
// now.
//
// The tab and the space it is in are announced as well, when they changed,
// because that is what a plugin decorating a tab is waiting for — and only
// when they changed, or every keystroke that moves focus inside one tab would
// announce the tab again.
func (s *Server) FocusPane(gained, lost session.PaneID) {
	if lost != 0 && lost != gained {
		s.sendFocus(lost, focusLost)
	}
	if gained == 0 {
		return
	}
	s.sendFocus(gained, focusGained)

	tab, workspace := s.placeOf(gained)
	s.mu.Lock()
	tabChanged := tab != 0 && tab != s.focusedTab
	wsChanged := workspace != 0 && workspace != s.focusedWorkspace
	paneChanged := gained != s.focusedPane
	s.focusedPane, s.focusedTab, s.focusedWorkspace = gained, tab, workspace
	// Looking at a tab is seeing what finished in it (herdr's switch_tab);
	// arriving at it from another is seeing what was marked unread by hand
	// too — focus moving within the tab is not, or marking the agent on
	// screen would undo itself at the next keystroke.
	var seen bool
	switch {
	case tab != 0 && tabChanged:
		seen = s.session.MarkTabArrived(tab)
	case tab != 0:
		seen = s.session.MarkTabSeen(tab)
	}
	s.mu.Unlock()

	if seen {
		s.publish(Event{Kind: EventPaneState, Pane: gained})
	}

	if wsChanged {
		s.publish(Event{Kind: EventWorkspaceFocused, Workspace: workspace})
	}
	if tabChanged {
		s.publish(Event{Kind: EventTabFocused, Tab: tab, Workspace: workspace})
	}
	if paneChanged {
		s.publish(Event{Kind: EventPaneFocused, Pane: gained, Tab: tab, Workspace: workspace})
	}
	// A sleeping agent wakes when somebody goes to it (dormant.go).
	s.wakeOnFocus(gained)
}

// placeOf is the tab and space a pane is in.
func (s *Server) placeOf(id session.PaneID) (session.TabID, session.WorkspaceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if _, ok := t.Pane(id); ok {
				return t.ID, w.ID
			}
		}
	}
	return 0, 0
}

// sendFocus writes a focus report to a pane that asked for one.
func (s *Server) sendFocus(id session.PaneID, sequence string) {
	rt, err := s.runtime(id)
	if err != nil {
		return
	}
	rt.mu.Lock()
	wants := rt.screen.Modes().FocusEvents
	rt.mu.Unlock()
	if !wants {
		// Sending it anyway would put an escape sequence into the input of
		// every program that never asked, which is a stray "[I" in a shell.
		return
	}
	_ = s.writeTo(rt, []byte(sequence))
}

// WantsFocusEvents reports whether a pane's program asked to be told.
func (s *Server) WantsFocusEvents(id session.PaneID) bool {
	rt, err := s.runtime(id)
	if err != nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.screen.Modes().FocusEvents
}

// WindowFocus records whether the client's terminal window has focus. While
// it does not, nothing is being looked at — an agent that finishes in the
// tab on screen finished unseen — and when it comes back, the tab on screen
// has been seen, as herdr acknowledges the active surface on focus.
func (s *Server) WindowFocus(focused bool) {
	s.mu.Lock()
	s.windowUnfocused = !focused
	seen := focused && s.focusedTab != 0 && s.session.MarkTabSeen(s.focusedTab)
	pane := s.focusedPane
	s.mu.Unlock()
	if seen {
		s.publish(Event{Kind: EventPaneState, Pane: pane})
	}
}

// watchedTabLocked is the tab somebody is looking at, or none while the
// window is behind something. The caller holds the lock.
func (s *Server) watchedTabLocked() session.TabID {
	if s.windowUnfocused {
		return 0
	}
	return s.focusedTab
}

// setPaneStateLocked records a pane's agent and state and reports whether an
// agent was newly recognised in it, which is herdr's pane.agent_detected.
// The caller holds the lock.
func (s *Server) setPaneStateLocked(id session.PaneID, agentLabel string, state detect.State) bool {
	previous := ""
	if p, ok := s.session.Pane(id); ok {
		previous = p.Agent
	}
	_ = s.session.SetPaneStateWatched(id, agentLabel, state, s.watchedTabLocked())
	return agentLabel != "" && agentLabel != previous
}

// RequestFocus asks every attached client to show a pane, and reports how
// many there were to ask (herdr's pane.focus answers "no client" when none).
func (s *Server) RequestFocus(id session.PaneID) (int, error) {
	tab, ws := s.placeOf(id)
	if tab == 0 {
		return 0, session.ErrNoSuchPane
	}
	s.mu.Lock()
	clients := len(s.conns)
	s.mu.Unlock()
	s.publish(Event{Kind: EventFocusRequest, Pane: id, Tab: tab, Workspace: ws})
	return clients, nil
}

// RequestScroll asks every attached client to show a pane scrolled back
// offset lines from the present, herdr's pane.scroll. Where herdr's app
// holds one scroll position per pane, tend's is each client's own (what one
// person is looking at), so the server asks, as RequestFocus does; zero
// asks for the live screen.
func (s *Server) RequestScroll(id session.PaneID, offset int) (int, error) {
	tab, ws := s.placeOf(id)
	if tab == 0 {
		return 0, session.ErrNoSuchPane
	}
	s.mu.Lock()
	clients := len(s.conns)
	s.mu.Unlock()
	s.publish(Event{Kind: EventScrollRequest, Pane: id, Tab: tab, Workspace: ws, Offset: max(offset, 0)})
	return clients, nil
}

// TabActivePane is the pane a tab last had focused, for tab.focus.
func (s *Server) TabActivePane(id session.TabID) (session.PaneID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if t.ID == id {
				return t.ActivePane(), nil
			}
		}
	}
	return 0, session.ErrNoSuchTab
}

// PaneLayout is a pane's tab as herdr's pane.layout describes it, laid out
// at the nominal size: which panes, where, and how the tab is split.
func (s *Server) PaneLayout(id session.PaneID) (tab session.TabID, ws session.WorkspaceID, focused session.PaneID,
	panes []session.PaneRect, splits []session.SplitRect, area session.Rect, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.session.TabOf(id)
	if !ok {
		return 0, 0, 0, nil, nil, area, session.ErrNoSuchPane
	}
	for _, w := range s.session.Workspaces() {
		for _, candidate := range w.Tabs() {
			if candidate.ID == t.ID {
				ws = w.ID
			}
		}
	}
	focused = t.ActivePane()
	if s.focusedTab == t.ID && s.focusedPane != 0 {
		focused = s.focusedPane
	}
	return t.ID, ws, focused, t.Layout(nominalArea), t.Splits(nominalArea), nominalArea, nil
}

// PaneContext is where a pane is and what it is looking at: its tab and
// space, whether it is the pane last focused, the directory it was opened
// in, and the one its program is in now.
type PaneContext struct {
	Tab       session.TabID
	Workspace session.WorkspaceID
	Focused   bool
	Dir       string
	Cwd       string
}

// PaneContext answers for one pane. The live directory is read after the
// lock, since asking the terminal reads /proc.
func (s *Server) PaneContext(id session.PaneID) (PaneContext, error) {
	var out PaneContext
	s.mu.Lock()
	rt, ok := s.runtimes[id]
	if !ok {
		s.mu.Unlock()
		return out, fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if p, ok := t.Pane(id); ok {
				out.Tab, out.Workspace, out.Dir = t.ID, w.ID, p.Dir
			}
		}
	}
	out.Focused = s.focusedPane == id
	s.mu.Unlock()
	out.Cwd = rt.pty.Cwd()
	return out, nil
}
