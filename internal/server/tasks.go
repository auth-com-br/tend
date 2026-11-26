package server

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
)

// The session's tasks (session/task.go) and the scheduler that starts their
// agents. The tasks are the server's because a task given to an agent for
// nine in the morning has to start with nobody attached, and because every
// client — the TUI, the phone, the desktop window — is to see one list.
//
// An agent started for a task is started as agent.start starts one
// (StartAgent): a shell in a new tab, the policy asked, the launch typed. Its
// first prompt is then given as the MCP server's start_agent gives one: once
// the agent is ready, and with Enter pressed again if the first was lost.

const (
	// taskLook bounds how long the scheduler sleeps between looks, whatever
	// the next start is. Go's timers run on the monotonic clock, which does
	// not advance while a laptop sleeps: a timer set for a start two hours
	// away, on a machine suspended for one of them, would fire an hour late.
	// Looking at the wall clock every so often catches that up.
	taskLook = 30 * time.Second
	// taskReadyDefault and taskSettleDefault are start_agent's: how long a
	// new agent has to draw its prompt box, and then to start on the task.
	taskReadyDefault  = 90 * time.Second
	taskSettleDefault = 8 * time.Second
)

// ErrTaskAgentNotReady is a task's agent that did not become ready for its
// prompt in time, or asked something first.
var ErrTaskAgentNotReady = errors.New("the agent was not ready for the task")

// Tasks is every task, in the order they were made.
func (s *Server) Tasks() []proto.TaskInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []proto.TaskInfo{}
	for _, t := range s.session.Tasks() {
		out = append(out, taskInfo(t))
	}
	return out
}

