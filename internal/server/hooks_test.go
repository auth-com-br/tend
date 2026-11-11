//go:build unix

package server

import (
	"testing"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
)

func u64(n uint64) *uint64 { return &n }

// TestAHookReportReachesTheSessionAndItsClients: a report that stops at the
// arbiter is one nobody sees. It has to change the pane's record and tell
// subscribers, or the sidebar goes on showing what the screen last suggested.
func TestAHookReportReachesTheSessionAndItsClients(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 30")
	sub := s.Subscribe(64)
	defer sub.Close()

	ok, err := s.ReportAgent(pane, agent.Report{
		Source: "my-script", Agent: " deploy-bot ", State: detect.StateBlocked,
		Message: "needs approval", Seq: u64(1),
	})
	if err != nil || !ok {
		t.Fatalf("ReportAgent = %v, %v", ok, err)
	}
	ev := waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventPaneState && ev.Pane == pane })
	if ev.State != detect.StateBlocked || ev.Rule != "hook:my-script" {
		t.Errorf("event = %+v, want blocked by hook:my-script", ev)
	}

	st, _ := s.PaneStatus(pane)
	if st.Agent != "deploy-bot" || st.State != detect.StateBlocked || st.Message != "needs approval" {
		t.Errorf("status = %+v", st)
	}
	s.Session(func(sess *session.Session) {
		if p, _ := sess.Pane(pane); p.Agent != "deploy-bot" || p.State != detect.StateBlocked {
			t.Errorf("session pane = %+v", p)
		}
	})

	// Released by the one who reported it, the pane is nobody's again.
	if ok, _ := s.ReleaseAgent(pane, "my-script", "deploy-bot", u64(2)); !ok {
		t.Fatal("the release was refused")
	}
	if st, _ := s.PaneStatus(pane); st.Agent != "" || st.State != detect.StateUnknown {
		t.Errorf("after release, status = %+v", st)
	}
}

// TestAReportMustNameAnAgentAndAPane: a hook with a bad environment must get an
// error it can log, not a silent success.
func TestAReportMustNameAnAgentAndAPane(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 30")
	if _, err := s.ReportAgent(pane, agent.Report{Source: "x", Agent: "  "}); err != ErrBadAgent {
		t.Errorf("err = %v, want ErrBadAgent", err)
	}
	if _, err := s.ReportAgent(pane+99, agent.Report{Source: "x", Agent: "a"}); err == nil {
		t.Error("a report for a pane that does not exist was accepted")
	}
}

// TestAHookCanSayWhatToShowBesideTheAgent: state is one word, and the sidebar
// in herdr is mostly the rest — the model, what is left of the context. If it
// regresses, a hook reporting those is ignored and the list is poorer for it.
func TestAHookCanSayWhatToShowBesideTheAgent(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 30")
	sub := s.Subscribe(64)
	defer sub.Close()

	value := func(v string) *string { return &v }
	if _, err := s.ReportMetadata(pane, agent.MetadataReport{
		Source: "my-hook", Agent: "deploy-bot",
		DisplayAgent: "deploy-bot (staging)",
		Tokens:       map[string]*string{"ctx": value("23%")},
		StateLabels:  map[string]string{"blocked": "waiting for approval"},
	}); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventPaneState && ev.Pane == pane })

	st, _ := s.PaneStatus(pane)
	if st.Presentation.DisplayAgent != "deploy-bot (staging)" {
		t.Errorf("display agent = %q", st.Presentation.DisplayAgent)
	}
	if len(st.Presentation.Tokens) != 1 || st.Presentation.Tokens[0].Value != "23%" {
		t.Errorf("tokens = %v", st.Presentation.Tokens)
	}
	if st.Presentation.StateLabels["blocked"] != "waiting for approval" {
		t.Errorf("state labels = %v", st.Presentation.StateLabels)
	}
	// Presentation is not state: what the pane is doing has not changed.
	if st.State != detect.StateUnknown {
		t.Errorf("metadata changed the pane's state to %v", st.State)
	}
}

