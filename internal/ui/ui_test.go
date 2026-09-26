package ui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/vt"

	"github.com/mattn/go-runewidth"
)

// gridText renders a grid as lines, so a whole screenful can be asserted as
// text instead of cell by cell.
func gridText(g *vt.Grid) []string {
	out := make([]string, g.Rows())
	for y := 0; y < g.Rows(); y++ {
		out[y] = g.Line(y).Text()
	}
	return out
}

func screenWith(t *testing.T, cols, rows int, input string) *vt.Screen {
	t.Helper()
	s := vt.NewScreen(cols, rows, 0)
	if _, err := s.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	return s
}

// --- drawing ---------------------------------------------------------------

func TestDrawSinglePane(t *testing.T) {
	dst := vt.NewGrid(20, 6, 0)
	Draw(dst, Frame{
		Session: "demo",
		Panes: []Pane{{
			ID:      1,
			Rect:    Rect{X: 0, Y: 0, Cols: 20, Rows: 5},
			Title:   "sh",
			Screen:  screenWith(t, 18, 3, "hello"),
			Running: true,
			Focused: true,
		}},
	}, DefaultTheme())

	lines := gridText(dst)
	if !strings.HasPrefix(lines[0], "┌") || !strings.HasSuffix(strings.TrimRight(lines[0], " "), "┐") {
		t.Errorf("top border = %q", lines[0])
	}
	if !strings.Contains(lines[0], "sh") {
		t.Errorf("the title should be in the top border: %q", lines[0])
	}
	if !strings.Contains(lines[1], "hello") {
		t.Errorf("the pane contents should be inside: %q", lines[1])
	}
	if !strings.HasPrefix(lines[4], "└") {
		t.Errorf("bottom border = %q", lines[4])
	}
}

// TestDrawKeepsPanesInTheirRects is what stops one pane painting over another:
// everything a pane draws, border included, stays inside the rectangle it was
// given.
func TestDrawKeepsPanesInTheirRects(t *testing.T) {
	dst := vt.NewGrid(40, 11, 0)
	left := screenWith(t, 18, 8, strings.Repeat("L", 200))
	right := screenWith(t, 18, 8, strings.Repeat("R", 200))

	Draw(dst, Frame{
		Panes: []Pane{
			{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 20, Rows: 10}, Screen: left, Running: true},
			{ID: 2, Rect: Rect{X: 20, Y: 0, Cols: 20, Rows: 10}, Screen: right, Running: true},
		},
	}, DefaultTheme())

	for y := 1; y < 9; y++ {
		row := dst.Line(y)
		for x := 0; x < 20; x++ {
			if r := row.Cell(x).R; r == 'R' {
				t.Fatalf("the right pane wrote into the left one at (%d,%d)", x, y)
			}
		}
		for x := 20; x < 40; x++ {
			if r := row.Cell(x).R; r == 'L' {
				t.Fatalf("the left pane wrote into the right one at (%d,%d)", x, y)
			}
		}
	}
}

func TestDrawPreservesPaneStyles(t *testing.T) {
	dst := vt.NewGrid(20, 5, 0)
	Draw(dst, Frame{
		Panes: []Pane{{
			ID:      1,
			Rect:    Rect{X: 0, Y: 0, Cols: 20, Rows: 4},
			Screen:  screenWith(t, 18, 2, "\x1b[31mred"),
			Running: true,
		}},
	}, DefaultTheme())

	// The pane's content starts one cell in from its border.
	if got := dst.Line(1).Cell(1).Style.FG; got != vt.IndexedColor(1) {
		t.Errorf("colour = %#v, want indexed 1; styling is lost in the blit", got)
	}
}

// TestPaneLabelFallsBackToTheCommand: a pane that has not named itself should
// say what it is running. "pane" tells nobody anything.
func TestPaneLabelFallsBackToTheCommand(t *testing.T) {
	dst := vt.NewGrid(30, 4, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 30, Rows: 3}, Command: "zsh", Running: true},
	}}, DefaultTheme())
	if got := dst.Line(0).Text(); !strings.Contains(got, "zsh") {
		t.Errorf("title = %q, want the command name", got)
	}

	// A title the pane set for itself wins over both.
	dst = vt.NewGrid(30, 4, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 30, Rows: 3}, Title: "my-project", Agent: "claude", Command: "zsh", Running: true},
	}}, DefaultTheme())
	if got := dst.Line(0).Text(); !strings.Contains(got, "my-project") {
		t.Errorf("title = %q, want the pane's own title", got)
	}
}

func TestDrawFocusedPaneIsDistinct(t *testing.T) {
	theme := DefaultTheme()

	focused := vt.NewGrid(20, 5, 0)
	Draw(focused, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 20, Rows: 4}, Running: true, Focused: true},
	}}, theme)

	plain := vt.NewGrid(20, 5, 0)
	Draw(plain, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 20, Rows: 4}, Running: true},
	}}, theme)

	if focused.Line(0).Cell(0).Style == plain.Line(0).Cell(0).Style {
		t.Error("a focused pane should look different from an unfocused one")
	}
}

// TestDrawSkipsPanesTooSmallForABorder: half a frame reads as a glitch, an
// empty space reads as a small pane.
func TestDrawSkipsPanesTooSmallForABorder(t *testing.T) {
	dst := vt.NewGrid(20, 5, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 1, Rows: 4}, Running: true},
	}}, DefaultTheme())

	// Nothing of the pane, that is. The handle that brings the sidebar back
	// is always in the corner, or a sidebar put away could not be recovered.
	if got := dst.Line(0).Text(); got != "»" {
		t.Errorf("row 0 = %q, want only the sidebar handle", got)
	}
}

func TestDrawHandlesANilScreen(t *testing.T) {
	// A pane between being opened and its first output has no screen yet.
	dst := vt.NewGrid(20, 5, 0)
	Draw(dst, Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{Cols: 20, Rows: 4}, Running: true},
	}}, DefaultTheme())

	if !strings.HasPrefix(dst.Line(0).Text(), "┌") {
		t.Error("a pane with no screen should still be framed")
	}
}

// TestDrawClipsWideCharactersAtTheEdge: a double-width character whose second
// half falls outside the pane has nowhere to put it, so it must be replaced
// rather than drawn into the border.
func TestDrawClipsWideCharactersAtTheEdge(t *testing.T) {
	dst := vt.NewGrid(8, 4, 0)
	Draw(dst, Frame{Panes: []Pane{{
		ID:      1,
		Rect:    Rect{X: 0, Y: 0, Cols: 8, Rows: 3},
		Screen:  screenWith(t, 6, 1, "世界世"),
		Running: true,
	}}}, DefaultTheme())

	// The border column must survive.
	if got := dst.Line(1).Cell(7).R; got != '│' {
		t.Errorf("the right border was overwritten: %q", got)
	}
}

func TestDrawStatusBar(t *testing.T) {
	dst := vt.NewGrid(60, 6, 0)
	Draw(dst, Frame{
		Session:   "demo",
		Workspace: "main",
		Tab:       "work",
		Panes: []Pane{
			{ID: 1, Rect: Rect{Cols: 30, Rows: 5}, Agent: "claude", State: "working", Running: true, Focused: true},
			{ID: 2, Rect: Rect{X: 30, Cols: 30, Rows: 5}, Agent: "codex", State: "idle", Running: true},
		},
	}, DefaultTheme())

	status := dst.Line(dst.Rows() - 1).Text()
	for _, want := range []string{"demo", "main", "work", "1:claude", "2:codex"} {
		if !strings.Contains(status, want) {
			t.Errorf("status %q is missing %q", status, want)
		}
	}
}

func TestDrawStatusShowsPrefix(t *testing.T) {
	dst := vt.NewGrid(40, 4, 0)
	Draw(dst, Frame{Session: "s", Prefix: true}, DefaultTheme())
	if got := dst.Line(3).Text(); !strings.Contains(got, "PREFIX") {
		t.Errorf("status = %q, want it to show the armed prefix", got)
	}
}

func TestDrawStatusShowsMessage(t *testing.T) {
	dst := vt.NewGrid(60, 4, 0)
	Draw(dst, Frame{Session: "s", Message: "pane 2 closed"}, DefaultTheme())
	if got := dst.Line(3).Text(); !strings.Contains(got, "pane 2 closed") {
		t.Errorf("status = %q", got)
	}
}

// TestDrawClearsStaleContent: a frame describes the whole screen, so anything
// from the last one is stale by definition.
func TestDrawClearsStaleContent(t *testing.T) {
	dst := vt.NewGrid(30, 6, 0)
	theme := DefaultTheme()

	Draw(dst, Frame{Panes: []Pane{{
		ID: 1, Rect: Rect{Cols: 30, Rows: 5},
		Screen: screenWith(t, 28, 3, "first frame"), Running: true,
	}}}, theme)

	Draw(dst, Frame{Panes: []Pane{{
		ID: 1, Rect: Rect{Cols: 30, Rows: 5},
		Screen: screenWith(t, 28, 3, "second"), Running: true,
	}}}, theme)

	for _, line := range gridText(dst) {
		if strings.Contains(line, "first frame") {
			t.Fatalf("stale content survived a redraw: %q", line)
		}
	}
}

func TestInnerSize(t *testing.T) {
	cols, rows := InnerSize(Rect{Cols: 20, Rows: 10})
	if cols != 18 || rows != 8 {
		t.Errorf("InnerSize = %dx%d, want 18x8", cols, rows)
	}
	// A rect too small for a border must still yield a usable size rather
	// than zero, which most programs refuse to draw into.
	cols, rows = InnerSize(Rect{Cols: 1, Rows: 1})
	if cols < 1 || rows < 1 {
		t.Errorf("InnerSize = %dx%d, want at least 1x1", cols, rows)
	}
}

func TestCursorFollowsTheFocusedPane(t *testing.T) {
	screen := screenWith(t, 18, 8, "abc")
	f := Frame{Panes: []Pane{
		{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 20, Rows: 10}, Screen: screen, Running: true},
		{ID: 2, Rect: Rect{X: 20, Y: 0, Cols: 20, Rows: 10}, Screen: screen, Running: true, Focused: true},
	}}

	x, y, visible := CursorPosition(f, 80, 24)
	if !visible {
		t.Fatal("the cursor should be visible")
	}
	// The focused pane starts at x=20, its border takes one column, and the
	// pane's own cursor sits after "abc".
	if x != 24 || y != 1 {
		t.Errorf("cursor = (%d,%d), want (24,1)", x, y)
	}
}

func TestCursorHiddenForAnExitedPane(t *testing.T) {
	f := Frame{Panes: []Pane{{
		ID: 1, Rect: Rect{Cols: 20, Rows: 10},
		Screen: screenWith(t, 18, 8, "x"), Focused: true, Running: false,
	}}}
	if _, _, visible := CursorPosition(f, 80, 24); visible {
		t.Error("an exited pane should not show a cursor")
	}
}

func TestCursorHiddenWithNoFocus(t *testing.T) {
	if _, _, visible := CursorPosition(Frame{}, 80, 24); visible {
		t.Error("an empty frame has no cursor")
	}
}

// --- keys ------------------------------------------------------------------

