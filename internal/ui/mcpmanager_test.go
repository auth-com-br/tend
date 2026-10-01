package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestAClickInTheMCPGridLandsOnTheCellDrawnThere: every agent's cell is hit
// where its mark is drawn, and the name is column 0. If it regresses, a
// click switches a server off in the agent beside the one under the
// pointer.
func TestAClickInTheMCPGridLandsOnTheCellDrawnThere(t *testing.T) {
	v := &MCPManagerView{
		Agents: []string{"claude", "codex", "gemini"},
		Rows: []MCPRow{
			{Name: "siyuan", Transport: "http", Cells: []MCPCell{MCPOn, MCPOff, MCPNone}},
			{Name: "context7", Transport: "stdio", Cells: []MCPCell{MCPNone, MCPOn, MCPOn}},
		},
	}
	const cols, rows = 120, 30
	g := vt.NewGrid(cols, rows, 0)
	drawMCPManager(g, v, DefaultTheme())
	lines := gridText(g)
	geo := MCPManagerLayout(v, cols, rows)
	y := geo.List.Y // the first row, siyuan
	if !strings.Contains(lines[y], "siyuan") {
		t.Fatalf("row 0 is not where it is drawn:\n%s", strings.Join(lines, "\n"))
	}
	runes := []rune(lines[y])
	for want, mark := range []string{"✓", "○", "·"} {
		x := -1
		for c := geo.ColX[want]; c < geo.ColX[want]+geo.ColW && c < len(runes); c++ {
			if string(runes[c]) == mark {
				x = c
				break
			}
		}
		if x < 0 {
			t.Fatalf("%s not drawn in column %d: %q", mark, want, lines[y])
		}
		if row, col, ok := MCPManagerCellAt(v, cols, rows, x, y); !ok || row != 0 || col != want+1 {
			t.Errorf("a click on %s lands on row %d col %d (%v), want col %d", mark, row, col, ok, want+1)
		}
	}
	if _, col, ok := MCPManagerCellAt(v, cols, rows, geo.List.X+1, y+1); !ok || col != 0 {
		t.Errorf("a click on a name lands on column %d", col)
	}
}
