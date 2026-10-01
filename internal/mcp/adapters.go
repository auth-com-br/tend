package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/auth-com-br/tend/internal/integration"
)

// Adapter is one agent's MCP configuration, global (user) scope only: the
// servers a project adds of its own are not this version's.
type Adapter interface {
	ID() string
	Name() string
	// Path is the file the servers are kept in.
	Path() (string, error)
	// List is the servers in the file, each as on or off by the agent's own
	// switch. Servers tend has stashed are the manager's to add.
	List() ([]Entry, error)
	// Raw is a server's exact bytes, under the name it is kept by.
	Raw(name string) (string, json.RawMessage, error)
	// Add writes a new server; one of that name already there is refused.
	Add(name string, d Def) error
	// AddRaw writes a server back exactly as it was, from the stash.
	AddRaw(name string, raw json.RawMessage) error
	// Remove takes a server out of the file, and out of whatever the agent
	// keeps beside it about the server (Gemini's enablement file).
	Remove(name string) error
	// SetEnabled is the agent's own on/off switch; ErrNoSwitch when it has
	// none, and the manager takes the server out into the stash instead.
	SetEnabled(name string, on bool) error
	// Supports reports whether the agent can reach a server that way.
	Supports(t Transport) bool
}

var (
	// ErrNoSwitch is an agent with no on/off switch for one server that
	// holds everywhere (Claude Code's and Cursor's are per project).
	ErrNoSwitch = errors.New("this agent has no switch of its own")
	// ErrNoServer is a server the agent does not have.
	ErrNoServer = errors.New("no such server")
	// ErrExists is a server name the agent has already.
	ErrExists = errors.New("already has a server of that name")
)

// jsonAgent is an agent whose servers are an object in a JSON file. The
// differences between them are the shape of one server (toDef, fromDef),
// and whether the server carries its own on/off field.
type jsonAgent struct {
	id, name string
	path     func() (string, error)
	// key is the field the servers are under.
	key string
	// lockDir, when set, is the lock the agent takes on the file.
	lockDir func(path string) string
	// enabledField is the server's own on/off field, or "" for none.
	enabledField string
	toDef        func(obj map[string]json.RawMessage) (Def, []string)
	fromDef      func(d Def) map[string]any
	transports   []Transport
	backups      string
}

func (a *jsonAgent) ID() string            { return a.id }
func (a *jsonAgent) Name() string          { return a.name }
func (a *jsonAgent) Path() (string, error) { return a.path() }

func (a *jsonAgent) Supports(t Transport) bool {
	for _, x := range a.transports {
		if x == t {
			return true
		}
	}
	return false
}

func (a *jsonAgent) opts(path string) writeOpts {
	o := writeOpts{backups: a.backups, backupName: a.id + "-" + filepath.Base(path), mode: 0o600}
	if a.lockDir != nil {
		o.lockDir = a.lockDir(path)
	}
	return o
}

