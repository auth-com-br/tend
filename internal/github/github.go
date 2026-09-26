// Package github reads a project's issues and pull requests — on GitHub
// through the GitHub CLI, and on GitLab through its web API (gitlab.go),
// into the same shapes, so the issues panel is one for both.
//
// For GitHub it
// is tend's own — herdr has nothing of the kind — and follows how Orca,
// which the owner uses, does it: every call is `gh`, so tend keeps no token
// and needs no login of its own; the repository is read from the project's
// git remotes, upstream before origin, so a fork shows the issues of what it
// forked unless origin is asked for; and the list is GitHub's issue search,
// so what is typed into it is GitHub's own search syntax.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Errors a caller tells apart, to say what to do about them.
var (
	// ErrNoGh is a machine without the GitHub CLI.
	ErrNoGh = errors.New("the GitHub CLI (gh) is not installed — https://cli.github.com")
	// ErrNotLoggedIn is gh with no account to use.
	ErrNotLoggedIn = errors.New("the GitHub CLI is not logged in — run `gh auth login`")
	// ErrNoRepo is a directory with no remote on github.com or on a GitLab.
	ErrNoRepo = errors.New("this project has no GitHub or GitLab remote")
)

// Gh is the program run, a variable so a test can name a stand-in.
var Gh = "gh"

// callTimeout bounds one call: gh over a slow network, not forever.
const callTimeout = 30 * time.Second

// Repo is a repository on GitHub or a GitLab, and the remote it was found
// through.
type Repo struct {
	// Host is the GitLab it is on (gitlab.com, or a company's own), empty
	// for GitHub. On a GitLab, Owner is the group, with its subgroups.
	Host        string
	Owner, Name string
	Remote      string
	// Dir is the checkout it was found in, where work on it starts.
	Dir string
}

// GitLab reports whether the repository is on a GitLab.
func (r Repo) GitLab() bool { return r.Host != "" }

// Slug names the repository across the wire: owner/name for GitHub, as
// gh's --repo takes it, and host/group/…/name for a GitLab — a GitHub owner
// has no dot in it, so the two are never taken for each other (ParseSlug).
func (r Repo) Slug() string {
	if r.Host != "" {
		return r.Host + "/" + r.Owner + "/" + r.Name
	}
	return r.Owner + "/" + r.Name
}

// ParseSlug is the repository a Slug names.
func ParseSlug(s string) (Repo, bool) {
	parts := strings.Split(s, "/")
	for _, p := range parts {
		if p == "" {
			return Repo{}, false
		}
	}
	switch {
	case len(parts) >= 3 && strings.Contains(parts[0], "."):
		return Repo{Host: parts[0], Owner: strings.Join(parts[1:len(parts)-1], "/"), Name: parts[len(parts)-1]}, true
	case len(parts) == 2 && !strings.Contains(parts[0], "."):
		return Repo{Owner: parts[0], Name: parts[1]}, true
	}
	return Repo{}, false
}

// remotePattern reads owner and name out of a remote on github.com: an
// https URL, git@github.com:owner/name, or ssh://git@github.com/owner/name,
// with or without .git.
var remotePattern = regexp.MustCompile(`^(?:https?://(?:[^@/]+@)?github\.com/|git@github\.com:|ssh://git@github\.com(?::\d+)?/)([^/]+)/([^/]+?)(?:\.git)?/?$`)

// ParseRemote is the repository a remote URL names, if it is on github.com.
func ParseRemote(u string) (owner, name string, ok bool) {
	m := remotePattern.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// ReposFor is the GitHub repositories a checkout's remotes name, upstream
// first, then origin, then the rest in git's order — Orca's rule, so a fork
// lists what it was forked from by default.
func ReposFor(dir string) ([]Repo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote", "-v").Output()
	if err != nil {
		return nil, fmt.Errorf("%w (%s is not a git checkout)", ErrNoRepo, dir)
	}
	var repos []Repo
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || seen[f[0]] {
			continue
		}
		if owner, name, ok := ParseRemote(f[1]); ok {
			seen[f[0]] = true
			repos = append(repos, Repo{Owner: owner, Name: name, Remote: f[0]})
		} else if host, path, ok := ParseGitLabRemote(f[1]); ok {
			seen[f[0]] = true
			i := strings.LastIndexByte(path, '/')
			repos = append(repos, Repo{Host: host, Owner: path[:i], Name: path[i+1:], Remote: f[0]})
		}
	}
	rank := func(r Repo) int {
		switch r.Remote {
		case "upstream":
			return 0
		case "origin":
			return 1
		}
		return 2
	}
	for i := 1; i < len(repos); i++ {
		for j := i; j > 0 && rank(repos[j]) < rank(repos[j-1]); j-- {
			repos[j], repos[j-1] = repos[j-1], repos[j]
		}
	}
	if len(repos) == 0 {
		return nil, ErrNoRepo
	}
	return repos, nil
}

