package main

import (
	"strconv"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The menu is how everything that is not a jump gets done with the mouse.
//
// It is opened on the thing it acts on — right-click a pane and the menu
// closes that pane, right-click a space and it closes that space — so there
// is no current selection to be wrong about. Whatever the menu does, it does
// to what was under the pointer when it opened.

// openMenu shows a menu, replacing any already open.
//
// Opening one turns on motion reporting, so the item under the pointer can be
// marked before it is clicked. Nothing else follows the pointer, and the
// reports cost a message per cell crossed, so they are asked for here and
// given back in closeMenu rather than left on.
func (t *tui) openMenu(m ui.Menu) {
	// No item starts marked: the pointer is on the thing the menu was opened
	// on, not on a choice, and pre-selecting one invites a stray Enter.
	m.Selected = -1

	t.mu.Lock()
	t.menu = &m
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// hoverMenu marks the item under the pointer, and reports whether a menu is
// open to do it in.
func (t *tui) hoverMenu(x, y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.menu == nil {
		return false
	}

	at := -1
	if item, onItem, _ := ui.MenuItemAt(*t.menu, t.cols, t.rows, x, y); onItem {
		for i, candidate := range t.menu.Items {
			if candidate == item {
				at = i
				break
			}
		}
	}
	if at != t.menu.Selected {
		t.menu.Selected = at
		t.dirty = true
	}
	return true
}

func (t *tui) closeMenu() {
	t.mu.Lock()
	had := t.menu != nil
	t.menu = nil
	t.dirty = true
	t.mu.Unlock()
	if !had {
		return
	}
	t.wakeUp()
}

func (t *tui) menuOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.menu != nil
}

// menuFor builds the menu for whatever is at a point, or reports that there is
// nothing there to act on.
func (t *tui) menuFor(x, y int) (ui.Menu, bool) {
	t.mu.Lock()
	frame := t.buildFrame()
	cols, rows := t.cols, t.rows
	panes := len(t.rects)
	groups := t.hasGroupsLocked()
	companies := len(t.snap.Companies)
	agentAt := t.paneAgentsLocked()
	asleep := map[uint64]bool{}
	marked := map[uint64]bool{}
	limited := map[uint64]*proto.PaneUsage{}
	muted := map[uint64]bool{}
	for _, p := range t.snap.Panes {
		asleep[p.ID] = p.Dormant
		marked[p.ID] = p.Marked
		limited[p.ID] = limitedUsage(p)
		muted[p.ID] = p.Muted
	}
	canMute := t.serverKnows(proto.MethodPaneMute)
	canSleep := t.serverKnows(proto.MethodPaneSleep)
	canMark := t.serverKnows(proto.MethodPaneMarkUnread)
	canResume := t.serverKnows(proto.MethodPaneResumeAfterLimit)
	var sel *ui.Selection
	if t.sel != nil {
		s := *t.sel
		sel = &s
	}
	programCopy := map[uint64]bool{}
	for pane, text := range t.programCopy {
		programCopy[pane] = text != ""
	}
	t.mu.Unlock()

	if row, ok := ui.SidebarRowAt(frame, x, y, rows); ok {
		if row.Machine != "" {
			// Another machine's space: its menu would act on this session's
			// space of the same number. Going there first is the way to it.
			return ui.Menu{}, false
		}
		switch row.Kind {
		case ui.SidebarSpace:
			return ui.SpaceMenu(row.Workspace, groups, x, y), true
		case ui.SidebarSpaceGroup:
			return ui.GroupMenu(row.Group, row.Folded, tildeHome(t.groupDirLocked(row.Group)), companies > 0, x, y), true
		case ui.SidebarAgent:
			m := ui.AgentMenu(row.Pane, row.Tab, row.Workspace, x, y)
			if canMark {
				m = ui.WithMarkUnread(m, marked[row.Pane])
			}
			if u := limited[row.Pane]; u != nil && canResume {
				m = ui.WithResumeAfterLimit(m, u.ResumeArmed)
			}
			if canMute {
				m = ui.WithMute(m, muted[row.Pane])
			}
			if canSleep {
				m = ui.WithSleep(m, asleep[row.Pane])
			}
			return m, true
		}
		return ui.Menu{}, false
	}
	if tab, newTab, ok := ui.TabAt(frame, x, y, cols, rows); ok && !newTab {
		return ui.TabMenu(tab, x, y), true
	}
	if pane := t.paneAt(x, y); pane != 0 {
		// The last pane in a tab has no "close pane": closing it would leave
		// an empty tab, and "close tab" is the honest name for that.
		// The selection is still there when the right button goes down —
		// only a left press clears it — so the menu can offer to copy it.
		// An agent that takes the mouse selects for itself and copies what
		// it selected; that copy stands for its selection.
		selected := (sel != nil && sel.Pane == pane && !sel.Empty()) || programCopy[pane]
		m := ui.PaneMenu(pane, x, y, panes > 1, agentAt[pane] != "", selected)
		if agentAt[pane] != "" && canMark {
			m = ui.WithMarkUnread(m, marked[pane])
		}
		if u := limited[pane]; agentAt[pane] != "" && u != nil && canResume {
			m = ui.WithResumeAfterLimit(m, u.ResumeArmed)
		}
		if agentAt[pane] != "" && canMute {
			m = ui.WithMute(m, muted[pane])
		}
		if agentAt[pane] != "" && canSleep {
			m = ui.WithSleep(m, asleep[pane])
		}
		return m, true
	}
	return ui.Menu{}, false
}

// clickMenu routes a press while a menu is open.
//
// A press inside the menu but not on an item is swallowed rather than falling
// through, or one gesture would close the menu and act on whatever it was
// covering.
func (t *tui) clickMenu(x, y int) (bool, error) {
	t.mu.Lock()
	menu := t.menu
	cols, rows := t.cols, t.rows
	t.mu.Unlock()
	if menu == nil {
		return false, nil
	}

	item, onItem, inside := ui.MenuItemAt(*menu, cols, rows, x, y)
	if !inside {
		t.closeMenu()
		return true, nil
	}
	if !onItem {
		return true, nil
	}
	t.closeMenu()
	return true, t.runMenu(*menu, item)
}

// scrollMenu turns the wheel over a long menu: the list moves, the cursor
// with it, so the item under the pointer is the one a click runs. Reports
// whether a menu with more items than it shows was under the point.
func (t *tui) scrollMenu(x, y, delta int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	m := t.menu
	if m == nil {
		return false
	}
	if _, _, inside := ui.MenuItemAt(*m, t.cols, t.rows, x, y); !inside {
		return false
	}
	m.Selected = -1
	m.Scroll = m.Top(t.cols, t.rows) + delta
	m.Scroll = m.Top(t.cols, t.rows)
	t.dirty = true
	return true
}

// menuKeys drives the menu from the keyboard, so it is not a mouse-only
// feature in a program people run over ssh.
func (t *tui) menuKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		switch key {
		case "\x1b[A", "k":
			t.moveMenu(-1)
		case "\x1b[B", "j":
			t.moveMenu(1)
		case "\r", "\n":
			t.mu.Lock()
			menu := t.menu
			t.mu.Unlock()
			if menu == nil {
				return true, nil
			}
			if menu.Selected < 0 || menu.Selected >= len(menu.Items) {
				// Nothing is marked, so there is nothing to choose. Closing is
				// the honest answer rather than running whichever item happens
				// to be first.
				t.closeMenu()
				return true, nil
			}
			item := menu.Items[menu.Selected]
			t.closeMenu()
			return true, t.runMenu(*menu, item)
		default:
			// Anything else closes it rather than being swallowed by a mode
			// the user has stopped thinking about.
			t.closeMenu()
			return true, nil
		}
	}
	return true, nil
}

