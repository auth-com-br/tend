package agent

import (
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/detect"
)

// An agent can say what it is doing instead of being watched doing it. A hook
// installed into the agent reports "working", "blocked", "idle" over the
// session's socket, and that report is better evidence than the screen: it is
// the agent's own account, not a reading of what it happened to draw.
//
// Better, not unconditional. A report outlives the thing it describes — the
// agent exits, another program takes the pane, and the last report still says
// "working". So the two sources are arbitrated, and the rules are herdr's
// (`terminal/state.rs`):
//
//   - A report for a different agent than the one detected in the pane is
//     ignored: the screen says who is there, and the hook does not get to argue.
//   - Reports from one source are ordered by a sequence number, and one that is
//     not newer than the last is dropped. Hooks are separate processes racing
//     each other to a socket; arrival order is not sending order.
//   - When the detected agent changes away from the one a report was about, the
//     report is dropped with it.
//   - A blocker visible on the screen beats a hook that says otherwise, as long
//     as the screen was read after the hook spoke: an agent asking for
//     permission is blocked whatever its last report was.
//   - A few integrations cover the agent's whole lifecycle, and for those the
//     screen is not consulted at all while the agent is the one detected. The
//     rest are hints between screen readings.
//
// Not ported yet, and recorded in docs/PORTING.md: herdr's bookkeeping for
// suppressed and stale full-lifecycle sessions, its window after an observed
// process exit, agent names, and metadata reports.

// Source prefix for the integrations tend installs itself.
const officialSourcePrefix = "tend:"

// fullLifecycle lists the integrations whose reports cover everything the agent
// does, so that the screen adds nothing while they are live. The list is
// herdr's, with its source names translated.
var fullLifecycle = map[string]string{
	"tend:pi":         "pi",
	"tend:omp":        "omp",
	"tend:mastracode": "mastracode",
	"tend:opencode":   "opencode",
	"tend:kilo":       "kilo",
	"tend:kimi":       "kimi",
}

// sessionOnly lists the integrations that report which conversation the agent
// is in and nothing about its state. A state report from one of them is a
// mistake in the hook, not evidence.
var sessionOnly = map[string]string{
	"tend:hermes":          "hermes",
	"tend:qwen":            "qwen",
	"tend:letta":           "letta",
	"tend:antigravity_cli": "agy",
}

// FullLifecycle reports whether source's reports about agent replace the
// screen rather than supplement it.
func FullLifecycle(source, agent string) bool { return fullLifecycle[source] == agent && agent != "" }

// SessionOnly reports whether source only ever identifies sessions.
func SessionOnly(source, agent string) bool { return sessionOnly[source] == agent && agent != "" }

// Official reports whether source is the integration tend ships for agent.
func Official(source, agent string) bool {
	return agent != "" && source == officialSourcePrefix+agent
}

// NormalizeLabel cleans an agent name received from outside. Empty means the
// name is unusable and the report should be refused. Any other name is taken:
// a script may report an agent tend has never heard of, and the pane is shown
// as that.
func NormalizeLabel(label string) string { return strings.TrimSpace(label) }

// SessionRef names the conversation an agent is in, in the agent's own terms.
// It is what lets the conversation be picked up again after the pane is gone.
type SessionRef struct {
	ID   string `json:"id,omitempty"`
	Path string `json:"path,omitempty"`
}

// SessionRefFromReport decides what, if anything, a report's session fields are
// worth. The rule is herdr's (`agent_resume.rs`).
//
// Only the integration tend ships for an agent is believed about that agent's
// conversations: the reference ends up on a command line to resume it, and a
// script that can name any file as "the session" should not get to write that
// command line. pi and omp resume from a transcript path, so for them the path
// is preferred; everything else resumes by id, and its path is ignored.
func SessionRefFromReport(source, agent, id, path string) SessionRef {
	if !Official(source, agent) {
		return SessionRef{}
	}
	if agent == "pi" || agent == "omp" {
		if path != "" {
			return SessionRef{Path: path}
		}
	}
	return SessionRef{ID: id}
}

// Kind and Value are the reference as the automation socket states it.
func (r SessionRef) Kind() string {
	if r.Path != "" {
		return "path"
	}
	return "id"
}

// Value is the id or the path, whichever the reference holds.
func (r SessionRef) Value() string {
	if r.Path != "" {
		return r.Path
	}
	return r.ID
}

// Empty reports whether the reference names nothing.
func (r SessionRef) Empty() bool { return r.ID == "" && r.Path == "" }

