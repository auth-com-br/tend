package api

import "github.com/auth-com-br/tend/internal/session"

// The parameters of each method, as named types: the handlers decode into
// them and the published schema (tend api schema) is read off them, so
// what a method takes and what the schema says it takes are one thing.

// PaneGetParams is what MethodPaneGet takes.
type PaneGetParams struct {
	PaneID string `json:"pane_id" schema:"required"`
}

// PaneReportAgentParams is what MethodPaneReportAgent takes.
type PaneReportAgentParams struct {
	PaneID           string  `json:"pane_id" schema:"required"`
	Source           string  `json:"source"`
	Agent            string  `json:"agent"`
	State            string  `json:"state"`
	Message          string  `json:"message"`
	Seq              *uint64 `json:"seq"`
	AgentSessionID   string  `json:"agent_session_id"`
	AgentSessionPath string  `json:"agent_session_path"`
}

// PaneReportAgentSessionParams is what MethodPaneReportAgentSession takes.
type PaneReportAgentSessionParams struct {
	PaneID           string  `json:"pane_id" schema:"required"`
	Source           string  `json:"source"`
	Agent            string  `json:"agent"`
	Seq              *uint64 `json:"seq"`
	AgentSessionID   string  `json:"agent_session_id"`
	AgentSessionPath string  `json:"agent_session_path"`
}

// PaneReleaseAgentParams is what MethodPaneReleaseAgent takes.
type PaneReleaseAgentParams struct {
	PaneID string  `json:"pane_id" schema:"required"`
	Source string  `json:"source"`
	Agent  string  `json:"agent"`
	Seq    *uint64 `json:"seq"`
}

// PaneReportMetadataParams is what MethodPaneReportMetadata takes.
type PaneReportMetadataParams struct {
	PaneID            string             `json:"pane_id" schema:"required"`
	Source            string             `json:"source"`
	Agent             string             `json:"agent"`
	Title             string             `json:"title"`
	DisplayAgent      string             `json:"display_agent"`
	StateLabels       map[string]string  `json:"state_labels"`
	Tokens            map[string]*string `json:"tokens"`
	TTLMs             uint64             `json:"ttl_ms"`
	ClearTitle        bool               `json:"clear_title"`
	ClearDisplayAgent bool               `json:"clear_display_agent"`
	ClearStateLabels  bool               `json:"clear_state_labels"`
	Seq               *uint64            `json:"seq"`
}

// PaneClearAgentAuthorityParams is what MethodPaneClearAgentAuthority takes.
type PaneClearAgentAuthorityParams struct {
	PaneID string  `json:"pane_id" schema:"required"`
	Source string  `json:"source"`
	Seq    *uint64 `json:"seq"`
}

// IntegrationInstallParams is what MethodIntegrationInstall takes.
type IntegrationInstallParams struct {
	Target string `json:"target" schema:"required"`
}

// IntegrationUninstallParams is what MethodIntegrationUninstall takes.
type IntegrationUninstallParams struct {
	Target string `json:"target" schema:"required"`
}

// WorkspaceGetParams is what MethodWorkspaceGet takes.
type WorkspaceGetParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
}

// WorkspaceCreateParams is what MethodWorkspaceCreate takes.
type WorkspaceCreateParams struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// WorkspaceRenameParams is what MethodWorkspaceRename takes.
type WorkspaceRenameParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
	Name        string `json:"name" schema:"required"`
}

// WorkspaceCloseParams is what MethodWorkspaceClose takes.
type WorkspaceCloseParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
}

// TabListParams is what MethodTabList takes.
type TabListParams struct {
	WorkspaceID string `json:"workspace_id"`
}

// TabGetParams is what MethodTabGet takes.
type TabGetParams struct {
	TabID string `json:"tab_id" schema:"required"`
}

// TabCreateParams is what MethodTabCreate takes.
type TabCreateParams struct {
	WorkspaceID string   `json:"workspace_id" schema:"required"`
	Name        string   `json:"name"`
	Command     []string `json:"command"`
	Dir         string   `json:"dir"`
	Agent       string   `json:"agent"`
	// CloseOnExit is tend's: the tab goes with its program, for one
	// opened to run a single thing (the file explorer's editor).
	CloseOnExit bool `json:"close_on_exit"`
}

