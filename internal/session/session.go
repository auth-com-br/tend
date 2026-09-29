// Package session holds the shape of a tend session: workspaces, tabs, panes
// and how they are arranged.
//
// Everything here is plain data. A pane's state is not its process: there is
// no PTY, no goroutine and no terminal in this package, which is what lets a
// workspace's whole behaviour be tested without starting anything. The server
// owns the runtime and pairs it with these records by id.
//
// A Session is not safe for concurrent use. Its owner serialises access.
package session

import (
	"errors"
	"fmt"
	"math"

	"github.com/auth-com-br/tend/internal/detect"
)

// Identifiers are unique for the lifetime of a session and are never reused,
// so a stale reference fails to resolve rather than silently addressing
// whatever took its place.
type (
	PaneID      uint64
	TabID       uint64
	WorkspaceID uint64
)

var (
	// ErrNoSuchPane and friends are returned rather than panicking: ids reach
	// this package from a socket API, where a bad one is input, not a bug.
	ErrNoSuchPane      = errors.New("session: no such pane")
	ErrNoSuchTab       = errors.New("session: no such tab")
	ErrNoSuchWorkspace = errors.New("session: no such workspace")
)

// Pane is one terminal's record.
type Pane struct {
	ID PaneID

	// Title is what the pane calls itself, usually from its terminal title.
	Title string
	// Named marks a title the user gave it, which the program's own title no
	// longer replaces: somebody who named a pane "api" meant it to stay "api"
	// when the shell inside it runs something else.
	Named bool
	// Command is what was started in it.
	Command []string
	// Dir is the working directory it started in.
	Dir string
	// CloseOnExit is a pane opened to run one thing — the files panel, an
	// editor on the scrollback — which goes when that thing ends. It is kept
	// here so a pane restarted after the server was keeps it too.
	CloseOnExit bool

	// Agent is the detection manifest id, empty when the pane is not running
	// a recognised agent.
	Agent string
	// State is the last detected agent state.
	State detect.State
	// AgentName is what a script calls the agent in this pane (herdr's agent
	// name): a handle that stays when two panes run the same agent and the
	// agent's own label no longer says which is meant.
	AgentName string
	// Unseen marks an agent that finished — went from working or blocked to
	// idle — while nobody was looking at its tab: herdr's "done", the one
	// thing a list of idle agents cannot otherwise tell apart. It clears when
	// the tab is looked at or the agent starts again.
	Unseen bool
	// StateSeq orders the panes by when their state last changed, for a list
	// that puts the most recent first.
	StateSeq uint64
}

// PaneSpec describes a pane to create.
type PaneSpec struct {
	Command []string
	Dir     string
	// Named marks a title the user gave rather than one a program reported.
	Named       bool
	Title       string
	Agent       string
	CloseOnExit bool
}

// Tab is one layout of panes.
type Tab struct {
	ID   TabID
	Name string

	root   *node
	active PaneID
	panes  map[PaneID]*Pane
}

// Workspace groups tabs.
type Workspace struct {
	ID   WorkspaceID
	Name string
	// Dir is what the workspace is about: the directory its panes open in,
	// and the one whose branch is shown beside its name. A workspace is how a
	// user says "this is the RVX backend", and a place is most of what that
	// means.
	Dir string
	// Group is what the workspace belongs with: a client of the same company,
	// a repository with several checkouts, anything the user keeps together.
	// Empty means it stands on its own.
	//
	// It is a session fact rather than a client one. Two people attached to
	// the same session should see the same grouping; which groups are folded
	// shut is the part that belongs to whoever is looking.
	Group string

	tabs   []*Tab
	active int
}

// Session is the whole tree.
type Session struct {
	workspaces []*Workspace
	active     int

	nextPane      uint64
	nextTab       uint64
	nextWorkspace uint64

	// companies are the user's groupings of workspaces (company.go).
	companies   []*Company
	nextCompany uint64

	// groups are the user's named sets of spaces (group.go).
	groups []*Group

	// stateSeq numbers state changes, for Pane.StateSeq.
	stateSeq uint64

	// index resolves a pane to its tab without walking. It is maintained by
	// every mutation and verified by CheckInvariants, so it cannot drift
	// unnoticed.
	index map[PaneID]*Tab
}

// New returns an empty session.
func New() *Session {
	return &Session{index: make(map[PaneID]*Tab)}
}

// --- reading ---------------------------------------------------------------

