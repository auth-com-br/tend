package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/keyring"
)

// GitLab (#9): the same list, issue, pull request — a merge request on
// GitLab — and actions as GitHub's, through GitLab's REST API (v4), for
// gitlab.com and a company's own GitLab alike. There is no CLI to lean on
// as gh is leaned on for GitHub, so tend asks the API itself, with a token
// when it has one: a public project is read without one, and writing, or
// reading a private project, needs one (GitLabToken).

// ErrGitLabToken is a GitLab that wants a token tend has not got.
var ErrGitLabToken = errors.New("GitLab wants a token for this: run `tend gitlab login` (or set GITLAB_TOKEN)")

// gitlabRemote reads host and path out of a remote on a GitLab: an https
// URL, git@host:path, or ssh://git@host[:port]/path, with or without .git.
// A host is taken as a GitLab when its name says so — gitlab.com, or
// gitlab.company.com — the way a company names its own.
var gitlabRemote = regexp.MustCompile(`^(?:https?://(?:[^@/]+@)?([^/:]+)(?::\d+)?/|git@([^:/]+):|ssh://git@([^/:]+)(?::\d+)?/)(.+?)(?:\.git)?/?$`)

// ParseGitLabRemote is the GitLab and project path a remote names, if it
// is on a GitLab. A path has a group and a name at least; subgroups are
// kept in it.
func ParseGitLabRemote(u string) (host, path string, ok bool) {
	m := gitlabRemote.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return "", "", false
	}
	host = m[1] + m[2] + m[3]
	path = m[4]
	if !strings.Contains(strings.ToLower(host), "gitlab") || !strings.Contains(path, "/") {
		return "", "", false
	}
	return strings.ToLower(host), path, true
}

// GitLabToken is the token for a GitLab host, or "" for none: the one the
// user gave tend for it (`tend gitlab login`), read from the settings each
// time so a login is used at once, from the keyring when it is kept there;
// else GITLAB_TOKEN, which GitLab's own CLI reads too. A test sets its own.
var GitLabToken = func(host string) string {
	if cfg, err := config.Load(); err == nil {
		if value := cfg.GitLab.Tokens[host]; value != "" {
			if name, ok := keyring.Name(value); ok {
				if secret, err := keyring.Get(name); err == nil {
					return secret
				}
			} else {
				return value
			}
		}
	}
	return os.Getenv("GITLAB_TOKEN")
}