func TestInputForwardsOrdinaryKeys(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte("hello world"))
	if string(forward) != "hello world" {
		t.Errorf("forwarded %q", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

// TestInputForwardsEscapeSequencesUntouched: an agent's own key handling needs
// the bytes exactly as typed, including sequences the client cannot name.
func TestInputForwardsEscapeSequencesUntouched(t *testing.T) {
	var in Input
	seq := "\x1b[1;5A\x1b[200~pasted\x1b[201~\x1bOP"
	forward, commands, _ := in.FeedAll([]byte(seq))
	if string(forward) != seq {
		t.Errorf("forwarded %q, want %q", forward, seq)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

func TestInputPrefixCommands(t *testing.T) {
	cases := map[string]Command{
		"|": CommandSplitColumns,
		"%": CommandSplitColumns,
		"-": CommandSplitRows,
		`"`: CommandSplitRows,
		"h": CommandFocusLeft,
		"l": CommandFocusRight,
		"k": CommandFocusUp,
		"j": CommandFocusDown,
		"o": CommandFocusNext,
		"x": CommandClosePane,
		"c": CommandNewTab,
		"n": CommandNextTab,
		"p": CommandPrevTab,
		"d": CommandDetach,
		// herdr's bindings: r is resize mode, and the shifted movement keys
		// swap. Redraw moved to R to make room.
		"r": CommandResizeMode,
		"R": CommandRefresh,
		"H": CommandSwapLeft,
		"J": CommandSwapDown,
		"K": CommandSwapUp,
		"L": CommandSwapRight,
		"?": CommandHelp,
	}
	for key, want := range cases {
		var in Input
		forward, commands, _ := in.FeedAll(append([]byte{Prefix}, key...))
		if len(commands) != 1 || commands[0].Command != want {
			t.Errorf("prefix %q gave %v, want %v", key, commands, want)
		}
		if len(forward) != 0 {
			t.Errorf("prefix %q forwarded %q, want nothing", key, forward)
		}
	}
}

func TestInputPrefixArrowKeys(t *testing.T) {
	cases := map[string]Command{
		"\x1b[A": CommandFocusUp,
		"\x1b[B": CommandFocusDown,
		"\x1b[C": CommandFocusRight,
		"\x1b[D": CommandFocusLeft,
	}
	for seq, want := range cases {
		var in Input
		_, commands, _ := in.FeedAll(append([]byte{Prefix}, seq...))
		if len(commands) != 1 || commands[0].Command != want {
			t.Errorf("prefix %q gave %v, want %v", seq, commands, want)
		}
	}
}

// TestInputArrowKeysArrivingApart: the three bytes of an arrow key can be read
// in separate chunks, and the state machine has to survive that.
func TestInputArrowKeysArrivingApart(t *testing.T) {
	var in Input
	var commands []Action
	for _, chunk := range [][]byte{{Prefix}, {0x1b}, {'['}, {'C'}} {
		_, cmds, _ := in.FeedAll(chunk)
		commands = append(commands, cmds...)
	}
	if len(commands) != 1 || commands[0].Command != CommandFocusRight {
		t.Errorf("commands = %v, want focus-right", commands)
	}
}

// TestInputDoublePrefixSendsItLiterally is how an inner multiplexer, or an
// editor bound to Ctrl+B, still receives the key.
func TestInputDoublePrefixSendsItLiterally(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, Prefix})
	if len(forward) != 1 || forward[0] != Prefix {
		t.Errorf("forwarded %q, want the prefix byte", forward)
	}
	if len(commands) != 1 || commands[0].Command != CommandLiteralPrefix {
		t.Errorf("commands = %v", commands)
	}
}

// TestInputUnboundKeyIsForwarded: a mistyped command must not silently eat the
// keystroke after it.
func TestInputUnboundKeyIsForwarded(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, 'Z'})
	if string(forward) != "Z" {
		t.Errorf("forwarded %q, want Z", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

// TestInputPrefixThenNonArrowEscape: ESC after the prefix that turns out not
// to be an arrow key releases what it collected rather than guessing.
func TestInputPrefixThenNonArrowEscape(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, 0x1b, 'O'})
	if string(forward) != "\x1bO" {
		t.Errorf("forwarded %q, want the collected bytes", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v, want none", commands)
	}
}

func TestInputArmedReporting(t *testing.T) {
	var in Input
	if in.Armed() {
		t.Error("a fresh Input is not armed")
	}
	in.Feed(Prefix)
	if !in.Armed() {
		t.Error("the prefix should arm it")
	}
	in.Feed('x')
	if in.Armed() {
		t.Error("a command should disarm it")
	}
}

// TestInputKeepsOrderWithinAChunk: a chunk can hold both a command and text,
// and they must stay in order relative to each other.
func TestInputKeepsOrderWithinAChunk(t *testing.T) {
	var in Input
	forward, commands, _ := in.FeedAll([]byte{'a', Prefix, 'x', 'b'})
	if string(forward) != "ab" {
		t.Errorf("forwarded %q, want ab", forward)
	}
	if len(commands) != 1 || commands[0].Command != CommandClosePane {
		t.Errorf("commands = %v", commands)
	}
}

// TestHelpMatchesTheBindings keeps the help text from drifting away from what
// the keys and the mouse actually do.
func TestHelpMatchesTheBindings(t *testing.T) {
	lines := HelpLines()
	// A heading for each of the two lists, and a line for every entry.
	if want := len(Keys) + len(Gestures) + 2; len(lines) != want {
		t.Fatalf("help has %d lines, want %d", len(lines), want)
	}

	mentioned := func(what string) bool {
		for _, line := range lines {
			if strings.Contains(line, what) {
				return true
			}
		}
		return false
	}
	for _, k := range Keys {
		if !mentioned(k.Help) {
			t.Errorf("help does not mention the %q key", k.Help)
		}
	}
	for _, g := range Gestures {
		if !mentioned(g.Help) {
			t.Errorf("help does not mention the %q gesture", g.Help)
		}
		if !mentioned(g.Gesture) {
			t.Errorf("help does not say how to %q", g.Help)
		}
	}
}

func TestCommandNames(t *testing.T) {
	if CommandNone.String() != "none" {
		t.Error("CommandNone should be named")
	}
	for _, k := range Keys {
		if k.Command.String() == "none" {
			t.Errorf("command for key %q has no name", k.Key)
		}
	}
}

func BenchmarkDraw(b *testing.B) {
	dst := vt.NewGrid(200, 60, 0)
	theme := DefaultTheme()

	screen := vt.NewScreen(98, 28, 0)
	screen.Write([]byte(strings.Repeat("\x1b[32mstatus\x1b[0m output line here\r\n", 28)))

	frame := Frame{Session: "bench"}
	for i := 0; i < 4; i++ {
		frame.Panes = append(frame.Panes, Pane{
			ID:      uint64(i + 1),
			Rect:    Rect{X: (i % 2) * 100, Y: (i / 2) * 30, Cols: 100, Rows: 29},
			Screen:  screen,
			Running: true,
			Focused: i == 0,
		})
	}

	b.ReportAllocs()
	for b.Loop() {
		Draw(dst, frame, theme)
	}
}

// --- mouse -----------------------------------------------------------------

func TestParseMouseReports(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want MouseEvent
	}{
		{"press", "\x1b[<0;10;5M", MouseEvent{Kind: MousePress, X: 9, Y: 4}},
		{"release", "\x1b[<0;10;5m", MouseEvent{Kind: MouseRelease, X: 9, Y: 4}},
		{"right button", "\x1b[<2;3;3M", MouseEvent{Kind: MousePress, X: 2, Y: 2, Button: 2}},
		{"drag", "\x1b[<32;7;9M", MouseEvent{Kind: MouseDrag, X: 6, Y: 8}},
		{"wheel up", "\x1b[<64;1;1M", MouseEvent{Kind: MouseWheelUp}},
		{"wheel down", "\x1b[<65;1;1M", MouseEvent{Kind: MouseWheelDown, Button: 1}},
		// Coordinates past 223 are exactly why the SGR encoding is asked for.
		{"far right", "\x1b[<0;400;200M", MouseEvent{Kind: MousePress, X: 399, Y: 199}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n, incomplete := parseMouse([]byte(c.in))
			if incomplete || n != len(c.in) {
				t.Fatalf("consumed %d of %d (incomplete=%v)", n, len(c.in), incomplete)
			}
			if got.Kind != c.want.Kind || got.X != c.want.X ||
				got.Y != c.want.Y || got.Button != c.want.Button {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
			// Raw is what gets forwarded to a pane, so it has to be the whole
			// sequence and nothing else.
			if string(got.Raw) != c.in {
				t.Errorf("Raw = %q, want %q", got.Raw, c.in)
			}
		})
	}
}

// TestParseMouseIncomplete: a report split across reads must be waited for,
// not forwarded in halves.
func TestParseMouseIncomplete(t *testing.T) {
	for _, partial := range []string{"\x1b", "\x1b[", "\x1b[<", "\x1b[<0", "\x1b[<0;10;"} {
		_, n, incomplete := parseMouse([]byte(partial))
		if n != 0 || !incomplete {
			t.Errorf("%q: n=%d incomplete=%v, want it to wait", partial, n, incomplete)
		}
	}
}

func TestParseMouseRejectsOtherSequences(t *testing.T) {
	for _, other := range []string{"\x1b[A", "\x1b[1;5A", "hello", "\x1b[<abcM"} {
		_, n, incomplete := parseMouse([]byte(other))
		if n != 0 || incomplete {
			t.Errorf("%q: n=%d incomplete=%v, want it rejected", other, n, incomplete)
		}
	}
}

func TestInputSeparatesMouseFromKeys(t *testing.T) {
	var in Input
	forward, commands, mice := in.FeedAll([]byte("ab\x1b[<0;5;5Mcd"))
	if string(forward) != "abcd" {
		t.Errorf("forwarded %q, want abcd", forward)
	}
	if len(commands) != 0 {
		t.Errorf("commands = %v", commands)
	}
	if len(mice) != 1 || mice[0].Kind != MousePress {
		t.Errorf("mice = %+v, want one press", mice)
	}
}

// TestInputBuffersASplitMouseReport: a terminal can deliver one across two
// reads, and half of it reaching a pane would appear there as typed text.
func TestInputBuffersASplitMouseReport(t *testing.T) {
	var in Input
	forward, _, mice := in.FeedAll([]byte("\x1b[<0;12"))
	if len(forward) != 0 || len(mice) != 0 {
		t.Fatalf("a partial report produced forward=%q mice=%v", forward, mice)
	}
	forward, _, mice = in.FeedAll([]byte(";7M"))
	if len(forward) != 0 {
		t.Errorf("forwarded %q, want nothing", forward)
	}
	if len(mice) != 1 || mice[0].X != 11 || mice[0].Y != 6 {
		t.Errorf("mice = %+v, want one press at (11,6)", mice)
	}
}

// TestInputGivesUpOnAnEndlessPartial: something that starts like a mouse
// report but never ends must not swallow input forever.
func TestInputGivesUpOnAnEndlessPartial(t *testing.T) {
	var in Input
	long := "\x1b[<" + strings.Repeat("1", maxPartialMouse+10)
	forward, _, _ := in.FeedAll([]byte(long))
	if len(forward) == 0 {
		t.Error("an over-long partial should be released as ordinary input")
	}
}

func TestMouseEnableAndDisableArePaired(t *testing.T) {
	// Whatever is turned on must be turned off, or every later click in that
	// terminal emits gibberish.
	for _, mode := range []string{"1002", "1006"} {
		if !strings.Contains(EnableMouse, mode+"h") {
			t.Errorf("EnableMouse does not set %s", mode)
		}
		if !strings.Contains(DisableMouse, mode+"l") {
			t.Errorf("DisableMouse does not clear %s", mode)
		}
	}
}

// TestInputSelectTabCarriesTheNumber: the digit keys all map to one command,
// so the number has to travel with it rather than being encoded in a dozen
// commands that differ only by a value.
func TestInputSelectTabCarriesTheNumber(t *testing.T) {
	for n := 1; n <= 9; n++ {
		var in Input
		_, commands, _ := in.FeedAll([]byte{Prefix, byte('0' + n)})
		if len(commands) != 1 {
			t.Fatalf("digit %d gave %v", n, commands)
		}
		if commands[0].Command != CommandSelectTab || commands[0].Arg != n {
			t.Errorf("digit %d = %+v, want select-tab with arg %d", n, commands[0], n)
		}
	}

	// Zero is not a tab: tabs are counted from one, and a key that quietly
	// selects nothing is worse than one that does nothing.
	var in Input
	forward, commands, _ := in.FeedAll([]byte{Prefix, '0'})
	if len(commands) != 0 {
		t.Errorf("zero gave %v, want no command", commands)
	}
	if string(forward) != "0" {
		t.Errorf("zero forwarded %q, want it passed to the pane", forward)
	}
}

func TestInputSpaceAndAgentCommands(t *testing.T) {
	cases := map[byte]Command{
		// herdr's keys: s opens settings, a new space is shift+n.
		's': CommandSettings,
		'N': CommandNewSpace,
		'G': CommandNewWorktree,
		')': CommandNextSpace,
		'(': CommandPrevSpace,
		'a': CommandToggleAgents,
		// g is herdr's navigator popup; w walks the sidebar, tend's.
		'g': CommandNavigator,
		'w': CommandNavigate,
	}
	for key, want := range cases {
		var in Input
		_, commands, _ := in.FeedAll([]byte{Prefix, key})
		if len(commands) != 1 || commands[0].Command != want {
			t.Errorf("key %q gave %v, want %v", string(key), commands, want)
		}
	}
}

// TestInputDeliversALoneEscape is why a lone escape is never held as a
// possible mouse report: inside an editor, a key that does not arrive until
// the next keystroke is indistinguishable from one that was eaten.
func TestInputDeliversALoneEscape(t *testing.T) {
	var in Input
	forward, _, mice := in.FeedAll([]byte("abc\x1b"))
	if string(forward) != "abc\x1b" {
		t.Errorf("forwarded %q, want the escape delivered with the rest", forward)
	}
	if len(mice) != 0 {
		t.Errorf("mice = %v", mice)
	}

	// On its own, too.
	var lone Input
	forward, _, _ = lone.FeedAll([]byte{0x1b})
	if len(forward) != 1 || forward[0] != 0x1b {
		t.Errorf("forwarded %q, want a single escape", forward)
	}
}

// TestInputStillWaitsForALikelyMouseReport keeps the fix above from throwing
// away the buffering it was narrowing.
func TestInputStillWaitsForALikelyMouseReport(t *testing.T) {
	var in Input
	forward, _, mice := in.FeedAll([]byte("\x1b["))
	if len(forward) != 0 || len(mice) != 0 {
		t.Fatalf("forward=%q mice=%v, want it held", forward, mice)
	}
	forward, _, mice = in.FeedAll([]byte("<0;4;2M"))
	if len(forward) != 0 {
		t.Errorf("forwarded %q, want nothing", forward)
	}
	if len(mice) != 1 || mice[0].X != 3 || mice[0].Y != 1 {
		t.Errorf("mice = %+v", mice)
	}
}

// TestInputReleasesAHeldSequenceThatIsNotAMouseReport: an arrow key begins the
// same way and must still reach the pane.
func TestInputReleasesAHeldSequenceThatIsNotAMouseReport(t *testing.T) {
	var in Input
	if forward, _, _ := in.FeedAll([]byte("\x1b[")); len(forward) != 0 {
		t.Fatalf("forwarded %q too early", forward)
	}
	forward, _, mice := in.FeedAll([]byte("A"))
	if string(forward) != "\x1b[A" {
		t.Errorf("forwarded %q, want the whole arrow key", forward)
	}
	if len(mice) != 0 {
		t.Errorf("mice = %v", mice)
	}
}

// sidebarFrame is a frame showing the sidebar and nothing else, which is what
// the sidebar tests are about.
func sidebarFrame(spaces, agents []SidebarRow) Frame {
	return Frame{
		Sidebar: true,
		Spaces:  SidebarSection{Rows: spaces},
		Agents:  SidebarSection{Rows: agents},
	}
}

func spaceRows(n int) []SidebarRow {
	rows := []SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}
	for i := 0; i < n; i++ {
		rows = append(rows, SidebarRow{
			Kind: SidebarSpace, Label: "space " + itoa(uint64(i+1)),
			Detail: "master", Workspace: uint64(i + 1),
		})
	}
	return append(rows, SidebarRow{Kind: SidebarAction, Label: "new", Action: ActionNewSpace})
}

