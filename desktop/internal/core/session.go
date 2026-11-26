package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// Session is the desktop's connection to one tend session and what this
// window shows of it: which space, which tab, which pane has the keyboard,
// and the tab's panes laid out as the server lays them out.
//
// What the window looks at is its own and never sent, as in the TUI
// (AGENTS.md, "Shared fact or client state?"): two windows on one session
// can show different spaces. The server is told only where the focus is
// (pane.focus), which it needs for "done" and unread.
//
// It is the client.Handler, so PaneOutput and Event run on the connection's
// reader goroutine. They do nothing slow there: a call to the server from
// inside its own reply handler deadlocks, as the TUI learned (tui.go,
// Event), so anything that needs a round trip goes to a goroutine.
type Session struct {
	mu     sync.Mutex
	client *client.Client
	name   string
	// tendPath is the tend binary the window starts servers with, and the
	// commands it opens in a tab (tend whatsapp link).
	tendPath string
	cfg      config.Config
	ui       config.UI
	notes    *notifier
	done     chan struct{}
	// serverExited is closed when a server this window started has exited;
	// nil when the window found one running.
	serverExited <-chan struct{}
	// notifyHook stands in for the system's notifications in tests.
	notifyHook func(title, body string, clicked func()) error

	snap                  proto.SessionSnapshot
	workspace, tab, focus uint64
	// The viewer's own: groups folded, agents grouped under tabs, the
	// company chosen.
	folded  map[string]bool
	grouped bool
	company uint64

	// areaCols and areaRows are the panes' area in cells, which the tab is
	// laid out in; rects is that layout.
	areaCols, areaRows int
	rects              []proto.PaneRect
	panes              map[uint64]*paneView
	colours            Colours
	// theme is the window's colours, and light whether the desktop is
	// light, which a theme with auto_switch follows.
	theme      Theme
	light      bool
	themeReady bool

	// refreshMu makes refreshes one at a time, and a change of view (the
	// tab, the space, the focus) part of the refresh that shows it.
	// Coalescing them instead — a refresh asked for during another
	// returning at once — lost changes: "+" opened a tab, the refresh
	// already running read a snapshot from before it, found no such tab
	// and put the view back on the first, and the caller's refresh had
	// returned without running. The race detector's slowness showed it.
	refreshMu sync.Mutex
	// eventRefresh is a refresh for events already on its way, so a burst
	// of them is one snapshot.
	eventRefresh atomic.Bool

	// The window's own, never sent: the menu open and the prompt one of its
	// items asked for, the panel the global menu opened, the pane zoomed
	// to fill the tab, and what was last copied from each pane (a menu's
	// "copy" copies it again).
	dock dock
	// pinned is the one tab a window of its own shows (tend-desktop -tab):
	// the view stays on it, and the window shows nothing else.
	pinned   uint64
	files    filesTab
	wa       waTab
	spend    spending
	docs     docs
	menu     *ui.Menu
	prompt   *Prompt
	panel    string
	zoom     uint64
	lastCopy map[uint64]string

	// catalog is the agents the server's machine has, for the new-tab menu;
	// read once, after the window opens, since it asks each agent for its
	// version and takes a moment.
	catalog []proto.AgentStatus

	// PaneChanged is called, without the lock, when a pane has something
	// new to paint. StateChanged when what the window lists or lays out
	// changed: the sidebar, the tabs, the panes shown. Status with a line
	// for the user. None of them may block: they can run on the
	// connection's reader. The Qt side posts each to the main thread.
	PaneChanged  func(pane uint64)
	StateChanged func()
	Status       func(string)
	// Copied is text for the clipboard: a selection made in a pane, or
	// what a pane's program copied (OSC 52).
	Copied func(string)
	// ClipboardText reads the clipboard, which only the Qt side can.
	ClipboardText func() string
	// Raise brings the window to the front: a click on a notification.
	Raise func()
	// OpenTabWindow puts a tab in a window of its own, which only the
	// program can do (it starts itself again).
	OpenTabWindow func(tab uint64) error
}

// paneView is one pane the window shows: its terminal, kept at the size the
// window draws it at.
type paneView struct {
	screen     *vt.Screen
	cols, rows int
	// scroll is the pane looked at offset lines back, from the server's
	// history (pane.screen), nil while it shows live; history is how far
	// back there is to go.
	scroll          *vt.Screen
	offset, history int
	sel             selection
}

