package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The tools' screens are tested against a real server in a home of the
// test's own: every agent's files — ~/.claude.json, ~/.claude, Codex's —
// are read and written there, never the owner's.

// openInHome opens a session whose server, and every agent it reads,
// lives in a fresh home, with only the system's programs on PATH (no real
// agent is asked for its version), and returns the home.
func openInHome(t *testing.T) (*Session, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "CURSOR_CONFIG_DIR", "GEMINI_CLI_HOME", "DSH_HOME"} {
		t.Setenv(v, "")
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	return s, home
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seeText waits for a pane to show text, at the size the window's item
// for it would give it.
func seeText(t *testing.T, s *Session, pane uint64, text string) {
	t.Helper()
	if err := s.ResizePane(pane, 98, 38); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "pane "+text, func() bool { return strings.Contains(screenText(s, pane), text) })
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, _ := os.ReadFile(path)
	return string(b)
}

// TestTheMCPScreenSwitchesAddsAndRemovesAnAgentsServer: the screen lists
// Claude Code's servers from its file; a click on a cell turns one off,
// which the file then says; a server added is written after a yes, and one
// removed goes. If it regresses, the window's switches do nothing, or
// write somewhere the agent does not read.
func TestTheMCPScreenSwitchesAddsAndRemovesAnAgentsServer(t *testing.T) {
	s, home := openInHome(t)
	claudeJSON := filepath.Join(home, ".claude.json")
	writeFile(t, claudeJSON, `{"mcpServers": {"notes": {"type": "stdio", "command": "echo", "args": ["hi"]}}}`)

	s.OpenPanel(panelMCP)
	col := -1
	waitFor(t, "the servers", func() bool {
		m := s.State().MCP
		if m == nil || m.Loading || len(m.Rows) == 0 {
			return false
		}
		for i, a := range m.Agents {
			if a == "claude" {
				col = i
			}
		}
		return col >= 0 && m.Rows[0].Name == "notes" && m.Rows[0].Cells[col] == "on"
	})
	if err := s.MCP(MCPAction{Do: "toggle", Row: 0, Agent: col}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "notes off", func() bool {
		m := s.State().MCP
		return m != nil && len(m.Rows) > 0 && m.Rows[0].Cells[col] == "off"
	})
	if m := s.State().MCP; !strings.Contains(m.Message, "off in claude") {
		t.Errorf("turning it off says %q", m.Message)
	}

	if err := s.MCP(MCPAction{Do: "add", Agent: col, Text: "docs https://example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if q := s.State().MCP.Confirm; !strings.Contains(q, "add docs (http) to claude") {
		t.Fatalf("adding asks %q", q)
	}
	if err := s.MCP(MCPAction{Do: "answer", Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, claudeJSON), "https://example.com/mcp") {
		t.Errorf("the server added is not in the file:\n%s", readFile(t, claudeJSON))
	}
	var docs int
	waitFor(t, "docs listed", func() bool {
		m := s.State().MCP
		for i, r := range m.Rows {
			if r.Name == "docs" {
				docs = i
				return true
			}
		}
		return false
	})
	if err := s.MCP(MCPAction{Do: "remove", Row: docs, Agent: -1}); err != nil {
		t.Fatal(err)
	}
	if err := s.MCP(MCPAction{Do: "answer", Yes: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, claudeJSON), "example.com") {
		t.Errorf("the server removed is still in the file")
	}
}

// TestThePermissionsScreenChangesARulesDecisionAndAddsOne: Claude Code's
// global rule is listed under its tab; changing its decision moves it to
// the next of Claude's, in its file; a rule added goes to the file picked.
// If it regresses, a click on allow does not make it ask, or a new rule
// lands in a file the agent does not read.
func TestThePermissionsScreenChangesARulesDecisionAndAddsOne(t *testing.T) {
	s, home := openInHome(t)
	settings := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, settings, `{"permissions": {"allow": ["Bash(ls)"]}}`)

	s.OpenPanel(panelPerm)
	row := -1
	waitFor(t, "the rules", func() bool {
		p := s.State().Perm
		if p == nil || p.Loading {
			return false
		}
		for i, tab := range p.Tabs {
			if tab.Name == "Claude Code" && p.Tab != i {
				_ = s.Perm(PermAction{Do: "tab", Index: i})
				return false
			}
		}
		for i, l := range p.Lines {
			if !l.Mode && l.Text == "Bash(ls)" {
				row = i
			}
		}
		return row >= 0
	})
	if err := s.Perm(PermAction{Do: "change", Row: row}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, settings); !strings.Contains(got, `"ask"`) || strings.Contains(got, `"allow": [`+"\n"+`      "Bash(ls)"`) {
		t.Errorf("after a change the file says:\n%s", got)
	}

	p := s.State().Perm
	global := -1
	for i, f := range p.Files {
		if strings.HasPrefix(f, "global") {
			global = i
		}
	}
	if global < 0 {
		t.Fatalf("no global file among %v", p.Files)
	}
	if err := s.Perm(PermAction{Do: "add", Text: "Read(docs/**)", Decision: "deny", Index: global}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, settings); !strings.Contains(got, "Read(docs/**)") || !strings.Contains(got, `"deny"`) {
		t.Errorf("the rule added is not in the file:\n%s", got)
	}
	if m := s.State().Perm.Message; !strings.Contains(m, "added deny Read(docs/**)") {
		t.Errorf("adding says %q", m)
	}
}

