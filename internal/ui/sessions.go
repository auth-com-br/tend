package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The sessions list is tend's own: a panel over the screen with the
// conversations agents keep on the session's machine, searched as it is
// typed into, to resume one in a new tab or delete the ones no longer
// wanted. It is drawn in the release notes panel's colours, as the agent
// manager is. What the keys do is the client's; this draws, filters, and
// says where a click landed.

// SessionEntry is one conversation as the list shows it.
type SessionEntry struct {
	ID    string
	Agent string
	Title string
	Dir   string
	// Modified is when it was last written; Size its bytes; Pane the pane
	// it is open in, zero for none.
	Modified time.Time
	Size     int64
	Prompts  int
	Pane     uint64
	// Unreachable is why it cannot be resumed here, or "".
	Unreachable string
}

// SessionsView is the panel while it is up.
type SessionsView struct {
	// Query is what has been typed; Shown the entries it matches, in the
	// order they are listed.
	Query string
	Shown []SessionEntry
	// Total is how many there are before the search.
	Total int
	// Marked are the ones picked for a delete, by id.
	Marked map[string]bool
	Cursor int
	Scroll int
	// Loading is while the server reads them; Confirm while a delete waits
	// for its answer.
	Loading bool
	Confirm bool
	Message string
	// Now is what the ages are counted from, so drawing reads no clock.
	Now time.Time
}

// SessionsGeometry is where the panel's parts are, for drawing and a click.
type SessionsGeometry struct {
	Box Rect
	// Search is the line typed into; List where the entries go, one a
	// line from Scroll.
	Search Rect
	List   Rect
	// The buttons on the bottom line.
	Resume, Delete, Close Rect
}

const (
	sessionsCols = 110
	sessionsRows = 34
)

var sessionsButtons = [3]string{"[ Resume ]", "[ Delete ]", "[ Close ]"}

// SessionsLayout is the panel's geometry on a screen of cols by rows.
func SessionsLayout(cols, rows int) SessionsGeometry {
	w, h := min(sessionsCols, cols-2), min(sessionsRows, rows-2)
	w, h = max(w, 0), max(h, 0)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := SessionsGeometry{Box: box}
	// A title, the search, a blank; the list; a blank, a message, a hint,
	// the buttons; inside a frame.
	g.Search = Rect{X: box.X + 2, Y: box.Y + 2, Cols: max(box.Cols-4, 0), Rows: 1}
	g.List = Rect{X: box.X + 2, Y: box.Y + 4, Cols: max(box.Cols-4, 0), Rows: max(box.Rows-9, 0)}
	bottom := box.Y + box.Rows - 2
	x := box.X + 2
	place := func(label string) Rect {
		r := Rect{X: x, Y: bottom, Cols: runewidth.StringWidth(label), Rows: 1}
		x += r.Cols + 1
		return r
	}
	g.Resume = place(sessionsButtons[0])
	g.Delete = place(sessionsButtons[1])
	g.Close = Rect{X: box.X + box.Cols - 2 - runewidth.StringWidth(sessionsButtons[2]), Y: bottom, Cols: runewidth.StringWidth(sessionsButtons[2]), Rows: 1}
	return g
}

// SessionAt is the entry on a line of the list, by its place in Shown.
func SessionAt(v *SessionsView, cols, rows, x, y int) (int, bool) {
	g := SessionsLayout(cols, rows)
	if x < g.List.X || x >= g.List.X+g.List.Cols || y < g.List.Y || y >= g.List.Y+g.List.Rows {
		return 0, false
	}
	i := clampSessionsScroll(v, g) + y - g.List.Y
	return i, i < len(v.Shown)
}

// SessionsScrollFor is the scroll that keeps the cursor in view.
func SessionsScrollFor(v *SessionsView, cols, rows int) int {
	g := SessionsLayout(cols, rows)
	top := clampSessionsScroll(v, g)
	if v.Cursor < top {
		top = v.Cursor
	}
	if g.List.Rows > 0 && v.Cursor >= top+g.List.Rows {
		top = v.Cursor - g.List.Rows + 1
	}
	return max(top, 0)
}

func clampSessionsScroll(v *SessionsView, g SessionsGeometry) int {
	return max(min(v.Scroll, len(v.Shown)-g.List.Rows), 0)
}

// FilterSessions is the entries a search matches: every word of it, in any
// case, somewhere in the title, the directory, the agent or the id. An
// empty search is all of them.
func FilterSessions(all []SessionEntry, query string) []SessionEntry {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return all
	}
	out := make([]SessionEntry, 0, len(all))
	for _, e := range all {
		hay := strings.ToLower(e.Title + " " + e.Dir + " " + e.Agent + " " + e.ID)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, e)
		}
	}
	return out
}

// SessionAge is how long ago a time was, in the fewest characters: "now",
// "12m", "5h", "3d", and past two months the date.
func SessionAge(now, then time.Time) string {
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return then.Format("Jan 06")
}

// SessionSize is a size in bytes as K, M or G.
func SessionSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%dB", n)
	case n < 1<<20:
		return fmt.Sprintf("%dK", n>>10)
	case n < 1<<30:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
}

// sessionProject is the last two parts of a directory, what tells projects
// apart without the whole path.
func sessionProject(dir string) string {
	if dir == "" {
		return ""
	}
	parent, base := filepath.Split(filepath.Clean(dir))
	if p := filepath.Base(parent); p != "" && p != "/" && p != "." {
		return p + "/" + base
	}
	return base
}