// Open connects to the named session, starting it with tendPath if it is not
// running, and settles on what to show.
func Open(name, tendPath string, cfg config.Config) (*Session, error) {
	s := &Session{
		name:     name,
		tendPath: tendPath,
		cfg:      cfg,
		ui:       cfg.UI,
		grouped:  cfg.UI.Grouped,
		notes:    newNotifier(cfg),
		done:     make(chan struct{}),
		colours:  DefaultColours,
		folded:   map[string]bool{},
		panes:    map[uint64]*paneView{},
		lastCopy: map[uint64]string{},
	}
	s.applyThemeLocked()
	c, started, err := OpenSession(name, tendPath, s)
	if err != nil {
		return nil, err
	}
	s.serverExited = started
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	if err := s.refresh(); err != nil {
		_ = c.Close()
		return nil, err
	}
	go s.runNotices(s.done)
	s.startOnboarding()
	s.mu.Lock()
	empty := len(s.snap.Panes) == 0
	s.mu.Unlock()
	if empty {
		// A session with no pane at all: open one, so the window has a
		// terminal to show. Only then, as the TUI's ensureSession: a space
		// shown with no tab in it is a real state, not a reason to make
		// another space — the first version did, and a restored session
		// opened with an extra empty space beside it.
		if err := s.ensurePane(); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return s, nil
}

// Close ends the connection. The server, and every pane, stay.
func (s *Session) Close() error {
	s.mu.Lock()
	c := s.client
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// Name is the session's name.
func (s *Session) Name() string { return s.name }

// Focused is the pane with the keyboard.
func (s *Session) Focused() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.focus
}

func (s *Session) ensurePane() error {
	s.mu.Lock()
	c, snap := s.client, s.snap
	s.mu.Unlock()
	// As the TUI's ensureSession: in the active space, or a new "main".
	ws := snap.ActiveWorkspace
	if ws == 0 {
		var err error
		if ws, err = c.NewWorkspace("main"); err != nil {
			return fmt.Errorf("opening a space: %w", err)
		}
	}
	if _, _, err := c.NewTab(ws, "tab 1", proto.PaneSpec{}); err != nil {
		return fmt.Errorf("opening a tab: %w", err)
	}
	return s.refresh()
}

// FocusedPane is the pane a session has in focus: the active space's active
// tab's active pane, falling back to the first of each, which is where the
// TUI lands too when the active one no longer resolves (resolveViewLocked).
func FocusedPane(snap proto.SessionSnapshot) uint64 {
	ws, ok := workspaceIn(snap, snap.ActiveWorkspace)
	if !ok {
		return 0
	}
	tab, ok := tabIn(ws, ws.ActiveTab)
	if !ok {
		return 0
	}
	return activePane(tab)
}

// workspaceIn is the space with that id, or the first, or none.
func workspaceIn(snap proto.SessionSnapshot, id uint64) (proto.WorkspaceInfo, bool) {
	if len(snap.Workspaces) == 0 {
		return proto.WorkspaceInfo{}, false
	}
	for _, w := range snap.Workspaces {
		if w.ID == id {
			return w, true
		}
	}
	return snap.Workspaces[0], true
}

// tabIn is the tab with that id in a space, or its first, or none.
func tabIn(ws proto.WorkspaceInfo, id uint64) (proto.TabInfo, bool) {
	if len(ws.Tabs) == 0 {
		return proto.TabInfo{}, false
	}
	for _, t := range ws.Tabs {
		if t.ID == id {
			return t, true
		}
	}
	return ws.Tabs[0], true
}

func activePane(tab proto.TabInfo) uint64 {
	if tab.ActivePane != 0 {
		return tab.ActivePane
	}
	if len(tab.Panes) > 0 {
		return tab.Panes[0]
	}
	return 0
}

func hasPane(tab proto.TabInfo, pane uint64) bool {
	for _, p := range tab.Panes {
		if p == pane {
			return true
		}
	}
	return false
}

// refresh reads the session and settles the view on something that exists:
// the space, tab and pane shown, if they still do, else what the session
// has active. Then the tab is laid out and its panes subscribed to.
func (s *Session) refresh() error { return s.refreshWith(nil) }

// refreshWith changes the view, then refreshes, as one step no other
// refresh can come between: the change is made against the snapshot the
// refresh then reads, never undone by one read before it.
func (s *Session) refreshWith(change func()) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if change != nil {
		s.mu.Lock()
		change()
		s.mu.Unlock()
	}
	return s.refreshOnce()
}

