package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guardConfig(t *testing.T, mode string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(path, []byte("[policy]\nguard = \""+mode+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_CONFIG", path)
	t.Setenv("TEND_SOCKET_PATH", "")
	t.Setenv("TEND_PANE_ID", "")
}

func hookAnswer(t *testing.T, tool, command string) map[string]any {
	t.Helper()
	in, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": tool,
		"tool_input": map[string]any{"command": command}, "cwd": t.TempDir(),
	})
	var out bytes.Buffer
	if err := guardHook(bytes.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("answer %q: %v", out.String(), err)
	}
	h, _ := m["hookSpecificOutput"].(map[string]any)
	return h
}

// TestTheGuardsHookAnswersClaudeCodeAsItsSettingsSay: deny refuses with the
// reason, ask hands it to the person, off and a harmless command say
// nothing — and nothing is what Claude Code reads as "go on". If it
// regresses, the guard stops nothing, or stops everything.
func TestTheGuardsHookAnswersClaudeCodeAsItsSettingsSay(t *testing.T) {
	guardConfig(t, "deny")
	a := hookAnswer(t, "Bash", "rm -rf ~/")
	if a["permissionDecision"] != "deny" || a["hookEventName"] != "PreToolUse" ||
		!strings.Contains(a["permissionDecisionReason"].(string), "home directory") {
		t.Fatalf("deny: %v", a)
	}
	if a := hookAnswer(t, "Bash", "go test ./..."); a != nil {
		t.Fatalf("a harmless command was answered: %v", a)
	}
	if a := hookAnswer(t, "Edit", "rm -rf /"); a != nil {
		t.Fatalf("a tool that is not the shell was judged: %v", a)
	}

	guardConfig(t, "ask")
	if a := hookAnswer(t, "Bash", "git push --force"); a["permissionDecision"] != "ask" {
		t.Fatalf("ask: %v", a)
	}
	guardConfig(t, "off")
	if a := hookAnswer(t, "Bash", "rm -rf /"); a != nil {
		t.Fatalf("off answered: %v", a)
	}
}
