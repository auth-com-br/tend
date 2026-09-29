package session

import (
	"errors"
	"fmt"

	"github.com/auth-com-br/tend/internal/detect"
)

// A snapshot is the session as it can be written down: which spaces, tabs and
// panes exist, how each tab is divided, and what each pane was running and
// where. It is what lets a server that has been stopped come back to the same
// arrangement instead of an empty screen.
//
// It does not hold processes. Nothing written to a file can: the shells and
// agents die with the server, and a restored pane is a new shell started in
// the directory the old one was in. What carries over is the place, not the
// program — which is how herdr does it, and the honest limit of a file.

// SnapshotVersion is the format this build writes. A file from a newer build
// is refused rather than half-read: restoring part of a layout is worse than
// restoring none, because it looks like it worked.
const SnapshotVersion = 1

// ErrSnapshotVersion means the file was written by a build this one does not
// understand.
var ErrSnapshotVersion = errors.New("session: snapshot version not understood")

// Snapshot is the whole session.
type Snapshot struct {
	Version    int                 `json:"version"`
	Active     int                 `json:"active"`
	Workspaces []WorkspaceSnapshot `json:"workspaces"`

	// The counters are kept so identifiers handed out after a restore do not
	// collide with ones a client may still be holding.
	NextPane      uint64 `json:"next_pane"`
	NextTab       uint64 `json:"next_tab"`
	NextWorkspace uint64 `json:"next_workspace"`

	// Companies are the user's groupings of workspaces (company.go). They
	// are left out while there are none, so a file from before them and a
	// session that never used them read the same.
	Companies   []CompanySnapshot `json:"companies,omitempty"`
	NextCompany uint64            `json:"next_company,omitempty"`

	// Groups are the user's named sets of spaces (group.go), empty ones
	// included. A file from before groups had records has none, and gets
	// them from the names its workspaces carry.
	Groups []GroupSnapshot `json:"groups,omitempty"`
}

// GroupSnapshot is one group and the companies it has had spaces of.
type GroupSnapshot struct {
	Name      string   `json:"name"`
	Companies []uint64 `json:"companies,omitempty"`
	Dir       string   `json:"dir,omitempty"`
}

// CompanySnapshot is one company and its workspaces, in its order.
type CompanySnapshot struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name"`
	Workspaces []uint64 `json:"workspaces,omitempty"`
}

// WorkspaceSnapshot is one space.
type WorkspaceSnapshot struct {
	ID     uint64        `json:"id"`
	Name   string        `json:"name,omitempty"`
	Dir    string        `json:"dir,omitempty"`
	Group  string        `json:"group,omitempty"`
	Active int           `json:"active"`
	Tabs   []TabSnapshot `json:"tabs"`
}

// TabSnapshot is one tab and how it is divided.
type TabSnapshot struct {
	ID     uint64         `json:"id"`
	Name   string         `json:"name,omitempty"`
	Active uint64         `json:"active"`
	Layout LayoutSnapshot `json:"layout"`
	Panes  []PaneSnapshot `json:"panes"`
}

// PaneSnapshot is what is needed to start a pane again.
type PaneSnapshot struct {
	ID    uint64 `json:"id"`
	Title string `json:"title,omitempty"`
	// Named marks a title the user gave, which is kept: a pane called "api"
	// is called "api" after a restart, not whatever its new shell reports.
	Named   bool     `json:"named,omitempty"`
	Command []string `json:"command,omitempty"`
	// Dir is where the pane was when the snapshot was taken, which is not
	// where it started: somebody who has spent an hour three directories down
	// wants to come back there, not to where the shell was opened.
	Dir   string `json:"dir,omitempty"`
	Agent string `json:"agent,omitempty"`
	// AgentName is the name a script gave the agent, which it is addressed
	// by (`tend agent prompt reviewer …`) across a restart as before one.
	AgentName string `json:"agent_name,omitempty"`
	// Session names the agent's own conversation, when a hook told tend
	// which one it was in. It is what lets a restored pane carry on rather
	// than start over; see internal/agent's Resume.
	Session *AgentSession `json:"agent_session,omitempty"`
	// CloseOnExit is a pane that goes with its program, which it still does
	// after a restart.
	CloseOnExit bool `json:"close_on_exit,omitempty"`
}

// AgentSession is a conversation as the agent names it. It is written down
// here rather than in internal/agent because the snapshot is what survives a
// restart, and this package is what a snapshot is made of.
type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	ID     string `json:"id,omitempty"`
	Path   string `json:"path,omitempty"`
}

// LayoutSnapshot is the split tree. A leaf names a pane; anything else has
// children and the share of the space each one takes.
type LayoutSnapshot struct {
	Pane  uint64           `json:"pane,omitempty"`
	Dir   string           `json:"dir,omitempty"`
	Sizes []float64        `json:"sizes,omitempty"`
	Kids  []LayoutSnapshot `json:"kids,omitempty"`
}

