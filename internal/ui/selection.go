package ui

import (
	"encoding/base64"
	"strings"
	"unicode"

	"github.com/auth-com-br/tend/internal/vt"
)

// Selecting text is the one thing a terminal multiplexer takes away.
//
// Asking the terminal for mouse reporting is what makes panes clickable, and
// it is also what stops the terminal doing its own selection: the drag now
// belongs to the program, not to the window. Most terminals keep a way out
// under Shift, but it selects across the whole window — the other panes, the
// borders, the sidebar — which for a column of text in a split is not a way
// out at all.
//
// So the selection is drawn here, over the pane it started in, and copied to
// the clipboard through the terminal rather than through any one platform's
// tools.

// Selection is a range of cells in one pane, from where the drag started to
// where the pointer is now.
//
// It keeps both ends rather than a normalised pair, because which one moves
// depends on which way the drag went, and normalising early loses that.
type Selection struct {
	Pane uint64
	// Anchor is where the press landed and Cursor is where the pointer is,
	// both in the pane's own cells with the top-left at zero.
	AnchorX, AnchorY int
	CursorX, CursorY int
	// Scroll is how far back the pane was being read when the ends were last
	// set. A selection follows the text, not the screen: when the view moves
	// under it the ends move with it, so what it covers does not change.
	Scroll int
	// Block takes a rectangle instead of a run of text.
	//
	// A run is right for prose and wrong for anything laid out in columns,
	// which inside an agent is most of what is on screen: the agent draws its
	// own panels, and a run spanning three lines takes the whole width of the
	// middle one, panel and all. A rectangle takes the columns the drag
	// covered and nothing beside them.
	Block bool
	// Dragging marks that the button is still down.
	Dragging bool
}

// CopyCursor is copy mode's cursor, in the pane's own cells.
type CopyCursor struct {
	Pane uint64
	X, Y int
}

// bounds returns the rectangle the two ends span.
func (s Selection) bounds() (left, top, right, bottom int) {
	return min(s.AnchorX, s.CursorX), min(s.AnchorY, s.CursorY),
		max(s.AnchorX, s.CursorX), max(s.AnchorY, s.CursorY)
}

// Empty reports whether the selection covers nothing.
func (s Selection) Empty() bool {
	return s.Pane == 0 || (s.AnchorX == s.CursorX && s.AnchorY == s.CursorY)
}

// ordered returns the two ends with the earlier one first.
func (s Selection) ordered() (fromX, fromY, toX, toY int) {
	if s.AnchorY < s.CursorY || (s.AnchorY == s.CursorY && s.AnchorX <= s.CursorX) {
		return s.AnchorX, s.AnchorY, s.CursorX, s.CursorY
	}
	return s.CursorX, s.CursorY, s.AnchorX, s.AnchorY
}

// covers reports whether a cell of the pane is inside the selection.
//
// The shape is a run of text, not a rectangle: a selection spanning three
// lines takes the end of the first, all of the second and the start of the
// third, which is what dragging over prose is asking for.
func (s Selection) covers(x, y int) bool {
	if s.Empty() {
		return false
	}
	if s.Block {
		left, top, right, bottom := s.bounds()
		return x >= left && x <= right && y >= top && y <= bottom
	}
	fromX, fromY, toX, toY := s.ordered()
	switch {
	case y < fromY || y > toY:
		return false
	case fromY == toY:
		return x >= fromX && x <= toX
	case y == fromY:
		return x >= fromX
	case y == toY:
		return x <= toX
	}
	return true
}

// drawSelection reverses the cells a selection covers.
//
// It reverses what is there rather than painting a colour over it, so text
// that is already styled stays readable and the mark reads as a selection in
// any theme.
func drawSelection(dst *vt.Grid, f Frame) {
	if f.Selection == nil || f.Selection.Empty() {
		return
	}
	sel := *f.Selection
	for _, p := range f.Panes {
		if p.ID != sel.Pane {
			continue
		}
		inner := innerRect(p.Rect)
		for y := 0; y < inner.Rows; y++ {
			row := dst.Line(inner.Y + y)
			if row == nil {
				continue
			}
			for x := 0; x < inner.Cols; x++ {
				if !sel.covers(x, y) {
					continue
				}
				cell := row.Cell(inner.X + x)
				cell.Style.Attrs ^= vt.AttrReverse
				row.SetCell(inner.X+x, cell)
			}
		}
		return
	}
}

// SetClipboard is the escape sequence that puts text on the clipboard through
// the terminal, which works over ssh and needs no platform tool.
//
// Terminals cap how much they will accept and many refuse it unless told to
// allow it, so this is best-effort by nature: there is no reply to wait for
// and no way to know it landed.
func SetClipboard(text string) string {
	if text == "" {
		return ""
	}
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}

// SetPrimary is OSC 52 for the primary selection ("p"), which xterm and
// kitty take and a terminal that does not know it ignores.
func SetPrimary(text string) string {
	if text == "" {
		return ""
	}
	return "\x1b]52;p;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}

// Highlight marks every visible match of a copy-mode search, as herdr keeps a
// window of them highlighted: the next one is where n goes, and the others
// say how far there is to go and whether the one wanted is on screen at all.
type Highlight struct {
	Pane  uint64
	Query string
}

// drawHighlights underlines every occurrence of the query in the pane's
// visible rows. It reads the cells already drawn, so what is marked is what
// is on screen, and uses the search's own rule for case: ignored unless the
// query has a capital, as vim's smartcase does.
//
// Row by row: a match broken across a wrapped edge is found by the search
// and not marked here, which costs a missing underline, never a wrong one.
func drawHighlights(dst *vt.Grid, f Frame) {
	h := f.Highlight
	if h == nil || h.Query == "" {
		return
	}
	needle := []rune(h.Query)
	fold := strings.ToLower(h.Query) == h.Query
	if fold {
		needle = []rune(strings.ToLower(h.Query))
	}
	for _, p := range f.Panes {
		if p.ID != h.Pane {
			continue
		}
		inner := innerRect(p.Rect)
		for y := 0; y < inner.Rows; y++ {
			row := dst.Line(inner.Y + y)
			if row == nil {
				continue
			}
			// The row as runes, each with the column it starts at.
			var runes []rune
			var cols []int
			for x := 0; x < inner.Cols; x++ {
				cell := row.Cell(inner.X + x)
				if cell.Width == 0 {
					continue // the second half of a wide character
				}
				r := cell.R
				if r == 0 {
					r = ' '
				}
				if fold {
					r = unicode.ToLower(r)
				}
				runes = append(runes, r)
				cols = append(cols, x)
			}
			for i := 0; i+len(needle) <= len(runes); i++ {
				if !runesEqual(runes[i:i+len(needle)], needle) {
					continue
				}
				end := inner.Cols
				if i+len(needle) < len(cols) {
					end = cols[i+len(needle)]
				}
				for x := cols[i]; x < end; x++ {
					cell := row.Cell(inner.X + x)
					cell.Style.Attrs |= vt.AttrUnderline | vt.AttrBold
					row.SetCell(inner.X+x, cell)
				}
			}
		}
		return
	}
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
