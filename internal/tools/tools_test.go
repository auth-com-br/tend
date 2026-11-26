package tools

import (
	"errors"
	"reflect"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// TestTheAgentToSendContextToIsTheOneBeingWorkedWith: the pane in focus when
// it runs an agent, else an agent running in the tab shown, else one in the
// space, else the pane in focus. If it regresses, "send to the agent" types
// a capture into a shell, or into an agent of another space.
func TestTheAgentToSendContextToIsTheOneBeingWorkedWith(t *testing.T) {
	panes := []proto.PaneInfo{
		{ID: 1, Command: []string{"/bin/zsh"}},
		{ID: 2, Agent: "codex", Running: true},
		{ID: 3, Agent: "claude", Running: true},
	}
	if id, name := ContextTarget(panes, 3, []uint64{1, 2}, nil); id != 3 || name != "3 claude" {
		t.Errorf("focused agent: %d %q", id, name)
	}
	if id, _ := ContextTarget(panes, 1, []uint64{1, 2}, nil); id != 2 {
		t.Errorf("agent in the tab: %d", id)
	}
	if id, _ := ContextTarget(panes, 1, []uint64{1}, [][]uint64{{1}, {3}}); id != 3 {
		t.Errorf("agent in the space: %d", id)
	}
	if id, name := ContextTarget(panes, 1, []uint64{1}, nil); id != 1 || name != "1 zsh" {
		t.Errorf("the focus: %d %q", id, name)
	}
}

// TestAPermissionListShowsModesThenTheRulesTheFilterKeeps: a mode whose file
// is not there and that is not set is left out (but for the global one, where
// it is set), and the filter narrows the rules only. If it regresses, the
// list fills with modes that cannot be set, or a filter hides the modes.
func TestAPermissionListShowsModesThenTheRulesTheFilterKeeps(t *testing.T) {
	a := proto.PermAgent{
		ID:        "claude",
		Files:     []proto.PermFile{{Path: "/g", Scope: "global", Exists: true}, {Path: "/p", Scope: "project"}},
		Modes:     []proto.PermMode{{File: "/g", Scope: "global", Key: "defaultMode"}, {File: "/p", Scope: "project", Key: "defaultMode"}},
		Rules:     []proto.PermRule{{File: "/g", Scope: "global", Decision: "allow", Text: "Bash(ssh x)"}, {File: "/g", Scope: "global", Decision: "deny", Text: "Read(.env)"}},
		Decisions: []string{"allow", "ask", "deny"},
	}
	items, lines := PermList(a, "", "ssh")
	if len(items) != 2 || items[0].Mode == nil || items[1].Rule == nil || items[1].Rule.Text != "Bash(ssh x)" {
		t.Fatalf("items %+v", items)
	}
	if lines[0].Text != "defaultMode = (not set)" || !lines[0].Mode {
		t.Errorf("mode line %+v", lines[0])
	}
	if got := PermNextDecision(a, "deny"); got != "allow" {
		t.Errorf("after deny: %q", got)
	}
	if got := PermNextDecision(proto.PermAgent{Decisions: []string{"allow"}}, "allow"); got != "" {
		t.Errorf("an agent with one decision has a next: %q", got)
	}
}

// TestTheCatalogListsInstalledAgentsFirstAndMemoryToolsLast: if it
// regresses, the manager's cursor and what it installs disagree with the
// order the list is drawn in.
func TestTheCatalogListsInstalledAgentsFirstAndMemoryToolsLast(t *testing.T) {
	list := []proto.AgentStatus{
		{ID: "mem", Kind: "memory", Installed: true},
		{ID: "a"},
		{ID: "b", Installed: true},
	}
	SortCatalog(list)
	var ids []string
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "a", "mem"}) {
		t.Errorf("order %v", ids)
	}
	if got := CatalogSummary(list); got != "1 of 2 agents installed on this machine" {
		t.Errorf("summary %q", got)
	}
}

// TestASessionThatCannotBeResumedHereSaysWhatWouldResumeIt: one whose
// directory cannot be entered gets the command, quoted, and no pane. If it
// regresses, a tab opens that cannot start.
func TestASessionThatCannotBeResumedHereSaysWhatWouldResumeIt(t *testing.T) {
	_, _, command, ok := Resume(ui.SessionEntry{ID: "abc", Agent: "claude", Dir: "/root/x y", Unreachable: "not readable"})
	if !ok || command != "cd '/root/x y' && 'claude' '--resume' 'abc'" {
		t.Errorf("command %q ok %v", command, ok)
	}
	spec, name, command, ok := Resume(ui.SessionEntry{ID: "abc", Agent: "claude", Dir: "/w", Title: "a very long title that goes on and on"})
	if !ok || command != "" || spec.Dir != "/w" || spec.Agent != "claude" || len([]rune(name)) != 24 {
		t.Errorf("spec %+v name %q", spec, name)
	}
}

// TestARefusalIsSaidWithoutTheMethod: if it regresses, every panel's error
// starts with a method name nobody typed.
func TestARefusalIsSaidWithoutTheMethod(t *testing.T) {
	if got := ErrorText(errors.New("mcp.add: no such agent")); got != "no such agent" {
		t.Errorf("%q", got)
	}
	if got := ErrorText(errors.New("could not: x")); got != "could not: x" {
		t.Errorf("%q", got)
	}
}
