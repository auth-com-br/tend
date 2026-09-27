// Package api is the session's socket for scripts, hooks and other agents.
//
// It is a second door, not a second protocol for the same callers. The client
// protocol is framed and carries terminal output; this one is a line of JSON
// in and a line of JSON out, because what calls it is a hook written in shell
// with `python3 -` or a few lines of JavaScript inside somebody else's agent,
// and the most such a caller can be asked to do is write a line to a socket.
// The shapes are herdr's socket API: {"id","method","params"} answered by
// {"id","result":{"type":...}} or {"id","error":{"code","message"}}.
package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/integration"
	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
)

// Protocol is the version of this API. It changes when a caller written for
// the old one would break.
const Protocol = 1

// maxLine bounds one request. A hook's report is a few hundred bytes; a line
// with no end is a caller that is broken or hostile, and neither should be
// able to grow the server's memory by not sending a newline.
const maxLine = 1 << 20

// Environment a pane's programs find themselves in, which is how a hook knows
// where to report and about which pane. The names mirror herdr's
// (HERDR_ENV / HERDR_SOCKET_PATH / …); the values are tend's.
const (
	EnvMarker      = "TEND_ENV"
	EnvMarkerValue = "1"
	EnvSocketPath  = "TEND_SOCKET_PATH"
	EnvPaneID      = "TEND_PANE_ID"
	EnvBinPath     = "TEND_BIN_PATH"
)

// PaneEnv is what is added to a pane's process environment so a hook inside
// it can find this session. bin may be empty: a server that cannot name its
// own binary still has a socket, and the hook falls back to looking for
// "tend" on PATH.
func PaneEnv(socketPath string, id session.PaneID, bin string) []string {
	env := []string{
		EnvMarker + "=" + EnvMarkerValue,
		EnvSocketPath + "=" + socketPath,
		EnvPaneID + "=" + PaneID(id),
	}
	if bin != "" {
		env = append(env, EnvBinPath+"="+bin)
	}
	return env
}

// Request is one call.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ErrorBody says what went wrong, with a code a script can switch on.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type successResponse struct {
	ID     string `json:"id"`
	Result any    `json:"result"`
}

type errorResponse struct {
	ID    string    `json:"id"`
	Error ErrorBody `json:"error"`
}

// callError is an error with a code.
type callError struct{ body ErrorBody }

func (e *callError) Error() string { return e.body.Message }

func fail(code, format string, args ...any) error {
	return &callError{ErrorBody{Code: code, Message: fmt.Sprintf(format, args...)}}
}

// Methods this build answers.
const (
	MethodPing                    = "ping"
	MethodPaneList                = "pane.list"
	MethodPaneGet                 = "pane.get"
	MethodPaneReportAgent         = "pane.report_agent"
	MethodPaneReportAgentSession  = "pane.report_agent_session"
	MethodPaneReleaseAgent        = "pane.release_agent"
	MethodPaneClearAgentAuthority = "pane.clear_agent_authority"
	MethodPaneReportMetadata      = "pane.report_metadata"
	MethodIntegrationList         = "integration.list"
	MethodIntegrationInstall      = "integration.install"
	MethodIntegrationUninstall    = "integration.uninstall"
)

// PaneID is how a pane is named on this socket: "p_7". A bare number is taken
// too, since that is what `tend ls` prints and what a person will type.
func PaneID(id session.PaneID) string { return "p_" + strconv.FormatUint(uint64(id), 10) }

// ParsePaneID reads a pane name.
func ParsePaneID(s string) (session.PaneID, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "p_")
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return session.PaneID(n), true
}

