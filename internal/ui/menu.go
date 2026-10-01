package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// A context menu is how everything that is not a jump gets done with the
// mouse: closing a pane, renaming a space, splitting where you are pointing.
//
// It exists because the alternative is a key for each of those, and a key for
// each is a list nobody reads. The menu is opened on the thing it acts on, so
// what it will affect is never in question — there is no "current selection"
// to be wrong about.

// Menu action names. They are the commands the client dispatches, so a menu
// item and a key binding end up in the same place.
const (
	MenuClose      = "menu-close"
	MenuRename     = "menu-rename"
	MenuNewTab     = "menu-new-tab"
	MenuNewSpace   = "menu-new-space"
	MenuSplitRight = "menu-split-right"
	MenuSplitDown  = "menu-split-down"
	MenuZoom       = "menu-zoom"
	MenuGoTo       = "menu-go-to"
	MenuGroup      = "menu-group"
	MenuFold       = "menu-fold"
	// MenuMoveBack and MenuMoveOn move a tab or a space one place along its
	// row. herdr has the actions and binds no key to them; a menu is where
	// somebody looking to reorder a tab will look.
	// MenuRenamePane names the pane itself rather than its tab.
	MenuRenamePane = "menu-rename-pane"
	// The worktree actions, herdr's: make one for this space's repository,
	// open one that exists, remove the one this space is.
	MenuNewWorktree    = "menu-new-worktree"
	MenuOpenWorktree   = "menu-open-worktree"
	MenuRemoveWorktree = "menu-remove-worktree"
	// MenuMoveToGroup lists the groups a space can be moved into, and
	// MenuPickGroup is one of them, with its name in Arg — or no name, for
	// taking the space out of the group it is in.
	MenuMoveToGroup = "menu-move-to-group"
	MenuPickGroup   = "menu-pick-group"
	// MenuGroupDir asks for the folder a group's new spaces start in, and
	// MenuGroupDirClear forgets it.
	MenuGroupDir      = "menu-group-dir"
	MenuGroupDirClear = "menu-group-dir-clear"
	// MenuGroupAgent lists the agents installed here, and MenuGroupAgentStart
	// starts the one in Arg in a new space of the group, in its folder.
	MenuGroupAgent      = "menu-group-agent"
	MenuGroupAgentStart = "menu-group-agent-start"
	// MenuContinueIn offers the agents a pane's task can be handed to, and
	// MenuContinueWith hands it to the one in Arg (docs/MEMORY.md).
	MenuContinueIn   = "continue-in"
	MenuContinueWith = "continue-with"
	// MenuFiles opens or closes the file explorer panel, prefix+f.
	MenuFiles    = "menu-files"
	MenuMoveBack = "menu-move-back"
	MenuMoveOn   = "menu-move-on"
	// The global menu's, herdr's (`client/shell/global_menu.rs`): the
	// settings screen, the list of keys, re-reading the settings, the
	// release notes, and detaching.
	MenuSettings = "menu-settings"
	MenuKeybinds = "menu-keybinds"
	MenuReload   = "menu-reload"
	MenuWhatsNew = "menu-whats-new"
	MenuDetach   = "menu-detach"
	// MenuAbout opens the about panel (about.go), tend's own: which tend
	// is running, the server's, who makes it and where it lives.
	MenuAbout = "menu-about"
)

// VersionLine says which tend is running: this client's build, and the
// server's beside it when they differ — which is the server that outlived
// an update, the thing to know before anything else about what it does.
func VersionLine(client, server string) string {
	line := "tend " + DisplayVersion(client)
	if server != "" && DisplayVersion(server) != DisplayVersion(client) {
		line += " · server " + DisplayVersion(server)
	}
	return line
}

// DisplayVersion is a build as it is shown: a release with its v, anything
// else — "development build" — as it is.
func DisplayVersion(s string) string {
	if s == "" || strings.HasPrefix(s, "v") || !strings.ContainsAny(s[:1], "0123456789") {
		return s
	}
	return "v" + s
}

// GlobalMenu is herdr's global menu, which its sidebar's "menu" button
// opens: settings, keybinds, reload config, the release notes when there
// are any — "update ready" with a dot while they are a release newer than
// the one running, "what's new" otherwise — and detach. tend's opens with
// "about tend", which says which tend is running — herdr shows that on its
// command line only — marked with a dot, as update ready is, while the
// server is another build than this client.
func GlobalMenu(stale, ready, notes bool, x, y int) Menu {
	about := "about tend"
	if stale {
		about += " ●"
	}
	items := []MenuItem{{Label: about, Action: MenuAbout}}
	items = append(items, []MenuItem{
		{Label: "settings", Action: MenuSettings},
		{Label: "keybinds", Action: MenuKeybinds},
		{Label: "reload config", Action: MenuReload},
	}...)
	switch {
	case ready:
		items = append(items, MenuItem{Label: "update ready ●", Action: MenuWhatsNew})
	case notes:
		items = append(items, MenuItem{Label: "what's new", Action: MenuWhatsNew})
	}
	return Menu{Title: "menu", Items: append(items, MenuItem{Label: "detach", Action: MenuDetach}), X: x, Y: y}
}

