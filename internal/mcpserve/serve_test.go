package mcpserve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeAPI answers the automation socket's methods from a script, and keeps
// what it was asked.
type fakeAPI struct {
	calls   []string
	answers map[string]map[string]any
	// queued answers come first, one per call, before answers.
	queued map[string][]map[string]any
	fail   map[string]error
}

func (f *fakeAPI) Call(method string, params map[string]any) (map[string]any, error) {
	raw, _ := json.Marshal(params)
	f.calls = append(f.calls, method+" "+string(raw))
	if err := f.fail[method]; err != nil {
		return nil, err
	}
	if q := f.queued[method]; len(q) > 0 {
		f.queued[method] = q[1:]
		return q[0], nil
	}
	if a, ok := f.answers[method]; ok {
		return a, nil
	}
	return map[string]any{"type": "ok"}, nil
}

func newFake() *fakeAPI {
	return &fakeAPI{
		answers: map[string]map[string]any{
			"pane.get":   {"pane": map[string]any{"pane_id": "p_1", "workspace_id": "w_2", "cwd": "/work"}},
			"tab.create": {"root_pane": map[string]any{"pane_id": "p_9"}},
			"agent.wait": {"agent": map[string]any{"pane_id": "p_9", "agent": "codex", "agent_state": "idle"}, "timed_out": false},
		},
		queued: map[string][]map[string]any{},
		fail:   map[string]error{},
	}
}

// session sends lines to a server and returns what it answered, one object
// per answer.
func session(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var answers []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("answer %q: %v", l, err)
		}
		answers = append(answers, m)
	}
	return answers
}

func callTool(name string, args map[string]any) string {
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 7, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	return string(raw)
}

func toolText(t *testing.T, answer map[string]any) (string, bool) {
	t.Helper()
	r, _ := answer["result"].(map[string]any)
	content, _ := r["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("answer = %v", answer)
	}
	c, _ := content[0].(map[string]any)
	isErr, _ := r["isError"].(bool)
	return c["text"].(string), isErr
}

// TestAnMCPClientIsGreetedAndShownTheTools is the handshake every agent's
// MCP client does first. If it regresses, the agent lists no tend tools.
func TestAnMCPClientIsGreetedAndShownTheTools(t *testing.T) {
	s := &Server{API: newFake(), Version: "test"}
	got := session(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(got) != 2 {
		t.Fatalf("a notification was answered, or a request was not: %v", got)
	}
	init, _ := got[0]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-03-26" {
		t.Fatalf("the client's protocol version was not answered in: %v", init)
	}
	list, _ := got[1]["result"].(map[string]any)
	tools, _ := list["tools"].([]any)
	var names []string
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		names = append(names, m["name"].(string))
		if _, ok := m["inputSchema"].(map[string]any); !ok {
			t.Errorf("%s has no input schema", m["name"])
		}
	}
	want := "list_agents start_agent send_message read_screen wait_for_agent focus_agent"
	if strings.Join(names, " ") != want {
		t.Fatalf("tools = %v, want %s", names, want)
	}
}

