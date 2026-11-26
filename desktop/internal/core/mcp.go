package core

import (
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The MCP servers screen: the TUI's MCP manager (cmd/tend/tui_mcp.go) as a
// window's grid — a row a server, a column an agent — read, switched,
// copied, added, removed and tested by the session's server, where the
// agents and their files are (internal/mcp). The grid and what each action
// says are internal/tools', the TUI's own.

// MCPState is the screen as QML draws it.
type MCPState struct {
	Agents  []string      `json:"agents"`
	Rows    []MCPRowState `json:"rows"`
	Loading bool          `json:"loading,omitempty"`
	Message string        `json:"message,omitempty"`
	Alert   bool          `json:"alert,omitempty"`
	Confirm string        `json:"confirm,omitempty"`
	// Detail, when set, is a row's servers agent by agent, or the help.
	Detail []string `json:"detail,omitempty"`
	Legend string   `json:"legend"`
}

// MCPRowState is one server across the agents: each cell "on", "off" or ""
// (the agent does not have it).
type MCPRowState struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Cells     []string `json:"cells"`
	Differs   []bool   `json:"differs"`
	Test      string   `json:"test,omitempty"`
	TestState string   `json:"testState,omitempty"`
}

// MCPAction is what the screen asks for: Do one of "reload", "toggle",
// "remove", "test", "test-all", "detail", "help", "add" and "answer", on
// Row, in the agent of column Agent (-1 is every agent: the name's column).
type MCPAction struct {
	Do    string `json:"do"`
	Row   int    `json:"row"`
	Agent int    `json:"agent"`
	Text  string `json:"text"`
	Yes   bool   `json:"yes"`
}

// mcpScreen is the screen's state and what it was drawn from.
type mcpScreen struct {
	state   MCPState
	list    proto.MCPListResult
	cols    []string
	keys    []string
	entries map[string]map[string]proto.MCPEntry
	// tests is each row's last result, kept across reloads.
	tests map[string][2]string
}

var mcpCells = map[ui.MCPCell]string{ui.MCPNone: "", ui.MCPOn: "on", ui.MCPOff: "off"}

// loadMCP reads the servers again; message, when set, is said once it is.
func (s *Session) loadMCP(message string) {
	s.mu.Lock()
	c, m := s.client, s.tools.mcp
	s.mu.Unlock()
	if m == nil {
		return
	}
	if !c.Supports(proto.MethodMCPList) {
		s.mcpSay("this server is older than the MCP manager; "+tools.HandoffCommand(s.name)+" moves it to this build", true)
		return
	}
	list, err := c.MCPList()
	s.mu.Lock()
	if s.tools.mcp != m {
		s.mu.Unlock()
		return // closed while it read
	}
	m.state.Loading = false
	if err != nil {
		m.state.Message, m.state.Alert = "could not read the agents' servers: "+tools.ErrorText(err), true
	} else {
		var rows []ui.MCPRow
		m.list = list
		m.state.Agents, m.cols, rows, m.keys, m.entries = tools.MCPGrid(list)
		m.state.Rows = nil
		for i, r := range rows {
			row := MCPRowState{Name: r.Name, Transport: r.Transport, Differs: r.Differs}
			for _, cell := range r.Cells {
				row.Cells = append(row.Cells, mcpCells[cell])
			}
			if t, ok := m.tests[m.keys[i]]; ok {
				row.Test, row.TestState = t[0], t[1]
			}
			m.state.Rows = append(m.state.Rows, row)
		}
		m.state.Message, m.state.Alert = message, false
		if message == "" {
			m.state.Message = tools.MCPSummary(list)
		}
	}
	s.mu.Unlock()
	s.stateChanged()
}

func (s *Session) mcpSay(msg string, alert bool) {
	s.mu.Lock()
	if m := s.tools.mcp; m != nil {
		m.state.Message, m.state.Alert, m.state.Loading = msg, alert, false
	}
	s.mu.Unlock()
	s.stateChanged()
}

// MCP does what the screen asks.
func (s *Session) MCP(a MCPAction) error {
	s.mu.Lock()
	m, c := s.tools.mcp, s.client
	if m == nil {
		s.mu.Unlock()
		return nil
	}
	var key, agent string
	if a.Row >= 0 && a.Row < len(m.keys) {
		key = m.keys[a.Row]
	}
	if a.Agent >= 0 && a.Agent < len(m.cols) {
		agent = m.cols[a.Agent]
	}
	s.mu.Unlock()

	switch a.Do {
	case "reload":
		s.mu.Lock()
		m.state.Loading, m.state.Detail = true, nil
		s.mu.Unlock()
		s.stateChanged()
		s.loadMCP("")
	case "answer":
		return s.answer(a.Yes, func() { m.state.Confirm = "" })
	case "help":
		s.mu.Lock()
		m.state.Detail = helpBefore(ui.MCPHelp, "WHAT YOU CAN DO")
		s.mu.Unlock()
		s.stateChanged()
	case "detail":
		if key == "" {
			return nil
		}
		s.mu.Lock()
		m.state.Detail = tools.MCPDetail(m.list, m.cols, m.entries[key], m.tests[key][0])
		s.mu.Unlock()
		s.stateChanged()
	case "toggle":
		if key == "" || agent == "" {
			return nil
		}
		return s.mcpToggle(m, key, agent)
	case "remove":
		if key == "" {
			return nil
		}
		s.mcpRemove(m, key, agent)
	case "test":
		if key != "" {
			s.mcpTestRow(m, a.Row, agent)
		}
	case "test-all":
		s.mu.Lock()
		n := len(m.keys)
		s.mu.Unlock()
		for i := 0; i < n; i++ {
			s.mcpTestRow(m, i, "")
		}
	case "add":
		name, def, err := tools.ParseMCPSpec(a.Text)
		if err != nil {
			s.mcpSay(err.Error(), true)
			return nil
		}
		s.mu.Lock()
		targets := []string{agent}
		if agent == "" {
			targets = append([]string(nil), m.cols...)
		}
		s.mu.Unlock()
		s.ask("add "+name+" ("+def.Transport+") to "+strings.Join(targets, ", ")+"?",
			func(q string) { m.state.Confirm = q },
			func() error {
				res, err := c.MCPAdd(proto.MCPAddParams{Name: name, Agents: targets, Def: &def})
				if err != nil {
					s.mcpSay(tools.ErrorText(err), true)
					return nil
				}
				s.loadMCP(tools.MCPWriteText("added "+name+" to", res))
				return nil
			})
	}
	return nil
}

