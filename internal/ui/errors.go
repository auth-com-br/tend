package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The errors panel is tend's own: the errors a GlitchTip server collected
// from the systems that report to it, a project, a status and a search over
// them, and one error whole — its exception, the stack with the
// application's own frames marked, the request, where it ran and what
// happened before — with a way to hand it to an agent to fix. It is drawn
// as the issues panel is. A panel with no server connected says so and
// offers to connect one, in a second, smaller box.

// ErrorEntry is one error, grouped, as the list shows it.
type ErrorEntry struct {
	ID, ShortID, Org, Project string
	Title, Culprit            string
	Level, Status             string
	Count, Users              int
	FirstSeen, LastSeen       time.Time
	URL                       string
}

// ErrorFrame is one line of a stack.
type ErrorFrame struct {
	File     string
	Line     int
	Function string
	InApp    bool
	Code     string
}

// ErrorException is one exception of the chain.
type ErrorException struct {
	Type, Value string
	Frames      []ErrorFrame
}

// ErrorDetailView is one error, over the list.
type ErrorDetailView struct {
	ErrorEntry
	Loading    bool
	Exceptions []ErrorException
	Request    string
	Release    string
	Tags       [][2]string
	Crumbs     []string
	Scroll     int
}

// ErrorsConnect is the box a server and its token are given in.
type ErrorsConnect struct {
	URL, Token string
	// InToken is typing going to the token rather than the address.
	InToken bool
	Testing bool
	Error   string
}

// ErrorsManage is the box the servers are kept in: each one's address,
// the one shown, and adding or removing one.
type ErrorsManage struct {
	Servers []string
	Cursor  int
	Active  int
	// Confirm is while a remove waits for its answer.
	Confirm bool
	Message string
}

// ErrorsView is the panel while it is up.
type ErrorsView struct {
	// Connected is a server and a token set; Server is its address.
	Connected bool
	Server    string
	// Sources are the servers kept, by address, and Source the one shown;
	// a line of chips when there is more than one.
	Sources []string
	Source  int
	// Manage is the servers box, when it is up.
	Manage *ErrorsManage
	// Folder is the project folder the panel was opened in, by its last
	// name, and Linked whether it is tied to the server and project shown;
	// the title offers to tie it, or says it is.
	Folder string
	Linked bool
	// Scopes are "all" and each project, Scope the one listed.
	Scopes  []string
	Scope   int
	Filters []string
	Filter  int
	Query   string
	Errors  []ErrorEntry
	Cursor  int
	Scroll  int
	Loading bool
	Error   string
	Message string
	Now     time.Time
	Detail  *ErrorDetailView
	Connect *ErrorsConnect
}

// Errors panel buttons.
const (
	ErrorsOpen     = "open"
	ErrorsBack     = "back"
	ErrorsFix      = "fix"
	ErrorsWork     = "work"
	ErrorsResolve  = "resolve"
	ErrorsIgnore   = "ignore"
	ErrorsReopen   = "reopen"
	ErrorsBrowser  = "browser"
	ErrorsSettings = "settings"
	ErrorsClose    = "close"
	ErrorsConnectB = "connect"
	ErrorsSave     = "save"
	ErrorsCancel   = "cancel"
	// The servers box's buttons.
	ErrorsUse       = "use"
	ErrorsAdd       = "add"
	ErrorsRemove    = "remove"
	ErrorsManageEnd = "manage-close"
	// ErrorsLink ties the project folder to what is shown, or unties it.
	ErrorsLink = "link"
)

// ErrorsGeometry is where the panel's parts are.
type ErrorsGeometry struct {
	Box         Rect
	SourceChips []Rect
	ScopeChips  []Rect
	Filters     []Rect
	Search      Rect
	List        Rect
	Body        Rect
	Buttons     []IssueButton
	// Settings is the gear on the title line, and Link what is said there
	// about the project folder.
	Settings Rect
	Link     Rect
	// ConnectBox is the connect box, when it is up, with its fields and
	// buttons.
	ConnectBox     Rect
	URLField       Rect
	TokenField     Rect
	ConnectButtons []IssueButton
	// ManageBox is the servers box, with a line for each server.
	ManageBox     Rect
	ManageList    Rect
	ManageButtons []IssueButton
}

