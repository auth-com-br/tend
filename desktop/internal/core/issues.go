package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/workon"
)

// The issues tab: the TUI's issues panel (cmd/tend/tui_issues.go and
// tui_pulls.go) natively — GitHub's or a GitLab's issues and pull requests
// of the project the pane in focus works in, through the server's github.*
// methods, so gh is asked on the machine the projects are on, as for the
// TUI. A search is GitHub's search syntax, typed after the preset. One
// issue opens with its body and thread; it can be commented on, closed or
// reopened, and worked on: a worktree on a branch named after it, with the
// agent started there (workon.Issue). A pull request opens with its checks
// and reviews, and can be merged, closed, reopened or made ready.

// issuesTab is what the session keeps of the issues tab.
type issuesTab struct {
	// pane is the pane whose project is listed, fixed when the tab first
	// opened, as the TUI fixes it when its panel opens; seq numbers each
	// ask, so an answer overtaken by a later one is dropped, and detailSeq
	// does the same for the one open.
	pane      uint64
	seq       int
	detailSeq int
	prs       bool
	filter    int
	query     string
	repo      string // one repository of a folder of several; "" is all
	remote    string
	listed    proto.GitHubIssuesResult
	issues    []proto.GitHubIssue
	pulls     []proto.GitHubPR
	issue     *proto.GitHubIssueDetail
	linked    []proto.GitHubPR
	pr        *proto.GitHubPRDetail
	reading   bool
	busy      bool
	message   string
}

// IssuesState is the issues tab as QML draws it.
type IssuesState struct {
	PullRequests bool         `json:"pullRequests"`
	Filters      []string     `json:"filters"`
	Filter       int          `json:"filter"`
	Query        string       `json:"query"`
	Repo         string       `json:"repo"`
	Remote       string       `json:"remote"`
	Remotes      []string     `json:"remotes"`
	Scopes       []ScopeChip  `json:"scopes"`
	Scope        string       `json:"scope"`
	Total        int          `json:"total"`
	Issues       []IssueRow   `json:"issues"`
	PRs          []PRRow      `json:"prs"`
	Issue        *IssueDetail `json:"issue,omitempty"`
	PR           *PRDetail    `json:"pr,omitempty"`
	Reading      bool         `json:"reading,omitempty"`
	Busy         bool         `json:"busy,omitempty"`
	Message      string       `json:"message,omitempty"`
	Agent        string       `json:"agent"`
}

