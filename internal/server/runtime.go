package server

import (
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// paneRuntime is one pane's live half: its process, its terminal, and the
// detector reading it. The session record holding the pane's identity and
// place in the layout lives separately, and the two are paired by id.
//
// Its mutex guards only this pane. The server's lock guards the session
// structure. Lock order is always server then runtime, never the reverse —
// which is why the terminal's own callbacks record into the runtime and let
// the detection loop carry the result up, rather than reaching for the
// session while a pane is parsing bytes.
type paneRuntime struct {
	id      session.PaneID
	agentID string

	// command is what the pane was opened with and explicit is the agent the
	// caller named, if any. Which agent a pane is watching is not settled when
	// it opens — the foreground program decides — and these are what that
	// falls back to when the foreground matches no manifest. An explicit
	// choice is never overridden by one: the user saying "this pane is claude"
	// outranks tend failing to recognise what is in front of it.
	command  string
	explicit string

	pty *pty.Pty

	mu       sync.Mutex
	screen   *vt.Screen
	detector *agent.Detector
	// arbiter weighs what hooks report against what the detector reads, and
	// shown is its last answer that was passed on — so a change is noticed
	// whichever side it came from.
	arbiter   *agent.Arbiter
	shown     agent.Effective
	shownRule string
	// shownPresentation is what a hook said to show beside the agent: the
	// name it goes by, the values worth a glance, the labels for its states.
	shownPresentation agent.Presentation
	// spending is what the agent's conversation has spent, kept up to
	// date by the activity recorder (usage.go); nil with no agent.
	spending *proto.PaneUsage
	// resumeArmed: tell the agent to continue when its usage limit resets.
	resumeArmed bool
	dirty       bool
	// statusRev is the last OSC 7501 report passed to the arbiter
	// (programstatus.go).
	statusRev uint64
	title     string
	// foreground is the program last seen in charge of the terminal, so the
	// costly part — resolving and swapping the detector — happens only when
	// it actually changes.
	foreground string
	// mouse is the last reported state of the pane program's mouse reporting.
	// Whether a click belongs to the pane or to the client turns on it, so a
	// client holding a stale answer sends the wheel to the wrong place.
	mouse bool
	// wheelKeys and appCursor are what the client was last told about the
	// wheel, so a change is told.
	wheelKeys, appCursor bool
	running              bool
	// closeOnExit closes the pane when its program ends, for a pane opened to
	// run one thing — a user's command, the scrollback editor — whose frame
	// would otherwise sit there saying "exited" until closed by hand. Set
	// before the reader starts and never changed.
	closeOnExit bool
	// clipboard holds copies the program asked for that have not been passed
	// on yet.
	clipboard [][]byte
	// replies are what the terminal answered the program while parsing its
	// output — a cursor report, device attributes, the OSC 7501 probe —
	// written back to it by the reader once the lock is let go.
	replies []byte
	closing bool
	exitErr string

	// dormant is set while the pane's agent sleeps (dormant.go): a shell
	// runs where it was, and this is how to bring it back. idleSince is
	// when the agent last became idle, zero while it is not, which is what
	// putting it to sleep by itself goes by.
	dormant   *dormantAgent
	idleSince time.Time

	// parked is how the reader says it has stopped for a handoff, verdict is
	// how it hears whether to carry on, and gone closes when the reader has
	// returned for good. Each holds one, so neither side waits on the other
	// being there at the same instant.
	parked  chan struct{}
	verdict chan bool
	gone    chan struct{}
}

func newPaneRuntime(
	id session.PaneID,
	p *pty.Pty,
	size pty.Size,
	manifest *detect.Manifest,
	scrollback int,
	command, explicit string,
	known func(string) bool,
) *paneRuntime {
	rt := &paneRuntime{
		id:       id,
		command:  command,
		explicit: explicit,
		pty:      p,
		screen:   vt.NewScreen(int(size.Cols), int(size.Rows), scrollback),
		running:  true,
		parked:   make(chan struct{}, 1),
		verdict:  make(chan bool, 1),
		gone:     make(chan struct{}),
		arbiter:  agent.NewArbiter(known),
	}
	if manifest != nil {
		rt.agentID = manifest.ID
		rt.detector = agent.NewDetector(manifest)
	}
	rt.arbiter.Observe(rt.agentID, detect.StateUnknown, false, time.Now())
	rt.shown = rt.arbiter.Effective()
	// The terminal reports a title from inside Write, which runs under this
	// runtime's lock. Recording it here and letting the detection loop apply
	// it keeps that callback from reaching for the session lock.
	rt.screen.OnTitle = func(title string) { rt.title = title }
	// Same arrangement for a clipboard write: recorded under this lock, and
	// carried out to subscribers by the reader once the write returns.
	rt.screen.OnClipboard = func(text []byte) {
		rt.clipboard = append(rt.clipboard, append([]byte(nil), text...))
	}
	// And for the terminal's answers to the program's questions. They were
	// never wired: the screen answered, and nothing carried the answer to
	// the program, so every query a program in a pane made went unanswered
	// — Claude Code's OSC 7501 probe among them, which is why it never told
	// tend its state (#50).
	rt.screen.Reply = func(b []byte) { rt.replies = append(rt.replies, b...) }
	return rt
}

// park reports that the reader has stopped, and waits to be told whether to
// carry on.
func (rt *paneRuntime) park() (carryOn bool) {
	select {
	case rt.parked <- struct{}{}:
	default: // already said so, and nobody has collected it
	}
	return <-rt.verdict
}

// decide answers a parked reader, or leaves the answer for one about to park.
func (rt *paneRuntime) decide(carryOn bool) {
	select {
	case rt.verdict <- carryOn:
	default:
	}
}

// drainVerdict discards what an earlier handoff left behind. An answer nobody
// collected would otherwise be taken for the answer to this one.
func (rt *paneRuntime) drainVerdict() {
	select {
	case <-rt.verdict:
	default:
	}
	select {
	case <-rt.parked:
	default:
	}
}

// write feeds terminal output to the screen, and returns any clipboard writes
// the output contained.
func (rt *paneRuntime) write(b []byte) [][]byte {
	copies, _ := rt.writeOutput(b)
	return copies
}

// writeOutput is write for what the program is printing now, which also
// says what the terminal answered it. Output replayed — the history put back
// above a restored pane, a handoff's screen — goes through write, whose
// answers are dropped: they are to questions asked of a process that is
// gone.
func (rt *paneRuntime) writeOutput(b []byte) (copies [][]byte, replies []byte) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, _ = rt.screen.Write(b)
	rt.dirty = true
	copies, rt.clipboard = rt.clipboard, nil
	replies, rt.replies = rt.replies, nil
	return copies, replies
}

