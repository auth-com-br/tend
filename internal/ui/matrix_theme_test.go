package ui

import (
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheMatrixThemeIsGreenAndStillTellsTheStatesApart: "matrix" and "The
// Matrix" load the same palette, its accent is the film's green, and the
// states an agent can be in keep four different colours. If it regresses,
// the theme cannot be picked by the name people call it, or a waiting agent
// is as green as an idle one and goes unnoticed.
func TestTheMatrixThemeIsGreenAndStillTellsTheStatesApart(t *testing.T) {
	for _, name := range []string{"matrix", "The Matrix", "MATRIX"} {
		if got, ok := config.CanonicalTheme(name); !ok || got != "matrix" {
			t.Errorf("%q is %q %v, want matrix", name, got, ok)
		}
	}
	p, ok := PaletteNamed("matrix")
	if !ok {
		t.Fatal("no matrix palette")
	}
	if p.Accent != vt.RGBColor(0, 255, 65) {
		t.Errorf("accent = %#x, want #00FF41", uint32(p.Accent))
	}

	th := ThemeFrom(config.Theme{Name: "matrix"})
	states := map[string]vt.Color{
		"working": th.Working.FG, "blocked": th.Blocked.FG,
		"idle": th.Idle.FG, "done": th.Done.FG,
	}
	seen := map[vt.Color]string{}
	for state, color := range states {
		if other, dup := seen[color]; dup {
			t.Errorf("%s and %s share the colour %#x", state, other, uint32(color))
		}
		seen[color] = state
	}
}
