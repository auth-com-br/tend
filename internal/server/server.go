// Package server owns a running tend session.
//
// It pairs the pure records in internal/session with the live halves they
// describe: a pty, a terminal, and a detector per pane. The split is the point
// — a pane's identity and place in the layout outlive its process, and the
// session can be reasoned about without any of this running.
//
// # Locking
//
// Two levels, always taken in this order and never the reverse:
//
//  1. the server's lock, guarding the session tree and the runtime map
//  2. a pane runtime's own lock, guarding that pane's terminal
//
// Pane output never touches the server's lock, which is what keeps one busy
// agent from serialising every other pane through a single mutex. Callbacks
// from inside a terminal record into the runtime and let the detection loop
// carry the result up.
package server

import (
	"errors"
	"fmt"
	"github.com/auth-com-br/tend/internal/agentview"
	"os"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/agentsessions"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

const (
	// defaultScrollback is how much history a pane keeps when unconfigured.
	defaultScrollback = 5000
	// readBuffer is the chunk size for pty reads. Agent output arrives in
	// bursts of full redraws, so a small buffer costs syscalls for nothing.
	readBuffer = 32 * 1024
	// defaultDetectInterval is how often panes are re-examined. Detection is
	// skipped entirely for a pane whose screen has not changed, so this bounds
	// how stale a state can be rather than how much work is done.
	defaultDetectInterval = 150 * time.Millisecond
	// defaultShutdownGrace is how long a pane gets to end on a hangup before
	// it is killed. A process that ignores SIGHUP must not be able to hold
	// shutdown open indefinitely.
	defaultShutdownGrace = 2 * time.Second
	// defaultAdoptInterval is how often a pane is asked what is running in it.
	// Slower than detection because it costs a syscall and a file read per
	// pane, and because starting an agent is something a person does, not
	// something that happens many times a second.
	defaultAdoptInterval = time.Second
)

// branchTTL is how long a directory's branch is trusted before being read
// again.
const branchTTL = 2 * time.Second

// branchCache remembers each workspace directory's branch, since a snapshot is
// taken on every redraw and reading one touches the filesystem.
//
// It has its own lock rather than the server's. Resolving a branch is I/O, and
// the server lock is the one every pane operation needs — holding it across a
// file read would put the whole session behind a slow disk. Keeping the locks
// separate also keeps this callable from inside a snapshot, which already
// holds the session lock and would otherwise deadlock on itself.
type branchCache struct {
	mu      sync.Mutex
	entries map[string]branchEntry
	track   map[string]trackEntry
}

func newBranchCache() *branchCache {
	return &branchCache{
		entries: make(map[string]branchEntry),
		track:   make(map[string]trackEntry),
	}
}

// lookup returns a directory's branch, reading it again once it goes stale.
func (c *branchCache) lookup(dir string) string {
	if dir == "" {
		return ""
	}

	c.mu.Lock()
	entry, ok := c.entries[dir]
	fresh := ok && time.Since(entry.at) < branchTTL
	c.mu.Unlock()
	if fresh {
		return entry.name
	}

	name := session.Branch(dir)

	c.mu.Lock()
	c.entries[dir] = branchEntry{name: name, at: time.Now()}
	c.mu.Unlock()
	return name
}

// trackingTTL is how long a directory's ahead/behind is trusted. Longer than
// the branch's, because it costs a process rather than two file reads and
// changes only when somebody commits, fetches or pushes.
const trackingTTL = 10 * time.Second

// trackingCheck is how often the server looks for a directory whose drift has
// gone stale. It is not how often git runs.
const trackingCheck = time.Second

// tracking returns what is known about a directory's drift, without going and
// finding out.
//
// Never blocking is the point. Reading this runs git, and a snapshot is what a
// client waits on to draw a frame — a redraw must not sit behind a subprocess,
// however short its timeout. The numbers are refreshed on a timer instead, so
// the worst this returns is the answer from a moment ago, or none at all the
// first time a directory is seen.
func (c *branchCache) tracking(dir string) session.Tracking {
	if dir == "" {
		return session.Tracking{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.track[dir].value
}

// refreshTracking reads the drift of every directory given and reports whether
// any of them changed.
//
// This is the only place git is run, and it is called from the server's own
// loop rather than from anything a client is waiting on.
func (c *branchCache) refreshTracking(dirs []string) (changed bool) {
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		c.mu.Lock()
		entry, ok := c.track[dir]
		fresh := ok && time.Since(entry.at) < trackingTTL
		c.mu.Unlock()
		if fresh {
			continue
		}

		next := session.AheadBehind(dir)

		c.mu.Lock()
		if !ok || entry.value != next {
			changed = true
		}
		c.track[dir] = trackEntry{value: next, at: time.Now()}
		c.mu.Unlock()
	}
	return changed
}

// trackEntry is a cached tracking count and when it was read.
type trackEntry struct {
	value session.Tracking
	at    time.Time
}

// branchEntry is a cached branch name and when it was read.
type branchEntry struct {
	name string
	at   time.Time
}

// ErrClosed is returned once the server has shut down.
var ErrClosed = errors.New("server: closed")

// Config configures a server. The zero value is usable.
type Config struct {
	// Catalog supplies the detection manifests. Nil loads the bundled ones.
	Catalog *detect.Catalog
	// DetectInterval is how often panes are re-examined. Zero picks a default.
	DetectInterval time.Duration
	// DefaultSize is the terminal size a pane starts at, before a client
	// attaches and resizes it.
	DefaultSize pty.Size
	// ShutdownGrace is how long Close waits for panes to end on their own
	// before killing them. Zero picks a default.
	ShutdownGrace time.Duration
	// Scrollback is how many lines of history each pane keeps. Zero picks a
	// default.
	Scrollback int
	// AdoptInterval is how often a pane is checked for the program now in
	// charge of its terminal. Zero picks a default.
	AdoptInterval time.Duration
	// Build is this binary's version, reported in the handshake so a client
	// can say which two builds are talking.
	Build string
	// StateFile is where the session's arrangement is written, and read back
	// from when a server starts. Empty means the session is not kept: it lives
	// exactly as long as the process does.
	StateFile string
	// OmitFeatures leaves the feature list out of the handshake, which is
	// what a server from before there was one looks like.
	OmitFeatures bool
	// Advertise overrides the method list sent in the handshake. Empty means
	// everything this build serves, which is what a real server sends; naming
	// fewer is how an older one is stood up to test against.
	Advertise []string
	// Dir is where a workspace created without one is rooted. Empty uses the
	// server's own working directory, which is where its panes start anyway.
	Dir string
	// Replace starts this server's replacement and returns once that one has
	// said it holds every pane, or with the reason it did not. Nil means the
	// server cannot be replaced while running, only restarted. Starting a
	// process is the caller's business, which is why it is a function: the
	// server knows what to hand over, not what to hand it to.
	Replace func(*Handoff) error
	// PaneEnv returns what is added to the environment of a pane's process:
	// where the session's automation socket is and which pane this is, which
	// is all a hook inside an agent has to go on. Nil adds nothing.
	PaneEnv func(session.PaneID) []string
	// Plugins is the host that runs installed plugins' hooks. Nil means this
	// server runs none, which is what a test that does not care about them
	// gets.
	Plugins *Plugins
	// Shell is what a pane runs when there is nothing else for it to run.
	// Nil falls back to $SHELL, then /bin/sh.
	Shell []string
	// CommandEnv is added to the environment of a command the server runs
	// for the user — a tab bar entry — so it can call back on the automation
	// socket. Nil adds nothing.
	CommandEnv []string
	// UpdateSource says where the release check looks and whether it looks
	// at all, read again at each check. Nil means this server never checks,
	// which is what a test that does not care gets (updatecheck.go).
	UpdateSource func() (manifest string, enabled bool)
	// UpdateInterval is how often the check runs until it finds a release.
	// Zero is herdr's half hour.
	UpdateInterval time.Duration
	// NotesPath is where release notes are kept: release-notes.json beside
	// the settings file. Empty keeps none.
	NotesPath string
	// FakeUpdate pretends a release of this version is published, herdr's
	// HERDR_FAKE_UPDATE_VERSION, to see the notice without publishing one.
	FakeUpdate string
	// Activity is where the record of what agents did is written
	// (activity.go). Nil keeps none.
	Activity *activity.Log
	// SessionName names this session in that record.
	SessionName string
	// ActivityInterval is how often the record is brought up to date. Zero
	// picks a default.
	ActivityInterval time.Duration
	// ActivityGrace is how long a pane may be without its agent before its
	// run is over. Zero picks a default.
	ActivityGrace time.Duration
	// Transcripts finds the file an agent keeps a conversation in. Nil
	// looks where the agents keep them on this machine.
	Transcripts activity.Transcripts
	// Policy returns the rules agents are held to, asked each time they are
	// checked so an edited settings file applies without a restart. Nil
	// holds them to none.
	Policy func() policy.Policy
}

// PaneSpec describes a pane to open.
type PaneSpec struct {
	Command []string
	Dir     string
	// DirOf, with no Dir, is the pane whose working directory the new one
	// starts in; see followDir.
	DirOf session.PaneID
	Env   []string
	Title string
	// Named marks a title the user gave rather than one a program reported.
	Named bool

	// CloseOnExit closes the pane when its program ends.
	CloseOnExit bool

	// Agent names the detection manifest. Empty means infer it from the
	// command; a command that matches nothing simply gets no detector, since
	// plenty of useful panes are not agents.
	Agent string

	// history is what a restored pane had said before, fed to its terminal
	// ahead of the new process so the past is above the prompt.
	history []byte
	// resume is the conversation a restored agent is being started back in,
	// so the pane knows it before its hook says so again.
	resume *agent.PersistedSession

	// Size is the pane's initial terminal size. Zero uses the server default.
	Size pty.Size

	// NoDetect runs no agent detection in the pane: a popup, which herdr
	// starts with detection disabled.
	NoDetect bool
}

// PaneStatus is what a client needs to show about a pane.
type PaneStatus struct {
	ID      session.PaneID
	Title   string
	Agent   string
	State   detect.State
	Rule    string
	Running bool
	Pid     int
	ExitErr string
	// Message is what a hook said alongside its state, if one is behind it.
	Message string
	// Presentation is what a hook said to show about the pane: a name for the
	// agent, values beside it, labels for its states.
	Presentation agent.Presentation
	// Mouse is whether the pane's program asked for mouse reports at all.
	// MouseDrag and MouseMotion say how much it asked for, and MouseSGR how
	// it wants them written: a client forwarding the mouse has to send what
	// the program subscribed to, in the encoding it said it reads.
	Mouse       bool
	MouseDrag   bool
	MouseMotion bool
	MouseSGR    bool
	// Graphics changes whenever the pane's images or their placements do.
	Graphics uint64
}

// Server owns a running session.
type Server struct {
	cfg     Config
	catalog *detect.Catalog
	events  *eventHub

	// sessionsOnce makes claudeSessions, the conversations Claude Code
	// keeps on this machine, read when the sessions list first asks.
	sessionsOnce   sync.Once
	claudeSessions *agentsessions.Claude

	mu       sync.Mutex
	session  *session.Session
	runtimes map[session.PaneID]*paneRuntime
	titles   map[session.PaneID]string
	// conns tracks connected clients so shutdown can hang them up. Without
	// this they sit blocked on a socket read that nothing ever ends, and Close
	// waits on them forever.
	conns map[*clientConn]struct{}
	// update is what the release check found (updatecheck.go).
	update updateState
	// captured is the context buffer (context.go).
	captured contextState
	// browsers are the browsers attached (browser.go).
	browsers browserHub
	branches *branchCache
	closed   bool
	// focusedPane, focusedTab and focusedWorkspace are what a client last
	// said it was looking at, so the events about focus are sent when it
	// moves rather than on every report.
	focusedPane      session.PaneID
	focusedTab       session.TabID
	focusedWorkspace session.WorkspaceID
	// windowUnfocused is the client's terminal window being behind
	// something, so nothing on screen is being seen.
	windowUnfocused bool
	// handingOff is set from the moment the session is described for a
	// replacement until that either takes over or fails to.
	handingOff bool
	// lastSaved is the session as it was last written to the state file, so
	// a snapshot identical to it is not written again.
	lastSaved []byte

	// popup is the one popup open, if any: a pane floating over the tab it
	// was opened from, in no layout.
	popup *popupState
	// windowTitle is a title a script set over the API, which replaces the
	// configured template in every client until it is cleared (herdr's
	// client.window_title.set). A fact about the session, not about one
	// client, so it is here and goes out in the snapshot.
	windowTitle string
	// tabBar is the right end of the tab bar, kept current by the commands
	// and the clock it runs. It has its own lock: a command finishing must
	// not wait on the lock every pane operation needs.
	tabBar tabBar
	// workspaceMeta is what scripts reported about each space.
	workspaceMeta workspaceMeta
	// agentView is the filter and order a script set on the agent list
	// (agent.view.set), or nil.
	agentView *agentview.View
	// activity keeps the record of what agents did (activity.go), or is nil.
	activity *recorder

	// retick carries a new detection interval to the loop, which cannot read
	// the configuration under the lock while it is doing a round of work.
	retick chan time.Duration

	done chan struct{}
	wg   sync.WaitGroup
}

// New starts a server with no panes.
func New(cfg Config) (*Server, error) {
	s, err := build(cfg)
	if err != nil {
		return nil, err
	}

	// Before anything can connect: a client must never see the empty session
	// that exists for an instant ahead of the restored one, or it would fill
	// it with a fresh shell and the two would be merged.
	s.restore()

	s.startLoops()
	return s, nil
}

// build makes a server that is not running yet: no panes, no loops.
func build(cfg Config) (*Server, error) {
	catalog := cfg.Catalog
	if catalog == nil {
		var err error
		catalog, err = detect.Bundled()
		if err != nil {
			return nil, fmt.Errorf("server: loading manifests: %w", err)
		}
	}
	if cfg.DetectInterval <= 0 {
		cfg.DetectInterval = defaultDetectInterval
	}
	if !cfg.DefaultSize.Valid() {
		cfg.DefaultSize = pty.DefaultSize
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = defaultShutdownGrace
	}
	if cfg.Scrollback <= 0 {
		cfg.Scrollback = defaultScrollback
	}
	if cfg.AdoptInterval <= 0 {
		cfg.AdoptInterval = defaultAdoptInterval
	}
	if cfg.Dir == "" {
		// A failure here is not worth refusing to start over: it only means
		// spaces show no branch.
		cfg.Dir, _ = os.Getwd()
	}

	s := &Server{
		cfg:      cfg,
		catalog:  catalog,
		events:   newEventHub(),
		session:  session.New(),
		runtimes: make(map[session.PaneID]*paneRuntime),
		titles:   make(map[session.PaneID]string),
		conns:    make(map[*clientConn]struct{}),
		branches: newBranchCache(),
		done:     make(chan struct{}),
		retick:   make(chan time.Duration, 1),
	}
	if cfg.Activity != nil {
		find := cfg.Transcripts
		if find == nil {
			find = DefaultTranscripts
		}
		s.activity = newRecorder(cfg.Activity, cfg.SessionName, find)
	}
	return s, nil
}

// startLoops starts what runs for the server's whole life.
func (s *Server) startLoops() {
	s.wg.Add(1)
	go s.detectLoop()
	if s.cfg.UpdateSource != nil || s.cfg.FakeUpdate != "" {
		s.loadUpdateState()
		s.wg.Add(1)
		go s.updateLoop()
	}
	if s.cfg.StateFile != "" {
		s.wg.Add(1)
		go s.persistLoop()
	}
	if s.activity != nil {
		s.wg.Add(1)
		go s.activityLoop()
	}
}

// Close stops every pane and ends every subscription.
//
// Each pane is hung up, which ends its process and so ends the goroutine
// reading it — closing the terminal alone would not, since a read already
// blocked on the master is a plain syscall Go cannot interrupt. A pane that
// ignores the hangup is killed once the grace period runs out.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// Written down while the panes are still alive. A moment later they are
	// hung up, and their directories and their last lines go with them: this
	// is the last point at which the session can still be asked what it is.
	s.saveStructure()
	s.saveHistory()
	// And the runs ended, for the same reason: what changed in each
	// checkout is still there to be asked about, and the agents are about
	// to go.
	s.stopActivity()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	close(s.done)
	for _, rt := range runtimes {
		rt.setClosing()
		_ = rt.pty.Close()
	}
	// Hang up the clients too. Their handlers are blocked on a socket read
	// that nothing else will end, and a client seeing its connection drop is
	// how it learns the server is gone.
	for _, c := range conns {
		_ = c.conn.Close()
	}

	// A pane that ignores the hangup gets killed rather than holding shutdown
	// open. Without this, one stubborn process would block Close forever.
	if !awaitGroup(&s.wg, s.cfg.ShutdownGrace) {
		for _, rt := range runtimes {
			_ = rt.pty.Kill()
		}
		s.wg.Wait()
	}

	s.events.closeAll()
	return nil
}

// awaitGroup reports whether wg finished within d.
func awaitGroup(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// Subscribe returns a stream of events. The caller closes it when done.
func (s *Server) Subscribe(buffer int) *Subscription { return s.events.subscribe(buffer) }

// --- session structure -----------------------------------------------------

// NewWorkspace adds a workspace and focuses it.
func (s *Server) NewWorkspace(name string) (session.WorkspaceID, error) {
	return s.NewWorkspaceIn(name, "")
}

// NewWorkspaceIn adds a workspace rooted at a directory.
//
// A workspace with no directory of its own takes the server's, since that is
// where its panes start. Without it the space would show no branch, which is
// the one thing that tells two spaces on the same repository apart.
func (s *Server) NewWorkspaceIn(name, dir string) (session.WorkspaceID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	if dir == "" {
		dir = s.cfg.Dir
	}
	id := s.session.AddWorkspaceIn(name, dir).ID
	// Published after the lock is released, because publishing reaches
	// plugins and nothing that touches the outside world runs under the lock
	// every pane operation needs.
	defer func() { go s.publish(Event{Kind: EventWorkspaceCreated, Workspace: id}) }()
	return id, nil
}

// NewTab creates a tab with one pane and starts its process.
//
// The pane is started while the session lock is held. Starting a process is
// brief and, unlike pane output, does not sit on a hot path; holding the lock
// means no caller can ever observe a pane record without the runtime that
// backs it.
func (s *Server) NewTab(ws session.WorkspaceID, name string, spec PaneSpec) (session.TabID, session.PaneID, error) {
	s.followDir(&spec, spec.DirOf)
	if err := s.admitSpec(spec); err != nil {
		return 0, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, 0, ErrClosed
	}

	tab, pane, err := s.session.AddTab(ws, name, spec.record())
	if err != nil {
		return 0, 0, err
	}
	if err := s.startLocked(pane.ID, spec); err != nil {
		// Roll the session back: a tab whose only pane never started is not a
		// tab the user asked for.
		_, _ = s.session.CloseTab(tab.ID)
		return 0, 0, err
	}
	defer func() { go s.publish(Event{Kind: EventTabCreated, Tab: tab.ID, Workspace: ws}) }()
	return tab.ID, pane.ID, nil
}

// followDir fills in a spec with no directory from the pane it follows:
// herdr's follow policy, the foreground program's directory, else the
// shell's, else nothing and the pane starts in the server's. It takes the
// lock only to find the pane: reading /proc is I/O, and is done after.
func (s *Server) followDir(spec *PaneSpec, from session.PaneID) {
	if spec.Dir != "" || from == 0 {
		return
	}
	s.mu.Lock()
	rt := s.runtimes[from]
	s.mu.Unlock()
	if rt != nil {
		spec.Dir = rt.pty.FollowCwd()
	}
}

// SplitPane divides a pane and starts a process in the new half.
func (s *Server) SplitPane(target session.PaneID, dir session.Direction, spec PaneSpec) (session.PaneID, error) {
	// herdr's split starts where the pane split is working, as its
	// launch_cwd_for_pane does; before this it started in the server's own
	// directory, wherever `tend` was first run.
	from := spec.DirOf
	if from == 0 {
		from = target
	}
	s.followDir(&spec, from)
	if err := s.admitSpec(spec); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}

	pane, err := s.session.SplitPane(target, dir, spec.record())
	if err != nil {
		return 0, err
	}
	if err := s.startLocked(pane.ID, spec); err != nil {
		_ = s.session.ClosePane(pane.ID)
		return 0, err
	}
	return pane.ID, nil
}

// DockPane opens a pane along the left edge of beside's tab and starts a
// process in it. With no directory given it starts where beside is now —
// where the user cd'd to, not where the pane was opened — which for the file
// explorer is the project being worked on.
func (s *Server) DockPane(beside session.PaneID, share float64, right bool, spec PaneSpec) (session.PaneID, error) {
	pane, _, err := s.DockPaneUnless(beside, share, right, spec, "")
	return pane, err
}

// DockPaneUnless is DockPane that opens nothing when beside's tab already
// has a pane named unless, and returns that one: the files panel opened on
// its own in every tab (herdr-sidebar's ensure) asks this way, so two
// clients showing the same tab at once cannot dock two panels. The check
// and the dock are under one hold of the lock, which is what makes it so.
func (s *Server) DockPaneUnless(beside session.PaneID, share float64, right bool, spec PaneSpec, unless string) (session.PaneID, bool, error) {
	if spec.Dir == "" {
		s.mu.Lock()
		rt := s.runtimes[beside]
		s.mu.Unlock()
		// After the lock: asking the terminal reads /proc, which is I/O.
		if rt != nil {
			spec.Dir = rt.pty.Cwd()
		}
	}
	if err := s.admitSpec(spec); err != nil {
		return 0, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, false, ErrClosed
	}
	tab, ok := s.session.TabOf(beside)
	if !ok {
		return 0, false, fmt.Errorf("%w: %d", session.ErrNoSuchPane, beside)
	}
	if unless != "" {
		for _, id := range tab.Panes() {
			if p, ok := tab.Pane(id); ok && p.Named && p.Title == unless {
				return id, true, nil
			}
		}
	}
	pane, err := s.session.DockPane(tab.ID, share, right, spec.record())
	if err != nil {
		return 0, false, err
	}
	if err := s.startLocked(pane.ID, spec); err != nil {
		_ = s.session.ClosePane(pane.ID)
		return 0, false, err
	}
	return pane.ID, false, nil
}

// ClosePane stops a pane's process and removes it from the session.
func (s *Server) ClosePane(id session.PaneID) error {
	if s.closePopupIf(id) {
		return nil
	}
	// The last pane of a tab takes the tab, and a popup of that tab with it.
	defer s.reconcilePopup()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	rt, ok := s.runtimes[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	if err := s.session.ClosePane(id); err != nil {
		s.mu.Unlock()
		return err
	}
	delete(s.runtimes, id)
	delete(s.titles, id)
	s.mu.Unlock()

	rt.setClosing()
	_ = rt.pty.Close()
	s.publish(Event{Kind: EventPaneClosed, Pane: id})
	return nil
}

// CloseTab stops every pane in a tab and removes it.
func (s *Server) CloseTab(id session.TabID) error {
	defer s.reconcilePopup()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	ws := s.workspaceOfTabLocked(id)
	closed, err := s.session.CloseTab(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stopped := make([]*paneRuntime, 0, len(closed))
	for _, pid := range closed {
		if rt, ok := s.runtimes[pid]; ok {
			stopped = append(stopped, rt)
		}
		delete(s.runtimes, pid)
		delete(s.titles, pid)
	}
	s.mu.Unlock()

	for _, rt := range stopped {
		rt.setClosing()
		_ = rt.pty.Close()
		s.publish(Event{Kind: EventPaneClosed, Pane: rt.id})
	}
	s.publish(Event{Kind: EventTabClosed, Tab: id, Workspace: ws})
	return nil
}

// workspaceOfTabLocked is the space a tab is in. The caller holds the lock.
func (s *Server) workspaceOfTabLocked(id session.TabID) session.WorkspaceID {
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if t.ID == id {
				return w.ID
			}
		}
	}
	return 0
}

// features is what this server says it provides beyond its methods.
// Features is what features() says in the handshake, for the automation
// socket's browser.attach to say too.
func (s *Server) Features() []string { return s.features() }

func (s *Server) features() []string {
	if s.cfg.OmitFeatures {
		return nil
	}
	return proto.KnownFeatures
}

// GroupWorkspace moves a workspace into a group, or out of one.
func (s *Server) GroupWorkspace(id session.WorkspaceID, group string) error {
	return s.rearrange(func(sess *session.Session) error { return sess.GroupWorkspace(id, group) })
}

// CloseWorkspace closes a workspace and stops every pane in it.
func (s *Server) CloseWorkspace(id session.WorkspaceID) error {
	defer s.reconcilePopup()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	closed, err := s.session.CloseWorkspace(id)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	stopped := make([]*paneRuntime, 0, len(closed))
	for _, pid := range closed {
		if rt, ok := s.runtimes[pid]; ok {
			stopped = append(stopped, rt)
		}
		delete(s.runtimes, pid)
		delete(s.titles, pid)
	}
	s.mu.Unlock()

	for _, rt := range stopped {
		rt.setClosing()
		_ = rt.pty.Close()
		s.publish(Event{Kind: EventPaneClosed, Pane: rt.id})
	}
	s.publish(Event{Kind: EventWorkspaceClosed, Workspace: id})
	return nil
}

// startLocked opens a pty for an existing pane record. The caller holds the
// session lock.
func (s *Server) startLocked(id session.PaneID, spec PaneSpec) error {
	if len(spec.Command) == 0 {
		// Nothing asked for: the shell of the machine the pane runs on. A
		// client on another machine does not know what that is — it sent its
		// own /usr/bin/zsh to a host without zsh, and every new tab failed.
		spec.Command = s.shell()
		if p, ok := s.session.Pane(id); ok {
			p.Command = spec.Command
		}
	}
	if s.handingOff {
		return ErrHandingOff
	}

	manifest, err := agent.ResolveManifest(s.catalog, spec.Agent, spec.Command[0])
	if err != nil {
		return err
	}
	if spec.NoDetect {
		manifest = nil
	}

	size := spec.Size
	if !size.Valid() {
		size = s.cfg.DefaultSize
	}

	env := spec.Env
	if s.cfg.PaneEnv != nil {
		if env == nil {
			// Nil means "inherit", and appending to nil would mean "only
			// these", which starts a shell with no PATH.
			env = os.Environ()
		}
		env = append(env[:len(env):len(env)], s.cfg.PaneEnv(id)...)
	}

	p, err := pty.Start(spec.Command[0], spec.Command[1:], pty.Options{
		Dir:  spec.Dir,
		Env:  env,
		Size: size,
	})
	if err != nil {
		return fmt.Errorf("server: starting %s: %w", spec.Command[0], err)
	}

	rt := newPaneRuntime(id, p, size, manifest, s.cfg.Scrollback, spec.Command[0], spec.Agent, s.knownAgent)
	rt.closeOnExit = spec.CloseOnExit
	if spec.resume != nil {
		rt.arbiter.RestoreSession(*spec.resume)
	}
	if len(spec.history) > 0 {
		// Before the reader starts, so the old output is above the new
		// process's first line rather than mixed into it.
		rt.write(spec.history)
	}
	s.runtimes[id] = rt
	s.titles[id] = ""
	if manifest != nil {
		_ = s.session.SetPaneState(id, manifest.ID, detect.StateUnknown)
	}

	s.wg.Add(1)
	go s.readPane(rt)

	s.publish(Event{Kind: EventPaneOpened, Pane: id})
	return nil
}

// record converts a spec into the session's own pane description.
func (spec PaneSpec) record() session.PaneSpec {
	return session.PaneSpec{
		Command: spec.Command,
		Dir:     spec.Dir,
		Title:   spec.Title,
		Named:   spec.Named,
		Agent:   spec.Agent,

		CloseOnExit: spec.CloseOnExit,
	}
}

// --- pane input and output -------------------------------------------------

// Write sends input to a pane, as though it had been typed.
func (s *Server) Write(id session.PaneID, data []byte) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	_, err = rt.pty.Write(data)
	return err
}

