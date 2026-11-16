//go:build unix

package main

import (
	"strings"
	"testing"
	"time"
)

// TestAPanesKeyboardProtocolIsSetOutsideAndItsKeysPassThrough is issue
// #66's chain with a terminal that has the protocol: a program asks the
// pane's terminal for it (as Claude Code does), the client sets the same
// flags on the terminal outside while the pane is focused, a Shift+Enter in
// the protocol's form reaches the program as it came, the prefix in that
// form still works, and the flags are taken back on the way out. (Checked
// end to end with the real Claude Code, whose input box took the
// Shift+Enter as a new line.) If it regresses, Shift+Enter inside tend
// sends the message in kitty and Ghostty, or tend's keys stop there.
func TestAPanesKeyboardProtocolIsSetOutsideAndItsKeysPassThrough(t *testing.T) {
	a := startSessionEnv(t, 100, 20, "TEND_KITTY_KEYBOARD=on", "TERM=xterm-kitty")
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	// A program that turns the protocol on as Claude Code does once its
	// query is answered (the query itself: internal/vt's test), then shows
	// the bytes it is given.
	a.send(t, "printf '\\033[>5u\\033[>4;2m'; echo READY-$((1+1)); od -An -c\n")
	a.waitForScreen(t, "the program ready", func(s string) bool { return strings.Contains(s, "READY-2") })
	a.waitForRaw(t, "the flags set outside", func(s string) bool { return strings.Contains(s, "\x1b[>5u") && strings.Contains(s, "\x1b[>4;2m") })

	// The terminal's echo of it: as it came, ESC [ 13 ; 2 u — in the legacy
	// form it would have been a line ending, and nothing to see.
	a.send(t, "\x1b[13;2u")
	a.waitForScreen(t, "Shift+Enter as it came", func(s string) bool { return strings.Contains(s, "^[[13;2u") })
	a.send(t, "\n\x04")

	a.send(t, "printf '\\033[<u\\033[>4m'; echo LEFT\n")
	a.waitForRaw(t, "the flags taken back when the program leaves", func(s string) bool { return strings.Contains(s, "\x1b[<1u") })

	// The prefix in the protocol's form is still the prefix.
	a.send(t, "printf '\\033[>1u'; echo AGAIN-$((2+2))\n")
	a.waitForScreen(t, "the flags on again", func(s string) bool { return strings.Contains(s, "AGAIN-4") })
	a.waitForRaw(t, "them outside", func(s string) bool { return strings.Count(s, "\x1b[>1u") > 0 })
	a.send(t, "\x1b[98;5u|")
	a.waitForScreen(t, "the split the prefix asked for", func(s string) bool { return strings.Count(s, "┌") == 2 })
}

// waitForRaw waits for the bytes the client wrote to its terminal.
func (a *attached) waitForRaw(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond(a.raw()) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s in what the client wrote", what)
}
