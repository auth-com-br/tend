package core

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/sidebar"
	"github.com/auth-com-br/tend/internal/ui"
)

// Menus are the TUI's: the same builders in internal/ui (SpaceMenu,
// PaneMenu, WithSleep…) and the same decorations by what the server says
// about a pane, so a right click offers the same things in both clients.
// What an item does is here, with the client protocol and, for worktrees,
// the automation socket, as cmd/tend's runMenu does it.
//
// A menu, and a prompt one of its items asks for, are this window's: what
// one person is doing, never sent.

// Prompt is a line of text the window asks for: a new name, a group's name,
// a branch.
type Prompt struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Text  string `json:"text"`
	// What it is about.
	Pane      uint64 `json:"-"`
	Tab       uint64 `json:"-"`
	Workspace uint64 `json:"-"`
	Group     string `json:"-"`
}

// Prompt kinds.
const (
	promptRenameTab   = "rename-tab"
	promptRenamePane  = "rename-pane"
	promptRenameSpace = "rename-space"
	promptRenameGroup = "rename-group"
	promptNewGroup    = "new-group"
	promptNewWorktree = "new-worktree"
	promptGroupDir    = "group-dir"
)

// notHere are menu items the window does not offer yet, rather than offer
// and do nothing: continuing a conversation in another agent comes with
// the agent sessions (phase 4), the files panel natively (phase 3), and the
// global menu's screens with phase 2's settings and phase 4's panels.
var notHere = map[string]bool{
	ui.MenuContinueIn:  true,
	ui.MenuFiles:       true,
	ui.MenuKeybinds:    true,
	ui.MenuDetach:      true,
	ui.MenuSettings:    true,
	ui.MenuMCP:         true,
	ui.MenuPermissions: true,
	ui.MenuTools:       true,
	ui.MenuPhone:       true,
	ui.MenuAbout:       true,
	ui.MenuWhatsNew:    true,
}

func withoutMissing(m ui.Menu) ui.Menu {
	items := m.Items[:0:0]
	for _, it := range m.Items {
		if !notHere[it.Action] {
			items = append(items, it)
		}
	}
	m.Items = items
	return m
}

// OpenMenu opens the menu for something in the window: kind is "space",
// "group", "agent", "tab", "pane" or "global", with what it is about.
func (s *Session) OpenMenu(kind string, pane, tab, ws uint64, group string, x, y int) {
	s.mu.Lock()
	m, ok := s.menuForLocked(kind, pane, tab, ws, group, x, y)
	if ok {
		mm := withoutMissing(m)
		s.menu = &mm
	} else {
		s.menu = nil
	}
	s.mu.Unlock()
	s.stateChanged()
}

func (s *Session) menuForLocked(kind string, pane, tab, ws uint64, group string, x, y int) (ui.Menu, bool) {
	info := sidebar.PaneIndex(&s.snap)
	groups := len(sidebar.GroupsIn(&s.snap)) > 0
	decorate := func(m ui.Menu, pane uint64) ui.Menu {
		p := info[pane]
		if p.Agent == "" {
			return m
		}
		m = ui.WithMarkUnread(m, p.Marked)
		// As the TUI's limitedUsage: a limit reached that says when it resets.
		if u := p.Usage; u != nil && u.Limit != nil && u.Limit.Reached && u.Limit.Resets != 0 {
			m = ui.WithResumeAfterLimit(m, u.ResumeArmed)
		}
		m = ui.WithMute(m, p.Muted)
		return ui.WithSleep(m, p.Dormant)
	}
	switch kind {
	case "space":
		return ui.SpaceMenu(ws, groups, x, y), true
	case "group":
		dir := ""
		for _, g := range s.snap.Groups {
			if g.Name == group {
				dir = g.Dir
			}
		}
		return ui.GroupMenu(group, s.folded[group], tildeHome(dir), len(s.snap.Companies) > 0, x, y), true
	case "agent":
		return decorate(ui.AgentMenu(pane, tab, ws, x, y), pane), true
	case "tab":
		return ui.TabMenu(tab, x, y), true
	case "pane":
		// The last pane of a tab has no "close pane": closing it would leave
		// an empty tab, and "close tab" is the honest name for that.
		pv := s.panes[pane]
		selected := pv != nil && pv.sel.done
		m := ui.PaneMenu(pane, x, y, len(s.rects) > 1, info[pane].Agent != "", selected)
		return decorate(m, pane), true
	case "global":
		return ui.GlobalMenu(false, s.snap.Update != nil && s.snap.Update.Ready != "", false, x, y), true
	}
	return ui.Menu{}, false
}

