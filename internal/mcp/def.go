// Package mcp manages the MCP servers of the coding agents tend runs: which
// ones each agent has, turning one off and on, adding and removing one, and
// testing one by speaking MCP to it.
//
// herdr has none of this; it is tend's own (docs/PORTING.md). Each agent
// keeps its servers in a file of its own, in a shape of its own, and the
// same server is usually set up in several of them — so the files are read
// and written here directly, one adapter per agent, and a server is shown
// once with the agents that have it.
//
// The files are the agents' and other programs write them too: Claude Code
// rewrites ~/.claude.json all the time. Every write goes through file.go,
// which takes the agent's own lock where it has one, refuses to write over
// a file that changed under it, writes atomically, keeps the file's mode,
// and leaves a backup of the day's first version.
package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// Transport is how a server is reached.
type Transport string

const (
	// Stdio is a program started by the agent, spoken to on its stdin and
	// stdout.
	Stdio Transport = "stdio"
	// HTTP is the streamable HTTP transport: requests POSTed to one URL.
	HTTP Transport = "http"
	// SSE is the older HTTP transport: a stream of events, and requests
	// POSTed to an address the stream names.
	SSE Transport = "sse"
)

// Def is a server as tend understands it, whichever agent it came from:
// enough to start it or reach it, and to write it into another agent.
type Def struct {
	Transport Transport         `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// Fingerprint tells two definitions apart without showing either: two
// agents with a server of the same name are marked when it is not the same
// server. Values go into it, so it is never sent anywhere it could be
// reversed from — it is a hash, and only its prefix is used.
func (d Def) Fingerprint() string {
	raw, _ := json.Marshal(d)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:6])
}

// Key is the row a server name belongs to: lowercased and trimmed, which is
// how Gemini itself compares server names (normalizeServerId), so Context7
// in one agent and context7 in another are one server.
func Key(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Entry is one server in one agent's configuration.
type Entry struct {
	Agent string
	Name  string
	// Enabled is whether the agent will start it.
	Enabled bool
	// Via is how it is off: "native" (the agent's own switch) or "stash"
	// (taken out of the agent's file and kept by tend). Empty when on.
	Via string
	Def Def
	// Extras are the agent's own settings for it that Def does not carry
	// (a timeout, a trust flag, OAuth), by name: what copying it to another
	// agent leaves behind.
	Extras []string
	// Reason says why it is off when that is not tend's to change, such as
	// a list of excluded servers in the agent's settings.
	Reason string
}

// extrasOf is the keys of an object that are not in known, sorted.
func extrasOf(obj map[string]json.RawMessage, known ...string) []string {
	var out []string
	for k := range obj {
		skip := false
		for _, kn := range known {
			if k == kn {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// usesVariables reports whether a definition refers to variables (${X},
// ${env:X}, {env:X}) — written the way one agent reads them, which another
// may not.
func usesVariables(d Def) bool {
	all := append([]string{d.Command, d.URL}, d.Args...)
	for _, v := range d.Env {
		all = append(all, v)
	}
	for _, v := range d.Headers {
		all = append(all, v)
	}
	for _, s := range all {
		if strings.Contains(s, "${") || strings.Contains(s, "{env:") {
			return true
		}
	}
	return false
}
