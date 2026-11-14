package api

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/agentview"
	"github.com/auth-com-br/tend/internal/server"
)

// The published schema is herdr's `herdr api schema`: a JSON Schema of what
// every method takes, for a plugin or a script to check itself against
// rather than read the source. herdr generates it from its types with
// schemars; tend reads it off the same named parameter types the handlers
// decode into (params.go), by reflection, so the schema cannot say one
// thing while a handler does another.

// SchemaVersion changes when the schema's own shape does.
const SchemaVersion = 1

// EmptyParams is what a method that takes nothing takes.
type EmptyParams struct{}

// methodParams is every method the socket answers, and what it takes.
var methodParams = map[string]any{
	MethodAgentExplain:            AgentParams{},
	MethodAgentGet:                AgentParams{},
	MethodAgentList:               EmptyParams{},
	MethodAgentPrompt:             AgentParams{},
	MethodAgentRead:               AgentParams{},
	MethodAgentRename:             AgentRenameParams{},
	MethodAgentSendKeys:           AgentParams{},
	MethodAgentStart:              AgentStartParams{},
	MethodAgentViewClear:          AgentViewClearParams{},
	MethodAgentViewSet:            agentview.View{},
	MethodAgentWait:               AgentParams{},
	MethodEventsSubscribe:         EventsSubscribeParams{},
	MethodEventsWait:              EventsWaitParams{},
	MethodIntegrationInstall:      IntegrationInstallParams{},
	MethodIntegrationList:         EmptyParams{},
	MethodIntegrationUninstall:    IntegrationUninstallParams{},
	MethodLayoutApply:             LayoutApplyParams{},
	MethodLayoutExport:            LayoutExportParams{},
	MethodNotificationShow:        NotificationShowParams{},
	MethodPaneClearAgentAuthority: PaneClearAgentAuthorityParams{},
	MethodPaneClose:               PaneCloseParams{},
	MethodPaneEdges:               PaneEdgesParams{},
	MethodPaneFocus:               FocusParams{},
	MethodPaneGet:                 PaneGetParams{},
	MethodPaneLayout:              PaneLayoutParams{},
	MethodPaneList:                EmptyParams{},
	MethodPaneMove:                PaneMoveParams{},
	MethodPaneNeighbor:            PaneNeighborParams{},
	MethodPaneProcesses:           PaneProcessesParams{},
	MethodPaneRead:                PaneReadParams{},
	MethodPaneReleaseAgent:        PaneReleaseAgentParams{},
	MethodPaneRename:              PaneRenameParams{},
	MethodPaneReportAgent:         PaneReportAgentParams{},
	MethodPaneReportAgentSession:  PaneReportAgentSessionParams{},
	MethodPaneReportMetadata:      PaneReportMetadataParams{},
	MethodPaneResize:              PaneResizeParams{},
	MethodPaneRespawn:             PaneRespawnParams{},
	MethodPaneSendInput:           PaneSendInputParams{},
	MethodPaneSendKeys:            PaneSendKeysParams{},
	MethodPaneSendText:            PaneSendTextParams{},
	MethodPaneSplit:               PaneSplitParams{},
	MethodPaneSwap:                PaneSwapParams{},
	MethodPaneWaitForOutput:       PaneWaitForOutputParams{},
	MethodPopupClose:              EmptyParams{},
	MethodPing:                    EmptyParams{},
	MethodPluginActionInvoke:      PluginActionInvokeParams{},
	MethodPluginActionList:        EmptyParams{},
	MethodPluginDisable:           PluginIDParams{},
	MethodPluginEnable:            PluginIDParams{},
	MethodPluginLink:              PluginLinkParams{},
	MethodPluginList:              EmptyParams{},
	MethodPluginLogList:           PluginLogListParams{},
	MethodPluginPaneOpen:          PluginPaneOpenParams{},
	MethodPluginReload:            PluginIDParams{},
	MethodPluginUnlink:            PluginIDParams{},
	MethodServerHandoff:           ServerHandoffParams{},
	MethodServerStop:              EmptyParams{},
	MethodContextAdd:              ContextAddParams{},
	MethodContextList:             EmptyParams{},
	MethodContextRemove:           ContextIDsParams{},
	MethodContextClear:            EmptyParams{},
	MethodContextSend:             ContextSendParams{},
	MethodBrowserAttach:           BrowserAttachParams{},
	MethodBrowserStatus:           EmptyParams{},
	MethodBrowserOpen:             BrowserURLParams{},
	MethodBrowserNavigate:         BrowserURLParams{},
	MethodBrowserSelect:           BrowserSelectParams{},
	MethodBrowserContext:          BrowserCaptureParams{},
	MethodBrowserSendToAgent:      BrowserCaptureParams{},
	MethodSessionSnapshot:         EmptyParams{},
	MethodTabClose:                TabCloseParams{},
	MethodTabCreate:               TabCreateParams{},
	MethodTabFocus:                FocusParams{},
	MethodTabGet:                  TabGetParams{},
	MethodTabList:                 TabListParams{},
	MethodTabMove:                 MoveParams{},
	MethodTabRename:               TabRenameParams{},
	MethodWindowTitleClear:        WindowTitleParams{},
	MethodWindowTitleSet:          WindowTitleParams{},
	MethodWorkspaceClose:          WorkspaceCloseParams{},
	MethodWorkspaceCreate:         WorkspaceCreateParams{},
	MethodWorkspaceGet:            WorkspaceGetParams{},
	MethodWorkspaceList:           EmptyParams{},
	MethodWorkspaceMove:           MoveParams{},
	MethodWorkspaceMoveBlock:      WorkspaceMoveBlockParams{},
	MethodWorkspaceRename:         WorkspaceRenameParams{},
	MethodWorkspaceReportMetadata: WorkspaceReportMetadataParams{},
	MethodWorktreeCreate:          WorktreeCreateParams{},
	MethodWorktreeList:            WorktreeListParams{},
	MethodWorktreeOpen:            WorktreeOpenParams{},
	MethodWorktreeRemove:          WorktreeRemoveParams{},
}

