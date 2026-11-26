// Package announce decides what is worth telling the user about an agent,
// and whether news held for a moment is still true when its moment comes.
// It says nothing itself: the TUI shows a card, rings or asks the desktop
// to notify; tend-desktop raises a native notification. Both decide the
// same way because they decide here.
//
// It was cmd/tend's (notify.go) until the desktop needed it.
//
// What is worth saying is herdr's rule (`app/actions.rs`,
// `notification_toast_for_state_change`): an agent that became blocked needs
// attention, and an agent that was working and went idle has finished.
// Anything else is a state change nobody asked to hear about.
package announce

import (
	"time"

	"github.com/auth-com-br/tend/internal/detect"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// Cooldown is how long one pane is left alone after it was announced. An
// agent that flickers between states — and they do, while a tool runs —
// would otherwise raise a notification a second.
const Cooldown = 10 * time.Second

// Grace is herdr's COMPLETION_EVIDENCE_GRACE: how long past its moment a
// notice waits for the session to show its pane in the state it announces,
// and RecheckEvery how often it looks.
const (
	Grace        = time.Second
	RecheckEvery = 50 * time.Millisecond
)

// Kind is what an announcement is about.
type Kind int

const (
	Nothing Kind = iota
	Blocked
	Finished
)

// Notice is what was last said about a pane.
type Notice struct {
	State detect.State
	At    time.Time
}

// Worth is herdr's rule, and the cooldown tend adds to it: an agent that
// became blocked needs attention; one that was working and went idle has
// finished; anything else is a state change nobody asked to hear about.
//
// The cooldown is not herdr's. An agent flickers between working and idle
// while a tool runs — the screen says one thing between two redraws — and
// without it the same agent finishes a dozen times a minute.
func Worth(previous Notice, state detect.State, now time.Time) (kind Kind, ok bool) {
	switch {
	case state == detect.StateBlocked && previous.State != detect.StateBlocked:
		kind = Blocked
	case state == detect.StateIdle && previous.State == detect.StateWorking:
		kind = Finished
	default:
		return Nothing, false
	}
	if !previous.At.IsZero() && now.Sub(previous.At) < Cooldown && previous.State == state {
		return kind, false
	}
	return kind, true
}

// Check is herdr's NotificationValidation.
type Check int

const (
	Current Check = iota
	Awaiting
	Stale
)

// Validate is herdr's notification_validation for a notice held about a
// pane: attention is current while the pane is still blocked; finished
// while it is still done, and awaited while it still shows working;
// anything else is stale. What is not about an agent (a toast kind of
// ui.ToastCustom) is always current. snap is the session the pane is in, nil
// when it has not been read yet.
func Validate(kind string, pane uint64, snap *proto.SessionSnapshot) Check {
	if kind == ui.ToastCustom {
		return Current
	}
	if pane == 0 {
		if kind == ui.ToastFinished {
			return Stale
		}
		return Current
	}
	if snap == nil {
		return Awaiting // its machine has not been read yet
	}
	for _, p := range snap.Panes {
		if p.ID != pane {
			continue
		}
		switch {
		case kind == ui.ToastAttention && p.State == "blocked":
			return Current
		case kind == ui.ToastFinished && p.Done:
			return Current
		case kind == ui.ToastFinished && p.State == "working":
			return Awaiting
		}
		return Stale
	}
	return Awaiting
}
