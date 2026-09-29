package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The GitHub issues panel (internal/ui/issues.go, internal/github): tend's
// own, after Orca's task page. The server asks gh, on the machine the
// projects are on, for the issues of the repository the pane being looked
// at is working in; this shows them, searches as the user types — GitHub's
// search, so a moment after typing stops rather than at every key — and
// opens one with its thread. What it looked at, and what was typed, are this
// client's alone.

// issueSearchDelay is how long typing must stop before GitHub is asked:
// each ask is a network call, and a word typed is several keys.
const issueSearchDelay = 400 * time.Millisecond

// issuesState is what the panel keeps between frames that the view does
// not draw.
type issuesState struct {
	// pane is the pane whose project is listed, fixed when the panel
	// opened; seq numbers each ask, so an answer overtaken by a later one
	// is dropped; detailSeq does the same for the issue open.
	pane      uint64
	seq       int
	detailSeq int
	// dir is the directory the list was found from, where a worktree for
	// an issue is made from; choices are the repositories of a folder of
	// several, each with its checkout, and repo the one of them listed,
	// empty for all.
	dir     string
	choices []proto.GitHubRepoChoice
	repo    string
}

// repoOfLocked is the repository an issue or a pull request is in: its
// own, which in a folder of several is not the list's; else the list's.
func (t *tui) repoOfLocked(own string) string {
	if own != "" {
		return own
	}
	if t.issues != nil {
		return t.issues.Repo
	}
	return ""
}

// dirOfLocked is the checkout a repository is in: its own in a folder of
// several, else where the list was found from.
func (t *tui) dirOfLocked(repo string) string {
	for _, c := range t.issueState.choices {
		if c.Slug == repo {
			return c.Dir
		}
	}
	return t.issueState.dir
}

// takeChoicesLocked keeps a folder's repositories from a list's answer and
// shows them as the scope chips: all, then each by name.
func (t *tui) takeChoicesLocked(multi bool, choices []proto.GitHubRepoChoice, listed string) {
	v := t.issues
	t.issueState.choices = nil
	v.Scopes, v.Scope = nil, 0
	if !multi {
		return
	}
	t.issueState.choices = choices
	v.Scopes = []string{"all"}
	for i, c := range choices {
		v.Scopes = append(v.Scopes, c.Name)
		if c.Slug == listed {
			v.Scope = i + 1
		}
	}
}

func (t *tui) issuesUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.issues != nil
}

// openIssues puts the panel up for the project of the pane in view.
func (t *tui) openIssues() error {
	t.mu.Lock()
	if t.issues == nil {
		t.issues = &ui.IssuesView{Filters: issueFilterNames(false), Now: time.Now()}
		t.issueState.pane = t.focus
	}
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.loadIssues(seq)
	return nil
}

// issueFilterNames are the presets of the issues' list, or the pull
// requests'.
func issueFilterNames(pullRequests bool) []string {
	if pullRequests {
		names := make([]string, len(github.PRFilters))
		for i, f := range github.PRFilters {
			names[i] = github.PRFilterName(f)
		}
		return names
	}
	names := make([]string, len(github.Filters))
	for i, f := range github.Filters {
		names[i] = f.String()
	}
	return names
}

// askIssuesLocked marks the list as being asked for, and numbers the ask.
func (t *tui) askIssuesLocked() int {
	t.issueState.seq++
	if t.issues != nil {
		t.issues.Loading = true
	}
	return t.issueState.seq
}

// loadIssues asks the server for the list as the view now says, and shows
// the answer if nothing was asked since.
func (t *tui) loadIssues(seq int) {
	t.mu.Lock()
	v := t.issues
	if v == nil || seq != t.issueState.seq {
		t.mu.Unlock()
		return
	}
	params := proto.GitHubIssuesParams{Pane: t.issueState.pane, Remote: v.Remote, Repo: t.issueState.repo, Filter: v.Filter, Query: v.Query}
	pullRequests := v.PullRequests
	t.mu.Unlock()
	if pullRequests {
		t.loadPRs(seq, params)
		return
	}

	if !t.client.Supports(proto.MethodGitHubIssues) {
		t.showIssuesError(seq, "this server is older than the issues panel; "+handoffCommand(t.session)+" moves it to this build")
		return
	}
	res, err := t.client.GitHubIssues(params)
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v = t.issues
	if v == nil || seq != t.issueState.seq {
		return
	}
	v.Loading, v.Now = false, time.Now()
	if err != nil {
		v.Error, v.Issues, v.Total = t.issueListError(err), nil, 0
		return
	}
	v.Error = ""
	v.Repo, v.Remote, v.Remotes, v.Total = res.Repo, res.Remote, res.Remotes, res.Total
	t.takeChoicesLocked(res.Multi, res.Choices, res.Repo)
	var at int
	if v.Cursor < len(v.Issues) {
		at = v.Issues[v.Cursor].Number
	}
	v.Issues = v.Issues[:0]
	v.Cursor = 0
	for i, x := range res.Issues {
		v.Issues = append(v.Issues, issueEntry(x))
		if x.Number == at {
			v.Cursor = i
		}
	}
	v.Scroll = ui.IssuesScrollFor(v, t.cols, t.rows)
	// Not in Message: a reload after closing an issue would put it over
	// "closed #42", before anybody read that.
	v.Dir = res.Dir
	t.issueState.dir = res.Dir
}

