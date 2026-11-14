//go:build unix

package main

import (
	"strings"
	"testing"
)

// TestSynchronisedInputTypesIntoEveryPaneOfTheTab: prefix+y, a command
// typed once runs in both panes, SYNC says so, and prefix+y again types
// into the focused pane alone. If it regresses, a fleet of panes is driven
// one at a time again — or, worse, keys keep going to every pane after the
// person thought they had stopped.
func TestSynchronisedInputTypesIntoEveryPaneOfTheTab(t *testing.T) {
	a := startSession(t, 100, 20)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02|")
	a.waitForScreen(t, "two panes", func(s string) bool { return strings.Count(s, "┌") == 2 })
	a.sendUntil(t, "printf 'READY\\n'\n", "the new pane's shell", func(s string) bool {
		return strings.Contains(s, "READY")
	})

	a.send(t, "\x02y")
	a.waitForScreen(t, "the SYNC mark", func(s string) bool { return strings.Contains(s, " SYNC ") })
	// The output, not the echo: 'BOTH-%s' is what was typed, BOTH-2 only
	// what a shell that ran it prints.
	a.send(t, "printf 'BOTH-%s\\n' $((1+1))\n")
	a.waitForScreen(t, "the command run in both panes", func(s string) bool {
		return strings.Count(s, "BOTH-2") == 2
	})

	a.send(t, "\x02y")
	a.waitForScreen(t, "SYNC gone", func(s string) bool { return !strings.Contains(s, " SYNC ") })
	a.send(t, "printf 'ONE-%s\\n' $((2+1))\n")
	a.waitForScreen(t, "the command run in one pane", func(s string) bool {
		return strings.Count(s, "ONE-3") == 1
	})
	// A later command seen run means the earlier one had its time to run
	// anywhere it was sent.
	a.send(t, "printf 'LATER-%s\\n' x\n")
	a.waitForScreen(t, "a later command", func(s string) bool { return strings.Contains(s, "LATER-x") })
	if n := strings.Count(a.text(), "ONE-3"); n != 1 {
		t.Fatalf("after turning it off the command ran in %d panes", n)
	}
}