// Snapshot writes the session down.
//
// dirs gives each pane's current directory, for the panes whose process could
// be asked. A pane missing from it keeps the directory it started in.
func (s *Session) Snapshot(dirs map[PaneID]string) Snapshot {
	return s.SnapshotWith(dirs, nil)
}

// SnapshotWith also records each pane's agent conversation, for the panes
// where one is known.
func (s *Session) SnapshotWith(dirs map[PaneID]string, sessions map[PaneID]AgentSession) Snapshot {
	snap := Snapshot{
		Version:       SnapshotVersion,
		Active:        s.active,
		NextPane:      s.nextPane,
		NextTab:       s.nextTab,
		NextWorkspace: s.nextWorkspace,
		NextCompany:   s.nextCompany,
	}
	for _, c := range s.companies {
		cs := CompanySnapshot{ID: uint64(c.ID), Name: c.Name}
		for _, w := range c.Workspaces {
			cs.Workspaces = append(cs.Workspaces, uint64(w))
		}
		snap.Companies = append(snap.Companies, cs)
	}
	for _, g := range s.groups {
		gs := GroupSnapshot{Name: g.Name, Dir: g.Dir}
		for _, c := range g.Companies {
			gs.Companies = append(gs.Companies, uint64(c))
		}
		snap.Groups = append(snap.Groups, gs)
	}
	for _, w := range s.workspaces {
		ws := WorkspaceSnapshot{
			ID: uint64(w.ID), Name: w.Name, Dir: w.Dir, Group: w.Group, Active: w.active,
		}
		for _, t := range w.tabs {
			ts := TabSnapshot{
				ID: uint64(t.ID), Name: t.Name, Active: uint64(t.active),
				Layout: snapshotNode(t.root),
			}
			// In layout order, so the file reads the way the screen does and
			// two snapshots of the same session compare equal.
			for _, id := range t.Panes() {
				p := t.panes[id]
				if p == nil {
					continue
				}
				dir := p.Dir
				if now := dirs[id]; now != "" {
					dir = now
				}
				pane := PaneSnapshot{
					ID: uint64(p.ID), Title: p.Title, Named: p.Named,
					Command: p.Command, Dir: dir, Agent: p.Agent, AgentName: p.AgentName,
					CloseOnExit: p.CloseOnExit,
				}
				if conversation, ok := sessions[id]; ok {
					pane.Session = &conversation
				}
				ts.Panes = append(ts.Panes, pane)
			}
			ws.Tabs = append(ws.Tabs, ts)
		}
		snap.Workspaces = append(snap.Workspaces, ws)
	}
	return snap
}

// TabLayout is a tab's split tree, in the same shape the state file uses.
func (s *Session) TabLayout(id TabID) (LayoutSnapshot, bool) {
	t, ok := s.Tab(id)
	if !ok {
		return LayoutSnapshot{}, false
	}
	return snapshotNode(t.root), true
}

// SetLayoutSizes copies the shares of a layout onto a tab that already has the
// same shape, which is how a layout applied by rebuilding it gets the
// proportions it was exported with.
func (s *Session) SetLayoutSizes(id TabID, spec LayoutSnapshot) error {
	t, ok := s.Tab(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchTab, id)
	}
	return copySizes(t.root, spec)
}

func copySizes(n *node, spec LayoutSnapshot) error {
	if n == nil {
		return nil
	}
	if len(n.kids) == 0 || len(spec.Kids) == 0 {
		return nil
	}
	if len(n.kids) != len(spec.Kids) {
		return fmt.Errorf("session: the layout has %d children where the tab has %d",
			len(spec.Kids), len(n.kids))
	}
	if len(spec.Sizes) == len(n.kids) {
		n.sizes = append([]float64(nil), spec.Sizes...)
	}
	for i, kid := range n.kids {
		if err := copySizes(kid, spec.Kids[i]); err != nil {
			return err
		}
	}
	return nil
}

func snapshotNode(n *node) LayoutSnapshot {
	if n == nil {
		return LayoutSnapshot{}
	}
	if len(n.kids) == 0 {
		return LayoutSnapshot{Pane: uint64(n.pane)}
	}
	out := LayoutSnapshot{Dir: directionName(n.dir), Sizes: append([]float64(nil), n.sizes...)}
	for _, kid := range n.kids {
		out.Kids = append(out.Kids, snapshotNode(kid))
	}
	return out
}

func directionName(d Direction) string {
	if d == Rows {
		return "rows"
	}
	return "columns"
}

