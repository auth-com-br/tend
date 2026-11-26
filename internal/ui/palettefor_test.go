package ui

import (
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestPaletteForResolvesAsThemeForDoes: tend-desktop colours its window from
// the palette the settings name, which must be the one the TUI is coloured
// from — the name, auto_switch's dark or light one, and the overrides. If it
// regresses, the two clients of one session wear different themes.
func TestPaletteForResolvesAsThemeForDoes(t *testing.T) {
	gruvbox, _ := PaletteNamed("gruvbox")
	latte, _ := PaletteNamed("catppuccin-latte")
	if p, ok := PaletteFor(config.Theme{Name: "gruvbox"}, false); !ok || p != gruvbox {
		t.Errorf("a named theme gave %+v", p)
	}
	if _, ok := PaletteFor(config.Theme{}, false); ok {
		t.Error("no theme at all gave a palette")
	}
	if p, _ := PaletteFor(config.Theme{AutoSwitch: true}, true); p != latte {
		t.Errorf("auto_switch on a light desktop gave %+v, want latte", p)
	}
	custom := config.Theme{Name: "gruvbox"}
	custom.Custom.All = map[string]string{"accent": "#010203"}
	if p, _ := PaletteFor(custom, false); p.Accent != vt.RGBColor(1, 2, 3) {
		t.Errorf("an override gave accent %v", p.Accent)
	}
}
