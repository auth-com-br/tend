package server

import (
	"sync"

	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/session"
)

// EventKind is what happened.
type EventKind uint8

const (
	// EventPaneOpened reports a new pane and its process.
	EventPaneOpened EventKind = iota
	// EventPaneOutput reports that a pane's screen changed. It carries no
	// data: a subscriber redraws from the current screen rather than
	// replaying a stream, so a missed one costs nothing.
	EventPaneOutput
	// EventPaneState reports that a pane's record changed: the agent state
	// detected in it, or whether its program has asked for the mouse. Both
	// are things a client can only learn by re-reading the session, so one
	// event saying "look again" serves them.
	EventPaneState
	// EventPaneExited reports that a pane's process ended. The pane record
	// remains until it is closed.
	EventPaneExited
	// EventPaneClosed reports that a pane was removed from the session.
	EventPaneClosed
	// EventPaneClipboard reports that a pane's program asked for text to be
	// put on the clipboard. The clipboard belongs to whoever is looking at
	// the pane, so the request is passed on rather than acted on here.
	EventPaneClipboard
	// EventPaneFocused, EventTabFocused and EventWorkspaceFocused report what
	// the user is looking at. Focus is the client's, so these are published
	// when a client says so; a session nobody is attached to has no focus and
	// sends none.
	EventPaneFocused
	EventTabFocused
	EventWorkspaceFocused
	// EventTabCreated and EventWorkspaceCreated report new ones. A plugin
	// that decorates a tab has to hear about the tab before it can.
	EventTabCreated
	EventWorkspaceCreated
	// EventNotify carries something somebody wants the user told: a script,
	// an agent's hook, a plugin. The server has no screen, so it passes it to
	// whoever is looking.
	EventNotify
	// EventSessionChanged reports that the session's shape changed without a
	// pane coming or going: panes swapped, a tab or a space moved or renamed.
	// Clients that did not make the change learn of it only this way, and
	// before it existed a rename by one client stayed invisible to another
	// until something unrelated made it look again.
	EventSessionChanged
	// The rest of herdr's lifecycle events, for plugins and scripts: what
	// was closed, renamed or moved, a pane moved elsewhere, a worktree made,
	// opened or removed, an agent recognised in a pane. Each is published
	// beside EventSessionChanged, which is what clients redraw on.
	EventWorkspaceClosed
	EventWorkspaceRenamed
	EventWorkspaceMoved
	EventTabClosed
	EventTabRenamed
	EventTabMoved
	EventPaneMoved
	EventWorktreeCreated
	EventWorktreeOpened
	EventWorktreeRemoved
	EventAgentDetected
	// EventFocusRequest asks the clients to show a pane: pane.focus and
	// tab.focus over the API. Focus is each client's, so the server asks
	// and each client moves itself; a session nobody is attached to has
	// nothing to move.
	EventFocusRequest
	// EventScrollRequest asks the clients to show a pane scrolled back:
	// pane.scroll over the automation socket.
	EventScrollRequest
	// EventUpdateReady says the release check found a newer release: Title
	// is its version, Body how to install it.
	EventUpdateReady
	// EventContextChanged says the context buffer changed (context.go).
	EventContextChanged
	// EventContextArrived says a tool handed the context something to look
	// at — Title is which ("browser"), Body how many items — for the
	// client to show it (browser.go).
	EventContextArrived
)

func (k EventKind) String() string {
	switch k {
	case EventPaneOpened:
		return "pane-opened"
	case EventPaneOutput:
		return "pane-output"
	case EventPaneState:
		return "pane-state"
	case EventPaneExited:
		return "pane-exited"
	case EventPaneClosed:
		return "pane-closed"
	case EventPaneClipboard:
		return "pane-clipboard"
	case EventPaneFocused:
		return "pane-focused"
	case EventTabFocused:
		return "tab-focused"
	case EventWorkspaceFocused:
		return "workspace-focused"
	case EventTabCreated:
		return "tab-created"
	case EventWorkspaceCreated:
		return "workspace-created"
	case EventNotify:
		return "notify"
	case EventSessionChanged:
		return "session-changed"
	case EventUpdateReady:
		return "update-ready"
	case EventContextChanged:
		return "context-changed"
	case EventContextArrived:
		return "context-arrived"
	default:
		return "unknown"
	}
}

// Event is one thing that happened to a pane.
type Event struct {
	Kind EventKind
	Pane session.PaneID

	// State, Rule and Agent are set on EventPaneState. Agent is who the
	// state is about, carried so a client can announce it before the
	// session it re-reads has caught up.
	State detect.State
	Rule  string
	Agent string

	// Err is set on EventPaneExited when the process failed.
	Err string

	// Data is set on EventPaneClipboard: the text to be copied.
	Data []byte

	// Title and Body are set on EventNotify.
	Title string
	Body  string

	// Tab and Workspace are set on the events about them.
	Tab       session.TabID
	Workspace session.WorkspaceID

	// Offset is set on EventScrollRequest: how many lines above the
	// present the pane is to be shown at.
	Offset int
}

// Subscription is a stream of events.
//
// The channel is buffered and the server never blocks on it. A subscriber that
// stops reading loses events rather than stalling the runtime: a stuck client
// must not be able to freeze the agents it is watching. Dropped counts what was
// lost, so a client can tell "nothing happened" from "I fell behind" and
// resynchronise from a fresh snapshot.
type Subscription struct {
	C <-chan Event

	hub     *eventHub
	ch      chan Event
	mu      sync.Mutex
	dropped uint64
	closed  bool
}

// Dropped returns how many events this subscription has lost so far.
func (s *Subscription) Dropped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() {
	s.hub.remove(s)
}

type eventHub struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[*Subscription]struct{})}
}

// subscribe returns a new subscription with the given buffer.
func (h *eventHub) subscribe(buffer int) *Subscription {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan Event, buffer)
	sub := &Subscription{C: ch, hub: h, ch: ch}

	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

func (h *eventHub) remove(sub *Subscription) {
	h.mu.Lock()
	_, present := h.subs[sub]
	delete(h.subs, sub)
	h.mu.Unlock()

	if !present {
		return
	}
	sub.mu.Lock()
	if !sub.closed {
		sub.closed = true
		close(sub.ch)
	}
	sub.mu.Unlock()
}

// publish delivers to every subscriber without blocking on any of them.
func (h *eventHub) publish(ev Event) {
	h.mu.Lock()
	subs := make([]*Subscription, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	for _, sub := range subs {
		sub.mu.Lock()
		if sub.closed {
			sub.mu.Unlock()
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			sub.dropped++
		}
		sub.mu.Unlock()
	}
}

// closeAll ends every subscription, which is how shutdown tells clients to
// stop waiting for events that will never come.
func (h *eventHub) closeAll() {
	h.mu.Lock()
	subs := make([]*Subscription, 0, len(h.subs))
	for sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subs = make(map[*Subscription]struct{})
	h.mu.Unlock()

	for _, sub := range subs {
		sub.mu.Lock()
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		sub.mu.Unlock()
	}
}
