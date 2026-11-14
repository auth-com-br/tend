package server

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// Dormant agents are tend's own; herdr resumes every agent of a restored
// session as soon as it knows the terminal's size, the ones in tabs nobody
// is looking at included (agent_resume.rs). With a dozen agents across a
// few companies that is a dozen Node processes of several hundred
// megabytes, most of them not wanted that day, and a machine out of memory.
//
// A dormant agent is a pane running a shell where the agent would be, and
// the command that brings the agent back in its conversation. A shell is
// what a restored pane runs anyway, costs little, and keeps the pane a pane
// like any other — handoff, persistence, the working directory and the
// drawing all assume a process, and none of them has to learn a pane
// without one. Waking types the command into the shell, as agent.start
// starts an agent; the agent then runs as the shell's child, and quitting
// it leaves the prompt rather than an exited pane.
//
// An agent goes to sleep when the server restarts (Config.SleepRestored),
// when asked (pane.sleep), and, idle and unwatched for Config.SleepAfter, by
// itself. It wakes when a client focuses its pane, or when asked
// (pane.wake).

// dormantAgent is what a dormant pane holds to wake its agent.
type dormantAgent struct {
	// Agent is who it is (a manifest id: claude, codex), shown while it
	// sleeps.
	Agent string
	// Argv brings it back: the resume command for its conversation, or, for
	// one whose conversation tend never learnt, the command it was started
	// with.
	Argv []string
	// Session is its conversation, when known, kept so the next snapshot
	// still has it and the agent is remembered in it once awake.
	Session *agent.PersistedSession
}

// ErrNotAnAgent is a pane with no agent in it to put to sleep. One whose
// conversation tend does not know is ErrNoConversation: putting it to sleep
// would lose it.
var ErrNotAnAgent = errors.New("there is no agent running in this pane")

// sleepGrace is how long an agent is given to end on SIGTERM before it is
// killed: Claude Code and Codex write their conversation as it happens, so
// what is lost to a kill is at most the reply being written.
const sleepGrace = 5 * time.Second

// sleepCheckInterval is how often idle agents are looked at, when they
// sleep by themselves at all.
const sleepCheckInterval = 30 * time.Second

// dormantBanner is the line a dormant pane shows under its history.
func dormantBanner(agentName string) []byte {
	return []byte("\r\n\x1b[2m💤 " + agentName + " is asleep — focus this pane to wake it\x1b[0m\r\n")
}

// shellLine is argv as a line a shell runs: each word quoted unless it is
// plainly safe, so a session id or a path with a space reaches the agent as
// it was.
func shellLine(argv []string) string {
	words := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:@%+,", r))
		}) < 0 {
			words[i] = a
			continue
		}
		words[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(words, " ")
}

// dormantFor says whether a restored pane's spec is an agent to restore
// asleep, and what it would wake as. The spec is what restartPaneLocked
// would start: a resume command, or the command the pane was opened with.
func (s *Server) dormantFor(spec PaneSpec) (dormantAgent, bool) {
	if !s.cfg.SleepRestored || len(spec.Command) == 0 {
		return dormantAgent{}, false
	}
	if spec.resume != nil {
		return dormantAgent{Agent: spec.Agent, Argv: spec.Command, Session: spec.resume}, true
	}
	// No conversation known: an agent opened as the pane's own command
	// (`tend new -- claude`) sleeps all the same, and wakes as it started.
	// The command itself must be the agent — a shell in which one was
	// typed is saved as the shell, and starts as it did.
	manifest, err := agent.ResolveManifest(s.catalog, "", spec.Command[0])
	if err != nil || manifest == nil {
		return dormantAgent{}, false
	}
	return dormantAgent{Agent: manifest.ID, Argv: spec.Command}, true
}

// startDormantLocked starts a shell in a pane where its agent would be, the
// agent's history above it and a line saying it sleeps. The caller holds
// the session lock.
func (s *Server) startDormantLocked(id session.PaneID, spec PaneSpec, d dormantAgent) error {
	spec.history = append(append([]byte(nil), spec.history...), dormantBanner(d.Agent)...)
	spec.Command, spec.Agent = s.shell(), ""
	if err := s.startLocked(id, spec); err != nil {
		return err
	}
	rt := s.runtimes[id]
	rt.mu.Lock()
	rt.dormant = &d
	rt.mu.Unlock()
	return nil
}