// settleLocked asks the arbiter what the pane should be shown as, and reports
// whether that differs from what was last passed on. The caller holds the lock.
func (rt *paneRuntime) settleLocked() (eff agent.Effective, rule string, changed bool) {
	eff = rt.arbiter.Effective()
	switch {
	case eff.Source != "":
		rule = "hook:" + eff.Source
	case rt.detector != nil:
		rule = rt.detector.Rule()
	}
	if eff == rt.shown && rule == rt.shownRule {
		return eff, rule, false
	}
	switch {
	case eff.State != detect.StateIdle || eff.Agent == "":
		rt.idleSince = time.Time{}
	case rt.idleSince.IsZero() || rt.shown.State != detect.StateIdle:
		rt.idleSince = time.Now()
	}
	rt.shown, rt.shownRule = eff, rule
	return eff, rule, true
}

// hooked runs fn against the arbiter — a report, a release — and settles.
func (rt *paneRuntime) hooked(fn func(*agent.Arbiter) bool) (accepted bool, eff agent.Effective, rule string, changed bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	accepted = fn(rt.arbiter)
	eff, rule, changed = rt.settleLocked()
	return accepted, eff, rule, changed
}

// observation is what one poll of a pane found.
type observation struct {
	title        string
	titleChanged bool

	// agent is who the pane is shown as, which a hook may have decided.
	agent        string
	state        detect.State
	rule         string
	stateChanged bool

	// mouseChanged reports that the pane turned mouse reporting on or off.
	mouseChanged bool
}

// poll runs detection if the screen changed since the last look.
//
// An unchanged screen costs nothing: parsing output is cheap, re-running every
// rule over it is not, and this runs per pane on every tick.
func (rt *paneRuntime) poll(lastTitle string) observation {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var obs observation
	if rt.title != lastTitle {
		obs.title = rt.title
		obs.titleChanged = true
	}
	// Checked before the early exit below, and for every pane rather than
	// only the ones with a detector: a plain shell running an editor asks for
	// the mouse too, and nothing else tells a client about it.
	if mouse := rt.screen.Modes().Mouse != vt.MouseOff; mouse != rt.mouse {
		rt.mouse = mouse
		obs.mouseChanged = true
	}
	// What the wheel does changes with the alternate screen too: a pager or
	// a full-screen agent opening is when the wheel starts meaning arrows.
	if keys, app := rt.screen.WheelKeys(), rt.screen.Modes().ApplicationCur; keys != rt.wheelKeys || app != rt.appCursor {
		rt.wheelKeys, rt.appCursor = keys, app
		obs.mouseChanged = true
	}
	if rt.dirty && rt.detector != nil {
		rt.dirty = false
		res, _ := rt.detector.Update(rt.screen)
		// The detector's own conclusion, not this reading's: an ambiguous
		// screen leaves the previous state standing, and an ambiguous reading
		// is no evidence of a blocker either.
		rt.arbiter.Observe(rt.agentID, rt.detector.State(),
			res.VisibleBlocker && !res.SkipStateUpdate, time.Now())
	}
	rt.programStatusLocked(time.Now())
	if eff, rule, changed := rt.settleLocked(); changed {
		obs.agent, obs.state, obs.rule = eff.Agent, eff.State, rule
		obs.stateChanged = true
	}
	return obs
}

