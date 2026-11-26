package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheTreesMenuDoesWhatTheTUIsDoes: a right click on the files tab's
// tree offers the TUI's tree menu, and its items do what the TUI's do — a
// new file in the folder clicked, revealed in the tree; a rename; a delete
// only when "yes" is typed; the path copied. If it regresses, the window's
// tree is one to look at but not to work in.
func TestTheTreesMenuDoesWhatTheTUIsDoes(t *testing.T) {
	project := shopRepo(t)
	if err := os.MkdirAll(filepath.Join(project, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "web", "app.js"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	inProject(t, s, project)
	var copied string
	s.Copied = func(text string) { copied = text }
	s.ToggleDock("files")
	entry := func(rel string) (EntryRow, bool) {
		d := s.State().Dock
		if d == nil || d.Files == nil {
			return EntryRow{}, false
		}
		for _, e := range d.Files.Entries {
			if e.Rel == rel {
				return e, true
			}
		}
		return EntryRow{}, false
	}
	waitFor(t, "the tree", func() bool { _, ok := entry("web"); return ok })
	web, _ := entry("web")

	pick := func(label string) {
		t.Helper()
		m := s.State().Menu
		if m == nil {
			t.Fatalf("no menu for %q", label)
		}
		for i, it := range m.Items {
			if it == label {
				if err := s.PickMenu(i); err != nil {
					t.Fatalf("%s: %v", label, err)
				}
				return
			}
		}
		t.Fatalf("the menu has no %q: %v", label, m.Items)
	}

	// On a folder: no "Open with default app", which is for files.
	s.OpenFileMenu(web.Path, web.Rel, true, 0, 0)
	for _, it := range s.State().Menu.Items {
		if it == "Open with default app" {
			t.Error("a folder's menu offers to open it with an app")
		}
	}
	pick("New file…")
	if p := s.State().Prompt; p == nil || !strings.Contains(p.Title, "web") {
		t.Fatalf("the prompt: %+v", p)
	}
	if err := s.SubmitPrompt("../escape"); err == nil {
		t.Error("a path was taken as a name")
	}
	s.OpenFileMenu(web.Path, web.Rel, true, 0, 0)
	pick("New file…")
	if err := s.SubmitPrompt("index.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "web", "index.html")); err != nil {
		t.Fatalf("the file was not made: %v", err)
	}
	waitFor(t, "the new file, revealed", func() bool { _, ok := entry("web/index.html"); return ok })

	made, _ := entry("web/index.html")
	s.OpenFileMenu(made.Path, made.Rel, false, 0, 0)
	pick("Copy relative path")
	if copied != "web/index.html" {
		t.Errorf("copied %q", copied)
	}

	s.OpenFileMenu(made.Path, made.Rel, false, 0, 0)
	pick("Rename…")
	if p := s.State().Prompt; p == nil || p.Text != "index.html" {
		t.Fatalf("the rename prompt: %+v", p)
	}
	if err := s.SubmitPrompt("home.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "web", "home.html")); err != nil {
		t.Fatalf("not renamed: %v", err)
	}

	renamed := filepath.Join(project, "web", "home.html")
	s.OpenFileMenu(renamed, "web/home.html", false, 0, 0)
	pick("Delete")
	if err := s.SubmitPrompt("no"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(renamed); err != nil {
		t.Fatal("deleted without a yes")
	}
	s.OpenFileMenu(renamed, "web/home.html", false, 0, 0)
	pick("Delete")
	if err := s.SubmitPrompt("yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Errorf("not deleted: %v", err)
	}
}
