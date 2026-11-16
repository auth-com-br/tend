package vt

import "strconv"

// CSIDispatch executes a control sequence.
//
// Private sequences are identified by a leading marker byte, which the parser
// stores among the intermediates. Sequences tend does not implement are
// consumed silently: an unknown sequence must never reach the grid as text.
func (s *Screen) CSIDispatch(ps *Params, intermediates []byte, final byte, ignore bool) {
	if ignore {
		return
	}

	var marker byte
	var inter byte
	for _, b := range intermediates {
		if b >= 0x3C && b <= 0x3F {
			marker = b
		} else {
			inter = b
		}
	}

	if marker == '?' {
		switch final {
		case 'h':
			s.setPrivateModes(ps, true)
		case 'l':
			s.setPrivateModes(ps, false)
		case 'n':
			s.deviceStatusPrivate(ps)
		case 'u':
			// The kitty keyboard query: answering it is what makes a
			// program (Claude Code) turn the protocol on. Unanswered, it
			// takes the terminal to have none.
			s.reply([]byte("\x1b[?" + strconv.Itoa(int(s.modes.KittyKeyboard)) + "u"))
		}
		return
	}
	if final == 'u' && (marker == '>' || marker == '<' || marker == '=') {
		s.kittyKeyboard(marker, ps)
		return
	}
	if marker == '>' && final == 'm' {
		// xterm's modifyOtherKeys: CSI > 4 ; level m, and CSI > 4 m off.
		if ps.Get(0) == 4 {
			s.modes.ModifyOtherKeys = uint8(min(max(int(ps.Get(1)), 0), 2))
		}
		return
	}
	if marker != 0 {
		// '>' and '=' introduce secondary/tertiary device attributes.
		if marker == '>' && final == 'c' {
			s.reply([]byte("\x1b[>0;10;1c"))
		}
		return
	}

	switch final {
	case '@': // ICH
		s.insertChars(int(ps.GetOr(0, 1)))
	case 'A': // CUU
		s.moveBy(0, -int(ps.GetOr(0, 1)))
	case 'B', 'e': // CUD, VPR
		s.moveBy(0, int(ps.GetOr(0, 1)))
	case 'C', 'a': // CUF, HPR
		s.moveBy(int(ps.GetOr(0, 1)), 0)
	case 'D': // CUB
		s.moveBy(-int(ps.GetOr(0, 1)), 0)
	case 'E': // CNL
		s.cur.X = 0
		s.moveBy(0, int(ps.GetOr(0, 1)))
	case 'F': // CPL
		s.cur.X = 0
		s.moveBy(0, -int(ps.GetOr(0, 1)))
	case 'G', '`': // CHA, HPA
		s.moveTo(int(ps.GetOr(0, 1))-1, s.cursorRow())
	case 'H', 'f': // CUP, HVP
		s.moveTo(int(ps.GetOr(1, 1))-1, int(ps.GetOr(0, 1))-1)
	case 'I': // CHT
		s.tabForward(int(ps.GetOr(0, 1)))
	case 'J': // ED
		s.eraseInDisplay(ps.Get(0))
	case 'K': // EL
		s.eraseInLine(ps.Get(0))
	case 'L': // IL
		s.insertLines(int(ps.GetOr(0, 1)))
	case 'M': // DL
		s.deleteLines(int(ps.GetOr(0, 1)))
	case 'P': // DCH
		s.deleteChars(int(ps.GetOr(0, 1)))
	case 'S': // SU
		s.grid.ScrollUp(s.top, s.bottom, int(ps.GetOr(0, 1)), s.eraseStyle(), s.scrollsToHistory())
	case 'T': // SD
		s.grid.ScrollDown(s.top, s.bottom, int(ps.GetOr(0, 1)), s.eraseStyle())
	case 'X': // ECH
		s.eraseChars(int(ps.GetOr(0, 1)))
	case 'Z': // CBT
		s.tabBackward(int(ps.GetOr(0, 1)))
	case 'd': // VPA
		s.moveTo(s.cur.X, int(ps.GetOr(0, 1))-1)
	case 'g': // TBC
		s.clearTabs(ps.Get(0))
	case 'h': // SM
		s.setAnsiModes(ps, true)
	case 'l': // RM
		s.setAnsiModes(ps, false)
	case 'm': // SGR
		s.sgr(ps)
	case 'n': // DSR
		s.deviceStatus(ps)
	case 'r': // DECSTBM
		s.setScrollRegion(ps)
	case 's': // save cursor (ANSI.SYS form)
		s.saved = s.cur
	case 'u': // restore cursor
		s.cur = s.saved
		s.clampCursor()
	case 'c': // DA
		s.reply([]byte("\x1b[?62;22c"))
	case 'q':
		// DECSCUSR (' q') sets the cursor shape, which is a client concern;
		// consumed here so it never reaches the grid.
		_ = inter
	}
}

