package proto

import (
	"encoding/json"
	"errors"

	"github.com/auth-com-br/tend/internal/agentview"
)

// Method names are permanent. Renaming one is a protocol break; adding one is
// not, because an unknown method is answered with an error rather than a
// disconnect, so an older server disables one action instead of refusing the
// client.
const (
	MethodHello = "hello"

	MethodSessionSnapshot = "session.snapshot"

	MethodWorkspaceNew = "workspace.new"

	MethodTabNew    = "tab.new"
	MethodTabClose  = "tab.close"
	MethodTabRename = "tab.rename"

	MethodWorkspaceClose  = "workspace.close"
	MethodWorkspaceGroup  = "workspace.group"
	MethodWorkspaceRename = "workspace.rename"

	MethodPaneSplit     = "pane.split"
	MethodPaneClose     = "pane.close"
	MethodPaneResize    = "pane.resize"
	MethodPaneSubscribe = "pane.subscribe"
	MethodPaneScreen    = "pane.screen"
	MethodPaneText      = "pane.text"

	MethodPaneAdjust = "pane.adjust"

	MethodTabLayout = "tab.layout"

	MethodServerShutdown = "server.shutdown"
	// MethodServerHandoff replaces the server with a new process running the
	// binary now on disk, keeping every pane's program. The reply comes once
	// the replacement holds the panes; the connection drops right after, and
	// reconnecting reaches the new server on the same socket.
	MethodServerHandoff = "server.handoff"
	// MethodPaneCopyMotion and MethodPaneCopySearch are copy mode's questions
	// to the server: where a motion lands, and where the next match is. The
	// cursor lives in the client; the text it moves through lives here.
	MethodPaneCopyMotion = "pane.copy_motion"
	MethodPaneCopySearch = "pane.copy_search"
	// MethodPaneSwap, MethodTabMove and MethodWorkspaceMove rearrange without
	// making anything again: the programs keep running wherever they end up.
	// MethodServerReloadConfig makes the server re-read the settings file.
	MethodServerReloadConfig = "server.reload_config"
	// MethodReleaseNotes reads the release notes the server keeps, and
	// MethodReleaseNotesDismiss marks them read: herdr's release_notes.
	// The notes are the server's, saved when its check found a release, so
	// a client on another machine reads the ones for the build it talks to.
	MethodReleaseNotes        = "release_notes.get"
	MethodReleaseNotesDismiss = "release_notes.dismiss"
	// MethodAgentsCatalog lists the agent CLIs tend knows of, as found on
	// the server's machine — where panes, and so agents, run — with how to
	// install the ones that are not there (internal/agents).
	MethodAgentsCatalog = "agents.catalog"
	// MethodAgentSessions lists the conversations agents keep on the
	// server's machine, and MethodAgentSessionsDelete deletes some
	// (internal/agentsessions): tend's own sessions list.
	MethodAgentSessions       = "agent_sessions.list"
	MethodAgentSessionsDelete = "agent_sessions.delete"
	// MethodAgentSessionHandoff reads a conversation — a pane's, or one by
	// id — for another agent to carry the task on (docs/MEMORY.md).
	MethodAgentSessionHandoff = "agent_sessions.handoff"
	// MethodGitHubIssues lists the issues of the GitHub repository a pane's
	// project is on, and MethodGitHubIssue reads one with its thread
	// (internal/github, through the gh of the server's machine).
	MethodGitHubIssues = "github.issues"
	MethodGitHubIssue  = "github.issue"
	// What is written to GitHub from the panel: a comment, an issue closed
	// or opened again, a new issue.
	MethodGitHubIssueComment = "github.issue.comment"
	MethodGitHubIssueState   = "github.issue.state"
	MethodGitHubIssueCreate  = "github.issue.create"
	// Pull requests: the list, one whole, what is done to one, and the
	// ones an issue has.
	MethodGitHubPRs      = "github.prs"
	MethodGitHubPR       = "github.pr"
	MethodGitHubPRAction = "github.pr.action"
	MethodGitHubIssuePRs = "github.issue.prs"
	// MethodGitHubPRChecks fills in a list's checks, after the list.
	MethodGitHubPRChecks = "github.prs.checks"
	// MethodGitHubRepoOptions is a repository's labels, or who can be
	// assigned, for the pickers; MethodGitHubIssueEdit changes an issue's
	// title, labels and assignees.
	MethodGitHubRepoOptions = "github.repo.options"
	MethodGitHubIssueEdit   = "github.issue.edit"
	// The context buffer (internal/capture): what tools captured — a page's
	// URL, an element picked in it, text, a file — kept by the server, which
	// is between every tool and every agent, until it is sent to one.
	MethodContextAdd    = "context.add"
	MethodContextList   = "context.list"
	MethodContextRemove = "context.remove"
	MethodContextClear  = "context.clear"
	MethodContextSend   = "context.send"
	// MethodBrowserOpen hands a page to the browsers attached to the server
	// (server/browser.go); none attached is an error, and the client opens
	// the page itself instead.
	MethodBrowserOpen = "browser.open"

	// The company methods keep herdr's user-defined Spaces (space.create,
	// space.rename, space.delete, space.assign, space.unassign), named for
	// what tend calls them. The companies themselves travel in the
	// snapshot.
	MethodCompanyCreate   = "company.create"
	MethodCompanyRename   = "company.rename"
	MethodCompanyDelete   = "company.delete"
	MethodCompanyAssign   = "company.assign"
	MethodCompanyUnassign = "company.unassign"
	// The group methods rename and delete a group of spaces. A group is
	// made by putting a space in it (workspace.group) and lasts, empty or
	// not, until group.delete; the groups travel in the snapshot.
	MethodGroupRename = "group.rename"
	MethodGroupDelete = "group.delete"
	// MethodGroupSetDir sets the folder a group's new spaces start in.
	MethodGroupSetDir = "group.set_dir"
	// The MCP methods are the MCP manager's (internal/mcp): the agents'
	// servers, read and written where the agents run, and tested there.
	// They are answered off the connection's request loop, since a test
	// can take seconds and keystrokes come down the same loop.
	MethodMCPList       = "mcp.list"
	MethodMCPTest       = "mcp.test"
	MethodMCPSetEnabled = "mcp.set_enabled"
	MethodMCPAdd        = "mcp.add"
	MethodMCPRemove     = "mcp.remove"
	// MethodDirList lists the folders in a folder on the server's machine,
	// for picking one with the mouse rather than typing its path: the
	// folders that matter are where the panes run, which over ssh is not
	// where the client is.
	MethodDirList = "dir.list"
	// MethodPaneFocus says which pane the client is looking at, so programs
	// that asked for focus events are told.
	MethodPaneFocus = "pane.focus"
	// MethodPaneRename gives a pane a name that its program cannot overwrite.
	MethodPaneRename = "pane.rename"
	// MethodPaneEditScrollback opens a pane's history in an editor, in a pane
	// of its own.
	MethodPaneEditScrollback = "pane.edit_scrollback"
	// MethodPaneGraphics fetches the images a pane holds and where they go.
	MethodPaneGraphics  = "pane.graphics"
	MethodPaneSwap      = "pane.swap"
	MethodTabMove       = "tab.move"
	MethodWorkspaceMove = "workspace.move"
	// MethodCommandRun runs one of the user's [[keys.command]] entries where
	// the panes are: in the background, in a pane that closes when it is
	// done, or as a plugin action.
	MethodCommandRun = "command.run"
	// MethodPaneDock opens a pane along the left edge of a pane's tab, the
	// tab's full height: the file explorer's place.
	MethodPaneDock = "pane.dock"
	// MethodPaneLinkActivate hands a URL clicked in a pane to the plugin that
	// claims it, if one does: herdr's pane link activation.
	MethodPaneLinkActivate = "pane.link_activate"
)

