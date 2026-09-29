// Package client talks to a tend server over a socket.
//
// A Client is the other half of internal/proto: it turns method calls into
// frames and matches replies back to the caller, so nothing above it deals
// with the wire.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
)

// ErrClosed is returned once the connection is gone.
var ErrClosed = errors.New("client: connection closed")

// Refusal is a request the server answered with an error of its own: a
// folder that is not there, a name that is empty, a pane that has gone. The
// connection is fine and the server said no, which is something to tell the
// user, never a reason to stop — a client that quit on it closed tend over a
// mistyped folder.
type Refusal struct {
	Method  string
	Message string
}

func (r *Refusal) Error() string { return r.Method + ": " + r.Message }

// callTimeout bounds a request. A server that stops answering must surface as
// a failed call rather than a hung client.
const callTimeout = 30 * time.Second

// Handler receives what the server sends unsolicited. Every method is called
// from the client's reader goroutine, so none may block for long or call back
// into the same client.
type Handler interface {
	// Disconnected reports that the connection ended. It is how a client
	// learns to stop drawing a session it can no longer see, and when to
	// reconnect.
	Disconnected()

	// Event reports something that happened in the session.
	Event(proto.Event)
	// PaneOutput reports a pane's current screen. data is only valid for the
	// duration of the call.
	PaneOutput(pane uint64, data []byte)
}

// Client is a connection to a tend server.
type Client struct {
	conn    *proto.Conn
	handler Handler

	nextID atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan proto.Response
	closed  bool
	closeWG sync.WaitGroup

	server proto.HelloResult
}

// Dial connects to a session's socket and completes the handshake.
//
// The handshake is not optional: a version mismatch has to be reported as
// itself, or it shows up later as a message the other side cannot parse.
func Dial(socketPath string, handler Handler) (*Client, error) {
	nc, err := transport.Dial(socketPath)
	if err != nil {
		return nil, err
	}
	return Attach(nc, handler)
}

// DialRemote reaches a session on another machine through the command that
// carries it, and completes the handshake.
//
// When the handshake fails, what the command said on stderr is the reason and
// is put in the error: "tend: command not found" or a refused host key is what
// the user needs, and "connection closed" is what they would otherwise get.
func DialRemote(argv []string, handler Handler) (*Client, error) {
	rc, err := transport.Remote(argv)
	if err != nil {
		return nil, err
	}
	c, err := Attach(rc, handler)
	if err != nil {
		_ = rc.Close()
		if why := rc.Complaint(); why != "" {
			return nil, fmt.Errorf("%w: %s", err, why)
		}
		return nil, err
	}
	return c, nil
}

// Attach completes the handshake over a connection that is already open.
func Attach(nc io.ReadWriteCloser, handler Handler) (*Client, error) {
	if handler == nil {
		handler = nopHandler{}
	}
	c := &Client{
		conn:    proto.NewConn(nc),
		handler: handler,
		pending: make(map[uint64]chan proto.Response),
	}

	c.closeWG.Add(1)
	go c.readLoop()

	var hello proto.HelloResult
	err := c.Call(proto.MethodHello, proto.HelloParams{
		Version: proto.Version,
		Client:  "tend",
	}, &hello)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("client: handshake: %w", err)
	}
	c.server = hello
	return c, nil
}

// Server returns what the server said about itself during the handshake.
func (c *Client) Server() proto.HelloResult { return c.server }

// Supports reports whether the server advertised a method.
//
// A client should disable an action the server cannot perform rather than let
// a user try it and receive an error, which is the difference between a
// feature being absent and being broken.
func (c *Client) Supports(method string) bool {
	for _, m := range c.server.Methods {
		if m == method {
			return true
		}
	}
	return false
}

