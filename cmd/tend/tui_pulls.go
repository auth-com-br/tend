package main

import (
	"fmt"
	"time"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// Pull requests in the issues panel (internal/ui/pulls.go): the other of
// its two lists, after Orca's, and one pull request whole, with what can be
// done to it — merged, commented, closed, made ready — and the worktree its
// branch is checked out in, gone to. An issue shows the pull requests it
// has, and p opens the first.

// setIssueMode shows the pull requests' list, or the issues'.
func (t *tui) setIssueMode(pullRequests bool) {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.PullRequests == pullRequests || v.Detail != nil || v.PR != nil {
		t.mu.Unlock()
		return
	}
	v.PullRequests = pullRequests
	v.Filters = issueFilterNames(pullRequests)
	v.Filter, v.Cursor, v.Scroll = 0, 0, 0
	v.Message = ""
	seq := t.askIssuesLocked()
	t.dirty = true
	t.mu.Unlock()
	go t.loadIssues(seq)
}

// loadPRs is loadIssues for the pull requests' list.
func (t *tui) loadPRs(seq int, params proto.GitHubIssuesParams) {
	if !t.client.Supports(proto.MethodGitHubPRs) {
		t.showIssuesError(seq, "this server is older than pull requests in the issues panel; "+handoffCommand(t.session)+" moves it to this build")
		return
	}
	res, err := t.client.GitHubPRs(params)
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v := t.issues
	if v == nil || seq != t.issueState.seq || !v.PullRequests {
		return
	}
	v.Loading, v.Now = false, time.Now()
	if err != nil {
		v.Error, v.PRs = t.issueListError(err), nil
		return
	}
	v.Error = ""
	v.Repo, v.Remote, v.Remotes = res.Repo, res.Remote, res.Remotes
	t.takeChoicesLocked(res.Multi, res.Choices, res.Repo)
	var at int
	if v.Cursor < len(v.PRs) {
		at = v.PRs[v.Cursor].Number
	}
	v.PRs, v.Cursor = v.PRs[:0], 0
	for i, p := range res.PRs {
		v.PRs = append(v.PRs, prEntry(p))
		if p.Number == at {
			v.Cursor = i
		}
	}
	v.Total = len(v.PRs)
	v.Scroll = ui.IssuesScrollFor(v, t.cols, t.rows)
	v.Dir = res.Dir
	t.issueState.dir = res.Dir
	if len(v.PRs) > 0 && t.client.Supports(proto.MethodGitHubPRChecks) {
		go t.loadPRChecks(seq, params)
	}
}

// loadPRChecks fills in the checks of the list asked for as seq, when it
// is still the one shown. A failure leaves the column empty: the list is
// what was asked for, and it is there.
func (t *tui) loadPRChecks(seq int, params proto.GitHubIssuesParams) {
	res, err := t.client.GitHubPRChecks(params)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v := t.issues
	if v == nil || seq != t.issueState.seq || !v.PullRequests {
		return
	}
	for i := range v.PRs {
		if c, ok := res.Checks[github.CheckKey(t.repoOfLocked(v.PRs[i].Repo), v.PRs[i].Number)]; ok {
			v.PRs[i].Pass, v.PRs[i].Fail, v.PRs[i].Pending = c[0], c[1], c[2]
		}
	}
}

func prEntry(p proto.GitHubPR) ui.PREntry {
	return ui.PREntry{
		Repo: p.Repo, Number: p.Number, Title: p.Title, State: p.State, Draft: p.Draft, Author: p.Author,
		Labels: p.Labels, Head: p.Head, Base: p.Base, Review: p.Review,
		Pass: p.Pass, Fail: p.Fail, Pending: p.Pending, Updated: time.Unix(p.Updated, 0), URL: p.URL,
	}
}

func (t *tui) prOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.issues != nil && t.issues.PR != nil
}

