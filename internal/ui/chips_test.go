package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestALongLineOfChipsScrollsToTheChosenOne: a line of more projects than
// the panel is wide keeps the chosen one in view, marks what is off to either
// side, and a click on a chip in view finds that chip. If it regresses, the
// line runs past the panel and a project far along it cannot be chosen.
func TestALongLineOfChipsScrollsToTheChosenOne(t *testing.T) {
	names := []string{"all"}
	for i := 1; i <= 20; i++ {
		names = append(names, fmt.Sprintf("project-%02d", i))
	}
	for _, chosen := range []int{0, 7, 20} {
		rects, before, after := layoutChips(10, 70, 3, names, chosen)
		r := rects[chosen]
		if r.Cols == 0 || r.X < 10 || r.X+r.Cols > 70 {
			t.Fatalf("chosen %d out of view: %+v", chosen, r)
		}
		for i, r := range rects {
			if r.Cols > 0 && (r.X < 10 || r.X+r.Cols > 70) {
				t.Errorf("chosen %d: chip %d past the edge: %+v", chosen, i, r)
			}
		}
		if before != (chosen > 0 && rects[0].Cols == 0) || (chosen == 0 && !after) || (chosen == 20 && !before) {
			t.Errorf("chosen %d: marks before=%v after=%v", chosen, before, after)
		}
	}
	// a line that fits is laid out as it was, with nothing hidden
	rects, before, after := layoutChips(10, 70, 3, names[:3], 2)
	if before || after || rects[0].X != 10 || rects[1].X != 10+rects[0].Cols+1 {
		t.Errorf("a short line moved: %+v %v %v", rects, before, after)
	}

	v := &ErrorsView{Connected: true, Scopes: names, Scope: 20, Filters: []string{"unresolved", "resolved", "ignored"}}
	g := vt.NewGrid(100, 30, 0)
	Draw(g, Frame{Errors: v}, DefaultTheme())
	text := strings.Join(gridText(g), "\n")
	if !strings.Contains(text, "project-20") || !strings.Contains(text, "‹") {
		t.Errorf("the last project and the mark before it:\n%s", text)
	}
	geo := ErrorsLayout(v, 100, 30)
	last := geo.ScopeChips[20]
	if id, ok := ErrorsAt(v, 100, 30, last.X+1, last.Y); !ok || id != "scope:20" {
		t.Errorf("a click on the last project finds %q %v", id, ok)
	}
	if id, _ := ErrorsAt(v, 100, 30, geo.Box.X+geo.Box.Cols-1, last.Y); strings.HasPrefix(id, "scope:") {
		t.Errorf("a click past the chips finds %q", id)
	}
}
