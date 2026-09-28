package ui

import (
	"strings"

	"github.com/auth-com-br/tend/internal/vt"
)

// The sidebar answers two questions without leaving the pane you are in:
// where else could I be, and which agent needs me.
//
// It is two lists, not one. Spaces are places — a repository, a branch, a
// piece of work — and they exist whether or not anything is running in them.
// Agents are what is running, and the one that stopped is rarely in the space
// being looked at, which is why the second list is not merely the first
// expanded.
//
// Both are drawn as a two-line entry: a name, and beneath it dimmed, the thing
// that tells two entries with the same name apart. For a space that is its
// branch; for an agent, which agent it is.

// SidebarKind is what a row describes.
type SidebarKind uint8

const (
	// SidebarHeading is a section title, such as "spaces".
	SidebarHeading SidebarKind = iota
	// SidebarSpace is a workspace, with its branch beneath.
	SidebarSpace
	// SidebarAgent is a running agent, with its kind beneath.
	SidebarAgent
	// SidebarGroup is a tab heading, shown when the agent list is grouped.
	SidebarGroup
	// SidebarSpaceGroup is a heading over spaces kept together, which folds.
	SidebarSpaceGroup
	// SidebarAction is a row that does something rather than going somewhere.
	SidebarAction
	// SidebarBlank is a spacer.
	SidebarBlank
	// SidebarMachine is a saved machine, with its spaces beneath it: herdr's
	// endpoint row, shown once there is more than one machine to be on.
	SidebarMachine
)

// Action names carried on an action row.
const (
	ActionNewSpace      = "new-space"
	ActionOpenMenu      = "open-menu"
	ActionToggleGrouped = "toggle-grouped"
	// ActionToggleGroup folds or unfolds the group named by the row.
	ActionToggleGroup = "toggle-group"
	// ActionHideSidebar puts the whole sidebar away.
	ActionHideSidebar = "hide-sidebar"
	// ActionMachine is a machine row's: fold the one being shown, go to
	// another.
	ActionMachine = "machine"
	// ActionCompanies is the mark beside the "spaces" heading: the
	// companies panel, which chooses what the list shows.
	ActionCompanies = "companies"
)

// SidebarRow is one entry. A two-line entry is one row: the detail is drawn
// beneath the name, so selecting and hit-testing count entries rather than
// lines.
type SidebarRow struct {
	Kind   SidebarKind
	Label  string
	Detail string
	// Trailing is drawn against the right edge, such as the "grouped" toggle
	// or the "menu" button. TrailingAction is what clicking it does, which is
	// not what clicking the rest of the row does.
	Trailing       string
	TrailingAction string

	// Pane, Tab and Workspace say where selecting this row goes. Action names
	// what it does instead.
	Pane      uint64
	Tab       uint64
	Workspace uint64
	Action    string

	// Group is the group a row belongs to, or names when it is the heading
	// over one. Depth indents a row under its heading.
	Group  string
	Depth  int
	Folded bool

	// Machine is the saved machine a row belongs to when it is not the one
	// being shown, and empty for the one that is. A space's number means
	// something only on its own machine, so a row with a Machine is one to go
	// to, never one to act on here.
	Machine string
	// Stale marks a row whose machine cannot be reached: what it shows is
	// the last that was heard, dimmed as herdr dims it.
	Stale bool

	State   string
	Running bool

	// Lines is the entry laid out as token rows ([ui.sidebar.*]), used in
	// place of Label and Detail when it is set; Gap is the blank lines after
	// the entry (row_gap).
	Lines [][]SidebarToken
	Gap   int
	// DropHere marks the space a dragged one would take the place of, or the
	// group heading it would join.
	DropHere bool
	// Symbols draws the state as herdr's distinct glyphs rather than dots
	// (status_indicators = "symbols").
	Symbols bool

	// Active marks what is currently shown; Selected marks the navigation
	// cursor. They are separate because moving the cursor must not move the
	// view — reading down the list has not left the pane being worked in.
	Active   bool
	Selected bool
}

// height is how many screen lines the row occupies.
func (r SidebarRow) height() int {
	if len(r.Lines) > 0 {
		return len(r.Lines) + r.Gap
	}
	if r.Detail != "" {
		return 2
	}
	return 1
}

// SidebarWidth is how many columns the sidebar takes unless a frame says
// otherwise (Frame.SidebarWidth): herdr's default. Wide enough for a
// repository name and a state marker, narrow enough to cost a pane little.
const SidebarWidth = 26

// The bounds a dragged sidebar is kept within, herdr's sidebar_min_width
// and sidebar_max_width defaults.
const (
	SidebarMinWidth = 18
	SidebarMaxWidth = 36
)

