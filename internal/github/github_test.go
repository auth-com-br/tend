//go:build unix

package github

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestARemoteIsReadInEveryFormGitWritesIt: https, scp-like and ssh remotes
// on github.com name their repository, with or without .git; another host
// names none. If it regresses, a project cloned over ssh has "no GitHub
// remote".
func TestARemoteIsReadInEveryFormGitWritesIt(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/sousaakira/tend.git":        "sousaakira/tend",
		"https://github.com/sousaakira/tend":            "sousaakira/tend",
		"https://token@github.com/stablyai/orca.git":    "stablyai/orca",
		"git@github.com:sousaakira/orca.git":            "sousaakira/orca",
		"ssh://git@github.com/sousaakira/tend.git":      "sousaakira/tend",
		"ssh://git@github.com:22/sousaakira/tend":       "sousaakira/tend",
		"https://gitlab.com/someone/project.git":        "",
		"git@bitbucket.org:someone/project.git":         "",
		"https://github.com/sousaakira/tend/extra/path": "",
	} {
		owner, name, ok := ParseRemote(remote)
		got := ""
		if ok {
			got = owner + "/" + name
		}
		if got != want {
			t.Errorf("%s = %q, want %q", remote, got, want)
		}
	}
}

// TestAForkListsWhatItForkedFirst: a checkout with origin and upstream on
// GitHub gives upstream first, as Orca does, a GitLab remote after them,
// and one with no remote on either says so. If it regresses, a fork's issues list shows the fork's, which
// usually has none, with no word that upstream is there.
func TestAForkListsWhatItForkedFirst(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if _, err := ReposFor(dir); !errors.Is(err, ErrNoRepo) {
		t.Errorf("no remotes: %v", err)
	}
	git("remote", "add", "origin", "git@github.com:me/orca.git")
	git("remote", "add", "mirror", "https://gitlab.com/me/orca.git")
	git("remote", "add", "upstream", "https://github.com/stablyai/orca.git")
	repos, err := ReposFor(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A GitLab remote is a repository too (gitlab.go), after upstream and
	// origin as any other remote is.
	if len(repos) != 3 || repos[0].Slug() != "stablyai/orca" || repos[0].Remote != "upstream" || repos[1].Slug() != "me/orca" ||
		repos[2].Slug() != "gitlab.com/me/orca" || !repos[2].GitLab() {
		t.Errorf("repos: %+v", repos)
	}
}

// fakeGh puts a gh on PATH that records its arguments and prints out, or
// fails with stderr.
func fakeGh(t *testing.T, out, stderr string, code int) (argsFile string) {
	t.Helper()
	bin := t.TempDir()
	argsFile = filepath.Join(bin, "args")
	outFile := filepath.Join(bin, "out")
	if err := os.WriteFile(outFile, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\ncat > " + argsFile + ".stdin\ncat " + outFile + "\n"
	if stderr != "" {
		script += "echo '" + stderr + "' >&2\n"
	}
	script += "exit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return argsFile
}

// TestTheListIsGitHubsSearchWithWhatWasTyped: the list asks GitHub's issue
// search for the repository's issues in the preset, with what was typed
// after it, the last updated first, and leaves pull requests out. If it
// regresses, typing label:bug finds nothing, or PRs show among issues.
func TestTheListIsGitHubsSearchWithWhatWasTyped(t *testing.T) {
	args := fakeGh(t, `{"total_count":7,"items":[
		{"number":12,"title":"Crash on start","state":"open","html_url":"https://github.com/o/r/issues/12",
		 "user":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[{"login":"me"}],"comments":3,"updated_at":"2026-09-20T10:00:00Z"},
		{"number":13,"title":"A PR","state":"open","pull_request":{"url":"x"},"user":{"login":"bo"},"updated_at":"2026-09-20T10:00:00Z"}]}`, "", 0)
	issues, total, err := List([]Repo{{Owner: "o", Name: "r"}}, FilterMine, "label:bug crash")
	if err != nil {
		t.Fatal(err)
	}
	if total != 7 || len(issues) != 1 {
		t.Fatalf("total %d, issues %+v", total, issues)
	}
	i := issues[0]
	if i.Number != 12 || i.Author != "ana" || i.Labels[0] != "bug" || i.Assignees[0] != "me" || i.Comments != 3 || i.Updated.Day() != 20 {
		t.Errorf("issue: %+v", i)
	}
	got, _ := os.ReadFile(args)
	q := ""
	for _, a := range strings.Split(string(got), "\n") {
		if strings.HasPrefix(a, "search/issues?") {
			v, _ := url.ParseQuery(strings.TrimPrefix(a, "search/issues?"))
			q = v.Get("q") + " sort=" + v.Get("sort")
		}
	}
	if q != "repo:o/r is:issue is:open assignee:@me label:bug crash sort=updated" {
		t.Errorf("search: %q (args %q)", q, got)
	}
}

// TestAnIssueIsReadWithItsThread: one issue comes with its text, labels,
// assignees and comments. If it regresses, the detail shows a title and
// nothing under it.
func TestAnIssueIsReadWithItsThread(t *testing.T) {
	fakeGh(t, `{"number":12,"title":"Crash","state":"OPEN","url":"https://github.com/o/r/issues/12","body":"It crashes.",
		"author":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[{"login":"me"}],
		"comments":[{"author":{"login":"bo"},"body":"Same here.","createdAt":"2026-09-21T10:00:00Z"}],
		"createdAt":"2026-09-19T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}`, "", 0)
	d, err := Get(Repo{Owner: "o", Name: "r"}, 12)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "Crash" || d.State != "open" || d.Body != "It crashes." || len(d.Thread) != 1 || d.Thread[0].Author != "bo" || d.Comments != 1 {
		t.Errorf("detail: %+v", d)
	}
}

// TestWhatGhNeedsIsSaid: gh missing and gh not logged in are told apart
// from other failures, which keep gh's own first line. If it regresses, the
// list says "exit status 4" where it should say to run gh auth login.
func TestWhatGhNeedsIsSaid(t *testing.T) {
	fakeGh(t, "", "To get started with GitHub CLI, please run:  gh auth login", 4)
	if _, _, err := List([]Repo{{Owner: "o", Name: "r"}}, FilterOpen, ""); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("not logged in: %v", err)
	}
	fakeGh(t, "", "HTTP 404: Not Found (https://api.github.com/search/issues)", 1)
	if _, _, err := List([]Repo{{Owner: "o", Name: "r"}}, FilterOpen, ""); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("404: %v", err)
	}
	old := Gh
	Gh = "tend-no-such-gh"
	defer func() { Gh = old }()
	if _, _, err := List([]Repo{{Owner: "o", Name: "r"}}, FilterOpen, ""); !errors.Is(err, ErrNoGh) {
		t.Errorf("no gh: %v", err)
	}
}

