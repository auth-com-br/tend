package vt

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// MouseMode is how much mouse activity an application asked to receive.
type MouseMode uint8

const (
	MouseOff         MouseMode = iota
	MouseX10                   // DECSET 9: press only
	MouseNormal                // DECSET 1000: press and release
	MouseButtonEvent           // DECSET 1002: plus drag while a button is down
	MouseAnyEvent              // DECSET 1003: plus motion with no button
)

// MouseEncoding is how mouse reports are framed on the wire.
type MouseEncoding uint8

const (
	MouseEncodingX10   MouseEncoding = iota // the original byte-offset form
	MouseEncodingUTF8                       // DECSET 1005
	MouseEncodingSGR                        // DECSET 1006
	MouseEncodingURXVT                      // DECSET 1015
	MouseEncodingSGRPixels
)

// Modes is the set of terminal modes an application can toggle. It is plain
// data so a client can be handed a copy without touching the screen.
type Modes struct {
	AutoWrap       bool // DECAWM (7), on by default
	Origin         bool // DECOM (6)
	CursorVisible  bool // DECTCEM (25), on by default
	Insert         bool // IRM (4)
	ReverseVideo   bool // DECSCNM (5)
	ApplicationCur bool // DECCKM (1), changes what arrow keys send
	BracketedPaste bool // 2004
	// AlternateScroll (1007): on the alternate screen, with no mouse
	// reporting, the wheel is sent as the arrow keys — how a full-screen
	// program that does not take the mouse is scrolled by it. On by default,
	// as in herdr (ghostty) and most terminals; a program may turn it off.
	AlternateScroll bool
	FocusEvents     bool // 1004
	SyncOutput      bool // 2026
	Mouse           MouseMode
	MouseEncoding   MouseEncoding
}

// Cursor is the caret and the style new text takes.
type Cursor struct {
	X, Y  int
	Style Style

	// pendingWrap records that the last printable character landed in the
	// final column. The cursor stays on that column and the wrap happens when
	// the next character arrives, which is what keeps a line that exactly
	// fills the width from scrolling early.
	pendingWrap bool
}

// Screen is a terminal: a parser, the grid it writes into, and the modes that
// govern both. It implements io.Writer, so a PTY can be copied straight into
// it, and vt.Handler, which is how the parser drives it.
//
// A Screen is not safe for concurrent use. Callers that share one across
// goroutines must serialise access themselves, and should hold the lock for as
// short a window as possible: this sits on a per-byte path.
type Screen struct {
	// links is the table of hyperlinks cells point into, and linkIDs its
	// reverse; curLink is the link text printed now is under (OSC 8).
	links   []string
	linkIDs map[string]uint16
	curLink uint16

	// Reply receives what the terminal sends back upstream, such as cursor
	// position reports. Nil means the answers are dropped, which is right for
	// a screen used only to inspect output.
	Reply func([]byte)
	// OnBell is called for BEL. Nil means ignore.
	OnBell func()
	// OnTitle is called when the window title changes. Nil means ignore.
	OnTitle func(string)
	// OnProgress is called when the OSC 9 progress report changes.
	OnProgress func(string)
	// OnClipboard is called when the program asks for text to be put on the
	// clipboard. This is how a program that holds the mouse copies: it does
	// its own selecting, over its own scrollback, and hands over the result.
	// Dropping the request makes every such copy silently fail.
	OnClipboard func([]byte)

	cols, rows int

	main, alt *Grid
	grid      *Grid
	onAlt     bool

	cur   Cursor
	saved Cursor // DECSC on the active screen

	top, bottom int // scroll region, inclusive, 0-based

	modes    Modes
	tabs     []bool
	title    string
	progress string
	// status is the last OSC 7501 a program sent of itself, and statusRev
	// counts them (status.go).
	status    ProgramStatus
	statusRev uint64

	parser Parser
	// replyBuf is reused so cursor reports do not allocate per request.
	replyBuf []byte
	// kitty holds the images a program in this terminal has sent, and where
	// it asked for them. Nil until one does: most panes never do.
	kitty *kittyState
}

