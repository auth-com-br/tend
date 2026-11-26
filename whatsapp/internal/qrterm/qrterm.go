// Package qrterm draws a QR code in a terminal, two rows of modules to a line
// with the half-block characters, so the code is square in a terminal's
// cells (about twice as tall as they are wide) and fits a small window.
//
// It is drawn dark on light whatever the terminal's colours: a phone's
// camera reads a code with a light quiet zone around it, and a dark-themed
// terminal drawing the code inverted is one it does not read.
package qrterm

import (
	"strings"

	"rsc.io/qr"
)

// quiet is the margin of light modules around the code the spec asks for.
const quiet = 2

// Render is text as a QR code, as lines for a terminal.
func Render(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	dark := func(x, y int) bool {
		x, y = x-quiet, y-quiet
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return false
		}
		return code.Black(x, y)
	}
	size := code.Size + 2*quiet
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		// Black on white, set for the line and reset at its end.
		b.WriteString("\x1b[30;47m")
		for x := 0; x < size; x++ {
			top, bottom := dark(x, y), dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}