// Call sends a request and waits for its answer. result may be nil.
func (c *Client) Call(method string, params, result any) error {
	id := c.nextID.Add(1)

	req := proto.Request{ID: id, Method: method}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("client: encoding params for %s: %w", method, err)
		}
		req.Params = encoded
	}

	reply := make(chan proto.Response, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.pending[id] = reply
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.conn.WriteJSON(proto.FrameRequest, req); err != nil {
		return fmt.Errorf("client: sending %s: %w", method, err)
	}

	select {
	case resp := <-reply:
		if resp.Error != "" {
			if strings.Contains(resp.Error, proto.ErrUnknownMethod.Error()) {
				// Carried across the wire as text and recognised here, so a
				// caller can say what it means rather than repeating it.
				return fmt.Errorf("%s: %w", method, proto.ErrUnknownMethod)
			}
			return &Refusal{Method: method, Message: resp.Error}
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("client: decoding %s result: %w", method, err)
		}
		return nil
	case <-time.After(callTimeout):
		return fmt.Errorf("client: %s timed out", method)
	}
}

// SendInput sends keystrokes to a pane. It does not wait for an answer: input
// is a stream, and a round trip per keystroke would be felt.
func (c *Client) SendInput(pane uint64, data []byte) error {
	return c.conn.WritePaneBytes(proto.FrameInput, pane, data)
}

// Close ends the connection. Pending calls fail with ErrClosed.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	err := c.conn.Close()
	c.closeWG.Wait()
	return err
}

// readLoop dispatches everything the server sends.
func (c *Client) readLoop() {
	defer c.closeWG.Done()
	defer c.handler.Disconnected()
	defer c.failPending()

	for {
		frame, err := c.conn.ReadFrame()
		if err != nil {
			return // including io.EOF: the server hung up
		}

		switch frame.Type {
		case proto.FrameResponse:
			var resp proto.Response
			if err := json.Unmarshal(frame.Payload, &resp); err != nil {
				continue
			}
			c.deliver(resp)

		case proto.FrameEvent:
			var ev proto.Event
			if err := json.Unmarshal(frame.Payload, &ev); err != nil {
				continue
			}
			c.handler.Event(ev)

		case proto.FrameOutput:
			pane, data, err := proto.DecodePaneBytes(frame.Payload)
			if err != nil {
				continue
			}
			c.handler.PaneOutput(pane, data)

		default:
			// A frame type this build does not know is ignored, so a newer
			// server can send one without breaking an older client.
			continue
		}
	}
}

func (c *Client) deliver(resp proto.Response) {
	c.mu.Lock()
	reply, ok := c.pending[resp.ID]
	c.mu.Unlock()
	if !ok {
		// A late answer to a call that already gave up. Dropping it is right:
		// the caller has moved on.
		return
	}
	select {
	case reply <- resp:
	default:
	}
}

// failPending wakes every waiting call when the connection ends, so a caller
// learns the server is gone instead of waiting out the timeout.
func (c *Client) failPending() {
	c.mu.Lock()
	c.closed = true
	pending := c.pending
	c.pending = make(map[uint64]chan proto.Response)
	c.mu.Unlock()

	for id, reply := range pending {
		select {
		case reply <- proto.Response{ID: id, Error: ErrClosed.Error()}:
		default:
		}
	}
}

type nopHandler struct{}

func (nopHandler) Event(proto.Event)         {}
func (nopHandler) PaneOutput(uint64, []byte) {}
func (nopHandler) Disconnected()             {}

// --- convenience wrappers --------------------------------------------------
//
// These exist so callers name a method once, here, rather than at every call
// site. A misspelled method string is otherwise a runtime error found by a
// user rather than a compile error found by the build.

// Snapshot returns the whole session.
func (c *Client) Snapshot() (proto.SessionSnapshot, error) {
	var out proto.SessionSnapshot
	return out, c.Call(proto.MethodSessionSnapshot, nil, &out)
}

// NewWorkspace adds a workspace.
func (c *Client) NewWorkspace(name string) (uint64, error) {
	return c.NewWorkspaceIn(name, "")
}