func (s *Session) refreshOnce() error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.snap = snap
	prevFocus := s.focus
	if s.company != 0 {
		// A company deleted, here or by another client, stops being chosen.
		found := false
		for _, co := range snap.Companies {
			found = found || co.ID == s.company
		}
		if !found {
			s.company = 0
		}
	}
	if s.pinned != 0 {
		for _, w := range snap.Workspaces {
			for _, t := range w.Tabs {
				if t.ID == s.pinned {
					s.workspace, s.tab = w.ID, t.ID
				}
			}
		}
	}
	if s.workspace == 0 {
		s.workspace = snap.ActiveWorkspace
	}
	ws, ok := workspaceIn(snap, s.workspace)
	if !ok {
		s.workspace, s.tab, s.focus = 0, 0, 0
	} else {
		s.workspace = ws.ID
		if s.tab == 0 {
			s.tab = ws.ActiveTab
		}
		tab, ok := tabIn(ws, s.tab)
		if !ok {
			// An empty space is a real state — its last tab was closed — and
			// is shown empty rather than skipped past, as the TUI does.
			s.tab, s.focus = 0, 0
		} else {
			s.tab = tab.ID
			if !hasPane(tab, s.focus) {
				s.focus = activePane(tab)
			}
		}
	}
	tab, cols, rows := s.tab, s.areaCols, s.areaRows
	focus := s.focus
	s.mu.Unlock()

	if focus != prevFocus && focus != 0 {
		_ = c.FocusPane(focus, prevFocus)
	}
	if err := s.layout(tab, cols, rows); err != nil {
		return err
	}
	s.stateChanged()
	return nil
}

// layout asks the server where the tab's panes go in the area, subscribes to
// them, and drops what it held for panes no longer shown.
func (s *Session) layout(tab uint64, cols, rows int) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	var rects []proto.PaneRect
	if tab != 0 && cols > 0 && rows > 0 {
		res, err := c.TabLayout(tab, cols, rows)
		if err != nil {
			return err
		}
		rects = res.Panes
	}
	s.mu.Lock()
	if s.tab != tab {
		// The view moved while the server was asked; the refresh that moved
		// it lays out the new tab.
		s.mu.Unlock()
		return nil
	}
	// A zoomed pane fills the area alone, as the TUI's zoom: the others run
	// on, unseen, and come back when it is unzoomed or leaves the tab.
	if s.zoom != 0 {
		zoomed := false
		for _, r := range rects {
			zoomed = zoomed || r.Pane == s.zoom
		}
		if zoomed {
			rects = []proto.PaneRect{{Pane: s.zoom, Cols: cols, Rows: rows}}
		} else {
			s.zoom = 0
		}
	}
	s.rects = rects
	shown := make(map[uint64]bool, len(rects))
	ids := make([]uint64, 0, len(rects))
	for _, r := range rects {
		shown[r.Pane] = true
		ids = append(ids, r.Pane)
	}
	for id := range s.panes {
		if !shown[id] {
			delete(s.panes, id)
		}
	}
	s.mu.Unlock()
	// Subscribing sends each pane's screen as it is now, which arrives
	// before the pane's item has reported its size: the pane's terminal
	// here is made at the layout's size for it, so what arrives lands at
	// the size the server's terminal has.
	return c.SubscribePanes(ids)
}

// SetArea is the panes' area in cells, which the tab is laid out in.
func (s *Session) SetArea(cols, rows int) error {
	s.mu.Lock()
	if cols == s.areaCols && rows == s.areaRows {
		s.mu.Unlock()
		return nil
	}
	s.areaCols, s.areaRows = cols, rows
	tab := s.tab
	s.mu.Unlock()
	if err := s.layout(tab, cols, rows); err != nil {
		return err
	}
	s.stateChanged()
	return nil
}

