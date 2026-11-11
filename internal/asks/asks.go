// Package asks reads, from the screen of an agent stopped for the user, what
// it is asking — for a notification to say it, on the TUI's card and on the
// phone.
package asks

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxRunes is how long Read's line may be: a notification is read at
// a glance, and a terminal's or a desktop's card cuts what is longer anyway.
const MaxRunes = 120

// Read reads, from the screen of an agent that stopped for the user,
// what it is asking — "Bash command: touch notes.txt — Do you want to
// proceed?" — so a notification can say it rather than only that the agent
// needs attention, which is what made people go and look at every one. It
// returns "" when the screen does not read as a question.
//
// Agents draw their questions differently, so this reads the shape they
// share rather than any one agent's words: the question is the last line
// that asks something (a "?" at its end or before more text), or, for a
// prompt that asks with its options alone, the line before the first option;
// what it is about is the block above it, up to the rule that opens it. The
// title of that block and its last line are kept — an agent's own heading and
// the command or the reason — and its options, key hints, tips and rules are
// not.
func Read(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	q, question := -1, ""
	for i := len(lines) - 1; i >= 0; i-- {
		if text, ok := asked(cleanLine(lines[i])); ok {
			q, question = i, text
			break
		}
	}
	if q < 0 {
		// No question mark: a prompt that asks with its options alone.
		for i, line := range lines {
			if optionLine.MatchString(line) {
				q = i
				break
			}
		}
		if q < 0 {
			return ""
		}
	}
	// The block the question belongs to: up from it to the rule (or box top)
	// that opens it.
	var block []string
	for i := q - 1; i >= 0 && i >= q-12; i-- {
		raw := lines[i]
		if isRule(raw) || strings.HasPrefix(strings.TrimSpace(raw), "╭") {
			break
		}
		if optionLine.MatchString(raw) {
			continue
		}
		text := cleanLine(raw)
		if text == "" || isDashed(raw) || isHint(text) {
			continue
		}
		block = append([]string{text}, block...)
	}
	var parts []string
	if len(block) > 0 {
		about := block[0]
		if len(block) > 1 {
			about = strings.TrimSuffix(about, ":") + ": " + block[len(block)-1]
		}
		parts = append(parts, about)
	}
	if question != "" {
		parts = append(parts, question)
	}
	if len(parts) == 0 {
		return ""
	}
	return clip(strings.Join(parts, " — "), MaxRunes)
}

// optionLine is one answer of a numbered choice: "❯ 1. Yes", "  2. No",
// "› 1. Allow once".
var optionLine = regexp.MustCompile(`^\s*[│┃]?\s*[❯›>▸●○]?\s*\d+[.)]\s+\S`)

// asked reports whether a line asks something, and the question as far as
// its "?": "…one you trust? (Like your own code, a" asks "…one you trust?".
func asked(text string) (string, bool) {
	if strings.HasSuffix(text, "?") {
		return text, true
	}
	if i := strings.Index(text, "? "); i > 0 {
		return text[:i+1], true
	}
	return "", false
}

// cleanLine is a screen line without a box's sides, a cursor or a spinner,
// and with its spaces collapsed.
func cleanLine(line string) string {
	line = strings.Trim(line, " │┃╎║")
	line = strings.TrimLeft(line, "❯›>▸●○▲△⚠✱✻* ")
	return strings.Join(strings.Fields(line), " ")
}

// isRule is a line that is a horizontal rule, or mostly one with a label in
// it ("─ Working 41s ──── esc interrupt ─"): what opens a prompt's block.
func isRule(line string) bool {
	n, rule := 0, 0
	for _, r := range line {
		if r == ' ' {
			continue
		}
		n++
		if strings.ContainsRune("─━═▔▁", r) {
			rule++
		}
	}
	return rule >= 10 && rule*2 >= n
}

// isDashed is a dashed rule inside a prompt, around a command: not a line of
// its text, and not the end of its block either.
func isDashed(line string) bool {
	t := strings.TrimSpace(line)
	return t != "" && strings.Trim(t, "╌┄┈-·") == ""
}

// isHint is a line about keys or a tip, not about what is asked.
func isHint(text string) bool {
	low := strings.ToLower(text)
	for _, p := range []string{"tip:", "esc to", "enter to", "↑↓", "press "} {
		if strings.HasPrefix(low, p) || strings.Contains(low, " · "+p) {
			return true
		}
	}
	return false
}

// clip cuts text to n runes, with an ellipsis when it was longer.
func clip(text string, n int) string {
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	r := []rune(text)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
