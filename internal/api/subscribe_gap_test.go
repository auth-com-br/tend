package api

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
)

// TestAStreamMissesNothingAfterItsReply: what happens between the reply to
// events.subscribe and the stream starting comes down the stream, and a
// command sent between a browser's "attached" and its stream starting
// reaches it. Both used to be taken only once the stream started, after
// the reply: a pane made the moment the caller read "subscribed" was never
// told of — CI's runner found it now and then in
// TestSubscribingFollowsTheSessionWithoutGaps. Here the gap is made on
// purpose, by acting between the call and the stream. If it regresses, a
// caller following the session misses what it did first.
func TestAStreamMissesNothingAfterItsReply(t *testing.T) {
	h := start(t)
	a := New(h.srv, "test-build", []string{"/bin/sh"})

	var pend pending
	params, _ := json.Marshal(map[string]any{"kinds": []string{"pane.opened"}})
	if _, err := a.call(Request{ID: "1", Method: MethodEventsSubscribe, Params: params}, &pend); err != nil {
		t.Fatal(err)
	}
	// In the gap: the reply is the caller's, the stream not yet started.
	if _, err := h.srv.SplitPane(h.pane, session.Columns, server.PaneSpec{Command: []string{"/bin/sh", "-c", "sleep 30"}}); err != nil {
		t.Fatal(err)
	}
	if ev := streamLine(t, pend.stream); ev["event"] != "pane.created" {
		t.Errorf("the stream began with %v, not the pane made in the gap", ev)
	}

	var bpend pending
	bparams, _ := json.Marshal(map[string]any{"name": "stand-in"})
	if _, err := a.call(Request{ID: "2", Method: MethodBrowserAttach, Params: bparams}, &bpend); err != nil {
		t.Fatal(err)
	}
	if _, err := h.srv.SendBrowser(server.BrowserCommand{Action: "open", URL: "https://example.com/"}); err != nil {
		t.Fatalf("a page sent once the browser had its answer: %v", err)
	}
	if cmd := streamLine(t, bpend.stream); cmd["action"] != "open" {
		t.Errorf("the browser's stream began with %v", cmd)
	}
}

// streamLine starts a stream on one end of a pipe and reads its first line.
func streamLine(t *testing.T, stream func(net.Conn) error) map[string]any {
	t.Helper()
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = ours.Close(); _ = theirs.Close() })
	go func() { _ = stream(theirs) }()
	_ = ours.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := bufio.NewReader(ours).ReadBytes('\n')
	if err != nil {
		t.Fatalf("the stream said nothing: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(line, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