// TestWhatIsWrittenGoesToGitHub: a comment's text goes on gh's standard
// input, a close carries its reason, a reopen is a reopen, and a new issue
// comes back with its number. If it regresses, a comment with a quote in it
// is cut on a command line, or an issue closed "not planned" is closed as
// done.
func TestWhatIsWrittenGoesToGitHub(t *testing.T) {
	repo := Repo{Owner: "o", Name: "r"}
	args := fakeGh(t, "", "", 0)
	if err := AddComment(repo, 12, "It's fixed in \"main\".\nSecond line."); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(args)
	in, _ := os.ReadFile(args + ".stdin")
	if string(got) != "issue\ncomment\n12\n--repo\no/r\n--body-file\n-\n" || string(in) != "It's fixed in \"main\".\nSecond line." {
		t.Errorf("comment: %q, stdin %q", got, in)
	}
	if err := AddComment(repo, 12, "  "); err == nil {
		t.Error("an empty comment was sent")
	}

	if err := Close(repo, 12, ReasonNotPlanned); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(args); string(got) != "issue\nclose\n12\n--repo\no/r\n--reason\nnot planned\n" {
		t.Errorf("close: %q", got)
	}
	if err := Reopen(repo, 12); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(args); string(got) != "issue\nreopen\n12\n--repo\no/r\n" {
		t.Errorf("reopen: %q", got)
	}

	fakeGh(t, "Creating issue in o/r\n\nhttps://github.com/o/r/issues/57\n", "", 0)
	n, url, err := Create(repo, "Footer overlaps", "On mobile.")
	if err != nil || n != 57 || url != "https://github.com/o/r/issues/57" {
		t.Errorf("create = %d %q %v", n, url, err)
	}
}

// TestAnIssuesBranchIsNamedAfterIt: the branch work on an issue goes on
// carries its number and a slug of its title, and is found again by the
// number whatever the title became. If it regresses, starting work twice on
// one issue makes two worktrees.
func TestAnIssuesBranchIsNamedAfterIt(t *testing.T) {
	for title, want := range map[string]string{
		"[Bug]: Checkout button is grey!":          "issue-42-bug-checkout-button-is-grey",
		"Ação com acentos":                         "issue-42-acao-com-acentos",
		"Teste: começar a trabalhar cria worktree": "issue-42-teste-comecar-a-trabalhar-cria-worktree",
		"Über Straße, déjà vu":                     "issue-42-uber-strasse-deja-vu",
		"???":                                      "issue-42",
		"A very long title that goes on and on well past the edge": "issue-42-a-very-long-title-that-goes-on-and-on",
	} {
		got := IssueBranch(42, title)
		if got != want {
			t.Errorf("%q = %q, want %q", title, got, want)
		}
		if !IsIssueBranch(got, 42) || IsIssueBranch(got, 4) {
			t.Errorf("%q is not found again by its number alone", got)
		}
	}
}

