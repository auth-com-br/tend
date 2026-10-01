package main

import (
	"time"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/ui"
)

// The toolbar over the sidebar's lists (internal/ui/toolbar.go): tend's own,
// not herdr's. Each tool is a way into something the client already does or
// will — Files is the files panel as prefix+f toggles it, the others come in
// later steps and are drawn disabled until then. The pointer, the keyboard's
// walk of the sidebar (prefix+w) and a click all end in runTool.

// toolLabels are the names a tool goes by, which its hover and its focus
// show on the status line in place of a tooltip, which a terminal has not.
var toolLabels = map[string]string{
	ui.ToolFiles:    "Files — the files panel (prefix+f)",
	ui.ToolAgents:   "Agents — find and install agents (prefix+A)",
	ui.ToolMCP:      "MCP servers — the tools your agents can use: on/off, test, add (prefix+M)",
	ui.ToolSessions: "Sessions — find, resume and delete agent sessions (prefix+S)",
	ui.ToolIssues:   "Issues — this project's GitHub issues (prefix+I)",
	ui.ToolErrors:   "Errors — what GlitchTip caught, to fix (prefix+E)",
	ui.ToolTickets:  "Tickets — Linear, Jira, ClickUp, Issue Gateway (prefix+T)",
	ui.ToolBrowser:  "Browser — open a page (prefix+B)",
	ui.ToolContext:  "Context — captured, to send to an agent (prefix+C)",
}

// toolReady is whether a tool does anything yet.
func toolReady(id string) bool { _, ok := toolLabels[id]; return ok }

// toolbarLocked is the toolbar as the frame draws it.
func (t *tui) toolbarLocked() []ui.ToolbarItem {
	ids := t.config.ToolbarItems()
	if len(ids) == 0 {
		return nil
	}
	items := make([]ui.ToolbarItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, ui.ToolbarItem{
			ID:       id,
			Label:    toolLabels[id],
			Active:   id == ui.ToolFiles && t.filesPanelLocked() != 0,
			Disabled: !toolReady(id),
			Hover:    id == t.toolbarHover,
			Focused:  t.navigating && t.nav.tool == id,
		})
	}
	return items
}

// filesPanelLocked is the files panel's pane in the tab shown, or zero.
func (t *tui) filesPanelLocked() uint64 {
	inTab := map[uint64]bool{}
	for _, r := range t.rects {
		inTab[r.Pane] = true
	}
	for _, p := range t.snap.Panes {
		if inTab[p.ID] && p.Title == filesTitle {
			return p.ID
		}
	}
	return 0
}

// runTool does what a tool is for.
func (t *tui) runTool(id string) error {
	if !toolReady(id) {
		t.setMessage(toolLabels[id], false)
		return nil
	}
	switch id {
	case ui.ToolFiles:
		return t.toggleFiles()
	case ui.ToolAgents:
		return t.openAgentManager()
	case ui.ToolMCP:
		return t.openMCPManager()
	case ui.ToolSessions:
		return t.openSessions()
	case ui.ToolIssues:
		return t.openIssues()
	case ui.ToolErrors:
		return t.openErrors()
	case ui.ToolTickets:
		return t.openTickets()
	case ui.ToolContext:
		return t.openContext()
	case ui.ToolBrowser:
		t.startPrompt(promptOpenURL)
		return nil
	}
	return nil
}

// clickToolbar answers a press on the toolbar's lines: the tool under it,
// or nothing — the rule under the tools is not a list to act on.
func (t *tui) clickToolbar(frame ui.Frame, x, y, rows int) error {
	i, ok := ui.ToolbarItemAt(frame, x, y, rows)
	if !ok {
		return nil
	}
	return t.runTool(frame.Toolbar[i].ID)
}

// hoverToolbar follows the pointer over the tools, saying the one under it
// on the status line; it reports whether the pointer is on the toolbar.
func (t *tui) hoverToolbar(x, y int) bool {
	t.mu.Lock()
	frame := t.buildFrame()
	id := ""
	onBar := t.sidebar && ui.SidebarPlaceAt(frame, x, y, t.rows) == ui.SidebarToolbar
	if i, ok := ui.ToolbarItemAt(frame, x, y, t.rows); ok {
		id = frame.Toolbar[i].ID
	}
	changed := id != t.toolbarHover
	previous := t.toolbarHover
	t.toolbarHover = id
	if changed {
		// The name comes and goes with the pointer, and never takes away a
		// message something else put there.
		if id != "" {
			t.message, t.alert, t.msgAt = toolLabels[id], false, time.Now()
		} else if t.message == toolLabels[previous] {
			t.message = ""
		}
		t.dirty = true
	}
	t.mu.Unlock()
	if changed {
		t.wakeUp()
	}
	return onBar
}

// toolChoices are the tools as the tools menu lists them: every one, in the
// toolbar's default order whatever the toolbar shows, each its name and
// what it is for, with its key.
func toolChoices() []ui.ToolChoice {
	var out []ui.ToolChoice
	for _, id := range config.ToolbarTools {
		if label, ok := toolLabels[id]; ok {
			out = append(out, ui.ToolChoice{ID: id, Label: label})
		}
	}
	return out
}
