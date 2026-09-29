//go:build unix

package client

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/transport"
)

// recorder collects what the server pushes, so a test can wait for it without
// racing the reader goroutine.
type recorder struct {
	mu           sync.Mutex
	events       []proto.Event
	screens      map[uint64]string
	disconnected bool
}

func newRecorder() *recorder {
	return &recorder{screens: make(map[uint64]string)}
}

func (r *recorder) Event(ev proto.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) PaneOutput(pane uint64, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// The data is only valid for the call, so it is copied.
	r.screens[pane] = string(data)
}

func (r *recorder) Disconnected() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disconnected = true
}

func (r *recorder) wasDisconnected() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disconnected
}

func (r *recorder) eventsOfKind(kind string) []proto.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []proto.Event
	for _, ev := range r.events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func (r *recorder) screen(pane uint64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.screens[pane]
}

// harness is a real server on a real socket with a real client.
type harness struct {
	srv    *server.Server
	client *Client
	rec    *recorder
	path   string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	// A private runtime directory per test, so a test never touches a real
	// session or another test's.
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())

	path, err := transport.SocketPath("test")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	srv, err := server.New(server.Config{
		DetectInterval: 10 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(ln)
	}()

	rec := newRecorder()
	c, err := Dial(path, rec)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	t.Cleanup(func() {
		_ = c.Close()
		_ = srv.Close()
		_ = ln.Close()
		<-serveDone
	})
	return &harness{srv: srv, client: c, rec: rec, path: path}
}

// waitFor polls until cond holds. Every wait is bounded: a stalled server must
// fail a test rather than hang the suite.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func shellSpec(script string) proto.PaneSpec {
	return proto.PaneSpec{Command: []string{"/bin/sh", "-c", script}}
}

// --- handshake -------------------------------------------------------------

func TestHandshake(t *testing.T) {
	h := newHarness(t)

	info := h.client.Server()
	if info.Version != proto.Version {
		t.Errorf("server version = %d, want %d", info.Version, proto.Version)
	}
	if len(info.Methods) == 0 {
		t.Fatal("the server should advertise its methods")
	}
	if !h.client.Supports(proto.MethodPaneSplit) {
		t.Error("pane.split should be advertised")
	}
	if h.client.Supports("pane.teleport") {
		t.Error("a method the server does not implement must not be advertised")
	}
}

// TestVersionMismatchIsRejected: a mismatch must be reported as itself rather
// than turning up later as a message the other side cannot parse.
func TestVersionMismatchIsRejected(t *testing.T) {
	h := newHarness(t)

	nc, err := transport.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()

	raw := &Client{conn: proto.NewConn(nc), handler: nopHandler{}, pending: map[uint64]chan proto.Response{}}
	raw.closeWG.Add(1)
	go raw.readLoop()
	defer raw.Close()

	var out proto.HelloResult
	err = raw.Call(proto.MethodHello, proto.HelloParams{Version: proto.Version + 99}, &out)
	if err == nil {
		t.Fatal("a version mismatch should be rejected")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("err = %v, want it to name the version problem", err)
	}
}

// TestUnknownMethodIsAnErrorNotADisconnect is the rule that keeps a missing
// feature from taking the whole session down.
func TestUnknownMethodIsAnErrorNotADisconnect(t *testing.T) {
	h := newHarness(t)

	err := h.client.Call("pane.teleport", nil, nil)
	if err == nil {
		t.Fatal("an unknown method should fail")
	}
	// Recognisable as a kind rather than as a sentence: a caller needs to
	// tell "this cannot be done" apart from "the server on the other end is
	// older than you are", and those read the same in a raw error.
	if !errors.Is(err, proto.ErrUnknownMethod) {
		t.Errorf("err = %v, want it to wrap ErrUnknownMethod", err)
	}
	if !strings.Contains(err.Error(), "pane.teleport") {
		t.Errorf("err = %v, want it to name the method", err)
	}

	// A method the server does know is not mistaken for one it does not.
	if _, err := h.client.SplitPane(999, "columns", proto.PaneSpec{}); errors.Is(err, proto.ErrUnknownMethod) {
		t.Errorf("a failing known method should not look unsupported: %v", err)
	}

	// The connection must still work.
	if _, err := h.client.Snapshot(); err != nil {
		t.Errorf("the connection should survive an unknown method: %v", err)
	}
}

// --- session operations ----------------------------------------------------

