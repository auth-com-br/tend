package server

import (
	"fmt"

	"github.com/auth-com-br/tend/internal/session"
)

// A layout is an arrangement somebody set up and would like back: three panes
// in a particular shape, each in its own directory, running the thing it runs.
// herdr exports and applies one (`layout.export`, `layout.apply`); this is
// that, over tend's own split tree rather than herdr's binary one.
//
// Exporting reads the tree; applying rebuilds it by splitting, which is the
// only way to make panes — there is no constructor for a shape, and the
// programs have to be started as panes are started everywhere else.

// LayoutSpec is a tab's arrangement: the tree, and what each leaf runs.
type LayoutSpec struct {
	Tree  session.LayoutSnapshot `json:"tree"`
	Panes map[uint64]PaneSpec    `json:"-"`
}

// ExportLayout describes a tab: its tree, and what each pane is.
func (s *Server) ExportLayout(id session.TabID) (session.LayoutSnapshot, []session.PaneSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return session.LayoutSnapshot{}, nil, ErrClosed
	}
	tree, ok := s.session.TabLayout(id)
	if !ok {
		return session.LayoutSnapshot{}, nil, fmt.Errorf("%w: %d", session.ErrNoSuchTab, id)
	}
	tab, _ := s.session.Tab(id)

	var panes []session.PaneSnapshot
	for _, paneID := range tab.Panes() {
		p, ok := tab.Pane(paneID)
		if !ok {
			continue
		}
		dir := p.Dir
		if rt, live := s.runtimes[paneID]; live {
			// Where the pane is now, not where it started: an arrangement is
			// worth having back in the directories it ended up in.
			if now := rt.pty.Cwd(); now != "" {
				dir = now
			}
		}
		panes = append(panes, session.PaneSnapshot{
			ID: uint64(p.ID), Title: p.Title, Named: p.Named,
			Command: p.Command, Dir: dir, Agent: p.Agent,
		})
	}
	return tree, panes, nil
}

// ApplyLayout builds an arrangement as a new tab in a space.
//
// The panes are started as the tree is rebuilt, so a layout that names a
// command nobody has takes the tab down with it rather than leaving half an
// arrangement: a half-applied layout is worse than none, because it looks like
// it worked.
func (s *Server) ApplyLayout(ws session.WorkspaceID, name string, tree session.LayoutSnapshot, specs map[uint64]PaneSpec, fallback PaneSpec) (session.TabID, error) {
	leaves := layoutLeaves(tree)
	if len(leaves) == 0 {
		return 0, fmt.Errorf("server: that layout has no panes in it")
	}

	first := specFor(specs, leaves[0], fallback)
	tab, root, err := s.NewTab(ws, name, first)
	if err != nil {
		return 0, err
	}

	if err := s.buildLayout(root, tree, specs, fallback); err != nil {
		_ = s.CloseTab(tab)
		return 0, err
	}

	s.mu.Lock()
	err = s.session.SetLayoutSizes(tab, tree)
	s.mu.Unlock()
	if err != nil {
		// The shape is right and the shares are not, which is worth saying
		// and not worth throwing the tab away over.
		s.logf("layout: %v", err)
	}
	s.publish(Event{Kind: EventSessionChanged})
	return tab, nil
}

// buildLayout makes pane, which holds the whole of node's area and runs its
// first leaf, into node's shape. Its children are made first, as siblings
// split off in node's direction one after another, each started as its own
// first leaf; then each is made into its own shape the same way. Splitting
// every leaf off the one before it in the root's direction, as this did
// before, built an editor beside two stacked panes as three side by side,
// and the sizes could not be copied onto that.
func (s *Server) buildLayout(pane session.PaneID, node session.LayoutSnapshot, specs map[uint64]PaneSpec, fallback PaneSpec) error {
	if len(node.Kids) == 0 {
		return nil
	}
	direction := session.Columns
	if node.Dir == "rows" {
		direction = session.Rows
	}
	kids := []session.PaneID{pane}
	for _, kid := range node.Kids[1:] {
		next, err := s.SplitPane(kids[len(kids)-1], direction, specFor(specs, layoutLeaves(kid)[0], fallback))
		if err != nil {
			return err
		}
		kids = append(kids, next)
	}
	for i, kid := range node.Kids {
		if err := s.buildLayout(kids[i], kid, specs, fallback); err != nil {
			return err
		}
	}
	return nil
}

// layoutLeaves lists a tree's panes, left to right and top to bottom.
func layoutLeaves(n session.LayoutSnapshot) []uint64 {
	if len(n.Kids) == 0 {
		return []uint64{n.Pane}
	}
	var out []uint64
	for _, kid := range n.Kids {
		out = append(out, layoutLeaves(kid)...)
	}
	return out
}

// specFor is what a leaf should run: what the layout said, with the
// fallback's command when it names none. A leaf with a directory and no
// command is a shell there; taking the whole fallback for it, as this did,
// opened it wherever the server was.
func specFor(specs map[uint64]PaneSpec, leaf uint64, fallback PaneSpec) PaneSpec {
	spec, ok := specs[leaf]
	if !ok {
		return fallback
	}
	if len(spec.Command) == 0 {
		spec.Command = fallback.Command
		if spec.Dir == "" {
			spec.Dir = fallback.Dir
		}
	}
	return spec
}
