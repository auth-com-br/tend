package tmuxshim

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// fake is a session of panes in tabs, recording what it was asked.
type fake struct {
	panes []Pane
	next  int
	calls []string
	runs  map[string]Run
	text  map[string]string
}

func newFake() *fake {
	return &fake{
		panes: []Pane{
			{ID: "p_1", Tab: "t_1", Cwd: "/work", Focused: true},
			{ID: "p_2", Tab: "t_1", Cwd: "/work"}, // a shell the user keeps beside the agent
			{ID: "p_3", Tab: "t_2", Cwd: "/elsewhere"},
		},
		next: 4, runs: map[string]Run{}, text: map[string]string{},
	}
}

func (f *fake) Panes() ([]Pane, error) { return append([]Pane(nil), f.panes...), nil }

func (f *fake) add(tab string, run Run) Pane {
	p := Pane{ID: fmt.Sprintf("p_%d", f.next), Tab: tab, Cwd: run.Dir}
	f.next++
	f.panes = append(f.panes, p)
	f.runs[p.ID] = run
	return p
}

func (f *fake) Split(target string, down bool, run Run) (Pane, error) {
	f.calls = append(f.calls, fmt.Sprintf("split %s down=%v %q", target, down, run.Argv))
	for _, p := range f.panes {
		if p.ID == target {
			return f.add(p.Tab, run), nil
		}
	}
	return Pane{}, fmt.Errorf("no pane %s", target)
}

func (f *fake) NewTab(near, name string, run Run) (Pane, error) {
	f.calls = append(f.calls, fmt.Sprintf("tab near %s %q %q", near, name, run.Argv))
	return f.add(fmt.Sprintf("t_%d", 10+f.next), run), nil
}

func (f *fake) Respawn(pane string, run Run) error {
	f.calls = append(f.calls, "respawn "+pane)
	f.runs[pane] = run
	return nil
}