// Request is a call from a client.
type Request struct {
	// ID pairs a response with its request. Zero means the client wants no
	// response, which is how it sends fire-and-forget calls.
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers a Request. Exactly one of Error and Result is meaningful.
type Response struct {
	ID     uint64          `json:"id"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// --- handshake -------------------------------------------------------------

// HelloParams is what a client announces.
type HelloParams struct {
	Version int    `json:"version"`
	Client  string `json:"client,omitempty"`
}

// HelloResult is what the server answers.
//
// Methods lists what this server actually implements, so a client can disable
// an action it cannot perform rather than discovering the gap when a user
// tries it.
// KnownMethods is every method this build of the protocol has a name for.
//
// It exists so the two halves can work out which of them is behind. Builds are
// git descriptions with no ordering, so comparing them says only that they
// differ; what one side can do and the other has never heard of is a fact with
// a direction in it.
var KnownMethods = []string{
	MethodHello,
	MethodSessionSnapshot,
	MethodWorkspaceNew,
	MethodWorkspaceClose,
	MethodWorkspaceGroup,
	MethodWorkspaceRename,
	MethodTabNew,
	MethodTabClose,
	MethodTabRename,
	MethodTabLayout,
	MethodPaneSplit,
	MethodPaneClose,
	MethodPaneResize,
	MethodPaneSubscribe,
	MethodPaneScreen,
	MethodPaneText,
	MethodPaneAdjust,
	MethodServerShutdown,
	MethodServerHandoff,
	MethodPaneCopyMotion,
	MethodPaneCopySearch,
	MethodServerReloadConfig,
	MethodPaneFocus,
	MethodPaneRename,
	MethodPaneEditScrollback,
	MethodPaneGraphics,
	MethodPaneSwap,
	MethodTabMove,
	MethodWorkspaceMove,
	MethodCommandRun,
	MethodPaneDock,
	MethodPaneLinkActivate,
	MethodReleaseNotes,
	MethodReleaseNotesDismiss,
	MethodAgentsCatalog,
	MethodAgentSessions,
	MethodAgentSessionsDelete,
	MethodAgentSessionHandoff,
	MethodGitHubIssues,
	MethodGitHubIssue,
	MethodGitHubIssueComment,
	MethodGitHubIssueState,
	MethodGitHubIssueCreate,
	MethodGitHubPRs,
	MethodGitHubPR,
	MethodGitHubPRAction,
	MethodGitHubIssuePRs,
	MethodGitHubPRChecks,
	MethodGitHubRepoOptions,
	MethodGitHubIssueEdit,
	MethodContextAdd,
	MethodContextList,
	MethodContextRemove,
	MethodContextClear,
	MethodContextSend,
	MethodBrowserOpen,
	MethodCompanyCreate,
	MethodCompanyRename,
	MethodCompanyDelete,
	MethodCompanyAssign,
	MethodCompanyUnassign,
	MethodGroupRename,
	MethodGroupDelete,
	MethodGroupSetDir,
	MethodDirList,
	MethodMCPList,
	MethodMCPTest,
	MethodMCPSetEnabled,
	MethodMCPAdd,
	MethodMCPRemove,
}

// ErrUnknownMethod is what a server answers when it has never heard of a
// method. A client recognises it to tell "this cannot be done" apart from
// "the server on the other end is older than you are", which are the same
// failure to anyone reading a raw protocol error.
var ErrUnknownMethod = errors.New("unknown method")

type HelloResult struct {
	Version int    `json:"version"`
	Server  string `json:"server,omitempty"`
	// Build is the server binary's version. The protocol version says the two
	// sides can talk; this says whether they are the same age, which is what
	// a client needs to explain a method the server has never heard of.
	Build   string   `json:"build,omitempty"`
	Methods []string `json:"methods,omitempty"`
	// Features are things a server does that are not methods: events it
	// sends, fields it fills in. A change of that kind is invisible in the
	// method list, and it is how a client ended up forwarding the mouse to a
	// server that did not say which encoding the program wanted, and waiting
	// for a clipboard event that server had never heard of.
	Features []string `json:"features,omitempty"`
	// Handoff reports that this server can be replaced without ending its
	// programs. Knowing the method is not the same thing: a server run by
	// something other than `tend serve` has the method and no way to start a
	// replacement, and a client that offered "everything keeps running" on the
	// strength of the method alone would be promising what it cannot deliver.
	Handoff bool `json:"handoff,omitempty"`
}

// Features a server of this build provides.
const (
	// FeaturePaneClipboard: a pane's OSC 52 write is passed on as an event.
	FeaturePaneClipboard = "pane-clipboard"
	// FeatureMouseDetail: the snapshot says how much mouse a pane asked for
	// and in which encoding.
	FeatureMouseDetail = "mouse-detail"
	// FeatureSessionChanged: the server says when panes are swapped, and tabs
	// or spaces moved, renamed or regrouped.
	FeatureSessionChanged = "session-changed"
	// FeatureGraphics: the server keeps the images a pane's program sent and
	// hands them over, so a client can draw them on its own terminal.
	FeatureGraphics = "graphics"
	// FeatureLifecycle: the server reports what is focused and what was
	// created, which plugins hook on.
	FeatureLifecycle = "lifecycle"
	// FeatureWindowTitle: the snapshot says which machine the server is on
	// and carries any window title set over the API.
	FeatureWindowTitle = "window-title"
	// FeatureTabBarStatus: the snapshot carries the right end of the tab bar.
	FeatureTabBarStatus = "tab-bar-status"
	// FeatureDone: panes say whether an agent finished unseen.
	FeatureDone = "done"
	// FeatureWindowFocus: pane.focus also takes the window's focus.
	FeatureWindowFocus = "window-focus"
	// FeatureFocusRequest: the server asks clients to show a pane.
	FeatureFocusRequest = "focus-request"
	// FeatureAgentView: the snapshot carries agent.view.set's view.
	FeatureAgentView = "agent-view"
	// FeatureServerShell: a pane asked for with no command runs the server's
	// shell, so a client on another machine need not name one.
	FeatureServerShell = "server-shell"
	// FeaturePopup is a server that opens popups and says so in the snapshot.
	FeaturePopup = "popup"
	// FeatureContext: the server keeps the context buffer and says when it
	// changes with EventContextChanged.
	FeatureContext = "context"
	// FeatureDockUnless: pane.dock takes Unless and docks nothing when the
	// tab already has that pane.
	FeatureDockUnless = "dock-unless"
	// FeatureUpdate: the server checks for releases, says so with
	// EventUpdateReady, and the snapshot carries what it found.
	FeatureUpdate = "update"
	// FeatureContextArrived: browser.context takes several items and a
	// message at once, and the server says they came with
	// EventContextArrived, which opens the client's context panel.
	FeatureContextArrived = "context-arrived"
	// FeatureFollowCwd: a pane spec's DirOf is honoured, and a split with
	// no directory starts where the pane split is working.
	FeatureFollowCwd = "follow-cwd"
	// FeatureGitHubFolders: the GitHub lists of a folder that is no
	// repository are every repository under it (GitHubIssuesResult.Multi),
	// each issue and pull request saying its own.
	FeatureGitHubFolders = "github-folders"
	// FeatureGroups: the snapshot lists the groups of spaces
	// (SessionSnapshot.Groups), the empty ones too.
	FeatureGroups = "groups"
)

// KnownFeatures is every feature this build knows of, for the same reason
// KnownMethods exists.
var KnownFeatures = []string{
	FeaturePaneClipboard, FeatureMouseDetail, FeatureSessionChanged, FeatureGraphics,
	FeatureLifecycle, FeatureWindowTitle, FeatureTabBarStatus, FeatureDone, FeatureWindowFocus,
	FeatureFocusRequest, FeatureAgentView, FeatureServerShell, FeaturePopup, FeatureUpdate,
	FeatureDockUnless, FeatureContext, FeatureContextArrived, FeatureFollowCwd, FeatureGitHubFolders,
	FeatureGroups,
}

// --- session ---------------------------------------------------------------

// PaneInfo describes one pane.
type PaneInfo struct {
	ID    uint64 `json:"id"`
	Title string `json:"title,omitempty"`
	Agent string `json:"agent,omitempty"`
	State string `json:"state"`
	Rule  string `json:"rule,omitempty"`
	// Done marks an idle agent that finished while nobody was looking at its
	// tab (herdr's "done"), and StateSeq orders panes by when their state
	// last changed.
	Done     bool   `json:"done,omitempty"`
	StateSeq uint64 `json:"state_seq,omitempty"`

	Running bool `json:"running"`
	Pid     int  `json:"pid,omitempty"`
	// Mouse is the mouse reporting the pane's own program asked for, so a
	// client knows whether a click belongs to the application or to tend.
	Mouse bool `json:"mouse,omitempty"`
	// MouseDrag and MouseMotion say how much the program subscribed to, and
	// MouseSGR how it wants reports written.
	MouseDrag   bool `json:"mouse_drag,omitempty"`
	MouseMotion bool `json:"mouse_motion,omitempty"`
	MouseSGR    bool `json:"mouse_sgr,omitempty"`
	// Graphics changes whenever a pane's images or their placements do, so a
	// client can tell whether to ask for them again.
	Graphics uint64 `json:"graphics,omitempty"`
	// Named marks a title the user gave the pane, which its program's own
	// title does not replace.
	Named bool `json:"named,omitempty"`
	// Display is what to call the agent, and Tokens are the values a hook
	// asked to have shown beside it — the model, what is left of the
	// context. StateLabels rename a state for this pane.
	Display     string            `json:"display,omitempty"`
	Tokens      []AgentToken      `json:"tokens,omitempty"`
	StateLabels map[string]string `json:"state_labels,omitempty"`
	ExitErr     string            `json:"exit_error,omitempty"`

	Command []string `json:"command,omitempty"`
	Dir     string   `json:"dir,omitempty"`
}

// AgentToken is one value to show beside an agent.
type AgentToken struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TabInfo describes a tab and the panes in it.
type TabInfo struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name,omitempty"`
	Panes      []uint64 `json:"panes"`
	ActivePane uint64   `json:"active_pane"`
}

// WorkspaceInfo describes a workspace.
type WorkspaceInfo struct {
	ID   uint64 `json:"id"`
	Name string `json:"name,omitempty"`
	// Dir is what the workspace is about, and Branch the git branch checked
	// out there. Branch is computed by the server because only it can see the
	// directory: a client may be on another machine entirely.
	Dir    string `json:"dir,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Ahead and Behind are how far the checkout has drifted from the branch
	// it follows. Zero for both means in step, or nothing to be in step with.
	Ahead  int `json:"ahead,omitempty"`
	Behind int `json:"behind,omitempty"`
	// Group is what the workspace is kept with. Empty means it stands alone.
	Group string `json:"group,omitempty"`
	// Tokens are values a script reported about the space
	// (workspace.report_metadata), for the sidebar's $name tokens.
	Tokens    []AgentToken `json:"tokens,omitempty"`
	Tabs      []TabInfo    `json:"tabs"`
	ActiveTab uint64       `json:"active_tab,omitempty"`
}

// PaneTextParams asks for the text in a region of a pane.
//
// The rows are counted from the top of the view at Scroll and may fall outside
// it: a selection dragged past the edge covers text the client has scrolled
// away from, and the point of asking is that the server still has it.
//
// The region is cut where the cells are rather than by the caller, because a
// column is a property of the terminal: a wide character fills two of them and
// a combining mark none, so counting runes in a line of text gets a different
// answer than counting cells on a screen.
type PaneTextParams struct {
	Pane   uint64 `json:"pane"`
	Scroll int    `json:"scroll,omitempty"`
	// FromRow and ToRow are inclusive viewport rows; FromCol and ToCol are
	// the columns where the drag started and ended.
	FromRow int `json:"from_row"`
	FromCol int `json:"from_col"`
	ToRow   int `json:"to_row"`
	ToCol   int `json:"to_col"`
	// Block takes a rectangle instead of a run of text.
	Block bool `json:"block,omitempty"`
}

// CopyPoint is a cell by absolute row: row 0 is the oldest line the pane
// keeps. Copy mode addresses the whole history this way, because a viewport
// row means a different line every time the view moves.
type CopyPoint struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// PaneCopyMotionParams asks where a motion from a point lands.
type PaneCopyMotionParams struct {
	Pane   uint64    `json:"pane"`
	From   CopyPoint `json:"from"`
	Motion string    `json:"motion"`
}

// PaneCopySearchParams asks for the nearest match of a query.
type PaneCopySearchParams struct {
	Pane      uint64    `json:"pane"`
	From      CopyPoint `json:"from"`
	Query     string    `json:"query"`
	Direction string    `json:"direction"`
}

// PaneCopyResult is where the cursor goes, and how much history there is now,
// so the client can turn the absolute row into a view.
type PaneCopyResult struct {
	To      CopyPoint `json:"to"`
	End     CopyPoint `json:"end,omitempty"`
	Found   bool      `json:"found,omitempty"`
	Total   int       `json:"total,omitempty"`
	History int       `json:"history"`
	Rows    int       `json:"rows"`
}

// PaneTextResult carries what the region holds.
type PaneTextResult struct {
	Text string `json:"text"`
}

// SessionSnapshot is the whole session as a client sees it.
type SessionSnapshot struct {
	// Popup is the popup open over a tab, if any.
	Popup           *PopupInfo      `json:"popup,omitempty"`
	Workspaces      []WorkspaceInfo `json:"workspaces"`
	ActiveWorkspace uint64          `json:"active_workspace,omitempty"`
	Panes           []PaneInfo      `json:"panes"`
	// Hostname is the server's machine, which is where the panes are: the
	// client may be on another one, and {hostname} in a window title means
	// this one.
	Hostname string `json:"hostname,omitempty"`
	// WindowTitle is a title set over the API, which every client writes in
	// place of its own template until it is cleared.
	WindowTitle string `json:"window_title,omitempty"`
	// TabBarRight is what goes at the right end of the tab bar, and
	// TabBarSeparator what goes between two entries.
	TabBarRight []StatusSegment `json:"tab_bar_right,omitempty"`
	// AgentView is a filter and order a script set on the agent list.
	AgentView       *agentview.View `json:"agent_view,omitempty"`
	TabBarSeparator string          `json:"tab_bar_separator,omitempty"`
	// Update is what the server's release check knows, if anything.
	Update *UpdateInfo `json:"update,omitempty"`
	// Companies are the user's groupings of workspaces, in the order they
	// were made. Which one is being looked at is the client's.
	Companies []CompanyInfo `json:"companies,omitempty"`
	// Groups are the groups of spaces, in the order they were made, empty
	// ones included. A server without FeatureGroups sends none, and a group
	// is then only the name its spaces carry.
	Groups []GroupInfo `json:"groups,omitempty"`
}

// GroupInfo is one group of spaces, and the companies it has had spaces of:
// with a company chosen, a group is listed when one of its spaces is in it,
// or when it once was.
type GroupInfo struct {
	Name      string   `json:"name"`
	Companies []uint64 `json:"companies,omitempty"`
	// Dir is the folder a space made in the group starts in, if one is set.
	Dir string `json:"dir,omitempty"`
}

// MCPAgent is an agent whose MCP servers the manager reads.
type MCPAgent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Present bool   `json:"present,omitempty"`
	Error   string `json:"error,omitempty"`
}

// MCPEntry is one server in one agent, as it can be shown: env and header
// values are not in it, only their names, and arguments and URLs have
// whatever looks secret taken out.
type MCPEntry struct {
	Agent       string   `json:"agent"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Via         string   `json:"via,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Transport   string   `json:"transport"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	URL         string   `json:"url,omitempty"`
	EnvKeys     []string `json:"env_keys,omitempty"`
	HeaderKeys  []string `json:"header_keys,omitempty"`
	Extras      []string `json:"extras,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
}

// MCPListResult is mcp.list's answer.
type MCPListResult struct {
	Agents  []MCPAgent `json:"agents"`
	Entries []MCPEntry `json:"entries"`
}

// MCPRef names one server in one agent.
type MCPRef struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
}

// MCPTestResult is what testing a server found (mcp.Probe).
type MCPTestResult struct {
	Status   string `json:"status"`
	Millis   int64  `json:"millis"`
	Tools    int    `json:"tools"`
	Server   string `json:"server,omitempty"`
	Version  string `json:"version,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Error    string `json:"error,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// MCPSetParams turns a server in an agent on or off.
type MCPSetParams struct {
	Agent   string `json:"agent"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// MCPDef is a server typed in by the user, values and all.
type MCPDef struct {
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// MCPAddParams adds a server to agents: copied from one an agent has
// (From), so its secrets stay on the server's side, or as typed in (Def).
type MCPAddParams struct {
	Name   string   `json:"name"`
	Agents []string `json:"agents"`
	From   *MCPRef  `json:"from,omitempty"`
	Def    *MCPDef  `json:"def,omitempty"`
}

// MCPRemoveParams removes a server from agents.
type MCPRemoveParams struct {
	Name   string   `json:"name"`
	Agents []string `json:"agents"`
}

// MCPWriteResult is what a write did, agent by agent.
type MCPWriteResult struct {
	Done     []string          `json:"done,omitempty"`
	Failed   map[string]string `json:"failed,omitempty"`
	Warnings []string          `json:"warnings,omitempty"`
}

// DirListParams names the folder to list; empty is the home folder of the
// user the server runs as, and a leading ~ is that folder too.
type DirListParams struct {
	Path string `json:"path,omitempty"`
}

// DirListResult is a folder and the folders in it, by name, sorted. Parent
// is the folder above, empty at the root.
type DirListResult struct {
	Path   string   `json:"path"`
	Parent string   `json:"parent,omitempty"`
	Dirs   []string `json:"dirs"`
}

// GroupParams names a group, and for group.rename its new name, for
// group.set_dir its folder (empty clears it).
type GroupParams struct {
	Group string `json:"group"`
	Name  string `json:"name,omitempty"`
	Dir   string `json:"dir,omitempty"`
}

// CompanyInfo is one company and its workspaces, in its order.
type CompanyInfo struct {
	ID         uint64   `json:"id"`
	Name       string   `json:"name"`
	Workspaces []uint64 `json:"workspaces,omitempty"`
}

// CompanyParams names a company, and for create, rename, assign and
// unassign what to do with it: Name for the first two, Workspace for the
// others. Create takes a Workspace too, to put in the new company at once,
// as herdr's space.create takes assign_workspace_id.
type CompanyParams struct {
	Company   uint64 `json:"company,omitempty"`
	Name      string `json:"name,omitempty"`
	Workspace uint64 `json:"workspace,omitempty"`
}

// CompanyCreateResult is the company made.
type CompanyCreateResult struct {
	Company uint64 `json:"company"`
}

// UpdateInfo is herdr's update state as a client shows it.
type UpdateInfo struct {
	// Ready is the version of a release newer than the server's build, once
	// a check has found one: "update ready" until it is installed.
	Ready string `json:"ready,omitempty"`
	// Install is how to install it, as the notice says.
	Install string `json:"install,omitempty"`
	// Notes is the version whose release notes are kept, which is what the
	// menu's "what's new" opens; empty when there are none.
	Notes string `json:"notes,omitempty"`
}

// ReleaseNotes are the notes release_notes.get returns. Newer is whether
// they are a release newer than the server's build — an update ready —
// rather than what is new in the one running.
type ReleaseNotes struct {
	Version string `json:"version"`
	Body    string `json:"body"`
	Newer   bool   `json:"newer,omitempty"`
	Install string `json:"install,omitempty"`
}

// ContextItem is one thing in the context buffer. Kind is what it is —
// "url", "element" (a part of a page, picked in a browser), "text", "file" —
// and Source what captured it ("browser", "files", "cli", ...). The fields
// that do not apply to a kind are left empty.
type ContextItem struct {
	ID       uint64 `json:"id,omitempty"`
	Kind     string `json:"kind"`
	Source   string `json:"source,omitempty"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Tag      string `json:"tag,omitempty"`
	Text     string `json:"text,omitempty"`
	Path     string `json:"path,omitempty"`
	// Attributes are an element's, as the page has them.
	Attributes map[string]string `json:"attributes,omitempty"`
	// Note is what the user wrote about it: what is wrong with it, what
	// to do to it. It is the part an agent is asked to act on.
	Note string `json:"note,omitempty"`
	// Created is when it was captured, in seconds since the epoch.
	Created int64 `json:"created,omitempty"`
}

// ContextList is context.list's answer, oldest first.
type ContextList struct {
	Items []ContextItem `json:"items"`
}

// ContextIDs names items of the buffer; none is all of them.
type ContextIDs struct {
	IDs []uint64 `json:"ids,omitempty"`
}

// ContextSendParams sends items (none: all) to a pane, typed into it for
// the user to read over and send; nothing is submitted.
type ContextSendParams struct {
	Pane uint64   `json:"pane"`
	IDs  []uint64 `json:"ids,omitempty"`
}

// BrowserOpenParams is a page for browser.open, and BrowserOpenResult how
// many attached browsers took it.
type BrowserOpenParams struct {
	URL string `json:"url"`
}

type BrowserOpenResult struct {
	Browsers int `json:"browsers"`
}

// AgentStatus is one agent as agents.catalog finds it.
type AgentStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Installed   bool   `json:"installed"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	// InstallKind and InstallCommand are the method to offer (npm, script,
	// ...), empty when none can run here; Missing then names the program the
	// vendor's first method needs.
	InstallKind    string `json:"install_kind,omitempty"`
	InstallCommand string `json:"install_command,omitempty"`
	InstallSource  string `json:"install_source,omitempty"`
	Missing        string `json:"missing,omitempty"`
	// Kind is "memory" for a memory tool, listed apart from the agents;
	// Running a service of one answering on this machine; Next what to run
	// once it is installed.
	Kind    string `json:"kind,omitempty"`
	Running bool   `json:"running,omitempty"`
	Next    string `json:"next,omitempty"`
}

// AgentSessionInfo is one conversation as agent_sessions.list finds it.
type AgentSessionInfo struct {
	Agent string `json:"agent"`
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Dir   string `json:"dir,omitempty"`
	// Modified is when it was last written, in Unix seconds.
	Modified int64 `json:"modified"`
	Size     int64 `json:"size"`
	Prompts  int   `json:"prompts"`
	// Pane is the pane it is open in now, zero when none is: a
	// conversation open in a pane is not deleted.
	Pane uint64 `json:"pane,omitempty"`
	// Unreachable says why the directory it was held in cannot be started
	// in — gone, or not open to the user the server runs as — and is empty
	// when it can. An agent finds a conversation by that directory, so one
	// that cannot be entered cannot be resumed from here.
	Unreachable string `json:"unreachable,omitempty"`
}

// AgentSessionsResult is agent_sessions.list's answer, the last written
// first.
type AgentSessionsResult struct {
	Sessions []AgentSessionInfo `json:"sessions"`
}

// AgentHandoffParams names the conversation to hand on: the one a pane's
// agent is in, or one by id.
type AgentHandoffParams struct {
	Pane uint64 `json:"pane,omitempty"`
	ID   string `json:"id,omitempty"`
}

// AgentHandoff is what a conversation says about its task, and the state
// of the project it left: agent_sessions.handoff's answer.
type AgentHandoff struct {
	Agent  string   `json:"agent"`
	ID     string   `json:"id"`
	Title  string   `json:"title,omitempty"`
	Dir    string   `json:"dir,omitempty"`
	Path   string   `json:"path,omitempty"`
	First  string   `json:"first,omitempty"`
	Recent []string `json:"recent,omitempty"`
	Files  []string `json:"files,omitempty"`
	Last   string   `json:"last,omitempty"`
	// Branch is the branch checked out where it was held, and Status what
	// git says is uncommitted there, one line a file.
	Branch string `json:"branch,omitempty"`
	Status string `json:"status,omitempty"`
}

// AgentSessionsDeleteParams names the conversations to delete.
type AgentSessionsDeleteParams struct {
	IDs []string `json:"ids"`
}

// AgentSessionsDeleteResult says what was deleted, and why the rest were
// not.
type AgentSessionsDeleteResult struct {
	Deleted []string          `json:"deleted"`
	Kept    map[string]string `json:"kept,omitempty"`
}

// GitHubIssuesParams asks for a list: of the project Pane is working in,
// through Remote (empty for the default, upstream before origin), in one of
// the presets (github.Filters), with Query in GitHub's search syntax.
type GitHubIssuesParams struct {
	Pane   uint64 `json:"pane,omitempty"`
	Remote string `json:"remote,omitempty"`
	// Repo is one repository of a folder of several, owner/name; empty is
	// all of them.
	Repo   string `json:"repo,omitempty"`
	Filter int    `json:"filter,omitempty"`
	Query  string `json:"query,omitempty"`
}

// GitHubIssue is one issue in a list.
type GitHubIssue struct {
	// Repo is owner/name: in a folder of repositories, the issue's own.
	Repo      string   `json:"repo,omitempty"`
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Author    string   `json:"author,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
	Comments  int      `json:"comments"`
	// Updated is in Unix seconds.
	Updated int64  `json:"updated"`
	URL     string `json:"url"`
}

// GitHubIssuesResult is github.issues' answer: the repository listed, the
// remote it came through and the others there are, the directory it was
// found from, the issues, and how many the search found in all.
type GitHubIssuesResult struct {
	Repo    string   `json:"repo"`
	Remote  string   `json:"remote"`
	Remotes []string `json:"remotes"`
	Dir     string   `json:"dir"`
	// Multi is a folder of repositories, and Choices each of them; Repo
	// is then the one listed, or empty for all.
	Multi   bool               `json:"multi,omitempty"`
	Choices []GitHubRepoChoice `json:"choices,omitempty"`
	Issues  []GitHubIssue      `json:"issues"`
	Total   int                `json:"total"`
}

// GitHubIssueParams names an issue: owner/name and its number. What is
// written takes the rest: a comment's Body; the State an issue is put in,
// open or closed, and the Reason it is closed for; a new issue's Title and
// Body.
type GitHubIssueParams struct {
	Repo   string `json:"repo"`
	Number int    `json:"number,omitempty"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// GitHubRepoChoice is one repository of a folder of several: owner/name,
// what it is called, and the checkout it is in.
type GitHubRepoChoice struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// GitHubPR is one pull request in a list.
type GitHubPR struct {
	Repo   string   `json:"repo,omitempty"`
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	Draft  bool     `json:"draft,omitempty"`
	Author string   `json:"author,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Head   string   `json:"head"`
	Base   string   `json:"base"`
	Review string   `json:"review,omitempty"`
	// Pass, Fail and Pending count its checks.
	Pass    int    `json:"pass"`
	Fail    int    `json:"fail"`
	Pending int    `json:"pending"`
	Updated int64  `json:"updated"`
	URL     string `json:"url"`
}

// GitHubPRsResult is github.prs' answer, as github.issues' is.
type GitHubPRsResult struct {
	Repo    string             `json:"repo"`
	Remote  string             `json:"remote"`
	Remotes []string           `json:"remotes"`
	Dir     string             `json:"dir"`
	Multi   bool               `json:"multi,omitempty"`
	Choices []GitHubRepoChoice `json:"choices,omitempty"`
	PRs     []GitHubPR         `json:"prs"`
}

// GitHubPRChecks is github.prs.checks' answer: each pull request's checks,
// counted — pass, fail, pending — by owner/name#number.
type GitHubPRChecks struct {
	Checks map[string][3]int `json:"checks"`
}

// GitHubRepoOptionsParams asks for a repository's labels or assignees.
type GitHubRepoOptionsParams struct {
	Repo string `json:"repo"`
	// Kind is "labels" or "assignees".
	Kind string `json:"kind"`
}

// GitHubRepoOptions is github.repo.options' answer.
type GitHubRepoOptions struct {
	Names []string `json:"names"`
}

// GitHubIssueEditParams is what an edit changes: a new title, when not
// empty, and what is added and taken off.
type GitHubIssueEditParams struct {
	Repo            string   `json:"repo"`
	Number          int      `json:"number"`
	Title           string   `json:"title,omitempty"`
	AddLabels       []string `json:"add_labels,omitempty"`
	RemoveLabels    []string `json:"remove_labels,omitempty"`
	AddAssignees    []string `json:"add_assignees,omitempty"`
	RemoveAssignees []string `json:"remove_assignees,omitempty"`
}

// GitHubCheck is one check on a pull request.
type GitHubCheck struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// GitHubPRDetail is a pull request whole.
type GitHubPRDetail struct {
	GitHubPR
	Body      string          `json:"body"`
	Created   int64           `json:"created"`
	Additions int             `json:"additions"`
	Deletions int             `json:"deletions"`
	Files     int             `json:"files"`
	Mergeable string          `json:"mergeable"`
	Checks    []GitHubCheck   `json:"checks"`
	Thread    []GitHubComment `json:"thread"`
}

// GitHubPRActionParams is what is done to a pull request: comment (with
// Body), merge (by Method: squash, merge, rebase), close, reopen, ready.
type GitHubPRActionParams struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Action string `json:"action"`
	Body   string `json:"body,omitempty"`
	Method string `json:"method,omitempty"`
}

