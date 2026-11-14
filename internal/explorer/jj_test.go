package explorer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJJRenamesAreReadAsGitWritesThem: jj's summary writes a rename as
// dir/{old => new}; the panel shows the new path and diffs both.
func TestJJRenamesAreReadAsGitWritesThem(t *testing.T) {
	st := parseJJSummary([]byte("M keep.txt\nR src/{a.go => b.go}\nR {old.txt => new.txt}\nA d/n.txt\n"))
	got := map[string]string{}
	for _, c := range st.Changes {
		got[c.Path] = string(c.Index) + " " + c.Orig
	}
	want := map[string]string{"keep.txt": "M ", "src/b.go": "R src/a.go", "new.txt": "R old.txt", "d/n.txt": "A "}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s = %q, want %q (all: %v)", path, got[path], w, got)
		}
	}
	if st.DirLetter("d") != 'A' {
		t.Errorf("the directory of a new file shows %q", st.DirLetter("d"))
	}
}

// jjRepo makes a jj repository with one commit, as a person would, with the
// real jj; colocated with git or not.
func jjRepo(t *testing.T, colocate bool) string {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj is not installed")
	}
	t.Setenv("JJ_USER", "Test")
	t.Setenv("JJ_EMAIL", "test@example.com")
	t.Setenv("JJ_CONFIG", filepath.Join(t.TempDir(), "none.toml"))
	dir := filepath.Join(t.TempDir(), "repo")
	flag := "--no-colocate"
	if colocate {
		flag = "--colocate"
	}
	run(t, "", "jj", "git", "init", flag, dir)
	write(t, dir, "keep.txt", "a\n")
	write(t, dir, "old.txt", "x\n")
	run(t, dir, "jj", "commit", "-m", "base")
	return dir
}

func run(t *testing.T, dir string, argv ...string) string {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", argv, err, out)
	}
	return string(out)
}

// TestTheChangesViewAsksJJInAJJRepository, colocated and not: the working
// copy's changes listed as ready to commit, a diff of each, a file put back,
// and a commit — through jj, with no staging. If it regresses, a jj user's
// panel shows nothing (not colocated: there is no .git), or makes git
// commits jj did not mean (colocated).
func TestTheChangesViewAsksJJInAJJRepository(t *testing.T) {
	for _, colocate := range []bool{false, true} {
		name := "not colocated"
		if colocate {
			name = "colocated"
		}
		t.Run(name, func(t *testing.T) {
			dir := jjRepo(t, colocate)
			g := FindRepo(filepath.Join(dir))
			if !g.JJ || g.Top != dir {
				t.Fatalf("FindRepo = %+v, want the jj workspace at %s", g, dir)
			}
			write(t, dir, "keep.txt", "a\nb\n")
			write(t, dir, "added.txt", "n\n")
			if err := os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")); err != nil {
				t.Fatal(err)
			}

			st, err := g.Status()
			if err != nil {
				t.Fatal(err)
			}
			letters := map[string]byte{}
			for _, c := range st.Changes {
				letters[c.Path] = c.Index
				if !c.Staged() {
					t.Errorf("%s is not ready to commit: jj has no index", c.Path)
				}
			}
			if letters["keep.txt"] != 'M' || letters["added.txt"] != 'A' || letters["new.txt"] != 'R' {
				t.Fatalf("changes = %v", letters)
			}
			if !strings.Contains(st.Branch, "@") && st.Branch == "" {
				t.Errorf("no place for the header: %q", st.Branch)
			}

			c, _ := st.Of("keep.txt")
			diff, err := g.Diff(c, true)
			if err != nil || !strings.Contains(diff, "+b") {
				t.Fatalf("diff = %q, %v", diff, err)
			}
			if err := g.Stage("keep.txt"); err == nil || !strings.Contains(err.Error(), "no staging") {
				t.Fatalf("staging in jj said %v", err)
			}

			added, _ := st.Of("added.txt")
			if err := g.Discard(added); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "added.txt")); !os.IsNotExist(err) {
				t.Fatal("jj restore did not take the new file away")
			}

			if err := g.Commit("change things"); err != nil {
				t.Fatal(err)
			}
			if after, _ := g.Status(); len(after.Changes) != 0 {
				t.Fatalf("after the commit @ still has %v", after.Changes)
			}
			log := run(t, dir, "jj", "log", "--no-graph", "-r", "@-", "-T", "description")
			if strings.TrimSpace(log) != "change things" {
				t.Fatalf("@- is described %q", log)
			}
			if files, err := g.Files(); err != nil || len(files) != 2 {
				t.Fatalf("files = %v, %v", files, err)
			}
		})
	}
}

// TestAConflictInJJIsShownAsOne: a merge jj could not settle is a conflict
// in the working copy, as git's is, so the panel marks it.
func TestAConflictInJJIsShownAsOne(t *testing.T) {
	dir := jjRepo(t, false)
	id := func(rev string) string {
		return strings.TrimSpace(run(t, dir, "jj", "log", "--no-graph", "-r", rev, "-T", "change_id.shortest(8)"))
	}
	base := id("@-")
	write(t, dir, "keep.txt", "b\n")
	run(t, dir, "jj", "describe", "-m", "A")
	a := id("@")
	run(t, dir, "jj", "new", base, "-m", "B")
	write(t, dir, "keep.txt", "c\n")
	b := id("@")
	run(t, dir, "jj", "new", a, b)

	st, err := FindRepo(dir).Status()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := st.Of("keep.txt")
	if !ok || !c.Conflicted() {
		t.Fatalf("keep.txt = %+v (%v), want a conflict", c, ok)
	}
}