// TestAnEditSendsOnlyWhatChanged: an edit is one gh issue edit with the new
// title and each label and assignee added or taken off, and none at all
// when nothing changed; the pickers read the repository's labels and who
// can be assigned. If it regresses, saving a label list puts back labels
// somebody else took off meanwhile, or an edit with nothing in it errors.
func TestAnEditSendsOnlyWhatChanged(t *testing.T) {
	repo := Repo{Owner: "o", Name: "r"}
	args := fakeGh(t, "bug\ngood first issue\n", "", 0)
	labels, err := Labels(repo)
	if err != nil || len(labels) != 2 || labels[1] != "good first issue" {
		t.Errorf("labels = %q, %v", labels, err)
	}
	if got, _ := os.ReadFile(args); !strings.Contains(string(got), "repos/o/r/labels?per_page=100\n--jq\n.[].name") {
		t.Errorf("labels asked: %q", got)
	}

	err = EditIssue(repo, 12, Edit{Title: "New title", AddLabels: []string{"bug", "help wanted"}, RemoveLabels: []string{"question"},
		AddAssignees: []string{"ana"}, RemoveAssignees: []string{"bo"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "issue\nedit\n12\n--repo\no/r\n--title\nNew title\n--add-label\nbug\n--add-label\nhelp wanted\n--remove-label\nquestion\n--add-assignee\nana\n--remove-assignee\nbo\n"
	if got, _ := os.ReadFile(args); string(got) != want {
		t.Errorf("edit: %q", got)
	}
	if err := os.Remove(args); err != nil {
		t.Fatal(err)
	}
	if err := EditIssue(repo, 12, Edit{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(args); !os.IsNotExist(err) {
		t.Error("an edit with nothing in it ran gh")
	}
}

// TestAFolderOfRepositoriesIsEachOfThem: a folder that is not a checkout,
// with checkouts under it — a project of several, the owner's mvno — is
// every one of them on GitHub, each with the checkout it is in, in name
// order; one not on GitHub, and one under node_modules, are left out; and a
// checkout is itself, as it was. The search asks for all of them at once.
// If it regresses, a project made of several repositories has "no GitHub
// remote".
func TestAFolderOfRepositoriesIsEachOfThem(t *testing.T) {
	root := t.TempDir()
	repo := func(path, remote string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", path, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if remote != "" {
			if out, err := exec.Command("git", "-C", path, "remote", "add", "origin", remote).CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
		}
	}
	repo(filepath.Join(root, "mvno-painel"), "git@github.com:hot-spot/mvno-painel.git")
	repo(filepath.Join(root, "mvno-api"), "git@github.com:hot-spot/mvno-api.git")
	repo(filepath.Join(root, "apps", "mvno-app"), "https://github.com/hot-spot/mvno-app")
	repo(filepath.Join(root, "local-only"), "")
	repo(filepath.Join(root, "web", "node_modules", "dep"), "https://github.com/someone/dep")
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}

	scope, err := ScopeFor(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range scope.Repos {
		got = append(got, r.Slug()+" "+filepath.Base(r.Dir))
	}
	if !scope.Multi || strings.Join(got, ", ") != "hot-spot/mvno-api mvno-api, hot-spot/mvno-app mvno-app, hot-spot/mvno-painel mvno-painel" {
		t.Errorf("scope: %v %q", scope.Multi, got)
	}
	if q := SearchQuery(scope.Repos, FilterOpen, "crash"); q != "repo:hot-spot/mvno-api repo:hot-spot/mvno-app repo:hot-spot/mvno-painel is:issue is:open crash" {
		t.Errorf("search: %q", q)
	}

	one, err := ScopeFor(filepath.Join(root, "mvno-api"))
	if err != nil || one.Multi || len(one.Repos) != 1 || one.Repos[0].Dir != filepath.Join(root, "mvno-api") {
		t.Errorf("a checkout: %+v %v", one, err)
	}
	if _, err := ScopeFor(filepath.Join(root, "docs")); !errors.Is(err, ErrNoRepo) {
		t.Errorf("a folder of none: %v", err)
	}
}
