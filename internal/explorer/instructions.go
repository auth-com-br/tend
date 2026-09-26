package explorer

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

// Agents read their instructions from files at three levels (docs/MEMORY.md,
// #29): the user's own, every project's (Claude Code's ~/.claude/CLAUDE.md),
// the project's, and a folder's, read when the agent works in it. Claude
// Code reads CLAUDE.md; Codex, OpenCode and most others AGENTS.md. One file
// serves them all when CLAUDE.md holds only "@AGENTS.md", Claude Code's way
// of reading another file in — this repository does it. The menu opens each
// level's file, and offers to share one between the agents.

// instructionFile is where the instructions for dir are: AGENTS.md when it
// is there, else CLAUDE.md when that is, else AGENTS.md, to be made.
func instructionFile(dir string) (string, bool) {
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, true
		}
	}
	return filepath.Join(dir, "AGENTS.md"), false
}

// globalInstructions is Claude Code's instructions for every project.
func globalInstructions() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "CLAUDE.md"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "CLAUDE.md"), nil
}

// linkLine is what a CLAUDE.md that reads AGENTS.md holds.
const linkLine = "@AGENTS.md"

// shareStep is what sharing a folder's instructions between agents would
// do there.
type shareStep int

const (
	// shareDone: already shared, or nothing to share yet.
	shareDone shareStep = iota
	// shareMakeLink: AGENTS.md alone — make CLAUDE.md read it.
	shareMakeLink
	// shareMoveThenLink: CLAUDE.md alone — it becomes AGENTS.md, and a
	// CLAUDE.md reads it.
	shareMoveThenLink
	// shareBoth: two files that differ, which only the user can merge.
	shareBoth
)

// shareStepIn is what sharing would do in dir.
func shareStepIn(dir string) shareStep {
	agents, errA := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	claude, errC := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	hasA, hasC := errA == nil, errC == nil
	switch {
	case hasC && bytes.Contains(claude, []byte(linkLine)):
		return shareDone
	case hasA && !hasC:
		return shareMakeLink
	case hasC && !hasA:
		return shareMoveThenLink
	case hasA && hasC:
		if bytes.Equal(bytes.TrimSpace(agents), bytes.TrimSpace(claude)) {
			return shareMakeLink // the same text twice: CLAUDE.md can just read it
		}
		return shareBoth
	}
	return shareDone
}

// share does what shareStepIn says, and says what it did.
func share(dir string) (string, error) {
	claude, agents := filepath.Join(dir, "CLAUDE.md"), filepath.Join(dir, "AGENTS.md")
	switch shareStepIn(dir) {
	case shareMakeLink:
		if err := os.WriteFile(claude, []byte(linkLine+"\n"), 0o644); err != nil {
			return "", err
		}
		return "CLAUDE.md now reads AGENTS.md: one file for every agent", nil
	case shareMoveThenLink:
		if err := os.Rename(claude, agents); err != nil {
			return "", err
		}
		if err := os.WriteFile(claude, []byte(linkLine+"\n"), 0o644); err != nil {
			return "", err
		}
		return "CLAUDE.md is AGENTS.md now, and CLAUDE.md reads it: one file for every agent", nil
	case shareBoth:
		return "", errors.New("AGENTS.md and CLAUDE.md say different things: merge them into AGENTS.md, then share")
	}
	return "already one file for every agent", nil
}

// openInstructions opens an instructions file in the editor, making it —
// and its folder, for the global one — when it is not there yet.
func (m *Model) openInstructions(path string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			m.fail(err)
			return
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			m.fail(err)
			return
		}
		m.Refresh()
	}
	m.open(path, 0)
}

// instructionItems are the menu's entries for instructions: the global
// file, the project's, the folder's when it is not the project, and
// sharing the project's between agents when there is something to do.
func (m *Model) instructionItems(target *Node) []menuItem {
	items := []menuItem{{actInstrGlobal, "Agent instructions: global"}, {actInstrProject, "Agent instructions: project"}}
	if target != nil {
		if dir := m.dirFor(target); dir != m.tree.Root {
			items = append(items, menuItem{actInstrHere, "Agent instructions: this folder"})
		}
	}
	if step := shareStepIn(m.tree.Root); step == shareMakeLink || step == shareMoveThenLink || step == shareBoth {
		items = append(items, menuItem{actInstrShare, "Share instructions with every agent"})
	}
	return items
}