// sidebarWidthOf is the sidebar's width in a frame.
func sidebarWidthOf(f Frame) int {
	if f.SidebarWidth > 0 {
		return f.SidebarWidth
	}
	return SidebarWidth
}

// SidebarWidthAt is the width a drag of the sidebar's edge to column x
// asks for, herdr's set_sidebar_width_from_column: the edge under the
// pointer, kept within min and max.
func SidebarWidthAt(x, minWidth, maxWidth int) int {
	return min(max(x+1, minWidth), maxWidth)
}

// SidebarColumns is how many columns the sidebar occupies in a frame, which is
// none when it is hidden.
//
// It is the left edge of everything else on screen, which is why the tab bar
// and the sidebar both measure from it rather than each deciding for itself.
func SidebarColumns(f Frame, cols int) int {
	if !f.Sidebar {
		return 0
	}
	return min(sidebarWidthOf(f), cols)
}

// TrailingStart is the column where a row's trailing label begins, or -1 when
// it has none or there is no room for it.
//
// Drawing and hit-testing both ask this, so a button cannot be drawn in one
// place and clicked in another.
func TrailingStart(r SidebarRow, width int) int {
	if r.Trailing == "" {
		return -1
	}
	at := width - 2 - len([]rune(r.Trailing))
	if at <= 1 {
		return -1
	}
	return at
}

// The sidebar is two lists, not one scrolling column of both.
//
// They answer different questions — where else could I be, and which agent
// needs me — and one of them is usually the reason the program is open. Sharing
// a single scroll meant the spaces pushed the agents off the bottom, so the
// list that matters most was the one you could not see. They get a region each
// now, with a divider the user can move, and each scrolls on its own.

// SidebarSection is one of the two lists.
type SidebarSection struct {
	Rows []SidebarRow
	// Footer sits against the bottom of the section, below the scrolling
	// rows. Buttons belong at the edge of the thing they act on, and one that
	// floats after the last entry moves every time the list grows — so the
	// place to reach for it changes with something the user did not do.
	Footer []SidebarRow
	// Scroll is how many entries are scrolled off the top of this list. It is
	// clamped where it is drawn, so a client need not track what fits.
	Scroll int
}

// footerHeight is how many lines a section's footer takes.
func footerHeight(s SidebarSection) int {
	total := 0
	for _, r := range s.Footer {
		total += r.height()
	}
	return total
}

// SidebarHeight is how many lines the sidebar has, divider included.
func SidebarHeight(rows int) int { return max(rows-StatusRows, 0) }

// sidebarMinSection is the fewest lines a section keeps when the divider is
// dragged at it. One line is not a list, it is a heading with nothing under it.
const sidebarMinSection = 3

// agentsFloor and agentsCeiling bound the strip the agents get by default.
//
// The floor is what makes it a place rather than a remainder: the list people
// watch should not have to earn its room by already being full, and without
// slack the first agent to appear has nowhere to appear. The ceiling keeps the
// spaces the larger of the two, since a space goes on existing and an agent is
// only what happens to be running.
func agentsFloor(height int) int   { return max(sidebarMinSection, height/3) }
func agentsCeiling(height int) int { return max(agentsFloor(height), height/2) }

// SidebarSplitAt is the line the divider sits on: the spaces list is above it
// and the agents list below.
//
// A split of zero means "decide for me", which is what a client that has never
// been dragged passes. The answer is as much as the spaces need and no more
// than half, so a session with two spaces does not spend half the column on
// them and a session with twenty does not bury the agents.
func SidebarSplitAt(f Frame, rows int) int {
	height := SidebarHeight(rows)
	// The toolbar, when there is one, is above both lists: they share what
	// is under it.
	top := toolbarTop(f, rows)
	if height-top < 2*sidebarMinSection+1 {
		// No room to divide. The spaces take what there is; the agents list
		// is the one that can be reached by other means.
		return height
	}

	at := f.SidebarSplit
	if at <= 0 {
		// The spaces get the room by default and the agents a contained strip
		// at the bottom. A space is a place that goes on existing; an agent is
		// what happens to be running, and there are rarely many at once. The
		// split going the other way meant a long list of places scrolled in a
		// keyhole under a mostly empty one.
		// A floor as well as a ceiling. Sizing the strip to what is in it
		// means an empty one is three lines and the first agent to appear has
		// nowhere to appear — and the list people watch should not have to
		// earn its room by already being full.
		want := sectionHeight(f.Agents) + footerHeight(f.Agents)
		want = min(max(want, agentsFloor(height)), agentsCeiling(height))
		at = height - want - 1
	}
	return min(max(at, top+sidebarMinSection), height-sidebarMinSection-1)
}

