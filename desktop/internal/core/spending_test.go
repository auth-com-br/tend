package core

import (
	"slices"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/usage"
)

// writeActivity writes a record as the servers write it, in the state
// directory the test's session uses.
func writeActivity(t *testing.T, recs ...activity.Record) {
	t.Helper()
	dir, err := transport.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := activity.NewLog(activity.Path(dir)).Append(recs...); err != nil {
		t.Fatal(err)
	}
}

// run is a run that began at start and ended an hour later, having spent
// a million input tokens of model.
func run(id, agent, model string, start time.Time, companies ...string) []activity.Record {
	return []activity.Record{
		{Kind: activity.KindStart, Time: start, Run: id, Agent: agent, Companies: companies},
		{Kind: activity.KindStop, Time: start.Add(time.Hour), Run: id, Reason: activity.ReasonExited,
			Usage: usage.Usage{model: {Input: 1_000_000}}},
	}
}

func barOf(bars []SpendBar, key string) (SpendBar, bool) {
	i := slices.IndexFunc(bars, func(b SpendBar) bool { return b.Key == key })
	if i < 0 {
		return SpendBar{}, false
	}
	return bars[i], true
}

// TestTheSpendingScreenTotalsTheRecordByAgentAndCompany: the screen reads
// the activity record as `tend activity -by agent|company -since today`
// does — a run from before today left out, a space in two companies counted
// in both, a cost from the models' prices — and the week has a bar per day.
// If it regresses, the window's day costs a different amount from the
// command's.
func TestTheSpendingScreenTotalsTheRecordByAgentAndCompany(t *testing.T) {
	s := openTest(t)
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	var recs []activity.Record
	recs = append(recs, run("a", "claude", "claude-opus-4-5", midnight.Add(time.Minute), "acme")...)            // $5
	recs = append(recs, run("b", "codex", "claude-sonnet-4-5", midnight.Add(2*time.Minute), "acme", "beta")...) // $3
	recs = append(recs, run("old", "claude", "claude-opus-4-5", midnight.AddDate(0, 0, -10))...)
	writeActivity(t, recs...)

	s.OpenSpending("today")
	var sp *SpendingState
	waitFor(t, "the spending screen", func() bool { sp = s.State().Spending; return sp != nil && !sp.Loading })
	if sp.Error != "" {
		t.Fatal(sp.Error)
	}
	if sp.Total.Runs != 2 || sp.Total.Cost != 8 || !sp.Total.Complete {
		t.Errorf("today's total is %+v; want two runs, $8", sp.Total)
	}
	if c, _ := barOf(sp.Agents, "claude"); c.Cost != 5 || c.Runs != 1 {
		t.Errorf("claude is %+v", c)
	}
	acme, _ := barOf(sp.Companies, "acme")
	beta, _ := barOf(sp.Companies, "beta")
	if acme.Cost != 8 || beta.Cost != 3 {
		t.Errorf("acme %+v, beta %+v", acme, beta)
	}
	if len(sp.Days) != 0 {
		t.Errorf("today has days: %+v", sp.Days)
	}

	s.OpenSpending("week")
	waitFor(t, "the week", func() bool {
		sp = s.State().Spending
		return sp != nil && sp.Period == "week" && !sp.Loading
	})
	if len(sp.Days) != 7 || sp.Days[0].Key != "Mon" {
		t.Fatalf("the week's days are %+v", sp.Days)
	}
	today, _ := barOf(sp.Days, now.Format("Mon"))
	if today.Cost != 8 {
		t.Errorf("today's bar in the week is %+v", today)
	}
}

// TestTheGlobalMenuOpensTheSpendingScreen: the window's global menu has
// "spending", before the settings, and picking it opens the screen. If it
// regresses, the dashboard has no way in but the status line.
func TestTheGlobalMenuOpensTheSpendingScreen(t *testing.T) {
	s := openTest(t)
	s.OpenMenu("global", 0, 0, 0, "", 0, 0)
	m := s.State().Menu
	i := slices.Index(m.Items, "spending")
	if i < 0 || m.Items[i+1] != "settings" {
		t.Fatalf("the global menu is %v", m.Items)
	}
	if err := s.PickMenu(i); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the spending screen", func() bool { return s.State().Panel == "spending" })
}

// TestTheAgentsOpenNowShowTheirContextAndCost: the screen's "now" is each
// agent's conversation as the snapshot gives it — context against its
// window, tokens, cost — with its space's companies, the most expensive
// first, and the status line's figure is their sum. If it regresses, the
// screen disagrees with the sidebar's gauge.
func TestTheAgentsOpenNowShowTheirContextAndCost(t *testing.T) {
	s := &Session{snap: proto.SessionSnapshot{
		Workspaces: []proto.WorkspaceInfo{{ID: 1, Name: "api", Tabs: []proto.TabInfo{{ID: 1, Panes: []uint64{1, 2, 3}}}}},
		Companies:  []proto.CompanyInfo{{ID: 1, Name: "acme", Workspaces: []uint64{1}}},
		Panes: []proto.PaneInfo{
			{ID: 1, Agent: "claude", Running: true, Usage: &proto.PaneUsage{Context: 50_000, Window: 200_000, Tokens: 9000, Cost: 0.4, CostKnown: true}},
			{ID: 2, Agent: "codex", Running: true, Usage: &proto.PaneUsage{Tokens: 100, Cost: 1.1, CostKnown: true}},
			{ID: 3},
		},
	}}
	live := s.liveAgentsLocked()
	if len(live) != 2 || live[0].Agent != "codex" || live[1].Context != 25 || live[1].Companies != "acme" || live[1].Where != "api" {
		t.Fatalf("live = %+v", live)
	}
	if got := s.liveCostLocked(); got < 1.49 || got > 1.51 {
		t.Errorf("the status line's cost is %v", got)
	}
}
