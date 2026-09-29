package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/worktree"
)

// Company answers the company methods: herdr's user-defined Spaces
// (src/app/api/spaces.rs), a grouping of workspaces that changes nothing
// about how they run. Every change is a session change, so every client
// redraws its list and the state file keeps it.
func (s *Server) Company(method string, p proto.CompanyParams) (any, error) {
	id, ws := session.CompanyID(p.Company), session.WorkspaceID(p.Workspace)
	var made session.CompanyID
	err := s.rearrange(func(sess *session.Session) error {
		switch method {
		case proto.MethodCompanyCreate:
			c, err := sess.AddCompany(p.Name)
			if err != nil {
				return err
			}
			made = c.ID
			if ws != 0 {
				// Made and filled in one step, as herdr's space.create
				// with assign_workspace_id is; a space that is gone is
				// not a reason to lose the company.
				if _, ok := sess.Workspace(ws); ok {
					return sess.AssignCompany(c.ID, ws)
				}
			}
			return nil
		case proto.MethodCompanyRename:
			return sess.RenameCompany(id, p.Name)
		case proto.MethodCompanyDelete:
			return sess.RemoveCompany(id)
		case proto.MethodCompanyAssign:
			return sess.AssignCompany(id, ws)
		case proto.MethodCompanyUnassign:
			return sess.UnassignCompany(id, ws)
		}
		return fmt.Errorf("%w: %s", proto.ErrUnknownMethod, method)
	})
	if err != nil {
		return nil, err
	}
	if method == proto.MethodCompanyCreate {
		return proto.CompanyCreateResult{Company: uint64(made)}, nil
	}
	return nil, nil
}

// JoinCompaniesOf files a space made from another in the companies its
// origin is in. It is the server's, not the client's, because the spaces it
// is for — worktrees — are made by the server on the automation socket, and
// a space made there from a space in a company belongs to that company for
// every client and for a script alike.
func (s *Server) JoinCompaniesOf(ws, origin session.WorkspaceID) error {
	return s.rearrange(func(sess *session.Session) error { return sess.JoinCompaniesOf(ws, origin) })
}

// companiesSnapshotLocked is the companies as a client is sent them. The
// caller holds the server lock.
func companiesSnapshotLocked(sess *session.Session) []proto.CompanyInfo {
	var out []proto.CompanyInfo
	for _, c := range sess.Companies() {
		info := proto.CompanyInfo{ID: uint64(c.ID), Name: c.Name}
		for _, w := range c.Workspaces {
			info.Workspaces = append(info.Workspaces, uint64(w))
		}
		out = append(out, info)
	}
	return out
}

// Group answers the group methods. A group is made by putting a space in it
// (workspace.group); these rename one and delete one, which are the only
// ways it goes now that an empty group is kept.
func (s *Server) Group(method string, p proto.GroupParams) error {
	if method == proto.MethodGroupSetDir && p.Dir != "" {
		// Checked here, before the lock, because it is I/O, and here rather
		// than in the client because the folder must be on the machine the
		// panes run on, which over ssh is not the client's.
		dir, err := groupDir(p.Dir)
		if err != nil {
			return err
		}
		p.Dir = dir
	}
	return s.rearrange(func(sess *session.Session) error {
		switch method {
		case proto.MethodGroupSetDir:
			return sess.SetGroupDir(p.Group, p.Dir)
		case proto.MethodGroupRename:
			return sess.RenameGroup(p.Group, p.Name)
		case proto.MethodGroupDelete:
			return sess.RemoveGroup(p.Group)
		}
		return fmt.Errorf("%w: %s", proto.ErrUnknownMethod, method)
	})
}

// groupDir is a folder as a group keeps it: a leading ~ is the home of the
// user the server runs as, and the result is absolute, cleaned, and a folder
// that exists — a group pointing at a typo would start every new space
// somewhere else without saying so.
func groupDir(dir string) (string, error) {
	dir = worktree.ExpandHome(strings.TrimSpace(dir))
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("no such folder: %s", abs)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a folder: %s", abs)
	}
	return abs, nil
}

// groupsSnapshotLocked is the groups as a client is sent them. The caller
// holds the server lock.
func groupsSnapshotLocked(sess *session.Session) []proto.GroupInfo {
	var out []proto.GroupInfo
	for _, g := range sess.Groups() {
		info := proto.GroupInfo{Name: g.Name, Dir: g.Dir}
		for _, c := range g.Companies {
			info.Companies = append(info.Companies, uint64(c))
		}
		out = append(out, info)
	}
	return out
}