// NewScreen returns a cols × rows screen with the given number of scrollback
// lines. The alternate screen never keeps history, by design: applications
// that use it redraw in full.
func NewScreen(cols, rows, scrollback int) *Screen {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	s := &Screen{
		cols: cols,
		rows: rows,
		main: NewGrid(cols, rows, scrollback),
		alt:  NewGrid(cols, rows, 0),
		modes: Modes{
			AutoWrap:        true,
			CursorVisible:   true,
			AlternateScroll: true,
		},
		top:    0,
		bottom: rows - 1,
	}
	s.grid = s.main
	s.resetTabs()
	return s
}

// Write feeds terminal output to the screen. It never fails; malformed input
// is absorbed by the parser.
func (s *Screen) Write(b []byte) (int, error) {
	s.parser.Parse(b, s)
	return len(b), nil
}

// Grid returns the active grid, main or alternate.
func (s *Screen) Grid() *Grid { return s.grid }

// MainGrid returns the primary grid, which owns the scrollback, whether or not
// it is currently displayed.
func (s *Screen) MainGrid() *Grid { return s.main }

// Cursor returns a copy of the cursor.
func (s *Screen) Cursor() Cursor { return s.cur }

// Modes returns a copy of the current modes.
func (s *Screen) Modes() Modes { return s.modes }

// IsAlt reports whether the alternate screen is displayed.
func (s *Screen) IsAlt() bool { return s.onAlt }

// WheelKeys reports whether the wheel over this screen should be sent as
// arrow keys: herdr's WheelRouting::AlternateScroll — the alternate screen,
// mode 1007 on, and no mouse reporting, which would take the wheel itself.
func (s *Screen) WheelKeys() bool {
	return s.onAlt && s.modes.AlternateScroll && s.modes.Mouse == MouseOff
}

// Title returns the last title set via OSC 0 or OSC 2.
func (s *Screen) Title() string { return s.title }

// Progress returns the last OSC 9 payload, without its leading command
// number: "4;1;-1" rather than "9;4;1;-1".
func (s *Screen) Progress() string { return s.progress }

// Size returns the screen dimensions.
func (s *Screen) Size() (cols, rows int) { return s.cols, s.rows }

// ScrollRegion returns the inclusive top and bottom rows of the scroll region.
func (s *Screen) ScrollRegion() (top, bottom int) { return s.top, s.bottom }

// Resize changes the screen size. Both grids are resized so that switching
// back to the alternate screen later finds it the right shape.
func (s *Screen) Resize(cols, rows int) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols == s.cols && rows == s.rows {
		return
	}
	fullRegion := s.top == 0 && s.bottom == s.rows-1
	erase := s.eraseStyle()

	if cols != s.cols {
		// The width changed, so lines that wrapped at the old width are broken
		// in the wrong places. Rewrapping is only right for the main screen:
		// an application on the alternate screen owns every cell of it and
		// redraws on the resize that is about to reach it.
		s.reflow(cols, rows, erase)
	} else {
		s.main.Resize(cols, rows, erase)
	}
	s.alt.Resize(cols, rows, erase)

	s.cols, s.rows = cols, rows
	if fullRegion {
		s.top, s.bottom = 0, rows-1
	} else {
		if s.bottom > rows-1 {
			s.bottom = rows - 1
		}
		if s.top > s.bottom {
			s.top = s.bottom
		}
	}
	s.resetTabs()
	s.clampCursor()
	s.cur.pendingWrap = false
}

// reflow rewraps the main screen to a new width, moving the cursor with the
// character it was sitting on.
func (s *Screen) reflow(cols, rows int, erase Style) {
	cursorRow := s.main.HistoryLen() + s.cur.Y
	lines, cursorLine, cursorOffset := collectLogical(s.main, cursorRow, s.cur.X)
	out := wrapLogical(lines, cols, cursorLine, cursorOffset, erase)
	applyReflow(s.main, out.rows, cols, rows, erase)

	if !s.onAlt {
		s.cur.Y = out.cursorRow - viewportOffset(len(out.rows), rows)
		s.cur.X = out.cursorCol
	}
	s.cur.pendingWrap = false
}

// eraseStyle is what an erased or newly exposed cell takes. Only the
// background carries over: this is what lets an application paint a coloured
// bar by setting a background and clearing a line.
func (s *Screen) eraseStyle() Style {
	return Style{BG: s.cur.Style.BG}
}

func (s *Screen) resetTabs() {
	s.tabs = make([]bool, s.cols)
	for x := 8; x < s.cols; x += 8 {
		s.tabs[x] = true
	}
}

