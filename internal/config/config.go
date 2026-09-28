// Package config reads tend's settings file.
//
// Every setting has a working default, so the file is optional and a partial
// one is normal: a user who wants a different prefix key should write three
// lines, not a full configuration. An unreadable or invalid file is an error
// rather than a silent fallback — starting with settings the user did not
// write, and not saying so, is worse than refusing to start.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/update"
	"github.com/auth-com-br/tend/internal/usage"
)

// Config is everything tend can be told.
type Config struct {
	// Onboarding is herdr's first-run welcome: shown while it is missing or
	// true, and written false once it has been through.
	Onboarding *bool     `toml:"onboarding"`
	Keys       Keys      `toml:"keys"`
	Pane       Pane      `toml:"pane"`
	UI         UI        `toml:"ui"`
	Server     Server    `toml:"server"`
	Worktrees  Worktrees `toml:"worktrees"`
	Notify     Notify    `toml:"notify"`
	Update     Update    `toml:"update"`
	Browser    Browser   `toml:"browser"`
	Sound      Sound     `toml:"sound"`
	Files      Files     `toml:"files"`
	Issues     Issues    `toml:"issues"`
	Errors     Errors    `toml:"errors"`
	GitLab     GitLab    `toml:"gitlab"`
	Tickets    Tickets   `toml:"tickets"`
	Activity   Activity  `toml:"activity"`
	// Policy is what agents may and may not do (internal/policy). The
	// server reads it again each time it checks, so a rule written here
	// applies from the next agent on, with no restart.
	Policy policy.Policy `toml:"policy"`
}

// Activity is the record of what agents did (internal/activity, `tend
// activity`).
type Activity struct {
	// Record is whether the session server writes the record at all. Unset
	// is on: it is a local file, readable by its owner alone.
	Record *bool `toml:"record"`
	// Prices replace or add to what tend thinks a model costs, in dollars
	// per million tokens, by the start of the model's id:
	// [activity.prices.claude-opus-5] input = 5, output = 25, cache_read = 0.5
	Prices map[string]usage.Price `toml:"prices"`
}

// RecordActivity is whether the server keeps the activity record.
func (c Config) RecordActivity() bool { return c.Activity.Record == nil || *c.Activity.Record }

// Errors connects the errors panel to a GlitchTip server. Both are set from
// the panel, which writes them here — the settings file is written readable
// by its owner alone — and TEND_GLITCHTIP_TOKEN, when set, is used for a
// token that is not.
type Errors struct {
	// URL and Token are the one server tend first knew how to keep, which
	// is still read as a source like the others.
	URL   string `toml:"url"`
	Token string `toml:"token"`
	// Sources are the servers added since, one table each
	// ([errors.sources.<name>]), so adding or removing one edits its own
	// table and leaves the rest of the file as the user wrote it.
	Sources map[string]ErrorSource `toml:"sources"`
	// Source is the one the panel last showed, by name.
	Source string `toml:"source"`
	// Projects ties project folders to the server in [errors] itself, as
	// a source's own Projects does.
	Projects map[string]string `toml:"projects"`
}

// ErrorSource is one GlitchTip server and a token for it.
type ErrorSource struct {
	URL   string `toml:"url"`
	Token string `toml:"token"`
	// Projects are the project folders this server's errors are for, each
	// with the GlitchTip project ("org/slug") to show, or "" for all of
	// them: the panel opened in a pane working under one of these folders
	// opens on this server and that project.
	Projects map[string]string `toml:"projects"`
}

// NamedErrorSource is a source with the name it is kept under. Legacy marks
// the one in [errors] itself, which is removed by emptying its keys rather
// than its table.
type NamedErrorSource struct {
	Name string
	ErrorSource
	Legacy bool
}

// ErrorsToken is the GlitchTip token: the settings', else the environment's.
func (c Config) ErrorsToken() string {
	if c.Errors.Token != "" {
		return c.Errors.Token
	}
	return os.Getenv("TEND_GLITCHTIP_TOKEN")
}

// ErrorSources are the GlitchTip servers the errors panel can show: the one
// in [errors], when it has an address and a token, then those in
// [errors.sources], by name. A source with no token is left out: there is
// nothing it could be asked.
func (c Config) ErrorSources() []NamedErrorSource {
	var out []NamedErrorSource
	if c.Errors.URL != "" && c.ErrorsToken() != "" {
		out = append(out, NamedErrorSource{
			Name:        ErrorSourceName(c.Errors.URL),
			ErrorSource: ErrorSource{URL: c.Errors.URL, Token: c.ErrorsToken(), Projects: c.Errors.Projects},
			Legacy:      true,
		})
	}
	names := make([]string, 0, len(c.Errors.Sources))
	for name := range c.Errors.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if src := c.Errors.Sources[name]; src.URL != "" && src.Token != "" {
			out = append(out, NamedErrorSource{Name: name, ErrorSource: src})
		}
	}
	return out
}

