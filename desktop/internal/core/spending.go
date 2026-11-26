package core

import (
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/sidebar"
	"github.com/auth-com-br/tend/internal/transport"
)

// The spending screen (issue #71): what the agents cost, as charts. Two
// sources, the two the TUI and the CLI already have:
//
//   - now, the agents open in the session, from the snapshot's PaneUsage —
//     each conversation's context, tokens and cost, as the sidebar's gauge
//     shows them;
//   - today and this week, the record the servers keep (internal/activity),
//     read with activity.Load and totalled with activity.Summarize, which is
//     what `tend activity -by agent|company -since today|week` prints: the
//     window and the command give the same number for the same period.
//
// Costs are estimates from the models' public prices, as the command says.

// SpendingState is the screen as QML draws it.
type SpendingState struct {
	// Period is "today" or "week".
	Period  string `json:"period"`
	Since   string `json:"since"`
	Loading bool   `json:"loading,omitempty"`
	Error   string `json:"error,omitempty"`
	// Total is every run of the period; Agents and Companies the same runs
	// totalled by agent and by company, the most expensive first.
	Total     SpendBar   `json:"total"`
	Agents    []SpendBar `json:"agents"`
	Companies []SpendBar `json:"companies"`
	// Days are the week's days, Monday first, by the day each run began;
	// empty for today.
	Days []SpendBar `json:"days,omitempty"`
	// Live are the agents open now, and LiveCost what their conversations
	// have cost so far.
	Live     []LiveAgent `json:"live"`
	LiveCost float64     `json:"liveCost"`
}

// SpendBar is one bar: a key and what it spent.
type SpendBar struct {
	Key     string  `json:"key"`
	Cost    float64 `json:"cost"`
	Tokens  int64   `json:"tokens"`
	Runs    int     `json:"runs"`
	Running int     `json:"running,omitempty"`
	// Complete is false when a model in it has no price: the cost is then
	// a floor.
	Complete bool `json:"complete"`
}

// LiveAgent is an agent open in the session now.
type LiveAgent struct {
	Pane      uint64 `json:"pane"`
	Agent     string `json:"agent"`
	Where     string `json:"where"`
	Companies string `json:"companies,omitempty"`
	Model     string `json:"model,omitempty"`
	// Context is how full its context window is, in percent, -1 when the
	// window is not known; ContextTokens and Window the numbers.
	Context       int     `json:"context"`
	ContextTokens int64   `json:"contextTokens"`
	Window        int64   `json:"window,omitempty"`
	Tokens        int64   `json:"tokens"`
	Cost          float64 `json:"cost"`
	Complete      bool    `json:"complete"`
}

// spending is what the session keeps of the screen.
type spending struct {
	period  string
	since   time.Time
	loading bool
	err     string
	runs    []*activity.Run
}

// OpenSpending shows the spending screen for a period, "today" or "week",
// and reads the record for it off the main thread.
func (s *Session) OpenSpending(period string) {
	if period != "week" {
		period = "today"
	}
	s.mu.Lock()
	s.panel = "spending"
	s.spend.period = period
	s.spend.loading = true
	s.mu.Unlock()
	s.stateChanged()
	go s.loadSpending(period)
}

func (s *Session) loadSpending(period string) {
	now := time.Now()
	since, err := activity.ParseSince(period, now)
	var runs []*activity.Run
	if err == nil {
		var dir string
		if dir, err = transport.StateDir(); err == nil {
			s.mu.Lock()
			prices := s.cfg.Activity.Prices
			s.mu.Unlock()
			runs, err = activity.Load(activity.Path(dir), activity.Filter{Since: since}, activity.DefaultTranscripts, prices)
		}
	}
	s.mu.Lock()
	if s.spend.period == period {
		s.spend.since, s.spend.runs, s.spend.loading, s.spend.err = since, runs, false, ""
		if err != nil {
			s.spend.err = err.Error()
		}
	}
	s.mu.Unlock()
	s.stateChanged()
}

