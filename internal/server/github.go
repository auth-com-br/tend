package server

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
)

// GitHubIssues is github.issues: the issues of the repository the project a
// pane is working in is on — the directory a new tab from it would start in
// (followDir) — or, with no pane, the server's own directory.
func (s *Server) GitHubIssues(p proto.GitHubIssuesParams) (proto.GitHubIssuesResult, error) {
	out := proto.GitHubIssuesResult{Issues: []proto.GitHubIssue{}}
	l, err := s.githubList(p)
	out.Dir, out.Remotes, out.Multi, out.Choices = l.dir, l.remotes, l.multi, l.choices
	if err != nil {
		return out, err
	}
	out.Repo, out.Remote = l.repo, l.remote
	filter := github.FilterOpen
	if p.Filter >= 0 && p.Filter < len(github.Filters) {
		filter = github.Filters[p.Filter]
	}
	issues, total, err := github.List(l.repos, filter, p.Query)
	if err != nil {
		return out, err
	}
	out.Total = total
	for _, i := range issues {
		out.Issues = append(out.Issues, wireIssue(i))
	}
	return out, nil
}

// repoOf reads owner/name, and with number an issue's number too.
func repoOf(p proto.GitHubIssueParams, number bool) (github.Repo, error) {
	repo, ok := github.ParseSlug(p.Repo)
	if !ok || (number && p.Number <= 0) {
		return github.Repo{}, fmt.Errorf("an issue is named by its repository and a number, not %q #%d", p.Repo, p.Number)
	}
	return repo, nil
}

// GitHubIssueWrite is what the panel writes to GitHub: a comment, an issue
// closed or opened again, a new issue.
func (s *Server) GitHubIssueWrite(method string, p proto.GitHubIssueParams) (any, error) {
	repo, err := repoOf(p, method != proto.MethodGitHubIssueCreate)
	if err != nil {
		return nil, err
	}
	switch method {
	case proto.MethodGitHubIssueComment:
		return struct{}{}, github.AddComment(repo, p.Number, p.Body)
	case proto.MethodGitHubIssueState:
		switch p.State {
		case "closed":
			return struct{}{}, github.Close(repo, p.Number, p.Reason)
		case "open":
			return struct{}{}, github.Reopen(repo, p.Number)
		}
		return nil, fmt.Errorf("an issue is open or closed, not %q", p.State)
	}
	n, url, err := github.Create(repo, p.Title, p.Body)
	if err != nil {
		return nil, err
	}
	return proto.GitHubIssueCreated{Number: n, URL: url}, nil
}

// GitHubIssue is github.issue: one issue with its thread.
func (s *Server) GitHubIssue(p proto.GitHubIssueParams) (proto.GitHubIssueDetail, error) {
	repo, err := repoOf(p, true)
	if err != nil {
		return proto.GitHubIssueDetail{}, err
	}
	d, err := github.Get(repo, p.Number)
	if err != nil {
		return proto.GitHubIssueDetail{}, err
	}
	out := proto.GitHubIssueDetail{GitHubIssue: wireIssue(d.Issue), Body: d.Body, Created: d.Created.Unix()}
	for _, c := range d.Thread {
		out.Thread = append(out.Thread, proto.GitHubComment{Author: c.Author, Body: c.Body, Created: c.Created.Unix()})
	}
	return out, nil
}

func wireIssue(i github.Issue) proto.GitHubIssue {
	return proto.GitHubIssue{
		Repo: i.Repo, Number: i.Number, Title: i.Title, State: i.State, Author: i.Author,
		Labels: i.Labels, Assignees: i.Assignees, Comments: i.Comments,
		Updated: i.Updated.Unix(), URL: i.URL,
	}
}

// githubList is what a list is of: the repository the project a pane is
// working in is on — the directory a new tab from it would start in
// (followDir), or with no pane the server's own — through the remote
// asked for, else the first (upstream before origin); or, for a folder of
// repositories, the one asked for of them, else all.
type githubList struct {
	dir     string
	repos   []github.Repo
	repo    string // owner/name listed; empty for all of a folder
	remote  string
	remotes []string
	multi   bool
	choices []proto.GitHubRepoChoice
}

