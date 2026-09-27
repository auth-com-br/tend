package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The tickets panel is tend's own (#9): the tickets of the trackers a team
// keeps its work in — Linear, Jira, ClickUp — as the errors panel shows
// what GlitchTip caught, with the same parts: accounts kept and a box to
// manage them, a scope and a preset over a search, one ticket whole with
// its comments, and what hands it to an agent. It is drawn as the errors
// panel is. What the keys do is the client's; this draws and says where a
// click landed.

// TicketEntry is one ticket as the list shows it.
type TicketEntry struct {
	Key, Title, State, Assignee, Scope string
	Done                               bool
	Updated                            time.Time
}

// TicketComment is one comment on a ticket.
type TicketComment struct {
	Author, Body string
	Created      time.Time
}

// TicketDetailView is one ticket, over the list.
type TicketDetailView struct {
	TicketEntry
	URL      string
	Body     string
	Comments []TicketComment
	Loading  bool
	Scroll   int
}

// TicketsConnect is the box an account is added in: which tracker, the
// site and the email for Jira, and the token.
type TicketsConnect struct {
	Kinds []string
	Kind  int
	URL   string
	Email string
	Token string
	// Field is the one typed into: 0 the tracker, 1 the site, 2 the email,
	// 3 the token.
	Field   int
	Testing bool
	Error   string
}

// TicketsInput is a comment being typed.
type TicketsInput struct {
	Text    string
	Sending bool
}

// TicketsView is the panel while it is up.
type TicketsView struct {
	Connected bool
	// Account is the one shown, by name; Accounts all of them, a line of
	// chips when there is more than one.
	Account  string
	Accounts []string
	Active   int
	Manage   *ErrorsManage
	// Scopes are "all" and each team, project or workspace.
	Scopes  []string
	Scope   int
	Filters []string
	Filter  int
	Query   string
	Tickets []TicketEntry
	Cursor  int
	Scroll  int
	Loading bool
	Error   string
	Message string
	Now     time.Time
	Detail  *TicketDetailView
	Connect *TicketsConnect
	Input   *TicketsInput
	// Confirm is while marking a ticket done waits for its answer.
	Confirm bool
	// Folder is the project folder the panel was opened in, and Linked
	// whether it opens on the account and scope shown.
	Folder string
	Linked bool
}

// Tickets panel buttons.
const (
	TicketsOpen    = "t-open"
	TicketsBack    = "t-back"
	TicketsFix     = "t-fix"
	TicketsWork    = "t-work"
	TicketsComment = "t-comment"
	TicketsDone    = "t-done"
	TicketsBrowser = "t-browser"
	TicketsClose   = "t-close"
	TicketsConnB   = "t-connect"
	TicketsSave    = "t-save"
	TicketsCancel  = "t-cancel"
	TicketsGear    = "t-gear"
	TicketsLink    = "t-link"
)

// TicketsGeometry is where the panel's parts are.
type TicketsGeometry struct {
	Box          Rect
	AccountChips []Rect
	ScopeChips   []Rect
	Filters      []Rect
	Search       Rect
	List         Rect
	Body         Rect
	Buttons      []IssueButton
	Gear, Link   Rect
	// ConnectBox is the connect box, with a rect for each of its fields
	// (Fields[0] the tracker chips) and its buttons; KindChips are the
	// trackers to choose among.
	ConnectBox     Rect
	Fields         [4]Rect
	KindChips      []Rect
	ConnectButtons []IssueButton
	ManageBox      Rect
	ManageList     Rect
	ManageButtons  []IssueButton
}

