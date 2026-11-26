package api

import (
	"encoding/json"
	"net"
	"time"

	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
)

// events.wait answers one event and returns; a caller that wants to follow the
// session calls it again, and whatever happened between the two calls is gone.
// That is enough for "wait until the agent stops" and not enough for a plugin
// or a dashboard, which needs everything in order.
//
// events.subscribe is the other shape: the reply says the stream has started,
// and every event after that goes down the same connection as its own line
// until the caller hangs up. herdr has both (`api/subscriptions.rs`).
//
// The connection becomes a stream, so nothing else can be asked on it. A
// caller that needs to act opens a second connection, which is how two scripts
// already share a session.

// MethodEventsSubscribe follows the session until the caller hangs up.
const MethodEventsSubscribe = "events.subscribe"

// subscribeBuffer is how many events are held for a slow reader. Past that the
// oldest go: a reader that cannot keep up with its own session is better off
// missing the middle than stopping the server's publisher.
const subscribeBuffer = 256

// streamWrite bounds one write to a subscriber, so a caller that stops reading
// cannot hold the goroutine that feeds it for ever.
const streamWrite = 10 * time.Second

// subscribe turns the connection into a stream of events, from sub.
//
// It returns when the caller hangs up or the server stops. The reply to the
// request itself has already been written by the caller; from here the
// connection carries events and nothing else.
//
// sub was made when the request was answered, not here: made after the
// reply, every event between the caller reading "subscribed" and this
// starting was lost — panes a caller made at once never came down the
// stream, which CI's runner found (TestSubscribingFollowsTheSessionWithout-
// Gaps). The stream is the session from the reply on, with no gap.
func (a *API) subscribe(conn net.Conn, sub *server.Subscription, kinds []string, pane string) error {
	defer sub.Close()
	var only session.PaneID
	if pane != "" {
		id, err := a.pane(pane)
		if err != nil {
			return err
		}
		only = id
	}
	want := wantedEvents(kinds)

	enc := json.NewEncoder(conn)
	for ev := range sub.C {
		if only != 0 && ev.Pane != only {
			continue
		}
		name := eventName(ev.Kind)
		if name == "" || len(want) > 0 && !want[name] {
			continue
		}
		out := map[string]any{"type": "event", "event": name}
		if ev.Pane != 0 {
			out["pane_id"] = PaneID(ev.Pane)
		}
		if ev.Tab != 0 {
			out["tab_id"] = TabID(ev.Tab)
		}
		if ev.Workspace != 0 {
			out["workspace_id"] = WorkspaceID(ev.Workspace)
		}
		if ev.Kind == server.EventPaneState {
			out["agent_state"] = ev.State.String()
			out["rule"] = ev.Rule
		}
		if ev.Err != "" {
			out["error"] = ev.Err
		}

		_ = conn.SetWriteDeadline(time.Now().Add(streamWrite))
		if err := enc.Encode(out); err != nil {
			// The caller has gone, or stopped reading. Either way this
			// stream is over; the session is not.
			return nil
		}
	}
	return nil
}
