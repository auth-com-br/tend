package explorer

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// reviewingOpener is an opener in a tend pane with an agent beside it.
type reviewingOpener struct {
	recordingOpener
	prefer, sent []string
}

func (o *reviewingOpener) Review(prefer, text string) (string, error) {
	o.prefer = append(o.prefer, prefer)
	o.sent = append(o.sent, text)
	return "claude", nil
}

// TestADiffLineIsCountedInTheNewFile: the line a review is about is the new
// file's, counted from the hunk header above the viewer's top row, removed
// lines not counted. If it regresses, the agent is told to look at the wrong
// line.
func TestADiffLineIsCountedInTheNewFile(t *testing.T) {
	lines := []string{
		"diff --git a/x b/x",
		"@@ -10,4 +12,5 @@ func f() {",
		" kept",   // 12
		"-gone",   // not in the new file
		"+added",  // 13
		" kept 2", // 14
	}
	for top, want := range map[int]int{0: 12, 1: 12, 2: 12, 3: 12, 4: 13, 5: 14, 99: 14} {
		if got := diffLine(lines, top); got != want {
			t.Errorf("top %d: line %d, want %d", top, got, want)
		}
	}
	if hunkNewStart("@@ -1 +7 @@") != 7 || hunkNewStart("not a hunk") != 0 {
		t.Error("hunk header without counts")
	}
}

// TestAReviewOfADiffIsSentToTheAgentBeside: r in a diff asks for a comment
// and sends it, on one line, about the file and the line on screen, to the
// agent the panel follows; esc leaves the comment unsent and the diff open;
// outside tend it says why it cannot. If it regresses, reviewing an agent's
// work and telling it what to change are two places again.
func TestAReviewOfADiffIsSentToTheAgentBeside(t *testing.T) {
	dir := repo(t)
	op := &reviewingOpener{}
	m := New(dir, op)
	m.follow.selected = "w1:p2"
	keys(m, "3")
	screen(m, 60, 14)
	for i, r := range m.changeRows {
		if r.change.Path == "src/changed.go" {
			m.cursor[ViewChanges] = i
		}
	}
	keys(m, "enter")
	// The row on screen is the added line (a short diff does not scroll, so
	// it is put there directly).
	want := 0
	for i, l := range m.viewer.lines {
		if strings.HasPrefix(l, "+var") {
			m.viewer.top, want = i, diffLine(m.viewer.lines, i)
		}
	}
	if want != 3 {
		t.Fatalf("+var x = 1 is line %d of the new file, want 3:\n%s", want, strings.Join(m.viewer.lines, "\n"))
	}

	keys(m, "r")
	if text := screen(m, 60, 14); !strings.Contains(text, "review of src/changed.go") {
		t.Fatalf("the question:\n%s", text)
	}
	keys(m, "esc")
	if m.mode != modeViewer || len(op.sent) != 0 {
		t.Fatalf("esc should go back to the diff, sending nothing (mode %v, sent %q)", m.mode, op.sent)
	}

	m.viewer.top = 0 // the viewer put it back on the header
	for i, l := range m.viewer.lines {
		if strings.HasPrefix(l, "+var") {
			m.viewer.top = i
		}
	}
	keys(m, "r")
	typeText(m, "name it  answer")
	keys(m, "enter")
	if len(op.sent) != 1 {
		t.Fatalf("sent %q (panel says %q)", op.sent, m.message)
	}
	if op.sent[0] != "Review of src/changed.go around line 3: name it answer" {
		t.Errorf("the agent got %q", op.sent[0])
	}
	if op.prefer[0] != "w1:p2" {
		t.Errorf("sent preferring %q, want the followed pane", op.prefer[0])
	}
	if m.mode != modeViewer || !strings.Contains(m.message, "sent to claude") {
		t.Errorf("after sending: mode %v, %q", m.mode, m.message)
	}

	plain := New(dir, &recordingOpener{})
	keys(plain, "3")
	screen(plain, 60, 14)
	keys(plain, "enter", "r")
	if !plain.messageErr || !strings.Contains(plain.message, "no agent") {
		t.Errorf("outside tend: %q", plain.message)
	}
}

// TestAReviewGoesToAnAgentInThePanelsTab: the session opener sends the
// review, submitted, to the followed pane when it is an agent in the
// panel's tab, else to the tab's first agent — never to a pane in another
// tab, nor to the panel itself; with no agent beside, it says so and sends
// nothing. If it regresses, a review is typed into a shell, or into an agent
// working on something else.
func TestAReviewGoesToAnAgentInThePanelsTab(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	panes := []map[string]any{
		{"pane_id": "p_1", "tab_id": "t_1"},                    // the panel
		{"pane_id": "p_2", "tab_id": "t_1"},                    // a shell
		{"pane_id": "p_3", "tab_id": "t_1", "agent": "codex"},  // an agent
		{"pane_id": "p_4", "tab_id": "t_1", "agent": "claude"}, // another
		{"pane_id": "p_9", "tab_id": "t_2", "agent": "claude"}, // elsewhere
	}
	var mu sync.Mutex
	var sent []map[string]any
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			var c struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			_ = json.Unmarshal(line, &c)
			var result any = map[string]any{}
			mu.Lock()
			switch c.Method {
			case "pane.list":
				result = map[string]any{"panes": panes}
			case "pane.send_text":
				sent = append(sent, c.Params)
			}
			mu.Unlock()
			reply, _ := json.Marshal(map[string]any{"id": "x", "result": result})
			_, _ = conn.Write(append(reply, '\n'))
			conn.Close()
		}
	}()
	last := func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		if len(sent) == 0 {
			return nil
		}
		return sent[len(sent)-1]
	}

	o := &SessionOpener{Socket: sock, Pane: "p_1"}
	if agent, err := o.Review("p_4", "Review of a.go: x"); err != nil || agent != "claude" {
		t.Fatalf("to the followed agent: %q %v", agent, err)
	}
	if p := last(); p["pane_id"] != "p_4" || p["text"] != "Review of a.go: x" || p["submit"] != true {
		t.Errorf("sent %+v", p)
	}
	for _, prefer := range []string{"", "p_2", "p_9"} {
		if agent, err := o.Review(prefer, "r"); err != nil || agent != "codex" || last()["pane_id"] != "p_3" {
			t.Errorf("preferring %q: to %v (%q, %v)", prefer, last()["pane_id"], agent, err)
		}
	}

	mu.Lock()
	panes = panes[:2]
	n := len(sent)
	mu.Unlock()
	if _, err := o.Review("", "r"); err == nil || !strings.Contains(err.Error(), "no agent") {
		t.Errorf("no agent beside: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != n {
		t.Error("sent with no agent beside")
	}
}
