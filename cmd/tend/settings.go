package main

import (
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/notify"
	"github.com/auth-com-br/tend/internal/ui"
)

// Settings are read at start and can be changed while tend runs: herdr
// reloads on a key (`ReloadConfig`) and tend does the same on ctrl+b R.
//
// The file is read twice, once on each side. What the client is told to do —
// the theme, the sidebar, what a notification looks like — it applies itself;
// what belongs to the server — how often panes are examined, how much
// scrollback a new pane keeps — the server re-reads on its own, because it is
// the process that lives with it and may be on another machine entirely.

// reloadSettings re-reads the settings file and applies it here and there.
func (t *tui) reloadSettings() error {
	cfg, err := config.Load()
	if err != nil {
		// A file that will not parse leaves everything as it was: half-applied
		// settings are worse than old ones.
		t.setMessage(err.Error(), true)
		return nil
	}

	t.mu.Lock()
	previousPrefix := t.config.Keys.Prefix
	t.config = cfg
	t.theme = ui.ThemeFor(cfg.UI.Theme, t.hostLight)
	t.titleTemplate = mustTitle(cfg.UI.WindowTitle)
	t.sidebar = cfg.SidebarShown()
	t.grouped = cfg.UI.Grouped
	t.toasts = cfg.Toasts()
	t.notifyDelay = cfg.NotifyDelay()
	t.notifyFocused = cfg.Notify.Focused
	t.sound = &notify.Player{
		Enabled: cfg.Sound.Enabled,
		Done:    expandHome(cfg.Sound.Done),
		Request: expandHome(cfg.Sound.Request),
		Bell:    bell,
	}
	t.dirty = true
	t.mu.Unlock()

	// Turned on by this reload, the terminal has to be asked.
	t.askHostScheme()

	if prefix, err := cfg.PrefixKey(); err == nil {
		t.keys.PrefixKey = prefix
	}
	if bindings, _, err := ui.BindingsFrom(cfg.Keys.Bind); err != nil {
		t.setMessage(err.Error(), true)
	} else {
		t.keys.Bindings = bindings
	}
	if copyKeys, _, err := ui.CopyKeysFrom(cfg.Keys.CopyMode); err != nil {
		t.setMessage(err.Error(), true)
	} else {
		t.mu.Lock()
		t.copyKeyMap = copyKeys
		t.mu.Unlock()
	}
	if custom, _, err := ui.CustomFrom(cfg.Keys.Command, t.keys.Bindings); err != nil {
		t.setMessage(err.Error(), true)
	} else {
		t.keys.Custom = custom
	}
	changedPrefix := previousPrefix != cfg.Keys.Prefix

	// The server's half. An older server has never heard of the method, which
	// is worth saying rather than silently applying half the file.
	said := []string{"settings reloaded"}
	result, err := t.client.ReloadConfig()
	switch {
	case err != nil && t.reportStaleServer(err):
		return nil
	case err != nil:
		said = append(said, "server: "+err.Error())
	case result.Err != "":
		said = append(said, "server: "+result.Err)
	case len(result.Changed) > 0:
		said = append(said, "server: "+strings.Join(result.Changed, ", "))
	}
	if changedPrefix {
		said = append(said, "prefix is now "+cfg.Keys.Prefix)
	}

	t.requestRepaint()
	t.setMessage(strings.Join(said, " · "), false)
	return t.refresh()
}
