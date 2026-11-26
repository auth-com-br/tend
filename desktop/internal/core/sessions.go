package core

import (
	"fmt"
	"sort"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The agents' sessions screen: the TUI's sessions list
// (cmd/tend/tui_sessions.go). The server reads the conversations its
// machine's agents keep and says which is open in which pane; this lists
// them, searches them as the query is typed, resumes one in a new tab where
// it was held (or goes to the pane it is open in), deletes the ones marked
// after asking, and hands one on to another agent ("continue in…").

// SessionsState is the screen as QML draws it.
type SessionsState struct {
	Shown   []SessionRow `json:"shown"`
	Total   int          `json:"total"`
	Query   string       `json:"query,omitempty"`
	Marked  int          `json:"marked"`
	Loading bool         `json:"loading,omitempty"`
	Message string       `json:"message,omitempty"`
	// Confirm is the question a delete asks first.
	Confirm string `json:"confirm,omitempty"`
	// Agents are those a conversation can be handed to.
	Agents []string `json:"agents"`
}

// SessionRow is one conversation.
type SessionRow struct {
	ID          string `json:"id"`
	Agent       string `json:"agent"`
	Title       string `json:"title"`
	Dir         string `json:"dir"`
	Age         string `json:"age"`
	Size        string `json:"size"`
	Prompts     int    `json:"prompts"`
	Pane        uint64 `json:"pane,omitempty"`
	Unreachable string `json:"unreachable,omitempty"`
	Marked      bool   `json:"marked,omitempty"`
}

// SessionsAction is what the screen asks for: Do "reload", "query"
// (Text), "mark" (ID), "mark-old", "mark-all", "delete" (ID, or the marked
// ones), "answer" (Yes), "resume" (ID) or "continue" (ID, Agent).
type SessionsAction struct {
	Do    string `json:"do"`
	ID    string `json:"id"`
	Text  string `json:"text"`
	Agent string `json:"agent"`
	Yes   bool   `json:"yes"`
}

type sessionsScreen struct {
	state  SessionsState
	list   []proto.AgentSessionInfo
	all    []ui.SessionEntry
	shown  []ui.SessionEntry
	marked map[string]bool
}

// stateLocked is the screen with its rows as they are now.
func (v *sessionsScreen) stateLocked() SessionsState {
	st := v.state
	now := time.Now()
	st.Shown = make([]SessionRow, 0, len(v.shown))
	for _, e := range v.shown {
		st.Shown = append(st.Shown, SessionRow{
			ID: e.ID, Agent: e.Agent, Title: e.Title, Dir: tools.TildeHome(e.Dir),
			Age: ui.SessionAge(now, e.Modified), Size: ui.SessionSize(e.Size), Prompts: e.Prompts,
			Pane: e.Pane, Unreachable: e.Unreachable, Marked: v.marked[e.ID],
		})
	}
	st.Total = len(v.all)
	st.Marked = len(v.marked)
	st.Agents = tools.ContinueAgents()
	return st
}

// loadSessions reads the list again, keeping the search and what is
// marked; message, when set, is said under it.
func (s *Session) loadSessions(message string) {
	s.mu.Lock()
	c, v := s.client, s.tools.sessions
	s.mu.Unlock()
	if v == nil {
		return
	}
	if !c.Supports(proto.MethodAgentSessions) {
		s.sessionsSay("this server is older than the sessions list; " + tools.HandoffCommand(s.name) + " moves it to this build")
		return
	}
	res, err := c.AgentSessions()
	s.mu.Lock()
	if s.tools.sessions != v {
		s.mu.Unlock()
		return
	}
	v.state.Loading = false
	if err != nil {
		v.state.Message = "could not read the sessions: " + err.Error()
	} else {
		v.list = res.Sessions
		here := map[string]bool{}
		for _, x := range res.Sessions {
			here[x.ID] = true
		}
		for id := range v.marked {
			if !here[id] {
				delete(v.marked, id)
			}
		}
		v.all = tools.SessionEntries(res.Sessions)
		v.shown = ui.FilterSessions(v.all, v.state.Query)
		if message == "" {
			message = tools.SessionsSummary(res.Sessions)
		}
		v.state.Message = message
	}
	s.mu.Unlock()
	s.stateChanged()
}

func (s *Session) sessionsSay(msg string) {
	s.mu.Lock()
	if v := s.tools.sessions; v != nil {
		v.state.Message, v.state.Loading = msg, false
	}
	s.mu.Unlock()
	s.stateChanged()
}

// Sessions does what the screen asks.
func (s *Session) Sessions(act SessionsAction) error {
	s.mu.Lock()
	v, c := s.tools.sessions, s.client
	if v == nil {
		s.mu.Unlock()
		return nil
	}
	var entry ui.SessionEntry
	for _, e := range v.all {
		if e.ID == act.ID {
			entry = e
		}
	}
	s.mu.Unlock()
	switch act.Do {
	case "reload":
		s.loadSessions("")
		return nil
	case "answer":
		return s.answer(act.Yes, func() { v.state.Confirm = "" })
	case "query":
		s.mu.Lock()
		v.state.Query = act.Text
		v.shown = ui.FilterSessions(v.all, act.Text)
		s.mu.Unlock()
	case "mark":
		s.mu.Lock()
		if v.marked[act.ID] {
			delete(v.marked, act.ID)
		} else if act.ID != "" {
			v.marked[act.ID] = true
		}
		s.mu.Unlock()
	case "mark-old", "mark-all":
		var older time.Duration
		if act.Do == "mark-old" {
			older = tools.SessionsOld
		}
		s.mu.Lock()
		v.state.Message = markShown(v, older)
		s.mu.Unlock()
	case "delete":
		s.mu.Lock()
		ids := make([]string, 0, len(v.marked))
		for id := range v.marked {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		if len(ids) == 0 && entry.ID != "" {
			if entry.Pane != 0 {
				v.state.Message = fmt.Sprintf("that session is open in pane %d; close it there first", entry.Pane)
				s.mu.Unlock()
				break
			}
			ids = []string{entry.ID}
		}
		s.mu.Unlock()
		if len(ids) == 0 {
			return nil
		}
		// Asked first: a conversation deleted cannot be resumed, and
		// nothing keeps a copy.
		s.ask(fmt.Sprintf("delete %d session(s)? This cannot be undone.", len(ids)),
			func(q string) { v.state.Confirm = q },
			func() error {
				res, err := c.DeleteAgentSessions(ids)
				s.mu.Lock()
				for _, id := range res.Deleted {
					delete(v.marked, id)
				}
				s.mu.Unlock()
				s.loadSessions(tools.DeleteText(res, err))
				return nil
			})
		return nil
	case "resume":
		if entry.ID == "" {
			return nil
		}
		return s.resumeSession(entry)
	case "continue":
		if entry.ID == "" {
			return nil
		}
		if entry.Agent != "claude" {
			s.sessionsSay("only Claude Code's conversations can be handed on so far")
			return nil
		}
		s.OpenPanel("")
		return s.continueIn(0, entry.ID, act.Agent)
	}
	s.stateChanged()
	return nil
}

// markShown marks what the search shows, those not written to in older
// when it is not zero; when every one of them is marked already, it
// unmarks them instead. One open in a pane is left out: it would only be
// refused. It returns what it did.
func markShown(v *sessionsScreen, older time.Duration) string {
	now := time.Now()
	var pick []string
	for _, e := range v.shown {
		if e.Pane == 0 && (older == 0 || now.Sub(e.Modified) >= older) {
			pick = append(pick, e.ID)
		}
	}
	all := len(pick) > 0
	for _, id := range pick {
		all = all && v.marked[id]
	}
	for _, id := range pick {
		if all {
			delete(v.marked, id)
		} else {
			v.marked[id] = true
		}
	}
	switch {
	case len(pick) == 0 && older > 0:
		return "none of these is 30 days old"
	case all:
		return fmt.Sprintf("unmarked %d", len(pick))
	}
	return fmt.Sprintf("marked %d", len(pick))
}

// resumeSession continues a conversation: in the pane it is open in, when
// one is, and otherwise in a new tab of the space shown, started in the
// directory it was held in — which is where the agent looks for it.
func (s *Session) resumeSession(e ui.SessionEntry) error {
	if e.Pane != 0 {
		s.OpenPanel("")
		return s.JumpToPane(e.Pane)
	}
	spec, name, command, ok := tools.Resume(e)
	switch {
	case !ok:
		s.sessionsSay("tend does not know how to resume a " + e.Agent + " session")
		return nil
	case command != "":
		// There is nowhere else to resume it from: what would, where its
		// directory can be entered, is handed over instead.
		if s.Copied != nil {
			s.Copied(command)
		}
		s.sessionsSay("cannot resume here: " + e.Unreachable + " — copied what resumes it where it can: " + command)
		return nil
	}
	s.mu.Lock()
	c, ws := s.client, s.workspace
	s.mu.Unlock()
	if ws == 0 {
		if err := s.ensurePane(); err != nil {
			return err
		}
		s.mu.Lock()
		ws = s.workspace
		s.mu.Unlock()
	}
	tab, _, err := c.NewTab(ws, name, spec)
	if err != nil {
		s.sessionsSay("could not resume it: " + err.Error())
		return nil
	}
	s.OpenPanel("")
	return s.ShowTab(tab)
}

// continueIn hands a task to agent, as the TUI's continueIn: the one of the
// agent in pane, or, with no pane, the conversation named, started in the
// space shown. The note goes in the context, which opens on it, to be sent
// to the new agent once that is ready.
func (s *Session) continueIn(pane uint64, conversation, agent string) error {
	s.mu.Lock()
	c := s.client
	ws := uint64(0)
	from := ""
	for _, w := range s.snap.Workspaces {
		for _, t := range w.Tabs {
			if hasPane(t, pane) {
				ws = w.ID
			}
		}
	}
	for _, p := range s.snap.Panes {
		if p.ID == pane {
			from = p.Agent
		}
	}
	if pane == 0 {
		ws, from = s.workspace, "claude"
	}
	s.mu.Unlock()
	if _, ok := tools.AgentArgv[agent]; !ok {
		return fmt.Errorf("tend does not know how to start %s", agent)
	}
	if !c.Supports(proto.MethodAgentSessionHandoff) {
		return fmt.Errorf("this server is older than handing tasks on; %s moves it to this build", tools.HandoffCommand(s.name))
	}
	if ws == 0 {
		return nil
	}
	if from == "" {
		from = "-"
	}
	s.status("reading what " + from + " was doing…")
	h, err := c.AgentHandoff(pane, conversation)
	if err != nil {
		return err
	}
	if _, err := c.ContextAdd(tools.HandoffItem(h)); err != nil {
		return fmt.Errorf("could not put the note in the context: %w", err)
	}
	spec, _ := tools.ContinueSpec(agent, h, pane)
	tab, _, err := c.NewTab(ws, agent+" ← "+h.Agent, spec)
	if err != nil {
		return fmt.Errorf("%s did not start: %w", agent, err)
	}
	if err := s.refreshWith(func() { s.workspace, s.tab, s.focus = ws, tab, 0 }); err != nil {
		return err
	}
	s.OpenPanel(panelContext)
	s.status("the note is first in the context: once " + agent + " is ready, send it from there")
	return nil
}