// PaneInfo describes a pane to a script.
type PaneInfo struct {
	PaneID string `json:"pane_id"`
	// WorkspaceID and TabID are where the pane is; Focused marks the pane
	// last focused; Cwd is where it was opened and ForegroundCwd where its
	// program is now. herdr's names, which a plugin following a pane's
	// directory reads.
	WorkspaceID   string `json:"workspace_id,omitempty"`
	TabID         string `json:"tab_id,omitempty"`
	Focused       bool   `json:"focused,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	ForegroundCwd string `json:"foreground_cwd,omitempty"`
	Title         string `json:"title,omitempty"`
	Agent         string `json:"agent,omitempty"`
	// Name is what a script called the agent (agent.rename), which it can
	// then be addressed by.
	Name    string `json:"name,omitempty"`
	State   string `json:"agent_state"`
	Message string `json:"message,omitempty"`
	Running bool   `json:"running"`
	// Display and Tokens are what a hook asked to have shown about the agent.
	Display string        `json:"display_agent,omitempty"`
	Tokens  []agent.Token `json:"tokens,omitempty"`
	Pid     int           `json:"pid,omitempty"`
	// AgentSession is the conversation a hook has named for the pane.
	AgentSession *AgentSessionInfo `json:"agent_session,omitempty"`
}

// AgentSessionInfo names a conversation in the agent's own terms.
type AgentSessionInfo struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// API serves the socket for one server.
type API struct {
	srv   *server.Server
	build string
	// shellCmd is what a pane opened through this socket runs when the caller
	// names no command, which is the same login shell the client would use.
	shellCmd []string
	// worktreeDir is where a new worktree goes, from the settings file.
	worktreeDir string

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	done  bool
}

// New returns the API for a server. build is reported by ping, and shell is
// what a pane runs when a caller names no command.
func New(srv *server.Server, build string, shell []string) *API {
	if len(shell) == 0 {
		shell = []string{"/bin/sh"}
	}
	return &API{srv: srv, build: build, shellCmd: shell, conns: make(map[net.Conn]struct{})}
}

// SetWorktreeDir says where new worktrees go.
func (a *API) SetWorktreeDir(dir string) { a.worktreeDir = dir }

// shell is the command a pane opened through this socket runs by default.
func (a *API) shell() []string { return append([]string(nil), a.shellCmd...) }

// Serve answers callers until the listener closes.
func (a *API) Serve(ln net.Listener) error {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if !a.track(conn) {
			_ = conn.Close()
			return nil
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer a.forget(conn)
			a.handle(conn)
		}()
	}
}

// Close hangs up every caller. The listener is the caller's to close.
func (a *API) Close() {
	a.mu.Lock()
	a.done = true
	conns := make([]net.Conn, 0, len(a.conns))
	for c := range a.conns {
		conns = append(conns, c)
	}
	a.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (a *API) track(c net.Conn) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return false
	}
	a.conns[c] = struct{}{}
	return true
}

func (a *API) forget(c net.Conn) {
	a.mu.Lock()
	delete(a.conns, c)
	a.mu.Unlock()
	_ = c.Close()
}

// handle answers one connection, a line at a time, until it hangs up. One bad
// line ends only that line: a hook that sends nonsense gets told so and may
// try again.
func (a *API) handle(conn net.Conn) {
	r := bufio.NewReaderSize(conn, 4096)
	enc := json.NewEncoder(conn)
	for {
		line, err := readLine(r)
		if err != nil {
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil || req.Method == "" {
			_ = enc.Encode(errorResponse{ID: req.ID, Error: ErrorBody{
				Code: "invalid_request", Message: "a request is one line of JSON with an id and a method",
			}})
			continue
		}
		var pending pending
		result, err := a.call(req, &pending)
		if err != nil {
			body := ErrorBody{Code: "internal_error", Message: err.Error()}
			var ce *callError
			if errors.As(err, &ce) {
				body = ce.body
			}
			if encErr := enc.Encode(errorResponse{ID: req.ID, Error: body}); encErr != nil {
				return
			}
			continue
		}
		if err := enc.Encode(successResponse{ID: req.ID, Result: result}); err != nil {
			return
		}
		if pending.stream != nil {
			// From here the connection is a stream. Returning ends the
			// handler, which closes it.
			_ = pending.stream(conn)
			return
		}
		if pending.after != nil {
			// Stopping the server, or letting go of its panes, cuts this
			// connection. Doing it after the reply is on the wire is what lets
			// the caller tell "it worked" from "it died".
			go pending.after()
		}
	}
}

// readLine reads up to a newline, refusing a line longer than maxLine.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, errors.New("api: request too long")
		}
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
	}
}

func decode(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fail("invalid_params", "params: %v", err)
	}
	return nil
}

func ok() any { return map[string]string{"type": "ok"} }

func (a *API) pane(name string) (session.PaneID, error) {
	id, valid := ParsePaneID(name)
	if !valid {
		return 0, fail("pane_not_found", "pane %s not found", name)
	}
	return id, nil
}

// paneErr turns the server's errors into the codes scripts switch on.
func paneErr(name string, err error) error {
	switch {
	case errors.Is(err, session.ErrNoSuchPane):
		return fail("pane_not_found", "pane %s not found", name)
	case errors.Is(err, server.ErrBadAgent):
		return fail("invalid_agent", "agent label must not be empty")
	case errors.Is(err, server.ErrPolicy):
		return fail("policy_denied", "%s", err.Error())
	}
	var badKey *server.ErrUnknownKey
	if errors.As(err, &badKey) {
		return fail("invalid_key", "unsupported key %q", badKey.Key)
	}
	return err
}

func parseState(s string) (detect.State, error) {
	switch s {
	case "idle":
		return detect.StateIdle, nil
	case "working":
		return detect.StateWorking, nil
	case "blocked":
		return detect.StateBlocked, nil
	case "unknown":
		return detect.StateUnknown, nil
	}
	return 0, fail("invalid_params", "state %q is not one of idle, working, blocked, unknown", s)
}

func (a *API) info(st server.PaneStatus) PaneInfo {
	info := PaneInfo{
		PaneID: PaneID(st.ID), Title: st.Title, Agent: st.Agent, State: st.State.String(),
		Message: st.Message, Running: st.Running, Pid: st.Pid,
		Display: st.Presentation.DisplayAgent, Tokens: st.Presentation.Tokens,
		Name: a.srv.AgentName(st.ID),
	}
	if c, err := a.srv.PaneContext(st.ID); err == nil {
		if c.Tab != 0 {
			info.WorkspaceID, info.TabID = WorkspaceID(c.Workspace), TabID(c.Tab)
		}
		info.Focused, info.Cwd, info.ForegroundCwd = c.Focused, c.Dir, c.Cwd
	}
	if p, ok, err := a.srv.AgentSession(st.ID); err == nil && ok {
		info.AgentSession = &AgentSessionInfo{
			Source: p.Source, Agent: p.Agent, Kind: p.Session.Kind(), Value: p.Session.Value(),
		}
	}
	return info
}

// pending is what one call asked to happen after its reply has been written.
// It belongs to the connection being answered, not to the API: two scripts
// calling at once must not inherit each other's.
type pending struct {
	after func()
	// stream turns the connection into an event stream once the reply has
	// been written. Nothing else can be asked on it afterwards.
	stream func(net.Conn) error
}

func (a *API) call(req Request, p *pending) (any, error) {
	switch req.Method {
	case MethodPing:
		return map[string]any{"type": "pong", "version": a.build, "protocol": Protocol}, nil

	case MethodPaneList:
		statuses := a.srv.Statuses()
		panes := make([]PaneInfo, 0, len(statuses))
		for _, st := range statuses {
			panes = append(panes, a.info(st))
		}
		return map[string]any{"type": "pane_list", "panes": panes}, nil

	case MethodPaneGet:
		var p PaneGetParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		st, err := a.srv.PaneStatus(id)
		if err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return map[string]any{"type": "pane_info", "pane": a.info(st)}, nil

	case MethodPaneReportAgent:
		var p PaneReportAgentParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		state, err := parseState(p.State)
		if err != nil {
			return nil, err
		}
		// Whether it was believed is not reported, as in herdr: a late report
		// is not the hook's error, and a hook has nothing to do about it.
		if _, err := a.srv.ReportAgent(id, agent.Report{
			Source: p.Source, Agent: p.Agent, State: state, Message: p.Message, Seq: p.Seq,
			Session: agent.SessionRefFromReport(p.Source, agent.NormalizeLabel(p.Agent), p.AgentSessionID, p.AgentSessionPath),
		}); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok(), nil

	case MethodPaneReportAgentSession:
		var p PaneReportAgentSessionParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		ref := agent.SessionRefFromReport(p.Source, agent.NormalizeLabel(p.Agent), p.AgentSessionID, p.AgentSessionPath)
		if _, err := a.srv.ReportAgentSession(id, p.Source, p.Agent, ref, p.Seq); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok(), nil

	case MethodPaneReleaseAgent:
		var p PaneReleaseAgentParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if _, err := a.srv.ReleaseAgent(id, p.Source, p.Agent, p.Seq); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok(), nil

	case MethodPaneReportMetadata:
		var p PaneReportMetadataParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if _, err := a.srv.ReportMetadata(id, agent.MetadataReport{
			Source: p.Source, Agent: p.Agent, Title: p.Title, DisplayAgent: p.DisplayAgent,
			StateLabels: p.StateLabels, Tokens: p.Tokens,
			TTL:               time.Duration(p.TTLMs) * time.Millisecond,
			ClearTitle:        p.ClearTitle,
			ClearDisplayAgent: p.ClearDisplayAgent,
			ClearStateLabels:  p.ClearStateLabels,
			Seq:               p.Seq,
		}); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok(), nil

	case MethodPaneClearAgentAuthority:
		var p PaneClearAgentAuthorityParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		id, err := a.pane(p.PaneID)
		if err != nil {
			return nil, err
		}
		if _, err := a.srv.ClearAgentAuthority(id, p.Source, p.Seq); err != nil {
			return nil, paneErr(p.PaneID, err)
		}
		return ok(), nil

	case MethodIntegrationList:
		infos := integration.ListInfos()
		return map[string]any{"type": "integration_list", "integrations": infos}, nil

	case MethodIntegrationInstall:
		var p IntegrationInstallParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		target, err := integration.ParseTarget(p.Target)
		if err != nil {
			return nil, fail("invalid_params", "%v", err)
		}
		messages, err := integration.Install(target)
		if err != nil {
			return nil, fail("integration_install_failed", "%v", err)
		}
		return map[string]any{
			"type":    "integration_install",
			"target":  target.Label(),
			"details": map[string]any{"messages": messages},
		}, nil

	case MethodIntegrationUninstall:
		var p IntegrationUninstallParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		target, err := integration.ParseTarget(p.Target)
		if err != nil {
			return nil, fail("invalid_params", "%v", err)
		}
		messages, err := integration.Uninstall(target)
		if err != nil {
			return nil, fail("integration_uninstall_failed", "%v", err)
		}
		return map[string]any{
			"type":    "integration_uninstall",
			"target":  target.Label(),
			"details": map[string]any{"messages": messages},
		}, nil
	}
	return a.callMore(req, p)
}
