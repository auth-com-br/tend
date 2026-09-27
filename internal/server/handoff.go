package server

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// A handoff replaces the server without ending what runs in its panes.
//
// Restarting loses every program: a restored pane is a new shell under the old
// scrollback, and an agent half way through a task is gone. A handoff keeps
// them. The terminals are open descriptors, and a descriptor can be given to
// another process — so the old server stops reading, writes down what each
// terminal looked like, gives the terminals to its replacement, and leaves
// without hanging anything up. The programs never learn that the process on the
// other end of their terminal changed.
//
// The order is what makes it safe, and it is herdr's: stop the readers first,
// so not a byte is consumed by a server that is about to forget it; hand over;
// wait to be told the replacement has everything; only then let go. Until that
// word arrives the old server can take it all back and carry on as though
// nothing had been tried.

// HandoffVersion is the manifest format this build writes and reads.
const HandoffVersion = 1

var (
	// ErrHandoffUnavailable means this server was not told how to start its
	// replacement.
	ErrHandoffUnavailable = errors.New("server: handoff is not available")
	// ErrHandingOff is what changes to the session are refused with while a
	// handoff is under way: the replacement is built from a description
	// already written, and a pane opened after it would belong to nobody.
	ErrHandingOff = errors.New("server: handing off to another server")
	// ErrHandoffVersion means the manifest came from a build this one does
	// not understand.
	ErrHandoffVersion = errors.New("server: handoff manifest version not understood")
)

// parkTimeout is how long a reader is given to notice it has been paused. It
// is interrupted rather than asked, so this is the scheduler's time and not the
// pane's; running out of it means something is wrong, and the handoff is
// called off rather than carried out on a guess.
const parkTimeout = 2 * time.Second

// handoffHistory is how much scrollback crosses over with each pane.
const handoffHistory = historyLines

// HandoffManifest is everything the replacement needs besides the terminals
// themselves.
type HandoffManifest struct {
	Version int              `json:"version"`
	Session session.Snapshot `json:"session"`
	// Panes is in the order the terminals are handed over.
	Panes []HandoffPane `json:"panes"`
	// APIListener reports that the automation socket's listener follows the
	// last pane's terminal. It is set by whoever passes the descriptors, and
	// absent from a server older than that socket — whose replacement then
	// opens one itself.
	APIListener bool `json:"api_listener,omitempty"`
	// WindowTitle is a title a script set (tend terminal title set), which
	// lives only in the server and would otherwise end with the old one.
	WindowTitle string `json:"window_title,omitempty"`
}

