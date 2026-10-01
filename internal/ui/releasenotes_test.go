package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheReleaseNotesPanelIsHerdrs: the panel says the version and whether
// it is an update ready or what is new, has a close button, and draws the
// notes as herdr does — a heading in capitals, a bullet, code without its
// backticks — scrolling to their end and no further. If it regresses, the
// notes show as raw markdown, or scroll off into nothing.
func TestTheReleaseNotesPanelIsHerdrs(t *testing.T) {
	body := "### New\n- the `files` panel, **bold**\n\n```\nmake dist\n```\n" + strings.Repeat("- more\n", 40)
	v := &ReleaseNotesView{Version: "v0.4.0", Body: body, Newer: true, Install: "run `tend update -handoff`"}
	g := vt.NewGrid(100, 30, 0)
	Draw(g, Frame{ReleaseNotes: v}, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	for _, want := range []string{"v0.4.0", "update ready", " esc close ", "● update ready", "NEW", "• the files panel, bold", "▏ make dist", "run tend update -handoff", "wheel ↑↓", "esc / enter"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "###") || strings.Contains(text, "`") || strings.Contains(text, "**") {
		t.Errorf("markdown left raw:\n%s", text)
	}
	geo, ok := ReleaseNotesLayout(100, 30)
	if !ok || geo.Box.Cols != 80 || geo.Box.Rows != 24 || !strings.HasPrefix(string([]rune(gridText(g)[geo.Close.Y])[geo.Close.X:]), " esc close ") {
		t.Errorf("an 80 by 24 panel with its close button where a click finds it: %+v", geo)
	}

	v.Scroll = 1 << 20
	end := ClampNotesScroll(v, 100, 30, DefaultTheme())
	if end <= 0 || end != ReleaseNotesLines(v, geo.Body.Cols, DefaultTheme())-geo.Body.Rows {
		t.Errorf("scrolling stops at the end: %d", end)
	}

	v.Newer, v.Scroll = false, 0
	g.Clear(vt.DefaultStyle)
	Draw(g, Frame{ReleaseNotes: v}, DefaultTheme())
	if text := strings.Join(gridText(g), "\n"); !strings.Contains(text, "what's new in this release") || strings.Contains(text, "● update ready") {
		t.Errorf("the running release's notes:\n%s", text)
	}
}

// TestUpdateReadyIsOnTheStatusBarAndTheMenu: a release found puts "update
// ready" at the right of the status bar, and the global menu offers its
// notes as "update ready" — or "what's new" once it runs, and nothing when
// there are none. If it regresses, a release found is said once in a
// notice and then nowhere.
func TestUpdateReadyIsOnTheStatusBarAndTheMenu(t *testing.T) {
	g := vt.NewGrid(100, 10, 0)
	Draw(g, Frame{Session: "work", UpdateReady: true}, DefaultTheme())
	if status := gridText(g)[10-StatusRows]; !strings.HasSuffix(strings.TrimRight(status, " "), "update ready") {
		t.Errorf("status bar: %q", status)
	}
	labels := func(m Menu) string {
		var out []string
		for _, it := range m.Items {
			out = append(out, it.Label)
		}
		return strings.Join(out, ", ")
	}
	if got := labels(GlobalMenu(false, true, true, 0, 0)); got != "about tend, all tools..., MCP servers, settings, keybinds, reload config, update ready ●, detach" {
		t.Errorf("ready: %s", got)
	}
	if got := labels(GlobalMenu(false, false, true, 0, 0)); got != "about tend, all tools..., MCP servers, settings, keybinds, reload config, what's new, detach" {
		t.Errorf("notes: %s", got)
	}
	if got := labels(GlobalMenu(false, false, false, 0, 0)); got != "about tend, all tools..., MCP servers, settings, keybinds, reload config, detach" {
		t.Errorf("none: %s", got)
	}
}

// TestTheMenuSaysWhichTendIsRunning: the version is shown with its v, a
// development build as it is, the server's only when it is another; and
// the global menu's about item carries a dot while the server is another
// build. If it regresses, nothing in tend says which version is running,
// or a server left behind by an update goes unseen.
func TestTheMenuSaysWhichTendIsRunning(t *testing.T) {
	for _, c := range []struct{ client, server, want string }{
		{"v0.6.1", "v0.6.1", "tend v0.6.1"},
		{"v0.6.1", "0.6.1", "tend v0.6.1"},
		{"v0.6.1", "v0.6.0", "tend v0.6.1 · server v0.6.0"},
		{"v0.6.1", "", "tend v0.6.1"},
		{"development build", "development build", "tend development build"},
	} {
		if got := VersionLine(c.client, c.server); got != c.want {
			t.Errorf("VersionLine(%q, %q) = %q, want %q", c.client, c.server, got, c.want)
		}
	}
	if m := GlobalMenu(true, false, false, 0, 0); m.Items[0].Label != "about tend ●" || m.Items[0].Action != MenuAbout {
		t.Errorf("menu: %+v", m.Items)
	}
}
