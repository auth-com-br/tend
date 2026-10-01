package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHome points every agent at a home of the test's own, and returns it
// with a manager whose stash and backups are there too.
func fakeHome(t *testing.T) (string, *Manager) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "CURSOR_CONFIG_DIR", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(v, "")
	}
	return home, NewManager(filepath.Join(home, "state"))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func entry(t *testing.T, m *Manager, agent, name string) (Entry, bool) {
	t.Helper()
	for _, e := range m.List().Entries {
		if e.Agent == agent && Key(e.Name) == Key(name) {
			return e, true
		}
	}
	return Entry{}, false
}

const claudeFixture = `{
  "numStartups": 12345678901234567890,
  "projects": {"/home/x": {"mcpServers": {"local-only": {"command": "x"}}}},
  "mcpServers": {
    "Context7": {"type": "stdio", "command": "npx", "args": ["-y", "@upstash/context7-mcp"], "env": {"TOKEN": "s3cret"}, "oauth": {"clientId": "abc"}},
    "siyuan": {"type": "http", "url": "http://127.0.0.1:6806/mcp?a=1&b=2"}
  }
}
`

// TestTurningClaudeOffKeepsTheDefinitionAsItWas: Claude Code has no switch
// of its own, so off takes the server out of ~/.claude.json into the stash,
// and on puts the very same value back, key order and all — its secrets and
// the fields
// tend does not understand among them — while the rest of the file, a
// number too big for a float included, is untouched. If it regresses,
// turning a server off and on loses its token or its OAuth settings.
func TestTurningClaudeOffKeepsTheDefinitionAsItWas(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".claude.json")
	write(t, path, claudeFixture)
	_, before, err := newClaude("").Raw("context7")
	if err != nil {
		t.Fatal(err)
	}

	if err := m.SetEnabled("claude", "context7", false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "Context7") {
		t.Fatal("off left the server in Claude's file")
	}
	e, ok := entry(t, m, "claude", "Context7")
	if !ok || e.Enabled || e.Via != "stash" || e.Def.Command != "npx" {
		t.Fatalf("while off the server is listed as %+v (found %v)", e, ok)
	}
	if info, _ := os.Stat(filepath.Join(home, "state", "mcp", "stash.json")); info == nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the stash holds secrets and must be 0600, is %v", info)
	}

	if err := m.SetEnabled("claude", "context7", true); err != nil {
		t.Fatal(err)
	}
	_, after, err := newClaude("").Raw("context7")
	if err != nil {
		t.Fatal(err)
	}
	// The file is indented again as a whole when it is written, so the
	// value is compared, not its whitespace.
	var b, a bytes.Buffer
	_ = json.Compact(&b, before)
	_ = json.Compact(&a, after)
	if b.String() != a.String() {
		t.Errorf("restored as\n%s\nwas\n%s", a.String(), b.String())
	}
	text := read(t, path)
	for _, want := range []string{"12345678901234567890", `"local-only"`, "a=1&b=2"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rest of the file lost %q:\n%s", want, text)
		}
	}
	if e, _ := entry(t, m, "claude", "context7"); !e.Enabled {
		t.Error("on again, the server is not listed as on")
	}
}

// TestAWriteWaitsForClaudesOwnLock: tend takes the lock Claude Code takes
// on its file, and takes over one left behind by a program that died. If
// it regresses, tend's edit and a running Claude's can interleave and one
// of them is lost.
func TestAWriteWaitsForClaudesOwnLock(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".claude.json")
	write(t, path, claudeFixture)
	lockDir := path + ".lock"
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.Remove(lockDir)
		close(released)
	}()
	start := time.Now()
	if err := m.SetEnabled("claude", "siyuan", false); err != nil {
		t.Fatal(err)
	}
	<-released
	if time.Since(start) < 250*time.Millisecond {
		t.Error("the write did not wait for the lock")
	}

	// A lock nobody has touched for longer than staleLock is a dead one.
	if err := os.Mkdir(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(lockDir, old, old)
	if err := m.SetEnabled("claude", "siyuan", true); err != nil {
		t.Fatalf("a stale lock was not taken over: %v", err)
	}
}

const codexFixture = `# my codex settings
model = "gpt-5"

[mcp_servers.Context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[profiles.fast]
model = "gpt-5-mini"

[mcp_servers.node_repl]
command = "node"
args = []
startup_timeout_sec = 30

[mcp_servers.node_repl.env]
NODE_ENV = "dev" # trailing note

[mcp_servers.siyuan]
url = "http://127.0.0.1:6806/mcp"
`