// CloseMenu puts the menu away.
func (s *Session) CloseMenu() {
	s.mu.Lock()
	had := s.menu != nil
	s.menu = nil
	s.mu.Unlock()
	if had {
		s.stateChanged()
	}
}

// PickMenu runs the menu's item at index i.
func (s *Session) PickMenu(i int) error {
	s.mu.Lock()
	m := s.menu
	s.menu = nil
	s.mu.Unlock()
	s.stateChanged()
	if m == nil || i < 0 || i >= len(m.Items) {
		return nil
	}
	return s.runMenu(*m, m.Items[i])
}

func (s *Session) setPrompt(p *Prompt) {
	s.mu.Lock()
	s.prompt = p
	s.mu.Unlock()
	s.stateChanged()
}

func (s *Session) openSubmenu(m ui.Menu) {
	s.mu.Lock()
	mm := withoutMissing(m)
	s.menu = &mm
	s.mu.Unlock()
	s.stateChanged()
}

// runMenu is what an item does: cmd/tend's runMenu, item for item, for the
// items the window offers.
func (s *Session) runMenu(m ui.Menu, item ui.MenuItem) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	switch item.Action {
	case ui.MenuGoTo:
		return s.JumpToPane(m.Pane)
	case ui.MenuNewSpace:
		if m.Group != "" {
			return s.newWorkspaceIn(m.Group)
		}
		return s.NewWorkspace()
	case ui.MenuNewTab:
		if m.Workspace != 0 {
			if err := s.ShowWorkspace(m.Workspace); err != nil {
				return err
			}
		}
		return s.NewTab()
	case ui.MenuSplitRight, ui.MenuSplitDown:
		dir := "columns"
		if item.Action == ui.MenuSplitDown {
			dir = "rows"
		}
		if err := s.Focus(m.Pane); err != nil {
			return err
		}
		return s.Split(dir)
	case ui.MenuZoom:
		return s.ToggleZoom(m.Pane)
	case ui.MenuCopy:
		s.mu.Lock()
		text := s.lastCopy[m.Pane]
		s.mu.Unlock()
		if text != "" && s.Copied != nil {
			s.Copied(text)
		}
		return nil
	case ui.MenuPaste:
		if s.ClipboardText == nil {
			return nil
		}
		return s.Paste(m.Pane, s.ClipboardText())
	case ui.MenuRename:
		return s.renameFor(m)
	case ui.MenuRenamePane:
		name := ""
		for _, p := range s.snap.Panes {
			if p.ID == m.Pane && p.Named {
				name = p.Title
			}
		}
		s.setPrompt(&Prompt{Kind: promptRenamePane, Title: "rename pane", Text: name, Pane: m.Pane})
		return nil
	case ui.MenuGroup:
		s.setPrompt(&Prompt{Kind: promptNewGroup, Title: "new group for this space", Workspace: m.Workspace})
		return nil
	case ui.MenuMoveToGroup:
		s.mu.Lock()
		current := ""
		var names []string
		for _, w := range s.snap.Workspaces {
			if w.ID == m.Workspace {
				current = w.Group
			}
		}
		for _, g := range sidebar.GroupsIn(&s.snap) {
			names = append(names, g.Name)
		}
		s.mu.Unlock()
		s.openSubmenu(ui.GroupPickMenu(m.Workspace, current, names, m.X, m.Y))
		return nil
	case ui.MenuPickGroup:
		return c.GroupWorkspace(m.Workspace, item.Arg)
	case ui.MenuFold:
		s.ToggleGroup(m.Group)
		return nil
	case ui.MenuGroupDir:
		s.mu.Lock()
		dir := ""
		for _, g := range s.snap.Groups {
			if g.Name == m.Group {
				dir = g.Dir
			}
		}
		s.mu.Unlock()
		s.setPrompt(&Prompt{Kind: promptGroupDir, Title: "folder for " + m.Group, Text: dir, Group: m.Group})
		return nil
	case ui.MenuGroupDirClear:
		return c.SetGroupDir(m.Group, "")
	case ui.MenuGroupAgent:
		s.mu.Lock()
		var choices []ui.AgentChoice
		for _, a := range s.catalog {
			if a.Installed && a.Kind != "memory" {
				choices = append(choices, ui.AgentChoice{ID: a.ID, Name: a.Name})
			}
		}
		s.mu.Unlock()
		if len(choices) == 0 {
			return fmt.Errorf("no agent is installed here")
		}
		s.openSubmenu(ui.GroupAgentMenu(m.Group, choices, m.X, m.Y))
		return nil
	case ui.MenuGroupAgentStart:
		return s.startAgentInGroup(m.Group, item.Arg)
	case ui.MenuMoveToCompany:
		s.mu.Lock()
		var choices []ui.CompanyChoice
		for _, co := range s.snap.Companies {
			if co.ID != s.company {
				choices = append(choices, ui.CompanyChoice{ID: strconv.FormatUint(co.ID, 10), Name: co.Name})
			}
		}
		s.mu.Unlock()
		if len(choices) == 0 {
			return fmt.Errorf("no other company to move it to")
		}
		s.openSubmenu(ui.CompanyPickMenu(m.Group, "", choices, m.X, m.Y))
		return nil
	case ui.MenuPickCompany:
		id, err := strconv.ParseUint(item.Arg, 10, 64)
		if err != nil {
			return err
		}
		s.mu.Lock()
		from := s.company
		s.mu.Unlock()
		return c.MoveGroupToCompany(m.Group, from, id)
	case ui.MenuMoveBack, ui.MenuMoveOn:
		delta := 1
		if item.Action == ui.MenuMoveBack {
			delta = -1
		}
		if m.Tab != 0 {
			return c.MoveTab(m.Tab, delta)
		}
		return c.MoveWorkspace(m.Workspace, delta)
	case ui.MenuClose:
		return s.closeFor(m)
	case ui.MenuNewWorktree:
		s.setPrompt(&Prompt{Kind: promptNewWorktree, Title: "new worktree: branch", Workspace: m.Workspace})
		return nil
	case ui.MenuOpenWorktree:
		return s.openWorktreeMenu(m)
	case ui.MenuRemoveWorktree:
		_, err := s.apiCall("worktree.remove", map[string]any{"workspace_id": fmt.Sprintf("w_%d", m.Workspace)})
		return err
	case "desktop-open-worktree":
		_, err := s.apiCall("worktree.open", map[string]any{
			"workspace_id": fmt.Sprintf("w_%d", m.Workspace), "path": item.Arg,
		})
		return err
	case ui.MenuSleep:
		_, err := c.SleepPane(m.Pane)
		return err
	case ui.MenuWake:
		return c.WakePane(m.Pane)
	case ui.MenuMute, ui.MenuUnmute:
		return c.Mute(m.Pane, item.Action == ui.MenuMute)
	case ui.MenuMarkUnread, ui.MenuMarkRead:
		return c.MarkUnread(m.Pane, item.Action == ui.MenuMarkUnread)
	case ui.MenuResumeAfterLimit, ui.MenuNoResume:
		return c.ResumeAfterLimit(m.Pane, item.Action == ui.MenuResumeAfterLimit)
	case ui.MenuReload:
		return c.Call(proto.MethodServerReloadConfig, nil, nil)
	case ui.MenuSleepPick:
		return s.openSleepMenu(m)
	case ui.MenuSleepOne:
		pane, err := strconv.ParseUint(item.Arg, 10, 64)
		if err != nil {
			return err
		}
		_, err = c.SleepPane(pane)
		return err
	case ui.MenuSleepIdle:
		s.mu.Lock()
		var idle []uint64
		for _, p := range s.snap.Panes {
			if p.Agent != "" && p.Running && !p.Dormant && sidebar.DisplayState(p) == "idle" {
				idle = append(idle, p.ID)
			}
		}
		s.mu.Unlock()
		for _, p := range idle {
			if _, err := c.SleepPane(p); err != nil {
				return err
			}
		}
		return nil
	}
	// The global menu's screens (settings, MCP, permissions, tools, phone,
	// about, what's new) are the window's panels, opened by QML.
	s.mu.Lock()
	s.panel = item.Action
	s.mu.Unlock()
	s.stateChanged()
	return nil
}

