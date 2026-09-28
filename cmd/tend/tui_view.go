package main

import (
	"errors"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// Scrolling looks back through a pane's history without stopping it.
//
// The pane keeps running and the server keeps its live screen; what changes is
// only which rows this client asks to be drawn. That is why leaving the view
// needs no resynchronisation: nothing was ever out of step.

// splitKeys breaks a chunk into whole keys.
//
// A handler that looks at only the first byte and then reports the chunk
// consumed drops everything after it, which is how "k" followed by Enter
// moved a cursor and never acted on it. Splitting first makes that shape of
// mistake impossible: every key in the chunk is offered in turn.
func splitKeys(data []byte) []string {
	var keys []string
	for i := 0; i < len(data); {
		if data[i] == 0x1b {
			if n := escapeLength(data[i:]); n > 0 {
				keys = append(keys, string(data[i:i+n]))
				i += n
				continue
			}
		}
		keys = append(keys, string(data[i:i+1]))
		i++
	}
	return keys
}

// escapeLength is how many bytes the escape sequence at the start of data
// occupies, or zero when it is not one this cares about.
func escapeLength(data []byte) int {
	if len(data) < 3 || data[1] != '[' {
		return 0
	}
	for i := 2; i < len(data); i++ {
		// A final byte ends a control sequence; the parameters before it are
		// digits and semicolons.
		if data[i] >= '@' && data[i] <= '~' {
			return i + 1
		}
		if data[i] != ';' && (data[i] < '0' || data[i] > '9') {
			return 0
		}
	}
	return 0
}

func (t *tui) scrolling() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scrollPane != 0
}

// enterScroll starts looking back through the focused pane.
func (t *tui) enterScroll() error {
	t.mu.Lock()
	pane := t.focus
	t.mu.Unlock()
	if pane == 0 {
		return nil
	}

	t.mu.Lock()
	t.scrollPane = pane
	t.scrollOffset = 0
	t.mu.Unlock()
	return t.scrollBy(1)
}

// leaveScroll returns to the live screen.
func (t *tui) leaveScroll() {
	t.mu.Lock()
	t.scrollPane, t.scrollOffset, t.scrollDepth, t.scrollScreen = 0, 0, 0, nil
	t.dirty = true
	t.mu.Unlock()
	t.requestRepaint()
	t.wakeUp()
}

// paneSizeLocked is the size a pane is drawn at, inside its border. The
// caller holds the lock.
func (t *tui) paneSizeLocked(pane uint64) (cols, rows int) {
	rect, ok := t.sizes[pane]
	if !ok {
		return 0, 0
	}
	return ui.InnerSize(rect)
}

// scrollTo shows a pane at an exact distance back. Unlike scrollBy it stays at
// zero: copy mode shows the present as a still picture, with a cursor on it,
// and leaving the view there would take the cursor away.
func (t *tui) scrollTo(pane uint64, offset int) error {
	view, err := t.client.PaneScreenAt(pane, max(offset, 0))
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scrollPane != pane {
		return nil // left the view while the request was in flight
	}
	if t.scrollScreen == nil || t.scrollScreen.Grid().Cols() != view.Cols {
		t.scrollScreen = vt.NewScreen(view.Cols, view.Rows, 0)
	} else {
		t.scrollScreen.Resize(view.Cols, view.Rows)
	}
	_, _ = t.scrollScreen.Write([]byte(view.ANSI))
	t.scrollOffset = max(view.Offset, 0)
	t.scrollDepth = view.History
	t.dirty = true
	return nil
}

