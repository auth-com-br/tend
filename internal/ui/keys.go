package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
)

// Key handling is a state machine over raw bytes rather than a parsed key
// event, because almost every byte belongs to the focused pane and must reach
// it untouched. Only the prefix key and the one command key after it are the
// client's; everything else is forwarded exactly as typed, including escape
// sequences the client has never heard of.

// Prefix is the key that arms a command. Ctrl+B, as in tmux and screen, since
// anyone reaching for a multiplexer already has it in their fingers.
const Prefix = 0x02

// Command is what a prefix key sequence asked for.
type Command uint8

const (
	// CommandNone means nothing was triggered.
	CommandNone Command = iota
	CommandSplitColumns
	CommandSplitRows
	CommandFocusLeft
	CommandFocusRight
	CommandFocusUp
	CommandFocusDown
	CommandFocusNext
	CommandClosePane
	CommandZoom
	CommandScroll
	CommandGrowLeft
	CommandGrowRight
	CommandGrowUp
	CommandGrowDown
	CommandNewTab
	CommandNextTab
	CommandPrevTab
	CommandSelectTab
	CommandNextSpace
	CommandPrevSpace
	CommandNewSpace
	CommandToggleAgents
	CommandNavigate
	CommandMenu
	CommandRenameTab
	CommandRenameSpace
	CommandDetach
	CommandRefresh
	CommandHelp
	// CommandSwapLeft and its siblings trade the focused pane with its
	// neighbour on that side, herdr's prefix+shift+h/j/k/l.
	CommandSwapLeft
	CommandSwapRight
	CommandSwapUp
	CommandSwapDown
	// CommandResizeMode enters resize mode, where h/j/k/l move the focused
	// pane's edges until escape — herdr's prefix+r.
	CommandResizeMode
	// CommandSettings opens the settings screen, herdr's prefix+s.
	CommandSettings
	// CommandNewWorktree makes a worktree for the current space's repository
	// and opens it as a space of its own, herdr's prefix+shift+g.
	CommandNewWorktree
	// CommandEditScrollback opens the focused pane's history in $EDITOR,
	// herdr's prefix+e.
	CommandEditScrollback
	// CommandFocusPrev goes back through the tab's panes, herdr's
	// prefix+shift+tab; CommandFocusNext is also on prefix+tab.
	CommandFocusPrev
	// CommandLastPane goes back to the pane that was focused before this one,
	// wherever it is.
	CommandLastPane
	// CommandPrevAgent and CommandNextAgent step through the agents in the
	// order the sidebar lists them.
	CommandPrevAgent
	CommandNextAgent
	// CommandOpenNotification goes to the pane the last notification was
	// about, herdr's open_notification_target. Unbound by default, as in
	// herdr; keys.bind gives it a key.
	CommandOpenNotification
	// CommandFiles opens the file explorer docked on the left of the tab, goes
	// to it when it is open, and closes it when it is where the focus is.
	CommandFiles
	// CommandNavigator opens herdr's navigator: every space, tab and pane in
	// a popup, searchable and filtered by state (herdr's goto, prefix+g).
	CommandNavigator
	// CommandAgentManager opens the agent manager: the agent CLIs tend knows
	// of, found or not, and a way to install each (tend's own, prefix+A).
	CommandAgentManager
	// CommandMCPManager opens the MCP manager: the agents' MCP servers, to
	// switch on and off, test, add and remove (tend's own, prefix+M).
	CommandMCPManager
	// CommandSessions opens the sessions list: the conversations agents
	// keep on this machine, to find, resume and delete (tend's own,
	// prefix+S).
	CommandSessions
	// CommandIssues opens the GitHub issues of the project being worked in
	// (tend's own, prefix+I).
	CommandIssues
	// CommandErrors opens the errors a GlitchTip server collected (tend's
	// own, prefix+E).
	CommandErrors
	// CommandContext opens the context panel: what tools captured, to copy
	// or send to an agent (tend's own, prefix+C).
	CommandContext
	// CommandBrowser asks for a page and opens it in the browser (tend's
	// own, prefix+B).
	CommandBrowser
	// CommandTickets opens the tickets panel: Linear, Jira, ClickUp and the
	// Issue Gateway (tend's own, prefix+T).
	CommandTickets
	// CommandCompanies opens the companies panel: which company's spaces
	// the sidebar shows (herdr's user-defined Spaces, prefix+O).
	CommandCompanies
	// CommandLiteralPrefix sends the prefix key itself to the pane, which is
	// how an inner multiplexer or an editor bound to Ctrl+B still receives it.
	CommandLiteralPrefix
	// CommandCustom runs one of the user's [[keys.command]] entries; Arg says
	// which. It has no name of its own and no default key: the entries are
	// the user's, not the table's.
	CommandCustom
)