// Scope is what a list is of: the repository a project is in, through each
// of its remotes, or — for a folder of repositories, which is not one
// itself — each repository under it.
type Scope struct {
	// Multi is a folder of repositories; Repos is then one each, else the
	// one repository through each remote, upstream first.
	Multi bool
	Repos []Repo
}

// maxRepoDepth is how far below a folder repositories are looked for, the
// files panel's rule (internal/explorer): a folder of projects, and a
// folder of folders of them.
const maxRepoDepth = 2

// ScopeFor is what a list from dir is of. A checkout is its repository; a
// folder that is none — a project made of several, the owner's mvno with
// its api, app and panel side by side — is every GitHub repository in it.
func ScopeFor(dir string) (Scope, error) {
	if top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil {
		repos, err := ReposFor(dir)
		for i := range repos {
			repos[i].Dir = strings.TrimSpace(string(top))
		}
		return Scope{Repos: repos}, err
	}
	var out []Repo
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxRepoDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || name == "node_modules" {
				continue
			}
			path := filepath.Join(dir, name)
			if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
				walk(path, depth+1)
				continue
			}
			if repos, err := ReposFor(path); err == nil {
				r := repos[0]
				r.Dir = path
				out = append(out, r)
			}
		}
	}
	walk(dir, 1)
	if len(out) == 0 {
		return Scope{}, fmt.Errorf("%w (%s is not a git checkout, and none under it is on GitHub or a GitLab)", ErrNoRepo, dir)
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	return Scope{Multi: true, Repos: out}, nil
}

// Issue is one issue as the list shows it.
type Issue struct {
	// Repo is owner/name, the repository it is in.
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

// Filter is one of the list's presets, Orca's: what is open, what is
// assigned to the user, what they opened, and what is closed.
type Filter int

const (
	FilterOpen Filter = iota
	FilterMine
	FilterCreated
	FilterClosed
)

// Filters are the presets in the order the list walks them.
var Filters = []Filter{FilterOpen, FilterMine, FilterCreated, FilterClosed}

func (f Filter) String() string {
	switch f {
	case FilterMine:
		return "assigned to me"
	case FilterCreated:
		return "created by me"
	case FilterClosed:
		return "closed"
	}
	return "open"
}

func (f Filter) query() string {
	switch f {
	case FilterMine:
		return "is:open assignee:@me"
	case FilterCreated:
		return "is:open author:@me"
	case FilterClosed:
		return "is:closed"
	}
	return "is:open"
}

// listLimit is how many the list reads: one page of GitHub's search.
const listLimit = 100

// SearchQuery is the search a list runs: the repositories — GitHub's search
// takes several, as any of them — issues only, the preset, and what was
// typed, in GitHub's own syntax.
func SearchQuery(repos []Repo, filter Filter, typed string) string {
	q := ""
	for _, r := range repos {
		q += "repo:" + r.Slug() + " "
	}
	q += "is:issue " + filter.query()
	if typed = strings.TrimSpace(typed); typed != "" {
		q += " " + typed
	}
	return q
}

// List is the issues a search finds, the last updated first, and how many
// it found in all. GitHub's repositories are one search; each GitLab's is
// asked apart, and the answers put together by when they were updated.
func List(repos []Repo, filter Filter, typed string) ([]Issue, int, error) {
	hub, lab := splitRepos(repos)
	var issues []Issue
	total := 0
	if len(hub) > 0 {
		got, n, err := listGitHub(hub, filter, typed)
		if err != nil {
			return nil, 0, err
		}
		issues, total = got, n
	}
	if len(lab) > 0 {
		got, err := glEachIssues(lab, func(r Repo) ([]Issue, error) { return glListIssues(r, filter, typed) })
		if err != nil {
			return nil, 0, err
		}
		issues, total = append(issues, got...), total+len(got)
	}
	if len(hub) > 0 && len(lab) > 0 {
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Updated.After(issues[j].Updated) })
		if len(issues) > listLimit {
			issues = issues[:listLimit]
		}
	}
	return issues, total, nil
}

