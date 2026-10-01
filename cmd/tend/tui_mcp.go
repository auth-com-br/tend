package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The MCP manager (prefix+M): the agents' MCP servers in a grid, one row a
// server and one column an agent, to switch on and off, test, add and
// remove. The servers are read, written and tested by the session's server,
// where the agents and their files are (internal/mcp); this is the panel,
// and what each key asks the server to do.

// mcpState is the client's side of the panel: the list it was drawn from,
// and what has been tested.
type mcpState struct {
	list proto.MCPListResult
	// cols are the agents' ids, in the order of the view's columns, and
	// keys the rows' keys (the server's name lowercased).
	cols []string
	keys []string
	// entries is each row's servers, by agent.
	entries map[string]map[string]proto.MCPEntry
	// tests is each row's last result, kept across reloads.
	tests map[string][2]string
	// pending is what enter does while a question is up.
	pending func() error
}

func (t *tui) mcpManagerUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mcpMgr != nil
}

// openMCPManager puts the panel up and reads the servers.
func (t *tui) openMCPManager() error {
	t.mu.Lock()
	if t.mcpMgr == nil {
		t.mcpMgr = &ui.MCPManagerView{Loading: true}
		t.mcp = &mcpState{tests: map[string][2]string{}}
	}
	t.mcpMgr.Loading = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.loadMCP("")
	return nil
}