// Report is one thing a hook said.
type Report struct {
	Source  string
	Agent   string
	State   detect.State
	Message string
	// Seq orders reports from one source. Nil means the hook does not number
	// them, which is accepted only from a source that never has.
	Seq     *uint64
	Session SessionRef
	// SessionSource is who named Session, when that is not Source: a
	// program's own status report (OSC 7501) carries on the conversation a
	// hook named, and resuming it is only for the hook's source (Official).
	SessionSource string
}

// Authority is the report currently believed.
type Authority struct {
	Source     string
	Agent      string
	State      detect.State
	Message    string
	ReportedAt time.Time
	Session    SessionRef
	// SessionSource is who named Session, when not Source (Report's).
	SessionSource string
}

// PersistedSession is a conversation known for the pane with no live report
// behind it: what is left when a hook's authority ends but the conversation
// may well not have.
type PersistedSession struct {
	Source  string     `json:"source"`
	Agent   string     `json:"agent"`
	Session SessionRef `json:"session"`
}

// Effective is what the pane should be shown as.
type Effective struct {
	Agent   string
	State   detect.State
	Message string
	// Source is the hook behind the state, or empty when it came from the
	// screen.
	Source string
}

// Arbiter decides, for one pane, between what hooks report and what the screen
// shows. It is plain data: no clock of its own, no locks, no I/O.
type Arbiter struct {
	detected        string
	fallback        detect.State
	fallbackBlocker bool
	fallbackAt      time.Time

	authority *Authority
	persisted *PersistedSession
	seqs      map[string]uint64

	// metadata is what each source said about how to show the pane, tokens
	// are the values to show beside it, and metadataSeqs orders those
	// reports — apart from the state ones, because a hook may send one kind
	// without the other.
	metadata     map[string]*metadataEntry
	tokens       map[string]token
	metadataSeqs map[string]uint64

	// known reports whether a label names an agent tend can detect. Only such
	// a label can disagree with the screen: a name the screen could never
	// produce is not contradicted by the screen producing another.
	known func(string) bool
}

// NewArbiter returns an arbiter that has heard nothing. known says which
// labels are agents detection can recognise; nil means none are.
func NewArbiter(known func(string) bool) *Arbiter {
	if known == nil {
		known = func(string) bool { return false }
	}
	return &Arbiter{seqs: make(map[string]uint64), known: known}
}

// conflicts reports whether a label names a detectable agent other than the
// one detected.
func (a *Arbiter) conflicts(label string) bool {
	return a.detected != "" && a.known(label) && label != a.detected
}

// Authority returns the report currently believed, if any.
func (a *Arbiter) Authority() *Authority { return a.authority }

// Session is the conversation known for the pane, from a live report or from
// one that has ended.
func (a *Arbiter) Session() (PersistedSession, bool) {
	if a.authority != nil && !a.authority.Session.Empty() {
		source := a.authority.Source
		if a.authority.SessionSource != "" {
			source = a.authority.SessionSource
		}
		return PersistedSession{Source: source, Agent: a.authority.Agent, Session: a.authority.Session}, true
	}
	if a.persisted != nil {
		return *a.persisted, true
	}
	return PersistedSession{}, false
}

// RestoreSession puts back a conversation read from a state file.
func (a *Arbiter) RestoreSession(p PersistedSession) {
	if p.Session.Empty() {
		return
	}
	a.persisted = &p
}

// Observe records what the screen shows: which agent is detected in the pane,
// if any, and what it appears to be doing.
func (a *Arbiter) Observe(agent string, state detect.State, visibleBlocker bool, now time.Time) {
	previous := a.detected
	a.detected = agent
	a.fallback = state
	a.fallbackBlocker = visibleBlocker
	a.fallbackAt = now

	if a.authority == nil || a.authority.ReportedAt.After(now) {
		return
	}
	// The report was about an agent that is not the one here now: either a
	// different one is detected, or the one it was about has just left.
	conflicts := a.conflicts(a.authority.Agent)
	departed := previous != "" && agent != previous && a.authority.Agent == previous
	if conflicts || departed {
		if known, ok := a.Session(); ok && !a.authority.Session.Empty() {
			// The conversation outlives the report. Somebody may want it
			// back, from whoever named it.
			a.persisted = &known
		}
		delete(a.seqs, a.authority.Source)
		a.authority = nil
	}
}

// accept applies the ordering rule for one source.
func (a *Arbiter) accept(source string, seq *uint64) bool {
	last, seen := a.seqs[source]
	if seq == nil {
		return !seen
	}
	if seen && *seq <= last {
		return false
	}
	a.seqs[source] = *seq
	return true
}

