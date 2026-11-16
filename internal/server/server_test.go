//go:build unix

package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/vt"
)

// newServer returns a server with a fast detection tick, so tests observe
// state changes without waiting on the production cadence.
func newServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{
		DetectInterval: 10 * time.Millisecond,
		AdoptInterval:  20 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// shell returns a spec running a shell command.
func shell(script string) PaneSpec {
	return PaneSpec{Command: []string{"/bin/sh", "-c", script}}
}

// openTab is the common setup: a workspace with one tab running script.
func openTab(t *testing.T, s *Server, script string) (session.TabID, session.PaneID) {
	t.Helper()
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	tab, pane, err := s.NewTab(ws, "shell", shell(script))
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	return tab, pane
}

// waitFor polls until cond holds, failing the test if it never does. Every
// wait in this file is bounded: a hung server must fail a test rather than
// hang the suite.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// waitForEvent drains a subscription until an event matches, or fails.
func waitForEvent(t *testing.T, sub *Subscription, match func(Event) bool) Event {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-sub.C:
			if !ok {
				t.Fatal("subscription closed before the event arrived")
			}
			if match(ev) {
				return ev
			}
		case <-timeout:
			t.Fatal("timed out waiting for an event")
		}
	}
}

func TestNewServerIsEmpty(t *testing.T) {
	s := newServer(t)
	if got := s.Statuses(); len(got) != 0 {
		t.Errorf("Statuses = %v, want none", got)
	}
	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants: %v", err)
		}
		if sess.PaneCount() != 0 {
			t.Errorf("PaneCount = %d, want 0", sess.PaneCount())
		}
	})
}

func TestNewTabStartsAProcess(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "printf hello; sleep 5")

	waitFor(t, "output to reach the screen", func() bool {
		text, err := s.ScreenText(pane)
		return err == nil && strings.Contains(text, "hello")
	})

	st, err := s.PaneStatus(pane)
	if err != nil {
		t.Fatalf("PaneStatus: %v", err)
	}
	if !st.Running {
		t.Error("the pane should be running")
	}
	if st.Pid == 0 {
		t.Error("the pane should have a pid")
	}
}

// TestNewTabRollsBackAFailedStart: a tab whose only pane never started is not
// a tab the user asked for, so the session must not keep it.
func TestNewTabRollsBackAFailedStart(t *testing.T) {
	s := newServer(t)
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = s.NewTab(ws, "broken", PaneSpec{Command: []string{"/nonexistent/command"}})
	if err == nil {
		t.Fatal("starting a missing command should fail")
	}

	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants after a failed start: %v", err)
		}
		if sess.PaneCount() != 0 {
			t.Errorf("PaneCount = %d, want 0 after rollback", sess.PaneCount())
		}
		if w, _ := sess.Workspace(ws); len(w.Tabs()) != 0 {
			t.Errorf("workspace kept %d tabs after rollback", len(w.Tabs()))
		}
	})
	if got := s.Statuses(); len(got) != 0 {
		t.Errorf("Statuses = %v, want none", got)
	}
}

// TestAPaneWithNoCommandRunsTheServersShell: a client on another machine
// names no command, and the pane runs the shell of the machine it is on. It
// used to be refused, and a client that named its own shell instead sent one
// the far machine did not have.
func TestAPaneWithNoCommandRunsTheServersShell(t *testing.T) {
	cfg := handoffConfig()
	cfg.Shell = []string{"/bin/sh"}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ws, _ := s.NewWorkspace("main")
	_, pane, err := s.NewTab(ws, "x", PaneSpec{})
	if err != nil {
		t.Fatalf("a pane with no command: %v", err)
	}
	var command []string
	s.Session(func(sess *session.Session) {
		if p, ok := sess.Pane(pane); ok {
			command = p.Command
		}
	})
	if len(command) != 1 || command[0] != "/bin/sh" {
		t.Errorf("the pane records %v, want the server's shell", command)
	}
}

