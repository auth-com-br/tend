// Package workon starts an agent on a piece of work — an issue, an error, a
// ticket — in a worktree of its own: the issues panel's "start work", the
// errors and tickets panels' "fix in a worktree". It is what the terminal
// client did in cmd/tend, moved out so the desktop does the same thing the
// same way (docs/DESKTOP.md, rule 6). It draws nothing and keeps no state:
// each client hands it a way to call its session's automation socket and to
// open a tab, and shows what came back as it shows things.
package workon

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/github"
	"github.com/auth-com-br/tend/internal/proto"
)

// The automation socket's worktree methods and how it names a space, as
// internal/api has them. Not imported from there: internal/api brings the
// whole server with it, which the desktop's binary must not carry (its
// weight is budgeted, docs/DESKTOP.md). A test holds them equal.
const (
	methodWorktreeList   = "worktree.list"
	methodWorktreeCreate = "worktree.create"
	methodWorktreeOpen   = "worktree.open"
)

func spaceID(ws uint64) string { return "w_" + strconv.FormatUint(ws, 10) }

// Caller makes one call on the session's automation socket (internal/api),
// waiting as long as a worktree add takes. The worktree methods live only
// there, which is why each client passes its own way of reaching it.
type Caller func(method string, params map[string]any) (map[string]any, error)

// Tabs opens a tab with one pane in a space: internal/client's NewTab.
type Tabs interface {
	NewTab(workspace uint64, name string, spec proto.PaneSpec) (tab, pane uint64, err error)
}

// Agents is how each agent is started with a prompt of its own: as the
// first argument for most, behind -i for Gemini and Antigravity, whose bare
// prompt runs once and exits (agy's -i is --prompt-interactive: the prompt,
// then the session goes on). An agent not here is started with nothing.
var Agents = map[string][]string{
	"agy":    {"agy", "-i"},
	"claude": {"claude"},
	"codex":  {"codex"},
	"cursor": {"cursor-agent"},
	"gemini": {"gemini", "-i"},
}

// ResumeScript runs the agent and leaves a shell in the pane when it ends,
// as a tab the user had opened and typed the command into would.
const ResumeScript = `"$@"
exec "${SHELL:-/bin/sh}"`

// AgentFor is the agent work is started in: the one the settings name
// (issues.agent), else the one in the pane the panel was opened from when
// tend knows how to hand it a prompt, else claude.
func AgentFor(configured, paneAgent string) string {
	name := strings.TrimSpace(configured)
	if name == "cursor-agent" {
		name = "cursor"
	}
	if name == "" {
		if _, ok := Agents[paneAgent]; ok {
			name = paneAgent
		}
	}
	if name == "" {
		name = "claude"
	}
	return name
}

// Started is where work was started, or found already begun: the space of
// the worktree, the tab the agent was started in (zero when the worktree
// was already there and only gone to — the space's own tab is shown), and
// a line saying which.
type Started struct {
	Workspace uint64
	Tab       uint64
	Message   string
}

// Issue is Orca's "start workspace from issue": a worktree on a branch
// named after the issue, from the project dir, opened as a space of its
// own, and the agent started there with prompt. An issue that already has
// a worktree is gone to instead — work already begun is not begun twice.
func Issue(call Caller, tabs Tabs, dir string, number int, title, agentName, prompt string) (Started, error) {
	where := map[string]any{"cwd": dir}
	ws, branch, found, err := open(call, where, func(b string) bool { return github.IsIssueBranch(b, number) })
	if err != nil {
		return Started{}, err
	}
	if found {
		return Started{Workspace: ws, Message: fmt.Sprintf("#%d already has a worktree: %s", number, branch)}, nil
	}
	branch = github.IssueBranch(number, title)
	label := fmt.Sprintf("#%d %s", number, title)
	ws, tab, err := create(call, tabs, where, branch, label, fmt.Sprintf("#%d %s", number, agentName), "tend-issue", agentName, prompt)
	if err != nil {
		return Started{Workspace: ws}, err
	}
	return Started{Workspace: ws, Tab: tab, Message: fmt.Sprintf("#%d: %s on %s", number, agentName, branch)}, nil
}