// TestCodexOffIsItsOwnSwitchInsideTheRightTable: off writes enabled = false
// in the server's own table — not a subtable, not another server — and on
// takes it out again, leaving the comments and the other tables as they
// were. If it regresses, a switched-off server lands in [profiles.fast] or
// the user's comments disappear.
func TestCodexOffIsItsOwnSwitchInsideTheRightTable(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".codex", "config.toml")
	write(t, path, codexFixture)

	if err := m.SetEnabled("codex", "node_repl", false); err != nil {
		t.Fatal(err)
	}
	text := read(t, path)
	if !strings.Contains(text, "[mcp_servers.node_repl]\nenabled = false\n") {
		t.Errorf("enabled = false is not under the server's header:\n%s", text)
	}
	if e, _ := entry(t, m, "codex", "node_repl"); e.Enabled || e.Via != "native" {
		t.Errorf("listed as %+v", e)
	}
	if err := m.SetEnabled("codex", "node_repl", true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != codexFixture {
		t.Errorf("off and on again did not give the file back:\n%s", got)
	}
}

// TestRemovingACodexServerTakesItsSubtables: a server's env table goes with
// it, and nothing around it does. If it regresses, an orphan
// [mcp_servers.x.env] makes Codex refuse its config.
func TestRemovingACodexServerTakesItsSubtables(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".codex", "config.toml")
	write(t, path, codexFixture)
	if err := m.Remove("codex", "node_repl"); err != nil {
		t.Fatal(err)
	}
	text := read(t, path)
	if strings.Contains(text, "node_repl") || strings.Contains(text, "NODE_ENV") {
		t.Errorf("the server or its env stayed:\n%s", text)
	}
	for _, want := range []string{"# my codex settings", "[profiles.fast]", "[mcp_servers.siyuan]", "[mcp_servers.Context7]"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q went with it:\n%s", want, text)
		}
	}
}

