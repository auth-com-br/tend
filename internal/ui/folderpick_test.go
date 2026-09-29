package ui

import (
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheFolderPickerOffersWhatAClickCanDo: the rows are taking the folder,
// going up when there is an up, and the folders — hidden ones only when the
// filter starts with a dot — and a click lands on the row drawn under it.
// If it regresses, a click opens the folder beside the one under the
// pointer, or a home folder is a screen of dotfiles.
func TestTheFolderPickerOffersWhatAClickCanDo(t *testing.T) {
	p := FolderPick{Title: "folder for clients", Path: "/home/me", Parent: "/home", Dirs: []string{".config", "acme", "beta"}}
	rows := p.Rows()
	if len(rows) != 4 || rows[0].Kind != FolderUse || rows[1].Kind != FolderUp || rows[2].Name != "acme" || rows[3].Name != "beta" {
		t.Fatalf("rows = %+v", rows)
	}
	p.Query = ".c"
	if rows := p.Rows(); len(rows) != 3 || rows[2].Name != ".config" {
		t.Errorf("a query with a dot gives %+v", rows)
	}
	p.Query = "BET"
	if at := p.FirstDir(); p.Rows()[at].Name != "beta" {
		t.Errorf("typing puts the cursor on %+v", p.Rows()[at])
	}
	p.Query = ""
	if root := (FolderPick{Path: "/"}); len(root.Rows()) != 1 {
		t.Errorf("the root has an up: %+v", root.Rows())
	}

	g := vt.NewGrid(100, 30, 0)
	drawFolderPick(g, p, DefaultTheme())
	lines := gridText(g)
	for i, want := range []string{"use this folder", "..", "acme/", "beta/"} {
		y := -1
		for ly, line := range lines {
			if strings.Contains(line, want) {
				y = ly
				break
			}
		}
		if y < 0 {
			t.Fatalf("%q is not drawn:\n%s", want, strings.Join(lines, "\n"))
		}
		r := FolderPickRect(p, 100, 30)
		if row, inside := FolderPickAt(p, 100, 30, r.X+4, y); !inside || row != i {
			t.Errorf("a click on %q lands on row %d (inside %v), want %d", want, row, inside, i)
		}
	}
	if _, inside := FolderPickAt(p, 100, 30, 0, 0); inside {
		t.Error("the corner of the screen is inside the picker")
	}
}
