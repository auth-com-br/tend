//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestAnAgentsTaskContinuesInAnother: a pane whose Claude Code has said,
// as its hook does, which conversation it is in offers "continue in..."
// on its menu; choosing codex starts codex in a new tab where the
// conversation was held, and puts a note of the task in the context — the
// requests, the files written, the last words, the uncommitted work — which
// enter types into codex. The conversation, the hook's report and codex are
// stand-ins; the repository, the menus and the context are real. If it
// regresses, handing a task on starts the other agent with nothing, or in
// the wrong place. The sessions list's ctrl+g does the same for a
// conversation chosen there.
func TestAnAgentsTaskContinuesInAnother(t *testing.T) {
	project := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", project}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("needs git: %v %s", err, out)
		}
	}
	git("init", "-q", "-b", "checkout-fix")
	if err := os.WriteFile(filepath.Join(project, "button.tsx"), []byte("grey\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "one")
	if err := os.WriteFile(filepath.Join(project, "button.tsx"), []byte("blue\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	claudeDir := t.TempDir()
	folder := filepath.Join(claudeDir, "projects", "-work")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	convo := strings.Join([]string{
		`{"type":"ai-title","aiTitle":"Checkout button"}`,
		`{"type":"user","cwd":"` + project + `","message":{"role":"user","content":"make the checkout button blue"}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"` + project + `/button.tsx"}}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Made it blue; the tests still need running."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(folder, id+".jsonl"), []byte(convo), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	codex := "#!/bin/sh\necho \"CODEX-IN:$(basename \"$PWD\")\"\nexec cat\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(codex), 0o755); err != nil {
		t.Fatal(err)
	}
	gemini := "#!/bin/sh\necho \"GEMINI-IN:$(basename \"$PWD\")\"\nexec cat\n"
	if err := os.WriteFile(filepath.Join(bin, "gemini"), []byte(gemini), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh",
		"CLAUDE_CONFIG_DIR="+claudeDir, "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "handon"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "handon") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// Claude Code's hook, as it reports (source tend:claude, as
	// internal/integration/assets/claude says): the agent, then its
	// conversation.
	for _, call := range [][2]string{
		{"pane.report_agent", `{"pane_id":"p_1","source":"tend:claude","agent":"claude","state":"idle","seq":1}`},
		{"pane.report_agent_session", `{"pane_id":"p_1","source":"tend:claude","agent":"claude","agent_session_id":"` + id + `","seq":2}`},
	} {
		cmd := exec.Command(tend, "api", "-s", "handon", call[0], call[1])
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", call[0], err, out)
		}
	}
	a.waitForScreen(t, "the agent listed", func(s string) bool { return sidebarHas(s, "claude") })

	a.rightClickAt(t, 70, 12)
	a.waitForScreen(t, "the pane's menu", func(s string) bool { return strings.Contains(s, "continue in...") })
	row := a.lineContaining(t, "continue in...")
	a.clickAt(t, columnOfString(a.lines()[row-1], "continue in...")+2, row)
	a.waitForScreen(t, "the agents", func(s string) bool {
		return strings.Contains(s, "continue in") && strings.Contains(s, "codex") && strings.Contains(s, "gemini")
	})
	row = a.lineContaining(t, "codex")
	a.clickAt(t, columnOfString(a.lines()[row-1], "codex")+1, row)
	a.waitForScreen(t, "codex in the project, the note in the context", func(s string) bool {
		return strings.Contains(s, "CODEX-IN:"+filepath.Base(project)) && strings.Contains(s, "Task: Checkout button") &&
			strings.Contains(s, "Send types it into 2 codex")
	})
	a.send(t, "\r")
	a.waitForScreen(t, "the note typed into codex", func(s string) bool {
		return strings.Contains(s, "You are taking over a task another agent (claude)") && strings.Contains(s, "button.tsx") &&
			strings.Contains(s, "checkout-fix")
	})

	// From the sessions list, a conversation is handed on the same way.
	a.send(t, "\x1b")
	a.waitForScreen(t, "the context closed", func(s string) bool { return !strings.Contains(s, "CONTEXT") })
	a.send(t, "\x02S")
	a.waitForScreen(t, "the list", func(s string) bool {
		return strings.Contains(s, "AGENT SESSIONS") && strings.Contains(s, "Checkout button")
	})
	a.send(t, "\x07")
	a.waitForScreen(t, "the agents", func(s string) bool { return !strings.Contains(s, "AGENT SESSIONS") && strings.Contains(s, "gemini") })
	row = a.lineContaining(t, "gemini")
	a.clickAt(t, columnOfString(a.lines()[row-1], "gemini")+1, row)
	a.waitForScreen(t, "gemini started, the note in the context", func(s string) bool {
		return strings.Contains(s, "GEMINI-IN:"+filepath.Base(project)) && strings.Contains(s, "gemini ← claude") &&
			strings.Contains(s, "Task: Checkout button")
	})
}

// TestAHandoffNoteSaysWhatTheNextAgentNeeds: the note has the task, each
// request, each file, the last words, the branch, the uncommitted work and
// where the whole conversation is, and leaves out what is empty. If it
// regresses, the next agent is told less than the conversation says.
func TestAHandoffNoteSaysWhatTheNextAgentNeeds(t *testing.T) {
	full := handoffNote(proto.AgentHandoff{
		Agent: "claude", Title: "Checkout", First: "fix it", Recent: []string{"now blue"},
		Files: []string{"src/a.ts"}, Last: "done, tests left", Branch: "fix-1", Status: " M src/a.ts",
		Path: "/home/u/.claude/projects/x/1.jsonl",
	})
	for _, want := range []string{"another agent (claude)", "Task: Checkout", "First request: fix it", "- now blue",
		"- src/a.ts", "its last message:\ndone, tests left", "Branch: fix-1", " M src/a.ts", "if you need more: /home/u/.claude/projects/x/1.jsonl"} {
		if !strings.Contains(full, want) {
			t.Errorf("missing %q:\n%s", want, full)
		}
	}
	bare := handoffNote(proto.AgentHandoff{Agent: "claude", First: "fix it"})
	if strings.Contains(bare, "Files it changed") || strings.Contains(bare, "Branch:") || strings.Contains(bare, "Uncommitted") {
		t.Errorf("empty parts said:\n%s", bare)
	}
}