// Fix makes, or finds, the worktree for an error — or a ticket, named by
// label — on branch in the repository of space ws, and starts the agent
// there with prompt when it is new.
func Fix(call Caller, tabs Tabs, ws uint64, branch, label, agentName, prompt string) (Started, error) {
	where := map[string]any{"workspace_id": spaceID(ws)}
	got, _, found, err := open(call, where, func(b string) bool { return b == branch })
	if err != nil {
		return Started{}, fmt.Errorf("this space is not a repository to fix it in: %w", err)
	}
	if found {
		return Started{Workspace: got, Message: label + " already has a worktree: " + branch}, nil
	}
	got, tab, err := create(call, tabs, where, branch, label, label+" "+agentName, "tend-fix", agentName, prompt)
	if err != nil {
		return Started{Workspace: got}, err
	}
	return Started{Workspace: got, Tab: tab, Message: fmt.Sprintf("%s: %s on %s", label, agentName, branch)}, nil
}

// GoToBranch opens the space of the worktree of the project dir that is on
// branch — a pull request's head, where its issue's work was started — and
// reports whether there was one.
func GoToBranch(call Caller, dir, branch string) (uint64, bool, error) {
	ws, _, found, err := open(call, map[string]any{"cwd": dir}, func(b string) bool { return b == branch })
	return ws, found, err
}

// open looks for a worktree whose branch is wanted among the project's, and
// opens its space when there is one.
func open(call Caller, where map[string]any, wanted func(string) bool) (ws uint64, branch string, found bool, err error) {
	list, err := call(methodWorktreeList, where)
	if err != nil {
		return 0, "", false, err
	}
	trees, _ := list["worktrees"].([]any)
	for _, raw := range trees {
		wt, _ := raw.(map[string]any)
		b, _ := wt["branch"].(string)
		if !wanted(b) {
			continue
		}
		path, _ := wt["path"].(string)
		params := map[string]any{"path": path}
		for k, v := range where {
			params[k] = v
		}
		opened, err := call(methodWorktreeOpen, params)
		if err != nil {
			return 0, "", false, err
		}
		return WorkspaceOf(opened), b, true, nil
	}
	return 0, "", false, nil
}

// create makes the worktree and its space, and starts the agent in a new
// tab there.
func create(call Caller, tabs Tabs, where map[string]any, branch, label, tabName, argv0, agentName, prompt string) (uint64, uint64, error) {
	params := map[string]any{"branch": branch, "label": label}
	for k, v := range where {
		params[k] = v
	}
	created, err := call(methodWorktreeCreate, params)
	if err != nil {
		return 0, 0, err
	}
	ws := WorkspaceOf(created)
	wt, _ := created["worktree"].(map[string]any)
	path, _ := wt["path"].(string)
	if ws == 0 || path == "" {
		return 0, 0, errors.New("the worktree was made, but tend could not tell where")
	}
	argv := append(append([]string{}, Agents[agentName]...), prompt)
	tab, _, err := tabs.NewTab(ws, tabName, proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", ResumeScript, argv0}, argv...),
		Dir:     path,
		Agent:   agentName,
	})
	if err != nil {
		return ws, 0, fmt.Errorf("the worktree %s is made, but %s did not start: %w", branch, agentName, err)
	}
	return ws, tab, nil
}

// WorkspaceOf is the space a worktree method answered with.
func WorkspaceOf(result map[string]any) uint64 {
	w, _ := result["workspace"].(map[string]any)
	id, _ := w["workspace_id"].(string)
	n, _ := strconv.ParseUint(strings.TrimPrefix(id, "w_"), 10, 64)
	return n
}

// ProjectRoot is the repository a directory is in, through its common git
// directory — for a worktree, the repository it was made from, so a fix in
// a worktree opens on the same errors and tickets; a directory in no
// repository — a folder of several, say — is its own project.
func ProjectRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return dir
	}
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return dir
}

// ContextItem is the context item for what an agent is handed to fix, an
// error or a ticket: put in the context and sent to a pane, it is typed in
// inert and submits nothing, so the user reads it and sends it.
func ContextItem(source, title, url, prompt string) proto.ContextItem {
	return proto.ContextItem{Kind: capture.KindText, Source: source, Title: title, URL: url, Text: prompt}
}