// RenamePane gives a pane a name of its own.
func (s *Server) RenamePane(id session.PaneID, name string) error {
	return s.rearrange(func(sess *session.Session) error { return sess.RenamePane(id, name) })
}

// RenameTab changes a tab's label.
func (s *Server) RenameTab(id session.TabID, name string) error {
	return s.announce(s.rearrange(func(sess *session.Session) error { return sess.RenameTab(id, name) }),
		Event{Kind: EventTabRenamed, Tab: id})
}

// RenameWorkspace changes a workspace's label.
func (s *Server) RenameWorkspace(id session.WorkspaceID, name string) error {
	return s.announce(s.rearrange(func(sess *session.Session) error { return sess.RenameWorkspace(id, name) }),
		Event{Kind: EventWorkspaceRenamed, Workspace: id})
}

// AdjustSplit moves one edge of a pane within its tab's layout, taking the
// space from the neighbour across it.
// SetWindowTitle sets the outer window title every client writes, or with ""
// goes back to each client's configured template. It reports how many clients
// are attached to be told, which is herdr's "no foreground client" answer when
// it is none.
func (s *Server) SetWindowTitle(title string) int {
	s.mu.Lock()
	s.windowTitle = title
	clients := len(s.conns)
	s.mu.Unlock()
	s.publish(Event{Kind: EventSessionChanged})
	return clients
}