// TestStartAgentOpensItInTheCallersSpaceAndHandsItTheTaskOnceReady is the
// heart of it: a tab in the caller's own space (so in its companies), the
// agent started through agent.start (so the policy is asked), and the task
// typed only once the agent is ready for it. If it regresses, the task is
// typed into a shell, or the agent opens in a space of somebody else's.
func TestStartAgentOpensItInTheCallersSpaceAndHandsItTheTaskOnceReady(t *testing.T) {
	f := newFake()
	s := &Server{API: f, Caller: "p_1"}
	got := session(t, s, callTool("start_agent", map[string]any{"agent": "codex", "name": "reviewer", "prompt": "review the diff"}))
	text, isErr := toolText(t, got[0])
	if isErr || !strings.Contains(text, "p_9") {
		t.Fatalf("start_agent = %q (error %v)", text, isErr)
	}
	want := []string{
		`pane.get {"pane_id":"p_1"}`,
		`tab.create {"dir":"/work","name":"reviewer","workspace_id":"w_2"}`,
		`agent.start {"agent":"codex","name":"reviewer","pane_id":"p_9"}`,
		`agent.wait {"target":"p_9","timeout_ms":90000,"until":["idle","blocked"]}`,
		`agent.prompt {"target":"p_9","text":"review the diff"}`,
		`agent.wait {"target":"p_9","timeout_ms":8000,"until":["working","blocked"]}`,
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("asked:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
}

// TestAnAgentAskingSomethingAtStartIsNotAnsweredWithTheTask: a new Claude
// Code in a folder it does not know asks whether to trust it. The task
// typed there would answer the question; the tool says what is asked.
func TestAnAgentAskingSomethingAtStartIsNotAnsweredWithTheTask(t *testing.T) {
	f := newFake()
	f.answers["agent.wait"] = map[string]any{"agent": map[string]any{"pane_id": "p_9", "agent_state": "blocked", "message": "trust this folder?"}}
	got := session(t, &Server{API: f, Caller: "p_1"}, callTool("start_agent", map[string]any{"agent": "claude", "prompt": "go"}))
	text, isErr := toolText(t, got[0])
	if !isErr || !strings.Contains(text, "trust this folder?") {
		t.Fatalf("start_agent = %q (error %v)", text, isErr)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "agent.prompt") {
			t.Fatalf("the task was typed into a question: %v", f.calls)
		}
	}
}

// TestAPolicyThatForbidsTheAgentIsSaidAndThePaneTakenAway: agent.start
// refuses an agent a policy forbids; the agent is told why, and the tab
// opened for it does not stay behind empty.
func TestAPolicyThatForbidsTheAgentIsSaidAndThePaneTakenAway(t *testing.T) {
	f := newFake()
	f.fail["agent.start"] = errors.New("codex is not allowed in this space")
	got := session(t, &Server{API: f, Caller: "p_1"}, callTool("start_agent", map[string]any{"agent": "codex"}))
	text, isErr := toolText(t, got[0])
	if !isErr || !strings.Contains(text, "not allowed") {
		t.Fatalf("start_agent = %q (error %v)", text, isErr)
	}
	if last := f.calls[len(f.calls)-1]; last != `pane.close {"pane_id":"p_9"}` {
		t.Fatalf("the empty pane was left: %v", f.calls)
	}
}

// TestSendMessageWithWaitWaitsForTheAgentToLeaveIdleFirst: an agent idle
// from before must not be taken to have answered already, which a plain
// wait right after the message would do.
func TestSendMessageWithWaitWaitsForTheAgentToLeaveIdleFirst(t *testing.T) {
	f := newFake()
	f.answers["agent.prompt"] = map[string]any{"agent": map[string]any{"pane_id": "p_9", "agent_state": "idle"}, "timed_out": false}
	got := session(t, &Server{API: f}, callTool("send_message", map[string]any{"target": "reviewer", "text": "now", "wait": true, "timeout_seconds": 5}))
	if text, isErr := toolText(t, got[0]); isErr || !strings.Contains(text, "state=idle") {
		t.Fatalf("send_message = %q", text)
	}
	if f.calls[0] != `agent.prompt {"target":"reviewer","text":"now","timeout_ms":5000,"wait":true}` {
		t.Fatalf("asked %v", f.calls)
	}
}

// TestAnUnknownToolIsAProtocolErrorAndAFailedOneIsTheAgentsToRead: the
// spec's split — a call that cannot be made is a JSON-RPC error, a tool
// that ran and failed is a result marked isError the agent reads.
func TestAnUnknownToolIsAProtocolErrorAndAFailedOneIsTheAgentsToRead(t *testing.T) {
	f := newFake()
	f.fail["agent.read"] = fmt.Errorf("agent p_4 not found")
	got := session(t, &Server{API: f}, callTool("nope", nil), callTool("read_screen", map[string]any{"target": "p_4"}))
	if got[0]["error"] == nil {
		t.Fatalf("an unknown tool was answered: %v", got[0])
	}
	if text, isErr := toolText(t, got[1]); !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("read_screen = %q (error %v)", text, isErr)
	}
}

// TestANewAgentsLostFirstEnterIsPressedAgain is what the real Claude Code
// 2.1.325 did: its prompt box was drawn — idle — before it acted on Enter,
// and the task sat typed in the box. If it regresses, start_agent says the
// task was sent and the agent never starts on it.
func TestANewAgentsLostFirstEnterIsPressedAgain(t *testing.T) {
	f := newFake()
	f.queued["agent.wait"] = []map[string]any{
		{"agent": map[string]any{"pane_id": "p_9", "agent_state": "idle"}, "timed_out": false},
		{"agent": map[string]any{"pane_id": "p_9", "agent_state": "idle"}, "timed_out": true},
		{"agent": map[string]any{"pane_id": "p_9", "agent_state": "working"}, "timed_out": false},
	}
	got := session(t, &Server{API: f, Caller: "p_1", Settle: 10}, callTool("start_agent", map[string]any{"agent": "claude", "prompt": "go"}))
	text, isErr := toolText(t, got[0])
	if isErr || !strings.Contains(text, "working on the task") {
		t.Fatalf("start_agent = %q (error %v)", text, isErr)
	}
	enters := 0
	for _, c := range f.calls {
		if c == `agent.send_keys {"keys":["enter"],"target":"p_9"}` {
			enters++
		}
	}
	if enters != 1 {
		t.Fatalf("Enter was pressed again %d times, want once: %v", enters, f.calls)
	}
}