// isDormant is whether a pane's agent sleeps.
func (rt *paneRuntime) isDormant() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.dormant != nil
}

// wakeOnFocus wakes a pane's agent when a client has just focused it.
func (s *Server) wakeOnFocus(id session.PaneID) {
	s.mu.Lock()
	rt := s.runtimes[id]
	s.mu.Unlock()
	if rt == nil || !rt.isDormant() {
		return
	}
	// Beside the request loop that reported the focus: typing the command
	// waits SubmitDelay before its Enter.
	go func() {
		if err := s.Wake(id); err != nil {
			s.logf("pane %d: waking its agent: %v", id, err)
		}
	}()
}

// Wake brings a dormant pane's agent back, in its conversation. A pane that
// is not dormant is left as it is.
func (s *Server) Wake(id session.PaneID) error {
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	d := rt.dormant
	rt.dormant = nil
	rt.mu.Unlock()
	if d == nil {
		return nil
	}
	if err := s.Submit(id, shellLine(agent.LaunchArgv(d.Argv))); err != nil {
		rt.mu.Lock()
		rt.dormant = d
		rt.mu.Unlock()
		return err
	}
	if d.Session != nil {
		// Remembered before the agent's own hook says it again, so a
		// snapshot taken in between still has the conversation.
		_ = s.RememberAgentSession(id, *d.Session)
	}
	s.publish(Event{Kind: EventPaneState, Pane: id})
	return nil
}

// SleepResult is what putting an agent to sleep did.
type SleepResult struct {
	Agent string
	// Freed is the memory its processes held, in bytes.
	Freed int64
}

// SleepAgent ends a pane's agent and leaves it dormant, to be woken in the
// same conversation. The conversation is read first: ending the agent may
// end what the arbiter knows of it.
func (s *Server) SleepAgent(id session.PaneID) (SleepResult, error) {
	rt, err := s.runtime(id)
	if err != nil {
		return SleepResult{}, err
	}
	if rt.isDormant() {
		return SleepResult{}, nil
	}
	st := rt.status()
	name := st.Agent
	if name == "" && st.Running && rt.pty.ChildForeground() != 0 {
		// The detector reads the screen, and an agent whose screen it does
		// not recognise — just started, or drawing something unusual — is
		// shown as none. The program in charge of the terminal says who is
		// there all the same.
		if m, _ := agent.ResolveManifest(s.catalog, "", rt.pty.Foreground()); m != nil {
			name = m.ID
		}
	}
	if name == "" || !st.Running {
		return SleepResult{}, ErrNotAnAgent
	}
	conversation, ok, err := s.AgentSession(id)
	if err != nil {
		return SleepResult{}, err
	}
	if !ok || conversation.Agent != name {
		return SleepResult{}, ErrNoConversation
	}
	argv, ok := agent.Resume(conversation)
	if !ok {
		return SleepResult{}, fmt.Errorf("tend cannot bring %s back in a conversation, so it is left awake", name)
	}
	d := dormantAgent{Agent: name, Argv: argv, Session: &conversation}
	result := SleepResult{Agent: name}

	if group := rt.pty.ChildForeground(); group != 0 {
		// The agent is the shell's child, as it is once woken or when typed
		// at the prompt: it ends, and the shell stays.
		result.Freed = pty.GroupMemory(group)
		if err := endGroup(group, func() bool { return rt.pty.ChildForeground() != group }); err != nil {
			return SleepResult{}, err
		}
		rt.mu.Lock()
		rt.dormant = &d
		rt.mu.Unlock()
		rt.write(dormantBanner(d.Agent))
	} else {
		// The agent is the pane's own process (`tend new -- claude`): the
		// pane is started again in place, as a shell under the same screen.
		result.Freed = pty.GroupMemory(rt.pty.Pid())
		if err := s.restartAsDormant(id, rt, d); err != nil {
			return SleepResult{}, err
		}
	}
	s.publish(Event{Kind: EventPaneState, Pane: id})
	return result, nil
}