// Report takes a state report, and says whether it was believed.
func (a *Arbiter) Report(r Report, now time.Time) bool {
	if SessionOnly(r.Source, r.Agent) {
		return false
	}
	if a.conflicts(r.Agent) {
		return false
	}
	if !a.accept(r.Source, r.Seq) {
		return false
	}
	session, sessionSource := r.Session, r.SessionSource
	if session.Empty() && a.authority != nil && a.authority.Source == r.Source && a.authority.Agent == r.Agent {
		// Most reports do not repeat the session. The one already known for
		// this source still stands.
		session, sessionSource = a.authority.Session, a.authority.SessionSource
	}
	a.persisted = nil
	a.authority = &Authority{
		Source: r.Source, Agent: r.Agent, State: r.State, Message: r.Message,
		ReportedAt: now, Session: session, SessionSource: sessionSource,
	}
	return true
}

// ReportSession takes a report that names the conversation and says nothing
// about state. It never changes what the pane is shown as.
func (a *Arbiter) ReportSession(source, agent string, session SessionRef, seq *uint64) bool {
	if session.Empty() {
		return false
	}
	if a.conflicts(agent) {
		return false
	}
	if !a.accept(source, seq) {
		return false
	}
	if a.authority != nil && a.authority.Agent == agent &&
		(a.authority.Source == source || a.authority.SessionSource == source) {
		// Its own report, or another's — a program's status — carrying on
		// the conversation this source named: the new one replaces it.
		a.authority.Session = session
		return true
	}
	a.persisted = &PersistedSession{Source: source, Agent: agent, Session: session}
	return true
}

// Clear drops the current report. An empty source clears whoever holds it; a
// named one clears only its own.
func (a *Arbiter) Clear(source string, seq *uint64) bool {
	if a.authority == nil || (source != "" && a.authority.Source != source) {
		return false
	}
	if !a.accept(a.authority.Source, seq) {
		return false
	}
	a.authority = nil
	a.persisted = nil
	return true
}

// Release is a hook saying its agent has gone. Only the holder may say so.
//
// When the screen still shows that agent running, the screen is believed about
// its presence and only the report goes. Otherwise the pane stops being that
// agent's altogether.
func (a *Arbiter) Release(source, agent string, seq *uint64) (released, agentGone bool) {
	if a.authority != nil && (a.authority.Agent != agent || a.authority.Source != source) {
		return false, false
	}
	matchesCurrent := a.Effective().Agent == agent
	matchesPersisted := a.persisted != nil && a.persisted.Source == source && a.persisted.Agent == agent
	if !matchesCurrent && !matchesPersisted {
		return false, false
	}
	if !a.accept(source, seq) {
		return false, false
	}
	processOwns := a.detected == agent
	if !processOwns {
		a.detected = ""
		a.fallback = detect.StateUnknown
		a.fallbackBlocker = false
		a.fallbackAt = time.Time{}
	}
	a.authority = nil
	if matchesPersisted {
		a.persisted = nil
	}
	return true, !processOwns
}

// authorityEffective reports whether the current report is to be believed
// about the pane right now. A full-lifecycle one counts only while its agent
// is the one detected; the others always do.
func (a *Arbiter) authorityEffective() bool {
	if a.authority == nil {
		return false
	}
	if !FullLifecycle(a.authority.Source, a.authority.Agent) {
		return true
	}
	return a.detected == a.authority.Agent
}

// Effective is the arbitrated answer.
func (a *Arbiter) Effective() Effective {
	if !a.authorityEffective() {
		return Effective{Agent: a.detected, State: a.fallback}
	}
	au := a.authority
	if a.blockerOverrides() {
		return Effective{Agent: au.Agent, State: detect.StateBlocked}
	}
	return Effective{Agent: au.Agent, State: au.State, Message: au.Message, Source: au.Source}
}

// blockerOverrides reports whether a blocker on the screen beats the hook.
func (a *Arbiter) blockerOverrides() bool {
	au := a.authority
	if au == nil || FullLifecycle(au.Source, au.Agent) {
		return false
	}
	return a.fallbackBlocker &&
		!a.fallbackAt.Before(au.ReportedAt) &&
		au.State != detect.StateBlocked &&
		au.Agent == a.detected
}

// IgnoresScreen reports whether detection results should be left unused: a
// full-lifecycle integration is live for the agent in the pane.
func (a *Arbiter) IgnoresScreen() bool {
	return a.authorityEffective() && FullLifecycle(a.authority.Source, a.authority.Agent)
}
