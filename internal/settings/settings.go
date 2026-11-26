// Package settings is the settings screen's rows: the handful of settings
// worth changing without opening an editor (herdr's prefix+s), each with
// its choices, where it lives in the settings file, and how to read the
// current one. It draws nothing. The TUI lists them in cells, tend-desktop
// natively; both write through config.Set and reload, so a change made in
// either is the same change, in the same file.
//
// It was cmd/tend's (settingsui.go) until the desktop needed it.
package settings

import (
	"strconv"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/integration"
)

// Row is one line in the screen.
type Row struct {
	Label string
	// Section and Key name the setting in the file.
	Section, Key string
	// Choices are what it can be, as shown and as written.
	Choices []Choice
	// Value reads the current one out of a configuration.
	Value func(config.Config) string
	// Apply, when set, does the change itself instead of writing Section
	// and Key to the file: an integration is installed, not configured.
	Apply func(next Choice) (string, error)
}

// Choice is one of what a row can be.
type Choice struct {
	Label string
	// TOML is the value as it goes into the file.
	TOML string
}

// Base is the screen's settings, in order.
var Base = []Row{
	{
		Label: "theme", Section: "ui.theme", Key: "name",
		Choices: ThemeChoices(),
		Value: func(c config.Config) string {
			name, _ := config.CanonicalTheme(c.UI.Theme.Name)
			return config.Quote(name)
		},
	},
	{
		// herdr's key, not tend's older `sidebar = true`: [ui.sidebar] is
		// also the table of what the rows show, and a file cannot have both.
		Label: "sidebar", Section: "ui", Key: "sidebar_start_collapsed",
		Choices: []Choice{{"on", "false"}, {"off", "true"}},
		Value:   func(c config.Config) string { return config.Bool(!c.SidebarShown()) },
	},
	{
		Label: "agent list", Section: "ui", Key: "grouped",
		Choices: []Choice{{"flat", "false"}, {"grouped", "true"}},
		Value:   func(c config.Config) string { return config.Bool(c.UI.Grouped) },
	},
	{
		// The files panel's (prefix+f). An open panel takes these up at its
		// next look, except the width, which is how it opens.
		Label: "files icons", Section: "files", Key: "icons",
		Choices: []Choice{{"none", `"none"`}, {"nerd font", `"nerd"`}, {"emoji", `"emoji"`}},
		Value: func(c config.Config) string {
			if c.Files.Icons == "" {
				return `"none"`
			}
			return config.Quote(c.Files.Icons)
		},
	},
	{
		Label: "files open", Section: "files", Key: "auto_open",
		Choices: []Choice{{"in every tab", "true"}, {"when asked (prefix+f)", "false"}},
		Value:   func(c config.Config) string { return config.Bool(c.FilesAutoOpen()) },
	},
	{
		Label: "files follow", Section: "files", Key: "follow",
		Choices: []Choice{{"the pane beside", "true"}, {"stay put", "false"}},
		Value:   func(c config.Config) string { return config.Bool(c.FilesFollow()) },
	},
	{
		Label: "files dotfiles", Section: "files", Key: "hidden",
		Choices: []Choice{{"shown", "false"}, {"hidden", "true"}},
		Value:   func(c config.Config) string { return config.Bool(c.Files.Hidden) },
	},
	{
		Label: "files side", Section: "files", Key: "dock",
		Choices: []Choice{{"right", `"right"`}, {"left", `"left"`}},
		Value: func(c config.Config) string {
			if c.FilesOnLeft() {
				return `"left"`
			}
			return `"right"`
		},
	},
	{
		Label: "files width", Section: "files", Key: "width",
		Choices: []Choice{{"28", "28"}, {"32", "32"}, {"40", "40"}, {"48", "48"}},
		Value:   func(c config.Config) string { return strconv.Itoa(c.FilesWidth()) },
	},
	{
		Label: "mouse", Section: "ui", Key: "mouse",
		Choices: []Choice{{"on", "true"}, {"off", "false"}},
		Value:   func(c config.Config) string { return config.Bool(c.UI.Mouse) },
	},
	{
		Label: "notifications", Section: "notify", Key: "toasts",
		Choices: []Choice{{"on screen", `"tend"`}, {"terminal", `"terminal"`}, {"off", `"off"`}},
		Value:   func(c config.Config) string { return config.Quote(c.Toasts()) },
	},
	{
		Label: "notification corner", Section: "notify", Key: "position",
		Choices: []Choice{{"bottom right", `"bottom-right"`}, {"top right", `"top-right"`}, {"bottom left", `"bottom-left"`}, {"top left", `"top-left"`}},
		Value: func(c config.Config) string {
			if c.Notify.Position == "" {
				return `"bottom-right"`
			}
			return config.Quote(c.Notify.Position)
		},
	},
	{
		Label: "notify focused pane", Section: "notify", Key: "focused",
		Choices: []Choice{{"no", "false"}, {"yes", "true"}},
		Value:   func(c config.Config) string { return config.Bool(c.Notify.Focused) },
	},
	{
		Label: "sound", Section: "sound", Key: "enabled",
		Choices: []Choice{{"off", "false"}, {"on", "true"}},
		Value:   func(c config.Config) string { return config.Bool(c.Sound.Enabled) },
	},
	{
		Label: "keep the session", Section: "server", Key: "persist",
		Choices: []Choice{{"yes", "true"}, {"no", "false"}},
		Value:   func(c config.Config) string { return config.Bool(c.Server.Persist) },
	},
}

