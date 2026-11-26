// Package sidebar decides what the sidebar lists: the spaces as a tree of
// groups, the agents with the state each shows, and the order the attention
// queue puts them in. It draws nothing. Its rows (ui.SidebarRow) are drawn
// into cells by the TUI (internal/ui) and natively by tend-desktop, so the
// two show the same list, in the same order, saying the same things.
//
// It was cmd/tend's (tui_nav.go) until the desktop needed it: a decision
// both clients make lives once, here (docs/DESKTOP.md, "Rules").
//
// What one viewer is looking at — the company chosen, the groups folded, the
// pane in focus, a drag in progress — is given in a View, never kept: it is
// the client's, not the session's.
package sidebar

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/agentview"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// View is one viewer's place in the session and how its settings lay the
// list out.
type View struct {
	UI config.UI
	// Snap is the session being shown. Rows for another machine's session
	// come from that source's own snapshot; the view's company, focus and
	// agent view are this one's.
	Snap *proto.SessionSnapshot
	// Company is the company chosen, 0 for none. One the session no longer
	// has is as good as none.
	Company uint64
	// Workspace, Tab and Focus are what is on screen.
	Workspace, Tab, Focus uint64
	// Grouped lists agents under their tabs.
	Grouped bool
	// Multi is a list of several saved machines: grouped headings name the
	// machine too.
	Multi bool
	// DropGroup and DropTarget mark where a space being dragged would land.
	DropGroup  string
	DropTarget uint64
	// Now is when the list is made, for usage that counts down to a reset.
	Now time.Time
}

// SpaceSource is one machine's session as the space list reads it: the one
// being shown, or another saved machine's, whose rows are laid out the same
// way and point back at it.
type SpaceSource struct {
	Snap *proto.SessionSnapshot
	// Machine is empty for the session being shown.
	Machine string
	Folded  map[string]bool
	// Current is the space in view, which is none on another machine.
	Current uint64
	// Depth is where the tree starts: under a machine's row, one in.
	Depth int
	Stale bool
}

// AgentSource is one machine's session as the agent list reads it.
type AgentSource struct {
	Snap *proto.SessionSnapshot
	// Machine is empty for the session shown; Label is what the machine
	// token says, empty when there is nothing to say.
	Machine, Label string
	Here, Stale    bool
}

// CompanyOf is the company chosen, if the session still has it.
func (v View) CompanyOf() (proto.CompanyInfo, bool) {
	if v.Company == 0 || v.Snap == nil {
		return proto.CompanyInfo{}, false
	}
	for _, c := range v.Snap.Companies {
		if c.ID == v.Company {
			return c, true
		}
	}
	return proto.CompanyInfo{}, false
}

// Shown reports whether a space of the session being shown is in the list:
// all of them with no company chosen, its own with one.
func (v View) Shown(ws uint64) bool {
	c, ok := v.CompanyOf()
	return !ok || slices.Contains(c.Workspaces, ws)
}

