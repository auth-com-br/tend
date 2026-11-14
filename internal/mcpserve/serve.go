// Package mcpserve is tend's own MCP server: tools with which an agent
// drives the session it runs in — starts another agent beside it, hands it
// a task, waits for it to finish and reads what it said.
//
// herdr has none; its agents reach the session through its CLI and socket
// API, which a person has to teach each agent to call. An MCP server is
// what every agent tend knows already speaks, so the same tools reach
// Claude Code, Codex, Gemini CLI and the rest without a word of
// instruction. Each tool is one or two calls on the automation socket
// (internal/api), so the policies and companies hold as they do for a
// script: agent.start asks the policy before it types anything, and a new
// agent opens in a tab of the caller's own space, which keeps it in the
// caller's companies.
//
// The transport is stdio, one JSON-RPC message per line, as the MCP spec's
// stdio transport has it; `tend mcp serve` is the command an agent's MCP
// settings name.
package mcpserve

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ProtocolVersion is the MCP revision answered when the client asks for
// one this server does not know.
const ProtocolVersion = "2025-06-18"

// known are the MCP revisions a client may ask for and be answered in: the
// tools used here are the same in all of them.
var known = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true}

// Caller is the automation socket: one method, its result object.
type Caller interface {
	Call(method string, params map[string]any) (map[string]any, error)
}

// Server answers one MCP client.
type Server struct {
	API Caller
	// Caller is the pane the agent runs in (TEND_PANE_ID), whose space new
	// agents open in. Empty outside a tend pane: they open in the space
	// the person is looking at.
	Caller  string
	Version string

	// ReadyTimeout bounds how long start_agent waits for a new agent to be
	// ready for its first prompt.
	ReadyTimeout time.Duration
	// Settle bounds how long a new agent is given to start on its first
	// task before Enter is pressed again.
	Settle time.Duration
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from in and answers on out until in ends.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	r := bufio.NewReaderSize(in, 1<<20)
	enc := json.NewEncoder(out)
	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			if resp, ok := s.handle(line); ok {
				if err := enc.Encode(resp); err != nil {
					return err
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// handle answers one message; a notification gets no answer.
func (s *Server) handle(line []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}}, true
	}
	if len(req.ID) == 0 {
		return response{}, false
	}
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := ProtocolVersion
		if known[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "tend", "version": s.Version},
			"instructions": "These tools drive the tend session you run in: start another coding agent " +
				"beside you, give it a task, wait for it and read what it says. An agent is named by its " +
				"pane id (p_7) or the name it was started with.",
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{-32602, "invalid params"}
			break
		}
		text, err := s.call(p.Name, p.Arguments)
		if errors.Is(err, errUnknownTool) {
			resp.Error = &rpcError{-32602, err.Error()}
			break
		}
		// A tool that failed answers with what went wrong as its result,
		// marked as an error, so the agent reads why and can try again:
		// the spec keeps protocol errors for a call that could not be made.
		if err != nil {
			resp.Result = map[string]any{"content": []any{textContent(err.Error())}, "isError": true}
			break
		}
		resp.Result = map[string]any{"content": []any{textContent(text)}}
	default:
		resp.Error = &rpcError{-32601, "method not found: " + req.Method}
	}
	return resp, true
}

