package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestThePhonePanelSaysOffOrWhereThePhoneReachesIt: off offers to turn it
// on; on lists the addresses, each a click away from being copied, and
// offers off. If it regresses, the switch says the wrong thing or an
// address cannot be copied.
func TestThePhonePanelSaysOffOrWhereThePhoneReachesIt(t *testing.T) {
	off := &PhoneView{Loaded: true}
	g := PhoneLayout(off, 120, 40)
	if g.Buttons[0].Label != "[ Turn on ]" || len(g.URLs) != 0 {
		t.Fatalf("off: %+v", g)
	}
	on := &PhoneView{Loaded: true, On: true, Running: true, User: "ana",
		URLs: []string{"http://192.0.2.7:7777/?t=abc", "http://10.0.0.2:7777/?t=abc"}}
	g = PhoneLayout(on, 120, 40)
	if g.Buttons[0].Label != "[ Turn off ]" || g.Buttons[1].ID != PhoneCopy || len(g.URLs) != 2 {
		t.Fatalf("on: %+v", g)
	}
	if id, ok := PhoneAt(on, 120, 40, g.URLs[1].X+2, g.URLs[1].Y); !ok || id != "url:1" {
		t.Fatalf("a click on the second address: %q %v", id, ok)
	}
	if id, _ := PhoneAt(on, 120, 40, g.Buttons[0].X+1, g.Buttons[0].Y); id != PhoneToggle {
		t.Fatalf("a click on the switch: %q", id)
	}
	grid := vt.NewGrid(120, 40, 0)
	drawPhone(grid, on, DefaultTheme())
	text := strings.Join(gridText(grid), "\n")
	for _, want := range []string{"phone view", "on — serving", "http://192.0.2.7:7777/?t=abc", "sign in as ana", "[ Turn off ]"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q not drawn", want)
		}
	}
}
