package session

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// TestACompanyHoldsWorkspacesAndOnlyLooksAtThem: a company is made empty,
// takes workspaces in the order they are given it, holds each once, may
// share one with another company, and deleting it leaves its workspaces
// where they were. If it regresses, deleting a company closes the spaces
// the user organised with it, or a space shows twice in one company.
func TestACompanyHoldsWorkspacesAndOnlyLooksAtThem(t *testing.T) {
	s := New()
	a, b := s.AddWorkspace("a"), s.AddWorkspace("b")
	acme, err := s.AddCompany("  Acme  ")
	if err != nil {
		t.Fatal(err)
	}
	if acme.Name != "Acme" || len(acme.Workspaces) != 0 {
		t.Errorf("new company = %+v", acme)
	}
	other, _ := s.AddCompany("Other")

	for _, w := range []WorkspaceID{b.ID, a.ID, b.ID} {
		if err := s.AssignCompany(acme.ID, w); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AssignCompany(other.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(acme.Workspaces, []WorkspaceID{b.ID, a.ID}) {
		t.Errorf("acme holds %v, want b then a once each", acme.Workspaces)
	}
	check(t, s)

	if err := s.UnassignCompany(acme.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnassignCompany(acme.ID, b.ID); err != nil {
		t.Errorf("taking out one that is not in it: %v", err)
	}
	if err := s.RenameCompany(acme.ID, "Acme Ltda"); err != nil || acme.Name != "Acme Ltda" {
		t.Errorf("rename: %v %q", err, acme.Name)
	}
	if err := s.RemoveCompany(acme.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Workspaces()) != 2 || len(s.Companies()) != 1 {
		t.Errorf("after delete: %d workspaces, %d companies", len(s.Workspaces()), len(s.Companies()))
	}
	check(t, s)
}

// TestACompanyRefusesWhatCannotBeShown: an empty name, a company or a
// workspace that does not exist are errors, each its own. If it regresses,
// the list gains a blank company, or a space id from a stale client is
// kept pointing at nothing.
func TestACompanyRefusesWhatCannotBeShown(t *testing.T) {
	s := New()
	w := s.AddWorkspace("w")
	if _, err := s.AddCompany("   "); !errors.Is(err, ErrEmptyName) {
		t.Errorf("empty name: %v", err)
	}
	c, _ := s.AddCompany("c")
	if err := s.RenameCompany(c.ID, ""); !errors.Is(err, ErrEmptyName) {
		t.Errorf("empty rename: %v", err)
	}
	if err := s.AssignCompany(c.ID, 999); !errors.Is(err, ErrNoSuchWorkspace) {
		t.Errorf("missing workspace: %v", err)
	}
	if err := s.AssignCompany(999, w.ID); !errors.Is(err, ErrNoSuchCompany) {
		t.Errorf("missing company: %v", err)
	}
	if err := s.RemoveCompany(999); !errors.Is(err, ErrNoSuchCompany) {
		t.Errorf("remove missing: %v", err)
	}
}

// TestClosingAWorkspaceTakesItOutOfItsCompanies: herdr's
// remove_workspace_from_user_spaces. If it regresses, the session fails
// its own invariants the moment a space in a company is closed.
func TestClosingAWorkspaceTakesItOutOfItsCompanies(t *testing.T) {
	s := New()
	a, b := s.AddWorkspace("a"), s.AddWorkspace("b")
	c, _ := s.AddCompany("c")
	_ = s.AssignCompany(c.ID, a.ID)
	_ = s.AssignCompany(c.ID, b.ID)
	if _, err := s.CloseWorkspace(a.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Workspaces, []WorkspaceID{b.ID}) {
		t.Errorf("company holds %v after a closed", c.Workspaces)
	}
	check(t, s)
}

// TestCompaniesComeBackAfterARestart: the companies, their order, their
// members and the counter survive a snapshot and a restore; a file that
// names a workspace that is gone, or one twice, is cleaned rather than
// refused (herdr's sanitize_user_spaces); and a session that never had a
// company writes nothing about them. If it regresses, restarting the
// server loses the user's companies, or refuses the whole session over a
// stale member.
func TestCompaniesComeBackAfterARestart(t *testing.T) {
	s := New()
	a := s.AddWorkspace("a")
	if _, _, err := s.AddTab(a.ID, "t", PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(s.Snapshot(nil))
	var fields map[string]any
	_ = json.Unmarshal(plain, &fields)
	if _, ok := fields["companies"]; ok {
		t.Errorf("a session with no companies wrote them: %s", plain)
	}

	first, _ := s.AddCompany("first")
	second, _ := s.AddCompany("second")
	_ = s.AssignCompany(second.ID, a.ID)

	snap := s.Snapshot(nil)
	snap.Companies[1].Workspaces = append(snap.Companies[1].Workspaces, 777, uint64(a.ID))
	back, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	got := back.Companies()
	if len(got) != 2 || got[0].ID != first.ID || got[1].Name != "second" ||
		!slices.Equal(got[1].Workspaces, []WorkspaceID{a.ID}) {
		t.Fatalf("restored %+v %+v", got[0], got[1])
	}
	third, _ := back.AddCompany("third")
	if third.ID <= second.ID {
		t.Errorf("a company made after the restore reused id %d", third.ID)
	}
	check(t, back)
}

// TestASpaceMadeFromAnotherJoinsItsCompanies: a worktree's space, opened
// from a space in two companies, is put in both and in no other; one already
// in a company stays there once; an origin that has gone leaves it unfiled
// rather than failing. If it regresses, a worktree opened while a company is
// chosen shows only under "all spaces" (issue #34).
func TestASpaceMadeFromAnotherJoinsItsCompanies(t *testing.T) {
	s := New()
	origin, made, stray := s.AddWorkspace("origin"), s.AddWorkspace("made"), s.AddWorkspace("stray")
	auth, _ := s.AddCompany("Auth")
	acme, _ := s.AddCompany("Acme")
	other, _ := s.AddCompany("Other")
	for _, c := range []CompanyID{auth.ID, acme.ID} {
		if err := s.AssignCompany(c, origin.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AssignCompany(acme.ID, made.ID); err != nil {
		t.Fatal(err)
	}

	if err := s.JoinCompaniesOf(made.ID, origin.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.CompaniesOf(made.ID); !slices.Equal(got, []CompanyID{auth.ID, acme.ID}) {
		t.Errorf("made is in %v, want Auth and Acme", got)
	}
	if slices.Contains(other.Workspaces, made.ID) {
		t.Error("made went into a company its origin is not in")
	}
	check(t, s)

	if _, err := s.CloseWorkspace(origin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.JoinCompaniesOf(stray.ID, origin.ID); err != nil {
		t.Errorf("an origin that has gone: %v", err)
	}
	if got := s.CompaniesOf(stray.ID); len(got) != 0 {
		t.Errorf("stray is in %v, want none", got)
	}
	if err := s.JoinCompaniesOf(999, made.ID); !errors.Is(err, ErrNoSuchWorkspace) {
		t.Errorf("a space that does not exist: %v", err)
	}
	check(t, s)
}