// Workspaces returns the workspaces in order.
func (s *Session) Workspaces() []*Workspace { return s.workspaces }

// ActiveWorkspace returns the focused workspace, or nil when there is none.
func (s *Session) ActiveWorkspace() *Workspace {
	if s.active < 0 || s.active >= len(s.workspaces) {
		return nil
	}
	return s.workspaces[s.active]
}

// Workspace looks a workspace up by id.
func (s *Session) Workspace(id WorkspaceID) (*Workspace, bool) {
	for _, w := range s.workspaces {
		if w.ID == id {
			return w, true
		}
	}
	return nil, false
}

// Tabs returns the workspace's tabs in order.
func (w *Workspace) Tabs() []*Tab { return w.tabs }

// ActiveTab returns the focused tab, or nil when the workspace has none.
func (w *Workspace) ActiveTab() *Tab {
	if w == nil || w.active < 0 || w.active >= len(w.tabs) {
		return nil
	}
	return w.tabs[w.active]
}

// Tab looks a tab up by id, anywhere in the session.
func (s *Session) Tab(id TabID) (*Tab, bool) {
	for _, w := range s.workspaces {
		for _, t := range w.tabs {
			if t.ID == id {
				return t, true
			}
		}
	}
	return nil, false
}

// ActiveTab returns the focused tab of the focused workspace.
func (s *Session) ActiveTab() *Tab { return s.ActiveWorkspace().ActiveTab() }

// Pane looks a pane up by id, anywhere in the session.
func (s *Session) Pane(id PaneID) (*Pane, bool) {
	t, ok := s.index[id]
	if !ok {
		return nil, false
	}
	p, ok := t.panes[id]
	return p, ok
}

// TabOf returns the tab holding a pane.
func (s *Session) TabOf(id PaneID) (*Tab, bool) {
	t, ok := s.index[id]
	return t, ok
}

// ActivePane returns the focused pane of the focused tab.
func (s *Session) ActivePane() *Pane {
	t := s.ActiveTab()
	if t == nil {
		return nil
	}
	return t.panes[t.active]
}

// PaneCount returns how many panes the session holds.
func (s *Session) PaneCount() int { return len(s.index) }

// Panes returns the tab's panes in layout order: left to right, top to bottom.
func (t *Tab) Panes() []PaneID { return t.root.panes(nil) }

// ActivePane returns the tab's focused pane id.
func (t *Tab) ActivePane() PaneID { return t.active }

// Pane looks a pane up within the tab.
func (t *Tab) Pane(id PaneID) (*Pane, bool) {
	p, ok := t.panes[id]
	return p, ok
}

// Layout computes where each pane goes inside area. It only reads: geometry
// never mutates the tree, so a client may compute a layout at any size without
// affecting what anyone else sees.
func (t *Tab) Layout(area Rect) []PaneRect {
	return t.root.layout(area, make([]PaneRect, 0, t.root.count()))
}

// --- workspaces and tabs ---------------------------------------------------

// AddWorkspace appends a workspace and focuses it.
func (s *Session) AddWorkspace(name string) *Workspace {
	return s.AddWorkspaceIn(name, "")
}

// AddWorkspaceIn appends a workspace rooted at a directory.
func (s *Session) AddWorkspaceIn(name, dir string) *Workspace {
	s.nextWorkspace++
	w := &Workspace{ID: WorkspaceID(s.nextWorkspace), Name: name, Dir: dir, active: -1}
	s.workspaces = append(s.workspaces, w)
	s.active = len(s.workspaces) - 1
	return w
}

// FocusWorkspace focuses a workspace by id.
func (s *Session) FocusWorkspace(id WorkspaceID) error {
	for i, w := range s.workspaces {
		if w.ID == id {
			s.active = i
			return nil
		}
	}
	return fmt.Errorf("%w: %d", ErrNoSuchWorkspace, id)
}

// AddTab creates a tab in a workspace, with one pane, and focuses both.
//
// A tab always holds at least one pane: an empty tab has nothing to show and
// nothing to close, and allowing one would put that special case in every
// caller.
func (s *Session) AddTab(ws WorkspaceID, name string, spec PaneSpec) (*Tab, *Pane, error) {
	w, ok := s.Workspace(ws)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %d", ErrNoSuchWorkspace, ws)
	}

	s.nextTab++
	t := &Tab{ID: TabID(s.nextTab), Name: name, panes: make(map[PaneID]*Pane)}

	p := s.newPane(spec)
	t.root = leaf(p.ID)
	t.active = p.ID
	t.panes[p.ID] = p
	s.index[p.ID] = t

	w.tabs = append(w.tabs, t)
	w.active = len(w.tabs) - 1
	return t, p, nil
}