func TestSplitPaneStartsASecondProcess(t *testing.T) {
	s := newServer(t)
	_, first := openTab(t, s, "sleep 5")

	second, err := s.SplitPane(first, session.Columns, shell("printf second; sleep 5"))
	if err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	waitFor(t, "the second pane to produce output", func() bool {
		text, err := s.ScreenText(second)
		return err == nil && strings.Contains(text, "second")
	})

	if got := len(s.Statuses()); got != 2 {
		t.Errorf("got %d panes, want 2", got)
	}
	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants: %v", err)
		}
	})
}

// TestSplitRollsBackAFailedStart: the same rollback as a tab, but the pane
// must not be left in a layout it has no process for.
func TestSplitRollsBackAFailedStart(t *testing.T) {
	s := newServer(t)
	_, first := openTab(t, s, "sleep 5")

	if _, err := s.SplitPane(first, session.Columns, PaneSpec{Command: []string{"/nonexistent"}}); err == nil {
		t.Fatal("expected the split to fail")
	}
	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants: %v", err)
		}
		if sess.PaneCount() != 1 {
			t.Errorf("PaneCount = %d, want 1", sess.PaneCount())
		}
	})
	if got := len(s.Statuses()); got != 1 {
		t.Errorf("got %d runtimes, want 1", got)
	}
}

func TestWriteReachesTheProcess(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, `read line; printf 'echoed:%s' "$line"; sleep 5`)

	if err := s.Write(pane, []byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the process to echo the input", func() bool {
		text, err := s.ScreenText(pane)
		return err == nil && strings.Contains(text, "echoed:ping")
	})
}

func TestResize(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "sleep 5")

	if err := s.Resize(pane, pty.Size{Cols: 100, Rows: 30}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	var cols, rows int
	if err := s.WithScreen(pane, func(scr *vt.Screen) { cols, rows = scr.Size() }); err != nil {
		t.Fatalf("WithScreen: %v", err)
	}
	if cols != 100 || rows != 30 {
		t.Errorf("screen = %dx%d, want 100x30", cols, rows)
	}
	// An invalid size is ignored rather than failing.
	if err := s.Resize(pane, pty.Size{}); err != nil {
		t.Errorf("Resize with a zero size: %v", err)
	}
}

func TestClosePaneStopsIt(t *testing.T) {
	s := newServer(t)
	_, first := openTab(t, s, "sleep 30")
	second, err := s.SplitPane(first, session.Rows, shell("sleep 30"))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ClosePane(second); err != nil {
		t.Fatalf("ClosePane: %v", err)
	}
	if _, err := s.PaneStatus(second); !errors.Is(err, session.ErrNoSuchPane) {
		t.Errorf("err = %v, want ErrNoSuchPane", err)
	}
	if got := len(s.Statuses()); got != 1 {
		t.Errorf("got %d panes, want 1", got)
	}
	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants: %v", err)
		}
	})
}

func TestCloseTabStopsEveryPane(t *testing.T) {
	s := newServer(t)
	tab, first := openTab(t, s, "sleep 30")
	if _, err := s.SplitPane(first, session.Columns, shell("sleep 30")); err != nil {
		t.Fatal(err)
	}

	if err := s.CloseTab(tab); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if got := len(s.Statuses()); got != 0 {
		t.Errorf("got %d panes, want 0", got)
	}
	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants: %v", err)
		}
	})
}

// TestPaneRecordSurvivesItsProcess: a pane's identity and place in the layout
// outlive the process, so a user can see that something exited rather than
// having it vanish.
func TestPaneRecordSurvivesItsProcess(t *testing.T) {
	s := newServer(t)
	sub := s.Subscribe(64)
	defer sub.Close()

	_, pane := openTab(t, s, "printf bye")

	waitForEvent(t, sub, func(ev Event) bool {
		return ev.Kind == EventPaneExited && ev.Pane == pane
	})

	st, err := s.PaneStatus(pane)
	if err != nil {
		t.Fatalf("the pane record should remain: %v", err)
	}
	if st.Running {
		t.Error("the pane should be marked stopped")
	}
	s.Session(func(sess *session.Session) {
		if _, ok := sess.Pane(pane); !ok {
			t.Error("the session should still hold the pane")
		}
	})
}