func errorButtons(v *ErrorsView) []IssueButton {
	switch {
	case !v.Connected:
		return []IssueButton{{ID: ErrorsConnectB, Label: "[ Connect ]"}, {ID: ErrorsClose, Label: "[ Close ]"}}
	case v.Detail != nil:
		state := IssueButton{ID: ErrorsResolve, Label: "[ Resolve ]"}
		if v.Detail.Status != "unresolved" {
			state = IssueButton{ID: ErrorsReopen, Label: "[ Reopen ]"}
		}
		return []IssueButton{
			{ID: ErrorsBack, Label: "[ Back ]"},
			{ID: ErrorsFix, Label: "[ Fix with agent ]"},
			{ID: ErrorsWork, Label: "[ Fix in worktree ]"},
			state,
			{ID: ErrorsIgnore, Label: "[ Ignore ]"},
			{ID: ErrorsBrowser, Label: "[ In browser ]"},
			{ID: ErrorsClose, Label: "[ Close ]"},
		}
	}
	// From the list too: an error already fixed is resolved without
	// opening it. A resolved or ignored one is opened again the same way.
	out := []IssueButton{{ID: ErrorsOpen, Label: "[ Open ]"}, {ID: ErrorsFix, Label: "[ Fix with agent ]"}}
	if v.Filter == 0 {
		out = append(out, IssueButton{ID: ErrorsResolve, Label: "[ Resolve ]"}, IssueButton{ID: ErrorsIgnore, Label: "[ Ignore ]"})
	} else {
		out = append(out, IssueButton{ID: ErrorsReopen, Label: "[ Reopen ]"})
	}
	return append(out, IssueButton{ID: ErrorsBrowser, Label: "[ In browser ]"}, IssueButton{ID: ErrorsClose, Label: "[ Close ]"})
}

// ErrorsLayout is the panel's geometry on a screen of cols by rows.
func ErrorsLayout(v *ErrorsView, cols, rows int) ErrorsGeometry {
	w, h := max(min(sessionsCols, cols-2), 0), max(min(sessionsRows, rows-2), 0)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := ErrorsGeometry{Box: box}
	g.Settings = Rect{X: box.X + box.Cols - 6, Y: box.Y + 1, Cols: 3, Rows: 1}
	if label := errorsLinkLabel(v); label != "" {
		w := runewidth.StringWidth(label)
		g.Link = Rect{X: box.X + 2 + min(runewidth.StringWidth(errorsTitle(v))+2, max(box.Cols-w-20, 0)), Y: box.Y + 1, Cols: w, Rows: 1}
	}
	// A line of chips for the servers when there is more than one, then
	// one for the projects: each pushes what is under it down a line.
	off := 0
	chips := func(label string, names []string) []Rect {
		var out []Rect
		sx := box.X + 2 + runewidth.StringWidth(label+" ")
		for _, name := range names {
			r := Rect{X: sx, Y: box.Y + 2 + off, Cols: runewidth.StringWidth(" " + name + " "), Rows: 1}
			out = append(out, r)
			sx += r.Cols + 1
		}
		off++
		return out
	}
	if len(v.Sources) > 1 && v.Detail == nil {
		g.SourceChips = chips("server ", v.Sources)
	}
	if len(v.Scopes) > 0 && v.Detail == nil {
		g.ScopeChips = chips("project", v.Scopes)
	}
	x := box.X + 2
	for _, name := range v.Filters {
		r := Rect{X: x, Y: box.Y + 2 + off, Cols: runewidth.StringWidth(" " + name + " "), Rows: 1}
		g.Filters = append(g.Filters, r)
		x += r.Cols + 1
	}
	g.Search = Rect{X: box.X + 2, Y: box.Y + 3 + off, Cols: max(box.Cols-4, 0), Rows: 1}
	g.List = Rect{X: box.X + 2, Y: box.Y + 6 + off, Cols: max(box.Cols-4, 0), Rows: max(box.Rows-11-off, 0)}
	g.Body = Rect{X: box.X + 2, Y: box.Y + 6, Cols: max(box.Cols-5, 0), Rows: max(box.Rows-11, 0)}
	bottom, bx := box.Y+box.Rows-2, box.X+2
	for _, b := range errorButtons(v) {
		b.Rect = Rect{X: bx, Y: bottom, Cols: runewidth.StringWidth(b.Label), Rows: 1}
		if b.ID == ErrorsClose {
			b.X = box.X + box.Cols - 2 - b.Cols
		} else {
			bx += b.Cols + 1
		}
		g.Buttons = append(g.Buttons, b)
	}
	if m := v.Manage; m != nil {
		mw, mh := min(64, cols-4), min(len(m.Servers)+9, rows-4)
		mb := Rect{X: (cols - mw) / 2, Y: (rows - mh) / 2, Cols: max(mw, 0), Rows: max(mh, 0)}
		g.ManageBox = mb
		g.ManageList = Rect{X: mb.X + 2, Y: mb.Y + 3, Cols: max(mb.Cols-4, 0), Rows: max(mb.Rows-8, 0)}
		mx := mb.X + 2
		for _, b := range []IssueButton{{ID: ErrorsUse, Label: "[ Use ]"}, {ID: ErrorsAdd, Label: "[ Add ]"}, {ID: ErrorsRemove, Label: "[ Remove ]"}, {ID: ErrorsManageEnd, Label: "[ Close ]"}} {
			b.Rect = Rect{X: mx, Y: mb.Y + mb.Rows - 2, Cols: runewidth.StringWidth(b.Label), Rows: 1}
			if b.ID == ErrorsManageEnd {
				b.X = mb.X + mb.Cols - 2 - b.Cols
			} else {
				mx += b.Cols + 1
			}
			g.ManageButtons = append(g.ManageButtons, b)
		}
	}
	if v.Connect != nil {
		cw, ch := min(64, cols-4), 11
		c := Rect{X: (cols - cw) / 2, Y: (rows - ch) / 2, Cols: max(cw, 0), Rows: ch}
		g.ConnectBox = c
		g.URLField = Rect{X: c.X + 10, Y: c.Y + 3, Cols: max(c.Cols-13, 0), Rows: 1}
		g.TokenField = Rect{X: c.X + 10, Y: c.Y + 5, Cols: max(c.Cols-13, 0), Rows: 1}
		cx := c.X + 2
		for _, b := range []IssueButton{{ID: ErrorsSave, Label: "[ Test and save ]"}, {ID: ErrorsCancel, Label: "[ Cancel ]"}} {
			b.Rect = Rect{X: cx, Y: c.Y + c.Rows - 2, Cols: runewidth.StringWidth(b.Label), Rows: 1}
			cx += b.Cols + 1
			g.ConnectButtons = append(g.ConnectButtons, b)
		}
	}
	return g
}