// GitLabWhoAmI is the user a token signs in as on a host: what login checks
// before it keeps a token.
func GitLabWhoAmI(host, token string) (string, error) {
	var u struct {
		Username string `json:"username"`
	}
	if err := gitlabCallWith(host, token, "GET", "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Username, nil
}

// GitLabBase is the API's address on a host; a test points it at a
// stand-in.
var GitLabBase = func(host string) string { return "https://" + host + "/api/v4" }

// gitlabCall asks a GitLab's API, with the host's token, and decodes the
// answer into out.
func gitlabCall(host, method, path string, body, out any) error {
	return gitlabCallWith(host, GitLabToken(host), method, path, body, out)
}

// gitlabCallWith is gitlabCall with a token of the caller's.
func gitlabCallWith(host, token, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, GitLabBase(host)+path, rd)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s did not answer in %s", host, callTimeout)
		}
		return fmt.Errorf("%s did not answer: %w", host, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrGitLabToken
	case resp.StatusCode == http.StatusForbidden && token == "":
		return ErrGitLabToken
	case resp.StatusCode == http.StatusNotFound && token == "":
		// A private project is "not found" to someone not signed in.
		return fmt.Errorf("%s: not found, or private — %w", host, ErrGitLabToken)
	case resp.StatusCode >= 300:
		var e struct {
			Message any    `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error
		if msg == "" && e.Message != nil {
			msg = fmt.Sprint(e.Message)
		}
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%s answered: %s", host, msg)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("reading %s's answer: %w", host, err)
	}
	return nil
}

// project is a repository's path in the API, its group and name as one
// escaped id.
func project(r Repo) string { return "/projects/" + url.PathEscape(r.Owner+"/"+r.Name) }

// gitlabMe is the signed-in user's name on a host, which "mine" and "needs
// my review" ask for.
func gitlabMe(host string) (string, error) {
	if GitLabToken(host) == "" {
		return "", ErrGitLabToken
	}
	var u struct {
		Username string `json:"username"`
	}
	if err := gitlabCall(host, "GET", "/user", nil, &u); err != nil {
		return "", err
	}
	return u.Username, nil
}

type glUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type glIssue struct {
	IID       int       `json:"iid"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	WebURL    string    `json:"web_url"`
	Author    glUser    `json:"author"`
	Labels    []string  `json:"labels"`
	Assignees []glUser  `json:"assignees"`
	Notes     int       `json:"user_notes_count"`
	Updated   time.Time `json:"updated_at"`
	Created   time.Time `json:"created_at"`
	Body      string    `json:"description"`
}

// glState is GitLab's state in GitHub's words: opened is open.
func glState(s string) string {
	if s == "opened" {
		return "open"
	}
	return s
}

func (w glIssue) issue(r Repo) Issue {
	i := Issue{Repo: r.Slug(), Number: w.IID, Title: w.Title, State: glState(w.State), Author: w.Author.Username,
		Labels: w.Labels, Comments: w.Notes, Updated: w.Updated, URL: w.WebURL}
	for _, a := range w.Assignees {
		i.Assignees = append(i.Assignees, a.Username)
	}
	return i
}

// glListIssues is a project's issues for a preset and what was typed, the
// last updated first.
func glListIssues(r Repo, filter Filter, typed string) ([]Issue, error) {
	q := url.Values{"order_by": {"updated_at"}, "sort": {"desc"}, "per_page": {strconv.Itoa(listLimit)}}
	switch filter {
	case FilterClosed:
		q.Set("state", "closed")
	default:
		q.Set("state", "opened")
	}
	switch filter {
	case FilterMine:
		q.Set("scope", "assigned_to_me")
	case FilterCreated:
		q.Set("scope", "created_by_me")
	}
	if (filter == FilterMine || filter == FilterCreated) && GitLabToken(r.Host) == "" {
		return nil, ErrGitLabToken
	}
	if typed = strings.TrimSpace(typed); typed != "" {
		q.Set("search", typed)
	}
	var wire []glIssue
	if err := gitlabCall(r.Host, "GET", project(r)+"/issues?"+q.Encode(), nil, &wire); err != nil {
		return nil, err
	}
	out := make([]Issue, 0, len(wire))
	for _, w := range wire {
		out = append(out, w.issue(r))
	}
	return out, nil
}

type glNote struct {
	Body    string    `json:"body"`
	Author  glUser    `json:"author"`
	Created time.Time `json:"created_at"`
	// System marks what GitLab writes itself ("changed the title"), which
	// is not the conversation.
	System bool `json:"system"`
}

// glNotes is the conversation on an issue or a merge request, oldest first.
// gitlab.com shows it only to someone signed in, where it shows the issue
// itself to anyone: without a token, the thread says so rather than the
// issue failing to open.
func glNotes(r Repo, kind string, iid int) ([]Comment, error) {
	var notes []glNote
	path := fmt.Sprintf("%s/%s/%d/notes?sort=asc&order_by=created_at&per_page=100", project(r), kind, iid)
	if err := gitlabCall(r.Host, "GET", path, nil, &notes); err != nil {
		if errors.Is(err, ErrGitLabToken) && GitLabToken(r.Host) == "" {
			return []Comment{{Author: "tend", Body: "GitLab shows the comments to someone signed in: run `tend gitlab login` to see them here."}}, nil
		}
		return nil, err
	}
	var out []Comment
	for _, n := range notes {
		if !n.System {
			out = append(out, Comment{Author: n.Author.Username, Body: n.Body, Created: n.Created})
		}
	}
	return out, nil
}

func glGet(r Repo, iid int) (Detail, error) {
	var w glIssue
	if err := gitlabCall(r.Host, "GET", fmt.Sprintf("%s/issues/%d", project(r), iid), nil, &w); err != nil {
		return Detail{}, err
	}
	thread, err := glNotes(r, "issues", iid)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Issue: w.issue(r), Body: w.Body, Created: w.Created, Thread: thread}, nil
}

func glComment(r Repo, kind string, iid int, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment needs some text")
	}
	return gitlabCall(r.Host, "POST", fmt.Sprintf("%s/%s/%d/notes", project(r), kind, iid), map[string]string{"body": body}, nil)
}

// glSetState closes or reopens an issue or a merge request. GitLab keeps
// no reason for closing, so none is sent.
func glSetState(r Repo, kind string, iid int, event string) error {
	return gitlabCall(r.Host, "PUT", fmt.Sprintf("%s/%s/%d", project(r), kind, iid), map[string]string{"state_event": event}, nil)
}

func glCreate(r Repo, title, body string) (int, string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, "", errors.New("an issue needs a title")
	}
	var w glIssue
	if err := gitlabCall(r.Host, "POST", project(r)+"/issues", map[string]string{"title": title, "description": body}, &w); err != nil {
		return 0, "", err
	}
	return w.IID, w.WebURL, nil
}

func glLabels(r Repo) ([]string, error) {
	var labels []struct {
		Name string `json:"name"`
	}
	if err := gitlabCall(r.Host, "GET", project(r)+"/labels?per_page=100", nil, &labels); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.Name)
	}
	return out, nil
}

