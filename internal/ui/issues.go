package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The issues panel is tend's own, after Orca's task page: the GitHub issues
// of the project being worked in, a preset and a search over them, and one
// issue whole — its text and its thread — in the same panel. It is drawn as
// the sessions list is. What the keys do, and asking GitHub, is the
// client's; this draws and says where a click landed.

// IssueEntry is one issue in the list.
type IssueEntry struct {
	// Repo is owner/name: in a folder of repositories, the issue's own.
	Repo      string
	Number    int
	Title     string
	State     string
	Author    string
	Labels    []string
	Assignees []string
	Comments  int
	Updated   time.Time
	URL       string
}

// IssueComment is one comment in an issue's thread.
type IssueComment struct {
	Author  string
	Body    string
	Created time.Time
}

// IssueDetailView is one issue, over the list.
type IssueDetailView struct {
	IssueEntry
	Body    string
	Created time.Time
	Thread  []IssueComment
	Loading bool
	Scroll  int
	// Linked are the pull requests the issue has, found after it is read.
	Linked []PREntry
}

// IssuesView is the panel while it is up.
type IssuesView struct {
	// Repo is owner/name, Remote the remote it came through, Remotes all
	// the GitHub ones the project has.
	Repo    string
	Remote  string
	Remotes []string
	// Filters are the presets' names and Filter the one in use.
	Filters []string
	Filter  int
	Query   string
	Issues  []IssueEntry
	Total   int
	Cursor  int
	Scroll  int
	Loading bool
	// Error is what stopped the list — no gh, no login, no remote — and
	// Message a line of what happened.
	Error   string
	Message string
	// Dir is the directory the repository was found from, said under the
	// list while nothing else is.
	Dir string
	Now time.Time
	// Detail is the issue open over the list, nil for the list.
	Detail *IssueDetailView
	// Scopes are a folder of repositories' chips — "all", then each by
	// name — and Scope the one listed; none for one repository.
	Scopes []string
	Scope  int
	// PullRequests is the pull requests' list rather than the issues';
	// PRs are its entries and PR the one open.
	PullRequests bool
	PRs          []PREntry
	PR           *PRDetailView
	// Compose is a comment or a new issue being written, nil when none is.
	Compose *IssueCompose
	// Picker is the open issue's labels or assignees being chosen.
	Picker *IssuePicker
	// Confirm is a question waiting for its answer: "close" asks for the
	// reason an issue is closed, "reopen" whether to open it again; Target
	// is the issue asked about — the one open, or the list's under the
	// cursor.
	Confirm string
	Target  *IssueEntry
}

// IssueCompose is what is being written to GitHub.
type IssueCompose struct {
	// NewIssue is a new issue, with a title; otherwise a comment on the
	// issue open.
	NewIssue bool
	// EditTitle is the open issue's title being changed: the title alone,
	// and enter saves it.
	EditTitle bool
	// Repo is where a new issue is filed, and Choose whether ctrl+t moves
	// it among a folder's repositories.
	Repo   string
	Choose bool
	Title  string
	Body   string
	// InBody is where typing goes: the title or the text.
	InBody  bool
	Sending bool
}

// Buttons, by what a click on one does.
const (
	IssueButtonOpen    = "open"
	IssueButtonBack    = "back"
	IssueButtonStart   = "start"
	IssueButtonNew     = "new"
	IssueButtonComment = "comment"
	IssueButtonState   = "state"
	IssueButtonBrowser = "browser"
	IssueButtonClose   = "close"
)

// IssueButton is one button on the bottom line.
type IssueButton struct {
	ID    string
	Label string
	Rect
}

