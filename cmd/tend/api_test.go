//go:build unix

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestServeOffersTheAutomationSocket is the hooks path end to end: a real
// server starts the JSON socket beside the session one, every pane inherits
// the env a hook reads, and a report over that socket changes what the pane
// is shown as. If it regresses, integrations have nowhere to speak and the
// arbiter never hears from them.
func TestServeOffersTheAutomationSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "hooks"}, pty.Options{
		Size: pty.Size{Cols: 80, Rows: 20},
		Env:  env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(80, 20, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "hooks")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	apiPath, err := transport.APISocketPath("hooks")
	if err != nil {
		t.Fatal(err)
	}
	conn := dialAPISocket(t, apiPath)
	defer conn.Close()
	r := bufio.NewReader(conn)

	reply := callAPI(t, conn, r, `{"id":"1","method":"ping"}`)
	result, _ := reply["result"].(map[string]any)
	if result["type"] != "pong" || result["protocol"] == nil {
		t.Fatalf("ping = %v", reply)
	}

	list := callAPI(t, conn, r, `{"id":"list","method":"pane.list"}`)
	panes, _ := list["result"].(map[string]any)["panes"].([]any)
	if len(panes) == 0 {
		t.Fatalf("pane.list = %v", list)
	}
	info, _ := panes[0].(map[string]any)
	paneID, _ := info["pane_id"].(string)
	if paneID == "" {
		t.Fatalf("pane.list entry = %v", info)
	}

	// Markers keep the values off the pane border characters that otherwise
	// glue themselves to whatever is printed at the edge of the frame.
	a.send(t, "printf '<<%s|%s>>\\n' \"$TEND_ENV\" \"$TEND_PANE_ID\"\n")
	a.waitForScreen(t, "pane env", func(s string) bool {
		return strings.Contains(s, "<<"+api.EnvMarkerValue+"|"+paneID+">>")
	})

	report := fmt.Sprintf(
		`{"id":"2","method":"pane.report_agent","params":{"pane_id":%q,"source":"test-hook","agent":"deploy-bot","state":"blocked","message":"approve?","seq":1}}`,
		paneID,
	)
	if reply = callAPI(t, conn, r, report); reply["error"] != nil {
		t.Fatalf("report = %v", reply)
	}
	got := callAPI(t, conn, r, fmt.Sprintf(
		`{"id":"3","method":"pane.get","params":{"pane_id":%q}}`, paneID,
	))
	info, _ = got["result"].(map[string]any)["pane"].(map[string]any)
	if info["agent"] != "deploy-bot" || info["agent_state"] != "blocked" || info["message"] != "approve?" {
		t.Errorf("after report, pane = %v", info)
	}

	// The socket path the pane sees must be this session's automation socket,
	// not the client protocol one. A hook that dials the wrong file gets a
	// framed protocol it cannot speak. Compared in the shell: the path wraps
	// on screen and border glyphs break a substring match.
	a.send(t, "case \"$TEND_SOCKET_PATH\" in */api/hooks.sock) printf 'sock-ok\\n';; *) printf 'sock-bad\\n';; esac\n")
	a.waitForScreen(t, "socket path", func(s string) bool {
		return strings.Contains(s, "sock-ok")
	})
}

func dialAPISocket(t *testing.T, path string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			return conn
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("automation socket %s never came up; log:\n%s", path, readServerLog(filepath.Dir(filepath.Dir(path)), "hooks"))
	return nil
}

func callAPI(t *testing.T, conn net.Conn, r *bufio.Reader, line string) map[string]any {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintln(conn, line); err != nil {
		t.Fatal(err)
	}
	reply, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("no reply to %s: %v", line, err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(reply), &out); err != nil {
		t.Fatalf("reply %q: %v", reply, err)
	}
	return out
}

