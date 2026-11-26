package main

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The permissions manager (prefix+P): what each agent may do without
// asking, a tab an agent, for the project of the pane it was opened over.
// The rules are read and written by the session's server, where the
// agents and their files are (internal/perm); this is the panel, and what
// each key asks the server to do. A file opens in an editor beside the
// pane, and the panel comes back over it, read again, when the editor is
// closed.

// permState is the client's side of the panel.
type permState struct {
	list proto.PermListResult
	// pane is the pane the panel was opened over: its project is the one
	// shown, and an editor opens beside it.
	pane uint64
	// agent is the id of the agent whose tab is up, kept across reloads.
	agent string
	// items are the lines of the list, a mode or a rule each.
	items []permItem
	// pending is what enter does while a question is up.
	pending func() error
	// typing is what the line being typed is for: "add" or "filter".
	typing string
	// adding is the rule typed, and addDecision the decision picked for
	// it, while its file is picked.
	adding, addDecision string
	filter              string
	editors             []proto.PermEditor
}

// permItem is one line: a mode or a rule.
type permItem = tools.PermItem

func (t *tui) permManagerUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.permMgr != nil
}

// openPermManager puts the panel up over the focused pane and reads the
// agents' permissions for its project.
func (t *tui) openPermManager() error {
	t.mu.Lock()
	agent := ""
	if t.perm != nil {
		agent = t.perm.agent
	}
	t.mu.Unlock()
	return t.openPermManagerAt(agent)
}

// openPermManagerAt opens it on an agent's tab.
func (t *tui) openPermManagerAt(agent string) error {
	t.mu.Lock()
	if t.permMgr == nil {
		t.permMgr = &ui.PermManagerView{Loading: true}
		t.perm = &permState{agent: agent}
	}
	t.perm.pane = t.focus
	t.permMgr.Loading = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.loadPerm("")
	return nil
}