// cursorRow returns the cursor row in the coordinate space the application
// sees, which origin mode shifts to the top of the scroll region.
func (s *Screen) cursorRow() int {
	if s.modes.Origin {
		return s.cur.Y - s.top
	}
	return s.cur.Y
}

// moveTo places the cursor, honouring origin mode. Coordinates are 0-based.
func (s *Screen) moveTo(x, y int) {
	if s.modes.Origin {
		y += s.top
		if y < s.top {
			y = s.top
		}
		if y > s.bottom {
			y = s.bottom
		}
	}
	s.cur.X, s.cur.Y = x, y
	s.clampCursor()
	s.cur.pendingWrap = false
}

// moveBy is relative movement. It never crosses a scroll-region boundary the
// cursor already sits inside, which is what stops an application's own pane
// from scrolling the whole screen.
func (s *Screen) moveBy(dx, dy int) {
	x := s.cur.X + dx
	y := s.cur.Y + dy

	if dy < 0 && s.cur.Y >= s.top && y < s.top {
		y = s.top
	}
	if dy > 0 && s.cur.Y <= s.bottom && y > s.bottom {
		y = s.bottom
	}
	s.cur.X, s.cur.Y = x, y
	s.clampCursor()
	s.cur.pendingWrap = false
}

// --- erasing ---------------------------------------------------------------

func (s *Screen) eraseInDisplay(mode int32) {
	style := s.eraseStyle()
	switch mode {
	case 0: // cursor to end of screen
		s.grid.Line(s.cur.Y).clearRange(s.cur.X, s.cols, style)
		for y := s.cur.Y + 1; y < s.rows; y++ {
			s.grid.Line(y).reset(s.cols, style)
		}
	case 1: // start of screen to cursor
		for y := 0; y < s.cur.Y; y++ {
			s.grid.Line(y).reset(s.cols, style)
		}
		s.grid.Line(s.cur.Y).clearRange(0, s.cur.X+1, style)
	case 2: // whole screen
		s.grid.Clear(style)
	case 3: // scrollback
		s.grid.ClearHistory()
	}
	s.cur.pendingWrap = false
}

func (s *Screen) eraseInLine(mode int32) {
	row := s.grid.Line(s.cur.Y)
	if row == nil {
		return
	}
	style := s.eraseStyle()
	switch mode {
	case 0:
		row.clearRange(s.cur.X, s.cols, style)
	case 1:
		row.clearRange(0, s.cur.X+1, style)
	case 2:
		row.clearRange(0, s.cols, style)
	}
	s.cur.pendingWrap = false
}

func (s *Screen) eraseChars(n int) {
	row := s.grid.Line(s.cur.Y)
	if row == nil || n <= 0 {
		return
	}
	row.clearRange(s.cur.X, s.cur.X+n, s.eraseStyle())
	s.cur.pendingWrap = false
}

// --- insert and delete -----------------------------------------------------

