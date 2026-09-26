package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeEnv is a machine with some programs on its PATH, each printing a
// version.
func fakeEnv(installed map[string]string) Env {
	return Env{
		LookPath: func(name string) (string, error) {
			if _, ok := installed[name]; ok {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Output: func(ctx context.Context, path string, args ...string) (string, error) {
			for name, version := range installed {
				if path == "/usr/bin/"+name {
					return version + "\nmore lines\n", nil
				}
			}
			return "", errors.New("no such program")
		},
	}
}

// TestAgentsAreFoundWithTheirVersionAndAWayToInstall: an agent whose
// executable is on the PATH is installed, with its path and the first line
// of its version; one that is not is offered the first install method whose
// program is there, or names what is missing when none is. If it regresses,
// the manager says an installed agent is missing, or offers an npm command
// on a machine without npm.
func TestAgentsAreFoundWithTheirVersionAndAWayToInstall(t *testing.T) {
	catalog := []Definition{
		{ID: "a", Binaries: []string{"a-cli", "a"}},
		{ID: "b", Binaries: []string{"b"}, Methods: []Method{
			{Kind: "npm", Needs: "npm", Command: "npm install -g b"},
			{Kind: "script", Needs: "curl", Command: "curl -fsSL https://b.test/install | sh"},
		}},
		{ID: "c", Binaries: []string{"c"}, Methods: []Method{{Kind: "npm", Needs: "npm", Command: "npm install -g c"}}},
	}
	got := Find(fakeEnv(map[string]string{"a": "a 1.2.3", "curl": ""}), catalog)
	if !got[0].Installed || got[0].Path != "/usr/bin/a" || got[0].Version != "a 1.2.3" {
		t.Errorf("a found by its second name, with its version: %+v", got[0])
	}
	if got[1].Installed || got[1].Install.Kind != "script" {
		t.Errorf("b offered the script, npm being absent: %+v", got[1])
	}
	if got[2].Install.Command != "" || got[2].Missing != "npm" {
		t.Errorf("c has no way here, and says it needs npm: %+v", got[2])
	}
}

// TestTheCatalogNamesEachAgentOnce: every agent has an ID, a name and an
// executable, no ID twice, and every install method says what it runs and
// where it was read. If it regresses, the manager lists an agent it can
// never find, or offers a command nobody can trace to its vendor.
func TestTheCatalogNamesEachAgentOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Catalog() {
		if d.ID == "" || d.Name == "" || len(d.Binaries) == 0 || seen[d.ID] {
			t.Errorf("incomplete or repeated: %+v", d)
		}
		seen[d.ID] = true
		for _, m := range d.Methods {
			if m.Kind == "" || m.Command == "" || m.Source == "" {
				t.Errorf("%s: a method without its kind, command or source: %+v", d.ID, m)
			}
		}
	}
}

// TestAMemoryToolIsFoundRunningOrOfferedTheWayTheMachineCan: ai-memory in a
// container has no program on the PATH, and is found installed and running
// by its port answering; on a machine without yay it is offered through
// Docker, and Graphify through uv when there is no pipx — each with what to
// run next to give it to the agents. If it regresses, a running ai-memory
// reads as missing, or the install offered needs a program the machine has
// not.
func TestAMemoryToolIsFoundRunningOrOfferedTheWayTheMachineCan(t *testing.T) {
	env := fakeEnv(map[string]string{"docker": "Docker 27", "uv": "uv 0.8"})
	listening := false
	env.Listening = func(port int) bool { return listening && port == 49374 }
	byID := func() map[string]Status {
		out := map[string]Status{}
		for _, s := range Find(env, Catalog()) {
			out[s.ID] = s
		}
		return out
	}
	found := byID()
	mem, graph := found["ai-memory"], found["graphify"]
	if mem.Kind != "memory" || mem.Installed || mem.Install.Kind != "docker" || !strings.Contains(mem.Install.Command, "127.0.0.1:49374:49374") {
		t.Errorf("ai-memory: %+v", mem)
	}
	if graph.Installed || graph.Install.Kind != "uv" || graph.Install.Command != "uv tool install graphifyy && graphify install" {
		t.Errorf("graphify: %+v", graph)
	}
	if !strings.Contains(mem.Next, "install-mcp") || graph.Next == "" {
		t.Errorf("next steps: %q %q", mem.Next, graph.Next)
	}
	listening = true
	if mem = byID()["ai-memory"]; !mem.Installed || !mem.Running || mem.Path != "" || mem.Version != "" {
		t.Errorf("a running ai-memory: %+v", mem)
	}
}
