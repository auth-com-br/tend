package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The sessions list (internal/ui/sessions.go, internal/agentsessions):
// tend's own. The server reads the conversations its machine's agents keep —
// Claude Code's so far — and says which is open in which pane; this lists
// them, searches them as the user types, resumes one in a new tab where it
// was held, and deletes the ones picked, after asking. A conversation open
// in a pane is gone to rather than opened twice, and is never deleted: the
// server refuses that whatever this asks.

// sessionsOld is how old "ctrl+o" means: not written to in a month.
const sessionsOld = 30 * 24 * time.Hour

// resumeScript runs the agent's resume and leaves a shell in the pane when
// it ends, as a tab the user had opened and typed the command into would.
const resumeScript = `"$@"
exec "${SHELL:-/bin/sh}"`

func (t *tui) sessionsUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions != nil
}

// openSessions puts the list up and asks the server for the conversations.
func (t *tui) openSessions() error {
	t.mu.Lock()
	if t.sessions == nil {
		t.sessions = &ui.SessionsView{Marked: map[string]bool{}}
	}
	t.sessions.Loading = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.loadSessions("")
	return nil
}

// loadSessions reads the list again, keeping the search and what is marked,
// and says message under it when there is one.
func (t *tui) loadSessions(message string) {
	if !t.client.Supports(proto.MethodAgentSessions) {
		t.mu.Lock()
		if v := t.sessions; v != nil {
			v.Loading = false
			v.Message = "this server is older than the sessions list; " + handoffCommand(t.session) + " moves it to this build"
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return
	}
	res, err := t.client.AgentSessions()
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v := t.sessions
	if v == nil {
		return // closed while it read
	}
	v.Loading = false
	if err != nil {
		v.Message = "could not read the sessions: " + err.Error()
		return
	}
	t.sessionList = res.Sessions
	here := map[string]bool{}
	for _, s := range res.Sessions {
		here[s.ID] = true
	}
	for id := range v.Marked {
		if !here[id] {
			delete(v.Marked, id)
		}
	}
	t.filterSessionsLocked()
	if message == "" {
		var total int64
		for _, s := range res.Sessions {
			total += s.Size
		}
		message = fmt.Sprintf("%d sessions, %s on disk", len(res.Sessions), ui.SessionSize(total))
	}
	v.Message = message
}

// filterSessionsLocked shows what the search matches, the cursor kept on
// the entry it was on when that is still shown.
func (t *tui) filterSessionsLocked() {
	v := t.sessions
	if v == nil {
		return
	}
	var at string
	if v.Cursor < len(v.Shown) {
		at = v.Shown[v.Cursor].ID
	}
	all := make([]ui.SessionEntry, 0, len(t.sessionList))
	for _, s := range t.sessionList {
		all = append(all, ui.SessionEntry{
			ID: s.ID, Agent: s.Agent, Title: s.Title, Dir: s.Dir,
			Modified: time.Unix(s.Modified, 0), Size: s.Size, Prompts: s.Prompts, Pane: s.Pane,
			Unreachable: s.Unreachable,
		})
	}
	v.Total = len(all)
	v.Shown = ui.FilterSessions(all, v.Query)
	v.Now = time.Now()
	v.Cursor = 0
	for i, e := range v.Shown {
		if e.ID == at {
			v.Cursor = i
		}
	}
	v.Scroll = ui.SessionsScrollFor(v, t.cols, t.rows)
}

func (t *tui) closeSessions() {
	t.mu.Lock()
	t.sessions = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// sessionsInput is every key and click while the list is up. What is
// typed searches, so the list moves by the arrows and acts by enter, tab
// and control keys only; esc steps back — out of a delete's question, then
// out of the search, then out of the list.
func (t *tui) sessionsInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		switch ev.Kind {
		case ui.MouseWheelUp:
			t.moveSessionCursor(-3)
			continue
		case ui.MouseWheelDown:
			t.moveSessionCursor(3)
			continue
		}
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		t.mu.Lock()
		v, cols, rows := t.sessions, t.cols, t.rows
		if v == nil {
			t.mu.Unlock()
			return nil
		}
		g := ui.SessionsLayout(cols, rows)
		i, onEntry := ui.SessionAt(v, cols, rows, ev.X, ev.Y)
		t.mu.Unlock()
		switch {
		case inRect(g.Close, ev.X, ev.Y), ui.OnCloseMark(g.Box, ev.X, ev.Y):
			t.closeSessions()
			return nil
		case inRect(g.Resume, ev.X, ev.Y):
			return t.resumeSession()
		case inRect(g.Delete, ev.X, ev.Y):
			t.askDeleteSessions()
		case onEntry:
			t.mu.Lock()
			again := v.Cursor == i && !v.Confirm
			v.Cursor, v.Confirm = i, false
			t.dirty = true
			t.mu.Unlock()
			// A click on the one already chosen resumes it.
			if again {
				return t.resumeSession()
			}
		}
	}
	for _, key := range splitKeys(forward) {
		switch key {
		case "\x1b":
			t.mu.Lock()
			v := t.sessions
			switch {
			case v == nil:
			case v.Confirm:
				v.Confirm = false
			case v.Query != "":
				v.Query = ""
				t.filterSessionsLocked()
			default:
				t.mu.Unlock()
				t.closeSessions()
				return nil
			}
			t.dirty = true
			t.mu.Unlock()
		case "\r", "\n":
			if t.sessionsConfirming() {
				t.deleteSessions()
				continue
			}
			return t.resumeSession()
		case "\x1b[A", "\x10": // up, ctrl+p
			t.moveSessionCursor(-1)
		case "\x1b[B", "\x0e": // down, ctrl+n
			t.moveSessionCursor(1)
		case "\x1b[5~":
			t.moveSessionCursor(-10)
		case "\x1b[6~":
			t.moveSessionCursor(10)
		case "\t":
			t.markSession()
		case "\x01": // ctrl+a
			t.markShownSessions(0)
		case "\x0f": // ctrl+o
			t.markShownSessions(sessionsOld)
		case "\x04", "\x1b[3~": // ctrl+d, delete
			t.askDeleteSessions()
		case "\x12": // ctrl+r
			return t.openSessions()
		case "\x07": // ctrl+g: continue the chosen conversation in an agent
			t.continueSession()
		case "\x15": // ctrl+u
			t.editSessionQuery(func(string) string { return "" })
		case "\x7f", "\x08":
			t.editSessionQuery(func(q string) string {
				_, size := utf8.DecodeLastRuneInString(q)
				return q[:len(q)-size]
			})
		default:
			if len(key) == 1 && (key[0] >= 0x20 && key[0] != 0x7f) {
				// A byte of what was typed; one of several for a letter
				// with an accent, which the next ones complete.
				t.editSessionQuery(func(q string) string { return q + key })
			}
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) sessionsConfirming() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions != nil && t.sessions.Confirm
}

func (t *tui) editSessionQuery(edit func(string) string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.sessions
	if v == nil {
		return
	}
	v.Query = edit(v.Query)
	v.Confirm = false
	if utf8.ValidString(v.Query) {
		t.filterSessionsLocked()
	}
	t.dirty = true
}

func (t *tui) moveSessionCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.sessions
	if v == nil || len(v.Shown) == 0 {
		return
	}
	v.Cursor = max(min(v.Cursor+by, len(v.Shown)-1), 0)
	v.Confirm = false
	v.Scroll = ui.SessionsScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

// markSession marks the one under the cursor, or unmarks it, and moves on,
// so tab down a run marks the run.
func (t *tui) markSession() {
	t.mu.Lock()
	v := t.sessions
	if v == nil || v.Cursor >= len(v.Shown) {
		t.mu.Unlock()
		return
	}
	id := v.Shown[v.Cursor].ID
	if v.Marked[id] {
		delete(v.Marked, id)
	} else {
		v.Marked[id] = true
	}
	t.mu.Unlock()
	t.moveSessionCursor(1)
}

// markShownSessions marks what the search shows, those not written to in
// olderThan when it is not zero; when every one of them is marked already,
// it unmarks them instead. One open in a pane is left out: it would only be
// refused.
func (t *tui) markShownSessions(olderThan time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.sessions
	if v == nil {
		return
	}
	var pick []string
	for _, e := range v.Shown {
		if e.Pane == 0 && (olderThan == 0 || v.Now.Sub(e.Modified) >= olderThan) {
			pick = append(pick, e.ID)
		}
	}
	all := len(pick) > 0
	for _, id := range pick {
		all = all && v.Marked[id]
	}
	for _, id := range pick {
		if all {
			delete(v.Marked, id)
		} else {
			v.Marked[id] = true
		}
	}
	v.Confirm = false
	switch {
	case len(pick) == 0 && olderThan > 0:
		v.Message = "none of these is 30 days old"
	case all:
		v.Message = fmt.Sprintf("unmarked %d", len(pick))
	default:
		v.Message = fmt.Sprintf("marked %d — ctrl+d deletes them", len(pick))
	}
	t.dirty = true
}

// sessionsToDeleteLocked is what a delete takes: the marked ones, or with
// none marked the one under the cursor.
func (t *tui) sessionsToDeleteLocked() []string {
	v := t.sessions
	if v == nil {
		return nil
	}
	if len(v.Marked) > 0 {
		ids := make([]string, 0, len(v.Marked))
		for id := range v.Marked {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return ids
	}
	if v.Cursor < len(v.Shown) {
		return []string{v.Shown[v.Cursor].ID}
	}
	return nil
}

// askDeleteSessions asks before deleting: a conversation deleted cannot be
// resumed, and nothing keeps a copy.
func (t *tui) askDeleteSessions() {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.sessions
	if v == nil {
		return
	}
	ids := t.sessionsToDeleteLocked()
	if len(ids) == 0 {
		return
	}
	if len(ids) == 1 && len(v.Marked) == 0 && v.Shown[v.Cursor].Pane != 0 {
		v.Message = fmt.Sprintf("that session is open in pane %d; close it there first", v.Shown[v.Cursor].Pane)
		t.dirty = true
		return
	}
	v.Confirm = true
	t.dirty = true
}

// deleteSessions deletes what was asked about, and reads the list again
// with what happened under it.
func (t *tui) deleteSessions() {
	t.mu.Lock()
	ids := t.sessionsToDeleteLocked()
	if v := t.sessions; v != nil {
		v.Confirm = false
		v.Loading = true
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if len(ids) == 0 {
		return
	}
	go func() {
		res, err := t.client.DeleteAgentSessions(ids)
		message := ""
		switch {
		case err != nil:
			message = "could not delete: " + err.Error()
		case len(res.Kept) > 0:
			var why []string
			for _, reason := range res.Kept {
				why = append(why, reason)
			}
			sort.Strings(why)
			message = fmt.Sprintf("deleted %d, kept %d (%s)", len(res.Deleted), len(res.Kept), strings.Join(dedupe(why), "; "))
		default:
			message = fmt.Sprintf("deleted %d", len(res.Deleted))
		}
		t.mu.Lock()
		if v := t.sessions; v != nil {
			for _, id := range res.Deleted {
				delete(v.Marked, id)
			}
		}
		t.mu.Unlock()
		t.loadSessions(message)
	}()
}

func dedupe(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// resumeSession continues the conversation under the cursor: in the pane
// it is open in, when one is, and otherwise in a new tab of the space shown,
// started in the directory it was held in — which is where Claude Code
// looks for it.
func (t *tui) resumeSession() error {
	t.mu.Lock()
	v := t.sessions
	if v == nil || v.Cursor >= len(v.Shown) {
		t.mu.Unlock()
		return nil
	}
	e := v.Shown[v.Cursor]
	t.mu.Unlock()
	ws := t.shownWorkspace()

	if e.Pane != 0 {
		t.closeSessions()
		return t.jumpToPane(e.Pane)
	}
	argv, ok := agent.Resume(agent.PersistedSession{
		Source: "tend:" + e.Agent, Agent: e.Agent, Session: agent.SessionRef{ID: e.ID},
	})
	if !ok {
		t.mu.Lock()
		if t.sessions != nil {
			t.sessions.Message = "tend does not know how to resume a " + e.Agent + " session"
		}
		t.dirty = true
		t.mu.Unlock()
		return nil
	}
	if e.Unreachable != "" {
		// An agent finds a conversation by the directory it was held in,
		// so there is nowhere else to resume it from. What would, run where
		// that directory can be entered — as root, for one in /root — is
		// handed over instead of a tab that cannot start.
		command := "cd " + shellQuote(e.Dir) + " && " + shellJoin(argv)
		t.copyToClipboard(command, "resume command copied")
		t.mu.Lock()
		if t.sessions != nil {
			t.sessions.Message = "cannot resume here: " + e.Unreachable + " — copied what resumes it where it can: " + command
		}
		t.dirty = true
		t.mu.Unlock()
		return nil
	}
	name := e.Title
	if name == "" {
		name = e.Agent
	}
	if r := []rune(name); len(r) > 24 {
		name = string(r[:23]) + "…"
	}
	tab, _, err := t.client.NewTab(ws, name, proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", resumeScript, "tend-resume"}, argv...),
		Dir:     e.Dir,
		Agent:   e.Agent,
	})
	if err != nil {
		t.mu.Lock()
		if t.sessions != nil {
			t.sessions.Message = "could not resume it: " + err.Error()
		}
		t.dirty = true
		t.mu.Unlock()
		return nil
	}
	t.closeSessions()
	t.mu.Lock()
	t.rememberFocusLocked()
	t.tab, t.focus, t.zoom = tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}

// shellJoin is argv as a shell reads it back, each word quoted.
func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// continueSession offers the agents the conversation under the cursor can
// be handed to (tui_continue.go): the list goes, and the "continue in" menu
// opens where the list was.
func (t *tui) continueSession() {
	t.mu.Lock()
	v := t.sessions
	if v == nil || v.Cursor >= len(v.Shown) {
		t.mu.Unlock()
		return
	}
	e := v.Shown[v.Cursor]
	g := ui.SessionsLayout(t.cols, t.rows)
	t.mu.Unlock()
	if e.Agent != "claude" {
		t.mu.Lock()
		if t.sessions != nil {
			t.sessions.Message = "only Claude Code's conversations can be handed on so far"
		}
		t.dirty = true
		t.mu.Unlock()
		return
	}
	t.closeSessions()
	t.openMenu(ui.ContinueMenu(0, e.ID, continueAgents(), g.Box.X+g.Box.Cols/3, g.Box.Y+2))
}