// FocusTab focuses a tab, and the workspace that holds it.
func (s *Session) FocusTab(id TabID) error {
	for wi, w := range s.workspaces {
		for ti, t := range w.tabs {
			if t.ID == id {
				s.active = wi
				w.active = ti
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %d", ErrNoSuchTab, id)
}

// CloseTab removes a tab and every pane in it, returning the closed panes so
// the caller can stop their processes.
func (s *Session) CloseTab(id TabID) ([]PaneID, error) {
	for _, w := range s.workspaces {
		for ti, t := range w.tabs {
			if t.ID != id {
				continue
			}
			closed := t.Panes()
			for _, p := range closed {
				delete(s.index, p)
			}
			w.tabs = append(w.tabs[:ti], w.tabs[ti+1:]...)
			if w.active >= len(w.tabs) {
				w.active = len(w.tabs) - 1
			}
			return closed, nil
		}
	}
	return nil, fmt.Errorf("%w: %d", ErrNoSuchTab, id)
}

// CloseWorkspace removes a workspace and reports every pane that went with it.
//
// The last workspace can be closed like any other. A session with none is a
// real state — the client makes one when it finds nothing — and refusing here
// would mean the rule lived in two places and disagreed in one of them.
func (s *Session) CloseWorkspace(id WorkspaceID) ([]PaneID, error) {
	for wi, w := range s.workspaces {
		if w.ID != id {
			continue
		}
		var closed []PaneID
		for _, t := range w.tabs {
			for _, p := range t.Panes() {
				delete(s.index, p)
				closed = append(closed, p)
			}
		}
		s.workspaces = append(s.workspaces[:wi], s.workspaces[wi+1:]...)
		if s.active >= len(s.workspaces) {
			s.active = len(s.workspaces) - 1
		}
		s.forgetWorkspace(id)
		return closed, nil
	}
	return nil, fmt.Errorf("%w: %d", ErrNoSuchWorkspace, id)
}

// AllocPaneID hands out a pane identifier for a pane that is in no tab: a
// popup, which floats over the layout rather than taking a place in it. It
// comes from the same counter, so it never collides with a pane's.
func (s *Session) AllocPaneID() PaneID {
	s.nextPane++
	return PaneID(s.nextPane)
}

func (s *Session) newPane(spec PaneSpec) *Pane {
	s.nextPane++
	return &Pane{
		ID:      PaneID(s.nextPane),
		Title:   spec.Title,
		Named:   spec.Named,
		Command: spec.Command,
		Dir:     spec.Dir,
		Agent:   spec.Agent,
		State:   detect.StateUnknown,

		CloseOnExit: spec.CloseOnExit,
	}
}

// --- panes -----------------------------------------------------------------

// SplitPane divides target and focuses the new pane.
func (s *Session) SplitPane(target PaneID, dir Direction, spec PaneSpec) (*Pane, error) {
	t, ok := s.index[target]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrNoSuchPane, target)
	}

	p := s.newPane(spec)
	root, split := t.root.split(target, p.ID, dir, false)
	if !split {
		// The index said this tab owns the pane but the tree disagrees, which
		// means the two have drifted. Do not leave a pane allocated for a
		// split that did not happen.
		return nil, fmt.Errorf("%w: %d", ErrNoSuchPane, target)
	}

	t.root = root
	t.panes[p.ID] = p
	t.active = p.ID
	s.index[p.ID] = t
	return p, nil
}

// DockPane opens a pane along the left edge of a tab (the right, when
// right is set), the tab's full height, taking share of its width (clamped to between a tenth and a
// half). It is how a panel such as the file explorer sits beside everything
// in the tab rather than beside one pane of it, which is all SplitPane can
// do. The new pane takes the focus, as a split's does.
func (s *Session) DockPane(tab TabID, share float64, right bool, spec PaneSpec) (*Pane, error) {
	t, ok := s.Tab(tab)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrNoSuchTab, tab)
	}
	share = math.Min(math.Max(share, 0.1), 0.5)
	p := s.newPane(spec)
	t.root = t.root.dock(p.ID, share, right)
	t.panes[p.ID] = p
	t.active = p.ID
	s.index[p.ID] = t
	return p, nil
}