// ResizePane is a pane's item drawing it at cols × rows. The pane's terminal
// on the server is set to that size, and the one here with it.
func (s *Session) ResizePane(pane uint64, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return nil
	}
	s.mu.Lock()
	c := s.client
	pv := s.panes[pane]
	if pv != nil && pv.cols == cols && pv.rows == rows {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := c.ResizePane(pane, cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	pv = s.panes[pane]
	if pv == nil {
		pv = &paneView{screen: vt.NewScreen(cols, rows, 0)}
		s.panes[pane] = pv
	} else {
		pv.screen.Resize(cols, rows)
	}
	pv.cols, pv.rows = cols, rows
	s.mu.Unlock()
	// The size changed under what the pane shows: ask for it again, laid
	// out for the new size, rather than wait for the program to redraw.
	_ = c.SubscribePanes(s.shownPanes())
	s.paneChanged(pane)
	return nil
}

func (s *Session) shownPanes() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]uint64, 0, len(s.rects))
	for _, r := range s.rects {
		ids = append(ids, r.Pane)
	}
	return ids
}

// Key sends a key press to a pane, encoded for the modes its program has
// asked for.
func (s *Session) Key(pane uint64, key, mods int, text string) error {
	s.mu.Lock()
	c := s.client
	var m vt.Modes
	if pv := s.panes[pane]; pv != nil {
		m = pv.screen.Modes()
	}
	// The keyboard protocol the program asked for, as the server keeps it
	// (FeatureKittyKeyboard): the screen here may have been made after the
	// program asked, and only the server saw the request.
	for _, p := range s.snap.Panes {
		if p.ID == pane {
			m.KittyKeyboard, m.ModifyOtherKeys = p.KittyKeyboard, p.ModifyOtherKeys
		}
	}
	s.mu.Unlock()
	if c == nil || pane == 0 {
		return nil
	}
	b, ok := EncodeQtKey(key, mods, text, m)
	if !ok {
		return nil
	}
	// Typing goes back to the present, as in a terminal: what is typed
	// lands there, and a view of the past would hide it.
	s.leaveScroll(pane)
	s.mu.Lock()
	if pv := s.panes[pane]; pv != nil && pv.sel.done {
		pv.sel = selection{}
		s.mu.Unlock()
		s.paneChanged(pane)
	} else {
		s.mu.Unlock()
	}
	return c.SendInput(pane, b)
}

// Frame is what to paint for a pane now, reusing buf's storage. ok is false
// while there is nothing to show for it yet.
func (s *Session) Frame(pane uint64, buf []Cell) (f Frame, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pv := s.panes[pane]
	if pv == nil {
		return Frame{BG: s.colours.BG}, false
	}
	screen := pv.screen
	if pv.scroll != nil {
		screen = pv.scroll
	}
	f = FrameOf(screen, &s.colours, buf)
	if pv.scroll != nil {
		// The past has no cursor: the program's is in the present.
		f.CursorVisible = false
	}
	highlight(&f, pv.sel)
	return f, true
}

// --- what the window does --------------------------------------------------

// Focus gives a pane of the tab shown the keyboard.
func (s *Session) Focus(pane uint64) error {
	s.mu.Lock()
	prev, c := s.focus, s.client
	if pane == prev {
		s.mu.Unlock()
		return nil
	}
	s.focus = pane
	s.mu.Unlock()
	s.stateChanged()
	return c.FocusPane(pane, prev)
}

// ShowWorkspace shows a space, on the tab it is on.
func (s *Session) ShowWorkspace(id uint64) error {
	return s.refreshWith(func() {
		if ws, ok := workspaceIn(s.snap, id); ok && ws.ID == id {
			s.workspace, s.tab, s.focus = id, ws.ActiveTab, 0
		}
	})
}

// ShowTab shows a tab of the space shown.
func (s *Session) ShowTab(id uint64) error {
	return s.refreshWith(func() { s.tab, s.focus = id, 0 })
}

// JumpToPane shows the space and tab a pane is in, with the keyboard on it:
// what a click on an agent does.
func (s *Session) JumpToPane(pane uint64) error {
	found := false
	var prev uint64
	err := s.refreshWith(func() {
		prev = s.focus
		for _, w := range s.snap.Workspaces {
			for _, t := range w.Tabs {
				if hasPane(t, pane) {
					s.workspace, s.tab, s.focus, found = w.ID, t.ID, pane, true
				}
			}
		}
	})
	if err != nil || !found {
		return err
	}
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	return c.FocusPane(pane, prev)
}

