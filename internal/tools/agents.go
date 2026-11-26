package tools

import (
	"sort"
	"strconv"

	"github.com/auth-com-br/tend/internal/proto"
)

// The agent manager's logic: the catalog the server reads of its machine,
// in the order it is listed, what its line under the list says, and the
// script an install runs in a tab of its own — where whoever asked for it
// watches it, answers it (a password, a licence) and can stop it — which
// then goes on as a shell, so the agent just installed can be run there.

// InstallScript runs an install command and says how it went, then leaves
// a shell: $1 is the command, $2 the agent's name.
const InstallScript = `printf '\033[1m[tend] installing %s\033[0m\n%s\n\n' "$2" "$1"
sh -c "$1"
status=$?
echo
if [ "$status" -eq 0 ]; then
	printf '[tend] %s installed. Open the agent manager again (prefix+A) to see it.\n' "$2"
else
	printf '[tend] the install stopped with status %s.\n' "$status"
fi
exec "${SHELL:-/bin/sh}"`

// InstallSpec is the pane an agent's install runs in.
func InstallSpec(a proto.AgentStatus) proto.PaneSpec {
	return proto.PaneSpec{
		Command: []string{"/bin/sh", "-c", InstallScript, "tend-install", a.InstallCommand, a.Name},
		Title:   "install " + a.ID,
	}
}

// SortCatalog puts the agents first, then the memory tools, each installed
// first, keeping the server's order otherwise: the order the list is drawn
// in, which a cursor counts through.
func SortCatalog(list []proto.AgentStatus) {
	sort.SliceStable(list, func(i, j int) bool {
		if mi, mj := list[i].Kind == "memory", list[j].Kind == "memory"; mi != mj {
			return mj
		}
		if list[i].Installed != list[j].Installed {
			return list[i].Installed
		}
		return false
	})
}

// CatalogSummary is the line under the list: how many of the agents (not
// the memory tools) are installed.
func CatalogSummary(list []proto.AgentStatus) string {
	installed, agents := 0, 0
	for _, a := range list {
		if a.Kind == "memory" {
			continue
		}
		agents++
		if a.Installed {
			installed++
		}
	}
	return strconv.Itoa(installed) + " of " + strconv.Itoa(agents) + " agents installed on this machine"
}

// AgentEnter is what choosing an agent says when it does not install it:
// where an installed one is, what a memory tool installed still needs, or
// what an agent that cannot be installed here needs. install is true for
// one that can be installed, which a client asks about before it does.
func AgentEnter(a proto.AgentStatus) (msg string, install bool) {
	switch {
	case a.Installed && a.Next != "":
		// A memory tool installed is not yet the agents': say how.
		return a.Name + " — " + a.Next, false
	case a.Installed:
		return a.Name + " is at " + a.Path, false
	case a.InstallCommand == "":
		return a.Name + " needs " + a.Missing + " to install, which this machine has not", false
	}
	return "", true
}
