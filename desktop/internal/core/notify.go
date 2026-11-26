package core

import (
	"time"

	"github.com/auth-com-br/tend/internal/announce"
	"github.com/auth-com-br/tend/internal/asks"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/notify"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/sidebar"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/worktree"
)

// Being told that an agent needs you, decided as the TUI decides it
// (internal/announce: herdr's rule, tend's cooldown, news held a moment
// and said only if still true) and said the way a desktop says things: a
// notification from the system, which a click on takes to the agent, and
// a card in the window for somebody looking at it. The settings are the
// TUI's: [notify] toasts ("off" says nothing), delay, focused; [sound].

// Toast is a card the window shows for a while.
type Toast struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Pane  uint64 `json:"pane,omitempty"`
}

// toastLife is how long a card stays: herdr's toast lasts about as long.
const toastLife = 6 * time.Second

type heldNotice struct {
	kind, title, body string
	pane              uint64
	sound             notify.Sound
	due, expires      time.Time
}

// notifier is what the session keeps to announce.
type notifier struct {
	toasts        string
	delay         time.Duration
	notifyFocused bool
	sounds        config.Sound
	player        *notify.Player
	notices       map[uint64]announce.Notice
	held          []heldNotice
	shown         []Toast
	expiry        map[int]time.Time
	nextToast     int
	windowFocused bool
}

func newNotifier(cfg config.Config) *notifier {
	return &notifier{
		toasts:        cfg.Toasts(),
		delay:         cfg.NotifyDelay(),
		notifyFocused: cfg.Notify.Focused,
		sounds:        cfg.Sound,
		player: &notify.Player{
			Enabled: cfg.Sound.Enabled,
			Done:    worktree.ExpandHome(cfg.Sound.Done),
			Request: worktree.ExpandHome(cfg.Sound.Request),
			// A window has no terminal bell to ring.
			Bell: func() {},
		},
		notices:       map[uint64]announce.Notice{},
		expiry:        map[int]time.Time{},
		windowFocused: true,
	}
}

// SetWindowFocused is the window gaining or losing the focus. The pane in
// view is not announced while the window has the focus — being told what is
// on screen is noise — but is when it is behind something else (herdr's
// active_tab_suppresses_notifications). The server is told too: it is how
// it knows an agent finished unseen.
func (s *Session) SetWindowFocused(focused bool) error {
	s.mu.Lock()
	s.notes.windowFocused = focused
	c := s.client
	s.mu.Unlock()
	return c.WindowFocus(focused)
}

// announceState decides whether a pane's new state is worth saying, as the
// TUI's announceFrom. It runs on the connection's reader: the one round
// trip it may need (what a blocked agent asks) goes to a goroutine.
func (s *Session) announceState(ev proto.Event) {
	state := detect.StateFromString(ev.State)
	if state != detect.StateBlocked && state != detect.StateIdle {
		return
	}
	s.mu.Lock()
	n := s.notes
	if n.toasts == "off" && !n.player.Enabled {
		s.mu.Unlock()
		return
	}
	focused := ev.Pane == s.focus && n.windowFocused
	previous := n.notices[ev.Pane]
	kind, worth := announce.Worth(previous, state, time.Now())
	n.notices[ev.Pane] = announce.Notice{State: state, At: time.Now()}
	var agent, where string
	muted := false
	for _, p := range s.snap.Panes {
		if p.ID == ev.Pane {
			agent, muted = p.Agent, p.Muted
		}
	}
	if agent == "" {
		// The event is ahead of the snapshot: an agent whose first word is
		// "I need you" would otherwise go unannounced.
		agent = ev.Agent
	}
	for _, w := range s.snap.Workspaces {
		for _, t := range w.Tabs {
			if hasPane(t, ev.Pane) {
				where = sidebar.AgentLabel(w, t)
			}
		}
	}
	allowed := n.sounds.AllowsAgent(agent)
	c := s.client
	s.mu.Unlock()
	if agent == "" || !worth || muted || (focused && !n.notifyFocused) {
		return
	}
	sound := notify.SoundNone
	switch kind {
	case announce.Blocked:
		if allowed {
			sound = notify.SoundRequest
		}
		// Say what it asks, read from its screen; without a question to
		// quote, the notice says where it is.
		go func() {
			title, body := agent+" needs attention", where
			if screen, err := c.PaneScreen(ev.Pane); err == nil {
				if asked := asks.Read(screen.Text); asked != "" {
					title, body = agent+" needs attention · "+where, asked
				}
			}
			s.hold(ui.ToastAttention, title, body, ev.Pane, sound)
		}()
	case announce.Finished:
		if allowed {
			sound = notify.SoundDone
		}
		s.hold(ui.ToastFinished, agent+" finished", where, ev.Pane, sound)
	}
}