// resize changes the terminal and tells the process about it.
func (rt *paneRuntime) resize(size pty.Size) error {
	rt.mu.Lock()
	rt.screen.Resize(int(size.Cols), int(size.Rows))
	rt.mu.Unlock()
	// Resizing the pty delivers SIGWINCH, so it must happen outside the lock:
	// the process may write its redraw before the call returns, and that write
	// needs the lock we would otherwise still be holding.
	return rt.pty.Resize(size)
}

// adoptAgent points the pane at a different manifest, or at none.
//
// The detector is rebuilt rather than reused: its memory of the last state
// belongs to the agent it was watching, and carrying that across would report
// the new one as already being in a state it has never been in.
func (rt *paneRuntime) adoptAgent(m *detect.Manifest) (changed bool, agentID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	next := ""
	if m != nil {
		next = m.ID
	}
	if next == rt.agentID {
		return false, rt.agentID
	}
	rt.agentID = next
	if m == nil {
		rt.detector = nil
	} else {
		rt.detector = agent.NewDetector(m)
	}
	// The screen has not changed, but what is being looked for has.
	rt.dirty = true
	// Told at once rather than at the next reading: a hook's report about the
	// agent that has just left must not survive until the screen next changes.
	rt.arbiter.Observe(next, detect.StateUnknown, false, time.Now())
	eff, _, _ := rt.settleLocked()
	return true, eff.Agent
}

// foregroundChanged reports the program in charge of the terminal, and whether
// it differs from the last look.
func (rt *paneRuntime) foregroundChanged() (string, bool) {
	name := rt.pty.Foreground()

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if name == rt.foreground {
		return name, false
	}
	rt.foreground = name
	return name, true
}

// setClosing records that tend is stopping this pane on purpose, so that the
// hangup it is about to receive is not reported as the process failing.
func (rt *paneRuntime) setClosing() {
	rt.mu.Lock()
	rt.closing = true
	rt.mu.Unlock()
}

// markExited records that the process ended, and reports whether the exit was
// deliberate.
func (rt *paneRuntime) markExited(err error) (deliberate bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.running = false
	if rt.closing {
		return true
	}
	if err != nil {
		rt.exitErr = err.Error()
	}
	return false
}

// status is a copy of what a client needs to show about the pane.
func (rt *paneRuntime) status() PaneStatus {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	modes := rt.screen.Modes()
	st := PaneStatus{
		Mouse:        modes.Mouse != vt.MouseOff,
		MouseDrag:    modes.Mouse >= vt.MouseButtonEvent,
		MouseMotion:  modes.Mouse >= vt.MouseAnyEvent,
		MouseSGR:     modes.MouseEncoding == vt.MouseEncodingSGR || modes.MouseEncoding == vt.MouseEncodingSGRPixels,
		WheelKeys:    rt.screen.WheelKeys(),
		AppCursor:    modes.ApplicationCur,
		Graphics:     rt.screen.KittyRevision(),
		ID:           rt.id,
		Title:        rt.title,
		Agent:        rt.shown.Agent,
		State:        rt.shown.State,
		Presentation: rt.shownPresentation,
		Usage:        rt.spending,
		Rule:         rt.shownRule,
		Message:      rt.shown.Message,
		Running:      rt.running,
		ExitErr:      rt.exitErr,
		Pid:          rt.pty.Pid(),
	}
	if rt.dormant != nil {
		st.Dormant = rt.dormant.Agent
	}
	return st
}

// screenText returns the pane's screen as plain text, which is what the CLI
// and the detector read.
func (rt *paneRuntime) screenText() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return agent.ScreenText(rt.screen)
}

// renderedScreen encodes the pane's terminal as the escape sequences that
// reproduce it, colours and all.
//
// This is what goes on the wire rather than plain text: a client drawing a
// pane needs the styling, and re-rendering here means the client reuses the
// same parser it already has instead of the two sides agreeing on a cell
// format.
func (rt *paneRuntime) renderedScreen() []byte {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return vt.RenderScreen(rt.screen)
}

// scrolledScreen renders a view of the pane `offset` lines back through its
// history, and reports how far back it could actually go.
func (rt *paneRuntime) scrolledScreen(offset int) (ansi []byte, actual, history int) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	history = rt.screen.MainGrid().HistoryLen()
	actual = min(max(offset, 0), history)
	return vt.RenderScrolled(rt.screen, actual), actual, history
}

// wantsMouse reports whether the pane's own program asked for mouse reporting,
// which decides whether a click belongs to it or to tend.
func (rt *paneRuntime) wantsMouse() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.screen.Modes().Mouse != vt.MouseOff
}

// withScreen runs fn against the pane's terminal while holding its lock.
//
// It exists so a renderer can read cells without the screen being copied, and
// it is the only way out of this package to the terminal. fn must not block,
// call back into the server, or keep the screen: every byte the pane produces
// waits behind it.
func (rt *paneRuntime) withScreen(fn func(*vt.Screen)) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	fn(rt.screen)
}
