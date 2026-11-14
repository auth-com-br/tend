package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheGuardsHookGoesInBesideWhatIsThereAndOutAgain: installed into a
// settings file with hooks and settings of its own, the guard adds one
// PreToolUse entry on Bash and touches nothing else; installed again it
// adds nothing; uninstalled it leaves the file as it found it. If it
// regresses, installing the guard costs somebody their own hooks.
func TestTheGuardsHookGoesInBesideWhatIsThereAndOutAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	before := `{"model":"opus","hooks":{"PreToolUse":[{"matcher":"Edit","hooks":[{"type":"command","command":"mine"}]}]}}`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	const cmd = "'/usr/local/bin/tend' guard"
	if changed, err := InstallClaudeGuard(path, cmd); err != nil || !changed {
		t.Fatalf("install: %v, %v", changed, err)
	}
	var s map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	pre := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 || s["model"] != "opus" || !strings.Contains(string(data), `"matcher": "Bash"`) {
		t.Fatalf("after install:\n%s", data)
	}
	if changed, _ := InstallClaudeGuard(path, cmd); changed {
		t.Fatal("a second install changed the file")
	}
	if changed, err := UninstallClaudeGuard(path, cmd); err != nil || !changed {
		t.Fatalf("uninstall: %v, %v", changed, err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "guard") || !strings.Contains(string(data), `"mine"`) {
		t.Fatalf("after uninstall:\n%s", data)
	}
}
