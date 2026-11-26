package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
)

// TestTheDockFollowsTheSpaceShown: the dock stays open as the person goes
// from space to space, and its tabs show the project of the space shown —
// the files of its folder, its issues — not the first space's. If it
// regresses, the files tab shows one project whatever space is open, and
// the issues are looked for on another project's forge, as the owner
// found.
func TestTheDockFollowsTheSpaceShown(t *testing.T) {
	repo := func(name, file string) string {
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	shop, blog := repo("shop", "cart.go"), repo("blog", "post.md")

	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	space := func(name, dir string) uint64 {
		ws, err := s.client.NewWorkspaceIn(name, dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.client.NewTab(ws, name, proto.PaneSpec{Dir: dir}); err != nil {
			t.Fatal(err)
		}
		if err := s.refresh(); err != nil {
			t.Fatal(err)
		}
		return ws
	}
	a, b := space("shop", shop), space("blog", blog)
	listed := func(name string) bool {
		d := s.State().Dock
		if d == nil || d.Files == nil {
			return false
		}
		for _, e := range d.Files.Entries {
			if e.Name == name {
				return true
			}
		}
		return false
	}

	if err := s.ShowWorkspace(a); err != nil {
		t.Fatal(err)
	}
	s.ToggleDock("files")
	waitFor(t, "shop's files", func() bool { return listed("cart.go") })

	if err := s.ShowWorkspace(b); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "blog's files, the dock still open", func() bool { return listed("post.md") && !listed("cart.go") })

	// The issues tab lets go of the pane it was read from, and reads the
	// one in focus in the space shown.
	s.mu.Lock()
	s.dock.issues.pane = 999
	s.dock.space = a
	s.mu.Unlock()
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	pane := s.dock.issues.pane
	s.mu.Unlock()
	if pane == 999 {
		t.Error("the issues tab kept the pane of the space it was read in")
	}
}
