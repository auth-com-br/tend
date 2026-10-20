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
	// MenuMoveToCompany lists the companies a group can be moved to, and
	// MenuPickCompany is one of them, its id in Arg.
	MenuMoveToCompany = "menu-move-to-company"
	MenuPickCompany   = "menu-pick-company"
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
	// MenuCopy and MenuPaste are the pane menu's clipboard: copy what is
	// selected in the pane, paste the clipboard into it. herdr's pane menu
	// has neither (PORTING.md, "Different from herdr on purpose").
	MenuCopy  = "menu-copy"
	MenuPaste = "menu-paste"
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
	// MenuPhone opens the phone view panel (phoneview.go): tend web turned
	// on and off from the menu.
	MenuPhone = "menu-phone"
	// MenuMCP opens the MCP manager, and MenuTools the list of every tool
	// tend has, each said in words with its key: tend's own, so that one
	// menu leads to everything for somebody who knows none of the keys.
	MenuMCP   = "menu-mcp"
	MenuTools = "menu-tools"
	// MenuTool opens the tool in Arg (a toolbar id).
	MenuTool = "menu-tool"
	// MenuMCPDo is an action on the MCP manager's row, from its own menu;
	// Arg is the action and the agent it is for, "action:agent".
	MenuMCPDo = "menu-mcp-do"
	// MenuPermissions opens the permissions manager, and MenuPermDo is an
	// action of its own menus; Arg says which.
	MenuPermissions = "menu-permissions"
	MenuPermDo      = "menu-perm-do"
	// MenuSleep puts the pane's agent to sleep — its process ended, its
	// conversation kept — and MenuWake brings it back.
	MenuSleep = "menu-sleep"
	MenuWake  = "menu-wake"
	// MenuSleepPick lists the agents that are awake, to put one to sleep
	// (MenuSleepOne, Arg its pane) or every idle one (MenuSleepIdle).
	MenuSleepPick = "menu-sleep-pick"
	MenuSleepOne  = "menu-sleep-one"
	MenuSleepIdle = "menu-sleep-idle"
)

// SleepChoice is an awake agent as the sleep menu offers it.
type SleepChoice struct {
	Pane  uint64
	Label string
	Idle  bool
}

// SleepMenu lists the agents that are awake, every idle one first as one
// item, then each by itself. One working is offered too, marked: putting
// it to sleep ends what it is doing, which is the person's to decide.
func SleepMenu(choices []SleepChoice, x, y int) Menu {
	var items []MenuItem
	idle := 0
	for _, c := range choices {
		if c.Idle {
			idle++
		}
	}
	if idle > 1 {
		items = append(items, MenuItem{Label: "every idle agent (" + itoa(uint64(idle)) + ")", Action: MenuSleepIdle})
	}
	for _, c := range choices {
		items = append(items, MenuItem{Label: c.Label, Action: MenuSleepOne, Arg: itoa(c.Pane)})
	}
	if len(items) == 0 {
		items = append(items, MenuItem{Label: "no agent is awake"})
	}
	return Menu{Title: "sleep which agent?", Items: items, X: x, Y: y}
}

// ToolChoice is a tool as the tools menu offers it.
type ToolChoice struct {
	ID, Label string
}

// ToolsMenu lists every tool, said in words.
func ToolsMenu(tools []ToolChoice, x, y int) Menu {
	items := make([]MenuItem, 0, len(tools))
	for _, t := range tools {
		items = append(items, MenuItem{Label: t.Label, Action: MenuTool, Arg: t.ID})
	}
	return Menu{Title: "tools", Items: items, X: x, Y: y}
}

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
		// tend's own, above herdr's: where to find everything else.
		{Label: "all tools...", Action: MenuTools},
		{Label: "MCP servers", Action: MenuMCP},
		{Label: "agent permissions", Action: MenuPermissions},
		{Label: "sleep an agent...", Action: MenuSleepPick},
		{Label: "phone view...", Action: MenuPhone},
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
	// Scroll is the first item shown when there are more than the screen
	// holds — a list of every agent of every space — kept by the client as
	// the cursor or the wheel moves it (Top says where it lands).
	Scroll int

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

