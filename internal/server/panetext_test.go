package server

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/vt"
)

// textScreen writes lines into a terminal, letting the earlier ones scroll off
// into the history when there are more than it has rows.
func textScreen(t *testing.T, cols, rows int, lines ...string) *vt.Screen {
	t.Helper()
	s := vt.NewScreen(cols, rows, 100)
	for _, line := range lines {
		if _, err := s.Write([]byte(line + "\r\n")); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// TestPaneTextTakesARunOfText: a run spanning three lines takes the end of the
// first, all of the second and the start of the third, which is what dragging
// over prose is asking for.
func TestPaneTextTakesARunOfText(t *testing.T) {
	screen := textScreen(t, 40, 6, "alpha bravo", "charlie delta", "echo foxtrot")

	one := proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 0, ToCol: 4}
	if got := paneText(screen, one); got != "alpha" {
		t.Errorf("one line = %q", got)
	}

	many := proto.PaneTextParams{FromRow: 0, FromCol: 6, ToRow: 2, ToCol: 3}
	const want = "bravo\ncharlie delta\necho"
	if got := paneText(screen, many); got != want {
		t.Errorf("three lines = %q, want %q", got, want)
	}

	// Dragged backwards the same text comes out: which end moved is not
	// something the clipboard should know about.
	back := proto.PaneTextParams{FromRow: 2, FromCol: 3, ToRow: 0, ToCol: 6}
	if got := paneText(screen, back); got != want {
		t.Errorf("backwards = %q, want %q", got, want)
	}

	// A terminal pads its rows, and pasting that padding turns one line of
	// code into one line and seventy spaces.
	whole := proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 1, ToCol: 39}
	if got := paneText(screen, whole); got != "alpha bravo\ncharlie delta" {
		t.Errorf("padded rows = %q", got)
	}
}

// TestPaneTextTakesARectangle is the case an agent creates: it draws its own
// panel down the right of the pane, and a run takes the whole width of every
// line in the middle, panel and all.
func TestPaneTextTakesARectangle(t *testing.T) {
	screen := textScreen(t, 40, 6, "AAA  ppp", "BBB  qqq", "CCC  rrr")

	run := proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 2, ToCol: 2}
	if got := paneText(screen, run); got != "AAA  ppp\nBBB  qqq\nCCC" {
		t.Errorf("run = %q", got)
	}

	block := run
	block.Block = true
	if got := paneText(screen, block); got != "AAA\nBBB\nCCC" {
		t.Errorf("block = %q, want the columns alone", got)
	}

	// A rectangle has corners, not a beginning and an end, so dragging from
	// any of them gives the same thing.
	for _, corners := range []proto.PaneTextParams{
		{FromRow: 2, FromCol: 2, ToRow: 0, ToCol: 0, Block: true},
		{FromRow: 0, FromCol: 2, ToRow: 2, ToCol: 0, Block: true},
	} {
		if got := paneText(screen, corners); got != "AAA\nBBB\nCCC" {
			t.Errorf("from %+v = %q", corners, got)
		}
	}
}

// TestPaneTextReachesIntoTheScrollback is why this lives on the server: a drag
// that scrolled covers text the client is no longer showing.
func TestPaneTextReachesIntoTheScrollback(t *testing.T) {
	// Six rows of terminal and ten lines written, so four are in the history.
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line"+string(rune('0'+i%10)))
	}
	screen := textScreen(t, 20, 6, lines...)
	if got := screen.MainGrid().HistoryLen(); got < 4 {
		t.Fatalf("expected a history to read from, got %d lines", got)
	}

	// Row -2 at the live view is two lines above the top of the screen.
	above := proto.PaneTextParams{FromRow: -2, FromCol: 0, ToRow: -2, ToCol: 19}
	got := paneText(screen, above)
	if got == "" || !strings.HasPrefix(got, "line") {
		t.Errorf("a row above the view = %q, want a line of history", got)
	}

	// Asking for more than there is stops at what there is rather than
	// failing: a drag can be flung past the top of everything.
	all := proto.PaneTextParams{FromRow: -1000, FromCol: 0, ToRow: 1000, ToCol: 19}
	if n := strings.Count(paneText(screen, all), "\n") + 1; n > 6+screen.MainGrid().HistoryLen() {
		t.Errorf("clamping failed: %d lines", n)
	}

	// Both ends are inside the selection, so dragging one cell to the left of
	// where it started takes two cells, not none.
	line := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 0, ToCol: 19})
	if len(line) < 5 {
		t.Fatalf("expected a line to cut from, got %q", line)
	}
	if got := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 4, ToRow: 0, ToCol: 3}); got != line[3:5] {
		t.Errorf("one cell leftwards = %q, want %q", got, line[3:5])
	}
	if got := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 2, ToRow: 0, ToCol: 2}); got != line[2:3] {
		t.Errorf("a single cell = %q, want %q", got, line[2:3])
	}
}

