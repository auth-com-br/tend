// Package notify tells the person running tend that an agent needs them.
//
// The whole reason to run tend is that the agent needing you is usually not
// the one on screen — and often tend is not the window on screen either. A
// count on the status bar cannot reach somebody reading something else; a
// desktop notification can.
//
// It is the terminal's own notification, not a desktop library: the terminal
// is already in front of the user and already knows how to raise one, and
// asking it costs an escape sequence rather than a dependency. herdr does the
// same (`terminal_notify.rs`), and the sequences here are its.
package notify

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// Backend is the escape sequence a terminal understands.
type Backend uint8

const (
	// BackendNone means nothing here takes notifications.
	BackendNone Backend = iota
	// BackendOSC9 is OSC 9, which Ghostty, iTerm2 and WezTerm take.
	BackendOSC9
	// BackendKitty is OSC 99, which carries a title and a body apart.
	BackendKitty
)

// Detect works out what the terminal takes, from what it says it is.
func Detect(env func(string) string) Backend {
	switch env("TERM_PROGRAM") {
	case "ghostty":
		return BackendOSC9
	case "iTerm.app":
		return BackendOSC9
	case "WezTerm":
		return BackendOSC9
	}
	if env("KITTY_WINDOW_ID") != "" {
		return BackendKitty
	}
	switch term := env("TERM"); {
	case term == "xterm-ghostty":
		return BackendOSC9
	case term == "xterm-kitty":
		return BackendKitty
	case strings.Contains(term, "wezterm"):
		return BackendOSC9
	}
	return BackendNone
}

// Sequence builds the notification for a backend, or nothing when the
// terminal takes none.
//
// insideTmux wraps it in tmux's passthrough, because tend inside tmux is a
// program inside a program: without the wrapper the outer multiplexer eats the
// sequence and nobody is told.
func Sequence(backend Backend, title, body string, insideTmux bool) []byte {
	title, body = sanitize(title), sanitize(body)
	var seq []byte
	switch backend {
	case BackendOSC9:
		message := title
		if body != "" {
			message = title + ": " + body
		}
		seq = []byte("\x1b]9;" + message + "\x1b\\")
	case BackendKitty:
		if body != "" {
			seq = []byte("\x1b]99;i=1:d=0;" + title + "\x1b\\" +
				"\x1b]99;i=1:p=body;" + body + "\x1b\\")
		} else {
			seq = []byte("\x1b]99;;" + title + "\x1b\\")
		}
	default:
		return nil
	}
	if insideTmux {
		return wrapTmux(seq)
	}
	return seq
}

// Notifier raises notifications on a terminal.
type Notifier struct {
	backend Backend
	tmux    bool
}

// New looks at the environment and returns a notifier for it.
func New() *Notifier {
	return &Notifier{
		backend: Detect(os.Getenv),
		tmux:    os.Getenv("TMUX") != "",
	}
}

// Available reports whether the terminal takes notifications at all.
func (n *Notifier) Available() bool { return n != nil && n.backend != BackendNone }

// Sequence is what to write to the terminal for one notification, or nothing.
func (n *Notifier) Sequence(title, body string) []byte {
	if n == nil {
		return nil
	}
	return Sequence(n.backend, title, body, n.tmux)
}

// Split takes "title: body" apart, as herdr does, so a message written as one
// line arrives as a notification with a heading.
func Split(message string) (title, body string) {
	title, body, found := strings.Cut(message, ": ")
	if !found || title == "" || body == "" {
		return message, ""
	}
	return title, body
}

// sanitize removes what would end the sequence early or break the line: a
// notification's text comes from an agent's own output, and an agent that
// printed an escape could otherwise write anything to the terminal through it.
func sanitize(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch r {
		case 0x1b, 0x07, 0x9c:
			continue
		case '\n', '\r', '\t':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// wrapTmux is tmux's passthrough: the sequence inside DCS, with every escape
// doubled so tmux passes it on rather than reading it.
func wrapTmux(seq []byte) []byte {
	out := make([]byte, 0, len(seq)+16)
	out = append(out, "\x1bPtmux;"...)
	for _, b := range seq {
		if b == 0x1b {
			out = append(out, 0x1b)
		}
		out = append(out, b)
	}
	return append(out, "\x1b\\"...)
}

// System raises a desktop notification through whatever the machine has:
// notify-send on Linux, osascript on macOS. herdr calls this delivery
// "system" (`ToastDelivery::System`).
//
// It is a separate thing from the terminal's own notification. A terminal
// notification goes to the window the user is looking at through; a desktop
// one reaches them when that window is not on screen at all — which is the
// case this exists for.
func System(title, body string) error {
	title, body = sanitize(title), sanitize(body)
	tool, args := systemCommand(title, body)
	if tool == "" {
		// No notify-send: the notification server itself, over D-Bus.
		return systemOverDBus(title, body)
	}
	cmd := exec.Command(tool, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Run()
}

// ErrNoSystemNotifier means the machine has nothing to raise one with.
var ErrNoSystemNotifier = errors.New("notify: no desktop notifier on this machine")

// systemCommand picks the tool, or nothing.
func systemCommand(title, body string) (string, []string) {
	if path, err := exec.LookPath("notify-send"); err == nil {
		args := []string{"--app-name=tend", title}
		if body != "" {
			args = append(args, body)
		}
		return path, args
	}
	if path, err := exec.LookPath("osascript"); err == nil {
		script := "display notification " + quoteAppleScript(body) +
			" with title " + quoteAppleScript(title)
		return path, []string{"-e", script}
	}
	return "", nil
}

// quoteAppleScript makes a string literal AppleScript will take, with the two
// characters that would end it removed: the text comes from an agent.
func quoteAppleScript(s string) string {
	s = strings.NewReplacer("\"", "'", "\\", "/").Replace(s)
	return "\"" + s + "\""
}