// Top is the first item a menu shows on a screen of rows: its Scroll, moved
// as little as keeps the cursor in view, and no further than leaves the
// last item at the bottom. A menu that fits shows from the first.
func (m Menu) Top(cols, rows int) int {
	visible := m.Rect(cols, rows).Rows - 2
	if visible <= 0 || len(m.Items) <= visible {
		return 0
	}
	top := m.Scroll
	if m.Selected >= 0 {
		if m.Selected < top {
			top = m.Selected
		}
		if m.Selected >= top+visible {
			top = m.Selected - visible + 1
		}
	}
	return min(max(top, 0), len(m.Items)-visible)
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
	if i < 0 || i >= r.Rows-2 || m.Top(cols, rows)+i >= len(m.Items) {
		return MenuItem{}, false, true
	}
	return m.Items[m.Top(cols, rows)+i], true, true
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
	top := m.Top(dst.Cols(), dst.Rows())
	// A list longer than the screen says so on its border, at the end that
	// has more: the items past it are a turn of the wheel away.
	if top > 0 {
		writeString(dst, r.X+r.Cols-3, r.Y, "▲", theme.MenuTitle, r.X+r.Cols)
	}
	if top+r.Rows-2 < len(m.Items) {
		writeString(dst, r.X+r.Cols-3, r.Y+r.Rows-1, "▼", theme.MenuTitle, r.X+r.Cols)
	}
	for i := top; i < len(m.Items); i++ {
		item := m.Items[i]
		y := r.Y + 1 + i - top
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
// Copy is offered when text is selected in the pane, and paste always: it
// needs the clipboard, not a selection. Both come first, where a hand that
// right-clicked a selection looks.
func PaneMenu(pane uint64, x, y int, closable, agent, selected bool) Menu {
	var items []MenuItem
	if selected {
		items = append(items, MenuItem{Label: "copy", Action: MenuCopy})
	}
	items = append(items, []MenuItem{
		{Label: "paste", Action: MenuPaste},
		{Label: "split right", Action: MenuSplitRight},
		{Label: "split down", Action: MenuSplitDown},
		{Label: "zoom", Action: MenuZoom},
		{Label: "rename pane", Action: MenuRenamePane},
		{Label: "rename tab", Action: MenuRename},
		{Label: "files panel", Action: MenuFiles},
	}...)
	if agent {
		items = append(items, MenuItem{Label: "continue in...", Action: MenuContinueIn})
	}
	if closable {
		items = append(items, MenuItem{Label: "close pane", Action: MenuClose})
	}
	return Menu{Title: "pane", Items: items, X: x, Y: y, Pane: pane}
}

// WithSleep adds to a pane's or an agent's menu what can be done about its
// agent's process: put it to sleep to give its memory back, or wake it.
// The item goes before "close pane", which stays last.
func WithSleep(m Menu, asleep bool) Menu {
	item := MenuItem{Label: "sleep (free its memory)", Action: MenuSleep}
	if asleep {
		item = MenuItem{Label: "wake the agent", Action: MenuWake}
	}
	at := len(m.Items)
	for i, it := range m.Items {
		if it.Action == MenuClose {
			at = i
		}
	}
	items := append([]MenuItem(nil), m.Items[:at]...)
	items = append(items, item)
	m.Items = append(items, m.Items[at:]...)
	return m
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

// CompanyPickMenu lists the companies a group can be moved to: every one
// but the one it is being moved from. from names that one in the title,
// or is empty when the group leaves every company it is in.
func CompanyPickMenu(group, from string, companies []CompanyChoice, x, y int) Menu {
	var items []MenuItem
	for _, c := range companies {
		items = append(items, MenuItem{Label: c.Name, Action: MenuPickCompany, Arg: c.ID})
	}
	title := "move to company"
	if from != "" {
		title = "move from " + from + " to"
	}
	return Menu{Title: title, Items: items, X: x, Y: y, Group: group}
}

// CompanyChoice is a company a menu offers, its id as text for MenuItem.Arg.
type CompanyChoice struct{ ID, Name string }

// GroupMenu is what a right-click on a group heading offers.
//
// It acts on the group as a whole, which is the set of spaces naming it: there
// is no group record to rename, so renaming one moves every member.
//
// folder is the one the group's new spaces start in, shown as it is written
// on the status line, or empty when none is set: the item that sets it says
// which it is now, so finding out does not mean opening the prompt.
//
// companies says there are companies to move it to.
func GroupMenu(group string, folded bool, folder string, companies bool, x, y int) Menu {
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
	if companies {
		items = append(items, MenuItem{Label: "move to company...", Action: MenuMoveToCompany})
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