// TestPaneTextReadsTheScreenThatIsShowing is a bug this had: a full-screen
// program is on the alternate screen, and reading the main grid there returns
// the shell it was started from — text the user is not looking at and did not
// select.
func TestPaneTextReadsTheScreenThatIsShowing(t *testing.T) {
	screen := textScreen(t, 20, 4, "SHELL-A", "SHELL-B")

	// Switch to the alternate screen and put different text on it.
	if _, err := screen.Write([]byte("\x1b[?1049h")); err != nil {
		t.Fatal(err)
	}
	if _, err := screen.Write([]byte("ALT-ONE\r\nALT-TWO\r\n")); err != nil {
		t.Fatal(err)
	}

	got := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 1, ToCol: 19})
	if !strings.Contains(got, "ALT-ONE") || strings.Contains(got, "SHELL-") {
		t.Errorf("read %q, want the alternate screen", got)
	}

	// And it has no history to reach into: that is what the alternate screen
	// is for. A row above it clamps to the top rather than returning the
	// main screen's scrollback.
	above := paneText(screen, proto.PaneTextParams{FromRow: -5, FromCol: 0, ToRow: -5, ToCol: 19})
	if strings.Contains(above, "SHELL-") {
		t.Errorf("above the alternate screen = %q, want nothing from the main one", above)
	}

	// Back on the main screen, the history is there again.
	if _, err := screen.Write([]byte("\x1b[?1049l")); err != nil {
		t.Fatal(err)
	}
	back := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 1, ToCol: 19})
	if strings.Contains(back, "ALT-") {
		t.Errorf("back on the main screen = %q, want the shell", back)
	}
}

// TestALineTheTerminalWrappedIsCopiedAsOneLine: a long command at a
// prompt, wider than the pane, is one line the terminal wrapped, and copied
// it must come back as the one line it was — not with a line break where
// the pane happened to end, which turns a command pasted into a shell into
// two commands. A line ended by the program (a newline) stays two lines.
func TestALineTheTerminalWrappedIsCopiedAsOneLine(t *testing.T) {
	screen := vt.NewScreen(20, 4, 0)
	long := "git tag -fa v1.1.0 && git push --force github v1.1.0"
	_, _ = screen.Write([]byte(long + "\r\nnext line"))
	got := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 3, ToCol: 19})
	if want := long + "\nnext line"; got != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
}

// screenOf is a terminal of the width given, each line its own row.
func screenOf(cols int, lines ...string) *vt.Screen {
	s := vt.NewScreen(cols, len(lines)+1, 0)
	_, _ = s.Write([]byte(strings.Join(lines, "\r\n")))
	return s
}

// TestALineAnAgentWrappedIsCopiedAsOneLine is the owner's: Claude Code
// gave a command longer than the pane and drew it as two rows, the second
// indented under the text — rows as the real Claude Code 2.1.325 drew them
// in an 82-column pane. Copied, it came back broken where the pane ended,
// "… && git\n  push …", which pasted into a shell is two commands. If it
// regresses, every long command an agent gives has to be mended by hand.
func TestALineAnAgentWrappedIsCopiedAsOneLine(t *testing.T) {
	row0 := "● cd /mnt/sdb1/Document/Desenvolvimentos/RetroStudioDev/retro-studio-cenas && git"
	row1 := `  tag -fa v1.1.0 -m "Retro Studio 1.1.0" && git push --force github v1.1.0`
	screen := screenOf(82, row0, row1)
	got := paneText(screen, proto.PaneTextParams{FromRow: 0, FromCol: 2, ToRow: 1, ToCol: 81})
	want := `cd /mnt/sdb1/Document/Desenvolvimentos/RetroStudioDev/retro-studio-cenas && git tag -fa v1.1.0 -m "Retro Studio 1.1.0" && git push --force github v1.1.0`
	if got != want {
		t.Fatalf("copied %q\nwant   %q", got, want)
	}
	// A third row of the same paragraph joins too.
	three := screenOf(30, "● one two three four five six", "  seven eight nine ten eleven", "  twelve")
	if got := paneText(three, proto.PaneTextParams{FromRow: 0, FromCol: 2, ToRow: 2, ToCol: 29}); got != "one two three four five six seven eight nine ten eleven twelve" {
		t.Fatalf("three rows: %q", got)
	}
}

// TestLinesAProgramEndedStayLines: what the rule must not join — a short
// line, a line with no indent after a long one, a new paragraph, a code
// block indented deeper, and a block selection.
func TestLinesAProgramEndedStayLines(t *testing.T) {
	for name, c := range map[string]struct {
		cols  int
		lines []string
	}{
		"short":         {40, []string{"● done", "  next"}},
		"no indent":     {30, []string{"a long log line that fills it", "another line"}},
		"new paragraph": {30, []string{"● one two three four five six", "● seven"}},
		"code block":    {30, []string{"● one two three four five six", "    seven"}},
	} {
		s := screenOf(c.cols, c.lines...)
		got := paneText(s, proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: len(c.lines) - 1, ToCol: c.cols - 1})
		if !strings.Contains(got, "\n") {
			t.Errorf("%s: joined into %q", name, got)
		}
	}
	block := screenOf(30, "● one two three four five six", "  seven")
	if got := paneText(block, proto.PaneTextParams{FromRow: 0, FromCol: 0, ToRow: 1, ToCol: 29, Block: true}); !strings.Contains(got, "\n") {
		t.Errorf("a block selection was joined: %q", got)
	}
}