// listGitHub is List on GitHub: one search for all its repositories.
func listGitHub(repos []Repo, filter Filter, typed string) ([]Issue, int, error) {
	path := fmt.Sprintf("search/issues?q=%s&sort=updated&order=desc&per_page=%d",
		url.QueryEscape(SearchQuery(repos, filter, typed)), listLimit)
	out, err := run("api", "--cache", "60s", path)
	if err != nil {
		return nil, 0, err
	}
	var res struct {
		Total int         `json:"total_count"`
		Items []wireIssue `json:"items"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, 0, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	issues := make([]Issue, 0, len(res.Items))
	for _, w := range res.Items {
		if w.PullRequest != nil {
			continue // the search's is:issue should keep them out; this makes sure
		}
		issues = append(issues, w.issue())
	}
	return issues, res.Total, nil
}

type wireIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
	Comments    int             `json:"comments"`
	Updated     time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
	// RepositoryURL is https://api.github.com/repos/owner/name.
	RepositoryURL string `json:"repository_url"`
}

func (w wireIssue) issue() Issue {
	repo := w.RepositoryURL
	if i := strings.Index(repo, "/repos/"); i >= 0 {
		repo = repo[i+len("/repos/"):]
	}
	i := Issue{Repo: repo, Number: w.Number, Title: w.Title, State: w.State, Author: w.User.Login,
		Comments: w.Comments, Updated: w.Updated, URL: w.URL}
	for _, l := range w.Labels {
		i.Labels = append(i.Labels, l.Name)
	}
	for _, a := range w.Assignees {
		i.Assignees = append(i.Assignees, a.Login)
	}
	return i
}

// Comment is one comment on an issue.
type Comment struct {
	Author  string
	Body    string
	Created time.Time
}

// Detail is an issue with its text and its comments.
type Detail struct {
	Issue
	Body     string
	Created  time.Time
	Thread   []Comment
	Assigned []string
}

// Get reads one issue whole.
func Get(repo Repo, number int) (Detail, error) {
	if repo.GitLab() {
		return glGet(repo, number)
	}
	out, err := run("issue", "view", fmt.Sprint(number), "--repo", repo.Slug(), "--json",
		"number,title,state,url,author,labels,assignees,body,comments,createdAt,updatedAt")
	if err != nil {
		return Detail{}, err
	}
	var w struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Body   string `json:"body"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
		Comments []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body    string    `json:"body"`
			Created time.Time `json:"createdAt"`
		} `json:"comments"`
		Created time.Time `json:"createdAt"`
		Updated time.Time `json:"updatedAt"`
	}
	if err := json.Unmarshal(out, &w); err != nil {
		return Detail{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	d := Detail{
		Issue: Issue{Repo: repo.Slug(), Number: w.Number, Title: w.Title, State: strings.ToLower(w.State), URL: w.URL,
			Author: w.Author.Login, Comments: len(w.Comments), Updated: w.Updated},
		Body: w.Body, Created: w.Created,
	}
	for _, l := range w.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	for _, a := range w.Assignees {
		d.Assignees = append(d.Assignees, a.Login)
	}
	for _, c := range w.Comments {
		d.Thread = append(d.Thread, Comment{Author: c.Author.Login, Body: c.Body, Created: c.Created})
	}
	return d, nil
}

// AddComment adds a comment to an issue. The text goes on gh's standard input,
// not its command line, where it would be cut, and seen in ps.
func AddComment(repo Repo, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment needs some text")
	}
	if repo.GitLab() {
		return glComment(repo, "issues", number, body)
	}
	_, err := runInput(body, "issue", "comment", fmt.Sprint(number), "--repo", repo.Slug(), "--body-file", "-")
	return err
}

// Reasons an issue is closed for, gh's and Orca's.
const (
	ReasonCompleted  = "completed"
	ReasonNotPlanned = "not planned"
)

// Close closes an issue, for a reason.
func Close(repo Repo, number int, reason string) error {
	if repo.GitLab() {
		return glSetState(repo, "issues", number, "close")
	}
	if reason != ReasonNotPlanned {
		reason = ReasonCompleted
	}
	_, err := run("issue", "close", fmt.Sprint(number), "--repo", repo.Slug(), "--reason", reason)
	return err
}

// Reopen opens a closed issue again.
func Reopen(repo Repo, number int) error {
	if repo.GitLab() {
		return glSetState(repo, "issues", number, "reopen")
	}
	_, err := run("issue", "reopen", fmt.Sprint(number), "--repo", repo.Slug())
	return err
}

// Create files an issue and returns its number and address.
func Create(repo Repo, title, body string) (int, string, error) {
	if repo.GitLab() {
		return glCreate(repo, title, body)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, "", errors.New("an issue needs a title")
	}
	out, err := runInput(body, "issue", "create", "--repo", repo.Slug(), "--title", title, "--body-file", "-")
	if err != nil {
		return 0, "", err
	}
	// gh prints the new issue's address, last.
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, "", errors.New("gh created the issue but did not say where")
	}
	url := fields[len(fields)-1]
	number := 0
	if i := strings.LastIndexByte(url, '/'); i >= 0 {
		fmt.Sscan(url[i+1:], &number)
	}
	return number, url, nil
}