func (s *Screen) insertChars(n int) {
	row := s.grid.Line(s.cur.Y)
	if row == nil || n <= 0 {
		return
	}
	row.shiftCells(s.cur.X, n, blankCell(s.eraseStyle()))
	s.cur.pendingWrap = false
}

func (s *Screen) deleteChars(n int) {
	row := s.grid.Line(s.cur.Y)
	if row == nil || n <= 0 {
		return
	}
	row.shiftCells(s.cur.X, -n, blankCell(s.eraseStyle()))
	s.cur.pendingWrap = false
}

// shiftRight is insert mode's half of printing: it opens n columns at x.
func (s *Screen) shiftRight(row *Row, x, n int) {
	row.shiftCells(x, n, blankCell(s.eraseStyle()))
}

func (s *Screen) insertLines(n int) {
	if s.cur.Y < s.top || s.cur.Y > s.bottom || n <= 0 {
		return
	}
	s.grid.ScrollDown(s.cur.Y, s.bottom, n, s.eraseStyle())
	s.cur.X = 0
	s.cur.pendingWrap = false
}

func (s *Screen) deleteLines(n int) {
	if s.cur.Y < s.top || s.cur.Y > s.bottom || n <= 0 {
		return
	}
	// Lines deleted by an application are not history: they were never
	// scrolled off the top of the screen.
	s.grid.ScrollUp(s.cur.Y, s.bottom, n, s.eraseStyle(), false)
	s.cur.X = 0
	s.cur.pendingWrap = false
}

// --- tabs and regions ------------------------------------------------------

func (s *Screen) clearTabs(mode int32) {
	switch mode {
	case 0:
		if s.cur.X < len(s.tabs) {
			s.tabs[s.cur.X] = false
		}
	case 3:
		for i := range s.tabs {
			s.tabs[i] = false
		}
	}
}

func (s *Screen) setScrollRegion(ps *Params) {
	top := int(ps.GetOr(0, 1)) - 1
	bottom := int(ps.GetOr(1, int32(s.rows))) - 1
	if bottom >= s.rows {
		bottom = s.rows - 1
	}
	if top < 0 {
		top = 0
	}
	// A degenerate region is rejected outright rather than clamped, which is
	// what stops a miscomputed region from pinning the cursor to one row.
	if top >= bottom {
		return
	}
	s.top, s.bottom = top, bottom
	// DECSTBM homes the cursor, inside the region when origin mode is on.
	s.moveTo(0, 0)
}

// --- modes -----------------------------------------------------------------

func (s *Screen) setAnsiModes(ps *Params, set bool) {
	for i := 0; i < ps.Len(); i++ {
		if ps.Get(i) == 4 { // IRM
			s.modes.Insert = set
		}
	}
}

func (s *Screen) setPrivateModes(ps *Params, set bool) {
	for i := 0; i < ps.Len(); i++ {
		switch ps.Get(i) {
		case 1: // DECCKM
			s.modes.ApplicationCur = set
		case 5: // DECSCNM
			s.modes.ReverseVideo = set
		case 6: // DECOM
			s.modes.Origin = set
			s.moveTo(0, 0)
		case 7: // DECAWM
			s.modes.AutoWrap = set
		case 25: // DECTCEM
			s.modes.CursorVisible = set
		case 9:
			s.setMouse(set, MouseX10)
		case 1000:
			s.setMouse(set, MouseNormal)
		case 1002:
			s.setMouse(set, MouseButtonEvent)
		case 1003:
			s.setMouse(set, MouseAnyEvent)
		case 1004:
			s.modes.FocusEvents = set
		case 1005:
			s.setMouseEncoding(set, MouseEncodingUTF8)
		case 1006:
			s.setMouseEncoding(set, MouseEncodingSGR)
		case 1015:
			s.setMouseEncoding(set, MouseEncodingURXVT)
		case 1016:
			s.setMouseEncoding(set, MouseEncodingSGRPixels)
		case 47:
			s.setAlt(set, false)
		case 1047:
			s.setAlt(set, true)
		case 1048:
			if set {
				s.saved = s.cur
			} else {
				s.cur = s.saved
				s.clampCursor()
			}
		case 1049:
			if set {
				s.saved = s.cur
				s.setAlt(true, false)
				s.alt.Clear(s.eraseStyle())
			} else {
				s.setAlt(false, false)
				s.cur = s.saved
				s.clampCursor()
			}
		case 1007:
			s.modes.AlternateScroll = set
		case 2004:
			s.modes.BracketedPaste = set
		case 2026:
			s.modes.SyncOutput = set
		}
	}
}

