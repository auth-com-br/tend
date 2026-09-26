package main

import (
	"sort"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The agent manager (internal/ui/agentmanager.go, internal/agents): tend's
// own. The server says which agent CLIs its machine has and how the rest
// install; this shows them, and installs one by running its command in a
// tab of its own in the space shown — where the user watches it, answers it
// (a password, a licence) and can stop it — which then goes on as a shell,
// so the agent just installed can be run there. The list is read again each
// time the manager opens and on r, so an install shows once it is done.

// installScript runs an install command and says how it went, then leaves
// a shell: $1 is the command, $2 the agent's name.
const installScript = `printf '\033[1m[tend] installing %s\033[0m\n%s\n\n' "$2" "$1"
sh -c "$1"
status=$?
echo
if [ "$status" -eq 0 ]; then
	printf '[tend] %s installed. Open the agent manager again (prefix+A) to see it.\n' "$2"
else
	printf '[tend] the install stopped with status %s.\n' "$status"
fi
exec "${SHELL:-/bin/sh}"`

func (t *tui) agentManagerUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.agentMgr != nil
}

// openAgentManager puts the manager up and asks the server for the list.
func (t *tui) openAgentManager() error {
	t.mu.Lock()
	t.agentMgr = &ui.AgentManagerView{Loading: true}
	t.agentStatus = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go t.loadAgents()
	return nil
}

