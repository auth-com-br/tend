package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/sidebar"
	"github.com/auth-com-br/tend/internal/ui"
)

// State is what the window shows besides the terminals' contents: the
// sidebar's two lists, the tabs of the space shown, and where each pane goes.
// It crosses to QML as JSON, small and only when something changes; the
// panes' contents, which change all the time, never go this way.
type State struct {
	Session   string    `json:"session"`
	Spaces    []Row     `json:"spaces"`
	Agents    []Row     `json:"agents"`
	Grouped   bool      `json:"grouped"`
	Waiting   int       `json:"waiting"`
	Workspace uint64    `json:"workspace"`
	Tabs      []TabRow  `json:"tabs"`
	Panes     []PaneBox `json:"panes"`
	Focus     uint64    `json:"focus"`
	AreaCols  int       `json:"areaCols"`
	AreaRows  int       `json:"areaRows"`
	// NewAgents are the agents installed on the server's machine, which
	// the new-tab menu offers.
	NewAgents []NewAgent `json:"newAgents"`
	// Menu is the menu open, Prompt the line the window asks for, Panel the
	// screen the global menu opened: this window's, never the session's.
	Menu   *MenuState `json:"menu,omitempty"`
	Prompt *Prompt    `json:"prompt,omitempty"`
	Panel  string     `json:"panel,omitempty"`
	Zoomed bool       `json:"zoomed,omitempty"`
	// Update is a newer release the server found, for the menu's mark.
	Update string `json:"update,omitempty"`
	// Pinned is a window kept on one tab: no sidebar, no dock, only its tab.
	Pinned bool `json:"pinned,omitempty"`
	// Toasts are the cards up now.
	Toasts []Toast `json:"toasts,omitempty"`
	// Theme is the window's colours.
	Theme Theme `json:"theme"`
	// Settings is the settings screen, while it is open.
	Settings *SettingsState `json:"settings,omitempty"`
	// Spending is the spending screen, while it is open, and Spent what
	// the agents open now have cost, for the status line.
	Spending *SpendingState `json:"spending,omitempty"`
	Spent    float64        `json:"spent,omitempty"`
	// The global menu's tools' screens, each while it is open (tools.go).
	MCP          *MCPState      `json:"mcp,omitempty"`
	Perm         *PermState     `json:"perm,omitempty"`
	AgentsScreen *AgentsState   `json:"agentsScreen,omitempty"`
	Sessions     *SessionsState `json:"sessions,omitempty"`
	Context      *ContextState  `json:"context,omitempty"`
	About        *AboutState    `json:"about,omitempty"`
	Notes        *NotesState    `json:"notes,omitempty"`
	Phone        *PhoneState    `json:"phone,omitempty"`
	// Dock is the right side, while it is open.
	Dock *DockState `json:"dock,omitempty"`
	// Docs are the document tabs of the space shown, and Doc the one in
	// view, whole; nil while the terminals are.
	Docs []DocTab `json:"docs,omitempty"`
	Doc  *Doc     `json:"doc,omitempty"`
}

// MenuState is a menu as QML draws it.
type MenuState struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
	X     int      `json:"x"`
	Y     int      `json:"y"`
}

// NewAgent is an agent the new-tab menu can start.
type NewAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Row is one sidebar entry, from the same ui.SidebarRow the TUI draws, so
// the two lists say the same things in the same order. The tokens are kept
// as they are resolved (text and kind), for QML to style.
type Row struct {
	Kind      string    `json:"kind"`
	Label     string    `json:"label"`
	Trailing  string    `json:"trailing,omitempty"`
	State     string    `json:"state,omitempty"`
	Running   bool      `json:"running,omitempty"`
	Active    bool      `json:"active,omitempty"`
	Folded    bool      `json:"folded,omitempty"`
	Depth     int       `json:"depth"`
	Group     string    `json:"group,omitempty"`
	Pane      uint64    `json:"pane,omitempty"`
	Tab       uint64    `json:"tab,omitempty"`
	Workspace uint64    `json:"workspace,omitempty"`
	Lines     [][]Token `json:"lines,omitempty"`
	// Space and Agent are the row as the window lays it out (sidebar.qml):
	// what it says, field by field, rather than the TUI's tokens laid in
	// columns of a terminal. The tokens stay, for the settings' own layout.
	Space *SpaceCard `json:"space,omitempty"`
	Agent *AgentCard `json:"agent,omitempty"`
}