// Restore builds a session from a snapshot.
//
// The result is checked before it is returned. A file is outside input: it may
// have been edited, truncated by a full disk, or written by a build with a bug
// this one does not share, and a session that violates its own invariants
// fails later, somewhere unrelated, in a way that points nowhere near the file.
func Restore(snap Snapshot) (*Session, error) {
	if snap.Version != SnapshotVersion {
		return nil, fmt.Errorf("%w: file is version %d, this build reads %d",
			ErrSnapshotVersion, snap.Version, SnapshotVersion)
	}

	s := New()
	s.nextPane, s.nextTab, s.nextWorkspace = snap.NextPane, snap.NextTab, snap.NextWorkspace

	for _, ws := range snap.Workspaces {
		w := &Workspace{
			ID: WorkspaceID(ws.ID), Name: ws.Name, Dir: ws.Dir, Group: ws.Group, active: -1,
		}
		for _, ts := range ws.Tabs {
			t := &Tab{ID: TabID(ts.ID), Name: ts.Name, panes: make(map[PaneID]*Pane)}
			for _, ps := range ts.Panes {
				id := PaneID(ps.ID)
				if _, dup := s.index[id]; dup || id == 0 {
					return nil, fmt.Errorf("session: snapshot names pane %d twice, or not at all", ps.ID)
				}
				t.panes[id] = &Pane{
					ID: id, Title: ps.Title, Named: ps.Named,
					Command: ps.Command, Dir: ps.Dir, Agent: ps.Agent, AgentName: ps.AgentName,
					State: detect.StateUnknown, CloseOnExit: ps.CloseOnExit,
				}
				s.index[id] = t
			}

			root, err := restoreNode(ts.Layout, t.panes)
			if err != nil {
				return nil, fmt.Errorf("session: tab %d: %w", ts.ID, err)
			}
			t.root = root
			t.active = PaneID(ts.Active)
			if _, ok := t.panes[t.active]; !ok {
				// The focused pane is a convenience, not a fact worth failing
				// over. Anything in the tab will do.
				if all := t.Panes(); len(all) > 0 {
					t.active = all[0]
				}
			}
			if len(t.panes) == 0 {
				continue // a tab with nothing in it is not a tab
			}
			w.tabs = append(w.tabs, t)
		}
		if len(w.tabs) > 0 {
			w.active = min(max(ws.Active, 0), len(w.tabs)-1)
		}
		s.workspaces = append(s.workspaces, w)
	}

	switch {
	case len(s.workspaces) == 0:
		s.active = 0
	default:
		s.active = min(max(snap.Active, 0), len(s.workspaces)-1)
	}
	s.raiseCounters()

	s.nextCompany = snap.NextCompany
	ids := make(map[uint64]bool, len(snap.Companies))
	for _, cs := range snap.Companies {
		if cs.ID == 0 || ids[cs.ID] {
			continue // no way to tell which of two is meant; keep the first
		}
		ids[cs.ID] = true
		c := &Company{ID: CompanyID(cs.ID), Name: cs.Name}
		for _, w := range cs.Workspaces {
			c.Workspaces = append(c.Workspaces, WorkspaceID(w))
		}
		s.companies = append(s.companies, c)
	}
	s.sanitizeCompanies()

	for _, gs := range snap.Groups {
		g := &Group{Name: gs.Name, Dir: gs.Dir}
		for _, c := range gs.Companies {
			g.Companies = append(g.Companies, CompanyID(c))
		}
		s.groups = append(s.groups, g)
	}
	s.sanitizeGroups()

	if err := s.CheckInvariants(); err != nil {
		return nil, fmt.Errorf("session: snapshot does not describe a valid session: %w", err)
	}
	return s, nil
}

// restoreNode rebuilds a split tree, refusing one that names a pane the tab
// does not have or gives its children shares that do not match them.
func restoreNode(l LayoutSnapshot, panes map[PaneID]*Pane) (*node, error) {
	if len(l.Kids) == 0 {
		id := PaneID(l.Pane)
		if _, ok := panes[id]; !ok {
			return nil, fmt.Errorf("layout names pane %d, which the tab does not have", l.Pane)
		}
		return leaf(id), nil
	}
	if len(l.Sizes) != len(l.Kids) {
		return nil, fmt.Errorf("layout has %d children and %d sizes", len(l.Kids), len(l.Sizes))
	}

	n := &node{dir: Columns, sizes: append([]float64(nil), l.Sizes...)}
	if l.Dir == "rows" {
		n.dir = Rows
	}
	for _, kid := range l.Kids {
		child, err := restoreNode(kid, panes)
		if err != nil {
			return nil, err
		}
		n.kids = append(n.kids, child)
	}
	return n, nil
}

// raiseCounters makes sure no identifier handed out from here on repeats one
// already in the session, whatever the file said the counters were.
func (s *Session) raiseCounters() {
	for _, w := range s.workspaces {
		s.nextWorkspace = max(s.nextWorkspace, uint64(w.ID))
		for _, t := range w.tabs {
			s.nextTab = max(s.nextTab, uint64(t.ID))
			for id := range t.panes {
				s.nextPane = max(s.nextPane, uint64(id))
			}
		}
	}
}