// NewWorkspaceIn adds a workspace rooted at a directory.
func (c *Client) NewWorkspaceIn(name, dir string) (uint64, error) {
	var out proto.WorkspaceNewResult
	err := c.Call(proto.MethodWorkspaceNew, proto.WorkspaceNewParams{Name: name, Dir: dir}, &out)
	return out.Workspace, err
}

// NewTab creates a tab with one pane.
func (c *Client) NewTab(workspace uint64, name string, spec proto.PaneSpec) (tab, pane uint64, err error) {
	var out proto.TabNewResult
	err = c.Call(proto.MethodTabNew, proto.TabNewParams{
		Workspace: workspace,
		Name:      name,
		Pane:      spec,
	}, &out)
	return out.Tab, out.Pane, err
}

// CloseTab closes a tab and every pane in it.
func (c *Client) CloseTab(tab uint64) error {
	return c.Call(proto.MethodTabClose, proto.TabCloseParams{Tab: tab}, nil)
}

// RenameTab changes a tab's label.
// GroupWorkspace moves a workspace into a group, or out of one when the name
// is empty.
func (c *Client) GroupWorkspace(ws uint64, group string) error {
	return c.Call(proto.MethodWorkspaceGroup, proto.WorkspaceGroupParams{Workspace: ws, Group: group}, nil)
}

// RenameGroup names a group of spaces again, with every space in it.
func (c *Client) RenameGroup(group, name string) error {
	return c.Call(proto.MethodGroupRename, proto.GroupParams{Group: group, Name: name}, nil)
}

// DeleteGroup deletes a group of spaces; its spaces stay, in no group.
func (c *Client) DeleteGroup(group string) error {
	return c.Call(proto.MethodGroupDelete, proto.GroupParams{Group: group}, nil)
}

// SetGroupDir sets the folder a group's new spaces start in; empty clears it.
func (c *Client) SetGroupDir(group, dir string) error {
	return c.Call(proto.MethodGroupSetDir, proto.GroupParams{Group: group, Dir: dir}, nil)
}

// ListDirs lists the folders in a folder on the server's machine.
func (c *Client) ListDirs(path string) (proto.DirListResult, error) {
	var out proto.DirListResult
	err := c.Call(proto.MethodDirList, proto.DirListParams{Path: path}, &out)
	return out, err
}

// CreateCompany makes a company, with a workspace in it already when ws is
// not zero, and returns its id.
func (c *Client) CreateCompany(name string, ws uint64) (uint64, error) {
	var res proto.CompanyCreateResult
	err := c.Call(proto.MethodCompanyCreate, proto.CompanyParams{Name: name, Workspace: ws}, &res)
	return res.Company, err
}

// RenameCompany names a company again.
func (c *Client) RenameCompany(company uint64, name string) error {
	return c.Call(proto.MethodCompanyRename, proto.CompanyParams{Company: company, Name: name}, nil)
}

// DeleteCompany deletes a company; its workspaces are left as they are.
func (c *Client) DeleteCompany(company uint64) error {
	return c.Call(proto.MethodCompanyDelete, proto.CompanyParams{Company: company}, nil)
}

// AssignCompany puts a workspace in a company, or with in false takes it
// out.
func (c *Client) AssignCompany(company, ws uint64, in bool) error {
	method := proto.MethodCompanyAssign
	if !in {
		method = proto.MethodCompanyUnassign
	}
	return c.Call(method, proto.CompanyParams{Company: company, Workspace: ws}, nil)
}

// CloseWorkspace closes a workspace and every pane in it.
func (c *Client) CloseWorkspace(ws uint64) error {
	return c.Call(proto.MethodWorkspaceClose, proto.WorkspaceCloseParams{Workspace: ws}, nil)
}

func (c *Client) RenameTab(tab uint64, name string) error {
	return c.Call(proto.MethodTabRename, proto.TabRenameParams{Tab: tab, Name: name}, nil)
}

// RenameWorkspace changes a workspace's label.
func (c *Client) RenameWorkspace(ws uint64, name string) error {
	return c.Call(proto.MethodWorkspaceRename, proto.WorkspaceRenameParams{
		Workspace: ws, Name: name,
	}, nil)
}

