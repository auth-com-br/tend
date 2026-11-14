package main

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/machines"
	"github.com/auth-com-br/tend/internal/notify"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// Saved machines in the sidebar: herdr's multi-endpoint client
// (client/shell/endpoint_sidebar.rs, endpoint_navigation.rs).
//
// Once there is a machine saved (`tend machine add`), the space list becomes a
// list of machines — this one first, as "Local" — each with its spaces beneath
// it. Every machine other than the one being shown is watched over its own
// connection, which asks for events and never for a pane's screen, so its
// spaces and what its agents are doing stay current at the cost of a snapshot
// now and then. Going to one of its spaces hands that connection to the
// client: the switch is immediate, as herdr's is, because the machine was
// already connected.

// endpoint is one machine the client can be on.
type endpoint struct {
	// id is "local" for this machine and the catalog's id for a saved one.
	id, label string
	// host and session are where it is, as openSessionOn takes them.
	host, session string
	enabled       bool

	// Under t.mu.
	status string
	snap   proto.SessionSnapshot
	have   bool
	// client is the watching connection, nil while there is none. It is
	// also the client's own connection while the machine is shown through
	// it, which link.forward says.
	client *client.Client
	link   *endpointLink
	// notices is what was last announced of each of its panes, as the
	// client's own record is for the machine shown.
	notices map[uint64]paneNotice
	// stop is closed to stop its watcher, and nil while none runs.
	stop chan struct{}
	// unsaved marks the shown machine when it is not in the catalog —
	// reached with -ssh, or removed while in view — which leaves the list
	// once the client leaves it.
	unsaved bool
}

// localEndpoint is this machine's id, herdr's ClientEndpointId::Local.
const localEndpoint = "local"

// endpointLink is a watching connection's handler. While the machine is only
// watched it keeps the machine's snapshot fresh; once the client is shown
// through it, forward is set and everything goes to the client as it would
// from a connection the client had opened itself.
type endpointLink struct {
	t       *tui
	e       *endpoint
	forward atomic.Bool
	lost    chan struct{}
	// fetching coalesces snapshot reads: a burst of events is one read.
	fetching atomic.Bool
}

func (l *endpointLink) Disconnected() {
	close(l.lost)
	if l.forward.Load() {
		l.t.Disconnected()
	}
}

func (l *endpointLink) Event(ev proto.Event) {
	if l.forward.Load() {
		l.t.Event(ev)
		return
	}
	switch ev.Kind {
	case proto.EventPaneClipboard, proto.EventFocusRequest, proto.EventScrollRequest, proto.EventPaneFocused,
		proto.EventTabFocused, proto.EventWorkspaceFocused:
		// Nothing a list of spaces shows.
		return
	case proto.EventPaneState:
		// Its agents are announced as the shown machine's are: herdr's
		// client takes notifications from every endpoint.
		l.t.announceFrom(l.e, ev)
	case proto.EventNotify:
		l.t.mu.Lock()
		label := l.e.label
		l.t.mu.Unlock()
		l.t.raiseOn(l.e.id, ui.ToastCustom, ev.Title, label+" · "+ev.Body, ev.Pane, notify.SoundRequest)
	}
	l.fetch()
}

func (l *endpointLink) PaneOutput(pane uint64, data []byte) {
	if l.forward.Load() {
		l.t.PaneOutput(pane, data)
	}
}

// fetch reads the machine's snapshot off the reader goroutine — the reply to
// the call comes back through it.
func (l *endpointLink) fetch() {
	if !l.fetching.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer l.fetching.Store(false)
		l.t.mu.Lock()
		c := l.e.client
		l.t.mu.Unlock()
		if c == nil {
			return
		}
		snap, err := c.Snapshot()
		if err != nil {
			return
		}
		l.t.mu.Lock()
		l.e.snap, l.e.have = snap, true
		l.t.dirty = true
		l.t.mu.Unlock()
		l.t.wakeUp()
	}()
}

// machinesState is the client's list of machines.
type machinesState struct {
	endpoints []*endpoint
	// active is the id of the machine being shown.
	active string
	// folded are the machines whose spaces are put away, and remoteFolded
	// the groups folded on machines other than this one's session: a fold is
	// what one person is looking at, kept in the client as herdr keeps it.
	folded       map[string]bool
	remoteFolded map[string]map[string]bool
	stop         chan struct{}
	wg           sync.WaitGroup
}

