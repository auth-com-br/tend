package core

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
)

// The agent manager screen: the TUI's (cmd/tend/tui_agents.go). The server
// says which agent CLIs and memory tools its machine has, at which version,
// and how the rest install; installing one runs its command in a tab of
// its own in the space shown, as the TUI does — where it is watched,
// answered and stopped — which then goes on as a shell.

// AgentsState is the screen as QML draws it.
type AgentsState struct {
	Agents  []AgentRow `json:"agents"`
	Loading bool       `json:"loading,omitempty"`
	Message string     `json:"message,omitempty"`
	// Confirm is the agent whose install is being asked about.
	Confirm string `json:"confirm,omitempty"`
}

// AgentRow is one agent or memory tool.
type AgentRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	Path        string `json:"path,omitempty"`
	Install     string `json:"install,omitempty"`
	Missing     string `json:"missing,omitempty"`
	Memory      bool   `json:"memory,omitempty"`
	Running     bool   `json:"running,omitempty"`
}

// AgentsAction is what the screen asks for: Do "reload", "choose" (ID:
// say where it is, or ask to install it), "install" (ID) or "cancel".
type AgentsAction struct {
	Do string `json:"do"`
	ID string `json:"id"`
}

type agentsScreen struct {
	state AgentsState
	list  []proto.AgentStatus
}

// loadAgentsScreen reads the catalog, in the order the TUI lists it. The
// new-tab menu's list is kept with it, so an agent just installed is
// offered there too.
func (s *Session) loadAgentsScreen() {
	s.mu.Lock()
	c, a := s.client, s.tools.agents
	s.mu.Unlock()
	if a == nil {
		return
	}
	if !c.Supports(proto.MethodAgentsCatalog) {
		s.mu.Lock()
		a.state.Loading = false
		a.state.Message = "this server is older than the agent manager; " + tools.HandoffCommand(s.name) + " moves it to this build"
		s.mu.Unlock()
		s.stateChanged()
		return
	}
	res, err := c.AgentsCatalog()
	s.mu.Lock()
	if s.tools.agents != a {
		s.mu.Unlock()
		return
	}
	a.state.Loading, a.state.Confirm = false, ""
	if err != nil {
		a.state.Message = "could not list the agents: " + err.Error()
	} else {
		s.catalog = append([]proto.AgentStatus(nil), res.Agents...)
		list := res.Agents
		tools.SortCatalog(list)
		a.list = list
		a.state.Agents = nil
		for _, x := range list {
			a.state.Agents = append(a.state.Agents, AgentRow{
				ID: x.ID, Name: x.Name, Description: x.Description, Installed: x.Installed,
				Version: x.Version, Path: x.Path, Install: x.InstallCommand, Missing: x.Missing,
				Memory: x.Kind == "memory", Running: x.Running,
			})
		}
		a.state.Message = tools.CatalogSummary(list)
	}
	s.mu.Unlock()
	s.stateChanged()
}

// AgentsScreen does what the screen asks.
func (s *Session) AgentsScreen(act AgentsAction) error {
	s.mu.Lock()
	a := s.tools.agents
	if a == nil {
		s.mu.Unlock()
		return nil
	}
	var agent proto.AgentStatus
	for _, x := range a.list {
		if x.ID == act.ID {
			agent = x
		}
	}
	s.mu.Unlock()
	switch act.Do {
	case "reload":
		s.mu.Lock()
		a.state.Loading = true
		s.mu.Unlock()
		s.stateChanged()
		s.loadAgentsScreen()
	case "cancel":
		s.mu.Lock()
		a.state.Confirm = ""
		s.mu.Unlock()
		s.stateChanged()
	case "choose":
		if agent.ID == "" {
			return nil
		}
		msg, install := tools.AgentEnter(agent)
		s.mu.Lock()
		if install {
			a.state.Confirm = agent.ID
		} else {
			a.state.Message, a.state.Confirm = msg, ""
		}
		s.mu.Unlock()
		s.stateChanged()
	case "install":
		if agent.ID == "" || agent.InstallCommand == "" {
			return nil
		}
		return s.installAgent(agent)
	}
	return nil
}

// installAgent runs an agent's install in a new tab of the space shown and
// goes there, taking the screen down.
func (s *Session) installAgent(a proto.AgentStatus) error {
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
	tab, _, err := c.NewTab(ws, "install "+a.ID, tools.InstallSpec(a))
	if err != nil {
		s.mu.Lock()
		if sc := s.tools.agents; sc != nil {
			sc.state.Confirm = ""
			sc.state.Message = "could not start the install: " + err.Error()
		}
		s.mu.Unlock()
		s.stateChanged()
		return nil
	}
	s.OpenPanel("")
	return s.ShowTab(tab)
}