func (t *tui) closePermManager() {
	t.mu.Lock()
	t.permMgr, t.perm = nil, nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// loadPerm reads the permissions again and lays the panel out, keeping the
// tab and the cursor; message, when set, is said once it is done.
func (t *tui) loadPerm(message string) {
	if !t.client.Supports(proto.MethodPermList) {
		t.permSay("this server is older than the permissions manager; "+handoffCommand(t.session)+" moves it to this build", true)
		return
	}
	t.mu.Lock()
	pane := uint64(0)
	if t.perm != nil {
		pane = t.perm.pane
	}
	t.mu.Unlock()
	list, err := t.client.PermList(pane, "")
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v, st := t.permMgr, t.perm
	if v == nil || st == nil {
		return
	}
	v.Loading = false
	if err != nil {
		v.Message, v.Alert = "could not read the agents' permissions: "+refusalText(err), true
		return
	}
	st.list = list
	t.permLayoutLocked()
	if message != "" {
		v.Message, v.Alert = message, false
	} else {
		v.Message, v.Alert = "", false
	}
}

// permAgentLocked is the agent whose tab is up.
func (t *tui) permAgentLocked() (proto.PermAgent, bool) {
	st := t.perm
	if st == nil {
		return proto.PermAgent{}, false
	}
	a, ok := tools.PermAgentOf(st.list, st.agent)
	if ok {
		st.agent = a.ID
	}
	return a, ok
}

// permLayoutLocked lays the panel out from the list: the tabs, the notes
// under them, and the agent's modes and rules (internal/tools, which
// tend-desktop's screen lists them with too).
func (t *tui) permLayoutLocked() {
	v, st := t.permMgr, t.perm
	if v == nil || st == nil {
		return
	}
	a, ok := t.permAgentLocked()
	v.Where = tools.PermWhere(st.list.Project)
	v.Tabs = tools.PermTabs(st.list)
	for i, agent := range st.list.Agents {
		if agent.ID == a.ID {
			v.Tab = i
		}
	}
	v.Notes = tools.PermNotes(a)
	st.items, v.Lines = nil, nil
	if ok {
		st.items, v.Lines = tools.PermList(a, st.list.Project, st.filter)
	}
	v.Filter = st.filter
	v.EmptyLabel = tools.PermEmptyLabel(a)
	v.Row = min(v.Row, max(len(v.Lines)-1, 0))
	v.Scroll = ui.PermManagerScrollFor(v, t.cols, t.rows)
}

func (t *tui) permSay(msg string, alert bool) {
	t.mu.Lock()
	if v := t.permMgr; v != nil {
		v.Message, v.Alert, v.Loading = msg, alert, false
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// permSelectedLocked is the line under the cursor.
func (t *tui) permSelectedLocked() (permItem, bool) {
	v, st := t.permMgr, t.perm
	if v == nil || st == nil || v.Row < 0 || v.Row >= len(st.items) {
		return permItem{}, false
	}
	return st.items[v.Row], true
}

// permManagerInput is every key and click while the panel is up.
func (t *tui) permManagerInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	t.mu.Lock()
	menuUp := t.menu != nil
	t.mu.Unlock()
	if menuUp {
		for _, ev := range mice {
			switch ev.Kind {
			case ui.MouseMove:
				t.hoverMenu(ev.X, ev.Y)
			case ui.MousePress:
				if _, err := t.clickMenu(ev.X, ev.Y); err != nil {
					return err
				}
			}
		}
		if len(forward) > 0 {
			if _, err := t.menuKeys(forward); err != nil {
				return err
			}
		}
		t.wakeUp()
		return nil
	}
	for _, ev := range mice {
		t.permMouse(ev)
	}
	for _, key := range splitKeys(forward) {
		t.mu.Lock()
		v := t.permMgr
		if v == nil {
			t.mu.Unlock()
			return nil
		}
		typing, asking, detail := v.Typing, v.Confirm != "", v.Detail != nil
		t.mu.Unlock()
		switch {
		case typing:
			t.permTypeKey(key)
		case asking:
			t.permAnswer(key == "\r" || key == "\n")
		case key == "\x1b" || key == "q":
			if detail {
				t.mu.Lock()
				v.Detail = nil
				t.dirty = true
				t.mu.Unlock()
				continue
			}
			t.closePermManager()
			return nil
		case key == "e":
			t.permEdit("")
		case key == "E":
			t.permEditorsMenu(-1, -1)
		case key == "o":
			t.permFilesMenu(-1, -1)
		case detail:
		case key == "\x1b[A" || key == "k":
			t.movePerm(-1)
		case key == "\x1b[B" || key == "j":
			t.movePerm(1)
		case key == "\x1b[5~":
			t.movePerm(-10)
		case key == "\x1b[6~":
			t.movePerm(10)
		case key == "\x1b[D" || key == "h":
			t.permTab(-1)
		case key == "\x1b[C" || key == "l" || key == "\t":
			t.permTab(1)
		case key == " ":
			t.permChange()
		case key == "\r" || key == "\n":
			t.permEnter()
		case key == "a":
			t.permStartAdd()
		case key == "d":
			t.permRemove()
		case key == "m":
			t.permMoveMenu(-1, -1)
		case key == "/":
			t.permStartFilter()
		case key == "r":
			go t.loadPerm("")
		case key == "?":
			t.permHelp()
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) permMouse(ev ui.MouseEvent) {
	switch ev.Kind {
	case ui.MouseWheelUp:
		t.movePerm(-1)
		return
	case ui.MouseWheelDown:
		t.movePerm(1)
		return
	case ui.MousePress:
	default:
		return
	}
	if ev.Button != 0 && ev.Button != mouseRight {
		return
	}
	t.mu.Lock()
	v := t.permMgr
	if v == nil {
		t.mu.Unlock()
		return
	}
	g := ui.PermManagerLayout(v, t.cols, t.rows)
	row, onRow := ui.PermManagerRowAt(v, t.cols, t.rows, ev.X, ev.Y)
	tab, onTab := ui.PermManagerTabAt(v, t.cols, t.rows, ev.X, ev.Y)
	busy := v.Typing || v.Confirm != ""
	t.mu.Unlock()
	switch {
	case inRect(g.Close, ev.X, ev.Y), ui.OnCloseMark(g.Box, ev.X, ev.Y):
		t.closePermManager()
	case busy:
	case inRect(g.Refresh, ev.X, ev.Y):
		go t.loadPerm("")
	case inRect(g.Help, ev.X, ev.Y):
		t.permHelp()
	case inRect(g.Add, ev.X, ev.Y):
		t.permStartAdd()
	case inRect(g.Edit, ev.X, ev.Y):
		t.permEdit("")
	case inRect(g.Editor, ev.X, ev.Y):
		t.permEditorsMenu(ev.X, ev.Y-1)
	case onTab:
		t.permTabTo(tab)
	case onRow && ev.Button == mouseRight:
		t.mu.Lock()
		v.Row, v.Detail = row, nil
		t.dirty = true
		t.mu.Unlock()
		t.permRowMenu(ev.X, ev.Y)
	case onRow:
		t.mu.Lock()
		again := v.Row == row
		v.Row, v.Detail = row, nil
		t.dirty = true
		t.mu.Unlock()
		// A click on the chosen line is enter on it.
		if again {
			t.permEnter()
		}
	}
	t.wakeUp()
}

func (t *tui) movePerm(d int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.permMgr
	if v == nil || len(v.Lines) == 0 || v.Detail != nil {
		return
	}
	v.Row = min(max(v.Row+d, 0), len(v.Lines)-1)
	v.Scroll = ui.PermManagerScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

// permTab moves to the agent beside this one.
func (t *tui) permTab(d int) {
	t.mu.Lock()
	n := 0
	cur := 0
	if v := t.permMgr; v != nil {
		n, cur = len(v.Tabs), v.Tab
	}
	t.mu.Unlock()
	if n == 0 {
		return
	}
	t.permTabTo((cur + d + n) % n)
}

func (t *tui) permTabTo(i int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, st := t.permMgr, t.perm
	if v == nil || st == nil || i < 0 || i >= len(st.list.Agents) {
		return
	}
	st.agent = st.list.Agents[i].ID
	v.Row, v.Scroll, v.Detail, v.Message, v.Alert = 0, 0, nil, "", false
	t.permLayoutLocked()
	t.dirty = true
}

// permAsk puts a question up; enter does what it asks.
func (t *tui) permAsk(question string, do func() error) {
	t.mu.Lock()
	if v, st := t.permMgr, t.perm; v != nil && st != nil {
		v.Confirm, st.pending = question, do
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) permAnswer(yes bool) {
	t.mu.Lock()
	v, st := t.permMgr, t.perm
	if v == nil || st == nil {
		t.mu.Unlock()
		return
	}
	do := st.pending
	v.Confirm, st.pending = "", nil
	t.dirty = true
	t.mu.Unlock()
	if yes && do != nil {
		go func() {
			if err := do(); err != nil {
				t.permSay(refusalText(err), true)
			}
		}()
	}
}

// permWrite sends one write and reads the list again, saying what was
// done and when the agent sees it.
func (t *tui) permWrite(method string, p proto.PermWriteParams, done string) {
	t.mu.Lock()
	if t.perm != nil {
		p.Pane = t.perm.pane
	}
	a, _ := t.permAgentLocked()
	t.mu.Unlock()
	p.Agent = a.ID
	go func() {
		if err := t.client.PermWrite(method, p); err != nil {
			t.permSay(refusalText(err), true)
			return
		}
		msg := done
		if a.Effect != "" {
			msg += " — " + a.Effect
		}
		t.loadPerm(msg)
	}()
}

// permChange is space: a rule says the next of the agent's decisions; a
// mode offers its values.
func (t *tui) permChange() {
	t.mu.Lock()
	item, ok := t.permSelectedLocked()
	a, _ := t.permAgentLocked()
	t.mu.Unlock()
	switch {
	case !ok:
	case item.Mode != nil:
		t.permModeMenu(-1, -1)
	case item.Rule.ReadOnly:
		t.permSay("tend does not edit this rule: e opens its file in your editor", false)
	default:
		r := item.Rule
		next := tools.PermNextDecision(a, r.Decision)
		if next == "" {
			t.permSay(a.Name+" has no other decision for a rule", false)
			return
		}
		t.permWrite(proto.MethodPermSetDecision, proto.PermWriteParams{File: r.File, Decision: r.Decision, Text: r.Text, ToDecision: next},
			r.Text+" is "+next+" now")
	}
}

// permEnter is enter: a rule's details, a mode's values.
func (t *tui) permEnter() {
	t.mu.Lock()
	item, ok := t.permSelectedLocked()
	a, _ := t.permAgentLocked()
	v := t.permMgr
	if !ok || v == nil {
		t.mu.Unlock()
		return
	}
	if item.Mode != nil {
		t.mu.Unlock()
		t.permModeMenu(-1, -1)
		return
	}
	v.Detail = tools.PermDetail(a, *item.Rule)
	t.dirty = true
	t.mu.Unlock()
}

// permStartAdd puts up the line a new rule is typed on.
func (t *tui) permStartAdd() {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, st := t.permMgr, t.perm
	a, ok := t.permAgentLocked()
	if v == nil || st == nil || !ok {
		return
	}
	if len(a.Decisions) == 0 || len(permRuleFiles(a)) == 0 {
		v.Message, v.Alert = a.Name+" keeps no rules tend can add to: e opens its file", false
		t.dirty = true
		return
	}
	st.typing = "add"
	v.Typing, v.TypeLabel, v.TypeText = true, "new rule for "+a.Name+": ", ""
	v.TypeHint = permRuleHint[a.ID] + " · enter · esc"
	v.Detail = nil
	t.dirty = true
}

func (t *tui) permStartFilter() {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, st := t.permMgr, t.perm
	if v == nil || st == nil {
		return
	}
	st.typing = "filter"
	v.Typing, v.TypeLabel, v.TypeText = true, "show rules with: ", st.filter
	v.TypeHint = "part of a rule, e.g. ssh or docker · enter · esc shows all"
	t.dirty = true
}

// permTypeKey is a key on the line being typed.
func (t *tui) permTypeKey(key string) {
	t.mu.Lock()
	v, st := t.permMgr, t.perm
	if v == nil || st == nil {
		t.mu.Unlock()
		return
	}
	t.dirty = true
	switch {
	case key == "\x1b":
		v.Typing = false
		if st.typing == "filter" {
			st.filter = ""
			t.permLayoutLocked()
		}
		t.mu.Unlock()
	case key == "\x7f" || key == "\x08":
		if v.TypeText != "" {
			_, size := utf8.DecodeLastRuneInString(v.TypeText)
			v.TypeText = v.TypeText[:len(v.TypeText)-size]
		}
		if st.typing == "filter" {
			st.filter = v.TypeText
			v.Row = 0
			t.permLayoutLocked()
		}
		t.mu.Unlock()
	case key == "\r" || key == "\n":
		v.Typing = false
		text := strings.TrimSpace(v.TypeText)
		purpose := st.typing
		if purpose == "add" {
			st.adding = text
		}
		t.mu.Unlock()
		if purpose == "add" && text != "" {
			t.permDecisionMenu()
		}
	case len(key) >= 1 && (key[0] >= 0x20 && key[0] != 0x7f || key[0] >= 0x80) && utf8.ValidString(key):
		v.TypeText += key
		if st.typing == "filter" {
			// The list narrows as it is typed.
			st.filter = v.TypeText
			v.Row = 0
			t.permLayoutLocked()
		}
		t.mu.Unlock()
	default:
		t.mu.Unlock()
	}
}

// permMenuXY is where a menu goes when no click put it somewhere.
func (t *tui) permMenuXYLocked() (int, int) {
	v := t.permMgr
	if v == nil {
		return t.cols / 3, t.rows / 3
	}
	g := ui.PermManagerLayout(v, t.cols, t.rows)
	return g.List.X + 4, g.List.Y + (v.Row - v.Scroll) + 1
}

// permDecisionMenu asks what the rule being added says.
func (t *tui) permDecisionMenu() {
	t.mu.Lock()
	a, ok := t.permAgentLocked()
	x, y := t.permMenuXYLocked()
	adding := ""
	if t.perm != nil {
		adding = t.perm.adding
	}
	t.mu.Unlock()
	if !ok {
		return
	}
	var items []ui.MenuItem
	for _, d := range a.Decisions {
		items = append(items, ui.MenuItem{Label: permDecisionWords[d], Action: ui.MenuPermDo, Arg: "decision:" + d})
	}
	t.openMenu(ui.Menu{Title: adding, Items: items, X: x, Y: y})
}

// permAddFileMenu asks which of the agent's files the new rule goes in.
func (t *tui) permAddFileMenu() {
	t.mu.Lock()
	a, ok := t.permAgentLocked()
	x, y := t.permMenuXYLocked()
	t.mu.Unlock()
	if !ok {
		return
	}
	var items []ui.MenuItem
	for i, f := range permRuleFiles(a) {
		items = append(items, ui.MenuItem{Label: permFileLabel(f), Action: ui.MenuPermDo, Arg: "addfile:" + strconv.Itoa(i)})
	}
	t.openMenu(ui.Menu{Title: "into which file?", Items: items, X: x, Y: y})
}

// permRemove asks, then takes the rule out of its file.
func (t *tui) permRemove() {
	t.mu.Lock()
	item, ok := t.permSelectedLocked()
	t.mu.Unlock()
	if !ok || item.Rule == nil {
		return
	}
	r := *item.Rule
	if r.ReadOnly {
		t.permSay("tend does not edit this rule: e opens its file in your editor", false)
		return
	}
	t.permAsk("remove "+r.Decision+" "+r.Text+" from "+tildeHome(r.File)+"?", func() error {
		t.permWrite(proto.MethodPermRemove, proto.PermWriteParams{File: r.File, Decision: r.Decision, Text: r.Text}, "removed "+r.Text)
		return nil
	})
}

// permMoveMenu offers the agent's other files for the rule.
func (t *tui) permMoveMenu(x, y int) {
	t.mu.Lock()
	item, ok := t.permSelectedLocked()
	a, _ := t.permAgentLocked()
	if x < 0 {
		x, y = t.permMenuXYLocked()
	}
	t.mu.Unlock()
	if !ok || item.Rule == nil {
		return
	}
	if item.Rule.ReadOnly {
		t.permSay("tend does not edit this rule: e opens its file in your editor", false)
		return
	}
	var items []ui.MenuItem
	for i, f := range permRuleFiles(a) {
		if f.Path == item.Rule.File {
			continue
		}
		items = append(items, ui.MenuItem{Label: permFileLabel(f), Action: ui.MenuPermDo, Arg: "move:" + strconv.Itoa(i)})
	}
	if len(items) == 0 {
		t.permSay(a.Name+" has no other file to move it to", false)
		return
	}
	t.openMenu(ui.Menu{Title: "move " + item.Rule.Text + " to", Items: items, X: x, Y: y})
}

// permModeMenu offers a mode's values.
func (t *tui) permModeMenu(x, y int) {
	t.mu.Lock()
	item, ok := t.permSelectedLocked()
	if x < 0 {
		x, y = t.permMenuXYLocked()
	}
	t.mu.Unlock()
	if !ok || item.Mode == nil {
		return
	}
	m := item.Mode
	var items []ui.MenuItem
	for _, c := range m.Choices {
		label := c
		if c == m.Value {
			label += "  ✓"
		}
		items = append(items, ui.MenuItem{Label: label, Action: ui.MenuPermDo, Arg: "mode:" + c})
	}
	items = append(items, ui.MenuItem{Label: "not set: the agent's default", Action: ui.MenuPermDo, Arg: "mode:"})
	title := permModeKey(m.Key) + " in " + tildeHome(m.File)
	if m.Help != "" {
		title += " — " + m.Help
	}
	t.openMenu(ui.Menu{Title: title, Items: items, X: x, Y: y})
}

// permFilesMenu offers every file of the agent, to open in the editor.
func (t *tui) permFilesMenu(x, y int) {
	t.mu.Lock()
	a, ok := t.permAgentLocked()
	if x < 0 {
		x, y = t.permMenuXYLocked()
	}
	t.mu.Unlock()
	if !ok {
		return
	}
	var items []ui.MenuItem
	for i, f := range a.Files {
		items = append(items, ui.MenuItem{Label: permFileLabel(f), Action: ui.MenuPermDo, Arg: "open:" + strconv.Itoa(i)})
	}
	t.openMenu(ui.Menu{Title: "open which of " + a.Name + "'s files?", Items: items, X: x, Y: y})
}

// permEditorsMenu offers the editors the server's machine has, to open the
// selected line's file in.
func (t *tui) permEditorsMenu(x, y int) {
	go func() {
		res, err := t.client.PermEditors()
		if err != nil {
			t.permSay(refusalText(err), true)
			return
		}
		t.mu.Lock()
		if t.perm != nil {
			t.perm.editors = res.Editors
		}
		if x < 0 {
			x, y = t.permMenuXYLocked()
		}
		t.mu.Unlock()
		var items []ui.MenuItem
		for i, e := range res.Editors {
			items = append(items, ui.MenuItem{Label: tools.PermEditorLabel(e), Action: ui.MenuPermDo, Arg: "editor:" + strconv.Itoa(i)})
		}
		t.openMenu(ui.Menu{Title: "open the file in", Items: items, X: x, Y: y})
		t.wakeUp()
	}()
}

// permSelectedFileLocked is the file the selected line is in, or the
// agent's first file that is there (its first at all when none is).
func (t *tui) permSelectedFileLocked() string {
	if item, ok := t.permSelectedLocked(); ok {
		if item.Rule != nil {
			return item.Rule.File
		}
		return item.Mode.File
	}
	a, ok := t.permAgentLocked()
	if !ok || len(a.Files) == 0 {
		return ""
	}
	for _, f := range a.Files {
		if f.Exists {
			return f.Path
		}
	}
	return a.Files[0].Path
}

// permEdit opens the selected line's file in an editor ("" the default):
// a terminal editor beside the pane, the panel back over it once it is
// closed; a graphical one in a window, the panel staying up.
func (t *tui) permEdit(editor string) {
	t.mu.Lock()
	path := t.permSelectedFileLocked()
	t.mu.Unlock()
	t.permOpen(path, editor)
}

func (t *tui) permOpen(path, editor string) {
	if path == "" {
		return
	}
	t.mu.Lock()
	st := t.perm
	if st == nil {
		t.mu.Unlock()
		return
	}
	pane, agent := st.pane, st.agent
	t.mu.Unlock()
	go func() {
		res, err := t.client.PermOpen(proto.PermOpenParams{Pane: pane, Path: path, Editor: editor})
		if err != nil {
			t.permSay(refusalText(err), true)
			return
		}
		if res.Window {
			t.permSay("opened "+tildeHome(path)+" in a window — r reads it again once you save", false)
			return
		}
		// The editor has the screen, as the history's does (prefix+e); the
		// panel comes back, read again, when it is closed.
		t.mu.Lock()
		t.permMgr, t.perm = nil, nil
		t.transient = &transientPane{pane: res.Pane, previous: pane, previousZoom: t.zoom,
			then: func() { _ = t.openPermManagerAt(agent) }}
		t.focus, t.zoom = res.Pane, true
		t.dirty = true
		t.mu.Unlock()
		_ = t.refresh()
		t.wakeUp()
	}()
}

// permHelp shows what the panel is and how to use it.
func (t *tui) permHelp() {
	t.mu.Lock()
	if v := t.permMgr; v != nil {
		v.Detail = ui.PermHelp
		v.Typing, v.Confirm = false, ""
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// permRowMenu is everything that can be done to the line, in words.
func (t *tui) permRowMenu(x, y int) {
	t.mu.Lock()
	item, ok := t.permSelectedLocked()
	t.mu.Unlock()
	if !ok {
		return
	}
	var items []ui.MenuItem
	title := ""
	if item.Mode != nil {
		title = permModeKey(item.Mode.Key)
		items = append(items, ui.MenuItem{Label: "change it...", Action: ui.MenuPermDo, Arg: "do:change"})
	} else {
		title = item.Rule.Text
		if !item.Rule.ReadOnly {
			items = append(items,
				ui.MenuItem{Label: "change allow / ask / deny", Action: ui.MenuPermDo, Arg: "do:change"},
				ui.MenuItem{Label: "move to another file...", Action: ui.MenuPermDo, Arg: "do:move"},
				ui.MenuItem{Label: "remove it", Action: ui.MenuPermDo, Arg: "do:remove"})
		}
		items = append(items, ui.MenuItem{Label: "details", Action: ui.MenuPermDo, Arg: "do:details"})
	}
	items = append(items,
		ui.MenuItem{Label: "open its file in the editor", Action: ui.MenuPermDo, Arg: "do:edit"},
		ui.MenuItem{Label: "open its file in...", Action: ui.MenuPermDo, Arg: "do:editors"},
		ui.MenuItem{Label: "add a rule...", Action: ui.MenuPermDo, Arg: "do:add"})
	t.openMenu(ui.Menu{Title: title, Items: items, X: x, Y: y})
}

// permMenuAction does what an item of the panel's menus says.
func (t *tui) permMenuAction(arg string) error {
	kind, value, _ := strings.Cut(arg, ":")
	t.mu.Lock()
	st := t.perm
	a, ok := t.permAgentLocked()
	t.mu.Unlock()
	if st == nil || !ok {
		return nil
	}
	index := func() (int, bool) {
		i, err := strconv.Atoi(value)
		return i, err == nil
	}
	switch kind {
	case "decision":
		t.mu.Lock()
		st.addDecision = value
		t.mu.Unlock()
		t.permAddFileMenu()
	case "addfile":
		files := permRuleFiles(a)
		if i, ok := index(); ok && i < len(files) {
			t.mu.Lock()
			text, d := st.adding, st.addDecision
			t.mu.Unlock()
			t.permWrite(proto.MethodPermAdd, proto.PermWriteParams{File: files[i].Path, Decision: d, Text: text},
				"added "+d+" "+text+" to "+tildeHome(files[i].Path))
		}
	case "move":
		files := permRuleFiles(a)
		t.mu.Lock()
		item, ok := t.permSelectedLocked()
		t.mu.Unlock()
		if i, okI := index(); okI && ok && item.Rule != nil && i < len(files) {
			r := *item.Rule
			t.permWrite(proto.MethodPermMove, proto.PermWriteParams{File: r.File, To: files[i].Path, Decision: r.Decision, Text: r.Text},
				"moved "+r.Text+" to "+tildeHome(files[i].Path))
		}
	case "mode":
		t.mu.Lock()
		item, ok := t.permSelectedLocked()
		t.mu.Unlock()
		if ok && item.Mode != nil {
			m := *item.Mode
			done := permModeKey(m.Key) + " is " + value
			if value == "" {
				done = permModeKey(m.Key) + " is not set"
			}
			t.permWrite(proto.MethodPermSetMode, proto.PermWriteParams{File: m.File, Key: m.Key, Value: value}, done)
		}
	case "open":
		if i, ok := index(); ok && i < len(a.Files) {
			t.permOpen(a.Files[i].Path, "")
		}
	case "editor":
		t.mu.Lock()
		editors := st.editors
		path := t.permSelectedFileLocked()
		t.mu.Unlock()
		if i, ok := index(); ok && i < len(editors) {
			t.permOpen(path, editors[i].Command)
		}
	case "do":
		switch value {
		case "change":
			t.permChange()
		case "move":
			t.permMoveMenu(-1, -1)
		case "remove":
			t.permRemove()
		case "details":
			t.permEnter()
		case "edit":
			t.permEdit("")
		case "editors":
			t.permEditorsMenu(-1, -1)
		case "add":
			t.permStartAdd()
		}
	}
	t.wakeUp()
	return nil
}

// What the list says, and how a rule and a file are labelled, are
// internal/tools', which tend-desktop's permissions screen uses too.
var (
	permModeKey       = tools.PermModeKey
	permRuleFiles     = tools.PermRuleFiles
	permFileLabel     = tools.PermFileLabel
	permRuleHint      = tools.PermRuleHint
	permDecisionWords = tools.PermDecisionWords
)
