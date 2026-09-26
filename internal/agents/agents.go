// Package agents is the catalog of agent CLIs tend knows how to find and
// install: for each, the executables that mean it is there, how to ask it
// its version, and the ways its vendor documents to install it.
//
// It is tend's own — herdr installs hooks into agents (internal/integration,
// which this reuses for the executables), not the agents themselves. Finding
// runs where the agents run, the server's machine; installing is a command
// the client runs in a tab of its own, so the user sees it, answers it and can
// stop it. Nothing here installs anything by itself.
package agents

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/integration"
)

// Method is one way to install an agent, as its vendor documents it. Kind
// says what sort (npm, script, brew, pipx, uv, ...), so methods of a kind can
// be treated alike later; Needs is the program it runs, which must be on the
// PATH for the method to be offered; Command is the shell command itself.
type Method struct {
	Kind    string
	Needs   string
	Command string
	// Source is where the command was read, the vendor's own page.
	Source string
}

// Definition is one agent.
type Definition struct {
	ID          string
	Name        string
	Description string
	// Binaries are the executables that mean it is installed, the first the
	// one to ask for a version.
	Binaries []string
	// VersionArgs make the executable print its version; nil is --version.
	VersionArgs []string
	// Methods are the ways to install it, the vendor's recommended first.
	Methods []Method
	// Kind is "memory" for a tool that gives agents memory (docs/MEMORY.md),
	// listed apart from the agents; empty for an agent.
	Kind string
	// Port is where a tool that runs as a service listens on this machine,
	// which finds one installed as a container, with no program on the
	// PATH; zero for none.
	Port int
	// Next is what to run once it is installed, to give it to the agents,
	// as its project documents it.
	Next string
}

// Status is an agent as found on this machine.
type Status struct {
	Definition
	Installed bool
	// Path is the executable found; Version the first line it printed.
	Path    string
	Version string
	// Install is the method to offer, the first whose program is here; zero
	// when there is none (Missing then names what the first one needs).
	Install Method
	Missing string
	// Running is a service answering on its Port.
	Running bool
}

// Env is what finding needs from the machine, so a test can stand in for it.
type Env struct {
	LookPath func(string) (string, error)
	// Output runs a program and returns what it printed.
	Output func(ctx context.Context, path string, args ...string) (string, error)
	// Listening reports whether something answers on a port of this
	// machine's loopback.
	Listening func(port int) bool
}

// System is the real machine.
var System = Env{
	LookPath: exec.LookPath,
	Output: func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, path, args...).Output()
		return string(out), err
	},
	Listening: func(port int) bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	},
}

// versionTimeout bounds asking for a version: an agent that starts a whole
// interface on --version, or hangs on a first-run prompt, must not hold the
// list up.
const versionTimeout = 3 * time.Second

// Find looks for every agent in the catalog, all at once: asking each one
// found for its version in turn would keep the list waiting on the slowest
// of them several times over.
func Find(env Env, catalog []Definition) []Status {
	out := make([]Status, len(catalog))
	var wg sync.WaitGroup
	for i, def := range catalog {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = find(env, def)
		}()
	}
	wg.Wait()
	return out
}

func find(env Env, def Definition) Status {
	s := Status{Definition: def}
	for _, bin := range def.Binaries {
		if path, err := env.LookPath(bin); err == nil {
			s.Installed, s.Path = true, path
			break
		}
	}
	if def.Port > 0 && env.Listening != nil && env.Listening(def.Port) {
		// A service in a container has no program here; answering on its
		// port is what says it is installed and running.
		s.Running, s.Installed = true, true
	}
	if s.Path != "" {
		args := def.VersionArgs
		if args == nil {
			args = []string{"--version"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
		if out, err := env.Output(ctx, s.Path, args...); err == nil {
			s.Version = firstLine(out)
		}
		cancel()
	}
	for _, m := range def.Methods {
		if m.Needs == "" {
			s.Install = m
			break
		}
		if _, err := env.LookPath(m.Needs); err == nil {
			s.Install = m
			break
		}
	}
	if s.Install.Command == "" && len(def.Methods) > 0 {
		s.Missing = def.Methods[0].Needs
	}
	return s
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// binariesOf is an integration target's executables, so the two lists of
// what an agent is called do not drift apart.
func binariesOf(t integration.Target) []string { return t.Commands() }
