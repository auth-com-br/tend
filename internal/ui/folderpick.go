package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The folder picker: choosing a folder with the mouse, a click at a time,
// rather than typing its path. It was asked for after a group's folder was
// refused over a path typed short by a few letters — typing a path is where
// the mistakes are, and the folders are there to be shown.
//
// It is drawn like the open-worktree popup: a box in the middle, what it is
// for, where it is, a filter, and the rows. The first row takes the folder
// being shown; ".." goes up; a folder goes into it.

// FolderRowKind is what a row of the picker does.
type FolderRowKind uint8

const (
	// FolderUse takes the folder being shown.
	FolderUse FolderRowKind = iota
	// FolderUp goes to the folder above.
	FolderUp
	// FolderDir goes into a folder.
	FolderDir
)

// FolderRow is one row of the picker.
type FolderRow struct {
	Kind FolderRowKind
	Name string
}

// FolderPick is the picker's state.
type FolderPick struct {
	// Title says what the folder is for.
	Title string
	// Path is the folder shown, Parent the one above it (empty at the root),
	// and Dirs the folders in it, by name.
	Path   string
	Parent string
	Dirs   []string
	// Query filters the folders by name. A query starting with "." shows
	// the hidden ones, which are otherwise left out: they are rarely what
	// anybody is looking for, and in a home folder they are most of it.
	Query string
	// Selected is the row the cursor is on, of Rows.
	Selected int
	Loading  bool
	Error    string
}

// Rows are the picker's rows as shown: taking the folder, going up when
// there is an up, and the folders the query leaves.
func (p FolderPick) Rows() []FolderRow {
	rows := []FolderRow{{Kind: FolderUse}}
	if p.Parent != "" {
		rows = append(rows, FolderRow{Kind: FolderUp, Name: ".."})
	}
	q := strings.ToLower(p.Query)
	hidden := strings.HasPrefix(q, ".")
	for _, d := range p.Dirs {
		if strings.HasPrefix(d, ".") && !hidden {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(d), q) {
			continue
		}
		rows = append(rows, FolderRow{Kind: FolderDir, Name: d})
	}
	return rows
}

// FirstDir is the row of the first folder the query leaves, or the selection
// as it is when none does: typing moves the cursor to what was typed.
func (p FolderPick) FirstDir() int {
	for i, r := range p.Rows() {
		if r.Kind == FolderDir {
			return i
		}
	}
	return min(p.Selected, len(p.Rows())-1)
}

// FolderPickRect is the box: 72 wide, as tall as the rows need between 12
// and 28 lines, in the middle.
func FolderPickRect(p FolderPick, cols, rows int) Rect {
	height := min(max(len(p.Rows())+8, 12), 28)
	r := Rect{Cols: min(72, cols), Rows: min(height, rows)}
	r.X, r.Y = (cols-r.Cols)/2, (rows-r.Rows)/2
	return r
}

// folderBody is where the rows go: under the title, the path, the filter
// and the rule, over the message and the keys.
func folderBody(p FolderPick, cols, rows int) Rect {
	r := FolderPickRect(p, cols, rows)
	return Rect{X: r.X + 1, Y: r.Y + 5, Cols: r.Cols - 2, Rows: max(r.Rows-8, 0)}
}

// folderStart is the first row shown, keeping the selection in view.
func folderStart(p FolderPick, visible int) int {
	return max(min(p.Selected-(visible-1), len(p.Rows())-visible), 0)
}

// FolderPickAt is the row at a point, or -1, and whether the point is
// inside the box at all.
func FolderPickAt(p FolderPick, cols, rows, x, y int) (row int, inside bool) {
	r := FolderPickRect(p, cols, rows)
	if x < r.X || x >= r.X+r.Cols || y < r.Y || y >= r.Y+r.Rows {
		return -1, false
	}
	body := folderBody(p, cols, rows)
	if y >= body.Y && y < body.Y+body.Rows {
		at := folderStart(p, body.Rows) + y - body.Y
		if at < len(p.Rows()) {
			return at, true
		}
	}
	return -1, true
}

// ellipsizeLeft keeps the end of a path, which is the part that says where
// it is, and marks what was cut from the front.
func ellipsizeLeft(s string, width int) string {
	if runewidth.StringWidth(s) <= width || width < 2 {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && runewidth.StringWidth(string(r))+1 > width {
		r = r[1:]
	}
	return "…" + string(r)
}

func drawFolderPick(dst *vt.Grid, p FolderPick, theme Theme) {
	cols, rows := dst.Cols(), dst.Rows()
	r := FolderPickRect(p, cols, rows)
	if r.Cols < 24 || r.Rows < 10 {
		return
	}
	for y := r.Y; y < r.Y+r.Rows; y++ {
		fill(dst, y, r.X, r.X+r.Cols, theme.Menu)
	}
	drawBox(dst, r, theme.BorderFocused)
	drawCloseMark(dst, r, theme.BorderFocused)
	inner := Rect{X: r.X + 1, Y: r.Y + 1, Cols: r.Cols - 2, Rows: r.Rows - 2}
	limit := inner.X + inner.Cols
	bold, dim := theme.Menu, theme.Menu
	bold.Attrs |= vt.AttrBold
	dim.Attrs |= vt.AttrDim

	writeString(dst, inner.X+1, inner.Y, truncate(p.Title, inner.Cols-2), bold, limit)
	writeString(dst, inner.X+1, inner.Y+1, ellipsizeLeft(p.Path, inner.Cols-2), theme.MenuTitle, limit)
	filter, style := " type to filter · . for hidden", dim
	if p.Query != "" {
		filter, style = " / "+p.Query, theme.Menu
	}
	writeString(dst, inner.X, inner.Y+2, truncate(filter, inner.Cols), style, limit)
	writeString(dst, inner.X, inner.Y+3, strings.Repeat("─", inner.Cols), dim, limit)

	body := folderBody(p, cols, rows)
	all := p.Rows()
	start := folderStart(p, body.Rows)
	for i := 0; i < body.Rows && start+i < len(all); i++ {
		row := all[start+i]
		y := body.Y + i
		style := theme.Menu
		label := ""
		switch row.Kind {
		case FolderUse:
			label, style = " ✓ use this folder", theme.MenuTitle
		case FolderUp:
			label = " ↑ .."
		case FolderDir:
			label = " ▸ " + row.Name + "/"
		}
		if start+i == p.Selected {
			style = theme.MenuSelected
			fill(dst, y, body.X, limit, style)
		}
		writeString(dst, body.X, y, truncate(label, body.Cols), style, limit)
	}
	if len(all) == 1 && p.Parent == "" && len(p.Dirs) == 0 && !p.Loading {
		writeString(dst, body.X, body.Y+1, " no folders here", dim, limit)
	}

	switch {
	case p.Loading:
		writeString(dst, inner.X, inner.Y+inner.Rows-2, " reading…", theme.BorderFocused, limit)
	case p.Error != "":
		writeString(dst, inner.X, inner.Y+inner.Rows-2, " "+truncate(p.Error, inner.Cols-2), theme.Blocked, limit)
	}
	writeString(dst, inner.X, inner.Y+inner.Rows-1, " click or ↵ open · ← up · esc cancel", dim, limit)
}