func (s *Screen) clampCursor() {
	if s.cur.X < 0 {
		s.cur.X = 0
	}
	if s.cur.X >= s.cols {
		s.cur.X = s.cols - 1
	}
	if s.cur.Y < 0 {
		s.cur.Y = 0
	}
	if s.cur.Y >= s.rows {
		s.cur.Y = s.rows - 1
	}
}

// scrollsToHistory reports whether lines leaving the top of the screen should
// be retained.
//
// Only the main screen keeps history, and only when the scroll region covers
// the whole screen. A narrower region means an application is drawing a pane
// of its own; its discarded lines are not a log scrolling past, and feeding
// them to the scrollback would fill it with somebody else's redraws.
func (s *Screen) scrollsToHistory() bool {
	return !s.onAlt && s.top == 0 && s.bottom == s.rows-1
}

func (s *Screen) reply(b []byte) {
	if s.Reply != nil {
		s.Reply(b)
	}
}

func (s *Screen) replyf(format string, args ...any) {
	if s.Reply == nil {
		return
	}
	s.replyBuf = fmt.Appendf(s.replyBuf[:0], format, args...)
	s.Reply(s.replyBuf)
}

// --- Handler: printing -----------------------------------------------------

// Print places a rune at the cursor, handling wrap, wide characters and
// combining marks.
func (s *Screen) Print(r rune) {
	w := runewidth.RuneWidth(r)

	if w == 0 {
		// A combining mark belongs to the character already on screen, which
		// is the cell before the cursor — or the cursor's own cell when a
		// wrap is pending and the cursor has not moved off it yet.
		s.attachCombining(r)
		return
	}

	if s.cur.pendingWrap {
		if s.modes.AutoWrap {
			s.wrapLine()
		} else {
			s.cur.pendingWrap = false
		}
	}

	// A double-width character cannot straddle the right margin.
	if s.cur.X+w > s.cols {
		if s.modes.AutoWrap {
			// The columns it could not use are padding, not spaces. Writing
			// spaces here would put a gap into the middle of the line the next
			// time it is rewrapped.
			if row := s.grid.Line(s.cur.Y); row != nil {
				pad := spacerCell(s.eraseStyle())
				for x := s.cur.X; x < s.cols; x++ {
					row.SetCell(x, pad)
				}
			}
			s.wrapLine()
		} else {
			s.cur.X = s.cols - w
			if s.cur.X < 0 {
				s.cur.X = 0
				w = 1 // a one-column screen cannot hold a wide character
			}
		}
	}

	row := s.grid.Line(s.cur.Y)
	if row == nil {
		return
	}
	if s.modes.Insert {
		s.shiftRight(row, s.cur.X, w)
	}

	// Overwriting either half of an existing wide character must clear the
	// other half, or a stale continuation cell is left behind.
	s.breakWideAt(row, s.cur.X)
	s.breakWideAt(row, s.cur.X+w-1)

	row.SetCell(s.cur.X, Cell{R: r, Style: s.cur.Style, Width: uint8(w), Link: s.curLink})
	for i := 1; i < w; i++ {
		row.SetCell(s.cur.X+i, Cell{R: 0, Style: s.cur.Style, Width: 0, Link: s.curLink})
	}

	s.cur.X += w
	if s.cur.X >= s.cols {
		s.cur.X = s.cols - 1
		s.cur.pendingWrap = true
	}
}

func (s *Screen) attachCombining(mark rune) {
	row := s.grid.Line(s.cur.Y)
	if row == nil {
		return
	}
	x := s.cur.X
	if !s.cur.pendingWrap {
		x--
	}
	if x < 0 {
		return
	}
	// Attach to the start of a wide character, not to its continuation half.
	if row.Cell(x).IsContinuation() && x > 0 {
		x--
	}
	row.AddCombining(x, mark)
}

// breakWideAt clears the partner half when x lands on part of a wide character.
func (s *Screen) breakWideAt(row *Row, x int) {
	if x < 0 || x >= row.Len() {
		return
	}
	c := row.Cell(x)
	blank := blankCell(s.eraseStyle())
	switch {
	case c.Width == 2:
		if x+1 < row.Len() {
			row.SetCell(x+1, blank)
		}
	case c.IsContinuation():
		if x > 0 {
			row.SetCell(x-1, blank)
		}
		row.SetCell(x, blank)
	}
}

