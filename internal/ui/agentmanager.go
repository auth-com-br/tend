package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The agent manager is tend's own: a panel over the screen listing the
// agent CLIs tend knows of, the ones on this machine first with their
// version, then the ones that are not with a way to install each. It is
// drawn in the release notes panel's colours, so the two read as one kind
// of thing. Installing is the client's: this draws, and says where a click
// landed.

// AgentEntry is one agent as the manager lists it.
type AgentEntry struct {
	Name      string
	Installed bool
	Version   string
	Path      string
	// InstallCommand is what installing runs, empty when nothing can here;
	// Missing is then the program the vendor's way needs.
	InstallCommand string
	Missing        string
	// Memory is a memory tool, listed under its own heading; Running a
	// service of one answering here.
	Memory  bool
	Running bool
}

// AgentManagerView is the panel while it is up.
type AgentManagerView struct {
	// Agents are installed first, then the rest (the client orders them).
	Agents  []AgentEntry
	Cursor  int
	Loading bool
	// Confirm is set once an install has been asked for, and before it is
	// run: the command is shown, and asked about again.
	Confirm bool
	// Message is a line under the list: an error, or what happened.
	Message string
	Scroll  int
}

// AgentManagerGeometry is where the panel's parts are, for drawing and for
// a click alike.
type AgentManagerGeometry struct {
	Box Rect
	// List is where the entries go, one line each, headings among them.
	List Rect
	// Refresh and Close are the buttons on the bottom line.
	Refresh, Close Rect
	// Rows maps each line of List to the entry on it, -1 for a heading or
	// a blank.
	Rows []int
}

const (
	agentManagerCols = 72
	agentManagerMax  = 26
)

// agentLines is the list as lines: a heading, the installed, a blank, a
// heading, the rest. Each is an entry's index, or -1 with a label.
func agentLines(v *AgentManagerView) (idx []int, labels []string) {
	add := func(i int, label string) { idx, labels = append(idx, i), append(labels, label) }
	var installed, available, memory []int
	for i, a := range v.Agents {
		switch {
		case a.Memory:
			memory = append(memory, i)
		case a.Installed:
			installed = append(installed, i)
		default:
			available = append(available, i)
		}
	}
	if len(installed) > 0 {
		add(-1, "INSTALLED")
		for _, i := range installed {
			add(i, "")
		}
	}
	if len(available) > 0 {
		if len(installed) > 0 {
			add(-1, "")
		}
		add(-1, "AVAILABLE")
		for _, i := range available {
			add(i, "")
		}
	}
	// What gives the agents memory (docs/MEMORY.md), apart from them.
	if len(memory) > 0 {
		if len(idx) > 0 {
			add(-1, "")
		}
		add(-1, "MEMORY")
		for _, i := range memory {
			add(i, "")
		}
	}
	return idx, labels
}

// AgentManagerLayout is the panel's geometry on a screen of cols by rows.
func AgentManagerLayout(v *AgentManagerView, cols, rows int) AgentManagerGeometry {
	idx, _ := agentLines(v)
	// A title, a blank, the list, a blank, a message line, a hint, the
	// buttons, inside a frame.
	h := min(len(idx)+9, agentManagerMax, rows)
	w := min(agentManagerCols, cols)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: w, Rows: h}
	g := AgentManagerGeometry{Box: box}
	g.List = Rect{X: box.X + 2, Y: box.Y + 3, Cols: box.Cols - 4, Rows: max(box.Rows-9, 0)}
	top := min(v.Scroll, max(len(idx)-g.List.Rows, 0))
	for i := 0; i < g.List.Rows; i++ {
		if top+i < len(idx) {
			g.Rows = append(g.Rows, idx[top+i])
		} else {
			g.Rows = append(g.Rows, -1)
		}
	}
	bottom := box.Y + box.Rows - 2
	g.Refresh = Rect{X: box.X + 2, Y: bottom, Cols: runewidth.StringWidth("[ Refresh ]"), Rows: 1}
	g.Close = Rect{X: box.X + box.Cols - 2 - runewidth.StringWidth("[ Close ]"), Y: bottom, Cols: runewidth.StringWidth("[ Close ]"), Rows: 1}
	return g
}