// sectionHeight is how many lines a section's entries would take in full.
func sectionHeight(s SidebarSection) int {
	total := 0
	for _, r := range s.Rows {
		total += r.height()
	}
	return total
}

// pinnedRows is how many rows at the top of a section stay put while the rest
// scrolls under them.
//
// It is the heading, when there is one. A list whose title scrolls away leaves
// the reader looking at names with nothing saying what they are names of, and
// the toggle that lives in the heading would be unreachable at the same time.
func pinnedRows(s SidebarSection) int {
	if len(s.Rows) > 0 && s.Rows[0].Kind == SidebarHeading {
		return 1
	}
	return 0
}

// scrollable splits a section into the part that stays and the part that moves.
func scrollable(s SidebarSection, height int) (pinned []SidebarRow, rest []SidebarRow, room int) {
	at := pinnedRows(s)
	pinned = s.Rows[:at]
	for _, r := range pinned {
		height -= r.height()
	}
	return pinned, s.Rows[at:], max(height-footerHeight(s), 0)
}

// sidebarRegions returns the lines each list is drawn in, and the divider row.
// A divider of -1 means there is no room for one.
func sidebarRegions(f Frame, rows int) (spaces, agents Rect, divider int) {
	height := SidebarHeight(rows)
	top := toolbarTop(f, rows)
	at := SidebarSplitAt(f, rows)
	if at >= height {
		return Rect{Y: top, Rows: height - top}, Rect{}, -1
	}
	return Rect{Y: top, Rows: at - top},
		Rect{Y: at + 1, Rows: height - at - 1},
		at
}

// SidebarMaxScroll is the furthest a section can be scrolled and still fill
// its region: scrolling past that would leave a gap at the bottom and nothing
// new at the top.
//
// It counts in entries rather than lines. A two-line entry is one thing, and
// scrolling half of one off the top would put a branch under a heading it does
// not belong to.
func SidebarMaxScroll(s SidebarSection, height int) int {
	_, rest, room := scrollable(s, height)
	total := 0
	for _, r := range rest {
		total += r.height()
	}
	if total <= room {
		return 0
	}
	at := len(rest)
	fits := 0
	for at > 0 {
		next := fits + rest[at-1].height()
		if next > room {
			break
		}
		fits = next
		at--
	}
	return at
}

// sidebarScroll is the offset actually used, which is the one asked for
// clamped to what there is to scroll.
func sidebarScroll(s SidebarSection, height int) int {
	return min(max(s.Scroll, 0), SidebarMaxScroll(s, height))
}

// SidebarScrollbar is a list's scrollbar, herdr's render_list_scrollbar: a
// track down the list's right edge, beside the entries that scroll, and a thumb
// saying how much of the list is in view and where. The arrows it replaces said
// only that there was more; a thumb says how much, and can be taken hold of.
//
// Rows are screen lines. The offset it maps to counts in entries, as the
// sidebar's scroll does everywhere else.
type SidebarScrollbar struct {
	// X, Y and Rows are the track.
	X, Y, Rows int
	// ThumbTop is the screen line the thumb starts on, ThumbLen its lines.
	ThumbTop, ThumbLen int
	// Max is the section's SidebarMaxScroll.
	Max int
}

// OnThumb reports whether a line is on the thumb.
func (b SidebarScrollbar) OnThumb(y int) bool {
	return y >= b.ThumbTop && y < b.ThumbTop+b.ThumbLen
}

// ScrollAt is the offset that puts the thumb's grab line on a screen line:
// grab is how far into the thumb it was taken hold of. herdr's
// scrollbar_offset_from_drag_row; a click on the track is the same with the
// thumb held by its middle (scrollbar_offset_from_row), so it centres there.
func (b SidebarScrollbar) ScrollAt(y, grab int) int {
	maxTop := b.Rows - b.ThumbLen
	if maxTop <= 0 || b.Max <= 0 {
		return 0
	}
	top := min(max(y-b.Y-grab, 0), maxTop)
	return roundDiv(top*b.Max, maxTop)
}

// roundDiv is a/b rounded to the nearest, as herdr's f32 round is, for the
// non-negative numbers a scrollbar deals in.
func roundDiv(a, b int) int { return (2*a + b) / (2 * b) }