// showPR reads a pull request whole and shows it over the list, or over
// the issue it was opened from; reload keeps what is shown until the
// answer replaces it.
func (t *tui) showPR(p ui.PREntry, reload bool) {
	t.mu.Lock()
	v := t.issues
	if v == nil {
		t.mu.Unlock()
		return
	}
	if !reload || v.PR == nil || v.PR.Number != p.Number {
		v.PR = &ui.PRDetailView{PREntry: p, Loading: true}
		v.Message = p.URL
	}
	t.issueState.detailSeq++
	seq, repo := t.issueState.detailSeq, t.repoOfLocked(p.Repo)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		d, err := t.client.GitHubPR(repo, p.Number)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.issues
		if v == nil || v.PR == nil || seq != t.issueState.detailSeq {
			return
		}
		pr := v.PR
		pr.Loading = false
		if err != nil {
			pr.Body = "could not read the pull request: " + ghError(err)
			return
		}
		pr.PREntry = prEntry(d.GitHubPR)
		pr.Body, pr.Created = d.Body, time.Unix(d.Created, 0)
		pr.Additions, pr.Deletions, pr.Files, pr.Mergeable = d.Additions, d.Deletions, d.Files, d.Mergeable
		pr.Checks = pr.Checks[:0]
		for _, c := range d.Checks {
			pr.Checks = append(pr.Checks, ui.PRCheck{Name: c.Name, State: c.State})
		}
		pr.Thread = pr.Thread[:0]
		for _, c := range d.Thread {
			pr.Thread = append(pr.Thread, ui.IssueComment{Author: c.Author, Body: c.Body, Created: time.Unix(c.Created, 0)})
		}
	}()
}

func (t *tui) reloadPR() {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.PR == nil {
		t.mu.Unlock()
		return
	}
	p := v.PR.PREntry
	t.mu.Unlock()
	t.showPR(p, true)
}

// prDetailKey is a key over an open pull request.
func (t *tui) prDetailKey(key string) {
	switch key {
	case "\x1b", "q":
		// Back to what it was opened from: the list, or the issue.
		t.mu.Lock()
		if v := t.issues; v != nil {
			v.PR = nil
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
		t.askPRState()
	case "m":
		t.askPRMerge()
	case "r":
		t.readyPR()
	case "w":
		t.goToPRWorktree()
	}
}

// askPRMerge asks how to merge the open pull request.
func (t *tui) askPRMerge() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if v := t.issues; v != nil && v.PR != nil && !v.PR.Loading && v.PR.State == "open" {
		v.Confirm = "merge"
		t.dirty = true
	}
}

// askPRState asks before closing the open pull request, or opening it
// again. A merged one is neither.
func (t *tui) askPRState() {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.issues
	if v == nil || v.PR == nil || v.PR.Loading {
		return
	}
	switch v.PR.State {
	case "open":
		v.Confirm = "closepr"
	case "closed":
		v.Confirm = "reopenpr"
	}
	t.dirty = true
}

// prConfirmKey answers a question about the open pull request.
func (t *tui) prConfirmKey(asked, key string) {
	action, method := "", ""
	switch {
	case asked == "merge" && key == "s":
		action, method = "merge", github.MergeSquash
	case asked == "merge" && key == "m":
		action, method = "merge", github.MergeMerge
	case asked == "merge" && key == "r":
		action, method = "merge", github.MergeRebase
	case asked == "closepr" && (key == "\r" || key == "\n"):
		action = "close"
	case asked == "reopenpr" && (key == "\r" || key == "\n"):
		action = "reopen"
	}
	if action != "" {
		t.prAction(action, method)
	}
}

// readyPR marks the open draft ready for review.
func (t *tui) readyPR() {
	t.mu.Lock()
	ok := t.issues != nil && t.issues.PR != nil && t.issues.PR.Draft && t.issues.PR.State == "open"
	t.mu.Unlock()
	if ok {
		t.prAction("ready", "")
	}
}

