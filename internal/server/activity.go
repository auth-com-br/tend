package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/integration"
	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/usage"
)

// The record of what agents did (internal/activity) is kept here because the
// server is the one program that sees every agent come and go, whoever
// started it and however.
//
// It is kept by looking rather than by listening. Every few seconds the
// recorder compares the panes that have an agent with the runs it has open:
// an agent it has no run for has started, a run whose agent it no longer
// finds has ended. Events would have been sooner, but an agent leaves a pane
// in half a dozen ways — it exits, the pane closes, its tab closes, another
// program takes the terminal, the server stops — and each would have needed
// remembering at its own call site. A comparison cannot miss one, and a
// record of work measured in minutes loses nothing to a two-second delay.
//
// Everything slow — git, reading a conversation file megabytes long — is
// done by the recorder's own goroutine with no lock held but its own.

// ErrPolicy is an agent tend will not start because a policy forbids it.
var ErrPolicy = errors.New("policy")

const (
	// activityInterval is how often the recorder compares.
	activityInterval = 2 * time.Second
	// activityGrace is how long a pane may be without its agent before the
	// run is over. Detection loses an agent for a moment when it runs a
	// command of its own and the foreground is briefly something else; that
	// is not the agent leaving.
	activityGrace = 6 * time.Second
)

// recorder is the activity record's state in a running server.
type recorder struct {
	log     *activity.Log
	session string
	find    activity.Transcripts

	// mu is the recorder's own and is taken before the server's, never
	// after: a round reads the session under the server lock while holding
	// it.
	mu   sync.Mutex
	open map[session.PaneID]*openRun
	// inherited are runs the record says were going in this session when
	// this server started: a replacement taking over from a handoff, or a
	// server started after one that died. The first round decides which of
	// them are still going.
	inherited map[session.PaneID]*activity.Run
	// began is when this recorder started. Inherited runs are given the
	// grace after it to be found: a pane taken over, or restored,
	// is not recognised as an agent in the very first round.
	began   time.Time
	stopped bool
}

// openRun is a run in progress.
type openRun struct {
	id       string
	agent    string
	place    activity.Place
	lastSeen time.Time
	convs    []activity.Conversation
}

// seenAgent is one pane with an agent, as a round found it.
type seenAgent struct {
	id    session.PaneID
	agent string
	space string
	tab   string
	dir   string
	rt    *paneRuntime
}

// newRecorder reads what the record already says about this session.
func newRecorder(log *activity.Log, sessionName string, find activity.Transcripts) *recorder {
	r := &recorder{
		log: log, session: sessionName, find: find,
		open:      map[session.PaneID]*openRun{},
		inherited: map[session.PaneID]*activity.Run{},
		began:     time.Now(),
	}
	recs, err := activity.Read(log.Path())
	if err != nil {
		return r
	}
	runs, _ := activity.Fold(recs)
	for _, run := range runs {
		if run.Running && run.Session == sessionName {
			r.inherited[session.PaneID(run.Pane)] = run
		}
	}
	return r
}

// DefaultTranscripts finds a conversation's file where the agents keep them
// on this machine.
func DefaultTranscripts(agentLabel string, c activity.Conversation) string {
	switch agentLabel {
	case "claude":
		if dir, err := integration.ClaudeDir(); err == nil {
			return usage.FindClaude(dir, c.ID)
		}
	case "codex":
		if dir, err := integration.CodexDir(); err == nil {
			return usage.FindCodex(dir, c.ID)
		}
	}
	return ""
}

// activityLoop runs the recorder until the server closes.
func (s *Server) activityLoop() {
	defer s.wg.Done()
	interval := s.cfg.ActivityInterval
	if interval <= 0 {
		interval = activityInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	s.recordActivity()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.recordActivity()
		}
	}
}

// agentsLocked is every pane with an agent whose process is running, with
// where it is. The caller holds the server lock, and each pane's is taken
// after it, in the order the server's locks go.
func (s *Server) agentsLocked() []seenAgent {
	var out []seenAgent
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			for _, id := range t.Panes() {
				p, ok := t.Pane(id)
				if !ok || p.Agent == "" {
					continue
				}
				rt := s.runtimes[id]
				if rt == nil || !rt.alive() {
					// A pane keeps the agent it was opened as after its
					// process ends — `tend new -- claude` then /exit still
					// says claude — so the label alone would keep the run
					// going, and count against the limit, forever.
					continue
				}
				out = append(out, seenAgent{id: id, agent: p.Agent, space: w.Name, tab: t.Name, dir: p.Dir, rt: rt})
			}
		}
	}
	return out
}

