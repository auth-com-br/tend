package main

import (
	"errors"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/tools"
)

// A tend server outlives the client that started it. Building a new binary and
// running it therefore leaves the old server answering, and everything works
// until the first thing the new client knows about and the old one does not.
//
// The client can see this at the handshake, so it says so there rather than
// letting the user find out when an action fails. A notice alone was not
// enough: it names a command in another terminal, which is one step further
// than most people will go while something is already in front of them. The
// restart is offered where the problem is, and taken only when asked for.

// staleServerOverlay is what the notice says, with the keys that answer it.
//
// Which half is behind decides what to offer. A server older than the client
// can be replaced from here, and the panes it holds are the price. A client
// older than the server cannot replace itself — this process is the old code
// — so the only honest advice is to leave and come back, and the panes are
// not at risk because the server is the half that holds them.
func staleServerOverlay(m mismatch, self, session string) []string {
	build := m.build
	if build == "" {
		// Old enough that it predates the field naming the build, which is
		// itself the answer to "how old".
		build = "an older build"
	}
	title := "This session runs an older server"
	switch {
	case m.serverAhead && m.clientAhead:
		title = "This client and this session have diverged"
	case m.serverAhead:
		title = "This session is newer than this client"
	}
	lines := []string{
		title,
		"",
		"server:  " + build,
		"client:  " + self,
		"",
	}

	if m.serverAhead {
		// This process is the old code and cannot replace itself. Restarting
		// the server would cost the panes and leave the client where it was.
		lines = append(lines,
			"Anything added since this client started is",
			"missing from it, however new the server is.",
			"",
			"Detach and run tend again to pick it up.",
			"",
			"ctrl+b d   detach",
			"enter      carry on with what this client has",
		)
		if m.clientAhead {
			lines = append(lines, "",
				"Each also has something the other lacks, so",
				"replacing the server alone will not settle it.")
		}
		return lines
	}
	if m.handoff {
		// A server that can hand its panes over costs nothing to replace, so
		// the warning about losing programs would be a false one.
		return append(lines,
			"Anything this client can do that the old server",
			"cannot will fail until it is replaced.",
			"",
			"r      replace it now: everything in it keeps running",
			"enter  keep it and carry on",
			"",
			"Elsewhere: "+handoffCommand(session),
		)
	}
	return append(lines,
		"Anything this client can do that the old server",
		"cannot will fail until it is replaced.",
		"",
		"r      restart it now: the layout and scrollback come",
		"       back, the programs running in it do not",
		"enter  keep it and carry on",
		"",
		"Elsewhere: "+restartCommand(session),
	)
}

// warnIfServerIsOlder puts the notice up when the session is being run by a
// binary other than this one.
func (t *tui) warnIfServerIsOlder() {
	m, ok := t.disagree()
	if !ok {
		return
	}
	t.showMismatch(m)
}

// showMismatch puts the notice up.
func (t *tui) showMismatch(m mismatch) {
	t.mu.Lock()
	t.staleServer = true
	t.canRestart = !m.serverAhead
	t.overlay = staleServerOverlay(m, version, t.session)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// mismatch is what the two halves disagree about.
type mismatch struct {
	build       string
	serverAhead bool
	clientAhead bool
	// handoff reports that the server can give its panes to a replacement,
	// which changes what replacing it costs from everything to nothing.
	handoff bool
}

// disagree compares this client with the server it is talking to.
//
// The builds are git descriptions with no ordering, so they say only that the
// two differ. Which of them is behind comes from what each knows how to do:
// a method this client uses and the server never heard of puts the server
// behind, and one the server offers and this client does not know puts the
// client behind. Both at once is divergence, and neither side is "old".
func (t *tui) disagree() (mismatch, bool) {
	hello := t.client.Server()
	serverAhead, clientAhead := proto.Compare(hello.Methods)
	featuresAhead, featuresBehind := proto.CompareFeatures(hello.Features)
	serverAhead = serverAhead || featuresAhead
	clientAhead = clientAhead || featuresBehind
	if !serverAhead && !clientAhead {
		return mismatch{}, false
	}
	return mismatch{
		build: hello.Build, serverAhead: serverAhead, clientAhead: clientAhead,
		handoff: hello.Handoff,
	}, true
}

// handoffCommand is the same replacement, from a shell.
var handoffCommand = tools.HandoffCommand

// staleServerKeys answers the notice. It reports whether it took the input.
func (t *tui) staleServerKeys(data []byte) (bool, error) {
	t.mu.Lock()
	up := t.staleServer
	t.mu.Unlock()
	if !up {
		return false, nil
	}

	t.mu.Lock()
	canRestart := t.canRestart
	t.mu.Unlock()

	for _, key := range splitKeys(data) {
		t.dismissStaleServer()
		if (key == "r" || key == "R") && canRestart {
			// Only offered when the server is the half behind. This process
			// cannot replace itself, and restarting the server it is already
			// behind would cost the panes for nothing.
			return true, t.restartServer()
		}
		// Every other key means "carry on", and is not passed through: the
		// notice is over the screen, so a keystroke aimed past it was aimed
		// at something the user could not see.
		return true, nil
	}
	return true, nil
}

func (t *tui) dismissStaleServer() {
	t.mu.Lock()
	t.staleServer = false
	t.overlay = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// restartServer stops the session's server so a new one takes its place.
//
// Nothing here starts the replacement. Shutting the old one down drops the
// connection, which is the same thing as a server dying for any other reason,
// and the reconnect that follows starts a fresh one exactly as the first
// attach did. One path, already the one that has to work.
func (t *tui) restartServer() error {
	if t.client.Server().Handoff {
		// The server gives its panes to a new process and hangs up, and the
		// reconnect finds that process on the same socket. A replacement that
		// fails to start leaves the old server as it was, so the failure is
		// something to say, not something to die of — and not a reason to fall
		// back to a restart nobody agreed to the cost of.
		t.setMessage("replacing the server", false)
		if err := t.client.Handoff(); err != nil {
			t.setMessage("the server could not be replaced: "+err.Error(), true)
		}
		return nil
	}
	t.setMessage("restarting the server", false)
	if err := t.client.Shutdown(); err != nil && !errors.Is(err, proto.ErrUnknownMethod) {
		return err
	}
	return nil
}

// staleServerCommand is the key binding that brings the notice back, for
// somebody who dismissed it and then hit the failure it was about.
func (t *tui) showStaleServerNotice() {
	m, ok := t.disagree()
	if !ok {
		// Nothing to compare found a difference, but a call was refused all
		// the same. Say what is known rather than nothing.
		m = mismatch{build: t.client.Server().Build}
	}
	t.showMismatch(m)
}

// reportStaleServer turns "the server has never heard of that" into the notice
// explaining why, and reports whether it handled the error.
//
// It is not fatal. The action did not happen, but everything else about the
// session still works, and closing the client over it would lose the user's
// place for a reason that had nothing to do with them.
func (t *tui) reportStaleServer(err error) bool {
	if !errors.Is(err, proto.ErrUnknownMethod) {
		return false
	}
	t.showStaleServerNotice()
	return true
}

// staleServerUp reports whether the notice has the keyboard.
func (t *tui) staleServerUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.staleServer
}