func (s *Screen) setMouse(set bool, mode MouseMode) {
	if set {
		s.modes.Mouse = mode
		return
	}
	// Only the mode that is actually active may turn tracking off, so a stale
	// reset for a mode the application never enabled cannot disable a newer one.
	if s.modes.Mouse == mode {
		s.modes.Mouse = MouseOff
	}
}

func (s *Screen) setMouseEncoding(set bool, enc MouseEncoding) {
	if set {
		s.modes.MouseEncoding = enc
		return
	}
	if s.modes.MouseEncoding == enc {
		s.modes.MouseEncoding = MouseEncodingX10
	}
}

// setAlt switches screens. clearOnExit is for mode 1047, which blanks the
// alternate screen as it leaves so returning to it later starts clean.
func (s *Screen) setAlt(on, clearOnExit bool) {
	if on == s.onAlt {
		return
	}
	if on {
		s.onAlt = true
		s.grid = s.alt
		return
	}
	if clearOnExit {
		s.alt.Clear(s.eraseStyle())
	}
	s.onAlt = false
	s.grid = s.main
}

// --- reports ---------------------------------------------------------------

func (s *Screen) deviceStatus(ps *Params) {
	switch ps.Get(0) {
	case 5: // operating status
		s.reply([]byte("\x1b[0n"))
	case 6: // CPR
		s.replyf("\x1b[%d;%dR", s.cursorRow()+1, s.cur.X+1)
	}
}

func (s *Screen) deviceStatusPrivate(ps *Params) {
	if ps.Get(0) == 6 { // DECXCPR
		s.replyf("\x1b[?%d;%d;1R", s.cursorRow()+1, s.cur.X+1)
	}
}

// --- SGR -------------------------------------------------------------------

func clamp8(v int32) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return uint8(v)
}

