package ui

import (
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/vt"
)

// Palette is one of the named themes: herdr's palette tokens (`app/state.rs`
// Palette), with the same names and the same values, so a theme picked here
// looks the way it does there.
//
// The bars, panels and the sidebar's entry in view use panel_bg, accent and
// active_row_bg; inactive tabs surface0; the navigation cursor selection_bg;
// the sidebar sidebar_bg. mauve, blue, peach, surface1 and subtext0 are
// carried for [ui.theme.custom] and read by nothing yet.
type Palette struct {
	Accent      vt.Color
	PanelBG     vt.Color
	SidebarBG   vt.Color
	ActiveRowBG vt.Color
	SelectionBG vt.Color
	Surface0    vt.Color
	Surface1    vt.Color
	SurfaceDim  vt.Color
	Overlay0    vt.Color
	Overlay1    vt.Color
	Text        vt.Color
	Subtext0    vt.Color
	Mauve       vt.Color
	Green       vt.Color
	Yellow      vt.Color
	Red         vt.Color
	Blue        vt.Color
	Teal        vt.Color
	Peach       vt.Color
}

// palettes are the themes by their canonical name (config.ThemeNames).
// They were transcribed from herdr's source by a script rather than by hand,
// and a test checks that every name has one.
var palettes = map[string]Palette{
	"catppuccin": {
		Accent:      vt.RGBColor(137, 180, 250),
		PanelBG:     vt.RGBColor(24, 24, 37),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(30, 30, 46),
		SelectionBG: vt.RGBColor(49, 50, 68),
		Surface0:    vt.RGBColor(49, 50, 68),
		Surface1:    vt.RGBColor(69, 71, 90),
		SurfaceDim:  vt.RGBColor(30, 30, 46),
		Overlay0:    vt.RGBColor(108, 112, 134),
		Overlay1:    vt.RGBColor(127, 132, 156),
		Text:        vt.RGBColor(205, 214, 244),
		Subtext0:    vt.RGBColor(166, 173, 200),
		Mauve:       vt.RGBColor(203, 166, 247),
		Green:       vt.RGBColor(166, 227, 161),
		Yellow:      vt.RGBColor(249, 226, 175),
		Red:         vt.RGBColor(243, 139, 168),
		Blue:        vt.RGBColor(137, 180, 250),
		Teal:        vt.RGBColor(148, 226, 213),
		Peach:       vt.RGBColor(250, 179, 135),
	},
	"catppuccin-latte": {
		Accent:      vt.RGBColor(30, 102, 245),
		PanelBG:     vt.RGBColor(239, 241, 245),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(230, 233, 239),
		SelectionBG: vt.RGBColor(189, 208, 245),
		Surface0:    vt.RGBColor(204, 208, 218),
		Surface1:    vt.RGBColor(188, 192, 204),
		SurfaceDim:  vt.RGBColor(230, 233, 239),
		Overlay0:    vt.RGBColor(156, 160, 176),
		Overlay1:    vt.RGBColor(140, 143, 161),
		Text:        vt.RGBColor(76, 79, 105),
		Subtext0:    vt.RGBColor(108, 111, 133),
		Mauve:       vt.RGBColor(136, 57, 239),
		Green:       vt.RGBColor(64, 160, 43),
		Yellow:      vt.RGBColor(223, 142, 29),
		Red:         vt.RGBColor(210, 15, 57),
		Blue:        vt.RGBColor(30, 102, 245),
		Teal:        vt.RGBColor(23, 146, 153),
		Peach:       vt.RGBColor(254, 100, 11),
	},
	"terminal": {
		Accent:      vt.IndexedColor(4),
		PanelBG:     vt.DefaultColor,
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.IndexedColor(8),
		SelectionBG: vt.DefaultColor,
		Surface0:    vt.DefaultColor,
		Surface1:    vt.IndexedColor(8),
		SurfaceDim:  vt.IndexedColor(8),
		Overlay0:    vt.IndexedColor(7),
		Overlay1:    vt.IndexedColor(15),
		Text:        vt.DefaultColor,
		Subtext0:    vt.IndexedColor(7),
		Mauve:       vt.IndexedColor(7),
		Green:       vt.IndexedColor(2),
		Yellow:      vt.IndexedColor(3),
		Red:         vt.IndexedColor(9),
		Blue:        vt.IndexedColor(4),
		Teal:        vt.IndexedColor(6),
		Peach:       vt.IndexedColor(3),
	},
	"tokyo-night": {
		Accent:      vt.RGBColor(122, 162, 247),
		PanelBG:     vt.RGBColor(26, 27, 38),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(35, 38, 54),
		SelectionBG: vt.RGBColor(45, 54, 80),
		Surface0:    vt.RGBColor(36, 40, 59),
		Surface1:    vt.RGBColor(65, 72, 104),
		SurfaceDim:  vt.RGBColor(26, 27, 38),
		Overlay0:    vt.RGBColor(86, 95, 137),
		Overlay1:    vt.RGBColor(105, 113, 150),
		Text:        vt.RGBColor(192, 202, 245),
		Subtext0:    vt.RGBColor(169, 177, 214),
		Mauve:       vt.RGBColor(187, 154, 247),
		Green:       vt.RGBColor(158, 206, 106),
		Yellow:      vt.RGBColor(224, 175, 104),
		Red:         vt.RGBColor(247, 118, 142),
		Blue:        vt.RGBColor(122, 162, 247),
		Teal:        vt.RGBColor(125, 207, 255),
		Peach:       vt.RGBColor(255, 158, 100),
	},
	"tokyo-night-day": {
		Accent:      vt.RGBColor(46, 125, 233),
		PanelBG:     vt.RGBColor(225, 226, 231),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(210, 211, 218),
		SelectionBG: vt.RGBColor(182, 202, 231),
		Surface0:    vt.RGBColor(196, 200, 218),
		Surface1:    vt.RGBColor(168, 174, 203),
		SurfaceDim:  vt.RGBColor(210, 211, 218),
		Overlay0:    vt.RGBColor(137, 144, 179),
		Overlay1:    vt.RGBColor(104, 112, 154),
		Text:        vt.RGBColor(55, 96, 191),
		Subtext0:    vt.RGBColor(97, 114, 176),
		Mauve:       vt.RGBColor(120, 71, 189),
		Green:       vt.RGBColor(88, 117, 57),
		Yellow:      vt.RGBColor(140, 108, 62),
		Red:         vt.RGBColor(245, 42, 101),
		Blue:        vt.RGBColor(46, 125, 233),
		Teal:        vt.RGBColor(17, 140, 116),
		Peach:       vt.RGBColor(177, 92, 0),
	},
	"dracula": {
		Accent:      vt.RGBColor(189, 147, 249),
		PanelBG:     vt.RGBColor(40, 42, 54),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(55, 60, 82),
		SelectionBG: vt.RGBColor(70, 63, 93),
		Surface0:    vt.RGBColor(68, 71, 90),
		Surface1:    vt.RGBColor(98, 114, 164),
		SurfaceDim:  vt.RGBColor(40, 42, 54),
		Overlay0:    vt.RGBColor(98, 114, 164),
		Overlay1:    vt.RGBColor(130, 140, 180),
		Text:        vt.RGBColor(248, 248, 242),
		Subtext0:    vt.RGBColor(210, 210, 220),
		Mauve:       vt.RGBColor(255, 121, 198),
		Green:       vt.RGBColor(80, 250, 123),
		Yellow:      vt.RGBColor(241, 250, 140),
		Red:         vt.RGBColor(255, 85, 85),
		Blue:        vt.RGBColor(139, 233, 253),
		Teal:        vt.RGBColor(139, 233, 253),
		Peach:       vt.RGBColor(255, 184, 108),
	},
	"nord": {
		Accent:      vt.RGBColor(136, 192, 208),
		PanelBG:     vt.RGBColor(46, 52, 64),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(67, 76, 94),
		SelectionBG: vt.RGBColor(64, 80, 93),
		Surface0:    vt.RGBColor(59, 66, 82),
		Surface1:    vt.RGBColor(67, 76, 94),
		SurfaceDim:  vt.RGBColor(46, 52, 64),
		Overlay0:    vt.RGBColor(76, 86, 106),
		Overlay1:    vt.RGBColor(100, 110, 130),
		Text:        vt.RGBColor(236, 239, 244),
		Subtext0:    vt.RGBColor(216, 222, 233),
		Mauve:       vt.RGBColor(180, 142, 173),
		Green:       vt.RGBColor(163, 190, 140),
		Yellow:      vt.RGBColor(235, 203, 139),
		Red:         vt.RGBColor(191, 97, 106),
		Blue:        vt.RGBColor(129, 161, 193),
		Teal:        vt.RGBColor(143, 188, 187),
		Peach:       vt.RGBColor(208, 135, 112),
	},
	"gruvbox": {
		Accent:      vt.RGBColor(215, 153, 33),
		PanelBG:     vt.RGBColor(40, 40, 40),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(50, 49, 48),
		SelectionBG: vt.RGBColor(75, 63, 39),
		Surface0:    vt.RGBColor(60, 56, 54),
		Surface1:    vt.RGBColor(80, 73, 69),
		SurfaceDim:  vt.RGBColor(40, 40, 40),
		Overlay0:    vt.RGBColor(146, 131, 116),
		Overlay1:    vt.RGBColor(168, 153, 132),
		Text:        vt.RGBColor(235, 219, 178),
		Subtext0:    vt.RGBColor(213, 196, 161),
		Mauve:       vt.RGBColor(211, 134, 155),
		Green:       vt.RGBColor(184, 187, 38),
		Yellow:      vt.RGBColor(250, 189, 47),
		Red:         vt.RGBColor(251, 73, 52),
		Blue:        vt.RGBColor(131, 165, 152),
		Teal:        vt.RGBColor(142, 192, 124),
		Peach:       vt.RGBColor(254, 128, 25),
	},
	"gruvbox-light": {
		Accent:      vt.RGBColor(7, 102, 120),
		PanelBG:     vt.RGBColor(251, 241, 199),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(242, 229, 188),
		SelectionBG: vt.RGBColor(235, 219, 178),
		Surface0:    vt.RGBColor(235, 219, 178),
		Surface1:    vt.RGBColor(213, 196, 161),
		SurfaceDim:  vt.RGBColor(242, 229, 188),
		Overlay0:    vt.RGBColor(146, 131, 116),
		Overlay1:    vt.RGBColor(124, 111, 100),
		Text:        vt.RGBColor(60, 56, 54),
		Subtext0:    vt.RGBColor(80, 73, 69),
		Mauve:       vt.RGBColor(143, 63, 113),
		Green:       vt.RGBColor(121, 116, 14),
		Yellow:      vt.RGBColor(181, 118, 20),
		Red:         vt.RGBColor(157, 0, 6),
		Blue:        vt.RGBColor(7, 102, 120),
		Teal:        vt.RGBColor(66, 123, 88),
		Peach:       vt.RGBColor(175, 58, 3),
	},
	"one-dark": {
		Accent:      vt.RGBColor(97, 175, 239),
		PanelBG:     vt.RGBColor(40, 44, 52),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(49, 54, 64),
		SelectionBG: vt.RGBColor(51, 70, 89),
		Surface0:    vt.RGBColor(44, 49, 58),
		Surface1:    vt.RGBColor(62, 68, 81),
		SurfaceDim:  vt.RGBColor(40, 44, 52),
		Overlay0:    vt.RGBColor(92, 99, 112),
		Overlay1:    vt.RGBColor(115, 122, 135),
		Text:        vt.RGBColor(171, 178, 191),
		Subtext0:    vt.RGBColor(150, 156, 168),
		Mauve:       vt.RGBColor(198, 120, 221),
		Green:       vt.RGBColor(152, 195, 121),
		Yellow:      vt.RGBColor(229, 192, 123),
		Red:         vt.RGBColor(224, 108, 117),
		Blue:        vt.RGBColor(97, 175, 239),
		Teal:        vt.RGBColor(86, 182, 194),
		Peach:       vt.RGBColor(209, 154, 102),
	},
	"one-light": {
		Accent:      vt.RGBColor(64, 120, 242),
		PanelBG:     vt.RGBColor(250, 250, 250),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(216, 219, 226),
		SelectionBG: vt.RGBColor(205, 219, 248),
		Surface0:    vt.RGBColor(240, 240, 241),
		Surface1:    vt.RGBColor(229, 229, 230),
		SurfaceDim:  vt.RGBColor(245, 245, 246),
		Overlay0:    vt.RGBColor(160, 161, 167),
		Overlay1:    vt.RGBColor(104, 107, 119),
		Text:        vt.RGBColor(56, 58, 66),
		Subtext0:    vt.RGBColor(104, 107, 119),
		Mauve:       vt.RGBColor(166, 38, 164),
		Green:       vt.RGBColor(80, 161, 79),
		Yellow:      vt.RGBColor(193, 132, 1),
		Red:         vt.RGBColor(228, 86, 73),
		Blue:        vt.RGBColor(64, 120, 242),
		Teal:        vt.RGBColor(1, 132, 188),
		Peach:       vt.RGBColor(152, 104, 1),
	},
	"solarized": {
		Accent:      vt.RGBColor(38, 139, 210),
		PanelBG:     vt.RGBColor(0, 43, 54),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(22, 75, 87),
		SelectionBG: vt.RGBColor(8, 62, 85),
		Surface0:    vt.RGBColor(7, 54, 66),
		Surface1:    vt.RGBColor(88, 110, 117),
		SurfaceDim:  vt.RGBColor(0, 43, 54),
		Overlay0:    vt.RGBColor(88, 110, 117),
		Overlay1:    vt.RGBColor(101, 123, 131),
		Text:        vt.RGBColor(147, 161, 161),
		Subtext0:    vt.RGBColor(131, 148, 150),
		Mauve:       vt.RGBColor(211, 54, 130),
		Green:       vt.RGBColor(133, 153, 0),
		Yellow:      vt.RGBColor(181, 137, 0),
		Red:         vt.RGBColor(220, 50, 47),
		Blue:        vt.RGBColor(38, 139, 210),
		Teal:        vt.RGBColor(42, 161, 152),
		Peach:       vt.RGBColor(203, 75, 22),
	},
	"solarized-light": {
		Accent:      vt.RGBColor(38, 139, 210),
		PanelBG:     vt.RGBColor(253, 246, 227),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(238, 232, 213),
		SelectionBG: vt.RGBColor(201, 220, 223),
		Surface0:    vt.RGBColor(238, 232, 213),
		Surface1:    vt.RGBColor(147, 161, 161),
		SurfaceDim:  vt.RGBColor(238, 232, 213),
		Overlay0:    vt.RGBColor(147, 161, 161),
		Overlay1:    vt.RGBColor(88, 110, 117),
		Text:        vt.RGBColor(101, 123, 131),
		Subtext0:    vt.RGBColor(131, 148, 150),
		Mauve:       vt.RGBColor(211, 54, 130),
		Green:       vt.RGBColor(133, 153, 0),
		Yellow:      vt.RGBColor(181, 137, 0),
		Red:         vt.RGBColor(220, 50, 47),
		Blue:        vt.RGBColor(38, 139, 210),
		Teal:        vt.RGBColor(42, 161, 152),
		Peach:       vt.RGBColor(203, 75, 22),
	},
	"kanagawa": {
		Accent:      vt.RGBColor(126, 156, 216),
		PanelBG:     vt.RGBColor(31, 31, 40),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(54, 54, 70),
		SelectionBG: vt.RGBColor(50, 56, 75),
		Surface0:    vt.RGBColor(42, 42, 55),
		Surface1:    vt.RGBColor(54, 54, 70),
		SurfaceDim:  vt.RGBColor(31, 31, 40),
		Overlay0:    vt.RGBColor(114, 113, 105),
		Overlay1:    vt.RGBColor(135, 134, 125),
		Text:        vt.RGBColor(220, 215, 186),
		Subtext0:    vt.RGBColor(200, 195, 170),
		Mauve:       vt.RGBColor(149, 127, 184),
		Green:       vt.RGBColor(118, 148, 106),
		Yellow:      vt.RGBColor(192, 163, 110),
		Red:         vt.RGBColor(195, 64, 67),
		Blue:        vt.RGBColor(126, 156, 216),
		Teal:        vt.RGBColor(127, 180, 202),
		Peach:       vt.RGBColor(255, 160, 102),
	},
	"kanagawa-lotus": {
		Accent:      vt.RGBColor(77, 105, 155),
		PanelBG:     vt.RGBColor(242, 236, 188),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(213, 206, 163),
		SelectionBG: vt.RGBColor(220, 213, 172),
		Surface0:    vt.RGBColor(220, 213, 172),
		Surface1:    vt.RGBColor(201, 203, 209),
		SurfaceDim:  vt.RGBColor(213, 206, 163),
		Overlay0:    vt.RGBColor(160, 156, 172),
		Overlay1:    vt.RGBColor(138, 137, 128),
		Text:        vt.RGBColor(84, 84, 100),
		Subtext0:    vt.RGBColor(67, 67, 108),
		Mauve:       vt.RGBColor(98, 76, 131),
		Green:       vt.RGBColor(111, 137, 78),
		Yellow:      vt.RGBColor(119, 113, 63),
		Red:         vt.RGBColor(200, 64, 83),
		Blue:        vt.RGBColor(77, 105, 155),
		Teal:        vt.RGBColor(78, 140, 162),
		Peach:       vt.RGBColor(204, 109, 0),
	},
	"rose-pine": {
		Accent:      vt.RGBColor(196, 167, 231),
		PanelBG:     vt.RGBColor(25, 23, 36),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(38, 35, 58),
		SelectionBG: vt.RGBColor(59, 52, 75),
		Surface0:    vt.RGBColor(31, 29, 46),
		Surface1:    vt.RGBColor(38, 35, 58),
		SurfaceDim:  vt.RGBColor(38, 35, 58),
		Overlay0:    vt.RGBColor(110, 106, 134),
		Overlay1:    vt.RGBColor(144, 140, 170),
		Text:        vt.RGBColor(224, 222, 244),
		Subtext0:    vt.RGBColor(200, 197, 220),
		Mauve:       vt.RGBColor(196, 167, 231),
		Green:       vt.RGBColor(49, 116, 143),
		Yellow:      vt.RGBColor(246, 193, 119),
		Red:         vt.RGBColor(235, 111, 146),
		Blue:        vt.RGBColor(49, 116, 143),
		Teal:        vt.RGBColor(156, 207, 216),
		Peach:       vt.RGBColor(234, 154, 151),
	},
	"rose-pine-dawn": {
		Accent:      vt.RGBColor(144, 122, 169),
		PanelBG:     vt.RGBColor(250, 244, 237),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(227, 217, 207),
		SelectionBG: vt.RGBColor(242, 233, 225),
		Surface0:    vt.RGBColor(242, 233, 225),
		Surface1:    vt.RGBColor(255, 250, 243),
		SurfaceDim:  vt.RGBColor(242, 233, 225),
		Overlay0:    vt.RGBColor(152, 147, 165),
		Overlay1:    vt.RGBColor(121, 117, 147),
		Text:        vt.RGBColor(70, 66, 97),
		Subtext0:    vt.RGBColor(121, 117, 147),
		Mauve:       vt.RGBColor(144, 122, 169),
		Green:       vt.RGBColor(40, 105, 131),
		Yellow:      vt.RGBColor(234, 157, 52),
		Red:         vt.RGBColor(180, 99, 122),
		Blue:        vt.RGBColor(40, 105, 131),
		Teal:        vt.RGBColor(86, 148, 159),
		Peach:       vt.RGBColor(215, 130, 126),
	},
	"vesper": {
		Accent:      vt.RGBColor(255, 199, 153),
		PanelBG:     vt.RGBColor(26, 26, 26),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(16, 16, 16),
		SelectionBG: vt.RGBColor(35, 35, 35),
		Surface0:    vt.RGBColor(35, 35, 35),
		Surface1:    vt.RGBColor(40, 40, 40),
		SurfaceDim:  vt.RGBColor(16, 16, 16),
		Overlay0:    vt.RGBColor(92, 92, 92),
		Overlay1:    vt.RGBColor(126, 126, 126),
		Text:        vt.RGBColor(255, 255, 255),
		Subtext0:    vt.RGBColor(160, 160, 160),
		Mauve:       vt.RGBColor(255, 209, 168),
		Green:       vt.RGBColor(153, 255, 228),
		Yellow:      vt.RGBColor(255, 199, 153),
		Red:         vt.RGBColor(255, 128, 128),
		Blue:        vt.RGBColor(176, 176, 176),
		Teal:        vt.RGBColor(102, 221, 204),
		Peach:       vt.RGBColor(255, 199, 153),
	},
	// matrix is tend's own, not herdr's: the green-on-black of the film's
	// rain (#00FF41, #008F11, #003B00, #0D0208). Only the chrome is green.
	// The states keep colours of their own — gold working, red blocked,
	// cyan done — because a sidebar where everything is green cannot say
	// which agent is waiting, which is what the sidebar is for.
	"matrix": {
		Accent:      vt.RGBColor(0, 255, 65),
		PanelBG:     vt.RGBColor(13, 2, 8),
		SidebarBG:   vt.DefaultColor,
		ActiveRowBG: vt.RGBColor(0, 59, 0),
		SelectionBG: vt.RGBColor(0, 42, 12),
		Surface0:    vt.RGBColor(0, 32, 8),
		Surface1:    vt.RGBColor(0, 59, 0),
		SurfaceDim:  vt.RGBColor(0, 24, 6),
		Overlay0:    vt.RGBColor(0, 143, 17),
		Overlay1:    vt.RGBColor(0, 190, 40),
		Text:        vt.RGBColor(0, 255, 65),
		Subtext0:    vt.RGBColor(0, 143, 17),
		Mauve:       vt.RGBColor(120, 255, 140),
		Green:       vt.RGBColor(0, 255, 65),
		Yellow:      vt.RGBColor(255, 215, 0),
		Red:         vt.RGBColor(255, 60, 60),
		Blue:        vt.RGBColor(0, 200, 255),
		Teal:        vt.RGBColor(0, 230, 230),
		Peach:       vt.RGBColor(255, 170, 0),
	},
}