// SpaceRows lays one session's spaces out as a tree.
//
// A group appears where its first member is, so the order on screen follows
// the order of the session rather than sorting groups to one end: a user who
// made a space expects to find it where they put it, not where an alphabet
// puts it.
func (v View) SpaceRows(src SpaceSource) []ui.SidebarRow {
	var rows []ui.SidebarRow
	seen := make(map[string]bool)
	here := src.Machine == ""

	// With a company chosen, the session being shown lists its spaces
	// only; another machine's list is its own session's, and has none.
	shown := func(w proto.WorkspaceInfo) bool { return !here || v.Shown(w.ID) }

	// A group with no space in the list is still listed when it is the
	// user's to see: always with no company chosen, and with one when the
	// group has had a space of it. It is not where a member puts it, having
	// none here, so it comes before the next group made after it that is
	// placed by a member, or at the end — which keeps groups in the order
	// they were made as far as the spaces' order lets them.
	groups := GroupsIn(src.Snap)
	company, chosen := uint64(0), false
	if here {
		if c, ok := v.CompanyOf(); ok {
			company, chosen = c.ID, true
		}
	}
	placed := make(map[string]bool)
	for _, w := range src.Snap.Workspaces {
		if w.Group != "" && shown(w) {
			placed[w.Group] = true
		}
	}
	unplaced := func(g proto.GroupInfo) bool {
		return !placed[g.Name] && (!chosen || slices.Contains(g.Companies, company))
	}
	var emit func(name string)
	flushBefore := func(name string) {
		for _, g := range groups {
			if g.Name == name {
				return
			}
			if unplaced(g) && !seen[g.Name] {
				emit(g.Name)
			}
		}
	}

	emit = func(name string) {
		seen[name] = true
		members := slices.DeleteFunc(GroupMembers(src.Snap, name), func(m proto.WorkspaceInfo) bool { return !shown(m) })
		folded := src.Folded[name]
		inGroup := false
		for _, m := range members {
			inGroup = inGroup || m.ID == src.Current
		}
		rows = append(rows, ui.SidebarRow{
			Kind:     ui.SidebarSpaceGroup,
			Label:    name,
			Group:    name,
			Folded:   folded,
			DropHere: here && name == v.DropGroup,
			Action:   ui.ActionToggleGroup,
			// A folded group still says what is happening inside it. Hiding
			// that would make folding a way to stop being told an agent is
			// waiting, which is the opposite of what folding is for.
			Trailing: GroupStateLabel(src.Snap, members),
			Active:   inGroup,
			Depth:    src.Depth,
			Machine:  src.Machine,
			Stale:    src.Stale,
		})
		if folded {
			return
		}
		for _, member := range members {
			rows = append(rows, v.spaceRow(src, member, src.Depth+1))
		}
	}

	for _, w := range src.Snap.Workspaces {
		if !shown(w) {
			continue
		}
		if w.Group == "" {
			rows = append(rows, v.spaceRow(src, w, src.Depth))
			continue
		}
		if seen[w.Group] {
			continue
		}
		flushBefore(w.Group)
		emit(w.Group)
	}
	for _, g := range groups {
		if unplaced(g) && !seen[g.Name] {
			emit(g.Name)
		}
	}
	return rows
}

// spaceRow is one space, at the given depth.
func (v View) spaceRow(src SpaceSource, w proto.WorkspaceInfo, depth int) ui.SidebarRow {
	state := SpaceState(src.Snap, w)
	return ui.SidebarRow{
		Kind:  ui.SidebarSpace,
		Label: OrDash(w.Name),
		Lines: ui.ResolveSpaceRows(v.UI.Sidebar.SpaceRows(), ui.SpaceTokenValues{
			StateText: state, Workspace: OrDash(w.Name), Branch: w.Branch,
			Ahead: w.Ahead, Behind: w.Behind, Custom: SpaceTokens(w),
		}),
		Gap:       v.UI.Sidebar.Spaces.RowGap,
		Symbols:   v.UI.StatusIndicators == "symbols",
		Group:     w.Group,
		Depth:     depth,
		Workspace: w.ID,
		DropHere:  src.Machine == "" && w.ID == v.DropTarget,
		State:     state,
		Running:   true,
		Active:    w.ID == src.Current,
		Machine:   src.Machine,
		Stale:     src.Stale,
	}
}

// AgentRows is the agent list of the session shown, on its own (one
// machine): in session order, or the attention queue when the settings sort
// by priority, or the order and filter a script set (proto's AgentView).
func (v View) AgentRows(label string) []ui.SidebarRow {
	rows, seqs := v.AgentEntries(AgentSource{Snap: v.Snap, Label: label, Here: true})
	if view := v.Snap.AgentView; view != nil {
		// A view a script set replaces the order and decides what is
		// shown, as herdr's agent_view_override does; tab headings mean
		// nothing in an order that is not the session's.
		return v.ApplyAgentView(view, rows, PaneIndex(v.Snap))
	}
	if v.UI.AgentPanelSort == "priority" {
		rows = ByAttention(rows, seqs, nil)
	}
	return rows
}

