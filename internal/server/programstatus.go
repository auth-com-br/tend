package server

import (
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/vt"
)

// A program's own word for its state (OSC 7501, internal/vt/status.go) is
// a report like a hook's: it goes to the arbiter, which believes reports
// over what the screen seems to say — the screen being what herdr's state
// bugs keep coming from (discussion #5073). It is read on the detection
// tick, as the screen is, since the terminal records it while parsing and
// the parser must not reach for anything else.

// programStatusSource is the reports' source in the arbiter.
const programStatusSource = "tend:osc7501"

// programStatusApps maps the app a program names itself to the agent tend
// knows it as.
// Codex (0.169) sends it too, without waiting for the probe.
var programStatusApps = map[string]string{"claude-code": "claude", "codex": "codex"}

// programStatusLocked passes a new OSC 7501 report on to the arbiter. The
// caller holds the pane's lock.
func (rt *paneRuntime) programStatusLocked(now time.Time) {
	ps, rev := rt.screen.ProgramStatus()
	if rev == rt.statusRev {
		return
	}
	rt.statusRev = rev
	seq := rev
	if ps.State == "" {
		// Claude Code clears its status as it exits. The conversation a hook
		// named is still one to resume; only the state goes.
		known, had := rt.arbiter.Session()
		if rt.arbiter.Clear(programStatusSource, &seq) && had {
			rt.arbiter.RestoreSession(known)
		}
		return
	}
	label := programStatusApps[ps.App]
	if label == "" {
		label = rt.agentID
	}
	if label == "" {
		return // nothing says who it is
	}
	r := agent.Report{Source: programStatusSource, Agent: label, State: programState(ps), Message: ps.Msg, Seq: &seq}
	// A report replaces the one believed, and with it the conversation a
	// hook had named — which the usage shown beside the agent and its
	// resume after a restart are read from. Kept.
	if known, ok := rt.arbiter.Session(); ok && known.Agent == label {
		r.Session, r.SessionSource = known.Session, known.Source
	}
	rt.arbiter.Report(r, now)
}

// programState is what a program's status means as an agent's state:
// done and error are idle — finished, the turn over — and the session's
// "done" (finished unseen) follows from the change to idle as it does for
// every other source.
func programState(ps vt.ProgramStatus) detect.State {
	switch ps.State {
	case "working":
		return detect.StateWorking
	case "blocked":
		return detect.StateBlocked
	}
	return detect.StateIdle
}