// TabRenameParams is what MethodTabRename takes.
type TabRenameParams struct {
	TabID string `json:"tab_id" schema:"required"`
	Name  string `json:"name" schema:"required"`
}

// TabCloseParams is what MethodTabClose takes.
type TabCloseParams struct {
	TabID string `json:"tab_id" schema:"required"`
}

// PaneReadParams is what MethodPaneRead takes.
type PaneReadParams struct {
	PaneID string `json:"pane_id" schema:"required"`
	Source string `json:"source"`
	Lines  int    `json:"lines"`
}

// PaneSendTextParams is what MethodPaneSendText takes.
type PaneSendTextParams struct {
	PaneID string `json:"pane_id" schema:"required"`
	Text   string `json:"text" schema:"required"`
	Submit bool   `json:"submit"`
}

// PaneSendKeysParams is what MethodPaneSendKeys takes.
type PaneSendKeysParams struct {
	PaneID string   `json:"pane_id" schema:"required"`
	Keys   []string `json:"keys" schema:"required"`
}

// WorkspaceMoveBlockParams is what MethodWorkspaceMoveBlock takes.
type WorkspaceMoveBlockParams struct {
	WorkspaceIDs      []string `json:"workspace_ids"`
	BeforeWorkspaceID string   `json:"before_workspace_id"`
}

// WorkspaceReportMetadataParams is what MethodWorkspaceReportMetadata takes.
type WorkspaceReportMetadataParams struct {
	WorkspaceID string             `json:"workspace_id" schema:"required"`
	Source      string             `json:"source"`
	Tokens      map[string]*string `json:"tokens"`
	Seq         *uint64            `json:"seq"`
	TTLMs       uint64             `json:"ttl_ms"`
}

// PaneSendInputParams is what MethodPaneSendInput takes.
type PaneSendInputParams struct {
	PaneID string   `json:"pane_id" schema:"required"`
	Text   string   `json:"text"`
	Keys   []string `json:"keys"`
}

// PaneRenameParams is what MethodPaneRename takes.
type PaneRenameParams struct {
	PaneID string  `json:"pane_id" schema:"required"`
	Label  *string `json:"label"`
}

// FocusParams is what MethodPaneFocus, MethodTabFocus take.
type FocusParams struct {
	PaneID string `json:"pane_id"`
	TabID  string `json:"tab_id"`
}

// PaneLayoutParams is what MethodPaneLayout takes.
type PaneLayoutParams struct {
	PaneID string `json:"pane_id" schema:"required"`
}

// PaneSplitParams is what MethodPaneSplit takes.
type PaneSplitParams struct {
	PaneID    string   `json:"pane_id" schema:"required"`
	Direction string   `json:"direction"`
	Command   []string `json:"command"`
	Dir       string   `json:"dir"`
	Agent     string   `json:"agent"`
	// CloseOnExit is tend's, as on tab.create: the files panel's
	// preview goes when it is closed.
	CloseOnExit bool `json:"close_on_exit"`
}

// PaneRespawnParams is what MethodPaneRespawn takes: what to run in the
// pane instead of what it runs now.
type PaneRespawnParams struct {
	PaneID      string   `json:"pane_id" schema:"required"`
	Command     []string `json:"command" schema:"required"`
	Dir         string   `json:"dir"`
	Agent       string   `json:"agent"`
	CloseOnExit bool     `json:"close_on_exit"`
}

// PaneScrollParams is what MethodPaneScroll takes: how far above the
// present to show the pane, herdr's offset_from_bottom.
type PaneScrollParams struct {
	PaneID           string `json:"pane_id" schema:"required"`
	OffsetFromBottom int    `json:"offset_from_bottom"`
}

// PaneCurrentParams is what MethodPaneCurrent takes.
type PaneCurrentParams struct {
	CallerPaneID string `json:"caller_pane_id"`
}

// PaneCloseParams is what MethodPaneClose takes.
type PaneCloseParams struct {
	PaneID string `json:"pane_id" schema:"required"`
}