// Methods is every method the socket answers, sorted.
func Methods() []string {
	out := make([]string, 0, len(methodParams))
	for m := range methodParams {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Schema is the published schema.
func Schema() map[string]any {
	defs := map[string]any{}
	var requests []any
	for _, m := range Methods() {
		params := schemaOf(reflect.TypeOf(methodParams[m]), defs)
		req := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":     map[string]any{"type": "string"},
				"method": map[string]any{"const": m},
				"params": params,
			},
			"required":             []string{"id", "method"},
			"additionalProperties": false,
		}
		requests = append(requests, req)
	}
	var events []string
	seen := map[string]bool{}
	for k := server.EventKind(0); k < 64; k++ {
		if name := server.EventName(k); name != "" && !seen[name] {
			seen[name] = true
			events = append(events, name)
		}
	}
	sort.Strings(events)
	return map[string]any{
		"$schema":        "https://json-schema.org/draft/2020-12/schema",
		"title":          "tend API",
		"protocol":       Protocol,
		"schema_version": SchemaVersion,
		"schemas": map[string]any{
			"request": map[string]any{"oneOf": requests, "$defs": defs},
			"success_response": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
					// Each result says what it is in its "type".
					"result": map[string]any{"type": "object", "properties": map[string]any{"type": map[string]any{"type": "string"}}},
				},
				"required": []string{"id", "result"},
			},
			"error_response": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
					"error": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"code":    map[string]any{"type": "string"},
							"message": map[string]any{"type": "string"},
						},
						"required": []string{"code", "message"},
					},
				},
				"required": []string{"id", "error"},
			},
			"event": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"event": map[string]any{"enum": events},
				},
				"required": []string{"event"},
			},
		},
	}
}

var rawMessage = reflect.TypeOf(json.RawMessage(nil))

// schemaOf is a type's schema. A named struct goes into defs once and is
// referred to; the rest is written in place.
func schemaOf(t reflect.Type, defs map[string]any) map[string]any {
	if t == rawMessage {
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		inner := schemaOf(t.Elem(), defs)
		return map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "minimum": 0}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": []string{"array", "null"}, "items": schemaOf(t.Elem(), defs)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(t.Elem(), defs)}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		name := t.Name()
		if name != "" {
			ref := map[string]any{"$ref": "#/schemas/request/$defs/" + name}
			if _, done := defs[name]; done {
				return ref
			}
			defs[name] = nil // placed first, so a type that refers to itself ends
			defs[name] = structSchema(t, defs)
			return ref
		}
		return structSchema(t, defs)
	}
	return map[string]any{}
}

// structSchema is a struct's fields by their JSON names.
func structSchema(t reflect.Type, defs map[string]any) map[string]any {
	props := map[string]any{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			// An embedded struct's fields are its outer one's.
			inner := structSchema(f.Type, defs)
			for k, v := range inner["properties"].(map[string]any) {
				props[k] = v
			}
			if r, ok := inner["required"].([]string); ok {
				required = append(required, r...)
			}
			continue
		}
		if name == "" {
			name = f.Name
		}
		props[name] = schemaOf(f.Type, defs)
		// Required only when a handler says so with schema:"required": a
		// missing field is its zero value to every handler, and most mean
		// "the default" by it, so guessing from omitempty would tell a
		// caller to send what it need not.
		if f.Tag.Get("schema") == "required" {
			required = append(required, name)
		}
		_ = opts
	}
	out := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		out["required"] = required
	}
	return out
}