func (t *tui) showIssuesError(seq int, message string) {
	t.mu.Lock()
	if v := t.issues; v != nil && seq == t.issueState.seq {
		v.Loading, v.Error = false, message
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func issueEntry(x proto.GitHubIssue) ui.IssueEntry {
	return ui.IssueEntry{
		Repo: x.Repo, Number: x.Number, Title: x.Title, State: x.State, Author: x.Author,
		Labels: x.Labels, Assignees: x.Assignees, Comments: x.Comments,
		Updated: time.Unix(x.Updated, 0), URL: x.URL,
	}
}

func (t *tui) closeIssues() {
	t.mu.Lock()
	t.issues = nil
	t.issueState.seq++ // what is still on its way is for nobody
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// issuesInput is every key and click while the panel is up: the list's
// when it shows the list, the issue's when one is open.
func (t *tui) issuesInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		if done, err := t.issuesMouse(ev); done || err != nil {
			return err
		}
	}
	for _, key := range splitKeys(forward) {
		t.mu.Lock()
		v := t.issues
		composing := v != nil && v.Compose != nil
		picking := v != nil && v.Picker != nil
		confirming := v != nil && v.Confirm != ""
		pr := v != nil && v.PR != nil
		detail := v != nil && v.Detail != nil
		t.mu.Unlock()
		var done bool
		switch {
		case composing:
			t.issueComposeKey(key)
		case picking:
			t.issuePickerKey(key)
		case confirming:
			t.issueConfirmKey(key)
		case pr:
			t.prDetailKey(key)
		case detail:
			done = t.issueDetailKey(key)
		default:
			done = t.issueListKey(key)
		}
		if done {
			return nil
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) issuesMouse(ev ui.MouseEvent) (bool, error) {
	t.mu.Lock()
	v, cols, rows := t.issues, t.cols, t.rows
	if v == nil {
		t.mu.Unlock()
		return true, nil
	}
	detail := v.Detail != nil || v.PR != nil
	t.mu.Unlock()
	switch ev.Kind {
	case ui.MouseWheelUp:
		if detail {
			t.scrollIssue(-3)
		} else {
			t.moveIssueCursor(-3)
		}
		return false, nil
	case ui.MouseWheelDown:
		if detail {
			t.scrollIssue(3)
		} else {
			t.moveIssueCursor(3)
		}
		return false, nil
	}
	if ev.Kind != ui.MousePress || ev.Button != 0 {
		return false, nil
	}
	if id, ok := ui.IssueButtonAt(v, cols, rows, ev.X, ev.Y); ok {
		return t.issueButton(id), nil
	}
	switch {
	case !detail:
		if n, ok := ui.IssueScopeAt(v, cols, rows, ev.X, ev.Y); ok {
			t.setIssueScopeTo(n)
			return false, nil
		}
		if m, ok := ui.IssueModeAt(v, cols, rows, ev.X, ev.Y); ok {
			t.setIssueMode(m == 1)
			return false, nil
		}
		if f, ok := ui.IssueFilterAt(v, cols, rows, ev.X, ev.Y); ok {
			t.setIssueFilter(f)
			return false, nil
		}
		if i, ok := ui.IssueAt(v, cols, rows, ev.X, ev.Y); ok {
			t.mu.Lock()
			again := v.Cursor == i
			v.Cursor = i
			t.dirty = true
			t.mu.Unlock()
			// A click on the one already chosen opens it.
			if again {
				t.openIssue()
			}
		}
	}
	return false, nil
}

// issueListKey is a key over the list; it reports whether the panel went.
func (t *tui) issueListKey(key string) bool {
	switch key {
	case "\x1b":
		t.mu.Lock()
		v := t.issues
		if v != nil && v.Query != "" {
			v.Query = ""
			seq := t.askIssuesLocked()
			t.dirty = true
			t.mu.Unlock()
			go t.loadIssues(seq)
			return false
		}
		t.mu.Unlock()
		t.closeIssues()
		return true
	case "\r", "\n":
		t.openIssue()
	case "\x1b[A", "\x10":
		t.moveIssueCursor(-1)
	case "\x1b[B":
		t.moveIssueCursor(1)
	case "\x1b[C":
		t.setIssueMode(true)
	case "\x1b[D":
		t.setIssueMode(false)
	case "\x0e": // ctrl+n
		t.startIssueCompose(true)
	case "\x18": // ctrl+x: close the one under the cursor, asking how
		t.askIssueState()
	case "\x1b[5~":
		t.moveIssueCursor(-10)
	case "\x1b[6~":
		t.moveIssueCursor(10)
	case "\t":
		t.stepIssueFilter(1)
	case "\x1b[Z":
		t.stepIssueFilter(-1)
	case "\x0f": // ctrl+o
		t.openIssueInBrowser()
	case "\x12": // ctrl+r
		t.mu.Lock()
		seq := t.askIssuesLocked()
		t.dirty = true
		t.mu.Unlock()
		go t.loadIssues(seq)
	case "\x14": // ctrl+t
		t.nextIssueScope()
	case "\x15": // ctrl+u
		t.editIssueQuery(func(string) string { return "" })
	case "\x7f", "\x08":
		t.editIssueQuery(func(q string) string {
			_, size := utf8.DecodeLastRuneInString(q)
			return q[:len(q)-size]
		})
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			t.editIssueQuery(func(q string) string { return q + key })
		}
	}
	return false
}

// issueDetailKey is a key over an open issue: it scrolls, opens it in the
// browser, or goes back to the list.
func (t *tui) issueDetailKey(key string) bool {
	switch key {
	case "\x1b", "q":
		t.mu.Lock()
		if v := t.issues; v != nil {
			v.Detail = nil
			v.Message = ""
		}
		t.issueState.detailSeq++
		t.dirty = true
		t.mu.Unlock()
	case "\x1b[A", "k":
		t.scrollIssue(-1)
	case "\x1b[B", "j":
		t.scrollIssue(1)
	case "\x1b[5~", "\x15":
		t.scrollIssue(-10)
	case "\x1b[6~", "\x04", " ":
		t.scrollIssue(10)
	case "g", "\x1b[H":
		t.scrollIssue(-1 << 20)
	case "G", "\x1b[F":
		t.scrollIssue(1 << 20)
	case "o", "\x0f":
		t.openIssueInBrowser()
	case "c":
		t.startIssueCompose(false)
	case "x":
		t.askIssueState()
	case "w":
		return t.startIssueWork()
	case "p":
		t.openLinkedPR()
	case "e":
		t.startTitleEdit()
	case "l":
		t.openIssuePicker("labels")
	case "a":
		t.openIssuePicker("assignees")
	}
	return false
}

// editIssueQuery changes what is typed, and asks GitHub once typing stops.
func (t *tui) editIssueQuery(edit func(string) string) {
	t.mu.Lock()
	v := t.issues
	if v == nil {
		t.mu.Unlock()
		return
	}
	v.Query = edit(v.Query)
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	time.AfterFunc(issueSearchDelay, func() { t.loadIssues(seq) })
}

func (t *tui) setIssueFilter(f int) {
	t.mu.Lock()
	v := t.issues
	if v == nil {
		t.mu.Unlock()
		return
	}
	v.Filter = f
	v.Cursor, v.Scroll = 0, 0
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	go t.loadIssues(seq)
}

func (t *tui) stepIssueFilter(by int) {
	t.mu.Lock()
	n, f := 0, 0
	if v := t.issues; v != nil {
		n, f = len(v.Filters), v.Filter
	}
	t.mu.Unlock()
	if n > 0 {
		t.setIssueFilter((f + by + n) % n)
	}
}

// nextIssueRemote lists the next GitHub remote the project has: upstream's
// issues, then origin's, for a fork.
func (t *tui) nextIssueRemote() {
	t.mu.Lock()
	v := t.issues
	if v == nil || len(v.Remotes) < 2 {
		t.mu.Unlock()
		return
	}
	next := v.Remotes[0]
	for i, r := range v.Remotes {
		if r == v.Remote {
			next = v.Remotes[(i+1)%len(v.Remotes)]
		}
	}
	v.Remote = next
	v.Cursor, v.Scroll = 0, 0
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	go t.loadIssues(seq)
}

func (t *tui) moveIssueCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.issues
	n := 0
	if v != nil {
		n = len(v.Issues)
		if v.PullRequests {
			n = len(v.PRs)
		}
	}
	if n == 0 {
		return
	}
	v.Cursor = max(min(v.Cursor+by, n-1), 0)
	v.Scroll = ui.IssuesScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

func (t *tui) scrollIssue(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.issues
	switch {
	case v == nil:
		return
	case v.PR != nil:
		v.PR.Scroll = max(v.PR.Scroll+by, 0)
		v.PR.Scroll = ui.ClampIssueScroll(v, t.cols, t.rows, t.theme)
	case v.Detail != nil:
		v.Detail.Scroll = max(v.Detail.Scroll+by, 0)
		v.Detail.Scroll = ui.ClampIssueScroll(v, t.cols, t.rows, t.theme)
	}
	t.dirty = true
}

// openIssue opens the issue under the cursor over the list.
func (t *tui) openIssue() {
	t.mu.Lock()
	v := t.issues
	if v != nil && v.PullRequests {
		if v.Cursor < len(v.PRs) {
			p := v.PRs[v.Cursor]
			t.mu.Unlock()
			t.showPR(p, false)
			return
		}
		t.mu.Unlock()
		return
	}
	if v == nil || v.Cursor >= len(v.Issues) {
		t.mu.Unlock()
		return
	}
	e := v.Issues[v.Cursor]
	t.mu.Unlock()
	t.showIssue(e, false)
}

// openIssueNumber opens an issue the list may not have yet: one just filed.
func (t *tui) openIssueNumber(number int, url string) {
	t.showIssue(ui.IssueEntry{Number: number, URL: url, State: "open"}, false)
}

// showIssue reads an issue whole and shows it; reload keeps what is shown
// of it, and where it was scrolled, until the answer replaces it.
func (t *tui) showIssue(e ui.IssueEntry, reload bool) {
	t.mu.Lock()
	v := t.issues
	if v == nil {
		t.mu.Unlock()
		return
	}
	if !reload || v.Detail == nil || v.Detail.Number != e.Number {
		v.Detail = &ui.IssueDetailView{IssueEntry: e, Loading: true}
		v.Message = e.URL
	}
	t.issueState.detailSeq++
	seq, repo := t.issueState.detailSeq, t.repoOfLocked(e.Repo)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		d, err := t.client.GitHubIssue(repo, e.Number)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.issues
		if v == nil || v.Detail == nil || seq != t.issueState.detailSeq {
			return
		}
		v.Detail.Loading = false
		if err != nil {
			v.Detail.Body = "could not read the issue: " + ghError(err)
			return
		}
		v.Detail.IssueEntry = issueEntry(d.GitHubIssue)
		v.Detail.Body, v.Detail.Created = d.Body, time.Unix(d.Created, 0)
		v.Detail.Thread = v.Detail.Thread[:0]
		for _, c := range d.Thread {
			v.Detail.Thread = append(v.Detail.Thread, ui.IssueComment{Author: c.Author, Body: c.Body, Created: time.Unix(c.Created, 0)})
		}
		go t.loadLinkedPRs(seq, repo, e.Number)
	}()
}

// openIssueInBrowser opens the issue open, or the one under the cursor, in
// the desktop's browser, on the machine the user is at.
func (t *tui) openIssueInBrowser() {
	t.mu.Lock()
	v := t.issues
	url := ""
	switch {
	case v == nil:
	case v.PR != nil:
		url = v.PR.URL
	case v.Detail != nil:
		url = v.Detail.URL
	case v.PullRequests && v.Cursor < len(v.PRs):
		url = v.PRs[v.Cursor].URL
	case !v.PullRequests && v.Cursor < len(v.Issues):
		url = v.Issues[v.Cursor].URL
	}
	t.mu.Unlock()
	if url == "" {
		return
	}
	message := "opened " + url
	if err := openURL(url); err != nil {
		message = fmt.Sprintf("could not open %s: %v", url, err)
	}
	t.mu.Lock()
	if t.issues != nil {
		t.issues.Message = message
	}
	t.dirty = true
	t.mu.Unlock()
}

// issueButton does what a button is for; it reports whether the panel
// went.
func (t *tui) issueButton(id string) bool {
	switch id {
	case ui.IssueButtonClose:
		t.closeIssues()
		return true
	case ui.IssueButtonBrowser:
		t.openIssueInBrowser()
	case ui.IssueButtonBack:
		if t.prOpen() {
			t.prDetailKey("\x1b")
		} else {
			t.issueDetailKey("\x1b")
		}
	case ui.IssueButtonOpen:
		t.openIssue()
	case ui.IssueButtonNew:
		t.startIssueCompose(true)
	case ui.IssueButtonComment:
		t.startIssueCompose(false)
	case ui.IssueButtonState:
		if t.prOpen() {
			t.askPRState()
		} else {
			t.askIssueState()
		}
	case ui.IssueButtonMerge:
		t.askPRMerge()
	case ui.IssueButtonReady:
		t.readyPR()
	case ui.IssueButtonStart:
		return t.startIssueWork()
	}
	return false
}

// ghError is what GitHub said, without the method it was said to.
// forgeSaid is what GitHub or a GitLab answered, for a line under the
// panel. A GitLab's answers name it already ("gitlab.com answered…", or the
// token it wants); what gh said is said to be GitHub's.
func forgeSaid(err error) string {
	msg := ghError(err)
	if strings.Contains(msg, "GitLab") || strings.Contains(msg, "gitlab") {
		return msg
	}
	return "GitHub said: " + msg
}

func ghError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && strings.HasPrefix(msg, "github.") {
		msg = msg[i+2:]
	}
	return msg
}