// Notify passes something to whoever is looking at the session. The server
// has no screen of its own; every attached client decides what to do with it.
func (s *Server) Notify(title, body string) {
	s.publish(Event{Kind: EventNotify, Title: title, Body: body})
}

// SwapPanes exchanges two panes' places in their tab.
func (s *Server) SwapPanes(a, b session.PaneID) error {
	return s.rearrange(func(sess *session.Session) error { return sess.SwapPanes(a, b) })
}

// SwapPaneToward exchanges a pane with its neighbour on one side.
func (s *Server) SwapPaneToward(id session.PaneID, side session.Side, area session.Rect) (session.PaneID, error) {
	var other session.PaneID
	err := s.rearrange(func(sess *session.Session) error {
		var err error
		other, err = sess.SwapPaneToward(id, side, area)
		return err
	})
	return other, err
}

// MovePane takes a pane out of its tab and puts it beside another, which may
// be in another tab or another space. Nothing restarts.
func (s *Server) MovePane(id, beside session.PaneID, dir session.Direction) error {
	return s.announce(s.rearrange(func(sess *session.Session) error { return sess.MovePane(id, beside, dir) }),
		Event{Kind: EventPaneMoved, Pane: id})
}

// MoveTab puts a tab at index, or delta places along when delta is set.
func (s *Server) MoveTab(id session.TabID, index, delta int) error {
	return s.announce(s.moveTab(id, index, delta), Event{Kind: EventTabMoved, Tab: id})
}