// TestTheAgentManagerInstallsInATabOfItsOwn: the catalog is listed with the
// agents first, and an install runs its command in a new tab, which says
// how it went and goes on as a shell. If it regresses, an install runs
// out of sight, or the window stays on the list.
func TestTheAgentManagerInstallsInATabOfItsOwn(t *testing.T) {
	s, _ := openInHome(t)
	waitFor(t, "a pane", func() bool { return s.Focused() != 0 })
	s.OpenPanel(panelAgents)
	waitFor(t, "the catalog", func() bool {
		a := s.State().AgentsScreen
		return a != nil && !a.Loading && len(a.Agents) > 0
	})
	if a := s.State().AgentsScreen; a.Agents[0].Memory || !strings.Contains(a.Message, "agents installed on this machine") {
		t.Errorf("first %+v, line %q", a.Agents[0], a.Message)
	}
	tabs := len(s.State().Tabs)
	if err := s.installAgent(proto.AgentStatus{ID: "fake", Name: "Fake", InstallCommand: "echo fetched-it"}); err != nil {
		t.Fatal(err)
	}
	if st := s.State(); st.Panel != "" || len(st.Tabs) != tabs+1 {
		t.Fatalf("after an install: panel %q, %d tabs (was %d)", st.Panel, len(st.Tabs), tabs)
	}
	seeText(t, s, s.Focused(), "[tend] Fake installed")
}