// startIssueCompose starts writing: a comment on the issue open, or a new
// issue in the repository listed.
func (t *tui) startIssueCompose(newIssue bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.issues
	if v == nil || (v.Repo == "" && len(t.issueState.choices) == 0) || (!newIssue && v.Detail == nil && v.PR == nil) || (newIssue && v.PullRequests) {
		return
	}
	v.Compose = &ui.IssueCompose{NewIssue: newIssue, InBody: !newIssue}
	if newIssue {
		// Filed in the repository listed; from all of a folder's, in the
		// first, which ctrl+t changes.
		v.Compose.Repo = v.Repo
		if v.Compose.Repo == "" {
			v.Compose.Repo = t.issueState.choices[0].Slug
		}
		v.Compose.Choose = len(t.issueState.choices) > 1 && v.Repo == ""
	}
	v.Confirm = ""
	t.dirty = true
}

// issueComposeKey is a key while writing: text goes in, backspace takes
// the last letter out, ctrl+j breaks the line, tab moves between a new
// issue's title and text, enter sends — or, in a new issue's title, moves
// to its text — and esc gives up.
func (t *tui) issueComposeKey(key string) {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Compose == nil {
		t.mu.Unlock()
		return
	}
	c := v.Compose
	if c.Sending {
		t.mu.Unlock()
		return
	}
	field := &c.Body
	if (c.NewIssue && !c.InBody) || c.EditTitle {
		field = &c.Title
	}
	send := false
	switch key {
	case "\x14": // ctrl+t: the repository a new issue goes to
		if c.Choose {
			for n, ch := range t.issueState.choices {
				if ch.Slug == c.Repo {
					c.Repo = t.issueState.choices[(n+1)%len(t.issueState.choices)].Slug
					break
				}
			}
		}
	case "\x1b":
		v.Compose = nil
	case "\t", "\x1b[Z":
		if c.NewIssue {
			c.InBody = !c.InBody
		}
	case "\r":
		if c.NewIssue && !c.InBody && !c.EditTitle {
			c.InBody = true
		} else {
			send = true
		}
	case "\n": // ctrl+j
		if c.EditTitle {
			break // a title is one line
		}
		if field == &c.Body {
			*field += "\n"
		} else {
			c.InBody = true
		}
	case "\x7f", "\x08":
		_, size := utf8.DecodeLastRuneInString(*field)
		*field = (*field)[:len(*field)-size]
	case "\x15":
		*field = ""
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			*field += key
		}
	}
	t.dirty = true
	t.mu.Unlock()
	if send {
		t.sendIssueCompose()
	}
}