// ClosePane removes a pane.
//
// Closing the last pane of a tab closes the tab, and the last tab of a
// workspace leaves that workspace empty rather than removing it: a workspace
// is something the user named and arranged, so it outlives its contents until
// the user says otherwise.
func (s *Session) ClosePane(id PaneID) error {
	t, ok := s.index[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchPane, id)
	}

	root, closed := t.root.closePane(id)
	if !closed {
		return fmt.Errorf("%w: %d", ErrNoSuchPane, id)
	}

	t.root = root
	delete(t.panes, id)
	delete(s.index, id)

	if root == nil {
		_, err := s.CloseTab(t.ID)
		return err
	}
	if t.active == id {
		// Focus the first surviving pane in layout order, which is the one
		// nearest where the closed pane was.
		if remaining := t.Panes(); len(remaining) > 0 {
			t.active = remaining[0]
		}
	}
	return nil
}

// FocusPane focuses a pane, and the tab and workspace holding it.
func (s *Session) FocusPane(id PaneID) error {
	t, ok := s.index[id]
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchPane, id)
	}
	t.active = id
	return s.FocusTab(t.ID)
}

// MoveFocus moves focus to the pane beside the active one.
//
// area is the region the tab is drawn in, because adjacency is spatial: which
// pane is "to the left" depends on the geometry a client is rendering, not on
// the shape of the tree.
func (s *Session) MoveFocus(side Side, area Rect) bool {
	t := s.ActiveTab()
	if t == nil {
		return false
	}
	next, ok := Neighbor(t.Layout(area), t.active, side)
	if !ok {
		return false
	}
	t.active = next
	return true
}

// RenameTab changes a tab's label.
func (s *Session) RenameTab(id TabID, name string) error {
	t, ok := s.Tab(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchTab, id)
	}
	t.Name = name
	return nil
}

// RenamePane changes a pane's title.
//
// A pane's title is otherwise whatever its program set, which for a shell is
// the command it is running and for an agent is the agent. A name given here
// stays: it is the user's, and the program does not get to overwrite it.
func (s *Session) RenamePane(id PaneID, name string) error {
	p, ok := s.Pane(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchPane, id)
	}
	p.Title = name
	p.Named = name != ""
	return nil
}

// RenameWorkspace changes a workspace's label.
func (s *Session) RenameWorkspace(id WorkspaceID, name string) error {
	w, ok := s.Workspace(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchWorkspace, id)
	}
	w.Name = name
	return nil
}

// AdjustSplit moves one edge of a pane by a number of cells, taking the space
// from the neighbour across that edge.
//
// area is the region the tab is drawn in, because a divider is stored as a
// proportion and the caller is asking in cells. Nothing to adjust is not an
// error: a pane with no divider on that side is a key press that does nothing,
// which is what a user expects at the edge of the screen.
func (s *Session) AdjustSplit(pane PaneID, side Side, cells int, area Rect) error {
	t, ok := s.index[pane]
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchPane, pane)
	}

	total := area.W
	if side == Up || side == Down {
		total = area.H
	}
	if total <= 0 || cells == 0 {
		return nil
	}
	t.root.adjust(pane, side, float64(cells)/float64(total))
	return nil
}

// SetPaneState records a detection result against a pane.
func (s *Session) SetPaneState(id PaneID, agent string, state detect.State) error {
	return s.SetPaneStateWatched(id, agent, state, 0)
}

// SetPaneStateWatched records a pane's agent state, knowing which tab is being
// looked at, by herdr's rule (`apply_pane_state_change`): anything but idle
// is seen, and finishing — working or blocked to idle — is unseen unless it
// happened in the tab in view.
func (s *Session) SetPaneStateWatched(id PaneID, agent string, state detect.State, watched TabID) error {
	p, ok := s.Pane(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchPane, id)
	}
	previous := p.State
	p.Agent = agent
	p.State = state
	switch {
	case state != detect.StateIdle:
		p.Unseen = false
	case previous == detect.StateWorking || previous == detect.StateBlocked:
		tab := s.index[id]
		p.Unseen = tab == nil || tab.ID != watched
	}
	if previous != state {
		s.stateSeq++
		p.StateSeq = s.stateSeq
	}
	return nil
}