// TestSidebarDrawsBothSections: the sidebar answers two questions — where
// else could I be, and which agent needs me — and they are separate lists.
func TestSidebarDrawsBothSections(t *testing.T) {
	g := vt.NewGrid(60, 16, 0)
	Draw(g, sidebarFrame(
		[]SidebarRow{
			{Kind: SidebarHeading, Label: "spaces"},
			{Kind: SidebarSpace, Label: "herdr", Detail: "master", Workspace: 1, Active: true},
			{Kind: SidebarAction, Label: "new", Action: ActionNewSpace},
		},
		[]SidebarRow{
			{Kind: SidebarHeading, Label: "agents", Trailing: "flat", TrailingAction: ActionToggleGrouped},
			{Kind: SidebarAgent, Label: "herdr · tab 1", Detail: "claude", Pane: 7, Running: true},
		},
	), DefaultTheme())

	lines := gridText(g)
	text := strings.Join(lines, "\n")
	for _, want := range []string{"spaces", "herdr", "master", "new", "agents", "flat", "claude"} {
		if !strings.Contains(text, want) {
			t.Errorf("the sidebar should show %q:\n%s", want, text)
		}
	}
	// The branch belongs under its space, not beside it.
	if !strings.Contains(lines[1], "herdr") || !strings.Contains(lines[2], "master") {
		t.Errorf("the detail should be drawn beneath the name:\n%s", text)
	}
	// And a rule divides the two, so they do not read as one list.
	if !strings.Contains(text, "─") {
		t.Errorf("the lists should be divided:\n%s", text)
	}
}

// TestSidebarTwoLineEntryIsOneTarget: clicking a branch selects the space it
// belongs to, because that is what it looks like it should do.
func TestSidebarTwoLineEntryIsOneTarget(t *testing.T) {
	f := sidebarFrame([]SidebarRow{
		{Kind: SidebarHeading, Label: "spaces"},
		{Kind: SidebarSpace, Label: "one", Detail: "master", Workspace: 1},
		{Kind: SidebarSpace, Label: "two", Detail: "topic", Workspace: 2},
	}, nil)
	const rows = 20

	cases := map[int]uint64{0: 0, 1: 1, 2: 1, 3: 2, 4: 2}
	for y, want := range cases {
		row, ok := SidebarRowAt(f, 2, y, rows)
		if !ok {
			t.Errorf("row %d: nothing there", y)
			continue
		}
		if row.Workspace != want {
			t.Errorf("row %d selects workspace %d, want %d", y, row.Workspace, want)
		}
	}
	if _, ok := SidebarRowAt(f, SidebarWidth, 1, rows); ok {
		t.Error("past the sidebar belongs to the pane")
	}
}

// TestSidebarHeadingCarriesItsToggle: the toggle sits at the edge of what it
// toggles, and clicking it has to reach it.
func TestSidebarHeadingCarriesItsToggle(t *testing.T) {
	f := sidebarFrame(
		[]SidebarRow{{Kind: SidebarHeading, Label: "spaces"}},
		[]SidebarRow{{
			Kind: SidebarHeading, Label: "agents", Trailing: "grouped",
			Action: ActionToggleGrouped, TrailingAction: ActionToggleGrouped,
		}},
	)
	const rows = 20
	_, agents := SidebarRegions(f, rows)

	row, ok := SidebarRowAt(f, SidebarWidth-4, agents.Y, rows)
	if !ok || row.Action != ActionToggleGrouped {
		t.Errorf("the toggle should be clickable, got %+v ok=%v", row, ok)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	line := gridText(g)[agents.Y]
	if at := strings.Index(line, "grouped"); at < strings.Index(line, "agents")+len("agents") {
		t.Errorf("the toggle should sit at the right edge:\n%q", line)
	}
}

// TestSidebarHiddenTakesNoColumns: turning it off has to give the columns back
// rather than leaving a blank margin.
func TestSidebarHiddenTakesNoColumns(t *testing.T) {
	g := vt.NewGrid(40, 6, 0)
	Draw(g, Frame{Spaces: SidebarSection{Rows: []SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}}}, DefaultTheme())
	if text := strings.Join(gridText(g), "\n"); strings.Contains(text, "spaces") {
		t.Errorf("a hidden sidebar should draw nothing:\n%s", text)
	}
	if _, ok := SidebarRowAt(Frame{}, 0, 0, 40); ok {
		t.Error("a hidden sidebar should have no targets")
	}
}

// TestSidebarTrailingIsItsOwnTarget: the "menu" button sits on the "new" row,
// and clicking it must not create a space.
func TestSidebarTrailingIsItsOwnTarget(t *testing.T) {
	f := sidebarFrame([]SidebarRow{{
		Kind: SidebarAction, Label: "new", Action: ActionNewSpace,
		Trailing: "menu", TrailingAction: ActionOpenMenu,
	}}, nil)
	const rows = 20

	if row, _ := SidebarRowAt(f, 2, 0, rows); row.Action != ActionNewSpace {
		t.Errorf("the left of the row should create a space, got %q", row.Action)
	}
	at := TrailingStart(f.Spaces.Rows[0], SidebarWidth)
	if at < 0 {
		t.Fatal("the trailing button should have a column")
	}
	if row, _ := SidebarRowAt(f, at, 0, rows); row.Action != ActionOpenMenu {
		t.Errorf("the button should open the menu, got %q", row.Action)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if line := gridText(g)[0]; !strings.Contains(line, "new") || !strings.Contains(line, "menu") {
		t.Errorf("both should be drawn on one row:\n%q", line)
	}
}

// TestSidebarDrawsAFoldedGroup: the triangle points at what it will do, and a
// folded group shows nothing of what is inside it except that it is waiting.
func TestSidebarDrawsAFoldedGroup(t *testing.T) {
	open := sidebarFrame([]SidebarRow{
		{Kind: SidebarSpaceGroup, Label: "clients", Group: "clients", Action: ActionToggleGroup},
		{Kind: SidebarSpace, Label: "backend", Detail: "develop", Depth: 1, Workspace: 1},
	}, nil)
	g := vt.NewGrid(40, 20, 0)
	Draw(g, open, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[0], "▼ clients") {
		t.Errorf("an open group points down:\n%q", lines[0])
	}
	if strings.Index(lines[1], "backend") <= strings.Index(lines[0], "clients") {
		t.Errorf("the member should be indented:\n%q\n%q", lines[0], lines[1])
	}

	folded := sidebarFrame([]SidebarRow{{
		Kind: SidebarSpaceGroup, Label: "clients", Group: "clients",
		Folded: true, Trailing: "2 waiting", Action: ActionToggleGroup,
	}}, nil)
	g = vt.NewGrid(40, 20, 0)
	Draw(g, folded, DefaultTheme())
	line := gridText(g)[0]
	if !strings.Contains(line, "▶ clients") {
		t.Errorf("a folded group points right:\n%q", line)
	}
	// Folding is not a way to stop being told an agent is waiting.
	if !strings.Contains(line, "2 waiting") {
		t.Errorf("a folded group should still report its members:\n%q", line)
	}

	row, ok := SidebarRowAt(folded, 3, 0, 20)
	if !ok || row.Action != ActionToggleGroup || row.Group != "clients" {
		t.Errorf("heading target = %+v ok=%v", row, ok)
	}
}

// TestGroupMenuActsOnTheGroup: a group has no record of its own, so its menu
// carries the name rather than an identifier.
func TestGroupMenuActsOnTheGroup(t *testing.T) {
	m := GroupMenu("clients", false, 0, 0)
	if m.Group != "clients" || m.Workspace != 0 {
		t.Errorf("group menu targets %+v", m)
	}
	if m.Items[0].Label != "fold" {
		t.Errorf("an open group offers to fold, got %q", m.Items[0].Label)
	}
	if shut := GroupMenu("clients", true, 0, 0); shut.Items[0].Label != "unfold" {
		t.Errorf("a folded group offers to unfold, got %q", shut.Items[0].Label)
	}
}

// TestSpaceMenuOffersANewGroupFirstHand: making a group is on the space's
// own menu, and moving into one appears once a group exists. If it
// regresses, the first group is behind a submenu nobody opens, as the owner
// found — "there is no new group".
func TestSpaceMenuOffersANewGroupFirstHand(t *testing.T) {
	has := func(m Menu, label string) bool {
		for _, it := range m.Items {
			if it.Label == label {
				return true
			}
		}
		return false
	}
	none := SpaceMenu(1, false, 0, 0)
	if !has(none, "new group...") || has(none, "move to group...") {
		t.Errorf("with no groups: %+v", none.Items)
	}
	some := SpaceMenu(1, true, 0, 0)
	if !has(some, "new group...") || !has(some, "move to group...") {
		t.Errorf("with groups: %+v", some.Items)
	}
}

// TestGroupPickMenuOffersEveryOtherGroup: moving a space into a group is
// picking one that exists, making one, or leaving the one it is in. If it
// regresses, joining a group means typing its name exactly, and a typo makes
// a second group instead.
func TestGroupPickMenuOffersEveryOtherGroup(t *testing.T) {
	m := GroupPickMenu(7, "clients", []string{"clients", "servers", "docs"}, 0, 0)
	var labels []string
	for _, it := range m.Items {
		labels = append(labels, it.Label)
	}
	want := []string{"servers", "docs", "new group...", "remove from group"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Errorf("items = %q, want %q", labels, want)
	}
	if m.Workspace != 7 || m.Items[0].Arg != "servers" || m.Items[3].Arg != "" {
		t.Errorf("menu = %+v", m)
	}
	if out := GroupPickMenu(7, "", nil, 0, 0); len(out.Items) != 1 || out.Items[0].Label != "new group..." {
		t.Errorf("a space in no group, with no groups made, can only make one: %+v", out.Items)
	}
}

// --- the divided sidebar ---------------------------------------------------

// TestSidebarSectionsAreSeparate is the bug this was found by: the spaces
// pushed the agents off the bottom, so the list that matters most was the one
// you could not see.
func TestSidebarSectionsAreSeparate(t *testing.T) {
	f := sidebarFrame(spaceRows(9), []SidebarRow{
		{Kind: SidebarHeading, Label: "agents"},
		{Kind: SidebarAgent, Label: "one", Detail: "claude", Pane: 1},
	})
	const rows = 20

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	if !strings.Contains(text, "agents") || !strings.Contains(text, "claude") {
		t.Errorf("a long list of spaces must not hide the agents:\n%s", text)
	}
	if !strings.Contains(text, "spaces") {
		t.Errorf("both headings stay on screen:\n%s", text)
	}
}

// TestSidebarHeadingsStayPut: a list whose title scrolls away leaves names
// with nothing saying what they are names of, and takes the toggle with it.
func TestSidebarHeadingsStayPut(t *testing.T) {
	f := sidebarFrame(spaceRows(9), nil)
	const rows = 20
	spaces, _ := SidebarRegions(f, rows)

	f.Spaces.Scroll = SidebarMaxScroll(f.Spaces, spaces.Rows)
	if f.Spaces.Scroll == 0 {
		t.Fatal("this list should be scrollable")
	}
	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[spaces.Y], "spaces") {
		t.Errorf("the heading should stay at the top of its section:\n%q", lines[spaces.Y])
	}
	// And it is still the click target it was.
	if row, ok := SidebarRowAt(f, 2, spaces.Y, rows); !ok || row.Kind != SidebarHeading {
		t.Errorf("the pinned heading should still be hit-testable, got %+v ok=%v", row, ok)
	}
}

