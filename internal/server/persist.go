package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// A stopped server used to take the whole session with it: every space, tab
// and split, gone, and an empty screen in their place. The arrangement is
// written to a file now and read back when a server starts.
//
// Two files, as herdr has them, because they change at different rates and
// cost different amounts. The structure is small and changes whenever somebody
// splits a pane, so it is checked every second and written when it differs.
// The history is every pane's scrollback, which is large and changes with
// every line of output, so it is written rarely and at shutdown.
//
// What comes back is the place, not the program: processes die with the
// server, and a restored pane is a new process started where the old one was,
// under the old one's scrollback.

const (
	// structureInterval is how often the arrangement is compared with what
	// was last written.
	structureInterval = time.Second
	// historyInterval is how often the scrollback is written while running,
	// so that a crash loses minutes of context rather than all of it.
	historyInterval = 30 * time.Second
	// historyLines bounds how much of each pane's past is kept. The file holds
	// every pane, and the last couple of thousand lines are the ones anybody
	// scrolls back to.
	historyLines = 2000

	statePerm    = 0o600 // terminal contents: private, like the socket
	stateDirPerm = 0o700
)

// savedHistory is the second file: what each pane had said.
type savedHistory struct {
	Version int                    `json:"version"`
	Panes   map[uint64]paneHistory `json:"panes"`
}

type paneHistory struct {
	ANSI  string `json:"ansi"`
	Lines int    `json:"lines"`
}

// historyFile is where the scrollback goes, beside the structure.
func historyFile(stateFile string) string {
	return strings.TrimSuffix(stateFile, ".json") + ".history.json"
}

// restore rebuilds the session from the state file, if there is one.
//
// A file that cannot be used is reported and then ignored. Refusing to start
// over it would turn a damaged file into a server nobody can run, and an empty
// session is a better place to be than no session.
func (s *Server) restore() {
	if s.cfg.StateFile == "" {
		return
	}
	data, err := os.ReadFile(s.cfg.StateFile)
	if err != nil {
		return // no file is the ordinary case: nothing to come back to
	}

	var snap session.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		s.logf("ignoring %s: %v", s.cfg.StateFile, err)
		return
	}
	restored, err := session.Restore(snap)
	if err != nil {
		s.logf("ignoring %s: %v", s.cfg.StateFile, err)
		return
	}

	history := loadHistory(historyFile(s.cfg.StateFile))

	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = restored
	s.lastSaved = data

	// Conversations already being resumed in this pass: two panes that were
	// in the same one must not both be started back in it (herdr's
	// dedupe_key), or the agent is running twice over one history.
	resumed := make(map[string]bool)
	for _, ws := range snap.Workspaces {
		for _, tab := range ws.Tabs {
			for _, p := range tab.Panes {
				s.restartPaneLocked(p, history[p.ID], resumed)
			}
		}
	}
}

// shell is what a pane runs when there is nothing else.
func (s *Server) shell() []string {
	if len(s.cfg.Shell) > 0 {
		return append([]string(nil), s.cfg.Shell...)
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	return []string{"/bin/sh"}
}

// restartPaneLocked starts a restored pane's process, under its old scrollback.
//
// A directory can be gone by the time this runs — a deleted worktree, an
// unmounted disk — and that is no reason to lose the pane, so it is tried again
// without one. A command that cannot be started at all is a pane that cannot
// exist, and it is taken out of the session rather than left as a record with
// nothing behind it.
func (s *Server) restartPaneLocked(p session.PaneSnapshot, past paneHistory, resumed map[string]bool) {
	id := session.PaneID(p.ID)
	spec := PaneSpec{
		Command: p.Command,
		Dir:     p.Dir,
		Title:   p.Title,
		Named:   p.Named,
		Agent:   p.Agent,
		history: []byte(past.ANSI),

		CloseOnExit: p.CloseOnExit,
	}

	// An agent whose conversation is known is started back in it. Restoring
	// the place and not the conversation gives the user an agent that has
	// forgotten everything, sitting in the directory where it used to know.
	if p.Session != nil {
		persisted := agent.PersistedSession{
			Source: p.Session.Source, Agent: p.Session.Agent,
			Session: agent.SessionRef{ID: p.Session.ID, Path: p.Session.Path},
		}
		key := p.Session.Source + "\x00" + p.Session.Agent + "\x00" + p.Session.ID + "\x00" + p.Session.Path
		if argv, ok := agent.Resume(persisted); ok {
			if resumed[key] {
				// Another pane is already being started back in this
				// conversation. This one gets a shell: its saved command
				// is the resume itself, and running it would be the same
				// agent twice over one history.
				spec.Command, spec.Agent = s.shell(), ""
			} else {
				resumed[key] = true
				spec.Command = argv
				spec.Agent = p.Session.Agent
				spec.resume = &persisted
			}
		}
	}

	// The record follows what is actually running: leaving the old command in
	// it would have the next snapshot save a command the pane is not running,
	// and the restore after that would resume from the wrong thing.
	if p.Session != nil && len(spec.Command) > 0 {
		if pane, ok := s.session.Pane(id); ok {
			pane.Command = spec.Command
		}
	}

	start := s.startLocked
	if d, ok := s.dormantFor(spec); ok {
		// Asleep: a shell where the agent was, until it is focused
		// (dormant.go). The record keeps what wakes it, so the restore
		// after this one finds the agent again.
		if pane, ok := s.session.Pane(id); ok {
			pane.Command = d.Argv
		}
		start = func(id session.PaneID, spec PaneSpec) error { return s.startDormantLocked(id, spec, d) }
	}
	err := start(id, spec)
	if err != nil && spec.Dir != "" {
		spec.Dir = ""
		err = start(id, spec)
	}
	if err != nil {
		s.logf("pane %d could not be restored: %v", p.ID, err)
		_ = s.session.ClosePane(id)
	}
}

func loadHistory(path string) map[uint64]paneHistory {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var h savedHistory
	if err := json.Unmarshal(data, &h); err != nil || h.Version != session.SnapshotVersion {
		return nil
	}
	return h.Panes
}

// persistLoop keeps the files current until the server stops.
func (s *Server) persistLoop() {
	defer s.wg.Done()

	structure := time.NewTicker(structureInterval)
	defer structure.Stop()
	history := time.NewTicker(historyInterval)
	defer history.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-structure.C:
			s.saveStructure()
		case <-history.C:
			s.saveHistory()
		}
	}
}