// glMembers is who can be assigned in a project, inherited members too.
func glMembers(r Repo) ([]glUser, error) {
	var members []glUser
	err := gitlabCall(r.Host, "GET", project(r)+"/members/all?per_page=100", nil, &members)
	return members, err
}

func glAssignable(r Repo) ([]string, error) {
	members, err := glMembers(r)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.Username)
	}
	return out, nil
}

// glEditIssue changes only what the edit names, as GitHub's does: labels
// are added and taken off by name, which GitLab takes as such; assignees
// are set as a whole, by id, so they are worked out from the issue's
// current ones.
func glEditIssue(r Repo, iid int, e Edit) error {
	body := map[string]any{}
	if t := strings.TrimSpace(e.Title); t != "" {
		body["title"] = t
	}
	if len(e.AddLabels) > 0 {
		body["add_labels"] = strings.Join(e.AddLabels, ",")
	}
	if len(e.RemoveLabels) > 0 {
		body["remove_labels"] = strings.Join(e.RemoveLabels, ",")
	}
	if len(e.AddAssignees) > 0 || len(e.RemoveAssignees) > 0 {
		var w glIssue
		if err := gitlabCall(r.Host, "GET", fmt.Sprintf("%s/issues/%d", project(r), iid), nil, &w); err != nil {
			return err
		}
		members, err := glMembers(r)
		if err != nil {
			return err
		}
		ids := map[string]int{}
		for _, m := range append(members, w.Assignees...) {
			ids[m.Username] = m.ID
		}
		keep := map[string]bool{}
		for _, a := range w.Assignees {
			keep[a.Username] = true
		}
		for _, n := range e.RemoveAssignees {
			delete(keep, n)
		}
		for _, n := range e.AddAssignees {
			if _, ok := ids[n]; !ok {
				return fmt.Errorf("%s is not a member of %s", n, r.Slug())
			}
			keep[n] = true
		}
		list := []int{}
		for n := range keep {
			list = append(list, ids[n])
		}
		sort.Ints(list)
		body["assignee_ids"] = list
	}
	if len(body) == 0 {
		return nil
	}
	return gitlabCall(r.Host, "PUT", fmt.Sprintf("%s/issues/%d", project(r), iid), body, nil)
}

