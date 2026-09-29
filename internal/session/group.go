package session

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Groups are the named sets the user keeps spaces together in, down the
// sidebar. herdr has none of its own (its only grouping is a repository's
// worktrees); tend's are the owner's, made by hand.
//
// A group used to be nothing but the name its spaces carried, so the last
// space leaving it — closed, or moved out — took the group with it. A group
// is something the user made, and it now lasts until they delete it: it has
// a record here, and an empty one is still listed.
//
// A group also remembers the companies it has had spaces in. With a company
// chosen, a group is listed when it has a space of that company in it, and
// when it once had: closing the last of a company's spaces in a group must
// not make the group vanish from that company either, while an empty group
// the company never had stays out of it.

// Group is one group.
type Group struct {
	Name string
	// Companies are the companies the group has held a space of.
	Companies []CompanyID
	// Dir is the folder a space made in the group starts in, or empty for
	// the one a space starts in anyway. It is the user's: a group is often
	// one client's work, and its folder is where every new terminal for it
	// should open.
	Dir string
}

// ErrNoSuchGroup is a group name the session does not have.
var ErrNoSuchGroup = errors.New("session: no such group")

// Groups returns the groups in the order they were made.
func (s *Session) Groups() []*Group { return s.groups }

// Group looks a group up by name.
func (s *Session) Group(name string) (*Group, bool) {
	for _, g := range s.groups {
		if g.Name == name {
			return g, true
		}
	}
	return nil, false
}

// ensureGroup is the group with a name, made when there is none.
func (s *Session) ensureGroup(name string) *Group {
	if g, ok := s.Group(name); ok {
		return g
	}
	g := &Group{Name: name}
	s.groups = append(s.groups, g)
	return g
}

// noteCompanies adds a workspace's companies to its group's.
func (s *Session) noteCompanies(w *Workspace) {
	if w.Group == "" {
		return
	}
	g := s.ensureGroup(w.Group)
	for _, c := range s.CompaniesOf(w.ID) {
		if !slices.Contains(g.Companies, c) {
			g.Companies = append(g.Companies, c)
		}
	}
}

// GroupWorkspace moves a workspace into a group, making the group when it is
// new, or out of any when the name is empty. The group it leaves stays, even
// empty.
func (s *Session) GroupWorkspace(id WorkspaceID, group string) error {
	w, ok := s.Workspace(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchWorkspace, id)
	}
	w.Group = group
	s.noteCompanies(w)
	return nil
}

// RenameGroup names a group again, and every space in it with it. A name
// another group has already merges the two, which is what moving every
// space of one into the other would have done.
func (s *Session) RenameGroup(old, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrEmptyName
	}
	g, ok := s.Group(old)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoSuchGroup, old)
	}
	if name == old {
		return nil
	}
	for _, w := range s.workspaces {
		if w.Group == old {
			w.Group = name
		}
	}
	if into, ok := s.Group(name); ok {
		if into.Dir == "" {
			into.Dir = g.Dir
		}
		for _, c := range g.Companies {
			if !slices.Contains(into.Companies, c) {
				into.Companies = append(into.Companies, c)
			}
		}
		s.groups = slices.DeleteFunc(s.groups, func(x *Group) bool { return x == g })
		return nil
	}
	g.Name = name
	return nil
}

// SetGroupDir sets the folder a group's new spaces start in, or clears it
// when dir is empty. Whether the folder exists is the server's to check: it
// is on the machine the panes run on, which this package knows nothing of.
func (s *Session) SetGroupDir(name, dir string) error {
	g, ok := s.Group(name)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoSuchGroup, name)
	}
	g.Dir = dir
	return nil
}

// RemoveGroup deletes a group. Its spaces are not touched but for leaving
// it: a group is a way of keeping them together, not what they are.
func (s *Session) RemoveGroup(name string) error {
	i := slices.IndexFunc(s.groups, func(g *Group) bool { return g.Name == name })
	if i < 0 {
		return fmt.Errorf("%w: %q", ErrNoSuchGroup, name)
	}
	s.groups = slices.Delete(s.groups, i, i+1)
	for _, w := range s.workspaces {
		if w.Group == name {
			w.Group = ""
		}
	}
	return nil
}

// forgetCompanyInGroups takes a deleted company out of every group.
func (s *Session) forgetCompanyInGroups(id CompanyID) {
	for _, g := range s.groups {
		g.Companies = slices.DeleteFunc(g.Companies, func(c CompanyID) bool { return c == id })
	}
}

// sanitizeGroups settles the groups a restored file has against its
// workspaces: a nameless or repeated group, or a company that is gone, is
// dropped, and a group a workspace names that the file does not list — every
// group, in a file from before groups had records — is made, in the order its
// spaces come, with the companies they are in.
func (s *Session) sanitizeGroups() {
	seen := make(map[string]bool, len(s.groups))
	s.groups = slices.DeleteFunc(s.groups, func(g *Group) bool {
		drop := g.Name == "" || seen[g.Name]
		seen[g.Name] = true
		return drop
	})
	for _, g := range s.groups {
		g.Companies = slices.DeleteFunc(g.Companies, func(c CompanyID) bool {
			_, ok := s.Company(c)
			return !ok
		})
	}
	for _, w := range s.workspaces {
		s.noteCompanies(w)
	}
}

// checkGroups is CheckInvariants' part for groups.
func (s *Session) checkGroups() error {
	names := make(map[string]bool, len(s.groups))
	for _, g := range s.groups {
		if g.Name == "" || names[g.Name] {
			return fmt.Errorf("group %q is nameless or listed twice", g.Name)
		}
		names[g.Name] = true
		for _, c := range g.Companies {
			if _, ok := s.Company(c); !ok {
				return fmt.Errorf("group %q remembers company %d, which does not exist", g.Name, c)
			}
		}
	}
	for _, w := range s.workspaces {
		if w.Group != "" && !names[w.Group] {
			return fmt.Errorf("workspace %d is in group %q, which is not listed", w.ID, w.Group)
		}
	}
	return nil
}