// ErrorsAt is what a click at a point is on: a button's ID, "source:N",
// "scope:N", "filter:N", "row:N", the connect box's parts ("url",
// "token"), or a line of the servers box ("server:N").
func ErrorsAt(v *ErrorsView, cols, rows, x, y int) (string, bool) {
	g := ErrorsLayout(v, cols, rows)
	in := func(r Rect) bool { return y >= r.Y && y < r.Y+r.Rows && x >= r.X && x < r.X+r.Cols }
	if v.Manage != nil && v.Connect == nil {
		if OnCloseMark(g.ManageBox, x, y) {
			return ErrorsManageEnd, true
		}
		for _, b := range g.ManageButtons {
			if in(b.Rect) {
				return b.ID, true
			}
		}
		if in(g.ManageList) {
			if i := y - g.ManageList.Y; i < len(v.Manage.Servers) {
				return fmt.Sprintf("server:%d", i), true
			}
		}
		return "", false
	}
	if v.Connect != nil {
		if OnCloseMark(g.ConnectBox, x, y) {
			return ErrorsCancel, true
		}
		for _, b := range g.ConnectButtons {
			if in(b.Rect) {
				return b.ID, true
			}
		}
		switch {
		case in(g.URLField):
			return "url", true
		case in(g.TokenField):
			return "token", true
		}
		return "", false
	}
	if OnCloseMark(g.Box, x, y) {
		return ErrorsClose, true
	}
	for _, b := range g.Buttons {
		if in(b.Rect) {
			return b.ID, true
		}
	}
	if v.Connected && in(g.Settings) {
		return ErrorsSettings, true
	}
	if g.Link.Cols > 0 && in(g.Link) {
		return ErrorsLink, true
	}
	if v.Detail != nil {
		return "", false
	}
	for i, r := range g.SourceChips {
		if in(r) {
			return fmt.Sprintf("source:%d", i), true
		}
	}
	for i, r := range g.ScopeChips {
		if in(r) {
			return fmt.Sprintf("scope:%d", i), true
		}
	}
	for i, r := range g.Filters {
		if in(r) {
			return fmt.Sprintf("filter:%d", i), true
		}
	}
	if in(g.List) {
		if i := clampErrorsScroll(v, g) + y - g.List.Y; i < len(v.Errors) {
			return fmt.Sprintf("row:%d", i), true
		}
	}
	return "", false
}