// ThemeChoices is every named theme, after the terminal's own colours. Each
// step writes the file and reloads, so moving along the row is the preview
// herdr's theme list gives, through the one path a setting takes.
func ThemeChoices() []Choice {
	out := []Choice{{"terminal colours", `""`}}
	for _, name := range config.ThemeNames {
		out = append(out, Choice{name, config.Quote(name)})
	}
	return out
}

// settingsState is the screen while it is open.

// IntegrationRows are herdr's settings section for integrations: one row
// per agent on this machine, whose hooks can be installed, updated or
// taken out from here as `tend integration` does it.
//
// Only for a client on the machine the session is on: the hooks go into the
// agent's settings where the agent runs, and a client attached over ssh is on
// another machine.
func IntegrationRows(remote bool) []Row {
	if remote {
		return nil
	}
	var rows []Row
	for _, st := range integration.Statuses() {
		if !st.Available && st.State == integration.StatusNotInstalled {
			continue // that agent is not on this machine
		}
		target := st.Target
		rows = append(rows, Row{
			Label: "hooks: " + target.Label(),
			Choices: []Choice{
				{"not installed", string(integration.StatusNotInstalled)},
				{"installed", string(integration.StatusCurrent)},
			},
			Value: func(config.Config) string {
				state := integrationState(target)
				if state == integration.StatusOutdated {
					// Shown as installed; the next step along installs the
					// current version over it.
					return string(integration.StatusCurrent)
				}
				return string(state)
			},
			Apply: func(next Choice) (string, error) {
				if next.TOML == string(integration.StatusNotInstalled) {
					_, err := integration.Uninstall(target)
					return "hooks removed from " + target.Label(), err
				}
				_, err := integration.Install(target)
				return "hooks installed for " + target.Label(), err
			},
		})
	}
	return rows
}

// integrationState is where one target's hooks stand now.
func integrationState(target integration.Target) integration.StatusKind {
	for _, st := range integration.Statuses() {
		if st.Target == target {
			return st.State
		}
	}
	return integration.StatusNotInstalled
}

// Rows is the screen in order: the settings, then a row per agent's hooks
// on this machine (none for a session on another machine, whose agents'
// files are there).
func Rows(remote bool) []Row {
	return append(append([]Row(nil), Base...), IntegrationRows(remote)...)
}

// Next is the choice delta steps from the current one, round the list: the
// screen's left and right.
func Next(row Row, current string, delta int) Choice {
	at := 0
	for i, c := range row.Choices {
		if c.TOML == current {
			at = i
		}
	}
	n := len(row.Choices)
	return row.Choices[((at+delta)%n+n)%n]
}