// MarkTabSeen marks every agent in a tab seen, as looking at the tab does,
// and reports whether any was not.
func (s *Session) MarkTabSeen(id TabID) bool {
	changed := false
	for _, w := range s.workspaces {
		for _, t := range w.tabs {
			if t.ID != id {
				continue
			}
			for _, p := range t.panes {
				if p.Unseen {
					p.Unseen = false
					changed = true
				}
			}
		}
	}
	return changed
}

// --- invariants ------------------------------------------------------------

// CheckInvariants verifies everything this package promises about its own
// shape. Tests call it after every mutation; a caller may call it anywhere.
//
// It exists because the failures this structure invites are quiet ones: an
// index that no longer matches the tree, a focus pointing at a pane that was
// closed, split shares that no longer sum to one. Each of those renders
// wrongly rather than crashing, which makes them expensive to find later.
func (s *Session) CheckInvariants() error {
	if len(s.workspaces) == 0 {
		if s.active != 0 && s.active != -1 {
			return fmt.Errorf("empty session focuses workspace %d", s.active)
		}
	} else if s.active < 0 || s.active >= len(s.workspaces) {
		return fmt.Errorf("active workspace %d out of range (%d workspaces)", s.active, len(s.workspaces))
	}

	seenPanes := make(map[PaneID]TabID)
	seenTabs := make(map[TabID]bool)
	seenWorkspaces := make(map[WorkspaceID]bool)

	for _, w := range s.workspaces {
		if seenWorkspaces[w.ID] {
			return fmt.Errorf("duplicate workspace id %d", w.ID)
		}
		seenWorkspaces[w.ID] = true

		if len(w.tabs) == 0 {
			if w.active != -1 {
				return fmt.Errorf("workspace %d has no tabs but focuses %d", w.ID, w.active)
			}
		} else if w.active < 0 || w.active >= len(w.tabs) {
			return fmt.Errorf("workspace %d focuses tab %d of %d", w.ID, w.active, len(w.tabs))
		}

		for _, t := range w.tabs {
			if seenTabs[t.ID] {
				return fmt.Errorf("duplicate tab id %d", t.ID)
			}
			seenTabs[t.ID] = true

			if t.root == nil {
				return fmt.Errorf("tab %d has no panes", t.ID)
			}
			if err := checkNode(t.root); err != nil {
				return fmt.Errorf("tab %d: %w", t.ID, err)
			}

			inTree := t.root.panes(nil)
			if len(inTree) != len(t.panes) {
				return fmt.Errorf("tab %d has %d panes in its tree and %d in its map",
					t.ID, len(inTree), len(t.panes))
			}
			for _, id := range inTree {
				if _, ok := t.panes[id]; !ok {
					return fmt.Errorf("tab %d: pane %d is in the tree but not the map", t.ID, id)
				}
				if prev, dup := seenPanes[id]; dup {
					return fmt.Errorf("pane %d is in tabs %d and %d", id, prev, t.ID)
				}
				seenPanes[id] = t.ID
			}
			if _, ok := t.panes[t.active]; !ok {
				return fmt.Errorf("tab %d focuses pane %d, which it does not hold", t.ID, t.active)
			}
		}
	}

	if len(s.index) != len(seenPanes) {
		return fmt.Errorf("index holds %d panes, the tree holds %d", len(s.index), len(seenPanes))
	}
	for id, tab := range s.index {
		want, ok := seenPanes[id]
		if !ok {
			return fmt.Errorf("index holds pane %d, which is in no tab", id)
		}
		if tab.ID != want {
			return fmt.Errorf("index maps pane %d to tab %d, the tree says %d", id, tab.ID, want)
		}
	}
	if err := s.checkCompanies(); err != nil {
		return err
	}
	return s.checkGroups()
}

func checkNode(n *node) error {
	if n.isLeaf() {
		if n.pane == 0 {
			return errors.New("leaf holds no pane")
		}
		return nil
	}
	if len(n.kids) < 2 {
		return fmt.Errorf("split has %d children, want at least 2", len(n.kids))
	}
	if len(n.sizes) != len(n.kids) {
		return fmt.Errorf("split has %d children and %d sizes", len(n.kids), len(n.sizes))
	}
	sum := 0.0
	for i, size := range n.sizes {
		if size <= 0 {
			return fmt.Errorf("child %d has size %g", i, size)
		}
		sum += size
	}
	if math.Abs(sum-1) > 1e-9 {
		return fmt.Errorf("split sizes sum to %g, want 1", sum)
	}
	for _, kid := range n.kids {
		if err := checkNode(kid); err != nil {
			return err
		}
	}
	return nil
}