// Labels is the repository's labels, by name, for the label picker.
func Labels(repo Repo) ([]string, error) {
	if repo.GitLab() {
		return glLabels(repo)
	}
	return names("repos/"+repo.Slug()+"/labels?per_page=100", ".[].name")
}

// Assignable is who an issue in the repository can be assigned to.
func Assignable(repo Repo) ([]string, error) {
	if repo.GitLab() {
		return glAssignable(repo)
	}
	return names("repos/"+repo.Slug()+"/assignees?per_page=100", ".[].login")
}

// names reads one field of every item of a paginated list, one a line.
func names(path, jq string) ([]string, error) {
	out, err := run("api", "--paginate", "--cache", "60s", path, "--jq", jq)
	if err != nil {
		return nil, err
	}
	var list []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			list = append(list, line)
		}
	}
	return list, nil
}

// Edit is what an edit of an issue changes: a new title when not empty,
// and the labels and assignees to add and to take off.
type Edit struct {
	Title                         string
	AddLabels, RemoveLabels       []string
	AddAssignees, RemoveAssignees []string
}

// EditIssue changes an issue, Orca's way: one gh issue edit with only
// what changed, so labels somebody else added meanwhile stay.
func EditIssue(repo Repo, number int, e Edit) error {
	if repo.GitLab() {
		return glEditIssue(repo, number, e)
	}
	args := []string{"issue", "edit", fmt.Sprint(number), "--repo", repo.Slug()}
	if t := strings.TrimSpace(e.Title); t != "" {
		args = append(args, "--title", t)
	}
	for _, pair := range []struct {
		flag  string
		names []string
	}{{"--add-label", e.AddLabels}, {"--remove-label", e.RemoveLabels},
		{"--add-assignee", e.AddAssignees}, {"--remove-assignee", e.RemoveAssignees}} {
		for _, n := range pair.names {
			args = append(args, pair.flag, n)
		}
	}
	if len(args) == 5 {
		return nil // nothing changed
	}
	_, err := run(args...)
	return err
}

// branchTitleMax is how much of a title goes into a branch's name.
const branchTitleMax = 40

// IssueBranch is the branch work on an issue goes on: issue-<number>-<the
// title, as a slug>, which Orca's workspace seed also takes from the title,
// and which says, in git branch, what each one was for.
func IssueBranch(number int, title string) string {
	var b strings.Builder
	dash := true // no dash first
	for _, r := range foldAccents(strings.ToLower(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > branchTitleMax {
		// Cut at the last word that fits, not in the middle of one.
		slug = slug[:branchTitleMax]
		if i := strings.LastIndexByte(slug, '-'); i > 0 {
			slug = slug[:i]
		}
	}
	if slug == "" {
		return fmt.Sprintf("issue-%d", number)
	}
	return fmt.Sprintf("issue-%d-%s", number, slug)
}

// accentFolds are the letters with marks a title is likely to have, as the
// letters under them, so "começar" is comecar in a branch and not come-ar:
// the Latin-1 and Latin Extended-A ones that Portuguese, Spanish, French
// and German use. A table rather than Unicode decomposition, which is
// golang.org/x/text, a dependency for this alone.
var accentFolds = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"ç", "c", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ñ", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y",
	"ß", "ss", "æ", "ae", "œ", "oe",
)

func foldAccents(s string) string { return accentFolds.Replace(s) }

// IsIssueBranch is whether a branch is the one IssueBranch names for an
// issue, whatever its title was then.
func IsIssueBranch(branch string, number int) bool {
	prefix := fmt.Sprintf("issue-%d", number)
	return branch == prefix || strings.HasPrefix(branch, prefix+"-")
}

// run runs gh, and turns what went wrong into something to act on.
func run(args ...string) ([]byte, error) { return runInput("", args...) }

// runInput is run with stdin, for the text of what is written.
func runInput(stdin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, Gh, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	// gh would otherwise ask, on a terminal it does not have, and wait.
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, ErrNoGh
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("GitHub did not answer in %s", callTimeout)
	}
	msg := strings.TrimSpace(stderr.String())
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "gh auth login"), strings.Contains(lower, "not logged in"):
		return nil, ErrNotLoggedIn
	case msg == "":
		return nil, err
	}
	if i := strings.IndexByte(msg, '\n'); i > 0 {
		msg = msg[:i]
	}
	return nil, errors.New(msg)
}