// sendIssueCompose writes what was written to GitHub, and shows it done: a
// comment read back into the issue, a new issue opened.
func (t *tui) sendIssueCompose() {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Compose == nil {
		t.mu.Unlock()
		return
	}
	c := *v.Compose
	if c.EditTitle {
		t.mu.Unlock()
		t.saveIssueTitle(c.Title)
		return
	}
	if strings.TrimSpace(c.Body) == "" && !c.NewIssue {
		v.Message = "a comment needs some text"
		t.mu.Unlock()
		return
	}
	v.Compose.Sending = true
	repo, number, onPR := c.Repo, 0, v.PR != nil
	switch {
	case onPR:
		number, repo = v.PR.Number, t.repoOfLocked(v.PR.Repo)
	case v.Detail != nil && !c.NewIssue:
		number, repo = v.Detail.Number, t.repoOfLocked(v.Detail.Repo)
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()

	go func() {
		var err error
		var created proto.GitHubIssueCreated
		switch {
		case c.NewIssue:
			created, err = t.client.GitHubIssueCreate(repo, c.Title, c.Body)
		case onPR:
			err = t.client.GitHubPRAction(proto.GitHubPRActionParams{Repo: repo, Number: number, Action: "comment", Body: c.Body})
		default:
			err = t.client.GitHubIssueComment(repo, number, c.Body)
		}
		t.mu.Lock()
		v := t.issues
		if v == nil {
			t.mu.Unlock()
			return
		}
		if err != nil {
			// What was written stays, to be sent again or copied out.
			if v.Compose != nil {
				v.Compose.Sending = false
			}
			v.Message = forgeSaid(err)
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		v.Compose = nil
		if c.NewIssue {
			v.Detail = nil
			seq := t.askIssuesLocked()
			t.dirty = true
			t.mu.Unlock()
			t.loadIssues(seq)
			t.openIssueNumber(created.Number, created.URL)
			t.mu.Lock()
			if v := t.issues; v != nil {
				v.Message = fmt.Sprintf("filed #%d · %s", created.Number, created.URL)
			}
			t.mu.Unlock()
			return
		}
		v.Message = fmt.Sprintf("commented on #%d", number)
		t.dirty = true
		t.mu.Unlock()
		if onPR {
			t.reloadPR()
			return
		}
		t.reloadIssue()
	}()
}

// askIssueState asks before closing the issue open, or opening it again.
func (t *tui) askIssueState() {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.issues
	if v == nil {
		return
	}
	// The one open, or from the list the one under the cursor: an issue
	// already fixed is closed without opening it.
	var target ui.IssueEntry
	switch {
	case v.Detail != nil && !v.Detail.Loading:
		target = v.Detail.IssueEntry
	case v.Detail == nil && !v.PullRequests && v.Cursor < len(v.Issues):
		target = v.Issues[v.Cursor]
	default:
		return
	}
	v.Target = &target
	v.Confirm = "close"
	if target.State == "closed" {
		v.Confirm = "reopen"
	}
	t.dirty = true
}

// issueConfirmKey answers the question: enter closes as completed, or
// reopens; n closes as not planned; anything else leaves it.
func (t *tui) issueConfirmKey(key string) {
	t.mu.Lock()
	v := t.issues
	if v == nil || (v.Target == nil && v.PR == nil) {
		t.mu.Unlock()
		return
	}
	asked := v.Confirm
	v.Confirm = ""
	t.dirty = true
	if v.PR != nil {
		t.mu.Unlock()
		t.prConfirmKey(asked, key)
		return
	}
	state, reason := "", ""
	switch {
	case asked == "close" && (key == "\r" || key == "\n"):
		state, reason = "closed", github.ReasonCompleted
	case asked == "close" && key == "n":
		state, reason = "closed", github.ReasonNotPlanned
	case asked == "reopen" && (key == "\r" || key == "\n"):
		state = "open"
	}
	if state == "" {
		v.Target = nil
		t.mu.Unlock()
		return
	}
	repo, number := t.repoOfLocked(v.Target.Repo), v.Target.Number
	v.Target = nil
	v.Message = "asking…"
	t.mu.Unlock()
	go func() {
		err := t.client.GitHubIssueState(repo, number, state, reason)
		t.mu.Lock()
		if v := t.issues; v != nil {
			if err != nil {
				v.Message = forgeSaid(err)
			} else if state == "closed" {
				v.Message = fmt.Sprintf("closed #%d as %s", number, reason)
			} else {
				v.Message = fmt.Sprintf("reopened #%d", number)
			}
		}
		seq := t.askIssuesLocked()
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		if err == nil {
			t.reloadIssue()
			t.loadIssues(seq)
		}
	}()
}

// reloadIssue reads the issue open again.
func (t *tui) reloadIssue() {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Detail == nil {
		t.mu.Unlock()
		return
	}
	e := v.Detail.IssueEntry
	t.mu.Unlock()
	t.showIssue(e, true)
}

// issueAgents is how each agent is started with a prompt of its own: as
// the first argument for most, behind -i for Gemini and Antigravity, whose
// bare prompt runs once and exits (agy's -i is --prompt-interactive: the
// prompt, then the session goes on). An agent not here is started with
// nothing.
var issueAgents = map[string][]string{
	"agy":    {"agy", "-i"},
	"claude": {"claude"},
	"codex":  {"codex"},
	"cursor": {"cursor-agent"},
	"gemini": {"gemini", "-i"},
}

// issueAgent is the agent work on an issue starts in: the one the settings
// name, else the one in the pane the panel was opened from, else claude.
func (t *tui) issueAgentLocked() string { return t.agentForLocked(t.issueState.pane) }

// agentForLocked is the agent work is started in, from a panel opened from
// pane: the one the settings name, else pane's, else claude.
func (t *tui) agentForLocked(pane uint64) string {
	name := strings.TrimSpace(t.config.Issues.Agent)
	if name == "cursor-agent" {
		name = "cursor"
	}
	if name == "" {
		for _, p := range t.snap.Panes {
			if p.ID == pane {
				if _, ok := issueAgents[p.Agent]; ok {
					name = p.Agent
				}
			}
		}
	}
	if name == "" {
		name = "claude"
	}
	return name
}

// startIssueWork is Orca's "start workspace from issue": a worktree on a
// branch named after the issue, from the project listed, opened as a space
// of its own, and the agent started there with a prompt that names the
// issue. An issue that already has one is gone to instead — work already
// begun is not begun twice. It reports whether the panel went.
func (t *tui) startIssueWork() bool {
	t.mu.Lock()
	v := t.issues
	var e ui.IssueEntry
	switch {
	case v == nil:
	case v.Detail != nil:
		e = v.Detail.IssueEntry
	case v.Cursor < len(v.Issues):
		e = v.Issues[v.Cursor]
	}
	repo := t.repoOfLocked(e.Repo)
	dir, session := t.dirOfLocked(repo), t.session
	if e.Number == 0 || dir == "" {
		t.mu.Unlock()
		return false
	}
	agentName := t.issueAgentLocked()
	prompt := t.config.IssuePrompt(e.URL, e.Number, e.Title, repo)
	v.Message = fmt.Sprintf("making a worktree for #%d…", e.Number)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()

	go func() {
		ws, tab, message, err := t.issueWorktree(session, dir, e, agentName, prompt)
		if err != nil {
			t.mu.Lock()
			if v := t.issues; v != nil {
				v.Message = err.Error()
			}
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		t.closeIssues()
		t.mu.Lock()
		t.rememberFocusLocked()
		t.workspace, t.zoom = ws, false
		// Zero, for a worktree already made, is the space's own tab, as
		// showWorkspace has it.
		t.tab, t.focus = tab, 0
		t.mu.Unlock()
		t.setMessage(message, false)
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}()
	return false
}

// issueWorktree makes, or finds, the issue's worktree and its space, and
// starts the agent in a new tab there when the worktree is new. The
// worktree methods are the automation socket's, as prefix+G's are.
func (t *tui) issueWorktree(session, dir string, e ui.IssueEntry, agentName, prompt string) (ws, tab uint64, message string, err error) {
	list, err := apiCall(session, api.MethodWorktreeList, map[string]any{"cwd": dir}, true)
	if err != nil {
		return 0, 0, "", err
	}
	trees, _ := list["worktrees"].([]any)
	for _, raw := range trees {
		wt, _ := raw.(map[string]any)
		branch, _ := wt["branch"].(string)
		if !github.IsIssueBranch(branch, e.Number) {
			continue
		}
		path, _ := wt["path"].(string)
		opened, err := apiCall(session, api.MethodWorktreeOpen, map[string]any{"cwd": dir, "path": path}, true)
		if err != nil {
			return 0, 0, "", err
		}
		return workspaceOf(opened), 0, fmt.Sprintf("#%d already has a worktree: %s", e.Number, branch), nil
	}

	branch := github.IssueBranch(e.Number, e.Title)
	created, err := apiCall(session, api.MethodWorktreeCreate, map[string]any{
		"cwd": dir, "branch": branch, "label": fmt.Sprintf("#%d %s", e.Number, e.Title),
	}, true)
	if err != nil {
		return 0, 0, "", err
	}
	ws = workspaceOf(created)
	wt, _ := created["worktree"].(map[string]any)
	path, _ := wt["path"].(string)
	if ws == 0 || path == "" {
		return 0, 0, "", errors.New("the worktree was made, but tend could not tell where")
	}
	argv := append(append([]string{}, issueAgents[agentName]...), prompt)
	tab, _, err = t.client.NewTab(ws, fmt.Sprintf("#%d %s", e.Number, agentName), proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", resumeScript, "tend-issue"}, argv...),
		Dir:     path,
		Agent:   agentName,
	})
	if err != nil {
		return ws, 0, "", fmt.Errorf("the worktree %s is made, but %s did not start: %w", branch, agentName, err)
	}
	return ws, tab, fmt.Sprintf("#%d: %s on %s", e.Number, agentName, branch), nil
}

// workspaceOf is the space a worktree method answered with.
func workspaceOf(result map[string]any) uint64 {
	w, _ := result["workspace"].(map[string]any)
	id, _ := w["workspace_id"].(string)
	n, _ := strconv.ParseUint(strings.TrimPrefix(id, "w_"), 10, 64)
	return n
}

// startTitleEdit opens the box on the open issue's title, to change it.
func (t *tui) startTitleEdit() {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.issues
	if v == nil || v.Detail == nil || v.Detail.Loading {
		return
	}
	v.Compose = &ui.IssueCompose{EditTitle: true, Title: v.Detail.Title}
	t.dirty = true
}

// saveIssueTitle sends the title written, when it changed.
func (t *tui) saveIssueTitle(title string) {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Detail == nil || v.Compose == nil {
		t.mu.Unlock()
		return
	}
	title = strings.TrimSpace(title)
	if title == "" || title == v.Detail.Title {
		v.Compose = nil
		t.dirty = true
		t.mu.Unlock()
		return
	}
	v.Compose.Sending = true
	repo, number := t.repoOfLocked(v.Detail.Repo), v.Detail.Number
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.editIssue(proto.GitHubIssueEditParams{Repo: repo, Number: number, Title: title}, "title saved")
}

// editIssue sends an edit, says how it went, and reads the issue and the
// list again.
func (t *tui) editIssue(p proto.GitHubIssueEditParams, done string) {
	err := t.client.GitHubIssueEdit(p)
	t.mu.Lock()
	v := t.issues
	if v == nil {
		t.mu.Unlock()
		return
	}
	if err != nil {
		// What was chosen or written stays, to be sent again.
		if v.Compose != nil {
			v.Compose.Sending = false
		}
		if v.Picker != nil {
			v.Picker.Saving = false
		}
		v.Message = forgeSaid(err)
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return
	}
	v.Compose, v.Picker = nil, nil
	v.Message = fmt.Sprintf("#%d: %s", p.Number, done)
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	t.reloadIssue()
	t.loadIssues(seq)
}

// openIssuePicker opens the labels, or the assignees, of the open issue
// with what the repository offers.
func (t *tui) openIssuePicker(kind string) {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Detail == nil || v.Detail.Loading {
		t.mu.Unlock()
		return
	}
	current := v.Detail.Labels
	if kind == "assignees" {
		current = v.Detail.Assignees
	}
	p := &ui.IssuePicker{Kind: kind, Loading: true, Marked: map[string]bool{}, Had: map[string]bool{}}
	for _, name := range current {
		p.Marked[name], p.Had[name] = true, true
	}
	v.Picker = p
	repo := t.repoOfLocked(v.Detail.Repo)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		names, err := t.client.GitHubRepoOptions(repo, kind)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.issues
		if v == nil || v.Picker != p {
			return
		}
		p.Loading = false
		if err != nil {
			v.Picker = nil
			v.Message = forgeSaid(err)
			return
		}
		// What the issue has comes first, and stays offered even when the
		// repository no longer lists it — somebody who left the project
		// can still be taken off.
		seen := map[string]bool{}
		for _, name := range current {
			p.Options = append(p.Options, name)
			seen[name] = true
		}
		for _, name := range names {
			if !seen[name] {
				p.Options = append(p.Options, name)
			}
		}
	}()
}

// issuePickerKey is a key in the picker: typing filters, the arrows move,
// enter marks or unmarks, ctrl+s saves what changed, esc gives up.
func (t *tui) issuePickerKey(key string) {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Picker == nil {
		t.mu.Unlock()
		return
	}
	p := v.Picker
	if p.Saving {
		t.mu.Unlock()
		return
	}
	shown := ui.PickerShown(p)
	save := false
	switch key {
	case "\x1b":
		v.Picker = nil
	case "\x1b[A", "\x10":
		p.Cursor = max(p.Cursor-1, 0)
	case "\x1b[B", "\x0e":
		p.Cursor = min(p.Cursor+1, max(len(shown)-1, 0))
	case "\r", "\n", "\t":
		if p.Cursor < len(shown) {
			name := shown[p.Cursor]
			p.Marked[name] = !p.Marked[name]
		}
	case "\x13": // ctrl+s
		save = true
	case "\x7f", "\x08":
		_, size := utf8.DecodeLastRuneInString(p.Query)
		p.Query = p.Query[:len(p.Query)-size]
		p.Cursor = 0
	case "\x15":
		p.Query, p.Cursor = "", 0
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			p.Query += key
			p.Cursor = 0
		}
	}
	t.dirty = true
	if !save {
		t.mu.Unlock()
		return
	}
	var add, remove []string
	for _, name := range p.Options {
		switch {
		case p.Marked[name] && !p.Had[name]:
			add = append(add, name)
		case !p.Marked[name] && p.Had[name]:
			remove = append(remove, name)
		}
	}
	if len(add)+len(remove) == 0 {
		v.Picker = nil
		t.mu.Unlock()
		return
	}
	p.Saving = true
	edit := proto.GitHubIssueEditParams{Repo: t.repoOfLocked(v.Detail.Repo), Number: v.Detail.Number}
	if p.Kind == "labels" {
		edit.AddLabels, edit.RemoveLabels = add, remove
	} else {
		edit.AddAssignees, edit.RemoveAssignees = add, remove
	}
	kind := p.Kind
	t.mu.Unlock()
	t.wakeUp()
	go t.editIssue(edit, kind+" saved")
}

