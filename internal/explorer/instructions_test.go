package explorer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSharingInstructionsMakesOneFileForEveryAgent: AGENTS.md alone gets a
// CLAUDE.md that reads it; CLAUDE.md alone becomes AGENTS.md, read by a new
// CLAUDE.md; the same text in both is shared too; two that differ are left
// for the user to merge; and a CLAUDE.md that already reads AGENTS.md is
// done. If it regresses, sharing overwrites instructions the user wrote,
// or leaves Codex reading nothing.
func TestSharingInstructionsMakesOneFileForEveryAgent(t *testing.T) {
	write := func(dir, name, text string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(dir, name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}

	onlyAgents := t.TempDir()
	write(onlyAgents, "AGENTS.md", "# rules\n")
	if shareStepIn(onlyAgents) != shareMakeLink {
		t.Fatal("AGENTS.md alone is not offered")
	}
	if _, err := share(onlyAgents); err != nil || read(onlyAgents, "CLAUDE.md") != "@AGENTS.md\n" || read(onlyAgents, "AGENTS.md") != "# rules\n" {
		t.Errorf("AGENTS.md alone: %v %q", err, read(onlyAgents, "CLAUDE.md"))
	}
	if shareStepIn(onlyAgents) != shareDone {
		t.Error("shared twice")
	}

	onlyClaude := t.TempDir()
	write(onlyClaude, "CLAUDE.md", "# mine\n")
	if _, err := share(onlyClaude); err != nil || read(onlyClaude, "AGENTS.md") != "# mine\n" || read(onlyClaude, "CLAUDE.md") != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md alone: %v agents=%q claude=%q", err, read(onlyClaude, "AGENTS.md"), read(onlyClaude, "CLAUDE.md"))
	}

	same := t.TempDir()
	write(same, "AGENTS.md", "# x\n")
	write(same, "CLAUDE.md", "# x\n\n")
	if shareStepIn(same) != shareMakeLink {
		t.Error("the same text twice is not offered")
	}

	differ := t.TempDir()
	write(differ, "AGENTS.md", "# a\n")
	write(differ, "CLAUDE.md", "# c\n")
	if _, err := share(differ); err == nil || !strings.Contains(err.Error(), "merge") || read(differ, "CLAUDE.md") != "# c\n" {
		t.Errorf("two that differ: %v %q", err, read(differ, "CLAUDE.md"))
	}

	if shareStepIn(t.TempDir()) != shareDone {
		t.Error("an empty folder offered sharing")
	}
}

// TestTheMenuOpensEachLevelsInstructions: the menu offers the global, the
// project's and a folder's instructions and opens each in the editor —
// AGENTS.md before CLAUDE.md, and one that is not there yet is made first
// — and offers sharing only while there is something to share. If it
// regresses, the instructions an agent reads are one more file to find by
// hand.
func TestTheMenuOpensEachLevelsInstructions(t *testing.T) {
	dir := repo(t)
	global := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(global, "claude"))
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opener := &recordingOpener{}
	m := New(dir, opener)
	selectNode(t, m, "src")
	m.openMenu(m.selectedNode())
	got := menuLabels(m.menu.items)
	for _, want := range []string{"Agent instructions: global", "Agent instructions: project", "Agent instructions: this folder", "Share instructions with every agent"} {
		if !strings.Contains(got, want) {
			t.Fatalf("menu lacks %q: %s", want, got)
		}
	}
	chooseMenu(t, m, "Agent instructions: global")
	m.openMenu(m.selectedNode())
	chooseMenu(t, m, "Agent instructions: project")
	m.openMenu(m.selectedNode())
	chooseMenu(t, m, "Agent instructions: this folder")
	want := []string{
		filepath.Join(global, "claude", "CLAUDE.md"),
		filepath.Join(dir, "CLAUDE.md"),
		filepath.Join(dir, "src", "AGENTS.md"),
	}
	if strings.Join(opener.opened, " ") != strings.Join(want, " ") {
		t.Errorf("opened %v, want %v", opener.opened, want)
	}
	for _, path := range []string{want[0], want[2]} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s was not made", path)
		}
	}

	m.openMenu(m.selectedNode())
	chooseMenu(t, m, "Share instructions with every agent")
	if b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md")); string(b) != "# project\n" {
		t.Errorf("AGENTS.md after sharing: %q", b)
	}
	m.openMenu(m.selectedNode())
	if strings.Contains(menuLabels(m.menu.items), "Share instructions") {
		t.Error("sharing offered once shared")
	}
}
