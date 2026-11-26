package core

import (
	"strconv"

	"github.com/auth-com-br/tend/internal/vt"
)

// Qt's key codes and modifier bits (Qt::Key, Qt::KeyboardModifier), the
// values a QKeyEvent carries. They are written here rather than read from the
// bindings so this package, and its tests, do not need Qt.
const (
	QtKeyEscape    = 0x01000000
	QtKeyTab       = 0x01000001
	QtKeyBacktab   = 0x01000002
	QtKeyBackspace = 0x01000003
	QtKeyReturn    = 0x01000004
	QtKeyEnter     = 0x01000005
	QtKeyInsert    = 0x01000006
	QtKeyDelete    = 0x01000007
	QtKeyHome      = 0x01000010
	QtKeyEnd       = 0x01000011
	QtKeyLeft      = 0x01000012
	QtKeyUp        = 0x01000013
	QtKeyRight     = 0x01000014
	QtKeyDown      = 0x01000015
	QtKeyPageUp    = 0x01000016
	QtKeyPageDown  = 0x01000017
	QtKeyF1        = 0x01000030
	QtKeyF12       = 0x0100003b

	QtShift   = 0x02000000
	QtControl = 0x04000000
	QtAlt     = 0x08000000
)

// EncodeQtKey is the bytes an xterm sends for a key press, as Qt reports it:
// the key code, the modifiers held, and the text the key produced.
//
// The TUI never does this: there the outer terminal encodes the key and the
// client forwards the bytes. A window has no terminal in front of it, so the
// desktop has to be that terminal, and the encoding is xterm's because it is
// what every program in a pane expects. ok is false for a key that sends
// nothing on its own (a bare modifier, a key with no text).
//
// A program that asked for the kitty keyboard protocol (vt.Modes.
// KittyKeyboard) or xterm's modifyOtherKeys gets keys in that form: the
// TUI hands those requests to the terminal it runs in, which encodes them;
// the window is that terminal. It is how Shift+Enter reaches Claude Code as
// a new line (issue #66) rather than as Enter.
func EncodeQtKey(key, mods int, text string, m vt.Modes) (out []byte, ok bool) {
	if m.KittyKeyboard != 0 {
		if b, ok := encodeKitty(key, mods, text, m); ok {
			return b, true
		}
	} else if m.ModifyOtherKeys >= 2 {
		if b, ok := encodeModifyOtherKeys(key, mods, text); ok {
			return b, true
		}
	}
	return encodeXterm(key, mods, text, m)
}

// Kitty keyboard flags (the protocol's progressive enhancements).
const (
	kittyDisambiguate = 1
	kittyAllAsEscapes = 8
)

// kittyCodes are the functional keys kitty sends as CSI code u, by the
// legacy byte they would otherwise be.
var kittyCodes = map[int]int{
	QtKeyEscape:    27,
	QtKeyReturn:    13,
	QtKeyEnter:     13,
	QtKeyTab:       9,
	QtKeyBacktab:   9,
	QtKeyBackspace: 127,
}

// modifierParam is the protocol's modifier number: 1, plus 1 for shift, 2
// for alt, 4 for ctrl — the same in kitty's and xterm's encodings.
func modifierParam(mods int) int {
	p := 1
	if mods&QtShift != 0 {
		p++
	}
	if mods&QtAlt != 0 {
		p += 2
	}
	if mods&QtControl != 0 {
		p += 4
	}
	return p
}

func csiU(code, param int) []byte {
	if param > 1 {
		return []byte("\x1b[" + strconv.Itoa(code) + ";" + strconv.Itoa(param) + "u")
	}
	return []byte("\x1b[" + strconv.Itoa(code) + "u")
}

// encodeKitty is kitty's protocol for the keys whose legacy bytes are
// ambiguous; the rest (arrows, function keys) fall to the legacy encoding,
// which the protocol keeps for them. ok is false for a key to send as
// legacy.
func encodeKitty(key, mods int, text string, m vt.Modes) ([]byte, bool) {
	all := m.KittyKeyboard&kittyAllAsEscapes != 0
	if m.KittyKeyboard&(kittyDisambiguate|kittyAllAsEscapes) == 0 {
		return nil, false
	}
	param := modifierParam(mods)
	if key == QtKeyBacktab {
		param = modifierParam(mods | QtShift)
	}
	if code, is := kittyCodes[key]; is {
		switch {
		case all:
			return csiU(code, param), true
		case key == QtKeyEscape:
			// Escape alone is the ambiguity the protocol exists to end.
			return csiU(code, param), true
		case param > 1:
			// Enter, Tab and Backspace stay legacy alone, and are told
			// apart with a modifier: Shift+Enter is CSI 13;2u.
			return csiU(code, param), true
		}
		return nil, false
	}
	if key >= 0x01000000 {
		return nil, false // arrows, function keys: legacy with modifiers
	}
	// A key that types: as text alone or with shift, unless every key is
	// to be an escape; with ctrl or alt, as its unshifted code point.
	plain := mods&(QtControl|QtAlt) == 0
	if plain && !all {
		return nil, false
	}
	code := key
	if code >= 'A' && code <= 'Z' {
		code += 'a' - 'A'
	}
	// With every key as an escape, the protocol says the key, and the text
	// it types is the program's to take from the code and the shift.
	return csiU(code, param), true
}

