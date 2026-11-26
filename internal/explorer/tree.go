package explorer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The tree is read a directory at a time, when it is opened, and never
// walked whole: a project with a node_modules has hundreds of thousands of
// entries, and nobody opens most of them.

// Node is one entry of the tree.
type Node struct {
	Name string
	// Rel is the path from the explorer's root, with forward slashes.
	Rel      string
	Dir      bool
	Link     bool
	Ignored  bool
	Expanded bool
	loaded   bool
	Children []*Node
	Depth    int
	parent   *Node
}

// Tree is the directory the explorer shows, and what of it is open.
type Tree struct {
	Root string
	top  *Node
	// repos are the repositories under the root, so a directory's ignored
	// entries are asked of the repository it is in.
	repos []Git
	// Hidden hides entries whose name starts with a dot.
	Hidden bool
}

// NewTree is a tree of root with only its first level read.
func NewTree(root string) *Tree {
	t := &Tree{Root: root, top: &Node{Name: filepath.Base(root), Dir: true, Expanded: true, Depth: -1}}
	return t
}

// load reads a directory's entries: directories first, then files, each by
// name without regard to case, as a file manager sorts them.
func (t *Tree) load(n *Node, g Git) {
	entries, err := os.ReadDir(filepath.Join(t.Root, filepath.FromSlash(n.Rel)))
	n.loaded = true
	if err != nil {
		n.Children = nil
		return
	}
	previous := map[string]*Node{}
	for _, c := range n.Children {
		previous[c.Name] = c
	}
	kids := make([]*Node, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if name == ".git" {
			continue
		}
		rel := name
		if n.Rel != "" {
			rel = n.Rel + "/" + name
		}
		child := previous[name]
		if child == nil {
			child = &Node{Name: name, Rel: rel}
		}
		child.parent, child.Depth = n, n.Depth+1
		child.Link = e.Type()&os.ModeSymlink != 0
		child.Dir = e.IsDir()
		if child.Link {
			// A link to a directory opens like one.
			if info, err := os.Stat(filepath.Join(t.Root, filepath.FromSlash(rel))); err == nil {
				child.Dir = info.IsDir()
			}
		}
		if !child.Dir {
			child.Expanded, child.Children, child.loaded = false, nil, false
		}
		kids = append(kids, child)
	}
	sort.SliceStable(kids, func(a, b int) bool {
		if kids[a].Dir != kids[b].Dir {
			return kids[a].Dir
		}
		return strings.ToLower(kids[a].Name) < strings.ToLower(kids[b].Name)
	})
	if r := t.repoAt(filepath.Join(t.Root, filepath.FromSlash(n.Rel))); r.Top != "" {
		g = r
	}
	if g.Top != "" && t.inside(g, n.Rel) {
		paths := make([]string, len(kids))
		for i, k := range kids {
			paths[i] = t.repoPath(g, k.Rel)
		}
		ignored := g.Ignored(paths)
		for i, k := range kids {
			k.Ignored = ignored[paths[i]]
		}
	}
	n.Children = kids
}

// repoPath is a tree path as git names it, relative to the repository's top.
func (t *Tree) repoPath(g Git, rel string) string {
	abs := filepath.Join(t.Root, filepath.FromSlash(rel))
	if r, err := filepath.Rel(g.Top, abs); err == nil {
		return filepath.ToSlash(r)
	}
	return rel
}

// Reload reads again every directory that is open, keeping what is open
// open. It is how the tree follows files an agent creates while it is shown.
func (t *Tree) Reload(g Git) {
	var walk func(n *Node)
	walk = func(n *Node) {
		if !n.Expanded {
			return
		}
		t.load(n, g)
		for _, c := range n.Children {
			if c.Dir && c.Expanded {
				walk(c)
			}
		}
	}
	walk(t.top)
}

// Rows is the tree as it is drawn: every entry whose parents are open.
func (t *Tree) Rows(g Git) []*Node {
	if !t.top.loaded {
		t.load(t.top, g)
	}
	var out []*Node
	var walk func(n *Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if t.Hidden && strings.HasPrefix(c.Name, ".") {
				continue
			}
			out = append(out, c)
			if c.Dir && c.Expanded {
				if !c.loaded {
					t.load(c, g)
				}
				walk(c)
			}
		}
	}
	walk(t.top)
	return out
}

// Toggle opens a closed directory and closes an open one.
func (t *Tree) Toggle(n *Node, g Git) {
	if !n.Dir {
		return
	}
	n.Expanded = !n.Expanded
	if n.Expanded {
		t.load(n, g)
	}
}

// Reveal opens every directory above a path, so a file found by search can
// be shown where it lives. It returns the node, or nil.
func (t *Tree) Reveal(rel string, g Git) *Node {
	n := t.top
	if !n.loaded {
		t.load(n, g)
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		var next *Node
		for _, c := range n.Children {
			if c.Name == part {
				next = c
			}
		}
		if next == nil {
			return nil
		}
		if i < len(parts)-1 {
			if !next.Expanded || !next.loaded {
				next.Expanded = true
				t.load(next, g)
			}
		}
		n = next
	}
	return n
}

// find is the node at a path whose directories above are loaded, or nil.
// It opens nothing: Reveal is for a path not yet shown.
func (t *Tree) find(rel string) *Node {
	n := t.top
	for _, part := range strings.Split(rel, "/") {
		var next *Node
		for _, c := range n.Children {
			if c.Name == part {
				next = c
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

// Parent is the directory a node is in, or nil at the top.
func (n *Node) Parent() *Node {
	if n.parent == nil || n.parent.Depth < 0 {
		return nil
	}
	return n.parent
}

// Path is the node's path on disk.
func (t *Tree) Path(n *Node) string {
	return filepath.Join(t.Root, filepath.FromSlash(n.Rel))
}

// repoAt is the repository a directory is in, of those the tree knows.
func (t *Tree) repoAt(abs string) Git {
	best := Git{}
	for _, r := range t.repos {
		if (abs == r.Top || strings.HasPrefix(abs, r.Top+string(filepath.Separator))) && len(r.Top) > len(best.Top) {
			best = r
		}
	}
	return best
}

// inside is whether a directory of the tree is within a repository, whose
// ignore rules then apply to it.
func (t *Tree) inside(g Git, rel string) bool {
	abs := filepath.Join(t.Root, filepath.FromSlash(rel))
	return abs == g.Top || strings.HasPrefix(abs, g.Top+string(filepath.Separator))
}