// ErrorsScrollFor is the scroll that keeps the cursor in view.
func ErrorsScrollFor(v *ErrorsView, cols, rows int) int {
	g := ErrorsLayout(v, cols, rows)
	top := clampErrorsScroll(v, g)
	if v.Cursor < top {
		top = v.Cursor
	}
	if g.List.Rows > 0 && v.Cursor >= top+g.List.Rows {
		top = v.Cursor - g.List.Rows + 1
	}
	return max(top, 0)
}

func clampErrorsScroll(v *ErrorsView, g ErrorsGeometry) int {
	return max(min(v.Scroll, len(v.Errors)-g.List.Rows), 0)
}

// ClampErrorScroll is the detail's scroll as it will be drawn.
func ClampErrorScroll(v *ErrorsView, cols, rows int, theme Theme) int {
	if v.Detail == nil {
		return 0
	}
	g := ErrorsLayout(v, cols, rows)
	return max(min(v.Detail.Scroll, len(errorBody(v, g.Body.Cols, theme))-g.Body.Rows), 0)
}

// levelStyle is how a level reads: fatal bold, error in the accent,
// warning plain, the rest dim.
func levelStyle(level string, theme Theme) vt.Style {
	switch level {
	case "fatal":
		return withBold(theme.NotesAccent)
	case "error":
		return theme.NotesAccent
	case "warning":
		return theme.Notes
	}
	return theme.NotesSub
}

// errorBody is the detail's scrolling part: each exception with its stack,
// the application's frames in the text's colour with their code under
// them and the libraries' dimmed; the request; where it ran; and the last
// breadcrumbs.
func errorBody(v *ErrorsView, width int, theme Theme) []notesLine {
	d := v.Detail
	var out []notesLine
	line := func(spans ...notesSpan) { out = append(out, notesLine{spans: spans, fill: theme.Notes}) }
	blank := func() { out = append(out, notesLine{fill: theme.Notes}) }
	for _, ex := range d.Exceptions {
		line(notesSpan{" " + ex.Type + ": ", withBold(theme.NotesAccent)}, notesSpan{truncate(ex.Value, max(width-runewidth.StringWidth(ex.Type)-4, 10)), withBold(theme.Notes)})
		for n := len(ex.Frames) - 1; n >= 0; n-- {
			f := ex.Frames[n]
			where := fmt.Sprintf("%s:%d", f.File, f.Line)
			if !f.InApp {
				line(notesSpan{"   at " + f.Function + "  " + truncateLeft(where, max(width-len(f.Function)-8, 10)), theme.NotesSub})
				continue
			}
			line(notesSpan{" ▸ at ", theme.NotesAccent}, notesSpan{f.Function + "  ", withBold(theme.Notes)}, notesSpan{truncateLeft(where, max(width-len(f.Function)-8, 10)), theme.Notes})
			if f.Code != "" {
				line(notesSpan{"      " + truncate(f.Code, width-8), theme.NotesCode})
			}
		}
		blank()
	}
	if d.Request != "" {
		line(notesSpan{" REQUEST  ", theme.NotesAccent}, notesSpan{truncate(d.Request, width-12), theme.Notes})
	}
	if d.Release != "" {
		line(notesSpan{" RELEASE  ", theme.NotesAccent}, notesSpan{d.Release, theme.Notes})
	}
	if len(d.Tags) > 0 {
		var tags []string
		for _, t := range d.Tags {
			tags = append(tags, t[0]+"="+t[1])
		}
		out = append(out, wrapNotes(notesLine{spans: []notesSpan{{" TAGS  ", theme.NotesAccent}, {strings.Join(tags, "  "), theme.NotesSub}}, fill: theme.Notes}, width)...)
	}
	if len(d.Crumbs) > 0 {
		blank()
		line(notesSpan{" BEFORE IT", theme.NotesAccent})
		from := max(len(d.Crumbs)-12, 0)
		for _, c := range d.Crumbs[from:] {
			line(notesSpan{"   " + truncate(c, width-4), theme.NotesSub})
		}
	}
	return out
}

