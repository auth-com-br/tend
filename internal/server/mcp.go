package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/mcp"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
)

// The MCP manager's methods: the agents' MCP servers, read, written and
// tested on the machine the panes run on, where the agents and their files
// are. None of it takes the server's lock — it is all I/O, and none of it
// is the session's — and the manager serialises its own writes.

// mcpManager is made on first use: a tend that never opens the MCP manager
// never reads the agents' files.
func (s *Server) mcpManager() (*mcp.Manager, error) {
	s.mcpOnce.Do(func() {
		dir, err := transport.StateDir()
		if err != nil {
			s.mcpErr = err
			return
		}
		s.mcp = mcp.NewManager(dir)
	})
	return s.mcp, s.mcpErr
}

// MCPList is every agent's servers, as they can be shown.
func (s *Server) MCPList() proto.MCPListResult {
	m, err := s.mcpManager()
	if err != nil {
		return proto.MCPListResult{Agents: []proto.MCPAgent{{ID: "tend", Error: err.Error()}}}
	}
	snap := m.List()
	out := proto.MCPListResult{Agents: []proto.MCPAgent{}, Entries: []proto.MCPEntry{}}
	for _, a := range snap.Agents {
		out.Agents = append(out.Agents, proto.MCPAgent{ID: a.ID, Name: a.Name, Path: a.Path, Present: a.Present, Error: a.Error})
	}
	for _, e := range snap.Entries {
		out.Entries = append(out.Entries, proto.MCPEntry{
			Agent: e.Agent, Name: e.Name, Enabled: e.Enabled, Via: e.Via, Reason: e.Reason,
			Transport: string(e.Def.Transport), Command: e.Def.Command,
			Args: mcp.RedactArgs(e.Def.Args), URL: mcp.RedactURL(e.Def.URL),
			EnvKeys: keysOf(e.Def.Env), HeaderKeys: keysOf(e.Def.Headers),
			Extras: e.Extras, Fingerprint: e.Def.Fingerprint(),
		})
	}
	return out
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MCPTest tests one agent's server, looked up here so its secrets never
// leave the server.
func (s *Server) MCPTest(ref proto.MCPRef) (proto.MCPTestResult, error) {
	m, err := s.mcpManager()
	if err != nil {
		return proto.MCPTestResult{}, err
	}
	d, err := m.Lookup(ref.Agent, ref.Name)
	if err != nil {
		return proto.MCPTestResult{}, err
	}
	r := mcp.Probe(context.Background(), d, s.cfg.Build)
	return proto.MCPTestResult{
		Status: r.Status, Millis: r.Elapsed.Milliseconds(), Tools: r.Tools,
		Server: r.Server, Version: r.Version, Protocol: r.Protocol, Error: r.Error, Stderr: r.Stderr,
	}, nil
}

// MCPSetEnabled turns a server on or off in one agent.
func (s *Server) MCPSetEnabled(p proto.MCPSetParams) error {
	m, err := s.mcpManager()
	if err != nil {
		return err
	}
	return m.SetEnabled(p.Agent, p.Name, p.Enabled)
}

// MCPAdd writes a server into each agent named, copied from another agent
// or as typed; each agent's outcome is its own, so one refusing does not
// stop the others.
func (s *Server) MCPAdd(p proto.MCPAddParams) proto.MCPWriteResult {
	res := proto.MCPWriteResult{Failed: map[string]string{}}
	m, err := s.mcpManager()
	if err != nil {
		res.Failed["tend"] = err.Error()
		return res
	}
	if strings.TrimSpace(p.Name) == "" {
		res.Failed["tend"] = "a server needs a name"
		return res
	}
	for _, agent := range p.Agents {
		var warn []string
		var err error
		switch {
		case p.From != nil:
			warn, err = m.Copy(p.From.Agent, p.From.Name, agent)
		case p.Def != nil:
			err = m.Add(agent, strings.TrimSpace(p.Name), mcp.Def{
				Transport: mcp.Transport(p.Def.Transport), Command: p.Def.Command, Args: p.Def.Args,
				Env: p.Def.Env, URL: p.Def.URL, Headers: p.Def.Headers,
			})
		default:
			err = errors.New("nothing to add: neither a server to copy nor one typed in")
		}
		if err != nil {
			res.Failed[agent] = err.Error()
			continue
		}
		res.Done = append(res.Done, agent)
		for _, w := range warn {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", agent, w))
		}
	}
	return res
}

// MCPRemove takes a server out of each agent named.
func (s *Server) MCPRemove(p proto.MCPRemoveParams) proto.MCPWriteResult {
	res := proto.MCPWriteResult{Failed: map[string]string{}}
	m, err := s.mcpManager()
	if err != nil {
		res.Failed["tend"] = err.Error()
		return res
	}
	for _, agent := range p.Agents {
		if err := m.Remove(agent, p.Name); err != nil {
			res.Failed[agent] = err.Error()
			continue
		}
		res.Done = append(res.Done, agent)
	}
	return res
}
