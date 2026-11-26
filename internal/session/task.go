package session

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Tasks are the session's planner: the work ahead, with dates, tied to spaces
// and agents, and a task can be given to an agent to start at a time
// (docs/DESKTOP.md, "Planning"). herdr has none; tend's are the session's —
// server state, not one client's — so the TUI, the phone and the desktop
// window all see the same list, and a task scheduled from one starts whether
// or not any of them is attached.
//
// This is the record. Starting the agent is the server's (server/tasks.go):
// a session is pure data and holds no clock, so every method that stamps a
// time is handed the time to stamp.

// TaskID identifies a task for the life of the session.
type TaskID uint64

// TaskStatus is where a task is: not started, being worked on, finished.
type TaskStatus string

// The statuses a task can be in. A planner with more (blocked, cancelled)
// would be a different one; these three are what a list and a calendar need,
// and what the scheduler moves a task between.
const (
	TaskTodo  TaskStatus = "todo"
	TaskDoing TaskStatus = "doing"
	TaskDone  TaskStatus = "done"
)

// Valid reports whether a status is one of the three.
func (s TaskStatus) Valid() bool {
	return s == TaskTodo || s == TaskDoing || s == TaskDone
}

// Task is one piece of work. A zero time is one that is not set.
type Task struct {
	ID     TaskID
	Title  string
	Notes  string
	Status TaskStatus
	// Due is when it is to be done by; StartAt is when its agent is to be
	// started, which only means something with an Agent.
	Due     time.Time
	StartAt time.Time
	// Workspace is the space it belongs to, and where its agent starts;
	// zero is none, and an agent started for it gets a space of its own.
	Workspace WorkspaceID
	// Agent is what to start, as agent.start takes it: "claude", or a
	// command with its arguments. Prompt is the first message typed to it.
	Agent  string
	Prompt string
	// Pane is the pane its agent was started in, set by the scheduler. A
	// pane closed since keeps its id here: ids are never reused, so it
	// names nothing else, and a client tells a live one by the session.
	Pane PaneID
	// Started is when the scheduler acted on StartAt, whether the agent
	// started or not, so it acts once; Error is why it did not.
	Started time.Time
	Error   string

	Created time.Time
	Updated time.Time
}

// TaskChange is what to set on a task. A nil field is left as it is, so an
// update names only what it changes; a zero time or id clears the field.
type TaskChange struct {
	Title     *string
	Notes     *string
	Status    *TaskStatus
	Due       *time.Time
	StartAt   *time.Time
	Workspace *WorkspaceID
	Agent     *string
	Prompt    *string
}

var (
	// ErrNoSuchTask is a task id the session does not have.
	ErrNoSuchTask = errors.New("session: no such task")
	// ErrBadTaskStatus is a status that is not todo, doing or done.
	ErrBadTaskStatus = errors.New("session: a task's status is todo, doing or done")
)

// Tasks returns the tasks in the order they were made.
func (s *Session) Tasks() []*Task { return s.tasks }

// Task looks a task up by id.
func (s *Session) Task(id TaskID) (*Task, bool) {
	for _, t := range s.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return nil, false
}

// AddTask makes a task from a change, which must give it a title. A task
// made with no status is to do.
func (s *Session) AddTask(c TaskChange, now time.Time) (*Task, error) {
	if c.Title == nil || strings.TrimSpace(*c.Title) == "" {
		return nil, ErrEmptyName
	}
	t := &Task{Status: TaskTodo}
	if err := s.applyTask(t, c); err != nil {
		return nil, err
	}
	s.nextTask++
	t.ID = TaskID(s.nextTask)
	t.Created, t.Updated = second(now), second(now)
	s.tasks = append(s.tasks, t)
	return t, nil
}

// UpdateTask changes what a change names. Nothing is changed when any of it
// is refused, so a caller never half-applies an edit.
func (s *Session) UpdateTask(id TaskID, c TaskChange, now time.Time) error {
	t, ok := s.Task(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchTask, id)
	}
	edited := *t
	if err := s.applyTask(&edited, c); err != nil {
		return err
	}
	// A new start time, or a new agent, is asking for a start again: the
	// scheduler acted on the old one, and would otherwise never look at
	// this task again. A person rescheduling a start that was refused by a
	// policy is how a task gets its second try.
	if (c.StartAt != nil && !edited.StartAt.Equal(t.StartAt)) || (c.Agent != nil && edited.Agent != t.Agent) {
		edited.Started, edited.Error = time.Time{}, ""
	}
	edited.Updated = second(now)
	*t = edited
	return nil
}

// applyTask writes a change into a task, checking it first.
func (s *Session) applyTask(t *Task, c TaskChange) error {
	if c.Title != nil {
		title := strings.TrimSpace(*c.Title)
		if title == "" {
			return ErrEmptyName
		}
		t.Title = title
	}
	if c.Status != nil {
		if !c.Status.Valid() {
			return fmt.Errorf("%w: %q", ErrBadTaskStatus, *c.Status)
		}
		t.Status = *c.Status
	}
	if c.Workspace != nil {
		if *c.Workspace != 0 {
			if _, ok := s.Workspace(*c.Workspace); !ok {
				return fmt.Errorf("%w: %d", ErrNoSuchWorkspace, *c.Workspace)
			}
		}
		t.Workspace = *c.Workspace
	}
	if c.Notes != nil {
		t.Notes = *c.Notes
	}
	if c.Due != nil {
		t.Due = second(*c.Due)
	}
	if c.StartAt != nil {
		t.StartAt = second(*c.StartAt)
	}
	if c.Agent != nil {
		t.Agent = strings.TrimSpace(*c.Agent)
	}
	if c.Prompt != nil {
		t.Prompt = *c.Prompt
	}
	return nil
}

