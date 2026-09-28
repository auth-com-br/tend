package main

import (
	"strconv"
	"strings"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/integration"
	sessionpkg "github.com/auth-com-br/tend/internal/session"
)

// The settings screen: herdr's `prefix+s`, a list of the few settings worth
// changing without opening an editor, each changed where it is seen.
//
// It edits the settings file rather than holding its own copy — a change made
// here is a change somebody will find next time they read the file — and then
// reloads, which is the same path `ctrl+b R` takes. So nothing here has a
// second way of applying a setting that could disagree with the first.
//
// It is not the whole file. The file is the whole file, and it is commented;
// this is the handful of things people change while using tend.

// settingRow is one line in the screen.
type settingRow struct {
	label string
	// section and key name the setting in the file.
	section, key string
	// choices are what it can be, as shown and as written.
	choices []settingChoice
	// value reads the current one out of a configuration.
	value func(config.Config) string
	// apply, when set, does the change itself instead of writing section
	// and key to the file: an integration is installed, not configured.
	apply func(next settingChoice) (string, error)
}

type settingChoice struct {
	label string
	// toml is the value as it goes into the file.
	toml string
}

// settingRows is the screen, in order.
var settingRows = []settingRow{
	{
		label: "theme", section: "ui.theme", key: "name",
		choices: themeChoices(),
		value: func(c config.Config) string {
			name, _ := config.CanonicalTheme(c.UI.Theme.Name)
			return config.Quote(name)
		},
	},
	{
		// herdr's key, not tend's older `sidebar = true`: [ui.sidebar] is
		// also the table of what the rows show, and a file cannot have both.
		label: "sidebar", section: "ui", key: "sidebar_start_collapsed",
		choices: []settingChoice{{"on", "false"}, {"off", "true"}},
		value:   func(c config.Config) string { return config.Bool(!c.SidebarShown()) },
	},
	{
		label: "agent list", section: "ui", key: "grouped",
		choices: []settingChoice{{"flat", "false"}, {"grouped", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.UI.Grouped) },
	},
	{
		// The files panel's (prefix+f). An open panel takes these up at its
		// next look, except the width, which is how it opens.
		label: "files icons", section: "files", key: "icons",
		choices: []settingChoice{{"none", `"none"`}, {"nerd font", `"nerd"`}, {"emoji", `"emoji"`}},
		value: func(c config.Config) string {
			if c.Files.Icons == "" {
				return `"none"`
			}
			return config.Quote(c.Files.Icons)
		},
	},
	{
		label: "files open", section: "files", key: "auto_open",
		choices: []settingChoice{{"in every tab", "true"}, {"when asked (prefix+f)", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.FilesAutoOpen()) },
	},
	{
		label: "files follow", section: "files", key: "follow",
		choices: []settingChoice{{"the pane beside", "true"}, {"stay put", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.FilesFollow()) },
	},
	{
		label: "files dotfiles", section: "files", key: "hidden",
		choices: []settingChoice{{"shown", "false"}, {"hidden", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.Files.Hidden) },
	},
	{
		label: "files side", section: "files", key: "dock",
		choices: []settingChoice{{"right", `"right"`}, {"left", `"left"`}},
		value: func(c config.Config) string {
			if c.FilesOnLeft() {
				return `"left"`
			}
			return `"right"`
		},
	},
	{
		label: "files width", section: "files", key: "width",
		choices: []settingChoice{{"28", "28"}, {"32", "32"}, {"40", "40"}, {"48", "48"}},
		value:   func(c config.Config) string { return strconv.Itoa(c.FilesWidth()) },
	},
	{
		label: "mouse", section: "ui", key: "mouse",
		choices: []settingChoice{{"on", "true"}, {"off", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.UI.Mouse) },
	},
	{
		label: "notifications", section: "notify", key: "toasts",
		choices: []settingChoice{{"on screen", `"tend"`}, {"terminal", `"terminal"`}, {"off", `"off"`}},
		value:   func(c config.Config) string { return config.Quote(c.Toasts()) },
	},
	{
		label: "notification corner", section: "notify", key: "position",
		choices: []settingChoice{{"bottom right", `"bottom-right"`}, {"top right", `"top-right"`}, {"bottom left", `"bottom-left"`}, {"top left", `"top-left"`}},
		value: func(c config.Config) string {
			if c.Notify.Position == "" {
				return `"bottom-right"`
			}
			return config.Quote(c.Notify.Position)
		},
	},
	{
		label: "notify focused pane", section: "notify", key: "focused",
		choices: []settingChoice{{"no", "false"}, {"yes", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.Notify.Focused) },
	},
	{
		label: "sound", section: "sound", key: "enabled",
		choices: []settingChoice{{"off", "false"}, {"on", "true"}},
		value:   func(c config.Config) string { return config.Bool(c.Sound.Enabled) },
	},
	{
		label: "keep the session", section: "server", key: "persist",
		choices: []settingChoice{{"yes", "true"}, {"no", "false"}},
		value:   func(c config.Config) string { return config.Bool(c.Server.Persist) },
	},
}