// mcpToggle is a click on a cell, as space is in the TUI: an agent that has
// the server turns it off or on; one that has not is offered a copy from
// one that has.
func (s *Session) mcpToggle(m *mcpScreen, key, agent string) error {
	s.mu.Lock()
	c := s.client
	e, has := m.entries[key][agent]
	var from proto.MCPEntry
	for _, id := range m.cols {
		if o, ok := m.entries[key][id]; ok && o.Agent != agent {
			from = o
			break
		}
	}
	s.mu.Unlock()
	switch {
	case has:
		on := !e.Enabled
		if e.Reason != "" && on {
			s.mcpSay(e.Reason+"; tend does not change that", true)
			return nil
		}
		if err := c.MCPSetEnabled(agent, e.Name, on); err != nil {
			s.mcpSay(tools.ErrorText(err), true)
			return nil
		}
		state := "off"
		if on {
			state = "on"
		}
		s.loadMCP(e.Name + " is " + state + " in " + agent + " — it takes effect in " + agent + "'s next session")
	case from.Name != "":
		s.ask("copy "+from.Name+" from "+from.Agent+" into "+agent+"?",
			func(q string) { m.state.Confirm = q },
			func() error {
				res, err := c.MCPAdd(proto.MCPAddParams{Name: from.Name, Agents: []string{agent}, From: &proto.MCPRef{Agent: from.Agent, Name: from.Name}})
				if err != nil {
					s.mcpSay(tools.ErrorText(err), true)
					return nil
				}
				s.loadMCP(tools.MCPWriteText("copied "+from.Name+" into", res))
				return nil
			})
	}
	return nil
}

// mcpRemove asks, naming the agents, and takes the server out of agent's
// file, or out of every agent's with none.
func (s *Session) mcpRemove(m *mcpScreen, key, agent string) {
	s.mu.Lock()
	c := s.client
	var agents []string
	name := ""
	for _, id := range m.cols {
		if e, has := m.entries[key][id]; has && (agent == "" || id == agent) {
			agents, name = append(agents, id), e.Name
		}
	}
	s.mu.Unlock()
	if len(agents) == 0 {
		s.mcpSay(agent+" does not have this server", false)
		return
	}
	s.ask("remove "+name+" from "+strings.Join(agents, ", ")+"? (a backup of each file is kept)",
		func(q string) { m.state.Confirm = q },
		func() error {
			res, err := c.MCPRemove(name, agents)
			if err != nil {
				s.mcpSay(tools.ErrorText(err), true)
				return nil
			}
			s.loadMCP(tools.MCPWriteText("removed "+name+" from", res))
			return nil
		})
}

// mcpTestRow tests a row: agent's server, or each distinct server of the
// row with no agent. The result shows on the row as it comes.
func (s *Session) mcpTestRow(m *mcpScreen, row int, agent string) {
	s.mu.Lock()
	c := s.client
	if row < 0 || row >= len(m.keys) {
		s.mu.Unlock()
		return
	}
	key := m.keys[row]
	var targets []proto.MCPEntry
	seen := map[string]bool{}
	for _, id := range m.cols {
		e, ok := m.entries[key][id]
		if !ok || (agent != "" && id != agent) || seen[e.Fingerprint] {
			continue
		}
		seen[e.Fingerprint] = true
		targets = append(targets, e)
	}
	if len(targets) == 0 {
		s.mu.Unlock()
		return
	}
	m.state.Rows[row].Test, m.state.Rows[row].TestState = "testing…", "running"
	s.mu.Unlock()
	s.stateChanged()
	go func() {
		var texts []string
		state := "ok"
		for _, e := range targets {
			r, err := c.MCPTest(e.Agent, e.Name)
			text := tools.MCPResultText(r, err)
			if len(targets) > 1 {
				text = e.Agent + ": " + text
			}
			texts = append(texts, text)
			switch {
			case err != nil || r.Status == "error" || r.Status == "timeout":
				state = "bad"
			case r.Status == "auth" && state == "ok":
				state = "auth"
			}
		}
		s.mu.Lock()
		m.tests[key] = [2]string{strings.Join(texts, " · "), state}
		for i, k := range m.keys {
			if k == key && i < len(m.state.Rows) {
				m.state.Rows[i].Test, m.state.Rows[i].TestState = m.tests[key][0], state
			}
		}
		s.mu.Unlock()
		s.stateChanged()
	}()
}