// sectionScrollbar is a section's scrollbar, and whether it has one: only a
// list with more than fits does, as only it has somewhere to go (herdr's
// should_show_scrollbar), and only a column wide enough to give one up.
//
// The track stops short of the hide handle, which owns the column's last
// corner: a list reaching the bottom of the sidebar would otherwise draw over
// it, and a press there would have two meanings.
func sectionScrollbar(s SidebarSection, region Rect, width, handleRow int) (SidebarScrollbar, bool) {
	maxScroll := SidebarMaxScroll(s, region.Rows)
	limit := width - 1
	if maxScroll == 0 || limit < 4 {
		return SidebarScrollbar{}, false
	}
	pinned, rest, room := scrollable(s, region.Rows)
	top := region.Y
	for _, r := range pinned {
		top += r.height()
	}
	rows := room
	if handleRow >= top && handleRow < top+rows {
		rows = handleRow - top
	}
	if rows <= 0 || len(rest) == 0 {
		return SidebarScrollbar{}, false
	}

	// herdr's scrollbar_thumb, with entries for its lines: the thumb is the
	// share of the list in view, and sits as far down the track as the list
	// is scrolled down its length.
	viewport := len(rest) - maxScroll
	length := min(max(roundDiv(viewport*rows, len(rest)), 1), rows)
	maxTop := rows - length
	at := 0
	if maxTop > 0 {
		at = min(roundDiv(sidebarScroll(s, region.Rows)*maxTop, maxScroll), maxTop)
	}
	return SidebarScrollbar{
		X: limit - 1, Y: top, Rows: rows,
		ThumbTop: top + at, ThumbLen: length,
		Max: maxScroll,
	}, true
}

// SidebarScrollbarAt is the list a point is on the scrollbar of, and that
// scrollbar; ok is false when the point is on neither list's.
func SidebarScrollbarAt(f Frame, x, y, rows int) (list SidebarPlace, bar SidebarScrollbar, ok bool) {
	if !f.Sidebar || y >= SidebarHeight(rows) {
		return SidebarNowhere, SidebarScrollbar{}, false
	}
	for _, list := range []SidebarPlace{SidebarSpacesList, SidebarAgentsList} {
		bar, ok := SidebarScrollbarOf(f, rows, list)
		if ok && x == bar.X && y >= bar.Y && y < bar.Y+bar.Rows {
			return list, bar, true
		}
	}
	return SidebarNowhere, SidebarScrollbar{}, false
}

// SidebarScrollbarOf is one list's scrollbar, which a drag that started on it
// follows wherever the pointer goes afterwards.
func SidebarScrollbarOf(f Frame, rows int, list SidebarPlace) (SidebarScrollbar, bool) {
	if !f.Sidebar {
		return SidebarScrollbar{}, false
	}
	spaces, agents, _ := sidebarRegions(f, rows)
	width := sidebarWidthOf(f)
	switch list {
	case SidebarSpacesList:
		return sectionScrollbar(f.Spaces, spaces, width, hideHandleRow(f, rows))
	case SidebarAgentsList:
		if agents.Rows > 0 {
			return sectionScrollbar(f.Agents, agents, width, hideHandleRow(f, rows))
		}
	}
	return SidebarScrollbar{}, false
}

// drawScrollbar draws a track and its thumb down the list's right edge: the
// track herdr's thin dim line, the thumb a thicker block in the accent.
//
// herdr draws both with one glyph in two colours (surface_dim, overlay0). Here
// the tokens those would map to — Border and SidebarGroup — are the same muted
// style in every theme, so the thumb vanished into the track and the bar read
// as a fixed rule that did not move. The accent and a wider glyph keep the two
// apart whatever the theme, including the terminal's own colours.
func drawScrollbar(dst *vt.Grid, b SidebarScrollbar, theme Theme) {
	for y := b.Y; y < b.Y+b.Rows; y++ {
		cell := vt.Cell{R: '▕', Style: theme.Border, Width: 1}
		if b.OnThumb(y) {
			cell = vt.Cell{R: '▐', Style: theme.SidebarGroupActive, Width: 1}
		}
		if row := dst.Line(y); row != nil {
			row.SetCell(b.X, cell)
		}
	}
}