// GitHubIssuePRsParams asks for an issue's pull requests; Dir is the
// project, whose branches made for the issue are looked for.
type GitHubIssuePRsParams struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Dir    string `json:"dir,omitempty"`
}

// GitHubIssueCreated is github.issue.create's answer.
type GitHubIssueCreated struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// GitHubComment is one comment in an issue's thread.
type GitHubComment struct {
	Author  string `json:"author"`
	Body    string `json:"body"`
	Created int64  `json:"created"`
}

// GitHubIssueDetail is one issue whole.
type GitHubIssueDetail struct {
	GitHubIssue
	Body    string          `json:"body"`
	Created int64           `json:"created"`
	Thread  []GitHubComment `json:"thread"`
}

// AgentsCatalogResult is agents.catalog's answer.
type AgentsCatalogResult struct {
	Agents []AgentStatus `json:"agents"`
}

// ReleaseNotesDismissParams names the notes read, so a dismiss for notes
// that were replaced meanwhile is refused rather than applied to others.
type ReleaseNotesDismissParams struct {
	Version string `json:"version"`
}

// StatusSegment is one entry at the right of the tab bar. Zoom marks the
// entry that reads ZOOM while this client has a pane zoomed, which only the
// client knows.
type StatusSegment struct {
	Text string `json:"text,omitempty"`
	Zoom bool   `json:"zoom,omitempty"`
}

