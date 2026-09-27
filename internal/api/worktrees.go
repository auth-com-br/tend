package api

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/worktree"
)

// Worktrees over the socket, herdr's four methods. A worktree becomes a space:
// rooted in the checkout, filed in a group named after its repository, so the
// sidebar shows an agent's room under the project it belongs to.
//
// The git work happens here, on the caller's goroutine, and never under the
// server's lock — a worktree add on a large repository takes seconds, and the
// server lock is the one every pane needs.

// Methods for worktrees.
const (
	MethodWorktreeList   = "worktree.list"
	MethodWorktreeCreate = "worktree.create"
	MethodWorktreeOpen   = "worktree.open"
	MethodWorktreeRemove = "worktree.remove"
)

// WorktreeInfo is a worktree, and the space it is open in if any.
type WorktreeInfo struct {
	worktree.Worktree
	OpenWorkspaceID string `json:"open_workspace_id,omitempty"`
	Label           string `json:"label"`
}

// callWorktrees answers the worktree methods, and reports whether the method
// was one of them.
func (a *API) callWorktrees(req Request) (any, bool, error) {
	switch req.Method {
	case MethodWorktreeList:
		var p WorktreeListParams
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		repo, err := a.worktreeRepo(p.WorkspaceID, p.Cwd, p.Trust)
		if err != nil {
			return nil, true, err
		}
		list, err := worktree.List(repo)
		if err != nil {
			return nil, true, fail("worktree_list_failed", "%v", err)
		}
		out := make([]WorktreeInfo, 0, len(list))
		for _, wt := range list {
			out = append(out, a.worktreeInfo(wt))
		}
		return map[string]any{"type": "worktree_list", "source": repo, "worktrees": out}, true, nil

	case MethodWorktreeCreate:
		var p WorktreeCreateParams
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		repo, err := a.worktreeRepo(p.WorkspaceID, p.Cwd, p.Trust)
		if err != nil {
			return nil, true, err
		}
		branch := strings.TrimSpace(p.Branch)
		if branch == "" {
			branch = worktree.GeneratedBranch(uint64(time.Now().UnixNano()))
		}
		path := p.Path
		switch {
		case path == "":
			path = worktree.DefaultPath(a.worktreeRoot(), repo.Name, branch)
		case !filepath.IsAbs(worktree.ExpandHome(path)):
			return nil, true, fail("invalid_request", "worktree path must be absolute")
		default:
			path = worktree.ExpandHome(path)
		}
		if err := worktree.Add(repo, path, branch, p.Base); err != nil {
			return nil, true, fail("worktree_create_failed", "%v", err)
		}
		return a.openWorktree(repo, worktree.Worktree{Path: path, Branch: branch, Linked: true}, p.Label, "worktree_created", p.WorkspaceID)

	case MethodWorktreeOpen:
		var p WorktreeOpenParams
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		if (p.Path == "") == (p.Branch == "") {
			return nil, true, fail("invalid_request", "exactly one of path or branch is required")
		}
		repo, err := a.worktreeRepo(p.WorkspaceID, p.Cwd, p.Trust)
		if err != nil {
			return nil, true, err
		}
		list, err := worktree.List(repo)
		if err != nil {
			return nil, true, fail("worktree_list_failed", "%v", err)
		}
		for _, wt := range list {
			match := (p.Path != "" && worktree.Same(wt.Path, worktree.ExpandHome(p.Path))) ||
				(p.Branch != "" && wt.Branch == p.Branch)
			if !match {
				continue
			}
			if wt.Bare || wt.Prunable {
				return nil, true, fail("worktree_not_found", "that worktree cannot be opened")
			}
			return a.openWorktree(repo, wt, p.Label, "worktree_opened", p.WorkspaceID)
		}
		return nil, true, fail("worktree_not_found", "no worktree of %s matches", repo.Name)

	case MethodWorktreeRemove:
		var p WorktreeRemoveParams
		if err := decode(req.Params, &p); err != nil {
			return nil, true, err
		}
		id, ok := parseID("w_", p.WorkspaceID)
		if !ok {
			return nil, true, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)
		}
		dir := a.workspaceDir(session.WorkspaceID(id))
		if dir == "" {
			return nil, true, fail("workspace_not_found", "workspace %s not found", p.WorkspaceID)
		}
		repo, err := worktree.FindTrusted(dir, p.Trust)
		if err != nil {
			return nil, true, fail("worktree_not_found", "%v", err)
		}
		if worktree.Same(repo.Checkout, repo.Root) {
			// The main checkout is the repository. Removing it is not what
			// "remove this worktree" can mean, and git would refuse anyway.
			return nil, true, fail("worktree_remove_failed", "%s is the repository's main checkout, not a worktree", dir)
		}
		// The order is herdr's (`should_shutdown_workspace_terminal_runtimes_
		// for_worktree_remove`). Forced, the space closes first: its programs
		// must not go on running in a directory being deleted under them.
		// Not forced, git is asked first, and a refusal — changes that would
		// be lost — leaves the space and the agent in it where they were.
		var closed spaceShape
		if p.Force {
			closed = a.shapeOf(session.WorkspaceID(id))
			if err := a.srv.CloseWorkspace(session.WorkspaceID(id)); err != nil {
				return nil, true, workspaceErr(p.WorkspaceID, err)
			}
		}
		if err := worktree.Remove(repo, repo.Checkout, p.Force); err != nil {
			if p.Force {
				// git refused after the space was shut for it. The space
				// comes back, as herdr brings back what it shut down
				// (restore_shutdown_worktree_panes): its programs were
				// ended, but the place and its tabs are not lost to a
				// removal that did not happen.
				a.reopenShape(closed)
			}
			if errors.Is(err, worktree.ErrDirty) {
				return nil, true, fail("worktree_dirty",
					"%s has changes that would be lost; pass force to remove it anyway", repo.Checkout)
			}
			return nil, true, fail("worktree_remove_failed", "%v", err)
		}
		if !p.Force {
			if err := a.srv.CloseWorkspace(session.WorkspaceID(id)); err != nil {
				return nil, true, workspaceErr(p.WorkspaceID, err)
			}
		}
		a.srv.AnnounceWorktree(server.EventWorktreeRemoved, session.WorkspaceID(id))
		return map[string]any{
			"type": "worktree_removed", "workspace_id": p.WorkspaceID,
			"path": repo.Checkout, "forced": p.Force,
		}, true, nil
	}
	return nil, false, nil
}