// encodeModifyOtherKeys is xterm's modifyOtherKeys at level 2: a key with
// a modifier that its legacy bytes would lose is CSI 27 ; mod ; code ~.
func encodeModifyOtherKeys(key, mods int, text string) ([]byte, bool) {
	param := modifierParam(mods)
	if param == 1 {
		return nil, false
	}
	code, is := kittyCodes[key]
	if !is {
		if key >= 0x01000000 {
			return nil, false
		}
		if mods&(QtControl|QtAlt) == 0 {
			return nil, false // shift alone types its character
		}
		code = key
		if code >= 'A' && code <= 'Z' && mods&QtShift == 0 {
			code += 'a' - 'A'
		}
	}
	return []byte("\x1b[27;" + strconv.Itoa(param) + ";" + strconv.Itoa(code) + "~"), true
}

// encodeXterm is the legacy encoding, every key as an xterm sends it.
func encodeXterm(key, mods int, text string, m vt.Modes) (out []byte, ok bool) {
	shift, alt, ctrl := mods&QtShift != 0, mods&QtAlt != 0, mods&QtControl != 0
	// xterm's modifier parameter: 1, plus 1 for shift, 2 for alt, 4 for ctrl.
	param := 1
	if shift {
		param++
	}
	if alt {
		param += 2
	}
	if ctrl {
		param += 4
	}

	escaped := func(b []byte) []byte {
		if alt {
			return append([]byte{0x1b}, b...)
		}
		return b
	}

	switch key {
	case QtKeyReturn, QtKeyEnter:
		return escaped([]byte{'\r'}), true
	case QtKeyTab:
		return escaped([]byte{'\t'}), true
	case QtKeyBacktab:
		return []byte("\x1b[Z"), true
	case QtKeyBackspace:
		if ctrl {
			return escaped([]byte{0x08}), true
		}
		return escaped([]byte{0x7f}), true
	case QtKeyEscape:
		return escaped([]byte{0x1b}), true
	}

	if final, is := cursorFinal[key]; is {
		if param > 1 {
			return []byte("\x1b[1;" + strconv.Itoa(param) + string(final)), true
		}
		if m.ApplicationCur {
			return []byte{0x1b, 'O', final}, true
		}
		return []byte{0x1b, '[', final}, true
	}
	if n, is := tildeKeys[key]; is {
		if param > 1 {
			return []byte("\x1b[" + strconv.Itoa(n) + ";" + strconv.Itoa(param) + "~"), true
		}
		return []byte("\x1b[" + strconv.Itoa(n) + "~"), true
	}
	if key >= QtKeyF1 && key <= QtKeyF1+3 {
		final := byte('P' + key - QtKeyF1)
		if param > 1 {
			return []byte("\x1b[1;" + strconv.Itoa(param) + string(final)), true
		}
		return []byte{0x1b, 'O', final}, true
	}

	// Ctrl with a letter or one of the punctuation keys that has a control
	// character. Qt reports ctrl+c with the text "\x03" on some platforms
	// and "" on others, so the key code is what is trusted, not the text.
	if ctrl && key < 0x01000000 {
		r := rune(key)
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if b, is := vt.EncodeKey("ctrl+"+string(r), m); is {
			return escaped(b), true
		}
	}

	if text == "" {
		return nil, false
	}
	return escaped([]byte(text)), true
}

// cursorFinal is the final letter of the cursor keys, which DECCKM moves
// between CSI and SS3.
var cursorFinal = map[int]byte{
	QtKeyUp:    'A',
	QtKeyDown:  'B',
	QtKeyRight: 'C',
	QtKeyLeft:  'D',
	QtKeyHome:  'H',
	QtKeyEnd:   'F',
}

// tildeKeys are the keys sent as CSI n ~, and F5 to F12 among them.
var tildeKeys = map[int]int{
	QtKeyInsert:   2,
	QtKeyDelete:   3,
	QtKeyPageUp:   5,
	QtKeyPageDown: 6,
	QtKeyF1 + 4:   15,
	QtKeyF1 + 5:   17,
	QtKeyF1 + 6:   18,
	QtKeyF1 + 7:   19,
	QtKeyF1 + 8:   20,
	QtKeyF1 + 9:   21,
	QtKeyF1 + 10:  23,
	QtKeyF12:      24,
}