// RemoveTask deletes a task. The agent started for it, if any, is not
// touched: deleting the note about the work does not stop the work.
func (s *Session) RemoveTask(id TaskID) error {
	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = slices.Delete(s.tasks, i, i+1)
			return nil
		}
	}
	return fmt.Errorf("%w: %d", ErrNoSuchTask, id)
}

// DueTasks are the tasks whose agent is to be started now: a start time that
// has come, an agent to start, not acted on yet, and not done. A start time
// that passed while the server was down is due when it comes back, once,
// because Started is kept with the session.
func (s *Session) DueTasks(now time.Time) []TaskID {
	var out []TaskID
	for _, t := range s.tasks {
		if t.due(now) {
			out = append(out, t.ID)
		}
	}
	return out
}

// due is a task whose start time has come and that has not been acted on.
// One with an agent has it started; one without is a reminder, said at its
// time — a start time on a task nobody is to start was otherwise a time
// nothing happened at.
func (t *Task) due(now time.Time) bool {
	return !t.StartAt.IsZero() && !t.StartAt.After(now) &&
		t.Started.IsZero() && t.Status != TaskDone
}

// NextTaskStart is the earliest start still to come, or zero when there is
// none, for the scheduler to sleep until.
func (s *Session) NextTaskStart() time.Time {
	var next time.Time
	for _, t := range s.tasks {
		if t.StartAt.IsZero() || !t.Started.IsZero() || t.Status == TaskDone {
			continue
		}
		if next.IsZero() || t.StartAt.Before(next) {
			next = t.StartAt
		}
	}
	return next
}

// ClaimTask marks a due task as acted on and reports whether it was due, so
// two callers looking at the same moment cannot both start its agent.
func (s *Session) ClaimTask(id TaskID, now time.Time) (Task, bool) {
	t, ok := s.Task(id)
	if !ok || !t.due(now) {
		return Task{}, false
	}
	t.Started, t.Updated, t.Error = second(now), second(now), ""
	return *t, true
}

// TaskStarted records the pane a task's agent was started in, and the space
// it went in when the task had none: the task is being worked on.
func (s *Session) TaskStarted(id TaskID, ws WorkspaceID, pane PaneID, now time.Time) error {
	t, ok := s.Task(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchTask, id)
	}
	if _, ok := s.Workspace(ws); ok {
		t.Workspace = ws
	}
	t.Pane, t.Error, t.Updated = pane, "", second(now)
	if t.Status == TaskTodo {
		t.Status = TaskDoing
	}
	return nil
}

// TaskFailed records why a task's agent was not started.
func (s *Session) TaskFailed(id TaskID, why string, now time.Time) error {
	t, ok := s.Task(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSuchTask, id)
	}
	t.Error, t.Updated = why, second(now)
	return nil
}

// second is a time as a task keeps it: to the second, as the state file
// writes it, so a task compared before and after a restore is the same.
func second(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return time.Unix(t.Unix(), 0)
}

// forgetTaskWorkspace takes a closed space off the tasks in it. A task
// pointing at a space that is gone would start its agent nowhere; with none
// it starts in a space of its own, which is what a task with no space does.
func (s *Session) forgetTaskWorkspace(ws WorkspaceID) {
	for _, t := range s.tasks {
		if t.Workspace == ws {
			t.Workspace = 0
		}
	}
}

// sanitizeTasks repairs what a restored file says about tasks: a space that
// is gone, a status this build does not know. A task is the person's note
// about their work, and losing the whole session over one is not worth it.
func (s *Session) sanitizeTasks() {
	for _, t := range s.tasks {
		if t.Workspace != 0 {
			if _, ok := s.Workspace(t.Workspace); !ok {
				t.Workspace = 0
			}
		}
		if !t.Status.Valid() {
			t.Status = TaskTodo
		}
		s.nextTask = max(s.nextTask, uint64(t.ID))
	}
}

// checkTasks is CheckInvariants' part for tasks.
func (s *Session) checkTasks() error {
	ids := make(map[TaskID]bool, len(s.tasks))
	for _, t := range s.tasks {
		if t.ID == 0 || ids[t.ID] {
			return fmt.Errorf("task id %d is zero or used twice", t.ID)
		}
		ids[t.ID] = true
		if uint64(t.ID) > s.nextTask {
			return fmt.Errorf("task %d is past the counter %d", t.ID, s.nextTask)
		}
		if !t.Status.Valid() {
			return fmt.Errorf("task %d has status %q", t.ID, t.Status)
		}
		if t.Workspace != 0 {
			if _, ok := s.Workspace(t.Workspace); !ok {
				return fmt.Errorf("task %d is in workspace %d, which does not exist", t.ID, t.Workspace)
			}
		}
	}
	return nil
}