func drawErrors(dst *vt.Grid, v *ErrorsView, theme Theme) {
	g := ErrorsLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 14 || box.Cols < 50 {
		return
	}
	right := box.X + box.Cols - 1
	if v.Connected {
		writeString(dst, g.Settings.X, g.Settings.Y, " ⚙ ", theme.NotesAccent, right)
	}
	writeString(dst, box.X+2, box.Y+1, truncate(errorsTitle(v), box.Cols-12), withBold(theme.NotesAccent), right)
	if g.Link.Cols > 0 {
		style := theme.NotesSub
		if v.Linked {
			style = theme.NotesAccent
		}
		writeString(dst, g.Link.X, g.Link.Y, errorsLinkLabel(v), style, right)
	}

	switch {
	case !v.Connected:
		msg := []string{"No GlitchTip or Sentry server is connected.", "",
			"Connect a server and an API token — in GlitchTip, Profile → Auth Tokens;",
			"in Sentry (https://sentry.io), User Settings → Personal Tokens —",
			"to see the errors your systems report and hand them to an agent to fix."}
		for i, m := range msg {
			style := theme.NotesSub
			if i == 0 {
				style = withBold(theme.Notes)
			}
			writeString(dst, box.X+4, box.Y+4+i, m, style, right)
		}
	case v.Detail != nil:
		drawErrorDetail(dst, v, g, theme)
	default:
		drawErrorList(dst, v, g, theme)
	}
	msgY := box.Y + box.Rows - 4
	msg := v.Message
	if v.Loading {
		msg = "asking GlitchTip…"
	}
	if msg != "" {
		writeString(dst, box.X+2, msgY, truncate(msg, box.Cols-4), theme.NotesSub, right)
	}
	hint := "type to search · tab status · ctrl+t project · ctrl+g server · ctrl+l link folder · ↑↓ move · enter open · ctrl+x resolve · ctrl+f fix with agent · ctrl+o browser · ctrl+r reload · esc"
	switch {
	case !v.Connected:
		hint = "enter connect · esc close"
	case v.Detail != nil:
		hint = "↑↓ scroll · f fix with agent · w fix in worktree · r resolve · i ignore · u reopen · c to context · o browser · esc back"
	}
	writeString(dst, box.X+2, box.Y+box.Rows-3, truncate(hint, box.Cols-4), theme.NotesSub, right)
	for _, b := range g.Buttons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
	if v.Manage != nil {
		drawErrorsManage(dst, v.Manage, g, theme)
	}
	if v.Connect != nil {
		drawErrorsConnect(dst, v.Connect, g, theme)
	}
}

// errorsTitle is the title line's words.
func errorsTitle(v *ErrorsView) string {
	if !v.Connected {
		return "ERRORS"
	}
	return "ERRORS · " + strings.TrimPrefix(strings.TrimPrefix(v.Server, "https://"), "http://")
}

// errorsLinkLabel is what the title says about the project folder: that
// it opens here, or an offer to make it — nothing over an open error, or
// with no folder or no server.
func errorsLinkLabel(v *ErrorsView) string {
	switch {
	case !v.Connected || v.Folder == "" || v.Detail != nil:
		return ""
	case v.Linked:
		return "⇄ " + v.Folder + " opens here"
	}
	return "[ link " + v.Folder + " here ]"
}