func (c Command) String() string {
	switch c {
	case CommandSplitColumns:
		return "split-columns"
	case CommandSplitRows:
		return "split-rows"
	case CommandFocusLeft:
		return "focus-left"
	case CommandFocusRight:
		return "focus-right"
	case CommandFocusUp:
		return "focus-up"
	case CommandFocusDown:
		return "focus-down"
	case CommandFocusNext:
		return "focus-next"
	case CommandClosePane:
		return "close-pane"
	case CommandZoom:
		return "zoom"
	case CommandScroll:
		return "scroll"
	case CommandGrowLeft:
		return "grow-left"
	case CommandGrowRight:
		return "grow-right"
	case CommandGrowUp:
		return "grow-up"
	case CommandGrowDown:
		return "grow-down"
	case CommandNewTab:
		return "new-tab"
	case CommandNextTab:
		return "next-tab"
	case CommandPrevTab:
		return "prev-tab"
	case CommandSelectTab:
		return "select-tab"
	case CommandNextSpace:
		return "next-space"
	case CommandPrevSpace:
		return "prev-space"
	case CommandNewSpace:
		return "new-space"
	case CommandToggleAgents:
		return "toggle-agents"
	case CommandNavigate:
		return "navigate"
	case CommandMenu:
		return "menu"
	case CommandRenameTab:
		return "rename-tab"
	case CommandRenameSpace:
		return "rename-space"
	case CommandDetach:
		return "detach"
	case CommandRefresh:
		return "refresh"
	case CommandHelp:
		return "help"
	case CommandSwapLeft:
		return "swap-left"
	case CommandSwapRight:
		return "swap-right"
	case CommandSwapUp:
		return "swap-up"
	case CommandSwapDown:
		return "swap-down"
	case CommandResizeMode:
		return "resize-mode"
	case CommandSettings:
		return "settings"
	case CommandNewWorktree:
		return "new-worktree"
	case CommandEditScrollback:
		return "edit-scrollback"
	case CommandFocusPrev:
		return "focus-prev"
	case CommandLastPane:
		return "last-pane"
	case CommandPrevAgent:
		return "prev-agent"
	case CommandNextAgent:
		return "next-agent"
	case CommandOpenNotification:
		return "open-notification"
	case CommandLiteralPrefix:
		return "literal-prefix"
	case CommandFiles:
		return "files"
	case CommandNavigator:
		return "navigator"
	case CommandAgentManager:
		return "agent-manager"
	case CommandMCPManager:
		return "mcp-manager"
	case CommandSessions:
		return "sessions"
	case CommandIssues:
		return "issues"
	case CommandErrors:
		return "errors"
	case CommandContext:
		return "context"
	case CommandBrowser:
		return "browser"
	case CommandTickets:
		return "tickets"
	case CommandCompanies:
		return "companies"
	default:
		return "none"
	}
}