// ScopeChip is one repository of a folder of several, for the chips over
// the list; an empty Slug is all of them.
type ScopeChip struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// IssueRow is an issue in the list.
type IssueRow struct {
	Repo      string   `json:"repo"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	Labels    []string `json:"labels"`
	Assignees []string `json:"assignees"`
	Comments  int      `json:"comments"`
	Age       string   `json:"age"`
	URL       string   `json:"url"`
}

// PRRow is a pull request in the list, its checks counted.
type PRRow struct {
	Repo    string   `json:"repo"`
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	State   string   `json:"state"`
	Draft   bool     `json:"draft"`
	Author  string   `json:"author"`
	Labels  []string `json:"labels"`
	Head    string   `json:"head"`
	Base    string   `json:"base"`
	Review  string   `json:"review"`
	Pass    int      `json:"pass"`
	Fail    int      `json:"fail"`
	Pending int      `json:"pending"`
	Age     string   `json:"age"`
	URL     string   `json:"url"`
}

// CommentRow is one comment of a thread.
type CommentRow struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Age    string `json:"age"`
}

// IssueDetail is an issue open: its body is markdown, for Qt to render.
type IssueDetail struct {
	IssueRow
	Body    string       `json:"body"`
	Created string       `json:"created"`
	Thread  []CommentRow `json:"thread"`
	Linked  []PRRow      `json:"linked"`
}

// PRDetail is a pull request open.
type PRDetail struct {
	PRRow
	Body      string              `json:"body"`
	Created   string              `json:"created"`
	Additions int                 `json:"additions"`
	Deletions int                 `json:"deletions"`
	Files     int                 `json:"files"`
	Mergeable string              `json:"mergeable"`
	Checks    []proto.GitHubCheck `json:"checks"`
	Thread    []CommentRow        `json:"thread"`
}

func unix(t int64) time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(t, 0)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func issueRow(x proto.GitHubIssue) IssueRow {
	return IssueRow{Repo: x.Repo, Number: x.Number, Title: x.Title, State: x.State, Author: x.Author,
		Labels: orEmpty(x.Labels), Assignees: orEmpty(x.Assignees), Comments: x.Comments, Age: ago(unix(x.Updated)), URL: x.URL}
}

func prRow(p proto.GitHubPR) PRRow {
	return PRRow{Repo: p.Repo, Number: p.Number, Title: p.Title, State: p.State, Draft: p.Draft, Author: p.Author,
		Labels: orEmpty(p.Labels), Head: p.Head, Base: p.Base, Review: p.Review,
		Pass: p.Pass, Fail: p.Fail, Pending: p.Pending, Age: ago(unix(p.Updated)), URL: p.URL}
}

func thread(cs []proto.GitHubComment) []CommentRow {
	out := []CommentRow{}
	for _, c := range cs {
		out = append(out, CommentRow{Author: c.Author, Body: c.Body, Age: ago(unix(c.Created))})
	}
	return out
}

// issuesStateLocked is the tab now. The caller holds the lock.
func (s *Session) issuesStateLocked() *IssuesState {
	t := &s.dock.issues
	st := &IssuesState{
		PullRequests: t.prs, Filters: github.FilterNames(t.prs), Filter: t.filter, Query: t.query,
		Repo: t.listed.Repo, Remote: t.listed.Remote, Remotes: orEmpty(t.listed.Remotes), Scope: t.repo,
		Total: t.listed.Total, Issues: []IssueRow{}, PRs: []PRRow{}, Scopes: []ScopeChip{},
		Reading: t.reading, Busy: t.busy, Message: t.message, Agent: s.agentForLocked(t.pane),
	}
	if t.listed.Multi {
		st.Scopes = append(st.Scopes, ScopeChip{Name: "all"})
		for _, c := range t.listed.Choices {
			st.Scopes = append(st.Scopes, ScopeChip{Slug: c.Slug, Name: c.Name})
		}
	}
	for _, x := range t.issues {
		st.Issues = append(st.Issues, issueRow(x))
	}
	for _, p := range t.pulls {
		st.PRs = append(st.PRs, prRow(p))
	}
	if t.prs {
		st.Total = len(t.pulls)
	}
	if d := t.issue; d != nil {
		st.Issue = &IssueDetail{IssueRow: issueRow(d.GitHubIssue), Body: d.Body, Created: ago(unix(d.Created)), Thread: thread(d.Thread), Linked: []PRRow{}}
		for _, p := range t.linked {
			st.Issue.Linked = append(st.Issue.Linked, prRow(p))
		}
	}
	if p := t.pr; p != nil {
		checks := p.Checks
		if checks == nil {
			checks = []proto.GitHubCheck{}
		}
		st.PR = &PRDetail{PRRow: prRow(p.GitHubPR), Body: p.Body, Created: ago(unix(p.Created)),
			Additions: p.Additions, Deletions: p.Deletions, Files: p.Files, Mergeable: p.Mergeable,
			Checks: checks, Thread: thread(p.Thread)}
	}
	return st
}

// repoOfLocked is the repository an issue or a pull request is in: its own,
// which in a folder of several is not the list's; else the list's.
func (t *issuesTab) repoOf(own string) string {
	if own != "" {
		return own
	}
	return t.listed.Repo
}

// dirOf is the checkout a repository is in: its own in a folder of several,
// else where the list was found from.
func (t *issuesTab) dirOf(repo string) string {
	for _, c := range t.listed.Choices {
		if c.Slug == repo {
			return c.Dir
		}
	}
	return t.listed.Dir
}

// loadIssues asks the server for the list as the tab now says, and keeps
// the answer if nothing was asked since.
func (s *Session) loadIssues() error {
	s.mu.Lock()
	t := &s.dock.issues
	if t.pane == 0 {
		t.pane = s.focus
	}
	t.seq++
	seq, c := t.seq, s.client
	params := proto.GitHubIssuesParams{Pane: t.pane, Remote: t.remote, Repo: t.repo, Filter: t.filter, Query: t.query}
	prs := t.prs
	t.busy = true
	s.mu.Unlock()
	s.stateChanged()
	defer func() {
		s.mu.Lock()
		if s.dock.issues.seq == seq {
			s.dock.issues.busy = false
		}
		s.mu.Unlock()
		s.stateChanged()
	}()

	method := proto.MethodGitHubIssues
	if prs {
		method = proto.MethodGitHubPRs
	}
	if !c.Supports(method) {
		return fmt.Errorf("this server is older than the issues tab: tend handoff moves it to this build")
	}
	var res proto.GitHubIssuesResult
	var pulls []proto.GitHubPR
	var err error
	if prs {
		var r proto.GitHubPRsResult
		r, err = c.GitHubPRs(params)
		res = proto.GitHubIssuesResult{Repo: r.Repo, Remote: r.Remote, Remotes: r.Remotes, Dir: r.Dir, Multi: r.Multi, Choices: r.Choices}
		pulls = r.PRs
	} else {
		res, err = c.GitHubIssues(params)
	}
	s.mu.Lock()
	if t.seq != seq {
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		t.issues, t.pulls = nil, nil
		s.mu.Unlock()
		return fmt.Errorf("%s", github.Said(err))
	}
	t.listed, t.issues, t.pulls = res, res.Issues, pulls
	s.mu.Unlock()
	// The checks come after the list, as in the TUI: asking for every
	// check of every pull request first would keep the list waiting.
	if prs && len(pulls) > 0 && c.Supports(proto.MethodGitHubPRChecks) {
		go s.loadPRChecks(seq, params)
	}
	return nil
}

// loadPRChecks fills in the checks of the list asked for as seq, when it is
// still the one shown. A failure leaves them blank: the list is there.
func (s *Session) loadPRChecks(seq int, params proto.GitHubIssuesParams) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	res, err := c.GitHubPRChecks(params)
	if err != nil {
		return
	}
	s.mu.Lock()
	t := &s.dock.issues
	if t.seq == seq {
		for i := range t.pulls {
			if n, ok := res.Checks[github.CheckKey(t.repoOf(t.pulls[i].Repo), t.pulls[i].Number)]; ok {
				t.pulls[i].Pass, t.pulls[i].Fail, t.pulls[i].Pending = n[0], n[1], n[2]
			}
		}
	}
	s.mu.Unlock()
	s.stateChanged()
}

// IssuesAction is one thing the issues tab asks for.
type IssuesAction struct {
	Do           string `json:"do"`
	PullRequests bool   `json:"pullRequests"`
	Filter       int    `json:"filter"`
	Query        string `json:"query"`
	Repo         string `json:"repo"`
	Remote       string `json:"remote"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
	Action       string `json:"action"`
	Method       string `json:"method"`
}

