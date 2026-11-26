package main

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/sidebar"
)

// The outer window's title: herdr's `ui.window_title` (`app/window_title.rs`).
//
// A pane's OSC 0 stops at tend, because tend is the terminal the pane writes
// to. Without this the window tend runs in keeps whatever the shell or ssh
// left there, and a window manager's bar full of "zsh" says nothing about
// which window holds which session.
//
// herdr renders the title on the server and pushes it to the client. tend
// renders it here, from the snapshot, with the server's hostname carried in
// the snapshot — the same answer, because {hostname} still names the machine
// the panes are on, without a second message for something the client
// already has every part of.

const (
	// pushTitle and popTitle save the outer terminal's title and put it back
	// (xterm's window manipulation, which most terminals take). herdr writes
	// its own name on the way out; putting back what was there leaves the
	// window as tend found it, which is what detaching is for.
	pushTitle = "\x1b[22;0t"
	popTitle  = "\x1b[23;0t"
)

// mustTitle parses a template the configuration has already accepted. A
// template that somehow does not parse means no title, never a crash.
func mustTitle(template string) *config.WindowTitleTemplate {
	tmpl, err := config.ParseWindowTitle(template)
	if err != nil {
		return nil
	}
	return tmpl
}

// windowTitleLocked is the title to write now, or false for none. The caller
// holds the lock.
func (t *tui) windowTitleLocked() (string, bool) {
	if t.snap.WindowTitle != "" {
		// Set by a script over the API, and it wins until it is cleared.
		return config.SanitizeWindowTitle(t.snap.WindowTitle)
	}
	if t.titleTemplate == nil {
		return "", false
	}
	return t.titleTemplate.Render(func(token config.WindowTitleToken) string {
		switch token {
		case config.TitleHostname:
			return t.titleHostLocked()
		case config.TitleWorkspace:
			if w, ok := t.workspaceLocked(); ok {
				return w.Name
			}
		case config.TitleTab:
			for _, tab := range t.tabsLocked() {
				if tab.ID == t.tab {
					if tab.Name != "" {
						return tab.Name
					}
					// What the tab bar calls an unnamed tab.
					return strconv.FormatUint(tab.ID, 10)
				}
			}
		case config.TitlePane, config.TitleTerminal:
			for _, p := range t.snap.Panes {
				if p.ID != t.focus {
					continue
				}
				// The server sends one title: the user's name for the pane
				// when there is one, the program's own otherwise. So {pane}
				// is empty for a pane nobody named, and {terminal_title} for
				// one somebody did.
				if token == config.TitlePane && p.Named {
					return p.Title
				}
				if token == config.TitleTerminal && !p.Named {
					return strippedTerminalTitle(p.Title)
				}
			}
		}
		return ""
	})
}

// titleHostLocked is the server's machine: from the snapshot, or for a server
// too old to say, the host this client dialled, or this one.
func (t *tui) titleHostLocked() string {
	if t.snap.Hostname != "" {
		return t.snap.Hostname
	}
	if t.host != "" {
		return t.host
	}
	name, _ := os.Hostname()
	return name
}

// syncWindowTitle writes the title when it has changed. Called by the paint
// goroutine, outside the lock, with what was computed under it.
func (t *tui) syncWindowTitle(title string, ok bool) {
	if ok == t.titleShown && title == t.titleText {
		return
	}
	var out strings.Builder
	switch {
	case ok:
		if !t.titlePushed {
			out.WriteString(pushTitle)
			t.titlePushed = true
		}
		out.WriteString("\x1b]0;" + title + "\x07")
	case t.titlePushed:
		// Nothing to say any more — the template is now "" and the script's
		// title was cleared — so the window gets back what it had, and the
		// stack is kept for the next title.
		out.WriteString(popTitle + pushTitle)
	}
	t.titleText, t.titleShown = title, ok
	if out.Len() > 0 {
		_, _ = io.WriteString(os.Stdout, out.String())
	}
}

// restoreWindowTitle gives the window its title back on the way out.
func (t *tui) restoreWindowTitle() {
	if t.titlePushed {
		_, _ = io.WriteString(os.Stdout, popTitle)
		t.titlePushed = false
	}
}

// strippedTerminalTitle drops one leading spinner frame from a program's
// title; it is internal/sidebar's, where the agent list uses it too.
func strippedTerminalTitle(title string) string { return sidebar.StrippedTerminalTitle(title) }