// issueButtons are the buttons the panel shows as it is: the list's, or the
// open issue's, and Close at the right of both.
func issueButtons(v *IssuesView) []IssueButton {
	if v.PR != nil {
		return prButtons(v.PR)
	}
	if v.PullRequests && v.Detail == nil {
		return []IssueButton{
			{ID: IssueButtonOpen, Label: "[ Open ]"},
			{ID: IssueButtonBrowser, Label: "[ In browser ]"},
			{ID: IssueButtonClose, Label: "[ Close ]"},
		}
	}
	if v.Detail != nil {
		state := "[ Close issue ]"
		if v.Detail.State == "closed" {
			state = "[ Reopen ]"
		}
		return []IssueButton{
			{ID: IssueButtonBack, Label: "[ Back ]"},
			{ID: IssueButtonStart, Label: "[ Start work ]"},
			{ID: IssueButtonComment, Label: "[ Comment ]"},
			{ID: IssueButtonState, Label: state},
			{ID: IssueButtonBrowser, Label: "[ In browser ]"},
			{ID: IssueButtonClose, Label: "[ Close ]"},
		}
	}
	state := IssueButton{ID: IssueButtonState, Label: "[ Close issue ]"}
	if v.Cursor < len(v.Issues) && v.Issues[v.Cursor].State == "closed" {
		state = IssueButton{ID: IssueButtonState, Label: "[ Reopen ]"}
	}
	return []IssueButton{
		{ID: IssueButtonOpen, Label: "[ Open ]"},
		{ID: IssueButtonStart, Label: "[ Start work ]"},
		{ID: IssueButtonNew, Label: "[ New issue ]"},
		state,
		{ID: IssueButtonBrowser, Label: "[ In browser ]"},
		{ID: IssueButtonClose, Label: "[ Close ]"},
	}
}

// IssueButtonAt is the button under a point, by its ID.
func IssueButtonAt(v *IssuesView, cols, rows, x, y int) (string, bool) {
	if OnCloseMark(IssuesLayout(v, cols, rows).Box, x, y) {
		return IssueButtonClose, true
	}
	for _, b := range IssuesLayout(v, cols, rows).Buttons {
		if y == b.Y && x >= b.X && x < b.X+b.Cols {
			return b.ID, true
		}
	}
	return "", false
}

// composeRows is how many lines what is being written takes, over the
// hint: a rule, the title for a new issue, the text, and a blank.
const composeRows = 8

// IssuesGeometry is where the panel's parts are.
type IssuesGeometry struct {
	Box Rect
	// Filters are the presets' chips; Search the line typed into; List
	// the issues, one a line, under a line of column names.
	Filters []Rect
	Search  Rect
	List    Rect
	// Body is where an issue's text scrolls, in the detail.
	Body Rect
	// Compose is where what is being written goes, when it is.
	Compose Rect
	// Buttons are on the bottom line.
	Buttons []IssueButton
	// Modes are the Issues and Pull requests chips, at the right of the
	// presets.
	Modes [2]Rect
	// ScopeChips are a folder of repositories' chips, on a line of their
	// own over the presets.
	ScopeChips []Rect
}

// issueModes are the two lists' names, as their chips show them.
var issueModes = [2]string{" Issues ", " Pull requests "}