// Issues does what the issues tab asks.
func (s *Session) Issues(a IssuesAction) error {
	s.mu.Lock()
	t := &s.dock.issues
	c := s.client
	s.mu.Unlock()
	relist := func(change func()) error {
		s.mu.Lock()
		change()
		s.mu.Unlock()
		return s.loadIssues()
	}
	switch a.Do {
	case "mode":
		return relist(func() {
			t.prs, t.filter, t.issue, t.pr, t.message = a.PullRequests, 0, nil, nil, ""
		})
	case "filter":
		return relist(func() { t.filter = a.Filter })
	case "search":
		return relist(func() { t.query = a.Query })
	case "scope":
		return relist(func() { t.repo = a.Repo })
	case "remote":
		return relist(func() { t.remote, t.repo = a.Remote, "" })
	case "refresh":
		return s.loadIssues()
	case "open":
		return s.readIssue(a.Repo, a.Number)
	case "openpr":
		return s.readPR(a.Repo, a.Number)
	case "back":
		s.mu.Lock()
		if t.pr != nil {
			t.pr = nil
		} else {
			t.issue, t.linked = nil, nil
		}
		t.detailSeq++
		t.message = ""
		s.mu.Unlock()
		s.stateChanged()
		return nil
	case "comment":
		return s.commentIssue(a.Body)
	case "state":
		s.mu.Lock()
		var repo string
		var number int
		if d := t.issue; d != nil {
			repo, number = t.repoOf(d.Repo), d.Number
		}
		s.mu.Unlock()
		if number == 0 {
			return fmt.Errorf("no issue open")
		}
		if err := c.GitHubIssueState(repo, number, a.State, a.Reason); err != nil {
			return fmt.Errorf("%s", github.Said(err))
		}
		say := fmt.Sprintf("reopened #%d", number)
		if a.State == "closed" {
			say = fmt.Sprintf("closed #%d as %s", number, a.Reason)
		}
		s.issuesSay(say)
		_ = s.loadIssues()
		return s.readIssue(repo, number)
	case "create":
		return s.createIssue(a.Repo, a.Title, a.Body)
	case "pr":
		return s.prAction(a.Action, a.Method)
	case "work":
		return s.workOnIssue()
	case "worktree":
		s.mu.Lock()
		var head, dir string
		if p := t.pr; p != nil {
			head, dir = p.Head, t.dirOf(t.repoOf(p.Repo))
		}
		s.mu.Unlock()
		if dir == "" {
			return fmt.Errorf("no project folder known for this pull request")
		}
		ws, found, err := workon.GoToBranch(s.apiCaller(), dir, head)
		if err != nil {
			return err
		}
		if !found {
			s.issuesSay("no worktree here is on " + head)
			return nil
		}
		return s.goToStarted(workon.Started{Workspace: ws, Message: "worktree of " + head})
	}
	return fmt.Errorf("the issues tab asked for %q", a.Do)
}

