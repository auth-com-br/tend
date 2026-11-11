package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestAgentAsksReadsWhatARealAgentAsks reads the screens of real agents
// stopped for the user, captured as they were drawn (testdata/asks): Claude
// Code asking to run a command and asking to trust a folder, and the DeepSeek
// Harness's TUI asking to approve a tool, which asks with its options alone.
// If it regresses, a notification says only that an agent needs attention,
// and every one has to be gone to before knowing which can wait.
func TestAgentAsksReadsWhatARealAgentAsks(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"claude-bash.txt", "Bash command: touch outro-arquivo.txt — Do you want to proceed?"},
		{"claude-trust.txt", "Accessing workspace: /tmp/claude-1000/cc-bloq — Quick safety check: Is this a project you created or one you trust?"},
		{"dsh-approval.txt", "approval needed · bash: escalate sandbox to danger-full-access: O caminho /home/akira/aprov…"},
	} {
		data, err := os.ReadFile(filepath.Join("testdata", "asks", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := AgentAsks(string(data)); got != tc.want {
			t.Errorf("%s:\n got  %q\n want %q", tc.file, got, tc.want)
		}
	}
}

// TestAgentAsksSaysNothingForAScreenThatAsksNothing: an agent's ordinary
// screen — its answer, its prompt — is not a question, and a notification
// must not quote it as one.
func TestAgentAsksSaysNothingForAScreenThatAsksNothing(t *testing.T) {
	screen := strings.Join([]string{
		"● I ran touch arquivo-teste.txt, and it finished without errors.",
		"✻ Crunched for 10s · done 1:29 PM",
		strings.Repeat("─", 60),
		"❯",
		strings.Repeat("─", 60),
		"  ⏵⏵ auto mode on (shift+tab to cycle)",
	}, "\n")
	if got := AgentAsks(screen); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

// TestAgentAsksIsShortEnoughToReadAtAGlance: a long question is cut.
func TestAgentAsksIsShortEnoughToReadAtAGlance(t *testing.T) {
	screen := strings.Repeat("─", 40) + "\n " + strings.Repeat("very long command ", 20) + "\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n"
	got := AgentAsks(screen)
	if n := utf8.RuneCountInString(got); n > AsksMaxRunes || !strings.HasSuffix(got, "…") {
		t.Errorf("got %d runes %q, want at most %d ending in …", n, got, AsksMaxRunes)
	}
}
