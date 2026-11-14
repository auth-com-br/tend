// Package tmuxshim answers the tmux commands Claude Code's agent teams give,
// with tend's panes.
//
// herdr has nothing like it: this is tend's, after cmux's `claude-teams`.
// Claude Code opens a teammate in a terminal pane of its own only through
// tmux (or iTerm2). It decides it is in tmux when $TMUX is set — nothing
// else, read from its source (2.1.323: `insideTmux = !!process.env.TMUX`) —
// and then drives the panes with the `tmux` it finds on PATH:
//
//	display-message -t %3 -p '#{window_id}'       which window am I in
//	list-panes -t @2 -F '#{pane_id}'              who is in it
//	split-window -d -t %3 -h -l 70% -P -F '#{pane_id}' -- cat
//	select-pane -t %7 -T researcher               the teammate's name
//	set-option -p -t %7 remain-on-exit failed
//	respawn-pane -k -t %7 -- 'cd … && env … claude --agent-id …'
//	kill-pane -t %7
//
// `tend claude-teams` starts Claude Code with $TMUX pointing here and this
// package's `tmux` first on its PATH, and each of those becomes a tend pane:
// split, named, started, closed — and seen by detection like any other.
//
// Only that launch is touched. $TMUX is not set in tend's panes: programs
// read it as "wrap your escapes for tmux" (neovim's clipboard, image
// protocols, Claude Code itself), and a real tmux started in a pane refuses
// to nest under it. Outside a claude-teams launch the `tmux` here is not on
// PATH at all, and inside one a tmux command meant for a real server — one
// with its own -L or -S — is handed to the real tmux (see cmd/tend).
package tmuxshim

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Version is what `tmux -V` answers. Claude Code only asks whether there is
// a tmux; a script that reads the number gets one that has every command
// used here.
const Version = "tmux 3.5"

// Pane is a tend pane as the shim sees it.
type Pane struct {
	ID      string // p_N
	Tab     string // t_N
	Title   string
	Cwd     string
	Pid     int
	Focused bool
}

// Backend is the session the panes are in: tend's automation socket, or a
// fake in a test.
type Backend interface {
	Panes() ([]Pane, error)
	// Split divides target and runs argv in the new pane; no argv is the
	// shell.
	Split(target string, down bool, run Run) (Pane, error)
	// NewTab opens a tab in near's space, running argv.
	NewTab(near, name string, run Run) (Pane, error)
	// Respawn ends what pane runs and runs argv there instead.
	Respawn(pane string, run Run) error
	Close(pane string) error
	Rename(pane, title string) error
	Focus(pane string) error
	// Read is the pane's text: what it shows, or with lines above zero that
	// many of its last lines, history included.
	Read(pane string, lines int) (string, error)
	SendText(pane, text string) error
	SendKeys(pane string, keys []string) error
}

// Run is what a pane is asked to run.
type Run struct {
	Argv []string
	Dir  string
	// Agent names the agent, when the shim can tell: a teammate's command
	// is a shell line whose first word is sh, and detection would otherwise
	// look for an agent called sh.
	Agent       string
	CloseOnExit bool
}

// Shim answers one tmux command line.
type Shim struct {
	Backend Backend
	// Leader is the pane `tend claude-teams` ran in: tmux's "current pane"
	// for every command that names none.
	Leader string
	// Session is the name `#{session_name}` gives.
	Session string
	// State is the file the team's own panes are kept in between commands,
	// each of which is a process of its own.
	State          string
	Stdout, Stderr io.Writer

	panes []Pane
	team  team
}

// team is what one launch has made, kept in Shim.State.
type team struct {
	// Panes are the panes this launch split or opened, in order: with the
	// leader, the "window" Claude Code lays its teammates out in.
	Panes []string `json:"panes"`
	// Remain is tmux's remain-on-exit, per pane, as it was set.
	Remain map[string]string `json:"remain,omitempty"`
}

// errUsage is a command tmux would refuse.
type errUsage struct{ msg string }

func (e errUsage) Error() string { return e.msg }

func usage(format string, args ...any) error { return errUsage{fmt.Sprintf(format, args...)} }