// PaneResizeParams is what MethodPaneResize takes.
type PaneResizeParams struct {
	PaneID string `json:"pane_id" schema:"required"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

// PaneWaitForOutputParams is what MethodPaneWaitForOutput takes.
type PaneWaitForOutputParams struct {
	PaneID    string `json:"pane_id" schema:"required"`
	Contains  string `json:"contains"`
	TimeoutMs uint64 `json:"timeout_ms"`
}

// AgentViewClearParams is what MethodAgentViewClear takes.
type AgentViewClearParams struct {
	Source string `json:"source"`
}

// AgentRenameParams is what MethodAgentRename takes.
type AgentRenameParams struct {
	Target string  `json:"target" schema:"required"`
	Name   *string `json:"name"`
}

// AgentStartParams is what MethodAgentStart takes.
type AgentStartParams struct {
	PaneID  string   `json:"pane_id"`
	Agent   string   `json:"agent"`
	Command []string `json:"command"`
	// Name is what to call the agent, as herdr's agent.start takes.
	Name string `json:"name"`
}

// EventsSubscribeParams is what MethodEventsSubscribe takes.
type EventsSubscribeParams struct {
	Kinds  []string `json:"kinds"`
	PaneID string   `json:"pane_id"`
}

// LayoutExportParams is what MethodLayoutExport takes.
type LayoutExportParams struct {
	TabID string `json:"tab_id" schema:"required"`
}

// LayoutApplyParams is what MethodLayoutApply takes.
type LayoutApplyParams struct {
	WorkspaceID string                 `json:"workspace_id" schema:"required"`
	Name        string                 `json:"name"`
	Tree        session.LayoutSnapshot `json:"tree"`
	Panes       []session.PaneSnapshot `json:"panes"`
	Env         map[string]string      `json:"-"`
}

// NotificationShowParams is what MethodNotificationShow takes.
type NotificationShowParams struct {
	Title string `json:"title" schema:"required"`
	Body  string `json:"body"`
}

// WindowTitleParams is what MethodWindowTitleSet, MethodWindowTitleClear take.
type WindowTitleParams struct {
	Title string `json:"title"`
}

// EventsWaitParams is what MethodEventsWait takes.
type EventsWaitParams struct {
	Kinds     []string `json:"kinds"`
	PaneID    string   `json:"pane_id"`
	TimeoutMs uint64   `json:"timeout_ms"`
}

// PaneNeighborParams is what MethodPaneNeighbor takes.
type PaneNeighborParams struct {
	PaneID    string `json:"pane_id" schema:"required"`
	Direction string `json:"direction"`
}

// PaneEdgesParams is what MethodPaneEdges takes.
type PaneEdgesParams struct {
	PaneID string `json:"pane_id" schema:"required"`
}

// PaneProcessesParams is what MethodPaneProcesses takes.
type PaneProcessesParams struct {
	PaneID string `json:"pane_id" schema:"required"`
}

// PaneMoveParams is what MethodPaneMove takes.
type PaneMoveParams struct {
	PaneID       string `json:"pane_id" schema:"required"`
	TargetPaneID string `json:"target_pane_id"`
	Direction    string `json:"direction"`
}

// PaneSwapParams is what MethodPaneSwap takes.
type PaneSwapParams struct {
	PaneID       string `json:"pane_id" schema:"required"`
	TargetPaneID string `json:"target_pane_id"`
	Direction    string `json:"direction"`
}

// MoveParams is what MethodTabMove, MethodWorkspaceMove take.
type MoveParams struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Index       *int   `json:"index"`
	Delta       int    `json:"delta"`
}

// ServerHandoffParams is what MethodServerHandoff takes.
type ServerHandoffParams struct {
	Binary string `json:"binary"`
}

// AgentParams is what the agent methods take.
type AgentParams struct {
	Target    string   `json:"target"`
	PaneID    string   `json:"pane_id"`
	Text      string   `json:"text"`
	Keys      []string `json:"keys"`
	Lines     int      `json:"lines"`
	Source    string   `json:"source"`
	Until     []string `json:"until"`
	TimeoutMs uint64   `json:"timeout_ms"`
	Wait      bool     `json:"wait"`
	// Screen asks for the text detection read, which is what an
	// explanation is checked against.
	Screen bool `json:"screen"`
}

// PluginLinkParams is what MethodPluginLink takes.
type PluginLinkParams struct {
	Path string `json:"path" schema:"required"`
}

// PluginIDParams is what MethodPluginUnlink, MethodPluginEnable, MethodPluginDisable, MethodPluginReload take.
type PluginIDParams struct {
	PluginID string `json:"plugin_id"`
}

// PluginLogListParams is what MethodPluginLogList takes.
type PluginLogListParams struct {
	PluginID string `json:"plugin_id"`
	Limit    int    `json:"limit"`
}

// PluginActionInvokeParams is what MethodPluginActionInvoke takes.
type PluginActionInvokeParams struct {
	ActionID string `json:"action_id"`
	PaneID   string `json:"pane_id"`
}

// PluginPaneOpenParams is what MethodPluginPaneOpen takes.
type PluginPaneOpenParams struct {
	Pane   string `json:"pane"`
	Target string `json:"pane_id"`
}

// WorktreeListParams is what MethodWorktreeList takes.
type WorktreeListParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
	Cwd         string `json:"cwd"`
	// Trust names the repository safe for this call, herdr's
	// trust_repository, for one owned by another user.
	Trust bool `json:"trust_repository"`
}

// WorktreeCreateParams is what MethodWorktreeCreate takes.
type WorktreeCreateParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
	Cwd         string `json:"cwd"`
	// Trust names the repository safe for this call, herdr's
	// trust_repository, for one owned by another user.
	Trust  bool   `json:"trust_repository"`
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Path   string `json:"path"`
	Label  string `json:"label"`
}

// WorktreeOpenParams is what MethodWorktreeOpen takes.
type WorktreeOpenParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
	Cwd         string `json:"cwd"`
	// Trust names the repository safe for this call, herdr's
	// trust_repository, for one owned by another user.
	Trust  bool   `json:"trust_repository"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Label  string `json:"label"`
}

