// Package tools is the logic of the global menu's tools — the MCP and
// permissions managers, the agent manager, the agents' sessions, handing a
// task on and the context buffer — that both clients need: what each lists
// and in what order, what a write says it did, what a script started in a
// tab runs. The TUI (cmd/tend) draws it in cells and tend-desktop in Qt
// Quick; neither decides any of it on its own, so the two cannot come to
// offer different things.
package tools

import (
	"os"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/ui"
)

// ErrorText is an error as a panel says it: a refusal from the server
// without the method's name in front ("mcp.add: no such agent" is "no such
// agent"), which says nothing to whoever pressed the key.
func ErrorText(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ": "); i > 0 && !strings.Contains(msg[:i], " ") {
		return msg[i+2:]
	}
	return msg
}

// TildeHome writes the home directory as ~, so a path fits on a line.
func TildeHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

// ShellQuote quotes a word for a shell.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellJoin is argv as a shell reads it back, each word quoted.
func ShellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = ShellQuote(a)
	}
	return strings.Join(quoted, " ")
}

// ToolLabels are the names a tool goes by, with the TUI's key for it: the
// toolbar's hover, and the lines of the "all tools" menu.
var ToolLabels = map[string]string{
	ui.ToolFiles:    "Files — the files panel (prefix+f)",
	ui.ToolAgents:   "Agents — find and install agents (prefix+A)",
	ui.ToolMCP:      "MCP servers — the tools your agents can use: on/off, test, add (prefix+M)",
	ui.ToolPerms:    "Permissions — what your agents may do without asking (prefix+P)",
	ui.ToolSessions: "Sessions — find, resume and delete agent sessions (prefix+S)",
	ui.ToolIssues:   "Issues — this project's GitHub issues (prefix+I)",
	ui.ToolErrors:   "Errors — what GlitchTip caught, to fix (prefix+E)",
	ui.ToolTickets:  "Tickets — Linear, Jira, ClickUp, Issue Gateway (prefix+T)",
	ui.ToolBrowser:  "Browser — open a page (prefix+B)",
	ui.ToolContext:  "Context — captured, to send to an agent (prefix+C)",
}

// ToolChoices are the tools as the "all tools" menu lists them: every one
// a client has (here says which; nil is all), in the toolbar's default
// order whatever the toolbar shows, each its name and what it is for.
func ToolChoices(here func(id string) bool) []ui.ToolChoice {
	var out []ui.ToolChoice
	for _, id := range config.ToolbarTools {
		if label, ok := ToolLabels[id]; ok && (here == nil || here(id)) {
			out = append(out, ui.ToolChoice{ID: id, Label: label})
		}
	}
	return out
}