// AgentEntries is one session's agents in session order, under their tabs
// when the list is grouped, with each one's state sequence for the attention
// order.
func (v View) AgentEntries(src AgentSource) ([]ui.SidebarRow, []uint64) {
	info := PaneIndex(src.Snap)
	priority := v.UI.AgentPanelSort == "priority"
	symbols := v.UI.StatusIndicators == "symbols"

	var rows []ui.SidebarRow
	var seqs []uint64
	for _, w := range src.Snap.Workspaces {
		if src.Here && !v.Shown(w.ID) {
			// A company chosen shows its agents only; the heading above
			// says when one elsewhere is waiting.
			continue
		}
		for _, tab := range w.Tabs {
			var entries []ui.SidebarRow
			var entrySeqs []uint64
			for _, id := range tab.Panes {
				p := info[id]
				if p.Agent == "" {
					// A shell is not an agent. Listing every pane would make
					// the list as long as the session and hide what it exists
					// to show.
					continue
				}
				entries = append(entries, ui.SidebarRow{
					Kind:      ui.SidebarAgent,
					Label:     AgentLabel(w, tab),
					Lines:     v.AgentLines(w, tab, p, src.Label),
					Gap:       v.UI.Sidebar.Agents.RowGap,
					Symbols:   symbols,
					Pane:      id,
					Tab:       tab.ID,
					Workspace: w.ID,
					State:     DisplayState(p),
					Running:   p.Running,
					Active:    src.Here && id == v.Focus && tab.ID == v.Tab && w.ID == v.Workspace,
					Machine:   src.Machine,
					Stale:     src.Stale,
				})
				entrySeqs = append(entrySeqs, p.StateSeq)
			}
			if len(entries) == 0 {
				continue
			}
			if v.Grouped && !priority {
				label := OrDash(w.Name) + " · " + OrDash(tab.Name)
				if v.Multi {
					label = src.Label + " · " + label
				}
				rows = append(rows, ui.SidebarRow{
					Kind:      ui.SidebarGroup,
					Label:     label,
					Tab:       tab.ID,
					Workspace: w.ID,
					Machine:   src.Machine,
				})
				seqs = append(seqs, 0)
			}
			rows = append(rows, entries...)
			seqs = append(seqs, entrySeqs...)
		}
	}
	return rows, seqs
}

// ApplyAgentView filters and orders the agent rows by a view a script set.
func (v View) ApplyAgentView(view *agentview.View, rows []ui.SidebarRow, info map[uint64]proto.PaneInfo) []ui.SidebarRow {
	wsOrder := map[uint64]uint64{}
	tabOrder := map[uint64]uint64{}
	paneOrder := map[uint64]uint64{}
	for i, w := range v.Snap.Workspaces {
		wsOrder[w.ID] = uint64(i + 1)
		for j, tab := range w.Tabs {
			tabOrder[tab.ID] = uint64(j + 1)
			for k, id := range tab.Panes {
				paneOrder[id] = uint64(k + 1)
			}
		}
	}
	ctx := agentview.Context{WorkspaceID: fmt.Sprintf("w_%d", v.Workspace), TabID: fmt.Sprintf("t_%d", v.Tab)}
	type entry struct {
		row ui.SidebarRow
		e   agentview.Entry
	}
	var kept []entry
	for _, r := range rows {
		if r.Kind != ui.SidebarAgent {
			continue
		}
		p := info[r.Pane]
		state := DisplayState(p)
		if !p.Running || state == "" {
			state = "unknown"
		}
		e := agentview.Entry{
			Status: state, WorkspaceID: fmt.Sprintf("w_%d", r.Workspace), TabID: fmt.Sprintf("t_%d", r.Tab),
			PaneID: fmt.Sprintf("p_%d", r.Pane), Agent: p.Agent, Seen: !p.Done, StateChangeSeq: p.StateSeq,
			WorkspaceOrder: wsOrder[r.Workspace], TabOrder: tabOrder[r.Tab], PaneOrder: paneOrder[r.Pane],
			Attention: uint64(AttentionPriority(state, p.Running)),
			Tokens:    map[string]string{},
		}
		for _, tok := range p.Tokens {
			e.Tokens[tok.Key] = tok.Value
		}
		if view.Filter == nil || view.Filter.Matches(ctx, e) {
			kept = append(kept, entry{row: r, e: e})
		}
	}
	if len(view.Sort) > 0 {
		sort.SliceStable(kept, func(i, j int) bool { return view.Less(kept[i].e, kept[j].e) })
	}
	out := make([]ui.SidebarRow, 0, len(kept))
	for _, k := range kept {
		out = append(out, k.row)
	}
	return out
}