// themeChoices is every named theme, after the terminal's own colours. Each
// step writes the file and reloads, so moving along the row is the preview
// herdr's theme list gives, through the one path a setting takes.
func themeChoices() []settingChoice {
	out := []settingChoice{{"terminal colours", `""`}}
	for _, name := range config.ThemeNames {
		out = append(out, settingChoice{name, config.Quote(name)})
	}
	return out
}

// settingsState is the screen while it is open.
type settingsState struct {
	row int
	// rows is the screen as it was opened: the settings, then the agent
	// integrations found on this machine, which is a list that depends on
	// what is installed and so is not fixed.
	rows []settingRow
}

// integrationRows are herdr's settings section for integrations: one row
// per agent on this machine, whose hooks can be installed, updated or
// taken out from here as `tend integration` does it.
//
// Only for a client on the machine the session is on: the hooks go into the
// agent's settings where the agent runs, and a client attached over ssh is on
// another machine.
func integrationRows(remote bool) []settingRow {
	if remote {
		return nil
	}
	var rows []settingRow
	for _, st := range integration.Statuses() {
		if !st.Available && st.State == integration.StatusNotInstalled {
			continue // that agent is not on this machine
		}
		target := st.Target
		rows = append(rows, settingRow{
			label: "hooks: " + target.Label(),
			choices: []settingChoice{
				{"not installed", string(integration.StatusNotInstalled)},
				{"installed", string(integration.StatusCurrent)},
			},
			value: func(config.Config) string {
				state := integrationState(target)
				if state == integration.StatusOutdated {
					// Shown as installed; the next step along installs the
					// current version over it.
					return string(integration.StatusCurrent)
				}
				return string(state)
			},
			apply: func(next settingChoice) (string, error) {
				if next.toml == string(integration.StatusNotInstalled) {
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

// openSettings shows the screen, or closes it if it is already up.
func (t *tui) openSettings() error {
	t.mu.Lock()
	open := t.settings != nil
	if open {
		t.settings = nil
		t.overlay = nil
	} else {
		t.settings = &settingsState{rows: append(append([]settingRow(nil), settingRows...), integrationRows(t.host != "")...)}
	}
	t.dirty = true
	t.mu.Unlock()
	if !open {
		t.drawSettings()
	}
	t.wakeUp()
	return nil
}

// settingsUp reports whether the screen has the keyboard.
func (t *tui) settingsUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.settings != nil
}

// settingsKeys drives the screen. Every key is its own while it is up.
func (t *tui) settingsKeys(data []byte) (bool, error) {
	for _, key := range splitKeys(data) {
		switch key {
		case "\x1b", "q", "\x03", "s":
			t.mu.Lock()
			t.settings, t.overlay = nil, nil
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return true, nil
		case "k", "\x1b[A":
			t.moveSetting(-1)
		case "j", "\x1b[B":
			t.moveSetting(1)
		case "h", "\x1b[D":
			if err := t.changeSetting(-1); err != nil {
				return true, err
			}
		case "l", "\x1b[C", "\r", " ":
			if err := t.changeSetting(1); err != nil {
				return true, err
			}
		}
	}
	return true, nil
}

func (t *tui) moveSetting(delta int) {
	t.mu.Lock()
	if t.settings != nil {
		t.settings.row = (t.settings.row + delta + len(t.settings.rows)) % len(t.settings.rows)
	}
	t.mu.Unlock()
	t.drawSettings()
}

// changeSetting moves one setting to the next choice, writes it, and reloads.
func (t *tui) changeSetting(delta int) error {
	t.mu.Lock()
	if t.settings == nil {
		t.mu.Unlock()
		return nil
	}
	row := t.settings.rows[t.settings.row]
	current := row.value(t.config)
	t.mu.Unlock()

	at := 0
	for i, choice := range row.choices {
		if choice.toml == current {
			at = i
		}
	}
	next := row.choices[(at+delta+len(row.choices))%len(row.choices)]

	if row.apply != nil {
		said, err := row.apply(next)
		if err != nil {
			t.setMessage(err.Error(), true)
		} else {
			t.setMessage(said, false)
		}
		t.drawSettings()
		return nil
	}
	if err := config.Set(row.section, row.key, next.toml); err != nil {
		t.setMessage(err.Error(), true)
		return nil
	}
	// Through the same reload the key uses, so a setting cannot be applied
	// here in a way that differs from applying it from the file.
	if err := t.reloadSettings(); err != nil {
		return err
	}
	t.setMessage(row.label+": "+next.label, false)
	t.drawSettings()
	return nil
}

// drawSettings renders the screen into the overlay panel.
func (t *tui) drawSettings() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.settings == nil {
		return
	}

	lines := []string{"settings", ""}
	for i, row := range t.settings.rows {
		current := row.value(t.config)
		var shown []string
		for _, choice := range row.choices {
			label := choice.label
			if choice.toml == current {
				label = "[" + label + "]"
			} else if len(row.choices) > maxShownChoices {
				continue // too many to list; the current one is what matters
			}
			shown = append(shown, label)
		}
		if len(row.choices) > maxShownChoices {
			shown = []string{"‹", strings.Join(shown, ""), "›"}
		}
		marker := "  "
		if i == t.settings.row {
			marker = "▸ "
		}
		lines = append(lines, marker+pad(row.label, 20)+strings.Join(shown, " "))
	}
	path, _ := config.Path()
	lines = append(lines,
		"",
		"j k  move     h l  change     q  close",
		"written to "+path,
	)
	t.overlay = lines
	t.dirty = true
}

// maxShownChoices is how many choices a row lists before it shows only the
// current one: nineteen themes do not fit on a line, and do not need to.
const maxShownChoices = 4

// pad is the column padding the overlay uses, spelled here because ui's is
// not exported and this is the only other place that needs it.
func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}

// newWorktreeHere asks for a branch and makes a worktree for it, for the
// repository the focused space is in: herdr's prefix+shift+g.
//
// The branch is asked for, as herdr asks, with a generated name already in the
// box: most of the time the name does not matter and enter is the whole
// answer, and when it does the user types over it.
func (t *tui) newWorktreeHere() error {
	t.mu.Lock()
	workspace := t.workspace
	t.mu.Unlock()
	if workspace == 0 {
		return nil
	}
	t.startPrompt(promptNewWorktree)
	return nil
}

// createWorktree makes the worktree the prompt named.
//
// Through the automation socket, which is where worktrees live, rather than a
// second implementation over the client protocol. It takes seconds — git has
// to check the files out — so it runs off the input goroutine and says how it
// went when it is done.
func (t *tui) createWorktree(workspace uint64, branch string) {
	t.mu.Lock()
	session := t.session
	t.mu.Unlock()

	t.setMessage("making "+branch+"…", false)
	go func() {
		result, err := apiCall(session, api.MethodWorktreeCreate, map[string]any{
			"workspace_id": api.WorkspaceID(sessionpkg.WorkspaceID(workspace)),
			"branch":       branch,
		}, true)
		if err != nil {
			t.setMessage(err.Error(), true)
			return
		}
		wt, _ := result["worktree"].(map[string]any)
		t.setMessage("worktree "+text(wt["branch"])+" opened", false)
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}()
}

// removeWorktree removes the worktree a space is, and the space with it.
//
// Unforced, as herdr's first attempt is: git refuses when there are changes
// that would be lost, and that refusal is passed on as it is — with the
// command that would remove it anyway, which is a decision for the person who
// knows what the changes are.
func (t *tui) removeWorktree(workspace uint64) {
	t.mu.Lock()
	session := t.session
	t.mu.Unlock()

	id := api.WorkspaceID(sessionpkg.WorkspaceID(workspace))
	go func() {
		_, err := apiCall(session, api.MethodWorktreeRemove, map[string]any{"workspace_id": id}, true)
		switch {
		case err == nil:
			t.setMessage("worktree removed", false)
		case strings.Contains(err.Error(), "worktree_dirty"):
			t.setMessage("it has changes that would be lost; tend worktree remove -force "+id, true)
			return
		default:
			t.setMessage(err.Error(), true)
			return
		}
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}()
}

// parseWorkspaceID reads "w_3".
func parseWorkspaceID(s string) (uint64, bool) {
	n, err := strconv.ParseUint(strings.TrimPrefix(s, "w_"), 10, 64)
	return n, err == nil && n != 0
}
