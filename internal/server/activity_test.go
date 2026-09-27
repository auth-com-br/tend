//go:build unix

package server

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
)

// gitRepo makes a repository with one commit, on branch.
func gitRepo(t *testing.T, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "one")
	return dir
}

// activityServer is a server keeping its record in a file of the test's, with
// the rules given, under the session name "s".
func activityServer(t *testing.T, log string, rules policy.Policy, transcripts activity.Transcripts) *Server {
	t.Helper()
	s, err := New(Config{
		DetectInterval:   10 * time.Millisecond,
		AdoptInterval:    20 * time.Millisecond,
		DefaultSize:      pty.Size{Cols: 80, Rows: 24},
		Activity:         activity.NewLog(log),
		SessionName:      "s",
		ActivityInterval: 10 * time.Millisecond,
		ActivityGrace:    300 * time.Millisecond,
		Transcripts:      transcripts,
		Policy:           func() policy.Policy { return rules },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// records waits until the record has a line that matches, and returns them
// all.
func records(t *testing.T, path, what string, match func(activity.Record) bool) []activity.Record {
	t.Helper()
	var recs []activity.Record
	waitFor(t, what, func() bool {
		recs, _ = activity.Read(path)
		for _, r := range recs {
			if match(r) {
				return true
			}
		}
		return false
	})
	return recs
}

func kinds(recs []activity.Record, kind string) []activity.Record {
	var out []activity.Record
	for _, r := range recs {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func claudeReply(id string, output int) string {
	return `{"type":"assistant","message":{"id":"` + id + `","model":"claude-opus-5-5","usage":{"input_tokens":1,"output_tokens":` + itoa(uint64(output)) + `}}}` + "\n"
}

// TestAnAgentsRunIsRecordedFromStartToClose: the whole of the record's
// promise, in one run. Where the agent worked, the conversation its hook
// named, what changed in its checkout and what it spent — the spending
// counted from where the conversation was when the run began, so a resumed
// conversation does not bill its past to this run. If it regresses, `tend
// activity` reports runs with no files, no cost, or somebody else's cost.
func TestAnAgentsRunIsRecordedFromStartToClose(t *testing.T) {
	repo := gitRepo(t, "issue-5-audit")
	log := filepath.Join(t.TempDir(), "activity.jsonl")
	transcript := filepath.Join(t.TempDir(), "c1.jsonl")
	if err := os.WriteFile(transcript, []byte(claudeReply("old", 1000)), 0o600); err != nil {
		t.Fatal(err)
	}
	s := activityServer(t, log, policy.Policy{}, func(label string, c activity.Conversation) string {
		if label == "claude" && c.ID == "c1" {
			return transcript
		}
		return ""
	})

	ws, _ := s.NewWorkspace("audit")
	_, pane, err := s.NewTab(ws, "work", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgent(pane, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	recs := records(t, log, "the start", func(r activity.Record) bool { return r.Kind == activity.KindStart })
	start := kinds(recs, activity.KindStart)[0]
	if start.Agent != "claude" || start.Branch != "issue-5-audit" || start.Space != "audit" || start.Tab != "work" || start.Head == "" {
		t.Errorf("start = %+v", start)
	}

	if _, err := s.ReportAgentSession(pane, "test", "claude", agent.SessionRef{ID: "c1"}, u64(2)); err != nil {
		t.Fatal(err)
	}
	records(t, log, "the conversation", func(r activity.Record) bool { return r.Kind == activity.KindConversation })

	// The run's work: a reply, and a file.
	f, _ := os.OpenFile(transcript, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(claudeReply("new", 42))
	f.Close()
	if err := os.WriteFile(filepath.Join(repo, "audit.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.ClosePane(pane); err != nil {
		t.Fatal(err)
	}
	recs = records(t, log, "the stop", func(r activity.Record) bool { return r.Kind == activity.KindStop })
	stop := kinds(recs, activity.KindStop)[0]
	if stop.Run != start.Run || stop.Reason != activity.ReasonClosed {
		t.Errorf("stop = %+v", stop)
	}
	if len(stop.Files) != 1 || stop.Files[0] != "audit.go" {
		t.Errorf("files = %v, want [audit.go]", stop.Files)
	}
	if got := stop.Usage["claude-opus-5-5"]; got.Output != 42 || got.Input != 1 {
		t.Errorf("usage = %+v, want only the reply made during the run", stop.Usage)
	}
}

// TestARunEndsWhenTheAgentsProcessDoes: a pane opened as `claude` keeps
// that name after claude exits, and the run used to go on forever — found
// with the real Claude Code, whose /exit left the record saying it was still
// running. The run ends when the process does, and an exited agent is not
// counted against a limit.
func TestARunEndsWhenTheAgentsProcessDoes(t *testing.T) {
	log := filepath.Join(t.TempDir(), "activity.jsonl")
	s := activityServer(t, log, policy.Policy{MaxAgents: 1}, nil)
	ws, _ := s.NewWorkspace("w")
	_, pane, err := s.NewTab(ws, "a", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 0.5"}, Dir: t.TempDir(), Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgent(pane, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	records(t, log, "the start", func(r activity.Record) bool { return r.Kind == activity.KindStart })
	recs := records(t, log, "the stop", func(r activity.Record) bool { return r.Kind == activity.KindStop })
	if stop := kinds(recs, activity.KindStop)[0]; stop.Reason != activity.ReasonExited {
		t.Errorf("stop = %+v, want the agent exited", stop)
	}
	if st, _ := s.PaneStatus(pane); st.Agent != "claude" {
		t.Logf("the pane is now %q; the test is about one that keeps its agent's name", st.Agent)
	}
	if _, _, err := s.NewTab(ws, "b", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: t.TempDir(), Agent: "claude"}); err != nil {
		t.Errorf("an exited agent counted against the limit: %v", err)
	}
}

// TestTendRefusesToStartAnAgentAgainstAPolicy: an agent tend is asked to
// start on a protected branch is not started, the caller is told why, and the
// refusal is written down. A pane that is not an agent is not held to rules
// about agents.
func TestTendRefusesToStartAnAgentAgainstAPolicy(t *testing.T) {
	repo := gitRepo(t, "main")
	log := filepath.Join(t.TempDir(), "activity.jsonl")
	s := activityServer(t, log, policy.Policy{ProtectedBranches: []string{"main"}}, nil)
	ws, _ := s.NewWorkspace("w")

	_, _, err := s.NewTab(ws, "agent", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: repo, Agent: "claude"})
	if !errors.Is(err, ErrPolicy) {
		t.Fatalf("NewTab = %v, want ErrPolicy", err)
	}
	s.Session(func(sess *session.Session) {
		if w, _ := sess.Workspace(ws); len(w.Tabs()) != 0 {
			t.Error("a tab was made for the agent that was refused")
		}
	})
	recs, _ := activity.Read(log)
	if r := kinds(recs, activity.KindRefused); len(r) != 1 || r[0].Policy != policy.RuleProtectedBranch || r[0].Agent != "claude" {
		t.Errorf("refusals = %+v", r)
	}

	if _, _, err := s.NewTab(ws, "shell", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: repo}); err != nil {
		t.Errorf("a shell was refused: %v", err)
	}
}

// TestTheAgentLimitCountsTheOthers: with a limit of one, the first agent
// starts and the second does not; and typing an agent into a pane is checked
// against the others, not against itself.
func TestTheAgentLimitCountsTheOthers(t *testing.T) {
	dir := t.TempDir()
	s := activityServer(t, filepath.Join(t.TempDir(), "activity.jsonl"), policy.Policy{MaxAgents: 1}, nil)
	ws, _ := s.NewWorkspace("w")
	_, first, err := s.NewTab(ws, "a", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: dir, Agent: "claude"})
	if err != nil {
		t.Fatalf("the first agent was refused: %v", err)
	}
	if _, err := s.ReportAgent(first, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitAgentIn(first, "claude"); err != nil {
		t.Errorf("the agent's own pane counted against it: %v", err)
	}
	if _, _, err := s.NewTab(ws, "b", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: dir, Agent: "codex"}); !errors.Is(err, ErrPolicy) {
		t.Errorf("the second agent: %v, want ErrPolicy", err)
	}
}

// TestAnAgentStartedByHandIsReportedNotStopped: somebody typed `claude` on
// main. tend did not start it and so could not refuse it; it says so, to
// whoever is watching and in the record, and leaves the pane alone.
func TestAnAgentStartedByHandIsReportedNotStopped(t *testing.T) {
	repo := gitRepo(t, "main")
	log := filepath.Join(t.TempDir(), "activity.jsonl")
	s := activityServer(t, log, policy.Policy{ProtectedBranches: []string{"main"}}, nil)
	sub := s.Subscribe(256)
	defer sub.Close()
	ws, _ := s.NewWorkspace("w")
	_, pane, err := s.NewTab(ws, "shell", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Dir: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgent(pane, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	ev := waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventNotify && ev.Pane == pane })
	if ev.Body == "" {
		t.Errorf("notice = %+v", ev)
	}
	recs := records(t, log, "the violation", func(r activity.Record) bool { return r.Kind == activity.KindViolation })
	if v := kinds(recs, activity.KindViolation)[0]; v.Policy != policy.RuleProtectedBranch || v.Run == "" {
		t.Errorf("violation = %+v", v)
	}
	if st, err := s.PaneStatus(pane); err != nil || !st.Running {
		t.Errorf("the pane was stopped: %+v, %v", st, err)
	}
}

// TestAReplacementServerCarriesOnTheRunsItInherits: after a handoff the
// agents are still running, in the same panes, under a new server. Their runs
// go on under the ids they had rather than starting again, which would split
// one piece of work in two and count its conversation twice; and a run whose
// agent is not there is ended as lost, not left running forever.
func TestAReplacementServerCarriesOnTheRunsItInherits(t *testing.T) {
	log := filepath.Join(t.TempDir(), "activity.jsonl")
	before := time.Now().Add(-time.Hour)
	if err := activity.NewLog(log).Append(
		activity.Record{Kind: activity.KindStart, Time: before, Run: "kept", Session: "s", Pane: 1, Agent: "claude"},
		activity.Record{Kind: activity.KindStart, Time: before, Run: "gone", Session: "s", Pane: 9, Agent: "codex"},
		activity.Record{Kind: activity.KindStart, Time: before, Run: "elsewhere", Session: "other", Pane: 1, Agent: "claude"},
	); err != nil {
		t.Fatal(err)
	}
	s := activityServer(t, log, policy.Policy{}, nil)
	ws, _ := s.NewWorkspace("w")
	_, pane, err := s.NewTab(ws, "a", shell("sleep 30"))
	if err != nil || pane != 1 {
		t.Fatalf("NewTab = %v, %v; the test needs pane 1", pane, err)
	}
	if _, err := s.ReportAgent(pane, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}

	recs := records(t, log, "the lost run's end", func(r activity.Record) bool {
		return r.Kind == activity.KindStop && r.Run == "gone"
	})
	if starts := kinds(recs, activity.KindStart); len(starts) != 3 {
		t.Errorf("starts = %d, want the 3 there were: the inherited run went on", len(starts))
	}
	for _, r := range kinds(recs, activity.KindStop) {
		if r.Run != "gone" || r.Reason != activity.ReasonLost {
			t.Errorf("stop = %+v", r)
		}
	}

	_ = s.Close()
	recs, _ = activity.Read(log)
	runs, _ := activity.Fold(recs)
	for _, r := range runs {
		switch r.ID {
		case "kept":
			if r.Running || r.Reason != activity.ReasonStopped {
				t.Errorf("kept = %+v, want ended by the server stopping", r)
			}
		case "elsewhere":
			if !r.Running {
				t.Error("a run of another session was ended")
			}
		}
	}
}