// endGroup asks a process group to end, and makes it end if it will not:
// SIGTERM, a grace period watched with gone, then SIGKILL.
func endGroup(group int, gone func() bool) error {
	if err := pty.SignalGroup(group, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if waitUntil(gone, sleepGrace) {
		return nil
	}
	_ = pty.SignalGroup(group, syscall.SIGKILL)
	if waitUntil(gone, time.Second) {
		return nil
	}
	return fmt.Errorf("the agent did not end")
}

func waitUntil(cond func() bool, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// restartAsDormant ends a pane's process and starts the pane again, with the
// same id and the screen it had, as a dormant shell.
func (s *Server) restartAsDormant(id session.PaneID, rt *paneRuntime, d dormantAgent) error {
	history, dir, err := endInPlace(rt)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.session.Pane(id); !ok {
		return fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	spec := PaneSpec{Dir: dir, history: history, resume: d.Session, restarted: true}
	if p, ok := s.session.Pane(id); ok {
		spec.Title, spec.Named = p.Title, p.Named
		// What the restore after this one brings back.
		p.Command = d.Argv
	}
	return s.startDormantLocked(id, spec, d)
}

// endInPlace ends a pane's process so another can be started in the same
// pane: what its screen showed, to be put back above the new program, and
// where the old one was working. The runtime is marked closing first, so
// its reader leaves quietly — no "exited", and no close-on-exit taking the
// pane away.
func endInPlace(rt *paneRuntime) (history []byte, dir string, err error) {
	rt.withScreen(func(screen *vt.Screen) {
		history = vt.RenderResume(screen, 2000)
	})
	dir = rt.pty.Cwd()
	rt.setClosing()
	if err := endGroup(rt.pty.Pid(), func() bool {
		select {
		case <-rt.gone:
			return true
		default:
			return false
		}
	}); err != nil {
		return nil, "", err
	}
	_ = rt.pty.Close()
	return history, dir, nil
}

// RespawnPane ends whatever a pane runs and starts spec in its place: the
// same pane, id and screen, a new program. It is tmux's `respawn-pane -k`,
// which is how Claude Code's agent teams start a teammate in the pane it
// split for it (see internal/tmuxshim). With no directory, the new program
// starts where the old one was.
func (s *Server) RespawnPane(id session.PaneID, spec PaneSpec) error {
	if len(spec.Command) == 0 {
		return fmt.Errorf("server: respawning pane %d: no command", id)
	}
	if err := s.admitSpec(spec); err != nil {
		return err
	}
	rt, err := s.runtime(id)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	rt.dormant = nil
	rt.mu.Unlock()
	history, dir, err := endInPlace(rt)
	if err != nil {
		return err
	}
	if spec.Dir == "" {
		spec.Dir = dir
	}
	spec.history, spec.restarted = history, true

	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.session.Pane(id)
	if !ok {
		return fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	spec.Title, spec.Named = p.Title, p.Named
	// What a restore brings back, and what the pane is now.
	p.Command, p.Dir, p.Agent, p.CloseOnExit = spec.Command, spec.Dir, spec.Agent, spec.CloseOnExit
	return s.startLocked(id, spec)
}

// sleepLoop puts agents to sleep that have been idle, unfocused and
// unwatched for Config.SleepAfter.
func (s *Server) sleepLoop() {
	defer s.wg.Done()
	every := s.cfg.SleepInterval
	if every <= 0 {
		every = sleepCheckInterval
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-tick.C:
			for _, id := range s.sleepyAgents(time.Now()) {
				if _, err := s.SleepAgent(id); err != nil && !errors.Is(err, ErrNoConversation) {
					s.logf("pane %d: putting its agent to sleep: %v", id, err)
				}
			}
		}
	}
}

// sleepyAgents are the panes whose agent has been idle for SleepAfter, with
// no client focused on it or showing it.
func (s *Server) sleepyAgents(now time.Time) []session.PaneID {
	s.mu.Lock()
	after := s.cfg.SleepAfter
	if after <= 0 || s.closed {
		s.mu.Unlock()
		return nil
	}
	focused := s.focusedPane
	runtimes := make(map[session.PaneID]*paneRuntime, len(s.runtimes))
	for id, rt := range s.runtimes {
		runtimes[id] = rt
	}
	conns := make([]*clientConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	var out []session.PaneID
	for id, rt := range runtimes {
		if id == focused {
			continue
		}
		watched := false
		for _, c := range conns {
			if c.isSubscribed(id) {
				watched = true
				break
			}
		}
		if watched {
			continue
		}
		rt.mu.Lock()
		sleepy := rt.dormant == nil && rt.running && rt.shown.Agent != "" &&
			rt.shown.State == detect.StateIdle && !rt.idleSince.IsZero() && now.Sub(rt.idleSince) >= after
		rt.mu.Unlock()
		if sleepy {
			out = append(out, id)
		}
	}
	return out
}
