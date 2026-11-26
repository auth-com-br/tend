package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
)

// TestTheSidebarCardsSayWhatEachSpaceAndAgentIs: the window's sidebar is
// drawn from cards, not the TUI's tokens. A space's says its name (its
// folder's when it has none), where it is with the home folder as ~, its
// branch and how far it is from upstream, and how its agents are; an
// agent's says which agent, what it is doing, where it is, its state in
// words and what it has spent — and not a shell's leftover title. If it
// regresses, the sidebar shows blanks or the TUI's dashes.
func TestTheSidebarCardsSayWhatEachSpaceAndAgentIs(t *testing.T) {
	home, _ := os.UserHomeDir()
	snap := &proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{{
			ID: 1, Dir: filepath.Join(home, "src", "shop"), Branch: "main", Ahead: 2, Behind: 1,
			Tabs: []proto.TabInfo{{ID: 10, Name: "fix", Panes: []uint64{100, 101, 102}}},
		}},
		Panes: []proto.PaneInfo{
			{ID: 100, Agent: "claude", Running: true, State: "working", Title: "✳ Fixing the checkout",
				Usage: &proto.PaneUsage{Cost: 0.42, CostKnown: true}},
			{ID: 101, Agent: "codex", Running: true, State: "blocked", Title: "akira@host:~/src/shop"},
			{ID: 102},
		},
	}
	info := map[uint64]proto.PaneInfo{}
	for _, p := range snap.Panes {
		info[p.ID] = p
	}
	spaces := []Row{{Kind: "space", Workspace: 1}}
	agents := []Row{{Kind: "agent", Pane: 100}, {Kind: "agent", Pane: 101}}
	cards(spaces, agents, snap, info, time.Now())

	sc := spaces[0].Space
	if sc == nil || sc.Name != "shop" || sc.Folder != "~/src/shop" || sc.Branch != "main" || sc.Ahead != 2 || sc.Behind != 1 {
		t.Fatalf("the space's card: %+v", sc)
	}
	if sc.Agents != 2 || sc.Working != 1 || sc.Waiting != 1 {
		t.Errorf("its agents: %+v", sc)
	}
	a := agents[0].Agent
	if a == nil || a.Kind != "claude" || a.Doing == "" || a.Where != "shop · fix" || a.StateText != "working" || a.Cost != "$0.42" {
		t.Errorf("the working agent's card: %+v", a)
	}
	b := agents[1].Agent
	if b == nil || b.Doing != "" || b.StateText != "needs you" {
		t.Errorf("the waiting agent's card: %+v", b)
	}
}
