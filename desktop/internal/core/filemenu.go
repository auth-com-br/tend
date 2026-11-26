package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/explorer"
	"github.com/auth-com-br/tend/internal/ui"
)

// A right click on the files tab's tree opens the TUI's tree menu
// (internal/explorer/menu.go, herdr-sidebar's actions.rs): make a file or a
// folder, open it with the desktop's app, stage it, its history, copy its
// path, rename, delete, put it in the context, reveal it in the file
// manager, change the folder shown. The same items in the same order, done
// the same way; drawn as the window's other menus are.

// The file menu's actions.
const (
	fileNew          = "file-new"
	fileNewFolder    = "file-new-folder"
	fileOpenExternal = "file-open-external"
	fileStage        = "file-stage"
	fileHistory      = "file-history"
	fileCopyPath     = "file-copy-path"
	fileCopyRelative = "file-copy-relative"
	fileRename       = "file-rename"
	fileDelete       = "file-delete"
	fileAddContext   = "file-add-context"
	fileReveal       = "file-reveal"
	fileChangeFolder = "file-change-folder"
)

// fileTarget is what the menu was opened on: an entry, or the folder shown
// itself (Path empty) when the click was below the entries.
type fileTarget struct {
	Path string
	Rel  string
	Dir  bool
}

func (t fileTarget) name() string { return filepath.Base(t.Path) }

// OpenFileMenu opens the tree's menu on an entry (path, rel to the folder
// shown), or on the folder shown when path is "".
func (s *Session) OpenFileMenu(path, rel string, dir bool, x, y int) {
	s.mu.Lock()
	p := s.files.panel
	s.mu.Unlock()
	if p == nil {
		return
	}
	t := fileTarget{Path: path, Rel: rel, Dir: dir}
	_, _, inRepo := repoOf(p, path)
	if path == "" {
		_, _, inRepo = repoOf(p, p.Root())
	}

	m := ui.Menu{X: x, Y: y, Title: p.Root()}
	if path != "" {
		m.Title = t.Rel
	}
	add := func(action, label string) { m.Items = append(m.Items, ui.MenuItem{Label: label, Action: action}) }
	add(fileNew, "New file…")
	add(fileNewFolder, "New folder…")
	if path != "" && !dir {
		add(fileOpenExternal, "Open with default app")
	}
	if path != "" && inRepo {
		add(fileStage, "Stage changes")
		if !dir {
			add(fileHistory, "File history")
		}
	}
	if path != "" {
		add(fileCopyPath, "Copy path")
		add(fileCopyRelative, "Copy relative path")
		add(fileRename, "Rename…")
		add(fileDelete, "Delete")
		add(fileAddContext, "Add to context")
	}
	add(fileReveal, "Reveal in file manager")
	add(fileChangeFolder, "Change folder…")

	s.mu.Lock()
	s.files.target = t
	s.menu = &m
	s.mu.Unlock()
	s.stateChanged()
}

// repoOf is the repository a path is in, among the panel's, and the path
// from its top.
func repoOf(p *explorer.Panel, path string) (top, rel string, ok bool) {
	best := ""
	for _, r := range p.Repos() {
		if r.Top != "" && (path == r.Top || strings.HasPrefix(path, r.Top+string(filepath.Separator))) && len(r.Top) > len(best) {
			best = r.Top
		}
	}
	if best == "" {
		return "", "", false
	}
	rel, err := filepath.Rel(best, path)
	if err != nil {
		return "", "", false
	}
	return best, filepath.ToSlash(rel), true
}

// dirFor is the folder something made from the menu goes in: the folder
// clicked, the one the file is in, or the folder shown.
func (t fileTarget) dirFor(root string) string {
	switch {
	case t.Path == "":
		return root
	case t.Dir:
		return t.Path
	}
	return filepath.Dir(t.Path)
}

