package main

import (
	"github.com/auth-com-br/tend/internal/ui"
)

// The version, and the about panel (internal/ui/about.go): tend's own.
// herdr says its version on its command line only; here the global menu's
// "about tend" says which tend is running, and whether the server is the
// same build — the server outlives the client, and one left behind by an
// update is the first thing to know about when something is missing.

// serverBuild is the build of the server this client talks to.
func (t *tui) serverBuild() string {
	if t.client == nil {
		return ""
	}
	return t.client.Server().Build
}

// serverStale is whether the server is another build than this client.
func (t *tui) serverStale() bool {
	server := t.serverBuild()
	return server != "" && ui.VersionLine(version, server) != ui.VersionLine(version, "")
}

// versionLine is which tend is running, the client's build and the
// server's when it is another, for the top of the help.
func (t *tui) versionLine() string {
	return ui.VersionLine(version, t.serverBuild())
}

// helpLines is the list of keys, under which tend it is.
func (t *tui) helpLines() []string {
	help := append([]string{t.versionLine(), ""}, ui.HelpLinesFor(t.keys.Bindings)...)
	return append(help, ui.CustomHelpLines(t.config.Keys.Command)...)
}

func (t *tui) aboutUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.about != nil
}

// openAbout puts the about panel up.
func (t *tui) openAbout() {
	t.mu.Lock()
	notes := t.notesAvailableLocked()
	t.mu.Unlock()
	v := &ui.AboutView{
		Version:     ui.DisplayVersion(version),
		Server:      ui.DisplayVersion(t.serverBuild()),
		Stale:       t.serverStale(),
		HandoffHint: "run " + handoffCommand(t.session),
		Notes:       notes,
	}
	t.mu.Lock()
	t.about = v
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) closeAbout() {
	t.mu.Lock()
	t.about = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// aboutInput is every key and click while the panel is up: esc, enter or
// q closes it; o opens the site, n the release notes; a click on an
// address opens it.
func (t *tui) aboutInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		t.mu.Lock()
		v, cols, rows := t.about, t.cols, t.rows
		t.mu.Unlock()
		if v == nil {
			return nil
		}
		if id, ok := ui.AboutAt(v, cols, rows, ev.X, ev.Y); ok {
			return t.aboutAction(id)
		}
	}
	for _, key := range splitKeys(forward) {
		switch key {
		case "\x1b", "\r", "\n", "q":
			t.closeAbout()
			return nil
		case "o":
			return t.aboutAction(ui.AboutSite)
		case "n":
			return t.aboutAction(ui.AboutNotes)
		}
	}
	return nil
}

func (t *tui) aboutAction(id string) error {
	switch id {
	case ui.AboutClose:
		t.closeAbout()
	case ui.AboutNotes:
		t.mu.Lock()
		notes := t.about != nil && t.about.Notes
		t.mu.Unlock()
		if notes {
			t.closeAbout()
			return t.openReleaseNotes()
		}
	case ui.AboutSite, ui.AboutSource, ui.AboutSponsor:
		url := map[string]string{
			ui.AboutSite:    ui.SiteURL,
			ui.AboutSource:  ui.SourceURL,
			ui.AboutSponsor: ui.SponsorURL,
		}[id]
		message := "opened " + url
		if err := openURL(url); err != nil {
			message = url + " — " + err.Error()
		}
		t.mu.Lock()
		if t.about != nil {
			t.about.Message = message
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}
	return nil
}