// ErrorSourceFor is the source whose project folders hold dir — the
// deepest folder of all of them that dir is in or is, so a folder inside
// another can go to another server — with the project it names.
func (c Config) ErrorSourceFor(dirs ...string) (NamedErrorSource, string, bool) {
	var best NamedErrorSource
	project, depth := "", -1
	for _, src := range c.ErrorSources() {
		for folder, p := range src.Projects {
			folder = strings.TrimRight(folder, "/")
			for _, dir := range dirs {
				if dir == "" || folder == "" || (dir != folder && !strings.HasPrefix(dir, folder+"/")) {
					continue
				}
				if len(folder) > depth {
					best, project, depth = src, p, len(folder)
				}
			}
		}
	}
	return best, project, depth >= 0
}

// ErrorSourceName is the name a server is kept under: its host, with what a
// bare TOML key cannot hold made a dash ("glitchtip.example.com" is
// glitchtip-example-com).
func ErrorSourceName(url string) string {
	host := url
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host = strings.SplitN(host, "/", 2)[0]
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if name := strings.Trim(b.String(), "-"); name != "" {
		return name
	}
	return "glitchtip"
}

// Tickets are the tracker accounts the tickets panel shows, one table each
// ([tickets.sources.<name>]), as the errors panel keeps its servers, and
// the one last shown.
type Tickets struct {
	Sources map[string]TicketSource `toml:"sources"`
	Source  string                  `toml:"source"`
}

// TicketSource is one tracker account: Linear, Jira, ClickUp or an Issue
// Gateway, its site and email for Jira, its address for the Issue Gateway,
// its token — or where the keyring keeps it — and the project folders
// whose tickets it has, each with the scope (a team, a project, a
// workspace) to show, "" for all.
type TicketSource struct {
	Kind     string            `toml:"kind"`
	URL      string            `toml:"url"`
	Email    string            `toml:"email"`
	Token    string            `toml:"token"`
	Projects map[string]string `toml:"projects"`
}

// NamedTicketSource is a tracker account with the name it is kept under.
type NamedTicketSource struct {
	Name string
	TicketSource
}