type glMR struct {
	IID       int       `json:"iid"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	WIP       bool      `json:"work_in_progress"`
	Author    glUser    `json:"author"`
	Labels    []string  `json:"labels"`
	Source    string    `json:"source_branch"`
	Target    string    `json:"target_branch"`
	Updated   time.Time `json:"updated_at"`
	Created   time.Time `json:"created_at"`
	WebURL    string    `json:"web_url"`
	Body      string    `json:"description"`
	Changes   string    `json:"changes_count"`
	Conflicts bool      `json:"has_conflicts"`
	Status    string    `json:"detailed_merge_status"`
	Pipeline  *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
}

// glCheck is a pipeline's status as a check: GitLab has one pipeline for a
// merge request where GitHub has many checks.
func glCheck(status string) Check {
	switch status {
	case "success":
		return Check{"pipeline", CheckPass}
	case "failed":
		return Check{"pipeline", CheckFail}
	case "skipped", "manual", "canceled":
		return Check{"pipeline", CheckSkipped}
	}
	return Check{"pipeline", CheckPending}
}

func (w glMR) pr(r Repo) PR {
	p := PR{Repo: r.Slug(), Number: w.IID, Title: w.Title, State: glState(w.State), Draft: w.Draft || w.WIP,
		Author: w.Author.Username, Labels: w.Labels, Head: w.Source, Base: w.Target, Updated: w.Updated, URL: w.WebURL}
	if w.Pipeline != nil {
		switch glCheck(w.Pipeline.Status).State {
		case CheckPass:
			p.Checks.Pass++
		case CheckFail:
			p.Checks.Fail++
		case CheckPending:
			p.Checks.Pending++
		}
	}
	return p
}

// glListMRs is a project's merge requests for a preset: open, the user's,
// those asking for the user's review, and the closed and merged.
func glListMRs(r Repo, filter Filter, typed string) ([]PR, error) {
	q := url.Values{"order_by": {"updated_at"}, "sort": {"desc"}, "per_page": {strconv.Itoa(listLimit)}, "state": {"opened"}}
	switch filter {
	case PRFilterMine:
		if GitLabToken(r.Host) == "" {
			return nil, ErrGitLabToken
		}
		q.Set("scope", "created_by_me")
	case PRFilterReview:
		me, err := gitlabMe(r.Host)
		if err != nil {
			return nil, err
		}
		q.Set("reviewer_username", me)
	case PRFilterClosed:
		q.Set("state", "all") // the closed and the merged; the open are dropped below
	}
	if typed = strings.TrimSpace(typed); typed != "" {
		q.Set("search", typed)
	}
	var wire []glMR
	if err := gitlabCall(r.Host, "GET", project(r)+"/merge_requests?"+q.Encode(), nil, &wire); err != nil {
		return nil, err
	}
	out := make([]PR, 0, len(wire))
	for _, w := range wire {
		if filter == PRFilterClosed && w.State == "opened" {
			continue
		}
		out = append(out, w.pr(r))
	}
	return out, nil
}

func glGetMR(r Repo, iid int) (PRDetail, error) {
	var w glMR
	if err := gitlabCall(r.Host, "GET", fmt.Sprintf("%s/merge_requests/%d", project(r), iid), nil, &w); err != nil {
		return PRDetail{}, err
	}
	thread, err := glNotes(r, "merge_requests", iid)
	if err != nil {
		return PRDetail{}, err
	}
	d := PRDetail{PR: w.pr(r), Body: w.Body, Created: w.Created, Thread: thread, Mergeable: "UNKNOWN"}
	d.Files, _ = strconv.Atoi(strings.TrimSuffix(w.Changes, "+"))
	switch {
	case w.Conflicts:
		d.Mergeable = "CONFLICTING"
	case w.Status == "mergeable":
		d.Mergeable = "MERGEABLE"
	}
	if w.Pipeline != nil {
		d.CheckList = []Check{glCheck(w.Pipeline.Status)}
	}
	return d, nil
}

// glMerge merges a merge request, squashed when asked. How GitLab merges
// otherwise — a merge commit, or fast-forward after a rebase — is the
// project's own setting, so "rebase" is not asked for here.
func glMerge(r Repo, iid int, method string) error {
	switch method {
	case MergeSquash, MergeMerge:
	case MergeRebase:
		return errors.New("GitLab merges the way the project is set to: squash or merge here")
	default:
		return fmt.Errorf("a merge request is merged by squash or merge, not %q", method)
	}
	return gitlabCall(r.Host, "PUT", fmt.Sprintf("%s/merge_requests/%d/merge", project(r), iid),
		map[string]bool{"squash": method == MergeSquash}, nil)
}

// draftPrefixes are how GitLab marks a merge request a draft: in its title.
var draftPrefixes = regexp.MustCompile(`(?i)^\s*(\[draft\]|\(draft\)|draft:|\[wip\]|wip:)\s*`)

// glReady marks a draft ready, which on GitLab is its title without the
// draft mark.
func glReady(r Repo, iid int) error {
	var w glMR
	if err := gitlabCall(r.Host, "GET", fmt.Sprintf("%s/merge_requests/%d", project(r), iid), nil, &w); err != nil {
		return err
	}
	title := draftPrefixes.ReplaceAllString(w.Title, "")
	if title == w.Title {
		return nil
	}
	return gitlabCall(r.Host, "PUT", fmt.Sprintf("%s/merge_requests/%d", project(r), iid), map[string]string{"title": title}, nil)
}

// glMRsForIssue is the merge requests an issue has: those GitLab relates to
// it, and those on the branches made for it.
func glMRsForIssue(r Repo, iid int, branches []string) ([]PR, error) {
	var related []glMR
	if err := gitlabCall(r.Host, "GET", fmt.Sprintf("%s/issues/%d/related_merge_requests", project(r), iid), nil, &related); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []PR
	add := func(ws []glMR) {
		for _, w := range ws {
			if !seen[w.IID] {
				seen[w.IID] = true
				out = append(out, w.pr(r))
			}
		}
	}
	add(related)
	for _, b := range branches {
		var mrs []glMR
		q := url.Values{"source_branch": {b}, "state": {"all"}}
		if err := gitlabCall(r.Host, "GET", project(r)+"/merge_requests?"+q.Encode(), nil, &mrs); err != nil {
			return nil, err
		}
		add(mrs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })
	return out, nil
}

// splitRepos is a list's repositories by where they are.
func splitRepos(repos []Repo) (hub, lab []Repo) {
	for _, r := range repos {
		if r.GitLab() {
			lab = append(lab, r)
		} else {
			hub = append(hub, r)
		}
	}
	return hub, lab
}

// glEachIssues asks every GitLab repository at once.
func glEachIssues(repos []Repo, ask func(Repo) ([]Issue, error)) ([]Issue, error) {
	answers := make([][]Issue, len(repos))
	errs := make([]error, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			answers[i], errs[i] = ask(r)
		}()
	}
	wg.Wait()
	var out []Issue
	for i := range repos {
		if errs[i] != nil {
			return nil, errs[i]
		}
		out = append(out, answers[i]...)
	}
	return out, nil
}