// --- pane operations -------------------------------------------------------

// PaneSpec describes a pane to start.
type PaneSpec struct {
	Command []string `json:"command"`
	Dir     string   `json:"dir,omitempty"`
	// DirOf, with no Dir, starts the pane where that pane is working now
	// (herdr's [terminal] new_cwd = "follow"): a new tab opened from an
	// agent's opens in its project. A server without FeatureFollowCwd
	// ignores it and starts the pane in its own directory.
	DirOf uint64   `json:"dir_of,omitempty"`
	Env   []string `json:"env,omitempty"`
	Agent string   `json:"agent,omitempty"`
	Title string   `json:"title,omitempty"`
	Cols  int      `json:"cols,omitempty"`
	Rows  int      `json:"rows,omitempty"`
}

// WorkspaceNewParams creates a workspace.
type WorkspaceNewParams struct {
	Name string `json:"name,omitempty"`
	Dir  string `json:"dir,omitempty"`
}

// WorkspaceCloseParams closes a workspace and every pane in it.
type WorkspaceCloseParams struct {
	Workspace uint64 `json:"workspace"`
}

// WorkspaceGroupParams moves a workspace into a group, or out of one when the
// group is empty.
type WorkspaceGroupParams struct {
	Workspace uint64 `json:"workspace"`
	Group     string `json:"group,omitempty"`
}

