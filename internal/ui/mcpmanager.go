package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The MCP manager is tend's own (herdr has none): a panel over the screen
// with one row per MCP server and one column per agent, saying which agents
// have it and whether it is on, and what testing it found. It is drawn in
// the agent manager's colours, as the two are one kind of thing. What the
// switches do is the client's; this draws, and says where a click landed.

// MCPCell is one agent's state for one server.
type MCPCell uint8

const (
	// MCPNone is a server the agent does not have.
	MCPNone MCPCell = iota
	// MCPOn and MCPOff are a server the agent has, on or off.
	MCPOn
	MCPOff
)

// MCPRow is one server across the agents.
type MCPRow struct {
	Name      string
	Transport string
	Cells     []MCPCell
	// Differs marks the agents whose server of this name is not the same
	// server as most of the others'.
	Differs []bool
	// Test is what testing it found, and TestState its kind: "ok", "auth",
	// "bad", "running", or "" when it has not been tested.
	Test, TestState string
}

// MCPManagerView is the panel while it is up.
type MCPManagerView struct {
	// Agents are the columns' names.
	Agents []string
	Rows   []MCPRow
	// Row and Col are the cursor: Col 0 is the server's name, 1 the first
	// agent.
	Row, Col int
	Scroll   int
	Loading  bool
	Message  string
	Alert    bool
	// Confirm is a question waiting for enter (yes) or esc (no).
	Confirm string
	// Adding is the line a new server is typed on, while it is.
	Adding  bool
	AddText string
	// Detail, when set, is shown in place of the grid.
	Detail []string
}

// MCPManagerGeometry is where the panel's parts are.
type MCPManagerGeometry struct {
	Box  Rect
	List Rect
	// NameW is the width of the name column, ColX each agent column's x,
	// ColW their width, and TransportX and TestX the last two columns.
	NameW, ColW       int
	ColX              []int
	TransportX, TestX int
	Add, TestAll      Rect
	Refresh, Help     Rect
	Close             Rect
}

// MCPLegend says what the grid's marks mean.
const MCPLegend = "✓ on   ○ off   · this agent does not have it   ≠ not the same server as the others"

// MCPHelp is the panel's help, for somebody meeting MCP for the first time.
var MCPHelp = []string{
	"WHAT IS THIS",
	"  An MCP server gives your coding agents a tool: your notes, a database, a",
	"  browser, the docs of a library. Each agent keeps its own list; this shows",
	"  them all together — a row per server, a column per agent.",
	"",
	"THE MARKS",
	"  ✓ on: the agent uses it.   ○ off: kept, but not used.   · not set up there.",
	"  ≠ that agent's server of this name is a different one.",
	"",
	"WHAT YOU CAN DO",
	"  Pick a cell with the arrows or the mouse, then:",
	"  space (or click it again)  turn it off or on in that agent — or, on a ·,",
	"                             copy it there from an agent that has it",
	"  t                          test it: tend talks to it and says if it answers",
	"  a                          add a new one: a name and its address, e.g.",
	"                             notes https://example.com/mcp",
	"  d                          remove it (asks first; a backup is kept)",
	"  m or right-click           a menu of all of this, in words",
	"",
	"  Changes are used from each agent's next session; the ones open now keep",
	"  what they started with. The menu (bottom of the spaces list) opens this",
	"  too, under \"MCP servers\".",
}

const (
	mcpNameW = 22
	mcpColW  = 9
)