// recordActivity is one round of comparing.
func (s *Server) recordActivity() {
	r := s.activity
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}

	s.mu.Lock()
	if s.closed || s.handingOff {
		s.mu.Unlock()
		return
	}
	seen := s.agentsLocked()
	live := make(map[session.PaneID]bool, len(s.runtimes))
	for id := range s.runtimes {
		live[id] = true
	}
	s.mu.Unlock()

	now := time.Now()
	here := map[session.PaneID]bool{}
	for _, a := range seen {
		here[a.id] = true
		run := r.open[a.id]
		if run != nil && run.agent != a.agent {
			s.endRun(r, a.id, run, activity.ReasonReplaced, now)
			run = nil
		}
		if run == nil {
			run = s.beginRun(r, a, len(seen)-1, now)
		}
		run.lastSeen = now
		s.followConversation(r, a, run, now)
	}

	// Runs the record says were going before this server, that no round
	// has found an agent for: whatever ended them happened while nobody was
	// watching, and when is not known.
	grace := s.cfg.ActivityGrace
	if grace <= 0 {
		grace = activityGrace
	}
	if len(r.inherited) > 0 && now.Sub(r.began) >= grace {
		s.loseInherited(r, now)
	}

	for id, run := range r.open {
		if here[id] {
			continue
		}
		switch {
		case !live[id]:
			s.endRun(r, id, run, activity.ReasonClosed, now)
		case now.Sub(run.lastSeen) >= grace:
			s.endRun(r, id, run, activity.ReasonExited, run.lastSeen)
		}
	}
}

// loseInherited ends the inherited runs no agent was found for.
func (s *Server) loseInherited(r *recorder, now time.Time) {
	for id, old := range r.inherited {
		if _, adopted := r.open[id]; adopted {
			continue
		}
		_ = r.log.Append(activity.Record{
			Kind: activity.KindStop, Time: now, Run: old.ID, Session: r.session, Pane: old.Pane,
			Agent: old.Agent, Reason: activity.ReasonLost,
			Usage: activity.Spent(old.Agent, old.Conversations, r.find),
		})
	}
	r.inherited = map[session.PaneID]*activity.Run{}
}

// beginRun opens a run for an agent the round found, or takes up the one the
// record says it was already in. others is how many other agents are running.
func (s *Server) beginRun(r *recorder, a seenAgent, others int, now time.Time) *openRun {
	if old := r.inherited[a.id]; old != nil && old.Agent == a.agent {
		// The same agent in the same pane: a replacement server taking over
		// the panes of the one before it. The run goes on under its id, with
		// the conversations and baselines it already had.
		run := &openRun{id: old.ID, agent: old.Agent, place: old.Place, convs: old.Conversations}
		r.open[a.id] = run
		delete(r.inherited, a.id)
		return run
	}

	dir := a.rt.pty.Cwd()
	if dir == "" {
		dir = a.dir
	}
	place := activity.Inspect(dir)
	run := &openRun{
		id:    activity.RunID(r.session, uint64(a.id), now),
		agent: a.agent,
		place: place,
	}
	r.open[a.id] = run
	_ = r.log.Append(activity.Record{
		Kind: activity.KindStart, Time: now, Run: run.id, Session: r.session, Pane: uint64(a.id),
		Agent: a.agent, Place: place, Space: a.space, Tab: a.tab,
	})

	// An agent tend did not start — somebody typed its name into a shell —
	// is checked here, since nothing asked tend first. It is told and
	// written down, not stopped: killing a program somebody started by hand
	// is not tend's to do.
	for _, v := range s.policy().Check(policyPlace(place, others)) {
		_ = r.log.Append(activity.Record{
			Kind: activity.KindViolation, Time: now, Run: run.id, Session: r.session, Pane: uint64(a.id),
			Agent: a.agent, Place: place, Policy: v.Rule, Message: v.Message,
		})
		s.publish(Event{Kind: EventNotify, Pane: a.id, Title: "policy: " + a.agent, Body: v.Message})
	}
	return run
}

// followConversation notes a conversation the agent's hook has named that the
// run has not seen yet, with what it had spent by then.
func (s *Server) followConversation(r *recorder, a seenAgent, run *openRun, now time.Time) {
	a.rt.mu.Lock()
	known, ok := a.rt.arbiter.Session()
	a.rt.mu.Unlock()
	if !ok || known.Agent != a.agent || known.Session.Empty() {
		return
	}
	for _, c := range run.convs {
		if c.ID == known.Session.ID && known.Session.ID != "" || c.Transcript != "" && c.Transcript == known.Session.Path {
			return
		}
	}
	c := activity.Conversation{ID: known.Session.ID, Transcript: known.Session.Path}
	if c.Transcript == "" && r.find != nil {
		c.Transcript = r.find(a.agent, c)
	}
	if c.Transcript != "" {
		if u, err := activity.Read1(a.agent, c.Transcript); err == nil {
			c.Baseline = u
		}
	}
	run.convs = append(run.convs, c)
	_ = r.log.Append(activity.Record{
		Kind: activity.KindConversation, Time: now, Run: run.id, Session: r.session, Pane: uint64(a.id),
		Agent: a.agent, Conversation: c.ID, Transcript: c.Transcript, Baseline: c.Baseline,
	})
}