// loadAgents reads the list, installed first and each part by name.
func (t *tui) loadAgents() {
	if !t.client.Supports(proto.MethodAgentsCatalog) {
		t.mu.Lock()
		if t.agentMgr != nil {
			t.agentMgr.Loading = false
			t.agentMgr.Message = "this server is older than the agent manager; tend handoff moves it to this build"
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return
	}
	res, err := t.client.AgentsCatalog()
	list := res.Agents
	// The agents first, then the memory tools, each installed first: the
	// order the panel draws them in, which its cursor counts through.
	sort.SliceStable(list, func(i, j int) bool {
		if mi, mj := list[i].Kind == "memory", list[j].Kind == "memory"; mi != mj {
			return mj
		}
		if list[i].Installed != list[j].Installed {
			return list[i].Installed
		}
		return false
	})
	t.mu.Lock()
	defer func() {
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
	if t.agentMgr == nil {
		return // closed while it looked
	}
	v := t.agentMgr
	v.Loading, v.Confirm = false, false
	if err != nil {
		v.Message = "could not list the agents: " + err.Error()
		return
	}
	t.agentStatus = list
	v.Agents = v.Agents[:0]
	installed := 0
	for _, a := range list {
		v.Agents = append(v.Agents, ui.AgentEntry{
			Name: a.Name, Installed: a.Installed, Version: a.Version, Path: a.Path,
			InstallCommand: a.InstallCommand, Missing: a.Missing,
			Memory: a.Kind == "memory", Running: a.Running,
		})
		if a.Installed && a.Kind != "memory" {
			installed++
		}
	}
	v.Cursor = min(v.Cursor, max(len(v.Agents)-1, 0))
	agents := 0
	for _, a := range list {
		if a.Kind != "memory" {
			agents++
		}
	}
	v.Message = itoaInt(installed) + " of " + itoaInt(agents) + " agents installed on this machine"
}

func (t *tui) closeAgentManager() {
	t.mu.Lock()
	t.agentMgr = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// agentManagerInput is every key and click while the manager is up: j/k
// and the arrows move, enter asks to install and enter again installs, r
// reads the list again, esc steps back (out of the question, then out of
// the manager). Everything else is taken, as the panel is over everything.
func (t *tui) agentManagerInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		if ev.Kind == ui.MouseWheelUp {
			t.moveAgentCursor(-1)
			continue
		}
		if ev.Kind == ui.MouseWheelDown {
			t.moveAgentCursor(1)
			continue
		}
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		t.mu.Lock()
		v, cols, rows := t.agentMgr, t.cols, t.rows
		if v == nil {
			t.mu.Unlock()
			return nil
		}
		g := ui.AgentManagerLayout(v, cols, rows)
		i, onEntry := ui.AgentManagerEntryAt(v, cols, rows, ev.X, ev.Y)
		t.mu.Unlock()
		switch {
		case inRect(g.Close, ev.X, ev.Y), ui.OnCloseMark(g.Box, ev.X, ev.Y):
			t.closeAgentManager()
			return nil
		case inRect(g.Refresh, ev.X, ev.Y):
			return t.openAgentManager()
		case onEntry:
			t.mu.Lock()
			again := v.Cursor == i
			v.Cursor = i
			t.dirty = true
			t.mu.Unlock()
			// A click on the one already chosen is enter on it.
			if again {
				if err := t.agentEnter(); err != nil {
					return err
				}
			}
		}
	}
	for _, key := range splitKeys(forward) {
		switch key {
		case "\x1b", "q":
			t.mu.Lock()
			if v := t.agentMgr; v != nil && v.Confirm {
				v.Confirm = false
				t.dirty = true
				t.mu.Unlock()
				continue
			}
			t.mu.Unlock()
			t.closeAgentManager()
			return nil
		case "\x1b[A", "k":
			t.moveAgentCursor(-1)
		case "\x1b[B", "j":
			t.moveAgentCursor(1)
		case "r":
			return t.openAgentManager()
		case "\r", "\n":
			if err := t.agentEnter(); err != nil {
				return err
			}
		}
	}
	t.wakeUp()
	return nil
}

func inRect(r ui.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.Cols && y >= r.Y && y < r.Y+r.Rows
}

func (t *tui) moveAgentCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.agentMgr
	if v == nil || len(v.Agents) == 0 {
		return
	}
	v.Cursor = (v.Cursor + by + len(v.Agents)) % len(v.Agents)
	v.Confirm = false
	v.Scroll = ui.AgentManagerScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

// agentEnter is enter on the chosen agent: an installed one says where it
// is; one that can be installed asks, and installs when asked again; one
// that cannot says what it needs.
func (t *tui) agentEnter() error {
	t.mu.Lock()
	v := t.agentMgr
	if v == nil || v.Cursor >= len(t.agentStatus) {
		t.mu.Unlock()
		return nil
	}
	a := t.agentStatus[v.Cursor]
	switch {
	case a.Installed && a.Next != "":
		// A memory tool installed is not yet the agents': say how.
		v.Message = a.Name + " — " + a.Next
	case a.Installed:
		v.Message = a.Name + " is at " + a.Path
	case a.InstallCommand == "":
		v.Message = a.Name + " needs " + a.Missing + " to install, which this machine has not"
	case !v.Confirm:
		v.Confirm = true
	default:
		t.mu.Unlock()
		return t.installAgent(a)
	}
	t.dirty = true
	t.mu.Unlock()
	return nil
}

// installAgent runs an agent's install in a new tab of the space shown and
// goes there, taking the manager down.
func (t *tui) installAgent(a proto.AgentStatus) error {
	t.mu.Lock()
	ws := t.workspace
	t.mu.Unlock()
	tab, _, err := t.client.NewTab(ws, "install "+a.ID, proto.PaneSpec{
		Command: []string{"/bin/sh", "-c", installScript, "tend-install", a.InstallCommand, a.Name},
		Title:   "install " + a.ID,
	})
	if err != nil {
		t.mu.Lock()
		if t.agentMgr != nil {
			t.agentMgr.Confirm = false
			t.agentMgr.Message = "could not start the install: " + err.Error()
		}
		t.dirty = true
		t.mu.Unlock()
		return nil
	}
	t.closeAgentManager()
	// Straight to it, as a new tab is gone to: the snapshot this client has
	// does not know of the tab yet, so it is not looked up there.
	t.mu.Lock()
	t.rememberFocusLocked()
	t.tab, t.focus, t.zoom = tab, 0, false
	t.mu.Unlock()
	return t.refresh()
}
