package core

import (
	"fmt"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The global menu's tools as the window's screens: the MCP servers, the
// agents' permissions, the agent manager, the agents' sessions, the
// context buffer, about and what's new. Each is a panel (s.panel is its
// name) whose state is kept here while it is open and read again from the
// server when it opens; what it lists and what its actions say are
// internal/tools', the TUI's own, so the two clients offer the same things.
//
// All of it is this window's: what one person is looking at. Nothing here
// is sent but the actions themselves.

// The tools' panels, by name. A tool of the "all tools" menu opens the
// panel of its id; the others are opened from the global menu.
const (
	panelMCP      = ui.ToolMCP
	panelPerm     = ui.ToolPerms
	panelAgents   = ui.ToolAgents
	panelSessions = ui.ToolSessions
	panelContext  = ui.ToolContext
	panelAbout    = "about"
	panelNotes    = "notes"
)

// toolPanels are the tools of the "all tools" menu the window has a
// screen for: the others (the files panel, issues, errors, tickets, the
// browser) are not offered there until theirs exist.
var toolPanels = map[string]bool{
	panelMCP:      true,
	panelPerm:     true,
	panelAgents:   true,
	panelSessions: true,
	panelContext:  true,
}

// toolScreens are the screens' states; only the open one is not nil.
type toolScreens struct {
	mcp      *mcpScreen
	perm     *permScreen
	agents   *agentsScreen
	sessions *sessionsScreen
	context  *contextScreen
	notes    *NotesState
	phone    *PhoneState
	// pending is what a "yes" to the open screen's question does.
	pending func() error
}

// panelOpened sets up a tool's screen as its panel opens, and starts
// reading what it shows; the screens of other panels are let go.
func (s *Session) panelOpened(name string) {
	s.mu.Lock()
	pane := s.focus
	s.tools = toolScreens{}
	var load func()
	switch name {
	case panelMCP:
		s.tools.mcp = &mcpScreen{state: MCPState{Loading: true, Legend: ui.MCPLegend}, tests: map[string][2]string{}}
		load = func() { s.loadMCP("") }
	case panelPerm:
		s.tools.perm = &permScreen{state: PermState{Loading: true}, pane: pane}
		load = func() { s.loadPerm("") }
	case panelAgents:
		s.tools.agents = &agentsScreen{state: AgentsState{Loading: true}}
		load = s.loadAgentsScreen
	case panelSessions:
		s.tools.sessions = &sessionsScreen{state: SessionsState{Loading: true}, marked: map[string]bool{}}
		load = func() { s.loadSessions("") }
	case panelContext:
		s.tools.context = &contextScreen{state: ContextState{Loading: true}}
		load = s.loadContext
	case panelNotes:
		load = s.loadNotes
	case panelPhone:
		s.tools.phone = &PhoneState{}
		load = func() { s.phoneCall(proto.MethodWebStatus, "") }
	}
	s.mu.Unlock()
	if load != nil {
		go load()
	}
}

// toolMenu is the "all tools" menu: the tools the window has a screen for.
func toolMenu(x, y int) ui.Menu {
	return ui.ToolsMenu(tools.ToolChoices(func(id string) bool { return toolPanels[id] }), x, y)
}

// openTool opens a tool's panel by its id.
func (s *Session) openTool(id string) error {
	if !toolPanels[id] {
		return fmt.Errorf("%s is not in the window yet", id)
	}
	s.OpenPanel(id)
	return nil
}

// toolStateLocked fills the open tool's screen into what QML is given. The
// caller holds the lock. A list a screen changes is replaced, never written
// into, but for the MCP rows, which are copied here.
func (s *Session) toolStateLocked(st *State) {
	t := &s.tools
	switch {
	case s.panel == panelMCP && t.mcp != nil:
		// The rows are copied: a test's result is written into them in
		// place, and the state is turned into JSON after the lock is let go.
		m := t.mcp.state
		m.Rows = append([]MCPRowState(nil), m.Rows...)
		st.MCP = &m
	case s.panel == panelPerm && t.perm != nil:
		p := t.perm.state
		st.Perm = &p
	case s.panel == panelAgents && t.agents != nil:
		a := t.agents.state
		st.AgentsScreen = &a
	case s.panel == panelSessions && t.sessions != nil:
		v := t.sessions.stateLocked()
		st.Sessions = &v
	case s.panel == panelContext && t.context != nil:
		c := t.context.state
		st.Context = &c
	case s.panel == panelAbout:
		st.About = s.aboutLocked()
	case s.panel == panelNotes && t.notes != nil:
		n := *t.notes
		st.Notes = &n
	case s.panel == panelPhone && t.phone != nil:
		p := *t.phone
		p.URLs = append([]string(nil), p.URLs...)
		st.Phone = &p
	}
}

// ask puts a question on the open screen, with what "yes" does.
func (s *Session) ask(question string, set func(string), do func() error) {
	s.mu.Lock()
	set(question)
	s.tools.pending = do
	s.mu.Unlock()
	s.stateChanged()
}

// answer is the open screen's question answered: yes does what was asked.
func (s *Session) answer(yes bool, clear func()) error {
	s.mu.Lock()
	do := s.tools.pending
	s.tools.pending = nil
	clear()
	s.mu.Unlock()
	s.stateChanged()
	if yes && do != nil {
		return do()
	}
	return nil
}
