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