// drawSidebar draws the two lists down the left edge.
func drawSidebar(dst *vt.Grid, f Frame, theme Theme) {
	if !f.Sidebar {
		return
	}
	// The sidebar runs from the very top: it is not inside the tab bar's
	// space, the tab bar is inside its own.
	bottom := SidebarHeight(dst.Rows())
	width := SidebarColumns(f, dst.Cols())

	for y := 0; y < bottom; y++ {
		row := dst.Line(y)
		if row == nil {
			continue
		}
		for x := 0; x < width; x++ {
			row.SetCell(x, vt.Cell{R: ' ', Style: theme.Sidebar, Width: 1})
		}
		// A rule down the right edge, so the sidebar reads as its own column
		// rather than as text spilling into the first pane.
		row.SetCell(width-1, vt.Cell{R: '│', Style: theme.Border, Width: 1})
	}

	if toolbarShown(f, dst.Rows()) {
		drawToolbar(dst, f, width, theme)
	}
	spaces, agents, divider := sidebarRegions(f, dst.Rows())
	handle := hideHandleRow(f, dst.Rows())
	drawSection(dst, f.Spaces, spaces, width, handle, theme)
	if divider >= 0 {
		drawSidebarDivider(dst, divider, width, theme)
		drawSection(dst, f.Agents, agents, width, handle, theme)
	}

	// The handle that puts the column away, in the corner it would leave
	// behind. A keystroke does the same thing, but somebody who found the
	// sidebar with the mouse should be able to dismiss it the same way.
	if at := hideHandleRow(f, dst.Rows()); at >= 0 {
		writeString(dst, width-2, at, "«", theme.SidebarGroup, width)
	}
}

// hideHandleRow is the line the collapse handle sits on: the last line of the
// sidebar, when there is one to spare.
func hideHandleRow(f Frame, rows int) int {
	height := SidebarHeight(rows)
	if height <= 0 {
		return -1
	}
	return height - 1
}

// ShowHandleWidth is the gutter kept for the handle that brings the sidebar
// back once it has been put away.
//
// Two columns, always there when the sidebar is hidden: a way out that is only
// a keystroke is one that somebody who arrived by mouse cannot find again.
const ShowHandleWidth = 2

// SidebarGutter is the column everything to the right of the sidebar starts
// at, whether that is the sidebar itself or the handle that brings it back.
func SidebarGutter(f Frame, cols int) int {
	if f.Sidebar {
		return SidebarColumns(f, cols)
	}
	return min(ShowHandleWidth, cols)
}

// drawShowHandle puts the handle in the gutter while the sidebar is away.
func drawShowHandle(dst *vt.Grid, f Frame, theme Theme) {
	if f.Sidebar {
		return
	}
	writeString(dst, 0, 0, "»", theme.SidebarGroup, dst.Cols())
}

// SidebarHandleAt reports whether a point is on the handle that shows or hides
// the sidebar, which is the same affordance in its two states.
func SidebarHandleAt(f Frame, x, y, rows int) bool {
	if !f.Sidebar {
		return y == 0 && x < ShowHandleWidth
	}
	width := sidebarWidthOf(f)
	return y == hideHandleRow(f, rows) && x >= width-2 && x < width
}

// drawSidebarDivider draws the line between the lists.
//
// It is drawn as something to grab rather than as a rule: the handle in the
// middle is the only thing saying the line can be moved, and a plain rule
// would read as decoration.
func drawSidebarDivider(dst *vt.Grid, y, width int, theme Theme) {
	row := dst.Line(y)
	if row == nil {
		return
	}
	for x := 0; x < width-1; x++ {
		row.SetCell(x, vt.Cell{R: '─', Style: theme.Border, Width: 1})
	}
	if handle := (width - 1 - 4) / 2; handle > 0 {
		writeString(dst, handle, y, "────", theme.SidebarGroupActive, width-1)
	}
}

// drawSection draws one list inside its region.
func drawSection(dst *vt.Grid, s SidebarSection, region Rect, width, handleRow int, theme Theme) {
	if region.Rows <= 0 {
		return
	}
	limit := width - 1
	pinned, rest, room := scrollable(s, region.Rows)
	from := sidebarScroll(s, region.Rows)
	bar, scrolls := sectionScrollbar(s, region, width, handleRow)

	y := region.Y
	for _, r := range pinned {
		drawSidebarRow(dst, r, y, limit, theme)
		y += r.height()
	}

	// The entries give the scrollbar its column when there is one, as
	// herdr's list does, so a name is cut short rather than drawn under it.
	head := y
	for _, r := range rest[min(from, len(rest)):] {
		if y+r.height() > head+room {
			break
		}
		drawSidebarRow(dst, r, y, entryLimit(limit, scrolls), theme)
		y += r.height()
	}

	// The footer is drawn against the bottom of the region rather than after
	// the last entry, so it stays where the hand expects it however long the
	// list gets.
	at := region.Y + region.Rows - footerHeight(s)
	for _, r := range s.Footer {
		drawSidebarRow(dst, r, at, limit, theme)
		at += r.height()
	}

	// Say that there is more, and how much. A list that silently ends is one
	// the user believes they have seen all of, which is how a waiting agent
	// goes unnoticed below the fold.
	if scrolls {
		drawScrollbar(dst, bar, theme)
	}
}

