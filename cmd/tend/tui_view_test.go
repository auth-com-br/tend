package main

import (
	"testing"

	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestASnapshotReadBeforeTheViewMovedDoesNotUndoIt: the view goes to a tab
// just made while a snapshot read a moment before — without the tab — is
// being applied. That snapshot is read again rather than applied. If it
// regresses, an agent started on an issue, an error or a ticket opens in a
// tab the screen does not go to: it stays on the space's first tab, as it
// did on CI's runner.
func TestASnapshotReadBeforeTheViewMovedDoesNotUndoIt(t *testing.T) {
	startRealSession(t, "view")
	path, err := transport.SocketPath("view")
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	tu := &tui{
		config: config.Defaults(), client: c, cols: 100, rows: 30,
		screens: map[uint64]*vt.Screen{}, sizes: map[uint64]ui.Rect{},
		wake: make(chan struct{}, 1), notices: map[uint64]paneNotice{},
	}
	if err := tu.refresh(); err != nil {
		t.Fatal(err)
	}
	ws, first := tu.workspace, tu.tab

	var made uint64
	snapshotRead = func() {
		snapshotRead = nil
		// The issue's agent, started while the snapshot was on its way.
		tab, _, err := c.NewTab(ws, "agent", proto.PaneSpec{Command: []string{"/bin/sh", "-c", "exec sleep 30"}})
		if err != nil {
			t.Error(err)
			return
		}
		made = tab
		tu.mu.Lock()
		tu.tab, tu.focus = tab, 0
		tu.mu.Unlock()
	}
	t.Cleanup(func() { snapshotRead = nil })
	_ = tu.refresh()

	if made == 0 {
		t.Fatal("no tab was made")
	}
	if tu.tab != made {
		t.Errorf("the view is on tab %d (the first was %d), not the new tab %d", tu.tab, first, made)
	}
}