// openSleepMenu lists the agents that can be put to sleep, as the TUI's
// sleepChoicesLocked.
func (s *Session) openSleepMenu(m ui.Menu) error {
	s.mu.Lock()
	var choices []ui.SleepChoice
	for _, w := range s.snap.Workspaces {
		for _, t := range w.Tabs {
			for _, id := range t.Panes {
				for _, p := range s.snap.Panes {
					if p.ID == id && p.Agent != "" && p.Running && !p.Dormant {
						choices = append(choices, ui.SleepChoice{
							Pane:  id,
							Label: sidebar.OrDash(w.Name) + " · " + p.Agent,
							Idle:  sidebar.DisplayState(p) == "idle",
						})
					}
				}
			}
		}
	}
	s.mu.Unlock()
	if len(choices) == 0 {
		return fmt.Errorf("no agent is awake to sleep")
	}
	s.openSubmenu(ui.SleepMenu(choices, m.X, m.Y))
	return nil
}

// openWorktreeMenu lists the space's repository's worktrees to open one as
// a space, through the automation socket as the TUI does.
func (s *Session) openWorktreeMenu(m ui.Menu) error {
	res, err := s.apiCall("worktree.list", map[string]any{"workspace_id": fmt.Sprintf("w_%d", m.Workspace)})
	if err != nil {
		return err
	}
	list, _ := res["worktrees"].([]any)
	var items []ui.MenuItem
	for _, w := range list {
		wt, _ := w.(map[string]any)
		branch, _ := wt["branch"].(string)
		path, _ := wt["path"].(string)
		if path == "" {
			continue
		}
		label := branch
		if label == "" {
			label = path
		}
		items = append(items, ui.MenuItem{Label: label, Action: "desktop-open-worktree", Arg: path})
	}
	if len(items) == 0 {
		return fmt.Errorf("this space's repository has no other worktree")
	}
	s.openSubmenu(ui.Menu{Title: "open worktree", Items: items, X: m.X, Y: m.Y, Workspace: m.Workspace})
	return nil
}