// drawErrorsManage draws the servers box: each server kept, the one shown
// marked, and what to do with them.
func drawErrorsManage(dst *vt.Grid, m *ErrorsManage, g ErrorsGeometry, theme Theme) {
	box := g.ManageBox
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	right := box.X + box.Cols - 1
	writeString(dst, box.X+2, box.Y, " GlitchTip servers ", withBold(theme.NotesAccent), right)
	writeString(dst, box.X+2, box.Y+1, truncate("the servers kept; the panel shows one at a time", box.Cols-4), theme.NotesSub, right)
	end := g.ManageList.X + g.ManageList.Cols
	for i, name := range m.Servers {
		if i >= g.ManageList.Rows {
			break
		}
		y := g.ManageList.Y + i
		base, accent := theme.Notes, theme.NotesAccent
		if i == m.Cursor {
			base, accent = theme.NotesButton, theme.NotesButton
			for x := g.ManageList.X; x < end; x++ {
				setCell(dst, x, y, ' ', base)
			}
		}
		if i == m.Active {
			writeString(dst, g.ManageList.X, y, "●", withBold(accent), end)
		}
		writeString(dst, g.ManageList.X+2, y, truncate(name, g.ManageList.Cols-3), base, end)
	}
	msg, style := "↑↓ move · enter use · a add · d remove · esc close", theme.NotesSub
	switch {
	case m.Confirm && m.Cursor < len(m.Servers):
		msg, style = "remove "+m.Servers[m.Cursor]+"? its token is forgotten · enter removes · esc keeps it", withBold(theme.Notes)
	case m.Message != "":
		msg = m.Message
	}
	writeString(dst, box.X+2, box.Y+box.Rows-3, truncate(msg, box.Cols-4), style, right)
	for _, b := range g.ManageButtons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}

func drawErrorList(dst *vt.Grid, v *ErrorsView, g ErrorsGeometry, theme Theme) {
	box := g.Box
	right := box.X + box.Cols - 1
	if len(v.Errors) > 0 {
		count := fmt.Sprintf("%d", len(v.Errors))
		writeString(dst, right-7-runewidth.StringWidth(count), box.Y+1, count, theme.NotesSub, right)
	}
	if len(g.SourceChips) > 0 {
		writeString(dst, box.X+2, g.SourceChips[0].Y, "server", theme.NotesSub, right)
		for i, r := range g.SourceChips {
			style := theme.NotesSub
			if i == v.Source {
				style = theme.NotesButton
			}
			writeString(dst, r.X, r.Y, " "+v.Sources[i]+" ", style, right)
		}
	}
	if len(g.ScopeChips) > 0 {
		writeString(dst, box.X+2, g.ScopeChips[0].Y, "project", theme.NotesSub, right)
		for i, r := range g.ScopeChips {
			style := theme.NotesSub
			if i == v.Scope {
				style = theme.NotesButton
			}
			writeString(dst, r.X, r.Y, " "+v.Scopes[i]+" ", style, right)
		}
	}
	for i, r := range g.Filters {
		style := theme.NotesSub
		if i == v.Filter {
			style = theme.NotesButton
		}
		writeString(dst, r.X, r.Y, " "+v.Filters[i]+" ", style, right)
	}
	end := g.Search.X + g.Search.Cols
	x := writeString(dst, g.Search.X, g.Search.Y, "search ", theme.NotesSub, end)
	if v.Query == "" {
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
		writeString(dst, x+2, g.Search.Y, "words in the error, or GlitchTip's own search", theme.NotesSub, end)
	} else {
		x = writeString(dst, x, g.Search.Y, truncateLeft(v.Query, g.Search.Cols-9), withBold(theme.Notes), end)
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
	}

	listEnd := g.List.X + g.List.Cols
	const lvlW, idW, projW, countW, usersW, ageW = 2, 26, 22, 7, 6, 5
	titleW := max(g.List.Cols-lvlW-idW-projW-countW-usersW-ageW-6, 10)
	cols := []int{g.List.X, g.List.X + lvlW}
	for _, w := range []int{idW, titleW, projW, countW, usersW} {
		cols = append(cols, cols[len(cols)-1]+w+1)
	}
	for i, name := range []string{"", "id", "error", "project", "events", "users", "last"} {
		writeString(dst, cols[i], g.List.Y-1, name, theme.NotesSub, listEnd)
	}
	switch {
	case v.Error != "":
		writeString(dst, g.List.X, g.List.Y, truncate(v.Error, g.List.Cols), withBold(theme.Notes), listEnd)
	case v.Loading && len(v.Errors) == 0:
		writeString(dst, g.List.X, g.List.Y, "asking GlitchTip…", theme.NotesSub, listEnd)
	case len(v.Errors) == 0:
		writeString(dst, g.List.X, g.List.Y, "no errors here", theme.NotesSub, listEnd)
	}
	top := clampErrorsScroll(v, g)
	for line := 0; line < g.List.Rows && top+line < len(v.Errors) && v.Error == ""; line++ {
		i := top + line
		e := v.Errors[i]
		y := g.List.Y + line
		base, sub := theme.Notes, theme.NotesSub
		lvl := levelStyle(e.Level, theme)
		if i == v.Cursor {
			base, sub, lvl = theme.NotesButton, theme.NotesButton, theme.NotesButton
			for x := g.List.X; x < listEnd; x++ {
				setCell(dst, x, y, ' ', base)
			}
		}
		writeString(dst, cols[0], y, "●", lvl, cols[1])
		writeString(dst, cols[1], y, truncate(e.ShortID, idW), sub, cols[2]-1)
		writeString(dst, cols[2], y, truncate(e.Title, titleW), base, cols[3]-1)
		writeString(dst, cols[3], y, truncate(e.Project, projW), sub, cols[4]-1)
		n := fmt.Sprintf("%d", e.Count)
		writeString(dst, cols[5]-1-runewidth.StringWidth(n), y, n, base, cols[5])
		u := fmt.Sprintf("%d", e.Users)
		writeString(dst, cols[6]-1-runewidth.StringWidth(u), y, u, sub, cols[6])
		writeString(dst, cols[6]+1, y, SessionAge(v.Now, e.LastSeen), sub, listEnd)
	}
}