// scrollBy moves the view, positive being further back.
func (t *tui) scrollBy(lines int) error {
	t.mu.Lock()
	pane := t.scrollPane
	want := t.scrollOffset + lines
	t.mu.Unlock()
	if pane == 0 {
		return nil
	}
	if want <= 0 {
		// Scrolling back to the present leaves the view rather than sitting
		// at an offset of zero, which would look live but not update.
		t.leaveScroll()
		return nil
	}

	view, err := t.client.PaneScreenAt(pane, want)
	if err != nil {
		return err
	}

	if view.Offset <= 0 {
		// The pane had nothing to scroll back to, so the server answered with
		// the present. Staying in the view at an offset of zero is the state
		// the check above exists to prevent, reached by another door: the
		// pane looks live and is a still picture, and everything done to it
		// afterwards — a selection above all — is aimed at text that is no
		// longer where the picture shows it.
		t.leaveScroll()
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.scrollPane != pane {
		return nil // left the view while the request was in flight
	}
	if t.scrollScreen == nil || t.scrollScreen.Grid().Cols() != view.Cols {
		t.scrollScreen = vt.NewScreen(view.Cols, view.Rows, 0)
	} else {
		t.scrollScreen.Resize(view.Cols, view.Rows)
	}
	_, _ = t.scrollScreen.Write([]byte(view.ANSI))

	t.scrollOffset = view.Offset
	t.scrollDepth = view.History
	t.dirty = true
	return nil
}

// scrollKeys drives the view while it is up. It reports whether the input was
// consumed, so that keys meant for a pane are not swallowed by a mode the
// user has already left.
func (t *tui) scrollKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		handled, err := t.scrollKey(key)
		if err != nil {
			return true, err
		}
		if !handled {
			// The view is closed and this key belongs to the pane. Anything
			// after it does too, so the rest of the chunk goes back.
			return false, nil
		}
	}
	return len(data) > 0, nil
}

func (t *tui) scrollKey(key string) (bool, error) {
	t.mu.Lock()
	page := max(t.rows-ui.StatusRows-4, 1)
	depth := t.scrollDepth
	t.mu.Unlock()

	switch key {
	case "\x1b[A", "k":
		return true, t.scrollBy(1)
	case "\x1b[B", "j":
		return true, t.scrollBy(-1)
	case "\x1b[5~":
		return true, t.scrollBy(page)
	case "\x1b[6~":
		return true, t.scrollBy(-page)
	case "\x1b[H", "g":
		return true, t.scrollBy(depth)
	case "G", "q", "\x1b", "\x03":
		t.leaveScroll()
		return true, nil
	}
	// Anything else leaves the view and reaches the pane, so typing at a
	// scrolled prompt does what it looks like it should.
	t.leaveScroll()
	return false, nil
}

// --- resize mode -------------------------------------------------------------

func (t *tui) resizingNow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.resizing
}