// TicketSources are the tracker accounts, by name; one with no kind or no
// token is left out.
func (c Config) TicketSources() []NamedTicketSource {
	names := make([]string, 0, len(c.Tickets.Sources))
	for n := range c.Tickets.Sources {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []NamedTicketSource
	for _, n := range names {
		if s := c.Tickets.Sources[n]; s.Kind != "" && s.Token != "" {
			out = append(out, NamedTicketSource{Name: n, TicketSource: s})
		}
	}
	return out
}

// TicketSourceFor is the account whose project folders hold dir, the
// deepest folder winning, with the scope it names.
func (c Config) TicketSourceFor(dirs ...string) (NamedTicketSource, string, bool) {
	var best NamedTicketSource
	scope, depth := "", -1
	for _, src := range c.TicketSources() {
		for folder, sc := range src.Projects {
			folder = strings.TrimRight(folder, "/")
			for _, dir := range dirs {
				if dir == "" || folder == "" || (dir != folder && !strings.HasPrefix(dir, folder+"/")) {
					continue
				}
				if len(folder) > depth {
					best, scope, depth = src, sc, len(folder)
				}
			}
		}
	}
	return best, scope, depth >= 0
}

// GitLab is the tokens `tend gitlab login` keeps, by host: the token itself,
// or where the keyring keeps it ("keyring:gitlab:<host>").
type GitLab struct {
	Tokens map[string]string `toml:"tokens"`
}

// Issues configures starting work on a GitHub issue from the issues panel.
type Issues struct {
	// Agent is the agent CLI the work starts in; empty is the agent of the
	// pane in view, else claude.
	Agent string `toml:"agent"`
	// Prompt is what the agent is started with. {url}, {number}, {title}
	// and {repo} are the issue's. Orca's default, whose "Complete {{
	// artifact_url}}" this is, trusts the agent to read the issue itself.
	Prompt string `toml:"prompt"`
}

// DefaultIssuePrompt is the prompt when none is set.
const DefaultIssuePrompt = "Complete {url}"

// IssuePrompt is the prompt an issue's work starts with.
func (c Config) IssuePrompt(url string, number int, title, repo string) string {
	prompt := c.Issues.Prompt
	if strings.TrimSpace(prompt) == "" {
		prompt = DefaultIssuePrompt
	}
	return strings.NewReplacer("{url}", url, "{number}", fmt.Sprint(number), "{title}", title, "{repo}", repo).Replace(prompt)
}

// Files configures the files panel (prefix+f). The panel runs on the
// session's machine and reads that machine's file; the width is the
// client's, since it is how the client lays the tab out.
type Files struct {
	// Icons is "none", "nerd" (a Nerd Font's glyphs, herdr-sidebar's
	// material theme) or "emoji" (which every terminal draws, two columns
	// each).
	Icons string `toml:"icons"`
	// Width is how many columns the panel opens at.
	Width int `toml:"width"`
	// Follow moves the panel to the project the pane beside it is in.
	// Nil is on.
	Follow *bool `toml:"follow"`
	// Hidden hides entries whose name starts with a dot.
	Hidden bool `toml:"hidden"`
	// Dock is the edge it opens on: "right" (the default, the owner's
	// choice) or "left".
	Dock string `toml:"dock"`
	// AutoOpen docks the panel in every tab as it is shown, herdr-sidebar's
	// auto_open, save in a tab where it was closed. Nil is on, as there.
	AutoOpen *bool `toml:"auto_open"`
}

// FilesAutoOpen is whether the panel opens by itself in each tab.
func (c Config) FilesAutoOpen() bool { return c.Files.AutoOpen == nil || *c.Files.AutoOpen }

// FilesOnLeft is whether the files panel opens on the left edge: only when
// the settings say so.
func (c Config) FilesOnLeft() bool { return c.Files.Dock == "left" }

// FilesFollow is whether the panel follows the pane beside it.
func (c Config) FilesFollow() bool { return c.Files.Follow == nil || *c.Files.Follow }

// FilesWidth is the panel's opening width, in columns.
func (c Config) FilesWidth() int {
	if c.Files.Width <= 0 {
		return 32
	}
	return c.Files.Width
}

// Browser configures the browser tend opens pages in when none is attached
// to the session. By default it is tend's own: a Chromium-family browser in
// a profile of tend's with tend's extension loaded (internal/browserext);
// Program names which browser (chromium, brave-browser, ...), else the
// first found. Command, when set, replaces all that with a program of the
// user's own, the page's URL added last, and no extension.
type Browser struct {
	Program string `toml:"program"`
	Command string `toml:"command"`
}

// Update configures where `tend update` and the background check look.
type Update struct {
	// Channel is "stable" or "preview".
	Channel string `toml:"channel"`
	// Manifest and Preview are the URLs of the two channels' manifests.
	// Stable's is the one published with tend's GitHub releases unless
	// another is set; preview has none unless one is.
	Manifest string `toml:"manifest"`
	Preview  string `toml:"preview"`
	// VersionCheck is herdr's version_check: look for a newer release in
	// the background, and say when there is one. Nothing is installed.
	VersionCheck bool `toml:"version_check"`
}

// ManifestURL is the manifest for the configured channel.
func (c Config) ManifestURL() string {
	if c.Update.Channel == "preview" {
		return c.Update.Preview
	}
	if c.Update.Manifest == "" {
		return update.StableManifest
	}
	return c.Update.Manifest
}

// Notify configures being told that an agent needs you.
type Notify struct {
	// Toasts is "terminal" (ask the terminal to raise a notification),
	// "system" (a desktop notification), "tend" (a card in a corner of
	// tend's screen, herdr's "herdr" delivery) or "off". herdr's default is off; tend's is the status bar, which costs
	// nothing and is already where the waiting count is.
	Toasts string `toml:"toasts"`
	// Focused notifies about the pane being looked at too. Off by default:
	// being told about what is on screen is noise.
	Focused bool `toml:"focused"`
	// Delay is how many seconds an agent's news is held before it is said,
	// and said only if still true then: herdr's toast delay_seconds. Nil
	// is herdr's one second.
	Delay *int `toml:"delay"`
	// Position is the corner tend's own notification card is shown in:
	// herdr's top-left, top-right, bottom-left or bottom-right (the
	// default).
	Position string `toml:"position"`
}

// Sound configures the noise made when an agent finishes or needs answering.
type Sound struct {
	Enabled bool `toml:"enabled"`
	// Done and Request are sound files. Empty rings the terminal's bell,
	// which is all tend has without bundling audio of its own.
	Done    string `toml:"done"`
	Request string `toml:"request"`
	// Agents turns the sound on or off for one agent: "default", "on" or
	// "off", herdr's [ui.sound.agents]. droid is muted unless it is turned
	// on, as herdr has it.
	Agents map[string]string `toml:"agents"`
}

// AllowsAgent reports whether an agent's changes make a sound.
func (s Sound) AllowsAgent(agent string) bool {
	switch s.Agents[agent] {
	case "on":
		return true
	case "off":
		return false
	}
	return agent != "droid"
}

// Worktrees configures where new worktrees go.
type Worktrees struct {
	// Directory holds a new worktree as <directory>/<repository>/<branch>.
	// herdr keeps them under ~/.herdr/worktrees; tend under ~/.tend/worktrees.
	Directory string `toml:"directory"`
}

// Keys configures the keyboard.
type Keys struct {
	// Prefix arms a command. Written as "ctrl+b", or "none" to disable it —
	// which is only sensible when something else is providing the keys.
	Prefix string `toml:"prefix"`
	// Bind rebinds what a key after the prefix does, as
	// <command> = "<key>". A command not named here keeps its default key,
	// and a key bound twice belongs to whichever command named it.
	//
	// What the names mean is the interface's business, not this package's:
	// the settings file only carries the pairs, and `internal/ui` decides
	// whether they name anything. Validating here would mean config importing
	// ui, which already imports config for the theme.
	Bind map[string]string `toml:"bind"`
	// Command binds keys to commands the user wrote: herdr's
	// [[keys.command]]. See commands.go.
	Command []CommandKey `toml:"command"`
}

// Pane configures new panes.
type Pane struct {
	// Shell is what a pane runs when no command is given. Empty follows
	// $SHELL.
	Shell []string `toml:"shell"`
	// Scrollback is how many lines of history a pane keeps.
	Scrollback int `toml:"scrollback"`
}

// UI configures the interface.
type UI struct {
	Mouse bool `toml:"mouse"`
	// Sidebar is what the sidebar's rows show, or, written as a boolean,
	// tend's older way of saying whether it is shown. See sidebar.go.
	Sidebar Sidebar `toml:"sidebar"`
	// SidebarStartCollapsed hides the sidebar at start: herdr's key. Nil
	// defers to the old boolean, and then to shown — knowing which agent
	// needs you is the reason to run tend, and a list behind a keystroke is
	// one most people never press.
	SidebarStartCollapsed *bool `toml:"sidebar_start_collapsed"`
	// StatusIndicators is "dots" (herdr's default) or "symbols": a glyph for
	// each state, for anyone who does not read state by colour.
	StatusIndicators string `toml:"status_indicators"`
	// AgentPanelSort is "spaces" (the order of the session; "workspaces" is
	// herdr's alias) or "priority": what needs you first, then what changed
	// most recently.
	AgentPanelSort string `toml:"agent_panel_sort"`
	// Grouped lists agents under their tab rather than flat.
	Grouped bool  `toml:"grouped"`
	Theme   Theme `toml:"theme"`
	// WindowTitle is the template for the outer terminal's title; "" leaves
	// that title alone. See windowtitle.go.
	WindowTitle string `toml:"window_title"`
	// TabBarRight is what goes at the right end of the tab bar, and
	// TabBarSeparator what goes between two of them. See tabbar.go.
	TabBarRight     []TabBarEntry `toml:"tab_bar_right"`
	TabBarSeparator string        `toml:"tab_bar_right_separator"`
	// TabBarPosition is "top" or "bottom", and HideTabBarWhenSingleTab gives
	// the row back while a space has one tab. Both herdr's.
	TabBarPosition          string `toml:"tab_bar_position"`
	HideTabBarWhenSingleTab bool   `toml:"hide_tab_bar_when_single_tab"`
	// SidebarWidth is the sidebar's width in columns, and SidebarMinWidth
	// and SidebarMaxWidth the bounds dragging its edge keeps it within:
	// herdr's, 26, 18 and 36.
	SidebarWidth    int `toml:"sidebar_width"`
	SidebarMinWidth int `toml:"sidebar_min_width"`
	SidebarMaxWidth int `toml:"sidebar_max_width"`
	// Toolbar is the row of tools over the sidebar's lists.
	Toolbar Toolbar `toml:"toolbar"`
}

// Toolbar configures the sidebar's tools: whether they are shown, and
// which, in order. Nil Enabled is on; no Items is every tool.
type Toolbar struct {
	Enabled *bool    `toml:"enabled"`
	Items   []string `toml:"items"`
}

// ToolbarTools are the tools a toolbar can hold, in their default order.
var ToolbarTools = []string{"files", "agents", "sessions", "issues", "errors", "tickets", "browser", "context"}

// ToolbarItems is the tools the sidebar shows, none when it is off.
func (c Config) ToolbarItems() []string {
	if c.UI.Toolbar.Enabled != nil && !*c.UI.Toolbar.Enabled {
		return nil
	}
	if len(c.UI.Toolbar.Items) == 0 {
		return ToolbarTools
	}
	return c.UI.Toolbar.Items
}

// SidebarBounds are the least and most a dragged sidebar may be, herdr's
// validated_sidebar_bounds: its defaults when either is unset.
func (c Config) SidebarBounds() (int, int) {
	lo, hi := c.UI.SidebarMinWidth, c.UI.SidebarMaxWidth
	if lo <= 0 {
		lo = 18
	}
	if hi <= 0 {
		hi = 36
	}
	return lo, hi
}

// SidebarWidth is the sidebar's width from the settings, within its bounds.
func (c Config) SidebarWidth() int {
	w := c.UI.SidebarWidth
	if w <= 0 {
		w = 26
	}
	lo, hi := c.SidebarBounds()
	return min(max(w, lo), hi)
}

// Theme names the colours. Name picks one of the palettes in ThemeNames; each
// other value is a colour name, a number from 0 to 255, or a #rrggbb value,
// and overrides that one colour of the palette. Empty keeps the default.
type Theme struct {
	Name string `toml:"name"`
	// AutoSwitch follows the outer terminal between light and dark, using
	// DarkName and LightName (herdr's defaults: catppuccin and
	// catppuccin-latte) in place of Name.
	AutoSwitch bool   `toml:"auto_switch"`
	DarkName   string `toml:"dark_name"`
	LightName  string `toml:"light_name"`
	// Custom overrides colours of the palette one token at a time.
	Custom        CustomColors `toml:"custom"`
	Border        string       `toml:"border"`
	BorderFocused string       `toml:"border_focused"`
	Working       string       `toml:"working"`
	Blocked       string       `toml:"blocked"`
	Idle          string       `toml:"idle"`
}

// Server configures the session server.
type Server struct {
	// DetectInterval is how often panes are re-examined, as "150ms".
	DetectInterval string `toml:"detect_interval"`
	// Persist writes the session down so that a restarted server comes back
	// to the same spaces, tabs and splits. On by default: losing an
	// arrangement to a restart is a surprise, and keeping one is not.
	Persist bool `toml:"persist"`
}

// SidebarShown is whether the sidebar starts shown: herdr's
// sidebar_start_collapsed if the file says, tend's older boolean if that is
// what it says, and shown otherwise.
func (c Config) SidebarShown() bool {
	if c.UI.SidebarStartCollapsed != nil {
		return !*c.UI.SidebarStartCollapsed
	}
	if c.UI.Sidebar.Shown != nil {
		return *c.UI.Sidebar.Shown
	}
	return true
}

// Defaults returns the configuration tend uses when told nothing.
func Defaults() Config {
	return Config{
		Keys:      Keys{Prefix: "ctrl+b"},
		Pane:      Pane{Scrollback: 5000},
		UI:        UI{Mouse: true, WindowTitle: DefaultWindowTitle, TabBarSeparator: " "},
		Server:    Server{DetectInterval: "150ms", Persist: true},
		Worktrees: Worktrees{Directory: "~/.tend/worktrees"},
		Notify:    Notify{Toasts: "tend"},
		Update:    Update{Channel: "stable", VersionCheck: true},
	}
}

// Path returns the settings file, whether or not it exists.
func Path() (string, error) {
	if override := os.Getenv("TEND_CONFIG"); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: finding the config directory: %w", err)
	}
	return filepath.Join(dir, "tend", "config.toml"), nil
}

