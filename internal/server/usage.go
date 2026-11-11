package server

import (
	"reflect"
	"strconv"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/usage"
)

// What each agent is spending, kept beside it (issue #46). The activity
// recorder already finds, every round, the conversation each agent is in and
// the file it keeps it in; following that file as it grows is what turns the
// record's after-the-fact totals into a number on screen while the agent
// works. Agents themselves tell nobody: Claude Code's /cost and /context are
// for the person in front of it, and no hook carries a number.

// Thresholds a notice is given at, once each per crossing: the context
// window filling (an agent that runs out compacts or stops, losing the
// thread), and the provider's limit running out.
var (
	contextWarnings = []int{80, 95}
	limitWarning    = 90.0
)

// followSpending reads what the run's current conversation has spent since
// the last round, and passes on what changed.
func (s *Server) followSpending(r *recorder, a seenAgent, run *openRun) {
	if len(run.convs) == 0 {
		return
	}
	c := run.convs[len(run.convs)-1]
	path := c.Transcript
	if path == "" && r.find != nil {
		// Claude Code names a conversation before it writes its file.
		path = r.find(a.agent, c)
	}
	if path == "" {
		return
	}
	if run.follow == nil || run.follow.Path() != path {
		run.follow = usage.NewFollower(a.agent, path)
		run.warned = map[string]bool{}
	}
	live, err := run.follow.Read()
	if err != nil {
		return
	}
	u := spendingOf(live, s.cfg.Prices)
	notices := run.warnings(u)

	a.rt.mu.Lock()
	changed := !reflect.DeepEqual(a.rt.spending, u)
	a.rt.spending = u
	a.rt.mu.Unlock()
	if changed {
		s.publish(Event{Kind: EventPaneState, Pane: a.id})
	}
	where := a.space
	if a.tab != "" {
		where += " › " + a.tab
	}
	for _, n := range notices {
		s.publish(Event{Kind: EventNotify, Pane: a.id, Title: a.agent + " " + n, Body: where})
	}
}

// clearSpending forgets what a pane's agent spent, once it is gone.
func (s *Server) clearSpending(rt *paneRuntime) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	had := rt.spending != nil
	rt.spending = nil
	rt.mu.Unlock()
	if had {
		s.publish(Event{Kind: EventPaneState, Pane: rt.id})
	}
}

// spendingOf is a conversation as the wire carries it.
func spendingOf(l usage.Live, prices map[string]usage.Price) *proto.PaneUsage {
	cost, known := l.Usage.Cost(prices)
	u := &proto.PaneUsage{
		Model: l.Model, Tokens: spentTokens(l.Usage.Total()),
		Context: l.Context, Window: l.Window, Reserve: l.Reserve,
		Cost: cost, CostKnown: known,
	}
	if l.Limit != nil {
		u.Limit = &proto.UsageLimit{UsedPercent: l.Limit.UsedPercent, Reached: l.Limit.Reached}
		if !l.Limit.Resets.IsZero() {
			u.Limit.Resets = l.Limit.Resets.Unix()
		}
	}
	return u
}

// warnings are the thresholds u has crossed since the last round, said as
// the end of a notice's title. One that is no longer crossed — a context
// compacted, a limit that started over — is armed again.
func (run *openRun) warnings(u *proto.PaneUsage) []string {
	var out []string
	pct := u.ContextPercent()
	for _, at := range contextWarnings {
		key := "context" + strconv.Itoa(at)
		switch {
		case pct >= at && !run.warned[key]:
			run.warned[key] = true
			// Only the highest crossed is said: a conversation resumed
			// at 97% is one notice, not two.
			if pct < nextWarning(at) {
				out = append(out, "context "+strconv.Itoa(pct)+"% full")
			}
		case pct >= 0 && pct < at:
			delete(run.warned, key)
		}
	}
	if l := u.Limit; l != nil {
		resets := ""
		if l.Resets > 0 {
			resets = " · resets " + time.Unix(l.Resets, 0).Format("Mon 15:04")
		}
		switch {
		case l.Reached && !run.warned["limit-reached"]:
			run.warned["limit-reached"], run.warned["limit"] = true, true
			out = append(out, "hit its usage limit"+resets)
		case !l.Reached && l.UsedPercent >= limitWarning && !run.warned["limit"]:
			run.warned["limit"] = true
			out = append(out, "has used "+strconv.Itoa(int(l.UsedPercent))+"% of its usage limit"+resets)
		case l.UsedPercent < limitWarning && !l.Reached:
			delete(run.warned, "limit")
			delete(run.warned, "limit-reached")
		}
	}
	return out
}

// nextWarning is the context threshold after at, or past 100.
func nextWarning(at int) int {
	for _, w := range contextWarnings {
		if w > at {
			return w
		}
	}
	return 101
}

// spentTokens is what a conversation is said to have spent: what went in
// and came out, without what was read back from the prompt cache — which is
// most of a long conversation's count, re-read with every reply, and which
// Codex leaves out of the total it shows too.
func spentTokens(t usage.Tokens) int64 {
	return t.Input + t.Output + t.CacheWrite
}