func spendBar(t activity.Total) SpendBar {
	return SpendBar{Key: t.Key, Cost: t.Cost, Tokens: t.Tokens, Runs: t.Runs, Running: t.Running, Complete: t.CostComplete}
}

// spendingStateLocked is the screen now. The caller holds the lock.
func (s *Session) spendingStateLocked(now time.Time) *SpendingState {
	sp := &s.spend
	st := &SpendingState{
		Period: sp.period, Loading: sp.loading, Error: sp.err,
		Agents: []SpendBar{}, Companies: []SpendBar{}, Live: s.liveAgentsLocked(),
		Total: SpendBar{Key: "total", Complete: true},
	}
	if !sp.since.IsZero() {
		st.Since = sp.since.Format("Mon 02 Jan")
	}
	for _, a := range st.Live {
		st.LiveCost += a.Cost
	}
	for _, t := range activity.Summarize(sp.runs, func(*activity.Run) string { return "total" }, now) {
		st.Total = spendBar(t)
	}
	for _, t := range activity.Summarize(sp.runs, activity.ByAgent, now) {
		st.Agents = append(st.Agents, spendBar(t))
	}
	for _, t := range activity.SummarizeEach(sp.runs, activity.ByCompany, now) {
		st.Companies = append(st.Companies, spendBar(t))
	}
	if sp.period == "week" && !sp.since.IsZero() {
		st.Days = weekDays(sp.runs, sp.since, now)
	}
	return st
}

// weekDays totals runs by the day each began, Monday to Sunday from
// monday; a run that began before the week, and ran into it, is Monday's.
func weekDays(runs []*activity.Run, monday, now time.Time) []SpendBar {
	totals := map[string]activity.Total{}
	for _, t := range activity.Summarize(runs, func(r *activity.Run) string {
		day := r.Started.In(monday.Location())
		if day.Before(monday) {
			day = monday
		}
		return day.Format("2006-01-02")
	}, now) {
		totals[t.Key] = t
	}
	days := make([]SpendBar, 0, 7)
	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		bar := SpendBar{Key: day.Format("Mon"), Complete: true}
		if t, ok := totals[day.Format("2006-01-02")]; ok {
			bar = spendBar(t)
			bar.Key = day.Format("Mon")
		}
		days = append(days, bar)
	}
	return days
}

// liveAgentsLocked are the agents open in the session with what their
// conversations show, the most expensive first. The caller holds the lock.
func (s *Session) liveAgentsLocked() []LiveAgent {
	companies := map[uint64][]string{}
	for _, c := range s.snap.Companies {
		for _, w := range c.Workspaces {
			companies[w] = append(companies[w], c.Name)
		}
	}
	info := sidebar.PaneIndex(&s.snap)
	out := []LiveAgent{}
	for _, w := range s.snap.Workspaces {
		for _, t := range w.Tabs {
			for _, id := range t.Panes {
				p := info[id]
				if p.Agent == "" || !p.Running {
					continue
				}
				a := LiveAgent{
					Pane: id, Agent: p.Agent, Where: sidebar.AgentLabel(w, t),
					Companies: strings.Join(companies[w.ID], ", "), Context: -1, Complete: true,
				}
				if u := p.Usage; u != nil {
					a.Model, a.Context, a.ContextTokens, a.Window = u.Model, u.ContextPercent(), u.Context, u.Window
					a.Tokens, a.Cost, a.Complete = u.Tokens, u.Cost, u.CostKnown
				}
				out = append(out, a)
			}
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Cost > out[j-1].Cost; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// liveCostLocked is what the open agents' conversations have cost, for the
// status line. The caller holds the lock.
func (s *Session) liveCostLocked() float64 {
	total := 0.0
	for _, p := range s.snap.Panes {
		if p.Agent != "" && p.Running && p.Usage != nil {
			total += p.Usage.Cost
		}
	}
	return total
}
