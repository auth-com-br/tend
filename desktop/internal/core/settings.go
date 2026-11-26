package core

import (
	"fmt"
	"os"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/settings"
)

// The settings screen: internal/settings' rows, the TUI's own, changed the
// way the TUI changes them — written to the settings file with config.Set,
// then read back, here and on the server (server.reload_config) — so a
// change made in the window is the change somebody finds in the file and
// in the TUI.

// SettingsState is the screen as QML draws it.
type SettingsState struct {
	Rows []SettingRow `json:"rows"`
	// Path is the file the settings are written to.
	Path string `json:"path"`
}

// SettingRow is one setting: its choices' labels and which is current.
type SettingRow struct {
	Label   string   `json:"label"`
	Choices []string `json:"choices"`
	Current int      `json:"current"`
}

// settingRowsLocked is the screen's rows for this window: a session on
// this machine has the hooks rows too. The caller holds the lock.
func (s *Session) settingRowsLocked() []settings.Row {
	return settings.Rows(false)
}

// settingsStateLocked is the screen now. The caller holds the lock.
func (s *Session) settingsStateLocked() *SettingsState {
	st := &SettingsState{}
	st.Path, _ = config.Path()
	for _, row := range s.settingRowsLocked() {
		current := row.Value(s.cfg)
		r := SettingRow{Label: row.Label, Current: -1}
		for i, c := range row.Choices {
			r.Choices = append(r.Choices, c.Label)
			if c.TOML == current {
				r.Current = i
			}
		}
		st.Rows = append(st.Rows, r)
	}
	return st
}

// skipOnboardingEnv is the TUI's test switch for the welcome, which tests
// that start windows with no settings file honour too.
const skipOnboardingEnv = "TEND_TEST_SKIP_ONBOARDING"

// startOnboarding opens the first-run welcome while the settings ask for it
// (herdr's should_show_onboarding: `onboarding` missing or true), as the
// TUI does. It is one welcome for both clients: going on in either writes
// `onboarding = false`.
func (s *Session) startOnboarding() {
	s.mu.Lock()
	if s.cfg.ShowOnboarding() && os.Getenv(skipOnboardingEnv) == "" {
		s.panel = "welcome"
	}
	s.mu.Unlock()
}

// FinishOnboarding writes `onboarding = false` and opens the settings, at
// the agents' integrations the welcome offers, as herdr's
// complete_onboarding.
func (s *Session) FinishOnboarding() error {
	if err := config.SetTopLevel("onboarding", "false"); err != nil {
		return err
	}
	if err := s.ReloadSettings(); err != nil {
		return err
	}
	s.OpenPanel("settings")
	return nil
}

// OpenPanel shows one of the window's screens ("settings"), or none ("").
func (s *Session) OpenPanel(name string) {
	s.mu.Lock()
	s.panel = name
	s.mu.Unlock()
	s.panelOpened(name)
	s.stateChanged()
}

// ChangeSetting steps a setting delta choices along (settings.Next), writes
// it and reads the settings again. It returns a line saying what changed.
func (s *Session) ChangeSetting(index, delta int) (string, error) {
	s.mu.Lock()
	rows := s.settingRowsLocked()
	if index < 0 || index >= len(rows) {
		s.mu.Unlock()
		return "", fmt.Errorf("no setting %d", index)
	}
	row := rows[index]
	next := settings.Next(row, row.Value(s.cfg), delta)
	s.mu.Unlock()

	if row.Apply != nil {
		said, err := row.Apply(next)
		s.stateChanged()
		return said, err
	}
	if err := config.Set(row.Section, row.Key, next.TOML); err != nil {
		return "", err
	}
	if err := s.ReloadSettings(); err != nil {
		return "", err
	}
	return row.Label + ": " + next.Label, nil
}

// ReloadSettings reads the settings file again and applies it, in the window
// and on the server, as the TUI's reloadSettings: a file that will not
// parse leaves everything as it was, since half-applied settings are worse
// than old ones.
func (s *Session) ReloadSettings() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = cfg
	s.ui = cfg.UI
	s.grouped = cfg.UI.Grouped
	shown, expiry, next, focused := s.notes.shown, s.notes.expiry, s.notes.nextToast, s.notes.windowFocused
	s.notes = newNotifier(cfg)
	s.notes.shown, s.notes.expiry, s.notes.nextToast, s.notes.windowFocused = shown, expiry, next, focused
	s.applyThemeLocked()
	panes := make([]uint64, 0, len(s.panes))
	for id := range s.panes {
		panes = append(panes, id)
	}
	c := s.client
	s.mu.Unlock()
	for _, p := range panes {
		s.paneChanged(p)
	}
	s.stateChanged()
	// The server's half: what it reads of the file (persist, sleep, the
	// tab bar…). An older server has never heard of the method.
	if c.Supports(proto.MethodServerReloadConfig) {
		return c.Call(proto.MethodServerReloadConfig, nil, nil)
	}
	return nil
}