// IssuesLayout is the panel's geometry on a screen of cols by rows, for a
// view with these preset names.
func IssuesLayout(v *IssuesView, cols, rows int) IssuesGeometry {
	w, h := max(min(sessionsCols, cols-2), 0), max(min(sessionsRows, rows-2), 0)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := IssuesGeometry{Box: box}
	// A folder of repositories has a line of its own, over the presets,
	// in the lists; everything under it moves down one.
	off := 0
	if len(v.Scopes) > 0 && v.Detail == nil && v.PR == nil {
		off = 1
		g.ScopeChips, _, _ = layoutChips(box.X+2+runewidth.StringWidth("repository "), box.X+box.Cols-2, box.Y+2, v.Scopes, v.Scope)
	}
	x := box.X + 2
	for _, name := range v.Filters {
		label := " " + name + " "
		r := Rect{X: x, Y: box.Y + 2 + off, Cols: runewidth.StringWidth(label), Rows: 1}
		g.Filters = append(g.Filters, r)
		x += r.Cols + 1
	}
	mx := box.X + box.Cols - 2
	for i := len(issueModes) - 1; i >= 0; i-- {
		w := runewidth.StringWidth(issueModes[i])
		mx -= w
		g.Modes[i] = Rect{X: mx, Y: box.Y + 2 + off, Cols: w, Rows: 1}
		mx--
	}
	g.Search = Rect{X: box.X + 2, Y: box.Y + 3 + off, Cols: max(box.Cols-4, 0), Rows: 1}
	// The column names at Y+5, the issues under them; a blank, a message,
	// a hint and the buttons at the bottom.
	g.List = Rect{X: box.X + 2, Y: box.Y + 6 + off, Cols: max(box.Cols-4, 0), Rows: max(box.Rows-11-off, 0)}
	// The detail's header takes three lines and a blank.
	g.Body = Rect{X: box.X + 2, Y: box.Y + 6, Cols: max(box.Cols-5, 0), Rows: max(box.Rows-11, 0)}
	if v.Compose != nil {
		// What is being written takes the bottom of the list or the text,
		// which is still there over it to be read while writing.
		n := min(composeRows, max(g.List.Rows-3, 0))
		g.List.Rows -= n
		g.Body.Rows -= n
		g.Compose = Rect{X: box.X + 2, Y: g.List.Y + g.List.Rows, Cols: max(box.Cols-4, 0), Rows: n}
	}
	bottom := box.Y + box.Rows - 2
	bx := box.X + 2
	for _, b := range issueButtons(v) {
		b.Rect = Rect{X: bx, Y: bottom, Cols: runewidth.StringWidth(b.Label), Rows: 1}
		if b.ID == IssueButtonClose {
			b.X = box.X + box.Cols - 2 - b.Cols
		} else {
			bx += b.Cols + 1
		}
		g.Buttons = append(g.Buttons, b)
	}
	return g
}

// IssueAt is the issue on a line of the list, by its place in Issues.
func IssueAt(v *IssuesView, cols, rows, x, y int) (int, bool) {
	g := IssuesLayout(v, cols, rows)
	if v.Detail != nil || v.PR != nil || x < g.List.X || x >= g.List.X+g.List.Cols || y < g.List.Y || y >= g.List.Y+g.List.Rows {
		return 0, false
	}
	i := clampIssuesScroll(v, g) + y - g.List.Y
	return i, i < issuesListLen(v)
}

// IssueScopeAt is the scope chip under a point: 0 is all, then each of a
// folder's repositories.
func IssueScopeAt(v *IssuesView, cols, rows, x, y int) (int, bool) {
	for i, r := range IssuesLayout(v, cols, rows).ScopeChips {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return i, true
		}
	}
	return 0, false
}

// repoName is a repository's name without its owner, what a column has
// room for.
func repoName(slug string) string {
	if i := strings.LastIndexByte(slug, '/'); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// allRepos is whether the list shows every repository of a folder, and
// so says which each entry is in.
func allRepos(v *IssuesView) bool { return len(v.Scopes) > 0 && v.Scope == 0 }

// IssueModeAt is the Issues (0) or Pull requests (1) chip under a point.
func IssueModeAt(v *IssuesView, cols, rows, x, y int) (int, bool) {
	if v.Detail != nil || v.PR != nil {
		return 0, false
	}
	for i, r := range IssuesLayout(v, cols, rows).Modes {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return i, true
		}
	}
	return 0, false
}

// issuesListLen is how long the list shown is.
func issuesListLen(v *IssuesView) int {
	if v.PullRequests {
		return len(v.PRs)
	}
	return len(v.Issues)
}

