package api

import (
	"errors"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/session"
)

// The task methods are the client protocol's task.* (server/tasks.go) in
// the socket's own dress: spaces and panes named as everywhere else here
// (w_3, p_7), and times as RFC 3339 text rather than Unix seconds, since a
// script, a shell and an agent all write and read that without arithmetic.

// TaskInfo is one task as the socket shows it.
type TaskInfo struct {
	TaskID      uint64 `json:"task_id"`
	Title       string `json:"title"`
	Notes       string `json:"notes,omitempty"`
	Status      string `json:"status"`
	Due         string `json:"due,omitempty"`
	StartAt     string `json:"start_at,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	PaneID      string `json:"pane_id,omitempty"`
	Started     string `json:"started,omitempty"`
	Error       string `json:"error,omitempty"`
	Created     string `json:"created,omitempty"`
	Updated     string `json:"updated,omitempty"`
}

// task answers task.list, task.create, task.update and task.delete.
func (a *API) task(req Request) (any, error) {
	if req.Method == MethodTaskList {
		tasks := a.srv.Tasks()
		out := make([]TaskInfo, 0, len(tasks))
		for _, t := range tasks {
			out = append(out, taskInfo(t))
		}
		return map[string]any{"type": "task_list", "tasks": out}, nil
	}
	var p TaskParams
	if err := decode(req.Params, &p); err != nil {
		return nil, err
	}
	if req.Method != MethodTaskCreate && p.TaskID == 0 {
		return nil, fail("invalid_params", "name the task with task_id")
	}
	params, err := taskParams(p)
	if err != nil {
		return nil, err
	}
	res, err := a.srv.ChangeTask(req.Method, params)
	switch {
	case errors.Is(err, session.ErrNoSuchTask):
		return nil, fail("task_not_found", "task %d not found", p.TaskID)
	case errors.Is(err, session.ErrNoSuchWorkspace):
		return nil, fail("workspace_not_found", "workspace %s not found", deref(p.WorkspaceID))
	case err != nil:
		return nil, fail("invalid_params", "%v", err)
	}
	if req.Method == MethodTaskDelete {
		return ok2(), nil
	}
	return map[string]any{"type": "task_info", "task": taskInfo(res.(proto.TaskInfo))}, nil
}

// taskParams turns the socket's text into the protocol's numbers.
func taskParams(p TaskParams) (proto.TaskParams, error) {
	out := proto.TaskParams{
		Task: p.TaskID, Title: p.Title, Notes: p.Notes, Status: p.Status, Agent: p.Agent, Prompt: p.Prompt,
	}
	var err error
	if out.Due, err = taskTime("due", p.Due); err != nil {
		return out, err
	}
	if out.StartAt, err = taskTime("start_at", p.StartAt); err != nil {
		return out, err
	}
	if p.WorkspaceID != nil {
		var ws uint64
		if *p.WorkspaceID != "" {
			id, ok := parseID("w_", *p.WorkspaceID)
			if !ok {
				return out, fail("workspace_not_found", "workspace %s not found", *p.WorkspaceID)
			}
			ws = id
		}
		out.Workspace = &ws
	}
	return out, nil
}

// taskTime reads an RFC 3339 time; empty is zero, which clears the field.
func taskTime(field string, v *string) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	var sec int64
	if s := strings.TrimSpace(*v); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fail("invalid_params", "%s %q is not an RFC 3339 time (2026-11-27T09:00:00-03:00)", field, s)
		}
		sec = t.Unix()
	}
	return &sec, nil
}

func taskInfo(t proto.TaskInfo) TaskInfo {
	stamp := func(sec int64) string {
		if sec == 0 {
			return ""
		}
		return time.Unix(sec, 0).Format(time.RFC3339)
	}
	out := TaskInfo{
		TaskID: t.ID, Title: t.Title, Notes: t.Notes, Status: t.Status,
		Due: stamp(t.Due), StartAt: stamp(t.StartAt), Agent: t.Agent, Prompt: t.Prompt,
		Started: stamp(t.Started), Error: t.Error, Created: stamp(t.Created), Updated: stamp(t.Updated),
	}
	if t.Workspace != 0 {
		out.WorkspaceID = WorkspaceID(session.WorkspaceID(t.Workspace))
	}
	if t.Pane != 0 {
		out.PaneID = PaneID(session.PaneID(t.Pane))
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