// WorkspaceNewResult reports the new workspace.
type WorkspaceNewResult struct {
	Workspace uint64 `json:"workspace"`
}

// TabNewParams creates a tab with one pane.
type TabNewParams struct {
	Workspace uint64   `json:"workspace"`
	Name      string   `json:"name,omitempty"`
	Pane      PaneSpec `json:"pane"`
}

// TabNewResult reports the new tab and its first pane.
type TabNewResult struct {
	Tab  uint64 `json:"tab"`
	Pane uint64 `json:"pane"`
}

// TabCloseParams closes a tab and every pane in it.
type TabCloseParams struct {
	Tab uint64 `json:"tab"`
}

// TabRenameParams renames a tab.
type TabRenameParams struct {
	Tab  uint64 `json:"tab"`
	Name string `json:"name"`
}

// WorkspaceRenameParams renames a workspace.
type WorkspaceRenameParams struct {
	Workspace uint64 `json:"workspace"`
	Name      string `json:"name"`
}

// PaneSplitParams divides a pane.
type PaneSplitParams struct {
	Target uint64 `json:"target"`
	// Direction is "columns" to place the new pane beside the target, or
	// "rows" to place it below. The names describe the arrangement rather than
	// the divider, which is the one naming choice in a multiplexer that
	// reliably confuses people.
	Direction string   `json:"direction"`
	Pane      PaneSpec `json:"pane"`
}