// IssueFilterAt is the preset chip under a point.
func IssueFilterAt(v *IssuesView, cols, rows, x, y int) (int, bool) {
	for i, r := range IssuesLayout(v, cols, rows).Filters {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return i, true
		}
	}
	return 0, false
}

// IssuesScrollFor is the scroll that keeps the cursor in view.
func IssuesScrollFor(v *IssuesView, cols, rows int) int {
	g := IssuesLayout(v, cols, rows)
	top := clampIssuesScroll(v, g)
	if v.Cursor < top {
		top = v.Cursor
	}
	if g.List.Rows > 0 && v.Cursor >= top+g.List.Rows {
		top = v.Cursor - g.List.Rows + 1
	}
	return max(top, 0)
}

func clampIssuesScroll(v *IssuesView, g IssuesGeometry) int {
	return max(min(v.Scroll, issuesListLen(v)-g.List.Rows), 0)
}

// ClampIssueScroll is the detail's scroll as it will be drawn, for the
// client to keep its own in step.
func ClampIssueScroll(v *IssuesView, cols, rows int, theme Theme) int {
	g := IssuesLayout(v, cols, rows)
	switch {
	case v.PR != nil:
		return max(min(v.PR.Scroll, len(prBody(v, g.Body.Cols, theme))-g.Body.Rows), 0)
	case v.Detail != nil:
		return max(min(v.Detail.Scroll, len(issueBody(v, g.Body.Cols, theme))-g.Body.Rows), 0)
	}
	return 0
}

// issueBody is the detail's scrolling part: the issue's text, then each
// comment under a rule with who wrote it and when.
func issueBody(v *IssuesView, width int, theme Theme) []notesLine {
	d := v.Detail
	var out []notesLine
	if len(d.Linked) > 0 {
		// Its pull requests first: whether the work on it is done is the
		// question an issue is opened to answer.
		out = append(out, notesLine{spans: []notesSpan{{" PULL REQUESTS", theme.NotesAccent}, {"  p opens the first", theme.NotesSub}}, fill: theme.Notes})
		for _, p := range d.Linked {
			out = append(out, notesLine{spans: append([]notesSpan{{" ", theme.Notes}}, prLineSpans(p, theme)...), fill: theme.Notes})
		}
		out = append(out, notesLine{fill: theme.Notes})
	}
	out = append(out, threadLines(v, d.Body, d.Thread, width, theme)...)
	return out
}

// threadLines is a text and the comments under it, each under a rule with
// who wrote it and when.
func threadLines(v *IssuesView, text string, thread []IssueComment, width int, theme Theme) []notesLine {
	var out []notesLine
	body := strings.TrimSpace(hideComments(text))
	if body == "" {
		out = append(out, notesLine{spans: []notesSpan{{" no description", theme.NotesSub}}, fill: theme.Notes})
	} else {
		out = append(out, markdownLines(body, width, theme)...)
	}
	for _, c := range thread {
		head := fmt.Sprintf(" %s · %s ", c.Author, SessionAge(v.Now, c.Created))
		rule := strings.Repeat("─", max(width-runewidth.StringWidth(head)-2, 0))
		out = append(out, notesLine{fill: theme.Notes},
			notesLine{spans: []notesSpan{{"──", theme.NotesSub}, {head, withBold(theme.NotesAccent)}, {rule, theme.NotesSub}}, fill: theme.Notes})
		out = append(out, markdownLines(strings.TrimSpace(hideComments(c.Body)), width, theme)...)
	}
	return out
}

// hideComments takes out what GitHub does not show: HTML comments, which
// bots fill with metadata.
func hideComments(s string) string {
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start:], "-->")
		if end < 0 {
			return s[:start]
		}
		s = s[:start] + s[start+end+3:]
	}
}