// issuesSay puts a line under the tab.
func (s *Session) issuesSay(message string) {
	s.mu.Lock()
	s.dock.issues.message = message
	s.mu.Unlock()
	s.stateChanged()
}

// readIssue reads an issue whole, and then the pull requests it has.
func (s *Session) readIssue(repo string, number int) error {
	s.mu.Lock()
	t := &s.dock.issues
	repo = t.repoOf(repo)
	t.detailSeq++
	seq, c := t.detailSeq, s.client
	t.pr, t.reading = nil, true
	if t.issue == nil || t.issue.Number != number {
		t.issue, t.linked = &proto.GitHubIssueDetail{GitHubIssue: proto.GitHubIssue{Repo: repo, Number: number}}, nil
	}
	s.mu.Unlock()
	s.stateChanged()
	d, err := c.GitHubIssue(repo, number)
	s.mu.Lock()
	if t.detailSeq != seq {
		s.mu.Unlock()
		return nil
	}
	t.reading = false
	if err != nil {
		t.issue.Body = "could not read the issue: " + github.ErrorText(err)
		s.mu.Unlock()
		s.stateChanged()
		return nil
	}
	if d.Repo == "" {
		d.Repo = repo
	}
	t.issue = &d
	dir := t.dirOf(repo)
	s.mu.Unlock()
	s.stateChanged()
	if !c.Supports(proto.MethodGitHubIssuePRs) {
		return nil
	}
	prs, err := c.GitHubIssuePRs(repo, number, dir)
	s.mu.Lock()
	if err == nil && t.detailSeq == seq {
		t.linked = prs
	}
	s.mu.Unlock()
	s.stateChanged()
	return nil
}

// readPR reads a pull request whole, over the list or the issue it was
// opened from.
func (s *Session) readPR(repo string, number int) error {
	s.mu.Lock()
	t := &s.dock.issues
	repo = t.repoOf(repo)
	t.detailSeq++
	seq, c := t.detailSeq, s.client
	t.reading = true
	if t.pr == nil || t.pr.Number != number {
		t.pr = &proto.GitHubPRDetail{GitHubPR: proto.GitHubPR{Repo: repo, Number: number}}
	}
	s.mu.Unlock()
	s.stateChanged()
	d, err := c.GitHubPR(repo, number)
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.stateChanged()
	}()
	if t.detailSeq != seq {
		return nil
	}
	t.reading = false
	if err != nil {
		t.pr.Body = "could not read the pull request: " + github.ErrorText(err)
		return nil
	}
	if d.Repo == "" {
		d.Repo = repo
	}
	t.pr = &d
	return nil
}