// AgentLines lays an agent's entry out as the settings say, herdr's
// agent_rows: what each token says, from the session.
func (v View) AgentLines(w proto.WorkspaceInfo, tab proto.TabInfo, p proto.PaneInfo, machine string) [][]ui.SidebarToken {
	tv := ui.AgentTokenValues{
		StateText: DisplayState(p),
		// Empty for this machine when it is the only one; with saved
		// machines, every row names its own, "Local" included, as herdr's
		// do.
		Machine:   machine,
		Workspace: OrDash(w.Name),
		Tab:       tab.Name,
		Agent:     p.Agent,
		Custom:    make(map[string]string, len(p.Tokens)),
	}
	if label := p.StateLabels[DisplayState(p)]; label != "" {
		tv.StateText = label // what a hook calls this state for this pane
	}
	if p.Display != "" {
		tv.Agent = p.Display
	}
	// One title goes over the wire: the user's name when there is one, the
	// program's own otherwise.
	if p.Named {
		tv.Pane = p.Title
	} else {
		tv.TerminalTitle = p.Title
		tv.TerminalTitleStripped = StrippedTerminalTitle(p.Title)
	}
	for _, token := range p.Tokens {
		tv.Custom[token.Key] = token.Value
	}
	now := v.Now
	if now.IsZero() {
		now = time.Now()
	}
	u := ui.FormatUsage(p.Usage, now)
	tv.Context, tv.Spent, tv.Cost, tv.Limit = u.Context, u.Spent, u.Cost, u.Limit
	tv.ContextLevel, tv.LimitLevel = u.ContextLevel, u.LimitLevel
	return ui.ResolveAgentRows(v.UI.Sidebar.AgentRows(p.Agent), tv)
}

// ByAttention is herdr's attention queue: what needs you first, and within
// that the most recent change first; across machines, what cannot be reached
// after what can (sort_aggregate_rows). Tab headings mean nothing in this
// order, and the list has none in it.
//
// A state sequence is counted by each server on its own, so between two
// machines "more recent" is only a guess; within one it is exact.
func ByAttention(rows []ui.SidebarRow, seqs []uint64, stale []bool) []ui.SidebarRow {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		if stale != nil && stale[order[a]] != stale[order[b]] {
			return !stale[order[a]]
		}
		ra, rb := rows[order[a]], rows[order[b]]
		pa, pb := AttentionPriority(ra.State, ra.Running), AttentionPriority(rb.State, rb.Running)
		if pa != pb {
			return pa > pb
		}
		return seqs[order[a]] > seqs[order[b]]
	})
	sorted := make([]ui.SidebarRow, 0, len(rows))
	for _, at := range order {
		if rows[at].Kind == ui.SidebarGroup {
			continue
		}
		sorted = append(sorted, rows[at])
	}
	return sorted
}

// DisplayState is the state as the list shows it: herdr's "done" for an
// agent that finished while nobody was looking.
func DisplayState(p proto.PaneInfo) string {
	if p.Dormant {
		// A sleeping agent (the server's dormant.go): a shell runs where it
		// was, and its state is that it sleeps.
		return "asleep"
	}
	if p.Done && p.State == "idle" {
		return "done"
	}
	return p.State
}

// AttentionPriority is herdr's tab_attention_priority.
func AttentionPriority(state string, running bool) int {
	if !running {
		return 0
	}
	switch state {
	case "blocked":
		return 4
	case "done":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	}
	return 0
}

