package api

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
)

// gitRepo is a repository with one commit, for the worktree methods to work on.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "sub", "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return dir
}

// TestAWorktreeSpaceJoinsTheCompaniesOfTheSpaceItCameFrom: a worktree
// created or opened from a space in a company — named by workspace_id, as the
// space menu and the errors and tickets panels do, or by a directory inside
// it, as the issues panel and the command line do — gets a space in that
// company and in no other; one already open but outside it is filed there
// when it is opened again. If it regresses, a worktree opened while a company
// is chosen shows only under "all spaces" (issue #34).
func TestAWorktreeSpaceJoinsTheCompaniesOfTheSpaceItCameFrom(t *testing.T) {
	srv, err := server.New(server.Config{DefaultSize: pty.Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	a := New(srv, "test-build", []string{"/bin/sh", "-c", "sleep 30"})
	t.Cleanup(func() {
		a.Close()
		_ = srv.Close()
	})
	a.SetWorktreeDir(t.TempDir())

	// Another space first, so "the first space" is not the origin by luck.
	if _, err := srv.NewWorkspaceIn("elsewhere", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	repo := gitRepo(t)
	origin, err := srv.NewWorkspaceIn("project", repo)
	if err != nil {
		t.Fatal(err)
	}
	company := func(name string) session.CompanyID {
		t.Helper()
		made, err := srv.Company(proto.MethodCompanyCreate, proto.CompanyParams{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return session.CompanyID(made.(proto.CompanyCreateResult).Company)
	}
	auth, other := company("Auth"), company("Other")
	if _, err := srv.Company(proto.MethodCompanyAssign, proto.CompanyParams{Company: uint64(auth), Workspace: uint64(origin)}); err != nil {
		t.Fatal(err)
	}

	worktreeCall := func(method string, params map[string]any) session.WorkspaceID {
		t.Helper()
		raw, _ := json.Marshal(params)
		res, _, err := a.callWorktrees(Request{Method: method, Params: raw})
		if err != nil {
			t.Fatalf("%s %v: %v", method, params, err)
		}
		// As a script reads it: the workspace in the reply is a struct.
		var reply struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
		}
		body, _ := json.Marshal(res)
		_ = json.Unmarshal(body, &reply)
		id, ok := parseID("w_", reply.Workspace.ID)
		if !ok {
			t.Fatalf("%s answered %v", method, res)
		}
		return session.WorkspaceID(id)
	}
	companiesOf := func(ws session.WorkspaceID) []session.CompanyID {
		var out []session.CompanyID
		srv.Session(func(sess *session.Session) { out = sess.CompaniesOf(ws) })
		return out
	}

	fromSpace := worktreeCall(MethodWorktreeCreate, map[string]any{
		"workspace_id": WorkspaceID(origin), "branch": "from-space",
	})
	if got := companiesOf(fromSpace); !slices.Equal(got, []session.CompanyID{auth}) {
		t.Errorf("created from the space: in %v, want Auth (%d) only; Other is %d", got, auth, other)
	}

	fromDir := worktreeCall(MethodWorktreeCreate, map[string]any{
		"cwd": filepath.Join(repo, "sub"), "branch": "from-dir",
	})
	if got := companiesOf(fromDir); !slices.Equal(got, []session.CompanyID{auth}) {
		t.Errorf("created from a directory in the space: in %v, want Auth only", got)
	}

	// Opened before this was so: open, and in no company.
	if _, err := srv.Company(proto.MethodCompanyUnassign, proto.CompanyParams{Company: uint64(auth), Workspace: uint64(fromSpace)}); err != nil {
		t.Fatal(err)
	}
	again := worktreeCall(MethodWorktreeOpen, map[string]any{
		"workspace_id": WorkspaceID(origin), "branch": "from-space",
	})
	if again != fromSpace {
		t.Fatalf("opening an open worktree made space %d, want %d", again, fromSpace)
	}
	if got := companiesOf(again); !slices.Equal(got, []session.CompanyID{auth}) {
		t.Errorf("opened again from the space: in %v, want Auth only", got)
	}
}

// TestASpaceBroughtBackAfterARefusedRemoveIsInItsCompaniesAgain: a forced
// remove shuts the space before git is asked, and brings it back when git
// refuses — a locked worktree takes more than one --force. Shutting it took it
// out of every company. If it regresses, the space comes back only under "all
// spaces", as if the remove had half happened.
func TestASpaceBroughtBackAfterARefusedRemoveIsInItsCompaniesAgain(t *testing.T) {
	srv, err := server.New(server.Config{DefaultSize: pty.Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	a := New(srv, "test-build", []string{"/bin/sh", "-c", "sleep 30"})
	t.Cleanup(func() {
		a.Close()
		_ = srv.Close()
	})
	a.SetWorktreeDir(t.TempDir())
	repo := gitRepo(t)
	origin, err := srv.NewWorkspaceIn("project", repo)
	if err != nil {
		t.Fatal(err)
	}
	made, err := srv.Company(proto.MethodCompanyCreate, proto.CompanyParams{Name: "Auth", Workspace: uint64(origin)})
	if err != nil {
		t.Fatal(err)
	}
	auth := session.CompanyID(made.(proto.CompanyCreateResult).Company)

	call := func(method string, params map[string]any) (any, error) {
		raw, _ := json.Marshal(params)
		res, _, err := a.callWorktrees(Request{Method: method, Params: raw})
		return res, err
	}
	res, err := call(MethodWorktreeCreate, map[string]any{"workspace_id": WorkspaceID(origin), "branch": "locked"})
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Worktree struct {
			Path string `json:"path"`
		} `json:"worktree"`
		Workspace struct {
			ID string `json:"workspace_id"`
		} `json:"workspace"`
	}
	body, _ := json.Marshal(res)
	_ = json.Unmarshal(body, &reply)
	if out, err := exec.Command("git", "-C", repo, "worktree", "lock", reply.Worktree.Path).CombinedOutput(); err != nil {
		t.Fatalf("git worktree lock: %v\n%s", err, out)
	}

	if _, err := call(MethodWorktreeRemove, map[string]any{"workspace_id": reply.Workspace.ID, "force": true}); err == nil {
		t.Fatal("git removed a locked worktree with one --force; the test needs a refusal")
	}
	var back session.WorkspaceID
	srv.Session(func(sess *session.Session) {
		for _, w := range sess.Workspaces() {
			if w.Dir == reply.Worktree.Path {
				back = w.ID
			}
		}
	})
	if back == 0 {
		t.Fatal("the space did not come back after the refused remove")
	}
	var got []session.CompanyID
	srv.Session(func(sess *session.Session) { got = sess.CompaniesOf(back) })
	if !slices.Equal(got, []session.CompanyID{auth}) {
		t.Errorf("the space came back in %v, want Auth (%d)", got, auth)
	}
}

// TestACallWithNoOriginFilesTheWorktreeNowhere: a worktree call that names
// no space and no directory — a script's; no screen of tend's makes one —
// or a directory in no space has no space it came from, so its worktree
// goes into no company, rather than into whatever company the first space
// happens to be in. If it regresses, a script's worktree turns up in a
// company it has nothing to do with.
func TestACallWithNoOriginFilesTheWorktreeNowhere(t *testing.T) {
	h := start(t)
	a := &API{srv: h.srv}
	if got := a.originSpace("", ""); got != 0 {
		t.Errorf("no workspace and no directory: origin %d, want none", got)
	}
	if got := a.originSpace("", t.TempDir()); got != 0 {
		t.Errorf("a directory in no space: origin %d, want none", got)
	}
}