func (s *Screen) wrapLine() {
	if row := s.grid.Line(s.cur.Y); row != nil {
		row.SetWrapped(true)
	}
	s.cur.X = 0
	s.cur.pendingWrap = false
	s.lineFeed()
}

func (s *Screen) lineFeed() {
	switch {
	case s.cur.Y == s.bottom:
		s.grid.ScrollUp(s.top, s.bottom, 1, s.eraseStyle(), s.scrollsToHistory())
	case s.cur.Y < s.rows-1:
		s.cur.Y++
	}
}

func (s *Screen) reverseIndex() {
	if s.cur.Y == s.top {
		s.grid.ScrollDown(s.top, s.bottom, 1, s.eraseStyle())
		return
	}
	if s.cur.Y > 0 {
		s.cur.Y--
	}
}

// --- Handler: controls -----------------------------------------------------

// Execute handles a C0 control byte.
func (s *Screen) Execute(b byte) {
	switch b {
	case 0x07: // BEL
		if s.OnBell != nil {
			s.OnBell()
		}
	case 0x08: // BS
		if s.cur.pendingWrap {
			s.cur.pendingWrap = false
		} else if s.cur.X > 0 {
			s.cur.X--
		}
	case 0x09: // HT
		s.tabForward(1)
	case 0x0A, 0x0B, 0x0C: // LF, VT, FF
		s.cur.pendingWrap = false
		s.lineFeed()
	case 0x0D: // CR
		s.cur.X = 0
		s.cur.pendingWrap = false
	}
}

func (s *Screen) tabForward(n int) {
	s.cur.pendingWrap = false
	for ; n > 0; n-- {
		x := s.cur.X + 1
		for x < s.cols && !s.tabs[x] {
			x++
		}
		if x >= s.cols {
			s.cur.X = s.cols - 1
			return
		}
		s.cur.X = x
	}
}

func (s *Screen) tabBackward(n int) {
	s.cur.pendingWrap = false
	for ; n > 0; n-- {
		x := s.cur.X - 1
		for x > 0 && !s.tabs[x] {
			x--
		}
		if x <= 0 {
			s.cur.X = 0
			return
		}
		s.cur.X = x
	}
}

// ESCDispatch handles an escape sequence with no control-sequence body.
func (s *Screen) ESCDispatch(intermediates []byte, final byte, ignore bool) {
	if ignore {
		return
	}
	if len(intermediates) > 0 {
		// '#8' is DECALN, the alignment pattern. Character-set designations
		// such as ESC ( B are consumed and ignored: tend is UTF-8 only.
		if intermediates[0] == '#' && final == '8' {
			s.decaln()
		}
		return
	}
	switch final {
	case 'D': // IND
		s.cur.pendingWrap = false
		s.lineFeed()
	case 'E': // NEL
		s.cur.X = 0
		s.cur.pendingWrap = false
		s.lineFeed()
	case 'M': // RI
		s.cur.pendingWrap = false
		s.reverseIndex()
	case 'H': // HTS
		if s.cur.X < len(s.tabs) {
			s.tabs[s.cur.X] = true
		}
	case '7': // DECSC
		s.saved = s.cur
	case '8': // DECRC
		s.cur = s.saved
		s.clampCursor()
	case 'c': // RIS
		s.Reset()
	}
}

// decaln fills the screen with 'E', the DEC alignment pattern.
func (s *Screen) decaln() {
	for y := 0; y < s.rows; y++ {
		row := s.grid.Line(y)
		for x := 0; x < s.cols; x++ {
			row.SetCell(x, Cell{R: 'E', Width: 1})
		}
		row.SetWrapped(false)
	}
	s.cur.X, s.cur.Y = 0, 0
	s.cur.pendingWrap = false
}

// Reset returns the screen to its power-on state, keeping its dimensions.
func (s *Screen) Reset() {
	s.cur = Cursor{}
	s.saved = Cursor{}
	s.modes = Modes{AutoWrap: true, CursorVisible: true, AlternateScroll: true}
	s.top, s.bottom = 0, s.rows-1
	s.onAlt = false
	s.grid = s.main
	s.main.Clear(DefaultStyle)
	s.main.ClearHistory()
	s.alt.Clear(DefaultStyle)
	s.title = ""
	s.progress = ""
	if s.status != (ProgramStatus{}) {
		s.status = ProgramStatus{}
		s.statusRev++
	}
	s.resetTabs()
	s.parser.Reset()
}