func drawSessions(dst *vt.Grid, v *SessionsView, theme Theme) {
	g := SessionsLayout(dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 10 || box.Cols < 40 {
		return
	}
	right := box.X + box.Cols - 1
	title := "AGENT SESSIONS"
	writeString(dst, box.X+(box.Cols-len(title))/2, box.Y+1, title, withBold(theme.NotesAccent), right)
	count := fmt.Sprintf("%d of %d", len(v.Shown), v.Total)
	if len(v.Marked) > 0 {
		count = fmt.Sprintf("%d marked · %s", len(v.Marked), count)
	}
	writeString(dst, right-1-runewidth.StringWidth(count), box.Y+1, count, theme.NotesSub, right)

	// The search, with the cursor at its end; what to type while empty.
	end := g.Search.X + g.Search.Cols
	x := writeString(dst, g.Search.X, g.Search.Y, "search ", theme.NotesSub, end)
	if v.Query == "" {
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
		writeString(dst, x+2, g.Search.Y, "title, project or id", theme.NotesSub, end)
	} else {
		x = writeString(dst, x, g.Search.Y, truncateLeft(v.Query, g.Search.Cols-9), withBold(theme.Notes), end)
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
	}

	listEnd := g.List.X + g.List.Cols
	switch {
	case v.Loading:
		writeString(dst, g.List.X, g.List.Y, "reading the sessions…", theme.NotesSub, listEnd)
	case len(v.Shown) == 0 && v.Total == 0:
		writeString(dst, g.List.X, g.List.Y, "no sessions on this machine yet", theme.NotesSub, listEnd)
	case len(v.Shown) == 0:
		writeString(dst, g.List.X, g.List.Y, "nothing matches the search", theme.NotesSub, listEnd)
	}
	// The columns from the right: size, age, project; the title takes the
	// rest.
	const sizeW, ageW = 6, 6
	projW := min(28, g.List.Cols/4)
	titleX := g.List.X + 4
	titleW := max(g.List.Cols-4-projW-ageW-sizeW-3, 8)
	top := clampSessionsScroll(v, g)
	for line := 0; line < g.List.Rows && top+line < len(v.Shown); line++ {
		i := top + line
		e := v.Shown[i]
		y := g.List.Y + line
		base, sub, accent := theme.Notes, theme.NotesSub, theme.NotesAccent
		if i == v.Cursor {
			base = theme.NotesButton
			sub, accent = base, base
			for x := g.List.X; x < listEnd; x++ {
				setCell(dst, x, y, ' ', base)
			}
		}
		if v.Marked[e.ID] {
			writeString(dst, g.List.X, y, "✓", withBold(accent), listEnd)
		}
		name := e.Title
		style := base
		switch {
		case e.Pane != 0:
			writeString(dst, g.List.X+2, y, "●", accent, listEnd)
		case e.Unreachable != "":
			// Dimmed, as a disabled tool is: there, and not to be opened.
			writeString(dst, g.List.X+2, y, "✗", sub, listEnd)
			style = sub
		}
		if name == "" {
			name, style = "(empty)", sub
		}
		writeString(dst, titleX, y, truncate(name, titleW), style, listEnd)
		px := titleX + titleW + 1
		writeString(dst, px, y, truncateLeft(sessionProject(e.Dir), projW), sub, listEnd)
		age := SessionAge(v.Now, e.Modified)
		writeString(dst, px+projW+1+ageW-runewidth.StringWidth(age), y, age, sub, listEnd)
		size := SessionSize(e.Size)
		writeString(dst, listEnd-runewidth.StringWidth(size), y, size, sub, listEnd)
	}
	if len(v.Shown) > g.List.Rows && g.List.Rows > 0 {
		more := fmt.Sprintf("%d–%d of %d", top+1, min(top+g.List.Rows, len(v.Shown)), len(v.Shown))
		writeString(dst, right-1-runewidth.StringWidth(more), g.List.Y+g.List.Rows, more, theme.NotesSub, right)
	}

	msgY, hintY := box.Y+box.Rows-4, box.Y+box.Rows-3
	if v.Confirm {
		n := len(v.Marked)
		if n == 0 && v.Cursor < len(v.Shown) {
			n = 1
		}
		writeString(dst, box.X+2, msgY, truncate(fmt.Sprintf("delete %d session(s) for good? the conversation cannot be resumed after", n), box.Cols-4), withBold(theme.Notes), right)
		writeString(dst, box.X+2, hintY, "enter deletes · esc cancels", theme.NotesSub, right)
	} else {
		if v.Message != "" {
			writeString(dst, box.X+2, msgY, truncate(v.Message, box.Cols-4), theme.NotesSub, right)
		}
		writeString(dst, box.X+2, hintY, truncate("type to search · ↑↓ move · enter resume · ^g continue in… · tab mark · ^a mark all · ^o mark 30+ days · ^d delete · esc", box.Cols-4), theme.NotesSub, right)
	}
	for i, r := range []Rect{g.Resume, g.Delete, g.Close} {
		writeString(dst, r.X, r.Y, sessionsButtons[i], theme.NotesAccent, right)
	}
	if g.List.Rows > 0 {
		legend := "● open in a pane · ✗ cannot be opened here"
		writeString(dst, g.Close.X-2-runewidth.StringWidth(legend), g.Close.Y, legend, theme.NotesSub, g.Close.X)
	}
}
