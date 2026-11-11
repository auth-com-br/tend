package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/proto"
)

// promptReply is a Claude Code reply whose prompt was context tokens.
func promptReply(id string, context int) string {
	return `{"type":"assistant","message":{"id":"` + id + `","model":"claude-sonnet-4-6","usage":{"input_tokens":` +
		itoa(uint64(context)) + `,"output_tokens":10}}}` + "\n"
}

func paneUsage(s *Server, pane uint64) *proto.PaneUsage {
	for _, p := range s.Snapshot().Panes {
		if p.ID == pane {
			return p.Usage
		}
	}
	return nil
}

// TestAnAgentsSpendingIsShownAndAFullContextToldOf: an agent whose hook
// named its conversation has, in the snapshot, the conversation's tokens,
// how full its context is and an estimated cost, kept up to date as the
// conversation grows; its context passing 80% is told once, with where the
// agent is, and passing 95% once more; when the agent is gone, so is the
// number. If it regresses, an agent fills its context or spends without a
// sign until it stops.
func TestAnAgentsSpendingIsShownAndAFullContextToldOf(t *testing.T) {
	log := filepath.Join(t.TempDir(), "activity.jsonl")
	transcript := filepath.Join(t.TempDir(), "c1.jsonl")
	if err := os.WriteFile(transcript, []byte(promptReply("a", 100_000)), 0o600); err != nil {
		t.Fatal(err)
	}
	s := activityServer(t, log, policy.Policy{}, func(label string, c activity.Conversation) string {
		if label == "claude" && c.ID == "c1" {
			return transcript
		}
		return ""
	})
	sub := s.Subscribe(1024)
	defer sub.Close()
	ws, _ := s.NewWorkspace("audit")
	_, pane, err := s.NewTab(ws, "work", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgent(pane, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgentSession(pane, "test", "claude", agent.SessionRef{ID: "c1"}, u64(2)); err != nil {
		t.Fatal(err)
	}
	id := uint64(pane)
	waitFor(t, "the usage", func() bool { return paneUsage(s, id) != nil })
	u := paneUsage(s, id)
	if u.Tokens != 100_010 || u.ContextPercent() != 50 || u.Cost <= 0 || !u.CostKnown || u.Model != "claude-sonnet-4-6" {
		t.Errorf("usage = %+v (%d%%)", u, u.ContextPercent())
	}

	add := func(line string) {
		f, _ := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
		_, _ = f.WriteString(line)
		f.Close()
	}
	add(promptReply("b", 170_000))
	ev := waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventNotify && ev.Pane == pane })
	if ev.Title != "claude context 85% full" || ev.Body != "audit › work" {
		t.Errorf("notice = %q / %q", ev.Title, ev.Body)
	}
	add(promptReply("c", 172_000))
	add(promptReply("d", 192_000))
	ev = waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventNotify && ev.Pane == pane })
	if !strings.Contains(ev.Title, "96% full") {
		t.Errorf("the second notice = %q (86%% said again?)", ev.Title)
	}
	waitFor(t, "the new count", func() bool { return paneUsage(s, id).ContextPercent() == 96 })

	// The agent leaves; the pane stays, a shell.
	if _, err := s.ReleaseAgent(pane, "test", "claude", u64(3)); err != nil {
		t.Fatal(err)
	}
	records(t, log, "the stop", func(r activity.Record) bool { return r.Kind == activity.KindStop })
	waitFor(t, "the usage gone", func() bool { return paneUsage(s, id) == nil })
}

// TestAWarningIsArmedAgainOnceTheGaugeFalls: a context compacted below the
// threshold, or a limit that started over, is warned of again the next time
// it crosses; a reached limit is told as reached, with when it resets.
func TestAWarningIsArmedAgainOnceTheGaugeFalls(t *testing.T) {
	run := &openRun{warned: map[string]bool{}}
	at := func(context int64) []string { return run.warnings(&proto.PaneUsage{Context: context, Window: 100}) }
	if got := at(81); len(got) != 1 {
		t.Fatalf("81%%: %q", got)
	}
	if got := at(85); len(got) != 0 {
		t.Errorf("85%% after 81%%: %q", got)
	}
	at(20) // compacted
	if got := at(82); len(got) != 1 {
		t.Errorf("82%% after compacting: %q", got)
	}
	limit := func(pct float64, reached bool) []string {
		return run.warnings(&proto.PaneUsage{Limit: &proto.UsageLimit{UsedPercent: pct, Reached: reached, Resets: 1794787010}})
	}
	if got := limit(92, false); len(got) != 1 || !strings.Contains(got[0], "92% of its usage limit · resets ") {
		t.Errorf("92%%: %q", got)
	}
	if got := limit(100, true); len(got) != 1 || !strings.HasPrefix(got[0], "hit its usage limit") {
		t.Errorf("reached: %q", got)
	}
	if got := limit(100, true); len(got) != 0 {
		t.Errorf("reached twice: %q", got)
	}
	limit(3, false) // started over
	if got := limit(95, false); len(got) != 1 {
		t.Errorf("after the reset: %q", got)
	}
}
