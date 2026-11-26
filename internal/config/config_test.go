package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsAreUsable(t *testing.T) {
	c := Defaults()
	if err := c.validate(); err != nil {
		t.Fatalf("the defaults do not validate: %v", err)
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Errorf("prefix = %#x, want ctrl+b", key)
	}
	if got := c.Scrollback(); got != 5000 {
		t.Errorf("scrollback = %d", got)
	}
	if !c.UI.Mouse {
		t.Error("the mouse should be on by default")
	}
}

// TestMissingFileIsNotAnError: the settings file is optional, and a user who
// has never written one should not be told about it.
func TestMissingFileIsNotAnError(t *testing.T) {
	t.Setenv("TEND_CONFIG", filepath.Join(t.TempDir(), "absent.toml"))
	c, err := Load()
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Error("a missing file should give the defaults")
	}
}

// TestPartialFileKeepsDefaults: a user changing one setting writes one
// setting, not a whole file.
func TestPartialFileKeepsDefaults(t *testing.T) {
	path := writeConfig(t, "[keys]\nprefix = \"ctrl+a\"\n")
	c, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if key, _ := c.PrefixKey(); key != 0x01 {
		t.Errorf("prefix = %#x, want ctrl+a", key)
	}
	if got := c.Scrollback(); got != 5000 {
		t.Errorf("scrollback = %d, want the default kept", got)
	}
	if !c.UI.Mouse {
		t.Error("mouse should keep its default")
	}
}

// TestUnknownSettingIsAnError: a misspelled setting that is silently ignored
// is one the user believes is in effect.
func TestUnknownSettingIsAnError(t *testing.T) {
	path := writeConfig(t, "[keys]\nprefx = \"ctrl+a\"\n")
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("a misspelled setting should fail")
	}
	if !strings.Contains(err.Error(), "prefx") {
		t.Errorf("err = %v, want it to name the setting", err)
	}
}

func TestMalformedFileIsAnError(t *testing.T) {
	path := writeConfig(t, "[keys\nprefix =\n")
	if _, err := LoadFile(path); err == nil {
		t.Error("malformed TOML should fail")
	}
}

