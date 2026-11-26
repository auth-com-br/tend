package tools

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/agent"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The agents' sessions: the conversations the server's machine's agents
// keep, listed and searched, resumed in a tab where they were held, and
// deleted after asking. A conversation open in a pane is gone to rather
// than opened twice, and is never deleted: the server refuses that
// whatever a client asks.

// SessionsOld is how old "mark the old ones" means: not written to in a
// month.
const SessionsOld = 30 * 24 * time.Hour

// ResumeScript runs an agent — resumed, continued, started — and leaves a
// shell in the pane when it ends, as a tab somebody had opened and typed
// the command into would.
const ResumeScript = `"$@"
exec "${SHELL:-/bin/sh}"`

// SessionEntries are the server's sessions as the list shows them.
func SessionEntries(list []proto.AgentSessionInfo) []ui.SessionEntry {
	all := make([]ui.SessionEntry, 0, len(list))
	for _, s := range list {
		all = append(all, ui.SessionEntry{
			ID: s.ID, Agent: s.Agent, Title: s.Title, Dir: s.Dir,
			Modified: time.Unix(s.Modified, 0), Size: s.Size, Prompts: s.Prompts, Pane: s.Pane,
			Unreachable: s.Unreachable,
		})
	}
	return all
}

// SessionsSummary is the line under a list freshly read: how many, and
// how much of the disk they take.
func SessionsSummary(list []proto.AgentSessionInfo) string {
	var total int64
	for _, s := range list {
		total += s.Size
	}
	return fmt.Sprintf("%d sessions, %s on disk", len(list), ui.SessionSize(total))
}

// DeleteText says what a delete did: how many went, and why any was kept.
func DeleteText(res proto.AgentSessionsDeleteResult, err error) string {
	switch {
	case err != nil:
		return "could not delete: " + err.Error()
	case len(res.Kept) > 0:
		var why []string
		for _, reason := range res.Kept {
			why = append(why, reason)
		}
		sort.Strings(why)
		return fmt.Sprintf("deleted %d, kept %d (%s)", len(res.Deleted), len(res.Kept), strings.Join(dedupe(why), "; "))
	}
	return fmt.Sprintf("deleted %d", len(res.Deleted))
}

func dedupe(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// Resume is how a session is resumed: the pane it starts in, in the
// directory the conversation was held in — which is where the agent looks
// for it — and the tab's name. ok is false for an agent tend cannot resume;
// command, when set, is what resumes it where its directory can be entered
// (as root, for one in /root), for a session that cannot be resumed here:
// there is nowhere else to resume it from.
func Resume(e ui.SessionEntry) (spec proto.PaneSpec, name, command string, ok bool) {
	argv, ok := agent.Resume(agent.PersistedSession{
		Source: "tend:" + e.Agent, Agent: e.Agent, Session: agent.SessionRef{ID: e.ID},
	})
	if !ok {
		return proto.PaneSpec{}, "", "", false
	}
	if e.Unreachable != "" {
		return proto.PaneSpec{}, "", "cd " + ShellQuote(e.Dir) + " && " + ShellJoin(argv), true
	}
	name = e.Title
	if name == "" {
		name = e.Agent
	}
	if r := []rune(name); len(r) > 24 {
		name = string(r[:23]) + "…"
	}
	return proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", ResumeScript, "tend-resume"}, argv...),
		Dir:     e.Dir,
		Agent:   e.Agent,
	}, name, "", true
}