func (f *fake) Close(pane string) error {
	f.calls = append(f.calls, "close "+pane)
	for i, p := range f.panes {
		if p.ID == pane {
			f.panes = append(f.panes[:i], f.panes[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fake) Rename(pane, title string) error {
	f.calls = append(f.calls, "rename "+pane+" "+title)
	for i := range f.panes {
		if f.panes[i].ID == pane {
			f.panes[i].Title = title
		}
	}
	return nil
}

func (f *fake) Focus(pane string) error { f.calls = append(f.calls, "focus "+pane); return nil }

func (f *fake) Read(pane string, lines int) (string, error) {
	f.calls = append(f.calls, fmt.Sprintf("read %s %d", pane, lines))
	return f.text[pane], nil
}

func (f *fake) SendText(pane, text string) error {
	f.calls = append(f.calls, fmt.Sprintf("text %s %q", pane, text))
	return nil
}

func (f *fake) SendKeys(pane string, keys []string) error {
	f.calls = append(f.calls, fmt.Sprintf("keys %s %v", pane, keys))
	return nil
}

// tmux runs one command line through a fresh shim, as each `tmux` Claude
// Code starts is a process of its own, and returns its output and status.
func tmux(t *testing.T, f *fake, state string, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	s := &Shim{Backend: f, Leader: "p_1", Session: "work", State: state, Stdout: &out, Stderr: &errOut}
	code := s.Main(args)
	if code != 0 {
		return strings.TrimSpace(errOut.String()), code
	}
	return strings.TrimSpace(out.String()), code
}

func must(t *testing.T, f *fake, state string, args ...string) string {
	t.Helper()
	out, code := tmux(t, f, state, args...)
	if code != 0 {
		t.Fatalf("tmux %q: status %d: %s", args, code, out)
	}
	return out
}

// TestClaudeCodesTeamBecomesPanesBesideItsLeader replays what Claude Code
// 2.1.323's TmuxBackend runs for two teammates, inside tmux, word for word.
// If it regresses, a team's teammates fail to start — or start in the shell
// the user keeps beside the agent.
func TestClaudeCodesTeamBecomesPanesBesideItsLeader(t *testing.T) {
	f := newFake()
	state := filepath.Join(t.TempDir(), "team.json")
	S := []string{"-S", "/run/tend/work.sock"}
	at := func(args ...string) []string { return append(append([]string(nil), S...), args...) }

	if got := must(t, f, state, at("display-message", "-t", "%1", "-p", "#{window_id}")...); got != "@1" {
		t.Fatalf("the leader's window is %q, want @1", got)
	}
	if got := must(t, f, state, at("list-panes", "-t", "@1", "-F", "#{pane_id}")...); got != "%1" {
		t.Fatalf("the leader's window before any teammate lists %q, want only the leader: Claude Code takes a count of one to mean its first teammate", got)
	}

	first := must(t, f, state, at("split-window", "-d", "-t", "%1", "-h", "-l", "70%", "-P", "-F", "#{pane_id}", "--", "cat")...)
	if first != "%4" {
		t.Fatalf("the split printed %q, want the new pane %%4", first)
	}
	must(t, f, state, at("set-option", "-p", "-t", first, "pane-border-style", "fg=blue")...)
	must(t, f, state, at("select-pane", "-t", first, "-T", "researcher")...)
	must(t, f, state, at("set-option", "-w", "-t", "@1", "pane-border-status", "top")...)
	must(t, f, state, at("set-option", "-p", "-t", first, "remain-on-exit", "failed")...)
	line := `sh -c '{ echo $PPID > "$1"/cgroup.procs; } 2>/dev/null' sh /sys/fs/cgroup/x; cd /work && env CLAUDECODE=1 /home/u/.local/share/claude/versions/2.1.323 --agent-id researcher@team --agent-name researcher --team-name team`
	must(t, f, state, at("respawn-pane", "-k", "-t", first, "--", line)...)

	if got := must(t, f, state, at("list-panes", "-t", "@1", "-F", "#{pane_id}")...); got != "%1\n%4" {
		t.Fatalf("the window lists %q, want the leader and its teammate — not the user's own shell %%2", got)
	}
	second := must(t, f, state, at("split-window", "-d", "-t", "%4", "-v", "-P", "-F", "#{pane_id}", "--", "cat")...)
	must(t, f, state, at("select-layout", "-t", "@1", "main-vertical")...)
	must(t, f, state, at("resize-pane", "-t", "%1", "-x", "30%")...)

	want := []string{
		`split p_1 down=false ["/bin/sh" "-c" "cat"]`,
		"rename p_4 researcher",
		"respawn p_4",
		`split p_4 down=true ["/bin/sh" "-c" "cat"]`,
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tend was asked:\n%s\nwant:\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
	if second != "%5" {
		t.Fatalf("the second teammate is %q, want %%5", second)
	}
	run := f.runs["p_4"]
	if run.Agent != "claude" {
		t.Fatalf("the teammate's agent is %q, want claude: its command starts with sh and its binary is named for its version", run.Agent)
	}
	if !run.CloseOnExit || !strings.Contains(run.Argv[2], line) || !strings.Contains(run.Argv[2], "press Enter") {
		t.Fatalf("remain-on-exit failed should close the pane on success and keep it on failure; ran %q", run.Argv)
	}
}

// TestTheShimSaysTmuxIsThereAndLeavesWhatItCannotDoFailing is what Claude
// Code probes besides the team: `tmux -V` must succeed for it to choose
// panes at all, and a format or option tend does not have must fail, not
// answer blank — a blank in the eight fields it reads for images would be
// taken as a value.
func TestTheShimSaysTmuxIsThereAndLeavesWhatItCannotDoFailing(t *testing.T) {
	f := newFake()
	if out, code := tmux(t, f, "", "-V"); code != 0 || !strings.HasPrefix(out, "tmux ") {
		t.Fatalf("tmux -V = %q, %d", out, code)
	}
	if _, code := tmux(t, f, "", "display-message", "-p", "-t", "%1", "#{client_termtype}\t#{allow-passthrough}"); code == 0 {
		t.Fatal("a format variable tend does not know was answered")
	}
	if _, code := tmux(t, f, "", "show", "-Av", "mouse"); code == 0 {
		t.Fatal("an option tend does not keep was answered")
	}
	if _, code := tmux(t, f, "", "load-buffer", "-w", "-"); code == 0 {
		t.Fatal("load-buffer succeeded: Claude Code would think the clipboard was set")
	}
	if got := must(t, f, "", "display-message", "-p", "-t", "%1", "#{session_name}:#{window_id}.#{pane_id}"); got != "work:@1.%1" {
		t.Fatalf("the leader's address is %q", got)
	}
}

// TestSendKeysTypesTextAndNamesKeysAsTmuxDoes: a tmux user's send-keys
// "make test" Enter is the words and then the key, C-c is ctrl+c, and -l
// is all text.
func TestSendKeysTypesTextAndNamesKeysAsTmuxDoes(t *testing.T) {
	f := newFake()
	must(t, f, "", "send-keys", "-t", "%2", "make test", "Enter", "C-c")
	must(t, f, "", "send-keys", "-l", "-t", "%2", "Enter")
	want := []string{`text p_2 "make test"`, "keys p_2 [enter]", "keys p_2 [ctrl+c]", `text p_2 "Enter"`}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(f.calls, "\n"), strings.Join(want, "\n"))
	}
}

// TestAPaneClosedByHandLeavesTheTeam: a teammate the user closed is not
// counted, so the next one is laid out as though it were never there.
func TestAPaneClosedByHandLeavesTheTeam(t *testing.T) {
	f := newFake()
	state := filepath.Join(t.TempDir(), "team.json")
	must(t, f, state, "split-window", "-d", "-t", "%1", "-h", "--", "cat")
	_ = f.Close("p_4")
	if got := must(t, f, state, "list-panes", "-t", "%1", "-F", "#{pane_id}"); got != "%1" {
		t.Fatalf("after its teammate was closed the window lists %q", got)
	}
}

// TestKillWindowLeavesTheLeader: closing the team's window closes the team,
// not the pane the person runs Claude Code in.
func TestKillWindowLeavesTheLeader(t *testing.T) {
	f := newFake()
	state := filepath.Join(t.TempDir(), "team.json")
	must(t, f, state, "split-window", "-t", "%1", "--", "cat")
	must(t, f, state, "kill-window", "-t", "@1")
	if strings.Contains(strings.Join(f.calls, "\n"), "close p_1") {
		t.Fatalf("the leader was closed: %v", f.calls)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "close p_4") {
		t.Fatalf("the teammate was not closed: %v", f.calls)
	}
}

// TestCommandsJoinedBySemicolonsAllRun: `tmux a \; b` is two commands.
func TestCommandsJoinedBySemicolonsAllRun(t *testing.T) {
	f := newFake()
	must(t, f, "", "select-pane", "-t", "%2", "-T", "one", ";", "select-pane", "-t", "%3", "-T", "two")
	if len(f.calls) != 2 {
		t.Fatalf("ran %v", f.calls)
	}
}
