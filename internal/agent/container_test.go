package agent

import (
	"strings"
	"testing"
)

// TestAnAgentInAContainerIsTheCommandAfterIt: the ways people run an agent
// in a container (herdr#699). If it regresses, a Claude Code started with
// docker exec is a pane with no agent: no state, no notice, nothing in the
// sidebar.
func TestAnAgentInAContainerIsTheCommandAfterIt(t *testing.T) {
	for line, want := range map[string]string{
		"docker exec -it box claude":                                    "claude",
		"docker exec -it -u node -w /work box claude --resume":          "claude",
		"docker exec -e TERM=xterm-256color -it box codex":              "codex",
		"/usr/bin/docker exec --env=A=1 -it box /usr/local/bin/claude":  "claude",
		"docker container exec -it box gemini":                          "gemini",
		"docker run --rm -it -v /src:/src -w /src agents:latest claude": "claude",
		"podman run -it --name x img opencode":                          "opencode",
		"docker compose exec app claude":                                "claude",
		"docker --context remote exec -it box claude":                   "claude",
		`docker exec -it box sh -c "claude --resume abc"`:               "claude",
		"docker exec -it box bash -lc codex":                            "codex",
		"kubectl exec -it -n dev pod-1 -- claude":                       "claude",
		"docker exec -it box node /app/bin/dsh":                         "dsh",
		"docker exec -it box":                                           "",
		"docker ps -a":                                                  "",
		"docker run -it ubuntu":                                         "",
		"kubectl exec -it pod-1":                                        "",
		"/bin/sh /usr/bin/docker exec -it box claude":                   "claude",
	} {
		argv := strings.Fields(line)
		if strings.Contains(line, `"`) {
			i := strings.Index(line, `"`)
			argv = append(strings.Fields(line[:i]), strings.Trim(line[i:], `"`))
		}
		if got := ContainerAgentName(argv); got != want {
			t.Errorf("%q = %q, want %q", line, got, want)
		}
	}
}