// SpaceCard is a space in the window's sidebar.
type SpaceCard struct {
	// Name is the space's, or its folder's when it has none.
	Name string `json:"name"`
	// Folder is where it is, the home folder as ~.
	Folder string `json:"folder,omitempty"`
	Branch string `json:"branch,omitempty"`
	Ahead  int    `json:"ahead,omitempty"`
	Behind int    `json:"behind,omitempty"`
	// Agents are the agents running in it; Working, Waiting and Done how
	// many are working, waiting for the person, or finished and unseen.
	Agents  int `json:"agents,omitempty"`
	Working int `json:"working,omitempty"`
	Waiting int `json:"waiting,omitempty"`
	Done    int `json:"done,omitempty"`
}

// AgentCard is an agent in the window's sidebar.
type AgentCard struct {
	// Kind is the agent (claude, codex…), Name what to call it.
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Doing is what its terminal title says it is doing, or the name the
	// person gave the pane.
	Doing string `json:"doing,omitempty"`
	// Where is its space and tab.
	Where string `json:"where,omitempty"`
	// StateText is the state in words: a hook's own label when it gave one.
	StateText    string `json:"stateText,omitempty"`
	Cost         string `json:"cost,omitempty"`
	Context      int    `json:"context"`
	ContextLevel uint8  `json:"contextLevel,omitempty"`
}

// Token is one resolved sidebar token.
type Token struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Level uint8  `json:"level,omitempty"`
}

// TabRow is a tab of the space shown.
type TabRow struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	// State is the most urgent state of an agent in it, for the tab's mark.
	State string `json:"state,omitempty"`
}