func (s *Server) moveTab(id session.TabID, index, delta int) error {
	return s.rearrange(func(sess *session.Session) error {
		if delta != 0 {
			from, ok := sess.TabIndex(id)
			if !ok {
				return session.ErrNoSuchTab
			}
			index = from + delta
		}
		return sess.MoveTab(id, index)
	})
}

// MoveWorkspace puts a space at index, or delta places along.
func (s *Server) MoveWorkspace(id session.WorkspaceID, index, delta int) error {
	return s.announce(s.moveWorkspace(id, index, delta), Event{Kind: EventWorkspaceMoved, Workspace: id})
}

func (s *Server) moveWorkspace(id session.WorkspaceID, index, delta int) error {
	return s.rearrange(func(sess *session.Session) error {
		if delta != 0 {
			from, ok := sess.WorkspaceIndex(id)
			if !ok {
				return session.ErrNoSuchWorkspace
			}
			index = from + delta
		}
		return sess.MoveWorkspace(id, index)
	})
}

// MoveWorkspaceBlock moves several spaces together before another, or to the
// end.
func (s *Server) MoveWorkspaceBlock(ids []session.WorkspaceID, before session.WorkspaceID) error {
	err := s.rearrange(func(sess *session.Session) error { return sess.MoveWorkspaceBlock(ids, before) })
	if err == nil {
		for _, id := range ids {
			s.publish(Event{Kind: EventWorkspaceMoved, Workspace: id})
		}
	}
	return err
}

