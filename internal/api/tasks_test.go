package api

import (
	"testing"
	"time"
)

// TestAScriptKeepsTheSessionsTasks: over the socket a task is made with RFC
// 3339 times and a space by its w_ name, listed, marked done and deleted, and
// what the socket cannot take — no task named, a time it cannot read, a space
// that is not there — is refused with a code saying which. If it regresses,
// `tend task` and the MCP server's task tools stop working, or a typo in a
// date is taken as "no date".
func TestAScriptKeepsTheSessionsTasks(t *testing.T) {
	h := start(t)
	due := time.Date(2026, 11, 27, 18, 0, 0, 0, time.Local).Format(time.RFC3339)

	made := result(t, call(t, h, MethodTaskCreate, map[string]any{
		"title": "write the report", "due": due, "workspace_id": "w_1",
	}))
	task, _ := made["task"].(map[string]any)
	id := task["task_id"]
	if task["status"] != "todo" || task["due"] != due || task["workspace_id"] != "w_1" {
		t.Fatalf("made %v", task)
	}

	list := result(t, call(t, h, MethodTaskList, nil))
	if tasks, _ := list["tasks"].([]any); len(tasks) != 1 {
		t.Fatalf("listed %v", list)
	}
	done := result(t, call(t, h, MethodTaskUpdate, map[string]any{"task_id": id, "status": "done", "due": ""}))
	if task, _ := done["task"].(map[string]any); task["status"] != "done" || task["due"] != nil || task["title"] != "write the report" {
		t.Errorf("after marking it done %v", task)
	}

	for _, c := range []struct {
		method string
		params map[string]any
		code   string
	}{
		{MethodTaskUpdate, map[string]any{"status": "done"}, "invalid_params"},
		{MethodTaskCreate, map[string]any{"title": "x", "start_at": "tomorrow"}, "invalid_params"},
		{MethodTaskCreate, map[string]any{"title": "x", "workspace_id": "w_99"}, "workspace_not_found"},
		{MethodTaskUpdate, map[string]any{"task_id": 99, "status": "done"}, "task_not_found"},
	} {
		if got := errorCode(call(t, h, c.method, c.params)); got != c.code {
			t.Errorf("%s %v: code %q, want %q", c.method, c.params, got, c.code)
		}
	}

	result(t, call(t, h, MethodTaskDelete, map[string]any{"task_id": id}))
	list = result(t, call(t, h, MethodTaskList, nil))
	if tasks, _ := list["tasks"].([]any); len(tasks) != 0 {
		t.Errorf("after the delete %v", list)
	}
}
