package main

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/auth-com-br/tend/internal/agentview"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// Navigating a session means three things that are easy to confuse: which
// workspace is shown, which of its tabs, and which pane has the keyboard.
//
// All three are this client's, not the server's. Two people attached to one
// session should be able to look at different things, so nothing here is sent
// upstream — the server is asked only for what exists, never for what is being
// looked at.

// workspaceInfo returns the workspace being shown. The caller holds the lock.
func (t *tui) workspaceLocked() (proto.WorkspaceInfo, bool) {
	for _, w := range t.snap.Workspaces {
		if w.ID == t.workspace {
			return w, true
		}
	}
	return proto.WorkspaceInfo{}, false
}

// tabsLocked returns the tabs of the workspace being shown.
//
// Only that workspace's: a bar listing every tab in the session would put
// tabs the user cannot reach next to ones they can, which is worse than not
// showing them at all.
func (t *tui) tabsLocked() []proto.TabInfo {
	w, ok := t.workspaceLocked()
	if !ok {
		return nil
	}
	return w.Tabs
}

// revealSidebarLocked scrolls the list so that what is current can be seen.
//
// Without it, creating the tenth space or jumping to one below the fold would
// leave the list showing the nine you are not in. The caller holds the lock.
func (t *tui) revealSidebarLocked() {
	if !t.sidebar {
		return
	}
	frame := t.buildFrame()
	spaces, agents := ui.SidebarRegions(frame, t.rows)

	if at := ui.SidebarActiveRow(frame.Spaces); at >= 0 {
		if next := ui.SidebarRevealScroll(frame.Spaces, spaces.Rows, at); next != t.spacesScroll {
			t.spacesScroll = next
			t.dirty = true
		}
	}
	if at := ui.SidebarActiveRow(frame.Agents); at >= 0 && agents.Rows > 0 {
		if next := ui.SidebarRevealScroll(frame.Agents, agents.Rows, at); next != t.agentsScroll {
			t.agentsScroll = next
			t.dirty = true
		}
	}
}

// resolveView settles which workspace and tab to show, given what exists.
//
// It is called after every refresh because the session can change underneath:
// a workspace can empty out, a tab can close, and the view has to land
// somewhere real rather than on an identifier that no longer resolves.
func (t *tui) resolveViewLocked() {
	if len(t.snap.Workspaces) == 0 {
		t.workspace, t.tab = 0, 0
		return
	}

	if _, ok := t.workspaceLocked(); !ok {
		t.workspace = t.snap.ActiveWorkspace
		if _, ok := t.workspaceLocked(); !ok || !t.shownLocked(t.workspace) {
			// Somewhere the list shows, when a company is chosen and has
			// any: landing on a space the sidebar hides leaves the user
			// nowhere they can see.
			t.workspace = t.snap.Workspaces[0].ID
			for _, w := range t.snap.Workspaces {
				if t.shownLocked(w.ID) {
					t.workspace = w.ID
					break
				}
			}
		}
	}

	tabs := t.tabsLocked()
	if len(tabs) == 0 {
		// An empty workspace is a real state — the user closed its last tab —
		// so it is shown empty rather than skipped past.
		t.tab = 0
		return
	}
	for _, tab := range tabs {
		if tab.ID == t.tab {
			return
		}
	}
	w, _ := t.workspaceLocked()
	if w.ActiveTab != 0 {
		for _, tab := range tabs {
			if tab.ID == w.ActiveTab {
				t.tab = w.ActiveTab
				return
			}
		}
	}
	t.tab = tabs[0].ID
}

// switchWorkspace moves to the next or previous workspace.
func (t *tui) switchWorkspace(forward bool) error {
	t.mu.Lock()
	if t.multiMachineLocked() {
		t.mu.Unlock()
		return t.switchWorkspaceAcross(forward)
	}
	// Among the spaces the list shows: with a company chosen, next space
	// does not leave it.
	var spaces []uint64
	for _, w := range t.snap.Workspaces {
		if t.shownLocked(w.ID) {
			spaces = append(spaces, w.ID)
		}
	}
	idx := slices.Index(spaces, t.workspace)
	if len(spaces) == 0 || (len(spaces) == 1 && idx == 0) {
		t.mu.Unlock()
		return nil
	}
	switch {
	case idx < 0 && forward:
		idx = 0
	case idx < 0:
		idx = len(spaces) - 1
	case forward:
		idx = (idx + 1) % len(spaces)
	default:
		idx = (idx - 1 + len(spaces)) % len(spaces)
	}
	t.workspace = spaces[idx]
	// The tab and pane belong to the workspace being left, so they are
	// dropped and resolved afresh against the one being entered.
	t.rememberFocusLocked()
	t.tab, t.focus, t.zoom = 0, 0, false
	t.mu.Unlock()

	return t.refresh()
}