// MCPManagerLayout is the panel's geometry on a screen of cols by rows.
func MCPManagerLayout(v *MCPManagerView, cols, rows int) MCPManagerGeometry {
	w := min(cols, 116)
	h := min(rows, max(len(v.Rows)+10, len(v.Detail)+8, 14))
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := MCPManagerGeometry{Box: box, NameW: mcpNameW, ColW: mcpColW}
	g.List = Rect{X: box.X + 2, Y: box.Y + 4, Cols: box.Cols - 4, Rows: max(box.Rows-10, 0)}
	x := g.List.X + mcpNameW + 1
	for range v.Agents {
		g.ColX = append(g.ColX, x)
		x += mcpColW
	}
	g.TransportX = x + 1
	g.TestX = x + 8
	bottom := box.Y + box.Rows - 2
	at := box.X + 2
	button := func(label string) Rect {
		r := Rect{X: at, Y: bottom, Cols: runewidth.StringWidth(label), Rows: 1}
		at += r.Cols + 2
		return r
	}
	g.Add = button("[ Add ]")
	g.TestAll = button("[ Test all ]")
	g.Refresh = button("[ Refresh ]")
	g.Help = button("[ Help ]")
	g.Close = Rect{X: box.X + box.Cols - 2 - runewidth.StringWidth("[ Close ]"), Y: bottom, Cols: runewidth.StringWidth("[ Close ]"), Rows: 1}
	return g
}

func mcpTop(v *MCPManagerView, g MCPManagerGeometry) int {
	return min(v.Scroll, max(len(v.Rows)-g.List.Rows, 0))
}

// MCPManagerCellAt is the row and column at a point of the grid: column 0
// for the name, and for anything right of the agents.
func MCPManagerCellAt(v *MCPManagerView, cols, rows, x, y int) (row, col int, ok bool) {
	g := MCPManagerLayout(v, cols, rows)
	if v.Detail != nil || x < g.List.X || x >= g.List.X+g.List.Cols || y < g.List.Y || y >= g.List.Y+g.List.Rows {
		return 0, 0, false
	}
	row = mcpTop(v, g) + y - g.List.Y
	if row >= len(v.Rows) {
		return 0, 0, false
	}
	for i, cx := range g.ColX {
		if x >= cx && x < cx+g.ColW {
			return row, i + 1, true
		}
	}
	return row, 0, true
}

// MCPManagerScrollFor is the scroll that keeps the cursor's row in view.
func MCPManagerScrollFor(v *MCPManagerView, cols, rows int) int {
	g := MCPManagerLayout(v, cols, rows)
	top := mcpTop(v, g)
	if v.Row < top {
		top = v.Row
	}
	if g.List.Rows > 0 && v.Row >= top+g.List.Rows {
		top = v.Row - g.List.Rows + 1
	}
	return max(top, 0)
}

// mcpCellText is how one cell reads: ✓ on, ○ off, · none, and ≠ beside
// a server that is not the same as the others of its name.
func mcpCellText(c MCPCell, differs bool) string {
	s := "·"
	switch c {
	case MCPOn:
		s = "✓"
	case MCPOff:
		s = "○"
	}
	if differs {
		s += "≠"
	}
	return s
}

