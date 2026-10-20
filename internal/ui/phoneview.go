package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The phone view panel, opened from the global menu: whether tend web is
// serving this session's agents to a phone, at which addresses, and a button
// to turn it on or off. The session's server starts and stops it
// (internal/server/web.go); the panel says what the server says. It is
// tend's own, as the phone view is.

// PhoneView is the panel while it is up.
type PhoneView struct {
	// Loaded is whether the server has answered yet; Busy says what is
	// being done meanwhile ("turning on…").
	Loaded bool
	Busy   string
	// On is what was asked for and Running what is: on and not running is
	// a view that failed to start, which Error says why.
	On, Running bool
	URLs        []string
	User        string
	Error       string
	// Message is a line of what just happened: an address copied.
	Message string
}

// Phone view buttons, and the address lines a click copies.
const (
	PhoneToggle = "toggle"
	PhoneCopy   = "copy"
	PhoneClose  = "close"
)

// phoneURLRows is how many addresses are listed; a machine with more
// networks than that is rare, and the first is the one usually wanted.
const phoneURLRows = 3

// PhoneGeometry is where the panel's parts are.
type PhoneGeometry struct {
	Box     Rect
	URLs    []Rect
	Buttons []IssueButton
}

const (
	phoneCols = 76
	phoneRows = 17
)

// PhoneLayout is the panel's geometry on a screen of cols by rows.
func PhoneLayout(v *PhoneView, cols, rows int) PhoneGeometry {
	w, h := min(phoneCols, cols-2), min(phoneRows, rows-2)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: max(w, 0), Rows: max(h, 0)}
	g := PhoneGeometry{Box: box}
	if v.Running {
		for i, u := range v.URLs {
			if i == phoneURLRows {
				break
			}
			g.URLs = append(g.URLs, Rect{X: box.X + 5, Y: box.Y + 7 + i, Cols: min(runewidth.StringWidth(u), box.Cols-8), Rows: 1})
		}
	}
	toggle := "[ Turn on ]"
	if v.On || v.Running {
		toggle = "[ Turn off ]"
	}
	buttons := []IssueButton{{ID: PhoneToggle, Label: toggle}}
	if v.Running && len(v.URLs) > 0 {
		buttons = append(buttons, IssueButton{ID: PhoneCopy, Label: "[ Copy address ]"})
	}
	buttons = append(buttons, IssueButton{ID: PhoneClose, Label: "[ Close ]"})
	bx, bottom := box.X+3, box.Y+box.Rows-2
	for _, b := range buttons {
		b.Rect = Rect{X: bx, Y: bottom, Cols: runewidth.StringWidth(b.Label), Rows: 1}
		if b.ID == PhoneClose {
			b.X = box.X + box.Cols - 3 - b.Cols
		} else {
			bx += b.Cols + 1
		}
		g.Buttons = append(g.Buttons, b)
	}
	return g
}

// PhoneAt is what a click at a point is on: a button, or an address
// ("url:" and its index), which a click copies.
func PhoneAt(v *PhoneView, cols, rows, x, y int) (string, bool) {
	g := PhoneLayout(v, cols, rows)
	if OnCloseMark(g.Box, x, y) {
		return PhoneClose, true
	}
	for _, b := range g.Buttons {
		if y == b.Y && x >= b.X && x < b.X+b.Cols {
			return b.ID, true
		}
	}
	for i, r := range g.URLs {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return "url:" + itoa(uint64(i)), true
		}
	}
	return "", false
}

func drawPhone(dst *vt.Grid, v *PhoneView, theme Theme) {
	g := PhoneLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 14 || box.Cols < 44 {
		return
	}
	right := box.X + box.Cols - 1
	width := box.Cols - 6
	writeString(dst, box.X+2, box.Y, " phone view ", withBold(theme.NotesAccent), right)
	x0 := box.X + 3
	writeString(dst, x0, box.Y+2, "your agents on a phone: watch them, message them, answer them", theme.NotesSub, right)

	state, style := "off", theme.NotesSub
	switch {
	case v.Busy != "":
		state, style = v.Busy, withBold(theme.Notes)
	case !v.Loaded:
		state = "asking the session…"
	case v.Running:
		state, style = "on — serving this session's agents", withBold(theme.Idle)
	case v.On:
		state, style = "on, but not running", withBold(theme.Blocked)
	}
	writeString(dst, x0, box.Y+4, "state", theme.NotesSub, right)
	writeString(dst, x0+8, box.Y+4, truncate(state, width-8), style, right)

	switch {
	case v.Running && len(v.URLs) > 0:
		writeString(dst, x0, box.Y+6, "open one of these on the phone (it pairs it):", theme.NotesSub, right)
		for i, r := range g.URLs {
			writeString(dst, r.X, r.Y, truncate(v.URLs[i], r.Cols), theme.NotesAccent, right)
		}
	case v.Running:
		writeString(dst, x0, box.Y+6, "running, but this machine has no network a phone can reach", theme.NotesSub, right)
	case v.Error != "":
		writeString(dst, x0, box.Y+6, truncate(v.Error, width), withBold(theme.Blocked), right)
	case v.Loaded:
		writeString(dst, x0, box.Y+6, "turned on, it serves a page to your network or VPN ([web] listen,", theme.NotesSub, right)
		writeString(dst, x0, box.Y+7, "port 7777 unless set), and comes back when tend's server restarts.", theme.NotesSub, right)
	}

	login := "no login set: `tend web -set-password` sets one, to sign in instead"
	if v.User != "" {
		login = "or open it without the address's token and sign in as " + v.User
	}
	if v.Loaded {
		writeString(dst, x0, box.Y+11, truncate(login, width), theme.NotesSub, right)
	}
	if v.Message != "" {
		writeString(dst, x0, box.Y+box.Rows-4, truncate(v.Message, width), theme.Notes, right)
	}
	for _, b := range g.Buttons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}