// Load reads the settings file, or returns the defaults when there is none.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Defaults(), err
	}
	cfg, err := LoadFile(path)
	if os.IsNotExist(err) {
		return Defaults(), nil
	}
	return cfg, err
}

// LoadFile reads a specific settings file.
//
// Unknown keys are an error. A misspelled setting that is silently ignored is
// a setting the user believes is in effect, which is the most confusing way
// for configuration to fail.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Defaults(), err
	}
	return parse(string(data), path)
}

// LoadLenient reads the settings file as Load does, but ignores settings it
// does not know rather than refusing the file. It is for a program that
// reads the file for a part of it and may be older than whoever wrote it:
// the files panel, started before tend was updated, reading a file the new
// settings screen wrote a new key into. Refusing the whole file there threw
// away the panel's own settings — its icons — over a key that was not its.
func LoadLenient() (Config, error) {
	path, err := Path()
	if err != nil {
		return Defaults(), err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), err
	}
	cfg := Defaults()
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Defaults(), fmt.Errorf("config: %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return Defaults(), fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

// parse reads settings from text, naming path in any complaint.
func parse(data, path string) (Config, error) {
	cfg := Defaults()
	md, err := toml.Decode(data, &cfg)
	if err != nil {
		return Defaults(), fmt.Errorf("config: %s: %w", path, err)
	}
	var keys []string
	for _, k := range md.Undecoded() {
		// ui.sidebar and ui.theme.custom read themselves and refuse their
		// own unknown keys; the decoder does not see that they did.
		if len(k) >= 2 && k[0] == "ui" && k[1] == "sidebar" {
			continue
		}
		if len(k) >= 3 && k[0] == "ui" && k[1] == "theme" && k[2] == "custom" {
			continue
		}
		keys = append(keys, k.String())
	}
	if len(keys) > 0 {
		return Defaults(), fmt.Errorf("config: %s: unknown setting(s): %s",
			path, strings.Join(keys, ", "))
	}
	if err := cfg.validate(); err != nil {
		return Defaults(), fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	if _, err := c.PrefixKey(); err != nil {
		return err
	}
	if _, err := c.DetectInterval(); err != nil {
		return err
	}
	switch c.Update.Channel {
	case "", "stable", "preview":
	default:
		return fmt.Errorf("update.channel is %q; use \"stable\" or \"preview\"", c.Update.Channel)
	}
	switch c.Files.Icons {
	case "", "none", "nerd", "emoji":
	default:
		return fmt.Errorf("files.icons is %q; use \"none\", \"nerd\" or \"emoji\"", c.Files.Icons)
	}
	switch c.Files.Dock {
	case "", "left", "right":
	default:
		return fmt.Errorf("files.dock is %q; use \"left\" or \"right\"", c.Files.Dock)
	}
	if c.Files.Width != 0 && (c.Files.Width < 16 || c.Files.Width > 120) {
		return fmt.Errorf("files.width is %d; use 16 to 120 columns", c.Files.Width)
	}
	if d := c.Notify.Delay; d != nil && (*d < 0 || *d > 3600) {
		return fmt.Errorf("notify.delay is %d; use 0 to 3600 seconds", *d)
	}
	switch c.Notify.Position {
	case "", "top-left", "top-right", "bottom-left", "bottom-right":
	default:
		return fmt.Errorf("notify.position is %q; use top-left, top-right, bottom-left or bottom-right", c.Notify.Position)
	}
	switch c.Notify.Toasts {
	case "", "tend", "terminal", "system", "off":
	default:
		return fmt.Errorf("notify.toasts is %q; use \"tend\", \"terminal\", \"system\" or \"off\"", c.Notify.Toasts)
	}
	if c.Pane.Scrollback < 0 {
		return fmt.Errorf("pane.scrollback is %d; it cannot be negative", c.Pane.Scrollback)
	}
	for agent, setting := range c.Sound.Agents {
		switch setting {
		case "default", "on", "off":
		default:
			return fmt.Errorf("sound.agents.%s is %q; use \"default\", \"on\" or \"off\"", agent, setting)
		}
	}
	if err := checkSidebar(c.UI.Sidebar); err != nil {
		return err
	}
	if err := checkCommandKeys(c.Keys.Command); err != nil {
		return err
	}
	switch c.UI.StatusIndicators {
	case "", "dots", "symbols":
	default:
		return fmt.Errorf("ui.status_indicators is %q; use \"dots\" or \"symbols\"", c.UI.StatusIndicators)
	}
	switch c.UI.AgentPanelSort {
	case "", "spaces", "workspaces", "priority":
	default:
		return fmt.Errorf("ui.agent_panel_sort is %q; use \"spaces\" or \"priority\"", c.UI.AgentPanelSort)
	}
	for _, item := range c.UI.Toolbar.Items {
		known := false
		for _, tool := range ToolbarTools {
			known = known || item == tool
		}
		if !known {
			return fmt.Errorf("ui.toolbar.items has %q; the tools are %s", item, strings.Join(ToolbarTools, ", "))
		}
	}
	if lo, hi := c.SidebarBounds(); lo < 4 || hi < lo {
		return fmt.Errorf("ui.sidebar_min_width and sidebar_max_width are %d and %d; the least must be at least 4 and no more than the most", lo, hi)
	}
	switch c.UI.TabBarPosition {
	case "", "top", "bottom":
	default:
		return fmt.Errorf("ui.tab_bar_position is %q; use \"top\" or \"bottom\"", c.UI.TabBarPosition)
	}
	if err := checkTabBar(c.UI.TabBarRight); err != nil {
		return err
	}
	if _, err := ParseWindowTitle(c.UI.WindowTitle); err != nil {
		return fmt.Errorf("ui.window_title %q %v", c.UI.WindowTitle, err)
	}
	for field, name := range map[string]string{
		"name": c.UI.Theme.Name, "dark_name": c.UI.Theme.DarkName, "light_name": c.UI.Theme.LightName,
	} {
		if name == "" {
			continue
		}
		if _, ok := CanonicalTheme(name); !ok {
			return fmt.Errorf("ui.theme.%s is %q; use one of %s",
				field, name, strings.Join(ThemeNames, ", "))
		}
	}
	for name, value := range map[string]string{
		"ui.theme.border":         c.UI.Theme.Border,
		"ui.theme.border_focused": c.UI.Theme.BorderFocused,
		"ui.theme.working":        c.UI.Theme.Working,
		"ui.theme.blocked":        c.UI.Theme.Blocked,
		"ui.theme.idle":           c.UI.Theme.Idle,
	} {
		if value == "" {
			continue
		}
		if _, ok := ParseColor(value); !ok {
			return fmt.Errorf("%s: %q is not a colour; use a name, 0-255, or #rrggbb", name, value)
		}
	}
	return nil
}

// PrefixKey returns the prefix as the byte it produces, or zero when disabled.
func (c Config) PrefixKey() (byte, error) {
	name := strings.ToLower(strings.TrimSpace(c.Keys.Prefix))
	switch name {
	case "":
		return 0x02, nil
	case "none", "off":
		return 0, nil
	}

	rest, ok := strings.CutPrefix(name, "ctrl+")
	if !ok || len(rest) != 1 {
		return 0, fmt.Errorf("keys.prefix: %q is not a key; use \"ctrl+<letter>\" or \"none\"", c.Keys.Prefix)
	}
	letter := rest[0]
	if letter < 'a' || letter > 'z' {
		return 0, fmt.Errorf("keys.prefix: %q is not a letter", rest)
	}
	// Ctrl+A is 0x01 and the alphabet follows from there.
	return letter - 'a' + 1, nil
}

// DetectInterval returns how often panes are re-examined.
func (c Config) DetectInterval() (time.Duration, error) {
	if c.Server.DetectInterval == "" {
		return 150 * time.Millisecond, nil
	}
	d, err := time.ParseDuration(c.Server.DetectInterval)
	if err != nil {
		return 0, fmt.Errorf("server.detect_interval: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("server.detect_interval must be positive, got %s", c.Server.DetectInterval)
	}
	return d, nil
}

// Shell returns the command a pane runs when given none.
func (c Config) Shell() []string {
	if len(c.Pane.Shell) > 0 {
		return c.Pane.Shell
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	return []string{"/bin/sh"}
}

// Scrollback returns how many lines of history a pane keeps.
// Toasts is how notifications are delivered, validated.
func (c Config) Toasts() string {
	switch c.Notify.Toasts {
	case "terminal", "system", "off":
		return c.Notify.Toasts
	}
	return "tend"
}

func (c Config) Scrollback() int {
	if c.Pane.Scrollback <= 0 {
		return 5000
	}
	return c.Pane.Scrollback
}

// Color is a parsed colour, in the terminal's own terms.
type Color struct {
	// RGB is set for a #rrggbb value.
	RGB     bool
	R, G, B uint8
	// Index is the palette entry otherwise.
	Index uint8
}

// namedColors are the sixteen every terminal has. They are used rather than
// fixed RGB so that tend follows the palette the user already chose.
var namedColors = map[string]uint8{
	"black": 0, "red": 1, "green": 2, "yellow": 3,
	"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
	"brightblack": 8, "gray": 8, "grey": 8,
	"brightred": 9, "brightgreen": 10, "brightyellow": 11,
	"brightblue": 12, "brightmagenta": 13, "brightcyan": 14, "brightwhite": 15,
}

// ParseColor reads a colour written as a name, a palette number, or #rrggbb.
func ParseColor(value string) (Color, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return Color{}, false
	}

	if hex, ok := strings.CutPrefix(value, "#"); ok {
		if len(hex) != 6 {
			return Color{}, false
		}
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return Color{}, false
		}
		return Color{RGB: true, R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}, true
	}

	if idx, ok := namedColors[value]; ok {
		return Color{Index: idx}, true
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 255 {
		return Color{}, false
	}
	return Color{Index: uint8(n)}, true
}

// ShowOnboarding is herdr's should_show_onboarding.
func (c Config) ShowOnboarding() bool {
	return c.Onboarding == nil || *c.Onboarding
}

// Example is a commented settings file, written by "tend config --init" so
// that the options are discoverable without a manual.
const Example = `# tend settings. Every value here is the default; delete what you do not change.

# Show the first-run welcome on startup. Missing also shows it; it is set
# false once it has been through.
# onboarding = true

[keys]
# The key that arms a command. "ctrl+<letter>", or "none" to disable it.
prefix = "ctrl+b"

# Rebind what a key after the prefix does, as <command> = "<key>". Run
# "tend keys" for every command and the key it is on now. A command left out
# keeps its default.
# [keys.bind]
# detach = "q"
# settings = ","

# Your own commands on a key after the prefix. type = "shell" runs it in the
# background, "pane" in a pane of its own that has the screen until the
# command ends, "plugin_action" invokes an installed plugin's action by id.
# ("popup" is accepted and runs as a pane.) It runs in the focused pane's
# directory, on the machine the panes are on.
# [[keys.command]]
# key = "Y"
# type = "pane"
# command = "lazygit"
# description = "git"

[pane]
# What a pane runs when no command is given. Empty follows $SHELL.
# shell = ["/bin/zsh"]

# How many lines of history a pane keeps.
scrollback = 5000

[ui]
# Click to focus a pane, drag a divider to resize, scroll to look back.
mouse = true

# Start with the spaces and agents down the left edge put away.
sidebar_start_collapsed = false

# List agents under their tab rather than flat.
grouped = false

# Agent order: "spaces" (as the session is laid out) or "priority" (what
# needs you first — blocked, then finished unseen, then working — and the
# most recent change first within that).
# agent_panel_sort = "spaces"

# Agent state marks: "dots" (coloured marks) or "symbols" (× blocked,
# ◐ working, ✓ finished unseen, ○ idle), which read without colour.
# status_indicators = "dots"

# What the sidebar's rows show. Agent tokens: state_icon, state_text,
# machine, workspace, tab, pane, agent, terminal_title,
# terminal_title_stripped. Space tokens: state_icon, state_text, workspace,
# branch, git_status. A value a hook reported is $name. A token can be styled,
# { token = "workspace", fg = "#89b4fa", bold = true, dim = false }, and given
# rules on its text: rules = [{ gt = 80, fg = "#f38ba8" }, { equals = "", hide = true }].
# (These tables go after the rest of [ui]; they are shown here to be found.)
# [ui.sidebar.agents]
# row_gap = 0
# rows = [["state_icon", "machine", "workspace", "tab"], ["agent"]]
# [ui.sidebar.agents.rows_by_agent]
# claude = [["state_icon", "workspace", "tab"], ["terminal_title_stripped"], ["agent"]]
# [ui.sidebar.spaces]
# row_gap = 0
# rows = [["state_icon", "workspace"], ["branch", "git_status"]]

# The title tend writes to the terminal it runs in, which is what window
# managers show in title, tab and group bars. Tokens are {hostname},
# {workspace}, {tab}, {pane} and {terminal_title}; {{ and }} are literal
# braces. {hostname} is the machine the server runs on, even when attaching
# from another one. Set to "" to leave the outer title alone.
# window_title = "{hostname}: {workspace}"

# Where the tab bar goes, "top" or "bottom" (above the status line), and
# whether to leave it out while a space has only one tab.
# tab_bar_position = "top"
# hide_tab_bar_when_single_tab = false

# The sidebar's width, and the least and most dragging its right edge can
# make it. A double click on the edge puts it back to sidebar_width.
# sidebar_width = 26
# sidebar_min_width = 18
# sidebar_max_width = 36

# What goes at the right end of the tab bar, in order. Types: zoom (ZOOM
# while a pane is zoomed), hostname, datetime (format is strftime, "%H:%M"
# by default), text, and command (the last line it prints, run every
# interval_seconds, 5 by default, and killed after timeout_seconds, 2).
# Hostname, datetime and command are worked out by the server, where the
# panes are.
# tab_bar_right = [
#   { type = "zoom" },
#   { type = "datetime", format = "%H:%M" },
#   { type = "command", command = "git -C ~/src/app branch --show-current" },
# ]
# tab_bar_right_separator = " "

# The tools over the sidebar's lists: files (the files panel, as prefix+f),
# agents, browser and context. items picks which, in order.
# [ui.toolbar]
# enabled = true
# items = ["files", "agents", "sessions", "issues", "errors", "tickets", "browser", "context"]

[ui.theme]
# A named theme: catppuccin, catppuccin-latte, terminal, tokyo-night,
# tokyo-night-day, dracula, nord, gruvbox, gruvbox-light, one-dark, one-light,
# solarized, solarized-light, kanagawa, kanagawa-lotus, rose-pine,
# rose-pine-dawn, vesper, matrix. Unset uses the terminal's own colours.
# name = "catppuccin"
#
# Or follow the terminal between light and dark, with a theme for each.
# auto_switch = true
# dark_name = "catppuccin"
# light_name = "catppuccin-latte"
#
# Override any of the palette's colours on top of the theme, herdr's tokens
# (accent, panel_bg, sidebar_bg, active_row_bg, selection_bg, surface0,
# surface1, surface_dim, overlay0, overlay1, text, subtext0, mauve, green,
# yellow, red, blue, teal, peach): #rrggbb, #rgb, rgb(r,g,b), or "reset".
# [ui.theme.custom]
# accent = "#f5c2e7"
# [ui.theme.custom.light]   # only while auto_switch has the light theme
# panel_bg = "#eff1f5"
#
# Each colour below overrides that one colour of the theme: a colour name,
# a number from 0 to 255, or "#rrggbb".
# border = "brightblack"
# border_focused = "blue"
# working = "yellow"
# blocked = "red"
# idle = "green"

[server]
# How often panes are re-examined for agent state.
detect_interval = "150ms"

# Write the session down, so a restarted server comes back to the same spaces,
# tabs and splits, each pane in the directory it was in and under what it had
# said. Programs do not survive a restart; the place does.
persist = true

[worktrees]
# Where "tend worktree create" puts a new checkout, as
# <directory>/<repository>/<branch>.
directory = "~/.tend/worktrees"

[notify]
# How to say that an agent finished or needs answering:
#   "tend"     a line on the status bar
#   "terminal" ask the terminal to raise a notification (ghostty, kitty,
#              iTerm2, WezTerm; others take none and fall back to the line)
#   "system"   a desktop notification (notify-send, or osascript on macOS)
#   "off"      nothing
toasts = "tend"
# Notify about the pane you are looking at too. Off, because being told about
# what is already on screen is noise.
focused = false

[update]
# Which channel "tend update" follows, and where each one's manifest is.
# Stable's is published with each release on GitHub; set manifest only to
# follow somewhere else. Preview has none unless you set one.
channel = "stable"
# manifest = "https://github.com/auth-com-br/tend/releases/latest/download/latest.json"
# preview = "https://example.invalid/tend/preview.json"
# Look for a newer release in the background and say when there is one:
# a notice, and "update ready" in the status bar. Nothing is installed.
version_check = true

[sound]
# Make a sound as well. With no file named, this is the desktop's sound theme
# (complete, message-new-instant), else the terminal bell; "bell" is the bell.
enabled = false
# done = "~/sounds/done.wav"
# request = "~/sounds/request.wav"

[errors]
# The errors panel (prefix+E) reads GlitchTip servers' errors, one server at
# a time. Connect them from the panel (the gear, or ctrl+k, adds and removes
# them), which writes a table for each, as below; source is the one shown.
# A server here in [errors] itself is kept too, and its token can come from
# TEND_GLITCHTIP_TOKEN. Where the system has a keyring (secret-tool on
# Linux, the Keychain on macOS) a token given in the panel is kept there,
# and token here only names it: "keyring:glitchtip:<name>".
# url = "https://glitchtip.example.com"
# token = ""
# source = "glitchtip-example-com"
#
# [errors.sources.glitchtip-example-com]
# url = "https://glitchtip.example.com"
# token = ""
# The project folders whose errors are on this server, each with the
# GlitchTip project to show ("org/slug", or "" for all): the panel opened in
# a pane working under one opens here. ctrl+l in the panel writes it.
# projects = { "/home/me/shop" = "acme/shop-api" }

[activity]
# The session server writes down what each agent did — when it started and
# stopped, where, which files changed, what its conversation spent — for
# "tend activity" to report. A local file, readable by you alone.
# record = true
# What a model costs, in dollars per million tokens, when the built-in
# prices (Anthropic's public API prices) are not what you pay.
# [activity.prices.claude-opus-5]
# input = 5
# output = 25
# cache_read = 0.5

[policy]
# Rules for agents. tend refuses to start an agent that breaks one, and
# reports (a notice, and a line in "tend activity") one started by hand.
# Branches agents do not work on; patterns such as "release/*" are allowed.
# protected_branches = ["main", "master"]
# Agents working on a repository do it in a worktree, not the main checkout.
# require_worktree = false
# How many agents may run at once in a session; 0 is no limit.
# max_agents = 0

[issues]
# Starting work on a GitHub issue (the issues panel, w) makes a worktree for
# it and starts an agent there. Which agent: empty is the one in the pane you
# are on, else claude.
# agent = "claude"
# What the agent is told; {url}, {number}, {title} and {repo} are the issue's.
# prompt = "Complete {url}"
`

// NotifyDelay is how long an agent's news is held before it is said.
func (c Config) NotifyDelay() time.Duration {
	if c.Notify.Delay == nil {
		return time.Second
	}
	return time.Duration(*c.Notify.Delay) * time.Second
}