// SetAgentView puts a view on the agent list, or with nil takes it away; a
// source given with nil takes it away only if that source set it, as
// herdr's agent.view.clear does. It returns the view now in place.
func (s *Server) SetAgentView(v *agentview.View, clearSource string) *agentview.View {
	s.mu.Lock()
	switch {
	case v != nil:
		s.agentView = v
	case clearSource == "" || (s.agentView != nil && s.agentView.Source == clearSource):
		s.agentView = nil
	}
	now := s.agentView
	s.mu.Unlock()
	s.publish(Event{Kind: EventSessionChanged})
	return now
}

// announce publishes ev when err is nil, and passes err on.
func (s *Server) announce(err error, ev Event) error {
	if err == nil {
		s.publish(ev)
	}
	return err
}

// AnnounceWorktree tells plugins and scripts a worktree was made, opened as
// a space, or removed; the worktree work itself happens outside the server.
func (s *Server) AnnounceWorktree(kind EventKind, ws session.WorkspaceID) {
	s.publish(Event{Kind: kind, Workspace: ws})
}

// rearrange changes the session's shape and tells every client, including the
// ones that did not ask for it.
func (s *Server) rearrange(fn func(*session.Session) error) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	err := fn(s.session)
	s.mu.Unlock()
	if err == nil {
		s.publish(Event{Kind: EventSessionChanged})
	}
	return err
}