// sgr applies character attributes. Parameters arrive either semicolon- or
// colon-separated; GroupEnd tells the two apart, which matters because
// 38:2::R:G:B is one parameter with sub-parameters while 38;2;R;G;B is five.
func (s *Screen) sgr(ps *Params) {
	if ps.Len() == 0 {
		s.cur.Style = DefaultStyle
		return
	}
	for i := 0; i < ps.Len(); {
		end := ps.GroupEnd(i)
		next := end
		n := ps.Get(i)

		switch {
		case n == 0:
			s.cur.Style = DefaultStyle
		case n == 1:
			s.cur.Style.Attrs |= AttrBold
		case n == 2:
			s.cur.Style.Attrs |= AttrDim
		case n == 3:
			s.cur.Style.Attrs |= AttrItalic
		case n == 4:
			s.setUnderline(ps, i, end)
		case n == 5 || n == 6:
			s.cur.Style.Attrs |= AttrBlink
		case n == 7:
			s.cur.Style.Attrs |= AttrReverse
		case n == 8:
			s.cur.Style.Attrs |= AttrHidden
		case n == 9:
			s.cur.Style.Attrs |= AttrStrike
		case n == 21:
			s.cur.Style.Attrs |= AttrUnderline
			s.cur.Style.Underline = UnderlineDouble
		case n == 22:
			s.cur.Style.Attrs &^= AttrBold | AttrDim
		case n == 23:
			s.cur.Style.Attrs &^= AttrItalic
		case n == 24:
			s.cur.Style.Attrs &^= AttrUnderline
			s.cur.Style.Underline = UnderlineNone
		case n == 25:
			s.cur.Style.Attrs &^= AttrBlink
		case n == 27:
			s.cur.Style.Attrs &^= AttrReverse
		case n == 28:
			s.cur.Style.Attrs &^= AttrHidden
		case n == 29:
			s.cur.Style.Attrs &^= AttrStrike
		case n >= 30 && n <= 37:
			s.cur.Style.FG = IndexedColor(uint8(n - 30))
		case n == 38:
			if c, nx, ok := extendedColor(ps, i, end); ok {
				s.cur.Style.FG = c
				next = nx
			} else {
				next = nx
			}
		case n == 39:
			s.cur.Style.FG = DefaultColor
		case n >= 40 && n <= 47:
			s.cur.Style.BG = IndexedColor(uint8(n - 40))
		case n == 48:
			if c, nx, ok := extendedColor(ps, i, end); ok {
				s.cur.Style.BG = c
				next = nx
			} else {
				next = nx
			}
		case n == 49:
			s.cur.Style.BG = DefaultColor
		case n == 58:
			// Underline colour. Consumed so its arguments cannot be misread as
			// further attributes; tend does not render it yet.
			_, nx, _ := extendedColor(ps, i, end)
			next = nx
		case n >= 90 && n <= 97:
			s.cur.Style.FG = IndexedColor(uint8(n-90) + 8)
		case n >= 100 && n <= 107:
			s.cur.Style.BG = IndexedColor(uint8(n-100) + 8)
		}

		if next <= i {
			next = i + 1
		}
		i = next
	}
}

func (s *Screen) setUnderline(ps *Params, i, end int) {
	style := UnderlineSingle
	if end-i > 1 {
		switch ps.Get(i + 1) {
		case 0:
			s.cur.Style.Attrs &^= AttrUnderline
			s.cur.Style.Underline = UnderlineNone
			return
		case 1:
			style = UnderlineSingle
		case 2:
			style = UnderlineDouble
		case 3:
			style = UnderlineCurly
		case 4:
			style = UnderlineDotted
		case 5:
			style = UnderlineDashed
		}
	}
	s.cur.Style.Attrs |= AttrUnderline
	s.cur.Style.Underline = style
}

// extendedColor reads an SGR 38/48/58 colour argument, in either the
// colon-joined form or the older semicolon-separated one. It returns the
// colour and the parameter index to resume from.
func extendedColor(ps *Params, i, end int) (Color, int, bool) {
	// Colon form: the whole colour lives inside this parameter group.
	if end-i > 1 {
		switch ps.Get(i + 1) {
		case 2:
			// 38:2:<colourspace>:R:G:B, or the older 38:2:R:G:B.
			switch end - i {
			case 6:
				return RGBColor(clamp8(ps.Get(i+3)), clamp8(ps.Get(i+4)), clamp8(ps.Get(i+5))), end, true
			case 5:
				return RGBColor(clamp8(ps.Get(i+2)), clamp8(ps.Get(i+3)), clamp8(ps.Get(i+4))), end, true
			}
		case 5:
			if end-i >= 3 {
				return IndexedColor(clamp8(ps.Get(i + 2))), end, true
			}
		}
		return 0, end, false
	}

	// Semicolon form: the arguments are separate parameters.
	switch ps.Get(i + 1) {
	case 2:
		if i+4 < ps.Len() {
			return RGBColor(clamp8(ps.Get(i+2)), clamp8(ps.Get(i+3)), clamp8(ps.Get(i+4))), i + 5, true
		}
	case 5:
		if i+2 < ps.Len() {
			return IndexedColor(clamp8(ps.Get(i + 2))), i + 3, true
		}
	}
	// A malformed colour consumes the rest of the sequence: its remaining
	// arguments are not attributes and must not be applied as such.
	return 0, ps.Len(), false
}