// SplitPane divides a pane. direction is "columns" or "rows".
func (c *Client) SplitPane(target uint64, direction string, spec proto.PaneSpec) (uint64, error) {
	var out proto.PaneSplitResult
	err := c.Call(proto.MethodPaneSplit, proto.PaneSplitParams{
		Target:    target,
		Direction: direction,
		Pane:      spec,
	}, &out)
	return out.Pane, err
}

// DockPane opens a pane along the left (or right) edge of beside's tab,
// taking share of its width.
func (c *Client) DockPane(beside uint64, share float64, right bool, spec proto.PaneSpec) (uint64, error) {
	pane, _, err := c.DockPaneUnless(beside, share, right, spec, "")
	return pane, err
}

// DockPaneUnless docks a pane unless beside's tab already has one named
// unless, and says which happened.
func (c *Client) DockPaneUnless(beside uint64, share float64, right bool, spec proto.PaneSpec, unless string) (uint64, bool, error) {
	var out proto.PaneSplitResult
	err := c.Call(proto.MethodPaneDock, proto.PaneDockParams{Beside: beside, Share: share, Right: right, Pane: spec, Unless: unless}, &out)
	return out.Pane, out.Existing, err
}

// ActivateLink offers a URL clicked in a pane to the plugins, and reports
// whether one took it.
func (c *Client) ActivateLink(pane uint64, url string) (bool, error) {
	var out proto.PaneLinkResult
	err := c.Call(proto.MethodPaneLinkActivate, proto.PaneLinkParams{Pane: pane, URL: url}, &out)
	return out.Handled, err
}

// ClosePane closes one pane.
func (c *Client) ClosePane(pane uint64) error {
	return c.Call(proto.MethodPaneClose, proto.PaneCloseParams{Pane: pane}, nil)
}

// ResizePane changes a pane's terminal size.
func (c *Client) ResizePane(pane uint64, cols, rows int) error {
	return c.Call(proto.MethodPaneResize, proto.PaneResizeParams{
		Pane: pane, Cols: cols, Rows: rows,
	}, nil)
}

// SubscribePanes replaces the set of panes whose screens this client receives.
func (c *Client) SubscribePanes(panes []uint64) error {
	return c.Call(proto.MethodPaneSubscribe, proto.PaneSubscribeParams{Panes: panes}, nil)
}

// PaneScreen fetches a pane's current screen.
func (c *Client) PaneScreen(pane uint64) (proto.PaneScreenResult, error) {
	return c.PaneScreenAt(pane, 0)
}

// PaneScreenAt fetches a pane as it was offset lines back through its history.
func (c *Client) PaneScreenAt(pane uint64, offset int) (proto.PaneScreenResult, error) {
	var out proto.PaneScreenResult
	return out, c.Call(proto.MethodPaneScreen, proto.PaneScreenParams{
		Pane: pane, Offset: offset,
	}, &out)
}

// AdjustSplit moves one edge of a pane. side is "left", "right", "up" or
// "down"; cols and rows are the size the tab is being drawn in.
// PaneText reads the text in a region of a pane, which may reach outside the
// view into the scrollback.
func (c *Client) PaneText(p proto.PaneTextParams) (string, error) {
	var out proto.PaneTextResult
	err := c.Call(proto.MethodPaneText, p, &out)
	return out.Text, err
}

func (c *Client) AdjustSplit(pane uint64, side string, cells, cols, rows int) error {
	return c.Call(proto.MethodPaneAdjust, proto.PaneAdjustParams{
		Target: pane, Side: side, Cells: cells, Cols: cols, Rows: rows,
	}, nil)
}

// TabLayout asks where a tab's panes go at the given size.
func (c *Client) TabLayout(tab uint64, cols, rows int) (proto.TabLayoutResult, error) {
	var out proto.TabLayoutResult
	return out, c.Call(proto.MethodTabLayout, proto.TabLayoutParams{
		Tab: tab, Cols: cols, Rows: rows,
	}, &out)
}