// TestCopyingIntoCodexWritesItsOwnShape: a server copied from Claude to
// Codex comes out as Codex's table, env as a subtable; one Codex cannot
// reach (the older SSE transport) is refused rather than written broken.
// If it regresses, a copied server is one Codex cannot start.
func TestCopyingIntoCodexWritesItsOwnShape(t *testing.T) {
	home, m := fakeHome(t)
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers": {
  "fs": {"type": "stdio", "command": "npx", "args": ["-y", "server-fs", "/tmp"], "env": {"MODE": "ro"}},
  "old": {"type": "sse", "url": "http://localhost:9/sse"}
}}`)
	write(t, filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5\"\n")
	if _, err := m.Copy("claude", "fs", "codex"); err != nil {
		t.Fatal(err)
	}
	e, ok := entry(t, m, "codex", "fs")
	if !ok || e.Def.Command != "npx" || len(e.Def.Args) != 3 || e.Def.Env["MODE"] != "ro" {
		t.Errorf("copied as %+v", e)
	}
	if _, err := m.Copy("claude", "old", "codex"); err == nil {
		t.Error("an SSE server was written into Codex")
	}
}

// TestGeminiOffIsItsEnablementFile: off is Gemini's own switch — the name
// lowercased with enabled false in mcp-server-enablement.json — and on
// removes the key; settings.json is not touched. If it regresses, tend's
// switch and `gemini mcp enable` disagree.
func TestGeminiOffIsItsEnablementFile(t *testing.T) {
	home, m := fakeHome(t)
	settings := filepath.Join(home, ".gemini", "settings.json")
	write(t, settings, `{"theme": "x", "mcpServers": {"SiYuan": {"httpUrl": "http://127.0.0.1:6806/mcp"}}}`)
	before := read(t, settings)

	if err := m.SetEnabled("gemini", "siyuan", false); err != nil {
		t.Fatal(err)
	}
	var state map[string]map[string]bool
	_ = json.Unmarshal([]byte(read(t, filepath.Join(home, ".gemini", "mcp-server-enablement.json"))), &state)
	if v, ok := state["siyuan"]; !ok || v["enabled"] {
		t.Errorf("enablement file = %v", state)
	}
	if read(t, settings) != before {
		t.Error("settings.json was changed")
	}
	if e, _ := entry(t, m, "gemini", "SiYuan"); e.Enabled || e.Def.Transport != HTTP {
		t.Errorf("listed as %+v", e)
	}
	if err := m.SetEnabled("gemini", "siyuan", true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, filepath.Join(home, ".gemini", "mcp-server-enablement.json")), "siyuan") {
		t.Error("on did not remove the key")
	}
}

// TestOpenCodeOffIsItsEnabledField: OpenCode's servers carry their own
// switch, which is the one tend uses. If it regresses, an OpenCode server
// is taken out of its file when a field would have done.
func TestOpenCodeOffIsItsEnabledField(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	write(t, path, `{"$schema": "https://opencode.ai/config.json", "mcp": {"headroom": {"type": "remote", "url": "http://localhost:8787/mcp", "enabled": true}}}`)
	if err := m.SetEnabled("opencode", "headroom", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, path), `"enabled": false`) {
		t.Errorf("not switched off in place:\n%s", read(t, path))
	}
	if e, _ := entry(t, m, "opencode", "headroom"); e.Enabled || e.Via != "native" {
		t.Errorf("listed as %+v", e)
	}
}

// TestTurningOnRefusesANameAddedAgainMeanwhile: a server stashed while off
// is not put back over one of the same name added to the agent since. If
// it regresses, turning a server on overwrites the user's newer one.
func TestTurningOnRefusesANameAddedAgainMeanwhile(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	write(t, path, `{"mcpServers": {"pw": {"command": "npx", "args": ["playwright"]}}}`)
	if err := m.SetEnabled("cursor", "pw", false); err != nil {
		t.Fatal(err)
	}
	write(t, path, `{"mcpServers": {"pw": {"command": "other"}}}`)
	if err := m.SetEnabled("cursor", "pw", true); err == nil {
		t.Error("the stashed server was written over the new one")
	}
	if !strings.Contains(read(t, path), "other") {
		t.Error("the new server was replaced")
	}
}

// TestAnEditDoneAgainWhenTheFileChangedUnderIt: a file another program
// writes between tend's read and tend's write is read again and the edit
// done on it, so that program's change is kept. If it regresses, tend
// writes back a version of ~/.claude.json from before Claude's last write.
func TestAnEditDoneAgainWhenTheFileChangedUnderIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.json")
	write(t, path, `{"a": 1}`)
	calls := 0
	err := editFile(path, writeOpts{}, func(data []byte) ([]byte, error) {
		calls++
		if calls == 1 {
			// Another program writes while this edit is being made.
			write(t, path, `{"a": 1, "theirs": true}`)
		}
		return editServers(data, "mcpServers", func(s map[string]json.RawMessage) error {
			s["mine"] = json.RawMessage(`{}`)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "theirs") || !strings.Contains(got, "mine") || calls != 2 {
		t.Errorf("after %d tries the file is %s", calls, got)
	}
}

// TestAWriteKeepsTheModeFollowsSymlinksAndBacksUp: the file keeps its mode,
// a symlinked file stays a symlink with its target written, and the day's
// first version is kept as a backup. If it regresses, ~/.claude.json
// becomes readable by others, a dotfiles link is replaced by a copy, or a
// bad edit cannot be undone.
func TestAWriteKeepsTheModeFollowsSymlinksAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	write(t, target, `{"mcpServers": {}}`)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	err := editFile(link, writeOpts{backups: backups, backupName: "x-real.json"}, func(data []byte) ([]byte, error) {
		return editServers(data, "mcpServers", func(s map[string]json.RawMessage) error {
			s["new"] = json.RawMessage(`{}`)
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a file")
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 || !strings.Contains(read(t, target), "new") {
		t.Errorf("target mode %v, content %s", fi.Mode().Perm(), read(t, target))
	}
	kept, _ := filepath.Glob(filepath.Join(backups, "x-real.json.*"))
	if len(kept) != 1 || strings.Contains(read(t, kept[0]), "new") {
		t.Errorf("backups %v", kept)
	}
}

// TestNamesDifferingOnlyInCaseAreOneServer: Context7 in one agent and
// context7 in another are one row. If it regresses, the same server is
// listed once per agent spelling.
func TestNamesDifferingOnlyInCaseAreOneServer(t *testing.T) {
	if Key("Context7 ") != Key("context7") {
		t.Error("the keys differ")
	}
	a := Def{Transport: HTTP, URL: "http://x"}
	b := Def{Transport: HTTP, URL: "http://y"}
	if a.Fingerprint() == b.Fingerprint() || a.Fingerprint() != (Def{Transport: HTTP, URL: "http://x"}).Fingerprint() {
		t.Error("fingerprints do not tell definitions apart")
	}
}

// TestAnUnreadableFileIsSaidNotGuessed: a JSONC file with comments is not
// rewritten, and the agent is listed with the reason. If it regresses, tend
// strips a user's comments, or the whole list fails over one file.
func TestAnUnreadableFileIsSaidNotGuessed(t *testing.T) {
	home, m := fakeHome(t)
	path := filepath.Join(home, ".cursor", "mcp.json")
	write(t, path, "{\n  // mine\n  \"mcpServers\": {}\n}\n")
	snap := m.List()
	for _, a := range snap.Agents {
		if a.ID == "cursor" && !strings.Contains(a.Error, "not plain JSON") {
			t.Errorf("cursor error = %q", a.Error)
		}
	}
	if err := m.Add("cursor", "x", Def{Transport: HTTP, URL: "http://x"}); err == nil {
		t.Error("a file with comments was rewritten")
	}
	if !strings.Contains(read(t, path), "// mine") {
		t.Error("the comment is gone")
	}
	if err := m.Remove("cursor", "nothing"); !errors.Is(err, ErrNoServer) && err == nil {
		t.Error("removing nothing succeeded")
	}
}