func drawIssues(dst *vt.Grid, v *IssuesView, theme Theme) {
	g := IssuesLayout(v, dst.Cols(), dst.Rows())
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
	title := "GITHUB ISSUES"
	if v.PullRequests || v.PR != nil {
		title = "GITHUB PULL REQUESTS"
	}
	switch {
	case v.Repo != "":
		title += " · " + v.Repo
		if len(v.Remotes) > 1 {
			title += " (" + v.Remote + ")"
		}
	case len(v.Scopes) > 1:
		title += fmt.Sprintf(" · %d repositories", len(v.Scopes)-1)
	}
	writeString(dst, box.X+2, box.Y+1, truncate(title, box.Cols-24), withBold(theme.NotesAccent), right)
	switch {
	case v.PR != nil:
		drawPRDetail(dst, v, g, theme)
		return
	case v.Detail != nil:
		drawIssueDetail(dst, v, g, theme)
		return
	}
	drawChips(dst, box.X+2, "repository", g.ScopeChips, v.Scopes, v.Scope, theme, right)
	for i, r := range g.Modes {
		style := theme.NotesSub
		if (i == 1) == v.PullRequests {
			style = withBold(theme.NotesAccent)
		}
		writeString(dst, r.X, r.Y, issueModes[i], style, right)
	}
	if v.PullRequests {
		drawPRList(dst, v, g, theme)
		return
	}
	if v.Total > 0 || len(v.Issues) > 0 {
		count := fmt.Sprintf("%d of %d", len(v.Issues), v.Total)
		writeString(dst, right-1-runewidth.StringWidth(count), box.Y+1, count, theme.NotesSub, right)
	}
	drawIssueSearchAndFilters(dst, v, g, theme)

	// The columns: number, title, labels, author, age, comments.
	listEnd := g.List.X + g.List.Cols
	const numW, authorW, ageW, cmtW = 7, 14, 5, 4
	labelW := min(24, g.List.Cols/5)
	titleW := max(g.List.Cols-numW-labelW-authorW-ageW-cmtW-4, 10)
	cols := []int{g.List.X, g.List.X + numW}
	cols = append(cols, cols[1]+titleW+1)
	cols = append(cols, cols[2]+labelW+1)
	cols = append(cols, cols[3]+authorW+1)
	head := g.List.Y - 1
	third := "labels"
	if allRepos(v) {
		third = "repository"
	}
	for i, name := range []string{"#", "title", third, "author", "age"} {
		writeString(dst, cols[i], head, name, theme.NotesSub, listEnd)
	}
	writeString(dst, listEnd-2, head, "💬", theme.NotesSub, listEnd)

	switch {
	case v.Error != "":
		writeString(dst, g.List.X, g.List.Y, truncate(v.Error, g.List.Cols), withBold(theme.Notes), listEnd)
	case v.Loading && len(v.Issues) == 0:
		writeString(dst, g.List.X, g.List.Y, "asking GitHub…", theme.NotesSub, listEnd)
	case len(v.Issues) == 0:
		writeString(dst, g.List.X, g.List.Y, "no issues match", theme.NotesSub, listEnd)
	}
	top := clampIssuesScroll(v, g)
	for line := 0; line < g.List.Rows && top+line < len(v.Issues) && v.Error == ""; line++ {
		i := top + line
		e := v.Issues[i]
		y := g.List.Y + line
		base, sub, accent := theme.Notes, theme.NotesSub, theme.NotesAccent
		if i == v.Cursor {
			base = theme.NotesButton
			sub, accent = base, base
			for x := g.List.X; x < listEnd; x++ {
				setCell(dst, x, y, ' ', base)
			}
		}
		numStyle := accent
		if e.State == "closed" {
			numStyle = sub
		}
		writeString(dst, cols[0], y, fmt.Sprintf("%d", e.Number), numStyle, cols[1]-1)
		writeString(dst, cols[1], y, truncate(e.Title, titleW), base, cols[2]-1)
		third := strings.Join(e.Labels, ", ")
		if allRepos(v) {
			third = repoName(e.Repo)
		}
		writeString(dst, cols[2], y, truncate(third, labelW), sub, cols[3]-1)
		writeString(dst, cols[3], y, truncate(e.Author, authorW), sub, cols[4]-1)
		writeString(dst, cols[4], y, SessionAge(v.Now, e.Updated), sub, listEnd)
		if e.Comments > 0 {
			n := fmt.Sprintf("%d", e.Comments)
			writeString(dst, listEnd-runewidth.StringWidth(n), y, n, sub, listEnd)
		}
	}

	msg := v.Message
	if msg == "" {
		msg = v.Dir
	}
	if v.Loading && len(v.Issues) > 0 {
		msg = "asking GitHub…"
	}
	hint := "type to search · tab next filter · ↑↓ move · enter open · ctrl+x close · ctrl+n new issue · ctrl+o in browser · ctrl+r reload · esc"
	switch {
	case len(v.Scopes) > 0:
		hint += " · ctrl+t repository"
	case len(v.Remotes) > 1:
		hint += " · ctrl+t " + strings.Join(v.Remotes, "/")
	}
	drawIssuesFoot(dst, v, g, msg, hint, theme)
}

