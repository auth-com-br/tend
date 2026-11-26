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

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/session"
)

// fakeClock is a wall clock a test moves by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// taskServer is a server on a fake clock, with a plain shell for its panes
// and short waits for a task's agent.
func taskServer(t *testing.T, state string, clock *fakeClock, rules policy.Policy) *Server {
	t.Helper()
	s, err := New(Config{
		StateFile:      state,
		DetectInterval: 10 * time.Millisecond,
		AdoptInterval:  20 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
		ShutdownGrace:  time.Second,
		Shell:          []string{"/bin/sh"},
		Now:            clock.Now,
		TaskReady:      5 * time.Second,
		TaskSettle:     100 * time.Millisecond,
		Policy:         func() policy.Policy { return rules },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func str(s string) *string { return &s }
func i64(n int64) *int64   { return &n }

func createTask(t *testing.T, s *Server, p proto.TaskParams) proto.TaskInfo {
	t.Helper()
	res, err := s.ChangeTask(proto.MethodTaskCreate, p)
	if err != nil {
		t.Fatalf("task.create: %v", err)
	}
	return res.(proto.TaskInfo)
}

func taskByID(s *Server, id uint64) (proto.TaskInfo, bool) {
	for _, t := range s.Tasks() {
		if t.ID == id {
			return t, true
		}
	}
	return proto.TaskInfo{}, false
}

// TestTasksAreToldToClientsAndSurviveARestart: a task made, edited and
// deleted over the task methods is what task.list answers, each change is
// announced with EventTasksChanged, and the tasks come back when the server
// is stopped and started on the same state file — with no space open, too.
// If it regresses, the planner the desktop and the phone show goes stale
// until reopened, or the work planned is lost with a restart.
func TestTasksAreToldToClientsAndSurviveARestart(t *testing.T) {
	state := filepath.Join(t.TempDir(), "work.json")
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	first := taskServer(t, state, clock, policy.Policy{})
	sub := first.Subscribe(64)
	defer sub.Close()

	kept := createTask(t, first, proto.TaskParams{Title: str("write the report"), Due: i64(1_800_086_400)})
	waitForEvent(t, sub, func(ev Event) bool { return ev.Kind == EventTasksChanged })
	gone := createTask(t, first, proto.TaskParams{Title: str("gone")})
	if _, err := first.ChangeTask(proto.MethodTaskUpdate, proto.TaskParams{Task: kept.ID, Status: str("done")}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ChangeTask(proto.MethodTaskDelete, proto.TaskParams{Task: gone.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ChangeTask(proto.MethodTaskUpdate, proto.TaskParams{Task: 999, Status: str("done")}); !errors.Is(err, session.ErrNoSuchTask) {
		t.Errorf("updating a task that does not exist: %v", err)
	}
	if _, err := first.ChangeTask(proto.MethodTaskCreate, proto.TaskParams{Title: str("x"), Agent: str("claude\nrm -rf ~")}); err == nil {
		t.Error("an agent with a line break in it was taken")
	}

	got := first.Tasks()
	if len(got) != 1 || got[0].ID != kept.ID || got[0].Status != "done" || got[0].Due != 1_800_086_400 {
		t.Fatalf("tasks = %+v", got)
	}

	first.saveStructure()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := taskServer(t, state, clock, policy.Policy{})
	if again := second.Tasks(); len(again) != 1 || again[0] != got[0] {
		t.Errorf("after a restart tasks = %+v, want %+v", again, got)
	}
}

// TestATasksAgentStartsAtItsTimeInItsSpace: a task given to an agent for a
// time starts nothing before it; when the time comes the agent is typed into
// a new tab of the task's space, in that space's folder, the task records the
// pane and is being worked on, and the prompt reaches the agent once it is
// ready. If it regresses, a task scheduled for the morning never starts, starts
// somewhere else, or starts with its task lost in the agent's start-up.
func TestATasksAgentStartsAtItsTimeInItsSpace(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	s := taskServer(t, "", clock, policy.Policy{})
	dir := t.TempDir()
	ws, err := s.NewWorkspaceIn("project", dir)
	if err != nil {
		t.Fatal(err)
	}
	// The stand-in agent says where it runs and writes what it is told.
	stand := fakeAgent(t, "stand-in", `pwd > started; read line; printf '%s' "$line" > prompted; sleep 30`)

	at := clock.Now().Add(time.Hour)
	task := createTask(t, s, proto.TaskParams{
		Title: str("review"), Agent: str(stand), Prompt: str("review the diff"),
		StartAt: i64(at.Unix()), Workspace: &[]uint64{uint64(ws)}[0],
	})
	time.Sleep(50 * time.Millisecond)
	if tabs := tabsIn(s, ws); tabs != 0 {
		t.Fatalf("the agent started before its time: %d tabs", tabs)
	}

	clock.set(at)
	s.tasksChanged()
	var started proto.TaskInfo
	waitFor(t, "the task to record its pane", func() bool {
		started, _ = taskByID(s, task.ID)
		return started.Pane != 0
	})
	if started.Status != "doing" || started.Workspace != uint64(ws) || started.Started != at.Unix() || started.Error != "" {
		t.Errorf("started task = %+v", started)
	}
	if tabs := tabsIn(s, ws); tabs != 1 {
		t.Errorf("the task's space has %d tabs, want the one opened for it", tabs)
	}
	waitFor(t, "the agent to start in the space's folder", func() bool {
		out, err := os.ReadFile(filepath.Join(dir, "started"))
		return err == nil && strings.TrimSpace(string(out)) == dir
	})

	// Ready, as a hook would say: the prompt goes in then, and not before.
	if _, err := os.Stat(filepath.Join(dir, "prompted")); err == nil {
		t.Fatal("the prompt was typed before the agent was ready")
	}
	if _, err := s.ReportAgent(session.PaneID(started.Pane), agent.Report{Source: "test", Agent: "claude", State: detect.StateIdle, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the prompt to reach the agent", func() bool {
		out, err := os.ReadFile(filepath.Join(dir, "prompted"))
		return err == nil && string(out) == "review the diff"
	})

	// Acted on once: the next look starts nothing more.
	clock.set(at.Add(time.Hour))
	s.tasksChanged()
	time.Sleep(50 * time.Millisecond)
	if tabs := tabsIn(s, ws); tabs != 1 {
		t.Errorf("the task started again: %d tabs", tabs)
	}
}

func tabsIn(s *Server, ws session.WorkspaceID) int {
	n := 0
	s.Session(func(sess *session.Session) {
		if w, ok := sess.Workspace(ws); ok {
			n = len(w.Tabs())
		}
	})
	return n
}

// TestATaskWhoseTimePassedWhileTheServerWasDownStartsOnceWhenItComesBack:
// a start time that went by with no server running is started by the next
// server as soon as it runs — in a space named after the task, when it has
// none — and the server after that does not start it again. If it regresses,
// a task planned for a time the machine was off never starts, or starts at
// every restart.
func TestATaskWhoseTimePassedWhileTheServerWasDownStartsOnceWhenItComesBack(t *testing.T) {
	state := filepath.Join(t.TempDir(), "work.json")
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	first := taskServer(t, state, clock, policy.Policy{})
	ws, _ := first.NewWorkspaceIn("other", t.TempDir())
	if _, _, err := first.NewTab(ws, "t", PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	stand := fakeAgent(t, "stand-in", "sleep 30")
	task := createTask(t, first, proto.TaskParams{
		Title: str("nightly"), Agent: str(stand), StartAt: i64(clock.Now().Add(time.Hour).Unix()),
	})
	first.saveStructure()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	clock.set(clock.Now().Add(3 * time.Hour))
	second := taskServer(t, state, clock, policy.Policy{})
	var started proto.TaskInfo
	waitFor(t, "the missed task to start", func() bool {
		started, _ = taskByID(second, task.ID)
		return started.Pane != 0
	})
	var name string
	second.Session(func(sess *session.Session) {
		if w, ok := sess.Workspace(session.WorkspaceID(started.Workspace)); ok {
			name = w.Name
		}
	})
	if name != "nightly" {
		t.Errorf("the agent went in space %q, want one named after the task", name)
	}
	second.saveStructure()
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	third := taskServer(t, state, clock, policy.Policy{})
	time.Sleep(100 * time.Millisecond)
	if after, _ := taskByID(third, task.ID); after.Pane != started.Pane {
		t.Errorf("the task was started again by the next server: pane %d, was %d", after.Pane, started.Pane)
	}
}

// TestATasksAgentIsHeldToThePolicy: an agent a policy forbids is not started
// for a task any more than for a script — the tab opened for it goes, the
// task says why, and it is not tried again until it is rescheduled. If it
// regresses, scheduling a task is a way round the agent limits and the
// protected branches.
func TestATasksAgentIsHeldToThePolicy(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_800_000_000, 0)}
	s := taskServer(t, "", clock, policy.Policy{MaxAgents: 1})
	ws, _ := s.NewWorkspaceIn("w", t.TempDir())
	_, busy, err := s.NewTab(ws, "busy", PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}, Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReportAgent(busy, agent.Report{Source: "test", Agent: "claude", State: detect.StateWorking, Seq: u64(1)}); err != nil {
		t.Fatal(err)
	}
	task := createTask(t, s, proto.TaskParams{
		Title: str("second agent"), Agent: str("codex"), StartAt: i64(clock.Now().Unix()),
		Workspace: &[]uint64{uint64(ws)}[0],
	})
	var failed proto.TaskInfo
	waitFor(t, "the refusal to be written on the task", func() bool {
		failed, _ = taskByID(s, task.ID)
		return failed.Error != ""
	})
	if !strings.Contains(failed.Error, "policy") || failed.Pane != 0 || failed.Status != "todo" {
		t.Errorf("refused task = %+v", failed)
	}
	waitFor(t, "the tab opened for it to close", func() bool { return tabsIn(s, ws) == 1 })
}