func ticketButtons(v *TicketsView) []IssueButton {
	switch {
	case !v.Connected:
		return []IssueButton{{ID: TicketsConnB, Label: "[ Connect ]"}, {ID: TicketsClose, Label: "[ Close ]"}}
	case v.Detail != nil:
		return []IssueButton{
			{ID: TicketsBack, Label: "[ Back ]"},
			{ID: TicketsFix, Label: "[ Fix with agent ]"},
			{ID: TicketsWork, Label: "[ Fix in worktree ]"},
			{ID: TicketsComment, Label: "[ Comment ]"},
			{ID: TicketsDone, Label: "[ Mark done ]"},
			{ID: TicketsBrowser, Label: "[ In browser ]"},
			{ID: TicketsClose, Label: "[ Close ]"},
		}
	}
	return []IssueButton{
		{ID: TicketsOpen, Label: "[ Open ]"},
		{ID: TicketsFix, Label: "[ Fix with agent ]"},
		{ID: TicketsDone, Label: "[ Mark done ]"},
		{ID: TicketsBrowser, Label: "[ In browser ]"},
		{ID: TicketsClose, Label: "[ Close ]"},
	}
}

func ticketsTitle(v *TicketsView) string {
	if !v.Connected {
		return "TICKETS"
	}
	return "TICKETS · " + v.Account
}

func ticketsLinkLabel(v *TicketsView) string {
	switch {
	case !v.Connected || v.Folder == "" || v.Detail != nil:
		return ""
	case v.Linked:
		return "⇄ " + v.Folder + " opens here"
	}
	return "[ link " + v.Folder + " here ]"
}

// TicketsLayout is the panel's geometry on a screen of cols by rows.
func TicketsLayout(v *TicketsView, cols, rows int) TicketsGeometry {
	w, h := max(min(sessionsCols, cols-2), 0), max(min(sessionsRows, rows-2), 0)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := TicketsGeometry{Box: box}
	g.Gear = Rect{X: box.X + box.Cols - 6, Y: box.Y + 1, Cols: 3, Rows: 1}
	if label := ticketsLinkLabel(v); label != "" {
		lw := runewidth.StringWidth(label)
		g.Link = Rect{X: box.X + 2 + min(runewidth.StringWidth(ticketsTitle(v))+2, max(box.Cols-lw-20, 0)), Y: box.Y + 1, Cols: lw, Rows: 1}
	}
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
	if len(v.Accounts) > 1 && v.Detail == nil {
		g.AccountChips = chips("account", v.Accounts)
	}
	if len(v.Scopes) > 0 && v.Detail == nil {
		g.ScopeChips = chips("scope  ", v.Scopes)
	}
	x := box.X + 2
	for _, name := range v.Filters {
		r := Rect{X: x, Y: box.Y + 2 + off, Cols: runewidth.StringWidth(" " + name + " "), Rows: 1}
		g.Filters = append(g.Filters, r)
		x += r.Cols + 1
	}
	g.Search = Rect{X: box.X + 2, Y: box.Y + 3 + off, Cols: max(box.Cols-4, 0), Rows: 1}
	g.List = Rect{X: box.X + 2, Y: box.Y + 6 + off, Cols: max(box.Cols-4, 0), Rows: max(box.Rows-11-off, 0)}
	g.Body = Rect{X: box.X + 2, Y: box.Y + 5, Cols: max(box.Cols-5, 0), Rows: max(box.Rows-10, 0)}
	bottom, bx := box.Y+box.Rows-2, box.X+2
	for _, b := range ticketButtons(v) {
		b.Rect = Rect{X: bx, Y: bottom, Cols: runewidth.StringWidth(b.Label), Rows: 1}
		if b.ID == TicketsClose {
			b.X = box.X + box.Cols - 2 - b.Cols
		} else {
			bx += b.Cols + 1
		}
		g.Buttons = append(g.Buttons, b)
	}
	if m := v.Manage; m != nil {
		g.ManageBox, g.ManageList, g.ManageButtons = manageLayout(m, cols, rows)
	}
	if c := v.Connect; c != nil {
		cw, ch := min(66, cols-4), 13
		cb := Rect{X: (cols - cw) / 2, Y: (rows - ch) / 2, Cols: max(cw, 0), Rows: ch}
		g.ConnectBox = cb
		kx := cb.X + 11
		for _, k := range c.Kinds {
			r := Rect{X: kx, Y: cb.Y + 3, Cols: runewidth.StringWidth(" " + k + " "), Rows: 1}
			g.KindChips = append(g.KindChips, r)
			kx += r.Cols + 1
		}
		g.Fields[0] = Rect{X: cb.X + 11, Y: cb.Y + 3, Cols: max(kx-cb.X-11, 0), Rows: 1}
		for i := 1; i < 4; i++ {
			g.Fields[i] = Rect{X: cb.X + 11, Y: cb.Y + 3 + i*2, Cols: max(cb.Cols-14, 0), Rows: 1}
		}
		cx := cb.X + 2
		for _, b := range []IssueButton{{ID: TicketsSave, Label: "[ Test and save ]"}, {ID: TicketsCancel, Label: "[ Cancel ]"}} {
			b.Rect = Rect{X: cx, Y: cb.Y + cb.Rows - 2, Cols: runewidth.StringWidth(b.Label), Rows: 1}
			cx += b.Cols + 1
			g.ConnectButtons = append(g.ConnectButtons, b)
		}
	}
	return g
}