// spaceShape is enough of a space to open it again: its name, directory,
// group, and each tab's name and panes' directories.
type spaceShape struct {
	name, dir, group string
	tabs             []tabShape
	// companies are the ones it was in, which it goes back into.
	companies []session.CompanyID
}

type tabShape struct {
	name string
	dirs []string
}

// shapeOf records a space before it is closed.
func (a *API) shapeOf(id session.WorkspaceID) spaceShape {
	var out spaceShape
	a.srv.Session(func(sess *session.Session) {
		w, ok := sess.Workspace(id)
		if !ok {
			return
		}
		out = spaceShape{name: w.Name, dir: w.Dir, group: w.Group, companies: sess.CompaniesOf(id)}
		for _, t := range w.Tabs() {
			tab := tabShape{name: t.Name}
			for _, pid := range t.Panes() {
				if p, ok := t.Pane(pid); ok {
					tab.dirs = append(tab.dirs, p.Dir)
				}
			}
			out.tabs = append(out.tabs, tab)
		}
	})
	return out
}

// reopenShape opens a recorded space again, each pane a shell in the
// directory it had: the programs are gone, the place is not.
func (a *API) reopenShape(shape spaceShape) {
	if shape.dir == "" {
		return
	}
	id, err := a.srv.NewWorkspaceIn(shape.name, shape.dir)
	if err != nil {
		return
	}
	if shape.group != "" {
		_ = a.srv.GroupWorkspace(id, shape.group)
	}
	_ = a.srv.JoinCompanies(id, shape.companies)
	for _, tab := range shape.tabs {
		if len(tab.dirs) == 0 {
			continue
		}
		_, first, err := a.srv.NewTab(id, tab.name, PaneSpecIn(a.shell(), tab.dirs[0]))
		if err != nil {
			continue
		}
		for _, dir := range tab.dirs[1:] {
			_, _ = a.srv.SplitPane(first, session.Columns, PaneSpecIn(a.shell(), dir))
		}
	}
}

// defaultWorktreeRoot is used when nothing configured one.
const defaultWorktreeRoot = "~/.tend/worktrees"

// worktreeRoot is where new worktrees go, always as an absolute path.
//
// An empty or relative setting must not reach git: `git -C <repo> worktree add
// project/branch` puts the checkout inside the repository it is a checkout of,
// which is what happened when the setting failed to arrive.
func (a *API) worktreeRoot() string {
	root := worktree.ExpandHome(a.worktreeDir)
	if root == "" || !filepath.IsAbs(root) {
		root = worktree.ExpandHome(defaultWorktreeRoot)
	}
	return root
}

