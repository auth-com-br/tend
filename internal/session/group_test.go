package session

import (
	"errors"
	"slices"
	"testing"
)

func groupNames(s *Session) []string {
	var out []string
	for _, g := range s.Groups() {
		out = append(out, g.Name)
	}
	return out
}

// TestAGroupOutlivesItsLastSpace: a group stays when its last space is moved
// out or closed, and goes only when it is deleted, which leaves its spaces
// where they were, ungrouped. If it regresses, closing everything in a group
// makes the group vanish, and the user has to make it again.
func TestAGroupOutlivesItsLastSpace(t *testing.T) {
	s := New()
	a, b := s.AddWorkspace("a"), s.AddWorkspace("b")
	for _, w := range []WorkspaceID{a.ID, b.ID} {
		if err := s.GroupWorkspace(w, "clients"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.GroupWorkspace(a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseWorkspace(b.ID); err != nil {
		t.Fatal(err)
	}
	if got := groupNames(s); !slices.Equal(got, []string{"clients"}) {
		t.Fatalf("after its spaces left, the groups are %v", got)
	}
	check(t, s)

	if err := s.GroupWorkspace(a.ID, "clients"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveGroup("clients"); err != nil {
		t.Fatal(err)
	}
	if len(s.Groups()) != 0 || a.Group != "" {
		t.Errorf("deleted: groups %v, a in %q", groupNames(s), a.Group)
	}
	if _, ok := s.Workspace(a.ID); !ok {
		t.Error("deleting a group closed its space")
	}
	if err := s.RemoveGroup("clients"); !errors.Is(err, ErrNoSuchGroup) {
		t.Errorf("deleting it twice: %v", err)
	}
	check(t, s)
}

// TestRenamingAGroupTakesItsSpacesAndItsEmptiness: a rename moves every space
// with it and leaves no group behind under the old name, empty or not, and a
// rename onto another group's name merges the two. If it regresses, renaming
// leaves the old name as an empty group now that empty groups are kept.
func TestRenamingAGroupTakesItsSpacesAndItsEmptiness(t *testing.T) {
	s := New()
	a, b := s.AddWorkspace("a"), s.AddWorkspace("b")
	_ = s.GroupWorkspace(a.ID, "old")
	_ = s.GroupWorkspace(b.ID, "other")
	if err := s.RenameGroup("old", "  new "); err != nil {
		t.Fatal(err)
	}
	if got := groupNames(s); !slices.Equal(got, []string{"new", "other"}) || a.Group != "new" {
		t.Fatalf("renamed: groups %v, a in %q", got, a.Group)
	}
	if err := s.RenameGroup("new", "other"); err != nil {
		t.Fatal(err)
	}
	if got := groupNames(s); !slices.Equal(got, []string{"other"}) || a.Group != "other" {
		t.Errorf("merged: groups %v, a in %q", got, a.Group)
	}
	if err := s.RenameGroup("other", " "); !errors.Is(err, ErrEmptyName) {
		t.Errorf("an empty name: %v", err)
	}
	if err := s.RenameGroup("nothing", "x"); !errors.Is(err, ErrNoSuchGroup) {
		t.Errorf("a group that is not there: %v", err)
	}
	check(t, s)
}

// TestAGroupRemembersTheCompaniesItHadSpacesOf: a group records every company
// one of its spaces is in, whether the space joined the group or the company
// first, and forgets a company that is deleted. If it regresses, emptying a
// group while a company is chosen hides it from that company.
func TestAGroupRemembersTheCompaniesItHadSpacesOf(t *testing.T) {
	s := New()
	a, b := s.AddWorkspace("a"), s.AddWorkspace("b")
	acme, _ := s.AddCompany("Acme")
	auth, _ := s.AddCompany("Auth")
	_ = s.AssignCompany(acme.ID, a.ID)
	_ = s.GroupWorkspace(a.ID, "g")
	_ = s.GroupWorkspace(b.ID, "g")
	_ = s.AssignCompany(auth.ID, b.ID)
	if _, err := s.CloseWorkspace(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseWorkspace(b.ID); err != nil {
		t.Fatal(err)
	}
	g, ok := s.Group("g")
	if !ok || !slices.Equal(g.Companies, []CompanyID{acme.ID, auth.ID}) {
		t.Fatalf("group %+v, want it to remember Acme and Auth", g)
	}
	if err := s.RemoveCompany(acme.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.Companies, []CompanyID{auth.ID}) {
		t.Errorf("after Acme was deleted the group remembers %v", g.Companies)
	}
	check(t, s)
}

// TestGroupsComeBackAfterARestart: an empty group and the companies a group
// remembers survive a snapshot; a file from before groups had records gets
// them from its spaces; a repeated or nameless group, or a company that is
// gone, is cleaned rather than refused. If it regresses, restarting the
// server loses the user's empty groups, or refuses an older state file.
func TestGroupsComeBackAfterARestart(t *testing.T) {
	s := New()
	a := s.AddWorkspace("a")
	if _, _, err := s.AddTab(a.ID, "t", PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	acme, _ := s.AddCompany("Acme")
	_ = s.AssignCompany(acme.ID, a.ID)
	_ = s.GroupWorkspace(a.ID, "kept")
	_ = s.GroupWorkspace(a.ID, "full")

	snap := s.Snapshot(nil)
	back, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	if got := groupNames(back); !slices.Equal(got, []string{"kept", "full"}) {
		t.Errorf("restored groups %v", got)
	}
	if g, _ := back.Group("kept"); !slices.Equal(g.Companies, []CompanyID{acme.ID}) {
		t.Errorf("the empty group forgot its company: %+v", g)
	}
	check(t, back)

	old := s.Snapshot(nil)
	old.Groups = nil
	back, err = Restore(old)
	if err != nil {
		t.Fatal(err)
	}
	if got := groupNames(back); !slices.Equal(got, []string{"full"}) {
		t.Errorf("an older file gave groups %v, want the one its space names", got)
	}
	check(t, back)

	messy := s.Snapshot(nil)
	messy.Groups = append(messy.Groups, GroupSnapshot{Name: "kept"}, GroupSnapshot{}, GroupSnapshot{Name: "x", Companies: []uint64{99}})
	back, err = Restore(messy)
	if err != nil {
		t.Fatal(err)
	}
	if got := groupNames(back); !slices.Equal(got, []string{"kept", "full", "x"}) {
		t.Errorf("a messy file gave groups %v", got)
	}
	if g, _ := back.Group("x"); len(g.Companies) != 0 {
		t.Errorf("a company that is gone was kept: %v", g.Companies)
	}
	check(t, back)
}

// TestAGroupKeepsItsFolder: a group's folder is set, cleared, carried
// through a rename that merges into a group with none, and survives a
// restart. If it regresses, a group's new spaces go back to starting in
// whatever folder tend was run from after a restart or a rename.
func TestAGroupKeepsItsFolder(t *testing.T) {
	s := New()
	a := s.AddWorkspace("a")
	if _, _, err := s.AddTab(a.ID, "t", PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	_ = s.GroupWorkspace(a.ID, "acme")
	if err := s.SetGroupDir("acme", "/work/acme"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupDir("nothing", "/x"); !errors.Is(err, ErrNoSuchGroup) {
		t.Errorf("a group that is not there: %v", err)
	}
	back, err := Restore(s.Snapshot(nil))
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := back.Group("acme"); g.Dir != "/work/acme" {
		t.Errorf("after a restart the folder is %q", g.Dir)
	}

	_ = s.GroupWorkspace(a.ID, "other")
	if err := s.RenameGroup("acme", "other"); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.Group("other"); g.Dir != "/work/acme" {
		t.Errorf("merged into a group with no folder, the folder is %q", g.Dir)
	}
	if err := s.SetGroupDir("other", ""); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.Group("other"); g.Dir != "" {
		t.Errorf("cleared, the folder is %q", g.Dir)
	}
	check(t, s)
}
