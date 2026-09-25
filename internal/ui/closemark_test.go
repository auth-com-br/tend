package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestEveryPanelsCloseMarkIsWhereAClickFindsIt: each panel draws its ✕ on
// its top edge where a click is taken as closing it — the issues panel, the
// errors panel and its connect box, the about panel and the keys' help.
// If it regresses, the ✕ is drawn where a click does something else, or
// closes nothing.
func TestEveryPanelsCloseMarkIsWhereAClickFindsIt(t *testing.T) {
	const cols, rows = 120, 36
	markAt := func(g *vt.Grid, box Rect) bool {
		r := CloseMarkRect(box)
		return g.Line(r.Y).Cell(r.X+1).R == '✕'
	}

	issues := &IssuesView{Filters: []string{"open"}}
	g := vt.NewGrid(cols, rows, 0)
	Draw(g, Frame{Issues: issues}, DefaultTheme())
	box := IssuesLayout(issues, cols, rows).Box
	if !markAt(g, box) {
		t.Error("issues: no ✕ drawn")
	}
	if id, ok := IssueButtonAt(issues, cols, rows, CloseMarkRect(box).X+1, box.Y); !ok || id != IssueButtonClose {
		t.Errorf("issues: a click on the ✕ is %q %v", id, ok)
	}

	errs := &ErrorsView{Connect: &ErrorsConnect{}}
	g = vt.NewGrid(cols, rows, 0)
	Draw(g, Frame{Errors: errs}, DefaultTheme())
	eg := ErrorsLayout(errs, cols, rows)
	if !markAt(g, eg.ConnectBox) {
		t.Error("connect box: no ✕ drawn")
	}
	if id, _ := ErrorsAt(errs, cols, rows, CloseMarkRect(eg.ConnectBox).X+1, eg.ConnectBox.Y); id != ErrorsCancel {
		t.Errorf("connect box: a click on the ✕ is %q", id)
	}
	errs.Connect = nil
	if id, _ := ErrorsAt(errs, cols, rows, CloseMarkRect(eg.Box).X+1, eg.Box.Y); id != ErrorsClose {
		t.Errorf("errors: a click on the ✕ is %q", id)
	}

	about := &AboutView{Version: "v1"}
	g = vt.NewGrid(cols, rows, 0)
	Draw(g, Frame{About: about}, DefaultTheme())
	ab := AboutLayout(about, cols, rows).Box
	if !markAt(g, ab) {
		t.Error("about: no ✕ drawn")
	}
	if id, _ := AboutAt(about, cols, rows, CloseMarkRect(ab).X+1, ab.Y); id != AboutClose {
		t.Errorf("about: a click on the ✕ is %q", id)
	}

	help := HelpLines()
	g = vt.NewGrid(cols, rows, 0)
	Draw(g, Frame{Overlay: help}, DefaultTheme())
	hb, ok := OverlayBox(help, cols, rows)
	if !ok || !markAt(g, hb) || !OnCloseMark(hb, CloseMarkRect(hb).X+1, hb.Y) {
		t.Error("help: no ✕ where a click finds it")
	}
}

// TestTheAboutPanelsSponsorLineIsWhereAClickOpensIt: the sponsor address is
// drawn on the line a click takes as opening it. If it regresses, the line
// is drawn and a click on it does nothing, or opens another address.
func TestTheAboutPanelsSponsorLineIsWhereAClickOpensIt(t *testing.T) {
	const cols, rows = 120, 36
	about := &AboutView{Version: "v1"}
	g := vt.NewGrid(cols, rows, 0)
	Draw(g, Frame{About: about}, DefaultTheme())
	r := AboutLayout(about, cols, rows).Sponsor
	if got := gridText(g)[r.Y]; !strings.Contains(got, SponsorURL) {
		t.Fatalf("sponsor row %q, want %s on it", got, SponsorURL)
	}
	if id, _ := AboutAt(about, cols, rows, r.X+r.Cols-1, r.Y); id != AboutSponsor {
		t.Errorf("a click on the sponsor line is %q", id)
	}
}