// TestSidebarDividerIsGrabbable: the line between the lists is a handle, and
// hit-testing has to find it where it is drawn.
func TestSidebarDividerIsGrabbable(t *testing.T) {
	f := sidebarFrame(spaceRows(3), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	const rows = 20

	at := SidebarSplitAt(f, rows)
	if got := SidebarPlaceAt(f, 2, at, rows); got != SidebarDivider {
		t.Errorf("the divider is at %d, hit-test says %v", at, got)
	}
	if got := SidebarPlaceAt(f, 2, at-1, rows); got != SidebarSpacesList {
		t.Errorf("above the divider is the spaces list, got %v", got)
	}
	if got := SidebarPlaceAt(f, 2, at+1, rows); got != SidebarAgentsList {
		t.Errorf("below the divider is the agents list, got %v", got)
	}
	if got := SidebarPlaceAt(f, SidebarWidth, at, rows); got != SidebarNowhere {
		t.Errorf("past the sidebar is nowhere, got %v", got)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if line := gridText(g)[at]; !strings.Contains(line, "─") {
		t.Errorf("the divider should be drawn where it is grabbed:\n%q", line)
	}
}

// TestSidebarSplitIsClamped: a drag past either end stops where the divider
// can go, rather than storing a position it cannot.
func TestSidebarSplitIsClamped(t *testing.T) {
	f := sidebarFrame(spaceRows(3), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	const rows = 20
	height := SidebarHeight(rows)

	f.SidebarSplit = -50
	if at := SidebarSplitAt(f, rows); at < 1 || at >= height {
		t.Errorf("dragged off the top = %d, out of 0..%d", at, height)
	}
	f.SidebarSplit = 500
	high := SidebarSplitAt(f, rows)
	if high >= height {
		// There must be room left for the list below it.
		_, agents := SidebarRegions(f, rows)
		if agents.Rows <= 0 {
			t.Errorf("dragged off the bottom leaves no agents list: split=%d", high)
		}
	}

	// A terminal too short to divide gives the space to one list rather than
	// drawing a divider with nothing either side of it.
	tiny := sidebarFrame(spaceRows(1), nil)
	if _, agents := SidebarRegions(tiny, 4); agents.Rows != 0 {
		t.Errorf("no room to divide should leave one list, got %+v", agents)
	}
}

// TestSidebarScrollsEachSectionOnItsOwn: one list filling up must not push the
// other out of sight, which is the whole reason they are divided.
func TestSidebarScrollsEachSectionOnItsOwn(t *testing.T) {
	f := sidebarFrame(spaceRows(9), []SidebarRow{
		{Kind: SidebarHeading, Label: "agents"},
		{Kind: SidebarAgent, Label: "one", Detail: "claude", Pane: 1},
	})
	const rows = 20
	spaces, agents := SidebarRegions(f, rows)

	if SidebarMaxScroll(f.Spaces, spaces.Rows) == 0 {
		t.Fatal("nine spaces should not fit their section")
	}
	if got := SidebarMaxScroll(f.Agents, agents.Rows); got != 0 {
		t.Errorf("one agent fits, so its list should not scroll: %d", got)
	}

	// Scrolling the spaces does not move the agents.
	f.Spaces.Scroll = 3
	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if !strings.Contains(strings.Join(gridText(g), "\n"), "claude") {
		t.Error("scrolling one list should leave the other where it was")
	}

	// Hit-testing follows the scroll: the first entry under the heading is
	// the one the offset put there.
	row, ok := SidebarRowAt(f, 2, spaces.Y+1, rows)
	if !ok || row.Workspace != 4 {
		t.Errorf("scrolled by three, the first space shown is 4, got %+v", row)
	}
}

// TestSidebarRevealMovesAsLittleAsPossible: jumping to another space should
// move the list only as far as it must, so what surrounds the entry being left
// stays where the eye last saw it.
func TestSidebarRevealMovesAsLittleAsPossible(t *testing.T) {
	f := sidebarFrame(spaceRows(9), nil)
	const rows = 20
	spaces, _ := SidebarRegions(f, rows)

	if got := SidebarRevealScroll(f.Spaces, spaces.Rows, 2); got != 0 {
		t.Errorf("reveal of a visible entry = %d, want 0", got)
	}
	last := len(f.Spaces.Rows) - 1
	at := SidebarRevealScroll(f.Spaces, spaces.Rows, last)
	if at == 0 || at > SidebarMaxScroll(f.Spaces, spaces.Rows) {
		t.Errorf("reveal of the last entry = %d, max %d", at, SidebarMaxScroll(f.Spaces, spaces.Rows))
	}
	f.Spaces.Scroll = at
	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if !strings.Contains(strings.Join(gridText(g), "\n"), "new") {
		t.Error("the revealed entry should be on screen")
	}

	// The heading is pinned, so revealing it moves nothing.
	if got := SidebarRevealScroll(f.Spaces, spaces.Rows, 0); got != at {
		t.Errorf("revealing a pinned row moved the list from %d to %d", at, got)
	}
}

// TestSidebarActiveRowPrefersTheCursor: while the list has the keyboard, what
// must stay in view is where the cursor is, not where the user came from.
func TestSidebarActiveRowPrefersTheCursor(t *testing.T) {
	s := SidebarSection{Rows: spaceRows(3)}
	s.Rows[1].Active = true
	s.Rows[3].Selected = true
	if got := SidebarActiveRow(s); got != 3 {
		t.Errorf("active row = %d, want the selected one", got)
	}

	s.Rows[3].Selected = false
	if got := SidebarActiveRow(s); got != 1 {
		t.Errorf("active row = %d, want the current space", got)
	}
	if got := SidebarActiveRow(SidebarSection{Rows: spaceRows(3)}); got != -1 {
		t.Errorf("with nothing current the answer is none, got %d", got)
	}
}

// TestMenuStaysOnTheScreen: a menu opened near the bottom edge would lose the
// items added last, which are the destructive ones.
func TestMenuStaysOnTheScreen(t *testing.T) {
	m := PaneMenu(1, 78, 19, true, false)
	r := m.Rect(80, 20)
	if r.X+r.Cols > 80 || r.Y+r.Rows > 20 {
		t.Errorf("menu at %+v runs off an 80x20 screen", r)
	}
	if r.Rows != len(m.Items)+2 {
		t.Errorf("menu should show every item: rows=%d items=%d", r.Rows, len(m.Items))
	}

	// On a screen too small for it, it is clipped rather than placed outside.
	small := m.Rect(10, 3)
	if small.X != 0 || small.Y != 0 || small.Cols > 10 || small.Rows > 3 {
		t.Errorf("menu on a tiny screen: %+v", small)
	}
}

// TestMenuHitTestSeparatesInsideFromOnAnItem: a click inside the border but
// not on an item must not fall through, or one gesture would close the menu
// and act on whatever it was covering.
func TestMenuHitTestSeparatesInsideFromOnAnItem(t *testing.T) {
	m := TabMenu(4, 10, 5)
	r := m.Rect(80, 24)

	if _, onItem, inside := MenuItemAt(m, 80, 24, r.X, r.Y); onItem || !inside {
		t.Errorf("the border is inside but not an item: onItem=%v inside=%v", onItem, inside)
	}
	item, onItem, inside := MenuItemAt(m, 80, 24, r.X+2, r.Y+1)
	if !onItem || !inside || item.Action != MenuNewTab {
		t.Errorf("first item = %+v onItem=%v inside=%v", item, onItem, inside)
	}
	last, onItem, _ := MenuItemAt(m, 80, 24, r.X+2, r.Y+len(m.Items))
	if !onItem || last.Action != MenuClose {
		t.Errorf("last item = %+v onItem=%v", last, onItem)
	}
	if _, _, inside := MenuItemAt(m, 80, 24, r.X-1, r.Y); inside {
		t.Error("a point outside the border is outside the menu")
	}
}

// TestMenuTargetsWhatItWasOpenedOn is the whole reason a menu is better than
// a key here: there is no current selection to be wrong about.
func TestMenuTargetsWhatItWasOpenedOn(t *testing.T) {
	if m := PaneMenu(7, 0, 0, true, false); m.Pane != 7 || m.Tab != 0 || m.Workspace != 0 {
		t.Errorf("pane menu targets %+v", m)
	}
	if m := SpaceMenu(3, false, 0, 0); m.Workspace != 3 || m.Pane != 0 {
		t.Errorf("space menu targets %+v", m)
	}

	// The last pane in a tab offers no "close pane": closing it would leave an
	// empty tab, and "close tab" is the honest name for that.
	alone := PaneMenu(1, 0, 0, false, false)
	for _, item := range alone.Items {
		if item.Action == MenuClose {
			t.Error("a lone pane should not offer to close itself")
		}
	}
}

// TestTabBarStartsWhereThePanesDo: the tabs belong to one space, so a bar
// running over the sidebar would read as though they belonged to the session.
func TestTabBarStartsWhereThePanesDo(t *testing.T) {
	f := sidebarFrame([]SidebarRow{{Kind: SidebarHeading, Label: "spaces"}}, nil)
	f.Tabs = []Tab{{ID: 1, Name: "tab 1", Active: true}}

	g := vt.NewGrid(60, 8, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)

	if at := strings.Index(lines[0], "tab 1"); at < SidebarWidth {
		t.Errorf("the bar should start past the sidebar, found at %d:\n%q", at, lines[0])
	}
	// The sidebar owns its own top row rather than starting below the bar.
	if !strings.Contains(lines[0], "spaces") {
		t.Errorf("the sidebar should run from the top:\n%q", lines[0])
	}

	// A click on the sidebar's top row is not a click on a tab.
	if _, _, ok := TabAt(f, 2, 0, 60, 20); ok {
		t.Error("the sidebar's columns should not answer for the tab bar")
	}
	if _, _, ok := TabAt(f, SidebarWidth+2, 0, 60, 20); !ok {
		t.Error("the first tab should be clickable where it is drawn")
	}

	// With no sidebar the bar starts after the gutter kept for the handle
	// that brings it back, and not before it.
	plain := Frame{Tabs: f.Tabs}
	if segs := TabSegments(plain, 60); len(segs) == 0 || segs[0].Start != ShowHandleWidth {
		t.Errorf("without a sidebar the bar should start past the handle: %+v", segs)
	}
}

// TestStatusSaysHowManyAreWaiting: the whole reason to run tend is that the
// agent needing you is usually not the one on screen, and the list can be
// folded, scrolled, or turned off.
func TestStatusSaysHowManyAreWaiting(t *testing.T) {
	g := vt.NewGrid(60, 6, 0)
	Draw(g, Frame{Session: "work", Waiting: 2}, DefaultTheme())
	last := gridText(g)[5]
	if !strings.Contains(last, "2 waiting") {
		t.Errorf("the status bar should say what is waiting:\n%q", last)
	}

	g = vt.NewGrid(60, 6, 0)
	Draw(g, Frame{Session: "work"}, DefaultTheme())
	if last := gridText(g)[5]; strings.Contains(last, "waiting") {
		t.Errorf("with nothing waiting it should say nothing:\n%q", last)
	}
}

// TestSidebarHandleTogglesFromEitherSide: a way out that is only a keystroke
// is one that somebody who arrived by mouse cannot find again.
func TestSidebarHandleTogglesFromEitherSide(t *testing.T) {
	const rows = 20
	shown := sidebarFrame(spaceRows(2), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})

	g := vt.NewGrid(60, rows, 0)
	Draw(g, shown, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	if !strings.Contains(text, "«") {
		t.Errorf("a shown sidebar offers to hide itself:\n%s", text)
	}
	if !SidebarHandleAt(shown, SidebarWidth-2, SidebarHeight(rows)-1, rows) {
		t.Error("the handle should be where it is drawn")
	}
	if SidebarHandleAt(shown, 2, 2, rows) {
		t.Error("the rest of the list is not the handle")
	}

	hidden := Frame{Tabs: []Tab{{ID: 1, Name: "tab 1"}}}
	g = vt.NewGrid(60, rows, 0)
	Draw(g, hidden, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[0], "»") {
		t.Errorf("a hidden sidebar offers to come back:\n%q", lines[0])
	}
	if !SidebarHandleAt(hidden, 0, 0, rows) {
		t.Error("the handle should answer where it is drawn")
	}

	// The gutter is kept for it, so the tab bar does not run over it.
	if got := SidebarGutter(hidden, 60); got != ShowHandleWidth {
		t.Errorf("hidden gutter = %d, want room for the handle", got)
	}
	if _, _, ok := TabAt(hidden, 0, 0, 60, 20); ok {
		t.Error("the handle's columns are not the tab bar's")
	}
	if at := strings.Index(lines[0], "tab 1"); at < ShowHandleWidth {
		t.Errorf("the tabs should start past the handle:\n%q", lines[0])
	}
}

// TestSidebarFooterStaysAtTheBottom: a button that floats after the last entry
// moves every time the list grows, so the place to reach for it changes with
// something the user did not do.
func TestSidebarFooterStaysAtTheBottom(t *testing.T) {
	const rows = 24
	f := sidebarFrame(spaceRows(2), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	f.Spaces.Footer = []SidebarRow{{
		Kind: SidebarAction, Label: "new", Action: ActionNewSpace,
		Trailing: "menu", TrailingAction: ActionOpenMenu,
	}}
	spaces, _ := SidebarRegions(f, rows)
	foot := spaces.Y + spaces.Rows - 1

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	lines := gridText(g)
	if !strings.Contains(lines[foot], "new") || !strings.Contains(lines[foot], "menu") {
		t.Errorf("the footer should be on the last line of its section:\n%q", lines[foot])
	}
	// Directly above the divider, not floating after the entries.
	if at := SidebarSplitAt(f, rows); foot != at-1 {
		t.Errorf("footer at %d, divider at %d: they should touch", foot, at)
	}

	// A longer list does not move it.
	f.Spaces.Rows = spaceRows(12)
	g = vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	if line := gridText(g)[foot]; !strings.Contains(line, "new") {
		t.Errorf("the footer moved when the list grew:\n%q", line)
	}

	// And it is clickable where it is drawn, both halves of it.
	if row, ok := SidebarRowAt(f, 2, foot, rows); !ok || row.Action != ActionNewSpace {
		t.Errorf("the footer should be a target, got %+v ok=%v", row, ok)
	}
	at := TrailingStart(f.Spaces.Footer[0], SidebarWidth)
	if row, ok := SidebarRowAt(f, at, foot, rows); !ok || row.Action != ActionOpenMenu {
		t.Errorf("the footer's button should be its own target, got %+v", row)
	}
}

// TestSidebarGivesTheRoomToSpaces: a space is a place that goes on existing,
// an agent is what happens to be running, and there are rarely many at once.
func TestSidebarGivesTheRoomToSpaces(t *testing.T) {
	const rows = 30
	f := sidebarFrame(spaceRows(12), []SidebarRow{
		{Kind: SidebarHeading, Label: "agents"},
		{Kind: SidebarAgent, Label: "one", Detail: "claude", Pane: 1},
	})
	spaces, agents := SidebarRegions(f, rows)

	if spaces.Rows <= agents.Rows {
		t.Errorf("spaces got %d lines, agents %d: the spaces should have the room",
			spaces.Rows, agents.Rows)
	}
	if agents.Rows < sidebarMinSection {
		t.Errorf("the agents list should keep a usable strip, got %d", agents.Rows)
	}
	// And the default is a default: a drag still decides.
	f.SidebarSplit = 6
	if got := SidebarSplitAt(f, rows); got != 6 {
		t.Errorf("a dragged split should be kept, got %d", got)
	}
}

// TestSidebarKeepsRoomForAgentsThatHaveNotArrived: sizing the strip to what is
// in it means an empty one is three lines and the first agent to appear has
// nowhere to appear.
func TestSidebarKeepsRoomForAgentsThatHaveNotArrived(t *testing.T) {
	const rows = 30
	empty := sidebarFrame(spaceRows(12), []SidebarRow{{Kind: SidebarHeading, Label: "agents"}})
	_, agents := SidebarRegions(empty, rows)

	if agents.Rows <= sectionHeight(empty.Agents) {
		t.Errorf("an empty list got %d lines for %d of content: it should keep room",
			agents.Rows, sectionHeight(empty.Agents))
	}
	// But not so much that the spaces suffer for it.
	spaces, _ := SidebarRegions(empty, rows)
	if spaces.Rows <= agents.Rows {
		t.Errorf("spaces %d, agents %d: the spaces still get the room", spaces.Rows, agents.Rows)
	}

	// A full list is capped rather than taking the column.
	full := sidebarFrame(spaceRows(2), append(
		[]SidebarRow{{Kind: SidebarHeading, Label: "agents"}},
		spaceRows(20)...,
	))
	_, many := SidebarRegions(full, rows)
	if many.Rows > agentsCeiling(SidebarHeight(rows)) {
		t.Errorf("a long list of agents took %d of %d lines", many.Rows, rows)
	}
	// At the ceiling the two are even; the agents never get more than half.
	if spacesOf, _ := SidebarRegions(full, rows); spacesOf.Rows < many.Rows {
		t.Errorf("spaces %d, agents %d: the agents should never take the larger share",
			spacesOf.Rows, many.Rows)
	}
}

// reversedRuns returns the text of the cells drawn reversed on a row, which is
// how a selection or a marked item shows.
func reversedRuns(g *vt.Grid, y int) string {
	row := g.Line(y)
	if row == nil {
		return ""
	}
	var b strings.Builder
	for x := 0; x < row.Len(); x++ {
		if c := row.Cell(x); c.Style.Has(vt.AttrReverse) && c.Width > 0 {
			r := c.R
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// TestMenuMarksTheSelectedItem: reversing the whole panel and then reversing
// the selected row inside it leaves nothing to reverse, so the mark has to be
// the thing the panel is not.
func TestMenuMarksTheSelectedItem(t *testing.T) {
	m := PaneMenu(1, 10, 4, true, false)
	m.Selected = 2
	r := m.Rect(80, 24)

	g := vt.NewGrid(80, 24, 0)
	Draw(g, Frame{Menu: &m}, DefaultTheme())

	// Only the selected row is marked, and it is marked whole.
	if got := reversedRuns(g, r.Y+1+m.Selected); got != m.Items[m.Selected].Label {
		t.Errorf("marked row = %q, want %q", got, m.Items[m.Selected].Label)
	}
	for i := range m.Items {
		if i == m.Selected {
			continue
		}
		if got := reversedRuns(g, r.Y+1+i); got != "" {
			t.Errorf("row %d should not be marked, got %q", i, got)
		}
	}

	// With nothing selected, nothing is marked: the pointer is on the thing
	// the menu was opened on, not on a choice.
	none := PaneMenu(1, 10, 4, true, false)
	none.Selected = -1
	g = vt.NewGrid(80, 24, 0)
	Draw(g, Frame{Menu: &none}, DefaultTheme())
	for i := range none.Items {
		if got := reversedRuns(g, r.Y+1+i); got != "" {
			t.Errorf("nothing should be marked, row %d = %q", i, got)
		}
	}
}

// TestParseMouseReadsMotionAndModifiers: the terminal packs both into the
// button number rather than reporting them separately.
func TestParseMouseReadsMotionAndModifiers(t *testing.T) {
	ev, n, incomplete := parseMouse([]byte("\x1b[<35;10;5M"))
	if incomplete || n == 0 {
		t.Fatalf("motion report not read: n=%d incomplete=%v", n, incomplete)
	}
	if ev.Kind != MouseMove {
		t.Errorf("kind = %v, want MouseMove", ev.Kind)
	}
	if ev.X != 9 || ev.Y != 4 {
		t.Errorf("at %d,%d, want 9,4", ev.X, ev.Y)
	}

	// A drag says motion too, but names a button with it.
	if ev, _, _ := parseMouse([]byte("\x1b[<32;10;5M")); ev.Kind != MouseDrag {
		t.Errorf("button held = %v, want MouseDrag", ev.Kind)
	}

	// Modifiers ride in the same number and must not be read as buttons.
	ev, _, _ = parseMouse([]byte("\x1b[<8;10;5M"))
	if ev.Kind != MousePress || ev.Button != 0 {
		t.Errorf("alt+left = kind %v button %d, want a left press", ev.Kind, ev.Button)
	}
	if !ev.Mods.Has(ModAlt) || ev.Mods.Has(ModCtrl) {
		t.Errorf("mods = %b, want alt alone", ev.Mods)
	}
	if ev, _, _ := parseMouse([]byte("\x1b[<20;10;5M")); !ev.Mods.Has(ModCtrl | ModShift) {
		t.Errorf("ctrl+shift = %b", ev.Mods)
	}
}

// selScreen is a terminal with lines written into it, for selection tests.
func selScreen(t *testing.T, cols, rows int, lines ...string) *vt.Screen {
	t.Helper()
	s := vt.NewScreen(cols, rows, 100)
	for i, line := range lines {
		if i > 0 {
			_, _ = s.Write([]byte("\r\n"))
		}
		_, _ = s.Write([]byte(line))
	}
	return s
}

// TestSelectionMarksWhatItCovers: the mark reverses what is there rather than
// painting over it, so styled text stays readable in any theme.
func TestSelectionMarksWhatItCovers(t *testing.T) {
	screen := selScreen(t, 20, 3, "alpha bravo")
	f := Frame{
		Panes: []Pane{{
			ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 20, Rows: 5},
			Screen: screen, Running: true,
		}},
		Selection: &Selection{Pane: 1, AnchorX: 0, AnchorY: 0, CursorX: 4, CursorY: 0},
	}

	g := vt.NewGrid(20, 6, 0)
	Draw(g, f, DefaultTheme())
	if got := reversedRuns(g, 1); got != "alpha" {
		t.Errorf("marked = %q, want %q", got, "alpha")
	}

	// A selection in another pane marks nothing here.
	f.Selection = &Selection{Pane: 9, AnchorX: 0, AnchorY: 0, CursorX: 4, CursorY: 0}
	g = vt.NewGrid(20, 6, 0)
	Draw(g, f, DefaultTheme())
	if got := reversedRuns(g, 1); got != "" {
		t.Errorf("another pane's selection marked %q", got)
	}
}

// TestSetClipboardIsTheTerminalsJob: through the terminal rather than a
// platform tool, because that is the only route that works over ssh.
func TestSetClipboardIsTheTerminalsJob(t *testing.T) {
	got := SetClipboard("hi")
	if !strings.HasPrefix(got, "\x1b]52;c;") || !strings.HasSuffix(got, "\a") {
		t.Errorf("clipboard sequence = %q", got)
	}
	if !strings.Contains(got, base64.StdEncoding.EncodeToString([]byte("hi"))) {
		t.Errorf("sequence should carry the text encoded: %q", got)
	}
	if SetClipboard("") != "" {
		t.Error("nothing to copy should send nothing")
	}
}

// TestSelectionBlockTakesOnlyTheColumns is the case an agent creates: it draws
// its own panel down the right of the pane, and a run spanning three lines
// takes the whole width of the middle one, panel and all.
func TestSelectionBlockTakesOnlyTheColumns(t *testing.T) {
	screen := selScreen(t, 40, 5, "AAA  ppp", "BBB  qqq", "CCC  rrr")
	drag := Selection{Pane: 1, AnchorX: 0, AnchorY: 0, CursorX: 2, CursorY: 2}

	drag.Block = true

	// The mark shows the shape; cutting the text out of it is the server's
	// job, because a column is a property of the cells and those live there.
	f := Frame{
		Panes:     []Pane{{ID: 1, Rect: Rect{X: 0, Y: 0, Cols: 40, Rows: 6}, Screen: screen, Running: true}},
		Selection: &drag,
	}
	g := vt.NewGrid(40, 8, 0)
	Draw(g, f, DefaultTheme())
	for i, want := range []string{"AAA", "BBB", "CCC"} {
		if got := reversedRuns(g, 1+i); got != want {
			t.Errorf("row %d marked %q, want %q", i, got, want)
		}
	}
}

// TestEncodeMouseSpeaksInThePanesCoordinates: the program believes it has a
// terminal to itself whose top-left cell is 1,1. Handed the report as it
// arrived, it sees every click displaced by the sidebar and the border.
func TestEncodeMouseSpeaksInThePanesCoordinates(t *testing.T) {
	press := MouseEvent{Kind: MousePress, Button: 0}
	if got := string(EncodeMouse(press, 0, 0, true)); got != "\x1b[<0;1;1M" {
		t.Errorf("top-left press = %q", got)
	}
	release := MouseEvent{Kind: MouseRelease, Button: 0}
	if got := string(EncodeMouse(release, 4, 2, true)); got != "\x1b[<0;5;3m" {
		t.Errorf("release = %q", got)
	}
	drag := MouseEvent{Kind: MouseDrag, Button: 0, Mods: ModShift}
	if got := string(EncodeMouse(drag, 9, 9, true)); got != "\x1b[<36;10;10M" {
		t.Errorf("shift+drag = %q", got)
	}
	if got := string(EncodeMouse(MouseEvent{Kind: MouseWheelUp}, 1, 1, true)); got != "\x1b[<64;2;2M" {
		t.Errorf("wheel up = %q", got)
	}
	if got := string(EncodeMouse(MouseEvent{Kind: MouseWheelDown}, 1, 1, true)); got != "\x1b[<65;2;2M" {
		t.Errorf("wheel down = %q", got)
	}

	// What goes out reads back as what went in.
	back, n, _ := parseMouse(EncodeMouse(drag, 9, 9, true))
	if n == 0 || back.Kind != MouseDrag || back.X != 9 || back.Y != 9 || !back.Mods.Has(ModShift) {
		t.Errorf("round trip = %+v", back)
	}

	// The legacy encoding, for a program that never asked for the newer one.
	if got := EncodeMouse(press, 0, 0, false); string(got) != "\x1b[M\x20\x21\x21" {
		t.Errorf("legacy press = %q", got)
	}
	if got := EncodeMouse(release, 0, 0, false); got[3] != 32+3 {
		t.Errorf("legacy release should say button 3, got %d", got[3]-32)
	}
	// It cannot describe a cell past 223, and is dropped rather than sent
	// somewhere else.
	if got := EncodeMouse(press, 300, 0, false); got != nil {
		t.Errorf("an unencodable position should be dropped, got %q", got)
	}
	if got := EncodeMouse(press, -1, 0, true); got != nil {
		t.Errorf("a position outside the pane should be dropped, got %q", got)
	}
}

// TestEveryNamedThemeHasAPalette: a name the config accepts but no palette
// answers to would load without complaint and draw the default colours.
func TestEveryNamedThemeHasAPalette(t *testing.T) {
	for _, name := range config.ThemeNames {
		if _, ok := PaletteNamed(name); !ok {
			t.Errorf("theme %q has no palette", name)
		}
	}
	if len(palettes) != len(config.ThemeNames) {
		t.Errorf("%d palettes for %d names; one is unreachable", len(palettes), len(config.ThemeNames))
	}
}

// TestNamedThemeColoursTheInterfaceAndOverridesStillWin: picking a theme must
// change what is drawn — focused frame in its accent, states in its colours —
// and a colour set beside the name must beat the name, or "tokyo-night with a
// brighter idle" is impossible to write.
func TestNamedThemeColoursTheInterfaceAndOverridesStillWin(t *testing.T) {
	p, _ := PaletteNamed("tokyo-night")
	theme := ThemeFrom(config.Theme{Name: "Tokyo Night", Idle: "#010203"})

	if theme.BorderFocused.FG != p.Accent {
		t.Errorf("focused border = %v, want the theme's accent %v", theme.BorderFocused.FG, p.Accent)
	}
	if theme.Border.FG != p.Overlay0 || theme.Border.Attrs&vt.AttrDim != 0 {
		t.Errorf("border = %+v, want overlay0 %v undimmed", theme.Border, p.Overlay0)
	}
	if theme.Working.FG != p.Yellow || theme.Blocked.FG != p.Red {
		t.Error("working and blocked should take the theme's yellow and red")
	}
	if theme.Idle.FG != vt.RGBColor(1, 2, 3) {
		t.Errorf("idle = %v, want the override over the theme", theme.Idle.FG)
	}
	if ThemeFrom(config.Theme{}) != DefaultTheme() {
		t.Error("no name should leave the terminal-colour default alone")
	}
}

// TestTabBarStatusSitsRightOfTheTabsAndGivesWay: the status is drawn at the
// right end of the bar, the tabs stop short of it so a click cannot land on a
// tab hidden under it, and on a bar too narrow for both the tabs win.
func TestTabBarStatusSitsRightOfTheTabsAndGivesWay(t *testing.T) {
	f := Frame{
		Tabs:            []Tab{{ID: 1, Name: "build", Active: true}, {ID: 2, Name: "review"}},
		Status:          []StatusEntry{{Text: "ZOOM", Accent: true}, {Text: "09:05"}},
		StatusSeparator: " · ",
	}
	dst := vt.NewGrid(60, 5, 0)
	Draw(dst, f, DefaultTheme())
	line := strings.TrimRight(gridText(dst)[0], " ")
	if !strings.HasSuffix(line, "ZOOM · 09:05") {
		t.Errorf("tab bar = %q, want the status at its right end", line)
	}
	start, width := StatusArea(f, 60)
	if width != len("ZOOM · 09:05")-1 || start != 60-width { // "·" is one column, three bytes
		t.Errorf("StatusArea = %d, %d", start, width)
	}
	for _, seg := range TabSegments(f, 60) {
		if seg.End > start-1 {
			t.Errorf("segment %+v runs into the status at %d", seg, start)
		}
	}

	narrow := vt.NewGrid(24, 5, 0)
	Draw(narrow, f, DefaultTheme())
	if strings.Contains(gridText(narrow)[0], "09:05") {
		t.Error("on a narrow bar the status should give way to the tabs")
	}
}

// TestTheTabBarCanMoveToTheBottomOrStepAside: tab_bar_position = "bottom"
// draws the bar just above the status line and clicks find it there, and
// hide_tab_bar_when_single_tab gives the row back while there is one tab and
// not a moment longer. If either regresses the bar is drawn where clicks do
// not reach it, or a second tab has nowhere to appear.
func TestTheTabBarCanMoveToTheBottomOrStepAside(t *testing.T) {
	f := Frame{Tabs: []Tab{{ID: 1, Name: "build", Active: true}}, TabBarBottom: true}
	dst := vt.NewGrid(40, 10, 0)
	Draw(dst, f, DefaultTheme())
	lines := gridText(dst)
	if row := TabBarRow(f, 10); row != 10-StatusRows-1 || !strings.Contains(lines[row], "build") {
		t.Errorf("bottom bar on row %d:\n%s", row, strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[0], "build") {
		t.Error("a bottom bar was drawn at the top as well")
	}
	if _, _, ok := TabAt(f, 2, 10-StatusRows-1, 40, 10); !ok {
		t.Error("a click on the bottom bar found no tab")
	}
	if _, _, ok := TabAt(f, 2, 0, 40, 10); ok {
		t.Error("a click on the top row found the bottom bar's tab")
	}

	single := Frame{Tabs: []Tab{{ID: 1, Name: "build", Active: true}}, HideSingleTab: true}
	if TabBarRow(single, 10) != -1 || TabRows(single) != 0 {
		t.Error("one tab with hide_tab_bar_when_single_tab should have no bar")
	}
	single.Tabs = append(single.Tabs, Tab{ID: 2, Name: "review"})
	if TabBarRow(single, 10) != 0 {
		t.Error("a second tab should bring the bar back")
	}
}

// TestAUserCommandTakesItsKey: a key given to one of the user's commands
// runs it, even one that had a default — as herdr lets a custom command take
// a key — and says so; a digit, which picks a tab, is refused.
func TestAUserCommandTakesItsKey(t *testing.T) {
	commands := []config.CommandKey{
		{Key: "prefix+g", Command: "lazygit"},
		{Key: "z", Command: "make", Description: "build"},
	}
	custom, notes, err := CustomFrom(commands, nil)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(notes, "\n"); !strings.Contains(joined, "z was zoom, now runs build") ||
		!strings.Contains(joined, "g was navigator, now runs lazygit") {
		t.Errorf("notes = %q, want both taken keys reported", notes)
	}
	in := Input{Custom: custom}
	_, actions, _ := in.FeedAll([]byte{Prefix, 'z', Prefix, 'g'})
	if len(actions) != 2 || actions[0] != (Action{Command: CommandCustom, Arg: 1}) ||
		actions[1] != (Action{Command: CommandCustom, Arg: 0}) {
		t.Errorf("actions = %+v, want the two user commands", actions)
	}
	if _, _, err := CustomFrom([]config.CommandKey{{Key: "3", Command: "x"}}, nil); err == nil {
		t.Error("a digit should be refused: it picks a tab")
	}
	if help := strings.Join(CustomHelpLines(commands), "\n"); !strings.Contains(help, "build") {
		t.Errorf("help = %q, want the description", help)
	}
}

// TestHostReportsAreTakenOutOfTheInput: the terminal's answers about its
// scheme and background are read and never reach a pane as typing, even when
// a read splits one in two. If it regresses, "^[[?997;1n" lands in the shell
// every time the desktop changes theme.
func TestHostReportsAreTakenOutOfTheInput(t *testing.T) {
	rest, reports, hold := HostReports(nil, []byte("a\x1b[?997;2nb\x1b]11;rgb:ffff/ffff/ffff\x1b\\c\x1b]10;rgb:0/0/0\x07d"))
	if string(rest) != "abcd" || hold != nil {
		t.Errorf("rest = %q, hold = %q", rest, hold)
	}
	if len(reports) != 2 || reports[0] != (HostReport{Explicit: true, Light: true}) || reports[1] != (HostReport{Light: true}) {
		t.Errorf("reports = %+v", reports)
	}

	rest, reports, hold = HostReports(nil, []byte("x\x1b]11;#10"))
	if string(rest) != "x" || len(reports) != 0 || string(hold) != "\x1b]11;#10" {
		t.Fatalf("split: rest %q, reports %v, hold %q", rest, reports, hold)
	}
	rest, reports, hold = HostReports(hold, []byte("1010\x07y"))
	if string(rest) != "y" || hold != nil || len(reports) != 1 || reports[0].Light {
		t.Errorf("joined: rest %q, reports %+v, hold %q", rest, reports, hold)
	}

	// The keyboard's own escapes pass: a lone escape, an arrow, a mouse report.
	for _, keys := range []string{"\x1b", "\x1b[A", "\x1b[<0;1;1M", "\x1b[?25h"} {
		if rest, reports, hold := HostReports(nil, []byte(keys)); string(rest) != keys || len(reports) != 0 || hold != nil {
			t.Errorf("%q: rest %q, reports %v, hold %q", keys, rest, reports, hold)
		}
	}
}

// TestAutoSwitchPicksTheThemeForTheTerminal: with auto_switch on, a light
// terminal gets the light theme and a dark one the dark, herdr's defaults
// when none is named.
func TestAutoSwitchPicksTheThemeForTheTerminal(t *testing.T) {
	latte, _ := PaletteNamed("catppuccin-latte")
	mocha, _ := PaletteNamed("catppuccin")
	nord, _ := PaletteNamed("nord")
	c := config.Theme{AutoSwitch: true, Name: "dracula"}
	if got := ThemeFor(c, true).BorderFocused.FG; got != latte.Accent {
		t.Errorf("light terminal: accent %v, want latte's", got)
	}
	if got := ThemeFor(c, false).BorderFocused.FG; got != mocha.Accent {
		t.Errorf("dark terminal: accent %v, want mocha's", got)
	}
	c.DarkName = "nord"
	if got := ThemeFor(c, false).BorderFocused.FG; got != nord.Accent {
		t.Errorf("dark_name: accent %v, want nord's", got)
	}
	if ThemeFor(config.Theme{Name: "nord"}, true) != ThemeFrom(config.Theme{Name: "nord"}) {
		t.Error("without auto_switch the terminal's scheme should not matter")
	}
}

// TestSidebarTokenRowsResolveAndFit: a layout says which tokens, the session
// says what they hold, an empty one is left out, a rule can hide one, and a
// row too long for the column shares what room there is — herdr's rules. If
// it regresses, a configured sidebar shows empty separators, a hidden value,
// or runs off the column.
func TestSidebarTokenRowsResolveAndFit(t *testing.T) {
	rows := [][]config.SidebarToken{
		{{Name: "state_icon"}, {Name: "machine"}, {Name: "workspace"}, {Name: "tab"}},
		{{Name: "agent"}, {Name: "$ctx"}, {Name: "$gone"}},
		{{Name: "pane"}},
	}
	lines := ResolveAgentRows(rows, AgentTokenValues{
		Workspace: "api", Tab: "build", Agent: "claude",
		Custom: map[string]string{"ctx": "23%"},
	})
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2: an empty row is dropped", len(lines))
	}
	if len(lines[0]) != 3 {
		t.Errorf("first row = %+v, want the empty machine left out", lines[0])
	}

	f := Frame{Sidebar: true, Agents: SidebarSection{Rows: []SidebarRow{{
		Kind: SidebarAgent, Label: "x", State: "working", Running: true, Lines: lines,
	}}}}
	dst := vt.NewGrid(80, 12, 0)
	Draw(dst, f, DefaultTheme())
	text := gridText(dst)
	joined := strings.Join(text, "\n")
	if !strings.Contains(joined, "● api · build") || !strings.Contains(joined, "claude · 23%") {
		t.Errorf("the entry is not laid out as configured:\n%s", joined)
	}

	// Too long for the column: the text shares the room, each cut short.
	long := ResolveAgentRows([][]config.SidebarToken{{{Name: "state_icon"}, {Name: "workspace"}, {Name: "tab"}}},
		AgentTokenValues{Workspace: strings.Repeat("w", 30), Tab: strings.Repeat("t", 30)})
	f.Agents.Rows[0].Lines = long
	dst = vt.NewGrid(80, 12, 0)
	Draw(dst, f, DefaultTheme())
	for _, line := range gridText(dst) {
		if strings.Contains(line, "www") {
			sidebar := []rune(line)[:SidebarWidth]
			if !strings.Contains(string(sidebar), "ttt") || !strings.Contains(string(sidebar), "│") {
				t.Errorf("row %q should hold both, cut, inside the column", string(sidebar))
			}
		}
	}
}

// TestASidebarRuleHidesOrRecoloursAToken covers herdr's rules through
// resolution: the first matching rule decides, hide leaves the token out.
func TestASidebarRuleHidesOrRecoloursAToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(path, []byte(`[ui.sidebar.agents]
rows = [["agent", { token = "$ctx", rules = [{ equals = "0%", hide = true }, { gt = 80, bold = true }] }]]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := c.UI.Sidebar.AgentRows("claude")
	hidden := ResolveAgentRows(rows, AgentTokenValues{Agent: "claude", Custom: map[string]string{"ctx": "0%"}})
	if len(hidden[0]) != 1 {
		t.Errorf("0%% should be hidden: %+v", hidden)
	}
	bold := ResolveAgentRows(rows, AgentTokenValues{Agent: "claude", Custom: map[string]string{"ctx": "91"}})
	if len(bold[0]) != 2 || bold[0][1].Style.Bold == nil || !*bold[0][1].Style.Bold {
		t.Errorf("91 should be bold: %+v", bold)
	}
}

// TestStatusIconsAreHerdrs: dots mark anything happening filled, idle
// hollow, nothing known with a dot; symbols give each state its own glyph,
// so the list reads without colour.
func TestStatusIconsAreHerdrs(t *testing.T) {
	for _, c := range []struct {
		state         string
		dots, symbols string
	}{
		{"blocked", "●", "×"}, {"working", "●", "◐"}, {"done", "●", "✓"},
		{"idle", "○", "○"}, {"", "·", "·"},
	} {
		if got := StatusIcon(c.state, true, false); got != c.dots {
			t.Errorf("dots %q = %q, want %q", c.state, got, c.dots)
		}
		if got := StatusIcon(c.state, true, true); got != c.symbols {
			t.Errorf("symbols %q = %q, want %q", c.state, got, c.symbols)
		}
	}
	if StatusIcon("working", false, false) != "·" {
		t.Error("an exited pane says nothing about an agent")
	}
	if DefaultTheme().StateStyle("done", true) != DefaultTheme().Done {
		t.Error("done should have its own colour")
	}
}

// TestANamedThemeDrawsItsOwnSurfaces: a palette with a panel colour puts the
// bars and panels on it, and what is chosen on its accent, as herdr does;
// one without (terminal) keeps the reversed bars, which are the only thing
// that sets them apart. If it regresses, a theme recolours the text and
// leaves the bars in the terminal's own reverse video.
func TestANamedThemeDrawsItsOwnSurfaces(t *testing.T) {
	p, _ := PaletteNamed("tokyo-night")
	th := ThemeFrom(config.Theme{Name: "tokyo-night"})
	if th.Status.BG != p.PanelBG || th.Status.Attrs&vt.AttrReverse != 0 {
		t.Errorf("status = %+v, want on panel_bg, not reversed", th.Status)
	}
	if th.StatusKey.BG != p.Accent || th.StatusKey.FG != p.PanelBG {
		t.Errorf("active tab = %+v, want panel colour on the accent", th.StatusKey)
	}
	if th.MenuSelected.BG != p.Accent || th.Menu.BG != p.PanelBG {
		t.Error("the menu should be on panel_bg with its selection on the accent")
	}
	if th.SidebarSelected.BG != p.ActiveRowBG {
		t.Errorf("sidebar band = %+v, want active_row_bg", th.SidebarSelected)
	}

	term := ThemeFrom(config.Theme{Name: "terminal"})
	if term.Status.Attrs&vt.AttrReverse == 0 || term.MenuSelected.Attrs&vt.AttrReverse == 0 {
		t.Error("the terminal palette has no panel colour and should keep reverse video")
	}

	// And a frame draws with it: the bar's cells carry the panel colour.
	dst := vt.NewGrid(60, 6, 0)
	Draw(dst, Frame{Tabs: []Tab{{ID: 1, Name: "a", Active: true}, {ID: 2, Name: "b"}}}, th)
	if got := dst.Line(0).Cell(59).Style.BG; got != p.PanelBG {
		t.Errorf("tab bar background = %v, want %v", got, p.PanelBG)
	}
}

// TestFocusReportsAreTakenOutOfTheInput: the window's focus reports are the
// terminal's, not typing; one reaching a shell prints "^[[O".
func TestFocusReportsAreTakenOutOfTheInput(t *testing.T) {
	rest, reports, _ := HostReports(nil, []byte("a\x1b[Ob\x1b[Ic"))
	if string(rest) != "abc" || len(reports) != 2 || reports[0].Focus != FocusOut || reports[1].Focus != FocusIn {
		t.Errorf("rest %q, reports %+v", rest, reports)
	}
}

// TestEveryVisibleMatchOfASearchIsMarked: in copy mode every match on
// screen is marked, not only the one the cursor is on, with the search's
// own rule for case. If it regresses, n jumps somewhere and the user cannot
// see where the other matches were.
func TestEveryVisibleMatchOfASearchIsMarked(t *testing.T) {
	screen := vt.NewScreen(30, 3, 0)
	_, _ = screen.Write([]byte("error here, Error there\r\nno match"))
	pane := Pane{ID: 1, Rect: Rect{Cols: 32, Rows: 5}, Screen: screen, Running: true}

	marked := func(query string) []int {
		dst := vt.NewGrid(40, 6, 0)
		Draw(dst, Frame{Panes: []Pane{pane}, Highlight: &Highlight{Pane: 1, Query: query}}, DefaultTheme())
		inner := innerRect(pane.Rect)
		var cols []int
		for x := 0; x < inner.Cols; x++ {
			if dst.Line(inner.Y).Cell(inner.X+x).Style.Attrs&vt.AttrUnderline != 0 {
				cols = append(cols, x)
			}
		}
		return cols
	}
	if got := marked("error"); len(got) != 10 || got[0] != 0 || got[5] != 12 {
		t.Errorf("lowercase query marked %v, want both words, case ignored", got)
	}
	if got := marked("Error"); len(got) != 5 || got[0] != 12 {
		t.Errorf("a capital should match case: marked %v", got)
	}
}

// TestThemeCustomOverridesAPalettesTokens is herdr's [theme.custom]: any
// token of the palette, in any of herdr's colour forms, over the theme, and
// the light or dark set only while auto_switch has that appearance. If it
// regresses, a herdr user's colour tweaks load and change nothing.
func TestThemeCustomOverridesAPalettesTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(path, []byte(`[ui.theme]
name = "nord"
auto_switch = true
dark_name = "nord"
[ui.theme.custom]
accent = "#f5c2e7"
surface0 = "rgb(1, 2, 3)"
[ui.theme.custom.light]
accent = "#123"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dark := ThemeFor(c.UI.Theme, false)
	if dark.BorderFocused.FG != vt.RGBColor(0xf5, 0xc2, 0xe7) {
		t.Errorf("dark accent = %v, want the override", dark.BorderFocused.FG)
	}
	if dark.TabInactive.BG != vt.RGBColor(1, 2, 3) {
		t.Errorf("inactive tab = %+v, want the overridden surface0", dark.TabInactive)
	}
	light := ThemeFor(c.UI.Theme, true)
	if light.BorderFocused.FG != vt.RGBColor(0x11, 0x22, 0x33) {
		t.Errorf("light accent = %v, want the light override over the rest", light.BorderFocused.FG)
	}

	for name, body := range map[string]string{
		"unknown token": "[ui.theme.custom]\nsparkle = \"#fff\"\n",
		"bad colour":    "[ui.theme.custom]\naccent = \"rgb(1,2)\"\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestTheNavigationCursorGetsThePalettesSelection: with a palette, the row
// under the navigation cursor sits on selection_bg, as in herdr; without
// one, the › in the margin is the whole cursor, as before.
func TestTheNavigationCursorGetsThePalettesSelection(t *testing.T) {
	p, _ := PaletteNamed("tokyo-night")
	f := Frame{Sidebar: true, Spaces: SidebarSection{Rows: []SidebarRow{
		{Kind: SidebarSpace, Label: "api", Selected: true, Running: true},
	}}}
	dst := vt.NewGrid(60, 8, 0)
	Draw(dst, f, ThemeFrom(config.Theme{Name: "tokyo-night"}))
	if got := dst.Line(0).Cell(5).Style.BG; got != p.SelectionBG {
		t.Errorf("cursor row background = %v, want selection_bg", got)
	}
	dst = vt.NewGrid(60, 8, 0)
	Draw(dst, f, DefaultTheme())
	if got := dst.Line(0).Cell(5).Style.BG; !got.IsDefault() {
		t.Errorf("without a palette the row keeps its background, got %v", got)
	}
}

// --- the navigator ---------------------------------------------------------

func navSource() NavSource {
	return NavSource{
		Focused: 3,
		Workspaces: []NavWorkspace{
			{ID: 1, Label: "api", Branch: "main", Tabs: []NavTab{
				{ID: 10, Label: "tab 1", Panes: []NavPane{
					{ID: 1, Label: "claude", Meta: "/work/api", State: "blocked"},
					{ID: 2, Label: "pane 2", Meta: "/work/api/cmd", State: "unknown"},
				}},
			}},
			{ID: 2, Label: "web", Branch: "feat/login", Tabs: []NavTab{
				{ID: 20, Label: "tab 1", Panes: []NavPane{
					{ID: 3, Label: "codex", Meta: "/work/web", State: "working"},
				}},
				{ID: 21, Label: "logs", Panes: []NavPane{
					{ID: 4, Label: "pane 1", Meta: "/var/log", State: "idle"},
				}},
			}},
		},
	}
}

func navLabels(rows []NavigatorRow) string {
	var out []string
	for _, r := range rows {
		out = append(out, strings.Repeat(".", r.Depth)+r.Label)
	}
	return strings.Join(out, " ")
}

// TestNavigatorListsTheSessionAsATree: every space, and the tabs and panes
// of the open ones, with the focused pane marked. If it regresses, the
// navigator hides what it is for.
func TestNavigatorListsTheSessionAsATree(t *testing.T) {
	rows := BuildNavigatorRows(navSource(), "", "", map[uint64]bool{2: true})
	if got, want := navLabels(rows), "api web .tab 1 ..codex .logs ..pane 1"; got != want {
		t.Errorf("rows = %q, want %q", got, want)
	}
	for _, r := range rows {
		if r.Current != (r.Target.Pane == 3) {
			t.Errorf("%s current = %v", r.Label, r.Current)
		}
	}
	if rows[1].Meta != "feat/login" || rows[2].Meta != "1 panes" {
		t.Errorf("meta: space %q, tab %q", rows[1].Meta, rows[2].Meta)
	}
}

// TestNavigatorSearchesAndFiltersLikeHerdr: a query matches a pane by name
// or directory, a tab by name, a space by name or branch, and opens every
// space while it is on; a filter keeps one state, with the tabs and spaces
// around what it keeps. If it regresses, a search finds a pane and then
// hides it inside a closed space.
func TestNavigatorSearchesAndFiltersLikeHerdr(t *testing.T) {
	closed := map[uint64]bool{}
	cases := []struct {
		query  string
		filter NavFilter
		want   string
	}{
		{"claude", "", "api .tab 1 ..claude"},
		{"/var", "", "web .logs ..pane 1"},
		{"login", "", "web"},
		{"LOGS", "", "web .logs"},
		{"", "blocked", "api .tab 1 ..claude"},
		{"", "working", "web .tab 1 ..codex"},
		{"", "done", ""},
		{"nothing", "", ""},
	}
	for _, c := range cases {
		got := navLabels(BuildNavigatorRows(navSource(), c.query, c.filter, closed))
		if got != c.want {
			t.Errorf("query %q filter %q: %q, want %q", c.query, c.filter, got, c.want)
		}
	}
}

// TestNavigatorDrawsBranchesAndHitsWhatItDraws: a tab's panes hang off ├──
// and └── as in herdr, and a click lands on the row drawn under it, on the
// caret of a space or on the search line. If it regresses, a click opens a
// different pane from the one under the pointer.
func TestNavigatorDrawsBranchesAndHitsWhatItDraws(t *testing.T) {
	expanded := map[uint64]bool{1: true, 2: true}
	n := Navigator{Rows: BuildNavigatorRows(navSource(), "", "", expanded), Selected: NavTarget{Workspace: 2, Tab: 20, Pane: 3}}
	g := vt.NewGrid(80, 24, 0)
	drawNavigator(g, n, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, want := range []string{"/ search panes", "4 panes", "├── ", "└── ", "◆ ", "claude", "feat/login"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	for y, line := range gridText(g) {
		for _, label := range []string{"codex", "logs"} {
			x := strings.Index(line, label)
			if x < 0 || !strings.Contains(line, "──") {
				continue // the detail line names the selection too
			}
			hit := NavigatorAt(n, 80, 24, len([]rune(line[:x])), y)
			if hit.Row < 0 || n.Rows[hit.Row].Label != label {
				t.Errorf("a click on %q hit %+v", label, hit)
			}
		}
		if x := strings.Index(line, "▸ web"); x >= 0 || strings.Contains(line, "▾ web") {
			x = strings.Index(line, "▾ web")
			hit := NavigatorAt(n, 80, 24, len([]rune(line[:x])), y)
			if !hit.Caret || n.Rows[hit.Row].Label != "web" {
				t.Errorf("a click on web's caret: %+v", hit)
			}
		}
	}
	r := NavigatorRect(80, 24)
	if hit := NavigatorAt(n, 80, 24, r.X+4, r.Y+1); !hit.Search {
		t.Errorf("the search line: %+v", hit)
	}
	if hit := NavigatorAt(n, 80, 24, 0, 0); hit.Inside {
		t.Errorf("outside the popup: %+v", hit)
	}
}

// TestTheToastCardGoesInItsCorner: herdr's four corners, above the status
// bar, and the rect a click is tested against is the one drawn. If it
// regresses, the card covers the status bar or a click misses it.
func TestTheToastCardGoesInItsCorner(t *testing.T) {
	card := Toast{Kind: ToastAttention, Title: "claude needs attention", Body: "api · tab 1"}
	for pos, want := range map[string][2]bool{
		"": {true, true}, ToastBottomRight: {true, true}, ToastTopLeft: {false, false},
		ToastTopRight: {true, false}, ToastBottomLeft: {false, true},
	} {
		card.Position = pos
		r := ToastRect(card, 80, 24)
		right, bottom := r.X+r.Cols == 80, r.Y+r.Rows == 24-StatusRows
		if right != want[0] || bottom != want[1] || r.Rows != 4 {
			t.Errorf("%q: %+v", pos, r)
		}
		g := vt.NewGrid(80, 24, 0)
		drawToast(g, card, DefaultTheme())
		if line := gridText(g)[r.Y+1]; !strings.Contains(line, "● claude needs attention") {
			t.Errorf("%q: the title is not on the card's first line: %q", pos, line)
		}
	}
}

// TestTheWorktreePopupFiltersAndHitsItsEntries: the filter matches label,
// branch, path or status; the count says how many are left; a click on
// either line of an entry is that entry. If it regresses, a click opens
// the checkout below the one under the pointer.
func TestTheWorktreePopupFiltersAndHitsItsEntries(t *testing.T) {
	w := WorktreeOpen{Entries: []WorktreeEntry{
		{Label: "main", Branch: "main", Path: "/src/app", Status: "open"},
		{Label: "feature/login", Branch: "feature/login", Path: "/wt/app/feature-login"},
		{Label: "old", Path: "/wt/app/old", Status: "detached"},
	}}
	g := vt.NewGrid(100, 30, 0)
	drawWorktreeOpen(g, w, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, want := range []string{"open worktree", "3 checkouts", "feature/login", "/wt/app/feature-login", "open", "detached"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	for y, line := range gridText(g) {
		for i, e := range w.Entries {
			if x := strings.Index(line, e.Path); x >= 0 {
				col := len([]rune(line[:x]))
				if got, _, _ := WorktreeOpenAt(w, 100, 30, col, y); got != i {
					t.Errorf("a click on %s's path hit %d", e.Label, got)
				}
				if got, _, _ := WorktreeOpenAt(w, 100, 30, col, y-1); got != i {
					t.Errorf("a click on %s's name hit %d", e.Label, got)
				}
			}
		}
	}
	w.Query = "DETACH"
	if f := w.Filtered(); len(f) != 1 || f[0] != 2 {
		t.Errorf("filter by status: %v", f)
	}
	if i, ok := w.SelectedEntry(); !ok || i != 2 {
		t.Errorf("the selection falls to what the filter leaves: %d", i)
	}
}

// TestLinkAtFindsWebURLsAsHerdrDoes: herdr's own cases — a closing bracket
// the URL opened stays, one it did not goes, a full stop goes, a markdown
// link's text is not the link, and file:// is not a web URL. If it
// regresses, a ctrl+click opens "https://example.com/docs)," or nothing.
func TestLinkAtFindsWebURLsAsHerdrDoes(t *testing.T) {
	cases := []struct {
		row, at string
		want    string
	}{
		{"see https://example.com/a(b)c.", "example", "https://example.com/a(b)c"},
		{"[docs](https://example.com/docs),", "example", "https://example.com/docs"},
		{"[docs](https://example.com/docs)", "docs", ""},
		{"open file:///tmp/report", "file", ""},
		{"go to http://localhost:8080/x?y=1!", "localhost", "http://localhost:8080/x?y=1"},
		{"日本 https://例え.jp/パス end", "jp", "https://例え.jp/パス"},
	}
	for _, c := range cases {
		col := runewidth.StringWidth(c.row[:strings.Index(c.row, c.at)])
		got, ok := LinkAt(c.row, col)
		if got.URL != c.want || ok != (c.want != "") {
			t.Errorf("LinkAt(%q at %q) = %q %v, want %q", c.row, c.at, got.URL, ok, c.want)
		}
	}
	span, _ := LinkAt("see https://example.com/a(b)c.", 6)
	if span.Start != 4 || span.End != 28 {
		t.Errorf("columns %d-%d, want 4-28", span.Start, span.End)
	}
}

// TestControlCharactersInATitleAreSpaces: a pane titled with a newline in
// it — a command of several lines, a title a program set — draws within
// its frame. If it regresses, the newline reaches the terminal raw and the
// frame breaks where the title is.
func TestControlCharactersInATitleAreSpaces(t *testing.T) {
	g := vt.NewGrid(30, 5, 0)
	drawPane(g, Pane{ID: 2, Rect: Rect{Cols: 30, Rows: 5}, Title: "printf 'x\n'; read y\tz", Running: true}, DefaultTheme())
	top := g.Line(0)
	for x := 0; x < top.Len(); x++ {
		for _, mark := range top.Combining(x) {
			if mark < 0x20 {
				t.Fatalf("a control character was kept on the title row at %d", x)
			}
		}
	}
	if line := gridText(g)[0]; !strings.HasSuffix(strings.TrimRight(line, " "), "┐") {
		t.Errorf("the frame's corner is gone: %q", line)
	}
}

// twoMachineNav is navSource on this machine and one saved machine with an
// agent waiting, whose space, tab and pane numbers are the same as here.
func twoMachineNav() NavSource {
	here := navSource()
	return NavSource{Focused: here.Focused, Machines: []NavMachine{
		{ID: "local", Label: "Local", Shown: true, Workspaces: here.Workspaces},
		{ID: "m_far", Label: "farbox", Signal: MachineSignal(MachineOnline), Expanded: map[uint64]bool{1: true},
			Workspaces: []NavWorkspace{{ID: 1, Label: "farspace", Tabs: []NavTab{
				{ID: 10, Label: "tab 1", Panes: []NavPane{{ID: 1, Label: "claude", Meta: "/srv", State: "blocked"}}},
			}}}},
	}}
}

// TestTheNavigatorListsEveryMachine is herdr's federated navigator: a row for
// each machine with its spaces beneath, the other machine's rows pointing at
// it and this one's at nothing but here; a query matching a machine's name
// keeps everything on it, and a filter finds the agent waiting wherever it
// is. If it regresses, prefix+g shows one machine of several, or opens this
// machine's pane 1 for the other's.
func TestTheNavigatorListsEveryMachine(t *testing.T) {
	rows := BuildNavigatorRows(twoMachineNav(), "", "", map[uint64]bool{})
	if got, want := navLabels(rows), "Local .api .web farbox .farspace ..tab 1 ...claude"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	if !rows[0].Target.Host || rows[0].Target.Machine != "local" || !rows[3].Target.Host || rows[3].Signal != "●" {
		t.Errorf("machine rows: %+v / %+v", rows[0], rows[3])
	}
	if rows[1].Target.Machine != "" || rows[6].Target != (NavTarget{Workspace: 1, Tab: 10, Pane: 1, Machine: "m_far"}) {
		t.Errorf("targets: here %+v, there %+v", rows[1].Target, rows[6].Target)
	}

	if got, want := navLabels(BuildNavigatorRows(twoMachineNav(), "FARBOX", "", map[uint64]bool{})), "farbox .farspace ..tab 1 ...claude"; got != want {
		t.Errorf("a machine's name: %q, want %q", got, want)
	}
	if got, want := navLabels(BuildNavigatorRows(twoMachineNav(), "", "blocked", map[uint64]bool{})), "Local .api ..tab 1 ...claude farbox .farspace ..tab 1 ...claude"; got != want {
		t.Errorf("blocked: %q, want %q", got, want)
	}

	n := Navigator{Rows: rows, Selected: rows[0].Target}
	g := vt.NewGrid(80, 24, 0)
	drawNavigator(g, n, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, want := range []string{"▾ Local", "▾ farbox", "▸ api", "▾ farspace", "└── ", "●"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	for y, line := range gridText(g) {
		if x := strings.Index(line, "▾ farspace"); x >= 0 {
			hit := NavigatorAt(n, 80, 24, len([]rune(line[:x])), y)
			if !hit.Caret || n.Rows[hit.Row].Label != "farspace" {
				t.Errorf("a click on farspace's caret: %+v", hit)
			}
		}
	}
}

// TestAClickFindsTheOverlayLineDrawnUnderIt: the hit-test reads the layout
// the panel is drawn with, so a click on a line is that line — the welcome's
// continue button is found this way. If it regresses, clicking the button
// does nothing, or clicking beside it goes on.
func TestAClickFindsTheOverlayLineDrawnUnderIt(t *testing.T) {
	lines := []string{"title", "first", "", "[ go on ]"}
	g := vt.NewGrid(60, 20, 0)
	drawOverlay(g, lines, DefaultTheme())
	for y, line := range gridText(g) {
		if x := strings.Index(line, "[ go on ]"); x >= 0 {
			col := len([]rune(line[:x]))
			if got := OverlayLineAt(lines, 60, 20, col+2, y); got != 2 {
				t.Errorf("a click on the button = %d, want 2", got)
			}
			if got := OverlayLineAt(lines, 60, 20, col+2, y-2); got != 0 {
				t.Errorf("a click on the first line = %d, want 0", got)
			}
			if got := OverlayLineAt(lines, 60, 20, 0, y); got != -1 {
				t.Errorf("a click outside = %d, want -1", got)
			}
			return
		}
	}
	t.Fatal("the button was not drawn")
}