// drawIssuesFoot is what both views share at the bottom: what is being
// written, the message, the hint — or the question waiting — and the
// buttons.
func drawIssuesFoot(dst *vt.Grid, v *IssuesView, g IssuesGeometry, msg, hint string, theme Theme) {
	box := g.Box
	right := box.X + box.Cols - 1
	if v.Compose != nil {
		drawIssueCompose(dst, v, g, theme)
		hint = "enter sends · ctrl+j new line · esc cancels"
		if v.Compose.EditTitle {
			hint = "enter saves · esc cancels"
		}
		if v.Compose.NewIssue {
			hint = "tab title/text · enter in the text files it · ctrl+j new line · esc cancels"
		}
	}
	switch v.Confirm {
	case "close":
		msg, hint = fmt.Sprintf("close #%d %s?", v.Target.Number, truncate(v.Target.Title, 50)), "enter as completed · n as not planned · esc cancels"
	case "reopen":
		msg, hint = fmt.Sprintf("reopen #%d %s?", v.Target.Number, truncate(v.Target.Title, 50)), "enter reopens · esc cancels"
	case "merge":
		msg, hint = fmt.Sprintf("merge #%d into %s?", v.PR.Number, v.PR.Base), "s squash · m merge commit · r rebase · esc cancels"
	case "closepr":
		msg, hint = fmt.Sprintf("close #%d without merging?", v.PR.Number), "enter closes · esc cancels"
	case "reopenpr":
		msg, hint = fmt.Sprintf("reopen #%d?", v.PR.Number), "enter reopens · esc cancels"
	}
	msgY, hintY := box.Y+box.Rows-4, box.Y+box.Rows-3
	if msg != "" {
		style := theme.NotesSub
		if v.Confirm != "" {
			style = withBold(theme.Notes)
		}
		writeString(dst, box.X+2, msgY, truncate(msg, box.Cols-4), style, right)
	}
	writeString(dst, box.X+2, hintY, truncate(hint, box.Cols-4), theme.NotesSub, right)
	for _, b := range g.Buttons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}