// nextIssueScope lists the next of what there is to list: in a folder of
// repositories, all of them, then each; in one repository, its next
// GitHub remote — upstream's issues, then origin's, for a fork.
func (t *tui) nextIssueScope() {
	t.mu.Lock()
	choices := t.issueState.choices
	if len(choices) == 0 {
		t.mu.Unlock()
		t.nextIssueRemote()
		return
	}
	at := 0
	for n, c := range choices {
		if c.Slug == t.issueState.repo {
			at = n + 1
		}
	}
	t.issueState.repo = ""
	if next := (at + 1) % (len(choices) + 1); next > 0 {
		t.issueState.repo = choices[next-1].Slug
	}
	t.mu.Unlock()
	t.setIssueScopeLocked()
}

// setIssueScope lists what issueState.repo says, from the top.
func (t *tui) setIssueScopeLocked() {
	t.mu.Lock()
	v := t.issues
	if v == nil {
		t.mu.Unlock()
		return
	}
	v.Cursor, v.Scroll = 0, 0
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	go t.loadIssues(seq)
}

// setIssueScopeTo lists one chip's scope: 0 is all, then each repository.
func (t *tui) setIssueScopeTo(n int) {
	t.mu.Lock()
	t.issueState.repo = ""
	if n > 0 && n <= len(t.issueState.choices) {
		t.issueState.repo = t.issueState.choices[n-1].Slug
	}
	t.mu.Unlock()
	t.setIssueScopeLocked()
}

// issueListError is why a list could not be made, and when the server is
// older than lists of a folder of repositories, what brings it along: a
// server from before them says a folder has no GitHub remote, which the
// owner read as tend not working.
func (t *tui) issueListError(err error) string {
	msg := ghError(err)
	if !t.serverHas(proto.FeatureGitHubFolders) {
		msg += " — this server is older than lists of several repositories; run " + handoffCommand(t.session)
	}
	return msg
}
