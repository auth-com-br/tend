package session

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

// TestATaskIsMadeEditedAndDeleted: a task needs a title, starts as todo,
// takes only the fields an update names, refuses a status or space it cannot
// have without changing anything, and deleting it leaves the rest. If it
// regresses, editing one field of a task in the planner blanks the others,
// or a refused edit leaves the task half-changed.
func TestATaskIsMadeEditedAndDeleted(t *testing.T) {
	s := New()
	ws := s.AddWorkspace("w")
	now := time.Unix(1_800_000_000, 0)

	if _, err := s.AddTask(TaskChange{Title: ptr("  ")}, now); !errors.Is(err, ErrEmptyName) {
		t.Errorf("a task with a blank title: %v", err)
	}
	a, err := s.AddTask(TaskChange{Title: ptr("write the report"), Notes: ptr("for Acme"), Workspace: ptr(ws.ID)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != TaskTodo || a.Created != now || a.Workspace != ws.ID {
		t.Errorf("made %+v", a)
	}
	b, _ := s.AddTask(TaskChange{Title: ptr("second")}, now)

	later := now.Add(time.Hour)
	if err := s.UpdateTask(a.ID, TaskChange{Status: ptr(TaskDone)}, later); err != nil {
		t.Fatal(err)
	}
	if a.Status != TaskDone || a.Notes != "for Acme" || a.Title != "write the report" || a.Updated != later {
		t.Errorf("after marking it done %+v", a)
	}
	if err := s.UpdateTask(a.ID, TaskChange{Title: ptr("x"), Status: ptr(TaskStatus("blocked"))}, later); !errors.Is(err, ErrBadTaskStatus) {
		t.Errorf("an unknown status: %v", err)
	}
	if err := s.UpdateTask(a.ID, TaskChange{Title: ptr("x"), Workspace: ptr(WorkspaceID(99))}, later); !errors.Is(err, ErrNoSuchWorkspace) {
		t.Errorf("a space that does not exist: %v", err)
	}
	if a.Title != "write the report" {
		t.Errorf("a refused edit changed the title to %q", a.Title)
	}
	if err := s.RemoveTask(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.Tasks(); len(got) != 1 || got[0].ID != b.ID {
		t.Errorf("after deleting the first, tasks = %+v", got)
	}
	if err := s.UpdateTask(a.ID, TaskChange{}, later); !errors.Is(err, ErrNoSuchTask) {
		t.Errorf("updating a deleted task: %v", err)
	}
	check(t, s)
}

// TestATaskIsDueOnceAtItsStartTime: a task with a start time is due when the
// time comes and not before — with an agent to start it, without one to say
// it — is claimed once, is not due again after that, and becomes due again
// when its start is moved. A task done is never due. If it regresses, the
// scheduler starts an agent before its time, starts it on every tick, never
// reminds of a task planned for a time, or never retries a start the person
// rescheduled.
func TestATaskIsDueOnceAtItsStartTime(t *testing.T) {
	s := New()
	at := time.Unix(1_800_000_000, 0)
	task, _ := s.AddTask(TaskChange{Title: ptr("review"), Agent: ptr("claude"), StartAt: ptr(at)}, at.Add(-time.Hour))
	reminder, _ := s.AddTask(TaskChange{Title: ptr("reminder"), StartAt: ptr(at)}, at)
	_, _ = s.AddTask(TaskChange{Title: ptr("done already"), Agent: ptr("claude"), StartAt: ptr(at), Status: ptr(TaskDone)}, at)

	if due := s.DueTasks(at.Add(-time.Second)); len(due) != 0 {
		t.Errorf("due before its time: %v", due)
	}
	if next := s.NextTaskStart(); !next.Equal(at) {
		t.Errorf("next start = %v, want %v", next, at)
	}
	if due := s.DueTasks(at); !slices.Equal(due, []TaskID{task.ID, reminder.ID}) {
		t.Fatalf("due at its time: %v", due)
	}
	if _, ok := s.ClaimTask(reminder.ID, at); !ok {
		t.Fatal("the reminder could not be claimed")
	}
	if _, ok := s.ClaimTask(task.ID, at); !ok {
		t.Fatal("the due task could not be claimed")
	}
	if _, ok := s.ClaimTask(task.ID, at); ok {
		t.Error("the task was claimed twice")
	}
	if due := s.DueTasks(at.Add(time.Hour)); len(due) != 0 {
		t.Errorf("due again after it was acted on: %v", due)
	}
	if err := s.TaskFailed(task.ID, "refused", at); err != nil {
		t.Fatal(err)
	}

	again := at.Add(2 * time.Hour)
	if err := s.UpdateTask(task.ID, TaskChange{StartAt: ptr(again)}, at); err != nil {
		t.Fatal(err)
	}
	if !task.Started.IsZero() || task.Error != "" {
		t.Errorf("a rescheduled task kept its last try: %+v", task)
	}
	if due := s.DueTasks(again); !slices.Equal(due, []TaskID{task.ID}) {
		t.Errorf("due after being rescheduled: %v", due)
	}
	check(t, s)
}

// TestATaskStartedIsBeingWorkedOn: the scheduler's record of a start is the
// pane and the space, and a task to do moves to doing. If it regresses, the
// planner shows a task whose agent is running as not started, with no way to
// reach the agent from it.
func TestATaskStartedIsBeingWorkedOn(t *testing.T) {
	s := New()
	ws := s.AddWorkspace("for the task")
	now := time.Unix(1_800_000_000, 0)
	task, _ := s.AddTask(TaskChange{Title: ptr("t"), Agent: ptr("claude"), StartAt: ptr(now)}, now)
	if err := s.TaskStarted(task.ID, ws.ID, 7, now); err != nil {
		t.Fatal(err)
	}
	if task.Status != TaskDoing || task.Pane != 7 || task.Workspace != ws.ID {
		t.Errorf("started %+v", task)
	}
}

// TestTasksComeBackAfterARestartAndLoseAClosedSpace: tasks and their counter
// survive a snapshot and a restore, field for field; a space closed takes
// itself off its tasks, and a file naming one that is gone is cleaned rather
// than refused; a session with no tasks writes nothing about them. If it
// regresses, restarting the server loses the planner, starts an agent twice
// (Started lost), or refuses the whole session over one stale task.
func TestTasksComeBackAfterARestartAndLoseAClosedSpace(t *testing.T) {
	s := New()
	a, b := s.AddWorkspace("a"), s.AddWorkspace("b")
	if _, _, err := s.AddTab(a.ID, "t", PaneSpec{}); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(s.Snapshot(nil))
	var fields map[string]any
	_ = json.Unmarshal(plain, &fields)
	if _, ok := fields["tasks"]; ok {
		t.Errorf("a session with no tasks wrote them: %s", plain)
	}

	now := time.Unix(1_800_000_000, 0)
	kept, _ := s.AddTask(TaskChange{
		Title: ptr("kept"), Notes: ptr("n"), Due: ptr(now.Add(24 * time.Hour)), StartAt: ptr(now),
		Workspace: ptr(a.ID), Agent: ptr("claude --model opus"), Prompt: ptr("do it"),
	}, now)
	_, _ = s.ClaimTask(kept.ID, now)
	_ = s.TaskStarted(kept.ID, a.ID, 3, now)
	inB, _ := s.AddTask(TaskChange{Title: ptr("in b"), Workspace: ptr(b.ID)}, now)
	if _, err := s.CloseWorkspace(b.ID); err != nil {
		t.Fatal(err)
	}
	if inB.Workspace != 0 {
		t.Errorf("a task kept the space that closed: %d", inB.Workspace)
	}

	snap := s.Snapshot(nil)
	snap.Tasks = append(snap.Tasks, TaskSnapshot{ID: 9, Title: "stale", Status: "todo", Workspace: 777})
	back, err := Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	got := back.Tasks()
	if len(got) != 3 {
		t.Fatalf("restored %d tasks", len(got))
	}
	if *got[0] != *kept {
		t.Errorf("restored\n %+v\nwant\n %+v", *got[0], *kept)
	}
	if got[2].Workspace != 0 {
		t.Errorf("a task kept a space the file names and the session does not have")
	}
	if due := back.DueTasks(now.Add(time.Hour)); len(due) != 0 {
		t.Errorf("a task already started is due again after a restore: %v", due)
	}
	next, _ := back.AddTask(TaskChange{Title: ptr("next")}, now)
	if next.ID <= 9 {
		t.Errorf("a task made after the restore reused id %d", next.ID)
	}
	check(t, back)
}