func TestExitErrorIsReported(t *testing.T) {
	s := newServer(t)
	sub := s.Subscribe(64)
	defer sub.Close()

	_, pane := openTab(t, s, "exit 3")

	ev := waitForEvent(t, sub, func(ev Event) bool {
		return ev.Kind == EventPaneExited && ev.Pane == pane
	})
	if ev.Err == "" {
		t.Error("a non-zero exit should be reported")
	}
	st, _ := s.PaneStatus(pane)
	if st.ExitErr == "" {
		t.Error("the status should carry the exit error")
	}
}

// --- detection -------------------------------------------------------------

func TestDetectionReportsStateChanges(t *testing.T) {
	s := newServer(t)
	sub := s.Subscribe(64)
	defer sub.Close()

	ws, _ := s.NewWorkspace("main")
	// A spinner in the terminal title is what claude's highest-priority rule
	// reads, and it needs no screen drawing to trigger.
	_, pane, err := s.NewTab(ws, "agent", PaneSpec{
		Command: []string{"/bin/sh", "-c", `printf '\033]0;\342\240\201 Thinking\007'; sleep 5`},
		Agent:   "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	ev := waitForEvent(t, sub, func(ev Event) bool {
		return ev.Kind == EventPaneState && ev.Pane == pane && ev.State == detect.StateWorking
	})
	if ev.Rule == "" {
		t.Error("a state event should name the rule behind it")
	}

	st, _ := s.PaneStatus(pane)
	if st.State != detect.StateWorking {
		t.Errorf("status state = %v, want working", st.State)
	}
	if st.Agent != "claude" {
		t.Errorf("status agent = %q, want claude", st.Agent)
	}

	// The session record is updated too, since that is what a client lists.
	s.Session(func(sess *session.Session) {
		p, ok := sess.Pane(pane)
		if !ok {
			t.Fatal("pane missing from the session")
		}
		if p.State != detect.StateWorking {
			t.Errorf("session pane state = %v, want working", p.State)
		}
	})
}

func TestTitleReachesTheSession(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, `printf '\033]0;my-title\007'; sleep 5`)

	waitFor(t, "the title to reach the session", func() bool {
		var got string
		s.Session(func(sess *session.Session) {
			if p, ok := sess.Pane(pane); ok {
				got = p.Title
			}
		})
		return got == "my-title"
	})
}

// TestPaneWithoutAManifestStillRuns: most panes are not agents, and a shell
// must not fail to open because nobody wrote rules for it.
func TestPaneWithoutAManifestStillRuns(t *testing.T) {
	s := newServer(t)
	_, pane := openTab(t, s, "printf plain; sleep 5")

	waitFor(t, "output", func() bool {
		text, _ := s.ScreenText(pane)
		return strings.Contains(text, "plain")
	})
	st, _ := s.PaneStatus(pane)
	if st.Agent != "" {
		t.Errorf("agent = %q, want none", st.Agent)
	}
	if st.State != detect.StateUnknown {
		t.Errorf("state = %v, want unknown", st.State)
	}
}

func TestUnknownAgentIsRejected(t *testing.T) {
	s := newServer(t)
	ws, _ := s.NewWorkspace("main")
	_, _, err := s.NewTab(ws, "x", PaneSpec{
		Command: []string{"/bin/sh", "-c", "true"},
		Agent:   "no-such-agent",
	})
	if err == nil {
		t.Error("naming an agent that does not exist should fail")
	}
}

// --- events ----------------------------------------------------------------

// TestSlowSubscriberDoesNotStallTheServer is the guarantee that matters most
// about the event hub: a client that stops reading must lose events, not
// freeze the agents it is watching.
func TestSlowSubscriberDoesNotStallTheServer(t *testing.T) {
	s := newServer(t)
	slow := s.Subscribe(1) // never read from
	defer slow.Close()

	_, pane := openTab(t, s, "for i in 1 2 3 4 5 6 7 8 9 10; do printf 'line %s\\n' $i; done; sleep 5")

	waitFor(t, "output despite the stalled subscriber", func() bool {
		text, err := s.ScreenText(pane)
		return err == nil && strings.Contains(text, "line 10")
	})
	waitFor(t, "the subscriber to record dropped events", func() bool {
		return slow.Dropped() > 0
	})
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	s := newServer(t)
	sub := s.Subscribe(4)
	sub.Close()
	sub.Close() // must not panic or close a closed channel

	if _, ok := <-sub.C; ok {
		t.Error("a closed subscription should yield no events")
	}
}

