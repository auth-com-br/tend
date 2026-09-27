package ui

import (
	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The about panel is tend's own, opened from the global menu: which tend is
// running and the server's, who makes it, where it lives, and its licence.
// It is drawn in the release notes panel's colours, as the other panels
// are. herdr has none — its version is on its command line only.

// The project's addresses, which the panel opens. Sponsor is last and
// plain, a line among the others: a way to support tend for whoever looks
// for one, not a request made of everybody who opens the panel.
const (
	SiteURL    = "https://tend.auth.com.br/"
	SourceURL  = "https://github.com/auth-com-br/tend"
	SponsorURL = "https://github.com/sponsors/sousaakira"
)

// AboutView is the panel while it is up.
type AboutView struct {
	// Version is this client's build and Server the server's; Stale is a
	// server that is another build, which HandoffHint says how to bring
	// along.
	Version     string
	Server      string
	Stale       bool
	HandoffHint string
	// Notes is whether there are release notes to open.
	Notes bool
	// Message is a line of what happened: a page opened, or not.
	Message string
}

// About buttons, and the two lines a click opens.
const (
	AboutNotes   = "notes"
	AboutSite    = "site"
	AboutSource  = "source"
	AboutSponsor = "sponsor"
	AboutClose   = "close"
)

// AboutGeometry is where the panel's parts are.
type AboutGeometry struct {
	Box Rect
	// Site, Source and Sponsor are the address lines, which open on a
	// click.
	Site, Source, Sponsor Rect
	Buttons               []IssueButton
}

const (
	aboutCols = 64
	aboutRows = 17
)

// aboutLabelX is where the values start, after the labels.
const aboutLabelX = 12

// AboutLayout is the panel's geometry on a screen of cols by rows.
func AboutLayout(v *AboutView, cols, rows int) AboutGeometry {
	w, h := min(aboutCols, cols-2), min(aboutRows, rows-2)
	box := Rect{X: (cols - w) / 2, Y: (rows - h) / 2, Cols: max(w, 0), Rows: max(h, 0)}
	g := AboutGeometry{Box: box}
	vx := box.X + 3 + aboutLabelX
	g.Site = Rect{X: vx, Y: box.Y + 9, Cols: runewidth.StringWidth(SiteURL), Rows: 1}
	g.Source = Rect{X: vx, Y: box.Y + 10, Cols: runewidth.StringWidth(SourceURL), Rows: 1}
	g.Sponsor = Rect{X: vx, Y: box.Y + 12, Cols: runewidth.StringWidth(SponsorURL), Rows: 1}
	buttons := []IssueButton{}
	if v.Notes {
		buttons = append(buttons, IssueButton{ID: AboutNotes, Label: "[ What's new ]"})
	}
	buttons = append(buttons, IssueButton{ID: AboutSite, Label: "[ Open site ]"}, IssueButton{ID: AboutClose, Label: "[ Close ]"})
	bx, bottom := box.X+3, box.Y+box.Rows-2
	for _, b := range buttons {
		b.Rect = Rect{X: bx, Y: bottom, Cols: runewidth.StringWidth(b.Label), Rows: 1}
		if b.ID == AboutClose {
			b.X = box.X + box.Cols - 3 - b.Cols
		} else {
			bx += b.Cols + 1
		}
		g.Buttons = append(g.Buttons, b)
	}
	return g
}

// AboutAt is what a click at a point is on: a button, or an address line.
func AboutAt(v *AboutView, cols, rows, x, y int) (string, bool) {
	g := AboutLayout(v, cols, rows)
	if OnCloseMark(g.Box, x, y) {
		return AboutClose, true
	}
	for _, b := range g.Buttons {
		if y == b.Y && x >= b.X && x < b.X+b.Cols {
			return b.ID, true
		}
	}
	for id, r := range map[string]Rect{AboutSite: g.Site, AboutSource: g.Source, AboutSponsor: g.Sponsor} {
		if y == r.Y && x >= r.X && x < r.X+r.Cols {
			return id, true
		}
	}
	return "", false
}

func drawAbout(dst *vt.Grid, v *AboutView, theme Theme) {
	g := AboutLayout(v, dst.Cols(), dst.Rows())
	box := g.Box
	for y := box.Y; y < box.Y+box.Rows; y++ {
		for x := box.X; x < box.X+box.Cols; x++ {
			setCell(dst, x, y, ' ', theme.Notes)
		}
	}
	drawBox(dst, box, theme.NotesAccent)
	drawCloseMark(dst, box, withBold(theme.NotesAccent))
	if box.Rows < 14 || box.Cols < 40 {
		return
	}
	right := box.X + box.Cols - 1
	// Its name on the top edge, as a menu's is.
	writeString(dst, box.X+2, box.Y, " about ", withBold(theme.NotesAccent), right)
	x0 := box.X + 3
	writeString(dst, x0, box.Y+2, "tend", withBold(theme.NotesAccent), right)
	writeString(dst, x0, box.Y+3, "a terminal runtime for coding agents", theme.NotesSub, right)

	row := func(y int, label, value string, style vt.Style) {
		writeString(dst, x0, y, label, theme.NotesSub, right)
		writeString(dst, x0+aboutLabelX, y, truncate(value, box.Cols-aboutLabelX-6), style, right)
	}
	row(box.Y+5, "version", v.Version, withBold(theme.Notes))
	server := v.Server
	style := theme.Notes
	if v.Stale {
		server += " — " + v.HandoffHint
		style = withBold(theme.NotesAccent)
	}
	if server != "" {
		row(box.Y+6, "server", server, style)
	}
	row(box.Y+7, "made by", "Auth Tecnologia Ltda · Akira Sousa", theme.Notes)
	row(box.Y+9, "site", SiteURL, theme.NotesAccent)
	row(box.Y+10, "source", SourceURL, theme.NotesAccent)
	row(box.Y+11, "license", "Apache-2.0 · see NOTICE", theme.Notes)
	row(box.Y+12, "sponsor", SponsorURL, theme.NotesAccent)

	if v.Message != "" {
		writeString(dst, x0, box.Y+box.Rows-4, truncate(v.Message, box.Cols-6), theme.NotesSub, right)
	}
	for _, b := range g.Buttons {
		writeString(dst, b.X, b.Y, b.Label, theme.NotesAccent, right)
	}
}