// attachedEndpoint is the id of a machine the client was attached to with
// -ssh that is not saved.
const attachedEndpoint = "attached"

// loadMachines reads the saved machines, and then reads them again every
// second for as long as the client runs, as herdr's client does
// (catalog_reload.rs): `tend machine add` in another terminal shows up here
// without a restart. Without any saved, nothing changes: the sidebar is the
// space list it always was.
func (t *tui) loadMachines() {
	catalog, err := machines.Load()
	if err != nil {
		t.setMessage(err.Error(), true)
	} else {
		t.applyCatalog(catalog)
	}
	t.catalogStop = make(chan struct{})
	t.catalogDone = make(chan struct{})
	go t.watchCatalog(catalog.Machines, t.catalogStop, t.catalogDone)
}

// watchCatalog reads the catalog once a second and applies it when it
// changed. A file that does not read is left alone rather than taken as no
// machines: a half-written edit must not disconnect everything.
func (t *tui) watchCatalog(previous []machines.Machine, stop, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		catalog, err := machines.Load()
		if err != nil || slices.Equal(catalog.Machines, previous) {
			continue
		}
		previous = catalog.Machines
		t.applyCatalog(catalog)
	}
}

// newEndpoint is a saved machine as the client keeps it.
func newEndpoint(m machines.Machine) *endpoint {
	e := &endpoint{id: m.ID, label: m.Label, host: m.Target, session: m.Session, enabled: m.Enabled, status: ui.MachineDisabled, notices: map[uint64]paneNotice{}}
	if m.Enabled {
		e.status = ui.MachineConnecting
	}
	return e
}

// applyCatalog brings the list in line with the saved machines, herdr's
// apply_profiles: a machine removed, or pointed somewhere else, stops being
// watched and leaves the list; one added is watched; one turned off stops
// being watched and stays, dimmed; a new label is shown at once.
//
// The machine being shown is not taken away from under the client, where
// herdr moves it to Local: it stays in the list until the client leaves it,
// since what is on screen is a session the user is working in.
func (t *tui) applyCatalog(catalog machines.Catalog) {
	t.mu.Lock()
	ms := t.machines
	if ms == nil {
		if len(catalog.Machines) == 0 {
			t.mu.Unlock()
			return
		}
		ms = t.firstMachinesLocked(catalog)
		var watch []*endpoint
		for _, e := range ms.endpoints {
			if e.id != ms.active && e.enabled {
				watch = append(watch, e)
			}
		}
		t.dirty = true
		t.mu.Unlock()
		for _, e := range watch {
			t.watchEndpoint(ms, e)
		}
		t.wakeUp()
		return
	}

	saved := make(map[string]machines.Machine, len(catalog.Machines))
	for _, m := range catalog.Machines {
		saved[m.ID] = m
	}
	var closing []*client.Client
	var watch []*endpoint
	kept := make(map[string]*endpoint)
	var local, extra []*endpoint
	for _, e := range ms.endpoints {
		m, ok := saved[e.id]
		switch {
		case e.id == localEndpoint:
			local = append(local, e)
			continue
		case e.id == ms.active:
			if ok && m.Target == e.host && m.Session == e.session {
				e.label, e.enabled, e.unsaved = m.Label, m.Enabled, false
				kept[e.id] = e
			} else {
				e.unsaved = true
				extra = append(extra, e)
			}
			continue
		case e.id == attachedEndpoint:
			// Only ever the shown machine, which the case above kept; left,
			// it is gone.
			if c := t.retireLocked(e); c != nil {
				closing = append(closing, c)
			}
			continue
		case !ok || m.Target != e.host || m.Session != e.session:
			if c := t.retireLocked(e); c != nil {
				closing = append(closing, c)
			}
			continue
		}
		e.label = m.Label
		if m.Enabled != e.enabled {
			e.enabled = m.Enabled
			if m.Enabled {
				e.status = ui.MachineConnecting
				watch = append(watch, e)
			} else {
				if c := t.retireLocked(e); c != nil {
					closing = append(closing, c)
				}
				e.status = ui.MachineDisabled
			}
		}
		kept[e.id] = e
	}
	list := local
	for _, m := range catalog.Machines {
		e := kept[m.ID]
		if e == nil {
			e = newEndpoint(m)
			if e.enabled {
				watch = append(watch, e)
			}
		}
		list = append(list, e)
	}
	ms.endpoints = append(list, extra...)
	t.dirty = true
	t.mu.Unlock()

	for _, c := range closing {
		_ = c.Close()
	}
	for _, e := range watch {
		t.watchEndpoint(ms, e)
	}
	t.wakeUp()
}

