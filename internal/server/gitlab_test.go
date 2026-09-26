//go:build unix

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/proto"
)

// TestAGitLabProjectListsItsIssuesThroughThePanelsMethods: a pane working
// in a checkout whose origin is on a GitLab gets that project's issues from
// github.issues, opens one with github.issue, and its merge requests with
// github.prs — the methods the issues panel calls, with the slug the panel
// is given sent back. GitLab's API is a stand-in. If it regresses, the
// panel says "no GitHub remote" in a GitLab project, or opens the wrong one.
func TestAGitLabProjectListsItsIssuesThroughThePanelsMethods(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const p = "/api/v4/projects/team%2Fapp"
		switch r.URL.EscapedPath() {
		case p + "/issues":
			_, _ = io.WriteString(w, `[{"iid":7,"title":"login is slow","state":"opened","author":{"username":"ana"},"updated_at":"2026-09-20T12:00:00Z"}]`)
		case p + "/issues/7":
			_, _ = io.WriteString(w, `{"iid":7,"title":"login is slow","state":"opened","description":"takes 9s"}`)
		case p + "/issues/7/notes":
			_, _ = io.WriteString(w, `[{"body":"seen it","author":{"username":"bia"}}]`)
		case p + "/merge_requests":
			_, _ = io.WriteString(w, `[{"iid":3,"title":"cache sessions","state":"opened","source_branch":"issue-7","target_branch":"main"}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()
	oldBase, oldToken := github.GitLabBase, github.GitLabToken
	defer func() { github.GitLabBase, github.GitLabToken = oldBase, oldToken }()
	github.GitLabBase = func(host string) string {
		if host != "gitlab.acme.com" {
			t.Errorf("asked %s", host)
		}
		return api.URL + "/api/v4"
	}
	github.GitLabToken = func(string) string { return "glpat-x" }

	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@gitlab.acme.com:team/app.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("needs git: %v %s", err, out)
		}
	}
	s := persistentServer(t, filepath.Join(t.TempDir(), "s.json"))
	ws, err := s.NewWorkspaceIn("app", dir)
	if err != nil {
		t.Fatal(err)
	}
	_, pane, err := s.NewTab(ws, "t", PaneSpec{Command: []string{"/bin/sh"}, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.GitHubIssues(proto.GitHubIssuesParams{Pane: uint64(pane)})
	if err != nil {
		t.Fatal(err)
	}
	if list.Repo != "gitlab.acme.com/team/app" || len(list.Issues) != 1 || list.Issues[0].Title != "login is slow" || list.Issues[0].State != "open" {
		t.Fatalf("list %+v", list)
	}
	d, err := s.GitHubIssue(proto.GitHubIssueParams{Repo: list.Repo, Number: 7})
	if err != nil || d.Body != "takes 9s" || len(d.Thread) != 1 || d.Thread[0].Author != "bia" {
		t.Errorf("issue %+v %v", d, err)
	}
	prs, err := s.GitHubPRs(proto.GitHubIssuesParams{Pane: uint64(pane)})
	if err != nil || len(prs.PRs) != 1 || prs.PRs[0].Head != "issue-7" || prs.PRs[0].Repo != list.Repo {
		t.Errorf("merge requests %+v %v", prs, err)
	}
}
