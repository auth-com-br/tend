package core

import (
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
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
