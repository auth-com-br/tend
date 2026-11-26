package core

import (
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestKeysEncodeAsAnXtermSendsThem: the desktop is the terminal in front of
// the pane, so a key has to arrive as the bytes an xterm would send. If it
// regresses, a program in the pane reads a stray letter for an arrow, or
// ctrl+c never interrupts anything.
func TestKeysEncodeAsAnXtermSendsThem(t *testing.T) {
	var normal, appCur vt.Modes
	appCur.ApplicationCur = true
	cases := []struct {
		name      string
		key, mods int
		text      string
		m         vt.Modes
		want      string
	}{
		{"a letter is its text", 'A', 0, "a", normal, "a"},
		{"accented text passes through", 0x00c3, 0, "ã", normal, "ã"},
		{"enter is a carriage return", QtKeyReturn, 0, "\r", normal, "\r"},
		{"keypad enter too", QtKeyEnter, 0, "\r", normal, "\r"},
		{"backspace is DEL", QtKeyBackspace, 0, "\b", normal, "\x7f"},
		{"ctrl+backspace is BS", QtKeyBackspace, QtControl, "", normal, "\x08"},
		{"tab", QtKeyTab, 0, "\t", normal, "\t"},
		{"shift+tab is back-tab", QtKeyBacktab, QtShift, "", normal, "\x1b[Z"},
		{"escape", QtKeyEscape, 0, "\x1b", normal, "\x1b"},
		{"ctrl+c by key code, whatever the text", 'C', QtControl, "", normal, "\x03"},
		{"ctrl+c with Qt's control text", 'C', QtControl, "\x03", normal, "\x03"},
		{"alt+x has an escape in front", 'X', QtAlt, "x", normal, "\x1bx"},
		{"up, normal cursor keys", QtKeyUp, 0, "", normal, "\x1b[A"},
		{"up under DECCKM", QtKeyUp, 0, "", appCur, "\x1bOA"},
		{"ctrl+left carries its modifier", QtKeyLeft, QtControl, "", appCur, "\x1b[1;5D"},
		{"shift+up", QtKeyUp, QtShift, "", normal, "\x1b[1;2A"},
		{"delete", QtKeyDelete, 0, "", normal, "\x1b[3~"},
		{"ctrl+pagedown", QtKeyPageDown, QtControl, "", normal, "\x1b[6;5~"},
		{"f1 is SS3", QtKeyF1, 0, "", normal, "\x1bOP"},
		{"f5 is a tilde key", QtKeyF1 + 4, 0, "", normal, "\x1b[15~"},
		{"f12", QtKeyF12, 0, "", normal, "\x1b[24~"},
	}
	for _, c := range cases {
		got, ok := EncodeQtKey(c.key, c.mods, c.text, c.m)
		if !ok || string(got) != c.want {
			t.Errorf("%s: got %q (ok=%v), want %q", c.name, got, ok, c.want)
		}
	}
}

// TestABareModifierSendsNothing: pressing shift on its own must not type
// anything into the pane.
func TestABareModifierSendsNothing(t *testing.T) {
	const qtKeyShift = 0x01000020
	if got, ok := EncodeQtKey(qtKeyShift, QtShift, "", vt.Modes{}); ok {
		t.Fatalf("shift alone sent %q", got)
	}
}

// TestAFrameHasTheScreensCellsInRGB: what the painter is given is what the
// pane's terminal holds, colours resolved. If it regresses, the window shows
// the wrong text, or colours other than the program asked for.
func TestAFrameHasTheScreensCellsInRGB(t *testing.T) {
	s := vt.NewScreen(10, 3, 0)
	// "hi" in red on the default background, then a reversed "x" in
	// direct colour, then the cursor parked on the second line.
	_, _ = s.Write([]byte("\x1b[31mhi\x1b[0m \x1b[7;38;2;1;2;3mx\x1b[0m\r\n"))
	p := DefaultColours
	f := FrameOf(s, &p, nil)

	if f.Cols != 10 || f.Rows != 3 || len(f.Cells) != 30 {
		t.Fatalf("frame is %dx%d with %d cells", f.Cols, f.Rows, len(f.Cells))
	}
	if c := f.Cells[0]; c.Ch != 'h' || c.FG != p.ANSI[1] || c.BG != p.BG {
		t.Errorf("first cell %+v, want h in ANSI red on the background", c)
	}
	if c := f.Cells[3]; c.Ch != 'x' || c.FG != p.BG || c.BG != 0x010203 {
		t.Errorf("reversed cell %+v, want its colours swapped", c)
	}
	if c := f.Cells[10]; c.Ch != ' ' {
		t.Errorf("second line starts with %q, want blank", rune(c.Ch))
	}
	if f.CursorX != 0 || f.CursorY != 1 || !f.CursorVisible {
		t.Errorf("cursor at %d,%d visible=%v, want 0,1 visible", f.CursorX, f.CursorY, f.CursorVisible)
	}
}

// TestBoldMakesTheFirstEightColoursBright: xterm draws bold red as bright
// red, and programs (ls, git, prompts) are written expecting it.
func TestBoldMakesTheFirstEightColoursBright(t *testing.T) {
	s := vt.NewScreen(4, 1, 0)
	_, _ = s.Write([]byte("\x1b[1;34mb"))
	p := DefaultColours
	p.ANSI[12] = 0xabcdef
	if c := FrameOf(s, &p, nil).Cells[0]; c.FG != 0xabcdef {
		t.Errorf("bold blue painted %06x, want bright blue %06x", c.FG, 0xabcdef)
	}
}

// TestThe256ColoursAreXtermsCubeAndRamp: a theme sets only the first 16;
// the rest are fixed, and a program that picks colour 196 expects red.
func TestThe256ColoursAreXtermsCubeAndRamp(t *testing.T) {
	p := DefaultColours
	for _, c := range []struct {
		i    uint8
		want uint32
	}{{16, 0x000000}, {196, 0xff0000}, {231, 0xffffff}, {232, 0x080808}, {255, 0xeeeeee}} {
		if got := p.indexed(c.i); got != c.want {
			t.Errorf("colour %d is %06x, want %06x", c.i, got, c.want)
		}
	}
}

// TestTheWindowShowsWhatTheSessionHasInFocus: the desktop lands where the
// session is, as the TUI does, falling back to the first space and tab when
// the active ones are not set.
func TestTheWindowShowsWhatTheSessionHasInFocus(t *testing.T) {
	snap := proto.SessionSnapshot{
		ActiveWorkspace: 2,
		Workspaces: []proto.WorkspaceInfo{
			{ID: 1, Tabs: []proto.TabInfo{{ID: 10, Panes: []uint64{100}, ActivePane: 100}}},
			{ID: 2, ActiveTab: 21, Tabs: []proto.TabInfo{
				{ID: 20, Panes: []uint64{200}, ActivePane: 200},
				{ID: 21, Panes: []uint64{210, 211}, ActivePane: 211},
			}},
		},
	}
	if got := FocusedPane(snap); got != 211 {
		t.Errorf("focused pane %d, want 211", got)
	}
	snap.ActiveWorkspace = 0
	if got := FocusedPane(snap); got != 100 {
		t.Errorf("with no active space, %d, want the first space's 100", got)
	}
	if got := FocusedPane(proto.SessionSnapshot{}); got != 0 {
		t.Errorf("an empty session gave pane %d", got)
	}
}

// TestASelectionIsShownWhereItWasDragged: a drag highlights the text from
// where it began to where it is, in reading order whichever way it went, or
// the rectangle with alt. If it regresses, the window copies one thing and
// shows another.
func TestASelectionIsShownWhereItWasDragged(t *testing.T) {
	frame := func() Frame {
		f := Frame{Cols: 4, Rows: 3, Cells: make([]Cell, 12)}
		for i := range f.Cells {
			f.Cells[i] = Cell{Ch: 'x', FG: 1, BG: 2, Width: 1}
		}
		return f
	}
	marked := func(f Frame) string {
		out := ""
		for i, c := range f.Cells {
			if i > 0 && i%f.Cols == 0 {
				out += "|"
			}
			if c.FG == 2 {
				out += "#"
			} else {
				out += "."
			}
		}
		return out
	}
	// Backwards from (1,2) to (2,0): the same as forwards.
	f := frame()
	highlight(&f, selection{active: true, anchorX: 1, anchorY: 2, x: 2, y: 0})
	if got, want := marked(f), "..##|####|##.."; got != want {
		t.Errorf("stream selection %s, want %s", got, want)
	}
	f = frame()
	highlight(&f, selection{done: true, block: true, anchorX: 1, anchorY: 0, x: 2, y: 1})
	if got, want := marked(f), ".##.|.##.|...."; got != want {
		t.Errorf("block selection %s, want %s", got, want)
	}
	f = frame()
	highlight(&f, selection{})
	if got := marked(f); got != "....|....|...." {
		t.Errorf("no selection marked %s", got)
	}
}

// TestTheWheelAsKeysFollowsDECCKM: a full-screen program that does not take
// the mouse is scrolled with arrows, in the form it reads them.
func TestTheWheelAsKeysFollowsDECCKM(t *testing.T) {
	if got := string(arrowKey(true, false)); got != "\x1b[A" {
		t.Errorf("up %q", got)
	}
	if got := string(arrowKey(false, true)); got != "\x1bOB" {
		t.Errorf("down under DECCKM %q", got)
	}
}

// TestTheWindowWearsTheThemeTheSettingsName: a theme named in the settings
// colours the window and the terminals, from the palette the TUI uses; with
// none, catppuccin. If it regresses, the window and the TUI of one session
// wear different colours.
func TestTheWindowWearsTheThemeTheSettingsName(t *testing.T) {
	gruvbox, _ := ui.PaletteNamed("gruvbox")
	p := paletteFor(config.Theme{Name: "gruvbox"}, false)
	if p != gruvbox {
		t.Fatalf("gruvbox resolved to %+v", p)
	}
	c := coloursFrom(p)
	if c.BG != rgbOf(gruvbox.ActiveRowBG, 0) || c.ANSI[1] != rgbOf(gruvbox.Red, 0) {
		t.Errorf("terminal colours %+v not gruvbox's", c)
	}
	if th := themeFrom(p, false); th.Accent != hex(rgbOf(gruvbox.Accent, 0)) {
		t.Errorf("window accent %s", th.Accent)
	}
	cat, _ := ui.PaletteNamed("catppuccin")
	if paletteFor(config.Theme{}, false) != cat {
		t.Error("no theme did not give catppuccin")
	}
}

// TestShiftEnterIsANewLineForAProgramThatAsked: Claude Code asks for the
// kitty keyboard protocol and reads Shift+Enter as CSI 13;2u, a new line in
// its prompt; a program that asked for xterm's modifyOtherKeys gets CSI
// 27;2;13~. One that asked for neither gets Enter, as in a terminal without
// them. If it regresses, Shift+Enter sends the prompt in Claude Code (#66).
func TestShiftEnterIsANewLineForAProgramThatAsked(t *testing.T) {
	kitty := vt.Modes{KittyKeyboard: 1}
	xterm := vt.Modes{ModifyOtherKeys: 2}
	for _, c := range []struct {
		name      string
		key, mods int
		text      string
		m         vt.Modes
		want      string
	}{
		{"shift+enter, kitty", QtKeyReturn, QtShift, "\r", kitty, "\x1b[13;2u"},
		{"enter alone stays enter, kitty", QtKeyReturn, 0, "\r", kitty, "\r"},
		{"escape, kitty", QtKeyEscape, 0, "\x1b", kitty, "\x1b[27u"},
		{"ctrl+c, kitty", 'C', QtControl, "\x03", kitty, "\x1b[99;5u"},
		{"a letter types, kitty", 'A', 0, "a", kitty, "a"},
		{"shift+a types, kitty", 'A', QtShift, "A", kitty, "A"},
		{"an arrow stays legacy, kitty", QtKeyUp, 0, "", kitty, "\x1b[A"},
		{"ctrl+up, kitty", QtKeyUp, QtControl, "", kitty, "\x1b[1;5A"},
		{"every key as escapes", 'A', 0, "a", vt.Modes{KittyKeyboard: 8}, "\x1b[97u"},
		{"shift+enter, modifyOtherKeys", QtKeyReturn, QtShift, "\r", xterm, "\x1b[27;2;13~"},
		{"ctrl+a, modifyOtherKeys", 'A', QtControl, "", xterm, "\x1b[27;5;97~"},
		{"a letter types, modifyOtherKeys", 'A', 0, "a", xterm, "a"},
		{"shift+enter, no protocol", QtKeyReturn, QtShift, "\r", vt.Modes{}, "\r"},
	} {
		got, ok := EncodeQtKey(c.key, c.mods, c.text, c.m)
		if !ok || string(got) != c.want {
			t.Errorf("%s: %q (ok=%v), want %q", c.name, got, ok, c.want)
		}
	}
}

// TestTheWindowWearsItsOwnThemeUnlessToldOtherwise: with no [desktop]
// theme the window wears its own, light or dark as the desktop is, whatever
// the TUI's theme is; "tui" gives it the TUI's, and a theme's name
// that theme. If it regresses, the window is matrix-green because the TUI
// is, which is what the owner did not want.
func TestTheWindowWearsItsOwnThemeUnlessToldOtherwise(t *testing.T) {
	s := &Session{}
	s.ui.Theme = config.Theme{Name: "matrix"}
	s.applyThemeLocked()
	dark, _ := tendTheme(false)
	if s.theme != dark {
		t.Errorf("with matrix in [ui] the window wears %+v, not its own", s.theme)
	}
	s.light = true
	s.applyThemeLocked()
	if !s.theme.Light || s.theme.Base != "#ffffff" {
		t.Errorf("on a light desktop: %+v", s.theme)
	}

	s.light = false
	s.cfg.Desktop.Theme = "tui"
	s.applyThemeLocked()
	if want := themeFrom(paletteFor(s.ui.Theme, false), false); s.theme != want {
		t.Errorf("tui: %+v, not the TUI's %+v", s.theme, want)
	}
	s.cfg.Desktop.Theme = "gruvbox"
	s.applyThemeLocked()
	gruvbox, _ := ui.PaletteNamed("gruvbox")
	if s.theme != themeFrom(gruvbox, false) {
		t.Errorf("gruvbox: %+v", s.theme)
	}
}
