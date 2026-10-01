package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Gemini CLI keeps its servers in settings.json and whether each is on in
// a file beside it, mcp-server-enablement.json: {"<name>": {"enabled":
// false}} for a server turned off, keyed by the name lowercased and trimmed,
// and nothing for one that is on (McpServerEnablementManager, read from its
// bundle). Its settings may also exclude servers by name, which tend reads
// to explain an "off" but does not change.

type geminiAgent struct{ *jsonAgent }

func newGemini(backups string) Adapter {
	return &geminiAgent{&jsonAgent{
		id: "gemini", name: "Gemini CLI", key: "mcpServers",
		path: func() (string, error) {
			dir, err := geminiDir()
			return filepath.Join(dir, "settings.json"), err
		},
		toDef: func(obj map[string]json.RawMessage) (Def, []string) {
			extras := extrasOf(obj, "type", "command", "args", "env", "url", "httpUrl", "headers")
			if u := str(obj, "httpUrl"); u != "" {
				return Def{Transport: HTTP, URL: u, Headers: strMap(obj, "headers")}, extras
			}
			if u := str(obj, "url"); u != "" {
				t := SSE
				if str(obj, "type") == "http" {
					t = HTTP
				}
				return Def{Transport: t, URL: u, Headers: strMap(obj, "headers")}, extras
			}
			return Def{Transport: Stdio, Command: str(obj, "command"), Args: strs(obj, "args"), Env: strMap(obj, "env")}, extras
		},
		fromDef: func(d Def) map[string]any {
			switch d.Transport {
			case HTTP:
				obj := map[string]any{"httpUrl": d.URL}
				if len(d.Headers) > 0 {
					obj["headers"] = d.Headers
				}
				return obj
			case SSE:
				obj := map[string]any{"url": d.URL}
				if len(d.Headers) > 0 {
					obj["headers"] = d.Headers
				}
				return obj
			}
			obj := map[string]any{"command": d.Command, "args": nonNil(d.Args)}
			if len(d.Env) > 0 {
				obj["env"] = d.Env
			}
			return obj
		},
		transports: []Transport{Stdio, HTTP, SSE},
		backups:    backups,
	}}
}

func enablementPath() (string, error) {
	dir, err := geminiDir()
	return filepath.Join(dir, "mcp-server-enablement.json"), err
}

// disabledInGemini is the servers the enablement file has off, by Key.
func disabledInGemini() (map[string]bool, error) {
	path, err := enablementPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var state map[string]struct {
		Enabled *bool `json:"enabled"`
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("%s is not JSON: %w", path, err)
		}
	}
	off := map[string]bool{}
	for k, v := range state {
		if v.Enabled != nil && !*v.Enabled {
			off[Key(k)] = true
		}
	}
	return off, nil
}

// excludedInGemini is the servers its settings exclude (mcp.excluded, and
// the older excludeMCPServers), by Key.
func excludedInGemini(path string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var s struct {
		MCP struct {
			Excluded []string `json:"excluded"`
		} `json:"mcp"`
		ExcludeMCPServers []string `json:"excludeMCPServers"`
	}
	if json.Unmarshal(data, &s) != nil {
		return out
	}
	for _, n := range append(s.MCP.Excluded, s.ExcludeMCPServers...) {
		out[Key(n)] = true
	}
	return out
}

func (g *geminiAgent) List() ([]Entry, error) {
	entries, err := g.jsonAgent.List()
	if err != nil {
		return nil, err
	}
	off, err := disabledInGemini()
	if err != nil {
		return nil, err
	}
	path, _ := g.path()
	excluded := excludedInGemini(path)
	for i := range entries {
		switch k := Key(entries[i].Name); {
		case off[k]:
			entries[i].Enabled, entries[i].Via = false, "native"
		case excluded[k]:
			entries[i].Enabled = false
			entries[i].Reason = "excluded in Gemini's settings"
		}
	}
	return entries, nil
}

// SetEnabled writes the enablement file as Gemini does: off is the key
// with enabled false, on is no key at all.
func (g *geminiAgent) SetEnabled(name string, on bool) error {
	if _, _, err := g.Raw(name); err != nil {
		return err
	}
	return g.setEnablement(name, on)
}

func (g *geminiAgent) setEnablement(name string, on bool) error {
	path, err := enablementPath()
	if err != nil {
		return err
	}
	opts := writeOpts{backups: g.backups, backupName: "gemini-" + filepath.Base(path), mode: 0o644}
	return editFile(path, opts, func(data []byte) ([]byte, error) {
		state, err := decodeObject(data)
		if err != nil {
			return nil, fmt.Errorf("%s %w", path, err)
		}
		key := Key(name)
		if on {
			if _, ok := state[key]; !ok {
				return data, nil
			}
			delete(state, key)
		} else {
			state[key] = json.RawMessage(`{"enabled":false}`)
		}
		return encodeObject(state)
	})
}

// Remove also forgets whether the server was off, so a server added again
// later under the name does not come back switched off.
func (g *geminiAgent) Remove(name string) error {
	if err := g.jsonAgent.Remove(name); err != nil {
		return err
	}
	return g.setEnablement(name, true)
}