// entryLimit is the column a scrolling entry is drawn up to: one short of the
// sidebar's when the list has a scrollbar to make room for. Drawing and
// hit-testing both ask it, so a row's button is clicked where it is drawn.
func entryLimit(limit int, scrollbar bool) int {
	if scrollbar {
		return limit - 1
	}
	return limit
}

func drawSidebarRow(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	switch r.Kind {
	case SidebarBlank:
		return

	case SidebarHeading:
		writeString(dst, 1, y, truncate(r.Label, limit-1), theme.SidebarGroup, limit)
		drawTrailing(dst, r, y, limit, theme)

	case SidebarGroup:
		writeString(dst, 3, y, truncate(r.Label, limit-3), theme.SidebarGroup, limit)

	case SidebarSpaceGroup:
		// The triangle points at what it will do: down when the group is open
		// and its spaces are below, right when it is shut and they are not.
		marker := "▼ "
		if r.Folded {
			marker = "▶ "
		}
		style := theme.SidebarGroup
		if r.Active {
			style = theme.SidebarGroupActive
		}
		if r.DropHere {
			// A dragged space let go here joins the group.
			writeString(dst, 0, y, "▎", theme.BorderFocused, limit)
		}
		x := writeString(dst, 1, y, indent(r.Depth)+marker, style, limit)
		writeString(dst, x, y, truncate(r.Label, limit-x), style, limit)
		drawTrailing(dst, r, y, limit, theme)

	case SidebarMachine:
		// herdr's endpoint row: the triangle, the machine's name in bold, and
		// against the edge whether it can be reached.
		marker := "▾ "
		if r.Folded {
			marker = "▸ "
		}
		style := theme.SidebarGroupActive
		style.Attrs |= vt.AttrBold
		if r.Stale {
			style = theme.SidebarDetail
		}
		x := writeString(dst, 1, y, marker, style, limit)
		writeString(dst, x, y, truncate(r.Label, limit-x), style, limit)
		if at := TrailingStart(r, limit+1); at >= 0 {
			writeString(dst, at, y, r.Trailing, machineSignalStyle(r.State, theme), limit)
		}

	case SidebarAction:
		style := theme.SidebarGroup
		if r.Selected {
			style = theme.SidebarActive
		}
		writeString(dst, 1, y, truncate(r.Label, limit-1), style, limit)
		drawTrailing(dst, r, y, limit, theme)

	case SidebarSpace, SidebarAgent:
		drawSidebarEntry(dst, r, y, limit, theme)
	}
}

// machineSignalStyle is the colour of a machine's reachability, herdr's
// endpoint_status_presentation: green online, yellow on the way, red when it
// needs somebody.
func machineSignalStyle(state string, theme Theme) vt.Style {
	switch state {
	case MachineOnline:
		return theme.Idle
	case MachineConnecting, MachineReconnecting:
		return theme.Working
	case MachineAttention:
		return theme.Blocked
	}
	return theme.SidebarDetail
}

// A machine row's State, and the glyph herdr shows for each.
const (
	MachineConnecting   = "connecting"
	MachineOnline       = "online"
	MachineReconnecting = "reconnecting"
	MachineAttention    = "attention"
	MachineDisabled     = "disabled"
)

// MachineSignal is what a machine row shows against the edge: the glyph alone
// when it is online, the glyph and the word otherwise.
func MachineSignal(state string) string {
	glyph := map[string]string{
		MachineConnecting: "◐", MachineOnline: "●", MachineReconnecting: "◐",
		MachineAttention: "!", MachineDisabled: "·",
	}[state]
	if state == MachineOnline || glyph == "" {
		return glyph
	}
	return glyph + " " + state
}

// drawTrailing puts a row's button against the right edge. A toggle belongs
// at the edge of what it toggles, not trailing the words that name it.
func drawTrailing(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	at := TrailingStart(r, limit+1)
	if at < 0 {
		return
	}
	style := theme.SidebarGroup
	if r.Active {
		style = theme.SidebarGroupActive
	}
	writeString(dst, at, y, r.Trailing, style, limit)
}

