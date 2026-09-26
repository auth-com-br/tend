package main

import (
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
	agentAt := t.paneAgentsLocked()
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
			return ui.GroupMenu(row.Group, row.Folded, x, y), true
		case ui.SidebarAgent:
			return ui.AgentMenu(row.Pane, row.Tab, row.Workspace, x, y), true
		}
		return ui.Menu{}, false
	}
	if tab, newTab, ok := ui.TabAt(frame, x, y, cols, rows); ok && !newTab {
		return ui.TabMenu(tab, x, y), true
	}
	if pane := t.paneAt(x, y); pane != 0 {
		// The last pane in a tab has no "close pane": closing it would leave
		// an empty tab, and "close tab" is the honest name for that.
		return ui.PaneMenu(pane, x, y, panes > 1, agentAt[pane] != ""), true
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
		current, groups := "", []string(nil)
		seen := make(map[string]bool)
		for _, w := range t.snap.Workspaces {
			if w.ID == m.Workspace {
				current = w.Group
			}
			if w.Group != "" && !seen[w.Group] {
				seen[w.Group] = true
				groups = append(groups, w.Group)
			}
		}
		t.mu.Unlock()
		t.openMenu(ui.GroupPickMenu(m.Workspace, current, groups, m.X, m.Y))
		return nil

	case ui.MenuPickGroup:
		return t.moveSpaceToGroup(m.Workspace, item.Arg)

	case ui.MenuContinueIn:
		t.openMenu(ui.ContinueMenu(m.Pane, "", continueAgents(), m.X, m.Y))
		return nil

	case ui.MenuContinueWith:
		go t.continueIn(m.Pane, m.Conversation, item.Arg)
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

// renameGroup moves every space in a group to a new name, which is what
// renaming a group means when a group is only the set of spaces naming it.
func (t *tui) renameGroup(group, name string) error {
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
		// "Ungroup" is renaming the group to nothing: its spaces stay, they
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
