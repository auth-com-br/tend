package core

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
)

// The issues, errors and tickets tabs against stand-ins: gh is a script
// answering as GitHub does (as cmd/tend's issues tests have it), GlitchTip
// and the Issue Gateway are httptest servers answering as their APIs do,
// claude is a script saying what it was given. git and the tend server are
// real. None of them is the real service: what is proven is that the tab
// asks what the TUI's panel asks, and does with the answer what it does.

// standIns puts gh and claude on PATH, and a settings file whose worktrees
// go to a directory of the test's, before the server starts; it returns
// the file gh writes what it was asked to.
func standIns(t *testing.T) (asked string) {
	t.Helper()
	bin := t.TempDir()
	asked = filepath.Join(bin, "asked")
	gh := `#!/bin/sh
printf '%s\n' "$*" >> ` + asked + `
case "$1 $2" in
"issue view") cat <<'JSON'
{"number":42,"title":"Checkout button is grey","state":"OPEN","url":"https://github.com/acme/shop/issues/42",
 "body":"Make it **green**.","author":{"login":"ana"},"labels":[],"assignees":[],
 "comments":[{"author":{"login":"bo"},"body":"Seen it too.","createdAt":"2026-09-21T09:00:00Z"}],
 "createdAt":"2026-09-20T10:00:00Z","updatedAt":"2026-09-21T10:00:00Z"}
JSON
;;
"issue comment") cat > ` + asked + `.comment ;;
"issue close"|"issue reopen") ;;
"issue create") cat > ` + asked + `.new; echo https://github.com/acme/shop/issues/42 ;;
"pr list")
  case "$*" in
  *statusCheckRollup*) echo '[{"number":9,"statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]}]' ;;
  *) echo '[{"number":9,"title":"Make checkout green","state":"OPEN","isDraft":false,"author":{"login":"bo"},"labels":[],"headRefName":"issue-42-checkout","baseRefName":"main","updatedAt":"2026-09-21T10:00:00Z","url":"https://github.com/acme/shop/pull/9","reviewDecision":"APPROVED"}]' ;;
  esac ;;
"pr view") cat <<'JSON'
{"number":9,"title":"Make checkout green","state":"OPEN","isDraft":false,"author":{"login":"bo"},"labels":[],"headRefName":"issue-42-checkout",
 "baseRefName":"main","updatedAt":"2026-09-21T10:00:00Z","url":"https://github.com/acme/shop/pull/9","reviewDecision":"APPROVED",
 "statusCheckRollup":[{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE"}],
 "body":"Fixes the colour.","createdAt":"2026-09-20T10:00:00Z","additions":10,"deletions":2,"changedFiles":1,"mergeable":"MERGEABLE",
 "comments":[],"latestReviews":[]}
JSON
;;
"pr merge") ;;
"api graphql") echo '{"data":{"repository":{"issue":{"closedByPullRequestsReferences":{"nodes":[{"number":9}]}}}}}' ;;
*) cat <<'JSON'
{"total_count":1,"items":[{"number":42,"title":"Checkout button is grey","state":"open","html_url":"https://github.com/acme/shop/issues/42","user":{"login":"ana"},"labels":[{"name":"bug"}],"assignees":[],"comments":1,"updated_at":"2026-09-21T10:00:00Z"}]}
JSON
;;
esac
`
	claude := "#!/bin/sh\necho \"claude was given: $*\"\necho \"on: $(git branch --show-current)\"\nexec sleep 60\n"
	for name, body := range map[string]string{"gh": gh, "claude": claude} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	settings := "onboarding = false\n\n[worktrees]\ndirectory = \"" + t.TempDir() + "\"\n"
	if err := os.WriteFile(cfg, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_CONFIG", cfg)
	return asked
}

// shopRepo is a repository whose origin is GitHub's acme/shop.
func shopRepo(t *testing.T) string {
	t.Helper()
	project := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"},
		{"remote", "add", "origin", "https://github.com/acme/shop.git"}} {
		cmd := exec.Command("git", append([]string{"-C", project, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return project
}

// inProject shows a space of its own whose directory is the project, with
// a shell working there in focus: what the tabs are opened from.
func inProject(t *testing.T, s *Session, project string) uint64 {
	t.Helper()
	ws, err := s.client.NewWorkspaceIn("shop", project)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.client.NewTab(ws, "shop", proto.PaneSpec{Dir: project}); err != nil {
		t.Fatal(err)
	}
	// Read first: ShowWorkspace goes to a space the window knows of.
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	if err := s.ShowWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	return ws
}

// shownText is what the pane in focus shows, its panes sized first as the
// window's terminal items size them when they are laid out: a pane no item
// has sized is never drawn.
func shownText(s *Session) string {
	s.mu.Lock()
	rects := append([]proto.PaneRect(nil), s.rects...)
	s.mu.Unlock()
	for _, r := range rects {
		_ = s.ResizePane(r.Pane, r.Cols-2, r.Rows-2)
	}
	return screenText(s, s.Focused())
}

// dumpDock logs what the dock and the pane in focus showed when a test
// ends, which go test prints only when it failed: a stand-in's answer the
// tab misread is then in the log, not guessed at.
func dumpDock(t *testing.T, s *Session) {
	b, _ := json.Marshal(s.State().Dock)
	t.Logf("dock: %s", b)
	t.Logf("screen: %s", shownText(s))
}

func waitForFile(t *testing.T, path, want string) {
	t.Helper()
	waitFor(t, want+" in "+filepath.Base(path), func() bool {
		b, _ := os.ReadFile(path)
		return strings.Contains(string(b), want)
	})
}

// TestTheIssuesTabIsTheIssuesPanel: the tab lists the project's issues as
// GitHub's search answers, opens one with its body and thread, comments on
// it as written, closes it as not planned, files a new one, starts the
// agent on it in a worktree on the issue's branch — and goes to that
// worktree the second time instead of making another; and lists the pull
// requests with their checks filled in, opens one and merges it by squash.
// If it regresses, the window's issues do not do what the TUI's do: a
// comment cut, an issue closed for the wrong reason, work on one issue
// begun twice, a merge made the wrong way.
func TestTheIssuesTabIsTheIssuesPanel(t *testing.T) {
	asked := standIns(t)
	project := shopRepo(t)
	s := openTest(t)
	s.cfg.Issues.Agent = "claude"
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	inProject(t, s, project)
	issues := func() *IssuesState {
		d := s.State().Dock
		if d == nil {
			return nil
		}
		return d.Issues
	}

	s.ToggleDock("issues")
	defer dumpDock(t, s)
	waitFor(t, "the list", func() bool {
		i := issues()
		return i != nil && len(i.Issues) == 1 && i.Issues[0].Title == "Checkout button is grey" && i.Repo == "acme/shop"
	})
	if got := issues().Issues[0].Labels; len(got) != 1 || got[0] != "bug" {
		t.Errorf("labels %v", got)
	}
	if err := s.Issues(IssuesAction{Do: "search", Query: "label:bug crash"}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, asked, "label%3Abug+crash")

	if err := s.Issues(IssuesAction{Do: "open", Number: 42}); err != nil {
		t.Fatal(err)
	}
	d := issues().Issue
	if d == nil || d.Body != "Make it **green**." || len(d.Thread) != 1 || d.Thread[0].Body != "Seen it too." {
		t.Fatalf("the issue open: %+v", d)
	}
	if err := s.Issues(IssuesAction{Do: "comment", Body: "It's \"fixed\" on main.\nSee the PR."}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, asked+".comment", "It's \"fixed\" on main.\nSee the PR.")
	if err := s.Issues(IssuesAction{Do: "state", State: "closed", Reason: "not planned"}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, asked, "issue close 42 --repo acme/shop --reason not planned")
	if msg := issues().Message; msg != "closed #42 as not planned" {
		t.Errorf("after closing: %q", msg)
	}
	if err := s.Issues(IssuesAction{Do: "create", Title: "Footer overlaps", Body: "On **mobile**."}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, asked, "issue create --repo acme/shop --title Footer overlaps --body-file -")
	waitForFile(t, asked+".new", "On **mobile**.")

	if err := s.Issues(IssuesAction{Do: "work"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent on the issue's branch", func() bool {
		text := shownText(s)
		return strings.Contains(text, "claude was given: Complete https://github.com/acme/shop/issues/42") &&
			strings.Contains(text, "on: issue-42-checkout-button-is-grey")
	})
	if err := s.Issues(IssuesAction{Do: "work"}); err != nil {
		t.Fatal(err)
	}
	if msg := issues().Message; msg != "#42 already has a worktree: issue-42-checkout-button-is-grey" {
		t.Errorf("the second start: %q", msg)
	}
	out, _ := exec.Command("git", "-C", project, "worktree", "list").Output()
	if n := strings.Count(string(out), "[issue-42"); n != 1 {
		t.Errorf("worktrees for #42: %d\n%s", n, out)
	}

	if err := s.Issues(IssuesAction{Do: "back"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Issues(IssuesAction{Do: "mode", PullRequests: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pull requests, checks filled in", func() bool {
		i := issues()
		return i != nil && i.PullRequests && len(i.PRs) == 1 && i.PRs[0].Fail == 1 && i.PRs[0].Review == "APPROVED"
	})
	if err := s.Issues(IssuesAction{Do: "openpr", Number: 9}); err != nil {
		t.Fatal(err)
	}
	pr := issues().PR
	if pr == nil || pr.Additions != 10 || len(pr.Checks) != 1 || pr.Checks[0].Name != "lint" {
		t.Fatalf("the pull request open: %+v", pr)
	}
	if err := s.Issues(IssuesAction{Do: "pr", Action: "merge", Method: "squash"}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, asked, "pr merge 9 --repo acme/shop --squash")
	if msg := issues().Message; msg != "merged #9 (squash)" {
		t.Errorf("after merging: %q", msg)
	}
}

// fakeGlitchTip answers as GlitchTip does, for one organization, and
// records what was changed.
func fakeGlitchTip(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var changed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/0/organizations/":
			_, _ = io.WriteString(w, `[{"slug":"shop","name":"Shop"}]`)
		case p == "/api/0/organizations/shop/projects/":
			_, _ = io.WriteString(w, `[{"slug":"api","name":"api"}]`)
		case p == "/api/0/organizations/shop/issues/":
			_, _ = io.WriteString(w, `[{"id":"75","shortId":"API-26","title":"KnexTimeoutError: pool full","culprit":"ApiController","level":"error","status":"unresolved","count":"1543","userCount":2,"firstSeen":"2026-08-01T10:00:00Z","lastSeen":"2026-08-09T12:00:00Z","project":{"slug":"api"}}]`)
		case p == "/api/0/issues/75/events/latest/":
			_, _ = io.WriteString(w, `{"eventID":"e1","title":"KnexTimeoutError","platform":"node","dateCreated":"2026-08-09T12:00:00Z",
				"tags":[{"key":"environment","value":"production"}],
				"entries":[{"type":"exception","data":{"values":[{"type":"KnexTimeoutError","value":"pool full","stacktrace":{"frames":[
				   {"absPath":"/app/src/models/Score.js","lineNo":144,"function":"complement","inApp":true,"context_line":"  await pg('t').first();"}]}}]}}]}`)
		case p == "/api/0/issues/75/" && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			changed = append(changed, "75 "+body["status"])
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), changed...)
	}
}

// TestTheErrorsTabIsTheErrorsPanel: with a server kept in the settings,
// the tab lists its errors, opens one with its stack, marks it resolved on
// the server, and types it, with its stack, into the pane it was opened
// from, submitting nothing. GlitchTip is a stand-in of its API. If it
// regresses, the window cannot see or fix the errors the TUI can.
func TestTheErrorsTabIsTheErrorsPanel(t *testing.T) {
	srv, changed := fakeGlitchTip(t)
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	pane := s.Focused()
	s.cfg.Errors.Sources = map[string]config.ErrorSource{"shop": {URL: srv.URL, Token: "good"}}
	errs := func() *ErrorsState { return s.State().Dock.Errors }

	s.ToggleDock("errors")
	defer dumpDock(t, s)
	waitFor(t, "the list", func() bool {
		d := s.State().Dock
		return d != nil && d.Errors != nil && len(d.Errors.Errors) == 1 && len(d.Errors.Scopes) == 2
	})
	if e := errs(); !e.Connected || e.Errors[0].ShortID != "API-26" || e.Errors[0].Count != 1543 {
		t.Fatalf("the list: %+v", e)
	}
	if err := s.Errors(ErrorsAction{Do: "open", ID: "75"}); err != nil {
		t.Fatal(err)
	}
	open := errs().Open
	if open == nil || len(open.Exceptions) != 1 || len(open.Exceptions[0].Frames) != 1 ||
		open.Exceptions[0].Frames[0].Function != "complement" || !open.Exceptions[0].Frames[0].InApp {
		t.Fatalf("the error open: %+v", open)
	}
	if err := s.Errors(ErrorsAction{Do: "set", Status: "resolved"}); err != nil {
		t.Fatal(err)
	}
	if got := changed(); len(got) != 1 || got[0] != "75 resolved" {
		t.Errorf("changed on the server: %v", got)
	}
	if err := s.Errors(ErrorsAction{Do: "fix"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the error typed into the pane", func() bool {
		return strings.Contains(shownText(s), "KnexTimeoutError") && s.Focused() == pane
	})
}

// TestTheTicketsTabIsTheTicketsPanel: with an Issue Gateway account kept
// in the settings, the tab lists its tickets, opens one with its comments,
// comments on it, marks it done, and starts the agent on it in a worktree
// of the space's project on ticket-<key>. The gateway is a stand-in of its
// contract. If it regresses, the window's tickets do not do what the TUI's
// do.
func TestTheTicketsTabIsTheTicketsPanel(t *testing.T) {
	standIns(t)
	project := shopRepo(t)
	var mu sync.Mutex
	var did []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ig_1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/v1/scopes":
			_, _ = io.WriteString(w, `{"items":[{"id":"s-1","name":"Recarga"}]}`)
		case r.URL.Path == "/v1/issues":
			_, _ = io.WriteString(w, `{"items":[{"key":"IG-12","ref":"u-12","title":"recarga falha","state":"published","done":false,"assignee":"akira","scope":"Recarga","updated":"2026-09-27T12:00:00Z","url":null}]}`)
		case r.URL.Path == "/v1/issues/u-12" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"key":"IG-12","ref":"u-12","title":"recarga falha","state":"published","done":false,"assignee":"akira","scope":"Recarga","updated":"2026-09-27T12:00:00Z","url":null,
				"body":"**falha** no pix","comments":[{"author":"ana","body":"older","created":"2026-09-25T00:00:00Z"}]}`)
		case r.URL.Path == "/v1/issues/u-12/comments" && r.Method == "POST":
			mu.Lock()
			did = append(did, "comment "+string(body))
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/v1/issues/u-12/close" && r.Method == "POST":
			mu.Lock()
			did = append(did, "close")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := openTest(t)
	s.cfg.Issues.Agent = "claude"
	s.cfg.Tickets.Sources = map[string]config.TicketSource{"ig": {Kind: "issuegateway", URL: srv.URL + "/", Token: "ig_1"}}
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	inProject(t, s, project)
	tk := func() *TicketsState { return s.State().Dock.Tickets }

	s.ToggleDock("tickets")
	defer dumpDock(t, s)
	waitFor(t, "the list", func() bool {
		d := s.State().Dock
		return d != nil && d.Tickets != nil && len(d.Tickets.Tickets) == 1
	})
	if got := tk(); !got.Connected || got.Tickets[0].Key != "IG-12" || len(got.Scopes) != 2 {
		t.Fatalf("the list: %+v", got)
	}
	if err := s.Tickets(TicketsAction{Do: "open", Key: "IG-12"}); err != nil {
		t.Fatal(err)
	}
	if o := tk().Open; o == nil || o.Body != "**falha** no pix" || len(o.Comments) != 1 {
		t.Fatalf("the ticket open: %+v", o)
	}
	if err := s.Tickets(TicketsAction{Do: "comment", Body: "on it"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Tickets(TicketsAction{Do: "done"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := strings.Join(did, " | ")
	mu.Unlock()
	if !strings.Contains(got, "comment ") || !strings.Contains(got, "on it") || !strings.HasSuffix(got, "close") {
		t.Errorf("asked of the tracker: %s", got)
	}
	if o := tk().Open; o == nil || !o.Done {
		t.Errorf("not shown done: %+v", o)
	}
	if err := s.Tickets(TicketsAction{Do: "worktree"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent on the ticket's branch", func() bool {
		text := shownText(s)
		return strings.Contains(text, "claude was given:") && strings.Contains(text, "on: ticket-ig-12")
	})
}