// Keys is the bindings, listed once so the help text cannot drift from what
// the keys actually do.
var Keys = []struct {
	Key     string
	Command Command
	Help    string
}{
	{"|", CommandSplitColumns, "split beside"},
	{"-", CommandSplitRows, "split below"},
	{"h ←", CommandFocusLeft, "focus left"},
	{"l →", CommandFocusRight, "focus right"},
	{"k ↑", CommandFocusUp, "focus up"},
	{"j ↓", CommandFocusDown, "focus down"},
	{"o tab", CommandFocusNext, "focus next"},
	{"⇧tab", CommandFocusPrev, "focus previous"},
	{";", CommandLastPane, "last pane"},
	{"< >", CommandNextAgent, "previous / next agent"},
	{"x", CommandClosePane, "close pane"},
	{"z", CommandZoom, "zoom pane"},
	{"[", CommandScroll, "copy mode"},
	{"e", CommandEditScrollback, "history in $EDITOR"},
	{"f", CommandFiles, "files and git, docked left"},
	{"HJKL", CommandSwapRight, "swap pane"},
	{"r", CommandResizeMode, "resize (hjkl, esc)"},
	{"c", CommandNewTab, "new tab"},
	{"n", CommandNextTab, "next tab"},
	{"p", CommandPrevTab, "previous tab"},
	{"1-9", CommandSelectTab, "go to tab"},
	{"s", CommandSettings, "settings"},
	{"N", CommandNewSpace, "new space"},
	{"G", CommandNewWorktree, "new worktree"},
	{"( )", CommandNextSpace, "switch space"},
	{"a", CommandToggleAgents, "show agents"},
	{"A", CommandAgentManager, "agent manager: find, install"},
	{"M", CommandMCPManager, "MCP servers: on/off, test, add, remove"},
	{"S", CommandSessions, "agent sessions: find, resume, delete"},
	{"I", CommandIssues, "GitHub issues of this project"},
	{"E", CommandErrors, "errors from GlitchTip, to fix"},
	{"T", CommandTickets, "tickets: Linear, Jira, ClickUp, Issue Gateway"},
	{"C", CommandContext, "context: captured, to send to an agent"},
	{"B", CommandBrowser, "open a page in the browser"},
	{"O", CommandCompanies, "companies: which spaces the sidebar shows"},
	{"g", CommandNavigator, "find a space, tab or pane"},
	{"w", CommandNavigate, "walk the sidebar"},
	{"m", CommandMenu, "menu"},
	{",", CommandRenameTab, "rename tab"},
	{".", CommandRenameSpace, "rename space"},
	{"d", CommandDetach, "detach"},
	{"R", CommandRefresh, "reload settings, redraw"},
	{"?", CommandHelp, "this help"},
}

// KeyName is what a key after the prefix is called, in the settings file and
// in the help.
//
// A key here is one byte or one escape sequence, because that is what a
// terminal sends: there is no key event with modifiers to inspect. So shift+s
// is "S", and ctrl+s is a control byte that nothing binds. The names for what
// has no printable form are herdr's ("tab", "up", "shift+tab").
func KeyName(b byte) string {
	switch b {
	case '\t':
		return "tab"
	case '\r':
		return "enter"
	case ' ':
		return "space"
	case 0x1b:
		return "esc"
	}
	if b < 0x20 || b > 0x7e {
		return ""
	}
	return string(rune(b))
}

// arrowName is what an escape sequence after the prefix is called.
func arrowName(final byte) string {
	switch final {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'C':
		return "right"
	case 'D':
		return "left"
	case 'Z':
		return "shift+tab"
	}
	return ""
}

// DefaultBindings is every key this build binds, by name.
//
// One table, so the parser, the help and the settings file cannot disagree
// about what a key does. A user's own bindings are laid over a copy of it.
func DefaultBindings() map[string]Command {
	out := make(map[string]Command, 48)
	for name, cmd := range map[string]Command{
		"|": CommandSplitColumns, "\\": CommandSplitColumns, "%": CommandSplitColumns,
		"-": CommandSplitRows, "\"": CommandSplitRows,
		"h": CommandFocusLeft, "left": CommandFocusLeft,
		"l": CommandFocusRight, "right": CommandFocusRight,
		"k": CommandFocusUp, "up": CommandFocusUp,
		"j": CommandFocusDown, "down": CommandFocusDown,
		"o": CommandFocusNext, "tab": CommandFocusNext,
		"shift+tab": CommandFocusPrev,
		";":         CommandLastPane,
		"<":         CommandPrevAgent,
		">":         CommandNextAgent,
		"x":         CommandClosePane,
		"z":         CommandZoom,
		"[":         CommandScroll,
		"H":         CommandSwapLeft,
		"L":         CommandSwapRight,
		"K":         CommandSwapUp,
		"J":         CommandSwapDown,
		"r":         CommandResizeMode,
		"c":         CommandNewTab,
		"n":         CommandNextTab,
		"p":         CommandPrevTab,
		"s":         CommandSettings,
		"N":         CommandNewSpace,
		"G":         CommandNewWorktree,
		")":         CommandNextSpace,
		"(":         CommandPrevSpace,
		"a":         CommandToggleAgents,
		"A":         CommandAgentManager,
		"M":         CommandMCPManager,
		"S":         CommandSessions,
		"I":         CommandIssues,
		"E":         CommandErrors,
		"T":         CommandTickets,
		"C":         CommandContext,
		"B":         CommandBrowser,
		"O":         CommandCompanies,
		"g":         CommandNavigator, "w": CommandNavigate,
		"e": CommandEditScrollback,
		"f": CommandFiles,
		"m": CommandMenu,
		",": CommandRenameTab,
		".": CommandRenameSpace,
		"d": CommandDetach,
		"R": CommandRefresh,
		"?": CommandHelp,
	} {
		out[name] = cmd
	}
	return out
}

