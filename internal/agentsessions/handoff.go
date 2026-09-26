package agentsessions

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Handoff is what a conversation says about the task it was on: enough for
// another agent to carry it on (docs/MEMORY.md, #27). It is read from the
// conversation itself, without asking a model to sum it up: what the user
// asked, which files the agent wrote to, and what it said last.
type Handoff struct {
	ID, Title string
	// Dir is where the conversation was held, and Path its file, which the
	// next agent may read for more.
	Dir, Path string
	// First is the first request, Recent the last ones after it, oldest
	// first.
	First  string
	Recent []string
	// Files are the files the agent wrote to, each once, in the order it
	// last wrote to them; relative to Dir when under it.
	Files []string
	// Last is what the agent said last.
	Last string
}

// How much of a conversation a handoff carries: enough to go on with, not
// the conversation again.
const (
	handoffRecent   = 5
	handoffFiles    = 30
	handoffPrompt   = 400
	handoffLastWord = 2400
)

// editTools are Claude Code's tools that write to a file, and the input
// that names it.
var editTools = map[string]string{
	"Edit": "file_path", "MultiEdit": "file_path", "Write": "file_path", "NotebookEdit": "notebook_path",
}

// Handoff reads a conversation for another agent to carry on.
func (c *Claude) Handoff(id string) (Handoff, error) {
	if !idPattern.MatchString(id) {
		return Handoff{}, fmt.Errorf("%w: %q", ErrNoSuchSession, id)
	}
	files, err := filepath.Glob(filepath.Join(c.root, "*", id+".jsonl"))
	if err != nil {
		return Handoff{}, err
	}
	if len(files) == 0 {
		return Handoff{}, fmt.Errorf("%w: %s", ErrNoSuchSession, id)
	}
	s, err := readClaude(files[0])
	if err != nil {
		return Handoff{}, err
	}
	h, err := readHandoff(files[0])
	if err != nil {
		return Handoff{}, err
	}
	h.ID, h.Title, h.Path = id, s.Title, files[0]
	if h.Dir == "" {
		h.Dir = s.Dir
	}
	// The project's files, relative to it. What the agent wrote elsewhere —
	// its scratch files, most often — is not the task's.
	if h.Dir != "" {
		kept := h.Files[:0]
		for _, f := range h.Files {
			if rel, err := filepath.Rel(h.Dir, f); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
				kept = append(kept, rel)
			}
		}
		h.Files = kept
	}
	return h, nil
}

// request is what the user asked in one of their lines: every text of it
// — a message with a screenshot has the picture's description as its first
// text, and what was asked after — without the pictures' descriptions, or
// "(an image)" when there was nothing but a picture. Lines that are not the
// user's own are skipped as the list skips them.
func request(l line) (string, bool) {
	if l.Sidechain || (l.Origin.Kind != "" && l.Origin.Kind != "human") {
		return "", false
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(l.Message, &msg) != nil {
		return "", false
	}
	var parts []string
	var plain string
	if json.Unmarshal(msg.Content, &plain) == nil {
		parts = []string{plain}
	} else {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(msg.Content, &blocks) != nil {
			return "", false
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parts = append(parts, b.Text)
			case "image":
				parts = append(parts, "[Image]")
			}
		}
	}
	var kept []string
	image := false
	for _, part := range parts {
		for _, ln := range strings.Split(part, "\n") {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "[Image") {
				image = true
				continue
			}
			if t != "" {
				kept = append(kept, t)
			}
		}
	}
	text := strings.Join(kept, " ")
	switch {
	case strings.HasPrefix(text, "<"):
		return "", false // a command's output, which Claude Code records as the user's
	case text == "" && image:
		return "(an image)", true
	case text == "":
		return "", false
	}
	return text, true
}

// readHandoff reads the requests, the files written and the last words.
// Only the user's and the agent's own lines are decoded; a tool's output,
// most of the bytes, is passed over by a look first.
func readHandoff(path string) (Handoff, error) {
	f, err := os.Open(path)
	if err != nil {
		return Handoff{}, err
	}
	defer f.Close()
	var h Handoff
	var prompts, written []string
	r := bufio.NewReaderSize(f, 1<<16)
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 && !bytes.Contains(raw, []byte(`"tool_use_id"`)) {
			switch {
			case bytes.Contains(raw, []byte(`"type":"user"`)):
				var l line
				if json.Unmarshal(raw, &l) == nil {
					if h.Dir == "" {
						h.Dir = l.Cwd
					}
					if text, ok := request(l); ok {
						prompts = append(prompts, text)
					}
				}
			case bytes.Contains(raw, []byte(`"type":"assistant"`)):
				readAssistant(raw, &h, &written)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return Handoff{}, err
		}
	}
	if len(prompts) > 0 {
		h.First = clip(prompts[0], handoffPrompt)
		rest := prompts[1:]
		if len(rest) > handoffRecent {
			rest = rest[len(rest)-handoffRecent:]
		}
		for _, p := range rest {
			h.Recent = append(h.Recent, clip(p, handoffPrompt))
		}
	}
	// Each file once, where it was last written; the last ones kept.
	seen := map[string]bool{}
	for i := len(written) - 1; i >= 0 && len(h.Files) < handoffFiles; i-- {
		if !seen[written[i]] {
			seen[written[i]] = true
			h.Files = append(h.Files, written[i])
		}
	}
	for i, j := 0, len(h.Files)-1; i < j; i, j = i+1, j-1 {
		h.Files[i], h.Files[j] = h.Files[j], h.Files[i]
	}
	h.Last = clipTail(h.Last, handoffLastWord)
	return h, nil
}

// readAssistant takes from one of the agent's lines what it said and the
// files it wrote to. A subagent's lines are its own, and not the task's.
func readAssistant(raw []byte, h *Handoff, written *[]string) {
	var l struct {
		Sidechain bool `json:"isSidechain"`
		Message   struct {
			Content []struct {
				Type  string                     `json:"type"`
				Text  string                     `json:"text"`
				Name  string                     `json:"name"`
				Input map[string]json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &l) != nil || l.Sidechain {
		return
	}
	for _, b := range l.Message.Content {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				h.Last = t
			}
		case "tool_use":
			key, ok := editTools[b.Name]
			if !ok {
				continue
			}
			var file string
			if json.Unmarshal(b.Input[key], &file) == nil && file != "" {
				*written = append(*written, file)
			}
		}
	}
}

// clip is text on one line, cut to n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// clipTail keeps the end of a long text, where an agent says where it
// stopped.
func clipTail(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return "…" + string(r[len(r)-n:])
	}
	return s
}