// drawIssueCompose draws what is being written, wrapped, with the cursor
// at the end of the field typing goes to.
func drawIssueCompose(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	c, r := v.Compose, g.Compose
	if r.Rows < 3 {
		return
	}
	end := r.X + r.Cols
	for y := r.Y; y < r.Y+r.Rows; y++ {
		for x := r.X; x < end; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	head := " comment "
	if v.Detail != nil {
		head = fmt.Sprintf(" comment on #%d ", v.Detail.Number)
	}
	if v.PR != nil {
		head = fmt.Sprintf(" comment on #%d ", v.PR.Number)
	}
	if c.NewIssue {
		head = " new issue in " + c.Repo + " "
		if c.Choose {
			head += "(ctrl+t another) "
		}
	}
	if c.EditTitle && v.Detail != nil {
		head = fmt.Sprintf(" title of #%d ", v.Detail.Number)
	}
	if c.Sending {
		head += "· sending… "
	}
	writeString(dst, r.X, r.Y, "──"+head+strings.Repeat("─", max(r.Cols-runewidth.StringWidth(head)-2, 0)), theme.NotesAccent, end)
	y := r.Y + 1
	if c.EditTitle {
		x := writeString(dst, r.X, y, "title ", theme.NotesSub, end)
		x = writeString(dst, x, y, truncateLeft(c.Title, r.Cols-8), withBold(theme.Notes), end)
		setCell(dst, x, y, ' ', theme.NotesButton)
		return
	}
	if c.NewIssue {
		style := theme.Notes
		if !c.InBody {
			style = withBold(theme.Notes)
		}
		x := writeString(dst, r.X, y, "title ", theme.NotesSub, end)
		x = writeString(dst, x, y, truncateLeft(c.Title, r.Cols-8), style, end)
		if !c.InBody {
			setCell(dst, x, y, ' ', theme.NotesButton)
		}
		y++
	}
	// The text, wrapped at the width; its last lines when it is longer
	// than the room, so the cursor stays in sight.
	var lines []string
	for _, para := range strings.Split(c.Body, "\n") {
		for {
			if runewidth.StringWidth(para) <= r.Cols-1 {
				lines = append(lines, para)
				break
			}
			cut := runewidth.Truncate(para, r.Cols-1, "")
			lines = append(lines, cut)
			para = para[len(cut):]
		}
	}
	room := r.Y + r.Rows - y
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	for i, line := range lines {
		x := writeString(dst, r.X, y+i, line, theme.Notes, end)
		if i == len(lines)-1 && (c.InBody || !c.NewIssue) {
			setCell(dst, x, y+i, ' ', theme.NotesButton)
		}
	}
	if c.Body == "" && (c.InBody || !c.NewIssue) {
		writeString(dst, r.X+2, y, "markdown works; it goes as written", theme.NotesSub, end)
	}
}

func drawIssueDetail(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	d := v.Detail
	box := g.Box
	right := box.X + box.Cols - 1
	state := d.State
	stateStyle := withBold(theme.NotesAccent)
	if state == "closed" {
		stateStyle = withBold(theme.NotesSub)
	}
	x := box.X + 2
	if len(v.Scopes) > 0 {
		x = writeString(dst, x, box.Y+2, repoName(d.Repo)+" ", theme.NotesSub, right)
	}
	x = writeString(dst, x, box.Y+2, fmt.Sprintf("#%d ", d.Number), withBold(theme.NotesAccent), right)
	x = writeString(dst, x, box.Y+2, state, stateStyle, right)
	meta := ""
	if d.Author != "" {
		meta = " · opened by " + d.Author
		if !d.Created.IsZero() {
			meta += " " + SessionAge(v.Now, d.Created) + " ago"
		}
	}
	if len(d.Assignees) > 0 {
		meta += " · assigned to " + strings.Join(d.Assignees, ", ")
	}
	writeString(dst, x, box.Y+2, truncate(meta, right-x-1), theme.NotesSub, right)
	writeString(dst, box.X+2, box.Y+3, truncate(d.Title, box.Cols-4), withBold(theme.Notes), right)
	if len(d.Labels) > 0 {
		writeString(dst, box.X+2, box.Y+4, truncate(strings.Join(d.Labels, " · "), box.Cols-4), theme.NotesAccent, right)
	}

	if d.Loading {
		writeString(dst, g.Body.X, g.Body.Y, "reading the issue…", theme.NotesSub, g.Body.X+g.Body.Cols)
	} else {
		if v.Picker != nil {
			drawIssuePicker(dst, v.Picker, g.Body, theme)
		} else {
			drawScrolled(dst, g.Body, issueBody(v, g.Body.Cols, theme), d.Scroll, theme)
		}
	}

	toggle := "x close"
	if d.State == "closed" {
		toggle = "x reopen"
	}
	hint := "↑↓ scroll · w start work · c comment · e title · l labels · a assignees · " + toggle + " · p its PR · o browser · esc back"
	if v.Picker != nil {
		hint = "type to filter · ↑↓ move · enter marks · ctrl+s saves · esc cancels"
	}
	drawIssuesFoot(dst, v, g, v.Message, hint, theme)
}

// drawIssueSearchAndFilters draws the presets' chips and the search line,
// which both lists have.
func drawIssueSearchAndFilters(dst *vt.Grid, v *IssuesView, g IssuesGeometry, theme Theme) {
	right := g.Box.X + g.Box.Cols - 1
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
		writeString(dst, x+2, g.Search.Y, "words, or GitHub's own: label:bug author:someone", theme.NotesSub, end)
	} else {
		x = writeString(dst, x, g.Search.Y, truncateLeft(v.Query, g.Search.Cols-9), withBold(theme.Notes), end)
		setCell(dst, x, g.Search.Y, ' ', theme.NotesButton)
	}
}