// firstMachinesLocked is the list when the first machine is saved: this one,
// then the saved ones, the shown one marked.
func (t *tui) firstMachinesLocked(catalog machines.Catalog) *machinesState {
	local := &endpoint{id: localEndpoint, label: "Local", session: transport.DefaultSessionName, enabled: true, status: ui.MachineOnline, notices: map[uint64]paneNotice{}}
	if t.host == "" {
		local.session = t.session
	}
	list := []*endpoint{local}
	active := ""
	if t.host == "" {
		active = localEndpoint
	}
	for _, m := range catalog.Machines {
		e := newEndpoint(m)
		if active == "" && m.Target == t.host && m.Session == t.session {
			active, e.enabled, e.status = m.ID, true, ui.MachineOnline
		}
		list = append(list, e)
	}
	if active == "" {
		// Attached with -ssh to a machine that is not saved: shown, as it is
		// where the client is, under the name it was reached by.
		list = append(list, &endpoint{id: attachedEndpoint, label: t.host, host: t.host, session: t.session, enabled: true, unsaved: true, status: ui.MachineOnline, notices: map[uint64]paneNotice{}})
		active = attachedEndpoint
	}
	t.machines = &machinesState{
		endpoints:    list,
		active:       active,
		folded:       make(map[string]bool),
		remoteFolded: make(map[string]map[string]bool),
		stop:         make(chan struct{}),
	}
	return t.machines
}

// retireLocked stops watching a machine and returns its connection, for the
// caller to close once the lock is let go: closing reports it lost, which
// takes the lock.
func (t *tui) retireLocked(e *endpoint) *client.Client {
	if e.stop != nil {
		close(e.stop)
		e.stop = nil
	}
	c := e.client
	e.client, e.link, e.have = nil, nil, false
	return c
}

// stopMachines ends the catalog's reading and every watching connection.
func (t *tui) stopMachines() {
	if t.catalogStop != nil {
		close(t.catalogStop)
		<-t.catalogDone
		t.catalogStop = nil
	}
	t.mu.Lock()
	ms := t.machines
	t.mu.Unlock()
	if ms == nil {
		return
	}
	close(ms.stop)
	t.mu.Lock()
	var clients []*client.Client
	for _, e := range ms.endpoints {
		if e.client != nil {
			clients = append(clients, e.client)
		}
	}
	t.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
	ms.wg.Wait()
}

// watchEndpoint keeps a connection to a machine until the client leaves or
// the machine is retired, reconnecting when it drops, with a backoff herdr's
// supervisor also caps at half a minute.
func (t *tui) watchEndpoint(ms *machinesState, e *endpoint) {
	t.mu.Lock()
	if e.stop != nil {
		t.mu.Unlock()
		return // already watched
	}
	retired := make(chan struct{})
	e.stop = retired
	t.mu.Unlock()

	ms.wg.Add(1)
	go func() {
		defer ms.wg.Done()
		delay := time.Second
		for {
			link := &endpointLink{t: t, e: e, lost: make(chan struct{})}
			c, err := openWatcher(e.host, e.session, link)
			if err != nil {
				t.mu.Lock()
				if e.stop == retired {
					e.status = ui.MachineReconnecting
					t.dirty = true
				}
				t.mu.Unlock()
				t.wakeUp()
				select {
				case <-ms.stop:
					return
				case <-retired:
					return
				case <-time.After(delay):
				}
				delay = min(delay*2, 30*time.Second)
				continue
			}
			delay = time.Second
			t.mu.Lock()
			stopped := false
			select {
			case <-ms.stop:
				stopped = true
			case <-retired:
				stopped = true
			default:
				e.client, e.link, e.status = c, link, ui.MachineOnline
			}
			t.dirty = true
			t.mu.Unlock()
			if stopped {
				_ = c.Close()
				return
			}
			link.fetch()
			t.wakeUp()

			select {
			case <-ms.stop:
				return // stopMachines closes the client
			case <-retired:
				return // and so does whoever retired it
			case <-link.lost:
			}
			t.mu.Lock()
			if e.client == c {
				e.client, e.link = nil, nil
			}
			shown := ms.active == e.id
			if shown && e.stop == retired {
				// Shown through this connection when it dropped: the client's
				// own reconnect takes the machine back, with a connection of
				// its own, and the machine is watched afresh once it is left.
				e.stop = nil
			} else if !shown {
				e.status = ui.MachineReconnecting
			}
			t.dirty = true
			t.mu.Unlock()
			if shown {
				return
			}
		}
	}()
}

