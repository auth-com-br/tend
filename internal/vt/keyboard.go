package vt

// The kitty keyboard protocol's flags, as a program sets them: herdr's
// KittyKeyboardTracker (pane/kitty_keyboard.rs), kept here with the rest of
// the screen's modes.

// kittyStackLimit bounds the stack, as kitty's own does: a program pushing
// without popping must not grow it for ever.
const kittyStackLimit = 16

// kittyKeyboard handles CSI > f u (push f), CSI < n u (pop n) and
// CSI = f ; m u (set, or, and-not by m).
func (s *Screen) kittyKeyboard(marker byte, ps *Params) {
	flags := uint16(max(int(ps.Get(0)), 0))
	switch marker {
	case '>':
		if len(s.kittyStack) >= kittyStackLimit {
			s.kittyStack = s.kittyStack[1:]
		}
		s.kittyStack = append(s.kittyStack, s.modes.KittyKeyboard)
		s.modes.KittyKeyboard = flags
	case '<':
		n := max(int(ps.Get(0)), 1)
		for i := 0; i < n; i++ {
			if len(s.kittyStack) == 0 {
				s.modes.KittyKeyboard = 0
				break
			}
			s.modes.KittyKeyboard = s.kittyStack[len(s.kittyStack)-1]
			s.kittyStack = s.kittyStack[:len(s.kittyStack)-1]
		}
	case '=':
		switch ps.Get(1) {
		case 2:
			s.modes.KittyKeyboard |= flags
		case 3:
			s.modes.KittyKeyboard &^= flags
		default:
			s.modes.KittyKeyboard = flags
		}
	}
}