// MenuItem is one line of a menu.
type MenuItem struct {
	Label  string
	Action string
	// Arg carries what an item built at run time is about: which worktree a
	// "open" item opens. Fixed menus leave it empty.
	Arg string
}

// Menu is a popup anchored at the point it was opened on, acting on whichever
// of Pane, Tab and Workspace is set.
type Menu struct {
	Title string
	Items []MenuItem
	// X and Y are where it was opened, which is its top-left corner unless
	// that would put it off the screen.
	X, Y int
	// Selected is the keyboard cursor. The mouse does not use it: clicking an
	// item runs that item, not whatever the cursor was on.
	Selected int

	Pane      uint64
	Tab       uint64
	Workspace uint64
	// Group names the group a group menu acts on, which has no identifier of
	// its own because it has no record of its own.
	Group string
	// Conversation is the agent conversation a "continue in" menu hands on
	// when it is not a pane's: one chosen in the sessions list.
	Conversation string
}

// menuPadding is the space either side of a label inside the border.
const menuPadding = 2

// Rect is where the menu is drawn, clamped to the screen.
//
// A menu that hangs off the bottom of the terminal loses exactly the items
// added last, which are the destructive ones, so it is moved rather than
// clipped.
func (m Menu) Rect(cols, rows int) Rect {
	width := runewidth.StringWidth(m.Title)
	for _, item := range m.Items {
		width = max(width, runewidth.StringWidth(item.Label))
	}
	r := Rect{
		Cols: min(width+2*menuPadding+2, cols),
		Rows: min(len(m.Items)+2, rows),
	}
	r.X = min(max(m.X, 0), max(cols-r.Cols, 0))
	r.Y = min(max(m.Y, 0), max(rows-r.Rows, 0))
	return r
}

// MenuItemAt returns the item under a point, and whether the point is inside
// the menu at all.
//
// The two answers are separate on purpose: a click inside the border but not
// on an item must not fall through to whatever is behind the menu, or the
// menu would close and act on the pane underneath in one gesture.
func MenuItemAt(m Menu, cols, rows, x, y int) (item MenuItem, onItem, inside bool) {
	r := m.Rect(cols, rows)
	if x < r.X || x >= r.X+r.Cols || y < r.Y || y >= r.Y+r.Rows {
		return MenuItem{}, false, false
	}
	i := y - r.Y - 1
	if i < 0 || i >= len(m.Items) {
		return MenuItem{}, false, true
	}
	return m.Items[i], true, true
}

// drawMenu paints the popup over whatever is behind it.
func drawMenu(dst *vt.Grid, m Menu, theme Theme) {
	if len(m.Items) == 0 {
		return
	}
	r := m.Rect(dst.Cols(), dst.Rows())

	for y := r.Y; y < r.Y+r.Rows; y++ {
		fill(dst, y, r.X, r.X+r.Cols, theme.Menu)
	}
	drawBox(dst, r, theme.MenuTitle)
	if m.Title != "" {
		writeString(dst, r.X+menuPadding, r.Y, " "+truncate(m.Title, r.Cols-4)+" ", theme.MenuTitle, r.X+r.Cols)
	}

	limit := r.X + r.Cols - 1
	for i, item := range m.Items {
		y := r.Y + 1 + i
		if y >= r.Y+r.Rows-1 {
			break
		}
		style := theme.Menu
		if i == m.Selected {
			// The whole row takes the mark, so it reads as a band rather than
			// as a word that changed weight. What the pointer is on has to be
			// obvious before the click, not after it.
			style = theme.MenuSelected
			fill(dst, y, r.X+1, limit, style)
		}
		writeString(dst, r.X+menuPadding, y, truncate(item.Label, r.Cols-2*menuPadding), style, limit)
	}
}

// PaneMenu is what a right-click on a pane offers.
//
// A pane running an agent also offers to hand its task to another agent.
func PaneMenu(pane uint64, x, y int, closable, agent bool) Menu {
	items := []MenuItem{
		{Label: "split right", Action: MenuSplitRight},
		{Label: "split down", Action: MenuSplitDown},
		{Label: "zoom", Action: MenuZoom},
		{Label: "rename pane", Action: MenuRenamePane},
		{Label: "rename tab", Action: MenuRename},
		{Label: "files panel", Action: MenuFiles},
	}
	if agent {
		items = append(items, MenuItem{Label: "continue in...", Action: MenuContinueIn})
	}
	if closable {
		items = append(items, MenuItem{Label: "close pane", Action: MenuClose})
	}
	return Menu{Title: "pane", Items: items, X: x, Y: y, Pane: pane}
}

// TabMenu is what a right-click on the tab bar offers.
func TabMenu(tab uint64, x, y int) Menu {
	return Menu{
		Title: "tab",
		Items: []MenuItem{
			{Label: "new tab", Action: MenuNewTab},
			{Label: "rename", Action: MenuRename},
			{Label: "move left", Action: MenuMoveBack},
			{Label: "move right", Action: MenuMoveOn},
			{Label: "close tab", Action: MenuClose},
		},
		X: x, Y: y, Tab: tab,
	}
}