func (t *tui) closeMCPManager() {
	t.mu.Lock()
	t.mcpMgr, t.mcp = nil, nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// loadMCP reads the servers again and lays the grid out, keeping the cursor
// where it was; message, when set, is said once it is done.
func (t *tui) loadMCP(message string) {
	if !t.client.Supports(proto.MethodMCPList) {
		t.mcpSay("this server is older than the MCP manager; "+handoffCommand(t.session)+" moves it to this build", true)
		return
	}
	list, err := t.client.MCPList()
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v, st := t.mcpMgr, t.mcp
	if v == nil || st == nil {
		return
	}
	v.Loading = false
	if err != nil {
		v.Message, v.Alert = "could not read the agents' servers: "+refusalText(err), true
		return
	}
	st.list = list
	v.Agents, st.cols, v.Rows, st.keys, st.entries = mcpGrid(list)
	for i, k := range st.keys {
		if r, ok := st.tests[k]; ok {
			v.Rows[i].Test, v.Rows[i].TestState = r[0], r[1]
		}
	}
	v.Row = min(v.Row, max(len(v.Rows)-1, 0))
	v.Col = min(v.Col, len(v.Agents))
	v.Scroll = ui.MCPManagerScrollFor(v, t.cols, t.rows)
	switch {
	case message != "":
		v.Message, v.Alert = message, false
	default:
		v.Message, v.Alert = mcpSummary(list), false
	}
}

// mcpSummary is the line under a freshly read grid: how many servers, and
// any agent whose file could not be read.
func mcpSummary(list proto.MCPListResult) string {
	keys := map[string]bool{}
	for _, e := range list.Entries {
		keys[strings.ToLower(e.Name)] = true
	}
	s := itoaInt(len(keys)) + " servers in " + itoaInt(len(mcpColumns(list))) + " agents (global settings; a project's own are not shown)"
	for _, a := range list.Agents {
		if a.Error != "" {
			s = a.Name + ": " + a.Error
		}
	}
	return s
}

// mcpColumns is the agents shown: those installed here or with servers.
func mcpColumns(list proto.MCPListResult) []proto.MCPAgent {
	has := map[string]bool{}
	for _, e := range list.Entries {
		has[e.Agent] = true
	}
	var out []proto.MCPAgent
	for _, a := range list.Agents {
		if a.Present || has[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// mcpGrid lays the servers out: a row per name (as Key matches them), a
// column per agent, each cell on, off or absent, and ≠ on a server whose
// definition is not the one most agents of the row have.
func mcpGrid(list proto.MCPListResult) (labels, cols []string, rows []ui.MCPRow, keys []string, entries map[string]map[string]proto.MCPEntry) {
	for _, a := range mcpColumns(list) {
		labels = append(labels, a.ID)
		cols = append(cols, a.ID)
	}
	entries = map[string]map[string]proto.MCPEntry{}
	names := map[string]string{}
	for _, e := range list.Entries {
		k := strings.ToLower(strings.TrimSpace(e.Name))
		if entries[k] == nil {
			entries[k] = map[string]proto.MCPEntry{}
			names[k] = e.Name
			keys = append(keys, k)
		}
		entries[k][e.Agent] = e
	}
	sort.Strings(keys)
	for _, k := range keys {
		row := ui.MCPRow{Name: names[k], Cells: make([]ui.MCPCell, len(cols)), Differs: make([]bool, len(cols))}
		count := map[string]int{}
		transports := map[string]int{}
		for _, e := range entries[k] {
			count[e.Fingerprint]++
			transports[e.Transport]++
		}
		common, most := "", 0
		for fp, n := range count {
			if n > most || n == most && fp < common {
				common, most = fp, n
			}
		}
		for tr, n := range transports {
			if n > transports[row.Transport] || row.Transport == "" {
				row.Transport = tr
			}
		}
		for i, id := range cols {
			e, ok := entries[k][id]
			if !ok {
				continue
			}
			row.Cells[i] = ui.MCPOff
			if e.Enabled {
				row.Cells[i] = ui.MCPOn
			}
			row.Differs[i] = len(count) > 1 && e.Fingerprint != common
		}
		rows = append(rows, row)
	}
	return labels, cols, rows, keys, entries
}

// mcpSay puts a line under the grid.
func (t *tui) mcpSay(msg string, alert bool) {
	t.mu.Lock()
	if v := t.mcpMgr; v != nil {
		v.Message, v.Alert, v.Loading = msg, alert, false
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// mcpSelected is the row's key and the agent of the cursor's column ("" on
// the name), and whether there is a row.
func (t *tui) mcpSelectedLocked() (key, agent string, ok bool) {
	v, st := t.mcpMgr, t.mcp
	if v == nil || st == nil || v.Row >= len(st.keys) || len(st.keys) == 0 {
		return "", "", false
	}
	key = st.keys[v.Row]
	if v.Col > 0 && v.Col <= len(st.cols) {
		agent = st.cols[v.Col-1]
	}
	return key, agent, true
}

// mcpManagerInput is every key and click while the panel is up.
func (t *tui) mcpManagerInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	// The row's menu is over the panel and has the input while it is up,
	// as any menu has.
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
		if err := t.mcpMouse(ev); err != nil {
			return err
		}
	}
	for _, key := range splitKeys(forward) {
		t.mu.Lock()
		v := t.mcpMgr
		if v == nil {
			t.mu.Unlock()
			return nil
		}
		adding, asking, detail := v.Adding, v.Confirm != "", v.Detail != nil
		t.mu.Unlock()
		switch {
		case adding:
			t.mcpAddKey(key)
		case asking:
			t.mcpAnswer(key == "\r" || key == "\n")
		case key == "\x1b" || key == "q":
			if detail {
				t.mu.Lock()
				v.Detail = nil
				t.dirty = true
				t.mu.Unlock()
				continue
			}
			t.closeMCPManager()
			return nil
		case detail:
		case key == "\x1b[A" || key == "k":
			t.moveMCP(-1, 0)
		case key == "\x1b[B" || key == "j":
			t.moveMCP(1, 0)
		case key == "\x1b[D" || key == "h":
			t.moveMCP(0, -1)
		case key == "\x1b[C" || key == "l":
			t.moveMCP(0, 1)
		case key == " ":
			t.mcpToggle()
		case key == "t":
			t.mcpTestRow(-1)
		case key == "T":
			t.mcpTestAll()
		case key == "a":
			t.mu.Lock()
			v.Adding, v.AddText = true, ""
			t.dirty = true
			t.mu.Unlock()
		case key == "d":
			t.mcpRemove()
		case key == "r":
			go t.loadMCP("")
		case key == "\r" || key == "\n":
			t.mcpDetail()
		case key == "m":
			t.mcpRowMenu(-1, -1)
		case key == "?":
			t.mcpHelp()
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) mcpMouse(ev ui.MouseEvent) error {
	switch ev.Kind {
	case ui.MouseWheelUp:
		t.moveMCP(-1, 0)
		return nil
	case ui.MouseWheelDown:
		t.moveMCP(1, 0)
		return nil
	case ui.MousePress:
	default:
		return nil
	}
	if ev.Button != 0 && ev.Button != mouseRight {
		return nil
	}
	t.mu.Lock()
	v := t.mcpMgr
	if v == nil {
		t.mu.Unlock()
		return nil
	}
	g := ui.MCPManagerLayout(v, t.cols, t.rows)
	row, col, onCell := ui.MCPManagerCellAt(v, t.cols, t.rows, ev.X, ev.Y)
	busy := v.Adding || v.Confirm != ""
	t.mu.Unlock()
	switch {
	case inRect(g.Close, ev.X, ev.Y), ui.OnCloseMark(g.Box, ev.X, ev.Y):
		t.closeMCPManager()
	case busy:
	case inRect(g.Refresh, ev.X, ev.Y):
		go t.loadMCP("")
	case inRect(g.TestAll, ev.X, ev.Y):
		t.mcpTestAll()
	case inRect(g.Help, ev.X, ev.Y):
		t.mcpHelp()
	case ev.Button == mouseRight && onCell:
		t.mu.Lock()
		v.Row, v.Col, v.Detail = row, col, nil
		t.mu.Unlock()
		t.mcpRowMenu(ev.X, ev.Y)
	case inRect(g.Add, ev.X, ev.Y):
		t.mu.Lock()
		v.Adding, v.AddText = true, ""
		t.dirty = true
		t.mu.Unlock()
	case onCell && ev.Button == 0:
		t.mu.Lock()
		again := v.Row == row && v.Col == col
		v.Row, v.Col, v.Detail = row, col, nil
		t.dirty = true
		t.mu.Unlock()
		// A click on the cell already chosen is space on it, as a click on
		// the agent manager's chosen row is enter.
		if again && col > 0 {
			t.mcpToggle()
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) moveMCP(dRow, dCol int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.mcpMgr
	if v == nil || len(v.Rows) == 0 {
		return
	}
	v.Row = (v.Row + dRow + len(v.Rows)) % len(v.Rows)
	v.Col = min(max(v.Col+dCol, 0), len(v.Agents))
	v.Scroll = ui.MCPManagerScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

// ask puts a question up; enter does what it asks.
func (t *tui) mcpAsk(question string, do func() error) {
	t.mu.Lock()
	if v, st := t.mcpMgr, t.mcp; v != nil && st != nil {
		v.Confirm, st.pending = question, do
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) mcpAnswer(yes bool) {
	t.mu.Lock()
	v, st := t.mcpMgr, t.mcp
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
				t.mcpSay(refusalText(err), true)
			}
		}()
	}
}

// mcpToggle is space on a cell: an agent that has the server turns it off
// or on; one that has not is offered a copy from one that has.
func (t *tui) mcpToggle() {
	t.mu.Lock()
	key, agent, ok := t.mcpSelectedLocked()
	if !ok {
		t.mu.Unlock()
		return
	}
	if agent == "" {
		t.mu.Unlock()
		t.mcpSay("move to an agent's column (← →) to switch the server there", false)
		return
	}
	st := t.mcp
	e, has := st.entries[key][agent]
	var from proto.MCPEntry
	for _, id := range st.cols {
		if c, ok := st.entries[key][id]; ok && c.Agent != agent {
			from = c
			break
		}
	}
	t.mu.Unlock()
	switch {
	case has:
		on := !e.Enabled
		if e.Reason != "" && on {
			t.mcpSay(e.Reason+"; tend does not change that", true)
			return
		}
		go func() {
			err := t.client.MCPSetEnabled(agent, e.Name, on)
			if err != nil {
				t.mcpSay(refusalText(err), true)
				return
			}
			state := "off"
			if on {
				state = "on"
			}
			t.loadMCP(e.Name + " is " + state + " in " + agent + " — it takes effect in " + agent + "'s next session")
		}()
	case from.Name != "":
		t.mcpAsk("copy "+from.Name+" from "+from.Agent+" into "+agent+"?", func() error {
			res, err := t.client.MCPAdd(proto.MCPAddParams{Name: from.Name, Agents: []string{agent}, From: &proto.MCPRef{Agent: from.Agent, Name: from.Name}})
			if err != nil {
				return err
			}
			t.loadMCP(mcpWriteText("copied "+from.Name+" into", res))
			return nil
		})
	}
}

// mcpRemove is d: the server out of the cursor's agent, or out of every
// agent on the name's column — asked first, naming them.
func (t *tui) mcpRemove() {
	t.mu.Lock()
	key, agent, ok := t.mcpSelectedLocked()
	if !ok {
		t.mu.Unlock()
		return
	}
	st := t.mcp
	var agents []string
	name := ""
	for _, id := range st.cols {
		if e, has := st.entries[key][id]; has && (agent == "" || id == agent) {
			agents, name = append(agents, id), e.Name
		}
	}
	t.mu.Unlock()
	if len(agents) == 0 {
		t.mcpSay(agent+" does not have this server", false)
		return
	}
	t.mcpAsk("remove "+name+" from "+strings.Join(agents, ", ")+"? (a backup of each file is kept)", func() error {
		res, err := t.client.MCPRemove(name, agents)
		if err != nil {
			return err
		}
		t.loadMCP(mcpWriteText("removed "+name+" from", res))
		return nil
	})
}

// mcpWriteText says what a write did.
func mcpWriteText(did string, res proto.MCPWriteResult) string {
	var parts []string
	if len(res.Done) > 0 {
		parts = append(parts, did+" "+strings.Join(res.Done, ", ")+" — it takes effect in their next sessions")
	}
	for agent, why := range res.Failed {
		parts = append(parts, agent+": "+why)
	}
	parts = append(parts, res.Warnings...)
	return strings.Join(parts, "; ")
}

// mcpTestRow tests the cursor's row: the cursor's agent's server, or each
// distinct server of the row on the name's column. row -1 is the cursor's.
func (t *tui) mcpTestRow(row int) {
	t.mu.Lock()
	v, st := t.mcpMgr, t.mcp
	if v == nil || st == nil || len(st.keys) == 0 {
		t.mu.Unlock()
		return
	}
	agent := ""
	if row < 0 {
		row = v.Row
		if v.Col > 0 && v.Col <= len(st.cols) {
			agent = st.cols[v.Col-1]
		}
	}
	key := st.keys[row]
	var targets []proto.MCPEntry
	seen := map[string]bool{}
	for _, id := range st.cols {
		e, ok := st.entries[key][id]
		if !ok || (agent != "" && id != agent) || seen[e.Fingerprint] {
			continue
		}
		seen[e.Fingerprint] = true
		targets = append(targets, e)
	}
	if len(targets) == 0 {
		t.mu.Unlock()
		return
	}
	v.Rows[row].Test, v.Rows[row].TestState = "testing…", "running"
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		var texts []string
		state := "ok"
		for _, e := range targets {
			r, err := t.client.MCPTest(e.Agent, e.Name)
			text := mcpResultText(r, err)
			if len(targets) > 1 {
				text = e.Agent + ": " + text
			}
			texts = append(texts, text)
			switch {
			case err != nil || r.Status == "error" || r.Status == "timeout":
				state = "bad"
			case r.Status == "auth" && state == "ok":
				state = "auth"
			}
		}
		t.mu.Lock()
		if st := t.mcp; st != nil {
			st.tests[key] = [2]string{strings.Join(texts, " · "), state}
			if v := t.mcpMgr; v != nil {
				for i, k := range st.keys {
					if k == key {
						v.Rows[i].Test, v.Rows[i].TestState = st.tests[key][0], state
					}
				}
			}
			t.dirty = true
		}
		t.mu.Unlock()
		t.wakeUp()
	}()
}

// mcpTestAll tests every row, four at a time on the server's side as the
// requests arrive; each row's result shows as it comes.
func (t *tui) mcpTestAll() {
	t.mu.Lock()
	n := 0
	if t.mcp != nil {
		n = len(t.mcp.keys)
	}
	t.mu.Unlock()
	for i := 0; i < n; i++ {
		t.mcpTestRow(i)
	}
}

// mcpDetail is enter: the row's servers, agent by agent, as far as they
// can be shown.
func (t *tui) mcpDetail() {
	t.mu.Lock()
	defer t.mu.Unlock()
	key, _, ok := t.mcpSelectedLocked()
	if !ok {
		return
	}
	st, v := t.mcp, t.mcpMgr
	var lines []string
	for _, id := range st.cols {
		e, has := st.entries[key][id]
		if !has {
			continue
		}
		state := "on"
		if !e.Enabled {
			state = "off"
			if e.Via == "stash" {
				state += " (kept by tend until it is turned on)"
			}
		}
		if e.Reason != "" {
			state += " — " + e.Reason
		}
		path := ""
		for _, a := range st.list.Agents {
			if a.ID == id {
				path = a.Path
			}
		}
		lines = append(lines, id+" · "+e.Name+" · "+state, "  file: "+tildeHome(path), "  "+e.Transport+": "+mcpTarget(e))
		if len(e.EnvKeys) > 0 {
			lines = append(lines, "  env: "+strings.Join(e.EnvKeys, ", "))
		}
		if len(e.HeaderKeys) > 0 {
			lines = append(lines, "  headers: "+strings.Join(e.HeaderKeys, ", "))
		}
		if len(e.Extras) > 0 {
			lines = append(lines, "  also: "+strings.Join(e.Extras, ", "))
		}
		lines = append(lines, "")
	}
	if r, ok := st.tests[key]; ok {
		lines = append(lines, "test: "+r[0])
	}
	v.Detail = lines
	t.dirty = true
}

// mcpAddKey is a key on the line a new server is typed on.
func (t *tui) mcpAddKey(key string) {
	t.mu.Lock()
	v := t.mcpMgr
	if v == nil {
		t.mu.Unlock()
		return
	}
	t.dirty = true
	switch {
	case key == "\x1b":
		v.Adding = false
		t.mu.Unlock()
	case key == "\x7f" || key == "\x08":
		if v.AddText != "" {
			_, size := utf8.DecodeLastRuneInString(v.AddText)
			v.AddText = v.AddText[:len(v.AddText)-size]
		}
		t.mu.Unlock()
	case key == "\r" || key == "\n":
		line := v.AddText
		_, agent, _ := t.mcpSelectedLocked()
		var targets []string
		if agent != "" {
			targets = []string{agent}
		} else if st := t.mcp; st != nil {
			targets = append(targets, st.cols...)
		}
		t.mu.Unlock()
		name, def, err := parseMCPSpec(line)
		if err != nil {
			t.mcpSay(err.Error(), true)
			return
		}
		t.mu.Lock()
		v.Adding = false
		t.mu.Unlock()
		where := strings.Join(targets, ", ")
		t.mcpAsk("add "+name+" ("+def.Transport+") to "+where+"?", func() error {
			res, err := t.client.MCPAdd(proto.MCPAddParams{Name: name, Agents: targets, Def: &def})
			if err != nil {
				return err
			}
			t.loadMCP(mcpWriteText("added "+name+" to", res))
			return nil
		})
	case len(key) >= 1 && (key[0] >= 0x20 && key[0] != 0x7f || key[0] >= 0x80) && utf8.ValidString(key):
		v.AddText += key
		t.mu.Unlock()
	default:
		t.mu.Unlock()
	}
}

// parseMCPSpec reads the line a server is added with:
//
//	name https://host/mcp [-H 'Name: value']...   streamable HTTP
//	name sse https://host/sse [-H ...]            the older SSE
//	name [KEY=value]... -- command [args]...      a program, on stdio
//
// The -- may be left out when the command is the first word that is not
// KEY=value. Words are split as a shell splits them, quotes and all.
func parseMCPSpec(line string) (string, proto.MCPDef, error) {
	words, err := shellWords(line)
	if err != nil {
		return "", proto.MCPDef{}, err
	}
	if len(words) < 2 {
		return "", proto.MCPDef{}, errors.New("give a name, then a URL or -- and a command")
	}
	name, rest := words[0], words[1:]
	def := proto.MCPDef{Transport: "stdio"}
	if rest[0] == "sse" || rest[0] == "http" {
		def.Transport, rest = rest[0], rest[1:]
		if len(rest) == 0 {
			return "", def, fmt.Errorf("%s needs a URL", def.Transport)
		}
	}
	if strings.Contains(rest[0], "://") {
		if def.Transport == "stdio" {
			def.Transport = "http"
		}
		def.URL, rest = rest[0], rest[1:]
		for len(rest) > 0 {
			if rest[0] != "-H" || len(rest) < 2 {
				return "", def, fmt.Errorf("after the URL only -H 'Name: value' is understood, not %q", rest[0])
			}
			k, val, ok := strings.Cut(rest[1], ":")
			if !ok {
				return "", def, fmt.Errorf("a header is Name: value, not %q", rest[1])
			}
			if def.Headers == nil {
				def.Headers = map[string]string{}
			}
			def.Headers[strings.TrimSpace(k)] = strings.TrimSpace(val)
			rest = rest[2:]
		}
		return name, def, nil
	}
	if def.Transport != "stdio" {
		return "", def, fmt.Errorf("%s needs a URL", def.Transport)
	}
	for len(rest) > 0 {
		if rest[0] == "--" {
			rest = rest[1:]
			break
		}
		k, val, ok := strings.Cut(rest[0], "=")
		if !ok || k == "" || strings.ContainsAny(k, "/ ") {
			break
		}
		if def.Env == nil {
			def.Env = map[string]string{}
		}
		def.Env[k] = val
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return "", def, errors.New("no command to start")
	}
	def.Command, def.Args = rest[0], rest[1:]
	return name, def, nil
}

// shellWords splits a line as a shell would: by spaces, with '…' and "…"
// keeping theirs, and \ escaping the next character outside single quotes.
func shellWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord, quote, escaped := false, rune(0), false
	for _, r := range line {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote is not closed")
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// mcpHelp shows what the panel is and how to use it, in words.
func (t *tui) mcpHelp() {
	t.mu.Lock()
	if v := t.mcpMgr; v != nil {
		v.Detail = ui.MCPHelp
		v.Adding, v.Confirm = false, ""
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// mcpRowMenu puts up what can be done to the cursor's row, said in words:
// for each agent, turning it off or on, or copying it there; testing it;
// its details; and removing it. It is the panel's keys as a menu, for
// somebody who has not learnt them. x and y -1 put it beside the row.
func (t *tui) mcpRowMenu(x, y int) {
	t.mu.Lock()
	key, _, ok := t.mcpSelectedLocked()
	v, st := t.mcpMgr, t.mcp
	if !ok {
		t.mu.Unlock()
		return
	}
	if x < 0 {
		g := ui.MCPManagerLayout(v, t.cols, t.rows)
		x, y = g.List.X+4, g.List.Y+(v.Row-v.Scroll)+1
	}
	name := v.Rows[v.Row].Name
	var items, removes []ui.MenuItem
	any := false
	for _, id := range st.cols {
		if _, has := st.entries[key][id]; has {
			any = true
		}
	}
	for _, id := range st.cols {
		e, has := st.entries[key][id]
		switch {
		case has && e.Enabled:
			items = append(items, ui.MenuItem{Label: "turn off in " + id, Action: ui.MenuMCPDo, Arg: "off:" + id})
		case has:
			items = append(items, ui.MenuItem{Label: "turn on in " + id, Action: ui.MenuMCPDo, Arg: "on:" + id})
		case any:
			items = append(items, ui.MenuItem{Label: "copy into " + id, Action: ui.MenuMCPDo, Arg: "copy:" + id})
		}
		if has {
			removes = append(removes, ui.MenuItem{Label: "remove from " + id, Action: ui.MenuMCPDo, Arg: "remove:" + id})
		}
	}
	items = append(items,
		ui.MenuItem{Label: "test it", Action: ui.MenuMCPDo, Arg: "test:"},
		ui.MenuItem{Label: "details", Action: ui.MenuMCPDo, Arg: "details:"})
	items = append(items, removes...)
	if len(removes) > 1 {
		items = append(items, ui.MenuItem{Label: "remove from every agent", Action: ui.MenuMCPDo, Arg: "remove:"})
	}
	items = append(items, ui.MenuItem{Label: "add a new server...", Action: ui.MenuMCPDo, Arg: "add:"})
	t.mu.Unlock()
	t.openMenu(ui.Menu{Title: name, Items: items, X: x, Y: y})
}

// mcpMenuAction does what an item of the row's menu says, by moving the
// cursor to the agent's column and doing what that key does there, so the
// menu and the keys cannot come to do different things.
func (t *tui) mcpMenuAction(arg string) error {
	action, agent, _ := strings.Cut(arg, ":")
	t.mu.Lock()
	v, st := t.mcpMgr, t.mcp
	if v == nil || st == nil {
		t.mu.Unlock()
		return nil
	}
	v.Col = 0
	for i, id := range st.cols {
		if id == agent {
			v.Col = i + 1
		}
	}
	t.dirty = true
	t.mu.Unlock()
	switch action {
	case "off", "on", "copy":
		t.mcpToggle()
	case "test":
		t.mcpTestRow(-1)
	case "details":
		t.mcpDetail()
	case "remove":
		t.mcpRemove()
	case "add":
		t.mu.Lock()
		v.Adding, v.AddText = true, ""
		t.mu.Unlock()
	}
	t.wakeUp()
	return nil
}