// commentIssue writes a comment on the pull request open, else the issue
// open, and reads it again to show it.
func (s *Session) commentIssue(body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("a comment needs some text")
	}
	s.mu.Lock()
	t := &s.dock.issues
	c := s.client
	var repo string
	var number int
	onPR := t.pr != nil
	switch {
	case onPR:
		repo, number = t.repoOf(t.pr.Repo), t.pr.Number
	case t.issue != nil:
		repo, number = t.repoOf(t.issue.Repo), t.issue.Number
	}
	s.mu.Unlock()
	if number == 0 {
		return fmt.Errorf("nothing open to comment on")
	}
	var err error
	if onPR {
		err = c.GitHubPRAction(proto.GitHubPRActionParams{Repo: repo, Number: number, Action: "comment", Body: body})
	} else {
		err = c.GitHubIssueComment(repo, number, body)
	}
	if err != nil {
		return fmt.Errorf("%s", github.Said(err))
	}
	s.issuesSay(fmt.Sprintf("commented on #%d", number))
	if onPR {
		return s.readPR(repo, number)
	}
	return s.readIssue(repo, number)
}

// createIssue files an issue in repo — the one listed when none is named,
// or a folder's first — and opens it.
func (s *Session) createIssue(repo, title, body string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("a new issue needs a title")
	}
	s.mu.Lock()
	t := &s.dock.issues
	c := s.client
	if repo == "" {
		repo = t.listed.Repo
	}
	if repo == "" && len(t.listed.Choices) > 0 {
		repo = t.listed.Choices[0].Slug
	}
	s.mu.Unlock()
	if repo == "" {
		return fmt.Errorf("no repository to file it in")
	}
	created, err := c.GitHubIssueCreate(repo, title, body)
	if err != nil {
		return fmt.Errorf("%s", github.Said(err))
	}
	s.issuesSay(fmt.Sprintf("filed #%d · %s", created.Number, created.URL))
	_ = s.loadIssues()
	return s.readIssue(repo, created.Number)
}

// prAction does something to the pull request open, says what came of it,
// and reads it and the list again.
func (s *Session) prAction(action, method string) error {
	s.mu.Lock()
	t := &s.dock.issues
	c := s.client
	if t.pr == nil {
		s.mu.Unlock()
		return fmt.Errorf("no pull request open")
	}
	repo, number := t.repoOf(t.pr.Repo), t.pr.Number
	s.mu.Unlock()
	if err := c.GitHubPRAction(proto.GitHubPRActionParams{Repo: repo, Number: number, Action: action, Method: method}); err != nil {
		return fmt.Errorf("%s", github.Said(err))
	}
	switch action {
	case "merge":
		s.issuesSay(fmt.Sprintf("merged #%d (%s)", number, method))
	case "ready":
		s.issuesSay(fmt.Sprintf("#%d is ready for review", number))
	default:
		s.issuesSay(fmt.Sprintf("%sd #%d", action, number))
	}
	_ = s.loadIssues()
	return s.readPR(repo, number)
}

// workOnIssue starts the agent on the issue open in a worktree of its own,
// or goes to the one already made for it (workon.Issue, as the TUI's w).
func (s *Session) workOnIssue() error {
	s.mu.Lock()
	t := &s.dock.issues
	c := s.client
	if t.issue == nil {
		s.mu.Unlock()
		return fmt.Errorf("no issue open")
	}
	e := t.issue.GitHubIssue
	repo := t.repoOf(e.Repo)
	dir := t.dirOf(repo)
	agentName := s.agentForLocked(t.pane)
	prompt := s.cfg.IssuePrompt(e.URL, e.Number, e.Title, repo)
	t.message = fmt.Sprintf("making a worktree for #%d…", e.Number)
	s.mu.Unlock()
	s.stateChanged()
	if dir == "" {
		return fmt.Errorf("no project folder known for #%d", e.Number)
	}
	got, err := workon.Issue(s.apiCaller(), c, dir, e.Number, e.Title, agentName, prompt)
	if err != nil {
		s.issuesSay(err.Error())
		return err
	}
	s.issuesSay(got.Message)
	return s.goToStarted(got)
}
