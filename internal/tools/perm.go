package tools

import (
	"os"
	"strings"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The permissions manager's logic: what an agent's tab lists — its modes,
// then its rules — how each is labelled, what the next decision of a rule
// is, and what a rule's details say. The rules are read and written by the
// session's server (internal/perm); this is how a client shows them.

// PermItem is one line of an agent's list: a mode or a rule.
type PermItem struct {
	Mode *proto.PermMode
	Rule *proto.PermRule
}

// PermAgentOf is the agent of id in the list, else the first, which is the
// tab a list read again lands on when its agent has gone.
func PermAgentOf(list proto.PermListResult, id string) (proto.PermAgent, bool) {
	for _, a := range list.Agents {
		if a.ID == id {
			return a, true
		}
	}
	if len(list.Agents) > 0 {
		return list.Agents[0], true
	}
	return proto.PermAgent{}, false
}

// PermWhere says what project the list is for.
func PermWhere(project string) string {
	if project == "" {
		return "global only: no pane to take a project from"
	}
	where := TildeHome(project)
	if home, _ := os.UserHomeDir(); project == home {
		where = "~"
	}
	return "project " + where
}

// PermTabs are the agents' tabs: a name, how many rules, and a mark on one
// with something to say. DeepSeek's warnings are its own notes, not trouble.
func PermTabs(list proto.PermListResult) []ui.PermTab {
	var tabs []ui.PermTab
	for _, agent := range list.Agents {
		tabs = append(tabs, ui.PermTab{Name: agent.Name, Rules: len(agent.Rules), Warn: len(agent.Warnings) > 0 && agent.ID != "dsh" || agent.Error != ""})
	}
	return tabs
}

// PermNotes are said under the tabs: the agent's error, its warnings, and
// when a change takes effect.
func PermNotes(a proto.PermAgent) []string {
	var notes []string
	if a.Error != "" {
		notes = append(notes, "! "+a.Error)
	}
	for _, w := range a.Warnings {
		notes = append(notes, "! "+w)
	}
	if a.Effect != "" {
		notes = append(notes, "changes: "+a.Effect)
	}
	return notes
}

// PermList is the agent's lines, modes first and then the rules filter
// keeps (all when it is empty), each with what it is.
func PermList(a proto.PermAgent, project, filter string) ([]PermItem, []ui.PermLine) {
	var items []PermItem
	var lines []ui.PermLine
	for i := range a.Modes {
		m := &a.Modes[i]
		// A mode of a file that is not there and is not set is noise,
		// but for the agent's global one, which is where to set it.
		if m.Value == "" && m.Scope != "global" && !PermFileExists(a, m.File) {
			continue
		}
		value := m.Value
		if value == "" {
			value = "(not set)"
		}
		items = append(items, PermItem{Mode: m})
		lines = append(lines, ui.PermLine{Mode: true, Scope: m.Scope, Text: PermModeKey(m.Key) + " = " + value})
	}
	for i := range a.Rules {
		r := &a.Rules[i]
		if filter != "" && !strings.Contains(strings.ToLower(r.Text), strings.ToLower(filter)) {
			continue
		}
		items = append(items, PermItem{Rule: r})
		lines = append(lines, ui.PermLine{Decision: r.Decision, Scope: PermScopeLabel(r.Scope, r.File, project), Text: r.Text, Elsewhere: r.Elsewhere, Why: r.Why, ReadOnly: r.ReadOnly})
	}
	return items, lines
}

// PermEmptyLabel is what an agent's list says with no rules: DeepSeek's
// says nothing, its modes being all it has.
func PermEmptyLabel(a proto.PermAgent) string {
	if a.ID == "dsh" {
		return ""
	}
	return "no rules: the agent asks before anything a mode does not allow — a adds one"
}

// PermScopeLabel is a scope as the list shows it: Claude's local file in
// the home folder is "local ~", told apart from the project's own — it is
// where "always allow" went for whoever starts Claude in ~, and it is not
// this project's unless this project is the home folder.
func PermScopeLabel(scope, file, project string) string {
	home, _ := os.UserHomeDir()
	if scope == "local" && strings.HasPrefix(TildeHome(file), "~/.claude/") && project != home {
		return "local ~"
	}
	return scope
}

// PermModeKey is a mode's key as it reads in the list: a Codex project's
// trust without the project's path, which the title already says.
func PermModeKey(key string) string {
	if strings.HasPrefix(key, "projects.") && strings.HasSuffix(key, ".trust_level") {
		return "this project's trust_level"
	}
	return key
}

// PermFileExists is whether the agent's file at path is there.
func PermFileExists(a proto.PermAgent, path string) bool {
	for _, f := range a.Files {
		if f.Path == path {
			return f.Exists
		}
	}
	return false
}

// PermRuleFiles are the agent's files a rule can be written to.
func PermRuleFiles(a proto.PermAgent) []proto.PermFile {
	var out []proto.PermFile
	for _, f := range a.Files {
		if !f.ReadOnly && !f.ModesOnly {
			out = append(out, f)
		}
	}
	return out
}

// PermFileLabel is a file as a menu offers it: its scope, where it is, and
// whether it is there yet.
func PermFileLabel(f proto.PermFile) string {
	s := f.Scope + "  " + TildeHome(f.Path)
	if !f.Exists {
		s += "  (new)"
	}
	if f.Note != "" {
		s += " — " + f.Note
	}
	return s
}

// PermNextDecision is the decision after d among the agent's, "" when it
// has no other.
func PermNextDecision(a proto.PermAgent, d string) string {
	for i, x := range a.Decisions {
		if x == d {
			if next := a.Decisions[(i+1)%len(a.Decisions)]; next != d {
				return next
			}
		}
	}
	return ""
}

// PermDetail is what a rule's details say: its file, what its marks mean,
// and when a change is seen.
func PermDetail(a proto.PermAgent, r proto.PermRule) []string {
	lines := []string{
		strings.ToUpper(r.Decision) + "  " + r.Text,
		"",
		"in       " + TildeHome(r.File) + "  (" + r.Scope + ")",
	}
	for _, f := range a.Files {
		if f.Path == r.File && f.Note != "" {
			lines = append(lines, "         "+f.Note)
		}
	}
	if r.Why != "" {
		lines = append(lines, "", "⚠ "+r.Why)
	}
	if r.Elsewhere {
		var others []string
		for _, o := range a.Rules {
			if o.Text == r.Text && o.Decision == r.Decision && o.File != r.File {
				others = append(others, TildeHome(o.File))
			}
		}
		lines = append(lines, "", "≡ also in "+strings.Join(others, ", ")+": one of them can go (d)")
	}
	if r.ReadOnly {
		lines = append(lines, "", "tend does not edit this one: e opens the file in your editor")
	}
	if a.Effect != "" {
		lines = append(lines, "", "a change: "+a.Effect)
	}
	return lines
}

// PermDecisionWords say what a decision does.
var PermDecisionWords = map[string]string{
	"allow": "allow — without asking",
	"ask":   "ask — every time",
	"deny":  "deny — never",
}

// PermRuleHint is how a rule is written for an agent, said under the line
// it is typed on.
var PermRuleHint = map[string]string{
	"claude":   "e.g. Bash(npm test:*) · Bash(git log *) · Read(src/**) · Edit(docs/**) · WebFetch(domain:x.com) · mcp__server",
	"codex":    "the command it starts with: git push · npm run {build|test} · \"a b\" quotes a word",
	"cursor":   "e.g. Shell(git log) · Read(src/**) · Write(docs/**) · Mcp(server:tool) · WebFetch(x.com)",
	"gemini":   "e.g. run_shell_command(git) · write_file · web_fetch",
	"opencode": "tool or tool(pattern): bash(git *) · edit · webfetch — a new rule goes last, where it wins",
}

// PermEditorLabel is an editor as the "open in" menu offers it.
func PermEditorLabel(e proto.PermEditor) string {
	label := e.Name
	if e.Command != e.Name {
		label += " (" + e.Command + ")"
	}
	if e.GUI {
		label += " — a window"
	} else {
		label += " — beside the pane"
	}
	if e.Default {
		label += "  (default)"
	}
	return label
}