// PaneBox is where a pane goes in the panes' area, in cells.
type PaneBox struct {
	Pane    uint64 `json:"pane"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Title   string `json:"title,omitempty"`
	Agent   string `json:"agent,omitempty"`
	State   string `json:"state,omitempty"`
	Focused bool   `json:"focused"`
	// Scroll is how far back the pane is shown, 0 live, of History lines.
	Scroll  int `json:"scroll,omitempty"`
	History int `json:"history,omitempty"`
}

var rowKinds = map[ui.SidebarKind]string{
	ui.SidebarHeading:    "heading",
	ui.SidebarSpace:      "space",
	ui.SidebarAgent:      "agent",
	ui.SidebarGroup:      "tab",
	ui.SidebarSpaceGroup: "group",
	ui.SidebarAction:     "action",
	ui.SidebarBlank:      "blank",
	ui.SidebarMachine:    "machine",
}

var tokenKinds = map[ui.TokenKind]string{
	ui.TokenStateIcon: "stateIcon",
	ui.TokenStateText: "stateText",
	ui.TokenPrimary:   "primary",
	ui.TokenSecondary: "secondary",
	ui.TokenValue:     "value",
	ui.TokenGitStatus: "git",
	ui.TokenGauge:     "gauge",
}

func rowOf(r ui.SidebarRow) Row {
	out := Row{
		Kind: rowKinds[r.Kind], Label: r.Label, Trailing: r.Trailing, State: r.State,
		Running: r.Running, Active: r.Active, Folded: r.Folded, Depth: r.Depth, Group: r.Group,
		Pane: r.Pane, Tab: r.Tab, Workspace: r.Workspace,
	}
	for _, line := range r.Lines {
		var toks []Token
		for _, t := range line {
			text := t.Text
			if t.Kind == ui.TokenGitStatus {
				text = gitStatus(t.Ahead, t.Behind)
			}
			if text == "" {
				continue
			}
			toks = append(toks, Token{Kind: tokenKinds[t.Kind], Text: text, Level: t.Level})
		}
		if len(toks) > 0 {
			out.Lines = append(out.Lines, toks)
		}
	}
	return out
}

func gitStatus(ahead, behind int) string {
	var b strings.Builder
	if ahead > 0 {
		b.WriteString("↑")
		b.WriteString(strconv.Itoa(ahead))
	}
	if behind > 0 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("↓")
		b.WriteString(strconv.Itoa(behind))
	}
	return b.String()
}

// State is what the window shows now.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := &s.snap
	view := sidebar.View{
		UI: s.ui, Snap: snap, Company: s.company,
		Workspace: s.workspace, Tab: s.tab, Focus: s.focus,
		Grouped: s.grouped, Now: time.Now(),
	}
	st := State{
		Session: s.name, Grouped: s.grouped, Workspace: s.workspace, Focus: s.focus,
		AreaCols: s.areaCols, AreaRows: s.areaRows,
	}
	if m := s.menu; m != nil {
		ms := &MenuState{Title: m.Title, X: m.X, Y: m.Y}
		for _, it := range m.Items {
			ms.Items = append(ms.Items, it.Label)
		}
		st.Menu = ms
	}
	st.Prompt = s.prompt
	st.Panel = s.panel
	st.Zoomed = s.zoom != 0
	st.Pinned = s.pinned != 0
	st.Theme = s.theme
	st.Dock = s.dockStateLocked()
	for _, d := range s.docs.list {
		if d.Workspace == s.workspace {
			st.Docs = append(st.Docs, DocTab{ID: d.ID, Kind: d.Kind, Title: d.Title, Workspace: d.Workspace})
			if d.ID == s.docs.active {
				doc := d
				st.Doc = &doc
			}
		}
	}
	if s.panel == "settings" {
		st.Settings = s.settingsStateLocked()
	}
	if s.panel == "spending" {
		st.Spending = s.spendingStateLocked(view.Now)
	}
	st.Spent = s.liveCostLocked()
	s.toolStateLocked(&st)
	st.Toasts = append([]Toast(nil), s.notes.shown...)
	if s.snap.Update != nil {
		st.Update = s.snap.Update.Ready
	}
	for _, a := range s.catalog {
		// Installed, and agents rather than the memory tools the catalog
		// lists with them, as the TUI's installedAgents.
		if a.Installed && a.Kind != "memory" {
			st.NewAgents = append(st.NewAgents, NewAgent{ID: a.ID, Name: a.Name})
		}
	}
	for _, r := range view.SpaceRows(sidebar.SpaceSource{Snap: snap, Folded: s.folded, Current: s.workspace}) {
		st.Spaces = append(st.Spaces, rowOf(r))
	}
	for _, r := range view.AgentRows("") {
		st.Agents = append(st.Agents, rowOf(r))
	}
	info := sidebar.PaneIndex(snap)
	cards(st.Spaces, st.Agents, snap, info, view.Now)
	for _, p := range snap.Panes {
		if p.Agent != "" && p.Running && sidebar.DisplayState(p) == "blocked" {
			st.Waiting++
		}
	}
	if ws, ok := workspaceIn(*snap, s.workspace); ok {
		for _, t := range ws.Tabs {
			if s.pinned != 0 && t.ID != s.pinned {
				continue
			}
			tr := TabRow{ID: t.ID, Name: t.Name, Active: t.ID == s.tab}
			if tr.Name == "" {
				tr.Name = "tab"
			}
			best := 0
			for _, id := range t.Panes {
				p := info[id]
				if p.Agent == "" {
					continue
				}
				state := sidebar.DisplayState(p)
				if pr := sidebar.AttentionPriority(state, p.Running); pr > best {
					best, tr.State = pr, state
				}
			}
			st.Tabs = append(st.Tabs, tr)
		}
	}
	for _, r := range s.rects {
		p := info[r.Pane]
		title := sidebar.StrippedTerminalTitle(p.Title)
		box := PaneBox{
			Pane: r.Pane, X: r.X, Y: r.Y, Cols: r.Cols, Rows: r.Rows,
			Title: title, Agent: p.Agent, State: sidebar.DisplayState(p),
			Focused: r.Pane == s.focus,
		}
		if pv := s.panes[r.Pane]; pv != nil && pv.scroll != nil {
			box.Scroll, box.History = pv.offset, pv.history
		}
		st.Panes = append(st.Panes, box)
	}
	return st
}

// StateJSON is State as QML reads it.
func (s *Session) StateJSON() []byte {
	b, _ := json.Marshal(s.State())
	return b
}

// Waiting is how many agents wait for an answer, and which has waited
// longest (the one whose state changed first: the attention queue's last
// word on equals), for the tray's "go to".
func (s *Session) Waiting() (count int, oldest uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var seq uint64
	for _, p := range s.snap.Panes {
		if p.Agent == "" || !p.Running || sidebar.DisplayState(p) != "blocked" {
			continue
		}
		count++
		if oldest == 0 || p.StateSeq < seq {
			oldest, seq = p.ID, p.StateSeq
		}
	}
	return count, oldest
}

// cards puts each space's and agent's card on its row.
func cards(spaces, agents []Row, snap *proto.SessionSnapshot, info map[uint64]proto.PaneInfo, now time.Time) {
	home, _ := os.UserHomeDir()
	type place struct {
		w   proto.WorkspaceInfo
		tab proto.TabInfo
	}
	where := map[uint64]place{}
	spaceOf := map[uint64]proto.WorkspaceInfo{}
	tally := map[uint64]*SpaceCard{}
	for _, w := range snap.Workspaces {
		spaceOf[w.ID] = w
		c := &SpaceCard{Name: w.Name, Branch: w.Branch, Ahead: w.Ahead, Behind: w.Behind}
		if w.Dir != "" {
			c.Folder = w.Dir
			if home != "" && (w.Dir == home || strings.HasPrefix(w.Dir, home+"/")) {
				c.Folder = "~" + strings.TrimPrefix(w.Dir, home)
			}
			if c.Name == "" {
				c.Name = filepath.Base(w.Dir)
			}
		}
		if c.Name == "" {
			c.Name = "space"
		}
		for _, t := range w.Tabs {
			for _, id := range t.Panes {
				where[id] = place{w, t}
				p := info[id]
				if p.Agent == "" || !p.Running {
					continue
				}
				c.Agents++
				switch sidebar.DisplayState(p) {
				case "working":
					c.Working++
				case "blocked":
					c.Waiting++
				case "done":
					c.Done++
				}
			}
		}
		tally[w.ID] = c
	}
	for i := range spaces {
		if spaces[i].Kind == "space" {
			spaces[i].Space = tally[spaces[i].Workspace]
		}
	}
	for i := range agents {
		r := &agents[i]
		if r.Kind != "agent" || r.Pane == 0 {
			continue
		}
		p, ok := info[r.Pane]
		if !ok {
			continue
		}
		state := sidebar.DisplayState(p)
		c := &AgentCard{Kind: p.Agent, Name: p.Agent, StateText: stateWords[state], Context: -1}
		if p.Display != "" {
			c.Name = p.Display
		}
		if label := p.StateLabels[state]; label != "" {
			c.StateText = label
		}
		if p.Named {
			c.Doing = p.Title
		} else {
			c.Doing = sidebar.StrippedTerminalTitle(p.Title)
		}
		if d := strings.TrimSpace(c.Doing); strings.EqualFold(d, c.Name) || strings.EqualFold(d, c.Kind) || shellTitle.MatchString(d) {
			// A title that only names the agent, or a shell's
			// user@host:folder left from before it started, says nothing
			// about what it is doing.
			c.Doing = ""
		}
		if pl, ok := where[r.Pane]; ok {
			name := pl.w.Name
			if name == "" {
				if sc := tally[pl.w.ID]; sc != nil {
					name = sc.Name
				}
			}
			c.Where = name
			if pl.tab.Name != "" {
				c.Where += " · " + pl.tab.Name
			}
		}
		u := ui.FormatUsage(p.Usage, now)
		c.Cost = u.Cost
		c.Context = p.Usage.ContextPercent()
		c.ContextLevel = u.ContextLevel
		r.Agent = c
	}
}

// shellTitle is a shell's own terminal title: user@host:folder.
var shellTitle = regexp.MustCompile(`^[^\s@]+@[^\s:]+:`)

// stateWords are the states as the sidebar says them.
var stateWords = map[string]string{
	"working": "working",
	"blocked": "needs you",
	"idle":    "idle",
	"done":    "done",
	"asleep":  "asleep",
}
