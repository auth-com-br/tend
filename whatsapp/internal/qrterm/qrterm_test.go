package qrterm

import (
	"strings"
	"testing"

	"rsc.io/qr"
)

// TestTheCodeIsDrawnSquareWithAQuietZone: two modules to a line, the margin
// light on every side, and every line the same width. If it regresses, the
// phone's camera does not read the code `tend whatsapp link` draws.
func TestTheCodeIsDrawnSquareWithAQuietZone(t *testing.T) {
	text := "2@AbCdEf0123456789,abcdefghijklmnop,qrstuvwxyz=="
	art, err := Render(text)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := qr.Encode(text, qr.L)
	size := code.Size + 2*quiet
	lines := strings.Split(strings.TrimRight(art, "\n"), "\n")
	if len(lines) != (size+1)/2 {
		t.Fatalf("%d lines for %d modules", len(lines), size)
	}
	for i, line := range lines {
		cells := strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[30;47m"), "\x1b[0m")
		if n := len([]rune(cells)); n != size {
			t.Fatalf("line %d is %d cells, want %d", i, n, size)
		}
		if i == 0 && strings.Trim(cells, " ") != "" {
			t.Errorf("the top margin has dark modules: %q", cells)
		}
		if r := []rune(cells); r[0] != ' ' || r[size-1] != ' ' {
			t.Errorf("line %d has no side margin", i)
		}
	}
}
