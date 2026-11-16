package ui

// Keys in the kitty keyboard protocol's form, as a terminal outside sends
// them once the client has set a pane's flags on it (issue #66): CSI code
// ; mods u. The pane asked for that form and gets the bytes as they came;
// what tend reads for itself — the prefix, and the key after it — is read
// in the legacy form its key machine knows. With the protocol on, ctrl+b
// arrives as CSI 98;5u, not as 0x02, and would otherwise never arm the
// prefix.

// kittyKey reads a complete CSI … u key at the start of data: the legacy
// bytes it stands for, and how long the sequence is. ok is false for
// anything else, and for a key with no legacy form (a release event, a
// modifier alone), which then goes to the pane untouched.
func kittyKey(data []byte) (legacy []byte, n int, ok bool) {
	if len(data) < 4 || data[0] != 0x1b || data[1] != '[' {
		return nil, 0, false
	}
	i := 2
	num := func() (int, bool) {
		start, v := i, 0
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			v = v*10 + int(data[i]-'0')
			i++
			if v > 0x10ffff {
				return 0, false
			}
		}
		return v, i > start
	}
	code, okCode := num()
	if !okCode {
		return nil, 0, false
	}
	// :shifted:base alternates (flag 4) after the code.
	for i < len(data) && data[i] == ':' {
		i++
		num()
	}
	mods, event := 1, 1
	if i < len(data) && data[i] == ';' {
		i++
		if m, ok := num(); ok {
			mods = m
		}
		if i < len(data) && data[i] == ':' {
			i++
			if e, ok := num(); ok {
				event = e
			}
		}
		// ;text-as-codepoints (flag 16) — skipped.
		for i < len(data) && data[i] == ';' {
			i++
			for i < len(data) && (data[i] >= '0' && data[i] <= '9' || data[i] == ':') {
				i++
			}
		}
	}
	if i >= len(data) || data[i] != 'u' {
		return nil, 0, false
	}
	n = i + 1
	if event == 3 {
		return nil, n, false // a release: nothing to type
	}
	m := mods - 1
	shift, alt, ctrl := m&1 != 0, m&2 != 0, m&4 != 0
	var b []byte
	switch {
	case code == 27:
		b = []byte{0x1b}
	case code == 13:
		b = []byte{'\r'}
	case code == 9:
		if shift {
			b = []byte("\x1b[Z")
		} else {
			b = []byte{'\t'}
		}
	case code == 127:
		b = []byte{0x7f}
	case ctrl && code >= 'a' && code <= 'z':
		b = []byte{byte(code - 'a' + 1)}
	case ctrl && code >= '@' && code <= '_':
		b = []byte{byte(code - '@')}
	case ctrl && code == ' ':
		b = []byte{0}
	case code >= 0x20 && code < 0x7f:
		c := byte(code)
		if shift && c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b = []byte{c}
	default:
		return nil, n, false
	}
	if alt {
		b = append([]byte{0x1b}, b...)
	}
	return b, n, true
}