// --- Handler: strings ------------------------------------------------------

// OSCDispatch handles operating-system commands.
//
// Two carry meaning for tend: the title commands, and OSC 9, which agents use
// to report progress. An agent can go from working to waiting without redrawing
// anything, so these fields are evidence the screen itself does not hold.
func (s *Screen) OSCDispatch(params [][]byte, _ bool) {
	if len(params) < 2 {
		return
	}
	switch string(params[0]) {
	case "0", "2": // icon+title, title
		s.setTitle(string(params[1]))
	case "9": // progress
		s.setProgress(joinParams(params[1:]))
	case "52": // clipboard
		s.clipboardWrite(params[1:])
	case "8": // hyperlink
		s.setHyperlink(params[1:])
	case "7501": // program status
		s.programStatus(joinParams(params[1:]))
	}
}

// maxLinks bounds a screen's table of hyperlinks. Past it no new link is
// kept — the text still shows, only unlinked — rather than letting a
// program that links every line grow the table without end.
const maxLinks = 4096

// maxLinkLen bounds one URI, as terminals commonly do.
const maxLinkLen = 2048

// setHyperlink is OSC 8: "8;params;URI" starts a link for the text printed
// after it, and an empty URI ends it. A URI may hold semicolons, which the
// parser split on, so what follows the parameters is joined back.
func (s *Screen) setHyperlink(params [][]byte) {
	if len(params) < 2 {
		s.curLink = 0
		return
	}
	uri := string(params[1])
	for _, p := range params[2:] {
		uri += ";" + string(p)
	}
	if uri == "" || len(uri) > maxLinkLen {
		s.curLink = 0
		return
	}
	if id, ok := s.linkIDs[uri]; ok {
		s.curLink = id
		return
	}
	if len(s.links) >= maxLinks {
		s.curLink = 0
		return
	}
	if s.links == nil {
		s.links = []string{""} // zero is no link
		s.linkIDs = map[string]uint16{}
	}
	id := uint16(len(s.links))
	s.links = append(s.links, uri)
	s.linkIDs[uri] = id
	s.curLink = id
}

// Hyperlink is the URI of a cell's link (OSC 8), or "".
func (s *Screen) Hyperlink(id uint16) string {
	if int(id) >= len(s.links) {
		return ""
	}
	return s.links[id]
}

// clipboardWrite handles OSC 52, "put this on the clipboard".
//
// Only writes are honoured. The same sequence with "?" for a payload asks to
// read the clipboard back, and answering that would let anything running in a
// pane see whatever the user last copied anywhere.
func (s *Screen) clipboardWrite(params [][]byte) {
	if s.OnClipboard == nil || len(params) < 2 {
		return
	}
	payload := params[len(params)-1]
	if len(payload) == 0 || string(payload) == "?" {
		return
	}
	text, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil || len(text) == 0 {
		return
	}
	s.OnClipboard(text)
}

// joinParams rebuilds an OSC payload minus its command number. Progress is
// matched as "4;1;-1" rather than "9;4;1;-1", so the command is dropped and
// the rest kept verbatim.
func joinParams(params [][]byte) string {
	n := 0
	for _, p := range params {
		n += len(p) + 1
	}
	var b strings.Builder
	b.Grow(n)
	for i, p := range params {
		if i > 0 {
			b.WriteByte(';')
		}
		b.Write(p)
	}
	return b.String()
}

func (s *Screen) setProgress(p string) {
	if s.progress == p {
		return
	}
	s.progress = p
	if s.OnProgress != nil {
		s.OnProgress(p)
	}
}

func (s *Screen) setTitle(t string) {
	if s.title == t {
		return
	}
	s.title = t
	if s.OnTitle != nil {
		s.OnTitle(t)
	}
}

// DCSHook, DCSPut, DCSUnhook and APCDispatch are accepted and discarded. The
// parser still tracks them so their payloads can never be mistaken for text.
func (s *Screen) DCSHook(*Params, []byte, byte, bool) {}
func (s *Screen) DCSPut(byte)                         {}
func (s *Screen) DCSUnhook()                          {}

// APCDispatch handles a kitty graphics command and discards anything else.
func (s *Screen) APCDispatch(data []byte) {
	if s.apcGraphics(data) {
		return
	}
}

var _ Handler = (*Screen)(nil)