func readServerLog(runtimeDir, name string) string {
	b, err := os.ReadFile(filepath.Join(runtimeDir, name+".log"))
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// TestTheCommandsScriptsUse drives a session the way a script or another agent
// does — from outside, with no client attached — and checks the answers are
// what a shell can act on. If it regresses, automation is unusable without
// somebody sitting in front of the session.
func TestTheCommandsScriptsUse(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// No session yet: the failure has to say so rather than hang or panic.
	if out, err := run("agent", "list", "-s", "cli"); err == nil {
		t.Fatalf("agent list against nothing succeeded: %s", out)
	} else if !strings.Contains(out, "not running") {
		t.Errorf("the error does not explain itself: %s", out)
	}

	if out, err := run("new", "-s", "cli", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "cli") })

	// A pane is there, and can be typed into and read back.
	out, err := run("pane", "list", "-s", "cli")
	if err != nil || !strings.Contains(out, `"p_1"`) {
		t.Fatalf("pane list = %q, %v", out, err)
	}
	if out, err := run("pane", "send-text", "-s", "cli", "-submit", "p_1", "printf from-the-cli"); err != nil {
		t.Fatalf("send-text: %v\n%s", err, out)
	}
	if out, err := run("pane", "wait", "-s", "cli", "-contains", "from-the-cli", "-timeout", "10s", "p_1"); err != nil {
		t.Fatalf("pane wait: %v\n%s", err, out)
	}
	out, err = run("pane", "read", "-s", "cli", "p_1")
	if err != nil || !strings.Contains(out, "from-the-cli") {
		t.Fatalf("pane read = %q, %v", out, err)
	}

	// A wait that runs out has to fail the command, so `&&` in a script means
	// what it looks like.
	if out, err := run("pane", "wait", "-s", "cli", "-contains", "never-printed", "-timeout", "300ms", "p_1"); err == nil {
		t.Errorf("a wait that timed out succeeded: %s", out)
	}

	// And the generic door reaches methods with no command of their own.
	out, err = run("api", "-s", "cli", "session.snapshot")
	if err != nil || !strings.Contains(out, `"session_snapshot"`) {
		t.Fatalf("tend api = %q, %v", out, err)
	}
}