func (s *Server) AdjustSplit(id session.PaneID, side session.Side, cells int, area session.Rect) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return s.session.AdjustSplit(id, side, cells, area)
}

// Resize changes a pane's terminal size.
func (s *Server) Resize(id session.PaneID, size pty.Size) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	if !size.Valid() {
		return nil
	}
	return rt.resize(size)
}

// ScreenText returns a pane's screen as plain text.
func (s *Server) ScreenText(id session.PaneID) (string, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return "", err
	}
	return rt.screenText(), nil
}

// RenderedScreen returns a pane's screen as escape sequences that reproduce
// it. It is what a client draws from.
func (s *Server) RenderedScreen(id session.PaneID) ([]byte, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return nil, err
	}
	return rt.renderedScreen(), nil
}

// ScrolledScreen renders a pane as it was `offset` lines ago, and reports how
// far back it could go.
func (s *Server) ScrolledScreen(id session.PaneID, offset int) (ansi []byte, actual, history int, err error) {
	rt, err := s.runtime(id)
	if err != nil {
		return nil, 0, 0, err
	}
	ansi, actual, history = rt.scrolledScreen(offset)
	return ansi, actual, history, nil
}

// WithScreen runs fn against a pane's terminal.
//
// It is how a renderer reads cells without copying the screen. fn must not
// block or call back into the server: every byte that pane produces waits
// behind it.
func (s *Server) WithScreen(id session.PaneID, fn func(*vt.Screen)) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	rt.withScreen(fn)
	return nil
}

