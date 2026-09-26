package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheAgentManagerListsFoundThenInstallable: the installed agents under
// INSTALLED with their version, the others under AVAILABLE with a way to
// install or what they need; asking to install shows the very command
// before it runs; a click finds the agent on its line. If it regresses, an
// install runs a command nobody was shown, or a click installs the agent on
// the line beside it.
func TestTheAgentManagerListsFoundThenInstallable(t *testing.T) {
	v := &AgentManagerView{Agents: []AgentEntry{
		{Name: "Claude Code", Installed: true, Version: "2.1.281 (Claude Code)"},
		{Name: "Gemini CLI", InstallCommand: "npm install -g @google/gemini-cli"},
		{Name: "Letta Code", Missing: "npm"},
	}, Cursor: 1}
	g := vt.NewGrid(100, 30, 0)
	Draw(g, Frame{AgentManager: v}, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, want := range []string{"AGENT MANAGER", "INSTALLED", "● Claude Code", "2.1.281", "AVAILABLE", "○ Gemini CLI", "[ install ]", "needs npm", "[ Refresh ]", "[ Close ]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "Claude Code") > strings.Index(text, "Gemini CLI") {
		t.Error("the installed first")
	}

	v.Confirm = true
	g.Clear(vt.DefaultStyle)
	Draw(g, Frame{AgentManager: v}, DefaultTheme())
	if text := strings.Join(gridText(g), "\n"); !strings.Contains(text, "install: npm install -g @google/gemini-cli") || !strings.Contains(text, "enter runs it") {
		t.Errorf("the command, before it runs:\n%s", text)
	}

	geo := AgentManagerLayout(v, 100, 30)
	for line, i := range geo.Rows {
		if i < 0 {
			continue
		}
		if got, ok := AgentManagerEntryAt(v, 100, 30, geo.List.X+3, geo.List.Y+line); !ok || got != i {
			t.Errorf("line %d is entry %d, a click found %d %v", line, i, got, ok)
		}
	}
}

// TestMemoryToolsAreListedApartFromTheAgents: the agents come first, the
// installed then the rest, and the memory tools under a heading of their
// own after them, a running one said to be running. If it regresses, a
// memory tool reads as an agent the user is missing.
func TestMemoryToolsAreListedApartFromTheAgents(t *testing.T) {
	v := &AgentManagerView{Agents: []AgentEntry{
		{Name: "Claude Code", Installed: true, Version: "2.0"},
		{Name: "Codex", InstallCommand: "npm install -g @openai/codex"},
		{Name: "ai-memory", Memory: true, Installed: true, Running: true},
		{Name: "Graphify", Memory: true, InstallCommand: "uv tool install graphifyy && graphify install"},
	}}
	_, labels := agentLines(v)
	if got := strings.Join(labels, "|"); got != "INSTALLED|||AVAILABLE|||MEMORY||" {
		t.Errorf("lines: %q", got)
	}
	g := vt.NewGrid(120, 36, 0)
	Draw(g, Frame{AgentManager: v}, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	if !strings.Contains(text, "MEMORY") || !regexp.MustCompile(`ai-memory\s+running`).MatchString(text) {
		t.Errorf("panel:\n%s", text)
	}
}
