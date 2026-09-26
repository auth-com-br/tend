package github

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestAGitLabRemoteIsARepository: an https, a scp-like and an ssh remote
// on gitlab.com or a company's GitLab name a project, subgroups kept; one
// on another host, or with no group, does not; and a GitLab slug and a
// GitHub one read back apart. If it regresses, a GitLab project shows "no
// remote", or a GitHub owner is taken for a GitLab host.
func TestAGitLabRemoteIsARepository(t *testing.T) {
	for remote, want := range map[string]string{
		"https://gitlab.com/group/project.git":            "gitlab.com group/project",
		"git@gitlab.com:group/sub/project.git":            "gitlab.com group/sub/project",
		"ssh://git@gitlab.acme.com:2222/team/app":         "gitlab.acme.com team/app",
		"https://oauth2:tok@GitLab.acme.com/team/app.git": "gitlab.acme.com team/app",
		"https://github.com/owner/name.git":               "",
		"https://gitlab.com/justone":                      "",
		"https://bitbucket.org/team/app.git":              "",
	} {
		host, path, ok := ParseGitLabRemote(remote)
		got := ""
		if ok {
			got = host + " " + path
		}
		if got != want {
			t.Errorf("%s = %q, want %q", remote, got, want)
		}
	}
	lab, ok := ParseSlug("gitlab.com/group/sub/project")
	if !ok || lab.Host != "gitlab.com" || lab.Owner != "group/sub" || lab.Name != "project" || lab.Slug() != "gitlab.com/group/sub/project" {
		t.Errorf("gitlab slug: %+v", lab)
	}
	hub, ok := ParseSlug("owner/name")
	if !ok || hub.GitLab() || hub.Slug() != "owner/name" {
		t.Errorf("github slug: %+v", hub)
	}
	for _, bad := range []string{"", "one", "a//b", "owner/name/extra", "x.y/name"} {
		if _, ok := ParseSlug(bad); ok {
			t.Errorf("%q read as a slug", bad)
		}
	}
}

// gitLabStandIn answers as GitLab's API does for one project,
// group/sub/app, and records what was written. With a token it says who
// the user is; without one, notes and labels want one, as gitlab.com's do.
func gitLabStandIn(t *testing.T) (*[]string, func(token string)) {
	t.Helper()
	var mu sync.Mutex
	var wrote []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signed := r.Header.Get("PRIVATE-TOKEN") == "glpat-good"
		const p = "/api/v4/projects/group%2Fsub%2Fapp"
		path := r.URL.EscapedPath()
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodGet {
			mu.Lock()
			wrote = append(wrote, r.Method+" "+strings.TrimPrefix(path, p)+" "+string(body))
			mu.Unlock()
		}
		q := r.URL.Query()
		switch {
		case path == "/api/v4/user":
			if !signed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"id":7,"username":"akira"}`)
		case path == p+"/issues" && r.Method == http.MethodGet:
			if q.Get("scope") != "" && !signed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			state := q.Get("state")
			_, _ = io.WriteString(w, `[{"iid":42,"title":"checkout is grey `+state+` `+q.Get("scope")+` `+q.Get("search")+`","state":"`+state+`","web_url":"https://gitlab.com/group/sub/app/-/issues/42","author":{"username":"ana"},"labels":["bug"],"assignees":[{"id":3,"username":"bia"}],"user_notes_count":2,"updated_at":"2026-09-20T12:00:00Z"}]`)
		case path == p+"/issues" && r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"iid":43,"web_url":"https://gitlab.com/group/sub/app/-/issues/43"}`)
		case path == p+"/issues/42" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"iid":42,"title":"checkout is grey","state":"opened","description":"it is grey","author":{"username":"ana"},"assignees":[{"id":3,"username":"bia"}],"created_at":"2026-09-01T12:00:00Z","user_notes_count":2}`)
		case path == p+"/issues/42/notes" && r.Method == http.MethodGet:
			if !signed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[{"body":"changed the title","system":true,"author":{"username":"ana"}},{"body":"on it","author":{"username":"bia"},"created_at":"2026-09-02T12:00:00Z"}]`)
		case path == p+"/members/all":
			_, _ = io.WriteString(w, `[{"id":3,"username":"bia"},{"id":9,"username":"caio"}]`)
		case path == p+"/labels":
			if !signed {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[{"name":"bug"},{"name":"ui"}]`)
		case path == p+"/merge_requests" && r.Method == http.MethodGet:
			if q.Get("source_branch") != "" {
				_, _ = io.WriteString(w, `[{"iid":8,"title":"fix grey","state":"opened","source_branch":"`+q.Get("source_branch")+`"}]`)
				return
			}
			_, _ = io.WriteString(w, `[{"iid":5,"title":"Draft: blue button","state":"opened","draft":true,"source_branch":"issue-42","target_branch":"main","author":{"username":"bia"},"updated_at":"2026-09-21T12:00:00Z"},
				{"iid":4,"title":"old","state":"merged","source_branch":"x","target_branch":"main","updated_at":"2026-09-10T12:00:00Z"}]`)
		case path == p+"/merge_requests/5" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"iid":5,"title":"Draft: blue button","state":"opened","draft":true,"changes_count":"3","detailed_merge_status":"mergeable","head_pipeline":{"status":"failed"},"description":"makes it blue"}`)
		case path == p+"/merge_requests/5/notes":
			_, _ = io.WriteString(w, `[]`)
		case path == p+"/issues/42/related_merge_requests":
			_, _ = io.WriteString(w, `[{"iid":5,"title":"blue","state":"opened"}]`)
		case r.Method != http.MethodGet:
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase, oldToken := GitLabBase, GitLabToken
	t.Cleanup(func() { GitLabBase, GitLabToken = oldBase, oldToken })
	GitLabBase = func(string) string { return srv.URL + "/api/v4" }
	return &wrote, func(token string) { GitLabToken = func(string) string { return token } }
}