// TestAnAgentThatFinishesInAnotherTabIsDone: the server marks an agent that
// finished while the client looked elsewhere, sends it in the snapshot, and
// forgets it once the client looks at that tab. If it regresses, the list
// cannot tell a fresh result from an agent idle since morning.
func TestAnAgentThatFinishesInAnotherTabIsDone(t *testing.T) {
	s := newServer(t)
	ws, _ := s.NewWorkspace("main")
	_, agentPane, err := s.NewTab(ws, "agent", shell("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := s.NewTab(ws, "other", shell("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	s.FocusPane(other, 0)

	report := func(state detect.State, seq uint64) {
		t.Helper()
		if _, err := s.ReportAgent(agentPane, agent.Report{Source: "h", Agent: "claude", State: state, Seq: u64(seq)}); err != nil {
			t.Fatal(err)
		}
	}
	done := func() bool {
		for _, p := range s.snapshot().Panes {
			if p.ID == uint64(agentPane) {
				return p.Done
			}
		}
		return false
	}
	report(detect.StateWorking, 1)
	report(detect.StateIdle, 2)
	if !done() {
		t.Fatal("finishing in a tab nobody is looking at should be done")
	}
	s.FocusPane(agentPane, other)
	if done() {
		t.Error("looking at its tab should clear it")
	}
}

// TestAnAgentThatFinishesWhileTheWindowIsAwayIsDone: with the terminal
// window behind something, the tab on screen is not being looked at, so an
// agent finishing there is done until the window comes back — herdr's rule.
// If it regresses, stepping away from the desk loses exactly the news that
// was waiting.
func TestAnAgentThatFinishesWhileTheWindowIsAwayIsDone(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 30")
	s.FocusPane(pane, 0)
	s.WindowFocus(false)

	for i, state := range []detect.State{detect.StateWorking, detect.StateIdle} {
		if _, err := s.ReportAgent(pane, agent.Report{Source: "h", Agent: "claude", State: state, Seq: u64(uint64(i + 1))}); err != nil {
			t.Fatal(err)
		}
	}
	done := func() bool {
		for _, p := range s.snapshot().Panes {
			if p.ID == uint64(pane) {
				return p.Done
			}
		}
		return false
	}
	if !done() {
		t.Fatal("finishing on screen with the window away should be done")
	}
	s.WindowFocus(true)
	if done() {
		t.Error("the window coming back should see it")
	}
}

// TestAnAgentMarkedUnreadSticksWhileItsTabIsOnScreen: marking unread the agent
// on screen is done (pane.mark_unread); moving focus within its tab and the
// window coming back leave the mark, and the snapshot shows the idle agent as
// done; going to another tab and back clears it. If it regresses, marking the
// agent in view undoes itself at the next keystroke, which is exactly the
// case herdr's users described.
func TestAnAgentMarkedUnreadSticksWhileItsTabIsOnScreen(t *testing.T) {
	s := newServer(t)
	ws, _ := s.NewWorkspace("main")
	_, agentPane, err := s.NewTab(ws, "agent", shell("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	beside, err := s.SplitPane(agentPane, session.Columns, shell("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := s.NewTab(ws, "other", shell("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgent(agentPane, agent.Report{Source: "h", Agent: "claude", State: detect.StateIdle, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	s.FocusPane(agentPane, 0)
	info := func() proto.PaneInfo {
		for _, p := range s.snapshot().Panes {
			if p.ID == uint64(agentPane) {
				return p
			}
		}
		return proto.PaneInfo{}
	}

	if err := s.MarkUnread(agentPane, true); err != nil {
		t.Fatal(err)
	}
	if p := info(); !p.Marked || !p.Done {
		t.Fatalf("marked unread and idle: marked=%v done=%v, want both", p.Marked, p.Done)
	}
	s.FocusPane(beside, agentPane)
	s.FocusPane(agentPane, beside)
	s.WindowFocus(false)
	s.WindowFocus(true)
	if !info().Marked {
		t.Fatal("focus moving within its tab, or the window coming back, should leave the mark")
	}
	s.FocusPane(other, agentPane)
	s.FocusPane(agentPane, other)
	if info().Marked {
		t.Error("arriving at its tab from another should clear the mark")
	}
}
