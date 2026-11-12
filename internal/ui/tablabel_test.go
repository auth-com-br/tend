package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestATabCanSayItsNumberAndItsAgentsState: with tab_numbers a named tab
// reads "2 build" and an unnamed one its place rather than its id; with
// tab_status the most pressing agent's state goes first in symbols — ×
// waiting, ✓ finished unseen, ◐ working — and an idle one says nothing;
// with neither, the label is herdr's. If it regresses, someone who keeps
// the sidebar hidden cannot see which tab is waiting, or which number
// alt+N goes to.
func TestATabCanSayItsNumberAndItsAgentsState(t *testing.T) {
	tabs := []Tab{
		{ID: 7, Name: "build", Number: 1, State: "blocked", Active: true},
		{ID: 9, Number: 2, State: "done"},
		{ID: 12, Name: "logs", Number: 3, State: "working"},
		{ID: 13, Name: "shell", Number: 4, State: "idle"},
	}
	bar := func(f Frame) string {
		f.Tabs = tabs
		dst := vt.NewGrid(120, 6, 0)
		Draw(dst, f, DefaultTheme())
		return gridText(dst)[0]
	}
	plain := bar(Frame{})
	for _, want := range []string{"build", " 9 ", "logs", "shell"} {
		if !strings.Contains(plain, want) {
			t.Errorf("herdr's labels: %q lacks %q", plain, want)
		}
	}
	if strings.ContainsAny(plain, "×✓◐") || strings.Contains(plain, "1 build") {
		t.Errorf("nothing asked for, something added: %q", plain)
	}
	both := bar(Frame{TabNumbers: true, TabStatus: true})
	for _, want := range []string{"× 1 build", "✓ 2 ", "◐ 3 logs", " 4 shell"} {
		if !strings.Contains(both, want) {
			t.Errorf("%q lacks %q", both, want)
		}
	}
	if strings.Contains(both, " 9 ") || strings.Contains(both, "○") {
		t.Errorf("an unnamed tab is its number, an idle one has no mark: %q", both)
	}
}

// TestThePrimarySelectionSequenceNamesIt: OSC 52 with target "p", which
// the terminal in front of the user sets when there is no local tool —
// over ssh — and the clipboard's own "c" untouched by it.
func TestThePrimarySelectionSequenceNamesIt(t *testing.T) {
	if got := SetPrimary("hi"); got != "\x1b]52;p;aGk=\a" {
		t.Errorf("SetPrimary = %q", got)
	}
	if SetPrimary("") != "" {
		t.Error("nothing selected, nothing sent")
	}
}