// IssuePicker is the labels, or the assignees, of the open issue being
// chosen from what the repository offers.
type IssuePicker struct {
	// Kind is "labels" or "assignees".
	Kind    string
	Options []string
	// Marked is what the issue will have; Had what it had.
	Marked, Had map[string]bool
	Query       string
	Cursor      int
	Loading     bool
	Saving      bool
}

// PickerShown is the options the filter matches, in the repository's order.
func PickerShown(p *IssuePicker) []string {
	q := strings.ToLower(strings.TrimSpace(p.Query))
	if q == "" {
		return p.Options
	}
	var out []string
	for _, o := range p.Options {
		if strings.Contains(strings.ToLower(o), q) {
			out = append(out, o)
		}
	}
	return out
}

// drawIssuePicker draws the picker where the issue's text was: a filter,
// and the options with a mark on what the issue will have.
func drawIssuePicker(dst *vt.Grid, p *IssuePicker, r Rect, theme Theme) {
	end := r.X + r.Cols
	head := " " + p.Kind + " "
	if p.Saving {
		head += "· saving… "
	}
	writeString(dst, r.X, r.Y, "──"+head+strings.Repeat("─", max(r.Cols-runewidth.StringWidth(head)-2, 0)), theme.NotesAccent, end)
	x := writeString(dst, r.X, r.Y+1, "filter ", theme.NotesSub, end)
	x = writeString(dst, x, r.Y+1, truncateLeft(p.Query, r.Cols-9), withBold(theme.Notes), end)
	setCell(dst, x, r.Y+1, ' ', theme.NotesButton)
	if p.Loading {
		writeString(dst, r.X, r.Y+3, "asking GitHub…", theme.NotesSub, end)
		return
	}
	shown := PickerShown(p)
	if len(shown) == 0 {
		writeString(dst, r.X, r.Y+3, "nothing matches", theme.NotesSub, end)
		return
	}
	rows := max(r.Rows-3, 1)
	top := 0
	if p.Cursor >= rows {
		top = p.Cursor - rows + 1
	}
	for i := 0; i < rows && top+i < len(shown); i++ {
		name := shown[top+i]
		y := r.Y + 3 + i
		style, mark := theme.Notes, "[ ] "
		if p.Marked[name] {
			mark = "[✓] "
		}
		if top+i == p.Cursor {
			style = theme.NotesButton
			for cx := r.X; cx < end; cx++ {
				setCell(dst, cx, y, ' ', style)
			}
		}
		cx := writeString(dst, r.X+1, y, mark, style, end)
		writeString(dst, cx, y, truncate(name, r.Cols-6), style, end)
	}
}
