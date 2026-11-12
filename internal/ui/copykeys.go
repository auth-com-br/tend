package ui

import (
	"fmt"
	"sort"
	"strings"
)

// Copy mode's keys, as a table the settings can change ([keys.copy_mode]:
// herdr discussion #587, "configurable keymaps in copy mode", for those
// whose hands are not vi's). The defaults are herdr's, which are vi's and
// tmux's. Each action is named; a key is written as the settings write
// keys — "h", "ctrl+u", "left", "pgup".

// CopyActions are the actions copy mode has, in the order help lists them.
var CopyActions = []string{
	"exit", "cancel", "yank", "select", "select-line",
	"left", "right", "down", "up",
	"page-up", "page-down", "half-page-up", "half-page-down",
	"top", "bottom", "line-start", "line-end", "first-non-blank",
	"word-next", "word-back", "word-end", "bigword-next", "bigword-back", "bigword-end",
	"paragraph-back", "paragraph-next",
	"search-forward", "search-backward", "search-next", "search-previous",
}

// defaultCopyKeys is every action's keys out of the box.
var defaultCopyKeys = map[string][]string{
	"exit": {"q", "ctrl+c"}, "cancel": {"esc"}, "yank": {"y", "enter"},
	"select": {"v", "space"}, "select-line": {"V"},
	"left": {"h", "left"}, "right": {"l", "right"}, "down": {"j", "down"}, "up": {"k", "up"},
	"page-up": {"pgup", "ctrl+b"}, "page-down": {"pgdown", "ctrl+f"},
	"half-page-up": {"ctrl+u"}, "half-page-down": {"ctrl+d"},
	"top": {"g"}, "bottom": {"G"}, "line-start": {"0", "home"}, "line-end": {"$", "end"},
	"first-non-blank": {"^"},
	"word-next":       {"w"}, "word-back": {"b"}, "word-end": {"e"},
	"bigword-next": {"W"}, "bigword-back": {"B"}, "bigword-end": {"E"},
	"paragraph-back": {"{"}, "paragraph-next": {"}"},
	"search-forward": {"/"}, "search-backward": {"?"}, "search-next": {"n"}, "search-previous": {"N"},
}

// copyKeyBytes is what a key name is as the terminal sends it.
func copyKeyBytes(name string) (string, bool) {
	switch name {
	case "enter":
		return "\r", true
	case "space":
		return " ", true
	case "esc":
		return "\x1b", true
	case "tab":
		return "\t", true
	case "backspace":
		return "\x7f", true
	case "left":
		return "\x1b[D", true
	case "right":
		return "\x1b[C", true
	case "down":
		return "\x1b[B", true
	case "up":
		return "\x1b[A", true
	case "pgup":
		return "\x1b[5~", true
	case "pgdown":
		return "\x1b[6~", true
	case "home":
		return "\x1b[H", true
	case "end":
		return "\x1b[F", true
	}
	if letter, ok := strings.CutPrefix(name, "ctrl+"); ok && len(letter) == 1 {
		c := letter[0] | 0x20 // ctrl+B is ctrl+b
		if c >= 'a' && c <= 'z' {
			return string(rune(c - 'a' + 1)), true
		}
	}
	if r := []rune(name); len(r) == 1 && r[0] >= 0x21 && r[0] <= 0x7e {
		return name, true
	}
	return "", false
}

// CopyKeyNames is each action's keys by name, in CopyActions' order, with
// pairs as CopyKeysFrom reads them: what `tend keys` lists.
func CopyKeyNames(pairs map[string]string) ([][2]string, error) {
	table, _, err := CopyKeysFrom(pairs)
	if err != nil {
		return nil, err
	}
	names := map[string][]string{}
	for _, action := range CopyActions {
		ks := defaultCopyKeys[action]
		if given, ok := pairs[action]; ok {
			ks = strings.Fields(given)
		}
		for _, k := range ks {
			if b, _ := copyKeyBytes(k); table[b] == action {
				names[action] = append(names[action], k)
			}
		}
	}
	out := make([][2]string, 0, len(CopyActions))
	for _, action := range CopyActions {
		out = append(out, [2]string{action, strings.Join(names[action], " ")})
	}
	return out, nil
}

// CopyKeysFrom is copy mode's table: what the terminal sends, to the action
// it does. An action the settings name has the keys they give it, written
// "h left" for two, in place of its own; a key they give one action is taken
// from whichever had it by default, and said in the notes.
func CopyKeysFrom(pairs map[string]string) (map[string]string, []string, error) {
	known := make(map[string]bool, len(CopyActions))
	for _, a := range CopyActions {
		known[a] = true
	}
	keys := make(map[string][]string, len(defaultCopyKeys))
	for a, ks := range defaultCopyKeys {
		keys[a] = ks
	}
	names := make([]string, 0, len(pairs))
	for name := range pairs {
		names = append(names, name)
	}
	sort.Strings(names)
	claimed := map[string]string{}
	for _, action := range names {
		if !known[action] {
			return nil, nil, fmt.Errorf("keys.copy_mode: no action called %q (they are %s)", action, strings.Join(CopyActions, ", "))
		}
		fields := strings.Fields(pairs[action])
		for _, k := range fields {
			if _, ok := copyKeyBytes(k); !ok {
				return nil, nil, fmt.Errorf("keys.copy_mode.%s: %q is not a key copy mode can read", action, k)
			}
			claimed[k] = action
		}
		keys[action] = fields
	}
	out := map[string]string{}
	var notes []string
	for _, action := range CopyActions {
		for _, k := range keys[action] {
			if owner, ok := claimed[k]; ok && owner != action {
				notes = append(notes, k+" was "+action+", now "+owner)
				continue
			}
			b, _ := copyKeyBytes(k)
			out[b] = action
		}
	}
	return out, notes, nil
}
