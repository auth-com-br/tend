//go:build unix

package explorer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentWorktree is a repository on main and a worktree beside it on the
// branch agent/fix, with one commit there.
func agentWorktree(t *testing.T) (main, wt string) {
	t.Helper()
	withIdentity(t)
	main = repo(t)
	gitIn(t, main, "add", "-A")
	gitIn(t, main, "commit", "-q", "-m", "second")
	wt = filepath.Join(t.TempDir(), "fix")
	gitIn(t, main, "worktree", "add", "-q", "-b", "agent/fix", wt)
	write(t, wt, "fix.txt", "fixed\n")
	gitIn(t, wt, "add", "-A")
	gitIn(t, wt, "commit", "-q", "-m", "the fix")
	return main, wt
}

// TestMMergesAWorktreesBranchIntoTheMainOne: in an agent's worktree, M twice
// merges its branch into the branch the main worktree is on, with a merge
// commit; once only asks. If it regresses, finishing an agent's work means
// leaving the panel for a shell in another directory.
func TestMMergesAWorktreesBranchIntoTheMainOne(t *testing.T) {
	main, wt := agentWorktree(t)
	m := New(wt, nil)
	keys(m, "3", "M")
	m.runJobNow()
	if !strings.Contains(m.message, "M again to merge agent/fix into main") {
		t.Fatalf("one M said %q", m.message)
	}
	if _, err := os.Stat(filepath.Join(main, "fix.txt")); err == nil {
		t.Fatal("one M merged")
	}
	keys(m, "M")
	m.runJobNow()
	if !strings.Contains(m.message, "merged agent/fix into main") {
		t.Fatalf("M M said %q (err %v)", m.message, m.messageErr)
	}
	if log := gitIn(t, main, "log", "-1", "--format=%s %p"); !strings.HasPrefix(log, "Merge branch 'agent/fix'") || len(strings.Fields(log)) < 2 {
		t.Errorf("main's last commit: %s", log)
	}
}

// TestAMergeThatConflictsChangesNothing: a conflict is undone, and the main
// worktree is left as it was; uncommitted work in either worktree refuses
// the merge before it starts; the main worktree refuses it for itself. If it
// regresses, a button leaves the user's main checkout half-merged.
func TestAMergeThatConflictsChangesNothing(t *testing.T) {
	main, wt := agentWorktree(t)
	write(t, main, "fix.txt", "other\n")
	gitIn(t, main, "add", "-A")
	gitIn(t, main, "commit", "-q", "-m", "clash")
	head := gitIn(t, main, "rev-parse", "HEAD")

	m := New(wt, nil)
	keys(m, "3", "M", "M")
	m.runJobNow()
	if !m.messageErr || !strings.Contains(m.message, "conflict merging into main") {
		t.Fatalf("a conflict said %q", m.message)
	}
	if gitIn(t, main, "rev-parse", "HEAD") != head || strings.TrimSpace(gitIn(t, main, "status", "--porcelain", "--untracked-files=no")) != "" {
		t.Error("the main worktree was left changed")
	}

	write(t, wt, "kept.txt", "edited\n")
	m.Refresh()
	keys(m, "M", "M")
	m.runJobNow()
	if !strings.Contains(m.message, "uncommitted changes here") {
		t.Errorf("uncommitted work here: %q", m.message)
	}

	top := New(main, nil)
	keys(top, "3", "M")
	if !strings.Contains(top.message, "main worktree") {
		t.Errorf("in the main worktree: %q", top.message)
	}
}

// TestRPushesAndOpensAPullRequest: R twice publishes the branch and runs
// gh pr create, filled from the commits, and says the address gh printed —
// or the one already open. gh here is a stand-in that records its
// arguments; it proves what is asked of gh, not what GitHub answers. If it
// regresses, an agent's branch cannot be put up for review from the panel.
func TestRPushesAndOpensAPullRequest(t *testing.T) {
	work, _ := remoteAndClone(t)
	gitIn(t, work, "switch", "-q", "-c", "agent/pr")
	write(t, work, "p.txt", "p\n")
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "for review")

	bin := t.TempDir()
	args := filepath.Join(bin, "args")
	gh := "#!/bin/sh\necho \"$@\" > " + args + "\n" +
		"if [ -n \"$GH_EXISTS\" ]; then echo 'a pull request for branch \"agent/pr\" into branch \"main\" already exists:' >&2; echo https://example.com/pr/1 >&2; exit 1; fi\n" +
		"echo https://example.com/pr/2\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := New(work, nil)
	keys(m, "3", "R")
	if !strings.Contains(m.message, "R again") {
		t.Fatalf("one R said %q", m.message)
	}
	keys(m, "R")
	m.runJobNow()
	if m.message != "pull request: https://example.com/pr/2" {
		t.Fatalf("R R said %q (err %v)", m.message, m.messageErr)
	}
	if up := gitIn(t, work, "rev-parse", "--abbrev-ref", "@{upstream}"); strings.TrimSpace(up) != "origin/agent/pr" {
		t.Errorf("the branch was not published first: %q", up)
	}
	if got, _ := os.ReadFile(args); strings.TrimSpace(string(got)) != "pr create --fill --head agent/pr" {
		t.Errorf("gh was run with %q", got)
	}

	t.Setenv("GH_EXISTS", "1")
	keys(m, "R", "R")
	m.runJobNow()
	if m.message != "already open: https://example.com/pr/1" || m.messageErr {
		t.Errorf("an open one: %q", m.message)
	}
}
