package github

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Pull requests, as Orca shows them beside issues: the list through
// `gh pr list` with GitHub's search, each with its checks summed up and its
// review decision; one whole through `gh pr view`; and the ones an issue
// has — those GitHub says close it, and those on a branch made for it.

// Check is one check on a pull request's head: its name, and pass, fail,
// pending or skipped.
type Check struct {
	Name  string
	State string
}

// Check states.
const (
	CheckPass    = "pass"
	CheckFail    = "fail"
	CheckPending = "pending"
	CheckSkipped = "skipped"
)

// Checks is how a pull request's checks stand, counted.
type Checks struct {
	Pass, Fail, Pending int
}

// PR is one pull request as the list shows it.
type PR struct {
	// Repo is owner/name, the repository it is in.
	Repo   string
	Number int
	Title  string
	// State is open, closed or merged; Draft a draft among the open.
	State  string
	Draft  bool
	Author string
	Labels []string
	Head   string
	Base   string
	// Review is GitHub's review decision: APPROVED, CHANGES_REQUESTED,
	// REVIEW_REQUIRED, or empty when none is asked for.
	Review  string
	Checks  Checks
	Updated time.Time
	URL     string
}

// PRDetail is a pull request whole.
type PRDetail struct {
	PR
	Body      string
	Created   time.Time
	Additions int
	Deletions int
	Files     int
	// Mergeable is GitHub's MERGEABLE, CONFLICTING or UNKNOWN.
	Mergeable string
	CheckList []Check
	// Thread is the comments and the reviews, oldest first; a review's
	// author is followed by what it said (approved, changes requested).
	Thread []Comment
}

// PR filters are Orca's for pull requests: open, the user's, those asking
// for the user's review, and the closed and merged.
const (
	PRFilterOpen Filter = iota
	PRFilterMine
	PRFilterReview
	PRFilterClosed
)

// PRFilters are the presets in the order the list walks them.
var PRFilters = []Filter{PRFilterOpen, PRFilterMine, PRFilterReview, PRFilterClosed}

// PRFilterName is what a pull request preset is called.
func PRFilterName(f Filter) string {
	switch f {
	case PRFilterMine:
		return "mine"
	case PRFilterReview:
		return "needs my review"
	case PRFilterClosed:
		return "closed"
	}
	return "open"
}

func prFilterQuery(f Filter) string {
	switch f {
	case PRFilterMine:
		return "is:open author:@me"
	case PRFilterReview:
		return "is:open review-requested:@me"
	case PRFilterClosed:
		return "is:closed"
	}
	return "is:open"
}

// prFields are what a pull request is read with; prListFields leaves out
// the checks, which a list asks for apart (PRChecks): a hundred pull
// requests with their checks in one call is seconds, and on a busy
// repository GitHub gave up on it with a 504.
const (
	prListFields = "number,title,state,isDraft,author,labels,headRefName,baseRefName,updatedAt,url,reviewDecision"
	prFields     = prListFields + ",statusCheckRollup"
)

// checksLimit is how many of a list's pull requests have their checks
// read: the ones at its top, which are the ones looked at.
const checksLimit = 30

func prSearch(filter Filter, typed string) string {
	search := prFilterQuery(filter) + " sort:updated-desc"
	if typed = strings.TrimSpace(typed); typed != "" {
		search = typed + " " + search
	}
	return search
}

// CheckKey is how PRChecks names a pull request: owner/name#number, as
// several repositories' numbers meet in one list.
func CheckKey(repo string, number int) string { return fmt.Sprintf("%s#%d", repo, number) }

// PRChecks is how the checks stand on the first pull requests a search
// finds in each repository, by CheckKey: the list's checks column, filled
// in after the list.
func PRChecks(repos []Repo, filter Filter, typed string) (map[string]Checks, error) {
	lists, err := eachRepo(repos, func(r Repo) ([]PR, error) {
		if r.GitLab() {
			// A merge request's pipeline is read with it (glMR.pr), so a
			// GitLab has nothing more to fill in.
			return nil, nil
		}
		out, err := run("pr", "list", "--repo", r.Slug(), "--state", "all", "--limit", fmt.Sprint(checksLimit),
			"--search", prSearch(filter, typed), "--json", "number,statusCheckRollup")
		if err != nil {
			return nil, err
		}
		return decodePRs(out, r)
	})
	if err != nil {
		return nil, err
	}
	checks := map[string]Checks{}
	for _, p := range lists {
		checks[CheckKey(p.Repo, p.Number)] = p.Checks
	}
	return checks, nil
}

