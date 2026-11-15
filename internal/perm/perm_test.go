package perm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHome points every agent at a fresh home, and returns a manager over
// the agents with backups under it, and a project folder in it.
func fakeHome(t *testing.T) (*Manager, string, Place) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "CURSOR_CONFIG_DIR", "GEMINI_CLI_HOME", "XDG_CONFIG_HOME", "DSH_HOME"} {
		t.Setenv(v, "")
	}
	// No codex to ask in the unit tests: its check is tested on its own.
	t.Setenv("PATH", "/usr/bin:/bin")
	proj := filepath.Join(h, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return NewManager(filepath.Join(h, "state"), nil), h, Place{Root: proj, Dir: proj}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
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

func report(t *testing.T, m *Manager, p Place, id string) Report {
	t.Helper()
	for _, r := range m.List(p).Agents {
		if r.ID == id {
			if r.Error != "" {
				t.Fatalf("%s: %s", id, r.Error)
			}
			return r
		}
	}
	t.Fatalf("no agent %s", id)
	return Report{}
}

func rulesIn(r Report, file string) []string {
	var out []string
	for _, rule := range r.Rules {
		if rule.File == file {
			out = append(out, string(rule.Decision)+" "+rule.Text)
		}
	}
	return out
}

// TestClaudeRulesAreEditedWithoutTouchingTheRestOfTheFile: settings.json
// holds hooks, the status line and plugins too; a permission edit that
// rewrote or reordered them would break the person's setup.
func TestClaudeRulesAreEditedWithoutTouchingTheRestOfTheFile(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".claude", "settings.json")
	write(t, path, `{
  "statusLine": {"type": "command", "command": "x && y"},
  "permissions": {"allow": ["Bash(git log:*)"], "defaultMode": "default"},
  "hooks": {}
}
`)
	if err := m.Add(p, "claude", path, Deny, "Bash(rm -rf *)"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDecision(p, "claude", path, "Bash(git log:*)", Allow, Ask); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	for _, want := range []string{`"command": "x && y"`, `"deny": [`, `"Bash(rm -rf *)"`, `"ask": [`, `"defaultMode": "default"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("%s missing from:\n%s", want, got)
		}
	}
	if strings.Index(got, "statusLine") > strings.Index(got, "hooks") {
		t.Fatalf("keys reordered:\n%s", got)
	}
	if strings.Contains(got, `"allow"`) {
		t.Fatalf("an emptied list was left behind:\n%s", got)
	}
	// Removing what was added leaves the lists as they were.
	if err := m.Remove(p, "claude", path, Deny, "Bash(rm -rf *)"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "deny") {
		t.Fatal("the deny list stayed after its last rule went")
	}
}

// TestARuleTheAgentChangedMeanwhileIsNotTouched: the panel names a rule by
// what it says, so a rule Claude rewrote since the panel read it is
// reported, never a different one edited in its place.
func TestARuleTheAgentChangedMeanwhileIsNotTouched(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".claude", "settings.json")
	write(t, path, `{"permissions": {"allow": ["Bash(ls)"]}}`)
	if err := m.Remove(p, "claude", path, Allow, "Bash(pwd)"); !errors.Is(err, ErrNoRule) {
		t.Fatalf("got %v", err)
	}
	if err := m.Add(p, "claude", path, Allow, "Bash(ls)"); !errors.Is(err, ErrExists) {
		t.Fatalf("got %v", err)
	}
}

// TestOnlyTheAgentsFilesAreWritten: the client names the file; the server
// writes nothing that is not one of the agent's in this project.
func TestOnlyTheAgentsFilesAreWritten(t *testing.T) {
	m, h, p := fakeHome(t)
	other := filepath.Join(h, ".bashrc")
	write(t, other, "x")
	if err := m.Add(p, "claude", other, Allow, "Bash(ls)"); !errors.Is(err, ErrNotTheirs) {
		t.Fatalf("got %v", err)
	}
	if read(t, other) != "x" {
		t.Fatal("a file that is not the agent's was written")
	}
}

// TestClaudeRuleSyntaxIsCheckedBeforeWriting: a rule Claude refuses would
// be written and then ignored, with the person thinking it applies.
func TestClaudeRuleSyntaxIsCheckedBeforeWriting(t *testing.T) {
	for _, bad := range []string{"bash(ls)", "Bash()", "mcp__x(y)", "Bash(ls", "Bash(:*)"} {
		if checkClaude(bad) == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	for _, good := range []string{"Bash(npm run test:*)", "Read(//etc/**)", "WebSearch", "mcp__siyuan__search", "WebFetch(domain:x.com)"} {
		if err := checkClaude(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	if whyClaude("Write(src/**)") == "" {
		t.Error("a Write rule, which Claude does not check, was not flagged")
	}
}

// TestMovingARuleNeverLosesIt: written to the new file, then taken from the
// old — a "project" rule made global ends up in global alone.
func TestMovingARuleNeverLosesIt(t *testing.T) {
	m, h, p := fakeHome(t)
	local := filepath.Join(p.Root, ".claude", "settings.local.json")
	global := filepath.Join(h, ".claude", "settings.json")
	write(t, local, `{"permissions": {"allow": ["Bash(make test)"]}}`)
	if err := m.Move(p, "claude", local, global, Allow, "Bash(make test)"); err != nil {
		t.Fatal(err)
	}
	r := report(t, m, p, "claude")
	if got := rulesIn(r, global); len(got) != 1 || got[0] != "allow Bash(make test)" {
		t.Fatalf("global: %v", got)
	}
	if got := rulesIn(r, local); len(got) != 0 {
		t.Fatalf("local kept: %v", got)
	}
}

// TestTheSameRuleInTwoFilesIsMarked: the leftovers of "always allow" in a
// project, once the rule is global, are what the mark points at.
func TestTheSameRuleInTwoFilesIsMarked(t *testing.T) {
	m, h, p := fakeHome(t)
	write(t, filepath.Join(h, ".claude", "settings.json"), `{"permissions": {"allow": ["Bash(ls)"]}}`)
	write(t, filepath.Join(p.Root, ".claude", "settings.local.json"), `{"permissions": {"allow": ["Bash(ls)", "Bash(pwd)"]}}`)
	marked := 0
	for _, r := range report(t, m, p, "claude").Rules {
		if r.Elsewhere {
			marked++
		}
	}
	if marked != 2 {
		t.Fatalf("%d marked, want the two Bash(ls)", marked)
	}
}

// TestCodexRulesAreEditedALineAtATime: default.rules is Codex's own, one
// prefix_rule a line; removing one leaves the others byte for byte, and a
// rule written over several lines is shown, not edited.
func TestCodexRulesAreEditedALineAtATime(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".codex", "rules", "default.rules")
	write(t, path, `# mine
prefix_rule(pattern=["gh", "run", "view"], decision="allow")
prefix_rule(
    pattern = ["git", ["push", "fetch"]],
    decision = "prompt",
    justification = "network",
)
prefix_rule(pattern=["npm", "run", "build"], decision="allow")
`)
	r := report(t, m, p, "codex")
	got := rulesIn(r, path)
	if len(got) != 3 || got[0] != "allow gh run view" || got[2] != "allow npm run build" {
		t.Fatalf("rules: %v", got)
	}
	if !r.Rules[1].ReadOnly || r.Rules[1].Decision != Ask {
		t.Fatalf("multi-line rule: %+v", r.Rules[1])
	}
	if err := m.Remove(p, "codex", path, Allow, "gh run view"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDecision(p, "codex", path, "npm run build", Allow, Deny); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(p, "codex", path, Allow, `git status "--short"`); err != nil {
		t.Fatal(err)
	}
	want := `# mine
prefix_rule(
    pattern = ["git", ["push", "fetch"]],
    decision = "prompt",
    justification = "network",
)
prefix_rule(pattern=["npm", "run", "build"], decision="forbidden")
prefix_rule(pattern=["git", "status", "--short"], decision="allow")
`
	if got := read(t, path); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if err := m.Remove(p, "codex", path, Ask, r.Rules[1].Text); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("a multi-line rule was edited: %v", err)
	}
}

// TestCodexAlternativesRoundTrip: {a|b} is a token with alternatives,
// written as Codex's nested list.
func TestCodexAlternativesRoundTrip(t *testing.T) {
	pattern, err := codexPattern(`git {push|fetch} "a b"`)
	if err != nil {
		t.Fatal(err)
	}
	if pattern != `["git", ["push", "fetch"], "a b"]` {
		t.Fatalf("pattern %s", pattern)
	}
	text, ok := codexPatternText(pattern)
	if !ok || text != `git {push|fetch} "a b"` {
		t.Fatalf("text %q", text)
	}
}

// TestCodexModesChangeOneKeyOfConfigToml: config.toml holds MCP servers
// with tokens and every project's trust; setting approval_policy must
// change that key and nothing else.
func TestCodexModesChangeOneKeyOfConfigToml(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".codex", "config.toml")
	orig := `model = "gpt-5"

[mcp_servers.x]
url = "https://x"

[projects."/elsewhere"]
trust_level = "trusted"
`
	write(t, path, orig)
	if err := m.SetMode(p, "codex", path, "approval_policy", "on-request"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMode(p, "codex", path, codexTrustKey(p.Root), "trusted"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.HasPrefix(got, "model = \"gpt-5\"\n\napproval_policy = \"on-request\"\n[mcp_servers.x]") &&
		!strings.Contains(got, `approval_policy = "on-request"`) {
		t.Fatalf("approval_policy not written:\n%s", got)
	}
	if !strings.Contains(got, "[projects."+`"`+p.Root+`"`+"]\ntrust_level = \"trusted\"\n") {
		t.Fatalf("trust not written:\n%s", got)
	}
	for _, mode := range report(t, m, p, "codex").Modes {
		if mode.Key == "approval_policy" && mode.Value != "on-request" {
			t.Fatalf("read back %q", mode.Value)
		}
	}
	if err := m.SetMode(p, "codex", path, "approval_policy", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.SetMode(p, "codex", path, codexTrustKey(p.Root), ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "approval_policy") {
		t.Fatalf("unset left the key:\n%s", read(t, path))
	}
}

// TestCursorSaysWhenItsListIsNotRead: with approvalMode unrestricted —
// this machine's — Cursor never consults the allowlist, and rules added
// there would do nothing.
func TestCursorSaysWhenItsListIsNotRead(t *testing.T) {
	m, h, p := fakeHome(t)
	write(t, filepath.Join(h, ".cursor", "cli-config.json"), `{"approvalMode": "unrestricted", "permissions": {"allow": ["Shell(ls)"], "deny": []}}`)
	r := report(t, m, p, "cursor")
	if len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "unrestricted") {
		t.Fatalf("warnings %v", r.Warnings)
	}
	if err := m.Add(p, "cursor", filepath.Join(h, ".cursor", "cli-config.json"), Ask, "Shell(git)"); !errors.Is(err, ErrDecision) {
		t.Fatalf("Cursor has no ask, got %v", err)
	}
}

// TestOpenCodeKeepsTheOrderItsRulesDependOn: OpenCode applies the last
// rule that matches; a new rule goes last, where it wins, and a rule
// covered by one after it is flagged.
func TestOpenCodeKeepsTheOrderItsRulesDependOn(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".config", "opencode", "opencode.json")
	write(t, path, `{"$schema": "https://opencode.ai/config.json", "permission": {"bash": {"git push *": "deny", "*": "ask"}, "edit": "allow"}}`)
	r := report(t, m, p, "opencode")
	got := rulesIn(r, path)
	if strings.Join(got, ",") != "deny bash(git push *),ask bash(*),allow edit" {
		t.Fatalf("rules %v", got)
	}
	if r.Rules[0].Why == "" {
		t.Fatal("git push *, covered by * after it, was not flagged")
	}
	if err := m.Add(p, "opencode", path, Allow, "bash(git status *)"); err != nil {
		t.Fatal(err)
	}
	if err := m.Add(p, "opencode", path, Ask, "webfetch"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDecision(p, "opencode", path, "edit", Allow, Ask); err != nil {
		t.Fatal(err)
	}
	got = rulesIn(report(t, m, p, "opencode"), path)
	want := "deny bash(git push *),ask bash(*),allow bash(git status *),ask edit,ask webfetch"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
	// $schema still first, and the file still on the one line it was on:
	// only the rules changed were written (issue #61).
	if text := read(t, path); !strings.HasPrefix(text, `{"$schema"`) || strings.Count(strings.TrimSpace(text), "\n") != 0 {
		t.Fatalf("$schema moved, or the file was laid out again:\n%s", text)
	}
	if err := m.Add(p, "opencode", path, Allow, "webfetch(x.com)"); err == nil {
		t.Fatal("webfetch took a pattern")
	}
}

// TestOpenCodeOneActionBecomesStarFirst: `"permission": "ask"` and a tool
// with one action keep their meaning when a pattern is added: the action
// becomes "*", first.
func TestOpenCodeOneActionBecomesStarFirst(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".config", "opencode", "opencode.json")
	write(t, path, `{"permission": {"bash": "ask"}}`)
	if err := m.Add(p, "opencode", path, Allow, "bash(ls *)"); err != nil {
		t.Fatal(err)
	}
	got := rulesIn(report(t, m, p, "opencode"), path)
	if strings.Join(got, ",") != "ask bash(*),allow bash(ls *)" {
		t.Fatalf("got %v", got)
	}
}

// TestAJSONCFileIsShownButNotWritten: tend would lose its comments.
func TestAJSONCFileIsShownButNotWritten(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".config", "opencode", "opencode.jsonc")
	write(t, path, "{\n // mine\n \"permission\": {\"edit\": \"ask\"}\n}\n")
	if err := m.Add(p, "opencode", path, Allow, "bash"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("got %v", err)
	}
}

// TestGeminiListsAndPoliciesAreRead: settings.json's lists are rules tend
// edits; a policy file's rules are shown read-only, with their priority.
func TestGeminiListsAndPoliciesAreRead(t *testing.T) {
	m, h, p := fakeHome(t)
	settings := filepath.Join(h, ".gemini", "settings.json")
	write(t, settings, `{"tools": {"allowed": ["run_shell_command(git)"]}}`)
	write(t, filepath.Join(h, ".gemini", "policies", "auto-saved.toml"), `[[rule]]
toolName = "run_shell_command"
commandPrefix = "rm -rf"
decision = "deny"
priority = 100
`)
	r := report(t, m, p, "gemini")
	if len(r.Rules) != 2 || r.Rules[1].Text != "run_shell_command(rm -rf)" || !r.Rules[1].ReadOnly || r.Rules[1].Decision != Deny {
		t.Fatalf("rules %+v", r.Rules)
	}
	if err := m.Add(p, "gemini", settings, Ask, "write_file"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, settings), `"confirmationRequired": [`) {
		t.Fatal(read(t, settings))
	}
}

// TestDshPresetKeepsTheFilesComments: settings.yaml is written by hand
// with comments explaining it; setting the preset keeps them.
func TestDshPresetKeepsTheFilesComments(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".dsh", "settings.yaml")
	write(t, path, "# the local model\nagent-default-model:\n  provider: bonsai\n")
	if err := m.SetMode(p, "dsh", path, "permission.defaultPreset", "read-only"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !strings.Contains(got, "# the local model") || !strings.Contains(got, "defaultPreset: read-only") {
		t.Fatalf("got:\n%s", got)
	}
	if err := m.SetMode(p, "dsh", path, "permission.defaultPreset", "yolo"); err == nil {
		t.Fatal("an unknown preset was written")
	}
	if err := m.SetMode(p, "dsh", path, "permission.defaultPreset", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, path), "permission") {
		t.Fatalf("unset left the key:\n%s", read(t, path))
	}
}

// TestABackupIsKeptBeforeTheFirstChangeOfTheDay: a permission edit is
// undone from the backup when it went wrong.
func TestABackupIsKeptBeforeTheFirstChangeOfTheDay(t *testing.T) {
	m, h, p := fakeHome(t)
	path := filepath.Join(h, ".claude", "settings.json")
	write(t, path, `{"permissions": {"allow": ["Bash(ls)"]}}`)
	if err := m.Add(p, "claude", path, Allow, "Bash(pwd)"); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(filepath.Join(h, "state", "permissions", "backups", "claude-settings.json-*"))
	if len(backups) != 1 || read(t, backups[0]) != `{"permissions": {"allow": ["Bash(ls)"]}}` {
		t.Fatalf("backups %v", backups)
	}
}

// TestProjectDirsGoFromTheRootDown: Cursor and OpenCode read every folder
// between the repository's root and where they run.
func TestProjectDirsGoFromTheRootDown(t *testing.T) {
	got := projectDirs(Place{Root: "/r", Dir: "/r/a/b"})
	if strings.Join(got, ",") != "/r,/r/a,/r/a/b" {
		t.Fatalf("got %v", got)
	}
	if got := projectDirs(Place{Root: "/r", Dir: "/elsewhere"}); len(got) != 1 {
		t.Fatalf("outside the root: %v", got)
	}
}

// TestAnAgentTendDoesNotKnowIsListedFromTheConfig: the way a new harness
// gets into the panel before it has an adapter.
func TestAnAgentTendDoesNotKnowIsListedFromTheConfig(t *testing.T) {
	_, h, p := fakeHome(t)
	m := NewManager(filepath.Join(h, "state"), []Extra{{Agent: "Kimi", Path: "~/.kimi/perm.json"}})
	r := report(t, m, p, "extra:Kimi")
	if len(r.Files) != 1 || r.Files[0].Path != filepath.Join(h, ".kimi", "perm.json") || !r.Files[0].ReadOnly {
		t.Fatalf("files %+v", r.Files)
	}
}

// TestCodexPutsBackAFileItCannotRead: the real codex parses the file after
// the change; a change it cannot read is undone rather than left to break
// Codex's next start. Needs codex installed.
func TestCodexPutsBackAFileItCannotRead(t *testing.T) {
	path0, home0 := os.Getenv("PATH"), os.Getenv("HOME")
	_, h, _ := fakeHome(t)
	t.Setenv("PATH", path0)
	if os.Getenv("ASDF_DATA_DIR") == "" {
		// An asdf shim finds its versions under the real home.
		t.Setenv("ASDF_DATA_DIR", filepath.Join(home0, ".asdf"))
	}
	// And the version it runs from the real home's .tool-versions.
	if data, err := os.ReadFile(filepath.Join(home0, ".tool-versions")); err == nil {
		write(t, filepath.Join(h, ".tool-versions"), string(data))
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex is not installed")
	}
	path := filepath.Join(h, "default.rules")
	orig := "prefix_rule(pattern=[\"ls\"], decision=\"allow\")\n"
	write(t, path, orig)
	c := &codexAgent{backups: filepath.Join(h, "backups")}
	err := c.editRules(path, func(lines []string) ([]string, error) {
		return append(lines, "prefix_rule(pattern=[\"x\" \"y\"], decision=\"allow\")\n"), nil
	})
	if err == nil || !strings.Contains(err.Error(), "put back") {
		t.Fatalf("got %v", err)
	}
	if read(t, path) != orig {
		t.Fatalf("not put back:\n%s", read(t, path))
	}
	// A file Codex could not read before is not "put back" by a change.
	broken := orig + "prefix_rule(\n"
	write(t, path, broken)
	if err := c.editRules(path, func(lines []string) ([]string, error) { return append(lines, "# note\n"), nil }); err != nil {
		t.Fatalf("got %v", err)
	}
}