// openWatcher connects to a machine for watching: without a question asked
// on the terminal when it is another one, and starting this one's server as
// attaching would, since herdr's Local is always there to be gone to.
func openWatcher(host, session string, handler client.Handler) (*client.Client, error) {
	if host == "" {
		return openSessionOn("", session, handler)
	}
	return client.DialRemote(transport.WatchArgv(host, session), handler)
}

func (t *tui) setEndpointStatus(e *endpoint, status string) {
	t.mu.Lock()
	if e.status != status {
		e.status = status
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// shownMachineLocked is the id of the machine shown, empty without saved
// machines.
func (t *tui) shownMachineLocked() string {
	if t.machines == nil {
		return ""
	}
	return t.machines.active
}

// snapOfLocked is a machine's session as last read, nil when it has not been.
func (t *tui) snapOfLocked(machine string) *proto.SessionSnapshot {
	if machine == t.shownMachineLocked() {
		return &t.snap
	}
	if e := t.endpointLocked(machine); e != nil && e.have {
		return &e.snap
	}
	return nil
}

// multiMachineLocked reports whether the sidebar lists machines.
func (t *tui) multiMachineLocked() bool {
	return t.machines != nil && len(t.machines.endpoints) > 1
}

// endpointLocked finds a machine by id.
func (t *tui) endpointLocked(id string) *endpoint {
	if t.machines == nil {
		return nil
	}
	for _, e := range t.machines.endpoints {
		if e.id == id {
			return e
		}
	}
	return nil
}

// activeLabelLocked is the name of the machine being shown.
func (t *tui) activeLabelLocked() string {
	if e := t.endpointLocked(t.machines.active); e != nil {
		return e.label
	}
	return "Local"
}

// machineRowsLocked is the space list with machines: each machine's row, and
// beneath it, unless it is folded, its spaces laid out as the plain list lays
// out this session's.
func (t *tui) machineRowsLocked() []ui.SidebarRow {
	ms := t.machines
	var rows []ui.SidebarRow
	for _, e := range ms.endpoints {
		shown := e.id == ms.active
		folded := ms.folded[e.id]
		signal := ""
		if e.id != localEndpoint {
			signal = ui.MachineSignal(e.status)
		}
		rows = append(rows, ui.SidebarRow{
			Kind:     ui.SidebarMachine,
			Label:    e.label,
			Trailing: signal,
			State:    e.status,
			Action:   ui.ActionMachine,
			Machine:  e.id,
			Folded:   folded,
			Active:   shown,
			Stale:    e.status == ui.MachineDisabled,
		})
		if folded {
			continue
		}
		if shown {
			rows = append(rows, t.spaceRowsFrom(spaceSource{snap: &t.snap, folded: t.folded, current: t.workspace, depth: 1})...)
			continue
		}
		if !e.have {
			continue
		}
		groups := ms.remoteFolded[e.id]
		if groups == nil {
			groups = make(map[string]bool)
			ms.remoteFolded[e.id] = groups
		}
		rows = append(rows, t.spaceRowsFrom(spaceSource{
			snap: &e.snap, machine: e.id, folded: groups, depth: 1,
			stale: e.status != ui.MachineOnline,
		})...)
	}
	return rows
}

// clickMachine is a machine row's click: the machine being shown folds and
// unfolds, as herdr's does; another one is gone to.
func (t *tui) clickMachine(id string) error {
	t.mu.Lock()
	if t.machines == nil {
		t.mu.Unlock()
		return nil
	}
	if id == t.machines.active {
		t.machines.folded[id] = !t.machines.folded[id]
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return nil
	}
	t.mu.Unlock()
	return t.switchMachine(id, 0, 0)
}

// toggleRemoteGroup folds a group on a machine that is not the one shown.
func (t *tui) toggleRemoteGroup(machine, group string) {
	t.mu.Lock()
	if t.machines != nil {
		groups := t.machines.remoteFolded[machine]
		if groups == nil {
			groups = make(map[string]bool)
			t.machines.remoteFolded[machine] = groups
		}
		groups[group] = !groups[group]
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// switchMachine shows another machine, at one of its spaces when workspace is
// set and at one of its panes when pane is: herdr's ActivateEndpoint with a
// focus target.
//
// The watching connection becomes the client's. The one the client had goes
// back to watching if it was a watcher, and is closed if the client opened it
// itself — at start, or on a reconnect — with the machine it showed watched
// afresh.
func (t *tui) switchMachine(id string, workspace, pane uint64) error {
	t.mu.Lock()
	ms := t.machines
	e := t.endpointLocked(id)
	if ms == nil || e == nil || id == ms.active {
		t.mu.Unlock()
		return nil
	}
	if e.client == nil || e.status != ui.MachineOnline {
		t.mu.Unlock()
		t.setMessage(e.label+" is not ready", true)
		return nil
	}
	previous := t.endpointLocked(ms.active)
	old := t.client
	oldWatched := previous != nil && previous.client == old
	t.mu.Unlock()

	if !oldWatched && old != nil {
		// Closed first: closing it reports it lost, which must not land
		// after the swap and mark the machine now shown offline, or send
		// the client to reconnect to it.
		_ = old.Close()
		select {
		case <-t.lostConn:
		default:
		}
	}

	t.mu.Lock()
	if e.client == nil {
		// It dropped in between. The client stays where it was, and if its
		// connection was the one just closed, it reconnects there.
		t.mu.Unlock()
		if !oldWatched {
			t.Disconnected()
		}
		t.setMessage(e.label+" is not ready", true)
		return nil
	}
	// What the old machine was showing is kept for its rows: the snapshot is
	// the one the client just had.
	// The record of what was announced goes with its machine: pane numbers
	// are each machine's own.
	if previous != nil {
		previous.notices = t.notices
	}
	t.notices = e.notices
	if previous != nil {
		previous.snap, previous.have = t.snap, true
		if previous.link != nil && oldWatched {
			previous.link.forward.Store(false)
		}
	}
	e.link.forward.Store(true)
	ms.active = id
	t.client = e.client
	t.host, t.session = e.host, e.session
	t.offline = false
	t.screens = make(map[uint64]*vt.Screen)
	t.sizes = make(map[uint64]ui.Rect)
	t.tabFocus = nil
	t.workspace, t.tab, t.focus, t.zoom = workspace, 0, 0, false
	t.dirty = true
	t.mu.Unlock()

	switch {
	case previous != nil && (previous.unsaved || !previous.enabled):
		// Not in the catalog any more, or turned off while it was shown:
		// left, it is not watched, and one no longer saved leaves the list.
		t.mu.Lock()
		c := t.retireLocked(previous)
		if previous.unsaved {
			ms.endpoints = slices.DeleteFunc(ms.endpoints, func(x *endpoint) bool { return x == previous })
		} else {
			previous.status = ui.MachineDisabled
		}
		t.mu.Unlock()
		if c != nil {
			_ = c.Close()
		}
	case oldWatched:
		// Its screens were for the client; watching needs none of them.
		_ = old.SubscribePanes(nil)
	case previous != nil:
		t.watchEndpoint(ms, previous)
	}

	t.requestRepaint()
	if err := t.ensureSession(); err != nil {
		return err
	}
	if err := t.refresh(); err != nil {
		return err
	}
	if pane != 0 {
		if err := t.jumpToPane(pane); err != nil {
			return err
		}
	}
	t.setMessage("on "+e.label, false)
	return nil
}