func TestOpenAndInspectASession(t *testing.T) {
	h := newHarness(t)

	ws, err := h.client.NewWorkspace("main")
	if err != nil {
		t.Fatal(err)
	}
	tab, pane, err := h.client.NewTab(ws, "shell", shellSpec("printf hello; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}

	snap, err := h.client.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Workspaces) != 1 || snap.Workspaces[0].ID != ws {
		t.Fatalf("workspaces = %+v", snap.Workspaces)
	}
	if len(snap.Workspaces[0].Tabs) != 1 || snap.Workspaces[0].Tabs[0].ID != tab {
		t.Fatalf("tabs = %+v", snap.Workspaces[0].Tabs)
	}
	if len(snap.Panes) != 1 || snap.Panes[0].ID != pane {
		t.Fatalf("panes = %+v", snap.Panes)
	}
	if !snap.Panes[0].Running || snap.Panes[0].Pid == 0 {
		t.Errorf("pane = %+v, want it running with a pid", snap.Panes[0])
	}

	waitFor(t, "the pane's output", func() bool {
		scr, err := h.client.PaneScreen(pane)
		return err == nil && strings.Contains(scr.Text, "hello")
	})
}

func TestSplitAndClosePanes(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, first, err := h.client.NewTab(ws, "t", shellSpec("sleep 10"))
	if err != nil {
		t.Fatal(err)
	}

	second, err := h.client.SplitPane(first, "rows", shellSpec("sleep 10"))
	if err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	snap, _ := h.client.Snapshot()
	if len(snap.Panes) != 2 {
		t.Fatalf("got %d panes, want 2", len(snap.Panes))
	}

	if err := h.client.ClosePane(second); err != nil {
		t.Fatalf("ClosePane: %v", err)
	}
	waitFor(t, "the pane to leave the snapshot", func() bool {
		snap, err := h.client.Snapshot()
		return err == nil && len(snap.Panes) == 1
	})
}

func TestSplitRejectsAnUnknownDirection(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, pane, _ := h.client.NewTab(ws, "t", shellSpec("sleep 10"))

	_, err := h.client.SplitPane(pane, "diagonally", shellSpec("true"))
	if err == nil {
		t.Fatal("an unknown direction should be rejected")
	}
	if !strings.Contains(err.Error(), "direction") {
		t.Errorf("err = %v", err)
	}
}

func TestServerErrorsReachTheCaller(t *testing.T) {
	h := newHarness(t)
	if err := h.client.ClosePane(9999); err == nil {
		t.Error("closing a pane that does not exist should fail")
	}
	ws, _ := h.client.NewWorkspace("main")
	if _, _, err := h.client.NewTab(ws, "t", proto.PaneSpec{Command: []string{"/nonexistent"}}); err == nil {
		t.Error("starting a missing command should fail")
	}
}

// TestARefusalIsTellableFromABrokenConnection: what the server refuses comes
// back as a Refusal carrying the server's reason, so the client can say it
// and go on. If it regresses, a folder typed a few letters short closes tend
// ("tend: group.set_dir: no such folder: …"), as it did.
func TestARefusalIsTellableFromABrokenConnection(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	if err := h.client.GroupWorkspace(ws, "clients"); err != nil {
		t.Fatal(err)
	}
	err := h.client.SetGroupDir("clients", filepath.Join(t.TempDir(), "retro-stud"))
	var refused *Refusal
	if !errors.As(err, &refused) || refused.Method != proto.MethodGroupSetDir || !strings.Contains(refused.Message, "no such folder") {
		t.Errorf("err = %#v, want a Refusal saying no such folder", err)
	}
	if _, err := h.client.Snapshot(); err != nil {
		t.Errorf("the connection did not survive the refusal: %v", err)
	}
}

// --- input and output ------------------------------------------------------

func TestInputReachesThePane(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, pane, err := h.client.NewTab(ws, "t", shellSpec(`read line; printf 'got:%s' "$line"; sleep 10`))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.client.SendInput(pane, []byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane to echo the input", func() bool {
		scr, err := h.client.PaneScreen(pane)
		return err == nil && strings.Contains(scr.Text, "got:ping")
	})
}