// switchWorkspaceAcross is next and previous space with saved machines:
// herdr's handle_endpoint_navigation, every reachable machine's spaces in the
// sidebar's machine order, going to another machine when the next space is
// on it.
func (t *tui) switchWorkspaceAcross(forward bool) error {
	type target struct {
		machine   string
		workspace uint64
	}
	t.mu.Lock()
	var spaces []target
	current := -1
	for _, e := range t.machines.endpoints {
		snap := &e.snap
		if e.id == t.machines.active {
			snap = &t.snap
		} else if e.status != ui.MachineOnline || !e.have {
			continue
		}
		for _, w := range snap.Workspaces {
			if e.id == t.machines.active && w.ID == t.workspace {
				current = len(spaces)
			}
			spaces = append(spaces, target{e.id, w.ID})
		}
	}
	active := t.machines.active
	t.mu.Unlock()
	if len(spaces) == 0 {
		return nil
	}
	var next target
	switch {
	case current >= 0 && forward:
		next = spaces[(current+1)%len(spaces)]
	case current >= 0:
		next = spaces[(current-1+len(spaces))%len(spaces)]
	case forward:
		next = spaces[0]
	default:
		next = spaces[len(spaces)-1]
	}
	if next.machine != active {
		return t.switchMachine(next.machine, next.workspace, 0)
	}
	return t.showWorkspace(next.workspace)
}

// newWorkspace creates a workspace with one pane and moves to it.
//
// It is named rather than left blank. An unnamed space shows as a dash in the
// list and vanishes from the status bar, which makes the one thing the user
// just created the hardest one to find.
func (t *tui) newWorkspace() error { return t.newWorkspaceIn("") }

// newWorkspaceIn creates a space already in a group, which is what "new space
// here" on a group heading means. A group with a folder set starts the space
// there, its first terminal too: that is what the folder is for.
func (t *tui) newWorkspaceIn(group string) error {
	return t.newWorkspaceRunning(group, t.nextName("space", len(t.snapshotWorkspaces())), t.nextName("tab", 0),
		func(dir string) proto.PaneSpec { return proto.PaneSpec{Command: t.paneShell(), Dir: dir} })
}

// newWorkspaceRunning creates a space in a group, in the group's folder,
// whose first tab runs what spec gives for that folder. A first tab that
// does not start takes the space with it: an empty space named after an
// agent that never ran is a leftover, not something the user made.
func (t *tui) newWorkspaceRunning(group, name, tabName string, spec func(dir string) proto.PaneSpec) error {
	t.mu.Lock()
	dir := t.groupDirLocked(group)
	t.mu.Unlock()
	ws, err := t.client.NewWorkspaceIn(name, dir)
	if err != nil {
		return err
	}
	if group != "" {
		if err := t.client.GroupWorkspace(ws, group); err != nil {
			return err
		}
	}
	// Made while a company is chosen, it belongs to that company: the user
	// is organising that company's work, and a space that appeared nowhere
	// in the list would be lost the moment it was made.
	t.mu.Lock()
	company, chosen := t.companyLocked()
	t.mu.Unlock()
	if chosen {
		if err := t.client.AssignCompany(company.ID, ws, true); err != nil {
			return err
		}
	}
	if _, _, err := t.client.NewTab(ws, tabName, spec(dir)); err != nil {
		_ = t.client.CloseWorkspace(ws)
		return err
	}
	t.mu.Lock()
	t.rememberFocusLocked()
	t.workspace, t.tab, t.focus, t.zoom = ws, 0, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// nextName labels a new workspace or tab by its position, so two of them are
// told apart without the user having to name everything by hand.
func (t *tui) nextName(kind string, existing int) string {
	return kind + " " + itoaInt(existing+1)
}

func (t *tui) snapshotWorkspaces() []proto.WorkspaceInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snap.Workspaces
}

