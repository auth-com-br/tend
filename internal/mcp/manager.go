package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Manager is every agent's servers together: what the MCP manager shows,
// and what its switches do. It serialises its own writes; it holds no lock
// of the tend server's, since everything it does is I/O.
type Manager struct {
	mu       sync.Mutex
	adapters []Adapter
	stash    stash
}

// NewManager is the agents tend manages servers for, with the stash and the
// backups under stateDir/mcp.
func NewManager(stateDir string) *Manager {
	dir := filepath.Join(stateDir, "mcp")
	backups := filepath.Join(dir, "backups")
	return NewManagerWith(stash{path: filepath.Join(dir, "stash.json")},
		newClaude(backups), newCodex(backups), newGemini(backups), newCursor(backups), newOpenCode(backups))
}

// NewManagerWith is a manager over given adapters, for tests.
func NewManagerWith(s stash, adapters ...Adapter) *Manager {
	return &Manager{adapters: adapters, stash: s}
}

// Agent is one agent as the manager shows it: whether it has a file to keep
// servers in, and why it could not be read when it could not.
type Agent struct {
	ID, Name, Path string
	Present        bool
	Error          string
}

// Snapshot is everything the manager shows.
type Snapshot struct {
	Agents  []Agent
	Entries []Entry
}

// List reads every agent's servers, and the servers tend has stashed, as
// off. An agent whose file cannot be read is listed with the reason rather
// than taking the others down with it.
func (m *Manager) List() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	var snap Snapshot
	st, stashErr := m.stash.read()
	for _, a := range m.adapters {
		info := Agent{ID: a.ID(), Name: a.Name()}
		path, err := a.Path()
		if err == nil {
			info.Path = path
			// An agent with a directory of its own is installed even when it
			// has no servers yet, and is a place to copy one to.
			if _, err := os.Stat(filepath.Dir(path)); err == nil {
				info.Present = true
			}
		}
		entries, err := a.List()
		if err != nil {
			info.Error = err.Error()
		}
		snap.Entries = append(snap.Entries, entries...)
		if stashErr == nil {
			for _, e := range st.Entries {
				if e.Agent != a.ID() {
					continue
				}
				if hasName(entries, e.Name) {
					continue // back in the agent's file; the file wins
				}
				obj, _ := object(e.Raw)
				entry := Entry{Agent: e.Agent, Name: e.Name, Via: "stash"}
				entry.Def, entry.Extras = defFromRaw(a, obj)
				snap.Entries = append(snap.Entries, entry)
			}
		}
		snap.Agents = append(snap.Agents, info)
	}
	sort.SliceStable(snap.Entries, func(i, j int) bool {
		if Key(snap.Entries[i].Name) != Key(snap.Entries[j].Name) {
			return Key(snap.Entries[i].Name) < Key(snap.Entries[j].Name)
		}
		return snap.Entries[i].Agent < snap.Entries[j].Agent
	})
	return snap
}

func hasName(entries []Entry, name string) bool {
	for _, e := range entries {
		if Key(e.Name) == Key(name) {
			return true
		}
	}
	return false
}

// defFromRaw reads a stashed server's definition the way its agent reads
// its own.
func defFromRaw(a Adapter, obj map[string]json.RawMessage) (Def, []string) {
	switch ja := a.(type) {
	case *jsonAgent:
		return ja.toDef(obj)
	case *geminiAgent:
		return ja.toDef(obj)
	}
	return Def{}, nil
}

func (m *Manager) adapter(id string) (Adapter, error) {
	for _, a := range m.adapters {
		if a.ID() == id {
			return a, nil
		}
	}
	return nil, fmt.Errorf("tend does not manage %s's servers", id)
}

// Lookup is a server's definition in one agent, stashed or not: what a test
// starts or reaches.
func (m *Manager) Lookup(agent, name string) (Def, error) {
	for _, e := range m.List().Entries {
		if e.Agent == agent && Key(e.Name) == Key(name) {
			return e.Def, nil
		}
	}
	return Def{}, fmt.Errorf("%s: %w: %s", agent, ErrNoServer, name)
}

// SetEnabled turns a server on or off in one agent: by the agent's own
// switch where it has one, else through the stash.
//
// Off through the stash writes the stash first and takes the server out of
// the agent's file after, so a failure in between leaves the server in both
// rather than in neither. On refuses when the agent has a server of that
// name again, keeping the stashed one.
func (m *Manager) SetEnabled(agent, name string, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.adapter(agent)
	if err != nil {
		return err
	}
	err = a.SetEnabled(name, on)
	if !errors.Is(err, ErrNoSwitch) {
		return err
	}
	if on {
		e, ok, err := m.stash.take(agent, name)
		if err != nil {
			return err
		}
		if !ok {
			if _, _, err := a.Raw(name); err == nil {
				return nil // on already
			}
			return fmt.Errorf("%s: %w: %s", a.Name(), ErrNoServer, name)
		}
		if err := a.AddRaw(e.Name, e.Raw); err != nil {
			if errors.Is(err, ErrExists) {
				return fmt.Errorf("%s has a server called %s again; the one tend kept is still kept", a.Name(), e.Name)
			}
			return err
		}
		return m.stash.drop(agent, name)
	}
	found, raw, err := a.Raw(name)
	if err != nil {
		return err
	}
	path, _ := a.Path()
	if err := m.stash.put(stashed{Agent: agent, Name: found, Raw: raw, File: path, At: time.Now()}); err != nil {
		return fmt.Errorf("keeping %s before turning it off: %w", found, err)
	}
	return a.Remove(found)
}

// Add writes a new server into an agent.
func (m *Manager) Add(agent, name string, d Def) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.adapter(agent)
	if err != nil {
		return err
	}
	if _, ok, _ := m.stash.take(agent, name); ok {
		return fmt.Errorf("%s has %s switched off; turn it on rather than adding it again", a.Name(), name)
	}
	return a.Add(name, d)
}

// Copy writes a server one agent has into another, translated to the other
// agent's shape, on the server's side: its env values and headers never go
// anywhere else. The warnings say what did not come along.
func (m *Manager) Copy(fromAgent, name, toAgent string) ([]string, error) {
	var src *Entry
	for _, e := range m.List().Entries {
		if e.Agent == fromAgent && Key(e.Name) == Key(name) {
			e := e
			src = &e
		}
	}
	if src == nil {
		return nil, fmt.Errorf("%s: %w: %s", fromAgent, ErrNoServer, name)
	}
	if err := m.Add(toAgent, src.Name, src.Def); err != nil {
		return nil, err
	}
	var warn []string
	if len(src.Extras) > 0 {
		warn = append(warn, fmt.Sprintf("left behind %v, which only %s understands", src.Extras, fromAgent))
	}
	if usesVariables(src.Def) {
		warn = append(warn, "it uses ${…} variables, written as they were; check that "+toAgent+" reads them the same way")
	}
	return warn, nil
}

// Remove takes a server out of an agent, stashed or not.
func (m *Manager) Remove(agent, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.adapter(agent)
	if err != nil {
		return err
	}
	if _, ok, _ := m.stash.take(agent, name); ok {
		return m.stash.drop(agent, name)
	}
	return a.Remove(name)
}
