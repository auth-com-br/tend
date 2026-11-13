package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/session"
)

// TestAProgramThatSaysItsStateIsBelieved: a pane whose program reports
// itself over OSC 7501 — the blocked report recorded from the real Claude
// Code — is that agent, in that state, with what it waits on as the
// message; and the conversation its hook named before is still the one
// known, so its usage and its resume after a restart are not lost. If it
// regresses, an agent's own word for its state is ignored for the screen's,
// or a status report forgets the conversation.
func TestAProgramThatSaysItsStateIsBelieved(t *testing.T) {
	s := newServer(t)
	dir := t.TempDir()
	report := filepath.Join(dir, "blocked")
	blocked := "\x1b]7501;state=blocked:app=claude-code:kind=permission:msg=YXBwcm92ZSBCYXNoOiB0b3VjaCBvc2MtcHJvYmUudHh0\x1b\\"
	if err := os.WriteFile(report, []byte(blocked), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, _ := s.NewWorkspace("w")
	_, pane, err := s.NewTab(ws, "t", PaneSpec{Command: []string{"/bin/sh", "-c", "read go; cat " + report + "; sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgentSession(pane, "tend:claude", "claude", agent.SessionRef{ID: "c-9"}, u64(1)); err != nil {
		t.Fatal(err)
	}
	if err := s.SendText(pane, "go\r"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the reported state", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Agent == "claude" && st.State == detect.StateBlocked
	})
	if st, _ := s.PaneStatus(pane); st.Message != "approve Bash: touch osc-probe.txt" {
		t.Errorf("message = %q", st.Message)
	}
	if known, ok := s.AgentSessionOf(pane); !ok || known.Session.ID != "c-9" || known.Source != "tend:claude" {
		t.Errorf("the conversation the hook named, by the hook, was lost: %+v %v", known, ok)
	}
	// What a restart resumes it with: only the hook's own source may.
	if argv, ok := agent.Resume(mustSession(t, s, pane)); !ok || strings.Join(argv, " ") != "claude --resume c-9" {
		t.Errorf("resumed with %q (%v)", argv, ok)
	}
}

// TestAProgramInAPaneHasItsQuestionsAnswered: a program asking its
// terminal where the cursor is, or whether it reads OSC 7501, gets the
// answer. They were never wired — the screen answered and nothing took the
// answer to the program — so every query from a pane went unanswered,
// and Claude Code, whose probe went unanswered, never said its state. If it
// regresses, programs that ask their terminal wait out a timeout, or give
// up on what they would have used.
func TestAProgramInAPaneHasItsQuestionsAnswered(t *testing.T) {
	s := newServer(t)
	ws, _ := s.NewWorkspace("w")
	script := `stty raw -echo; printf '\033[6n\033]7501;?\033\\'; ` +
		`a=$(dd bs=1 count=16 2>/dev/null | od -An -c | tr -d ' \n'); stty sane; printf '\n<%s>\n' "$a"; sleep 30`
	_, pane, err := s.NewTab(ws, "t", PaneSpec{Command: []string{"/bin/sh", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the answers, printed", func() bool {
		text, _ := s.ScreenText(pane)
		return strings.Contains(text, `<033[1;1R033]7501;?033\>`)
	})
}

func mustSession(t *testing.T, s *Server, pane session.PaneID) agent.PersistedSession {
	t.Helper()
	known, ok := s.AgentSessionOf(pane)
	if !ok {
		t.Fatal("no conversation")
	}
	return known
}

// TestAProgramsStatusKeepsTheConversationsAHookNames: with a program's
// status believed, a new conversation its hook names (after /clear, say)
// replaces the old one, and the status cleared as the program exits leaves
// the conversation to resume. If it regresses, tend resumes yesterday's
// conversation, or none, after a restart.
func TestAProgramsStatusKeepsTheConversationsAHookNames(t *testing.T) {
	s := newServer(t)
	ws, _ := s.NewWorkspace("w")
	_, pane, err := s.NewTab(ws, "t", PaneSpec{Command: []string{"/bin/sh", "-c", "read a; printf '\033]7501;state=working:app=claude-code\033\\\\'; read b; printf '\033]7501;state=clear\033\\\\'; sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgentSession(pane, "tend:claude", "claude", agent.SessionRef{ID: "old"}, u64(1)); err != nil {
		t.Fatal(err)
	}
	_ = s.SendText(pane, "1\r")
	waitFor(t, "working, from the program", func() bool {
		st, _ := s.PaneStatus(pane)
		return st.State == detect.StateWorking
	})
	if _, err := s.ReportAgentSession(pane, "tend:claude", "claude", agent.SessionRef{ID: "new"}, u64(2)); err != nil {
		t.Fatal(err)
	}
	if got := mustSession(t, s, pane); got.Session.ID != "new" || got.Source != "tend:claude" {
		t.Errorf("after the hook named another: %+v", got)
	}
	_ = s.SendText(pane, "2\r")
	waitFor(t, "the status cleared", func() bool {
		st, _ := s.PaneStatus(pane)
		return st.State != detect.StateWorking
	})
	if got := mustSession(t, s, pane); got.Session.ID != "new" || got.Source != "tend:claude" {
		t.Errorf("after the status cleared: %+v", got)
	}
}
