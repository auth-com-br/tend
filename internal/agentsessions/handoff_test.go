package agentsessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAHandoffCarriesTheTask: from a conversation shaped as Claude Code
// writes it, the handoff has the title, the first request and the last
// five after it — a request with a screenshot read as what was typed, one
// with only a picture as "(an image)" — the project's files the agent
// wrote, each once in the order last written, and its last words; a
// subagent's lines, a tool's output and a file outside the project are
// left out. If it regresses, the next agent is handed a picture's size
// instead of the request, or the agent's scratch files as the task's.
func TestAHandoffCarriesTheTask(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "projects", "-work-shop")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	user := func(content string) string {
		return `{"type":"user","cwd":"/work/shop","message":{"role":"user","content":` + content + `}}`
	}
	edit := func(tool, key, file string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"` + tool + `","input":{"` + key + `":"` + file + `"}}]}}`
	}
	say := func(text string) string {
		return `{"type":"assistant","message":{"content":[{"type":"text","text":"` + text + `"}]}}`
	}
	lines := []string{
		`{"type":"ai-title","aiTitle":"Checkout redesign"}`,
		user(`"fix the checkout button"`),
		edit("Edit", "file_path", "/work/shop/src/button.tsx"),
		edit("Write", "file_path", "/tmp/scratch/notes.md"),
		user(`[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]`),
		`{"type":"user","isSidechain":true,"message":{"content":"a subagent's prompt"}}`,
		`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"a subagent's words"}]}}`,
		user(`"one"`), user(`"two"`), user(`"three"`),
		user(`[{"type":"text","text":"[Image: original 800x600]"},{"type":"text","text":"now it is grey,\nmake it blue"}]`),
		user(`[{"type":"image","source":{}}]`),
		user(`"<command-name>/status</command-name>"`),
		edit("MultiEdit", "file_path", "/work/shop/src/theme.css"),
		edit("Edit", "file_path", "/work/shop/src/button.tsx"),
		say("Made it blue; the tests still need running."),
	}
	if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewClaude(root).Handoff(id)
	if err != nil {
		t.Fatal(err)
	}
	if h.Title != "Checkout redesign" || h.Dir != "/work/shop" || h.First != "fix the checkout button" {
		t.Errorf("head: %+v", h)
	}
	if got := strings.Join(h.Recent, " | "); got != "one | two | three | now it is grey, make it blue | (an image)" {
		t.Errorf("recent: %s", got)
	}
	if got := strings.Join(h.Files, " "); got != "src/theme.css src/button.tsx" {
		t.Errorf("files: %s", got)
	}
	if h.Last != "Made it blue; the tests still need running." || !strings.HasSuffix(h.Path, id+".jsonl") {
		t.Errorf("last %q path %q", h.Last, h.Path)
	}
	if _, err := NewClaude(root).Handoff("99999999-2222-3333-4444-555555555555"); err == nil {
		t.Error("a conversation that is not there was read")
	}
	if _, err := NewClaude(root).Handoff("../../etc/passwd"); err == nil {
		t.Error("a name that is not an id was read")
	}
}