// SpaceMenu is what a right-click on a space offers. groups says whether
// the session has any group yet: making one is always offered, where it
// can be seen, and moving into one only once there is one to move into.
func SpaceMenu(workspace uint64, groups bool, x, y int) Menu {
	items := []MenuItem{
		{Label: "new space", Action: MenuNewSpace},
		{Label: "new tab", Action: MenuNewTab},
		{Label: "rename", Action: MenuRename},
		{Label: "new group...", Action: MenuGroup},
	}
	if groups {
		items = append(items, MenuItem{Label: "move to group...", Action: MenuMoveToGroup})
	}
	return Menu{
		Title: "space",
		Items: append(items, []MenuItem{
			{Label: "new worktree...", Action: MenuNewWorktree},
			{Label: "open worktree...", Action: MenuOpenWorktree},
			{Label: "remove worktree", Action: MenuRemoveWorktree},
			{Label: "move up", Action: MenuMoveBack},
			{Label: "move down", Action: MenuMoveOn},
			{Label: "close space", Action: MenuClose},
		}...),
		X: x, Y: y, Workspace: workspace,
	}
}

// GroupPickMenu lists where a space can go: each group it is not in, a new
// one (also on the space menu itself), and out of the one it is in. Typing a name that already exists is
// how a space joined a group before, and it is easy to get one letter wrong
// and make a second group; picking it from a list is not.
func GroupPickMenu(workspace uint64, current string, groups []string, x, y int) Menu {
	var items []MenuItem
	for _, g := range groups {
		if g != current {
			items = append(items, MenuItem{Label: g, Action: MenuPickGroup, Arg: g})
		}
	}
	items = append(items, MenuItem{Label: "new group...", Action: MenuGroup})
	if current != "" {
		items = append(items, MenuItem{Label: "remove from group", Action: MenuPickGroup})
	}
	return Menu{Title: "move to group", Items: items, X: x, Y: y, Workspace: workspace}
}

// GroupMenu is what a right-click on a group heading offers.
//
// It acts on the group as a whole, which is the set of spaces naming it: there
// is no group record to rename, so renaming one moves every member.
//
// folder is the one the group's new spaces start in, shown as it is written
// on the status line, or empty when none is set: the item that sets it says
// which it is now, so finding out does not mean opening the prompt.
func GroupMenu(group string, folded bool, folder string, x, y int) Menu {
	fold := "fold"
	if folded {
		fold = "unfold"
	}
	items := []MenuItem{
		{Label: fold, Action: MenuFold},
		{Label: "new space here", Action: MenuNewSpace},
		{Label: "new agent here...", Action: MenuGroupAgent},
	}
	if folder == "" {
		items = append(items, MenuItem{Label: "set folder...", Action: MenuGroupDir})
	} else {
		items = append(items,
			MenuItem{Label: "folder: " + folder, Action: MenuGroupDir},
			MenuItem{Label: "clear folder", Action: MenuGroupDirClear})
	}
	items = append(items,
		MenuItem{Label: "rename group", Action: MenuRename},
		MenuItem{Label: "delete group", Action: MenuClose})
	return Menu{Title: "group", Items: items, X: x, Y: y, Group: group}
}

// AgentMenu is what a right-click on an agent row offers. It acts on the
// pane the agent is running in, which is the only thing there is to act on.
func AgentMenu(pane, tab, workspace uint64, x, y int) Menu {
	return Menu{
		Title: "agent",
		Items: []MenuItem{
			{Label: "go to", Action: MenuGoTo},
			{Label: "continue in...", Action: MenuContinueIn},
			{Label: "rename tab", Action: MenuRename},
			{Label: "close pane", Action: MenuClose},
		},
		X: x, Y: y, Pane: pane, Tab: tab, Workspace: workspace,
	}
}

// AgentChoice is an agent a menu offers to start: its catalog id, and the
// name it is shown by.
type AgentChoice struct {
	ID, Name string
}

// GroupAgentMenu lists the agents that can be started in a group: each one
// starts in a new space of the group, in the group's folder.
func GroupAgentMenu(group string, agents []AgentChoice, x, y int) Menu {
	items := make([]MenuItem, 0, len(agents))
	for _, a := range agents {
		items = append(items, MenuItem{Label: a.Name, Action: MenuGroupAgentStart, Arg: a.ID})
	}
	return Menu{Title: "start in " + group, Items: items, X: x, Y: y, Group: group}
}

// ContinueMenu lists the agents a pane's task can be handed to — the same
// one too, which is a fresh conversation with the task's note.
func ContinueMenu(pane uint64, conversation string, agents []string, x, y int) Menu {
	items := make([]MenuItem, 0, len(agents))
	for _, a := range agents {
		items = append(items, MenuItem{Label: a, Action: MenuContinueWith, Arg: a})
	}
	return Menu{Title: "continue in", Items: items, X: x, Y: y, Pane: pane, Conversation: conversation}
}