// worktreeRepo finds the repository a call is about: the one a space is in,
// or the one a directory is in. Exactly one may be given; neither means the
// first space, which is where somebody at a prompt usually is.
func (a *API) worktreeRepo(workspaceID, cwd string, trust bool) (worktree.Repo, error) {
	if workspaceID != "" && cwd != "" {
		return worktree.Repo{}, fail("invalid_request", "only one of workspace_id or cwd may be supplied")
	}
	dir := worktree.ExpandHome(cwd)
	if workspaceID != "" {
		id, ok := parseID("w_", workspaceID)
		if !ok {
			return worktree.Repo{}, fail("workspace_not_found", "workspace %s not found", workspaceID)
		}
		if dir = a.workspaceDir(session.WorkspaceID(id)); dir == "" {
			return worktree.Repo{}, fail("workspace_not_found", "workspace %s has no directory", workspaceID)
		}
	}
	if dir == "" {
		if ws, err := a.firstWorkspace(); err == nil {
			dir = a.workspaceDir(ws)
		}
	}
	if dir == "" {
		return worktree.Repo{}, fail("invalid_request", "name a space or a directory inside a repository")
	}
	repo, err := worktree.FindTrusted(dir, trust)
	if err != nil {
		return worktree.Repo{}, fail("not_a_repository", "%v", err)
	}
	return repo, nil
}

// workspaceDir is where a space is rooted.
func (a *API) workspaceDir(id session.WorkspaceID) string {
	var dir string
	a.srv.Session(func(sess *session.Session) {
		if w, ok := sess.Workspace(id); ok {
			dir = w.Dir
		}
	})
	return dir
}

// openWorktree puts a worktree in a space: the one it is already open in, or
// a new one in the repository's group.
//
// The space goes into the companies of the space it was opened from (from,
// a workspace id), as a space made in the sidebar goes into the company
// shown: made from a space of a company, it is found in that company rather
// than only among all spaces (#34).
func (a *API) openWorktree(repo worktree.Repo, wt worktree.Worktree, label, kind, from string) (any, bool, error) {
	var companies []session.CompanyID
	if src, ok := parseID("w_", from); ok {
		companies = a.srv.CompaniesOf(session.WorkspaceID(src))
	}
	info := a.worktreeInfo(wt)
	if info.OpenWorkspaceID != "" {
		id, _ := parseID("w_", info.OpenWorkspaceID)
		_ = a.srv.JoinCompanies(session.WorkspaceID(id), companies)
		result, err := a.workspaceResult(session.WorkspaceID(id))
		if err != nil {
			return nil, true, err
		}
		if kind == "worktree_created" {
			a.srv.AnnounceWorktree(server.EventWorktreeCreated, session.WorkspaceID(id))
		}
		a.srv.AnnounceWorktree(server.EventWorktreeOpened, session.WorkspaceID(id))
		return map[string]any{
			"type": kind, "workspace": result.(map[string]any)["workspace"],
			"worktree": info, "already_open": true,
		}, true, nil
	}

	name := label
	if name == "" {
		name = info.Label
	}
	id, err := a.srv.NewWorkspaceIn(name, wt.Path)
	if err != nil {
		return nil, true, fail("worktree_open_failed", "%v", err)
	}
	// Filed under the repository, so a project's rooms sit together in the
	// sidebar. herdr calls this membership; tend already has groups.
	_ = a.srv.GroupWorkspace(id, repo.Name)
	_ = a.srv.JoinCompanies(id, companies)
	if _, _, err := a.srv.NewTab(id, "tab 1", PaneSpecIn(a.shell(), wt.Path)); err != nil {
		return nil, true, fail("worktree_open_failed", "%v", err)
	}
	info.OpenWorkspaceID = WorkspaceID(id)
	result, err := a.workspaceResult(id)
	if err != nil {
		return nil, true, err
	}
	if kind == "worktree_created" {
		a.srv.AnnounceWorktree(server.EventWorktreeCreated, id)
	}
	a.srv.AnnounceWorktree(server.EventWorktreeOpened, id)
	return map[string]any{
		"type": kind, "workspace": result.(map[string]any)["workspace"],
		"worktree": info, "already_open": false,
	}, true, nil
}

// worktreeInfo describes a worktree and says which space it is open in.
func (a *API) worktreeInfo(wt worktree.Worktree) WorktreeInfo {
	info := WorktreeInfo{Worktree: wt, Label: wt.Branch}
	if info.Label == "" {
		info.Label = filepath.Base(wt.Path)
	}
	// Copied out first and compared after: comparing resolves symlinks, which
	// is the filesystem, and nothing touches the filesystem under the lock
	// every pane operation needs.
	type space struct {
		id  session.WorkspaceID
		dir string
	}
	var spaces []space
	a.srv.Session(func(sess *session.Session) {
		for _, w := range sess.Workspaces() {
			if w.Dir != "" {
				spaces = append(spaces, space{w.ID, w.Dir})
			}
		}
	})
	for _, sp := range spaces {
		if worktree.Same(sp.dir, wt.Path) {
			info.OpenWorkspaceID = WorkspaceID(sp.id)
			break
		}
	}
	return info
}

// PaneSpecIn is a pane running command in dir.
func PaneSpecIn(command []string, dir string) server.PaneSpec {
	return server.PaneSpec{Command: command, Dir: dir}
}
