//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMCPServer is a streamable HTTP MCP server with one tool, answering in
// plain JSON.
func fakeMCPServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		if len(m.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := `{"tools":[{"name":"only"}]}`
		if m.Method == "initialize" {
			result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fakeweb","version":"1"}}`
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, m.ID, result)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheMCPManagerTestsAndSwitchesAServer: prefix+M lists the server the
// two agents share as one row; t tests it and says what answered; space
// on Claude's cell takes it out of ~/.claude.json, kept by tend, and space
// again puts it back. If it regresses, the MCP manager cannot be reached,
// its test does not reach a server, or a switch loses a server.
func TestTheMCPManagerTestsAndSwitchesAServer(t *testing.T) {
	srv := fakeMCPServer(t)
	// Each agent's settings are pointed somewhere of the test's own by the
	// variable the agent itself reads, rather than by moving HOME: a session
	// with an empty home takes seconds longer to shut down.
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	claude := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(claude, []byte(`{"mcpServers": {"web": {"type": "http", "url": "`+srv.URL+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[mcp_servers.web]\nurl = \""+srv.URL+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := startSession(t, 120, 30)
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	a.send(t, "\x02M")
	a.waitForScreen(t, "the MCP manager with the shared server", func(s string) bool {
		return strings.Contains(s, "MCP SERVERS") && strings.Contains(s, "web") && strings.Contains(s, "1 servers in 2 agents")
	})

	a.send(t, "t")
	a.waitForScreen(t, "the test's result", func(s string) bool {
		return strings.Contains(s, "1 tools") && strings.Contains(s, "fakeweb")
	})

	a.send(t, "l") // Claude's column
	a.send(t, " ")
	a.waitForScreen(t, "the server off in Claude", func(s string) bool { return strings.Contains(s, "web is off in claude") })
	data, _ := os.ReadFile(claude)
	if strings.Contains(string(data), srv.URL) {
		t.Errorf("off left the server in Claude's file:\n%s", data)
	}

	a.send(t, " ")
	a.waitForScreen(t, "the server on again", func(s string) bool { return strings.Contains(s, "web is on in claude") })
	data, _ = os.ReadFile(claude)
	if !strings.Contains(string(data), srv.URL) {
		t.Errorf("on did not put the server back:\n%s", data)
	}

	a.send(t, "\x1b")
	a.waitForScreen(t, "the panel to close", func(s string) bool { return !strings.Contains(s, "MCP SERVERS") })
}

// TestTheMCPManagerIsFoundAndUsedWithoutKeys: the menu under the spaces
// leads to it ("MCP servers"), and to every tool ("all tools..."); in it, a
// right-click on a server offers what can be done in words, and choosing
// "turn off in claude" does it; ? explains the panel. If it regresses,
// somebody who knows no keys cannot find the MCP manager, or cannot use it
// once there.
func TestTheMCPManagerIsFoundAndUsedWithoutKeys(t *testing.T) {
	srv := fakeMCPServer(t)
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, ".cursor"))
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers": {"web": {"type": "http", "url": "`+srv.URL+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	a := startSession(t, 120, 34)
	a.waitForScreen(t, "the sidebar", func(s string) bool { return strings.Contains(s, "menu") })
	menu := func() {
		row := a.lineContaining(t, "menu")
		a.clickAt(t, columnOf(a.lines()[row-1], 'm')+1, row)
	}
	menu()
	a.waitForScreen(t, "the menu's tools", func(s string) bool {
		return strings.Contains(s, "all tools...") && strings.Contains(s, "MCP servers")
	})
	row := a.lineContaining(t, "all tools...")
	a.clickAt(t, runeIndex(a.lines()[row-1], "all tools")+1, row)
	a.waitForScreen(t, "every tool in words", func(s string) bool {
		return strings.Contains(s, "MCP servers — the tools your agents can use") && strings.Contains(s, "Agents — find and install")
	})
	a.send(t, "\x1b")
	a.waitForScreen(t, "the tools menu to close", func(s string) bool { return !strings.Contains(s, "─ tools ─") })
	menu()
	a.waitForScreen(t, "the menu again", func(s string) bool { return strings.Contains(s, "MCP servers") })
	row = a.lineContaining(t, "MCP servers")
	a.clickAt(t, runeIndex(a.lines()[row-1], "MCP servers")+1, row)
	a.waitForScreen(t, "the MCP manager", func(s string) bool { return strings.Contains(s, "MCP SERVERS") && strings.Contains(s, "✓ on") })

	row = a.lineContaining(t, "web")
	a.send(t, "\x1b[<2;"+itoa(runeIndex(a.lines()[row-1], "web")+1)+";"+itoa(row)+"M")
	a.waitForScreen(t, "the row's menu in words", func(s string) bool {
		return strings.Contains(s, "turn off in claude") && strings.Contains(s, "test it")
	})
	row = a.lineContaining(t, "turn off in claude")
	a.clickAt(t, runeIndex(a.lines()[row-1], "turn off")+1, row)
	a.waitForScreen(t, "the server off", func(s string) bool { return strings.Contains(s, "web is off in claude") })

	a.send(t, "?")
	a.waitForScreen(t, "the help", func(s string) bool { return strings.Contains(s, "WHAT IS THIS") })
}

// runeIndex is the screen column of text in a line: lines are full of
// box-drawing characters, so a byte index is not a column.
func runeIndex(line, text string) int {
	i := strings.Index(line, text)
	if i < 0 {
		return -1
	}
	return len([]rune(line[:i]))
}
