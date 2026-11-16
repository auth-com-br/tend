package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/auth-com-br/tend/internal/proto"
)

// The kitty keyboard protocol, given to the terminal outside (issue #66):
// herdr's direct mode (terminal_modes.rs, set_direct_host_keyboard_protocol).
// A pane's program asks its terminal — tend's — for the protocol; the server
// answers and keeps the flags (vt.Modes.KittyKeyboard). While that pane is
// focused the client sets the same flags on the terminal it runs in, which
// then sends keys in the form the program asked for — Shift+Enter as
// CSI 13;2u, which Claude Code reads as a new line where a bare \r sends
// the message — and the client passes them on as they came. What tend reads
// for itself, it reads in the legacy form (ui.LegacyKeys, ui.Input).

// hostKeyboard is what the client has set on the terminal outside: one
// entry of its flag stack, pushed by tend and popped by it, so the flags the
// person's own shell keeps there are never touched; and modifyOtherKeys.
var hostKeyboard struct {
	mu        sync.Mutex
	supported bool
	flags     uint16
	mok       uint8
}

// hostKeyboardSupported is whether the terminal has the protocol, asked of
// the environment as graphicsSupported asks it: the query answers on the
// input stream, which is the keyboard going to a pane. TEND_KITTY_KEYBOARD
// says on or off.
func hostKeyboardSupported(env func(string) string) bool {
	switch env("TEND_KITTY_KEYBOARD") {
	case "off", "0", "no":
		return false
	case "on", "1", "yes":
		return true
	}
	if env("KITTY_WINDOW_ID") != "" {
		return true
	}
	switch env("TERM_PROGRAM") {
	case "ghostty", "WezTerm":
		return true
	}
	term := env("TERM")
	return term == "xterm-kitty" || term == "xterm-ghostty" || strings.Contains(term, "wezterm")
}

func initHostKeyboard() {
	hostKeyboard.mu.Lock()
	hostKeyboard.supported = hostKeyboardSupported(os.Getenv)
	hostKeyboard.mu.Unlock()
}

// hostKeyboardOn is whether keys may be arriving in the protocol's form.
func hostKeyboardOn() bool {
	hostKeyboard.mu.Lock()
	defer hostKeyboard.mu.Unlock()
	return hostKeyboard.flags != 0 || hostKeyboard.mok != 0
}

// setHostKeyboard gives the terminal flags and a modifyOtherKeys level,
// writing only what changed.
func setHostKeyboard(w io.Writer, flags uint16, mok uint8) {
	hostKeyboard.mu.Lock()
	defer hostKeyboard.mu.Unlock()
	if !hostKeyboard.supported || (flags == hostKeyboard.flags && mok == hostKeyboard.mok) {
		return
	}
	var b strings.Builder
	if flags != hostKeyboard.flags {
		if hostKeyboard.flags != 0 {
			b.WriteString("\x1b[<1u")
		}
		if flags != 0 {
			fmt.Fprintf(&b, "\x1b[>%du", flags)
		}
	}
	if mok != hostKeyboard.mok {
		fmt.Fprintf(&b, "\x1b[>4;%dm", mok)
	}
	_, _ = io.WriteString(w, b.String())
	hostKeyboard.flags, hostKeyboard.mok = flags, mok
}

// resetHostKeyboard takes back what tend set, on the way out: a terminal
// left in the protocol would send the shell after it escape codes for keys.
func resetHostKeyboard(w io.Writer) { setHostKeyboard(w, 0, 0) }

// focusedKeyboardLocked is the protocol the focused pane's program asked
// for. The caller holds t.mu.
func (t *tui) focusedKeyboardLocked() (uint16, uint8) {
	if t.offline {
		return 0, 0
	}
	for _, p := range t.snap.Panes {
		if p.ID == t.focus {
			return p.KittyKeyboard, p.ModifyOtherKeys
		}
	}
	return 0, 0
}

var _ = proto.FeatureKittyKeyboard