func (t *tui) moveMenu(delta int) {
	t.mu.Lock()
	if t.menu != nil && len(t.menu.Items) > 0 {
		n := len(t.menu.Items)
		at := t.menu.Selected
		if at < 0 {
			// Nothing marked yet: down lands on the first item and up on the
			// last, rather than both landing next to an arbitrary starting
			// point the user never saw.
			at = -1
			if delta < 0 {
				at = 0
			}
		}
		t.menu.Selected = (at + delta + n) % n
		t.menu.Scroll = t.menu.Top(t.cols, t.rows)
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// runMenu performs an item against the menu's target.
func (t *tui) runMenu(m ui.Menu, item ui.MenuItem) error {
	switch item.Action {
	case ui.MenuGoTo:
		return t.jumpToPane(m.Pane)

	case ui.MenuNewSpace:
		if m.Group != "" {
			return t.newWorkspaceIn(m.Group)
		}
		return t.newWorkspace()

	case ui.MenuGroup:
		if err := t.showWorkspace(m.Workspace); err != nil {
			return err
		}
		t.startPrompt(promptGroupSpace)
		return nil

	case ui.MenuMoveToGroup:
		t.mu.Lock()
		current, groups := t.groupOfLocked(m.Workspace), []string(nil)
		for _, g := range groupsIn(&t.snap) {
			groups = append(groups, g.Name)
		}
		t.mu.Unlock()
		t.openMenu(ui.GroupPickMenu(m.Workspace, current, groups, m.X, m.Y))
		return nil

	case ui.MenuGroupDir:
		if !t.client.Supports(proto.MethodGroupSetDir) {
			t.setMessage("this needs a newer server: run tend handoff", true)
			return nil
		}
		if t.client.Supports(proto.MethodDirList) {
			t.openFolderPick(m.Group)
			return nil
		}
		t.mu.Lock()
		t.promptGroup = m.Group
		t.mu.Unlock()
		t.startPrompt(promptGroupDir)
		return nil

	case ui.MenuGroupAgent:
		go t.openGroupAgentMenu(m.Group, m.X, m.Y)
		return nil

	case ui.MenuGroupAgentStart:
		return t.startAgentInGroup(m.Group, item.Arg)

	case ui.MenuGroupDirClear:
		if err := t.client.SetGroupDir(m.Group, ""); err != nil {
			return err
		}
		return t.refresh()

	case ui.MenuPickGroup:
		return t.moveSpaceToGroup(m.Workspace, item.Arg)

	case ui.MenuMoveToCompany:
		// From the company being looked at; from every one when none is.
		t.mu.Lock()
		from, chosen := t.companyLocked()
		var choices []ui.CompanyChoice
		for _, c := range t.snap.Companies {
			if !chosen || c.ID != from.ID {
				choices = append(choices, ui.CompanyChoice{ID: strconv.FormatUint(c.ID, 10), Name: c.Name})
			}
		}
		t.mu.Unlock()
		if len(choices) == 0 {
			t.setMessage("no other company to move it to: make one with the mark beside \"spaces\"", false)
			return nil
		}
		t.openMenu(ui.CompanyPickMenu(m.Group, from.Name, choices, m.X, m.Y))
		return nil

	case ui.MenuPickCompany:
		return t.moveGroupToCompany(m.Group, item.Arg, item.Label)

	case ui.MenuContinueIn:
		t.openMenu(ui.ContinueMenu(m.Pane, "", continueAgents(), m.X, m.Y))
		return nil

	case ui.MenuContinueWith:
		go t.continueIn(m.Pane, m.Conversation, item.Arg)
		return nil

	case ui.MenuCopy:
		go t.copySelection(m.Pane)
		return nil

	case ui.MenuPaste:
		if m.Pane != 0 {
			t.focusPane(m.Pane)
		}
		go t.pasteInto(m.Pane)
		return nil

	case ui.MenuFiles:
		if m.Pane != 0 {
			t.focusPane(m.Pane)
		}
		return t.toggleFiles()

	case ui.MenuFold:
		t.toggleGroup(m.Group)
		return nil

	case ui.MenuMoveBack, ui.MenuMoveOn:
		delta := 1
		if item.Action == ui.MenuMoveBack {
			delta = -1
		}
		var err error
		if m.Tab != 0 {
			err = t.client.MoveTab(m.Tab, delta)
		} else if m.Workspace != 0 {
			err = t.client.MoveWorkspace(m.Workspace, delta)
		}
		if err != nil {
			if t.reportStaleServer(err) || isNothingToMove(err) {
				return nil // already at that end of the row
			}
			return err
		}
		return t.refresh()

	case ui.MenuNewTab:
		if m.Workspace != 0 {
			if err := t.showWorkspace(m.Workspace); err != nil {
				return err
			}
		}
		return t.newTabHere()

	case ui.MenuRename:
		return t.renameFor(m)

	case ui.MenuNewWorktree:
		if err := t.showWorkspace(m.Workspace); err != nil {
			return err
		}
		t.startPrompt(promptNewWorktree)
		return nil

	case ui.MenuOpenWorktree:
		t.openWorktreeMenu(m.Workspace, m.X, m.Y)
		return nil

	case ui.MenuRemoveWorktree:
		t.removeWorktree(m.Workspace)
		return nil

	case ui.MenuRenamePane:
		if m.Pane == 0 {
			return nil
		}
		if err := t.jumpToPane(m.Pane); err != nil {
			return err
		}
		t.startPrompt(promptRenamePane)
		return nil

	case ui.MenuSplitRight, ui.MenuSplitDown:
		dir := "columns"
		if item.Action == ui.MenuSplitDown {
			dir = "rows"
		}
		created, err := t.client.SplitPane(m.Pane, dir, proto.PaneSpec{Command: t.paneShell()})
		if err != nil {
			return err
		}
		t.mu.Lock()
		t.focus = created
		t.mu.Unlock()
		return t.refresh()

	case ui.MenuZoom:
		if err := t.jumpToPane(m.Pane); err != nil {
			return err
		}
		t.mu.Lock()
		t.zoom = !t.zoom
		t.mu.Unlock()
		return t.refresh()

	case ui.MenuClose:
		return t.closeFor(m)

	case ui.MenuSettings:
		return t.openSettings()
	case ui.MenuKeybinds:
		t.toggleOverlay(t.helpLines())
		return nil
	case ui.MenuReload:
		t.requestRepaint()
		return t.reloadSettings()
	case ui.MenuWhatsNew:
		return t.openReleaseNotes()
	case ui.MenuAbout:
		t.openAbout()
		return nil
	case ui.MenuPhone:
		t.openPhone()
		return nil
	case ui.MenuMCP:
		return t.openMCPManager()
	case ui.MenuTools:
		t.openMenu(ui.ToolsMenu(toolChoices(), m.X, m.Y))
		return nil
	case ui.MenuTool:
		return t.runTool(item.Arg)
	case ui.MenuMCPDo:
		return t.mcpMenuAction(item.Arg)
	case ui.MenuSleep:
		go t.sleepAgent(m.Pane)
		return nil
	case ui.MenuMute, ui.MenuUnmute:
		mute := item.Action == ui.MenuMute
		go func() {
			if err := t.client.Mute(m.Pane, mute); err != nil {
				t.setMessage("could not mute it: "+err.Error(), true)
				return
			}
			if mute {
				t.setMessage("muted — it is shown, not announced", false)
			} else {
				t.setMessage("unmuted", false)
			}
		}()
		return nil
	case ui.MenuResumeAfterLimit, ui.MenuNoResume:
		on := item.Action == ui.MenuResumeAfterLimit
		go func() {
			if err := t.client.ResumeAfterLimit(m.Pane, on); err != nil {
				t.setMessage("could not arm it: "+err.Error(), true)
				return
			}
			if on {
				t.setMessage("it will be told to continue when its limit resets", false)
			}
		}()
		return nil
	case ui.MenuMarkUnread, ui.MenuMarkRead:
		unread := item.Action == ui.MenuMarkUnread
		go func() {
			if err := t.client.MarkUnread(m.Pane, unread); err != nil {
				t.setMessage("could not mark it: "+err.Error(), true)
				return
			}
			if unread {
				t.setMessage("marked unread — it stays in the list until you go to its tab", false)
			}
		}()
		return nil
	case ui.MenuSleepPick:
		if !t.serverKnows(proto.MethodPaneSleep) {
			t.setMessage("this server is older than sleeping agents; "+handoffCommand(t.session)+" moves it to this build", true)
			return nil
		}
		t.mu.Lock()
		choices := t.sleepChoicesLocked()
		t.mu.Unlock()
		t.openMenu(ui.SleepMenu(choices, m.X, m.Y))
		return nil
	case ui.MenuSleepOne:
		if pane, err := strconv.ParseUint(item.Arg, 10, 64); err == nil {
			go t.sleepAgent(pane)
		}
		return nil
	case ui.MenuSleepIdle:
		t.mu.Lock()
		var idle []uint64
		for _, c := range t.sleepChoicesLocked() {
			if c.Idle {
				idle = append(idle, c.Pane)
			}
		}
		t.mu.Unlock()
		go t.sleepAgents(idle)
		return nil
	case ui.MenuWake:
		go func() {
			if err := t.client.WakePane(m.Pane); err != nil {
				t.setMessage(refusalText(err), true)
			}
		}()
		return nil
	case ui.MenuPermissions:
		return t.openPermManager()
	case ui.MenuPermDo:
		return t.permMenuAction(item.Arg)
	case ui.MenuDetach:
		t.detach = true
		return nil
	}
	return nil
}

// moveSpaceToGroup puts a space in a group, or in none.
func (t *tui) moveSpaceToGroup(workspace uint64, group string) error {
	if err := t.client.GroupWorkspace(workspace, group); err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}
	return t.refresh()
}

// moveGroupToCompany moves a group and its spaces to another company: from
// the one being looked at, or from every one when none is. Looking at the
// company it left, the group is gone from the list, which says it moved.
func (t *tui) moveGroupToCompany(group, to, name string) error {
	if !t.client.Supports(proto.MethodGroupMoveCompany) {
		t.setMessage("moving a group needs a newer server: run tend handoff", true)
		return nil
	}
	id, err := strconv.ParseUint(to, 10, 64)
	if err != nil {
		return err
	}
	t.mu.Lock()
	from, _ := t.companyLocked()
	t.mu.Unlock()
	if err := t.client.MoveGroupToCompany(group, from.ID, id); err != nil {
		if t.reportStaleServer(err) {
			return nil
		}
		return err
	}
	t.setMessage("moved "+group+" to "+name, false)
	return t.refresh()
}

// renameGroup names a group again. The server does it in one step, keeping
// the group whether or not it has spaces; a server from before groups had
// records is asked to move every space in it, which is what renaming meant
// when a group was only the set of spaces naming it. An empty name is
// deleting the group.
func (t *tui) renameGroup(group, name string) error {
	if name == "" && t.client.Supports(proto.MethodGroupDelete) {
		t.mu.Lock()
		delete(t.folded, group)
		t.mu.Unlock()
		if err := t.client.DeleteGroup(group); err != nil {
			return err
		}
		return t.refresh()
	}
	if name != "" && t.client.Supports(proto.MethodGroupRename) {
		t.mu.Lock()
		folded := t.folded[group]
		delete(t.folded, group)
		t.folded[name] = folded
		t.mu.Unlock()
		if err := t.client.RenameGroup(group, name); err != nil {
			return err
		}
		return t.refresh()
	}

	t.mu.Lock()
	var ids []uint64
	for _, w := range t.snap.Workspaces {
		if w.Group == group {
			ids = append(ids, w.ID)
		}
	}
	folded := t.folded[group]
	delete(t.folded, group)
	if name != "" {
		t.folded[name] = folded
	}
	t.mu.Unlock()

	for _, id := range ids {
		if err := t.client.GroupWorkspace(id, name); err != nil {
			return err
		}
	}
	return t.refresh()
}

// renameFor opens the rename prompt on the menu's target, moving there first:
// the prompt edits what is being looked at, and renaming something out of
// view would leave the user reading a name that is not the one changing.
func (t *tui) renameFor(m ui.Menu) error {
	switch {
	case m.Group != "":
		t.startGroupRename(m.Group)
	case m.Pane != 0:
		if err := t.jumpToPane(m.Pane); err != nil {
			return err
		}
		t.startPrompt(promptRenameTab)
	case m.Tab != 0:
		if err := t.showTab(m.Tab); err != nil {
			return err
		}
		t.startPrompt(promptRenameTab)
	case m.Workspace != 0:
		if err := t.showWorkspace(m.Workspace); err != nil {
			return err
		}
		t.startPrompt(promptRenameSpace)
	}
	return nil
}

// closeFor closes the menu's target, whichever kind it is.
func (t *tui) closeFor(m ui.Menu) error {
	switch {
	case m.Group != "":
		// Deleting a group is the one way it goes: its spaces stay, they
		// just stop being kept together.
		return t.renameGroup(m.Group, "")
	case m.Pane != 0:
		if err := t.client.ClosePane(m.Pane); err != nil {
			return err
		}
	case m.Tab != 0:
		if err := t.client.CloseTab(m.Tab); err != nil {
			return err
		}
	case m.Workspace != 0:
		if err := t.client.CloseWorkspace(m.Workspace); err != nil {
			return err
		}
		t.mu.Lock()
		if t.workspace == m.Workspace {
			// The space being looked at has gone, so nothing about the view
			// still resolves. Dropping it lets the refresh land somewhere real
			// rather than on identifiers that no longer exist.
			t.workspace, t.tab, t.focus, t.zoom = 0, 0, 0, false
		}
		t.mu.Unlock()
	default:
		return nil
	}
	// Nothing is created to replace what was just closed. Closing the last
	// space used to hand back a fresh one immediately, which from the outside
	// is indistinguishable from the close having failed. An empty session is
	// a real state, and the "new" button is sitting in the list.
	return t.refresh()
}

// limitedUsage is a pane's usage while a limit has its agent stopped, for
// the menu's "continue when the limit resets"; nil otherwise.
func limitedUsage(p proto.PaneInfo) *proto.PaneUsage {
	if p.Usage == nil || p.Usage.Limit == nil || !p.Usage.Limit.Reached || p.Usage.Limit.Resets == 0 {
		return nil
	}
	return p.Usage
}
