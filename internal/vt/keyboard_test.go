package vt

import "testing"

// TestAProgramGetsTheKittyKeyboardItAsksFor is Claude Code's own start, as
// recorded in a pty (issue #66): it asks CSI ? u, and only when answered
// pushes its flags and turns modifyOtherKeys on; it pops them on its way
// out. If it regresses, Claude Code inside tend never turns the protocol on,
// and Shift+Enter sends the message instead of breaking the line.
func TestAProgramGetsTheKittyKeyboardItAsksFor(t *testing.T) {
	s := NewScreen(20, 4, 0)
	var replies []string
	s.Reply = func(b []byte) { replies = append(replies, string(b)) }

	_, _ = s.Write([]byte("\x1b[?u"))
	if len(replies) != 1 || replies[0] != "\x1b[?0u" {
		t.Fatalf("the query was answered %q, want CSI ? 0 u", replies)
	}
	_, _ = s.Write([]byte("\x1b[>5u\x1b[>4;2m"))
	if m := s.Modes(); m.KittyKeyboard != 5 || m.ModifyOtherKeys != 2 {
		t.Fatalf("after the push: flags %d, modifyOtherKeys %d", m.KittyKeyboard, m.ModifyOtherKeys)
	}
	_, _ = s.Write([]byte("\x1b[?u"))
	if replies[len(replies)-1] != "\x1b[?5u" {
		t.Fatalf("the query after the push said %q", replies[len(replies)-1])
	}
	_, _ = s.Write([]byte("\x1b[>1u\x1b[<u"))
	if s.Modes().KittyKeyboard != 5 {
		t.Fatalf("a push and a pop left %d", s.Modes().KittyKeyboard)
	}
	_, _ = s.Write([]byte("\x1b[<1u\x1b[>4m"))
	if m := s.Modes(); m.KittyKeyboard != 0 || m.ModifyOtherKeys != 0 {
		t.Fatalf("after leaving: flags %d, modifyOtherKeys %d", m.KittyKeyboard, m.ModifyOtherKeys)
	}
	_, _ = s.Write([]byte("\x1b[=3u\x1b[=4;2u\x1b[=1;3u"))
	if s.Modes().KittyKeyboard != 6 {
		t.Fatalf("set 3, or 4, and-not 1 is %d, want 6", s.Modes().KittyKeyboard)
	}
	_, _ = s.Write([]byte("\x1b[>9u\x1bc"))
	if s.Modes().KittyKeyboard != 0 {
		t.Fatal("RIS left the flags")
	}
	// CSI u with no marker is still restore-cursor.
	_, _ = s.Write([]byte("ab\x1b[sxy\x1b[uZ"))
	if got := s.Grid().Line(0).Cell(2).R; got != 'Z' {
		t.Fatalf("restore cursor broke: %q at column 2", got)
	}
}