// drawSidebarEntry draws a name with its marker, and its detail beneath.
func drawSidebarEntry(dst *vt.Grid, r SidebarRow, y, limit int, theme Theme) {
	// The cursor is a marker in the margin rather than a highlight, so it does
	// not compete with the marker saying which entry is current.
	cursor := " "
	if r.Selected {
		cursor = "›"
	}
	style := theme.Sidebar
	mark := theme.StateStyle(r.State, r.Running)
	if r.Selected && !r.Active && theme.SidebarCursor != (vt.Style{}) {
		// herdr's selection_bg under the navigation cursor.
		style = theme.SidebarCursor
		for i := 0; i < r.height()-r.Gap; i++ {
			fill(dst, y+i, 0, limit, style)
		}
	}
	if r.Stale {
		style, mark = theme.SidebarDetail, theme.SidebarDetail
	}
	if r.Active {
		// The current entry is a band rather than a word that changed weight:
		// scanning twenty rows, weight is not enough to find one.
		style = theme.SidebarSelected
		mark = style
		fill(dst, y, 0, limit, style)
	}

	x := writeString(dst, 0, y, cursor, style, limit)
	if r.DropHere {
		// Where the dragged space will land, in the accent herdr marks it
		// with.
		writeString(dst, 0, y, "▎", theme.BorderFocused, limit)
	}
	x = writeString(dst, x, y, indent(r.Depth), style, limit)
	if len(r.Lines) > 0 {
		drawTokenEntry(dst, r, x, y, limit, style, mark, theme)
		return
	}
	x = writeString(dst, x, y, stateCircle(r), mark, limit)
	writeString(dst, x, y, truncate(r.Label, limit-x), style, limit)

	if r.Detail != "" {
		detail := theme.SidebarDetail
		if r.Active {
			detail = style
			fill(dst, y+1, 0, limit, style)
		}
		// Lined up under the name rather than under the marker: the detail
		// belongs to the name, and a column of branches that steps in and out
		// with the tree is harder to read down than one that does not.
		at := 3 + 2*r.Depth
		writeString(dst, at, y+1, truncate(r.Detail, limit-at), detail, limit)
	}
}

// drawTokenEntry draws an entry laid out as token rows. The first row starts
// after the cursor and indent, the rest line up under the name as a detail
// line does; an entry in view is a band across all of its rows.
func drawTokenEntry(dst *vt.Grid, r SidebarRow, x, y, limit int, style, mark vt.Style, theme Theme) {
	st := tokenStyles{
		icon:      mark,
		iconText:  strings.TrimSpace(stateCircle(r)),
		stateText: theme.StateStyle(r.State, r.Running),
		primary:   style,
		secondary: theme.SidebarDetail,
		value:     theme.SidebarDetail,
		separator: theme.SidebarDetail,
		ahead:     theme.Idle,
		behind:    theme.Blocked,
	}
	if r.Active {
		st.stateText, st.secondary, st.value, st.separator, st.ahead, st.behind =
			style, style, style, style, style, style
	}
	for i, line := range r.Lines {
		at := 3 + 2*r.Depth
		if i == 0 {
			at = x + 1 // the space before the mark, as the plain row has
		} else if r.Active {
			fill(dst, y+i, 0, limit, style)
		}
		drawTokenLine(dst, line, at, y+i, limit, st)
	}
}

// indent is the space a row sits in under its heading.
func indent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
}

// stateCircle is the mark beside an entry, padded as the plain row draws it.
func stateCircle(r SidebarRow) string {
	return " " + StatusIcon(r.State, r.Running, r.Symbols) + " "
}

// StatusIcon is herdr's status_icon: with dots, a filled mark for anything
// happening, hollow for idle, a dot for nothing known; with symbols, a glyph
// for each state, so the state reads without colour. Which entry is in view
// is the band across it, not the mark.
func StatusIcon(state string, running, symbols bool) string {
	if !running {
		state = "" // an exited pane says nothing about an agent
	}
	switch state {
	case "blocked":
		if symbols {
			return "×"
		}
		return "●"
	case "working":
		if symbols {
			return "◐"
		}
		return "●"
	case "done":
		if symbols {
			return "✓"
		}
		return "●"
	case "idle":
		return "○"
	}
	return "·"
}

// SidebarPlace names which of the two lists a point is in.
type SidebarPlace uint8

const (
	// SidebarNowhere is outside the sidebar.
	SidebarNowhere SidebarPlace = iota
	// SidebarSpacesList and SidebarAgentsList are the two regions.
	SidebarSpacesList
	SidebarAgentsList
	// SidebarDivider is the line between them, which can be dragged.
	SidebarDivider
	// SidebarEdge is the rule down the sidebar's right edge, which dragged
	// sets its width.
	SidebarEdge
	// SidebarToolbar is the toolbar over the lists (toolbar.go).
	SidebarToolbar
)