// TestInvalidValuesAreRejectedAtLoad: a bad value must be reported when the
// file is read, not when the setting is first used.
func TestInvalidValuesAreRejectedAtLoad(t *testing.T) {
	cases := map[string]string{
		"bad prefix":     "[keys]\nprefix = \"meta+q\"\n",
		"bad duration":   "[server]\ndetect_interval = \"soon\"\n",
		"zero duration":  "[server]\ndetect_interval = \"0s\"\n",
		"bad colour":     "[ui.theme]\nworking = \"chartreuse\"\n",
		"unknown theme":  "[ui.theme]\nname = \"mauve-dreams\"\n",
		"bad scrollback": "[pane]\nscrollback = -1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadFile(writeConfig(t, body)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// TestInvalidConfigFallsBackToDefaults: whatever else happens, the caller gets
// a usable configuration rather than a zero one.
func TestInvalidConfigFallsBackToDefaults(t *testing.T) {
	c, err := LoadFile(writeConfig(t, "[keys]\nprefix = \"nonsense\"\n"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Error("the returned config should still be usable")
	}
}

func TestPrefixKey(t *testing.T) {
	cases := map[string]byte{
		"ctrl+a":   0x01,
		"ctrl+b":   0x02,
		"ctrl+z":   0x1a,
		"CTRL+B":   0x02,
		" ctrl+b ": 0x02,
		"":         0x02,
		"none":     0,
		"off":      0,
	}
	for input, want := range cases {
		c := Defaults()
		c.Keys.Prefix = input
		got, err := c.PrefixKey()
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %#x, want %#x", input, got, want)
		}
	}

	for _, bad := range []string{"b", "alt+b", "ctrl+", "ctrl+bb", "ctrl+1"} {
		c := Defaults()
		c.Keys.Prefix = bad
		if _, err := c.PrefixKey(); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestDetectInterval(t *testing.T) {
	c := Defaults()
	if got, _ := c.DetectInterval(); got != 150*time.Millisecond {
		t.Errorf("default = %v", got)
	}
	c.Server.DetectInterval = "1s"
	if got, _ := c.DetectInterval(); got != time.Second {
		t.Errorf("got %v, want 1s", got)
	}
}

func TestShellFollowsTheEnvironment(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/fish")
	c := Defaults()
	if got := c.Shell(); len(got) != 1 || got[0] != "/usr/bin/fish" {
		t.Errorf("Shell = %v, want $SHELL", got)
	}

	// An explicit setting wins over the environment.
	c.Pane.Shell = []string{"/bin/dash", "-l"}
	if got := c.Shell(); len(got) != 2 || got[0] != "/bin/dash" {
		t.Errorf("Shell = %v, want the configured one", got)
	}
}

func TestParseColor(t *testing.T) {
	cases := map[string]Color{
		"red":         {Index: 1},
		"BrightBlue":  {Index: 12},
		"grey":        {Index: 8},
		"0":           {Index: 0},
		"208":         {Index: 208},
		"255":         {Index: 255},
		"#ff8800":     {RGB: true, R: 0xff, G: 0x88, B: 0x00},
		"  #000000  ": {RGB: true},
	}
	for input, want := range cases {
		got, ok := ParseColor(input)
		if !ok {
			t.Errorf("%q was rejected", input)
			continue
		}
		if got != want {
			t.Errorf("%q = %+v, want %+v", input, got, want)
		}
	}

	for _, bad := range []string{"", "chartreuse", "256", "-1", "#fff", "#gggggg", "#ff88000"} {
		if _, ok := ParseColor(bad); ok {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// TestExampleIsValid keeps the file tend writes for people from being one it
// would itself refuse to read.
func TestExampleIsValid(t *testing.T) {
	path := writeConfig(t, Example)
	c, err := LoadFile(path)
	if err != nil {
		t.Fatalf("the example config does not load: %v", err)
	}
	if key, _ := c.PrefixKey(); key != 0x02 {
		t.Error("the example should describe the defaults")
	}
}

func TestPathRespectsOverride(t *testing.T) {
	t.Setenv("TEND_CONFIG", "/tmp/somewhere/else.toml")
	got, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/somewhere/else.toml" {
		t.Errorf("Path = %q", got)
	}
}

// TestSidebarDefaultsOn: the list of what needs attention is the reason to
// run tend, so it is not behind a keystroke nobody presses.
func TestSidebarDefaultsOn(t *testing.T) {
	if !Defaults().SidebarShown() {
		t.Error("the sidebar should be on by default")
	}
	if Defaults().UI.Grouped {
		t.Error("agents should start flat")
	}

	c, err := LoadFile(writeConfig(t, "[ui]\nsidebar = false\ngrouped = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.SidebarShown() {
		t.Error("sidebar = false should turn it off")
	}
	// herdr's key wins over tend's old one when both are there.
	both, err := LoadFile(writeConfig(t, "[ui]\nsidebar = false\nsidebar_start_collapsed = false\n"))
	if err != nil || !both.SidebarShown() {
		t.Errorf("sidebar_start_collapsed = false should show it: %v", err)
	}
	if !c.UI.Grouped {
		t.Error("grouped = true should group")
	}
	if !c.UI.Mouse {
		t.Error("the other ui settings should keep their defaults")
	}
}

// TestThemeNamesAcceptTheSpellingsHerdrDoes: a theme written the way people
// know it — "Tokyo Night", "catppuccin_mocha", "onedark" — must load, or a
// config carried over from herdr is refused for its spelling.
func TestThemeNamesAcceptTheSpellingsHerdrDoes(t *testing.T) {
	cases := map[string]string{
		"Tokyo Night":      "tokyo-night",
		"catppuccin_mocha": "catppuccin",
		"onedark":          "one-dark",
		"dawn":             "rose-pine-dawn",
		"  VESPER ":        "vesper",
	}
	for input, want := range cases {
		if got, ok := CanonicalTheme(input); !ok || got != want {
			t.Errorf("CanonicalTheme(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, name := range ThemeNames {
		if got, ok := CanonicalTheme(name); !ok || got != name {
			t.Errorf("%q is listed but does not resolve to itself (got %q)", name, got)
		}
	}
	if _, ok := CanonicalTheme("mauve-dreams"); ok {
		t.Error("a name that is no theme should not resolve")
	}
}

// TestWindowTitleTemplates: the template is herdr's syntax, so a line copied
// from a herdr config means the same thing here, and a mistyped one is
// reported instead of writing "{worksapce}" into every window bar.
func TestWindowTitleTemplates(t *testing.T) {
	tmpl, err := ParseWindowTitle("{hostname}: { workspace } {{a}}")
	if err != nil || tmpl == nil {
		t.Fatalf("parse: %v", err)
	}
	values := map[WindowTitleToken]string{TitleHostname: "box", TitleWorkspace: "api"}
	got, ok := tmpl.Render(func(k WindowTitleToken) string { return values[k] })
	if !ok || got != "box: api {a}" {
		t.Errorf("rendered %q, want %q", got, "box: api {a}")
	}

	if tmpl, err := ParseWindowTitle(""); tmpl != nil || err != nil {
		t.Error("an empty template should mean no title, without an error")
	}
	for bad, want := range map[string]string{
		"{hostname": "unclosed",
		"a } b":     "unmatched",
		"{session}": "unknown token '{session}'",
	} {
		if _, err := ParseWindowTitle(bad); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseWindowTitle(%q) = %v, want an error about %s", bad, err, want)
		}
	}
	if _, err := LoadFile(writeConfig(t, "[ui]\nwindow_title = \"{worksapce}\"\n")); err == nil {
		t.Error("a mistyped token should be refused at load")
	}
	if c := Defaults(); c.UI.WindowTitle != DefaultWindowTitle {
		t.Errorf("default window title = %q, want herdr's %q", c.UI.WindowTitle, DefaultWindowTitle)
	}
}

// TestWindowTitlesAreSanitisedAndBounded: a pane title is chosen by whatever
// runs in the pane, and an escape in it would otherwise end the OSC early and
// write the rest to the terminal as commands.
func TestWindowTitlesAreSanitisedAndBounded(t *testing.T) {
	if got, _ := SanitizeWindowTitle("  tend\x1b api\x07\n  "); got != "tend api" {
		t.Errorf("got %q", got)
	}
	if _, ok := SanitizeWindowTitle("\x07\n"); ok {
		t.Error("a title of nothing but control characters is no title")
	}
	if got, _ := SanitizeWindowTitle(strings.Repeat("x", MaxWindowTitle+1)); len([]rune(got)) != MaxWindowTitle {
		t.Errorf("title of %d characters, want %d", len([]rune(got)), MaxWindowTitle)
	}
}

// TestTabBarEntries: herdr's entries load as herdr writes them, with its
// defaults, and an entry that could never be shown is refused at load rather
// than found missing from the bar.
func TestTabBarEntries(t *testing.T) {
	c, err := LoadFile(writeConfig(t, `[ui]
tab_bar_right = [
  { type = "zoom" },
  { type = "hostname" },
  { type = "datetime", format = "%H:%M" },
  { type = "text", text = "prod" },
  { type = "command", command = "status.sh" },
]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.UI.TabBarRight) != 5 {
		t.Fatalf("%d entries, want 5", len(c.UI.TabBarRight))
	}
	cmd := c.UI.TabBarRight[4]
	if cmd.Interval() != 5*time.Second || cmd.Timeout() != 2*time.Second {
		t.Errorf("command defaults = %v, %v; want herdr's 5s and 2s", cmd.Interval(), cmd.Timeout())
	}
	if c.UI.TabBarSeparator != " " {
		t.Errorf("separator = %q, want a space", c.UI.TabBarSeparator)
	}

	for name, body := range map[string]string{
		"unknown type":    `tab_bar_right = [{ type = "weather" }]`,
		"bad directive":   `tab_bar_right = [{ type = "datetime", format = "%Q" }]`,
		"offset":          `tab_bar_right = [{ type = "datetime", format = "%z" }]`,
		"empty command":   `tab_bar_right = [{ type = "command", command = " " }]`,
		"endless timeout": `tab_bar_right = [{ type = "command", command = "x", timeout_seconds = 3601 }]`,
	} {
		if _, err := LoadFile(writeConfig(t, "[ui]\n"+body+"\n")); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestStrftime covers the directives people put in a clock.
func TestStrftime(t *testing.T) {
	at := time.Date(2026, time.September, 7, 9, 5, 3, 0, time.UTC)
	for format, want := range map[string]string{
		"%H:%M":       "09:05",
		"%I:%M %p":    "09:05 AM",
		"%a %e %b":    "Mon  7 Sep",
		"%Y-%m-%d":    "2026-09-07",
		"%F %T":       "2026-09-07 09:05:03",
		"%A %B %d %%": "Monday September 07 %",
		"%j %u %w":    "250 1 1",
	} {
		if got := Strftime(format, at); got != want {
			t.Errorf("Strftime(%q) = %q, want %q", format, got, want)
		}
	}
}

// TestCommandKeys: [[keys.command]] reads as herdr writes it — "prefix+g",
// no type meaning shell — and an entry that could never run is refused.
func TestCommandKeys(t *testing.T) {
	c, err := LoadFile(writeConfig(t, `[[keys.command]]
key = "prefix+g"
type = "popup"
command = "lazygit"
width = "80%"

[[keys.command]]
key = "y"
command = "make test"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Keys.Command) != 2 {
		t.Fatalf("%d commands, want 2", len(c.Keys.Command))
	}
	if got := c.Keys.Command[0].KeyName(); got != "g" {
		t.Errorf("key = %q, want the prefix word dropped", got)
	}
	if got := c.Keys.Command[1].Kind(); got != CommandShell {
		t.Errorf("type = %q, want herdr's default of shell", got)
	}
	for name, body := range map[string]string{
		"no command": "[[keys.command]]\nkey = \"g\"\n",
		"no key":     "[[keys.command]]\ncommand = \"x\"\n",
		"bad type":   "[[keys.command]]\nkey = \"g\"\ncommand = \"x\"\ntype = \"window\"\n",
	} {
		if _, err := LoadFile(writeConfig(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestSidebarRowsReadAsHerdrWritesThem: herdr's [ui.sidebar.*] tables load
// — plain tokens, $custom ones, styled ones with rules — beside tend's own
// settings, and a token that does not exist is refused.
func TestSidebarRowsReadAsHerdrWritesThem(t *testing.T) {
	c, err := LoadFile(writeConfig(t, `[ui]
sidebar_start_collapsed = false

[ui.sidebar.agents]
rows = [["state_icon", { token = "workspace", fg = "#89b", bold = true }], ["agent", { token = "$ctx", rules = [{ gt = 80, fg = "#f38ba8" }, { equals = "0", hide = true }] }]]
row_gap = 1

[ui.sidebar.agents.rows_by_agent]
claude = [["terminal_title_stripped"]]

[ui.sidebar.spaces]
rows = [["workspace"], ["$jj_status"]]
`))
	if err != nil {
		t.Fatal(err)
	}
	s := c.UI.Sidebar
	if !c.SidebarShown() || s.Agents.RowGap != 1 || len(s.AgentRows("codex")) != 2 {
		t.Fatalf("agents = %+v", s.Agents)
	}
	if got := s.AgentRows("claude"); len(got) != 1 || got[0][0].Name != "terminal_title_stripped" {
		t.Errorf("claude's rows = %+v", got)
	}
	ws := s.AgentRows("codex")[0][1]
	if ws.Name != "workspace" || ws.Style.FG == nil || *ws.Style.FG != (RGB{0x88, 0x99, 0xbb}) || ws.Style.Bold == nil {
		t.Errorf("styled token = %+v", ws)
	}
	ctx := s.AgentRows("codex")[1][1]
	if style, shown := ctx.StyleFor("91"); !shown || style.FG == nil || *style.FG != (RGB{0xf3, 0x8b, 0xa8}) {
		t.Errorf("91 should take the gt rule's colour: %+v %v", style, shown)
	}
	if _, shown := ctx.StyleFor("0"); shown {
		t.Error("0 should be hidden by its rule")
	}
	if style, shown := ctx.StyleFor("12"); !shown || style.FG != nil {
		t.Error("12 matches no rule and keeps the token's style")
	}
	if got := Defaults().UI.Sidebar.SpaceRows(); len(got) != 2 || got[1][1].Name != "git_status" {
		t.Errorf("default space rows = %+v", got)
	}

	for name, body := range map[string]string{
		"unknown token":   "[ui.sidebar.agents]\nrows = [[\"weather\"]]\n",
		"space-only name": "[ui.sidebar.agents]\nrows = [[\"branch\"]]\n",
		"bad colour":      "[ui.sidebar.spaces]\nrows = [[{ token = \"workspace\", fg = \"blue\" }]]\n",
		"two conditions":  "[ui.sidebar.spaces]\nrows = [[{ token = \"$x\", rules = [{ gt = 1, lt = 2 }] }]]\n",
		"unknown key":     "[ui.sidebar.spaces]\nrow_gaps = 1\n",
		"unknown agent":   "[ui.sidebar.agents.rows_by_agent]\nclaudee = [[\"agent\"]]\n",
	} {
		if _, err := LoadFile(writeConfig(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

// TestSoundCanBeTurnedOffForOneAgent is herdr's [ui.sound.agents]: one agent
// muted or unmuted, droid muted unless asked, and a value that is not one
// refused.
func TestSoundCanBeTurnedOffForOneAgent(t *testing.T) {
	c, err := LoadFile(writeConfig(t, "[sound]\nenabled = true\n[sound.agents]\nclaude = \"off\"\ndroid = \"on\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Sound.AllowsAgent("claude") || !c.Sound.AllowsAgent("droid") || !c.Sound.AllowsAgent("codex") {
		t.Errorf("agents = %v", c.Sound.Agents)
	}
	if Defaults().Sound.AllowsAgent("droid") {
		t.Error("droid is muted by default, as in herdr")
	}
	if _, err := LoadFile(writeConfig(t, "[sound.agents]\nclaude = \"quiet\"\n")); err == nil {
		t.Error("a setting that is not default, on or off should be refused")
	}
}

// TestFilesSettingsAreChecked: the panel's icons and width are read with
// their defaults and refused when they make no sense. If it regresses, a
// typo in [files] is silently a panel with no icons.
func TestFilesSettingsAreChecked(t *testing.T) {
	c := Defaults()
	if c.FilesWidth() != 32 || !c.FilesFollow() {
		t.Errorf("defaults: width %d follow %v", c.FilesWidth(), c.FilesFollow())
	}
	c.Files.Icons = "material"
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "files.icons") {
		t.Errorf("a theme that is not one: %v", err)
	}
	c.Files.Icons, c.Files.Width = "nerd", 4
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "files.width") {
		t.Errorf("a width too small: %v", err)
	}
	off := false
	c.Files.Width, c.Files.Follow = 40, &off
	if err := c.validate(); err != nil || c.FilesFollow() || c.FilesWidth() != 40 {
		t.Errorf("valid: %v follow %v width %d", err, c.FilesFollow(), c.FilesWidth())
	}
}

// TestNotifyDelayIsHerdrsSecondAndBounded: held a second unless set, zero
// allowed, an hour at most as herdr's MAX_TOAST_DELAY_SECONDS. If it
// regresses, news is said at once and a flicker pops cards up.
func TestNotifyDelayIsHerdrsSecondAndBounded(t *testing.T) {
	c := Defaults()
	if c.NotifyDelay() != time.Second {
		t.Errorf("default %v", c.NotifyDelay())
	}
	zero, huge := 0, 4000
	c.Notify.Delay = &zero
	if err := c.validate(); err != nil || c.NotifyDelay() != 0 {
		t.Errorf("zero: %v %v", err, c.NotifyDelay())
	}
	c.Notify.Delay = &huge
	if err := c.validate(); err == nil {
		t.Error("an hour and more should be refused")
	}
	c.Notify.Delay = nil
	c.Notify.Position = "middle"
	if err := c.validate(); err == nil {
		t.Error("a corner that is not one should be refused")
	}
}

// TestLoadLenientKeepsWhatItKnows: a setting from a newer tend does not
// cost the reader the ones it knows. If it regresses, a files panel started
// before an update shows no icons once the new settings screen has written
// a key it has never heard of.
func TestLoadLenientKeepsWhatItKnows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("TEND_CONFIG", path)
	body := "[files]\nicons = \"nerd\"\nsomething_newer = 3\n\n[brand_new_section]\nx = 1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("the client stays strict about unknown settings")
	}
	cfg, err := LoadLenient()
	if err != nil || cfg.Files.Icons != "nerd" {
		t.Errorf("lenient: %v, icons %q", err, cfg.Files.Icons)
	}
	if err := os.WriteFile(path, []byte("[files\nicons = "), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLenient(); err == nil {
		t.Error("a file that is not TOML is still an error, to be said")
	}
}

// TestTheWindowsThemeIsItsOwnSetting: [desktop] theme takes tend, tui or a
// theme's name and refuses anything else; with none, the window wears
// its own. If it regresses, a settings file with the window's theme in it
// is refused by every tend, or a typo goes unsaid.
func TestTheWindowsThemeIsItsOwnSetting(t *testing.T) {
	for _, ok := range []string{"tend", "tui", "terminal", "catppuccin", "tokyo-night"} {
		if _, err := parse("[desktop]\ntheme = \""+ok+"\"\n", "t.toml"); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	if _, err := parse("[desktop]\ntheme = \"neon\"\n", "t.toml"); err == nil {
		t.Error("an unknown theme was taken")
	}
	if Defaults().Desktop.Theme != "" {
		t.Error("the default is not the window's own theme")
	}
}
