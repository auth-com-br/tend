package main

import (
	"reflect"
	"testing"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// TestTheAddLineReadsTheThreeShapes: a URL is streamable HTTP with its
// headers, "sse" before one is the older transport, and anything else is a
// command with its env, quotes kept as a shell keeps them. If it regresses,
// adding a server means editing a file by hand.
func TestTheAddLineReadsTheThreeShapes(t *testing.T) {
	name, d, err := parseMCPSpec(`siyuan https://h:6806/mcp -H 'Authorization: Bearer x y'`)
	if err != nil || name != "siyuan" || d.Transport != "http" || d.URL != "https://h:6806/mcp" || d.Headers["Authorization"] != "Bearer x y" {
		t.Errorf("http: %q %+v %v", name, d, err)
	}
	if _, d, err := parseMCPSpec("old sse http://h/sse"); err != nil || d.Transport != "sse" {
		t.Errorf("sse: %+v %v", d, err)
	}
	name, d, err = parseMCPSpec(`fs MODE=ro TOKEN="a b" -- npx -y server-fs "/my dir"`)
	want := proto.MCPDef{Transport: "stdio", Command: "npx", Args: []string{"-y", "server-fs", "/my dir"}, Env: map[string]string{"MODE": "ro", "TOKEN": "a b"}}
	if err != nil || name != "fs" || !reflect.DeepEqual(d, want) {
		t.Errorf("stdio: %q %+v %v", name, d, err)
	}
	if _, d, err := parseMCPSpec("c7 npx -y @upstash/context7-mcp"); err != nil || d.Command != "npx" || len(d.Args) != 2 {
		t.Errorf("without --: %+v %v", d, err)
	}
	for _, bad := range []string{"", "lonely", "x sse", "x https://h -Z y", `x "open`} {
		if _, _, err := parseMCPSpec(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// TestTheGridIsOneRowAServerAndMarksTheOddOneOut: servers of one name are
// one row whatever their case; each agent's cell says on, off or absent;
// the agent whose server is not the one most have is marked; an agent with
// neither servers nor a place for them gets no column. If it regresses,
// the same server shows once per spelling, or a different server under a
// known name passes for the same one.
func TestTheGridIsOneRowAServerAndMarksTheOddOneOut(t *testing.T) {
	list := proto.MCPListResult{
		Agents: []proto.MCPAgent{{ID: "claude", Present: true}, {ID: "codex", Present: true}, {ID: "cursor", Present: true}, {ID: "gemini"}},
		Entries: []proto.MCPEntry{
			{Agent: "claude", Name: "Context7", Enabled: true, Fingerprint: "a", Transport: "stdio"},
			{Agent: "codex", Name: "Context7", Enabled: false, Fingerprint: "a", Transport: "stdio"},
			{Agent: "cursor", Name: "context7", Enabled: true, Fingerprint: "b", Transport: "stdio"},
		},
	}
	labels, cols, rows, keys, _ := mcpGrid(list)
	if !reflect.DeepEqual(cols, []string{"claude", "codex", "cursor"}) || len(labels) != 3 {
		t.Fatalf("columns %v", cols)
	}
	if len(rows) != 1 || keys[0] != "context7" {
		t.Fatalf("rows %+v keys %v", rows, keys)
	}
	r := rows[0]
	if !reflect.DeepEqual(r.Cells, []ui.MCPCell{ui.MCPOn, ui.MCPOff, ui.MCPOn}) || !reflect.DeepEqual(r.Differs, []bool{false, false, true}) {
		t.Errorf("cells %v differs %v", r.Cells, r.Differs)
	}
}