// ListPRs is the pull requests a search finds, the last updated first,
// without their checks: gh's list for one repository, and for several, each
// one's asked at once and put together by when they were last updated —
// GitHub's search, which takes several repositories at once, has neither
// branches nor review decisions.
func ListPRs(repos []Repo, filter Filter, typed string) ([]PR, error) {
	prs, err := eachRepo(repos, func(r Repo) ([]PR, error) {
		if r.GitLab() {
			return glListMRs(r, filter, typed)
		}
		out, err := run("pr", "list", "--repo", r.Slug(), "--state", "all", "--limit", fmt.Sprint(listLimit),
			"--search", prSearch(filter, typed), "--json", prListFields)
		if err != nil {
			return nil, err
		}
		return decodePRs(out, r)
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(prs, func(i, j int) bool { return prs[i].Updated.After(prs[j].Updated) })
	if len(prs) > listLimit {
		prs = prs[:listLimit]
	}
	return prs, nil
}

func decodePRs(out []byte, r Repo) ([]PR, error) {
	var wire []wirePR
	if err := json.Unmarshal(out, &wire); err != nil {
		return nil, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	prs := make([]PR, 0, len(wire))
	for _, w := range wire {
		p := w.pr()
		p.Repo = r.Slug()
		prs = append(prs, p)
	}
	return prs, nil
}

// eachRepo asks every repository at once and puts the answers together; the
// first failure is the answer.
func eachRepo(repos []Repo, ask func(Repo) ([]PR, error)) ([]PR, error) {
	type answer struct {
		prs []PR
		err error
	}
	answers := make([]answer, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prs, err := ask(r)
			answers[i] = answer{prs, err}
		}()
	}
	wg.Wait()
	var out []PR
	for _, a := range answers {
		if a.err != nil {
			return nil, a.err
		}
		out = append(out, a.prs...)
	}
	return out, nil
}

type wireCheck struct {
	Type       string `json:"__typename"`
	Name       string `json:"name"`
	Context    string `json:"context"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// state reads a check run or a commit status the way GitHub's own list of
// checks does.
func (c wireCheck) check() Check {
	name := c.Name
	if name == "" {
		name = c.Context
	}
	if c.Type == "StatusContext" || (c.Status == "" && c.State != "") {
		switch c.State {
		case "SUCCESS":
			return Check{name, CheckPass}
		case "FAILURE", "ERROR":
			return Check{name, CheckFail}
		}
		return Check{name, CheckPending}
	}
	if c.Status != "COMPLETED" {
		return Check{name, CheckPending}
	}
	switch c.Conclusion {
	case "SUCCESS", "NEUTRAL":
		return Check{name, CheckPass}
	case "SKIPPED", "STALE":
		return Check{name, CheckSkipped}
	}
	return Check{name, CheckFail}
}

type wirePR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Draft  bool   `json:"isDraft"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Head    string      `json:"headRefName"`
	Base    string      `json:"baseRefName"`
	Updated time.Time   `json:"updatedAt"`
	URL     string      `json:"url"`
	Review  string      `json:"reviewDecision"`
	Rollup  []wireCheck `json:"statusCheckRollup"`
}

func (w wirePR) pr() PR {
	p := PR{Number: w.Number, Title: w.Title, State: strings.ToLower(w.State), Draft: w.Draft,
		Author: w.Author.Login, Head: w.Head, Base: w.Base, Review: w.Review, Updated: w.Updated, URL: w.URL}
	for _, l := range w.Labels {
		p.Labels = append(p.Labels, l.Name)
	}
	for _, c := range w.Rollup {
		switch c.check().State {
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

// GetPR reads one pull request whole.
func GetPR(repo Repo, number int) (PRDetail, error) {
	if repo.GitLab() {
		return glGetMR(repo, number)
	}
	out, err := run("pr", "view", fmt.Sprint(number), "--repo", repo.Slug(), "--json",
		prFields+",body,createdAt,additions,deletions,changedFiles,mergeable,comments,latestReviews")
	if err != nil {
		return PRDetail{}, err
	}
	var w struct {
		wirePR
		Body      string    `json:"body"`
		Created   time.Time `json:"createdAt"`
		Additions int       `json:"additions"`
		Deletions int       `json:"deletions"`
		Files     int       `json:"changedFiles"`
		Mergeable string    `json:"mergeable"`
		Comments  []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body    string    `json:"body"`
			Created time.Time `json:"createdAt"`
		} `json:"comments"`
		Reviews []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body      string    `json:"body"`
			State     string    `json:"state"`
			Submitted time.Time `json:"submittedAt"`
		} `json:"latestReviews"`
	}
	if err := json.Unmarshal(out, &w); err != nil {
		return PRDetail{}, fmt.Errorf("reading GitHub's answer: %w", err)
	}
	pr := w.pr()
	pr.Repo = repo.Slug()
	d := PRDetail{PR: pr, Body: w.Body, Created: w.Created, Additions: w.Additions,
		Deletions: w.Deletions, Files: w.Files, Mergeable: w.Mergeable}
	for _, c := range w.Rollup {
		d.CheckList = append(d.CheckList, c.check())
	}
	for _, c := range w.Comments {
		d.Thread = append(d.Thread, Comment{Author: c.Author.Login, Body: c.Body, Created: c.Created})
	}
	for _, r := range w.Reviews {
		said := strings.ToLower(strings.ReplaceAll(r.State, "_", " "))
		d.Thread = append(d.Thread, Comment{Author: r.Author.Login + " · " + said, Body: r.Body, Created: r.Submitted})
	}
	sort.SliceStable(d.Thread, func(i, j int) bool { return d.Thread[i].Created.Before(d.Thread[j].Created) })
	return d, nil
}

// PRComment comments on a pull request.
func PRComment(repo Repo, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("a comment needs some text")
	}
	if repo.GitLab() {
		return glComment(repo, "merge_requests", number, body)
	}
	_, err := runInput(body, "pr", "comment", fmt.Sprint(number), "--repo", repo.Slug(), "--body-file", "-")
	return err
}

// Merge methods, gh's.
const (
	MergeSquash = "squash"
	MergeMerge  = "merge"
	MergeRebase = "rebase"
)

// MergePR merges a pull request by a method.
func MergePR(repo Repo, number int, method string) error {
	if repo.GitLab() {
		return glMerge(repo, number, method)
	}
	switch method {
	case MergeSquash, MergeMerge, MergeRebase:
	default:
		return fmt.Errorf("a pull request is merged by squash, merge or rebase, not %q", method)
	}
	_, err := run("pr", "merge", fmt.Sprint(number), "--repo", repo.Slug(), "--"+method)
	return err
}

// ClosePR closes a pull request unmerged, and ReopenPR opens it again.
func ClosePR(repo Repo, number int) error {
	if repo.GitLab() {
		return glSetState(repo, "merge_requests", number, "close")
	}
	_, err := run("pr", "close", fmt.Sprint(number), "--repo", repo.Slug())
	return err
}

func ReopenPR(repo Repo, number int) error {
	if repo.GitLab() {
		return glSetState(repo, "merge_requests", number, "reopen")
	}
	_, err := run("pr", "reopen", fmt.Sprint(number), "--repo", repo.Slug())
	return err
}

// ReadyPR marks a draft ready for review.
func ReadyPR(repo Repo, number int) error {
	if repo.GitLab() {
		return glReady(repo, number)
	}
	_, err := run("pr", "ready", fmt.Sprint(number), "--repo", repo.Slug())
	return err
}

// linkedQuery is GitHub's own link from an issue to the pull requests that
// close it ("Closes #42"), which gh's issue view does not carry.
const linkedQuery = `query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n){
closedByPullRequestsReferences(first:10,includeClosedPrs:true){nodes{number}}}}}`

// PRsForIssue is the pull requests an issue has: the ones GitHub says close
// it, and the ones on branches — those made for it, which the caller names
// — whatever their text says. Each is read in the list's shape.
func PRsForIssue(repo Repo, number int, branches []string) ([]PR, error) {
	if repo.GitLab() {
		return glMRsForIssue(repo, number, branches)
	}
	numbers := map[int]bool{}
	out, err := run("api", "graphql", "-F", "o="+repo.Owner, "-F", "r="+repo.Name, "-F", fmt.Sprintf("n=%d", number), "-f", "query="+linkedQuery)
	if err != nil {
		return nil, err
	}
	var linked struct {
		Data struct {
			Repository struct {
				Issue struct {
					Refs struct {
						Nodes []struct {
							Number int `json:"number"`
						} `json:"nodes"`
					} `json:"closedByPullRequestsReferences"`
				} `json:"issue"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &linked); err == nil {
		for _, n := range linked.Data.Repository.Issue.Refs.Nodes {
			numbers[n.Number] = true
		}
	}
	var prs []PR
	seen := map[int]bool{}
	for _, b := range branches {
		out, err := run("pr", "list", "--repo", repo.Slug(), "--state", "all", "--head", b, "--json", prFields)
		if err != nil {
			return nil, err
		}
		var wire []wirePR
		if json.Unmarshal(out, &wire) != nil {
			continue
		}
		for _, w := range wire {
			if !seen[w.Number] {
				seen[w.Number] = true
				p := w.pr()
				p.Repo = repo.Slug()
				prs = append(prs, p)
			}
		}
	}
	for n := range numbers {
		if seen[n] {
			continue
		}
		d, err := GetPR(repo, n)
		if err != nil {
			return nil, err
		}
		seen[n] = true
		prs = append(prs, d.PR)
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].Number > prs[j].Number })
	return prs, nil
}