// TicketsAt is what a click at a point is on: a button's ID, "account:N",
// "scope:N", "filter:N", "row:N", "kind:N" or "field:N" in the connect box,
// or "server:N" in the accounts box.
func TicketsAt(v *TicketsView, cols, rows, x, y int) (string, bool) {
	g := TicketsLayout(v, cols, rows)
	in := func(r Rect) bool { return y >= r.Y && y < r.Y+r.Rows && x >= r.X && x < r.X+r.Cols }
	if v.Connect != nil {
		if OnCloseMark(g.ConnectBox, x, y) {
			return TicketsCancel, true
		}
		for _, b := range g.ConnectButtons {
			if in(b.Rect) {
				return b.ID, true
			}
		}
		for i, r := range g.KindChips {
			if in(r) {
				return fmt.Sprintf("kind:%d", i), true
			}
		}
		for i, r := range g.Fields {
			if i > 0 && in(r) {
				return fmt.Sprintf("field:%d", i), true
			}
		}
		return "", false
	}
	if v.Manage != nil {
		return manageAt(v.Manage, cols, rows, x, y)
	}
	if OnCloseMark(g.Box, x, y) {
		return TicketsClose, true
	}
	for _, b := range g.Buttons {
		if in(b.Rect) {
			return b.ID, true
		}
	}
	if v.Connected && in(g.Gear) {
		return TicketsGear, true
	}
	if g.Link.Cols > 0 && in(g.Link) {
		return TicketsLink, true
	}
	if v.Detail != nil {
		return "", false
	}
	for i, r := range g.AccountChips {
		if in(r) {
			return fmt.Sprintf("account:%d", i), true
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
		if i := clampTicketsScroll(v, g) + y - g.List.Y; i < len(v.Tickets) {
			return fmt.Sprintf("row:%d", i), true
		}
	}
	return "", false
}

// TicketsScrollFor is the scroll that keeps the cursor in view.
func TicketsScrollFor(v *TicketsView, cols, rows int) int {
	g := TicketsLayout(v, cols, rows)
	top := clampTicketsScroll(v, g)
	if v.Cursor < top {
		top = v.Cursor
	}
	if g.List.Rows > 0 && v.Cursor >= top+g.List.Rows {
		top = v.Cursor - g.List.Rows + 1
	}
	return max(top, 0)
}

func clampTicketsScroll(v *TicketsView, g TicketsGeometry) int {
	return max(min(v.Scroll, len(v.Tickets)-g.List.Rows), 0)
}

// ticketBody is the open ticket's text and its comments, as markdown.
func ticketBody(d *TicketDetailView, width int, theme Theme) []notesLine {
	var b strings.Builder
	body := strings.TrimSpace(d.Body)
	if body == "" {
		body = "*(no description)*"
	}
	b.WriteString(body)
	if len(d.Comments) > 0 {
		fmt.Fprintf(&b, "\n\n### %d comment%s\n", len(d.Comments), plural(len(d.Comments)))
		for _, c := range d.Comments {
			when := ""
			if !c.Created.IsZero() {
				when = " · " + c.Created.Local().Format("2006-01-02 15:04")
			}
			fmt.Fprintf(&b, "\n**%s**%s\n\n%s\n", c.Author, when, strings.TrimSpace(c.Body))
		}
	}
	return markdownLines(b.String(), width, theme)
}

// ClampTicketScroll is the detail's scroll as it will be drawn.
func ClampTicketScroll(v *TicketsView, cols, rows int, theme Theme) int {
	if v.Detail == nil {
		return 0
	}
	g := TicketsLayout(v, cols, rows)
	return max(min(v.Detail.Scroll, len(ticketBody(v.Detail, g.Body.Cols, theme))-g.Body.Rows), 0)
}

func drawTickets(dst *vt.Grid, v *TicketsView, theme Theme) {
	g := TicketsLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		fillLine(dst, box.X, box.X+box.Cols, y, theme.Notes)
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 14 || box.Cols < 50 {
		return
	}
	right := box.X + box.Cols - 1
	if v.Connected {
		writeString(dst, g.Gear.X, g.Gear.Y, " ⚙ ", theme.NotesAccent, right)
	}
	writeString(dst, box.X+2, box.Y+1, truncate(ticketsTitle(v), box.Cols-12), withBold(theme.NotesAccent), right)
	if g.Link.Cols > 0 {
		style := theme.NotesSub
		if v.Linked {
			style = theme.NotesAccent
		}
		writeString(dst, g.Link.X, g.Link.Y, ticketsLinkLabel(v), style, right)
	}
	switch {
	case !v.Connected:
		msg := []string{"No tracker is connected.", "",
			"Connect Linear, Jira or ClickUp with a token from it to see your",
			"tickets here and hand them to an agent to work on."}
		for i, m := range msg {
			style := theme.NotesSub
			if i == 0 {
				style = withBold(theme.Notes)
			}
			writeString(dst, box.X+4, box.Y+4+i, m, style, right)
		}
	case v.Detail != nil:
		drawTicketDetail(dst, v, g, theme)
	default:
		drawTicketList(dst, v, g, theme)
	}
	msgY, hintY := box.Y+box.Rows-4, box.Y+box.Rows-3
	switch {
	case v.Input != nil:
		x := writeString(dst, box.X+2, msgY, "comment: ", theme.NotesSub, right)
		x = writeString(dst, x, msgY, truncateLeft(v.Input.Text, box.Cols-14), withBold(theme.Notes), right)
		setCell(dst, x, msgY, ' ', theme.NotesButton)
		hint := "enter sends · esc cancels"
		if v.Input.Sending {
			hint = "sending…"
		}
		writeString(dst, box.X+2, hintY, hint, theme.NotesSub, right)
	case v.Confirm:
		key := ""
		if d := v.Detail; d != nil {
			key = d.Key
		} else if v.Cursor < len(v.Tickets) {
			key = v.Tickets[v.Cursor].Key
		}
		writeString(dst, box.X+2, msgY, truncate("mark "+key+" done? it moves to the tracker's finished state", box.Cols-4), withBold(theme.Notes), right)
		writeString(dst, box.X+2, hintY, "enter marks it done · esc keeps it", theme.NotesSub, right)
	default:
		msg := v.Message
		if v.Loading {
			msg = "asking the tracker…"
		}
		if msg != "" {
			writeString(dst, box.X+2, msgY, truncate(msg, box.Cols-4), theme.NotesSub, right)
		}
		hint := "type to search · tab preset · ctrl+t scope · ctrl+g account · ctrl+l link folder · ↑↓ move · enter open · ctrl+x done · ctrl+f fix · esc"
		switch {
		case !v.Connected:
			hint = "enter connect · esc close"
		case v.Detail != nil:
			hint = "↑↓ scroll · f fix with agent · w fix in worktree · c comment · x done · o browser · esc back"
		}
		writeString(dst, box.X+2, hintY, truncate(hint, box.Cols-4), theme.NotesSub, right)
	}
	for _, b := range g.Buttons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
	if v.Manage != nil {
		drawManage(dst, v.Manage, g.ManageBox, g.ManageList, g.ManageButtons, theme)
	}
	if v.Connect != nil {
		drawTicketsConnect(dst, v.Connect, g, theme)
	}
}

func drawTicketList(dst *vt.Grid, v *TicketsView, g TicketsGeometry, theme Theme) {
	box := g.Box
	right := box.X + box.Cols - 1
	if len(v.Tickets) > 0 {
		count := fmt.Sprintf("%d", len(v.Tickets))
		writeString(dst, right-7-runewidth.StringWidth(count), box.Y+1, count, theme.NotesSub, right)
	}
	chipRow := func(label string, rects []Rect, names []string, at int) {
		if len(rects) == 0 {
			return
		}
		writeString(dst, box.X+2, rects[0].Y, label, theme.NotesSub, right)
		for i, r := range rects {
			style := theme.NotesSub
			if i == at {
				style = theme.NotesButton
			}
			writeString(dst, r.X, r.Y, " "+names[i]+" ", style, right)
		}
	}
	chipRow("account", g.AccountChips, v.Accounts, v.Active)
	chipRow("scope", g.ScopeChips, v.Scopes, v.Scope)
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
		writeString(dst, x+2, g.Search.Y, "words in the title", theme.NotesSub, end)
	} else {
		x = writeString(dst, x, g.Search.Y, truncateLeft(v.Query, g.Search.Cols-9), withBold(theme.Notes), end)
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
	}
	listEnd := g.List.X + g.List.Cols
	const keyW, stateW, whoW, ageW = 14, 16, 16, 5
	titleW := max(g.List.Cols-keyW-stateW-whoW-ageW-4, 10)
	cols := []int{g.List.X, g.List.X + keyW + 1}
	cols = append(cols, cols[1]+titleW+1)
	cols = append(cols, cols[2]+stateW+1)
	cols = append(cols, cols[3]+whoW+1)
	for i, name := range []string{"ticket", "title", "state", "assignee", "last"} {
		writeString(dst, cols[i], g.List.Y-1, name, theme.NotesSub, listEnd)
	}
	switch {
	case v.Error != "":
		writeString(dst, g.List.X, g.List.Y, truncate(v.Error, g.List.Cols), withBold(theme.Notes), listEnd)
	case v.Loading && len(v.Tickets) == 0:
		writeString(dst, g.List.X, g.List.Y, "asking the tracker…", theme.NotesSub, listEnd)
	case len(v.Tickets) == 0:
		writeString(dst, g.List.X, g.List.Y, "no tickets here", theme.NotesSub, listEnd)
	}
	top := clampTicketsScroll(v, g)
	for line := 0; line < g.List.Rows && top+line < len(v.Tickets) && v.Error == ""; line++ {
		i := top + line
		t := v.Tickets[i]
		y := g.List.Y + line
		base, sub := theme.Notes, theme.NotesSub
		if i == v.Cursor {
			base, sub = theme.NotesButton, theme.NotesButton
			fillLine(dst, g.List.X, listEnd, y, base)
		}
		stateStyle := sub
		if !t.Done && i != v.Cursor {
			stateStyle = theme.NotesAccent
		}
		writeString(dst, cols[0], y, truncate(t.Key, keyW), sub, cols[1]-1)
		writeString(dst, cols[1], y, truncate(t.Title, titleW), base, cols[2]-1)
		writeString(dst, cols[2], y, truncate(t.State, stateW), stateStyle, cols[3]-1)
		writeString(dst, cols[3], y, truncate(t.Assignee, whoW), sub, cols[4]-1)
		if !t.Updated.IsZero() {
			writeString(dst, cols[4], y, SessionAge(v.Now, t.Updated), sub, listEnd)
		}
	}
}