// SpaceState is the most urgent thing happening in a space, because a space
// marked idle while an agent inside it waits for an answer is worse than no
// mark at all.
func SpaceState(snap *proto.SessionSnapshot, w proto.WorkspaceInfo) string {
	panes := make(map[uint64]bool)
	for _, tab := range w.Tabs {
		for _, id := range tab.Panes {
			panes[id] = true
		}
	}

	// The most urgent, by herdr's attention order: an agent waiting beats one
	// that finished unseen, which beats one still working.
	state, best := "", 0
	for _, p := range snap.Panes {
		if !panes[p.ID] || p.Agent == "" || !p.Running {
			continue
		}
		if s := DisplayState(p); AttentionPriority(s, true) > best {
			state, best = s, AttentionPriority(s, true)
		}
	}
	return state
}

// GroupStateLabel is what a group heading says about its members: the
// number of them that want attention, or nothing when none do.
func GroupStateLabel(snap *proto.SessionSnapshot, members []proto.WorkspaceInfo) string {
	blocked := 0
	for _, w := range members {
		if SpaceState(snap, w) == "blocked" {
			blocked++
		}
	}
	if blocked == 0 {
		return ""
	}
	return strconv.Itoa(blocked) + " waiting"
}

// GroupsIn is a session's groups in the order they were made: the ones its
// server keeps, empty ones too, or from a server that keeps no record of
// them the names its spaces carry, as a group was before.
func GroupsIn(snap *proto.SessionSnapshot) []proto.GroupInfo {
	if len(snap.Groups) > 0 {
		return snap.Groups
	}
	var out []proto.GroupInfo
	seen := make(map[string]bool)
	for _, w := range snap.Workspaces {
		if w.Group != "" && !seen[w.Group] {
			seen[w.Group] = true
			out = append(out, proto.GroupInfo{Name: w.Group})
		}
	}
	return out
}

// GroupMembers returns a group's spaces in one session, in its order.
func GroupMembers(snap *proto.SessionSnapshot, group string) []proto.WorkspaceInfo {
	var out []proto.WorkspaceInfo
	for _, w := range snap.Workspaces {
		if w.Group == group {
			out = append(out, w)
		}
	}
	return out
}

// PaneIndex indexes a session's panes by id.
func PaneIndex(snap *proto.SessionSnapshot) map[uint64]proto.PaneInfo {
	info := make(map[uint64]proto.PaneInfo, len(snap.Panes))
	for _, p := range snap.Panes {
		info[p.ID] = p
	}
	return info
}

// SpaceTokens are the values reported about a space, for its $name tokens.
func SpaceTokens(w proto.WorkspaceInfo) map[string]string {
	if len(w.Tokens) == 0 {
		return nil
	}
	out := make(map[string]string, len(w.Tokens))
	for _, t := range w.Tokens {
		out[t.Key] = t.Value
	}
	return out
}

// AgentLabel names where an agent is, since what it is goes underneath.
func AgentLabel(w proto.WorkspaceInfo, tab proto.TabInfo) string {
	name := OrDash(w.Name)
	if tab.Name != "" {
		name += " · " + tab.Name
	}
	return name
}

// OrDash is a name, or "-" for none, so an unnamed space is still a row.
func OrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ClaudeActivityGlyphs are what Claude Code puts in front of its title while
// it works, as herdr lists them (`terminal/title.rs`).
const ClaudeActivityGlyphs = "·✢✳✶✻✽◐◓◑◒"

// StrippedTerminalTitle drops one leading spinner frame from a program's
// title, as herdr does: a window bar whose text changes ten times a second
// is a bar nobody can read, and the frame says nothing the sidebar does not.
//
// Only a braille frame or one of Claude's glyphs, and only when a space or
// the end follows it — "★ production" is somebody's name for a window.
func StrippedTerminalTitle(title string) string {
	title = strings.TrimSpace(title)
	first, size := utf8.DecodeRuneInString(title)
	if size == 0 {
		return ""
	}
	recognised := (first >= 0x2800 && first <= 0x28ff) || strings.ContainsRune(ClaudeActivityGlyphs, first)
	rest := title[size:]
	if recognised {
		next, _ := utf8.DecodeRuneInString(rest)
		if rest == "" || unicode.IsSpace(next) {
			return strings.TrimSpace(rest)
		}
	}
	return title
}
