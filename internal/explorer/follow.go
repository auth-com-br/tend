package explorer

import (
	"sort"
)

// The panel follows the pane beside it: when that pane's program moves to
// another project, the panel shows that project. The rule is herdr-sidebar's
// (`launch.rs`, CwdFollower), which reads the live directory of the other
// panes in the tab — never the one a pane was opened in, which is stale the
// moment its shell cd's — and picks, in order, the focused one, the one it
// was already following, and the one with the lowest id, so the choice does
// not flicker with the order panes are listed in.

// Sibling is another pane of the panel's tab and where its program is.
type Sibling struct {
	Pane    string
	Cwd     string
	Focused bool
}

// Neighbours lists the panel's siblings, as the session says.
type Neighbours interface {
	Siblings() ([]Sibling, error)
}

// follower remembers what it saw last time, to tell a move from a focus
// change.
type follower struct {
	seen     map[string]string
	selected string
	started  bool
	// manual is a folder the user chose, which holds until a pane already
	// seen moves; a new pane or a change of focus does not override it.
	manual bool
}

// next is the directory to follow now, or "" when nothing changed.
func (f *follower) next(siblings []Sibling) string {
	var live []Sibling
	for _, s := range siblings {
		if s.Cwd != "" {
			live = append(live, s)
		}
	}
	sort.Slice(live, func(a, b int) bool { return live[a].Pane < live[b].Pane })
	nextSeen := make(map[string]string, len(live))
	var changed []Sibling
	for _, s := range live {
		nextSeen[s.Pane] = s.Cwd
		if old, ok := f.seen[s.Pane]; ok && old != s.Cwd {
			changed = append(changed, s)
		}
	}
	wasStarted := f.started
	f.started = true

	if f.manual {
		f.seen = nextSeen
		if !wasStarted || len(changed) == 0 {
			return ""
		}
		picked, ok := pickSibling(changed, f.selected)
		if !ok {
			return ""
		}
		f.selected, f.manual = picked.Pane, false
		return picked.Cwd
	}

	priorSelected := f.selected
	priorCwd := f.seen[priorSelected]
	picked, ok := pickSibling(live, f.selected)
	f.seen = nextSeen
	if !ok {
		return ""
	}
	f.selected = picked.Pane
	if priorSelected != picked.Pane || priorCwd != picked.Cwd {
		return picked.Cwd
	}
	return ""
}

func pickSibling(siblings []Sibling, selected string) (Sibling, bool) {
	for _, s := range siblings {
		if s.Focused {
			return s, true
		}
	}
	for _, s := range siblings {
		if s.Pane == selected {
			return s, true
		}
	}
	if len(siblings) > 0 {
		return siblings[0], true
	}
	return Sibling{}, false
}

// Follow asks the neighbours where they are and moves the panel to the
// project the one it follows is in. It runs on the refresh timer.
func (m *Model) Follow() {
	if m.neighbours == nil {
		return
	}
	siblings, err := m.neighbours.Siblings()
	if err != nil {
		return // the session not answering is no reason to say anything
	}
	if dir := m.follow.nextFor(siblings, m.tree.Root); dir != "" {
		m.Reroot(dir)
	}
}

// nextFor is next for a panel showing root: the first time, the sibling
// whose project root is counts as the one already followed.
func (f *follower) nextFor(siblings []Sibling, root string) string {
	if !f.started {
		// The panel opened where the pane it was opened from is, and has
		// the focus itself now; with no sibling focused the rule would fall
		// to the lowest id, which may be another project. The one already
		// shown is the one being followed.
		for _, s := range siblings {
			if s.Cwd == "" {
				continue
			}
			at := s.Cwd
			if g := FindRepo(s.Cwd); g.Top != "" {
				at = g.Top
			}
			if at == root {
				f.selected = s.Pane
				break
			}
		}
	}
	return f.next(siblings)
}

// Reroot shows another directory: the repository it is in, or itself.
// Staying in the same repository changes nothing, since the panel shows the
// whole of it wherever in it the pane is.
func (m *Model) Reroot(dir string) {
	root, repos := reposFor(dir)
	if root == m.tree.Root {
		return
	}
	hidden := m.tree.Hidden
	m.tree = NewTree(root)
	m.tree.Hidden, m.tree.repos = hidden, repos
	m.repos, m.repoless = nil, len(repos) == 0
	for _, g := range repos {
		m.repos = append(m.repos, repoState{git: g})
	}
	m.activate(-1)
	m.cursor, m.scroll = [3]int{}, [3]int{}
	m.csearch.results, m.csearch.rows = nil, nil
	m.searchChanged()
	m.Refresh()
	m.say("following "+root, false)
}