// TestGitLabIssuesAndMergeRequestsReadAsGitHubs: through a stand-in of
// GitLab's API, issues are listed per preset — "opened" as open, the
// user's by scope, what was typed as GitLab's search — and read whole,
// GitLab's own notes left out of the conversation; comments, closing,
// reopening, a new issue and assignees by id are written as GitLab takes
// them; merge requests list and read with their pipeline as a check, the
// closed preset dropping the open; a squash merge, a draft made ready by
// its title, and rebase refused; and an issue's merge requests are GitLab's
// related ones and those on its branches. Without a token, what gitlab.com
// keeps to users says so. If it regresses, a GitLab project's panel is
// empty, or writes what GitLab rejects.
func TestGitLabIssuesAndMergeRequestsReadAsGitHubs(t *testing.T) {
	wrote, useToken := gitLabStandIn(t)
	r, _ := ParseSlug("gitlab.com/group/sub/app")

	useToken("")
	issues, _, err := List([]Repo{r}, FilterOpen, "grey")
	if err != nil || len(issues) != 1 || issues[0].State != "open" || issues[0].Repo != "gitlab.com/group/sub/app" ||
		issues[0].Title != "checkout is grey opened  grey" || issues[0].Assignees[0] != "bia" {
		t.Fatalf("open: %+v %v", issues, err)
	}
	if _, _, err := List([]Repo{r}, FilterMine, ""); !errors.Is(err, ErrGitLabToken) {
		t.Errorf("mine without a token: %v", err)
	}
	d, err := Get(r, 42)
	if err != nil || d.Body != "it is grey" || len(d.Thread) != 1 || !strings.Contains(d.Thread[0].Body, "tend gitlab login") {
		t.Errorf("detail without a token: %+v %v", d, err)
	}
	if _, err := Labels(r); !errors.Is(err, ErrGitLabToken) {
		t.Errorf("labels without a token: %v", err)
	}

	useToken("glpat-good")
	if mine, _, err := List([]Repo{r}, FilterMine, ""); err != nil || !strings.Contains(mine[0].Title, "assigned_to_me") {
		t.Errorf("mine: %+v %v", mine, err)
	}
	if closed, _, _ := List([]Repo{r}, FilterClosed, ""); closed[0].State != "closed" {
		t.Errorf("closed: %+v", closed)
	}
	d, err = Get(r, 42)
	if err != nil || len(d.Thread) != 1 || d.Thread[0].Author != "bia" || d.Thread[0].Body != "on it" {
		t.Errorf("detail: %+v %v", d.Thread, err)
	}
	if labels, err := Labels(r); err != nil || strings.Join(labels, ",") != "bug,ui" {
		t.Errorf("labels: %v %v", labels, err)
	}
	must := func(what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	must("comment", AddComment(r, 42, "on it too"))
	must("close", Close(r, 42, ReasonNotPlanned))
	must("reopen", Reopen(r, 42))
	n, u, err := Create(r, "new one", "text")
	must("create", err)
	if n != 43 || !strings.HasSuffix(u, "/issues/43") {
		t.Errorf("created %d %s", n, u)
	}
	must("edit", EditIssue(r, 42, Edit{Title: "blue", AddLabels: []string{"ui"}, RemoveLabels: []string{"bug"}, AddAssignees: []string{"caio"}, RemoveAssignees: []string{"bia"}}))
	if err := MergePR(r, 5, MergeRebase); err == nil {
		t.Error("rebase was asked of GitLab")
	}
	must("merge", MergePR(r, 5, MergeSquash))
	must("ready", ReadyPR(r, 5))
	must("close mr", ClosePR(r, 5))

	got := strings.Join(*wrote, "\n")
	for _, want := range []string{
		`POST /issues/42/notes {"body":"on it too"}`,
		`PUT /issues/42 {"state_event":"close"}`,
		`PUT /issues/42 {"state_event":"reopen"}`,
		`POST /issues {"description":"text","title":"new one"}`,
		`PUT /merge_requests/5/merge {"squash":true}`,
		`PUT /merge_requests/5 {"title":"blue button"}`,
		`PUT /merge_requests/5 {"state_event":"close"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	var edit map[string]any
	for _, line := range *wrote {
		if strings.HasPrefix(line, "PUT /issues/42 {\"add_labels") {
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "PUT /issues/42 ")), &edit)
		}
	}
	if edit["add_labels"] != "ui" || edit["remove_labels"] != "bug" || edit["title"] != "blue" || len(edit["assignee_ids"].([]any)) != 1 || edit["assignee_ids"].([]any)[0].(float64) != 9 {
		t.Errorf("edit sent %v", edit)
	}

	prs, err := ListPRs([]Repo{r}, PRFilterOpen, "")
	if err != nil || len(prs) != 2 || !prs[0].Draft || prs[0].Head != "issue-42" {
		t.Errorf("mrs: %+v %v", prs, err)
	}
	if closed, _ := ListPRs([]Repo{r}, PRFilterClosed, ""); len(closed) != 1 || closed[0].State != "merged" {
		t.Errorf("closed mrs: %+v", closed)
	}
	pd, err := GetPR(r, 5)
	if err != nil || pd.Files != 3 || pd.Mergeable != "MERGEABLE" || len(pd.CheckList) != 1 || pd.CheckList[0].State != CheckFail || pd.Checks.Fail != 1 {
		t.Errorf("mr detail: %+v %v", pd, err)
	}
	rel, err := PRsForIssue(r, 42, []string{"issue-42-grey"})
	if err != nil || len(rel) != 2 || rel[0].Number != 8 || rel[1].Number != 5 {
		t.Errorf("related: %+v %v", rel, err)
	}
	if who, err := GitLabWhoAmI("gitlab.com", "glpat-good"); err != nil || who != "akira" {
		t.Errorf("whoami: %q %v", who, err)
	}
	if _, err := GitLabWhoAmI("gitlab.com", "bad"); !errors.Is(err, ErrGitLabToken) {
		t.Errorf("a bad token: %v", err)
	}
}
