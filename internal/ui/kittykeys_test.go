package ui

import "testing"

// TestThePrefixWorksInTheKittyKeyboardsForm: with a pane's protocol on in
// the terminal outside, ctrl+b arrives as CSI 98;5u. It must still arm the
// prefix, and the key after it be the command; Shift+Enter, not tend's,
// must reach the pane in the form it came. If it regresses, tend's keys
// stop working whenever Claude Code is focused in kitty or Ghostty — or the
// pane gets Shift+Enter as a bare Enter, and sends the message.
func TestThePrefixWorksInTheKittyKeyboardsForm(t *testing.T) {
	var in Input
	forward, cmds, _ := in.FeedAll([]byte("\x1b[98;5u|"))
	if len(forward) != 0 || len(cmds) != 1 || cmds[0].Command != CommandSplitColumns {
		t.Fatalf("prefix then |: forward %q, commands %v", forward, cmds)
	}
	forward, cmds, _ = in.FeedAll([]byte("hi\x1b[13;2u"))
	if string(forward) != "hi\x1b[13;2u" || len(cmds) != 0 {
		t.Fatalf("Shift+Enter: forward %q, commands %v", forward, cmds)
	}
	// The key after the prefix in the protocol's form too: Escape.
	forward, cmds, _ = in.FeedAll([]byte("\x1b[98;5u\x1b[27u"))
	if len(cmds) != 0 || len(forward) != 0 {
		t.Fatalf("prefix then Escape: forward %q, commands %v", forward, cmds)
	}
}

// TestKittyKeysReadAsTheBytesATerminalWithoutItSends, for tend's panels.
func TestKittyKeysReadAsTheBytesATerminalWithoutItSends(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[27u":      "\x1b",
		"\x1b[13u":      "\r",
		"\x1b[13;2u":    "\r",
		"\x1b[9;2u":     "\x1b[Z",
		"\x1b[99;5u":    "\x03",
		"\x1b[120;3u":   "\x1bx",
		"\x1b[97;2u":    "A",
		"\x1b[97:65;2u": "A",
		"\x1b[97;1:3u":  "", // a release
		"plain\x1b[A":   "plain\x1b[A",
	} {
		if got := string(LegacyKeys([]byte(in))); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
