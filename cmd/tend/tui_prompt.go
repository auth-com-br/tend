package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/worktree"
)

// Renaming needs somewhere to type, and a prompt on the status line is the
// smallest thing that works: it costs no space when it is not up, and it is
// where the name it is changing is already shown.

type promptKind uint8

const (
	promptNone promptKind = iota
	promptRenameTab
	promptRenameSpace
	// promptRenamePane names one pane, which its program cannot overwrite.
	promptRenamePane
	// promptNewWorktree asks for the branch of a new worktree, with a
	// generated one already in the box.
	promptNewWorktree
	// promptGroupSpace names a new group for a space. Leaving a group and
	// joining one that exists are picked from a menu, so an empty answer
	// cancels here as it does everywhere else.
	promptGroupSpace
	// promptRenameGroup renames a group, which means moving every space in it
	// at once: a group is only the set of spaces naming it.
	promptRenameGroup
	// promptGroupDir asks for the folder a group's new spaces start in,
	// with the one set, or the space in view's, already in the box.
	promptGroupDir
	// promptOpenURL asks for a page to open in the browser (tui_browser.go),
	// the last one opened already in the box.
	promptOpenURL
)

// startPrompt opens the prompt, seeded with the current name.
//
// The seed starts selected, as it would in any rename field: typing replaces
// it, and backspace keeps it and edits from the end. Seeding without that
// turns "rename this to backend" into "space 2backend", which is a name
// nobody asked for.
func (t *tui) startPrompt(kind promptKind) {
	t.mu.Lock()
	t.prompt = kind
	t.promptText = ""
	t.promptPristine = true

	switch kind {
	case promptRenameTab:
		for _, tab := range t.tabsLocked() {
			if tab.ID == t.tab {
				t.promptText = tab.Name
			}
		}
	case promptRenameSpace:
		if w, ok := t.workspaceLocked(); ok {
			t.promptText = w.Name
		}
	case promptOpenURL:
		if !t.urlLoaded {
			t.urlHistory, t.urlLoaded = loadURLHistory(), true
		}
		t.promptChoice = -1
		t.promptText = t.lastURL
		if t.promptText == "" && len(t.urlHistory) > 0 {
			t.promptText = t.urlHistory[0]
		}
		if t.promptText == "" {
			t.promptText = "https://"
			t.promptPristine = false // typed after, not over
		}
	case promptNewWorktree:
		t.promptText = worktree.GeneratedBranch(uint64(time.Now().UnixNano()))
		// Found once, for the path shown as the branch is typed (herdr's
		// create overlay shows where the checkout will go). Only here: over
		// ssh the repository is on the other machine.
		t.promptRepo = ""
		if w, ok := t.workspaceLocked(); ok && t.host == "" && w.Dir != "" {
			if repo, err := worktree.Find(w.Dir); err == nil {
				t.promptRepo = repo.Name
			}
		}
	case promptRenamePane:
		for _, p := range t.snap.Panes {
			if p.ID == t.focus {
				t.promptText = p.Title
			}
		}
	case promptRenameGroup:
		t.promptText = t.promptGroup
	case promptGroupDir:
		t.promptText = t.groupDirLocked(t.promptGroup)
		if t.promptText == "" {
			// Most often the group is for the project being looked at, so
			// its folder is the likeliest answer.
			if w, ok := t.workspaceLocked(); ok {
				t.promptText = w.Dir
			}
		}
		t.promptText = tildeHome(t.promptText)
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// startGroupRename opens the rename prompt on a group rather than on whatever
// space is being looked at.
func (t *tui) startGroupRename(group string) {
	t.mu.Lock()
	t.promptGroup = group
	t.mu.Unlock()
	t.startPrompt(promptRenameGroup)
}

func (t *tui) prompting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.prompt != promptNone
}

func (t *tui) cancelPrompt() {
	t.mu.Lock()
	t.prompt, t.promptText, t.promptPristine = promptNone, "", false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// promptKeys collects the name. It reports whether the input was consumed.
//
// It goes key by key, not byte by byte: an arrow is an escape sequence, and
// read a byte at a time its escape cancelled the prompt.
func (t *tui) promptKeys(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}

	for _, key := range splitKeys(data) {
		switch key {
		case "\r", "\n":
			return true, t.commitPrompt()
		case "\x1b", "\x03": // escape, ctrl+c
			t.cancelPrompt()
			return true, nil
		case "\x1b[A":
			t.stepPromptChoice(-1)
			continue
		case "\x1b[B":
			t.stepPromptChoice(1)
			continue
		case "\x7f", "\x08": // backspace
			t.mu.Lock()
			if t.promptPristine {
				// Backspace on the seed keeps it and starts editing, rather
				// than deleting a character of something about to be replaced.
				t.promptPristine = false
				t.dirty = true
				t.mu.Unlock()
				continue
			}
			if n := len(t.promptText); n > 0 {
				// Trimmed by rune, so deleting a character does not leave half
				// of one behind.
				runes := []rune(t.promptText)
				t.promptText = string(runes[:len(runes)-1])
			}
			t.dirty = true
			t.mu.Unlock()
			continue
		}
		if len(key) != 1 || key[0] < 0x20 {
			continue // other control keys and sequences have no meaning here
		}
		t.mu.Lock()
		if t.promptPristine {
			t.promptText = ""
			t.promptPristine = false
		}
		// A name is short; a page's address is not, and cut at 64 it opened
		// the wrong page.
		limit := 64
		if t.prompt == promptOpenURL {
			limit = 2048
		}
		if len(t.promptText) < limit {
			t.promptText += key
		}
		t.promptChoice = -1
		t.dirty = true
		t.mu.Unlock()
	}
	t.wakeUp()
	return true, nil
}

// stepPromptChoice moves the arrows through the answers given before, each
// put in the field as it is reached, to be opened with enter or typed over.
func (t *tui) stepPromptChoice(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prompt != promptOpenURL || len(t.urlHistory) == 0 {
		return
	}
	t.promptChoice = max(min(t.promptChoice+by, len(t.urlHistory)-1), 0)
	t.promptText = t.urlHistory[t.promptChoice]
	t.promptPristine = true
	t.dirty = true
}

// promptMouse is a press on the prompt's list: a page opened before opens
// again, at once. It reports whether it took the press.
func (t *tui) promptMouse(ev ui.MouseEvent) bool {
	if ev.Kind != ui.MousePress || ev.Button != 0 {
		return false
	}
	t.mu.Lock()
	if t.prompt != promptOpenURL {
		t.mu.Unlock()
		return false
	}
	frame := t.buildFrame()
	i, ok := ui.PromptChoiceAt(frame, t.cols, t.rows, ev.X, ev.Y)
	if !ok {
		t.mu.Unlock()
		return false
	}
	url := t.urlHistory[i]
	t.prompt, t.promptText, t.promptPristine = promptNone, "", false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.openInBrowser(url)
	return true
}

func (t *tui) commitPrompt() error {
	t.mu.Lock()
	kind, name := t.prompt, strings.TrimSpace(t.promptText)
	tab, ws, group, pane := t.tab, t.workspace, t.promptGroup, t.focus
	t.prompt, t.promptText, t.promptPristine = promptNone, "", false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()

	if name == "" {
		// An empty name would make the thing disappear from every list it is
		// in, so it is treated as cancelling rather than as a name.
		return nil
	}

	switch kind {
	case promptOpenURL:
		go t.openInBrowser(name)
		return nil
	case promptRenameTab:
		if tab == 0 {
			return nil
		}
		if err := t.client.RenameTab(tab, name); err != nil {
			return err
		}
	case promptRenameSpace:
		if ws == 0 {
			return nil
		}
		if err := t.client.RenameWorkspace(ws, name); err != nil {
			return err
		}
	case promptNewWorktree:
		if ws == 0 {
			return nil
		}
		t.createWorktree(ws, name)
	case promptRenamePane:
		if pane == 0 {
			return nil
		}
		if err := t.client.RenamePane(pane, name); err != nil {
			if t.reportStaleServer(err) {
				return nil
			}
			return err
		}
	case promptGroupSpace:
		if ws == 0 {
			return nil
		}
		if err := t.client.GroupWorkspace(ws, name); err != nil {
			return err
		}
	case promptRenameGroup:
		if group == "" {
			return nil
		}
		return t.renameGroup(group, name)
	case promptGroupDir:
		if group == "" {
			return nil
		}
		// The server expands ~ and checks the folder, on the machine the
		// panes run on; what it refuses is said on the status line.
		if err := t.client.SetGroupDir(group, name); err != nil {
			return err
		}
	default:
		return nil
	}
	return t.refresh()
}

// promptLabel is what the status line says while the prompt is up.
func (t *tui) promptLabelLocked() string {
	switch t.prompt {
	case promptRenameTab:
		return "rename tab"
	case promptRenameSpace:
		return "rename space"
	case promptRenamePane:
		return "rename pane"
	case promptNewWorktree:
		return "new worktree — branch"
	case promptGroupSpace:
		return "new group — name"
	case promptRenameGroup:
		return "rename group"
	case promptGroupDir:
		return "folder for " + t.promptGroup
	case promptOpenURL:
		return "open in browser"
	}
	return ""
}

// promptHintLocked says where the answer being typed leads: for a new
// worktree, the checkout's path, recomputed with every key as herdr's create
// overlay shows it. The caller holds the lock.
func (t *tui) promptHintLocked() string {
	if t.prompt != promptNewWorktree || t.promptRepo == "" {
		return ""
	}
	return "→ " + tildeHome(worktree.DefaultPath(t.worktreeRootLocked(), t.promptRepo, t.promptText))
}

// worktreeRootLocked is where new worktrees go, as the server works it out:
// the setting, or ~/.tend/worktrees. The caller holds the lock.
func (t *tui) worktreeRootLocked() string {
	root := worktree.ExpandHome(t.config.Worktrees.Directory)
	if root == "" || !filepath.IsAbs(root) {
		root = worktree.ExpandHome("~/.tend/worktrees")
	}
	return root
}

// tildeHome writes the home directory as ~, so a path fits on a status line.
func tildeHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}