// resizeKeys drives resize mode: herdr's, where the movement keys push the
// focused pane's edges and escape ends it. The prefix is not needed before
// each press, which is the whole point of a mode — resizing by a few cells at
// a time is otherwise two keys per cell.
//
// Any key that is not a resize key ends the mode and goes where it was going,
// so typing at a prompt after resizing does what it looks like.
func (t *tui) resizeKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		var cmd ui.Command
		switch key {
		case "h", "H", "\x1b[D":
			cmd = ui.CommandGrowLeft
		case "l", "L", "\x1b[C":
			cmd = ui.CommandGrowRight
		case "k", "K", "\x1b[A":
			cmd = ui.CommandGrowUp
		case "j", "J", "\x1b[B":
			cmd = ui.CommandGrowDown
		case "\x1b", "\r", "q", "r":
			t.leaveResize()
			continue
		default:
			t.leaveResize()
			return false, nil
		}
		if err := t.command(ui.Action{Command: cmd}); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (t *tui) leaveResize() {
	t.mu.Lock()
	t.resizing = false
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// --- mouse -----------------------------------------------------------------

// handleMouse turns a mouse report into focus, a resize, a scroll, or input
// for a pane's own program.
func (t *tui) handleMouse(ev ui.MouseEvent) error {
	if t.promptMouse(ev) {
		return nil
	}
	if ev.Kind == ui.MousePress && ev.Button == 0 {
		// The overlay's ✕ — the keys' help — closes it, as a key does.
		t.mu.Lock()
		lines, cols, rows := t.overlay, t.cols, t.rows
		t.mu.Unlock()
		if box, ok := ui.OverlayBox(lines, cols, rows); ok && len(lines) > 0 && ui.OnCloseMark(box, ev.X, ev.Y) {
			t.dismissOverlay()
			return nil
		}
	}
	// The navigator is over everything, so the mouse is all its while it is
	// up; a press outside it puts it away.
	if handled, err := t.navigatorMouse(ev); handled {
		return err
	}
	if handled, err := t.worktreeOpenMouse(ev); handled {
		return err
	}
	switch ev.Kind {
	case ui.MouseWheelUp:
		if t.scrollSidebar(ev.X, ev.Y, -sidebarScrollStep) {
			return nil
		}
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 {
			// A program that asked for the mouse gets the wheel. It has its
			// own idea of what is above the screen — an agent's transcript, a
			// pager, an editor — and showing tend's scrollback instead means
			// the wheel does nothing the user recognises.
			if took, err := t.forwardMouse(pane, ev, false); took {
				t.focusPane(pane)
				return err
			}
			t.focusPane(pane)
			if !t.scrolling() {
				return t.enterScroll()
			}
			return t.scrollBy(wheelLines)
		}
		return nil

	case ui.MouseWheelDown:
		if t.scrollSidebar(ev.X, ev.Y, sidebarScrollStep) {
			return nil
		}
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 {
			if took, err := t.forwardMouse(pane, ev, false); took {
				return err
			}
		}
		if t.scrolling() {
			return t.scrollBy(-wheelLines)
		}
		return nil

	case ui.MousePress:
		// An open menu is on top, so it answers first, and a press anywhere
		// else closes it rather than acting through it.
		if handled, err := t.clickMenu(ev.X, ev.Y); handled {
			return err
		}
		if ev.Button != mouseRight {
			if handled, err := t.clickToast(ev.X, ev.Y); handled {
				return err
			}
		}
		// ctrl+click on a link is the link's, before the program under it
		// or a selection has it, as in herdr.
		if t.clickLink(ev) {
			return nil
		}
		if ev.Button == mouseRight {
			if m, ok := t.menuFor(ev.X, ev.Y); ok {
				t.openMenu(m)
			}
			return nil
		}
		if t.grabSidebarDivider(ev.X, ev.Y) || t.grabSidebarScrollbar(ev.X, ev.Y) {
			return nil
		}
		if handled, err := t.clickTabBar(ev.X, ev.Y); handled {
			return err
		}
		if handled, err := t.clickSidebar(ev.X, ev.Y); handled {
			return err
		}
		if pane, side, ok := t.dividerAt(ev.X, ev.Y); ok {
			// A press on a border is a grab, not a focus change: the user is
			// reaching for the divider, not for the pane behind it.
			t.mu.Lock()
			t.dragPane, t.dragSide = pane, side
			t.dragAt = ev.X
			if side == "up" || side == "down" {
				t.dragAt = ev.Y
			}
			t.mu.Unlock()
			return nil
		}
		pane := t.paneAt(ev.X, ev.Y)
		if pane == 0 {
			return nil
		}
		t.mu.Lock()
		blocked := t.popupRect != nil && pane != t.popupRect.Pane
		t.mu.Unlock()
		if blocked {
			return nil // the popup has the input, as herdr's has
		}
		// Two presses in the same cell, close together, are a double click:
		// terminals report presses and leave the counting to whoever cares.
		if t.isDoubleClick(ev) {
			t.clearSelection()
			t.focusPane(pane)
			if t.selectWord(ev) {
				return nil
			}
		}
		// A press anywhere drops the last selection: it marked text the user
		// has now moved on from, and leaving it lit suggests it is still what
		// a copy would take.
		t.clearSelection()
		t.focusPane(pane)
		if t.beginSelection(ev) {
			return nil
		}
		// Not tend's to select in, so the press and everything that follows
		// it belong to the program.
		_, err := t.beginGesture(pane, ev)
		return err

	case ui.MouseMove:
		// An open menu follows the pointer; otherwise the move is for the
		// program under it, if it asked for motion (forwardMouse drops it
		// for one that did not), as herdr forwards it.
		if t.hoverMenu(ev.X, ev.Y) {
			return nil
		}
		if t.hoverToolbar(ev.X, ev.Y) {
			return nil
		}
		t.hoverLink(ev)
		if pane := t.paneAt(ev.X, ev.Y); pane != 0 {
			_, err := t.forwardMouse(pane, ev, false)
			return err
		}
		return nil

	case ui.MouseDrag:
		if t.dragTab(ev) || t.dragSpace(ev) {
			return nil
		}
		if took, err := t.continueGesture(ev); took {
			return err
		}
		if t.dragSelection(ev) {
			return nil
		}
		if t.dragSidebarDivider(ev.Y) || t.dragSidebarScrollbar(ev.Y) {
			return nil
		}
		if t.dragSidebarWidth(ev.X) {
			return nil
		}
		return t.dragDivider(ev)

	case ui.MouseRelease:
		if dropped, err := t.dropTab(); dropped {
			return err
		}
		if dropped, err := t.dropSpace(); dropped {
			return err
		}
		// paneAt takes the same lock, so the grab is released first and the
		// lookup happens after. A mutex that is not reentrant turns a nested
		// call into a frozen client, which is exactly how this was found.
		if took, err := t.continueGesture(ev); took {
			return err
		}
		if t.endSelection() {
			return nil
		}

		t.mu.Lock()
		t.dragPane, t.dragSide = 0, ""
		t.draggingSidebar = false
		t.draggingSidebarWidth = false
		t.draggingScrollbar = ui.SidebarNowhere
		t.mu.Unlock()

		return nil
	}
	return nil
}

func (t *tui) dragDivider(ev ui.MouseEvent) error {
	t.mu.Lock()
	pane, side, at := t.dragPane, t.dragSide, t.dragAt
	t.mu.Unlock()
	if pane == 0 {
		return nil
	}

	now := ev.X
	if side == "up" || side == "down" {
		now = ev.Y
	}
	delta := now - at
	if delta == 0 {
		return nil
	}

	t.mu.Lock()
	t.dragAt = now
	t.mu.Unlock()

	area := t.layoutArea()
	if err := t.client.AdjustSplit(pane, side, delta, area.Cols, area.Rows); err != nil {
		return err
	}
	return t.refresh()
}

// sidebarScrollStep is how far one notch of the wheel moves the list. Two
// entries, because most of them are two lines and moving by one would read as
// half a step.
const sidebarScrollStep = 2

// wheelLines is how far one notch moves a pane's view.
const wheelLines = 3

// scrollSidebar moves the list when the pointer is over it, and reports
// whether it took the event.
//
// The sidebar takes the wheel before the panes do: the pointer is over the
// list, and scrolling the pane under a pointer that is not on it is the kind
// of thing that makes people stop trusting the mouse.
func (t *tui) scrollSidebar(x, y, delta int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sidebar {
		return false
	}
	frame := t.buildFrame()
	spaces, agents := ui.SidebarRegions(frame, t.rows)

	switch ui.SidebarPlaceAt(frame, x, y, t.rows) {
	case ui.SidebarSpacesList:
		t.spacesScroll = clampScroll(t.spacesScroll+delta, frame.Spaces, spaces.Rows)
	case ui.SidebarAgentsList:
		t.agentsScroll = clampScroll(t.agentsScroll+delta, frame.Agents, agents.Rows)
	case ui.SidebarDivider:
		// The divider is a handle, not a list. The wheel over it does nothing
		// rather than scrolling whichever side happens to be nearer.
		return true
	default:
		return false
	}
	t.dirty = true
	// Taken either way: at the end of a list the wheel has nowhere to go, and
	// falling through to the pane behind would be a surprise.
	return true
}

func clampScroll(at int, s ui.SidebarSection, height int) int {
	return min(max(at, 0), ui.SidebarMaxScroll(s, height))
}

// grabSidebarScrollbar handles a press on a list's scrollbar, and reports
// whether the press was on one. herdr's: on the thumb it takes hold of it
// where it was pressed, so the thumb does not jump under the pointer; on the
// track it moves the list so the thumb is centred there.
func (t *tui) grabSidebarScrollbar(x, y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sidebar {
		return false
	}
	list, bar, ok := ui.SidebarScrollbarAt(t.buildFrame(), x, y, t.rows)
	if !ok {
		return false
	}
	if bar.OnThumb(y) {
		t.draggingScrollbar, t.scrollbarGrab = list, y-bar.ThumbTop
		return true
	}
	t.setListScrollLocked(list, bar.ScrollAt(y, bar.ThumbLen/2))
	return true
}

// dragSidebarScrollbar follows a held thumb, and reports whether one was
// held. The pointer may leave the column while it drags; only its line counts.
func (t *tui) dragSidebarScrollbar(y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.draggingScrollbar == ui.SidebarNowhere {
		return false
	}
	if bar, ok := ui.SidebarScrollbarOf(t.buildFrame(), t.rows, t.draggingScrollbar); ok {
		t.setListScrollLocked(t.draggingScrollbar, bar.ScrollAt(y, t.scrollbarGrab))
	}
	return true
}

// setListScrollLocked scrolls one list, and marks the screen stale if that
// moved it.
func (t *tui) setListScrollLocked(list ui.SidebarPlace, at int) {
	scroll := &t.spacesScroll
	if list == ui.SidebarAgentsList {
		scroll = &t.agentsScroll
	}
	if *scroll != at {
		*scroll = at
		t.dirty = true
	}
}

// grabSidebarDivider takes hold of the line between the two lists, and reports
// whether the press was on it.
//
// A press on a divider is a grab, not a click: the user is reaching for the
// line, not for whatever is drawn under it.
func (t *tui) grabSidebarDivider(x, y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sidebar {
		return false
	}
	switch ui.SidebarPlaceAt(t.buildFrame(), x, y, t.rows) {
	case ui.SidebarDivider:
		t.draggingSidebar = true
		return true
	case ui.SidebarEdge:
		// herdr's sidebar divider: a drag sets the width, a double click
		// puts it back to the settings' (client/shell/mouse.rs).
		now := time.Now()
		if now.Sub(t.lastEdgeClick) <= 350*time.Millisecond {
			t.lastEdgeClick = time.Time{}
			if t.sidebarWidth != 0 {
				t.sidebarWidth = 0
				t.dirty = true
				go t.resizeForSidebar()
			}
			return true
		}
		t.lastEdgeClick = now
		t.draggingSidebarWidth = true
		return true
	}
	return false
}

// sidebarWidthLocked is the sidebar's width: where it was dragged to, or
// the settings'.
func (t *tui) sidebarWidthLocked() int {
	if t.sidebarWidth > 0 {
		return t.sidebarWidth
	}
	return t.config.SidebarWidth()
}

// dragSidebarWidth moves the sidebar's edge to the pointer, and reports
// whether the edge was the one being dragged.
func (t *tui) dragSidebarWidth(x int) bool {
	t.mu.Lock()
	if !t.draggingSidebarWidth {
		t.mu.Unlock()
		return false
	}
	lo, hi := t.config.SidebarBounds()
	next := ui.SidebarWidthAt(x, lo, min(hi, t.cols-1))
	changed := next != t.sidebarWidthLocked()
	if changed {
		t.sidebarWidth = next
		t.dirty = true
	}
	t.mu.Unlock()
	if changed {
		t.resizeForSidebar()
	}
	return true
}

// resizeForSidebar lays the panes out again beside a sidebar of a new width.
func (t *tui) resizeForSidebar() {
	if err := t.refresh(); err != nil {
		t.setMessage(err.Error(), true)
	}
}

// dragSidebarDivider moves the line to where the pointer is, and reports
// whether it was the one being dragged.
//
// The line follows the pointer rather than moving by steps, and the clamping
// lives in the sidebar with the drawing, so a drag past either end stops where
// the divider can actually go instead of storing a position it cannot.
func (t *tui) dragSidebarDivider(y int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.draggingSidebar {
		return false
	}
	frame := t.buildFrame()
	frame.SidebarSplit = y
	if next := ui.SidebarSplitAt(frame, t.rows); next != t.sidebarSplit {
		t.sidebarSplit = next
		t.dirty = true
	}
	return true
}

// mouseRight is the button number a right-click reports, which is what opens
// a menu here as it does everywhere else.
const mouseRight = 2

// clickTabBar switches tabs, or makes one.
func (t *tui) clickTabBar(x, y int) (bool, error) {
	t.mu.Lock()
	frame := t.buildFrame()
	cols, rows := t.cols, t.rows
	t.mu.Unlock()

	tab, newTab, ok := ui.TabAt(frame, x, y, cols, rows)
	if !ok {
		return false, nil
	}
	if newTab {
		return true, t.newTabHere()
	}
	// The press may be the start of a drag that moves the tab: herdr's tab
	// drag. The tab is shown at once either way.
	t.mu.Lock()
	t.tabDrag, t.tabDropTarget = tab, 0
	t.mu.Unlock()
	return true, t.showTab(tab)
}

// dragTab follows a tab being dragged along the bar, marking where it would
// land. It reports whether a tab drag is under way.
func (t *tui) dragTab(ev ui.MouseEvent) bool {
	t.mu.Lock()
	dragging := t.tabDrag
	if dragging == 0 {
		t.mu.Unlock()
		return false
	}
	frame := t.buildFrame()
	cols, rows := t.cols, t.rows
	// The bar's row, whatever row the pointer drifted to: a drag along a
	// one-line bar is rarely exactly on it.
	target, newTab, ok := ui.TabAt(frame, ev.X, ui.TabBarRow(frame, rows), cols, rows)
	if !ok || newTab || target == dragging {
		target = 0
	}
	if target != t.tabDropTarget {
		t.tabDropTarget = target
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
	return true
}

// dropTab ends a tab drag, moving the tab to where it was dropped. It
// reports whether there was one.
func (t *tui) dropTab() (bool, error) {
	t.mu.Lock()
	dragging, target := t.tabDrag, t.tabDropTarget
	t.tabDrag, t.tabDropTarget = 0, 0
	from, to := -1, -1
	for i, tab := range t.tabsLocked() {
		if tab.ID == dragging {
			from = i
		}
		if tab.ID == target {
			to = i
		}
	}
	t.dirty = true
	t.mu.Unlock()
	if dragging == 0 {
		return false, nil
	}
	if target == 0 || from < 0 || to < 0 {
		return true, nil // a click, or a drop off the bar
	}
	if err := t.client.MoveTab(dragging, to-from); err != nil {
		return true, err
	}
	return true, t.refresh()
}

// clickSidebar goes wherever the clicked row points.
func (t *tui) clickSidebar(x, y int) (bool, error) {
	t.mu.Lock()
	frame := t.buildFrame()
	rows := t.rows
	t.mu.Unlock()

	// The handle answers first: it is drawn over the corner of a list, and a
	// click there means the handle, not whatever row is underneath.
	if ui.SidebarHandleAt(frame, x, y, rows) {
		return true, t.toggleSidebar()
	}
	if ui.SidebarPlaceAt(frame, x, y, rows) == ui.SidebarToolbar {
		return true, t.clickToolbar(frame, x, y, rows)
	}

	row, ok := ui.SidebarRowAt(frame, x, y, rows)
	if !ok {
		return false, nil
	}
	switch {
	case row.Action == ui.ActionMachine:
		return true, t.clickMachine(row.Machine)
	case row.Machine != "" && row.Action == ui.ActionToggleGroup:
		t.toggleRemoteGroup(row.Machine, row.Group)
		return true, nil
	case row.Machine != "" && row.Workspace != 0:
		// Another machine's space or agent: going there is going to the
		// machine.
		return true, t.switchMachine(row.Machine, row.Workspace, row.Pane)
	case row.Action == ui.ActionNewSpace:
		return true, t.newWorkspace()
	case row.Action == ui.ActionToggleGroup:
		t.toggleGroup(row.Group)
		return true, nil
	case row.Action == ui.ActionOpenMenu:
		// herdr's global menu. What the space's own menu offers is on a
		// right-click on the space, as in herdr.
		t.mu.Lock()
		ready, notes := t.updateReadyLocked() != "", t.notesAvailableLocked()
		t.mu.Unlock()
		t.openMenu(ui.GlobalMenu(t.serverStale(), ready, notes, x, y))
		return true, nil
	case row.Action == ui.ActionCompanies:
		return true, t.openCompanies()
	case row.Action == ui.ActionToggleGrouped:
		t.mu.Lock()
		t.grouped = !t.grouped
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return true, nil
	case row.Pane != 0:
		return true, t.jumpToPane(row.Pane)
	case row.Tab != 0:
		return true, t.showTab(row.Tab)
	case row.Workspace != 0:
		// The press may start a drag that moves the space, as a tab's may.
		if row.Kind == ui.SidebarSpace {
			t.mu.Lock()
			t.spaceDrag, t.spaceDropTarget, t.spaceDropGroup = row.Workspace, 0, ""
			t.mu.Unlock()
		}
		return true, t.showWorkspace(row.Workspace)
	}
	return true, nil
}

// dragSpace follows a space dragged down the sidebar, marking the space it
// would take the place of. It reports whether a space drag is under way.
func (t *tui) dragSpace(ev ui.MouseEvent) bool {
	t.mu.Lock()
	dragging := t.spaceDrag
	if dragging == 0 {
		t.mu.Unlock()
		return false
	}
	frame := t.buildFrame()
	rows := t.rows
	target, group := uint64(0), ""
	if row, ok := ui.SidebarRowAt(frame, 1, ev.Y, rows); ok {
		switch {
		case row.Machine != "":
			// A space cannot be dragged to another machine.
		case row.Kind == ui.SidebarSpace && row.Workspace != dragging:
			target = row.Workspace
		case row.Kind == ui.SidebarSpaceGroup && row.Group != t.groupOfLocked(dragging):
			group = row.Group
		}
	}
	if target != t.spaceDropTarget || group != t.spaceDropGroup {
		t.spaceDropTarget, t.spaceDropGroup = target, group
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
	return true
}

// dropSpace ends a space drag, moving the space to the place of the one it
// was dropped on, or into the group whose heading it was dropped on. It
// reports whether there was one.
//
// A space dropped among another group's spaces joins that group, and one
// dropped among spaces in none leaves its own: the sidebar draws a space
// under its group's heading whatever its place in the session, so moving it
// without regrouping it would put it back where it came from on screen.
func (t *tui) dropSpace() (bool, error) {
	t.mu.Lock()
	dragging, target, group := t.spaceDrag, t.spaceDropTarget, t.spaceDropGroup
	t.spaceDrag, t.spaceDropTarget, t.spaceDropGroup = 0, 0, ""
	from, to := -1, -1
	own, theirs := "", ""
	for i, w := range t.snap.Workspaces {
		if w.ID == dragging {
			from, own = i, w.Group
		}
		if w.ID == target {
			to, theirs = i, w.Group
		}
	}
	t.dirty = true
	t.mu.Unlock()
	if dragging == 0 {
		return false, nil
	}
	if group != "" {
		return true, t.moveSpaceToGroup(dragging, group)
	}
	if target == 0 || from < 0 || to < 0 {
		return true, nil
	}
	if theirs != own {
		if err := t.client.GroupWorkspace(dragging, theirs); err != nil {
			return true, err
		}
	}
	if err := t.client.MoveWorkspace(dragging, to-from); err != nil {
		return true, err
	}
	return true, t.refresh()
}

// paneAt returns the pane drawn at a point, or zero.
func (t *tui) paneAt(x, y int) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	rects := t.paneRects()
	// From the end: what is drawn last is on top, and a popup is drawn last.
	for i := len(rects) - 1; i >= 0; i-- {
		r := rects[i]
		if x >= r.X && x < r.X+r.Cols && y >= r.Y && y < r.Y+r.Rows {
			return r.Pane
		}
	}
	return 0
}

// dividerAt reports whether a point is on a border shared with a neighbour,
// and which of the pane's edges it is.
//
// A border on the outside of the layout is not a divider: there is nothing on
// the far side of it to take space from.
func (t *tui) dividerAt(x, y int) (uint64, string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	area := t.layoutAreaLocked()

	for _, r := range t.paneRects() {
		inRows := y >= r.Y && y < r.Y+r.Rows
		inCols := x >= r.X && x < r.X+r.Cols

		if inRows && x == r.X+r.Cols-1 && r.X+r.Cols < area.X+area.Cols {
			return r.Pane, "right", true
		}
		if inRows && x == r.X && r.X > area.X {
			return r.Pane, "left", true
		}
		if inCols && y == r.Y+r.Rows-1 && r.Y+r.Rows < area.Y+area.Rows {
			return r.Pane, "down", true
		}
		if inCols && y == r.Y && r.Y > area.Y {
			return r.Pane, "up", true
		}
	}
	return 0, "", false
}

// forwardsMouse reports whether a pane's own program asked for the mouse, in
// which case a click inside it belongs to that program rather than to tend.
func (t *tui) forwardsMouse(pane uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range t.snap.Panes {
		if p.ID == pane {
			return p.Mouse
		}
	}
	return false
}

func (t *tui) focusPane(pane uint64) {
	t.mu.Lock()
	changed := t.focus != pane
	t.focus = pane
	t.dirty = true
	t.mu.Unlock()
	if changed {
		t.wakeUp()
	}
}

// scrollPaneBy moves a named pane's view, entering the scrollback if it is not
// already in it.
//
// scrollBy works on whichever pane the scroll view is on, which is the right
// answer for the wheel and the keys. A selection is dragged in one particular
// pane, and it must not move a view that belongs to another one.
func (t *tui) scrollPaneBy(pane uint64, lines int) error {
	t.mu.Lock()
	current := t.scrollPane
	t.mu.Unlock()

	if current != pane {
		if lines <= 0 {
			// Already at the present: there is nowhere further forward to go.
			return nil
		}
		t.mu.Lock()
		t.scrollPane, t.scrollOffset = pane, 0
		t.mu.Unlock()
	}
	return t.scrollBy(lines)
}

// tellFocus lets the server know which pane is being looked at, so a program
// that asked for focus events (mode 1004) is told it gained or lost it.
//
// Only worth saying when somebody asked: the call is cheap, but a server that
// has never heard of the method would answer every focus change with an error,
// and the notice about an older server is not what a user moving between panes
// wants to see.
func (t *tui) tellFocus(gained, lost uint64) {
	if !t.serverKnows(proto.MethodPaneFocus) {
		return
	}
	if err := t.client.FocusPane(gained, lost); err != nil && !errors.Is(err, proto.ErrUnknownMethod) {
		// Nothing to say to the user: a program not being told about focus is
		// not something they asked for or can act on.
		return
	}
}

// serverKnows reports whether the server named a method in its handshake.
func (t *tui) serverKnows(method string) bool {
	for _, known := range t.client.Server().Methods {
		if known == method {
			return true
		}
	}
	return false
}