// SidebarPlaceAt says what is under a point.
//
// Drawing and hit-testing share sidebarRegions, so a click cannot land
// somewhere other than what it looks like it is on.
func SidebarPlaceAt(f Frame, x, y, rows int) SidebarPlace {
	if !f.Sidebar || x >= sidebarWidthOf(f) || y >= SidebarHeight(rows) {
		return SidebarNowhere
	}
	// The rule down the right edge is herdr's sidebar divider: dragged, it
	// sets the sidebar's width. Not on the hide handle's row, whose corner
	// is the handle.
	if x == sidebarWidthOf(f)-1 && !SidebarHandleAt(f, x, y, rows) {
		return SidebarEdge
	}
	if y < toolbarTop(f, rows) {
		return SidebarToolbar
	}
	spaces, agents, divider := sidebarRegions(f, rows)
	switch {
	case y == divider:
		return SidebarDivider
	case y >= spaces.Y && y < spaces.Y+spaces.Rows:
		return SidebarSpacesList
	case agents.Rows > 0 && y >= agents.Y && y < agents.Y+agents.Rows:
		return SidebarAgentsList
	}
	return SidebarNowhere
}

// SidebarRowAt returns the row under a point, and whether there is one.
//
// Rows are found by walking the same heights the drawing uses, so a two-line
// entry is one target: clicking a branch selects the space it belongs to,
// which is what it looks like it should do.
func SidebarRowAt(f Frame, x, y, rows int) (SidebarRow, bool) {
	spaces, agents, _ := sidebarRegions(f, rows)
	width := sidebarWidthOf(f)
	switch SidebarPlaceAt(f, x, y, rows) {
	case SidebarSpacesList:
		return rowInSection(f.Spaces, spaces, width, hideHandleRow(f, rows), x, y)
	case SidebarAgentsList:
		return rowInSection(f.Agents, agents, width, hideHandleRow(f, rows), x, y)
	}
	return SidebarRow{}, false
}

func rowInSection(s SidebarSection, region Rect, width, handleRow, x, y int) (SidebarRow, bool) {
	// The footer is looked at first: it is drawn over the bottom of the
	// region, and a click there means the footer, not whatever entry would
	// have reached that far.
	at := region.Y + region.Rows - footerHeight(s)
	for _, r := range s.Footer {
		if y >= at && y < at+r.height() {
			return resolveTrailing(r, width, x, y, at), true
		}
		at += r.height()
	}

	pinned, rest, _ := scrollable(s, region.Rows)
	from := sidebarScroll(s, region.Rows)

	at = region.Y
	for _, r := range pinned {
		if y >= at && y < at+r.height() {
			return resolveTrailing(r, width, x, y, at), true
		}
		at += r.height()
	}
	_, scrolls := sectionScrollbar(s, region, width, handleRow)
	entries := entryLimit(width-1, scrolls) + 1
	for _, r := range rest[min(from, len(rest)):] {
		height := r.height()
		if y >= at && y < at+height {
			return resolveTrailing(r, entries, x, y, at), true
		}
		at += height
	}
	return SidebarRow{}, false
}

// resolveTrailing points a row at its trailing button when the click was on
// one. Without it the "menu" beside "new" would create a space, which is the
// one thing somebody reaching for a menu did not ask for.
func resolveTrailing(r SidebarRow, width, x, y, top int) SidebarRow {
	if start := TrailingStart(r, width); r.TrailingAction != "" && start >= 0 && x >= start && y == top {
		r.Action = r.TrailingAction
	}
	return r
}

// SidebarRevealScroll is the smallest offset that brings an entry into view,
// given where a section is scrolled now.
//
// Smallest on purpose: jumping to another space should move the list only as
// far as it must, so the entries around the one being left stay where the eye
// last saw them.
func SidebarRevealScroll(s SidebarSection, height, index int) int {
	_, rest, room := scrollable(s, height)
	index -= pinnedRows(s)
	if index < 0 || index >= len(rest) {
		// A pinned row is always in view, so nothing needs to move for it.
		return sidebarScroll(s, height)
	}

	at := sidebarScroll(s, height)
	if index < at {
		return index
	}
	for {
		used := 0
		for i := at; i <= index; i++ {
			used += rest[i].height()
		}
		if used <= room || at >= index {
			return at
		}
		at++
	}
}

// SidebarActiveRow is the entry a section should keep in view: the navigation
// cursor when it is up, and otherwise whatever is current.
func SidebarActiveRow(s SidebarSection) int {
	fallback := -1
	for i, r := range s.Rows {
		if r.Selected {
			return i
		}
		if fallback < 0 && r.Active && (r.Kind == SidebarSpace || r.Kind == SidebarAgent) {
			fallback = i
		}
	}
	return fallback
}

// SidebarRegions is where each list is drawn, for a client that needs to know
// how tall a section is before it can scroll or reveal inside it.
func SidebarRegions(f Frame, rows int) (spaces, agents Rect) {
	spaces, agents, _ = sidebarRegions(f, rows)
	return spaces, agents
}
