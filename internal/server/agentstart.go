package server

import (
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/session"
)

// StartAgent starts an agent in a pane that exists: herdr's agent.start,
// which the automation socket answers with this, and which the task
// scheduler (tasks.go) starts a task's agent with. One path, so an agent
// started at a task's time is held to the same policy, named the same way and
// remembered the same way as one a script starts.
//
// label is what the policy is asked about and the refusal is written down
// as; empty is the command's name. name is what to call the agent, empty for
// nothing.
func (s *Server) StartAgent(id session.PaneID, argv []string, label, name string) error {
	if label == "" {
		label = filepath.Base(argv[0])
	}
	// Asked before anything is typed: an agent a policy forbids is one tend
	// does not start, however it was asked to.
	if err := s.AdmitAgentIn(id, label); err != nil {
		return err
	}
	if name != "" {
		// Named before it starts, so a script can address it by name from
		// its first report on.
		if err := s.NameAgent(id, name); err != nil {
			return err
		}
	}
	// Typed into whatever is in the pane, which is how a person starts an
	// agent: the shell runs it, and detection picks it up from the
	// foreground process as it does for any other pane.
	if err := s.Submit(id, strings.Join(agent.LaunchArgv(argv), " ")); err != nil {
		return err
	}
	if known, ok := agent.SessionFromLaunch(argv); ok {
		_ = s.RememberAgentSession(id, known)
	}
	return nil
}