// CopyMotion asks where a copy-mode motion lands.
func (c *Client) CopyMotion(pane uint64, from proto.CopyPoint, motion string) (proto.PaneCopyResult, error) {
	var out proto.PaneCopyResult
	return out, c.Call(proto.MethodPaneCopyMotion, proto.PaneCopyMotionParams{
		Pane: pane, From: from, Motion: motion,
	}, &out)
}

// CopySearch asks where the nearest match of a query is.
func (c *Client) CopySearch(pane uint64, from proto.CopyPoint, query, direction string) (proto.PaneCopyResult, error) {
	var out proto.PaneCopyResult
	return out, c.Call(proto.MethodPaneCopySearch, proto.PaneCopySearchParams{
		Pane: pane, From: from, Query: query, Direction: direction,
	}, &out)
}

// EditScrollback opens a pane's history in an editor, in a new pane.
func (c *Client) EditScrollback(pane uint64) (uint64, error) {
	var out proto.PaneSplitResult
	err := c.Call(proto.MethodPaneEditScrollback, proto.PaneScreenParams{Pane: pane}, &out)
	return out.Pane, err
}

// RunCommand runs one of the user's commands from a pane, and says which pane
// it opened, if it opened one.
func (c *Client) RunCommand(pane uint64, kind, command string, size ...string) (uint64, error) {
	var out proto.CommandRunResult
	params := proto.CommandRunParams{Pane: pane, Type: kind, Command: command}
	if len(size) == 2 {
		params.Width, params.Height = size[0], size[1]
	}
	err := c.Call(proto.MethodCommandRun, params, &out)
	return out.Pane, err
}

// RenamePane gives a pane a name of its own.
func (c *Client) RenamePane(pane uint64, name string) error {
	return c.Call(proto.MethodPaneRename, proto.PaneRenameParams{Pane: pane, Name: name}, nil)
}

// FocusPane tells the server which pane is being looked at, so a program that
// asked for focus events hears about it.
func (c *Client) FocusPane(pane, lost uint64) error {
	return c.Call(proto.MethodPaneFocus, proto.PaneFocusParams{Pane: pane, Lost: lost}, nil)
}

// WindowFocus tells the server whether the terminal's window has focus.
func (c *Client) WindowFocus(focused bool) error {
	return c.Call(proto.MethodPaneFocus, proto.PaneFocusParams{Window: &focused}, nil)
}

// PaneGraphics fetches a pane's images and where they go.
func (c *Client) PaneGraphics(pane uint64) (proto.PaneGraphicsResult, error) {
	var out proto.PaneGraphicsResult
	return out, c.Call(proto.MethodPaneGraphics, proto.PaneScreenParams{Pane: pane}, &out)
}

// ReloadConfig makes the server re-read the settings file.
// GitHubIssues lists the issues of the repository a pane's project is on.
func (c *Client) GitHubIssues(p proto.GitHubIssuesParams) (proto.GitHubIssuesResult, error) {
	var out proto.GitHubIssuesResult
	return out, c.Call(proto.MethodGitHubIssues, p, &out)
}

// GitHubIssueComment comments on an issue.
func (c *Client) GitHubIssueComment(repo string, number int, body string) error {
	return c.Call(proto.MethodGitHubIssueComment, proto.GitHubIssueParams{Repo: repo, Number: number, Body: body}, nil)
}

// GitHubIssueState closes an issue for a reason, or opens it again.
func (c *Client) GitHubIssueState(repo string, number int, state, reason string) error {
	return c.Call(proto.MethodGitHubIssueState, proto.GitHubIssueParams{Repo: repo, Number: number, State: state, Reason: reason}, nil)
}

// GitHubIssueCreate files an issue.
func (c *Client) GitHubIssueCreate(repo, title, body string) (proto.GitHubIssueCreated, error) {
	var out proto.GitHubIssueCreated
	return out, c.Call(proto.MethodGitHubIssueCreate, proto.GitHubIssueParams{Repo: repo, Title: title, Body: body}, &out)
}

