package ui

import (
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestAListThatOverflowsHasAScrollbarWhereItIsClicked: a list with more than
// fits gets herdr's scrollbar down its right edge, beside the scrolling
// entries only; the thumb sits at the top unscrolled and at the bottom
// scrolled all the way; the thumb is drawn apart from the track; and the bar
// hit-testing finds is the one drawn. If it regresses, the list gives no sign
// of how much is below it — the thumb in the track's style made the bar look
// like a fixed rule — or a click on the bar lands on the space beside it.
func TestAListThatOverflowsHasAScrollbarWhereItIsClicked(t *testing.T) {
	f := sidebarFrame(spaceRows(9), nil)
	const rows = 20
	spaces, _ := SidebarRegions(f, rows)

	bar, ok := SidebarScrollbarOf(f, rows, SidebarSpacesList)
	if !ok {
		t.Fatal("nine spaces overflow, so the list should have a scrollbar")
	}
	if bar.Y != spaces.Y+1 {
		t.Errorf("the track starts at %d, want under the heading at %d", bar.Y, spaces.Y+1)
	}
	if bar.ThumbTop != bar.Y || bar.ThumbLen < 1 || bar.ThumbLen >= bar.Rows {
		t.Errorf("unscrolled, the thumb is %d+%d on a track %d+%d", bar.ThumbTop, bar.ThumbLen, bar.Y, bar.Rows)
	}

	g := vt.NewGrid(40, rows, 0)
	Draw(g, f, DefaultTheme())
	theme := DefaultTheme()
	for y := bar.Y; y < bar.Y+bar.Rows; y++ {
		c := g.Line(y).Cell(bar.X)
		want, style := '▕', theme.Border
		if bar.OnThumb(y) {
			want, style = '▐', theme.SidebarGroupActive
		}
		if c.R != want || c.Style != style {
			t.Errorf("line %d column %d is %q %+v, want %q %+v", y, bar.X, c.R, c.Style, want, style)
		}
		if list, _, ok := SidebarScrollbarAt(f, bar.X, y, rows); !ok || list != SidebarSpacesList {
			t.Errorf("a press on the track at line %d is not on the spaces' bar", y)
		}
	}
	if _, _, ok := SidebarScrollbarAt(f, bar.X-1, bar.Y, rows); ok {
		t.Error("the column beside the track is the list's, not the bar's")
	}

	f.Spaces.Scroll = bar.Max
	end, _ := SidebarScrollbarOf(f, rows, SidebarSpacesList)
	if end.ThumbTop+end.ThumbLen != end.Y+end.Rows {
		t.Errorf("scrolled to the end, the thumb ends at %d, the track at %d", end.ThumbTop+end.ThumbLen, end.Y+end.Rows)
	}
}

// TestAListThatFitsHasNoScrollbar: herdr's should_show_scrollbar. If it
// regresses, a short list gives up a column to a bar with nowhere to go.
func TestAListThatFitsHasNoScrollbar(t *testing.T) {
	f := sidebarFrame(spaceRows(2), nil)
	if _, ok := SidebarScrollbarOf(f, 20, SidebarSpacesList); ok {
		t.Error("two spaces fit, yet the list has a scrollbar")
	}
}

// TestTheScrollbarMovesTheListLikeHerdrs: a press on the track centres the
// thumb there, and a held thumb follows the pointer by where it was held,
// reaching both ends of the list and never past them. If it regresses, the
// thumb jumps under the pointer when grabbed, or dragging it cannot reach
// the last space.
func TestTheScrollbarMovesTheListLikeHerdrs(t *testing.T) {
	f := sidebarFrame(spaceRows(20), nil)
	const rows = 20
	bar, ok := SidebarScrollbarOf(f, rows, SidebarSpacesList)
	if !ok {
		t.Fatal("twenty spaces overflow")
	}

	if got := bar.ScrollAt(bar.Y+bar.Rows-1, bar.ThumbLen/2); got != bar.Max {
		t.Errorf("a press at the bottom of the track scrolls to %d, want the end %d", got, bar.Max)
	}
	if got := bar.ScrollAt(bar.Y, bar.ThumbLen/2); got != 0 {
		t.Errorf("a press at the top of the track scrolls to %d, want 0", got)
	}
	if got := bar.ScrollAt(bar.Y+100, 0); got != bar.Max {
		t.Errorf("a drag far below the track scrolls to %d, want the end %d", got, bar.Max)
	}
	if got := bar.ScrollAt(-100, 0); got != 0 {
		t.Errorf("a drag far above the track scrolls to %d, want 0", got)
	}

	// Held by its middle and not moved, the thumb stays where it is.
	f.Spaces.Scroll = bar.Max / 2
	mid, _ := SidebarScrollbarOf(f, rows, SidebarSpacesList)
	grab := mid.ThumbLen / 2
	again := f
	again.Spaces.Scroll = mid.ScrollAt(mid.ThumbTop+grab, grab)
	if now, _ := SidebarScrollbarOf(again, rows, SidebarSpacesList); now.ThumbTop != mid.ThumbTop {
		t.Errorf("grabbing the thumb moved it from %d to %d", mid.ThumbTop, now.ThumbTop)
	}
}

// TestTheScrollbarLeavesTheHideHandleAlone: the list reaching the bottom of
// the sidebar stops its track above the corner the hide handle owns. If it
// regresses, the handle is drawn over and a press there both hides the
// sidebar and scrolls the list.
func TestTheScrollbarLeavesTheHideHandleAlone(t *testing.T) {
	f := sidebarFrame(spaceRows(20), nil)
	const rows = 20
	bar, ok := SidebarScrollbarOf(f, rows, SidebarSpacesList)
	if !ok {
		t.Fatal("twenty spaces overflow")
	}
	if handle := hideHandleRow(f, rows); bar.Y+bar.Rows > handle {
		t.Errorf("the track runs to %d, over the hide handle at %d", bar.Y+bar.Rows, handle)
	}
}

// TestTheThumbStandsApartFromTheTrackInEveryTheme: whatever the theme, the
// thumb is not drawn in the track's style. If it regresses, the thumb is
// invisible and the scrollbar reads as a fixed line that never moves, which
// is how it first shipped: the styles herdr's colours mapped to were the same
// muted one in every theme.
func TestTheThumbStandsApartFromTheTrackInEveryTheme(t *testing.T) {
	themes := map[string]Theme{"default": DefaultTheme()}
	for name := range palettes {
		themes[name] = ThemeFrom(config.Theme{Name: name})
	}
	for name, theme := range themes {
		if theme.SidebarGroupActive == theme.Border {
			t.Errorf("%s: the thumb and the track share the style %+v", name, theme.Border)
		}
	}
}