// WorktreeRemoveParams is what MethodWorktreeRemove takes.
type WorktreeRemoveParams struct {
	WorkspaceID string `json:"workspace_id" schema:"required"`
	Force       bool   `json:"force"`
	Trust       bool   `json:"trust_repository"`
}

// ContextAddParams is what MethodContextAdd takes: one captured item. Kind
// is url, element, text or file; the fields for it are required by kind.
type ContextAddParams struct {
	Note       string            `json:"note"`
	Kind       string            `json:"kind" schema:"required"`
	Source     string            `json:"source"`
	Title      string            `json:"title"`
	URL        string            `json:"url"`
	Selector   string            `json:"selector"`
	Tag        string            `json:"tag"`
	Text       string            `json:"text"`
	Path       string            `json:"path"`
	Attributes map[string]string `json:"attributes"`
}

// ContextIDsParams names items of the buffer, for MethodContextRemove.
type ContextIDsParams struct {
	IDs []uint64 `json:"ids"`
}

// ContextSendParams sends items (none: all) to a pane, typed in and not
// submitted.
type ContextSendParams struct {
	PaneID string   `json:"pane_id" schema:"required"`
	IDs    []uint64 `json:"ids"`
}

// BrowserAttachParams attaches a browser; Name says which, for status.
type BrowserAttachParams struct {
	Name string `json:"name"`
}

// BrowserURLParams is a page to open or go to.
type BrowserURLParams struct {
	URL string `json:"url" schema:"required"`
}

// BrowserSelectParams turns element picking on or off.
type BrowserSelectParams struct {
	On bool `json:"on"`
}

// BrowserCaptureParams is what a browser picked: an element by default,
// with the page it is on; PaneID, for send_to_agent, is where it goes (none:
// the pane the user was last in).
type BrowserCaptureParams struct {
	// Items are several at once, for send_to_agent: sent together, as one
	// message. With Items, the fields below are not read.
	Items []BrowserCaptureParams `json:"items"`
	// Message leads what send_to_agent types: the user's words for the
	// items together.
	Message    string            `json:"message"`
	Note       string            `json:"note"`
	Kind       string            `json:"kind"`
	Title      string            `json:"title"`
	URL        string            `json:"url"`
	Selector   string            `json:"selector"`
	Tag        string            `json:"tag"`
	Text       string            `json:"text"`
	Attributes map[string]string `json:"attributes"`
	PaneID     string            `json:"pane_id"`
}
