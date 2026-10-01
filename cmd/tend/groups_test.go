package main

import (
	"slices"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

func groupHeadings(rows []ui.SidebarRow) []string {
	var out []string
	for _, r := range rows {
		if r.Kind == ui.SidebarSpaceGroup {
			out = append(out, r.Group)
		}
	}
	return out
}

// TestAnEmptyGroupIsListedWhereItBelongs: a group with no space in the list
// is still listed — with no company chosen always, and with one chosen when
// the group has had a space of that company — in the order the groups were
// made; and from a server that keeps no groups, a group is still the names
// its spaces carry. If it regresses, closing every space in a group makes it
// vanish, or an empty group of one company clutters another's list.
func TestAnEmptyGroupIsListedWhereItBelongs(t *testing.T) {
	tt := &tui{}
	tt.snap = proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{
			{ID: 1, Name: "a"},
			{ID: 2, Name: "b", Group: "later"},
		},
		Groups: []proto.GroupInfo{
			{Name: "first", Companies: []uint64{7}},
			{Name: "later", Companies: []uint64{7}},
			{Name: "elsewhere", Companies: []uint64{8}},
		},
		Companies: []proto.CompanyInfo{
			{ID: 7, Name: "Acme", Workspaces: []uint64{2}},
			{ID: 8, Name: "Auth"},
		},
	}

	if got := groupHeadings(tt.spaceRowsLocked()); !slices.Equal(got, []string{"first", "later", "elsewhere"}) {
		t.Errorf("with no company chosen the groups are %v", got)
	}

	tt.company = 7
	if got := groupHeadings(tt.spaceRowsLocked()); !slices.Equal(got, []string{"first", "later"}) {
		t.Errorf("with Acme chosen the groups are %v, want its two", got)
	}

	tt.company = 8
	if got := groupHeadings(tt.spaceRowsLocked()); !slices.Equal(got, []string{"elsewhere"}) {
		t.Errorf("with Auth chosen the groups are %v, want its one", got)
	}

	tt.company = 0
	tt.snap.Groups = nil
	if got := groupHeadings(tt.spaceRowsLocked()); !slices.Equal(got, []string{"later"}) {
		t.Errorf("from an older server the groups are %v, want the one a space names", got)
	}
}

// TestAGroupStartsOnlyInstalledAgents: the list a group offers is the
// catalog's agents that are installed, without the memory tools listed
// with them. If it regresses, picking an agent that is not there leaves a
// space with a terminal that says "not found".
func TestAGroupStartsOnlyInstalledAgents(t *testing.T) {
	got := installedAgents([]proto.AgentStatus{
		{ID: "claude", Name: "Claude Code", Installed: true},
		{ID: "codex", Name: "Codex"},
		{ID: "graphify", Name: "Graphify", Installed: true, Kind: "memory"},
		{ID: "agy", Name: "Antigravity CLI", Installed: true},
	})
	if len(got) != 2 || got[0].ID != "claude" || got[1].ID != "agy" {
		t.Errorf("offered %+v, want Claude Code and Antigravity CLI", got)
	}
}
