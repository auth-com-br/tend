package core

import (
	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/proto"
)

// The TUI's files panel is a pane: `tend files` docked along a tab's edge,
// the tab's full height, named "files" (cmd/tend/tui_files.go,
// filesTitle), which is how the TUI finds it again. The session is the
// same for both clients, so a tab the TUI gave a panel has it in the window
// too — and it drew there as a terminal of the TUI's tree beside the
// window's own files tab, the thing the window replaces. The window leaves
// it out: it is not drawn, and the other panes are laid out over its room.
// It is not closed: it is the TUI's, and goes on there.

// tuiFilesTitle is the name the TUI's files panel goes by.
const tuiFilesTitle = "files"

// tuiFilesPanel is the TUI's files panel among a tab's panes, or 0. A
// pane alone in its tab is not left out, whatever it is called: there
// would be nothing to show instead.
func (s *Session) tuiFilesPanel(rects []proto.PaneRect) uint64 {
	if len(rects) < 2 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	top, bottom := rects[0].Y, rects[0].Y+rects[0].Rows
	for _, r := range rects {
		top, bottom = min(top, r.Y), max(bottom, r.Y+r.Rows)
	}
	for _, p := range s.snap.Panes {
		if p.Title != tuiFilesTitle || p.Agent != "" {
			continue
		}
		for _, r := range rects {
			// Docked: the tab's full height.
			if r.Pane == p.ID && r.Y == top && r.Y+r.Rows == bottom {
				return p.ID
			}
		}
	}
	return 0
}

// layoutWithout lays a tab out with one docked pane left out: the server
// is asked for the layout of a wider tab, the pane is dropped, and what was
// beside it moves over, so the others fill the width the window has. How
// much wider depends on how the panel's split is kept — by ratio, so a
// wider tab gives it more too, or by width — so the width asked for is
// first scaled by the share the others had, then corrected by what is
// still missing, a few times at most.
func layoutWithout(c *client.Client, tab uint64, cols, rows int, rects []proto.PaneRect, panel uint64) ([]proto.PaneRect, error) {
	asked := cols
	for try := 0; ; try++ {
		out, span := without(rects, panel)
		if span == cols || span <= 0 || try == 4 {
			return clampTo(out, cols), nil
		}
		if try == 0 {
			asked = (asked*cols + span - 1) / span
		} else {
			asked += cols - span
		}
		if asked <= 0 {
			return clampTo(out, cols), nil
		}
		res, err := c.TabLayout(tab, asked, rows)
		if err != nil {
			return nil, err
		}
		rects = res.Panes
	}
}

// without is a layout with a pane dropped and the rest moved to the left
// edge, and how wide the rest is.
func without(rects []proto.PaneRect, panel uint64) ([]proto.PaneRect, int) {
	left, right := -1, 0
	for _, r := range rects {
		if r.Pane == panel {
			continue
		}
		if left < 0 || r.X < left {
			left = r.X
		}
		right = max(right, r.X+r.Cols)
	}
	var out []proto.PaneRect
	for _, r := range rects {
		if r.Pane != panel {
			r.X -= left
			out = append(out, r)
		}
	}
	return out, right - left
}

// clampTo keeps every pane inside the width there is.
func clampTo(rects []proto.PaneRect, cols int) []proto.PaneRect {
	for i := range rects {
		if over := rects[i].X + rects[i].Cols - cols; over > 0 {
			rects[i].Cols = max(1, rects[i].Cols-over)
		}
	}
	return rects
}
