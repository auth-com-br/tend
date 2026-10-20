package main

import (
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The phone view panel (internal/ui/phoneview.go): tend web turned on and
// off from the global menu. The session's server starts it, finds it and
// stops it (internal/server/web.go), so it outlives this client and comes
// back with the server; the panel only asks and shows.

func (t *tui) phoneUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.phone != nil
}

// openPhone puts the panel up and asks the server how the view is.
func (t *tui) openPhone() {
	if !t.serverKnows(proto.MethodWebStatus) {
		t.setMessage("this server is older than the phone view in the menu; "+handoffCommand(t.session)+" moves it to this build", true)
		return
	}
	t.mu.Lock()
	t.phone = &ui.PhoneView{}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.phoneCall(t.client.WebStatus, "")
}

// phoneCall asks the server for the view's state, or to change it, and
// shows the answer. busy is what the panel says while it waits.
func (t *tui) phoneCall(call func() (proto.WebStatus, error), busy string) {
	if busy != "" {
		t.updatePhone(func(v *ui.PhoneView) { v.Busy, v.Message = busy, "" })
	}
	st, err := call()
	t.updatePhone(func(v *ui.PhoneView) {
		v.Busy = ""
		if err != nil {
			v.Loaded = true
			v.Error = err.Error()
			return
		}
		v.Loaded, v.On, v.Running, v.URLs, v.User, v.Error = true, st.On, st.Running, st.URLs, st.User, st.Error
	})
}

func (t *tui) updatePhone(change func(*ui.PhoneView)) {
	t.mu.Lock()
	if t.phone != nil {
		change(t.phone)
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) closePhone() {
	t.mu.Lock()
	t.phone = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// phoneInput is every key and click while the panel is up: o (or space)
// turns the view on or off, c copies the first address, esc, enter or q
// closes it; a click on an address copies that one.
func (t *tui) phoneInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		t.mu.Lock()
		v, cols, rows := t.phone, t.cols, t.rows
		t.mu.Unlock()
		if v == nil {
			return nil
		}
		if id, ok := ui.PhoneAt(v, cols, rows, ev.X, ev.Y); ok {
			t.phoneAction(id)
		}
	}
	for _, key := range splitKeys(forward) {
		switch key {
		case "\x1b", "\r", "\n", "q":
			t.closePhone()
			return nil
		case "o", " ":
			t.phoneAction(ui.PhoneToggle)
		case "c":
			t.phoneAction(ui.PhoneCopy)
		}
	}
	return nil
}

func (t *tui) phoneAction(id string) {
	t.mu.Lock()
	v := t.phone
	var on, busy bool
	var urls []string
	if v != nil {
		on, busy, urls = v.On || v.Running, v.Busy != "" || !v.Loaded, v.URLs
	}
	t.mu.Unlock()
	if v == nil {
		return
	}
	switch {
	case id == ui.PhoneClose:
		t.closePhone()
	case id == ui.PhoneToggle && !busy:
		if on {
			go t.phoneCall(t.client.WebStop, "turning off…")
		} else {
			go t.phoneCall(t.client.WebStart, "turning on…")
		}
	case id == ui.PhoneCopy && len(urls) > 0:
		t.copyPhoneURL(urls[0])
	case strings.HasPrefix(id, "url:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "url:")); err == nil && i < len(urls) {
			t.copyPhoneURL(urls[i])
		}
	}
}

// copyPhoneURL copies an address, to send it to the phone some other way
// than typing it.
func (t *tui) copyPhoneURL(u string) {
	t.copyToClipboard(u, "phone address copied")
	t.updatePhone(func(v *ui.PhoneView) { v.Message = "copied: send it to the phone, or open it there" })
}
