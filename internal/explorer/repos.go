package explorer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A folder of projects — ~/src with a repository in each directory — is
// herdr-sidebar's multi-repo folder: the changes view lists each
// repository's changes under its name, the tree has every repository's
// letters, and what acts on a repository (branch, sync, history, commit)
// acts on the one the selection is in. The panel keeps them all and makes
// the one under the cursor the active one, which is what m.git and
// m.status are.

// repoState is one repository and what git last said of it.
type repoState struct {
	git    Git
	status *Status
	err    error
}

// maxRepoDepth is how far below the folder repositories are looked for:
// a folder of projects, and a folder of folders of them.
const maxRepoDepth = 2

// discoverRepos finds the repositories under a folder that is not one.
func discoverRepos(root string) []Git {
	var out []Git
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxRepoDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || strings.HasPrefix(name, ".") || name == "node_modules" {
				continue
			}
			path := filepath.Join(dir, name)
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				out = append(out, Git{Top: path})
				continue // a repository's own subdirectories are its business
			}
			walk(path, depth+1)
		}
	}
	walk(root, 1)
	// In the tree's order, which does not put capitals first.
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Top) < strings.ToLower(out[b].Top) })
	return out
}

// reposFor is the repositories a panel rooted at dir shows, and the root:
// the repository dir is in, whole; or the folder with those under it.
func reposFor(dir string) (root string, repos []Git) {
	if g := FindRepo(dir); g.Top != "" {
		return g.Top, []Git{g}
	}
	return dir, discoverRepos(dir)
}

// multiRepo is whether the panel shows a folder of repositories.
func (m *Model) multiRepo() bool { return isMultiRepo(m.tree.Root, m.repos) }

// isMultiRepo is whether a panel rooted at root, with these repositories,
// shows a folder of them rather than one whole.
func isMultiRepo(root string, repos []repoState) bool {
	return len(repos) > 1 || len(repos) == 1 && repos[0].git.Top != root
}

// activate makes a repository the one actions go to; -1 is none.
func (m *Model) activate(i int) {
	if i < 0 || i >= len(m.repos) {
		m.active, m.git, m.status, m.statusErr = -1, Git{}, nil, nil
		return
	}
	r := m.repos[i]
	m.active, m.git, m.status, m.statusErr = i, r.git, r.status, r.err
	m.changesStaged = 0
	if r.status != nil {
		for _, c := range r.status.Changes {
			if c.Staged() {
				m.changesStaged++
			}
		}
	}
}

// repoOf is the repository a path from the root is in, or -1.
func (m *Model) repoOf(rel string) int { return repoIndex(m.tree.Root, m.repos, rel) }

// repoIndex is the repository a path from root is in, or -1: the deepest,
// for a repository inside another's folder.
func repoIndex(root string, repos []repoState, rel string) int {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	best, bestLen := -1, -1
	for i, r := range repos {
		if (abs == r.git.Top || strings.HasPrefix(abs, r.git.Top+string(filepath.Separator))) && len(r.git.Top) > bestLen {
			best, bestLen = i, len(r.git.Top)
		}
	}
	return best
}

// statusFor is the git letter of a tree entry, from whichever repository
// it is in.
func (m *Model) statusFor(n *Node) byte { return entryLetter(m.tree, m.repos, n) }

// entryLetter is the git letter of a tree entry, or 0, from the status of
// the repository it is in: what the panel and the desktop's tree both show.
func entryLetter(t *Tree, repos []repoState, n *Node) byte {
	i := repoIndex(t.Root, repos, n.Rel)
	if i < 0 || repos[i].status == nil {
		return 0
	}
	st, g := repos[i].status, repos[i].git
	rel := t.repoPath(g, n.Rel)
	if rel == "." {
		// A repository's own folder, in a folder of them: marked when it
		// holds any change, as any folder is.
		if len(st.Changes) > 0 {
			return st.Changes[0].Letter()
		}
		return 0
	}
	if n.Dir {
		letter := st.DirLetter(rel)
		if c, ok := st.Of(rel); ok {
			letter = c.Letter() // an untracked directory is one change
		}
		return letter
	}
	if c, ok := st.Of(rel); ok {
		return c.Letter()
	}
	return 0
}

// syncActive makes the repository of the selection the active one.
func (m *Model) syncActive() {
	if len(m.repos) <= 1 {
		return
	}
	switch m.view {
	case ViewFiles:
		if n := m.selectedNode(); n != nil {
			if i := m.repoOf(n.Rel); i >= 0 && i != m.active {
				m.activate(i)
			}
		}
	case ViewChanges:
		if i := m.cursor[ViewChanges]; i < len(m.changeRows) && m.changeRows[i].repo != m.active {
			m.activate(m.changeRows[i].repo)
		}
	}
}

// repoName is a repository as the panel names it: its path from the
// folder shown.
func (m *Model) repoName(i int) string {
	if i < 0 || i >= len(m.repos) {
		return ""
	}
	return repoLabel(m.tree.Root, m.repos[i].git.Top)
}

// repoLabel is a repository named by its path from the folder shown, or by
// its own name when it is the folder.
func repoLabel(root, top string) string {
	if rel, err := filepath.Rel(root, top); err == nil && rel != "." {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(top)
}
