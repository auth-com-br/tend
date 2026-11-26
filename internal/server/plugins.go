package server

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/auth-com-br/tend/internal/plugin"
	"github.com/auth-com-br/tend/internal/session"
)

// A plugin's event hooks run here, because this is where events are. What a
// hook does with an event it does through the automation socket, so the server
// starts the process and forgets about it; nothing in the session waits on a
// plugin.
//
// Two limits matter and both come from what an event hook is. It runs on
// things that happen constantly — a pane's output, an agent's state — so a
// hook that is slow must not be started again while the last one is still
// going, and a hook that hangs must be killed rather than accumulate. herdr
// bounds these too (`app/api/plugins/runtime.rs`).

// hookLimit is how many of a plugin's hook processes may be in flight at once.
// Past that, the event is dropped for that plugin rather than queued: a queue
// would only delay the same overload, and an event hook that cannot keep up is
// better off missing one than running minutes behind.
const hookLimit = 4

// Plugins is the host the server runs hooks through. Nil means the server was
// started without plugin support, which is what every test that does not care
// about plugins gets.
type Plugins struct {
	Registry *plugin.Registry
	// Env is what every plugin command is told: the socket to call back on,
	// the marker, this binary's path. It is the same set a pane gets.
	Env []string
	// ConfigDir and StateDir are where a plugin's own files go, one directory
	// per plugin underneath.
	ConfigDir string
	StateDir  string

	mu      sync.Mutex
	running map[string]int

	// log is the last runs of every plugin command, for plugin.log.list.
	log pluginLog
}

// dirsFor is where one plugin keeps its settings and its state.
func (p *Plugins) dirsFor(id string) (config, state string) {
	if p.ConfigDir != "" {
		config = p.ConfigDir + "/" + id
	}
	if p.StateDir != "" {
		state = p.StateDir + "/" + id
	}
	return config, state
}

// Invocation fills in everything a command needs except what it is for.
func (p *Plugins) Invocation(installed plugin.Installed, command []string) plugin.Invocation {
	config, state := p.dirsFor(installed.ID)
	return plugin.Invocation{
		Plugin: installed, Command: command, Env: p.Env,
		ConfigDir: config, StateDir: state,
	}
}

// enter reports whether another of this plugin's hooks may start.
func (p *Plugins) enter(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running == nil {
		p.running = map[string]int{}
	}
	if p.running[id] >= hookLimit {
		return false
	}
	p.running[id]++
	return true
}

func (p *Plugins) leave(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running[id] > 0 {
		p.running[id]--
	}
}

// RunStartupPlugins runs what every enabled plugin asked to have run when the
// session comes up. It returns once they have been started, not once they have
// finished: a plugin that takes a minute to warm up must not hold the session.
func (s *Server) RunStartupPlugins() {
	host := s.cfg.Plugins
	if host == nil {
		return
	}
	for _, installed := range host.Registry.Enabled() {
		for _, step := range plugin.ForThisPlatform(installed.Startup, func(st plugin.Step) []string { return st.Platforms }) {
			inv := host.Invocation(installed, step.Command)
			inv.Event = "startup"
			s.wg.Add(1)
			go func(inv plugin.Invocation, id string) {
				defer s.wg.Done()
				result := host.RunPlugin(s.context(), inv)
				if result.Err != "" {
					s.logf("plugin %s: startup: %s", id, result.Err)
				}
			}(inv, installed.ID)
		}
	}
}

// PluginHost is the plugin host this server was given, or nil when it runs
// none.
func (s *Server) PluginHost() *Plugins { return s.cfg.Plugins }

// PluginContext describes a pane for a plugin command invoked against it.
func (s *Server) PluginContext(pane session.PaneID) plugin.Context {
	return s.pluginContext(pane)
}

// publish sends an event to subscribers and to the plugins that asked for it.
//
// One funnel rather than a call beside every publish: a new event added later
// reaches plugins without anybody remembering to tell them, which is exactly
// the kind of thing that is forgotten.
func (s *Server) publish(ev Event) {
	s.events.publish(ev)
	s.notifyPlugins(ev)
}

