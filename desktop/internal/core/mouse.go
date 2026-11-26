package core

import (
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// The mouse over a pane, on herdr's model as the TUI has it (tui_select.go,
// herdr's client/shell/mouse.rs): a program that asked for the mouse is
// given the whole gesture in its own coordinates, and what it copies (OSC
// 52) reaches the clipboard; in a pane whose program did not ask, tend
// selects, and the wheel scrolls tend's history — or, for a full-screen
// program on the alternate screen, is sent as arrow keys. Alt selects in
// any pane, for a program that holds the mouse and does nothing with a
// drag.

// scrollStep is how many lines a notch of the wheel moves through history.
const scrollStep = 3

// selection is a drag in a pane, in its cells as shown: the history, when
// the pane is scrolled back.
type selection struct {
	active, done           bool
	block                  bool
	anchorX, anchorY, x, y int
}

// Mouse is a mouse event over a pane, in the pane's cells.
func (s *Session) Mouse(pane uint64, ev ui.MouseEvent) error {
	s.mu.Lock()
	c := s.client
	info := s.paneInfoLocked(pane)
	pv := s.panes[pane]
	scrolled := pv != nil && pv.scroll != nil
	s.mu.Unlock()
	if c == nil || pv == nil {
		return nil
	}

	if info.Mouse && !scrolled && !ev.Mods.Has(ui.ModAlt) {
		// Only what it subscribed to: a program that asked for clicks
		// alone has no code for a drag report.
		if ev.Kind == ui.MouseDrag && !info.MouseDrag {
			return nil
		}
		if ev.Kind == ui.MouseMove && !info.MouseMotion {
			return nil
		}
		if seq := ui.EncodeMouse(ev, ev.X, ev.Y, info.MouseSGR); seq != nil {
			return c.SendInput(pane, seq)
		}
		return nil
	}

	switch ev.Kind {
	case ui.MouseWheelUp, ui.MouseWheelDown:
		up := ev.Kind == ui.MouseWheelUp
		if info.WheelKeys && !scrolled {
			// herdr's WheelRouting::AlternateScroll: one arrow a notch, SS3
			// under DECCKM.
			return c.SendInput(pane, arrowKey(up, info.AppCursor))
		}
		if up {
			return s.ScrollBy(pane, scrollStep)
		}
		return s.ScrollBy(pane, -scrollStep)
	case ui.MousePress:
		s.mu.Lock()
		if pv := s.panes[pane]; pv != nil {
			pv.sel = selection{}
			if ev.Button == 0 {
				pv.sel = selection{active: true, block: ev.Mods.Has(ui.ModAlt), anchorX: ev.X, anchorY: ev.Y, x: ev.X, y: ev.Y}
			}
		}
		s.mu.Unlock()
		s.paneChanged(pane)
	case ui.MouseDrag:
		s.mu.Lock()
		if pv := s.panes[pane]; pv != nil && pv.sel.active {
			pv.sel.x, pv.sel.y = ev.X, ev.Y
		}
		s.mu.Unlock()
		s.paneChanged(pane)
	case ui.MouseRelease:
		return s.finishSelection(pane, ev)
	}
	return nil
}

// finishSelection copies what a drag covered. A press and release on one
// cell selects nothing: it was a click.
func (s *Session) finishSelection(pane uint64, ev ui.MouseEvent) error {
	s.mu.Lock()
	pv := s.panes[pane]
	if pv == nil || !pv.sel.active {
		s.mu.Unlock()
		return nil
	}
	pv.sel.x, pv.sel.y = ev.X, ev.Y
	sel := pv.sel
	if sel.anchorX == sel.x && sel.anchorY == sel.y {
		pv.sel = selection{}
		s.mu.Unlock()
		s.paneChanged(pane)
		return nil
	}
	pv.sel.active, pv.sel.done = false, true
	offset := pv.offset
	c := s.client
	s.mu.Unlock()
	s.paneChanged(pane)

	// The server reads the text, as it does for the TUI: it knows which
	// lines a program wrapped and which the terminal did, and a long line
	// copies as one line.
	fromY, fromX, toY, toX := sel.anchorY, sel.anchorX, sel.y, sel.x
	if !sel.block && (toY < fromY || (toY == fromY && toX < fromX)) {
		fromY, fromX, toY, toX = toY, toX, fromY, fromX
	}
	text, err := c.PaneText(proto.PaneTextParams{
		Pane: pane, Scroll: offset,
		FromRow: fromY, FromCol: fromX, ToRow: toY, ToCol: toX,
		Block: sel.block,
	})
	if err != nil {
		return err
	}
	if text != "" && s.Copied != nil {
		s.Copied(text)
	}
	return nil
}

// ScrollBy moves a pane's view lines back through its history (negative:
// toward the present). At zero the pane shows live again.
func (s *Session) ScrollBy(pane uint64, lines int) error {
	s.mu.Lock()
	pv := s.panes[pane]
	c := s.client
	if pv == nil {
		s.mu.Unlock()
		return nil
	}
	target := pv.offset + lines
	if pv.history > 0 && target > pv.history {
		target = pv.history
	}
	s.mu.Unlock()
	if target <= 0 {
		s.leaveScroll(pane)
		return nil
	}
	view, err := c.PaneScreenAt(pane, target)
	if err != nil {
		return err
	}
	if view.Offset <= 0 {
		s.leaveScroll(pane)
		return nil
	}
	screen := vt.NewScreen(view.Cols, view.Rows, 0)
	_, _ = screen.Write([]byte(view.ANSI))
	s.mu.Lock()
	if pv := s.panes[pane]; pv != nil {
		pv.scroll, pv.offset, pv.history = screen, view.Offset, view.History
		pv.sel = selection{}
	}
	s.mu.Unlock()
	s.paneChanged(pane)
	s.stateChanged()
	return nil
}

// leaveScroll shows a pane live again.
func (s *Session) leaveScroll(pane uint64) {
	s.mu.Lock()
	pv := s.panes[pane]
	was := pv != nil && pv.scroll != nil
	if pv != nil {
		pv.scroll, pv.offset = nil, 0
		if was {
			pv.sel = selection{}
		}
	}
	s.mu.Unlock()
	if was {
		s.paneChanged(pane)
		s.stateChanged()
	}
}

// Paste gives a pane the clipboard, as a paste: bracketed when its program
// asked, so an agent takes several lines as one message.
func (s *Session) Paste(pane uint64, text string) error {
	s.leaveScroll(pane)
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	return c.PastePane(pane, text)
}

func (s *Session) paneInfoLocked(pane uint64) proto.PaneInfo {
	for _, p := range s.snap.Panes {
		if p.ID == pane {
			return p
		}
	}
	return proto.PaneInfo{}
}

// arrowKey is the up or down arrow as a program reading the keyboard gets
// it: SS3 in application cursor mode (DECCKM), CSI otherwise.
func arrowKey(up, app bool) []byte {
	final := byte('B')
	if up {
		final = 'A'
	}
	if app {
		return []byte{0x1b, 'O', final}
	}
	return []byte{0x1b, '[', final}
}

// highlight marks a selection in a frame by swapping its cells' colours.
func highlight(f *Frame, sel selection) {
	if !sel.active && !sel.done {
		return
	}
	inside := func(x, y int) bool {
		if sel.block {
			lx, rx := min(sel.anchorX, sel.x), max(sel.anchorX, sel.x)
			ty, by := min(sel.anchorY, sel.y), max(sel.anchorY, sel.y)
			return x >= lx && x <= rx && y >= ty && y <= by
		}
		fy, fx, ty, tx := sel.anchorY, sel.anchorX, sel.y, sel.x
		if ty < fy || (ty == fy && tx < fx) {
			fy, fx, ty, tx = ty, tx, fy, fx
		}
		if y < fy || y > ty {
			return false
		}
		if y == fy && x < fx {
			return false
		}
		if y == ty && x > tx {
			return false
		}
		return true
	}
	for y := 0; y < f.Rows; y++ {
		for x := 0; x < f.Cols; x++ {
			if inside(x, y) {
				c := &f.Cells[y*f.Cols+x]
				c.FG, c.BG = c.BG, c.FG
			}
		}
	}
}
