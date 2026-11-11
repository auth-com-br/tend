package main

import (
	"io"
	"os"
	"time"

	"github.com/auth-com-br/tend/internal/asks"
	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/notify"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/worktree"
)

// Being told that an agent needs you, which is the reason this program exists.
// The count on the status bar only reaches somebody already looking at tend;
// this reaches somebody reading something else.
//
// What is worth saying is herdr's rule (`app/actions.rs`,
// `notification_toast_for_state_change`): an agent that became blocked needs
// attention, and an agent that was working and went idle has finished.
// Anything else is a state change nobody asked to hear about. The pane being
// looked at says nothing, because being told what is on screen is noise.

// notifyCooldown is how long one pane is left alone after it was announced.
// An agent that flickers between states — and they do, while a tool runs —
// would otherwise raise a notification a second.
const notifyCooldown = 10 * time.Second

// paneNotice is what was last said about a pane.
type paneNotice struct {
	state detect.State
	at    time.Time
}

// announce decides whether a state change is worth telling the user about, and
// tells them. It runs on the event goroutine, so it does no round trips.
func (t *tui) announce(ev proto.Event) { t.announceFrom(nil, ev) }

// announceFrom is announce for a machine: the one shown when e is nil,
// another saved machine otherwise, whose agents are announced as herdr's
// client announces every endpoint's — never as the pane in view, and saying
// which machine it is on.
func (t *tui) announceFrom(e *endpoint, ev proto.Event) {
	if t.toasts == "off" && !t.sound.Enabled {
		return
	}
	state := detect.StateFromString(ev.State)
	if state != detect.StateBlocked && state != detect.StateIdle {
		return
	}

	t.mu.Lock()
	notices, snap, machine := t.notices, &t.snap, t.shownMachineLocked()
	// The pane in view is not announced — unless the window is behind
	// something else, when nobody is looking at it either (herdr's
	// active_tab_suppresses_notifications).
	focused := ev.Pane == t.focus && t.windowFocused
	if e != nil {
		notices, snap, machine, focused = e.notices, &e.snap, e.id, false
	}
	previous := notices[ev.Pane]
	kind, worth := worthAnnouncing(previous, state, time.Now())
	notices[ev.Pane] = paneNotice{state: state, at: time.Now()}

	var agent, where string
	for _, p := range snap.Panes {
		if p.ID == ev.Pane {
			agent = p.Agent
		}
	}
	if agent == "" {
		// The event is ahead of the session this client last read: an agent
		// whose first word is "I need you" would otherwise go unannounced.
		agent = ev.Agent
	}
	for _, w := range snap.Workspaces {
		for _, tab := range w.Tabs {
			for _, id := range tab.Panes {
				if id == ev.Pane {
					where = agentLabel(w, tab)
				}
			}
		}
	}
	if e != nil {
		where = e.label + " · " + where
	}
	notifyFocused := t.notifyFocused
	t.mu.Unlock()

	if agent == "" || !worth || (focused && !notifyFocused) {
		return
	}
	t.mu.Lock()
	t.lastNotice, t.lastNoticeMachine = ev.Pane, machine // for open-notification
	t.mu.Unlock()
	switch kind {
	case announceBlocked:
		if e != nil {
			t.raiseOn(machine, ui.ToastAttention, agent+" needs attention", where, ev.Pane, t.soundFor(agent, notify.SoundRequest))
			return
		}
		// Say what it asks, read from its screen — on a goroutine of its own,
		// since that is a call to the server and this is the goroutine its
		// events arrive on. Where it is moves to the title; without a
		// question to quote, the notice is as it always was.
		go func() {
			title, body := agent+" needs attention", where
			if screen, err := t.client.PaneScreen(ev.Pane); err == nil {
				if asked := asks.Read(screen.Text); asked != "" {
					title, body = agent+" needs attention · "+where, asked
				}
			}
			t.raiseOn(machine, ui.ToastAttention, title, body, ev.Pane, t.soundFor(agent, notify.SoundRequest))
		}()
	case announceFinished:
		t.raiseOn(machine, ui.ToastFinished, agent+" finished", where, ev.Pane, t.soundFor(agent, notify.SoundDone))
	}
}

// What an announcement is about.
const (
	announceNothing = iota
	announceBlocked
	announceFinished
)

// worthAnnouncing is herdr's rule, and the cooldown tend adds to it: an agent
// that became blocked needs attention; one that was working and went idle has
// finished; anything else is a state change nobody asked to hear about.
//
// The cooldown is not herdr's. An agent flickers between working and idle
// while a tool runs — the screen says one thing between two redraws — and
// without it the same agent finishes a dozen times a minute.
func worthAnnouncing(previous paneNotice, state detect.State, now time.Time) (kind int, ok bool) {
	switch {
	case state == detect.StateBlocked && previous.state != detect.StateBlocked:
		kind = announceBlocked
	case state == detect.StateIdle && previous.state == detect.StateWorking:
		kind = announceFinished
	default:
		return announceNothing, false
	}
	if !previous.at.IsZero() && now.Sub(previous.at) < notifyCooldown && previous.state == state {
		return kind, false
	}
	return kind, true
}