// HandoffPane is one pane's live half, written down.
type HandoffPane struct {
	ID   uint64 `json:"id"`
	Pid  int    `json:"pid"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
	// Command and Explicit are what detection falls back to, as in the
	// runtime they were read from.
	Command  string `json:"command,omitempty"`
	Explicit string `json:"explicit,omitempty"`
	// Resume is terminal output that, written to an empty terminal, leaves it
	// in the state the old one was in: scrollback, both screens, modes, cursor.
	Resume []byte `json:"resume"`
	// AgentSession is the conversation the pane's agent is in. Without it the
	// new server knows none, and the next time it writes the state file it
	// writes over the one the old server saved: a restart after a handoff
	// then starts the agent fresh instead of carrying the conversation on.
	// Optional, so a manifest from a build without it still reads.
	AgentSession *agent.PersistedSession `json:"agent_session,omitempty"`
	// CloseOnExit is a pane opened to run one command, which still closes
	// when it ends on the other side of a handoff.
	CloseOnExit bool `json:"close_on_exit,omitempty"`
}

// Handoff is one attempt, from the moment the readers stop.
type Handoff struct {
	Manifest HandoffManifest
	// Files are duplicates of the pane terminals, in Manifest.Panes order.
	// Whoever holds the Handoff closes them; the server's own descriptors are
	// separate and are let go of by CommitHandoff.
	Files []*os.File

	parked []*paneRuntime

	// Binary is the tend to replace this server with, when the one asking
	// named it — herdr's live handoff names the updated executable. Empty
	// means the binary at this server's own path.
	Binary string
}

// CloseFiles closes the duplicated terminals. The panes are unaffected: the
// server still holds its own descriptor for each.
func (h *Handoff) CloseFiles() {
	for _, f := range h.Files {
		_ = f.Close()
	}
	h.Files = nil
}

// BeginHandoff stops every pane's reader and describes the session.
//
// It ends in one of two ways, and the caller owes it one of them: CommitHandoff
// once the replacement has taken over, or AbortHandoff if it did not.
func (s *Server) BeginHandoff() (*Handoff, error) {
	// A popup is not carried across: it is in no layout to put it back
	// into, and herdr's handoff leaves it out too. Closed first, so its
	// process is not left behind as a pane nobody has.
	if p, ok := s.PopupOpen(); ok {
		s.closePopupIf(p.Pane)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	if s.handingOff {
		s.mu.Unlock()
		return nil, ErrHandingOff
	}
	s.handingOff = true
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	s.mu.Unlock()
	// The replacement reads the activity record when it starts and carries
	// on the runs it finds there. A round of this server's recorder already
	// under way is let finish first, and no other begins while handing off,
	// so nothing is written after the replacement has read.
	s.waitForActivityRound()

	h := &Handoff{}
	for _, rt := range runtimes {
		rt.drainVerdict()
		rt.pty.Pause()
	}
	for _, rt := range runtimes {
		select {
		case <-rt.parked:
			h.parked = append(h.parked, rt)
		case <-rt.gone:
			// Its process ended on its own, before or during the pause. There
			// is no terminal left to hand over, and the replacement drops the
			// pane as a restore drops one it cannot start.
		case <-time.After(parkTimeout):
			s.AbortHandoff(h)
			return nil, fmt.Errorf("server: pane %d did not stop reading", rt.id)
		}
	}

	// Only now, with every reader stopped, is what the terminals show what the
	// replacement will continue from.
	dirs := make(map[session.PaneID]string, len(h.parked))
	for _, rt := range h.parked {
		if dir := rt.pty.Cwd(); dir != "" {
			dirs[rt.id] = dir
		}
		f, err := rt.pty.Dup()
		if err != nil {
			s.AbortHandoff(h)
			return nil, fmt.Errorf("server: duplicating pane %d's terminal: %w", rt.id, err)
		}
		h.Files = append(h.Files, f)
		h.Manifest.Panes = append(h.Manifest.Panes, rt.handoffPane())
	}

	s.mu.Lock()
	h.Manifest.Version = HandoffVersion
	h.Manifest.Session = s.session.Snapshot(dirs)
	h.Manifest.WindowTitle = s.windowTitle
	s.mu.Unlock()
	return h, nil
}

// AbortHandoff takes the session back: readers carry on from where they
// stopped, and nothing a pane wrote in the meantime was lost, because nobody
// read it.
func (s *Server) AbortHandoff(h *Handoff) {
	h.CloseFiles()

	s.mu.Lock()
	runtimes := make([]*paneRuntime, 0, len(s.runtimes))
	for _, rt := range s.runtimes {
		runtimes = append(runtimes, rt)
	}
	s.handingOff = false
	s.mu.Unlock()

	// Every pane, not only the ones that parked: a reader that was slow to
	// notice the pause may yet notice it, and must find the answer waiting.
	for _, rt := range runtimes {
		rt.pty.Resume()
		rt.decide(true)
	}
}

// CommitHandoff lets go of everything without ending any of it, and closes the
// server. The panes' processes are somebody else's now.
func (s *Server) CommitHandoff(h *Handoff) error {
	h.CloseFiles()

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

	// No state is saved on the way out, unlike Close. The file belongs to the
	// replacement from the moment it answered, and it already holds a newer
	// account of the session than this server does.
	close(s.done)
	for _, rt := range runtimes {
		rt.setClosing()
		rt.decide(false)
		// Released, not closed: closing hangs the process up, and the process
		// is the whole point.
		_ = rt.pty.Release()
	}
	for _, c := range conns {
		_ = c.conn.Close()
	}
	s.wg.Wait()
	s.events.closeAll()
	return nil
}

// Replace carries out a handoff through the function the server was configured
// with, and reports whether the replacement took over. It does not commit:
// the caller does, once it has told whoever asked. Until then this server is
// still the one answering, and a caller that never commits leaves it that way.
func (s *Server) Replace(binary string) (*Handoff, error) {
	if s.cfg.Replace == nil {
		return nil, ErrHandoffUnavailable
	}
	h, err := s.BeginHandoff()
	if err != nil {
		return nil, err
	}
	h.Binary = binary
	if err := s.cfg.Replace(h); err != nil {
		s.AbortHandoff(h)
		return nil, fmt.Errorf("server: the replacement did not take over: %w", err)
	}
	return h, nil
}

// handoffPane describes a parked pane.
func (rt *paneRuntime) handoffPane() HandoffPane {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	cols, rows := rt.screen.Size()
	hp := HandoffPane{
		ID:       uint64(rt.id),
		Pid:      rt.pty.Pid(),
		Cols:     uint16(cols),
		Rows:     uint16(rows),
		Command:  rt.command,
		Explicit: rt.explicit,
		Resume:   vt.RenderResume(rt.screen, handoffHistory),

		CloseOnExit: rt.closeOnExit,
	}
	if p, ok := rt.arbiter.Session(); ok {
		hp.AgentSession = &p
	}
	return hp
}

// NewFromHandoff starts a server that continues another's session.
//
// files are the pane terminals in manifest order, and are this server's from
// here on, whatever happens. ready is called once everything has been adopted
// and before a single byte is read from any terminal: it is where the old
// server is told it may go. If it fails, the old server is taking the session
// back, and this one must not have consumed any of its panes' output.
func NewFromHandoff(cfg Config, m HandoffManifest, files []*os.File, ready func() error) (*Server, error) {
	closeAll := func() {
		for _, f := range files {
			if f != nil {
				_ = f.Close()
			}
		}
	}
	if m.Version != HandoffVersion {
		closeAll()
		return nil, fmt.Errorf("%w: manifest is version %d, this build reads %d",
			ErrHandoffVersion, m.Version, HandoffVersion)
	}
	if len(files) != len(m.Panes) {
		closeAll()
		return nil, fmt.Errorf("server: manifest names %d panes and %d terminals came with it",
			len(m.Panes), len(files))
	}
	restored, err := session.Restore(m.Session)
	if err != nil {
		closeAll()
		return nil, err
	}
	s, err := build(cfg)
	if err != nil {
		closeAll()
		return nil, err
	}
	s.session = restored
	s.windowTitle = m.WindowTitle

	adopted := make(map[session.PaneID]bool, len(m.Panes))
	runtimes := make([]*paneRuntime, 0, len(m.Panes))
	for i, p := range m.Panes {
		id := session.PaneID(p.ID)
		if _, ok := s.session.Pane(id); !ok {
			_ = files[i].Close()
			continue // a terminal for a pane the session does not have
		}
		term, err := pty.Adopt(files[i], p.Pid)
		if err != nil {
			for _, rt := range runtimes {
				_ = rt.pty.Release()
			}
			for _, f := range files[i:] {
				_ = f.Close()
			}
			return nil, fmt.Errorf("server: adopting pane %d: %w", p.ID, err)
		}

		var manifest *detect.Manifest
		if p.Command != "" || p.Explicit != "" {
			// A manifest that no longer resolves costs the pane its detector,
			// not its place.
			manifest, _ = agent.ResolveManifest(s.catalog, p.Explicit, p.Command)
		}
		size := pty.Size{Cols: p.Cols, Rows: p.Rows}
		if !size.Valid() {
			size = s.cfg.DefaultSize
		}
		rt := newPaneRuntime(id, term, size, manifest, s.cfg.Scrollback, p.Command, p.Explicit, s.knownAgent)
		if p.AgentSession != nil {
			rt.arbiter.RestoreSession(*p.AgentSession)
		}
		rt.closeOnExit = p.CloseOnExit
		rt.write(p.Resume)
		s.runtimes[id] = rt
		s.titles[id] = ""
		adopted[id] = true
		runtimes = append(runtimes, rt)
	}

	// A pane whose process ended before it could be handed over has a record
	// and nothing behind it.
	for _, ws := range m.Session.Workspaces {
		for _, tab := range ws.Tabs {
			for _, p := range tab.Panes {
				if id := session.PaneID(p.ID); !adopted[id] {
					_ = s.session.ClosePane(id)
				}
			}
		}
	}

	if ready != nil {
		if err := ready(); err != nil {
			for _, rt := range runtimes {
				_ = rt.pty.Release()
			}
			return nil, fmt.Errorf("server: telling the old server: %w", err)
		}
	}

	for _, rt := range runtimes {
		s.wg.Add(1)
		go s.readPane(rt)
	}
	s.startLoops()
	return s, nil
}