// NewTab opens a tab in the space shown, starting where the pane in focus is
// working, as the TUI's does (herdr's new_cwd = "follow").
func (s *Session) NewTab() error {
	s.mu.Lock()
	c, ws, focus := s.client, s.workspace, s.focus
	s.mu.Unlock()
	if ws == 0 {
		return s.ensurePane()
	}
	tab, _, err := c.NewTab(ws, "", proto.PaneSpec{DirOf: focus})
	if err != nil {
		return err
	}
	return s.ShowTab(tab)
}

// LoadAgents reads which agents the server's machine has, for the new-tab
// menu. It runs once, off the main thread: the catalog asks each agent for
// its version.
func (s *Session) LoadAgents() error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if !c.Supports(proto.MethodAgentsCatalog) {
		return nil
	}
	res, err := c.AgentsCatalog()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.catalog = res.Agents
	s.mu.Unlock()
	s.stateChanged()
	return nil
}

// resumeScript runs an agent and leaves a shell when it exits, so the
// terminal stays as the TUI's "new agent here" leaves it (cmd/tend's
// resumeScript, the same two lines).
const resumeScript = `"$@"
exec "${SHELL:-/bin/sh}"`

// NewAgentTab opens a tab running an agent, where the pane in focus is
// working: the TUI's groupAgentSpec — the path the catalog found it at,
// the arguments it is started with, and its name for its states only when
// tend has rules for it (one without, Aider, is refused if named). A Codex
// is started without its shared daemon by the server, as everywhere tend
// starts one (agent.LaunchArgv).
func (s *Session) NewAgentTab(id string) error {
	s.mu.Lock()
	c, ws, focus := s.client, s.workspace, s.focus
	var agent proto.AgentStatus
	for _, a := range s.catalog {
		if a.ID == id {
			agent = a
		}
	}
	s.mu.Unlock()
	if agent.ID == "" {
		return fmt.Errorf("no agent %q here", id)
	}
	if ws == 0 {
		if err := s.ensurePane(); err != nil {
			return err
		}
		s.mu.Lock()
		ws = s.workspace
		s.mu.Unlock()
	}
	spec := agentSpec(agent)
	spec.DirOf = focus
	tab, _, err := c.NewTab(ws, agent.Name, spec)
	if err != nil {
		return fmt.Errorf("%s did not start: %w", agent.Name, err)
	}
	return s.ShowTab(tab)
}

// NewWorkspace opens a space with a shell and shows it.
func (s *Session) NewWorkspace() error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	ws, err := c.NewWorkspace("")
	if err != nil {
		return err
	}
	tab, _, err := c.NewTab(ws, "", proto.PaneSpec{})
	if err != nil {
		return err
	}
	return s.refreshWith(func() { s.workspace, s.tab, s.focus = ws, tab, 0 })
}

// Split splits the pane in focus, "columns" side by side or "rows" one over
// the other, the new pane starting where the focused one is working.
func (s *Session) Split(direction string) error {
	s.mu.Lock()
	c, focus := s.client, s.focus
	s.mu.Unlock()
	if focus == 0 {
		return nil
	}
	pane, err := c.SplitPane(focus, direction, proto.PaneSpec{DirOf: focus})
	if err != nil {
		return err
	}
	return s.refreshWith(func() { s.focus = pane })
}

// ClosePane closes the pane in focus.
func (s *Session) ClosePane() error {
	s.mu.Lock()
	c, focus := s.client, s.focus
	s.mu.Unlock()
	if focus == 0 {
		return nil
	}
	return c.ClosePane(focus)
}

// CloseTab closes a tab.
func (s *Session) CloseTab(id uint64) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	return c.CloseTab(id)
}

// AdjustSplit moves the border on a pane's side by cells, as dragging a
// divider in the TUI does (dragDivider): the server's pane.adjust, at the
// size this window lays the tab out at.
func (s *Session) AdjustSplit(pane uint64, side string, cells int) error {
	if cells == 0 {
		return nil
	}
	s.mu.Lock()
	c, cols, rows, tab := s.client, s.areaCols, s.areaRows, s.tab
	s.mu.Unlock()
	if err := c.AdjustSplit(pane, side, cells, cols, rows); err != nil {
		return err
	}
	if err := s.layout(tab, cols, rows); err != nil {
		return err
	}
	s.stateChanged()
	return nil
}

// Pin keeps this window on one tab: a tab in a window of its own.
func (s *Session) Pin(tab uint64) error {
	return s.refreshWith(func() { s.pinned, s.tab, s.focus = tab, tab, 0 })
}

