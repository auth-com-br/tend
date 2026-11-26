package main

import (
	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/notify"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
	"github.com/auth-com-br/tend/internal/ui"
)

// The context panel (internal/ui/contextpanel.go): the server's context
// buffer, tend's own. What a tool captured — the files panel's Add to
// context, `tend context add`, a browser extension over the socket — is
// listed; the chosen item can be copied, sent to the agent this tab is
// working with (typed into its pane, not submitted, for the user to read
// over), or dropped. The panel reads the buffer again whenever the server
// says it changed, so a capture made while it is up shows at once.

func (t *tui) contextUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.contextView != nil
}

// openContext puts the panel up and reads the buffer.
func (t *tui) openContext() error {
	t.mu.Lock()
	t.contextView = &ui.ContextView{Loading: true}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.loadContext()
	return nil
}

func (t *tui) closeContext() {
	t.mu.Lock()
	t.contextView = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// contextChanged is EventContextChanged: the panel, if up, reads again.
func (t *tui) contextChanged() {
	if t.contextUp() {
		go t.loadContext()
	}
}

// loadContext reads the buffer into the panel, newest first, keeping the
// cursor on the item it was on when that item is still there.
func (t *tui) loadContext() {
	if !t.serverHas(proto.FeatureContext) {
		t.mu.Lock()
		if v := t.contextView; v != nil {
			v.Loading = false
			v.Message = "this server is older than the context panel; tend handoff moves it to this build"
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return
	}
	items, err := t.client.ContextList()
	tools.ContextNewestFirst(items)
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v := t.contextView
	if v == nil {
		return
	}
	v.Loading = false
	if err != nil {
		v.Message = "could not read the context: " + err.Error()
		return
	}
	var was uint64
	if v.Cursor < len(t.contextItems) {
		was = t.contextItems[v.Cursor].ID
	}
	t.contextItems = items
	v.Items = v.Items[:0]
	v.Cursor = 0
	for i, it := range items {
		v.Items = append(v.Items, contextEntry(it))
		if it.ID == was {
			v.Cursor = i
		}
	}
	_, v.Target = t.contextTargetLocked()
}

// contextEntry is an item as the panel shows it (internal/tools).
var contextEntry = tools.ContextEntry

// contextTargetLocked is the pane Send to Agent types into, and its name: the
// pane in focus when it runs an agent, else an agent in this tab, else one in
// this space, else the pane in focus — the one the user is working with,
// which is how the session already decides what "the agent" is.
func (t *tui) contextTargetLocked() (uint64, string) {
	shown := make([]uint64, 0, len(t.rects))
	for _, r := range t.rects {
		shown = append(shown, r.Pane)
	}
	var tabs [][]uint64
	if w, ok := t.workspaceLocked(); ok {
		for _, tab := range w.Tabs {
			tabs = append(tabs, tab.Panes)
		}
	}
	return tools.ContextTarget(t.snap.Panes, t.focus, shown, tabs)
}

// contextInput is every key and click while the panel is up.
func (t *tui) contextInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		switch ev.Kind {
		case ui.MouseWheelUp:
			t.moveContextCursor(-1)
			continue
		case ui.MouseWheelDown:
			t.moveContextCursor(1)
			continue
		}
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		t.mu.Lock()
		v, cols, rows := t.contextView, t.cols, t.rows
		if v == nil {
			t.mu.Unlock()
			return nil
		}
		if ui.OnCloseMark(ui.ContextLayout(v, cols, rows).Box, ev.X, ev.Y) {
			t.mu.Unlock()
			return t.contextButton(ui.ContextClose)
		}
		b, onButton := ui.ContextButtonAt(v, cols, rows, ev.X, ev.Y)
		i, onItem := ui.ContextItemAt(v, cols, rows, ev.X, ev.Y)
		if onItem {
			v.Cursor, v.ClearArmed = i, false
			t.dirty = true
		}
		t.mu.Unlock()
		if onButton {
			return t.contextButton(b)
		}
	}
	for _, key := range splitKeys(forward) {
		var err error
		switch key {
		case "\x1b", "q":
			t.closeContext()
			return nil
		case "\x1b[A", "k":
			t.moveContextCursor(-1)
		case "\x1b[B", "j":
			t.moveContextCursor(1)
		case "c", "y":
			err = t.contextButton(ui.ContextCopy)
		case "s", "\r", "\n":
			err = t.contextButton(ui.ContextSend)
		case "S":
			err = t.contextButton(ui.ContextSendAll)
		case "x", "d":
			err = t.contextButton(ui.ContextRemove)
		case "X":
			err = t.contextButton(ui.ContextClear)
		}
		if err != nil || !t.contextUp() {
			return err
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) moveContextCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.contextView
	if v == nil || len(v.Items) == 0 {
		return
	}
	v.Cursor = min(max(v.Cursor+by, 0), len(v.Items)-1)
	v.ClearArmed = false
	t.dirty = true
}

// contextButton does what a button says, to the chosen item.
func (t *tui) contextButton(b ui.ContextButton) error {
	t.mu.Lock()
	v := t.contextView
	if v == nil {
		t.mu.Unlock()
		return nil
	}
	if b == ui.ContextClose {
		t.mu.Unlock()
		t.closeContext()
		return nil
	}
	if v.Cursor >= len(t.contextItems) {
		t.mu.Unlock()
		return nil
	}
	item := t.contextItems[v.Cursor]
	target, targetName := t.contextTargetLocked()
	armed := v.ClearArmed
	v.ClearArmed = false
	t.mu.Unlock()

	switch b {
	case ui.ContextCopy:
		t.copyToClipboard(capture.Format([]proto.ContextItem{item}), "copied the context")
		return nil
	case ui.ContextSend, ui.ContextSendAll:
		if target == 0 {
			t.setContextMessage("no pane to send to in this tab")
			return nil
		}
		ids := []uint64{item.ID}
		if b == ui.ContextSendAll {
			// Oldest first, as they were captured: the browser's message
			// leads the elements it came with.
			ids = nil
		}
		if err := t.client.ContextSend(target, ids); err != nil {
			t.setContextMessage("could not send it: " + err.Error())
			return nil
		}
		// To the agent, where it was typed, for the user to read it over
		// and send it on.
		t.closeContext()
		t.setMessage("typed into "+targetName+" — read it over and press enter to send", false)
		return t.jumpToPane(target)
	case ui.ContextRemove:
		if err := t.client.ContextRemove([]uint64{item.ID}); err != nil {
			t.setContextMessage("could not remove it: " + err.Error())
		}
		return nil
	case ui.ContextClear:
		if !armed {
			t.mu.Lock()
			if t.contextView != nil {
				t.contextView.ClearArmed = true
			}
			t.dirty = true
			t.mu.Unlock()
			return nil
		}
		if err := t.client.ContextClear(); err != nil {
			t.setContextMessage("could not clear it: " + err.Error())
		}
		return nil
	}
	return nil
}

func (t *tui) setContextMessage(msg string) {
	t.mu.Lock()
	if t.contextView != nil {
		t.contextView.Message = msg
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// contextArrived is EventContextArrived: a tool — the browser — handed the
// context something. The panel is where it is looked over and sent on, so
// it opens, unless something else is up and has the keys, when a notice
// says where it is instead.
func (t *tui) contextArrived(ev proto.Event) {
	what := "from the " + ev.Title
	if n := ev.Body; n != "" {
		what = n + " " + what
	}
	t.mu.Lock()
	busy := t.notes != nil || t.about != nil || t.phone != nil || t.agentMgr != nil || t.mcpMgr != nil || t.permMgr != nil || t.sessions != nil || t.issues != nil || t.errors != nil || t.menu != nil || t.settings != nil ||
		t.prompt != promptNone || t.navigator != nil || t.onboarding
	open := t.contextView != nil
	t.mu.Unlock()
	switch {
	case open:
		t.contextChanged()
	case busy:
		t.raise(ui.ToastCustom, "context", what+" — prefix+C to look it over", 0, notify.SoundNone)
	default:
		_ = t.openContext()
		t.setMessage(what+" in the context — s sends one, S all of it", false)
	}
}