// PaneStatus reports what a client needs to show about a pane.
func (s *Server) PaneStatus(id session.PaneID) (PaneStatus, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return PaneStatus{}, err
	}
	return s.withName(rt.status()), nil
}

// withName puts a pane's given name in place of its program's title, as the
// snapshot does: a pane somebody called "sidebar" is "sidebar" to a script
// too, whatever the program in it calls itself.
func (s *Server) withName(st PaneStatus) PaneStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.session.Pane(st.ID); ok && p.Named {
		st.Title = p.Title
	}
	return st
}

// Statuses reports every pane, in no particular order.
func (s *Server) Statuses() []PaneStatus {
	s.mu.Lock()
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	s.mu.Unlock()

	out := make([]PaneStatus, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, s.withName(rt.status()))
	}
	return out
}

// Session runs fn against the session tree while holding the server's lock.
//
// The session is not safe for concurrent use, so this is the only way to read
// it. fn must not block or call back into the server, and must not keep the
// pointer.
func (s *Server) Session(fn func(*session.Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.session)
}

func (s *Server) runtime(id session.PaneID) (*paneRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	rt, ok := s.runtimes[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	return rt, nil
}

// --- goroutines ------------------------------------------------------------

// readPane copies a pane's output into its terminal until the process ends.
func (s *Server) readPane(rt *paneRuntime) {
	defer s.wg.Done()
	defer close(rt.gone)

	buf := make([]byte, readBuffer)
	for {
		n, err := rt.pty.Read(buf)
		if n > 0 {
			for _, text := range rt.write(buf[:n]) {
				s.publish(Event{Kind: EventPaneClipboard, Pane: rt.id, Data: text})
			}
			s.publish(Event{Kind: EventPaneOutput, Pane: rt.id})
		}
		if errors.Is(err, pty.ErrPaused) {
			// A handoff stopped this reader. It waits to hear how that went:
			// called off, and it reads on as though never interrupted; carried
			// out, and the terminal is another server's, so it leaves without
			// waiting for a process that has not ended and without reporting
			// an exit that has not happened.
			if rt.park() {
				continue
			}
			return
		}
		if err != nil {
			break
		}
	}

	waitErr := rt.pty.Wait()
	if deliberate := rt.markExited(waitErr); deliberate {
		// tend hung this pane up itself. Reporting the resulting "hangup" as
		// the process failing would put an error on every pane the user closes.
		return
	}

	ev := Event{Kind: EventPaneExited, Pane: rt.id}
	if waitErr != nil {
		ev.Err = waitErr.Error()
	}
	s.publish(ev)
	if rt.closeOnExit {
		// It was opened to run one thing, and that thing is done.
		_ = s.ClosePane(rt.id)
	}
}

// detectLoop re-examines panes on a tick.
//
// One loop serves every pane rather than one goroutine each: detection is
// per-pane work on a shared schedule, and a goroutine and timer per pane would
// scale the cost of an idle workspace with its size.
func (s *Server) detectLoop() {
	defer s.wg.Done()

	t := time.NewTicker(s.cfg.DetectInterval)
	defer t.Stop()

	adopt := time.NewTicker(s.cfg.AdoptInterval)
	defer adopt.Stop()

	// The check is frequent and the work is not: refreshTracking skips every
	// directory whose answer is still fresh, so this wakes each second and
	// runs git only for one it has not seen for trackingTTL. Waking on the
	// long period instead would leave a new space showing no drift for ten
	// seconds, which reads as "in step" rather than "not looked yet".
	track := time.NewTicker(trackingCheck)
	defer track.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.detectOnce()
		case <-adopt.C:
			s.adoptOnce()
		case <-track.C:
			s.trackOnce()
		case interval := <-s.retick:
			// The settings were re-read while this was running.
			t.Reset(interval)
		}
	}
}

