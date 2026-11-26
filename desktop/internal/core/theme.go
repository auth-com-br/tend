package core

import (
	"fmt"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/vt"
)

// The window wears the theme the settings name ([ui] theme, auto_switch and
// [ui.theme.custom]), resolved as the TUI resolves it (ui.PaletteFor), so
// the two clients of a session look alike. The TUI's default with no theme
// is the terminal's own colours; a window has no terminal, and takes
// catppuccin, tend's default family.

// Theme is the window's colours as QML reads them, named by herdr's
// palette tokens where the window uses them for the same things.
type Theme struct {
	Base     string `json:"base"`
	Mantle   string `json:"mantle"`
	Crust    string `json:"crust"`
	Surface0 string `json:"surface0"`
	Surface1 string `json:"surface1"`
	Overlay0 string `json:"overlay0"`
	Subtext  string `json:"subtext"`
	Text     string `json:"text"`
	Accent   string `json:"accent"`
	Green    string `json:"green"`
	Yellow   string `json:"yellow"`
	Red      string `json:"red"`
	Teal     string `json:"teal"`
	Peach    string `json:"peach"`
	Light    bool   `json:"light"`
}

// paletteFor is the palette for the settings on a light or dark desktop,
// catppuccin's when they name none.
func paletteFor(c config.Theme, light bool) ui.Palette {
	if p, ok := ui.PaletteFor(c, light); ok {
		return p
	}
	name := "catppuccin"
	if light && c.AutoSwitch {
		name = "catppuccin-latte"
	}
	p, _ := ui.PaletteNamed(name)
	return p
}

// rgbOf is a palette colour as 0xRRGGBB, def for one that defers to the
// terminal (no terminal here) or is indexed.
func rgbOf(c vt.Color, def uint32) uint32 {
	if !c.IsRGB() {
		return def
	}
	r, g, b := c.RGB()
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

func hex(c uint32) string { return fmt.Sprintf("#%06x", c&0xffffff) }

// darker is c moved toward black by f (0..1), for the crust under the
// mantle, which herdr's palettes do not name.
func darker(c uint32, f float64) uint32 {
	ch := func(shift uint) uint32 {
		v := float64((c >> shift) & 0xff)
		return uint32(v*(1-f)) << shift
	}
	return ch(16) | ch(8) | ch(0)
}

// coloursFrom is the terminal's palette from a theme's: its text on its
// base, and the sixteen ANSI colours from the tokens that mean them,
// catppuccin's own mapping (surface1 black, subtext white, overlay0 bright
// black).
func coloursFrom(p ui.Palette) Colours {
	text := rgbOf(p.Text, 0xcdd6f4)
	base := rgbOf(p.ActiveRowBG, 0x1e1e2e)
	red, green, yellow := rgbOf(p.Red, 0xf38ba8), rgbOf(p.Green, 0xa6e3a1), rgbOf(p.Yellow, 0xf9e2af)
	blue, magenta, cyan := rgbOf(p.Blue, 0x89b4fa), rgbOf(p.Mauve, 0xf5c2e7), rgbOf(p.Teal, 0x94e2d5)
	return Colours{
		FG: text,
		BG: base,
		ANSI: [16]uint32{
			rgbOf(p.Surface1, 0x45475a), red, green, yellow, blue, magenta, cyan, rgbOf(p.Subtext0, 0xbac2de),
			rgbOf(p.Overlay0, 0x585b70), red, green, yellow, blue, magenta, cyan, text,
		},
	}
}

// themeFrom is the window's colours from a theme's palette.
func themeFrom(p ui.Palette, light bool) Theme {
	mantle := rgbOf(p.PanelBG, 0x181825)
	return Theme{
		Base:     hex(rgbOf(p.ActiveRowBG, 0x1e1e2e)),
		Mantle:   hex(mantle),
		Crust:    hex(darker(mantle, 0.25)),
		Surface0: hex(rgbOf(p.Surface0, 0x313244)),
		Surface1: hex(rgbOf(p.Surface1, 0x45475a)),
		Overlay0: hex(rgbOf(p.Overlay0, 0x6c7086)),
		Subtext:  hex(rgbOf(p.Subtext0, 0xa6adc8)),
		Text:     hex(rgbOf(p.Text, 0xcdd6f4)),
		Accent:   hex(rgbOf(p.Accent, 0x89b4fa)),
		Green:    hex(rgbOf(p.Green, 0xa6e3a1)),
		Yellow:   hex(rgbOf(p.Yellow, 0xf9e2af)),
		Red:      hex(rgbOf(p.Red, 0xf38ba8)),
		Teal:     hex(rgbOf(p.Teal, 0x94e2d5)),
		Peach:    hex(rgbOf(p.Peach, 0xfab387)),
		Light:    light,
	}
}

// SetAppearance is the desktop being light or dark, which a theme with
// auto_switch follows. Every pane is painted again in the new colours.
func (s *Session) SetAppearance(light bool) {
	s.mu.Lock()
	if light == s.light && s.themeReady {
		s.mu.Unlock()
		return
	}
	s.light, s.themeReady = light, true
	s.applyThemeLocked()
	panes := make([]uint64, 0, len(s.panes))
	for id := range s.panes {
		panes = append(panes, id)
	}
	s.mu.Unlock()
	for _, p := range panes {
		s.paneChanged(p)
	}
	s.stateChanged()
}

// applyThemeLocked resolves the theme for the settings and the desktop's
// appearance. The caller holds the lock.
func (s *Session) applyThemeLocked() {
	p := paletteFor(s.ui.Theme, s.light)
	s.colours = coloursFrom(p)
	s.theme = themeFrom(p, s.light)
}
