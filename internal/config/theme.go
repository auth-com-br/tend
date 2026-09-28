package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ThemeNames are the palettes tend ships, in the order a list shows them.
// They are herdr's (`config/theme.rs` THEME_NAMES), so a name that works in
// one works in the other — and, after them, tend's own, which herdr does not
// have: "matrix", which the owner asked for.
var ThemeNames = []string{
	"catppuccin",
	"catppuccin-latte",
	"terminal",
	"tokyo-night",
	"tokyo-night-day",
	"dracula",
	"nord",
	"gruvbox",
	"gruvbox-light",
	"one-dark",
	"one-light",
	"solarized",
	"solarized-light",
	"kanagawa",
	"kanagawa-lotus",
	"rose-pine",
	"rose-pine-dawn",
	"vesper",
	"matrix",
}

// themeAliases are the other spellings herdr accepts for the same palettes.
// People write the name they know the theme by, and "tokyonight" or
// "catppuccin-mocha" refusing to load would be a pedantry nobody asked for.
var themeAliases = map[string]string{
	"catppuccin-mocha": "catppuccin",
	"latte":            "catppuccin-latte",
	"light":            "catppuccin-latte",
	"tokyonight":       "tokyo-night",
	"tokyo-day":        "tokyo-night-day",
	"tokyonight-day":   "tokyo-night-day",
	"gruvbox-dark":     "gruvbox",
	"onedark":          "one-dark",
	"onelight":         "one-light",
	"solarized-dark":   "solarized",
	"lotus":            "kanagawa-lotus",
	"rosepine":         "rose-pine",
	"rosepine-dawn":    "rose-pine-dawn",
	"dawn":             "rose-pine-dawn",
	"the-matrix":       "matrix",
}

// CanonicalTheme is the name a theme is known by, from any spelling of it:
// case, spaces and underscores do not matter, as in herdr. It reports false
// for a name that is no theme.
func CanonicalTheme(name string) (string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.NewReplacer(" ", "-", "_", "-").Replace(n)
	if alias, ok := themeAliases[n]; ok {
		return alias, true
	}
	for _, known := range ThemeNames {
		if n == known {
			return known, true
		}
	}
	return "", false
}

// PaletteTokens are the colours a palette has, which [ui.theme.custom] may
// override one by one: herdr's (`CustomThemeColors`).
var PaletteTokens = []string{
	"accent", "panel_bg", "sidebar_bg", "active_row_bg", "selection_bg",
	"surface0", "surface1", "surface_dim", "overlay0", "overlay1",
	"text", "subtext0", "mauve", "green", "yellow", "red", "blue", "teal", "peach",
}

// ThemeColor is a colour in [ui.theme.custom]: a colour ParseColor takes,
// "#rgb", "rgb(r,g,b)", or "reset" (also default, none, transparent) for the
// terminal's own, as herdr's parse_color takes.
type ThemeColor struct {
	Color
	Reset bool
}

// ParseThemeColor reads a [ui.theme.custom] colour.
func ParseThemeColor(value string) (ThemeColor, bool) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "reset", "default", "none", "transparent":
		return ThemeColor{Reset: true}, true
	}
	if hex, ok := strings.CutPrefix(v, "#"); ok && len(hex) == 3 {
		v = "#" + string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if inner, ok := strings.CutPrefix(v, "rgb("); ok && strings.HasSuffix(inner, ")") {
		parts := strings.Split(strings.TrimSuffix(inner, ")"), ",")
		if len(parts) != 3 {
			return ThemeColor{}, false
		}
		var rgb [3]uint8
		for i, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || n < 0 || n > 255 {
				return ThemeColor{}, false
			}
			rgb[i] = uint8(n)
		}
		return ThemeColor{Color: Color{RGB: true, R: rgb[0], G: rgb[1], B: rgb[2]}}, true
	}
	c, ok := ParseColor(v)
	return ThemeColor{Color: c}, ok
}

// CustomColors is [ui.theme.custom]: token overrides for any appearance,
// and ones for only the light or only the dark, laid over them when
// auto_switch picks that one.
type CustomColors struct {
	All, Light, Dark map[string]string
}

// UnmarshalTOML reads the table, with its light and dark subtables.
func (c *CustomColors) UnmarshalTOML(v any) error {
	table, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("ui.theme.custom is a table of colours")
	}
	read := func(where string, m map[string]any) (map[string]string, error) {
		out := map[string]string{}
		for key, raw := range m {
			text, isText := raw.(string)
			if !isText {
				return nil, fmt.Errorf("%s.%s is a colour, written as text", where, key)
			}
			known := false
			for _, t := range PaletteTokens {
				known = known || t == key
			}
			if !known {
				return nil, fmt.Errorf("%s has no colour %q; the tokens are %s", where, key, strings.Join(PaletteTokens, ", "))
			}
			if _, ok := ParseThemeColor(text); !ok {
				return nil, fmt.Errorf("%s.%s: %q is not a colour; use #rrggbb, #rgb, rgb(r,g,b), a name, 0-255 or reset", where, key, text)
			}
			out[key] = text
		}
		return out, nil
	}
	all := map[string]any{}
	for key, raw := range table {
		switch key {
		case "light", "dark":
			sub, isTable := raw.(map[string]any)
			if !isTable {
				return fmt.Errorf("ui.theme.custom.%s is a table", key)
			}
			m, err := read("ui.theme.custom."+key, sub)
			if err != nil {
				return err
			}
			if key == "light" {
				c.Light = m
			} else {
				c.Dark = m
			}
		default:
			all[key] = raw
		}
	}
	m, err := read("ui.theme.custom", all)
	if err != nil {
		return err
	}
	c.All = m
	return nil
}
