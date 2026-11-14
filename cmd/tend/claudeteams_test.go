//go:build unix

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestClaudeTeamsOpensTeammatesAsPanesOfTheSession drives the chain end to
// end against a real server: `tend claude-teams` in a pane, the `tmux` it
// puts first on PATH, and the panes that `tmux` opens. The `claude` here is
// a script giving the commands Claude Code 2.1.323 gives for a teammate —
// it stands in for Claude Code's side, which was checked against the real
// one by hand; this checks tend's. If it regresses, a team's teammates do
// not open, or open somewhere other than beside the agent.
func TestClaudeTeamsOpensTeammatesAsPanesOfTheSession(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	fakes := t.TempDir()
	script := `#!/bin/sh
S="${TMUX%%,*}"
w=$(tmux -S "$S" display-message -t "$TMUX_PANE" -p '#{window_id}')
p=$(tmux -S "$S" split-window -d -t "$TMUX_PANE" -h -l 70% -P -F '#{pane_id}' -- cat)
tmux -S "$S" select-pane -t "$p" -T researcher
tmux -S "$S" set-option -p -t "$p" remain-on-exit failed
tmux -S "$S" respawn-pane -k -t "$p" -- 'echo TEAMMATE-RAN; exec cat'
n=$(tmux -S "$S" list-panes -t "$w" -F '#{pane_id}' | wc -l)
echo "<<$*|$CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS|$n>>"
exec cat
`
	if err := os.WriteFile(filepath.Join(fakes, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "teams"}, pty.Options{Size: pty.Size{Cols: 120, Rows: 30}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 30, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "teams")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, fmt.Sprintf("PATH=%s:$PATH %s claude-teams --model sonnet\n", fakes, bin))
	a.waitForScreen(t, "the lead's report", func(s string) bool {
		return strings.Contains(s, "<<--teammate-mode tmux --model sonnet|1|2>>")
	})

	apiPath, err := transport.APISocketPath("teams")
	if err != nil {
		t.Fatal(err)
	}
	conn := dialAPISocket(t, apiPath)
	defer conn.Close()
	r := bufio.NewReader(conn)
	deadline := time.Now().Add(10 * time.Second)
	for {
		list := callAPI(t, conn, r, `{"id":"l","method":"pane.list"}`)
		panes, _ := list["result"].(map[string]any)["panes"].([]any)
		var mate map[string]any
		for _, v := range panes {
			if m, _ := v.(map[string]any); m["title"] == "researcher" {
				mate = m
			}
		}
		if mate != nil {
			read := callAPI(t, conn, r, fmt.Sprintf(`{"id":"r","method":"pane.read","params":{"pane_id":%q}}`, mate["pane_id"]))
			text, _ := read["result"].(map[string]any)["text"].(string)
			if strings.Contains(text, "TEAMMATE-RAN") {
				if len(panes) != 2 {
					t.Fatalf("the session has %d panes, want the lead and its teammate", len(panes))
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no teammate pane running its command: %v", panes)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestTheTmuxShimLeavesOtherServersToTheRealTmux: inside a claude-teams
// launch, a tmux the agent runs for its own work (its own -L or -S) is not
// tend's to answer.
func TestTheTmuxShimLeavesOtherServersToTheRealTmux(t *testing.T) {
	sock := "/run/user/1000/tend/api/work.sock"
	for _, c := range []struct {
		args []string
		want bool
	}{
		{[]string{"-S", sock, "split-window"}, true},
		{[]string{"-S" + sock, "list-panes"}, true},
		{[]string{"display-message", "-p", "#{pane_id}"}, true},
		{[]string{"-V"}, true},
		{[]string{"-L", "claude-swarm-42", "new-session", "-d"}, false},
		{[]string{"-S", "/tmp/other.sock", "list-sessions"}, false},
		{[]string{"-f", "/dev/null", "-S", sock, "kill-pane"}, true},
	} {
		if got := forTend(c.args, sock); got != c.want {
			t.Errorf("forTend(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestClaudeTeamsKeepsATeammateModeTheUserChose: tmux is the default only
// because Claude Code's own (in-process) puts no teammate in a pane.
func TestClaudeTeamsKeepsATeammateModeTheUserChose(t *testing.T) {
	if got := claudeTeamsArgs([]string{"--teammate-mode", "in-process"}); strings.Join(got, " ") != "--teammate-mode in-process" {
		t.Fatalf("got %q", got)
	}
	if got := claudeTeamsArgs([]string{"-c"}); strings.Join(got, " ") != "--teammate-mode tmux -c" {
		t.Fatalf("got %q", got)
	}
}

// TestClaudeTeamsPutsItsTmuxFirstAndReplacesAnInheritedTMUX: Claude Code
// must find tend's tmux before a real one, and a $TMUX from an outer tmux
// must not survive to point it at the wrong server.
func TestClaudeTeamsPutsItsTmuxFirstAndReplacesAnInheritedTMUX(t *testing.T) {
	env := claudeTeamsEnv([]string{"PATH=/usr/bin", "TMUX=/tmp/tmux-1000/default,1,0", "HOME=/h"}, "/r/api/s.sock", "p_3", "/r/tmux-shim", 77)
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := got[k]; dup {
			t.Fatalf("%s set twice: %q", k, env)
		}
		got[k] = v
	}
	if got["PATH"] != "/r/tmux-shim:/usr/bin" || got["TMUX"] != "/r/api/s.sock,77,0" || got["TMUX_PANE"] != "%3" || got["HOME"] != "/h" {
		t.Fatalf("env = %q", env)
	}
}
