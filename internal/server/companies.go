package server

import (
	"fmt"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
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

// JoinCompanies puts a workspace in each company named — the companies of
// the space it was made from, so a worktree opened from a space of a
// company is found in that company (#34). A company deleted meanwhile is
// passed over.
func (s *Server) JoinCompanies(ws session.WorkspaceID, ids []session.CompanyID) error {
	if len(ids) == 0 {
		return nil
	}
	return s.rearrange(func(sess *session.Session) error {
		for _, id := range ids {
			if _, ok := sess.Company(id); ok {
				if err := sess.AssignCompany(id, ws); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// CompaniesOf is the companies a workspace is in.
func (s *Server) CompaniesOf(ws session.WorkspaceID) []session.CompanyID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.CompaniesOf(ws)
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
