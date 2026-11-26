package core

import (
	"fmt"
	"sync"

	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/vt"
)

// Session is the desktop's connection to one tend session and what it shows
// of it. For now that is one pane, the one the session has in focus; the
// sidebar, tabs and splits come next (docs/DESKTOP.md).
//
// It is the client.Handler, so PaneOutput and Event run on the connection's
// reader goroutine. They do nothing slow there: a call to the server from
// inside its own reply handler deadlocks, as the TUI learned (tui.go,
// Event), so anything that needs a round trip is sent off to a goroutine.
type Session struct {
	mu     sync.Mutex
	client *client.Client
	pane   uint64
	screen *vt.Screen
	// cols and rows are the size the window is drawing the pane at, which
	// the pane's terminal on the server is kept to.
	cols, rows int
	subscribed bool
	colours    Colours

	// Changed is called, without the lock held, when there is something new
	// to paint or a different pane to show. It must not block: it runs on
	// the connection's reader. The Qt side sets it to post a repaint to the
	// main thread.
	Changed func()
	// Status is called with a line for the user when something goes wrong
	// that the window should say: the session ending, a pane failing.
	Status func(string)
}

// Open connects to the named session, starting it with tendPath if it is not
// running, and settles on the pane to show.
func Open(name, tendPath string) (*Session, error) {
	s := &Session{colours: DefaultColours}
	c, err := OpenSession(name, tendPath, s)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	if err := s.refocus(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return s, nil
}

// Close ends the connection. The server, and every pane, stay.
func (s *Session) Close() error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// Pane is the pane being shown.
func (s *Session) Pane() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pane
}

// FocusedPane is the pane a session has in focus: the active space's active
// tab's active pane, falling back to the first of each, which is where the
// TUI lands too when the active one no longer resolves (resolveViewLocked).
func FocusedPane(snap proto.SessionSnapshot) uint64 {
	if len(snap.Workspaces) == 0 {
		return 0
	}
	ws := snap.Workspaces[0]
	for _, w := range snap.Workspaces {
		if w.ID == snap.ActiveWorkspace {
			ws = w
			break
		}
	}
	if len(ws.Tabs) == 0 {
		return 0
	}
	tab := ws.Tabs[0]
	for _, t := range ws.Tabs {
		if t.ID == ws.ActiveTab {
			tab = t
			break
		}
	}
	if tab.ActivePane != 0 {
		return tab.ActivePane
	}
	if len(tab.Panes) > 0 {
		return tab.Panes[0]
	}
	return 0
}

// refocus reads the session and switches to the pane it has in focus,
// opening a space with a shell when it has none, so the window always has a
// terminal in it.
func (s *Session) refocus() error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	pane := FocusedPane(snap)
	if pane == 0 {
		ws := snap.ActiveWorkspace
		if len(snap.Workspaces) == 0 {
			if ws, err = c.NewWorkspace(""); err != nil {
				return fmt.Errorf("opening a space: %w", err)
			}
		}
		if _, pane, err = c.NewTab(ws, "", proto.PaneSpec{}); err != nil {
			return fmt.Errorf("opening a tab: %w", err)
		}
	}

	s.mu.Lock()
	if pane == s.pane {
		s.mu.Unlock()
		return nil
	}
	s.pane, s.screen, s.subscribed = pane, nil, false
	// Drawn at a size already: the new pane is given that size and
	// subscribed to, which Resize does once it no longer matches.
	cols, rows := s.cols, s.rows
	s.cols, s.rows = 0, 0
	s.mu.Unlock()
	if cols > 0 && rows > 0 {
		if err := s.Resize(cols, rows); err != nil {
			return err
		}
	}
	s.changed()
	return nil
}

// Resize is the window drawing the pane at cols × rows. The pane's terminal
// on the server is set to that size before subscribing, because subscribing
// sends what is on screen now, and it has to arrive already laid out for the
// size it will be drawn at.
func (s *Session) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 {
		return nil
	}
	s.mu.Lock()
	if cols == s.cols && rows == s.rows && s.screen != nil {
		s.mu.Unlock()
		return nil
	}
	c, pane := s.client, s.pane
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	if c == nil || pane == 0 {
		return nil
	}
	if err := c.ResizePane(pane, cols, rows); err != nil {
		return err
	}
	s.mu.Lock()
	if s.pane != pane {
		s.mu.Unlock()
		return nil
	}
	if s.screen == nil {
		s.screen = vt.NewScreen(cols, rows, 0)
	} else {
		s.screen.Resize(cols, rows)
	}
	subscribe := !s.subscribed
	s.subscribed = true
	s.mu.Unlock()
	if subscribe {
		if err := c.SubscribePanes([]uint64{pane}); err != nil {
			return err
		}
	}
	s.changed()
	return nil
}

// Key sends a key press to the pane, encoded for the modes its program has
// asked for.
func (s *Session) Key(key, mods int, text string) error {
	s.mu.Lock()
	c, pane, screen := s.client, s.pane, s.screen
	var m vt.Modes
	if screen != nil {
		m = screen.Modes()
	}
	s.mu.Unlock()
	if c == nil || pane == 0 {
		return nil
	}
	b, ok := EncodeQtKey(key, mods, text, m)
	if !ok {
		return nil
	}
	return c.SendInput(pane, b)
}

// Frame is what to paint now, reusing buf's storage. ok is false while there
// is nothing to show yet.
func (s *Session) Frame(buf []Cell) (f Frame, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.screen == nil {
		return Frame{BG: s.colours.BG}, false
	}
	return FrameOf(s.screen, &s.colours, buf), true
}

func (s *Session) changed() {
	if s.Changed != nil {
		s.Changed()
	}
}

func (s *Session) status(msg string) {
	if s.Status != nil {
		s.Status(msg)
	}
}

// PaneOutput is part of client.Handler.
func (s *Session) PaneOutput(pane uint64, data []byte) {
	s.mu.Lock()
	if pane != s.pane || s.screen == nil {
		s.mu.Unlock()
		return
	}
	_, _ = s.screen.Write(data)
	s.mu.Unlock()
	s.changed()
}

// Event is part of client.Handler.
func (s *Session) Event(ev proto.Event) {
	switch ev.Kind {
	case proto.EventPaneClosed, proto.EventPaneExited, proto.EventPaneFocused,
		proto.EventTabFocused, proto.EventWorkspaceFocused, proto.EventSessionChanged:
		// Another client moved the focus, or the pane shown went away: show
		// what the session has in focus now. Off this goroutine, since it
		// calls the server.
		go func() {
			if err := s.refocus(); err != nil {
				s.status(err.Error())
			}
		}()
	}
}

// Disconnected is part of client.Handler.
func (s *Session) Disconnected() {
	s.status("the session ended")
}