// ChangeTask answers task.create, task.update and task.delete. Create and
// update return the task as it now is.
func (s *Server) ChangeTask(method string, p proto.TaskParams) (any, error) {
	change, err := taskChange(p)
	if err != nil {
		return nil, err
	}
	now := s.cfg.Now()
	var info proto.TaskInfo
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	sess := s.session
	switch method {
	case proto.MethodTaskCreate:
		var t *session.Task
		if t, err = sess.AddTask(change, now); err == nil {
			info = taskInfo(t)
		}
	case proto.MethodTaskUpdate:
		if err = sess.UpdateTask(session.TaskID(p.Task), change, now); err == nil {
			t, _ := sess.Task(session.TaskID(p.Task))
			info = taskInfo(t)
		}
	case proto.MethodTaskDelete:
		err = sess.RemoveTask(session.TaskID(p.Task))
	default:
		err = fmt.Errorf("%w: %s", proto.ErrUnknownMethod, method)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.tasksChanged()
	if method == proto.MethodTaskDelete {
		return nil, nil
	}
	return info, nil
}

// tasksChanged tells the clients, and the scheduler, that the tasks did.
func (s *Server) tasksChanged() {
	s.publish(Event{Kind: EventTasksChanged})
	select {
	case s.taskWake <- struct{}{}:
	default: // already woken; it reads every task when it looks
	}
}

// taskChange is what a client asked to set, as the session takes it.
func taskChange(p proto.TaskParams) (session.TaskChange, error) {
	c := session.TaskChange{Title: p.Title, Notes: p.Notes, Agent: p.Agent, Prompt: p.Prompt}
	if p.Status != nil {
		st := session.TaskStatus(*p.Status)
		c.Status = &st
	}
	when := func(sec *int64) *time.Time {
		if sec == nil {
			return nil
		}
		var t time.Time
		if *sec != 0 {
			t = time.Unix(*sec, 0)
		}
		return &t
	}
	c.Due, c.StartAt = when(p.Due), when(p.StartAt)
	if p.Workspace != nil {
		ws := session.WorkspaceID(*p.Workspace)
		c.Workspace = &ws
	}
	if c.Agent != nil && strings.ContainsAny(*c.Agent, "\n\r") {
		// It is typed into a shell: a line break would run what follows it.
		return c, errors.New("an agent is one command line")
	}
	return c, nil
}

func taskInfo(t *session.Task) proto.TaskInfo {
	sec := func(at time.Time) int64 {
		if at.IsZero() {
			return 0
		}
		return at.Unix()
	}
	return proto.TaskInfo{
		ID: uint64(t.ID), Title: t.Title, Notes: t.Notes, Status: string(t.Status),
		Due: sec(t.Due), StartAt: sec(t.StartAt), Workspace: uint64(t.Workspace),
		Agent: t.Agent, Prompt: t.Prompt, Pane: uint64(t.Pane),
		Started: sec(t.Started), Error: t.Error,
		Created: sec(t.Created), Updated: sec(t.Updated),
	}
}

// taskLoop starts each task's agent when its time comes, until the server
// stops. Its first look is at once, which is how a start that passed while no
// server was running happens when one comes back: once, because the task
// records that it was acted on and the record is saved with the session.
func (s *Server) taskLoop() {
	defer s.wg.Done()
	for {
		s.startDueTasks()

		wait := taskLook
		s.mu.Lock()
		next := s.session.NextTaskStart()
		s.mu.Unlock()
		if !next.IsZero() {
			wait = min(wait, max(next.Sub(s.cfg.Now()), 0))
		}
		timer := time.NewTimer(wait)
		select {
		case <-s.done:
			timer.Stop()
			return
		case <-s.taskWake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// startDueTasks starts the agent of every task whose time has come. Each is
// claimed under the lock before anything is started, so a task is acted on
// once however many looks overlap; the starting itself is done without it.
func (s *Server) startDueTasks() {
	now := s.cfg.Now()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	var claimed []session.Task
	for _, id := range s.session.DueTasks(now) {
		if t, ok := s.session.ClaimTask(id, now); ok {
			claimed = append(claimed, t)
		}
	}
	s.mu.Unlock()
	if len(claimed) == 0 {
		return
	}
	s.tasksChanged()
	for _, t := range claimed {
		if strings.TrimSpace(t.Agent) == "" {
			s.remindTask(t)
			continue
		}
		s.startTask(t)
	}
}

// remindTask says a task without an agent has come due: the person planned
// it for now, and every client announces it as it announces what a script
// sends (EventNotify). It is claimed, so it is said once.
func (s *Server) remindTask(t session.Task) {
	body := strings.TrimSpace(t.Notes)
	if body == "" {
		body = "it is time"
	}
	s.Notify("Task: "+t.Title, body)
}

// startTask opens a tab for a task's agent and starts it there: in the
// task's space, in that space's folder, or in a space of its own named after
// the task when it has none. A start that fails is written on the task and
// told to whoever is watching; the tab opened for it is closed again.
func (s *Server) startTask(t session.Task) {
	ws, dir, made, err := s.taskSpace(t)
	if err != nil {
		s.taskFailed(t, err)
		return
	}
	_, pane, err := s.NewTab(ws, t.Title, PaneSpec{Dir: dir})
	if err != nil {
		if made {
			_ = s.CloseWorkspace(ws)
		}
		s.taskFailed(t, err)
		return
	}
	if err := s.StartAgent(pane, strings.Fields(t.Agent), "", ""); err != nil {
		// The pane was opened for the agent; without it, it is litter.
		if made {
			_ = s.CloseWorkspace(ws)
		} else {
			_ = s.ClosePane(pane)
		}
		s.taskFailed(t, err)
		return
	}

	s.mu.Lock()
	err = s.session.TaskStarted(t.ID, ws, pane, s.cfg.Now())
	s.mu.Unlock()
	if err != nil {
		return // deleted while its agent was starting; the agent stays
	}
	s.tasksChanged()
	s.Notify("Task started", fmt.Sprintf("%s: %s", t.Title, t.Agent))

	if strings.TrimSpace(t.Prompt) != "" {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.givePrompt(pane, t.Prompt); err != nil && !errors.Is(err, ErrClosed) {
				s.taskFailed(t, err)
			}
		}()
	}
}

// taskSpace is where a task's agent opens: its space and that space's folder,
// or a new space named after it.
func (s *Server) taskSpace(t session.Task) (session.WorkspaceID, string, bool, error) {
	s.mu.Lock()
	if w, ok := s.session.Workspace(t.Workspace); ok && t.Workspace != 0 {
		dir := w.Dir
		s.mu.Unlock()
		return w.ID, dir, false, nil
	}
	s.mu.Unlock()
	ws, err := s.NewWorkspaceIn(t.Title, "")
	return ws, "", true, err
}

// taskFailed writes on a task why its agent did not start, or did not take
// its prompt, and tells whoever is watching.
func (s *Server) taskFailed(t session.Task, err error) {
	s.mu.Lock()
	failed := s.session.TaskFailed(t.ID, err.Error(), s.cfg.Now())
	s.mu.Unlock()
	if failed == nil {
		s.tasksChanged()
	}
	s.Notify("Task not started", fmt.Sprintf("%s: %v", t.Title, err))
}

// givePrompt types a task's first message to its new agent. Typed before the
// agent draws its prompt box, it is read by the shell or lost in the agent's
// start-up, so it waits for the agent to be ready: idle, or blocked on a
// question of its own (a folder to trust), which the task must not answer.
//
// A new agent's first Enter can be lost: Claude Code draws its prompt box —
// which detection reads as idle — a moment before it acts on Enter (seen
// with the real one by the MCP server's start_agent, which this follows). So
// the prompt is taken to be sent once the agent starts on it, and Enter is
// pressed once more if it does not.
func (s *Server) givePrompt(pane session.PaneID, prompt string) error {
	ready := s.cfg.TaskReady
	if ready <= 0 {
		ready = taskReadyDefault
	}
	settle := s.cfg.TaskSettle
	if settle <= 0 {
		settle = taskSettleDefault
	}
	st, timedOut, err := s.awaitState(pane, ready, detect.StateIdle, detect.StateBlocked)
	switch {
	case err != nil:
		return err
	case timedOut:
		return fmt.Errorf("%w within %s; the prompt was not sent", ErrTaskAgentNotReady, ready)
	case st == detect.StateBlocked:
		return fmt.Errorf("%w: it is asking something first; the prompt was not sent", ErrTaskAgentNotReady)
	}
	if err := s.Submit(pane, prompt); err != nil {
		return err
	}
	for try := 0; try < 2; try++ {
		_, timedOut, err := s.awaitState(pane, settle, detect.StateWorking, detect.StateBlocked)
		if err != nil || !timedOut {
			return err
		}
		if try == 0 {
			if err := s.SendKeys(pane, []string{"enter"}); err != nil {
				return err
			}
		}
	}
	// Or it did, quickly, before it was watched; nothing to say it failed.
	return nil
}

// awaitState waits for a pane's agent to be in one of the states, up to a
// timeout, and says which it reached. The pane's program ending is an error,
// and so is the server stopping.
func (s *Server) awaitState(pane session.PaneID, timeout time.Duration, states ...detect.State) (detect.State, bool, error) {
	matches := func(st detect.State) bool {
		for _, want := range states {
			if st == want {
				return true
			}
		}
		return false
	}
	// Subscribed before the first look, so a change between the two is not
	// missed.
	sub := s.Subscribe(64)
	defer sub.Close()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		st, err := s.PaneStatus(pane)
		if err != nil {
			return 0, false, err
		}
		if matches(st.State) {
			return st.State, false, nil
		}
		if !st.Running {
			return 0, false, fmt.Errorf("the program in pane %d ended", pane)
		}
		// Only this pane's state, or its end, is worth looking again for.
		for relevant := false; !relevant; {
			select {
			case <-s.done:
				return 0, false, ErrClosed
			case <-timer.C:
				return st.State, true, nil
			case ev, open := <-sub.C:
				if !open {
					return 0, false, ErrClosed
				}
				relevant = ev.Pane == pane &&
					(ev.Kind == EventPaneState || ev.Kind == EventPaneExited || ev.Kind == EventPaneClosed)
			}
		}
	}
}
