package main

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// Starting an agent from a group's menu: "new agent here..." lists the
// agents installed on the server's machine — the agent manager's catalog,
// found the same way — and the one picked starts in a new space of the
// group, in the group's folder. When the agent exits the terminal stays, a
// shell in the same folder, as a tab handed a task does (resumeScript).

// openGroupAgentMenu puts up the list of agents to start in a group, where
// its menu was. The catalog asks each agent for its version, which takes a
// moment, so the list the manager last read is used when there is one.
func (t *tui) openGroupAgentMenu(group string, x, y int) {
	if !t.client.Supports(proto.MethodAgentsCatalog) {
		t.setMessage("this server is older than the agent list; "+handoffCommand(t.session)+" moves it to this build", true)
		return
	}
	t.mu.Lock()
	list := t.agentStatus
	t.mu.Unlock()
	if list == nil {
		t.setMessage("looking for the agents installed here…", false)
		res, err := t.client.AgentsCatalog()
		if err != nil {
			t.setMessage("could not list the agents: "+refusalText(err), true)
			return
		}
		list = res.Agents
		t.mu.Lock()
		t.agentStatus = list
		t.mu.Unlock()
		t.setMessage("", false)
	}
	choices := installedAgents(list)
	if len(choices) == 0 {
		t.setMessage("no agent is installed here; ctrl+b A installs one", true)
		return
	}
	t.openMenu(ui.GroupAgentMenu(group, choices, x, y))
}

// installedAgents are the agents a group can start: installed, and agents
// rather than the memory tools the catalog lists with them.
func installedAgents(list []proto.AgentStatus) []ui.AgentChoice {
	var out []ui.AgentChoice
	for _, a := range list {
		if a.Installed && a.Kind != "memory" {
			out = append(out, ui.AgentChoice{ID: a.ID, Name: a.Name})
		}
	}
	return out
}

// startAgentInGroup starts an agent in a new space of a group, named after
// the agent, in the group's folder.
func (t *tui) startAgentInGroup(group, id string) error {
	t.mu.Lock()
	var agent proto.AgentStatus
	for _, a := range t.agentStatus {
		if a.ID == id {
			agent = a
		}
	}
	t.mu.Unlock()
	if agent.ID == "" {
		return nil
	}
	// The path the catalog found it at: the program a pane runs is the
	// server machine's, and a name looked up again could find another.
	program := agent.Path
	if program == "" {
		program = agent.ID
	}
	err := t.newWorkspaceRunning(group, agent.Name, agent.Name, func(dir string) proto.PaneSpec {
		return proto.PaneSpec{
			Command: []string{"/bin/sh", "-c", resumeScript, "tend-agent", program},
			Dir:     dir,
			Agent:   agent.ID,
		}
	})
	if err != nil {
		t.setMessage(agent.Name+" did not start: "+refusalText(err), true)
	}
	return nil
}