// runFileMenu does a file menu item.
func (s *Session) runFileMenu(action string) error {
	s.mu.Lock()
	p, t := s.files.panel, s.files.target
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	root := p.Root()
	relOrName := func(dir string) string {
		if rel, err := filepath.Rel(root, dir); err == nil && rel != "." {
			return filepath.ToSlash(rel)
		}
		return filepath.Base(root)
	}
	switch action {
	case fileNew:
		s.setPrompt(&Prompt{Kind: fileNew, Title: "New file in " + relOrName(t.dirFor(root))})
	case fileNewFolder:
		s.setPrompt(&Prompt{Kind: fileNewFolder, Title: "New folder in " + relOrName(t.dirFor(root))})
	case fileRename:
		s.setPrompt(&Prompt{Kind: fileRename, Title: "Rename " + t.name() + " to", Text: t.name()})
	case fileDelete:
		what := t.name()
		if t.Dir {
			what += " and everything in it"
		}
		// Typed, not clicked twice: a folder takes everything under it,
		// and nothing outside git's keeping comes back (the TUI's ask).
		s.setPrompt(&Prompt{Kind: fileDelete, Title: "Delete " + what + "? Type yes"})
	case fileChangeFolder:
		s.setPrompt(&Prompt{Kind: fileChangeFolder, Title: "Show folder", Text: tildeHome(root)})
	case fileOpenExternal:
		return explorer.OpenWithSystem(t.Path)
	case fileReveal:
		return explorer.OpenWithSystem(t.dirFor(root))
	case fileStage:
		top, rel, ok := repoOf(p, t.Path)
		if !ok {
			return errors.New(t.Rel + " is not in a repository")
		}
		return s.Files(FilesAction{Do: "stage", Repo: top, Path: rel})
	case fileHistory:
		top, rel, ok := repoOf(p, t.Path)
		if !ok {
			return errors.New(t.Rel + " is not in a repository")
		}
		return s.Files(FilesAction{Do: "history", Kind: "commits", Repo: top, Path: rel})
	case fileCopyPath, fileCopyRelative:
		text := t.Path
		if action == fileCopyRelative {
			text = t.Rel
		}
		if s.Copied != nil {
			s.Copied(text)
		}
		s.say("copied " + text)
	case fileAddContext:
		if _, err := s.apiCall("context.add", map[string]any{"kind": "file", "path": t.Path, "source": "files"}); err != nil {
			return fmt.Errorf("context: %w", err)
		}
		s.say("added " + t.Rel + " to the context")
	}
	return nil
}

// fileAnswer is the line typed for a file menu prompt.
func (s *Session) fileAnswer(kind, text string) error {
	s.mu.Lock()
	p, t := s.files.panel, s.files.target
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	root := p.Root()
	switch kind {
	case fileNew, fileNewFolder:
		name, ok := explorer.ValidName(text)
		if !ok {
			return errors.New("a name, not a path")
		}
		path := filepath.Join(t.dirFor(root), name)
		if _, err := os.Lstat(path); err == nil {
			return errors.New(name + " already exists")
		}
		var err error
		if kind == fileNewFolder {
			err = os.Mkdir(path, 0o755)
		} else {
			err = os.WriteFile(path, nil, 0o644)
		}
		if err != nil {
			return err
		}
		s.revealFile(p, path)
		s.say("made " + name)
	case fileRename:
		name, ok := explorer.ValidName(text)
		if !ok {
			return errors.New("a name, not a path")
		}
		if name == t.name() {
			return nil
		}
		path := filepath.Join(filepath.Dir(t.Path), name)
		if _, err := os.Lstat(path); err == nil {
			return errors.New(name + " already exists")
		}
		if err := os.Rename(t.Path, path); err != nil {
			return err
		}
		s.revealFile(p, path)
		s.say("renamed to " + name)
	case fileDelete:
		if strings.ToLower(text) != "yes" {
			s.say("not deleted")
			return nil
		}
		var err error
		if t.Dir {
			err = os.RemoveAll(t.Path)
		} else {
			err = os.Remove(t.Path)
		}
		if err != nil {
			return err
		}
		p.Refresh()
		s.say("deleted " + t.name())
		s.stateChanged()
	case fileChangeFolder:
		dir := expandHome(text)
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return errors.New(dir + " is not a folder")
		}
		if err := p.ChangeFolder(dir); err != nil {
			return err
		}
		s.mu.Lock()
		s.files.root = p.Root()
		s.mu.Unlock()
		s.stateChanged()
	}
	return nil
}

// revealFile shows a file just made or renamed: the tree read again, and
// every folder above it opened.
func (s *Session) revealFile(p *explorer.Panel, path string) {
	p.Refresh()
	if rel, err := filepath.Rel(p.Root(), path); err == nil {
		_ = p.Reveal(filepath.ToSlash(rel))
	}
	s.stateChanged()
}

// say puts a line in the window's status line.
func (s *Session) say(text string) {
	if s.Status != nil {
		s.Status(text)
	}
}