// raise says it, a moment from now: herdr's notification policy holds an
// agent's news for notify.delay (a second by default) and says it only if
// the pane is still in the state it announces — an agent answered in the
// meantime is not "needing attention" any more. What a script said goes at
// once.
func (t *tui) raise(kind, title, body string, pane uint64, sound notify.Sound) {
	t.mu.Lock()
	machine := t.shownMachineLocked()
	t.mu.Unlock()
	t.raiseOn(machine, kind, title, body, pane, sound)
}

// raiseOn is raise for news from a given machine, which is where its pane
// is looked for and where a click on its card goes.
func (t *tui) raiseOn(machine, kind, title, body string, pane uint64, sound notify.Sound) {
	now := time.Now()
	delay := time.Duration(0)
	if kind != ui.ToastCustom {
		delay = t.notifyDelay
	}
	t.mu.Lock()
	t.pendingNotices = append(t.pendingNotices, pendingNotice{
		kind: kind, title: title, body: body, pane: pane, sound: sound, machine: machine,
		due: now.Add(delay), expires: now.Add(max(delay, completionGrace)),
	})
	t.mu.Unlock()
	t.deliverDue()
}

// completionGrace is herdr's COMPLETION_EVIDENCE_GRACE: how long past its
// moment a notice waits for the session to show its pane in the state it
// announces, and recheckEvery how often it looks.
const (
	completionGrace = time.Second
	recheckEvery    = 50 * time.Millisecond
)

// pendingNotice is news held until its moment.
type pendingNotice struct {
	kind, title, body string
	pane              uint64
	// machine is the saved machine the pane is on, empty without any.
	machine      string
	sound        notify.Sound
	due, expires time.Time
}

// noticeCheck is herdr's NotificationValidation.
type noticeCheck int

const (
	noticeCurrent noticeCheck = iota
	noticeAwaiting
	noticeStale
)

// checkNoticeLocked is herdr's notification_validation: attention is
// current while the pane is still blocked; finished while it is still done,
// and awaited while it still shows working; anything else is stale. What
// is not about an agent is always current.
func (t *tui) checkNoticeLocked(n pendingNotice) noticeCheck {
	if n.kind == ui.ToastCustom {
		return noticeCurrent
	}
	if n.pane == 0 {
		if n.kind == ui.ToastFinished {
			return noticeStale
		}
		return noticeCurrent
	}
	snap := t.snapOfLocked(n.machine)
	if snap == nil {
		return noticeAwaiting // its machine has not been read yet
	}
	for _, p := range snap.Panes {
		if p.ID != n.pane {
			continue
		}
		switch {
		case n.kind == ui.ToastAttention && p.State == "blocked":
			return noticeCurrent
		case n.kind == ui.ToastFinished && p.Done:
			return noticeCurrent
		case n.kind == ui.ToastFinished && p.State == "working":
			return noticeAwaiting
		}
		return noticeStale
	}
	return noticeAwaiting
}

// deliverDue says whatever held news has come due and is still true.
func (t *tui) deliverDue() {
	now := time.Now()
	t.mu.Lock()
	var due []pendingNotice
	kept := t.pendingNotices[:0]
	for _, n := range t.pendingNotices {
		if now.Before(n.due) {
			kept = append(kept, n)
			continue
		}
		switch t.checkNoticeLocked(n) {
		case noticeCurrent:
			due = append(due, n)
		case noticeAwaiting:
			if now.Before(n.expires) {
				n.due = now.Add(recheckEvery)
				kept = append(kept, n)
			}
		}
	}
	t.pendingNotices = kept
	t.mu.Unlock()
	for _, n := range due {
		t.deliver(n.machine, n.kind, n.title, n.body, n.pane, n.sound)
	}
}

// deliver says it, by whichever means are turned on. kind and pane are for
// tend's own card: what colour its dot is, and where a click on it goes.
func (t *tui) deliver(machine, kind, title, body string, pane uint64, sound notify.Sound) {
	if t.toasts != "off" {
		// tend's own card, herdr's "herdr" delivery, for every setting but
		// off: the terminal's or the desktop's notification is for somebody
		// looking elsewhere, and this is for somebody looking here.
		t.pushToastOn(machine, kind, title, body, pane)
	}
	if t.toasts == "system" {
		// On its own goroutine: raising one runs another program, and the
		// event goroutine is the one delivering pane output.
		go func() {
			if err := notify.System(title, body); err != nil {
				t.setMessage("notification: "+err.Error(), true)
			}
		}()
	}
	if t.toasts == "terminal" && t.notifier.Available() {
		if seq := t.notifier.Sequence(title, body); len(seq) > 0 {
			// Straight to the terminal rather than through the frame: it is
			// not drawing, it is asking the terminal for something, and the
			// next redraw must not repeat it.
			_, _ = os.Stdout.Write(seq)
		}
	}
	t.sound.Play(sound)
}

// soundFor is the sound an agent's change makes, or none when the settings
// mute that agent ([sound.agents], herdr's per-agent override).
func (t *tui) soundFor(agent string, sound notify.Sound) notify.Sound {
	t.mu.Lock()
	allowed := t.config.Sound.AllowsAgent(agent)
	t.mu.Unlock()
	if !allowed {
		return notify.SoundNone
	}
	return sound
}

// expandHome turns a leading ~ into the home directory, so a sound can be
// named the way it is written down.
func expandHome(path string) string { return worktree.ExpandHome(path) }

// bell rings the terminal's bell, which is the sound tend has when no file is
// configured.
func bell() { _, _ = io.WriteString(os.Stdout, "\a") }
