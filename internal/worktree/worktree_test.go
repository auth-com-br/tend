package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes a real repository with one commit, since everything here is git
// and a stand-in for git would only test the stand-in.
func repo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "first")
	return dir
}

// TestAWorktreeIsASecondCheckoutFoundFromEither is the round trip an agent's
// space depends on: made, listed, found again from inside itself, removed.
func TestAWorktreeIsASecondCheckoutFoundFromEither(t *testing.T) {
	root := repo(t)
	r, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if r.Name != "project" || !Same(r.Root, root) {
		t.Fatalf("repo = %+v", r)
	}

	path := DefaultPath(t.TempDir(), r.Name, "feature/Login Page")
	if filepath.Base(path) != "feature-login-page" {
		t.Errorf("the checkout is at %s; the branch should become a plain directory name", path)
	}
	if err := Add(r, path, "feature/login", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Found from inside the new checkout, it still leads back to the main one:
	// that is how a space opened there knows which repository it belongs to.
	inside, err := Find(path)
	if err != nil {
		t.Fatalf("Find from the worktree: %v", err)
	}
	if !Same(inside.Root, root) || !Same(inside.Checkout, path) {
		t.Errorf("from the worktree: %+v; want the main repository and this checkout", inside)
	}

	list, err := List(r)
	if err != nil {
		t.Fatal(err)
	}
	var linked *Worktree
	for i := range list {
		if list[i].Linked {
			linked = &list[i]
		}
	}
	if len(list) != 2 || linked == nil || linked.Branch != "feature/login" {
		t.Fatalf("list = %+v", list)
	}

	// A change in it makes git refuse, and the refusal is recognisable.
	if err := os.WriteFile(filepath.Join(path, "draft"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(r, path, false); !errors.Is(err, ErrDirty) {
		t.Errorf("removing a dirty worktree: err = %v, want ErrDirty", err)
	}
	if err := Remove(r, path, true); err != nil {
		t.Fatalf("forced Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the worktree's checkout is still there after removing it")
	}
}

// TestAnExistingBranchIsCheckedOutNotRecreated: asking for a worktree on a
// branch that exists must not fail with "branch already exists" — reopening
// work is as common as starting it.
func TestAnExistingBranchIsCheckedOutNotRecreated(t *testing.T) {
	root := repo(t)
	r, _ := Find(root)
	if _, err := git(root, "branch", "old-work"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "old-work")
	if err := Add(r, path, "old-work", ""); err != nil {
		t.Fatalf("Add on an existing branch: %v", err)
	}
}

// TestOutsideARepositoryIsSaidSo: a worktree asked for from a plain directory
// must say why nothing happened.
func TestOutsideARepositoryIsSaidSo(t *testing.T) {
	if _, err := Find(t.TempDir()); !errors.Is(err, ErrNotARepository) {
		t.Errorf("err = %v, want ErrNotARepository", err)
	}
}

func TestPorcelainIsReadBlockByBlock(t *testing.T) {
	list := ParsePorcelain("worktree /a\nHEAD 1\nbranch refs/heads/main\n\nworktree /b\nHEAD 2\ndetached\n\nworktree /c\nbare\n\nworktree /d\nprunable gitdir file points to non-existent location\n")
	if len(list) != 4 || list[0].Branch != "main" || !list[1].Detached || !list[2].Bare || !list[3].Prunable {
		t.Errorf("parsed %+v", list)
	}
}

// TestGeneratedBranchesAreHerdrsShape: 0x1234 is 4660, adjective 4660%8 = 4
// ("lucky") and noun (4660/8)%8 = 6 ("stone"), by herdr's own formula.
func TestGeneratedBranchesAreHerdrsShape(t *testing.T) {
	if got := GeneratedBranch(0x1234); got != "worktree/lucky-stone-1234" {
		t.Errorf("GeneratedBranch = %q", got)
	}
}

// TestTrustingARepositoryIsForOneCallOnly is herdr's trust_repository: git is
// told the repository is safe with -c on the command, and nothing is written
// to the user's git configuration. If it regresses, trusting one repository
// once would change git's behaviour for good.
func TestTrustingARepositoryIsForOneCallOnly(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	repo, err := FindTrusted(dir, true)
	if err != nil || !repo.Trusted {
		t.Fatalf("FindTrusted = %+v, %v", repo, err)
	}
	if _, err := List(repo); err != nil {
		t.Fatalf("List on a trusted repository: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(home, ".gitconfig")); err == nil && strings.Contains(string(data), "safe") {
		t.Errorf("trusting wrote to the user's git configuration:\n%s", data)
	}
}

// TestWithinIsTheDirectoryOrBelowIt: a directory is within itself and its
// parents, not within a sibling that shares its name as a prefix, and a
// symlink to it is the same place. If it regresses, a worktree opened from
// "project-old" is filed in the companies of the space rooted in "project".
func TestWithinIsTheDirectoryOrBelowIt(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	sub := filepath.Join(project, "sub")
	sibling := filepath.Join(root, "project-old")
	for _, d := range []string{sub, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path, dir string
		want      bool
	}{
		{project, project, true},
		{sub, project, true},
		{filepath.Join(link, "sub"), project, true},
		{project, sub, false},
		{sibling, project, false},
		{root, project, false},
	} {
		if got := Within(c.path, c.dir); got != c.want {
			t.Errorf("Within(%s, %s) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}