func (s *Server) githubList(p proto.GitHubIssuesParams) (githubList, error) {
	spec := PaneSpec{}
	s.followDir(&spec, session.PaneID(p.Pane))
	l := githubList{dir: spec.Dir}
	if l.dir == "" {
		l.dir = s.cfg.Dir
	}
	scope, err := github.ScopeFor(l.dir)
	if err != nil {
		return l, err
	}
	if scope.Multi {
		l.multi = true
		for _, r := range scope.Repos {
			l.choices = append(l.choices, proto.GitHubRepoChoice{Slug: r.Slug(), Name: r.Name, Dir: r.Dir})
			if r.Slug() == p.Repo {
				l.repos, l.repo = []github.Repo{r}, r.Slug()
			}
		}
		if l.repos == nil {
			l.repos = scope.Repos
		}
		return l, nil
	}
	repo := scope.Repos[0]
	for _, r := range scope.Repos {
		l.remotes = append(l.remotes, r.Remote)
		if r.Remote == p.Remote {
			repo = r
		}
	}
	l.repos, l.repo, l.remote = []github.Repo{repo}, repo.Slug(), repo.Remote
	return l, nil
}

// GitHubPRs is github.prs: the pull requests of the repository a pane's
// project is on, as GitHubIssues lists its issues.
func (s *Server) GitHubPRs(p proto.GitHubIssuesParams) (proto.GitHubPRsResult, error) {
	out := proto.GitHubPRsResult{PRs: []proto.GitHubPR{}}
	l, err := s.githubList(p)
	out.Dir, out.Remotes, out.Multi, out.Choices = l.dir, l.remotes, l.multi, l.choices
	if err != nil {
		return out, err
	}
	out.Repo, out.Remote = l.repo, l.remote
	filter := github.PRFilterOpen
	if p.Filter >= 0 && p.Filter < len(github.PRFilters) {
		filter = github.PRFilters[p.Filter]
	}
	prs, err := github.ListPRs(l.repos, filter, p.Query)
	if err != nil {
		return out, err
	}
	for _, pr := range prs {
		out.PRs = append(out.PRs, wirePR(pr))
	}
	return out, nil
}

// GitHubPR is github.pr: one pull request whole.
func (s *Server) GitHubPR(p proto.GitHubIssueParams) (proto.GitHubPRDetail, error) {
	repo, err := repoOf(p, true)
	if err != nil {
		return proto.GitHubPRDetail{}, err
	}
	d, err := github.GetPR(repo, p.Number)
	if err != nil {
		return proto.GitHubPRDetail{}, err
	}
	out := proto.GitHubPRDetail{GitHubPR: wirePR(d.PR), Body: d.Body, Created: d.Created.Unix(),
		Additions: d.Additions, Deletions: d.Deletions, Files: d.Files, Mergeable: d.Mergeable}
	for _, c := range d.CheckList {
		out.Checks = append(out.Checks, proto.GitHubCheck{Name: c.Name, State: c.State})
	}
	for _, c := range d.Thread {
		out.Thread = append(out.Thread, proto.GitHubComment{Author: c.Author, Body: c.Body, Created: c.Created.Unix()})
	}
	return out, nil
}

// GitHubPRAction is github.pr.action.
func (s *Server) GitHubPRAction(p proto.GitHubPRActionParams) error {
	repo, err := repoOf(proto.GitHubIssueParams{Repo: p.Repo, Number: p.Number}, true)
	if err != nil {
		return err
	}
	switch p.Action {
	case "comment":
		return github.PRComment(repo, p.Number, p.Body)
	case "merge":
		return github.MergePR(repo, p.Number, p.Method)
	case "close":
		return github.ClosePR(repo, p.Number)
	case "reopen":
		return github.ReopenPR(repo, p.Number)
	case "ready":
		return github.ReadyPR(repo, p.Number)
	}
	return fmt.Errorf("a pull request is commented, merged, closed, reopened or made ready, not %q", p.Action)
}