func itoaInt(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// selectTab moves to the nth tab of the current workspace, counting from one.
func (t *tui) selectTab(n int) error {
	t.mu.Lock()
	tabs := t.tabsLocked()
	if n < 1 || n > len(tabs) {
		t.mu.Unlock()
		return nil
	}
	t.rememberFocusLocked()
	t.tab = tabs[n-1].ID
	t.focus, t.zoom = 0, false
	t.mu.Unlock()
	return t.refresh()
}

// agentStep is the agent before or after the focused pane, in the order the
// sidebar lists them, and the saved machine it is on ("" for the one shown).
// It wraps, and from a pane that is not an agent it starts at either end —
// which is what herdr does, and what makes the key useful from a shell pane.
//
// With saved machines the list is every machine's, as herdr's
// online_agent_targets is: an agent on a machine that cannot be reached is
// skipped, and one on another machine is gone to.
func (t *tui) agentStep(focus uint64, step int) (string, uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	type target struct {
		machine string
		pane    uint64
	}
	var agents []target
	for _, row := range t.agentRowsLocked() {
		if row.Kind == ui.SidebarAgent && !row.Stale {
			agents = append(agents, target{row.Machine, row.Pane})
		}
	}
	if len(agents) == 0 {
		return "", 0
	}
	current := -1
	for i, a := range agents {
		if a.machine == "" && a.pane == focus {
			current = i
		}
	}
	var next target
	switch {
	case current >= 0:
		next = agents[(current+step+len(agents))%len(agents)]
	case step < 0:
		next = agents[len(agents)-1]
	default:
		next = agents[0]
	}
	return next.machine, next.pane
}

// jumpToPane shows whichever workspace and tab hold a pane, and focuses it.
//
// This is what the agent list is for: the point of seeing every agent at once
// is being able to reach the one that stopped, wherever it happens to live.
func (t *tui) jumpToPane(pane uint64) error {
	t.mu.Lock()
	var ws, tab uint64
	for _, w := range t.snap.Workspaces {
		for _, tb := range w.Tabs {
			for _, id := range tb.Panes {
				if id == pane {
					ws, tab = w.ID, tb.ID
				}
			}
		}
	}
	if ws == 0 {
		t.mu.Unlock()
		return nil
	}
	same := t.workspace == ws && t.tab == tab
	t.revealCompanyLocked(ws)
	t.rememberFocusLocked()
	t.workspace, t.tab, t.focus = ws, tab, pane
	if !same {
		t.zoom = false
	}
	t.mu.Unlock()

	if same {
		t.markDirty()
		return nil
	}
	return t.refresh()
}

// showTab moves to a tab by identifier, switching space if it lives in
// another one. Clicking something in a list should go there, wherever it is.
func (t *tui) showTab(tab uint64) error {
	t.mu.Lock()
	var ws uint64
	for _, w := range t.snap.Workspaces {
		for _, tb := range w.Tabs {
			if tb.ID == tab {
				ws = w.ID
			}
		}
	}
	if ws == 0 || (t.workspace == ws && t.tab == tab) {
		t.mu.Unlock()
		return nil
	}
	t.revealCompanyLocked(ws)
	t.rememberFocusLocked()
	t.workspace, t.tab, t.focus, t.zoom = ws, tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// showWorkspace moves to a space by identifier.
func (t *tui) showWorkspace(ws uint64) error {
	t.mu.Lock()
	if ws == 0 || t.workspace == ws {
		t.mu.Unlock()
		return nil
	}
	t.revealCompanyLocked(ws)
	t.rememberFocusLocked()
	t.workspace, t.tab, t.focus, t.zoom = ws, 0, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// newTabHere opens a tab in the space being shown and moves to it.
func (t *tui) newTabHere() error {
	ws := t.shownWorkspace()
	if ws == 0 {
		return nil
	}
	t.mu.Lock()
	name := t.nextName("tab", len(t.tabsLocked()))
	// The tab opens where the pane being looked at is working — the
	// agent's project, not wherever tend was first started — as herdr's
	// does by default.
	from := t.focus
	t.mu.Unlock()

	tab, _, err := t.client.NewTab(ws, name, proto.PaneSpec{Command: t.paneShell(), DirOf: from})
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.rememberFocusLocked()
	t.tab, t.focus, t.zoom = tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// --- the agent list --------------------------------------------------------

// sidebarRowsLocked builds both lists: the places, then what is running.
//
// The two are separate because they answer different questions. Spaces exist
// whether or not anything runs in them, and the agent that stopped is rarely
// in the space being looked at — so listing agents under their spaces alone
// would bury the one thing the sidebar is for.
// spacesSectionLocked is the top list: the heading, the tree, and the button
// that makes another one.
func (t *tui) spacesSectionLocked() ui.SidebarSection {
	rows := []ui.SidebarRow{t.spacesHeadingLocked()}
	newLabel := "new"
	if t.multiMachineLocked() {
		// herdr's: the list is of machines, and a new space goes on the
		// one being shown, which the button says.
		rows[0].Label = "machines"
		rows = append(rows, t.machineRowsLocked()...)
		newLabel = "new · " + t.activeLabelLocked()
	} else {
		rows = append(rows, t.spaceRowsLocked()...)
	}
	// herdr's launcher reads "● menu" while an update is ready, so the menu
	// that says so is found.
	menu := "menu"
	if t.updateReadyLocked() != "" {
		menu = "● menu"
	}
	return ui.SidebarSection{
		Rows: rows,
		Footer: []ui.SidebarRow{{
			Kind:           ui.SidebarAction,
			Label:          newLabel,
			Action:         ui.ActionNewSpace,
			Trailing:       menu,
			TrailingAction: ui.ActionOpenMenu,
		}},
		Scroll: t.spacesScroll,
	}
}

// spacesHeadingLocked is the "spaces" heading, with the companies mark at
// its right: the company shown, and "!" before it when an agent in a space
// the list hides is waiting, so choosing a company never silences one.
func (t *tui) spacesHeadingLocked() ui.SidebarRow {
	row := ui.SidebarRow{Kind: ui.SidebarHeading, Label: "spaces"}
	if t.multiMachineLocked() || t.client == nil || !t.client.Supports(proto.MethodCompanyCreate) {
		// A list of machines has no one session to choose within, and an
		// older server has no companies to choose.
		return row
	}
	name := ""
	if c, ok := t.companyLocked(); ok {
		name = c.Name
	}
	width := t.sidebarWidthLocked() - len(row.Label) - 7
	row.Trailing = ui.CompanyLabel(name, len(t.snap.Companies) > 0, width)
	if t.waitingElsewhereLocked() > 0 {
		row.Trailing = "! " + row.Trailing
	}
	row.TrailingAction = ui.ActionCompanies
	row.Active = name != ""
	return row
}

// agentsSectionLocked is the bottom list: the heading with its toggle, and
// what is running.
func (t *tui) agentsSectionLocked() ui.SidebarSection {
	grouped := "flat"
	if t.grouped {
		grouped = "grouped"
	}
	heading := "agents"
	if v := t.snap.AgentView; v != nil {
		// Said, so a list that shows three agents of eight does not look
		// like the whole session (herdr labels it, "filtered" by default).
		label := v.Label
		if label == "" {
			label = "filtered"
		}
		heading += " · " + label
	}
	rows := []ui.SidebarRow{{
		Kind:           ui.SidebarHeading,
		Label:          heading,
		Trailing:       grouped,
		Action:         ui.ActionToggleGrouped,
		TrailingAction: ui.ActionToggleGrouped,
		Active:         t.grouped,
	}}
	return ui.SidebarSection{Rows: append(rows, t.agentRowsLocked()...), Scroll: t.agentsScroll}
}

// spaceRowsLocked lays the spaces out as a tree.
//
// A group appears where its first member is, so the order on screen follows
// the order of the session rather than sorting groups to one end: a user who
// made a space expects to find it where they put it, not where an alphabet
// puts it.
func (t *tui) spaceRowsLocked() []ui.SidebarRow {
	return t.spaceRowsFrom(spaceSource{snap: &t.snap, folded: t.folded, current: t.workspace})
}

// spaceSource is one machine's session as the space list reads it: the one
// being shown, or another saved machine's, whose rows are laid out the same
// way and point back at it.
type spaceSource struct {
	snap *proto.SessionSnapshot
	// machine is empty for the session being shown.
	machine string
	folded  map[string]bool
	// current is the space in view, which is none on another machine.
	current uint64
	// depth is where the tree starts: under a machine's row, one in.
	depth int
	stale bool
}

// spaceRowsFrom lays one session's spaces out as a tree.
func (t *tui) spaceRowsFrom(src spaceSource) []ui.SidebarRow {
	var rows []ui.SidebarRow
	seen := make(map[string]bool)
	here := src.machine == ""

	// With a company chosen, the session being shown lists its spaces
	// only; another machine's list is its own session's, and has none.
	shown := func(w proto.WorkspaceInfo) bool { return !here || t.shownLocked(w.ID) }

	// A group with no space in the list is still listed when it is the
	// user's to see: always with no company chosen, and with one when the
	// group has had a space of it. It is not where a member puts it, having
	// none here, so it comes before the next group made after it that is
	// placed by a member, or at the end — which keeps groups in the order
	// they were made as far as the spaces' order lets them.
	groups := groupsIn(src.snap)
	company, chosen := uint64(0), false
	if here {
		if c, ok := t.companyLocked(); ok {
			company, chosen = c.ID, true
		}
	}
	placed := make(map[string]bool)
	for _, w := range src.snap.Workspaces {
		if w.Group != "" && shown(w) {
			placed[w.Group] = true
		}
	}
	unplaced := func(g proto.GroupInfo) bool {
		return !placed[g.Name] && (!chosen || slices.Contains(g.Companies, company))
	}
	var emit func(name string)
	flushBefore := func(name string) {
		for _, g := range groups {
			if g.Name == name {
				return
			}
			if unplaced(g) && !seen[g.Name] {
				emit(g.Name)
			}
		}
	}

	emit = func(name string) {
		seen[name] = true
		members := slices.DeleteFunc(groupMembersIn(src.snap, name), func(m proto.WorkspaceInfo) bool { return !shown(m) })
		folded := src.folded[name]
		inGroup := false
		for _, m := range members {
			inGroup = inGroup || m.ID == src.current
		}
		rows = append(rows, ui.SidebarRow{
			Kind:     ui.SidebarSpaceGroup,
			Label:    name,
			Group:    name,
			Folded:   folded,
			DropHere: here && name == t.spaceDropGroup,
			Action:   ui.ActionToggleGroup,
			// A folded group still says what is happening inside it. Hiding
			// that would make folding a way to stop being told an agent is
			// waiting, which is the opposite of what folding is for.
			Trailing: groupStateLabel(src.snap, members),
			Active:   inGroup,
			Depth:    src.depth,
			Machine:  src.machine,
			Stale:    src.stale,
		})
		if folded {
			return
		}
		for _, member := range members {
			rows = append(rows, t.spaceRowFrom(src, member, src.depth+1))
		}
	}

	for _, w := range src.snap.Workspaces {
		if !shown(w) {
			continue
		}
		if w.Group == "" {
			rows = append(rows, t.spaceRowFrom(src, w, src.depth))
			continue
		}
		if seen[w.Group] {
			continue
		}
		flushBefore(w.Group)
		emit(w.Group)
	}
	for _, g := range groups {
		if unplaced(g) && !seen[g.Name] {
			emit(g.Name)
		}
	}
	return rows
}

// groupDirLocked is the folder a group's new spaces start in, or "".
func (t *tui) groupDirLocked(group string) string {
	for _, g := range t.snap.Groups {
		if g.Name == group {
			return g.Dir
		}
	}
	return ""
}

// groupsIn is a session's groups in the order they were made: the ones its
// server keeps, empty ones too, or from a server that keeps no record of
// them the names its spaces carry, as a group was before.
func groupsIn(snap *proto.SessionSnapshot) []proto.GroupInfo {
	if len(snap.Groups) > 0 {
		return snap.Groups
	}
	var out []proto.GroupInfo
	seen := make(map[string]bool)
	for _, w := range snap.Workspaces {
		if w.Group != "" && !seen[w.Group] {
			seen[w.Group] = true
			out = append(out, proto.GroupInfo{Name: w.Group})
		}
	}
	return out
}

// spaceRowFrom is one space, at the given depth.
func (t *tui) spaceRowFrom(src spaceSource, w proto.WorkspaceInfo, depth int) ui.SidebarRow {
	state := spaceStateIn(src.snap, w)
	return ui.SidebarRow{
		Kind:  ui.SidebarSpace,
		Label: orDash(w.Name),
		Lines: ui.ResolveSpaceRows(t.config.UI.Sidebar.SpaceRows(), ui.SpaceTokenValues{
			StateText: state, Workspace: orDash(w.Name), Branch: w.Branch,
			Ahead: w.Ahead, Behind: w.Behind, Custom: spaceTokens(w),
		}),
		Gap:       t.config.UI.Sidebar.Spaces.RowGap,
		Symbols:   t.config.UI.StatusIndicators == "symbols",
		Group:     w.Group,
		Depth:     depth,
		Workspace: w.ID,
		DropHere:  src.machine == "" && w.ID == t.spaceDropTarget,
		State:     state,
		Running:   true,
		Active:    w.ID == src.current,
		Machine:   src.machine,
		Stale:     src.stale,
	}
}

// hasGroupsLocked reports whether there is a group to move a space to.
func (t *tui) hasGroupsLocked() bool {
	return len(groupsIn(&t.snap)) > 0
}

// groupOfLocked is the group a space is in, or "".
func (t *tui) groupOfLocked(workspace uint64) string {
	for _, w := range t.snap.Workspaces {
		if w.ID == workspace {
			return w.Group
		}
	}
	return ""
}

// groupMembersLocked returns a group's spaces in session order.
func (t *tui) groupMembersLocked(group string) []proto.WorkspaceInfo {
	return groupMembersIn(&t.snap, group)
}

// groupMembersIn returns a group's spaces in one session, in its order.
func groupMembersIn(snap *proto.SessionSnapshot, group string) []proto.WorkspaceInfo {
	var out []proto.WorkspaceInfo
	for _, w := range snap.Workspaces {
		if w.Group == group {
			out = append(out, w)
		}
	}
	return out
}

// inGroupLocked reports whether the space being looked at is in a group.
func (t *tui) inGroupLocked(group string) bool {
	for _, w := range t.snap.Workspaces {
		if w.ID == t.workspace {
			return w.Group == group
		}
	}
	return false
}

// groupStateLabel is what a group heading says about its members: the number
// of them that want attention, or nothing when none do.
func groupStateLabel(snap *proto.SessionSnapshot, members []proto.WorkspaceInfo) string {
	blocked := 0
	for _, w := range members {
		if spaceStateIn(snap, w) == "blocked" {
			blocked++
		}
	}
	if blocked == 0 {
		return ""
	}
	return itoaInt(blocked) + " waiting"
}

// agentRowsLocked lists what is running, flat or under its tab.
//
// With saved machines it is herdr's aggregate list (endpoint_agents.rs):
// every machine's agents, each row naming its machine through the machine
// token, and in priority order the attention queue across all of them, with
// what cannot be reached last.
func (t *tui) agentRowsLocked() []ui.SidebarRow {
	priority := t.config.UI.AgentPanelSort == "priority"
	if !t.multiMachineLocked() {
		// The machine is named only when it is another one: every row saying
		// this laptop's name is a column of noise.
		rows, seqs := t.agentEntriesFrom(agentSource{snap: &t.snap, label: t.host, here: true})
		if view := t.snap.AgentView; view != nil {
			// A view a script set replaces the order and decides what is
			// shown, as herdr's agent_view_override does; tab headings mean
			// nothing in an order that is not the session's.
			return t.applyAgentViewLocked(view, rows, paneInfo(&t.snap))
		}
		if priority {
			rows = byAttention(rows, seqs, nil)
		}
		return rows
	}

	var rows []ui.SidebarRow
	var seqs []uint64
	var stale []bool
	for _, e := range t.machines.endpoints {
		src := agentSource{snap: &e.snap, machine: e.id, label: e.label, stale: e.status != ui.MachineOnline}
		if e.id == t.machines.active {
			src = agentSource{snap: &t.snap, label: e.label, here: true}
		} else if !e.have {
			continue
		}
		r, s := t.agentEntriesFrom(src)
		if src.here && t.snap.AgentView != nil {
			// The view is the shown session's, and orders only its agents;
			// the other machines' follow in their own order.
			r = t.applyAgentViewLocked(t.snap.AgentView, r, paneInfo(&t.snap))
			s = make([]uint64, len(r))
		}
		rows, seqs = append(rows, r...), append(seqs, s...)
		for range r {
			stale = append(stale, src.stale)
		}
	}
	if priority && t.snap.AgentView == nil {
		rows = byAttention(rows, seqs, stale)
	}
	return rows
}

// agentSource is one machine's session as the agent list reads it.
type agentSource struct {
	snap *proto.SessionSnapshot
	// machine is empty for the session shown; label is what the machine
	// token says, empty when there is nothing to say.
	machine, label string
	here, stale    bool
}

// paneInfo indexes a session's panes.
func paneInfo(snap *proto.SessionSnapshot) map[uint64]proto.PaneInfo {
	info := make(map[uint64]proto.PaneInfo, len(snap.Panes))
	for _, p := range snap.Panes {
		info[p.ID] = p
	}
	return info
}

// agentEntriesFrom is one session's agents in session order, under their
// tabs when the list is grouped, with each one's state sequence for the
// attention order.
func (t *tui) agentEntriesFrom(src agentSource) ([]ui.SidebarRow, []uint64) {
	info := paneInfo(src.snap)
	priority := t.config.UI.AgentPanelSort == "priority"
	symbols := t.config.UI.StatusIndicators == "symbols"
	multi := t.multiMachineLocked()

	var rows []ui.SidebarRow
	var seqs []uint64
	for _, w := range src.snap.Workspaces {
		if src.here && !t.shownLocked(w.ID) {
			// A company chosen shows its agents only; the heading above
			// says when one elsewhere is waiting.
			continue
		}
		for _, tab := range w.Tabs {
			var entries []ui.SidebarRow
			var entrySeqs []uint64
			for _, id := range tab.Panes {
				p := info[id]
				if p.Agent == "" {
					// A shell is not an agent. Listing every pane would make
					// the list as long as the session and hide what it exists
					// to show.
					continue
				}
				entries = append(entries, ui.SidebarRow{
					Kind:      ui.SidebarAgent,
					Label:     agentLabel(w, tab),
					Lines:     t.agentLinesLocked(w, tab, p, src.label),
					Gap:       t.config.UI.Sidebar.Agents.RowGap,
					Symbols:   symbols,
					Pane:      id,
					Tab:       tab.ID,
					Workspace: w.ID,
					State:     displayState(p),
					Running:   p.Running,
					Active:    src.here && id == t.focus && tab.ID == t.tab && w.ID == t.workspace,
					Machine:   src.machine,
					Stale:     src.stale,
				})
				entrySeqs = append(entrySeqs, p.StateSeq)
			}
			if len(entries) == 0 {
				continue
			}
			if t.grouped && !priority {
				label := orDash(w.Name) + " · " + orDash(tab.Name)
				if multi {
					label = src.label + " · " + label
				}
				rows = append(rows, ui.SidebarRow{
					Kind:      ui.SidebarGroup,
					Label:     label,
					Tab:       tab.ID,
					Workspace: w.ID,
					Machine:   src.machine,
				})
				seqs = append(seqs, 0)
			}
			rows = append(rows, entries...)
			seqs = append(seqs, entrySeqs...)
		}
	}
	return rows, seqs
}

// byAttention is herdr's attention queue: what needs you first, and within
// that the most recent change first; across machines, what cannot be reached
// after what can (sort_aggregate_rows). Tab headings mean nothing in this
// order, and the list has none in it.
//
// A state sequence is counted by each server on its own, so between two
// machines "more recent" is only a guess; within one it is exact.
func byAttention(rows []ui.SidebarRow, seqs []uint64, stale []bool) []ui.SidebarRow {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		if stale != nil && stale[order[a]] != stale[order[b]] {
			return !stale[order[a]]
		}
		ra, rb := rows[order[a]], rows[order[b]]
		pa, pb := attentionPriority(ra.State, ra.Running), attentionPriority(rb.State, rb.Running)
		if pa != pb {
			return pa > pb
		}
		return seqs[order[a]] > seqs[order[b]]
	})
	sorted := make([]ui.SidebarRow, 0, len(rows))
	for _, at := range order {
		if rows[at].Kind == ui.SidebarGroup {
			continue
		}
		sorted = append(sorted, rows[at])
	}
	return sorted
}

// applyAgentViewLocked filters and orders the agent rows by a view. The
// caller holds the lock.
func (t *tui) applyAgentViewLocked(view *agentview.View, rows []ui.SidebarRow, info map[uint64]proto.PaneInfo) []ui.SidebarRow {
	wsOrder := map[uint64]uint64{}
	tabOrder := map[uint64]uint64{}
	paneOrder := map[uint64]uint64{}
	for i, w := range t.snap.Workspaces {
		wsOrder[w.ID] = uint64(i + 1)
		for j, tab := range w.Tabs {
			tabOrder[tab.ID] = uint64(j + 1)
			for k, id := range tab.Panes {
				paneOrder[id] = uint64(k + 1)
			}
		}
	}
	ctx := agentview.Context{WorkspaceID: fmt.Sprintf("w_%d", t.workspace), TabID: fmt.Sprintf("t_%d", t.tab)}
	type entry struct {
		row ui.SidebarRow
		e   agentview.Entry
	}
	var kept []entry
	for _, r := range rows {
		if r.Kind != ui.SidebarAgent {
			continue
		}
		p := info[r.Pane]
		state := displayState(p)
		if !p.Running || state == "" {
			state = "unknown"
		}
		e := agentview.Entry{
			Status: state, WorkspaceID: fmt.Sprintf("w_%d", r.Workspace), TabID: fmt.Sprintf("t_%d", r.Tab),
			PaneID: fmt.Sprintf("p_%d", r.Pane), Agent: p.Agent, Seen: !p.Done, StateChangeSeq: p.StateSeq,
			WorkspaceOrder: wsOrder[r.Workspace], TabOrder: tabOrder[r.Tab], PaneOrder: paneOrder[r.Pane],
			Attention: uint64(attentionPriority(state, p.Running)),
			Tokens:    map[string]string{},
		}
		for _, tok := range p.Tokens {
			e.Tokens[tok.Key] = tok.Value
		}
		if view.Filter == nil || view.Filter.Matches(ctx, e) {
			kept = append(kept, entry{row: r, e: e})
		}
	}
	if len(view.Sort) > 0 {
		sort.SliceStable(kept, func(i, j int) bool { return view.Less(kept[i].e, kept[j].e) })
	}
	out := make([]ui.SidebarRow, 0, len(kept))
	for _, k := range kept {
		out = append(out, k.row)
	}
	return out
}

// displayState is the state as the list shows it: herdr's "done" for an
// agent that finished while nobody was looking.
func displayState(p proto.PaneInfo) string {
	if p.Done && p.State == "idle" {
		return "done"
	}
	return p.State
}

// attentionPriority is herdr's tab_attention_priority.
func attentionPriority(state string, running bool) int {
	if !running {
		return 0
	}
	switch state {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	}
	return 0
}

// spaceTokens are the values reported about a space, for its $name tokens.
func spaceTokens(w proto.WorkspaceInfo) map[string]string {
	if len(w.Tokens) == 0 {
		return nil
	}
	out := make(map[string]string, len(w.Tokens))
	for _, t := range w.Tokens {
		out[t.Key] = t.Value
	}
	return out
}

// agentLinesLocked lays an agent's entry out as the settings say, herdr's
// agent_rows: what each token says, from the session. The caller holds the
// lock.
func (t *tui) agentLinesLocked(w proto.WorkspaceInfo, tab proto.TabInfo, p proto.PaneInfo, machine string) [][]ui.SidebarToken {
	v := ui.AgentTokenValues{
		StateText: displayState(p),
		// Empty for this machine when it is the only one; with saved
		// machines, every row names its own, "Local" included, as herdr's
		// do.
		Machine:   machine,
		Workspace: orDash(w.Name),
		Tab:       tab.Name,
		Agent:     p.Agent,
		Custom:    make(map[string]string, len(p.Tokens)),
	}
	if label := p.StateLabels[displayState(p)]; label != "" {
		v.StateText = label // what a hook calls this state for this pane
	}
	if p.Display != "" {
		v.Agent = p.Display
	}
	// One title goes over the wire: the user's name when there is one, the
	// program's own otherwise.
	if p.Named {
		v.Pane = p.Title
	} else {
		v.TerminalTitle = p.Title
		v.TerminalTitleStripped = strippedTerminalTitle(p.Title)
	}
	for _, token := range p.Tokens {
		v.Custom[token.Key] = token.Value
	}
	return ui.ResolveAgentRows(t.config.UI.Sidebar.AgentRows(p.Agent), v)
}

// agentLabel names where an agent is, since what it is goes underneath.
func agentLabel(w proto.WorkspaceInfo, tab proto.TabInfo) string {
	name := orDash(w.Name)
	if tab.Name != "" {
		name += " · " + tab.Name
	}
	return name
}

// spaceStateLocked is the most urgent thing happening in a space, because a
// space marked idle while an agent inside it waits for an answer is worse
// than no mark at all.
func (t *tui) spaceStateLocked(w proto.WorkspaceInfo) string {
	return spaceStateIn(&t.snap, w)
}

// spaceStateIn is spaceStateLocked for any machine's session.
func spaceStateIn(snap *proto.SessionSnapshot, w proto.WorkspaceInfo) string {
	panes := make(map[uint64]bool)
	for _, tab := range w.Tabs {
		for _, id := range tab.Panes {
			panes[id] = true
		}
	}

	// The most urgent, by herdr's attention order: an agent waiting beats one
	// that finished unseen, which beats one still working.
	state, best := "", 0
	for _, p := range snap.Panes {
		if !panes[p.ID] || p.Agent == "" || !p.Running {
			continue
		}
		if s := displayState(p); attentionPriority(s, true) > best {
			state, best = s, attentionPriority(s, true)
		}
	}
	return state
}

// navTarget is where a sidebar row goes, and is how the navigation cursor
// remembers its place.
//
// The rows are rebuilt from the session on every refresh, so an index into
// them is a position in a list that moves underneath the cursor. What the row
// points at does not move.
type navTarget struct {
	pane      uint64
	workspace uint64
	// group is set for a group heading, which is a place the cursor stops so
	// that a folded group can be opened without reaching for the mouse.
	group string
	// machine is set for a row on another saved machine, and for a
	// machine's own row, which header says.
	machine string
	header  bool
	// tool is set for a tool on the toolbar, the first places the cursor
	// stops, above the spaces.
	tool string
}

// targetOf returns where a row goes, and whether it goes anywhere. Headings,
// actions and spacers do not.
func targetOf(r ui.SidebarRow) (navTarget, bool) {
	switch r.Kind {
	case ui.SidebarAgent:
		return navTarget{pane: r.Pane, workspace: r.Workspace, machine: r.Machine}, true
	case ui.SidebarSpace:
		return navTarget{workspace: r.Workspace, machine: r.Machine}, true
	case ui.SidebarSpaceGroup:
		return navTarget{group: r.Group, machine: r.Machine}, true
	case ui.SidebarMachine:
		return navTarget{machine: r.Machine, header: true}, true
	}
	return navTarget{}, false
}

// sidebarWalkLocked is both lists end to end, which is the order the keyboard
// moves through them.
//
// The divider is a thing to look at, not a thing to stop on: pressing down at
// the last space should reach the first agent, the same as dragging the eye
// down the column does.
func (t *tui) sidebarWalkLocked() []ui.SidebarRow {
	spaces := t.spacesSectionLocked()
	return append(spaces.Rows, t.agentsSectionLocked().Rows...)
}

// startTargetLocked is where the cursor opens: the focused pane if it is in
// the list, and otherwise the space being looked at.
//
// The fallback matters more than it looks. Most panes are shells, and shells
// are not listed, so opening on "nothing selected" would make the first
// keypress move from an arbitrary end of the list rather than from here.
func (t *tui) startTargetLocked() navTarget {
	rows := t.sidebarWalkLocked()
	focused := navTarget{pane: t.focus, workspace: t.workspace}
	space := navTarget{workspace: t.workspace}

	var first navTarget
	found := false
	for _, r := range rows {
		target, ok := targetOf(r)
		if !ok {
			continue
		}
		if target == focused {
			return focused
		}
		if target == space {
			found = true
		}
		if first == (navTarget{}) {
			first = target
		}
	}
	if found {
		return space
	}
	return first
}

// navigate moves the selection down the sidebar, over everything that goes
// somewhere and nothing that does not.
//
// Spaces are walked as well as agents. A space with no agent running in it is
// still somewhere the user wants to reach, and leaving it out would mean the
// keyboard could not reach half of what the mouse can click.
func (t *tui) navigate(delta int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rows := t.sidebarWalkLocked()
	targets := make([]navTarget, 0, len(rows)+4)
	for _, it := range t.toolbarLocked() {
		targets = append(targets, navTarget{tool: it.ID})
	}
	for _, r := range rows {
		if target, ok := targetOf(r); ok {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return
	}

	at := 0
	for i, target := range targets {
		if target == t.nav {
			at = i
			break
		}
	}
	t.nav = targets[(at+delta+len(targets))%len(targets)]
	if t.nav.tool != "" {
		// A tool's name, as its hover says it, since the icon alone does not.
		t.message, t.alert, t.msgAt = toolLabels[t.nav.tool], false, time.Now()
	}
	t.revealSidebarLocked()
	t.dirty = true
}

// toggleGroup folds a group shut or open.
func (t *tui) toggleGroup(group string) {
	t.mu.Lock()
	t.folded[group] = !t.folded[group]
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// enterNavigate opens the list and puts the cursor on the focused pane, so
// moving from it is relative to where the user already is.
func (t *tui) enterNavigate() {
	t.mu.Lock()
	t.sidebar = true
	t.navigating = true
	t.nav = t.startTargetLocked()
	t.revealSidebarLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) leaveNavigate() {
	t.mu.Lock()
	t.navigating = false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// navigateKeys drives the list while it has the keyboard.
func (t *tui) navigateKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		handled, err := t.navigateKey(key)
		if err != nil {
			return true, err
		}
		if !handled {
			return false, nil
		}
	}
	return len(data) > 0, nil
}

func (t *tui) navigateKey(key string) (bool, error) {
	switch key {
	case "\x1b[A", "k":
		t.navigate(-1)
		return true, nil
	case "\x1b[B", "j":
		t.navigate(1)
		return true, nil
	case "\r", "\n":
		t.mu.Lock()
		target := t.nav
		t.mu.Unlock()
		t.leaveNavigate()
		switch {
		case target.tool != "":
			return true, t.runTool(target.tool)
		case target.header:
			return true, t.clickMachine(target.machine)
		case target.machine != "" && target.workspace != 0:
			return true, t.switchMachine(target.machine, target.workspace, target.pane)
		case target.machine != "" && target.group != "":
			t.toggleRemoteGroup(target.machine, target.group)
			return true, nil
		}
		if target.pane != 0 {
			return true, t.jumpToPane(target.pane)
		}
		if target.workspace != 0 {
			return true, t.showWorkspace(target.workspace)
		}
		if target.group != "" {
			// A group is not somewhere to go, so choosing it opens it. That
			// is the only thing there is to do with a heading, and leaving it
			// inert would make a folded group a dead end for the keyboard.
			t.toggleGroup(target.group)
		}
		return true, nil
	case "\x1b", "q", "\x03":
		t.leaveNavigate()
		return true, nil
	}
	// Anything else leaves the list rather than being swallowed by a mode the
	// user has stopped thinking about.
	t.leaveNavigate()
	return false, nil
}

// waitingLocked counts the agents anywhere in the session that are blocked on
// an answer.
//
// Anywhere, not here: the reason to run tend is that the one needing you is
// usually not the one on screen. Panes with no agent are not counted — a shell
// sitting at a prompt is not waiting for anybody.
func (t *tui) waitingLocked() int {
	n := 0
	for _, p := range t.snap.Panes {
		if p.Agent != "" && p.Running && p.State == "blocked" {
			n++
		}
	}
	return n
}

// paneShell is the command a new pane runs: this machine's shell for a
// session here, and nothing — the server's own — for a session on another
// machine, whose shells this client knows nothing about. A server too old
// to choose one gets this machine's, as before.
func (t *tui) paneShell() []string {
	if t.host != "" && t.serverHas(proto.FeatureServerShell) {
		return nil
	}
	return t.config.Shell()
}