// TestASessionIsResumedHandedOnAndDeleted: a conversation Claude Code
// keeps is listed with its title; resuming it opens a tab in the
// directory it was held in; handing it to another agent opens that agent's
// tab and puts the note first in the context, which opens; deleting asks,
// then removes the file. If it regresses, the window loses conversations
// or resumes them where the agent cannot find them.
func TestASessionIsResumedHandedOnAndDeleted(t *testing.T) {
	s, home := openInHome(t)
	waitFor(t, "a pane", func() bool { return s.Focused() != 0 })
	work := filepath.Join(home, "shop")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	conv := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(work, "/", "-"), id+".jsonl")
	writeFile(t, conv, `{"type":"ai-title","aiTitle":"Checkout button"}`+"\n"+
		`{"type":"user","cwd":"`+work+`","message":{"role":"user","content":"fix the checkout button"}}`+"\n"+
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Made it blue."}]}}`+"\n")

	s.OpenPanel(panelSessions)
	waitFor(t, "the conversation", func() bool {
		v := s.State().Sessions
		return v != nil && len(v.Shown) == 1 && v.Shown[0].Title == "Checkout button"
	})
	if err := s.Sessions(SessionsAction{Do: "query", Text: "nothing like it"}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.State().Sessions.Shown); n != 0 {
		t.Errorf("a search for something else shows %d", n)
	}
	_ = s.Sessions(SessionsAction{Do: "query", Text: "checkout"})

	tabs := len(s.State().Tabs)
	if err := s.Sessions(SessionsAction{Do: "resume", ID: id}); err != nil {
		t.Fatal(err)
	}
	if st := s.State(); st.Panel != "" || len(st.Tabs) != tabs+1 {
		t.Fatalf("resuming: panel %q, %d tabs (was %d)", st.Panel, len(st.Tabs), tabs)
	}
	// The agent is not on this PATH, so the pane goes on as the shell the
	// resume leaves, in the directory it was started in.
	pane := s.Focused()
	seeText(t, s, pane, "$")
	for _, k := range "pwd\r" {
		_ = s.Key(pane, int(k), 0, string(k))
	}
	seeText(t, s, pane, work)

	s.OpenPanel(panelSessions)
	waitFor(t, "the list again", func() bool { v := s.State().Sessions; return v != nil && len(v.Shown) == 1 })
	if err := s.Sessions(SessionsAction{Do: "continue", ID: id, Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	if p := s.State().Panel; p != panelContext {
		t.Fatalf("handing on opened %q, want the context", p)
	}
	waitFor(t, "the note in the context", func() bool {
		c := s.State().Context
		if c == nil || len(c.Items) == 0 {
			return false
		}
		for _, part := range c.Items[0].Parts {
			if part.Heading == "TEXT" && strings.Contains(part.Value, "First request: fix the checkout button") {
				return true
			}
		}
		return false
	})

	s.OpenPanel(panelSessions)
	waitFor(t, "the list once more", func() bool { v := s.State().Sessions; return v != nil && len(v.Shown) == 1 })
	if err := s.Sessions(SessionsAction{Do: "delete", ID: id}); err != nil {
		t.Fatal(err)
	}
	if q := s.State().Sessions.Confirm; q == "" {
		t.Fatal("deleting did not ask first")
	}
	if _, err := os.Stat(conv); err != nil {
		t.Fatal("the conversation went before the yes")
	}
	if err := s.Sessions(SessionsAction{Do: "answer", Yes: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(conv); !os.IsNotExist(err) {
		t.Errorf("the conversation is still there (%v)", err)
	}
}

// TestTheContextScreenListsSendsAndClears: an item captured is listed,
// newest first, and follows the buffer as it changes; send types it into
// the pane; clear asks twice. If it regresses, what the browser captured
// never reaches the agent from the window.
func TestTheContextScreenListsSendsAndClears(t *testing.T) {
	s, _ := openInHome(t)
	waitFor(t, "a pane", func() bool { return s.Focused() != 0 })
	s.OpenPanel(panelContext)
	waitFor(t, "the empty buffer", func() bool { c := s.State().Context; return c != nil && !c.Loading })
	if _, err := s.client.ContextAdd(proto.ContextItem{Kind: "text", Source: "cli", Text: "first-capture"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.client.ContextAdd(proto.ContextItem{Kind: "text", Source: "cli", Text: "echo sent-from-context"}); err != nil {
		t.Fatal(err)
	}
	var items []ContextRow
	waitFor(t, "both items, from the server's word", func() bool {
		items = s.State().Context.Items
		return len(items) == 2
	})
	if !strings.Contains(items[0].Summary, "sent-from-context") {
		t.Errorf("the newest is not first: %+v", items)
	}
	if err := s.Context(ContextAction{Do: "send", ID: items[0].ID}); err != nil {
		t.Fatal(err)
	}
	if p := s.State().Panel; p != "" {
		t.Errorf("sending left %q open", p)
	}
	seeText(t, s, s.Focused(), "echo sent-from-context")

	s.OpenPanel(panelContext)
	waitFor(t, "the items again", func() bool { c := s.State().Context; return c != nil && len(c.Items) == 2 })
	_ = s.Context(ContextAction{Do: "clear"})
	if c := s.State().Context; !c.ClearArmed || len(c.Items) != 2 {
		t.Fatalf("one clear emptied it, or did not ask: %+v", c)
	}
	_ = s.Context(ContextAction{Do: "clear"})
	waitFor(t, "an empty buffer", func() bool { return len(s.State().Context.Items) == 0 })
}

// TestTheGlobalMenuOffersTheToolsScreens: MCP, permissions, about and the
// tools menu are offered, the tools menu lists only screens the window has,
// and a pane's agent menu offers "continue in…". If it regresses, a menu
// item does nothing, or a screen is there but cannot be reached.
func TestTheGlobalMenuOffersTheToolsScreens(t *testing.T) {
	s, _ := openInHome(t)
	waitFor(t, "a pane", func() bool { return s.Focused() != 0 })
	s.OpenMenu("global", 0, 0, 0, "", 0, 0)
	items := strings.Join(s.State().Menu.Items, "|")
	for _, want := range []string{"about tend", "all tools...", "MCP servers", "agent permissions"} {
		if !strings.Contains(items, want) {
			t.Errorf("the global menu has no %q: %s", want, items)
		}
	}
	pick := func(label string) {
		t.Helper()
		for i, it := range s.State().Menu.Items {
			if strings.HasPrefix(it, label) {
				if err := s.PickMenu(i); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("no %q in %v", label, s.State().Menu.Items)
	}
	pick("all tools")
	tools := s.State().Menu.Items
	if len(tools) != len(toolPanels) {
		t.Errorf("the tools menu offers %v", tools)
	}
	pick("Sessions")
	if p := s.State().Panel; p != panelSessions {
		t.Errorf("the tools menu opened %q", p)
	}

	s.OpenMenu("global", 0, 0, 0, "", 0, 0)
	pick("about tend")
	if a := s.State().About; a == nil || a.Version == "" || a.Server == "" || a.Site != ui.SiteURL {
		t.Errorf("about says %+v", a)
	}

	m := ui.PaneMenu(1, 0, 0, false, true, false)
	if !strings.Contains(strings.Join(labels(withoutMissing(m)), "|"), "continue in") {
		t.Errorf("an agent's pane menu has no continue in: %v", labels(withoutMissing(m)))
	}
}

func labels(m ui.Menu) []string {
	var out []string
	for _, it := range m.Items {
		out = append(out, it.Label)
	}
	return out
}
