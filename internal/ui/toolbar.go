package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The toolbar is tend's own, not herdr's: a row of tools at the top of the
// sidebar, over the spaces, and a rule under it. It stays put while the
// lists under it scroll. What each tool does is the client's; this is only
// where they are drawn and where a click finds them.

// ToolbarRows is how many lines the toolbar takes: its icons and the rule.
const ToolbarRows = 2

// The tools, by the name a configuration and a key binding use.
const (
	ToolFiles    = "files"
	ToolAgents   = "agents"
	ToolSessions = "sessions"
	ToolIssues   = "issues"
	ToolErrors   = "errors"
	ToolTickets  = "tickets"
	ToolBrowser  = "browser"
	ToolContext  = "context"
)

// ToolbarItem is one tool as a frame shows it.
type ToolbarItem struct {
	ID    string
	Label string
	// Active is a tool whose thing is open (the files panel in this tab);
	// Focused is the keyboard's cursor on it; Hover the pointer over it;
	// Disabled a tool not there yet, drawn dimmed and doing nothing.
	Active, Focused, Hover, Disabled bool
}

// toolIcons are each tool's glyph in the files panel's icon themes
// ([files] icons), so the two read as one family: Font Awesome's folder,
// robot, globe and clipboard with a Nerd Font, their emoji otherwise, and a
// letter with neither — a glyph the font lacks is a box.
var toolIcons = map[string][3]string{
	ToolFiles:    {"\uf07b", "📁", "F"},
	ToolAgents:   {"\uee0d", "🤖", "A"},
	ToolSessions: {"\uf1da", "🕘", "S"},
	ToolIssues:   {"\uf09b", "🐙", "I"},
	ToolErrors:   {"\uf188", "🐞", "E"},
	ToolTickets:  {"\uf145", "🎫", "T"},
	ToolBrowser:  {"\uf0ac", "🌐", "B"},
	ToolContext:  {"\uf0ea", "📋", "C"},
}

// ToolIcon is a tool's glyph in an icon theme ("nerd", "emoji", else none).
func ToolIcon(id, theme string) string {
	icons, ok := toolIcons[id]
	if !ok {
		return "?"
	}
	switch theme {
	case "nerd":
		return icons[0]
	case "emoji":
		return icons[1]
	}
	return icons[2]
}

// toolbarShown is whether the frame has a toolbar and the sidebar room for
// it and both lists.
func toolbarShown(f Frame, rows int) bool {
	return f.Sidebar && len(f.Toolbar) > 0 && SidebarHeight(rows) >= ToolbarRows+2*sidebarMinSection+1
}

// toolbarTop is the first line under the toolbar.
func toolbarTop(f Frame, rows int) int {
	if toolbarShown(f, rows) {
		return ToolbarRows
	}
	return 0
}

// ToolbarZone is where one tool is on the toolbar's row: [From, To).
type ToolbarZone struct{ From, To int }

// ToolbarLayout is where each tool goes: a chip of its icon with a space
// either side, a column between two chips, from the second column. When the
// sidebar is too narrow for the gaps they go, and past that the tools that
// do not fit are left off rather than drawn over the edge.
func ToolbarLayout(f Frame) []ToolbarZone {
	width := sidebarWidthOf(f) - 1 // the rule down the edge
	chip := func(it ToolbarItem) int {
		return runewidth.StringWidth(" " + ToolIcon(it.ID, f.ToolbarIcons) + " ")
	}
	lay := func(gap int) ([]ToolbarZone, bool) {
		out := make([]ToolbarZone, 0, len(f.Toolbar))
		x := 1
		for i, it := range f.Toolbar {
			if i > 0 {
				x += gap
			}
			w := chip(it)
			if x+w > width {
				return out, false
			}
			out = append(out, ToolbarZone{x, x + w})
			x += w
		}
		return out, true
	}
	if zones, ok := lay(1); ok {
		return zones
	}
	zones, _ := lay(0)
	return zones
}

// ToolbarItemAt is the tool under a point, by its place in f.Toolbar.
func ToolbarItemAt(f Frame, x, y, rows int) (int, bool) {
	if !toolbarShown(f, rows) || y != 0 {
		return 0, false
	}
	for i, z := range ToolbarLayout(f) {
		if x >= z.From && x < z.To {
			return i, true
		}
	}
	return 0, false
}

// drawToolbar paints the tools and the rule under them. Every state is in
// the icon's own colour and weight, never a block behind it: a glyph sits
// high in its cell, and a background around it showed that. The active
// tool's icon is in the accent (the colour the tab in view is on), the
// pointer's in bold, the keyboard's cursor underlined, a disabled one
// dimmed.
func drawToolbar(dst *vt.Grid, f Frame, width int, theme Theme) {
	for i, z := range ToolbarLayout(f) {
		it := f.Toolbar[i]
		style := theme.Sidebar
		switch {
		case it.Disabled:
			style = theme.SidebarDetail
		case it.Active:
			style.FG = theme.TabActive.BG
			style.Attrs |= vt.AttrBold
		}
		if it.Hover && !it.Disabled {
			style.Attrs |= vt.AttrBold
		}
		if it.Focused {
			style.Attrs |= vt.AttrUnderline | vt.AttrBold
		}
		// The state is on the glyph alone: an underline under the spaces
		// either side would be wider than what it marks.
		x := writeString(dst, z.From, 0, " ", theme.Sidebar, z.To)
		x = writeString(dst, x, 0, ToolIcon(it.ID, f.ToolbarIcons), style, z.To)
		writeString(dst, x, 0, " ", theme.Sidebar, z.To)
	}
	row := dst.Line(1)
	if row == nil {
		return
	}
	for x := 0; x < width-1; x++ {
		row.SetCell(x, vt.Cell{R: '─', Style: theme.Border, Width: 1})
	}
}