func textContent(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

var errUnknownTool = errors.New("unknown tool")

// --- the tools ---------------------------------------------------------------

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

var target = str("The agent: its pane id (p_7) or the name it was given.")

var tools = []map[string]any{
	{
		"name":        "list_agents",
		"description": "Every agent in the tend session: its pane id, name, which agent it is, its state (idle, working, blocked), its title and directory.",
		"inputSchema": schema(map[string]any{}),
	},
	{
		"name": "start_agent",
		"description": "Start a coding agent (claude, codex, gemini, opencode, …) in a new tab of your own space, " +
			"optionally with a first task. Returns its pane id. The session's policies decide which agents may start.",
		"inputSchema": schema(map[string]any{
			"agent":  str("Which agent to start: the command, as claude or codex."),
			"name":   str("A name to address it by afterwards, as reviewer."),
			"prompt": str("A first task, typed once the agent is ready."),
			"dir":    str("The directory to start it in. Default: where you are."),
			"split":  map[string]any{"type": "boolean", "description": "Open it beside you in your tab instead of in a tab of its own."},
		}, "agent"),
	},
	{
		"name": "send_message",
		"description": "Type a message into an agent's prompt and send it. With wait, return once the agent " +
			"has worked on it and stopped (idle or blocked), with its state; then read_screen has its answer.",
		"inputSchema": schema(map[string]any{
			"target":          target,
			"text":            str("What to send."),
			"wait":            map[string]any{"type": "boolean", "description": "Wait for the agent to finish with it."},
			"timeout_seconds": map[string]any{"type": "integer", "description": "With wait, how long at most. Default 600."},
		}, "target", "text"),
	},
	{
		"name":        "read_screen",
		"description": "What an agent's terminal shows: its last lines, scrollback included.",
		"inputSchema": schema(map[string]any{
			"target": target,
			"lines":  map[string]any{"type": "integer", "description": "How many of its last lines. Default 80."},
		}, "target"),
	},
	{
		"name": "wait_for_agent",
		"description": "Wait until an agent is idle (finished) or blocked (asking something), then return its state. " +
			"An agent just sent a message may not have started yet: send_message with wait waits for the answer itself.",
		"inputSchema": schema(map[string]any{
			"target": target,
			"until": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"idle", "blocked", "working"}},
				"description": "The states that end the wait. Default idle and blocked."},
			"timeout_seconds": map[string]any{"type": "integer", "description": "How long to wait at most. Default 600."},
		}, "target"),
	},
	{
		"name":        "focus_agent",
		"description": "Show an agent's pane to the person using tend.",
		"inputSchema": schema(map[string]any{"target": target}, "target"),
	},
}

func (s *Server) call(name string, args map[string]any) (string, error) {
	switch name {
	case "list_agents":
		return s.listAgents()
	case "start_agent":
		return s.startAgent(args)
	case "send_message":
		t, err := need(args, "target")
		if err != nil {
			return "", err
		}
		text, err := need(args, "text")
		if err != nil {
			return "", err
		}
		params := map[string]any{"target": t, "text": text}
		wait, _ := args["wait"].(bool)
		timeout := timeoutOf(args)
		if wait {
			// agent.prompt's own wait: for the agent to leave the state it
			// was in first, so an agent still idle from before is not
			// taken to have answered already.
			params["wait"], params["timeout_ms"] = true, timeout.Milliseconds()
		}
		r, err := s.API.Call("agent.prompt", params)
		if err != nil {
			return "", err
		}
		if !wait {
			return "sent to " + t, nil
		}
		return waited(r, timeout), nil
	case "read_screen":
		t, err := need(args, "target")
		if err != nil {
			return "", err
		}
		lines := 80
		if n, ok := args["lines"].(float64); ok && n > 0 {
			lines = int(n)
		}
		r, err := s.API.Call("agent.read", map[string]any{"target": t, "source": "recent", "lines": lines})
		if err != nil {
			return "", err
		}
		return strings.TrimRight(text(r["text"]), "\n"), nil
	case "wait_for_agent":
		return s.waitForAgent(args)
	case "focus_agent":
		t, err := need(args, "target")
		if err != nil {
			return "", err
		}
		if _, err := s.API.Call("agent.focus", map[string]any{"target": t}); err != nil {
			return "", err
		}
		return "showing " + t, nil
	}
	return "", fmt.Errorf("%w: %s", errUnknownTool, name)
}

func need(args map[string]any, key string) (string, error) {
	v := strings.TrimSpace(text(args[key]))
	if v == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return v, nil
}