// PaneDockParams docks a new pane beside everything in Beside's tab. Share
// is its fraction of the tab's width. A pane with no directory given starts
// in the one Beside is in now. A docked pane closes when its program ends,
// and keeps the title it is given.
type PaneDockParams struct {
	Beside uint64  `json:"beside"`
	Share  float64 `json:"share"`
	// Right docks on the right edge. A server from before it docks on the
	// left, which is a panel on the wrong side rather than no panel.
	Right bool     `json:"right,omitempty"`
	Pane  PaneSpec `json:"pane"`
	// Unless names a pane the tab may already have, in which case nothing is
	// docked and that pane is the answer, with Existing set. A server from
	// before it (FeatureDockUnless) docks regardless.
	Unless string `json:"unless,omitempty"`
}

// PopupInfo is a popup: its pane, the tab it floats over, and its size as
// herdr gives it — cells, or a percentage of the area, empty for half.
type PopupInfo struct {
	Pane   uint64 `json:"pane"`
	Tab    uint64 `json:"tab"`
	Width  string `json:"width,omitempty"`
	Height string `json:"height,omitempty"`
	Title  string `json:"title,omitempty"`
}

// PaneLinkParams is a URL clicked in a pane.
type PaneLinkParams struct {
	Pane uint64 `json:"pane"`
	URL  string `json:"url"`
}