func TestEventsEndOnShutdown(t *testing.T) {
	s, err := New(Config{DetectInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	sub := s.Subscribe(4)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-sub.C:
			if !ok {
				return // closed, as it should be
			}
		case <-deadline:
			t.Fatal("the subscription did not end on shutdown")
		}
	}
}

// --- lifecycle -------------------------------------------------------------

func TestCloseStopsEveryPane(t *testing.T) {
	s, err := New(Config{DetectInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := s.NewWorkspace("main")
	if _, _, err := s.NewTab(ws, "a", shell("sleep 30")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.NewTab(ws, "b", shell("sleep 30")); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; a reader goroutine is stuck")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestOperationsAfterCloseFail(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := s.NewWorkspace("main")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.NewWorkspace("late"); !errors.Is(err, ErrClosed) {
		t.Errorf("NewWorkspace: %v, want ErrClosed", err)
	}
	if _, _, err := s.NewTab(ws, "late", shell("true")); !errors.Is(err, ErrClosed) {
		t.Errorf("NewTab: %v, want ErrClosed", err)
	}
	if err := s.Write(session.PaneID(1), nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Write: %v, want ErrClosed", err)
	}
}

// TestConcurrentUse drives the server from several goroutines at once. The
// value is not any single assertion but running the whole thing under -race:
// this is the first package where two goroutines touch the same state.
func TestConcurrentUse(t *testing.T) {
	s := newServer(t)
	sub := s.Subscribe(256)
	defer sub.Close()

	_, first := openTab(t, s, "sleep 10")

	var wg sync.WaitGroup
	panes := make(chan session.PaneID, 8)

	// Splitters.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.SplitPane(first, session.Columns, shell("sleep 10"))
			if err != nil {
				return // losing a race to a closing pane is fine
			}
			panes <- id
		}()
	}

	// Readers hammering the accessors while the tree changes underneath.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = s.Statuses()
				_, _ = s.ScreenText(first)
				s.Session(func(sess *session.Session) {
					if err := sess.CheckInvariants(); err != nil {
						t.Errorf("invariants during concurrent use: %v", err)
					}
				})
			}
		}()
	}

	// Writers and resizers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			_ = s.Write(first, []byte("x"))
			_ = s.Resize(first, pty.Size{Cols: uint16(80 + j%20), Rows: 24})
		}
	}()

	// Drain events so the hub is exercised rather than just dropping.
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(3 * time.Second)
		for {
			select {
			case _, ok := <-sub.C:
				if !ok {
					return
				}
			case <-timeout:
				return
			}
		}
	}()

	wg.Wait()
	close(panes)

	for id := range panes {
		_ = s.ClosePane(id)
	}
	s.Session(func(sess *session.Session) {
		if err := sess.CheckInvariants(); err != nil {
			t.Errorf("invariants after concurrent use: %v", err)
		}
	})
}

// --- adopting an agent -----------------------------------------------------

// fakeAgent writes an executable with the given name and returns its path. A
// shell script is enough: Linux takes a process's name from the file that was
// executed, script or not.
func fakeAgent(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestAdoptsAnAgentStartedInAShell is the case that matters most, and the one
// that was missing: almost nobody opens a pane by naming an agent. They open a
// shell and type its name, which leaves the pane's command as the shell while
// the thing on screen is an agent.
func TestAdoptsAnAgentStartedInAShell(t *testing.T) {
	s := newServer(t)
	claude := fakeAgent(t, "claude", "printf '\\033]0;\\342\\240\\201 Thinking\\007'; sleep 10")

	_, pane := openTab(t, s, "exec sh -i")
	waitFor(t, "the shell to start", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Running
	})
	if st, _ := s.PaneStatus(pane); st.Agent != "" {
		t.Fatalf("a shell was identified as %q", st.Agent)
	}

	if err := s.Write(pane, []byte(claude+"\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent to be adopted", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Agent == "claude"
	})

	// And it is being watched, not merely labelled.
	waitFor(t, "its state to be detected", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.State == detect.StateWorking
	})
}

