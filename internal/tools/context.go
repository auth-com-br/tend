package tools

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// The context buffer: what a tool captured — the files panel's Add to
// context, `tend context add`, a browser extension over the socket, a
// handoff's note — kept by the server, listed by a client, and copied,
// sent to the agent being worked with (typed into its pane, not
// submitted, to be read over) or dropped.

// ContextEntry is an item as a list shows it: a line, and its parts under
// headings.
func ContextEntry(it proto.ContextItem) ui.ContextEntry {
	e := ui.ContextEntry{Kind: it.Kind, Summary: capture.Summary(it)}
	add := func(heading, value string) {
		if value != "" {
			e.Parts = append(e.Parts, ui.ContextPart{Heading: heading, Value: value})
		}
	}
	add("NOTE", it.Note)
	if it.Title != "" && it.URL != "" {
		add("PAGE", it.Title)
	}
	add("URL", it.URL)
	element := it.Selector
	if it.Tag != "" && !strings.HasPrefix(element, it.Tag) {
		element = strings.TrimSpace(it.Tag + " " + element)
	}
	if it.Kind == capture.KindElement {
		add("SELECTED ELEMENT", element)
	}
	add("FILE", it.Path)
	if len(it.Attributes) > 0 {
		names := make([]string, 0, len(it.Attributes))
		for name := range it.Attributes {
			names = append(names, name)
		}
		sort.Strings(names)
		var pairs []string
		for _, name := range names {
			pairs = append(pairs, name+"="+it.Attributes[name])
		}
		add("ATTRIBUTES", strings.Join(pairs, "  "))
	}
	add("TEXT", it.Text)
	if it.Source != "" {
		add("FROM", it.Source)
	}
	return e
}

// ContextNewestFirst orders the buffer as it is listed: the newest first.
func ContextNewestFirst(items []proto.ContextItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].ID > items[j].ID })
}

// ContextTarget is the pane "send to the agent" types into, and its name:
// the pane in focus when it runs an agent, else an agent running in the
// tab shown (shown, in the layout's order), else one in the space (tabs,
// each tab's panes), else the pane in focus — the one being worked with,
// which is how the session already decides what "the agent" is.
func ContextTarget(panes []proto.PaneInfo, focus uint64, shown []uint64, tabs [][]uint64) (uint64, string) {
	info := make(map[uint64]proto.PaneInfo, len(panes))
	for _, p := range panes {
		info[p.ID] = p
	}
	name := func(id uint64) string {
		p := info[id]
		label := p.Agent
		if label == "" && len(p.Command) > 0 {
			label = filepath.Base(p.Command[0])
		}
		if label == "" {
			label = p.Title
		}
		return strings.TrimSpace(strconv.FormatUint(id, 10) + " " + label)
	}
	if p, ok := info[focus]; ok && p.Agent != "" {
		return focus, name(focus)
	}
	for _, id := range shown {
		if p := info[id]; p.Agent != "" && p.Running {
			return id, name(id)
		}
	}
	for _, tab := range tabs {
		for _, id := range tab {
			if p := info[id]; p.Agent != "" && p.Running {
				return id, name(id)
			}
		}
	}
	if focus != 0 {
		return focus, name(focus)
	}
	return 0, ""
}