// ParseCommand reads a command's name, as the settings file writes it. The
// names are what Command.String produces, so the file and the code cannot
// drift apart.
func ParseCommand(name string) (Command, bool) {
	for cmd := CommandNone; cmd < CommandLiteralPrefix; cmd++ {
		if cmd != CommandNone && cmd.String() == name {
			return cmd, true
		}
	}
	return CommandNone, false
}

// CommandNames is every command a key can be bound to, in the order they are
// listed in the help.
func CommandNames() []string {
	seen := map[Command]bool{}
	var out []string
	for _, k := range Keys {
		if !seen[k.Command] {
			seen[k.Command] = true
			out = append(out, k.Command.String())
		}
	}
	for _, cmd := range []Command{
		CommandSplitRows, CommandFocusLeft, CommandFocusRight, CommandFocusUp,
		CommandFocusDown, CommandSwapLeft, CommandSwapUp, CommandSwapDown,
		CommandPrevAgent, CommandPrevSpace, CommandPrevTab, CommandFocusPrev,
		CommandOpenNotification,
	} {
		if !seen[cmd] {
			seen[cmd] = true
			out = append(out, cmd.String())
		}
	}
	return out
}

// BindingsFrom lays a user's bindings over the defaults.
//
// A binding is "<command> = <key>", named as the help names both. Binding a
// key that something else already has takes it: last one wins, and the one
// that lost is reported so a user who bound two things to one key is told
// rather than left wondering.
func BindingsFrom(pairs map[string]string) (map[string]Command, []string, error) {
	out := DefaultBindings()
	var notes []string
	for name, key := range pairs {
		cmd, ok := ParseCommand(name)
		if !ok {
			return nil, nil, fmt.Errorf("keys.bind: no command called %q", name)
		}
		key = strings.TrimSpace(key)
		if !ValidKeyName(key) {
			return nil, nil, fmt.Errorf("keys.bind.%s: %q is not a key tend can read after the prefix", name, key)
		}
		if previous, taken := out[key]; taken && previous != cmd {
			notes = append(notes, key+" was "+previous.String()+", now "+cmd.String())
		}
		// Whatever this command was on before is left alone: a user who
		// binds one key keeps the other, which is what "bind" means.
		out[key] = cmd
	}
	return out, notes, nil
}

// CustomFrom is the key table for the user's commands. A key that one of
// them takes from a default command is reported, as BindingsFrom reports a
// key rebound over another.
func CustomFrom(commands []config.CommandKey, bindings map[string]Command) (map[string]int, []string, error) {
	if len(commands) == 0 {
		return nil, nil, nil
	}
	if bindings == nil {
		bindings = defaultBindings
	}
	out := make(map[string]int, len(commands))
	var notes []string
	for i, c := range commands {
		key := c.KeyName()
		if !ValidKeyName(key) {
			return nil, nil, fmt.Errorf("keys.command[%d]: %q is not a key tend can read after the prefix", i, c.Key)
		}
		if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
			return nil, nil, fmt.Errorf("keys.command[%d]: %s picks a tab and cannot run a command", i, key)
		}
		if cmd, taken := bindings[key]; taken {
			notes = append(notes, key+" was "+cmd.String()+", now runs "+commandLabel(c))
		}
		out[key] = i
	}
	return out, notes, nil
}

// commandLabel is what a user's command is called in the help: what they
// wrote about it, or the command itself.
func commandLabel(c config.CommandKey) string {
	if c.Description != "" {
		return c.Description
	}
	return c.Command
}

// CustomHelpLines are the user's commands, for the end of the help.
func CustomHelpLines(commands []config.CommandKey) []string {
	if len(commands) == 0 {
		return nil
	}
	lines := []string{"your commands:"}
	for _, c := range commands {
		lines = append(lines, "  "+pad(c.KeyName(), 5)+" "+commandLabel(c))
	}
	return lines
}

// ValidKeyName reports whether a key name is one the parser can produce.
func ValidKeyName(name string) bool {
	switch name {
	case "tab", "enter", "space", "esc", "up", "down", "left", "right", "shift+tab":
		return true
	}
	r := []rune(name)
	if len(r) != 1 {
		return false
	}
	return r[0] >= 0x21 && r[0] <= 0x7e
}

