package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestAGroupFolderMustBeAFolderOnTheServer: a folder is expanded from ~,
// made absolute, and refused when it does not exist or is a file. If it
// regresses, a typo in a group's folder starts every new space somewhere
// else without a word.
func TestAGroupFolderMustBeAFolderOnTheServer(t *testing.T) {
	dir := t.TempDir()
	if got, err := groupDir(" " + dir + "/ "); err != nil || got != dir {
		t.Errorf("an existing folder gives %q %v", got, err)
	}
	home, _ := os.UserHomeDir()
	if got, err := groupDir("~"); err != nil || got != filepath.Clean(home) {
		t.Errorf("~ gives %q %v, want %q", got, err, home)
	}
	if _, err := groupDir(filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "no such folder") {
		t.Errorf("a missing folder: %v", err)
	}
	file := filepath.Join(dir, "f")
	_ = os.WriteFile(file, nil, 0o600)
	if _, err := groupDir(file); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("a file: %v", err)
	}
}

// TestAFolderListsItsFoldersOnly: dir.list gives a folder's subfolders by
// name, sorted without regard to case, a link to a folder among them, and no
// files; the parent is the folder above and there is none at the root; a
// folder that is not there is said so. If it regresses, the folder picker
// offers files to open as folders, or cannot climb out of where it started.
func TestAFolderListsItsFoldersOnly(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"beta", "Alpha", ".hidden"} {
		_ = os.Mkdir(filepath.Join(dir, d), 0o755)
	}
	_ = os.WriteFile(filepath.Join(dir, "file"), nil, 0o600)
	_ = os.Symlink(filepath.Join(dir, "beta"), filepath.Join(dir, "gamma"))

	got, err := listDirs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".hidden", "Alpha", "beta", "gamma"}; !slices.Equal(got.Dirs, want) {
		t.Errorf("dirs = %v, want %v", got.Dirs, want)
	}
	if got.Path != dir || got.Parent != filepath.Dir(dir) {
		t.Errorf("path %q parent %q", got.Path, got.Parent)
	}
	if root, _ := listDirs("/"); root.Parent != "" {
		t.Errorf("the root has parent %q", root.Parent)
	}
	if _, err := listDirs(filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "no such folder") {
		t.Errorf("a missing folder: %v", err)
	}
}