// TestReleasesTheAgentWhenItExits: a stale state left on screen is worse than
// none, because it looks live.
func TestReleasesTheAgentWhenItExits(t *testing.T) {
	s := newServer(t)
	claude := fakeAgent(t, "claude", "sleep 0.4")

	_, pane := openTab(t, s, "exec sh -i")
	waitFor(t, "the shell", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Running
	})
	if err := s.Write(pane, []byte(claude+"\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent to be adopted", func() bool {
		st, _ := s.PaneStatus(pane)
		return st.Agent == "claude"
	})
	waitFor(t, "the agent to be released", func() bool {
		st, _ := s.PaneStatus(pane)
		return st.Agent == ""
	})
}

// TestKeepsAnExplicitAgentWhileItWorks: a pane opened as an agent must not
// lose it the moment that agent runs a command of its own.
func TestKeepsAnExplicitAgentWhileItWorks(t *testing.T) {
	s := newServer(t)
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	_, pane, err := s.NewTab(ws, "agent", PaneSpec{
		Command: []string{"/bin/sh", "-c", "sleep 10"},
		Agent:   "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane to settle", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Running
	})
	time.Sleep(300 * time.Millisecond) // several chances to get it wrong

	if st, _ := s.PaneStatus(pane); st.Agent != "claude" {
		t.Errorf("agent = %q, want it kept as claude", st.Agent)
	}
}

// TestANewTabOpensWhereThePaneItCameFromIsWorking: a tab opened from a
// pane, and a split of one, start in the directory that pane's program is
// in now — here one it moved to after it started — as herdr's follow_cwd
// does. If it regresses, a new tab next to an agent working in a project
// opens in whatever directory tend was first started from.
func TestANewTabOpensWhereThePaneItCameFromIsWorking(t *testing.T) {
	s := newServer(t)
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	_, pane, err := s.NewTab(ws, "shell", shell("cd '"+project+"' && exec sleep 60"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane in the project", func() bool {
		s.mu.Lock()
		rt := s.runtimes[pane]
		s.mu.Unlock()
		return rt != nil && rt.pty.Cwd() == project
	})

	cwdOf := func(id session.PaneID) func() bool {
		return func() bool {
			c, err := s.PaneContext(id)
			return err == nil && c.Cwd == project
		}
	}
	_, opened, err := s.NewTab(ws, "next", PaneSpec{Command: []string{"/bin/sh", "-c", "exec sleep 60"}, DirOf: pane})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the new tab in the project", cwdOf(opened))

	split, err := s.SplitPane(pane, session.Columns, shell("exec sleep 60"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the split in the project", cwdOf(split))

	// A directory given is kept: following is for when none is.
	elsewhere, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, named, err := s.NewTab(ws, "named", PaneSpec{Command: []string{"/bin/sh", "-c", "exec sleep 60"}, Dir: elsewhere, DirOf: pane})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the given directory", func() bool {
		c, err := s.PaneContext(named)
		return err == nil && c.Cwd == elsewhere
	})
}

// TestAdoptsAnAgentRunInAContainer: a pane whose foreground program is
// docker, running `exec -it box claude`, is Claude Code (herdr#699, issue
// #53). The "docker" here is a stand-in that prints what Claude Code's
// title says and waits, named docker so the foreground is: no container is
// run. If it regresses, an agent in a container has no state in tend.
func TestAdoptsAnAgentRunInAContainer(t *testing.T) {
	s := newServer(t)
	docker := fakeAgent(t, "docker", "printf '\\033]0;\\342\\240\\201 Thinking\\007'; sleep 10")

	_, pane := openTab(t, s, "exec sh -i")
	waitFor(t, "the shell to start", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Running
	})
	if err := s.Write(pane, []byte(docker+" exec -it box claude --resume\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent in the container to be adopted", func() bool {
		st, err := s.PaneStatus(pane)
		return err == nil && st.Agent == "claude"
	})
}