// PaletteNamed is a theme's palette, from any spelling config accepts.
func PaletteNamed(name string) (Palette, bool) {
	canonical, ok := config.CanonicalTheme(name)
	if !ok {
		return Palette{}, false
	}
	p, ok := palettes[canonical]
	return p, ok
}

// withCustom lays [ui.theme.custom] overrides over the palette, token by
// token, as herdr's with_overrides does.
func (p Palette) withCustom(custom map[string]string) Palette {
	for token, value := range custom {
		c, ok := config.ParseThemeColor(value)
		if !ok {
			continue // refused at load
		}
		color := vt.DefaultColor
		switch {
		case c.Reset:
		case c.RGB:
			color = vt.RGBColor(c.R, c.G, c.B)
		default:
			color = vt.IndexedColor(c.Index)
		}
		switch token {
		case "accent":
			p.Accent = color
		case "panel_bg":
			p.PanelBG = color
		case "sidebar_bg":
			p.SidebarBG = color
		case "active_row_bg":
			p.ActiveRowBG = color
		case "selection_bg":
			p.SelectionBG = color
		case "surface0":
			p.Surface0 = color
		case "surface1":
			p.Surface1 = color
		case "surface_dim":
			p.SurfaceDim = color
		case "overlay0":
			p.Overlay0 = color
		case "overlay1":
			p.Overlay1 = color
		case "text":
			p.Text = color
		case "subtext0":
			p.Subtext0 = color
		case "mauve":
			p.Mauve = color
		case "green":
			p.Green = color
		case "yellow":
			p.Yellow = color
		case "red":
			p.Red = color
		case "blue":
			p.Blue = color
		case "teal":
			p.Teal = color
		case "peach":
			p.Peach = color
		}
	}
	return p
}