// GitHubPRs lists the pull requests of the repository a pane's project is on.
func (c *Client) GitHubPRs(p proto.GitHubIssuesParams) (proto.GitHubPRsResult, error) {
	var out proto.GitHubPRsResult
	return out, c.Call(proto.MethodGitHubPRs, p, &out)
}

// GitHubPRChecks reads the checks of a list's first pull requests.
func (c *Client) GitHubPRChecks(p proto.GitHubIssuesParams) (proto.GitHubPRChecks, error) {
	var out proto.GitHubPRChecks
	return out, c.Call(proto.MethodGitHubPRChecks, p, &out)
}

// GitHubRepoOptions is a repository's labels, or who can be assigned.
func (c *Client) GitHubRepoOptions(repo, kind string) ([]string, error) {
	var out proto.GitHubRepoOptions
	err := c.Call(proto.MethodGitHubRepoOptions, proto.GitHubRepoOptionsParams{Repo: repo, Kind: kind}, &out)
	return out.Names, err
}

// GitHubIssueEdit changes an issue's title, labels or assignees.
func (c *Client) GitHubIssueEdit(p proto.GitHubIssueEditParams) error {
	return c.Call(proto.MethodGitHubIssueEdit, p, nil)
}

// GitHubPR reads one pull request whole.
func (c *Client) GitHubPR(repo string, number int) (proto.GitHubPRDetail, error) {
	var out proto.GitHubPRDetail
	return out, c.Call(proto.MethodGitHubPR, proto.GitHubIssueParams{Repo: repo, Number: number}, &out)
}

// GitHubPRAction comments on, merges, closes, reopens or readies a pull
// request.
func (c *Client) GitHubPRAction(p proto.GitHubPRActionParams) error {
	return c.Call(proto.MethodGitHubPRAction, p, nil)
}

// GitHubIssuePRs is the pull requests an issue has.
func (c *Client) GitHubIssuePRs(repo string, number int, dir string) ([]proto.GitHubPR, error) {
	var out []proto.GitHubPR
	return out, c.Call(proto.MethodGitHubIssuePRs, proto.GitHubIssuePRsParams{Repo: repo, Number: number, Dir: dir}, &out)
}

// GitHubIssue reads one issue with its thread.
func (c *Client) GitHubIssue(repo string, number int) (proto.GitHubIssueDetail, error) {
	var out proto.GitHubIssueDetail
	return out, c.Call(proto.MethodGitHubIssue, proto.GitHubIssueParams{Repo: repo, Number: number}, &out)
}

// AgentSessions lists the conversations agents keep on the server's
// machine, the last written first.
func (c *Client) AgentSessions() (proto.AgentSessionsResult, error) {
	var out proto.AgentSessionsResult
	return out, c.Call(proto.MethodAgentSessions, nil, &out)
}

// AgentHandoff reads a conversation — the pane's when pane is not zero, else
// the one with id — for another agent to carry on.
func (c *Client) AgentHandoff(pane uint64, id string) (proto.AgentHandoff, error) {
	var out proto.AgentHandoff
	return out, c.Call(proto.MethodAgentSessionHandoff, proto.AgentHandoffParams{Pane: pane, ID: id}, &out)
}

// DeleteAgentSessions deletes conversations; one open in a pane is kept.
func (c *Client) DeleteAgentSessions(ids []string) (proto.AgentSessionsDeleteResult, error) {
	var out proto.AgentSessionsDeleteResult
	return out, c.Call(proto.MethodAgentSessionsDelete, proto.AgentSessionsDeleteParams{IDs: ids}, &out)
}

// AgentsCatalog lists the agent CLIs the server's machine has, and how to
// install the ones it has not.
func (c *Client) AgentsCatalog() (proto.AgentsCatalogResult, error) {
	var out proto.AgentsCatalogResult
	return out, c.Call(proto.MethodAgentsCatalog, nil, &out)
}