// Main runs a tmux command line, global flags and all, and returns the exit
// status tmux would.
func (s *Shim) Main(args []string) int {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
		flag := args[0]
		args = args[1:]
		switch flag {
		case "-V":
			fmt.Fprintln(s.Stdout, Version)
			return 0
		case "-S", "-L", "-f", "-T":
			// The server to talk to and its config: there is one server
			// here, the session the leader is in.
			if len(args) == 0 {
				fmt.Fprintf(s.Stderr, "tmux: option requires an argument -- %s\n", flag[1:])
				return 1
			}
			args = args[1:]
		case "--":
			goto commands
		default:
			// -2 -u -v -N -l: terminal and logging choices with nothing
			// to do here.
		}
	}
commands:
	if len(args) == 0 {
		fmt.Fprintln(s.Stderr, "tmux: tend answers tmux commands for Claude Code's agent teams; it does not start an interactive tmux")
		return 1
	}
	if err := s.load(); err != nil {
		fmt.Fprintf(s.Stderr, "tmux: %v\n", err)
		return 1
	}
	for _, cmd := range splitCommands(args) {
		if err := s.run(cmd); err != nil {
			fmt.Fprintf(s.Stderr, "%v\n", err)
			return 1
		}
	}
	if err := s.save(); err != nil {
		fmt.Fprintf(s.Stderr, "tmux: %v\n", err)
		return 1
	}
	return 0
}

// splitCommands cuts `a \; b` — which reaches tmux as a lone ";" — into
// its commands.
func splitCommands(args []string) [][]string {
	var out [][]string
	var cur []string
	for _, a := range args {
		if a == ";" {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, a)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func (s *Shim) load() error {
	panes, err := s.Backend.Panes()
	if err != nil {
		return err
	}
	s.panes = panes
	s.team = team{}
	if s.State != "" {
		raw, err := os.ReadFile(s.State)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &s.team)
		}
	}
	// A pane closed since — by hand, or by its program ending — is no
	// longer the team's.
	live := s.team.Panes[:0]
	for _, id := range s.team.Panes {
		if _, ok := s.pane(id); ok {
			live = append(live, id)
		}
	}
	s.team.Panes = live
	return nil
}

func (s *Shim) save() error {
	if s.State == "" {
		return nil
	}
	raw, err := json.Marshal(s.team)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.State), 0o700); err != nil {
		return err
	}
	tmp := s.State + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.State)
}

func (s *Shim) pane(id string) (Pane, bool) {
	for _, p := range s.panes {
		if p.ID == id {
			return p, true
		}
	}
	return Pane{}, false
}

func (s *Shim) adopt(p Pane) {
	s.panes = append(s.panes, p)
	s.team.Panes = append(s.team.Panes, p.ID)
}

// --- names -------------------------------------------------------------------

// TmuxPane is tmux's name for a pane: p_7 is %7.
func TmuxPane(id string) string { return "%" + strings.TrimPrefix(id, "p_") }

func tmuxWindow(tab string) string { return "@" + strings.TrimPrefix(tab, "t_") }

// paneTarget reads a -t naming a pane: %7, session:@2.%7, @2 (its first
// pane), or anything else tmux would read as "here" — the leader.
func (s *Shim) paneTarget(t string) (Pane, error) {
	if t == "" || t == "." || t == "!" {
		return s.leader()
	}
	if i := strings.LastIndexByte(t, '%'); i >= 0 {
		n := digits(t[i+1:])
		if n == "" {
			return Pane{}, usage("can't find pane: %s", t)
		}
		if p, ok := s.pane("p_" + n); ok {
			return p, nil
		}
		return Pane{}, usage("can't find pane: %s", t)
	}
	if i := strings.LastIndexByte(t, '@'); i >= 0 {
		tab := "t_" + digits(t[i+1:])
		if panes := s.windowPanes(tab); len(panes) > 0 {
			return panes[0], nil
		}
		return Pane{}, usage("can't find window: %s", t)
	}
	return s.leader()
}

// windowTarget reads a -t naming a window: @2, %7 (the window it is in), or
// anything else — the leader's.
func (s *Shim) windowTarget(t string) (string, error) {
	if i := strings.LastIndexByte(t, '@'); i >= 0 && !strings.Contains(t[i:], "%") {
		tab := "t_" + digits(t[i+1:])
		if len(s.windowPanes(tab)) > 0 {
			return tab, nil
		}
		return "", usage("can't find window: %s", t)
	}
	p, err := s.paneTarget(t)
	if err != nil {
		return "", err
	}
	return p.Tab, nil
}