// endRun closes a run: what changed in its checkout, and what its
// conversations spent.
func (s *Server) endRun(r *recorder, id session.PaneID, run *openRun, reason string, at time.Time) {
	delete(r.open, id)
	files, commits := activity.Changes(run.place)
	_ = r.log.Append(activity.Record{
		Kind: activity.KindStop, Time: at, Run: run.id, Session: r.session, Pane: uint64(id),
		Agent: run.agent, Reason: reason, Files: files, Commits: commits,
		Usage: activity.Spent(run.agent, run.convs, r.find),
	})
}

// waitForActivityRound returns once no round of the recorder is under way.
func (s *Server) waitForActivityRound() {
	if r := s.activity; r != nil {
		// Taken only to wait for whoever holds it.
		r.mu.Lock()
		r.mu.Unlock()
	}
}

// stopActivity ends every open run, for a server that is stopping and taking
// its panes with it. A server handing its panes to a replacement does not
// call it: the runs go on there.
func (s *Server) stopActivity() {
	r := s.activity
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	now := time.Now()
	for id, run := range r.open {
		s.endRun(r, id, run, activity.ReasonStopped, now)
	}
}

// policy is the rules in force now.
func (s *Server) policy() policy.Policy {
	if s.cfg.Policy == nil {
		return policy.Policy{}
	}
	return s.cfg.Policy()
}

func policyPlace(p activity.Place, others int) policy.Place {
	return policy.Place{InRepo: p.Project != "", Branch: p.Branch, Worktree: p.Worktree, Running: others}
}

// admitAgent is whether an agent may start in dir, for a pane tend is about
// to open or type an agent into. except is a pane not to count among the
// running, the one the agent is going into. A refusal is written down.
func (s *Server) admitAgent(agentLabel, dir string, except session.PaneID) error {
	rules := s.policy()
	if rules.Empty() {
		return nil
	}
	s.mu.Lock()
	running := 0
	for _, a := range s.agentsLocked() {
		if a.id != except {
			running++
		}
	}
	if dir == "" {
		dir = s.cfg.Dir
	}
	s.mu.Unlock()
	if dir == "" {
		dir, _ = os.Getwd()
	}

	place := activity.Inspect(dir)
	broken := rules.Check(policyPlace(place, running))
	if len(broken) == 0 {
		return nil
	}
	msgs := make([]string, len(broken))
	var recs []activity.Record
	for i, v := range broken {
		msgs[i] = v.Message
		recs = append(recs, activity.Record{
			Kind: activity.KindRefused, Session: s.cfg.SessionName, Agent: agentLabel,
			Place: place, Policy: v.Rule, Message: v.Message,
		})
	}
	if s.activity != nil {
		_ = s.activity.log.Append(recs...)
	}
	return fmt.Errorf("%w: %s", ErrPolicy, strings.Join(msgs, "; "))
}

// admitSpec is admitAgent for a pane about to be opened, when what it runs is
// an agent. What it runs is decided the way detection decides it, from the
// agent the caller named or else the command.
func (s *Server) admitSpec(spec PaneSpec) error {
	if spec.NoDetect || spec.resume != nil || spec.history != nil {
		// A popup is never an agent to detection, and a pane coming back
		// from a restart is one that was already allowed to run.
		return nil
	}
	label := spec.Agent
	if label == "" {
		if len(spec.Command) == 0 {
			return nil
		}
		m, err := agent.ResolveManifest(s.catalog, "", spec.Command[0])
		if err != nil || m == nil {
			return nil
		}
		label = m.ID
	}
	return s.admitAgent(label, spec.Dir, 0)
}

// AdmitAgentIn is whether an agent may be started in a pane that exists,
// where agent.start types it: in the directory the pane is in now.
func (s *Server) AdmitAgentIn(id session.PaneID, agentLabel string) error {
	s.mu.Lock()
	rt := s.runtimes[id]
	s.mu.Unlock()
	if rt == nil {
		return fmt.Errorf("%w: %d", session.ErrNoSuchPane, id)
	}
	return s.admitAgent(agentLabel, rt.pty.Cwd(), id)
}

// alive reports whether the pane's process is still running.
func (rt *paneRuntime) alive() bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.running
}