func text(v any) string {
	s, _ := v.(string)
	return s
}

// describe is one agent as a line an agent reads.
func describe(m map[string]any) string {
	parts := []string{text(m["pane_id"])}
	if n := text(m["name"]); n != "" {
		parts = append(parts, "name="+n)
	}
	if a := text(m["agent"]); a != "" {
		parts = append(parts, "agent="+a)
	}
	parts = append(parts, "state="+text(m["agent_state"]))
	if t := text(m["title"]); t != "" {
		parts = append(parts, fmt.Sprintf("title=%q", t))
	}
	dir := text(m["foreground_cwd"])
	if dir == "" {
		dir = text(m["cwd"])
	}
	if dir != "" {
		parts = append(parts, "dir="+dir)
	}
	if msg := text(m["message"]); msg != "" {
		parts = append(parts, fmt.Sprintf("says=%q", msg))
	}
	return strings.Join(parts, " ")
}

func (s *Server) listAgents() (string, error) {
	r, err := s.API.Call("agent.list", nil)
	if err != nil {
		return "", err
	}
	list, _ := r["agents"].([]any)
	if len(list) == 0 {
		return "no agents in the session", nil
	}
	var b strings.Builder
	for _, v := range list {
		m, _ := v.(map[string]any)
		line := describe(m)
		if text(m["pane_id"]) == s.Caller {
			line += " (you)"
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// where is the pane a new agent opens beside: the caller's, or the one the
// person last focused.
func (s *Server) where() (map[string]any, error) {
	if s.Caller != "" {
		r, err := s.API.Call("pane.get", map[string]any{"pane_id": s.Caller})
		if err != nil {
			return nil, err
		}
		m, _ := r["pane"].(map[string]any)
		return m, nil
	}
	r, err := s.API.Call("pane.current", nil)
	if err != nil {
		return nil, errors.New("there is no pane to start the agent beside: run the tools from a tend pane, or focus one")
	}
	m, _ := r["pane"].(map[string]any)
	return m, nil
}

func (s *Server) startAgent(args map[string]any) (string, error) {
	agentName, err := need(args, "agent")
	if err != nil {
		return "", err
	}
	here, err := s.where()
	if err != nil {
		return "", err
	}
	dir := text(args["dir"])
	if dir == "" {
		dir = text(here["foreground_cwd"])
	}
	if dir == "" {
		dir = text(here["cwd"])
	}

	// A shell first, the agent typed into it, as agent.start does it: the
	// agent runs as the shell's child, and quitting it leaves a prompt.
	var pane string
	if split, _ := args["split"].(bool); split {
		r, err := s.API.Call("pane.split", map[string]any{"pane_id": text(here["pane_id"]), "direction": "right", "dir": dir})
		if err != nil {
			return "", err
		}
		m, _ := r["pane"].(map[string]any)
		pane = text(m["pane_id"])
	} else {
		label := text(args["name"])
		if label == "" {
			label = agentName
		}
		r, err := s.API.Call("tab.create", map[string]any{
			"workspace_id": text(here["workspace_id"]), "name": label, "dir": dir,
		})
		if err != nil {
			return "", err
		}
		m, _ := r["root_pane"].(map[string]any)
		pane = text(m["pane_id"])
	}
	if pane == "" {
		return "", errors.New("tend opened no pane for the agent")
	}
	start := map[string]any{"pane_id": pane, "agent": agentName}
	if n := text(args["name"]); n != "" {
		start["name"] = n
	}
	if _, err := s.API.Call("agent.start", start); err != nil {
		// The pane was opened for the agent; without it, it is litter.
		_, _ = s.API.Call("pane.close", map[string]any{"pane_id": pane})
		return "", err
	}

	prompt := strings.TrimSpace(text(args["prompt"]))
	if prompt == "" {
		return fmt.Sprintf("started %s in %s", agentName, pane), nil
	}
	// Typed before the agent draws its prompt box, a task is read by the
	// shell or lost in the agent's start-up. Detection says when it is
	// ready: idle, or blocked on a question of its own (a folder to trust),
	// which the task must not answer.
	ready := s.ReadyTimeout
	if ready <= 0 {
		ready = 90 * time.Second
	}
	r, err := s.API.Call("agent.wait", map[string]any{
		"target": pane, "until": []string{"idle", "blocked"}, "timeout_ms": ready.Milliseconds(),
	})
	if err != nil {
		return "", fmt.Errorf("started %s in %s, but waiting for it to be ready failed: %w", agentName, pane, err)
	}
	info, _ := r["agent"].(map[string]any)
	if timedOut, _ := r["timed_out"].(bool); timedOut {
		return "", fmt.Errorf("started %s in %s, but it was not ready for the task within %s; read_screen says why", agentName, pane, ready)
	}
	if text(info["agent_state"]) == "blocked" {
		return "", fmt.Errorf("started %s in %s, but it is asking something before it takes a task: %s; read_screen shows it, focus_agent shows it to the person", agentName, pane, text(info["message"]))
	}
	if _, err := s.API.Call("agent.prompt", map[string]any{"target": pane, "text": prompt}); err != nil {
		return "", fmt.Errorf("started %s in %s, but the task could not be sent: %w", agentName, pane, err)
	}
	// A new agent's first Enter can be lost: Claude Code 2.1.325 draws its
	// prompt box — which detection reads as idle — a moment before it acts
	// on Enter, and the task sat typed in the box, never sent (seen with the
	// real one; a prompt to it a few seconds later went through). So the
	// task is taken to be sent once the agent starts on it, and Enter is
	// pressed again if it does not.
	for try := 0; ; try++ {
		r, err := s.API.Call("agent.wait", map[string]any{
			"target": pane, "until": []string{"working", "blocked"}, "timeout_ms": s.settle().Milliseconds(),
		})
		if err != nil {
			return "", fmt.Errorf("started %s in %s and typed the task, but watching it failed: %w", agentName, pane, err)
		}
		if timedOut, _ := r["timed_out"].(bool); !timedOut {
			break
		}
		if try == 1 {
			// Or it did, quickly, before it was watched: the screen says.
			return fmt.Sprintf("started %s in %s and sent the task, but did not see it start on it — it may already be done; read_screen %s shows where it is", agentName, pane, pane), nil
		}
		if _, err := s.API.Call("agent.send_keys", map[string]any{"target": pane, "keys": []string{"enter"}}); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("started %s in %s and it is working on the task; wait_for_agent %s waits for it to finish", agentName, pane, pane), nil
}

// settle is how long a new agent is given to start on its first task.
func (s *Server) settle() time.Duration {
	if s.Settle > 0 {
		return s.Settle
	}
	return 8 * time.Second
}

func (s *Server) waitForAgent(args map[string]any) (string, error) {
	t, err := need(args, "target")
	if err != nil {
		return "", err
	}
	timeout := timeoutOf(args)
	params := map[string]any{"target": t, "timeout_ms": timeout.Milliseconds()}
	if until, ok := args["until"].([]any); ok && len(until) > 0 {
		var states []string
		for _, u := range until {
			states = append(states, text(u))
		}
		params["until"] = states
	}
	r, err := s.API.Call("agent.wait", params)
	if err != nil {
		return "", err
	}
	return waited(r, timeout), nil
}

func timeoutOf(args map[string]any) time.Duration {
	if n, ok := args["timeout_seconds"].(float64); ok && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 600 * time.Second
}

// waited says where a wait ended.
func waited(r map[string]any, timeout time.Duration) string {
	info, _ := r["agent"].(map[string]any)
	line := describe(info)
	if timedOut, _ := r["timed_out"].(bool); timedOut {
		return "not done after " + timeout.String() + ": " + line
	}
	return line
}
