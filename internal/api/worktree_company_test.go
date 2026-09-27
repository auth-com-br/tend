package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
)

// TestAWorktreeJoinsTheCompanyOfTheSpaceItCameFrom: a worktree made from a
// space in a company opens as a space in that company, and one opened
// again from another space of another company joins that one too — the
// company shown in the sidebar then lists it without choosing "all spaces".
// If it regresses, starting work on an issue from a company's project
// leaves the worktree out of the company, as #34 found.
func TestAWorktreeJoinsTheCompanyOfTheSpaceItCameFrom(t *testing.T) {
	h := start(t)
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("needs git: %v %s", err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")

	app, err := h.srv.NewWorkspaceIn("app", repo)
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.srv.Company(proto.MethodCompanyCreate, proto.CompanyParams{Name: "Acme", Workspace: uint64(app)})
	if err != nil {
		t.Fatal(err)
	}
	acme := res.(proto.CompanyCreateResult).Company

	made := result(t, call(t, h, MethodWorktreeCreate, map[string]any{
		"workspace_id": WorkspaceID(app), "branch": "fix-1", "path": filepath.Join(t.TempDir(), "fix-1"),
	}))
	ws, _ := made["workspace"].(map[string]any)
	id, ok := parseID("w_", text(ws["workspace_id"]))
	if !ok {
		t.Fatalf("created %v", made)
	}
	if got := h.srv.CompaniesOf(session.WorkspaceID(id)); !slices.Equal(got, []session.CompanyID{session.CompanyID(acme)}) {
		t.Errorf("the worktree's space is in %v, want Acme's %d", got, acme)
	}

	// Opened again from a space of another company, it joins that one too.
	other, _ := h.srv.NewWorkspaceIn("app too", repo)
	res, _ = h.srv.Company(proto.MethodCompanyCreate, proto.CompanyParams{Name: "Globex", Workspace: uint64(other)})
	globex := res.(proto.CompanyCreateResult).Company
	result(t, call(t, h, MethodWorktreeOpen, map[string]any{"workspace_id": WorkspaceID(other), "branch": "fix-1"}))
	if got := h.srv.CompaniesOf(session.WorkspaceID(id)); len(got) != 2 || !slices.Contains(got, session.CompanyID(globex)) {
		t.Errorf("opened again, it is in %v", got)
	}

	// From a space in no company, a worktree is in none.
	lone, _ := h.srv.NewWorkspaceIn("lone", repo)
	made = result(t, call(t, h, MethodWorktreeCreate, map[string]any{
		"workspace_id": WorkspaceID(lone), "branch": "fix-2", "path": filepath.Join(t.TempDir(), "fix-2"),
	}))
	ws, _ = made["workspace"].(map[string]any)
	id2, _ := parseID("w_", text(ws["workspace_id"]))
	if got := h.srv.CompaniesOf(session.WorkspaceID(id2)); len(got) != 0 {
		t.Errorf("a worktree from no company is in %v", got)
	}
}
