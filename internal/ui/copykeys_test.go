package ui

import "testing"

// TestCopyModesKeysCanBeChanged: with nothing set the keys are herdr's —
// vi's, arrows and pages too; an action the settings name has their keys in
// place of its own; a key they give one action is taken from the one that
// had it, and said; a name that is no action, or no key, is refused. If it
// regresses, copy mode is vi's or nothing, which herdr's users asked to
// change (discussion #587).
func TestCopyModesKeysCanBeChanged(t *testing.T) {
	def, notes, err := CopyKeysFrom(nil)
	if err != nil || len(notes) != 0 {
		t.Fatal(err, notes)
	}
	for key, want := range map[string]string{"h": "left", "\x1b[D": "left", "\x15": "half-page-up", "\x1b[5~": "page-up", "\x1b": "cancel", "\r": "yank", "/": "search-forward"} {
		if def[key] != want {
			t.Errorf("default %q = %q, want %q", key, def[key], want)
		}
	}

	got, notes, err := CopyKeysFrom(map[string]string{"left": "a left", "yank": "y ctrl+w enter", "exit": "q h j"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "left" || got["\x1b[D"] != "left" || got["\x17"] != "yank" {
		t.Errorf("the keys given: %v", got)
	}
	if got["h"] != "exit" {
		t.Errorf("h was given to exit: %q", got["h"])
	}
	if got["k"] != "up" {
		t.Error("an action not named keeps its keys")
	}
	if got["j"] != "exit" || len(notes) != 1 || notes[0] != "j was down, now exit" {
		t.Errorf("notes = %q", notes)
	}

	if _, _, err := CopyKeysFrom(map[string]string{"teleport": "t"}); err == nil {
		t.Error("an action that does not exist should be refused")
	}
	if _, _, err := CopyKeysFrom(map[string]string{"left": "ctrl+left"}); err == nil {
		t.Error("a key copy mode cannot read should be refused")
	}
}