// trackOnce updates how far each workspace has drifted from its upstream.
func (s *Server) trackOnce() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	dirs := make([]string, 0, len(s.session.Workspaces()))
	for _, w := range s.session.Workspaces() {
		dirs = append(dirs, w.Dir)
	}
	s.mu.Unlock()

	if !s.branches.refreshTracking(dirs) {
		return
	}
	// Clients read this from the session, so they are told to look again the
	// same way a change of agent state tells them.
	s.publish(Event{Kind: EventPaneState})
}

// adoptOnce points each pane at whichever agent is now running in it.
//
// A pane opened as a shell and then used to start an agent is the normal case,
// not the exception, so which agent a pane is watching cannot be settled once
// when it opens. It follows the terminal's foreground process instead, which
// also means quitting the agent puts the pane back to reporting nothing rather
// than leaving a stale state on screen.
func (s *Server) adoptOnce() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	catalog := s.catalog
	s.mu.Unlock()

	for _, rt := range runtimes {
		name, changed := rt.foregroundChanged()
		if !changed {
			continue
		}

		manifest, err := agent.ResolveManifest(catalog, "", name)
		if err != nil {
			continue
		}
		if manifest == nil {
			// The foreground is something with no rules — a shell, a pager, a
			// build. Fall back to what the pane was opened as, so a pane
			// started as an agent does not lose it the moment that agent runs
			// a command of its own. The caller's explicit choice is tried
			// first, since it outranks tend failing to recognise something.
			if rt.explicit != "" {
				manifest, _ = agent.ResolveManifest(catalog, rt.explicit, "")
			}
			if manifest == nil {
				manifest, _ = agent.ResolveManifest(catalog, "", rt.command)
			}
		}
		swapped, agentID := rt.adoptAgent(manifest)
		if !swapped {
			continue
		}

		st := rt.status()
		s.mu.Lock()
		if _, live := s.runtimes[rt.id]; live {
			_ = s.session.SetPaneState(rt.id, agentID, st.State)
		}
		s.mu.Unlock()

		s.publish(Event{
			Kind:  EventPaneState,
			Pane:  rt.id,
			State: st.State,
			Rule:  st.Rule,
		})
	}
}

func (s *Server) detectOnce() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	type pending struct {
		rt        *paneRuntime
		lastTitle string
	}
	work := make([]pending, 0, len(s.runtimes))
	for id, rt := range s.runtimes {
		work = append(work, pending{rt: rt, lastTitle: s.titles[id]})
	}
	s.mu.Unlock()

	for _, w := range work {
		// Each pane is examined under its own lock, so a slow one delays only
		// itself.
		obs := w.rt.poll(w.lastTitle)
		if !obs.titleChanged && !obs.stateChanged && !obs.mouseChanged {
			continue
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		if _, live := s.runtimes[w.rt.id]; !live {
			// The pane was closed while we were looking at it.
			s.mu.Unlock()
			continue
		}
		if obs.titleChanged {
			s.titles[w.rt.id] = obs.title
			if p, ok := s.session.Pane(w.rt.id); ok && !p.Named {
				// A pane the user named keeps that name: the program's own
				// title is a default, not an instruction.
				p.Title = obs.title
			}
		}
		detected := false
		if obs.stateChanged {
			detected = s.setPaneStateLocked(w.rt.id, obs.agent, obs.state)
		}
		s.mu.Unlock()
		if detected {
			s.publish(Event{Kind: EventAgentDetected, Pane: w.rt.id})
		}

		if obs.stateChanged || obs.mouseChanged {
			s.publish(Event{
				Kind:  EventPaneState,
				Pane:  w.rt.id,
				State: obs.state,
				Rule:  obs.rule,
				Agent: obs.agent,
			})
		}
	}
}