func (s *Session) renameFor(m ui.Menu) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case m.Group != "":
		s.prompt = &Prompt{Kind: promptRenameGroup, Title: "rename group", Text: m.Group, Group: m.Group}
	case m.Tab != 0 || m.Pane != 0:
		tab := m.Tab
		if tab == 0 {
			for _, w := range s.snap.Workspaces {
				for _, t := range w.Tabs {
					if hasPane(t, m.Pane) {
						tab = t.ID
					}
				}
			}
		}
		name := ""
		for _, w := range s.snap.Workspaces {
			for _, t := range w.Tabs {
				if t.ID == tab {
					name = t.Name
				}
			}
		}
		s.prompt = &Prompt{Kind: promptRenameTab, Title: "rename tab", Text: name, Tab: tab}
	case m.Workspace != 0:
		name := ""
		for _, w := range s.snap.Workspaces {
			if w.ID == m.Workspace {
				name = w.Name
			}
		}
		s.prompt = &Prompt{Kind: promptRenameSpace, Title: "rename space", Text: name, Workspace: m.Workspace}
	}
	go s.stateChanged()
	return nil
}

// closeFor closes the menu's target, whichever kind it is. A group is
// deleted — its spaces stay, no longer kept together.
func (s *Session) closeFor(m ui.Menu) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	switch {
	case m.Group != "":
		return c.DeleteGroup(m.Group)
	case m.Pane != 0:
		return c.ClosePane(m.Pane)
	case m.Tab != 0:
		return c.CloseTab(m.Tab)
	case m.Workspace != 0:
		if err := c.CloseWorkspace(m.Workspace); err != nil {
			return err
		}
		s.mu.Lock()
		if s.workspace == m.Workspace {
			// The space looked at went, so nothing about the view still
			// resolves; the refresh lands somewhere real.
			s.workspace, s.tab, s.focus = 0, 0, 0
		}
		s.mu.Unlock()
		return s.refresh()
	}
	return nil
}