// Pinned is the tab this window is kept on, 0 for none.
func (s *Session) Pinned() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinned
}

// LeaveTab moves this window off a tab another window now shows, to another
// tab of its space, so the two do not each size the same panes to their
// own area.
func (s *Session) LeaveTab(tab uint64) error {
	return s.refreshWith(func() {
		if s.tab != tab {
			return
		}
		if ws, ok := workspaceIn(s.snap, s.workspace); ok {
			for _, t := range ws.Tabs {
				if t.ID != tab {
					s.tab, s.focus = t.ID, 0
					return
				}
			}
		}
	})
}

// ToggleZoom makes a pane fill its tab, or the tab whole again.
func (s *Session) ToggleZoom(pane uint64) error {
	s.mu.Lock()
	if s.zoom == pane {
		s.zoom = 0
	} else {
		s.zoom, s.focus = pane, pane
	}
	tab, cols, rows := s.tab, s.areaCols, s.areaRows
	s.mu.Unlock()
	if err := s.layout(tab, cols, rows); err != nil {
		return err
	}
	s.stateChanged()
	return nil
}

// ToggleGroup folds or unfolds a group of spaces in this window's list.
func (s *Session) ToggleGroup(name string) {
	s.mu.Lock()
	s.folded[name] = !s.folded[name]
	s.mu.Unlock()
	s.stateChanged()
}

// ToggleGrouped lists agents under their tabs, or flat.
func (s *Session) ToggleGrouped() {
	s.mu.Lock()
	s.grouped = !s.grouped
	s.mu.Unlock()
	s.stateChanged()
}

func (s *Session) paneChanged(pane uint64) {
	if s.PaneChanged != nil {
		s.PaneChanged(pane)
	}
}

func (s *Session) stateChanged() {
	if s.StateChanged != nil {
		s.StateChanged()
	}
}

func (s *Session) status(msg string) {
	if s.Status != nil {
		s.Status(msg)
	}
}

// --- client.Handler ---------------------------------------------------------

// PaneOutput is part of client.Handler.
func (s *Session) PaneOutput(pane uint64, data []byte) {
	s.mu.Lock()
	pv := s.panes[pane]
	if pv == nil {
		// Output for a pane whose item has not reported its size yet: the
		// screen is made at the layout's size for it, which is the size the
		// server's terminal has, so the screen subscribing sends lands right.
		for _, r := range s.rects {
			if r.Pane == pane {
				pv = &paneView{screen: vt.NewScreen(r.Cols, r.Rows, 0), cols: r.Cols, rows: r.Rows}
				s.panes[pane] = pv
			}
		}
	}
	if pv == nil {
		s.mu.Unlock()
		return
	}
	_, _ = pv.screen.Write(data)
	s.mu.Unlock()
	s.paneChanged(pane)
}

// Event is part of client.Handler. Anything that changes what the session
// is — a pane opened or closed, an agent's state, a space renamed — is read
// again from a snapshot, off this goroutine.
func (s *Session) Event(ev proto.Event) {
	switch ev.Kind {
	case proto.EventPaneClipboard:
		// A program copied (OSC 52): the clipboard is the viewer's, so the
		// server passes it on and this window puts it there — how Claude
		// Code's own selection reaches the clipboard.
		if len(ev.Data) > 0 {
			s.mu.Lock()
			s.lastCopy[ev.Pane] = string(ev.Data)
			s.mu.Unlock()
			if s.Copied != nil {
				s.Copied(string(ev.Data))
			}
		}
		return
	case proto.EventNotify:
		s.announceCustom(ev.Title, ev.Body, ev.Pane)
		return
	case proto.EventContextArrived:
		return
	case proto.EventPaneState:
		s.announceState(ev)
	case proto.EventTasksChanged:
		s.reloadDockIf("tasks")
		return
	}
	if !s.eventRefresh.CompareAndSwap(false, true) {
		return // one is on its way, and will read this change too
	}
	go func() {
		// A moment's wait gathers the events of one change (a split is an
		// opened pane, a focus and a layout) into one snapshot.
		time.Sleep(20 * time.Millisecond)
		s.eventRefresh.Store(false)
		if err := s.refresh(); err != nil {
			s.status(err.Error())
		}
	}()
}

// Disconnected is part of client.Handler.
func (s *Session) Disconnected() {
	s.status("the session ended")
}
