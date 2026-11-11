package web

import (
	"sort"
	"strconv"

	"github.com/auth-com-br/tend/internal/proto"
)

// Agent is one agent as the page lists it: where it is — company, space,
// tab — and what it is doing.
type Agent struct {
	Pane    uint64 `json:"pane"`
	Agent   string `json:"agent"`
	Title   string `json:"title,omitempty"`
	State   string `json:"state"`
	Company string `json:"company,omitempty"`
	Space   string `json:"space"`
	Tab     string `json:"tab,omitempty"`
	Dormant bool   `json:"dormant,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Running bool   `json:"running"`
	// Usage is what its conversation is spending, as tend's own sidebar
	// shows it; nil when not known.
	Usage *proto.PaneUsage `json:"usage,omitempty"`
}

// Agents are the session's agents, those asleep included, in the order of
// the companies and spaces; the ones that need the person first within
// each — that is what a glance at a phone is for.
func Agents(snap proto.SessionSnapshot) []Agent {
	company := map[uint64]string{}
	for _, c := range snap.Companies {
		for _, w := range c.Workspaces {
			company[w] = c.Name
		}
	}
	panes := map[uint64]proto.PaneInfo{}
	for _, p := range snap.Panes {
		panes[p.ID] = p
	}
	var out []Agent
	for wi, w := range snap.Workspaces {
		space := w.Name
		if space == "" {
			space = "space " + strconv.Itoa(wi+1)
		}
		for ti, t := range w.Tabs {
			tab := t.Name
			if tab == "" {
				tab = "tab " + strconv.Itoa(ti+1)
			}
			for _, id := range t.Panes {
				p, ok := panes[id]
				if !ok || p.Agent == "" {
					continue
				}
				state := p.State
				switch {
				case p.Dormant:
					state = "asleep"
				case !p.Running:
					state = "exited"
				}
				out = append(out, Agent{
					Pane: id, Agent: p.Agent, Title: p.Title, State: state,
					Company: company[w.ID], Space: space, Tab: tab,
					Dormant: p.Dormant, Done: p.Done, Running: p.Running, Usage: p.Usage,
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Company != out[j].Company {
			return out[i].Company < out[j].Company
		}
		return urgency(out[i]) > urgency(out[j])
	})
	return out
}

// urgency orders what wants the person: asking first, then finished and
// unseen, then working, then the rest.
func urgency(a Agent) int {
	switch {
	case a.State == "blocked":
		return 4
	case a.Done:
		return 3
	case a.State == "working":
		return 2
	case a.State == "idle":
		return 1
	}
	return 0
}