// PaneLinkResult says whether a plugin took the link.
type PaneLinkResult struct {
	Handled bool `json:"handled"`
}

// PaneSplitResult reports the new pane.
type PaneSplitResult struct {
	Pane uint64 `json:"pane"`
	// Existing is set when pane.dock's Unless found the pane already there.
	Existing bool `json:"existing,omitempty"`
}

// PaneCloseParams closes one pane.
type PaneCloseParams struct {
	Pane uint64 `json:"pane"`
}

// PaneResizeParams changes a pane's terminal size.
type PaneResizeParams struct {
	Pane uint64 `json:"pane"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// PaneRenameParams names a pane.
type PaneRenameParams struct {
	Pane uint64 `json:"pane"`
	Name string `json:"name"`
}

// PaneFocusParams says which pane has the focus now and which had it.
type PaneFocusParams struct {
	Pane uint64 `json:"pane"`
	Lost uint64 `json:"lost,omitempty"`
	// Window, when set, is instead the terminal window gaining or losing
	// focus, which the server cannot see for itself.
	Window *bool `json:"window,omitempty"`
}

// GraphicsImage is one image a pane holds, as bytes to pass on.
type GraphicsImage struct {
	ID     uint32 `json:"id"`
	Format uint8  `json:"format"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	// Data is the image exactly as the program sent it, base64 in JSON.
	Data []byte `json:"data"`
}