// AgentManagerEntryAt is the entry on a line of the list, if one is.
func AgentManagerEntryAt(v *AgentManagerView, cols, rows, x, y int) (int, bool) {
	g := AgentManagerLayout(v, cols, rows)
	if x < g.List.X || x >= g.List.X+g.List.Cols || y < g.List.Y || y >= g.List.Y+g.List.Rows {
		return 0, false
	}
	i := g.Rows[y-g.List.Y]
	return i, i >= 0
}

// AgentManagerScrollFor is the scroll that keeps the cursor's entry in view.
func AgentManagerScrollFor(v *AgentManagerView, cols, rows int) int {
	idx, _ := agentLines(v)
	g := AgentManagerLayout(v, cols, rows)
	at := 0
	for line, i := range idx {
		if i == v.Cursor {
			at = line
		}
	}
	top := min(v.Scroll, max(len(idx)-g.List.Rows, 0))
	if at < top {
		top = at
	}
	if g.List.Rows > 0 && at >= top+g.List.Rows {
		top = at - g.List.Rows + 1
	}
	return max(top, 0)
}

func drawAgentManager(dst *vt.Grid, v *AgentManagerView, theme Theme) {
	g := AgentManagerLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 8 || box.Cols < 30 {
		return
	}
	right := box.X + box.Cols - 1
	title := "AGENT MANAGER"
	writeString(dst, box.X+(box.Cols-len(title))/2, box.Y+1, title, withBold(theme.NotesAccent), right)

	_, labels := agentLines(v)
	idx, _ := agentLines(v)
	top := 0
	if len(g.Rows) > 0 {
		top = min(v.Scroll, max(len(idx)-g.List.Rows, 0))
	}
	end := g.List.X + g.List.Cols
	if v.Loading {
		writeString(dst, g.List.X, g.List.Y, "looking for agents…", theme.NotesSub, end)
	}
	for line := 0; line < g.List.Rows && top+line < len(idx); line++ {
		y := g.List.Y + line
		i := idx[top+line]
		if i < 0 {
			writeString(dst, g.List.X, y, labels[top+line], theme.NotesSub, end)
			continue
		}
		a := v.Agents[i]
		selected := i == v.Cursor
		base := theme.Notes
		if selected {
			base = theme.NotesButton
			for x := g.List.X; x < end; x++ {
				setCell(dst, x, y, ' ', base)
			}
		}
		mark, markStyle := "○", theme.NotesSub
		if a.Installed {
			mark, markStyle = "●", theme.NotesAccent
		}
		if selected {
			markStyle = base
		}
		x := writeString(dst, g.List.X+1, y, mark, markStyle, end)
		writeString(dst, x+1, y, truncate(a.Name, 22), base, end)
		detail, detailStyle := "", theme.NotesSub
		switch {
		case a.Installed:
			detail = truncate(a.Version, g.List.Cols-28)
			if a.Running && a.Path == "" {
				detail = "running"
			}
			if detail == "" {
				detail = "installed"
			}
		case a.InstallCommand != "":
			detail, detailStyle = "[ install ]", theme.NotesAccent
		case a.Missing != "":
			detail = "needs " + a.Missing
		default:
			detail = "install by hand"
		}
		if selected {
			detailStyle = base
		}
		writeString(dst, g.List.X+26, y, detail, detailStyle, end)
	}

	msgY := box.Y + box.Rows - 4
	hintY := box.Y + box.Rows - 3
	if v.Confirm && v.Cursor >= 0 && v.Cursor < len(v.Agents) {
		cmd := v.Agents[v.Cursor].InstallCommand
		writeString(dst, box.X+2, msgY, truncate("install: "+cmd, box.Cols-4), withBold(theme.Notes), right)
		writeString(dst, box.X+2, hintY, "enter runs it in a new tab · esc cancels", theme.NotesSub, right)
	} else {
		if v.Message != "" {
			writeString(dst, box.X+2, msgY, truncate(v.Message, box.Cols-4), theme.NotesSub, right)
		}
		writeString(dst, box.X+2, hintY, "j k move · enter install · r refresh · esc close", theme.NotesSub, right)
	}
	writeString(dst, g.Refresh.X, g.Refresh.Y, "[ Refresh ]", theme.NotesAccent, right)
	writeString(dst, g.Close.X, g.Close.Y, "[ Close ]", theme.NotesAccent, right)
}