func drawErrorDetail(dst *vt.Grid, v *ErrorsView, g ErrorsGeometry, theme Theme) {
	d := v.Detail
	box := g.Box
	right := box.X + box.Cols - 1
	x := writeString(dst, box.X+2, box.Y+2, d.Project+" ", theme.NotesSub, right)
	x = writeString(dst, x, box.Y+2, d.ShortID+" ", withBold(theme.NotesAccent), right)
	x = writeString(dst, x, box.Y+2, d.Level, levelStyle(d.Level, theme), right)
	meta := fmt.Sprintf(" · %s · %d events · %d users · first %s ago · last %s ago",
		d.Status, d.Count, d.Users, SessionAge(v.Now, d.FirstSeen), SessionAge(v.Now, d.LastSeen))
	writeString(dst, x, box.Y+2, truncate(meta, right-x-1), theme.NotesSub, right)
	writeString(dst, box.X+2, box.Y+3, truncate(d.Title, box.Cols-4), withBold(theme.Notes), right)
	if d.Culprit != "" {
		writeString(dst, box.X+2, box.Y+4, truncate("in "+d.Culprit, box.Cols-4), theme.NotesSub, right)
	}
	if d.Loading {
		writeString(dst, g.Body.X, g.Body.Y, "reading its latest event…", theme.NotesSub, g.Body.X+g.Body.Cols)
		return
	}
	drawScrolled(dst, g.Body, errorBody(v, g.Body.Cols, theme), d.Scroll, theme)
}

// drawErrorsConnect draws the connect box: the address, the token masked,
// what went wrong, and its buttons.
func drawErrorsConnect(dst *vt.Grid, c *ErrorsConnect, g ErrorsGeometry, theme Theme) {
	box := g.ConnectBox
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	right := box.X + box.Cols - 1
	writeString(dst, box.X+2, box.Y, " connect GlitchTip or Sentry ", withBold(theme.NotesAccent), right)
	writeString(dst, box.X+2, box.Y+1, truncate("a server, and an API token from it (Sentry: https://sentry.io)", box.Cols-4), theme.NotesSub, right)
	field := func(r Rect, label, value string, on bool) {
		writeString(dst, box.X+2, r.Y, label, theme.NotesSub, right)
		style := theme.NotesButton
		if !on {
			style = theme.NotesFence
		}
		for x := r.X; x < r.X+r.Cols; x++ {
			setCell(dst, x, r.Y, ' ', style)
		}
		x := writeString(dst, r.X+1, r.Y, truncateLeft(value, r.Cols-3), style, r.X+r.Cols)
		if on {
			setCell(dst, x, r.Y, '▏', style)
		}
	}
	field(g.URLField, "server", c.URL, !c.InToken)
	field(g.TokenField, "token", strings.Repeat("•", min(len([]rune(c.Token)), 48)), c.InToken)
	msg, style := "tab next field · enter test and save · esc cancel", theme.NotesSub
	switch {
	case c.Testing:
		msg = "asking the server…"
	case c.Error != "":
		msg, style = c.Error, withBold(theme.Notes)
	}
	writeString(dst, box.X+2, box.Y+7, truncate(msg, box.Cols-4), style, right)
	for _, b := range g.ConnectButtons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}
