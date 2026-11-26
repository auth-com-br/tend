package sidebar

import (
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// session is two spaces in a group, one alone, and an agent in each of three
// states; space 1 has a blocked agent.
func session() *proto.SessionSnapshot {
	return &proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{
			{ID: 1, Name: "api", Group: "acme", Tabs: []proto.TabInfo{{ID: 10, Panes: []uint64{100, 101}}}},
			{ID: 2, Name: "solo", Tabs: []proto.TabInfo{{ID: 20, Panes: []uint64{200}}}},
			{ID: 3, Name: "web", Group: "acme", Tabs: []proto.TabInfo{{ID: 30, Name: "main", Panes: []uint64{300}}}},
		},
		Panes: []proto.PaneInfo{
			{ID: 100, Agent: "claude", State: "working", Running: true, StateSeq: 5},
			{ID: 101}, // a shell
			{ID: 200, Agent: "codex", State: "idle", Done: true, Running: true, StateSeq: 9},
			{ID: 300, Agent: "claude", State: "blocked", Running: true, StateSeq: 1},
		},
		Companies: []proto.CompanyInfo{{ID: 7, Name: "acme", Workspaces: []uint64{1, 3}}},
	}
}

func labels(rows []ui.SidebarRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Label)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTheSpacesAreATreeInTheSessionsOrder: a group sits where its first
// space is, with its spaces under it, and says how many are waiting even
// folded. If it regresses, the TUI and the desktop list spaces somewhere
// other than where the user put them, or a folded group hides a waiting
// agent.
func TestTheSpacesAreATreeInTheSessionsOrder(t *testing.T) {
	snap := session()
	v := View{Snap: snap}
	rows := v.SpaceRows(SpaceSource{Snap: snap, Folded: map[string]bool{}})
	if got, want := labels(rows), []string{"acme", "api", "web", "solo"}; !equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	if rows[0].Kind != ui.SidebarSpaceGroup || rows[1].Depth != 1 || rows[3].Depth != 0 {
		t.Errorf("tree shape wrong: %+v", rows)
	}
	folded := v.SpaceRows(SpaceSource{Snap: snap, Folded: map[string]bool{"acme": true}})
	if got, want := labels(folded), []string{"acme", "solo"}; !equal(got, want) {
		t.Errorf("folded rows %v, want %v", got, want)
	}
	if folded[0].Trailing != "1 waiting" {
		t.Errorf("a folded group says %q, want 1 waiting", folded[0].Trailing)
	}
}

// TestAChosenCompanyListsItsOwn: with a company chosen, its spaces and
// agents are listed and the rest are not.
func TestAChosenCompanyListsItsOwn(t *testing.T) {
	snap := session()
	v := View{Snap: snap, Company: 7}
	if got, want := labels(v.SpaceRows(SpaceSource{Snap: snap})), []string{"acme", "api", "web"}; !equal(got, want) {
		t.Errorf("spaces %v, want %v", got, want)
	}
	for _, r := range v.AgentRows("") {
		if r.Pane == 200 {
			t.Error("the agent of a space outside the company was listed")
		}
	}
	// A company the session no longer has shows everything.
	gone := View{Snap: snap, Company: 99}
	if len(gone.SpaceRows(SpaceSource{Snap: snap})) != 4 {
		t.Error("a deleted company still filtered the list")
	}
}

// TestTheAttentionQueuePutsWhatNeedsYouFirst: herdr's order — blocked, then
// done, then working — and shells are never agents. If it regresses, the
// agent waiting for an answer is not at the top of the list.
func TestTheAttentionQueuePutsWhatNeedsYouFirst(t *testing.T) {
	snap := session()
	v := View{Snap: snap}
	v.UI.AgentPanelSort = "priority"
	rows := v.AgentRows("")
	var panes []uint64
	for _, r := range rows {
		panes = append(panes, r.Pane)
	}
	if len(panes) != 3 || panes[0] != 300 || panes[1] != 200 || panes[2] != 100 {
		t.Fatalf("order %v, want [300 200 100]", panes)
	}
	if rows[1].State != "done" {
		t.Errorf("an idle agent that finished unseen shows %q, want done", rows[1].State)
	}
}

// TestGroupedAgentsAreUnderTheirTabs: in session order with the list
// grouped, each tab with agents has a heading.
func TestGroupedAgentsAreUnderTheirTabs(t *testing.T) {
	snap := session()
	v := View{Snap: snap, Grouped: true}
	rows := v.AgentRows("")
	if got, want := labels(rows), []string{"api · -", "api", "solo · -", "solo", "web · main", "web · main"}; !equal(got, want) {
		t.Errorf("rows %v, want %v", got, want)
	}
}

// TestDisplayState: a sleeping agent is asleep, an idle one that finished
// unseen is done.
func TestDisplayState(t *testing.T) {
	cases := map[string]proto.PaneInfo{
		"asleep":  {State: "working", Dormant: true},
		"done":    {State: "idle", Done: true},
		"idle":    {State: "idle"},
		"blocked": {State: "blocked", Done: true},
	}
	for want, p := range cases {
		if got := DisplayState(p); got != want {
			t.Errorf("%+v shows %q, want %q", p, got, want)
		}
	}
}

// TestASpinnerFrameIsDroppedFromATitle, and only one.
func TestASpinnerFrameIsDroppedFromATitle(t *testing.T) {
	for in, want := range map[string]string{
		"✳ Fix the build": "Fix the build",
		"⠋ npm test":      "npm test",
		"★ production":    "★ production",
		"":                "",
	} {
		if got := StrippedTerminalTitle(in); got != want {
			t.Errorf("%q gave %q, want %q", in, got, want)
		}
	}
}