func drawTicketDetail(dst *vt.Grid, v *TicketsView, g TicketsGeometry, theme Theme) {
	d := v.Detail
	box := g.Box
	right := box.X + box.Cols - 1
	x := writeString(dst, box.X+2, box.Y+2, d.Key+" ", withBold(theme.NotesAccent), right)
	meta := d.State
	if d.Assignee != "" {
		meta += " · " + d.Assignee
	}
	if d.Scope != "" {
		meta += " · " + d.Scope
	}
	writeString(dst, x, box.Y+2, truncate(meta, right-x-1), theme.NotesSub, right)
	writeString(dst, box.X+2, box.Y+3, truncate(d.Title, box.Cols-4), withBold(theme.Notes), right)
	if d.Loading {
		writeString(dst, g.Body.X, g.Body.Y, "reading the ticket…", theme.NotesSub, g.Body.X+g.Body.Cols)
		return
	}
	drawScrolled(dst, g.Body, ticketBody(d, g.Body.Cols, theme), d.Scroll, theme)
}

// drawTicketsConnect draws the connect box: the tracker, the site and the
// email for Jira, the token masked, what went wrong, and its buttons.
func drawTicketsConnect(dst *vt.Grid, c *TicketsConnect, g TicketsGeometry, theme Theme) {
	box := g.ConnectBox
	for y := box.Y; y < box.Y+box.Rows; y++ {
		fillLine(dst, box.X, box.X+box.Cols, y, theme.Notes)
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	right := box.X + box.Cols - 1
	writeString(dst, box.X+2, box.Y, " connect a tracker ", withBold(theme.NotesAccent), right)
	kind := ""
	if c.Kind < len(c.Kinds) {
		kind = c.Kinds[c.Kind]
	}
	help := map[string]string{
		"linear":  "a personal API key: Linear → Settings → Security & access",
		"jira":    "the site; the email and an API token for Jira Cloud, a PAT alone for Server",
		"clickup": "a personal token: ClickUp → Settings → Apps",
	}[kind]
	writeString(dst, box.X+2, box.Y+1, truncate(help, box.Cols-4), theme.NotesSub, right)
	writeString(dst, box.X+2, g.Fields[0].Y, "tracker", theme.NotesSub, right)
	for i, r := range g.KindChips {
		style := theme.NotesSub
		if i == c.Kind {
			style = theme.NotesButton
			if c.Field != 0 {
				style = withBold(theme.NotesAccent)
			}
		}
		writeString(dst, r.X, r.Y, " "+c.Kinds[i]+" ", style, right)
	}
	field := func(i int, label, value string, show bool) {
		r := g.Fields[i]
		labelStyle := theme.NotesSub
		if !show {
			labelStyle = theme.NotesFence
		}
		writeString(dst, box.X+2, r.Y, label, labelStyle, right)
		if !show {
			return
		}
		style := theme.NotesFence
		if c.Field == i {
			style = theme.NotesButton
		}
		fillLine(dst, r.X, r.X+r.Cols, r.Y, style)
		x := writeString(dst, r.X+1, r.Y, truncateLeft(value, r.Cols-3), style, r.X+r.Cols)
		if c.Field == i {
			setCell(dst, x, r.Y, '▏', style)
		}
	}
	jira := kind == "jira"
	field(1, "site", c.URL, jira)
	field(2, "email", c.Email, jira)
	field(3, "token", strings.Repeat("•", min(len([]rune(c.Token)), 48)), true)
	msg, style := "tab next field · ←→ tracker · enter test and save · esc cancel", theme.NotesSub
	switch {
	case c.Testing:
		msg = "asking the tracker…"
	case c.Error != "":
		msg, style = c.Error, withBold(theme.Notes)
	}
	writeString(dst, box.X+2, box.Y+box.Rows-3, truncate(msg, box.Cols-4), style, right)
	for _, b := range g.ConnectButtons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}
