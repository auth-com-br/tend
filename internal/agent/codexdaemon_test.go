package agent

import (
	"reflect"
	"sync"
	"testing"
)

func withCodex(t *testing.T, hasFlag bool) {
	t.Helper()
	old := probeCodex
	probeCodex = func() bool { return hasFlag }
	codexDaemonFlag.once = sync.Once{}
	t.Cleanup(func() {
		probeCodex = old
		codexDaemonFlag.once = sync.Once{}
	})
}

// TestCodexIsStartedWithoutTheSharedDaemon: a Codex whose help lists
// --no-daemon is started with it — on its own, by name, or as the program
// a shell script is handed — and only once; anything else, and a Codex too
// old for the flag, is started as asked. Found on the owner's machine: with
// Codex 0.167.1 its daemon kept the first pane's TEND_PANE_ID, and every
// later Codex's hooks spoke as that pane (issue #65). If it regresses, no
// Codex agent's conversation, usage or state reaches its own pane.
func TestCodexIsStartedWithoutTheSharedDaemon(t *testing.T) {
	withCodex(t, true)
	cases := []struct{ in, want []string }{
		{[]string{"codex"}, []string{"codex", "--no-daemon"}},
		{[]string{"/usr/bin/codex", "resume", "abc"}, []string{"/usr/bin/codex", "--no-daemon", "resume", "abc"}},
		{[]string{"codex", "--no-daemon", "resume", "abc"}, []string{"codex", "--no-daemon", "resume", "abc"}},
		{[]string{"/bin/sh", "-c", "exec \"$@\"", "tend-fix", "codex", "fix it"}, []string{"/bin/sh", "-c", "exec \"$@\"", "tend-fix", "codex", "--no-daemon", "fix it"}},
		{[]string{"claude", "--resume", "x"}, []string{"claude", "--resume", "x"}},
		{[]string{"/bin/sh", "-c", "codex"}, []string{"/bin/sh", "-c", "codex"}},
		{nil, nil},
	}
	for _, c := range cases {
		if got := LaunchArgv(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("LaunchArgv(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	withCodex(t, false)
	if got := LaunchArgv([]string{"codex", "resume", "abc"}); !reflect.DeepEqual(got, []string{"codex", "resume", "abc"}) {
		t.Errorf("an older Codex was given the flag: %q", got)
	}
}