// Input turns a byte stream into pane input and client commands.
//
// The zero value is ready to use. It is not safe for concurrent use; one
// reader feeds it.
type Input struct {
	// PrefixKey overrides the default. Zero uses Prefix; a prefix of none is
	// expressed by setting it to a byte no keyboard produces, which Disabled
	// does.
	PrefixKey byte
	// Bindings overrides what the keys after the prefix do. Nil uses
	// DefaultBindings, which is what every key in the help comes from.
	Bindings map[string]Command
	// Custom maps a key to one of the user's commands, by index. It is
	// looked at first: a key the user gave a command of their own does that,
	// as herdr lets a custom command take a default key.
	Custom map[string]int

	armed bool
	// pending holds an escape sequence being read after the prefix, so that
	// an arrow key — three bytes that may arrive separately — is recognised
	// rather than half-forwarded.
	pending []byte
	// partialMouse holds the beginning of a mouse report split across reads.
	partialMouse []byte
}

// maxPartialMouse bounds how long a suspected mouse report is held. Beyond
// this it was never one, and holding it would swallow real input.
const maxPartialMouse = 32

// Disabled is a prefix that no key produces, for a user who has turned the
// prefix off and drives tend some other way.
const Disabled byte = 0xFF

// binding looks a key name up in whatever this input is bound to.
func (in *Input) binding(name string) (Command, bool) {
	if name == "" {
		return CommandNone, false
	}
	if in.Bindings != nil {
		cmd, ok := in.Bindings[name]
		return cmd, ok
	}
	cmd, ok := defaultBindings[name]
	return cmd, ok
}

// defaultBindings is built once: the parser asks it for every key.
var defaultBindings = DefaultBindings()

func (in *Input) prefix() byte {
	if in.PrefixKey == 0 {
		return Prefix
	}
	return in.PrefixKey
}

// Armed reports whether the prefix key is waiting for a command, which the
// status bar shows so the user is never guessing about the mode they are in.
func (in *Input) Armed() bool { return in.armed }

// Result is what one byte produced.
type Result struct {
	// Forward is bytes destined for the focused pane.
	Forward []byte
	// Command is a client action, or CommandNone.
	Command Command
	// Arg carries a command's argument, such as which tab a digit selected.
	Arg int
}

// Feed processes one byte.
func (in *Input) Feed(b byte) Result {
	if !in.armed {
		if b == in.prefix() {
			in.armed = true
			in.pending = in.pending[:0]
			return Result{}
		}
		return Result{Forward: []byte{b}}
	}
	return in.command(b)
}

// FeedAll processes a chunk, returning everything to forward and every command
// in order.
//
// Bytes and commands are kept in order relative to each other, because a chunk
// can hold both: a paste that ends mid-sequence, or a prefix typed fast enough
// to arrive with the key after it.
// Action is a command and its argument, kept together so a caller does not
// have to pair them up by position.
type Action struct {
	Command Command
	Arg     int
}

func (in *Input) FeedAll(data []byte) ([]byte, []Action, []MouseEvent) {
	if len(in.partialMouse) > 0 {
		data = append(in.partialMouse, data...)
		in.partialMouse = nil
	}

	var forward []byte
	var commands []Action
	var mice []MouseEvent

	for i := 0; i < len(data); {
		// Mouse reports are read before the key machine sees them: they are
		// not keys, and the prefix has nothing to do with them.
		ev, n, incomplete := parseMouse(data[i:])
		if n > 0 {
			mice = append(mice, ev)
			i += n
			continue
		}
		if incomplete {
			rest := data[i:]
			// A lone escape is never held. It is far more often the Escape
			// key than the start of a mouse report, and holding it means the
			// key does not arrive until the user presses something else —
			// which inside an editor is indistinguishable from tend having
			// eaten it. Two bytes are enough to be worth waiting for.
			if len(rest) >= 2 && len(rest) < maxPartialMouse {
				in.partialMouse = append(in.partialMouse[:0], rest...)
				return forward, commands, mice
			}
		}

		r := in.Feed(data[i])
		forward = append(forward, r.Forward...)
		if r.Command != CommandNone {
			commands = append(commands, Action{Command: r.Command, Arg: r.Arg})
		}
		i++
	}
	return forward, commands, mice
}