func drawMCPManager(dst *vt.Grid, v *MCPManagerView, theme Theme) {
	g := MCPManagerLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 12 || box.Cols < 60 {
		return
	}
	right := box.X + box.Cols - 1
	end := g.List.X + g.List.Cols
	title := "MCP SERVERS"
	writeString(dst, box.X+(box.Cols-len(title))/2, box.Y+1, title, withBold(theme.NotesAccent), right)
	if v.Detail == nil {
		// What the marks mean, always in view: the grid is read by
		// somebody who has not learnt it yet as often as by somebody who
		// has.
		writeString(dst, g.List.X, box.Y+2, truncate(MCPLegend, g.List.Cols), theme.NotesSub, end)
	}

	if v.Detail != nil {
		for i, line := range v.Detail {
			if i >= box.Rows-7 {
				break
			}
			writeString(dst, g.List.X, box.Y+3+i, truncate(line, g.List.Cols), theme.Notes, end)
		}
	} else {
		head := box.Y + 3
		writeString(dst, g.List.X, head, "SERVER", theme.NotesSub, end)
		for i, name := range v.Agents {
			writeString(dst, g.ColX[i], head, truncate(name, g.ColW-1), theme.NotesSub, end)
		}
		writeString(dst, g.TransportX, head, "VIA", theme.NotesSub, end)
		writeString(dst, g.TestX, head, "TEST", theme.NotesSub, end)
		if v.Loading {
			writeString(dst, g.List.X, g.List.Y, "reading the agents' settings…", theme.NotesSub, end)
		} else if len(v.Rows) == 0 {
			writeString(dst, g.List.X, g.List.Y, "no MCP servers in any agent yet — a adds one", theme.NotesSub, end)
		}
		top := mcpTop(v, g)
		for line := 0; line < g.List.Rows && top+line < len(v.Rows); line++ {
			i := top + line
			r := v.Rows[i]
			y := g.List.Y + line
			name := theme.Notes
			if i == v.Row {
				name = withBold(theme.Notes)
				if v.Col == 0 {
					name = theme.NotesButton
					for x := g.List.X; x < g.List.X+g.NameW; x++ {
						setCell(dst, x, y, ' ', name)
					}
				}
			}
			writeString(dst, g.List.X, y, truncate(r.Name, g.NameW), name, end)
			for c := range v.Agents {
				cell, differs := MCPNone, false
				if c < len(r.Cells) {
					cell = r.Cells[c]
				}
				if c < len(r.Differs) {
					differs = r.Differs[c]
				}
				style := theme.NotesSub
				switch {
				case cell == MCPOn:
					style = theme.NotesAccent
				case differs:
					style = theme.Notes
				}
				if i == v.Row && c+1 == v.Col {
					style = theme.NotesButton
					for x := g.ColX[c]; x < g.ColX[c]+g.ColW-1; x++ {
						setCell(dst, x, y, ' ', style)
					}
				}
				writeString(dst, g.ColX[c]+g.ColW/2-1, y, mcpCellText(cell, differs), style, end)
			}
			writeString(dst, g.TransportX, y, r.Transport, theme.NotesSub, end)
			test := theme.NotesSub
			switch r.TestState {
			case "ok":
				test = theme.NotesAccent
			case "bad":
				test = theme.Blocked
			}
			writeString(dst, g.TestX, y, truncate(r.Test, max(end-g.TestX, 0)), test, end)
		}
	}

	msgY := box.Y + box.Rows - 4
	hintY := box.Y + box.Rows - 3
	switch {
	case v.Adding:
		line := "add: " + v.AddText + "█"
		writeString(dst, box.X+2, msgY, truncateLeft(line, box.Cols-4), withBold(theme.Notes), right)
		writeString(dst, box.X+2, hintY, truncate("name https://host/mcp · name sse https://… · name KEY=v -H 'K: v' -- command args · enter · esc", box.Cols-4), theme.NotesSub, right)
	case v.Confirm != "":
		writeString(dst, box.X+2, msgY, truncate(v.Confirm, box.Cols-4), withBold(theme.Notes), right)
		writeString(dst, box.X+2, hintY, "enter yes · esc no", theme.NotesSub, right)
	default:
		if v.Message != "" {
			style := theme.NotesSub
			if v.Alert {
				style = theme.Blocked
			}
			writeString(dst, box.X+2, msgY, truncate(v.Message, box.Cols-4), style, right)
		}
		hint := "space on/off · t test · a add · d remove · m or right-click: menu · ? help · esc close"
		if v.Detail != nil {
			hint = "esc back"
		}
		writeString(dst, box.X+2, hintY, truncate(hint, box.Cols-4), theme.NotesSub, right)
	}
	for _, b := range []struct {
		r     Rect
		label string
	}{{g.Add, "[ Add ]"}, {g.TestAll, "[ Test all ]"}, {g.Refresh, "[ Refresh ]"}, {g.Help, "[ Help ]"}, {g.Close, "[ Close ]"}} {
		writeString(dst, b.r.X, b.r.Y, b.label, theme.NotesAccent, right)
	}
}