// SubmitPrompt is the line typed into the prompt, which does what the
// prompt was for.
func (s *Session) SubmitPrompt(text string) error {
	s.mu.Lock()
	p, c := s.prompt, s.client
	s.prompt = nil
	s.mu.Unlock()
	s.stateChanged()
	if p == nil {
		return nil
	}
	text = strings.TrimSpace(text)
	switch p.Kind {
	case promptRenameTab:
		return c.RenameTab(p.Tab, text)
	case promptRenamePane:
		return c.RenamePane(p.Pane, text)
	case promptRenameSpace:
		return c.RenameWorkspace(p.Workspace, text)
	case promptRenameGroup:
		if text == "" || text == p.Group {
			return nil
		}
		return c.RenameGroup(p.Group, text)
	case promptNewGroup:
		if text == "" {
			return nil
		}
		return c.GroupWorkspace(p.Workspace, text)
	case promptGroupDir:
		return c.SetGroupDir(p.Group, expandHome(text))
	case promptNewWorktree:
		if text == "" {
			return nil
		}
		_, err := s.apiCall("worktree.create", map[string]any{
			"workspace_id": fmt.Sprintf("w_%d", p.Workspace), "branch": text,
		})
		return err
	}
	return nil
}

// CancelPrompt puts the prompt away.
func (s *Session) CancelPrompt() {
	s.setPrompt(nil)
}

// newWorkspaceIn opens a space in a group, in the group's folder when it
// has one, as the TUI's newWorkspaceIn.
func (s *Session) newWorkspaceIn(group string) error {
	s.mu.Lock()
	c := s.client
	dir := ""
	for _, g := range s.snap.Groups {
		if g.Name == group {
			dir = g.Dir
		}
	}
	s.mu.Unlock()
	ws, err := c.NewWorkspaceIn("", dir)
	if err != nil {
		return err
	}
	if err := c.GroupWorkspace(ws, group); err != nil {
		return err
	}
	tab, _, err := c.NewTab(ws, "", proto.PaneSpec{Dir: dir})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.workspace, s.tab, s.focus = ws, tab, 0
	s.mu.Unlock()
	return s.refresh()
}

// startAgentInGroup starts an agent in a new space of a group, named after
// it, in the group's folder: the TUI's startAgentInGroup.
func (s *Session) startAgentInGroup(group, id string) error {
	s.mu.Lock()
	c := s.client
	var agent proto.AgentStatus
	for _, a := range s.catalog {
		if a.ID == id {
			agent = a
		}
	}
	dir := ""
	for _, g := range s.snap.Groups {
		if g.Name == group {
			dir = g.Dir
		}
	}
	s.mu.Unlock()
	if agent.ID == "" {
		return nil
	}
	ws, err := c.NewWorkspaceIn(agent.Name, dir)
	if err != nil {
		return err
	}
	if err := c.GroupWorkspace(ws, group); err != nil {
		return err
	}
	spec := agentSpec(agent)
	spec.Dir = dir
	tab, _, err := c.NewTab(ws, agent.Name, spec)
	if err != nil {
		return fmt.Errorf("%s did not start: %w", agent.Name, err)
	}
	s.mu.Lock()
	s.workspace, s.tab, s.focus = ws, tab, 0
	s.mu.Unlock()
	return s.refresh()
}

// agentSpec is the pane an agent starts in: cmd/tend's groupAgentSpec.
func agentSpec(agent proto.AgentStatus) proto.PaneSpec {
	program := agent.Path
	if program == "" {
		program = agent.ID
	}
	label := ""
	if agent.Detects {
		label = agent.ID
	}
	return proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", resumeScript, "tend-agent", program}, agent.Args...),
		Agent:   label,
	}
}

func tildeHome(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && dir != "" && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

func expandHome(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && (dir == "~" || strings.HasPrefix(dir, "~/")) {
		return home + strings.TrimPrefix(dir, "~")
	}
	return dir
}