// prAction does something to the open pull request, says what came of it,
// and reads it and the list again.
func (t *tui) prAction(action, method string) {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.PR == nil {
		t.mu.Unlock()
		return
	}
	repo, number := t.repoOfLocked(v.PR.Repo), v.PR.Number
	v.Message = "asking…"
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		err := t.client.GitHubPRAction(proto.GitHubPRActionParams{Repo: repo, Number: number, Action: action, Method: method})
		t.mu.Lock()
		if v := t.issues; v != nil {
			switch {
			case err != nil:
				v.Message = forgeSaid(err)
			case action == "merge":
				v.Message = fmt.Sprintf("merged #%d (%s)", number, method)
			case action == "ready":
				v.Message = fmt.Sprintf("#%d is ready for review", number)
			default:
				v.Message = fmt.Sprintf("%sd #%d", action, number)
			}
		}
		seq := t.askIssuesLocked()
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		if err == nil {
			t.reloadPR()
			t.loadIssues(seq)
		}
	}()
}

// loadLinkedPRs finds the pull requests an issue has, once it is shown:
// the ones GitHub says close it, and the ones on the project's branches
// made for it.
func (t *tui) loadLinkedPRs(seq int, repo string, number int) {
	if !t.client.Supports(proto.MethodGitHubIssuePRs) {
		return
	}
	t.mu.Lock()
	dir := t.dirOfLocked(repo)
	t.mu.Unlock()
	prs, err := t.client.GitHubIssuePRs(repo, number, dir)
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	v := t.issues
	if err != nil || v == nil || v.Detail == nil || v.Detail.Number != number || seq != t.issueState.detailSeq {
		return
	}
	v.Detail.Linked = v.Detail.Linked[:0]
	for _, p := range prs {
		v.Detail.Linked = append(v.Detail.Linked, prEntry(p))
	}
}

// openLinkedPR opens the first pull request of the issue open, over it.
func (t *tui) openLinkedPR() {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.Detail == nil || len(v.Detail.Linked) == 0 {
		if v != nil && v.Detail != nil {
			v.Message = fmt.Sprintf("#%d has no pull request yet", v.Detail.Number)
			t.dirty = true
		}
		t.mu.Unlock()
		return
	}
	p := v.Detail.Linked[0]
	t.mu.Unlock()
	t.showPR(p, false)
}

// goToPRWorktree goes to the space of the worktree the open pull request's
// branch is checked out in — the one an issue's work was started in — when
// the project has one.
func (t *tui) goToPRWorktree() {
	t.mu.Lock()
	v := t.issues
	if v == nil || v.PR == nil {
		t.mu.Unlock()
		return
	}
	head, dir, session := v.PR.Head, t.dirOfLocked(t.repoOfLocked(v.PR.Repo)), t.session
	if dir == "" {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	go func() {
		message := "no worktree here is on " + head
		list, err := apiCall(session, api.MethodWorktreeList, map[string]any{"cwd": dir}, true)
		if err == nil {
			trees, _ := list["worktrees"].([]any)
			for _, raw := range trees {
				wt, _ := raw.(map[string]any)
				if branch, _ := wt["branch"].(string); branch != head {
					continue
				}
				path, _ := wt["path"].(string)
				opened, err := apiCall(session, api.MethodWorktreeOpen, map[string]any{"cwd": dir, "path": path}, true)
				if err != nil {
					message = err.Error()
					break
				}
				t.closeIssues()
				t.mu.Lock()
				t.rememberFocusLocked()
				t.workspace, t.tab, t.focus, t.zoom = workspaceOf(opened), 0, 0, false
				t.mu.Unlock()
				t.setMessage("worktree of "+head, false)
				if err := t.refresh(); err != nil {
					t.setMessage(err.Error(), true)
				}
				return
			}
		} else {
			message = err.Error()
		}
		t.mu.Lock()
		if v := t.issues; v != nil {
			v.Message = message
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
}
