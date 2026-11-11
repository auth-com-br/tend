package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestUsageIsSaidInAFewCharacters: an agent's spending in the sidebar's
// words — the context as a share of its window, tokens the way agents count
// them, a cost marked as a floor when a model has no price, a limit, a
// reached one with when it resets — and nothing for what is not known. If it
// regresses, the sidebar's agent row runs out of room or says "ctx -1%".
func TestUsageIsSaidInAFewCharacters(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	u := FormatUsage(&proto.PaneUsage{Tokens: 1_234_567, Context: 170_000, Window: 200_000, Cost: 3.104, CostKnown: true,
		Limit: &proto.UsageLimit{UsedPercent: 61.7}}, now)
	if u.Context != "ctx 85%" || u.ContextLevel != 1 || u.Spent != "1.2M tok" || u.Cost != "$3.10" || u.Limit != "limit 61%" || u.LimitLevel != 0 {
		t.Errorf("%+v", u)
	}
	u = FormatUsage(&proto.PaneUsage{Tokens: 950, Context: 10, Cost: 0.5,
		Limit: &proto.UsageLimit{UsedPercent: 100, Reached: true, Resets: now.Add(3*time.Hour + 20*time.Minute).Unix()}}, now)
	if u.Context != "" || u.Spent != "950 tok" || u.Cost != "≥$0.50" || u.Limit != "limit hit · in 3h 20m" || u.LimitLevel != 2 {
		t.Errorf("%+v", u)
	}
	if (FormatUsage(nil, now) != UsageText{}) {
		t.Error("nothing known should say nothing")
	}
	for n, want := range map[int64]string{999: "999", 12_345: "12k", 1_234: "1.2k", 50_000: "50k", 123_456: "123k", 2_000_000: "2M", 3_400_000_000: "3.4G"} {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestTheDefaultAgentRowSaysHowFullTheContextIs: with no layout configured,
// an agent's second row is its name and how full its context is, the gauge
// in the working colour from 80% and the blocked one from 95%; with nothing
// known the row is herdr's. If it regresses, the one number that says an
// agent is about to lose its thread is not on screen.
func TestTheDefaultAgentRowSaysHowFullTheContextIs(t *testing.T) {
	theme := DefaultTheme()
	draw := func(u *proto.PaneUsage) (string, vt.Style) {
		ut := FormatUsage(u, time.Now())
		lines := ResolveAgentRows(config.DefaultAgentRows(), AgentTokenValues{
			Workspace: "api", Tab: "build", Agent: "claude",
			Context: ut.Context, ContextLevel: ut.ContextLevel,
		})
		f := Frame{Sidebar: true, Agents: SidebarSection{Rows: []SidebarRow{{
			Kind: SidebarAgent, Label: "x", State: "working", Running: true, Lines: lines,
		}}}}
		dst := vt.NewGrid(80, 12, 0)
		Draw(dst, f, theme)
		for y, line := range gridText(dst) {
			if x := strings.Index(line, "ctx"); x >= 0 {
				return line, dst.Line(y).Cell(len([]rune(line[:x]))).Style
			}
		}
		return strings.Join(gridText(dst), "\n"), vt.Style{}
	}
	line, st := draw(&proto.PaneUsage{Context: 170_000, Window: 200_000})
	if !strings.Contains(line, "claude · ctx 85%") || st.FG != theme.Working.FG {
		t.Errorf("at 85%%: %q, colour %v", line, st.FG)
	}
	if _, st = draw(&proto.PaneUsage{Context: 196_000, Window: 200_000}); st.FG != theme.Blocked.FG {
		t.Errorf("at 98%%: colour %v", st.FG)
	}
	if line, _ := draw(nil); strings.Contains(line, "ctx") || !strings.Contains(line, "claude") {
		t.Errorf("unknown:\n%s", line)
	}
}