// hold keeps news for the delay the settings ask, then says it if it is
// still true (deliverDue).
func (s *Session) hold(kind, title, body string, pane uint64, sound notify.Sound) {
	now := time.Now()
	s.mu.Lock()
	d := s.notes.delay
	s.notes.held = append(s.notes.held, heldNotice{
		kind: kind, title: title, body: body, pane: pane, sound: sound,
		due: now.Add(d), expires: now.Add(max(d, announce.Grace)),
	})
	s.mu.Unlock()
}

// deliverDue says whatever held news has come due and is still true, and
// puts away cards that have been up long enough. It is called on a tick.
func (s *Session) deliverDue() {
	now := time.Now()
	s.mu.Lock()
	n := s.notes
	var due []heldNotice
	kept := n.held[:0]
	for _, h := range n.held {
		if now.Before(h.due) {
			kept = append(kept, h)
			continue
		}
		switch announce.Validate(h.kind, h.pane, &s.snap) {
		case announce.Current:
			due = append(due, h)
		case announce.Awaiting:
			if now.Before(h.expires) {
				h.due = now.Add(announce.RecheckEvery)
				kept = append(kept, h)
			}
		}
	}
	n.held = kept
	changed := false
	shown := n.shown[:0]
	for _, t := range n.shown {
		if now.Before(n.expiry[t.ID]) {
			shown = append(shown, t)
		} else {
			delete(n.expiry, t.ID)
			changed = true
		}
	}
	n.shown = shown
	toasts, player := n.toasts, n.player
	s.mu.Unlock()

	for _, h := range due {
		changed = true
		s.deliver(toasts, player, h)
	}
	if changed {
		s.stateChanged()
	}
}

func (s *Session) deliver(mode string, player *notify.Player, h heldNotice) {
	if mode != "off" {
		s.mu.Lock()
		n := s.notes
		n.nextToast++
		t := Toast{ID: n.nextToast, Kind: h.kind, Title: h.title, Body: h.body, Pane: h.pane}
		n.shown = append(n.shown, t)
		n.expiry[t.ID] = time.Now().Add(toastLife)
		s.mu.Unlock()
	}
	// The system's notification is for somebody looking elsewhere: in
	// "system" mode, as the TUI, and in the default mode while the window
	// is not the one in front — a desktop's notification is what a window
	// has where a terminal has its bell.
	s.mu.Lock()
	behind := !s.notes.windowFocused
	s.mu.Unlock()
	if mode == "system" || (mode == "tend" && behind) {
		go func() {
			if h.pane != 0 {
				err := s.systemNotify(h.title, h.body, func() {
					if s.Raise != nil {
						s.Raise()
					}
					_ = s.JumpToPane(h.pane)
				})
				if err == nil {
					return
				}
			}
			_ = notify.System(h.title, h.body)
		}()
	}
	player.Play(h.sound)
}

// systemNotify raises the system's notification; a test replaces it, so it
// does not raise real ones on the desktop it runs on.
func (s *Session) systemNotify(title, body string, clicked func()) error {
	s.mu.Lock()
	hook := s.notifyHook
	s.mu.Unlock()
	if hook != nil {
		return hook(title, body, clicked)
	}
	return notify.SystemWithClick(title, body, clicked)
}

// DismissToast puts a card away before its time.
func (s *Session) DismissToast(id int) {
	s.mu.Lock()
	n := s.notes
	for i, t := range n.shown {
		if t.ID == id {
			n.shown = append(n.shown[:i], n.shown[i+1:]...)
			delete(n.expiry, id)
			break
		}
	}
	s.mu.Unlock()
	s.stateChanged()
}

// runNotices ticks deliverDue until the session closes.
func (s *Session) runNotices(done <-chan struct{}) {
	t := time.NewTicker(announce.RecheckEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.deliverDue()
		}
	}
}