// GitHubIssuePRs is github.issue.prs: the pull requests GitHub says close
// an issue, and those on the project's branches made for it (issue-<n>-…),
// which say nothing of it in their text when an agent opened them.
func (s *Server) GitHubIssuePRs(p proto.GitHubIssuePRsParams) ([]proto.GitHubPR, error) {
	repo, err := repoOf(proto.GitHubIssueParams{Repo: p.Repo, Number: p.Number}, true)
	if err != nil {
		return nil, err
	}
	var branches []string
	if p.Dir != "" {
		if out, err := exec.Command("git", "-C", p.Dir, "for-each-ref", "--format=%(refname:short)", "refs/heads").Output(); err == nil {
			for _, b := range strings.Fields(string(out)) {
				if github.IsIssueBranch(b, p.Number) {
					branches = append(branches, b)
				}
			}
		}
	}
	prs, err := github.PRsForIssue(repo, p.Number, branches)
	if err != nil {
		return nil, err
	}
	out := make([]proto.GitHubPR, 0, len(prs))
	for _, pr := range prs {
		out = append(out, wirePR(pr))
	}
	return out, nil
}

func wirePR(p github.PR) proto.GitHubPR {
	return proto.GitHubPR{
		Repo: p.Repo, Number: p.Number, Title: p.Title, State: p.State, Draft: p.Draft, Author: p.Author,
		Labels: p.Labels, Head: p.Head, Base: p.Base, Review: p.Review,
		Pass: p.Checks.Pass, Fail: p.Checks.Fail, Pending: p.Checks.Pending,
		Updated: p.Updated.Unix(), URL: p.URL,
	}
}

// GitHubPRChecks is github.prs.checks: the checks of the first pull
// requests the same list finds, as pass, fail and pending counts.
func (s *Server) GitHubPRChecks(p proto.GitHubIssuesParams) (proto.GitHubPRChecks, error) {
	l, err := s.githubList(p)
	if err != nil {
		return proto.GitHubPRChecks{}, err
	}
	filter := github.PRFilterOpen
	if p.Filter >= 0 && p.Filter < len(github.PRFilters) {
		filter = github.PRFilters[p.Filter]
	}
	checks, err := github.PRChecks(l.repos, filter, p.Query)
	if err != nil {
		return proto.GitHubPRChecks{}, err
	}
	out := proto.GitHubPRChecks{Checks: make(map[string][3]int, len(checks))}
	for key, c := range checks {
		out.Checks[key] = [3]int{c.Pass, c.Fail, c.Pending}
	}
	return out, nil
}

// GitHubRepoOptions is github.repo.options: what the pickers offer.
func (s *Server) GitHubRepoOptions(p proto.GitHubRepoOptionsParams) (proto.GitHubRepoOptions, error) {
	repo, err := repoOf(proto.GitHubIssueParams{Repo: p.Repo}, false)
	if err != nil {
		return proto.GitHubRepoOptions{}, err
	}
	var names []string
	switch p.Kind {
	case "labels":
		names, err = github.Labels(repo)
	case "assignees":
		names, err = github.Assignable(repo)
	default:
		return proto.GitHubRepoOptions{}, fmt.Errorf("a repository offers labels or assignees, not %q", p.Kind)
	}
	if err != nil {
		return proto.GitHubRepoOptions{}, err
	}
	if names == nil {
		names = []string{}
	}
	return proto.GitHubRepoOptions{Names: names}, nil
}

// GitHubIssueEdit is github.issue.edit.
func (s *Server) GitHubIssueEdit(p proto.GitHubIssueEditParams) error {
	repo, err := repoOf(proto.GitHubIssueParams{Repo: p.Repo, Number: p.Number}, true)
	if err != nil {
		return err
	}
	return github.EditIssue(repo, p.Number, github.Edit{
		Title: p.Title, AddLabels: p.AddLabels, RemoveLabels: p.RemoveLabels,
		AddAssignees: p.AddAssignees, RemoveAssignees: p.RemoveAssignees,
	})
}