// TestSubscribedPanesStreamTheirScreens covers the push path: a subscribed
// pane's screen arrives without being asked for.
func TestSubscribedPanesStreamTheirScreens(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, pane, err := h.client.NewTab(ws, "t", shellSpec("printf streamed; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.client.SubscribePanes([]uint64{pane}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a pushed screen", func() bool {
		return strings.Contains(h.rec.screen(pane), "streamed")
	})
}

// TestUnsubscribedPanesAreSilent: a client showing one tab must not be sent
// the others, or its traffic would scale with the whole session.
func TestUnsubscribedPanesAreSilent(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, watched, err := h.client.NewTab(ws, "t", shellSpec("printf watched; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}
	ignored, err := h.client.SplitPane(watched, "columns", shellSpec("printf ignored; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.client.SubscribePanes([]uint64{watched}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the watched pane", func() bool {
		return strings.Contains(h.rec.screen(watched), "watched")
	})
	if got := h.rec.screen(ignored); got != "" {
		t.Errorf("an unsubscribed pane sent %q", got)
	}
}

// TestSubscribeReplacesTheSet: switching tabs is one message, not an
// unsubscribe and a subscribe with a gap between them.
func TestSubscribeReplacesTheSet(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, first, err := h.client.NewTab(ws, "t", shellSpec("printf first; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.client.SplitPane(first, "columns", shellSpec("printf second; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}

	if err := h.client.SubscribePanes([]uint64{first}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first pane", func() bool {
		return strings.Contains(h.rec.screen(first), "first")
	})

	if err := h.client.SubscribePanes([]uint64{second}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the second pane", func() bool {
		return strings.Contains(h.rec.screen(second), "second")
	})
}

// --- events ----------------------------------------------------------------

func TestEventsReachTheClient(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")

	_, pane, err := h.client.NewTab(ws, "agent", proto.PaneSpec{
		Command: []string{"/bin/sh", "-c", `printf '\033]0;\342\240\201 Thinking\007'; sleep 10`},
		Agent:   "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "a pane-opened event", func() bool {
		for _, ev := range h.rec.eventsOfKind(proto.EventPaneOpened) {
			if ev.Pane == pane {
				return true
			}
		}
		return false
	})
	waitFor(t, "a working state event", func() bool {
		for _, ev := range h.rec.eventsOfKind(proto.EventPaneState) {
			if ev.Pane == pane && ev.State == "working" && ev.Rule != "" {
				return true
			}
		}
		return false
	})
}

func TestExitEventCarriesTheError(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, pane, err := h.client.NewTab(ws, "t", shellSpec("exit 5"))
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "an exit event", func() bool {
		for _, ev := range h.rec.eventsOfKind(proto.EventPaneExited) {
			if ev.Pane == pane && ev.Err != "" {
				return true
			}
		}
		return false
	})
}

// --- connection lifetime ---------------------------------------------------

// TestPanesOutliveTheClient is the whole point of the socket: a client going
// away must not take the agents with it.
func TestPanesOutliveTheClient(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	_, pane, err := h.client.NewTab(ws, "t", shellSpec("printf alive; sleep 10"))
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pane to start", func() bool {
		scr, err := h.client.PaneScreen(pane)
		return err == nil && strings.Contains(scr.Text, "alive")
	})

	if err := h.client.Close(); err != nil {
		t.Fatal(err)
	}

	// Reconnect and find everything as it was.
	rec := newRecorder()
	again, err := Dial(h.path, rec)
	if err != nil {
		t.Fatalf("reconnecting: %v", err)
	}
	defer again.Close()

	snap, err := again.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 1 || snap.Panes[0].ID != pane || !snap.Panes[0].Running {
		t.Fatalf("after reconnecting, panes = %+v", snap.Panes)
	}
	scr, err := again.PaneScreen(pane)
	if err != nil || !strings.Contains(scr.Text, "alive") {
		t.Errorf("screen = %q, err = %v", scr.Text, err)
	}
}

func TestSeveralClientsShareTheSession(t *testing.T) {
	h := newHarness(t)

	second, err := Dial(h.path, newRecorder())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	ws, _ := h.client.NewWorkspace("main")
	_, pane, err := h.client.NewTab(ws, "t", shellSpec("sleep 10"))
	if err != nil {
		t.Fatal(err)
	}

	snap, err := second.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Panes) != 1 || snap.Panes[0].ID != pane {
		t.Errorf("the second client sees %+v", snap.Panes)
	}
}

func TestCallsFailAfterClose(t *testing.T) {
	h := newHarness(t)
	if err := h.client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.client.Snapshot(); !errors.Is(err, ErrClosed) {
		t.Errorf("err = %v, want ErrClosed", err)
	}
}