// GraphicsPlacement is where an image goes, in the pane's own rows counted
// from the oldest line it keeps.
type GraphicsPlacement struct {
	ImageID uint32 `json:"image"`
	ID      uint32 `json:"id,omitempty"`
	Row     int    `json:"row"`
	Col     int    `json:"col"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Z       int    `json:"z,omitempty"`
}

// PaneGraphicsResult is a pane's images and placements.
type PaneGraphicsResult struct {
	Revision   uint64              `json:"revision"`
	History    int                 `json:"history"`
	Images     []GraphicsImage     `json:"images,omitempty"`
	Placements []GraphicsPlacement `json:"placements,omitempty"`
}

// ReloadResult says what a reload changed, or what was wrong with the file.
type ReloadResult struct {
	Path    string   `json:"path"`
	Changed []string `json:"changed,omitempty"`
	Err     string   `json:"error,omitempty"`
}

// HandoffParams names the binary to replace the server with. Empty means the
// server's own path; a tend installed somewhere else — a remote attach puts
// it in ~/.local/bin — names itself, or the server would start the old
// binary again.
type HandoffParams struct {
	Binary string `json:"binary,omitempty"`
}

// CommandRunParams is a user's command and the pane it was run from, whose
// directory it runs in.
type CommandRunParams struct {
	Pane    uint64 `json:"pane,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command"`
	// Width and Height size a popup: cells, or a percentage like "80%".
	Width  string `json:"width,omitempty"`
	Height string `json:"height,omitempty"`
}

// CommandRunResult names the pane a command opened, when it opened one.
type CommandRunResult struct {
	Pane uint64 `json:"pane,omitempty"`
}

// PaneSwapParams exchanges a pane with another, named or found on a side of it
// as laid out at Cols by Rows.
type PaneSwapParams struct {
	Pane   uint64 `json:"pane"`
	Target uint64 `json:"target,omitempty"`
	Side   string `json:"side,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

// PaneSwapResult names the pane it traded places with.
type PaneSwapResult struct {
	Other uint64 `json:"other"`
}

// MoveParams puts a tab or a space at an index in its row. Delta, when set,
// moves it that many places instead, which is what a key binding asks for.
type MoveParams struct {
	ID    uint64 `json:"id"`
	Index int    `json:"index"`
	Delta int    `json:"delta,omitempty"`
}

// PaneAdjustParams moves one edge of a pane, taking the space from the
// neighbour across it.
//
// Cells and the area are both given because a divider is stored as a
// proportion of its split while the user is asking in cells, and only the
// client knows how many cells the tab is being drawn in.
type PaneAdjustParams struct {
	Pane string `json:"-"`

	Target uint64 `json:"target"`
	// Side is "left", "right", "up" or "down": the edge of the pane to move.
	Side string `json:"side"`
	// Cells is how far to move it. Negative shrinks.
	Cells int `json:"cells"`
	Cols  int `json:"cols"`
	Rows  int `json:"rows"`
}

// PaneSubscribeParams chooses which panes stream their output to this client.
//
// Panes replaces the current set rather than adding to it, so a client that
// switches tabs sends one message instead of unsubscribing and resubscribing
// and briefly receiving both or neither.
type PaneSubscribeParams struct {
	Panes []uint64 `json:"panes"`
}

// PaneScreenParams asks for a pane's screen.
type PaneScreenParams struct {
	Pane uint64 `json:"pane"`
	// Offset is how many lines back through the scrollback to look. Zero is
	// the live screen.
	Offset int `json:"offset,omitempty"`
}

// PaneScreenResult carries a pane's screen.
//
// Text is what a human or a script reads; ANSI is what a terminal draws. Both
// are sent because they answer different questions, and a client that only
// wants to list panes should not have to parse escape sequences to do it.
type PaneScreenResult struct {
	Text  string `json:"text"`
	ANSI  string `json:"ansi,omitempty"`
	Title string `json:"title,omitempty"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	// History is how many lines are behind the top of the screen, so a client
	// knows how far back it can scroll without asking and being refused.
	History int `json:"history,omitempty"`
	// Offset is how far back this view actually is, which may be less than
	// was asked for.
	Offset int `json:"offset,omitempty"`
}

// TabLayoutParams asks where a tab's panes go at a given size.
//
// The size is the client's, not the server's: the layout tree stores
// proportions rather than cells, so two clients of different sizes get
// different rectangles from the same tab without either being wrong.
type TabLayoutParams struct {
	Tab  uint64 `json:"tab"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// PaneRect is where one pane sits, in cells.
type PaneRect struct {
	Pane uint64 `json:"pane"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// TabLayoutResult is the arrangement of a tab's panes.
type TabLayoutResult struct {
	Panes []PaneRect `json:"panes"`
}

// --- events ----------------------------------------------------------------

// Event kind names are permanent. A client that does not know one must ignore
// it rather than treat it as an error, so a newer server can report more
// without breaking an older client.
const (
	EventPaneOpened = "pane-opened"
	EventPaneState  = "pane-state"
	EventPaneExited = "pane-exited"
	EventPaneClosed = "pane-closed"
	// EventPaneClipboard carries text a pane's program asked to have copied.
	EventPaneClipboard = "pane-clipboard"
	// EventSessionChanged says the session's shape changed with no pane
	// opened or closed. It carries nothing: the client reads the session.
	EventSessionChanged = "session-changed"
	// EventNotify carries something to tell the user: a title and a body.
	EventNotify = "notify"
	// The events about what is focused and what was created. A client sends
	// focus and every client hears it, which is how a second one follows
	// along; a plugin hears them under herdr's names.
	EventPaneFocused      = "pane-focused"
	EventTabFocused       = "tab-focused"
	EventWorkspaceFocused = "workspace-focused"
	EventTabCreated       = "tab-created"
	EventWorkspaceCreated = "workspace-created"
	// EventFocusRequest asks a client to show a pane, for pane.focus and
	// tab.focus over the automation socket.
	EventFocusRequest = "focus-request"
	// EventUpdateReady says the server's check found a newer release:
	// Title is its version, Body how to install it.
	EventUpdateReady = "update-ready"
	// EventContextChanged says the context buffer changed. It carries
	// nothing: a client that shows the buffer reads it again.
	EventContextChanged = "context-changed"
	// EventContextArrived says a tool handed the context something to look
	// at: Title is which ("browser"), Body how many items. A client shows
	// the context panel for it.
	EventContextArrived = "context-arrived"
)

// Event is something that happened, sent unsolicited.
type Event struct {
	Kind string `json:"kind"`
	Pane uint64 `json:"pane,omitempty"`

	State string `json:"state,omitempty"`
	Rule  string `json:"rule,omitempty"`
	// Agent is who a state event is about.
	Agent string `json:"agent,omitempty"`
	Err   string `json:"error,omitempty"`
	// Data is the text of a clipboard event. JSON carries it as base64.
	Data []byte `json:"data,omitempty"`
	// Title and Body are set on a notify event.
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	// Tab and Workspace are set on the events about them.
	Tab       uint64 `json:"tab,omitempty"`
	Workspace uint64 `json:"workspace,omitempty"`
}

// Compare reports which side of a connection knows things the other does not,
// given the methods a server advertises.
//
// Both can be true at once: two builds that each added something the other
// lacks have diverged rather than one being behind, and saying "you are old"
// to either of them would be wrong.
func Compare(advertised []string) (serverAhead, clientAhead bool) {
	return compare(advertised, KnownMethods, MethodHello)
}

// CompareFeatures is Compare for the things that are not methods.
func CompareFeatures(advertised []string) (serverAhead, clientAhead bool) {
	return compare(advertised, KnownFeatures, "")
}

func compare(advertised, mine []string, skip string) (serverAhead, clientAhead bool) {
	has := make(map[string]bool, len(advertised))
	for _, m := range advertised {
		has[m] = true
	}
	known := make(map[string]bool, len(mine))
	for _, m := range mine {
		known[m] = true
	}

	for _, m := range advertised {
		if !known[m] {
			serverAhead = true
		}
	}
	for _, m := range mine {
		if m != skip && !has[m] {
			clientAhead = true
		}
	}
	return serverAhead, clientAhead
}