// command interprets a byte while the prefix is armed.
func (in *Input) command(b byte) Result {
	// An escape sequence after the prefix is an arrow key. It is collected
	// rather than acted on byte by byte, since its bytes can arrive apart.
	if len(in.pending) > 0 || b == 0x1b {
		in.pending = append(in.pending, b)
		switch {
		case len(in.pending) == 1:
			return Result{} // just ESC so far
		case len(in.pending) == 2:
			if in.pending[1] != '[' {
				// Not an arrow key after all. The prefix is spent and the
				// bytes go to the pane, since guessing would eat a key.
				return in.release()
			}
			return Result{}
		default:
			name := arrowName(in.pending[2])
			in.disarm()
			if i, ok := in.Custom[name]; ok && name != "" {
				return Result{Command: CommandCustom, Arg: i}
			}
			cmd, ok := in.binding(name)
			if !ok {
				return Result{}
			}
			return Result{Command: cmd}
		}
	}

	in.disarm()

	// Digits pick a tab, which is a command with an argument rather than a
	// binding, and nothing else in the table takes one.
	if b >= '1' && b <= '9' {
		return Result{Command: CommandSelectTab, Arg: int(b - '0')}
	}
	if b == in.prefix() {
		return Result{Command: CommandLiteralPrefix, Forward: []byte{in.prefix()}}
	}
	if i, ok := in.Custom[KeyName(b)]; ok {
		return Result{Command: CommandCustom, Arg: i}
	}
	if cmd, ok := in.binding(KeyName(b)); ok {
		return Result{Command: cmd}
	}

	// An unbound key cancels the prefix and is forwarded, so a mistyped
	// command does not silently swallow the next keystroke.
	return Result{Forward: []byte{b}}
}

// release cancels the prefix and forwards what was collected.
func (in *Input) release() Result {
	out := make([]byte, len(in.pending))
	copy(out, in.pending)
	in.disarm()
	return Result{Forward: out}
}

func (in *Input) disarm() {
	in.armed = false
	in.pending = in.pending[:0]
}

// HelpLines renders the bindings for the help overlay.
func HelpLines() []string { return HelpLinesFor(nil) }

// KeyFor is the key a command is on after the prefix: a rebinding's, or
// else its default.
func KeyFor(bindings map[string]Command, cmd Command) string {
	if bindings != nil {
		if bound := keysFor(bindings, cmd); bound != "" {
			return bound
		}
	}
	for _, k := range Keys {
		if k.Command == cmd {
			return k.Key
		}
	}
	return ""
}

// HelpLinesFor renders the help for a particular set of bindings, so a user
// who rebound a key is shown the key they have rather than the default.
func HelpLinesFor(bindings map[string]Command) []string {
	lines := make([]string, 0, len(Keys)+len(Gestures)+2)
	lines = append(lines, "ctrl+b then:")
	for _, k := range Keys {
		key := k.Key
		if bindings != nil {
			if bound := keysFor(bindings, k.Command); bound != "" {
				key = bound
			}
		}
		lines = append(lines, "  "+pad(key, 5)+" "+k.Help)
	}
	lines = append(lines, "mouse:")
	for _, g := range Gestures {
		lines = append(lines, "  "+pad(g.Gesture, 9)+" "+g.Help)
	}
	return lines
}

// Gestures are what the mouse does, listed beside the keys because somebody
// looking for how to copy looks in the same place either way.
//
// They are not in Keys: that table maps a key to a command and is checked to
// be exactly that, and a drag is neither.
var Gestures = []struct {
	Gesture string
	Help    string
}{
	{"drag", "select text"},
	{"alt+drag", "select a block"},
	{"right", "menu for what is under it"},
	{"wheel", "scroll back"},
}

// keysFor is every key bound to a command, as the help shows them.
func keysFor(bindings map[string]Command, cmd Command) string {
	if cmd == CommandSelectTab {
		// The digits are read before the table: they carry which tab, and
		// nothing else a key can be bound to takes an argument. They are
		// still keys, and the list would be lying to leave them out.
		return "1-9"
	}
	var keys []string
	for name, bound := range bindings {
		if bound == cmd {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, " ")
}

// Bound lists every command that has a key, with the keys it has, for
// anything that prints the bindings.
func Bound(bindings map[string]Command) [][2]string {
	if bindings == nil {
		bindings = defaultBindings
	}
	var out [][2]string
	for _, name := range CommandNames() {
		cmd, ok := ParseCommand(name)
		if !ok {
			continue
		}
		out = append(out, [2]string{name, keysFor(bindings, cmd)})
	}
	return out
}

func pad(s string, width int) string {
	for len([]rune(s)) < width {
		s += " "
	}
	return s
}