func (a *jsonAgent) List() ([]Entry, error) {
	path, err := a.path()
	if err != nil {
		return nil, err
	}
	servers, err := readServers(path, a.key)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for name, raw := range servers {
		obj, err := object(raw)
		if err != nil {
			out = append(out, Entry{Agent: a.id, Name: name, Enabled: true, Reason: "not an object; left as it is"})
			continue
		}
		d, extras := a.toDef(obj)
		e := Entry{Agent: a.id, Name: name, Enabled: true, Def: d, Extras: extras}
		if a.enabledField != "" {
			if on, ok := boolField(obj, a.enabledField); ok && !on {
				e.Enabled, e.Via = false, "native"
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func (a *jsonAgent) Raw(name string) (string, json.RawMessage, error) {
	path, err := a.path()
	if err != nil {
		return "", nil, err
	}
	servers, err := readServers(path, a.key)
	if err != nil {
		return "", nil, err
	}
	found := findName(servers, name)
	if found == "" {
		return "", nil, fmt.Errorf("%s: %w: %s", a.name, ErrNoServer, name)
	}
	return found, servers[found], nil
}

func (a *jsonAgent) Add(name string, d Def) error {
	if !a.Supports(d.Transport) {
		return fmt.Errorf("%s cannot reach a server over %s", a.name, d.Transport)
	}
	obj := a.fromDef(d)
	if a.enabledField != "" {
		obj[a.enabledField] = true
	}
	raw, err := marshal(obj)
	if err != nil {
		return err
	}
	return a.AddRaw(name, raw)
}

func (a *jsonAgent) AddRaw(name string, raw json.RawMessage) error {
	path, err := a.path()
	if err != nil {
		return err
	}
	return editFile(path, a.opts(path), func(data []byte) ([]byte, error) {
		return editServers(data, a.key, func(servers map[string]json.RawMessage) error {
			if findName(servers, name) != "" {
				return fmt.Errorf("%s %w: %s", a.name, ErrExists, name)
			}
			servers[name] = raw
			return nil
		})
	})
}

func (a *jsonAgent) Remove(name string) error {
	path, err := a.path()
	if err != nil {
		return err
	}
	return editFile(path, a.opts(path), func(data []byte) ([]byte, error) {
		return editServers(data, a.key, func(servers map[string]json.RawMessage) error {
			found := findName(servers, name)
			if found == "" {
				return fmt.Errorf("%s: %w: %s", a.name, ErrNoServer, name)
			}
			delete(servers, found)
			return nil
		})
	})
}

func (a *jsonAgent) SetEnabled(name string, on bool) error {
	if a.enabledField == "" {
		return ErrNoSwitch
	}
	path, err := a.path()
	if err != nil {
		return err
	}
	return editFile(path, a.opts(path), func(data []byte) ([]byte, error) {
		return editServers(data, a.key, func(servers map[string]json.RawMessage) error {
			found := findName(servers, name)
			if found == "" {
				return fmt.Errorf("%s: %w: %s", a.name, ErrNoServer, name)
			}
			obj, err := object(servers[found])
			if err != nil {
				return err
			}
			obj[a.enabledField] = json.RawMessage(fmt.Sprint(on))
			raw, err := marshal(obj)
			if err != nil {
				return err
			}
			servers[found] = raw
			return nil
		})
	})
}

// --- the agents ---------------------------------------------------------

// claudeJSON is Claude Code's ~/.claude.json, or the one in
// $CLAUDE_CONFIG_DIR when that is set: the file, not the ~/.claude
// directory, holds the user-scope servers.
func claudeJSON() (string, error) {
	if dir := os.Getenv(integration.ClaudeConfigDirEnv); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// opencodeJSON is OpenCode's global config: $XDG_CONFIG_HOME/opencode when
// that is set, as OpenCode finds its own (xdg-basedir), else
// ~/.config/opencode.
func opencodeJSON() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opencode", "opencode.json"), nil
	}
	dir, err := integration.OpencodeDir()
	return filepath.Join(dir, "opencode.json"), err
}

// geminiDir is Gemini CLI's settings directory: $GEMINI_CLI_HOME/.gemini,
// else ~/.gemini — its Storage.getGlobalGeminiDir, where GEMINI_CLI_HOME
// stands in for the home directory.
func geminiDir() (string, error) {
	home := os.Getenv("GEMINI_CLI_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = h
	}
	return filepath.Join(home, ".gemini"), nil
}

func newClaude(backups string) Adapter {
	return &jsonAgent{
		id: "claude", name: "Claude Code", path: claudeJSON, key: "mcpServers",
		lockDir: func(path string) string { return path + ".lock" },
		toDef: func(obj map[string]json.RawMessage) (Def, []string) {
			d := urlOrCommand(obj, str(obj, "type"))
			return d, extrasOf(obj, "type", "command", "args", "env", "url", "headers")
		},
		fromDef: func(d Def) map[string]any {
			if d.Transport == Stdio {
				return map[string]any{"type": "stdio", "command": d.Command, "args": nonNil(d.Args), "env": nonNilMap(d.Env)}
			}
			obj := map[string]any{"type": string(d.Transport), "url": d.URL}
			if len(d.Headers) > 0 {
				obj["headers"] = d.Headers
			}
			return obj
		},
		transports: []Transport{Stdio, HTTP, SSE},
		backups:    backups,
	}
}

func newCursor(backups string) Adapter {
	return &jsonAgent{
		id: "cursor", name: "Cursor", key: "mcpServers",
		path: func() (string, error) {
			dir, err := integration.CursorDir()
			return filepath.Join(dir, "mcp.json"), err
		},
		toDef: func(obj map[string]json.RawMessage) (Def, []string) {
			return urlOrCommand(obj, str(obj, "type")), extrasOf(obj, "type", "command", "args", "env", "url", "headers")
		},
		fromDef: func(d Def) map[string]any {
			if d.Transport == Stdio {
				return map[string]any{"command": d.Command, "args": nonNil(d.Args), "env": nonNilMap(d.Env)}
			}
			// Cursor tells the two HTTP transports apart itself.
			obj := map[string]any{"url": d.URL}
			if len(d.Headers) > 0 {
				obj["headers"] = d.Headers
			}
			return obj
		},
		transports: []Transport{Stdio, HTTP, SSE},
		backups:    backups,
	}
}

func newOpenCode(backups string) Adapter {
	return &jsonAgent{
		id: "opencode", name: "OpenCode", key: "mcp", enabledField: "enabled",
		path: opencodeJSON,
		toDef: func(obj map[string]json.RawMessage) (Def, []string) {
			extras := extrasOf(obj, "type", "command", "environment", "url", "headers", "enabled")
			if str(obj, "type") == "remote" || str(obj, "url") != "" {
				return Def{Transport: HTTP, URL: str(obj, "url"), Headers: strMap(obj, "headers")}, extras
			}
			cmd := strs(obj, "command")
			d := Def{Transport: Stdio, Env: strMap(obj, "environment")}
			if len(cmd) > 0 {
				d.Command, d.Args = cmd[0], cmd[1:]
			}
			return d, extras
		},
		fromDef: func(d Def) map[string]any {
			if d.Transport == Stdio {
				obj := map[string]any{"type": "local", "command": append([]string{d.Command}, d.Args...)}
				if len(d.Env) > 0 {
					obj["environment"] = d.Env
				}
				return obj
			}
			// OpenCode tries streamable HTTP and falls back to SSE itself.
			obj := map[string]any{"type": "remote", "url": d.URL}
			if len(d.Headers) > 0 {
				obj["headers"] = d.Headers
			}
			return obj
		},
		transports: []Transport{Stdio, HTTP, SSE},
		backups:    backups,
	}
}

// urlOrCommand reads the common shape: a command with args and env, or a
// URL with headers, and the transport named by typ when there is one.
func urlOrCommand(obj map[string]json.RawMessage, typ string) Def {
	if url := str(obj, "url"); url != "" || typ == "http" || typ == "sse" {
		t := HTTP
		if typ == "sse" {
			t = SSE
		}
		return Def{Transport: t, URL: url, Headers: strMap(obj, "headers")}
	}
	return Def{Transport: Stdio, Command: str(obj, "command"), Args: strs(obj, "args"), Env: strMap(obj, "env")}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
