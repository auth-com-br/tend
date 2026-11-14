package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestAJJRepositoryGivesAnAgentAWorkspaceOfItsOwn is worktree.create,
// worktree.list and worktree.remove in a Jujutsu repository, with the real
// jj: a jj workspace where git would have a worktree, found again from
// inside it, and removed with its directory. Not colocated: jj 0.47 adds a
// git worktree beside a colocated workspace, which needs git 2.42, newer
// than some machines have. If it regresses, an agent asked into a jj
// repository is given a git worktree jj never snapshots, or none at all.
func TestAJJRepositoryGivesAnAgentAWorkspaceOfItsOwn(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}
	t.Setenv("JJ_USER", "Test")
	t.Setenv("JJ_EMAIL", "test@example.com")
	t.Setenv("JJ_CONFIG", filepath.Join(t.TempDir(), "none.toml"))
	base := t.TempDir()
	dir := filepath.Join(base, "proj")
	if out, err := exec.Command("jj", "git", "init", "--no-colocate", dir).CombinedOutput(); err != nil {
		t.Fatalf("jj git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("jj", "-R", dir, "commit", "-m", "one").CombinedOutput(); err != nil {
		t.Fatalf("jj commit: %v\n%s", err, out)
	}

	repo, err := Find(filepath.Join(dir))
	if err != nil || !repo.JJ || !Same(repo.Root, dir) || repo.Name != "proj" {
		t.Fatalf("Find = %+v, %v", repo, err)
	}
	path := filepath.Join(base, "proj-agent")
	if err := Add(repo, path, "worktree/agent-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(path, "a.txt")); err != nil {
		t.Fatalf("the workspace has no checkout: %v", err)
	}

	from, err := Find(path)
	if err != nil || !Same(from.Root, dir) || !Same(from.Checkout, path) {
		t.Fatalf("from inside the workspace, Find = %+v, %v: its repository is the main workspace's", from, err)
	}
	list, err := List(repo)
	if err != nil || len(list) != 2 || list[0].Linked || !list[1].Linked || list[1].Branch != "worktree/agent-1" || !Same(list[1].Path, path) {
		t.Fatalf("List = %+v, %v", list, err)
	}

	if err := Remove(repo, dir, false); err == nil {
		t.Fatal("the default workspace was removed")
	}
	if err := Remove(repo, path, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the workspace's directory is still there")
	}
	if list, _ := List(repo); len(list) != 1 {
		t.Fatalf("after removing it, List = %+v", list)
	}
}