// TestAPluginExtendsTheSession is the plugin host end to end with the real
// binary: a directory with a manifest is linked, its hook runs when a pane
// opens, its action runs when invoked, and the pane it offers really runs its
// own command. If it regresses, nothing installed beside tend can extend it.
func TestAPluginExtendsTheSession(t *testing.T) {
	runtimeDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", filepath.Join(configDir, "tend.toml"))
	bin := buildBinary(t)
	env := append(os.Environ(),
		"TEND_RUNTIME_DIR="+runtimeDir,
		"TEND_CONFIG="+filepath.Join(configDir, "tend.toml"),
		"SHELL=/bin/sh",
	)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	root := t.TempDir()
	manifest := `
id = "demo"
name = "Demo"
version = "0.1.0"

[[events]]
on = "pane.opened"
command = ["./record.sh", "opened"]

[[actions]]
id = "greet"
title = "Greet"
command = ["./record.sh", "greet"]

[[panes]]
id = "side"
title = "Demo pane"
command = ["/bin/sh", "-c", "printf i-am-the-plugin-pane\\n; sleep 30"]
`
	if err := os.WriteFile(filepath.Join(root, "tend-plugin.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	record := "#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$TEND_PLUGIN_CONTEXT_JSON\" >>\"$TEND_PLUGIN_ROOT/ran.log\"\nif [ \"$1\" = greet ]; then printf 'the-action-ran\\n'; fi\n"
	if err := os.WriteFile(filepath.Join(root, "record.sh"), []byte(record), 0o755); err != nil {
		t.Fatal(err)
	}

	if out, err := run("plugin", "link", root); err != nil {
		t.Fatalf("plugin link: %v\n%s", err, out)
	}
	if out, err := run("plugin", "list"); err != nil || !strings.Contains(out, "demo") {
		t.Fatalf("plugin list = %q, %v", out, err)
	}

	if out, err := run("new", "-s", "plug", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "plug") })

	// The hook ran when the pane opened, and was told where.
	log := filepath.Join(root, "ran.log")
	deadline := time.Now().Add(10 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(log); err == nil && strings.Contains(string(b), "opened") {
			text = string(b)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(text, `"pane_id":"p_1"`) {
		t.Fatalf("the pane.opened hook did not run with a pane in its context; the log holds:\n%s", text)
	}

	// An action's output comes back to whoever invoked it.
	out, err := run("plugin", "run", "-s", "plug", "greet")
	if err != nil || !strings.Contains(out, "the-action-ran") {
		t.Fatalf("plugin run = %q, %v", out, err)
	}

	// And a pane the plugin offers runs the plugin's own command.
	if out, err := run("plugin", "open", "-s", "plug", "side"); err != nil {
		t.Fatalf("plugin open: %v\n%s", err, out)
	}
	for time.Now().Before(deadline.Add(10 * time.Second)) {
		out, _ := run("pane", "read", "-s", "plug", "p_2")
		if strings.Contains(out, "i-am-the-plugin-pane") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("the pane the plugin opened never ran its command")
}

// TestAWorktreeBecomesASpace is worktrees end to end with the real binary and
// real git: a checkout made for an agent, opened as a space in the
// repository's group, found in the list, and removed with its space. If it
// regresses, two agents on one project go back to sharing a checkout.
func TestAWorktreeBecomesASpace(t *testing.T) {
	runtimeDir := t.TempDir()
	configDir := t.TempDir()
	worktrees := t.TempDir()
	cfg := filepath.Join(configDir, "tend.toml")
	if err := os.WriteFile(cfg, []byte("[worktrees]\ndirectory = \""+worktrees+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)

	repo := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"commit", "-q", "--allow-empty", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Dir = env, repo
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("new", "-s", "wt", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "wt") })

	out, err := run("worktree", "create", "-s", "wt", "-json", "feature/login")
	if err != nil {
		t.Fatalf("worktree create: %v\n%s", err, out)
	}
	want := filepath.Join(worktrees, "project", "feature-login")
	if !strings.Contains(out, want) {
		t.Fatalf("the worktree is not where the settings say: %s", out)
	}
	if _, err := os.Stat(filepath.Join(want, ".git")); err != nil {
		t.Fatalf("no checkout at %s: %v", want, err)
	}

	// Open as a space, filed under the repository.
	out, err = run("api", "-s", "wt", "workspace.list")
	if err != nil || !strings.Contains(out, `"group":"project"`) || !strings.Contains(out, want) {
		t.Fatalf("the new space is not in the repository's group: %s %v", out, err)
	}

	out, err = run("worktree", "list", "-s", "wt")
	if err != nil || !strings.Contains(out, "feature/login") || !strings.Contains(out, "w_2") {
		t.Fatalf("worktree list = %q, %v", out, err)
	}

	// A change in it makes an unforced remove refuse, and keep the space.
	if err := os.WriteFile(filepath.Join(want, "draft"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run("worktree", "remove", "-s", "wt", "w_2"); err == nil || !strings.Contains(out, "worktree_dirty") {
		t.Fatalf("removing a dirty worktree = %q, %v; want a refusal", out, err)
	}
	if out, _ := run("api", "-s", "wt", "workspace.list"); !strings.Contains(out, "w_2") {
		t.Fatal("a refused removal closed the space anyway")
	}

	// A locked worktree makes even a forced remove refuse, after the space
	// was shut for it. The space comes back, in the same place (herdr's
	// restore_shutdown_worktree_panes); losing it to a removal that did not
	// happen is the bug this guards.
	lock := exec.Command("git", "-C", want, "worktree", "lock", want)
	if out, err := lock.CombinedOutput(); err != nil {
		t.Fatalf("git worktree lock: %v\n%s", err, out)
	}
	if out, err := run("worktree", "remove", "-s", "wt", "-force", "w_2"); err == nil {
		t.Fatalf("removing a locked worktree = %q; want a refusal", out)
	}
	space := ""
	if out, _ := run("api", "-s", "wt", "workspace.list"); strings.Contains(out, want) {
		for _, id := range []string{"w_3", "w_4"} {
			if strings.Contains(out, `"workspace_id":"`+id+`"`) {
				space = id
			}
		}
	}
	if space == "" {
		t.Fatal("a refused forced removal lost the space")
	}
	unlock := exec.Command("git", "-C", want, "worktree", "unlock", want)
	if out, err := unlock.CombinedOutput(); err != nil {
		t.Fatalf("git worktree unlock: %v\n%s", err, out)
	}

	if out, err := run("worktree", "remove", "-s", "wt", "-force", space); err != nil {
		t.Fatalf("forced remove: %v\n%s", err, out)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Error("the checkout is still there after removing it")
	}
	if out, _ := run("api", "-s", "wt", "workspace.list"); strings.Contains(out, want) {
		t.Error("the space is still open after its worktree was removed")
	}
}

// TestFollowingASessionsEventsFromTheShell: a script that reacts to a session
// reads this. If it regresses, the only way to follow a session is to poll it.
func TestFollowingASessionsEventsFromTheShell(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")

	if out, err := exec.Command(bin, "new", "-s", "ev", "--", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	// The command above ran with the test's own environment; the session it
	// made is in the temporary runtime directory because of t.Setenv.
	t.Cleanup(func() { stopSession(t, "ev") })

	watch := exec.Command(bin, "events", "-s", "ev", "-kinds", "pane.opened")
	watch.Env = env
	out, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = watch.Process.Kill(); _ = watch.Wait() }()

	lines := bufio.NewScanner(out)
	// Give the subscription a moment to be in place before making an event.
	time.Sleep(300 * time.Millisecond)
	if out, err := exec.Command(bin, "new", "-s", "ev", "--", "/bin/sh").CombinedOutput(); err != nil {
		t.Fatalf("second pane: %v\n%s", err, out)
	}

	done := make(chan string, 1)
	go func() {
		for lines.Scan() {
			// Asked for by tend's older name, sent by herdr's.
			if strings.Contains(lines.Text(), "pane.created") {
				done <- lines.Text()
				return
			}
		}
		done <- ""
	}()
	select {
	case line := <-done:
		if !strings.Contains(line, `"pane_id"`) {
			t.Errorf("the event does not name the pane: %q", line)
		}
	case <-time.After(15 * time.Second):
		t.Error("no event arrived on the stream")
	}
}

// TestSavingAndRebuildingALayoutFromTheShell: setting up a session by hand
// every morning is the thing a layout file exists to stop.
func TestSavingAndRebuildingALayoutFromTheShell(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("new", "-s", "lay", "--", "/bin/sh"); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	t.Cleanup(func() { stopSession(t, "lay") })
	if out, err := run("api", "-s", "lay", "pane.split",
		`{"pane_id":"p_1","direction":"down","command":["/bin/sh","-c","sleep 30"]}`); err != nil {
		t.Fatalf("split: %v\n%s", err, out)
	}

	file := filepath.Join(t.TempDir(), "layout.json")
	if out, err := run("layout", "save", "-s", "lay", "-o", file); err != nil {
		t.Fatalf("layout save: %v\n%s", err, out)
	}
	saved, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"tree"`) || !strings.Contains(string(saved), "sleep 30") {
		t.Fatalf("the saved layout does not describe the tab:\n%s", saved)
	}

	if out, err := run("layout", "apply", "-s", "lay", file, "-name", "rebuilt"); err != nil {
		t.Fatalf("layout apply: %v\n%s", err, out)
	}
	out, err := run("api", "-s", "lay", "session.snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, `"pane_id"`) < 4 {
		t.Errorf("the rebuilt arrangement is not there:\n%s", out)
	}
}

// TestWhatAHookSaysAboutAnAgentReachesTheSidebar is the point of metadata: a
// list that says only "working" is a list that makes you open the pane to find
// out anything. The name a hook gives the agent replaces its own, and a value
// it reports is shown where the sidebar's rows put a $token for it, which is
// how herdr shows them.
func TestWhatAHookSaysAboutAnAgentReachesTheSidebar(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte("[ui.sidebar.agents]\nrows = [[\"state_icon\", \"workspace\"], [\"agent\", \"$ctx\"]]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "TEND_CONFIG="+cfg)

	// A session with an agent in it, and a client watching.
	prog := fakeAgentBin(t, "claude", "printf 'esc to interrupt\\n'; sleep 60")
	if out, err := exec.Command(bin, "new", "-s", "meta", "--", prog).CombinedOutput(); err != nil {
		t.Fatalf("tend new: %v\n%s", err, out)
	}
	p, err := pty.Start(bin, []string{"attach", "-s", "meta"}, pty.Options{
		Size: pty.Size{Cols: 110, Rows: 16}, Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 16, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "meta") })
	a.waitForScreen(t, "the agent in the list", func(s string) bool {
		return strings.Contains(s, "claude")
	})

	report := `{"pane_id":"p_1","source":"my-hook","agent":"claude",` +
		`"display_agent":"claude opus","tokens":{"ctx":"23%"}}`
	cmd := exec.Command(bin, "api", "-s", "meta", "pane.report_metadata", report)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}

	a.waitForScreen(t, "what the hook said, in the list", func(s string) bool {
		return strings.Contains(s, "claude opus · 23%")
	})
}

// TestMakingAWorktreeFromTheSpaceMenu: the worktree commands are useful, and
// a command is something to remember. The space menu is where somebody
// looking at the space they want a room beside will look.
func TestMakingAWorktreeFromTheSpaceMenu(t *testing.T) {
	runtimeDir := t.TempDir()
	worktrees := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte("[worktrees]\ndirectory = \""+worktrees+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)

	repo := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	p, err := pty.Start(bin, []string{"attach", "-s", "wtm"}, pty.Options{
		Size: pty.Size{Cols: 110, Rows: 24}, Env: env, Dir: repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 24, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "wtm") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.openMenuOn(t, 6, a.lineContaining(t, "main"), "new worktree")
	row := a.lineContaining(t, "new worktree")
	a.clickAt(t, columnOfString(a.lines()[row-1], "new worktree")+2, row)
	a.waitForScreen(t, "the branch prompt", func(s string) bool {
		return strings.Contains(s, "new worktree — branch")
	})
	// Typed over the generated name, with the checkout's path following the
	// branch as it is typed, as herdr's overlay shows it.
	a.send(t, "\x15feature/menu")
	a.waitForScreen(t, "the path as the branch is typed", func(s string) bool {
		return strings.Contains(s, "project/feature-menu")
	})
	a.send(t, "\r")

	// In the sidebar, not anywhere on screen: the status line says
	// "making feature/menu…" before the checkout exists, and a wait that
	// message satisfies raced the git it was waiting for.
	// And with the prompt gone: its box can be wide enough to reach over
	// the sidebar with the branch typed in it.
	a.waitForScreen(t, "the worktree as a space", func(s string) bool {
		return !strings.Contains(s, "enter · esc") && strings.Contains(a.sidebarText(), "feature/menu")
	})
	if _, err := os.Stat(filepath.Join(worktrees, "project", "feature-menu", ".git")); err != nil {
		t.Errorf("no checkout was made: %v", err)
	}
}

// TestAWorktreeMadeWhileACompanyIsChosenIsInIt: with a company chosen, a
// worktree made from the menu of a space in it gets a space in it too. The
// space is made by the server, on the automation socket, where only the
// client's "new space" used to file a new space in the chosen company. If it
// regresses, the worktree just asked for is nowhere in the list until "all
// spaces" is chosen (issue #34).
func TestAWorktreeMadeWhileACompanyIsChosenIsInIt(t *testing.T) {
	runtimeDir := t.TempDir()
	worktrees := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte("[worktrees]\ndirectory = \""+worktrees+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)

	repo := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	p, err := pty.Start(bin, []string{"attach", "-s", "wtc"}, pty.Options{
		Size: pty.Size{Cols: 120, Rows: 36}, Env: env, Dir: repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(120, 36, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "wtc") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	// A company holding the space, chosen.
	a.send(t, "\x02O")
	a.waitForScreen(t, "the panel", func(s string) bool { return strings.Contains(s, "COMPANIES") })
	a.send(t, "n")
	a.waitForScreen(t, "the name asked for", func(s string) bool { return strings.Contains(s, "new company:") })
	a.send(t, "Auth\r")
	a.waitForScreen(t, "its spaces offered", func(s string) bool { return strings.Contains(s, "[ ] main") })
	a.send(t, " ")
	a.waitForScreen(t, "the space ticked", func(s string) bool { return strings.Contains(s, "[✓] main") })
	a.send(t, "\x1b")
	a.waitForScreen(t, "the list with it", func(s string) bool {
		return strings.Contains(s, "Auth") && strings.Contains(s, "1 space")
	})
	a.send(t, "\r")
	a.waitForScreen(t, "the company chosen", func(s string) bool {
		return !strings.Contains(s, "COMPANIES") && strings.Contains(s, "◆ Auth") && sidebarHas(s, "main")
	})

	a.openMenuOn(t, 6, a.lineContaining(t, "main"), "new worktree")
	row := a.lineContaining(t, "new worktree")
	a.clickAt(t, columnOfString(a.lines()[row-1], "new worktree")+2, row)
	a.waitForScreen(t, "the branch prompt", func(s string) bool {
		return strings.Contains(s, "new worktree — branch")
	})
	a.send(t, "\x15feature/auth\r")
	a.waitForScreen(t, "the worktree as a space, the company still chosen", func(s string) bool {
		return !strings.Contains(s, "enter · esc") && strings.Contains(s, "◆ Auth") &&
			strings.Contains(a.sidebarText(), "feature/auth")
	})
}

// TestAScriptCanAskTheClientToShowAPane: pane.focus over the socket reaches
// the attached client, which moves to the pane — how a plugin brings the
// panel it just opened into view. If it regresses, the call answers and
// nothing on screen moves.
func TestAScriptCanAskTheClientToShowAPane(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("tend %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	p, err := pty.Start(bin, []string{"attach", "-s", "shown"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 14}, Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 14, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "shown") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'FIRST-TAB\\n'\n", "the first tab", func(s string) bool {
		return strings.Contains(s, "FIRST-TAB")
	})
	// A second tab, which the client moves to.
	a.send(t, "\x02c")
	a.waitForScreen(t, "the second tab", func(s string) bool { return !strings.Contains(s, "FIRST-TAB") })

	run("api", "-s", "shown", "pane.focus", `{"pane_id":"p_1"}`)
	a.waitForScreen(t, "the first tab again", func(s string) bool { return strings.Contains(s, "FIRST-TAB") })
}

// TestAKeyGoesToWhatTheLastNotificationWasAbout is herdr's
// open_notification_target: bound with keys.bind, it jumps to the pane the
// last announcement named, wherever it is. If it regresses, "claude needs
// attention" says which agent and gives no way to get there.
func TestAKeyGoesToWhatTheLastNotificationWasAbout(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte("[keys.bind]\nopen-notification = \"O\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh", "TEND_CONFIG="+cfg)

	p, err := pty.Start(bin, []string{"attach", "-s", "noticed"}, pty.Options{
		Size: pty.Size{Cols: 100, Rows: 14}, Env: env,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 14, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "noticed") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.sendUntil(t, "printf 'AGENT-TAB\\n'\n", "the first tab", func(s string) bool {
		return strings.Contains(s, "AGENT-TAB")
	})
	a.send(t, "\x02c")
	a.waitForScreen(t, "the second tab", func(s string) bool { return !strings.Contains(s, "AGENT-TAB") })

	// The agent in the first tab needs an answer, as a hook would say.
	cmd := exec.Command(bin, "api", "-s", "noticed", "pane.report_agent",
		`{"pane_id":"p_1","source":"h","agent":"claude","state":"blocked","seq":1}`)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}
	a.waitForScreen(t, "the announcement", func(s string) bool { return strings.Contains(s, "needs attention") })

	a.send(t, "\x02O")
	a.waitForScreen(t, "the agent's tab", func(s string) bool { return strings.Contains(s, "AGENT-TAB") })
}

// TestInstallingAPluginFromGithub is herdr's `plugin install owner/repo/dir`
// end to end, against a local repository standing in for GitHub: cloned,
// built, kept where tend keeps it, listed, and removed with its files by the
// same shorthand. If it regresses, a plugin published the way herdr's are
// has to be cloned and linked by hand.
func TestInstallingAPluginFromGithub(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(t.TempDir(), "repo")
	pluginDir := filepath.Join(work, "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"demo\"\nname = \"demo\"\nversion = \"1.0.0\"\n\n[[build]]\ncommand = [\"/bin/sh\", \"-c\", \"echo built > built.txt\"]\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "tend-plugin.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{
		{"-C", work, "init", "-q", "-b", "main"}, {"-C", work, "add", "."},
		{"-C", work, "commit", "-q", "-m", "plugin"},
		{"clone", "-q", "--bare", work, filepath.Join(base, "someone", "tools.git")},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	runtimeDir := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	bin := buildBinary(t)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg,
		"TEND_GITHUB_URL=file://"+base)
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("plugin", "install", "someone/tools/plugins/demo"); err == nil || !strings.Contains(out, "-yes") {
		t.Fatalf("without a terminal to ask, install should want -yes: %v\n%s", err, out)
	}
	out, err := run("plugin", "install", "someone/tools/plugins/demo", "-yes")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	managed := filepath.Join(runtimeDir, "state", "plugin-checkouts", "demo")
	if data, err := os.ReadFile(filepath.Join(managed, "plugins", "demo", "built.txt")); err != nil || !strings.Contains(string(data), "built") {
		t.Fatalf("the plugin was not built where tend keeps it: %v", err)
	}
	if out, _ := run("plugin", "list"); !strings.Contains(out, "demo") || !strings.Contains(out, managed) {
		t.Errorf("plugin list = %s", out)
	}

	if out, err := run("plugin", "uninstall", "someone/tools/plugins/demo"); err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if _, err := os.Stat(managed); !os.IsNotExist(err) {
		t.Error("the checkout tend made is still there")
	}
}

// TestAScriptsSSHReachesTheOtherMachine: `tend pane read -ssh host` answers
// from the session on that host. It was found on a real host that the flag
// was read and ignored: the command answered from the local session of the
// same name — another machine's panes, without a word. Two runtime
// directories stand in for two machines, each with a session "same" whose
// pane says which it is.
func TestAScriptsSSHReachesTheOtherMachine(t *testing.T) {
	bin := buildBinary(t)
	near, far := t.TempDir(), t.TempDir()
	stand := filepath.Join(t.TempDir(), "fake-ssh")
	script := "#!/bin/sh\nshift; shift\nTEND_RUNTIME_DIR=" + far + " exec " + bin + " \"$@\"\n"
	if err := os.WriteFile(stand, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+dir, "TEND_SSH="+stand, "SHELL=/bin/sh")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for dir, word := range map[string]string{near: "I-AM-NEAR", far: "I-AM-FAR"} {
		if out, err := run(dir, "new", "-s", "same", "--", "sh", "-c", "echo "+word+"; sleep 60"); err != nil {
			t.Fatalf("new: %v\n%s", err, out)
		}
	}
	t.Cleanup(func() {
		for _, dir := range []string{near, far} {
			stop := exec.Command(bin, "kill", "-s", "same", "-server")
			stop.Env = append(os.Environ(), "TEND_RUNTIME_DIR="+dir)
			_ = stop.Run()
			waitForSocketGone(filepath.Join(dir, "same.sock"))
		}
	})

	var out string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, _ = run(near, "pane", "read", "-ssh", "user@farhost", "-s", "same", "1")
		if strings.Contains(out, "I-AM-") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(out, "I-AM-FAR") || strings.Contains(out, "I-AM-NEAR") {
		t.Fatalf("pane read -ssh answered %q; want the far session's pane", out)
	}

	// A session not running there says so, with the far side's own words.
	out, err := run(near, "pane", "list", "-ssh", "user@farhost", "-s", "absent")
	if err == nil || !strings.Contains(out, "not running on this machine") {
		t.Errorf("an absent far session = %v: %s", err, out)
	}
}

// TestOpeningAWorktreeFromThePopup: "open worktree..." on a space puts up
// herdr's popup of the repository's checkouts, / filters them, and enter
// opens the one chosen as a space. If it regresses, a worktree made
// elsewhere cannot be opened without typing its path.
func TestOpeningAWorktreeFromThePopup(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	cfg := filepath.Join(t.TempDir(), "absent.toml")
	t.Setenv("TEND_CONFIG", cfg)
	bin := buildBinary(t)

	repo := filepath.Join(t.TempDir(), "project")
	elsewhere := filepath.Join(t.TempDir(), "side-checkout")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "first"},
		{"worktree", "add", "-q", "-b", "feature/side", elsewhere},
		{"worktree", "add", "-q", "-b", "other/thing", elsewhere + "-2"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh")
	p, err := pty.Start(bin, []string{"attach", "-s", "wto"}, pty.Options{
		Size: pty.Size{Cols: 110, Rows: 28}, Env: env, Dir: repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 28, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "wto") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.openMenuOn(t, 6, a.lineContaining(t, "main"), "open worktree")
	row := a.lineContaining(t, "open worktree")
	a.clickAt(t, columnOfString(a.lines()[row-1], "open worktree")+2, row)
	a.waitForScreen(t, "the popup", func(s string) bool {
		return strings.Contains(s, "/ filter worktrees") && strings.Contains(s, "3 checkouts") && strings.Contains(s, "feature/side")
	})
	a.send(t, "/feature")
	a.waitForScreen(t, "the filter", func(s string) bool { return strings.Contains(s, "1/3 checkouts") })
	a.send(t, "\r")
	a.waitForScreen(t, "the worktree as a space", func(s string) bool {
		return !strings.Contains(s, "checkouts") && strings.Contains(a.sidebarText(), "feature/side")
	})
}

// TestAContextCaptureReachesThePaneThroughTheServer: what a tool hands the
// server — here `tend context add`, over the automation socket a browser
// extension will use — shows in the context panel (prefix+C) with its parts,
// and Send to Agent types it into the pane this tab works with, without
// submitting it. If it regresses, a capture has no way from the tool that
// made it to the agent that needs it.
func TestAContextCaptureReachesThePaneThroughTheServer(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "ctx"}, pty.Options{Size: pty.Size{Cols: 110, Rows: 34}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 34, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "ctx")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	add := exec.Command(bin, "context", "add", "-s", "ctx", "-source", "browser", "-url", "https://example.com/login",
		"-title", "Login", "-selector", "#email", "-tag", "input")
	add.Env = env
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("context add: %v\n%s", err, out)
	}

	a.send(t, "\x02C")
	a.waitForScreen(t, "the capture in the context panel", func(s string) bool {
		return strings.Contains(s, "CONTEXT") && strings.Contains(s, "[element] #email") &&
			strings.Contains(s, "SELECTED ELEMENT") && strings.Contains(s, "https://example.com/login")
	})
	a.send(t, "s")
	a.waitForScreen(t, "typed into the pane, the panel gone", func(s string) bool {
		return strings.Contains(s, "Context captured in tend:") && !strings.Contains(s, "SELECTED ELEMENT")
	})
}

// TestABrowserAttachedTakesThePagesAndItsCapturesReachThePane: a browser
// attached over the socket — `tend browser attach` standing in for the
// extension's bridge — is sent the page prefix+B asks for, and an element it
// sends to an agent is typed into the pane. If it regresses, the browser's
// road to and from the session is cut somewhere along it.
func TestABrowserAttachedTakesThePagesAndItsCapturesReachThePane(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "web"}, pty.Options{Size: pty.Size{Cols: 110, Rows: 34}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 34, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, "web")
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	browser := exec.Command(bin, "browser", "attach", "-s", "web", "-name", "stand-in")
	browser.Env = env
	out, err := browser.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := browser.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = browser.Process.Kill(); _ = browser.Wait() })
	lines := make(chan string, 8)
	go func() {
		r := bufio.NewReader(out)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			lines <- line
		}
	}()
	// Attached once status names it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := exec.Command(bin, "browser", "status", "-s", "web")
		st.Env = env
		if got, _ := st.Output(); strings.Contains(string(got), "stand-in") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in never attached")
		}
		time.Sleep(100 * time.Millisecond)
	}

	a.send(t, "\x02B")
	a.waitForScreen(t, "the page prompt", func(s string) bool { return strings.Contains(s, "open in browser") })
	a.send(t, "example.com/login\r")
	select {
	case line := <-lines:
		if !strings.Contains(line, `"action":"open"`) || !strings.Contains(line, `"url":"https://example.com/login"`) {
			t.Errorf("the browser was sent %s", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the browser was sent nothing")
	}

	apiPath, err := transport.APISocketPath("web")
	if err != nil {
		t.Fatal(err)
	}
	conn := dialAPISocket(t, apiPath)
	defer conn.Close()
	r := bufio.NewReader(conn)
	list := callAPI(t, conn, r, `{"id":"list","method":"pane.list"}`)
	panes, _ := list["result"].(map[string]any)["panes"].([]any)
	pane, _ := panes[0].(map[string]any)["pane_id"].(string)
	reply := callAPI(t, conn, r, `{"id":"s","method":"browser.send_to_agent","params":{"url":"https://example.com/login","selector":"#email","tag":"input","pane_id":"`+pane+`"}}`)
	if reply["error"] != nil {
		t.Fatalf("send_to_agent: %v", reply)
	}
	a.waitForScreen(t, "the capture typed into the pane", func(s string) bool {
		return strings.Contains(s, "Context captured in tend:") && strings.Contains(s, "(from browser)")
	})
}