// BrowserOpen hands a page to the browsers attached to the server, and says
// how many took it.
func (c *Client) BrowserOpen(url string) (int, error) {
	var out proto.BrowserOpenResult
	err := c.Call(proto.MethodBrowserOpen, proto.BrowserOpenParams{URL: url}, &out)
	return out.Browsers, err
}

// ContextList reads the context buffer, oldest first.
func (c *Client) ContextList() ([]proto.ContextItem, error) {
	var out proto.ContextList
	return out.Items, c.Call(proto.MethodContextList, nil, &out)
}

// ContextRemove takes items out of the buffer; ContextClear takes them all.
func (c *Client) ContextRemove(ids []uint64) error {
	return c.Call(proto.MethodContextRemove, proto.ContextIDs{IDs: ids}, nil)
}

func (c *Client) ContextClear() error {
	return c.Call(proto.MethodContextClear, proto.ContextIDs{}, nil)
}

// ContextAdd puts an item in the buffer, and says what was kept.
func (c *Client) ContextAdd(item proto.ContextItem) (proto.ContextItem, error) {
	var out proto.ContextItem
	return out, c.Call(proto.MethodContextAdd, item, &out)
}

// ContextSend types items (none: all) into a pane, submitting nothing.
func (c *Client) ContextSend(pane uint64, ids []uint64) error {
	return c.Call(proto.MethodContextSend, proto.ContextSendParams{Pane: pane, IDs: ids}, nil)
}

// ReleaseNotes reads the release notes the server keeps.
func (c *Client) ReleaseNotes() (proto.ReleaseNotes, error) {
	var out proto.ReleaseNotes
	return out, c.Call(proto.MethodReleaseNotes, nil, &out)
}

// DismissReleaseNotes marks the notes of a version read.
func (c *Client) DismissReleaseNotes(version string) error {
	return c.Call(proto.MethodReleaseNotesDismiss, proto.ReleaseNotesDismissParams{Version: version}, nil)
}

func (c *Client) ReloadConfig() (proto.ReloadResult, error) {
	var out proto.ReloadResult
	return out, c.Call(proto.MethodServerReloadConfig, nil, &out)
}

// SwapPaneToward exchanges a pane with its neighbour on a side, as laid out at
// cols by rows, and names the neighbour.
func (c *Client) SwapPaneToward(pane uint64, side string, cols, rows int) (uint64, error) {
	var out proto.PaneSwapResult
	err := c.Call(proto.MethodPaneSwap, proto.PaneSwapParams{
		Pane: pane, Side: side, Cols: cols, Rows: rows,
	}, &out)
	return out.Other, err
}

// SwapPanes exchanges two panes.
func (c *Client) SwapPanes(pane, target uint64) error {
	return c.Call(proto.MethodPaneSwap, proto.PaneSwapParams{Pane: pane, Target: target}, nil)
}

// MoveTab moves a tab delta places along its row.
func (c *Client) MoveTab(tab uint64, delta int) error {
	return c.Call(proto.MethodTabMove, proto.MoveParams{ID: tab, Delta: delta}, nil)
}

// MoveWorkspace moves a space delta places along the list.
func (c *Client) MoveWorkspace(ws uint64, delta int) error {
	return c.Call(proto.MethodWorkspaceMove, proto.MoveParams{ID: ws, Delta: delta}, nil)
}

// Handoff asks the server to replace itself with the binary now on disk,
// keeping what runs in its panes. The connection drops once it has.
func (c *Client) Handoff() error { return c.HandoffTo("") }

// HandoffTo asks the server to replace itself with a particular binary.
func (c *Client) HandoffTo(binary string) error {
	return c.Call(proto.MethodServerHandoff, proto.HandoffParams{Binary: binary}, nil)
}

// Shutdown asks the server to stop.
func (c *Client) Shutdown() error {
	return c.Call(proto.MethodServerShutdown, nil, nil)
}
