package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The stash is where a server is kept while it is off in an agent with no
// switch of its own that holds everywhere (Claude Code, Cursor): taken out
// of the agent's file, kept here exactly as it was, and put back exactly
// so. The owner chose this over the agents' undocumented per-project keys.
//
// It holds what the agents' files hold — env values and headers, tokens
// among them — so it is as private as they are: 0600 in a 0700 directory.

// stashed is one server kept here.
type stashed struct {
	Agent string          `json:"agent"`
	Name  string          `json:"name"`
	Raw   json.RawMessage `json:"raw"`
	File  string          `json:"file"`
	At    time.Time       `json:"at"`
}

type stashFile struct {
	Version int       `json:"version"`
	Entries []stashed `json:"entries"`
}

type stash struct{ path string }

func (s stash) read() (stashFile, error) {
	var f stashFile
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return stashFile{Version: 1}, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("%s is not readable: %w", s.path, err)
	}
	return f, nil
}

func (s stash) write(f stashFile) error {
	f.Version = 1
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return writeAtomic(s.path, append(raw, '\n'), 0o600)
}

// find is the stashed server of an agent with a name, by Key.
func (f stashFile) find(agent, name string) (int, bool) {
	for i, e := range f.Entries {
		if e.Agent == agent && Key(e.Name) == Key(name) {
			return i, true
		}
	}
	return -1, false
}

func (s stash) put(e stashed) error {
	f, err := s.read()
	if err != nil {
		return err
	}
	if i, ok := f.find(e.Agent, e.Name); ok {
		f.Entries[i] = e
	} else {
		f.Entries = append(f.Entries, e)
	}
	return s.write(f)
}

func (s stash) take(agent, name string) (stashed, bool, error) {
	f, err := s.read()
	if err != nil {
		return stashed{}, false, err
	}
	i, ok := f.find(agent, name)
	if !ok {
		return stashed{}, false, nil
	}
	return f.Entries[i], true, nil
}

func (s stash) drop(agent, name string) error {
	f, err := s.read()
	if err != nil {
		return err
	}
	i, ok := f.find(agent, name)
	if !ok {
		return nil
	}
	f.Entries = append(f.Entries[:i], f.Entries[i+1:]...)
	return s.write(f)
}
