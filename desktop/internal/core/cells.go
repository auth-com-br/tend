package core

import "github.com/auth-com-br/tend/internal/vt"

// Cell is one screen position as the painter draws it: the colours already
// resolved to RGB, reverse video already applied. Its layout is the C++
// TendCell's (qtterm/terminal.h), field for field, so a frame crosses to the
// painter as one copy of a slice.
type Cell struct {
	Ch    uint32
	FG    uint32 // 0xRRGGBB
	BG    uint32 // 0xRRGGBB
	Attrs uint16
	Width uint8
	_     uint8
}

// Attributes the painter reads. They are vt's bits, kept as they are so the
// two never need translating.
const (
	AttrBold      = uint16(vt.AttrBold)
	AttrDim       = uint16(vt.AttrDim)
	AttrItalic    = uint16(vt.AttrItalic)
	AttrUnderline = uint16(vt.AttrUnderline)
	AttrStrike    = uint16(vt.AttrStrike)
)

// Frame is one pane's screen, ready to paint.
type Frame struct {
	Cols, Rows       int
	Cells            []Cell
	CursorX, CursorY int
	CursorVisible    bool
	// BG is the pane's default background, which the painter fills the
	// whole item with first: the cells never cover the strip that the cell
	// grid leaves at the right and bottom edges.
	BG uint32
}

// Colours is the palette a pane is painted with: the default foreground and
// background, and the 16 ANSI colours. The rest of the 256 are xterm's cube
// and grey ramp, which no theme changes.
type Colours struct {
	FG, BG uint32
	ANSI   [16]uint32
}

// DefaultColours is catppuccin mocha, tend's default theme's family, until
// the desktop reads the theme from the settings.
var DefaultColours = Colours{
	FG: 0xcdd6f4,
	BG: 0x1e1e2e,
	ANSI: [16]uint32{
		0x45475a, 0xf38ba8, 0xa6e3a1, 0xf9e2af, 0x89b4fa, 0xf5c2e7, 0x94e2d5, 0xbac2de,
		0x585b70, 0xf38ba8, 0xa6e3a1, 0xf9e2af, 0x89b4fa, 0xf5c2e7, 0x94e2d5, 0xa6adc8,
	},
}

// rgb is the colour c paints as, def standing in for the default.
func (p *Colours) rgb(c vt.Color, def uint32) uint32 {
	switch {
	case c.IsRGB():
		r, g, b := c.RGB()
		return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
	case c.IsIndexed():
		return p.indexed(c.Index())
	}
	return def
}

func (p *Colours) indexed(i uint8) uint32 {
	switch {
	case i < 16:
		return p.ANSI[i]
	case i < 232:
		i -= 16
		level := func(v uint8) uint32 {
			if v == 0 {
				return 0
			}
			return uint32(55 + 40*uint32(v))
		}
		return level(i/36)<<16 | level(i/6%6)<<8 | level(i%6)
	}
	g := uint32(8 + 10*(uint32(i)-232))
	return g<<16 | g<<8 | g
}

// FrameOf reads a screen into a Frame, reusing buf's storage when it is big
// enough: this runs for every repaint of every visible pane.
func FrameOf(s *vt.Screen, p *Colours, buf []Cell) Frame {
	cols, rows := s.Size()
	n := cols * rows
	if cap(buf) < n {
		buf = make([]Cell, n)
	}
	buf = buf[:n]
	grid := s.Grid()
	for y := 0; y < rows; y++ {
		line := grid.Line(y).Cells()
		for x := 0; x < cols; x++ {
			out := &buf[y*cols+x]
			if x >= len(line) {
				*out = Cell{Ch: ' ', FG: p.FG, BG: p.BG, Width: 1}
				continue
			}
			c := line[x]
			fg, bg := p.rgb(c.Style.FG, p.FG), p.rgb(c.Style.BG, p.BG)
			// Bold with one of the first eight colours is its bright one,
			// as xterm and most terminals draw it.
			if c.Style.Has(vt.AttrBold) && c.Style.FG.IsIndexed() && c.Style.FG.Index() < 8 {
				fg = p.ANSI[c.Style.FG.Index()+8]
			}
			if c.Style.Has(vt.AttrReverse) {
				fg, bg = bg, fg
			}
			if c.Style.Has(vt.AttrHidden) {
				fg = bg
			}
			ch := uint32(c.R)
			if ch == 0 {
				ch = ' '
			}
			*out = Cell{Ch: ch, FG: fg, BG: bg, Attrs: uint16(c.Style.Attrs), Width: c.Width}
		}
	}
	cur := s.Cursor()
	return Frame{
		Cols: cols, Rows: rows, Cells: buf,
		CursorX: cur.X, CursorY: cur.Y,
		CursorVisible: s.Modes().CursorVisible,
		BG:            p.BG,
	}
}
