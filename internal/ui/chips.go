package ui

import (
	"github.com/auth-com-br/tend/internal/vt"
	"github.com/mattn/go-runewidth"
)

// layoutChips lays a line of chips out from x to right (exclusive) at y. When
// they do not fit, the line scrolls so the chosen one is in view: the chips
// scrolled off keep a zero-width Rect, which nothing draws and no click finds,
// and before and after say whether there are more to either side. Without it
// a long list of projects ran past the panel, and the chosen one with it.
func layoutChips(x, right, y int, names []string, chosen int) (rects []Rect, before, after bool) {
	widths := make([]int, len(names))
	total := 0
	for i, name := range names {
		widths[i] = runewidth.StringWidth(" " + name + " ")
		total += widths[i] + 1
	}
	rects = make([]Rect, len(names))
	for i := range rects {
		rects[i] = Rect{Y: y, Rows: 1}
	}
	first := 0
	if total-1 > right-x {
		// room for "‹ " before and " ›" after the chips in view
		span := func(from, to int) int {
			w := 0
			for i := from; i <= to; i++ {
				w += widths[i] + 1
			}
			return w - 1
		}
		chosen = max(0, min(chosen, len(names)-1))
		for first < chosen && 2+span(first, chosen)+2 > right-x {
			first++
		}
	}
	before = first > 0
	sx := x
	if before {
		sx += 2
	}
	limit := right
	if total-1 > right-x {
		limit = right - 2
	}
	for i := first; i < len(names); i++ {
		if sx+widths[i] > limit {
			after = true
			break
		}
		rects[i] = Rect{X: sx, Y: y, Cols: widths[i], Rows: 1}
		sx += widths[i] + 1
	}
	return rects, before, after
}

// drawChips draws a line layoutChips laid out: the label, the chips in view
// with the chosen one lit, and the marks for the ones scrolled off.
func drawChips(dst *vt.Grid, labelX int, label string, rects []Rect, names []string, chosen int, theme Theme, right int) {
	if len(rects) == 0 {
		return
	}
	y := rects[0].Y
	writeString(dst, labelX, y, label, theme.NotesSub, right)
	firstX, lastEnd := -1, -1
	for i, r := range rects {
		if r.Cols == 0 {
			continue
		}
		if firstX < 0 {
			firstX = r.X
		}
		lastEnd = r.X + r.Cols
		style := theme.NotesSub
		if i == chosen {
			style = theme.NotesButton
		}
		writeString(dst, r.X, r.Y, " "+names[i]+" ", style, right)
	}
	if firstX < 0 {
		return
	}
	if rects[0].Cols == 0 {
		writeString(dst, firstX-2, y, "‹", theme.NotesSub, right)
	}
	if rects[len(rects)-1].Cols == 0 && lastEnd >= 0 {
		writeString(dst, lastEnd+1, y, "›", theme.NotesSub, right)
	}
}
