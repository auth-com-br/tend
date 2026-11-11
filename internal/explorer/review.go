package explorer

import (
	"strconv"
	"strings"
)

// A review: reading an agent's diff in the changes view, r writes a comment
// about the part on screen and sends it to the agent working beside the
// panel, as a message it acts on — so reviewing what an agent did and
// telling it what to change happen in one place (issue #45).

// startReview asks for the comment, about the diff's file and the line the
// viewer is at, and sends it.
func (m *Model) startReview() {
	rev, ok := m.opener.(Reviewer)
	if !ok {
		m.say("no agent to send it to: not running in a tend pane", true)
		return
	}
	v := m.viewer
	about := v.rel
	if line := diffLine(v.lines, v.top); line > 0 {
		about += " around line " + strconv.Itoa(line)
	}
	m.ask("review of "+about+" — to the agent beside", "", func(m *Model, text string) {
		m.mode = modeViewer
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			return
		}
		agent, err := rev.Review(m.follow.selected, ReviewMessage(about, text))
		if err != nil {
			m.fail(err)
			return
		}
		m.say("sent to "+agent, false)
	})
	m.prompt.back = modeViewer
}

// ReviewMessage is what the agent is sent: one line — a line break would
// submit the message early in some agents' prompt boxes.
func ReviewMessage(about, comment string) string {
	return "Review of " + about + ": " + comment
}

// diffLine is the line of the new file the viewer's top row of a unified
// diff is at: counted from the nearest hunk header above it ("@@ -a,b +c,d
// @@"), skipping removed lines, which are not in the new file. Above the
// first hunk — the file header, where a diff short enough not to scroll
// always rests — it is where the first hunk starts. Zero with no hunk.
func diffLine(lines []string, top int) int {
	if top >= len(lines) {
		top = len(lines) - 1
	}
	for h := top; h >= 0; h-- {
		if !strings.HasPrefix(lines[h], "@@") {
			continue
		}
		start := hunkNewStart(lines[h])
		if start == 0 {
			return 0
		}
		n := start
		for _, l := range lines[h+1 : top+1] {
			if !strings.HasPrefix(l, "-") {
				n++
			}
		}
		if top > h {
			n-- // the row on screen is the line itself, not the one after it
		}
		return n
	}
	for _, l := range lines[top+1:] {
		if strings.HasPrefix(l, "@@") {
			return hunkNewStart(l)
		}
	}
	return 0
}

// hunkNewStart reads c from "@@ -a,b +c,d @@ …".
func hunkNewStart(header string) int {
	i := strings.Index(header, " +")
	if i < 0 {
		return 0
	}
	rest := header[i+2:]
	if j := strings.IndexAny(rest, ", "); j >= 0 {
		rest = rest[:j]
	}
	n, _ := strconv.Atoi(rest)
	return n
}
