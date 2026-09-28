package session

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Companies are herdr's user-defined Spaces (src/user_space.rs): named
// groupings of workspaces, for whoever looks at the session to organise it
// by — the companies one works for, each with its clients' spaces. They are
// presentation only. A workspace in a company runs exactly as one outside
// it, and membership is many-to-many as herdr's is: the same space may be
// kept under two companies.
//
// herdr calls them spaces; tend already calls its workspaces that, so here
// they are companies, which is what they were asked for as.
//
// Which company is being looked at is not here. It is what one person is
// looking at, and stays in the client; the companies and who belongs to
// them are a fact about the session, and every client sees the same ones.

// CompanyID identifies a company for the life of the session.
type CompanyID uint64

// Company is one named set of workspaces, in the order they were added.
type Company struct {
	ID         CompanyID
	Name       string
	Workspaces []WorkspaceID
}

var (
	// ErrNoSuchCompany is a company id the session does not have.
	ErrNoSuchCompany = errors.New("session: no such company")
	// ErrEmptyName is a name with nothing in it, which herdr refuses too: a
	// company listed as a blank line cannot be told apart or picked.
	ErrEmptyName = errors.New("session: the name is empty")
)

// Companies returns the companies in the order they were made.
func (s *Session) Companies() []*Company { return s.companies }

// Company looks a company up by id.
func (s *Session) Company(id CompanyID) (*Company, bool) {
	for _, c := range s.companies {
		if c.ID == id {
			return c, true
		}
	}
	return nil, false
}

// AddCompany makes an empty company.
func (s *Session) AddCompany(name string) (*Company, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrEmptyName
	}
	s.nextCompany++
	c := &Company{ID: CompanyID(s.nextCompany), Name: name}
	s.companies = append(s.companies, c)
	return c, nil
}

// RenameCompany names a company again.
func (s *Session) RenameCompany(id CompanyID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrEmptyName
	}
	c, ok := s.Company(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchCompany, id)
	}
	c.Name = name
	return nil
}

// RemoveCompany deletes a company. Its workspaces are not touched: a
// company is only a way of looking at them.
func (s *Session) RemoveCompany(id CompanyID) error {
	for i, c := range s.companies {
		if c.ID == id {
			s.companies = slices.Delete(s.companies, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("%w: %d", ErrNoSuchCompany, id)
}

// AssignCompany puts a workspace in a company, after the ones already
// there. Assigning one that is there already leaves it where it is.
func (s *Session) AssignCompany(id CompanyID, ws WorkspaceID) error {
	c, ok := s.Company(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchCompany, id)
	}
	if _, ok := s.Workspace(ws); !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchWorkspace, ws)
	}
	if !slices.Contains(c.Workspaces, ws) {
		c.Workspaces = append(c.Workspaces, ws)
	}
	return nil
}

// UnassignCompany takes a workspace out of a company. One that was not in
// it is not an error, as it is not in herdr: the state asked for holds.
func (s *Session) UnassignCompany(id CompanyID, ws WorkspaceID) error {
	c, ok := s.Company(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchCompany, id)
	}
	c.Workspaces = slices.DeleteFunc(c.Workspaces, func(w WorkspaceID) bool { return w == ws })
	return nil
}

// CompaniesOf is every company a workspace is in, in the order they were made.
func (s *Session) CompaniesOf(ws WorkspaceID) []CompanyID {
	var out []CompanyID
	for _, c := range s.companies {
		if slices.Contains(c.Workspaces, ws) {
			out = append(out, c.ID)
		}
	}
	return out
}

// JoinCompaniesOf puts a workspace in every company another one is in. A
// space made from another — a worktree opened from a space — belongs where
// the space it came from does: made while one company was chosen, it used to
// land in none, and showed only under "all spaces" (issue #34).
//
// It only adds. A company the workspace is already in, or one the origin is
// not, is left as it is; an origin that has gone since is not an error, since
// the space it was asked for exists and is merely unfiled.
func (s *Session) JoinCompaniesOf(ws, origin WorkspaceID) error {
	if _, ok := s.Workspace(ws); !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchWorkspace, ws)
	}
	for _, id := range s.CompaniesOf(origin) {
		if err := s.AssignCompany(id, ws); err != nil {
			return err
		}
	}
	return nil
}

// forgetWorkspace takes a closed workspace out of every company (herdr's
// remove_workspace_from_user_spaces), so none of them points at nothing.
func (s *Session) forgetWorkspace(ws WorkspaceID) {
	for _, c := range s.companies {
		c.Workspaces = slices.DeleteFunc(c.Workspaces, func(w WorkspaceID) bool { return w == ws })
	}
}

// sanitizeCompanies drops the members a restored file names that the
// session does not have, and a member named twice (herdr's
// sanitize_user_spaces). A company is a way of looking at workspaces, and
// one pointing at a space that is gone is not worth refusing a restore over.
func (s *Session) sanitizeCompanies() {
	known := make(map[WorkspaceID]bool, len(s.workspaces))
	for _, w := range s.workspaces {
		known[w.ID] = true
	}
	for _, c := range s.companies {
		seen := make(map[WorkspaceID]bool, len(c.Workspaces))
		c.Workspaces = slices.DeleteFunc(c.Workspaces, func(w WorkspaceID) bool {
			drop := !known[w] || seen[w]
			seen[w] = true
			return drop
		})
		s.nextCompany = max(s.nextCompany, uint64(c.ID))
	}
}

// checkCompanies is CheckInvariants' part for companies.
func (s *Session) checkCompanies() error {
	known := make(map[WorkspaceID]bool, len(s.workspaces))
	for _, w := range s.workspaces {
		known[w.ID] = true
	}
	ids := make(map[CompanyID]bool, len(s.companies))
	for _, c := range s.companies {
		if c.ID == 0 || ids[c.ID] {
			return fmt.Errorf("company id %d is zero or used twice", c.ID)
		}
		ids[c.ID] = true
		if uint64(c.ID) > s.nextCompany {
			return fmt.Errorf("company %d is past the counter %d", c.ID, s.nextCompany)
		}
		seen := make(map[WorkspaceID]bool, len(c.Workspaces))
		for _, w := range c.Workspaces {
			if !known[w] {
				return fmt.Errorf("company %d holds workspace %d, which does not exist", c.ID, w)
			}
			if seen[w] {
				return fmt.Errorf("company %d holds workspace %d twice", c.ID, w)
			}
			seen[w] = true
		}
	}
	return nil
}
