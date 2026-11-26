package core

import (
	"fmt"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/workon"
)

// What the issues, errors and tickets tabs share: the pane each was opened
// from, and handing a piece of work to an agent — typed into that pane, or
// started in a worktree of its own (internal/workon, the TUI's same code).

// paneCwd is where a pane is working now, as the automation socket's
// pane.get says: the TUI's errors and tickets panels open on the project
// folder of the pane they were opened from, found the same way.
func (s *Session) paneCwd(pane uint64) string {
	if pane == 0 {
		return ""
	}
	res, err := s.apiCall("pane.get", map[string]any{"pane_id": fmt.Sprintf("p_%d", pane)})
	if err != nil {
		return ""
	}
	p, _ := res["pane"].(map[string]any)
	for _, k := range []string{"foreground_cwd", "cwd"} {
		if d, _ := p[k].(string); d != "" {
			return d
		}
	}
	return ""
}

// agentForLocked is the agent work is started in, from a tab opened from
// pane (workon.AgentFor). The caller holds the lock.
func (s *Session) agentForLocked(pane uint64) string {
	paneAgent := ""
	for _, p := range s.snap.Panes {
		if p.ID == pane {
			paneAgent = p.Agent
		}
	}
	return workon.AgentFor(s.cfg.Issues.Agent, paneAgent)
}

// shownSpaceLocked is the space the window shows, which a fix's worktree is
// made from, as the TUI's shownWorkspace. The caller holds the lock.
func (s *Session) shownSpaceLocked() uint64 {
	if s.workspace != 0 {
		return s.workspace
	}
	if s.snap.ActiveWorkspace != 0 {
		return s.snap.ActiveWorkspace
	}
	if len(s.snap.Workspaces) > 0 {
		return s.snap.Workspaces[0].ID
	}
	return 0
}

// apiCaller is the automation socket as workon calls it.
func (s *Session) apiCaller() workon.Caller { return s.apiCall }

// goToStarted shows where work was started or found: the worktree's space,
// and the agent's tab in it (zero is the space's own tab).
func (s *Session) goToStarted(got workon.Started) error {
	err := s.refreshWith(func() {
		s.workspace, s.tab, s.focus, s.zoom = got.Workspace, got.Tab, 0, 0
		// The dock took the window there, to a worktree of the project it
		// shows: it is not a space the person went to, and the dock goes on
		// showing what it showed — the issue, error or ticket open — rather
		// than reading the space anew (followSpaceLocked).
		s.dock.space = got.Workspace
	})
	s.status(got.Message)
	return err
}

// handToPane types what an agent is to fix into a pane through the
// context, which makes it inert and submits nothing — the user reads it
// and sends it — and shows the pane, as the TUI's errors panel does.
func (s *Session) handToPane(pane uint64, item proto.ContextItem, what string) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if pane == 0 {
		return fmt.Errorf("no pane to hand it to: open the tab from the agent's pane")
	}
	added, err := c.ContextAdd(item)
	if err == nil {
		err = c.ContextSend(pane, []uint64{added.ID})
	}
	if err != nil {
		return fmt.Errorf("could not hand it over: %w", err)
	}
	if err := s.JumpToPane(pane); err != nil {
		return err
	}
	s.status(what + " typed into the pane — read it over and press enter to send")
	return nil
}

// ago is how long ago a time was, as the TUI's lists say it.
func ago(then time.Time) string {
	if then.IsZero() {
		return ""
	}
	return ui.SessionAge(time.Now(), then)
}