// saveStructure writes the arrangement when it differs from what is on disk.
//
// Compared as bytes rather than tracked by a dirty flag. A flag has to be set
// by every method that changes the session, and the one that forgets is a
// change that silently does not survive a restart; the comparison cannot
// forget, and the snapshot is small enough that taking it every second costs
// nothing anyone will measure.
func (s *Server) saveStructure() {
	if s.cfg.StateFile == "" {
		return
	}

	s.mu.Lock()
	runtimes := make(map[session.PaneID]*paneRuntime, len(s.runtimes))
	for id, rt := range s.runtimes {
		runtimes[id] = rt
	}
	s.mu.Unlock()

	// Asked outside the lock: it reads /proc, and nothing that touches the
	// filesystem belongs under the lock every pane operation needs.
	dirs := make(map[session.PaneID]string, len(runtimes))
	for id, rt := range runtimes {
		if dir := rt.pty.Cwd(); dir != "" {
			dirs[id] = dir
		}
	}

	// The conversations each pane's agent is in, so a restored pane can
	// carry one on rather than start over.
	sessions := make(map[session.PaneID]session.AgentSession, len(runtimes))
	for id, rt := range runtimes {
		rt.mu.Lock()
		p, ok := rt.arbiter.Session()
		rt.mu.Unlock()
		if ok {
			sessions[id] = session.AgentSession{
				Source: p.Source, Agent: p.Agent, ID: p.Session.ID, Path: p.Session.Path,
			}
		}
	}

	s.mu.Lock()
	snap := s.session.SnapshotWith(dirs, sessions)
	last := s.lastSaved
	s.mu.Unlock()

	if len(snap.Workspaces) == 0 && len(snap.Tasks) == 0 {
		// Nothing left to come back to. Leaving the old file would bring back
		// a session the user closed on purpose. The tasks are kept with no
		// space open: the planner is the work ahead, and closing the last
		// space is not deleting it.
		if last != nil {
			_ = os.Remove(s.cfg.StateFile)
			_ = os.Remove(historyFile(s.cfg.StateFile))
			s.mu.Lock()
			s.lastSaved = nil
			s.mu.Unlock()
		}
		return
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil || bytes.Equal(data, last) {
		return
	}
	if err := writeAtomic(s.cfg.StateFile, data); err != nil {
		s.logf("saving the session: %v", err)
		return
	}
	s.mu.Lock()
	s.lastSaved = data
	s.mu.Unlock()
}

// saveHistory writes what every pane has said.
func (s *Server) saveHistory() {
	if s.cfg.StateFile == "" {
		return
	}
	s.mu.Lock()
	runtimes := make(map[session.PaneID]*paneRuntime, len(s.runtimes))
	for id, rt := range s.runtimes {
		runtimes[id] = rt
	}
	s.mu.Unlock()
	if len(runtimes) == 0 {
		return
	}

	out := savedHistory{Version: session.SnapshotVersion, Panes: make(map[uint64]paneHistory, len(runtimes))}
	for id, rt := range runtimes {
		rt.withScreen(func(screen *vt.Screen) {
			ansi, lines := vt.RenderHistory(screen, historyLines)
			if lines > 0 {
				out.Panes[uint64(id)] = paneHistory{ANSI: string(ansi), Lines: lines}
			}
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return
	}
	if err := writeAtomic(historyFile(s.cfg.StateFile), data); err != nil {
		s.logf("saving the scrollback: %v", err)
	}
}

// writeAtomic replaces a file in one step, so a crash or a full disk part way
// through leaves the previous version rather than half of the next one.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(statePerm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// logf reports something that went wrong with persistence. It goes to stderr,
// which for a background server is its log file.
func (s *Server) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[tend] "+format+"\n", args...)
}