// notifyPlugins runs the hooks that asked for this event.
func (s *Server) notifyPlugins(ev Event) {
	host := s.cfg.Plugins
	if host == nil {
		return
	}
	name := PluginEventName(ev.Kind)
	if name == "" {
		return
	}
	hooks := host.Registry.Hooks(name)
	if len(hooks) == 0 {
		return
	}

	// Everything from here happens on another goroutine, and that is not for
	// speed. Events are published from under the server lock — a pane opening
	// is one — and describing where the event happened needs that same lock,
	// which a publisher already holds. Doing it here would deadlock the server
	// on the first pane opened with a plugin installed. It is also the rule
	// this codebase already has: no I/O under the lock every pane needs.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Read once for all of this event's hooks, so two hooks on one event
		// cannot disagree about what the session looked like.
		ctx := s.pluginContext(ev.Pane)
		if ev.Tab != 0 {
			ctx.TabID = "t_" + itoa(uint64(ev.Tab))
		}
		if ev.Workspace != 0 {
			ctx.WorkspaceID = "w_" + itoa(uint64(ev.Workspace))
		}
		data, _ := json.Marshal(map[string]any{
			"event": name, "pane_id": ctx.PaneID,
			"tab_id": ctx.TabID, "workspace_id": ctx.WorkspaceID,
			"agent_state": ev.State.String(), "rule": ev.Rule,
		})

		for _, hook := range hooks {
			if !host.enter(hook.Plugin.ID) {
				s.logf("plugin %s: %s dropped; %d of its hooks are still running",
					hook.Plugin.ID, name, hookLimit)
				continue
			}
			inv := host.Invocation(hook.Plugin, hook.Hook.Command)
			inv.Event, inv.EventJSON, inv.Context = name, string(data), ctx

			s.wg.Add(1)
			go func(inv plugin.Invocation, id string) {
				defer s.wg.Done()
				defer host.leave(id)
				if result := host.RunPlugin(s.context(), inv); result.Err != "" {
					s.logf("plugin %s: %s: %s", id, inv.Event, result.Err)
				}
			}(inv, hook.Plugin.ID)
		}
	}()
}

// PluginEventName is how an event is named to plugins, or empty for one they
// are not told about: herdr's hook events, and tend's clipboard.
func PluginEventName(k EventKind) string {
	switch k {
	case EventPaneOutput, EventNotify, EventSessionChanged, EventFocusRequest:
		return "" // too frequent, or not a lifecycle event
	}
	return EventName(k)
}

// EventName is an event's name, herdr's where herdr has one.
func EventName(k EventKind) string {
	switch k {
	case EventPaneOpened:
		return "pane.created"
	case EventPaneClosed:
		return "pane.closed"
	case EventPaneExited:
		return "pane.exited"
	case EventPaneOutput:
		return "pane.output_changed"
	case EventPaneState:
		return "pane.agent_status_changed"
	case EventPaneClipboard:
		return "pane.clipboard"
	case EventPaneFocused:
		return "pane.focused"
	case EventPaneMoved:
		return "pane.moved"
	case EventAgentDetected:
		return "pane.agent_detected"
	case EventTabCreated:
		return "tab.created"
	case EventTabClosed:
		return "tab.closed"
	case EventTabRenamed:
		return "tab.renamed"
	case EventTabMoved:
		return "tab.moved"
	case EventTabFocused:
		return "tab.focused"
	case EventWorkspaceCreated:
		return "workspace.created"
	case EventWorkspaceClosed:
		return "workspace.closed"
	case EventWorkspaceRenamed:
		return "workspace.renamed"
	case EventWorkspaceMoved:
		return "workspace.moved"
	case EventWorkspaceFocused:
		return "workspace.focused"
	case EventWorktreeCreated:
		return "worktree.created"
	case EventWorktreeOpened:
		return "worktree.opened"
	case EventWorktreeRemoved:
		return "worktree.removed"
	case EventNotify:
		return "notification"
	case EventSessionChanged:
		return "session.changed"
	case EventTasksChanged:
		return "task.changed"
	}
	return ""
}

// pluginContext describes where an invocation is happening.
func (s *Server) pluginContext(pane session.PaneID) plugin.Context {
	ctx := plugin.Context{}
	if pane == 0 {
		return ctx
	}
	ctx.PaneID = "p_" + itoa(uint64(pane))

	s.mu.Lock()
	for _, w := range s.session.Workspaces() {
		for _, t := range w.Tabs() {
			if p, ok := t.Pane(pane); ok {
				ctx.WorkspaceID = "w_" + itoa(uint64(w.ID))
				ctx.TabID = "t_" + itoa(uint64(t.ID))
				ctx.PaneAgent = p.Agent
				ctx.PaneState = p.State.String()
				ctx.Dir = p.Dir
			}
		}
	}
	s.mu.Unlock()
	return ctx
}

// context is a context that ends when the server does, so a plugin command
// does not outlive the session that started it.
func (s *Server) context() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx
}

// itoa is strconv without the import, for ids that are always small.
func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