func digits(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

func (s *Shim) leader() (Pane, error) {
	if p, ok := s.pane(s.Leader); ok {
		return p, nil
	}
	return Pane{}, usage("no current pane: the pane tend claude-teams ran in (%s) is gone", s.Leader)
}

// windowPanes are a window's panes in tmux's order. The leader's window is
// the leader and the panes this team made there, not every pane of the tab:
// Claude Code counts the window's panes to choose where the next teammate
// goes, and splits the ones it finds — a shell the user keeps beside the
// agent is not one of its teammates to split.
func (s *Shim) windowPanes(tab string) []Pane {
	lead, ok := s.pane(s.Leader)
	if ok && lead.Tab == tab {
		out := []Pane{lead}
		for _, id := range s.team.Panes {
			if p, ok := s.pane(id); ok && p.Tab == tab {
				out = append(out, p)
			}
		}
		return out
	}
	var out []Pane
	for _, p := range s.panes {
		if p.Tab == tab {
			out = append(out, p)
		}
	}
	return out
}

func (s *Shim) windows() []string {
	seen := map[string]bool{}
	var tabs []string
	for _, p := range s.panes {
		if p.Tab != "" && !seen[p.Tab] {
			seen[p.Tab] = true
			tabs = append(tabs, p.Tab)
		}
	}
	sort.Slice(tabs, func(i, j int) bool { return number(tabs[i]) < number(tabs[j]) })
	return tabs
}

func number(id string) int {
	_, n, _ := strings.Cut(id, "_")
	v, _ := strconv.Atoi(n)
	return v
}

// --- formats -----------------------------------------------------------------

// vars are the format variables for a pane.
func (s *Shim) vars(p Pane) map[string]string {
	index := 0
	panes := s.windowPanes(p.Tab)
	for i, q := range panes {
		if q.ID == p.ID {
			index = i
		}
	}
	window := 0
	for i, t := range s.windows() {
		if t == p.Tab {
			window = i
		}
	}
	active := "0"
	if p.Focused {
		active = "1"
	}
	pid := ""
	if p.Pid > 0 {
		pid = strconv.Itoa(p.Pid)
	}
	return map[string]string{
		"pane_id": TmuxPane(p.ID), "pane_index": strconv.Itoa(index), "pane_title": p.Title,
		"pane_current_path": p.Cwd, "pane_pid": pid, "pane_active": active, "pane_dead": "0",
		"window_id": tmuxWindow(p.Tab), "window_index": strconv.Itoa(window), "window_name": "tend",
		"window_panes": strconv.Itoa(len(panes)), "window_active": active,
		"session_name": s.Session, "session_id": "$0", "host": hostname(),
	}
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// short are tmux's one-letter aliases.
var short = map[byte]string{
	'D': "pane_id", 'P': "pane_index", 'T': "pane_title", 'S': "session_name",
	'I': "window_index", 'W': "window_name", 'H': "host",
}

// expand fills in a format. A variable tend does not know is an error, not
// an empty string: Claude Code reads an answer it cannot parse as "not
// tmux" and gives up cleanly, where a blank it took for a value would send
// it the wrong way (eight tab-separated fields it reads for images, say).
func expand(format string, vars map[string]string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '#' || i+1 >= len(format) {
			b.WriteByte(c)
			continue
		}
		next := format[i+1]
		switch {
		case next == '#':
			b.WriteByte('#')
			i++
		case next == '{':
			end := strings.IndexByte(format[i:], '}')
			if end < 0 {
				return "", usage("format: unterminated #{ in %q", format)
			}
			name := format[i+2 : i+end]
			v, ok := vars[name]
			if !ok {
				return "", usage("format: tend does not know #{%s}", name)
			}
			b.WriteString(v)
			i += end
		case short[next] != "":
			b.WriteString(vars[short[next]])
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// --- flags -------------------------------------------------------------------

type flags struct {
	set    map[byte]bool
	values map[byte]string
	rest   []string
}

// parse reads getopt-style flags as tmux does: -dP is -d -P, -t%7 is -t %7,
// and the first word that is not a flag, or a --, ends them.
func parse(args []string, valued string) (flags, error) {
	f := flags{set: map[byte]bool{}, values: map[byte]string{}}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if len(a) < 2 || a[0] != '-' {
			break
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte(valued, c) < 0 {
				f.set[c] = true
				continue
			}
			v := a[j+1:]
			if v == "" {
				if i+1 >= len(args) {
					return f, usage("option requires an argument -- %c", c)
				}
				i++
				v = args[i]
			}
			f.values[c] = v
			f.set[c] = true
			break
		}
	}
	f.rest = args[i:]
	return f, nil
}

// --- commands ----------------------------------------------------------------

// aliases are tmux's short command names.
var aliases = map[string]string{
	"splitw": "split-window", "send": "send-keys", "capturep": "capture-pane",
	"lsp": "list-panes", "lsw": "list-windows", "display": "display-message",
	"killp": "kill-pane", "killw": "kill-window", "selectp": "select-pane",
	"selectw": "select-window", "respawnp": "respawn-pane", "neww": "new-window",
	"new": "new-session", "has": "has-session", "set": "set-option",
	"setw": "set-window-option", "selectl": "select-layout", "resizep": "resize-pane",
	"show": "show-options", "showw": "show-window-options", "renamew": "rename-window",
	"refresh": "refresh-client", "loadb": "load-buffer", "attach": "attach-session",
	"switchc": "switch-client", "kill-server": "kill-server",
}

func (s *Shim) run(cmd []string) error {
	name := cmd[0]
	if full, ok := aliases[name]; ok {
		name = full
	}
	args := cmd[1:]
	switch name {
	case "display-message":
		return s.displayMessage(args)
	case "list-panes":
		return s.listPanes(args)
	case "list-windows":
		return s.listWindows(args)
	case "split-window":
		return s.splitWindow(args)
	case "new-window", "new-session":
		return s.newWindow(name, args)
	case "respawn-pane":
		return s.respawnPane(args)
	case "kill-pane":
		return s.killPane(args)
	case "kill-window":
		return s.killWindow(args)
	case "select-pane":
		return s.selectPane(args)
	case "select-window":
		f, err := parse(args, "t")
		if err != nil {
			return err
		}
		tab, err := s.windowTarget(f.values['t'])
		if err != nil {
			return err
		}
		return s.Backend.Focus(s.windowPanes(tab)[0].ID)
	case "send-keys":
		return s.sendKeys(args)
	case "capture-pane":
		return s.capturePane(args)
	case "has-session":
		return nil
	case "set-option", "set-window-option":
		return s.setOption(args)
	case "select-layout", "resize-pane", "rename-window", "refresh-client", "set-hook",
		"bind-key", "unbind-key", "source-file", "set-environment":
		// How panes are laid out and drawn is tend's to decide, and these
		// change nothing a teammate needs: succeeding is what lets Claude
		// Code go on to start it.
		return nil
	case "show-options", "show-window-options":
		// tend keeps no tmux options to show. Claude Code reads a failure
		// here as "unknown" and shows no hint about them.
		return usage("tend keeps no tmux options")
	default:
		return usage("tend does not answer tmux %s", name)
	}
}

func (s *Shim) print(format string, p Pane) error {
	out, err := expand(format, s.vars(p))
	if err != nil {
		return err
	}
	fmt.Fprintln(s.Stdout, out)
	return nil
}

func (s *Shim) displayMessage(args []string) error {
	f, err := parse(args, "tcdF")
	if err != nil {
		return err
	}
	if !f.set['p'] {
		// A message for the status line, which tend does not have.
		return nil
	}
	p, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	format := strings.Join(f.rest, " ")
	if f.set['F'] {
		format = f.values['F']
	}
	return s.print(format, p)
}

func (s *Shim) listPanes(args []string) error {
	f, err := parse(args, "tFf")
	if err != nil {
		return err
	}
	format := "#{pane_index}: #{pane_id}"
	if f.set['F'] {
		format = f.values['F']
	}
	var panes []Pane
	if f.set['a'] || f.set['s'] {
		for _, tab := range s.windows() {
			panes = append(panes, s.windowPanes(tab)...)
		}
	} else {
		tab, err := s.windowTarget(f.values['t'])
		if err != nil {
			return err
		}
		panes = s.windowPanes(tab)
	}
	for _, p := range panes {
		if err := s.print(format, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Shim) listWindows(args []string) error {
	f, err := parse(args, "tFf")
	if err != nil {
		return err
	}
	format := "#{window_index}: #{window_name} (#{window_panes} panes) #{window_id}"
	if f.set['F'] {
		format = f.values['F']
	}
	for _, tab := range s.windows() {
		if err := s.print(format, s.windowPanes(tab)[0]); err != nil {
			return err
		}
	}
	return nil
}

// command reads a tmux shell-command: one word is a line for the shell, as
// tmux runs it; several are the program and its arguments. -e VAR=value
// goes in front through env, which keeps the line as it was given.
func command(rest []string, env string) []string {
	var argv []string
	switch len(rest) {
	case 0:
	case 1:
		argv = []string{"/bin/sh", "-c", rest[0]}
	default:
		argv = append([]string(nil), rest...)
	}
	if env != "" && len(argv) > 0 {
		argv = append([]string{"env", env}, argv...)
	}
	return argv
}

func (s *Shim) splitWindow(args []string) error {
	f, err := parse(args, "tlcepF")
	if err != nil {
		return err
	}
	target, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	// tmux's default split is one pane above the other; -h puts them side
	// by side. The -l size is not kept: tend halves the pane, and Claude
	// Code's own select-layout after it changes nothing here either.
	run := Run{Argv: command(f.rest, f.values['e']), Dir: f.values['c'], CloseOnExit: true}
	p, err := s.Backend.Split(target.ID, !f.set['h'], run)
	if err != nil {
		return err
	}
	s.adopt(p)
	if f.set['P'] {
		format := "#{session_name}:#{window_index}.#{pane_index}"
		if f.set['F'] {
			format = f.values['F']
		}
		return s.print(format, p)
	}
	return nil
}

func (s *Shim) newWindow(name string, args []string) error {
	valued := "tncefF"
	if name == "new-session" {
		valued = "sntcefFxy"
	}
	f, err := parse(args, valued)
	if err != nil {
		return err
	}
	if name == "new-session" && !f.set['d'] {
		return usage("tend does not attach to a tmux session; give new-session -d")
	}
	title := f.values['n']
	if title == "" {
		title = f.values['s']
	}
	run := Run{Argv: command(f.rest, f.values['e']), Dir: f.values['c'], CloseOnExit: true}
	p, err := s.Backend.NewTab(s.Leader, title, run)
	if err != nil {
		return err
	}
	s.adopt(p)
	if f.set['P'] {
		format := "#{session_name}:#{window_index}.#{pane_index}"
		if f.set['F'] {
			format = f.values['F']
		}
		return s.print(format, p)
	}
	return nil
}

// remainWrap is a shell line made to keep its pane when it fails, as tmux's
// remain-on-exit failed does: the pane closes when the program ends well,
// and stays with its last words on screen when it does not. tend closes a
// close-on-exit pane either way, so the line waits for a key instead.
func remainWrap(line string) string {
	return line + "\n" +
		`status=$?; [ "$status" -eq 0 ] && exit 0; ` +
		`printf '\n\033[2m[exited with status %s — press Enter to close]\033[0m\n' "$status"; ` +
		`read -r _; exit "$status"`
}

// teammate names the agent a respawned line runs, when it is Claude Code's
// teammate: the line is `cd … && env … <claude's own path> --agent-id …`,
// and the path is often a versioned binary whose name is its version.
func teammate(argv []string) string {
	line := strings.Join(argv, " ")
	if strings.Contains(line, "--agent-id") && strings.Contains(line, "--team-name") {
		return "claude"
	}
	for _, w := range strings.Fields(line) {
		if filepath.Base(w) == "claude" {
			return "claude"
		}
	}
	return ""
}

func (s *Shim) respawnPane(args []string) error {
	f, err := parse(args, "tce")
	if err != nil {
		return err
	}
	p, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	argv := command(f.rest, f.values['e'])
	if len(argv) == 0 {
		return usage("tend needs the command to respawn %s with", TmuxPane(p.ID))
	}
	run := Run{Argv: argv, Dir: f.values['c'], Agent: teammate(argv), CloseOnExit: true}
	switch s.team.Remain[p.ID] {
	case "on":
		run.CloseOnExit = false
	case "failed":
		if len(argv) == 3 && argv[0] == "/bin/sh" && argv[1] == "-c" {
			run.Argv = []string{"/bin/sh", "-c", remainWrap(argv[2])}
		}
	}
	return s.Backend.Respawn(p.ID, run)
}

func (s *Shim) killPane(args []string) error {
	f, err := parse(args, "t")
	if err != nil {
		return err
	}
	p, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	if f.set['a'] {
		// Every pane but this one: in the leader's window, the team.
		for _, q := range s.windowPanes(p.Tab) {
			if q.ID != p.ID {
				if err := s.Backend.Close(q.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return s.Backend.Close(p.ID)
}

func (s *Shim) killWindow(args []string) error {
	f, err := parse(args, "t")
	if err != nil {
		return err
	}
	tab, err := s.windowTarget(f.values['t'])
	if err != nil {
		return err
	}
	for _, q := range s.windowPanes(tab) {
		if q.ID == s.Leader {
			// The leader's window is the team; the leader itself is the
			// person's pane, not the shim's to close.
			continue
		}
		if err := s.Backend.Close(q.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Shim) selectPane(args []string) error {
	f, err := parse(args, "tT")
	if err != nil {
		return err
	}
	p, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	if f.set['T'] {
		return s.Backend.Rename(p.ID, f.values['T'])
	}
	if f.set['L'] || f.set['R'] || f.set['U'] || f.set['D'] || f.set['l'] ||
		f.set['d'] || f.set['e'] || f.set['m'] || f.set['M'] || f.set['Z'] {
		return nil
	}
	return s.Backend.Focus(p.ID)
}

func (s *Shim) setOption(args []string) error {
	f, err := parse(args, "t")
	if err != nil {
		return err
	}
	if len(f.rest) >= 2 && f.rest[0] == "remain-on-exit" {
		p, err := s.paneTarget(f.values['t'])
		if err != nil {
			return err
		}
		if s.team.Remain == nil {
			s.team.Remain = map[string]string{}
		}
		s.team.Remain[p.ID] = f.rest[1]
	}
	// Everything else is how tmux draws a pane — its border's colour and
	// title — which tend draws its own way.
	return nil
}

// keyNames are tmux's key names, as tend's send_keys names them.
var keyNames = map[string]string{
	"enter": "enter", "c-m": "enter", "escape": "escape", "tab": "tab", "c-i": "tab",
	"bspace": "backspace", "space": "space", "up": "up", "down": "down", "left": "left",
	"right": "right", "home": "home", "end": "end", "ppage": "pageup", "pageup": "pageup",
	"npage": "pagedown", "pagedown": "pagedown", "dc": "delete", "ic": "insert", "btab": "shift+tab",
}

// tmuxKey turns a tmux key name into tend's, or says it is text.
func tmuxKey(word string) (string, bool) {
	lower := strings.ToLower(word)
	if k, ok := keyNames[lower]; ok {
		return k, true
	}
	if len(word) > 2 && (word[1] == '-') {
		switch word[0] {
		case 'C', 'c':
			if k, ok := tmuxKey(word[2:]); ok {
				return "ctrl+" + k, true
			}
			if len(word) == 3 {
				return "ctrl+" + strings.ToLower(word[2:]), true
			}
		case 'M', 'm':
			if k, ok := tmuxKey(word[2:]); ok {
				return "alt+" + k, true
			}
			if len(word) == 3 {
				return "alt+" + word[2:], true
			}
		case 'S', 's':
			if k, ok := tmuxKey(word[2:]); ok {
				return "shift+" + k, true
			}
		}
	}
	return "", false
}

func (s *Shim) sendKeys(args []string) error {
	f, err := parse(args, "tNX")
	if err != nil {
		return err
	}
	p, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	if f.set['l'] {
		return s.Backend.SendText(p.ID, strings.Join(f.rest, ""))
	}
	// In order, the text between keys sent as text: "ls" "Enter" is the
	// two letters and then the key.
	var text strings.Builder
	flush := func() error {
		if text.Len() == 0 {
			return nil
		}
		err := s.Backend.SendText(p.ID, text.String())
		text.Reset()
		return err
	}
	for _, w := range f.rest {
		if k, ok := tmuxKey(w); ok {
			if err := flush(); err != nil {
				return err
			}
			if err := s.Backend.SendKeys(p.ID, []string{k}); err != nil {
				return err
			}
			continue
		}
		text.WriteString(w)
	}
	return flush()
}

func (s *Shim) capturePane(args []string) error {
	f, err := parse(args, "tSEb")
	if err != nil {
		return err
	}
	p, err := s.paneTarget(f.values['t'])
	if err != nil {
		return err
	}
	if !f.set['p'] {
		// Into a paste buffer, which tend does not keep.
		return nil
	}
	lines := 0
	switch start := f.values['S']; {
	case start == "-":
		lines = 10000
	case strings.HasPrefix(start, "-"):
		n, _ := strconv.Atoi(start[1:])
		// -S -N is N lines above the screen, which tend's reading counts
		// with the screen's own.
		lines = n + 100
	}
	text, err := s.Backend.Read(p.ID, lines)
	if err != nil {
		return err
	}
	text = strings.TrimRight(text, "\n")
	fmt.Fprintln(s.Stdout, text)
	return nil
}