// TestPendingCallsFailWhenTheServerGoesAway: a caller must learn the server is
// gone rather than waiting out the request timeout.
func TestPendingCallsFailWhenTheServerGoesAway(t *testing.T) {
	h := newHarness(t)

	done := make(chan error, 1)
	go func() {
		_, err := h.client.Snapshot()
		done <- err
	}()
	// Give the call a moment to be in flight, then cut the server off.
	time.Sleep(20 * time.Millisecond)
	_ = h.srv.Close()

	select {
	case <-done:
		// Either an answer or a failure is fine; hanging is not.
	case <-time.After(5 * time.Second):
		t.Fatal("a pending call did not finish after the server went away")
	}
}

// --- transport -------------------------------------------------------------

func TestListenRefusesALiveSocket(t *testing.T) {
	h := newHarness(t)
	if _, err := transport.Listen(h.path); !errors.Is(err, transport.ErrAlreadyRunning) {
		t.Errorf("err = %v, want ErrAlreadyRunning", err)
	}
}

// TestListenReplacesAStaleSocket: a socket left by a crashed server must not
// block every later start.
func TestListenReplacesAStaleSocket(t *testing.T) {
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())
	path, err := transport.SocketPath("stale")
	if err != nil {
		t.Fatal(err)
	}

	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	// Close the listener but leave the file behind, which is what a crashed
	// server looks like from the outside.
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the socket file should still exist: %v", err)
	}

	again, err := transport.Listen(path)
	if err != nil {
		t.Fatalf("a stale socket should be replaced: %v", err)
	}
	_ = again.Close()
}

// TestShutdownAnswersBeforeClosing pins an ordering that is easy to get
// backwards.
//
// Shutting down closes every client connection, including the one that asked
// for it. If the shutdown runs before the reply is written, the caller sees
// its connection drop and cannot tell success from a crash — so the reply goes
// out first, and only then does the server stop.
func TestShutdownAnswersBeforeClosing(t *testing.T) {
	h := newHarness(t)
	ws, _ := h.client.NewWorkspace("main")
	if _, _, err := h.client.NewTab(ws, "t", shellSpec("sleep 30")); err != nil {
		t.Fatal(err)
	}

	if err := h.client.Shutdown(); err != nil {
		t.Fatalf("Shutdown should succeed, not fail with a dropped connection: %v", err)
	}

	// And it must actually stop: a later call has nothing to talk to.
	waitFor(t, "the server to stop", func() bool {
		_, err := h.client.Snapshot()
		return err != nil
	})
}

// TestServeReturnsWhenTheServerStops: Accept only unblocks when the listener
// closes, so a server stopped over the socket must reach its own listener or
// the process never exits.
func TestServeReturnsWhenTheServerStops(t *testing.T) {
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())
	path, err := transport.SocketPath("standalone")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	srv, err := server.New(server.Config{DetectInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	c, err := Dial(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("Serve returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the server stopped")
	}
}

// TestDisconnectIsReported: a client has to learn that the session is gone, or
// it goes on drawing a screen that nothing is behind.
func TestDisconnectIsReported(t *testing.T) {
	h := newHarness(t)
	if _, err := h.client.Snapshot(); err != nil {
		t.Fatal(err)
	}

	_ = h.srv.Close()

	waitFor(t, "the disconnect to be reported", func() bool {
		return h.rec.wasDisconnected()
	})
}

// TestListenTellsARefusalFromATimeout is the bug this was found by: the probe
// gave a live server 250ms to answer and read anything slower as "nothing is
// there", so a loaded machine would delete a running server's socket and bind
// its own. Two servers on one session, and the first one's clients left
// talking to a file nobody reads.
func TestListenTellsARefusalFromATimeout(t *testing.T) {
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())
	path, err := transport.SocketPath("probe")
	if err != nil {
		t.Fatal(err)
	}

	// A listener that never accepts is exactly what a busy server looks like.
	// The connection still completes, because the kernel queues it, so this
	// must read as live rather than as stale.
	ln, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if _, err := transport.Listen(path); !errors.Is(err, transport.ErrAlreadyRunning) {
		t.Errorf("err = %v, want ErrAlreadyRunning", err)
	}
	// And the socket it refused is still there for the server that holds it.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the live socket should have been left alone: %v", err)
	}
}
