//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestASidebarOnTheRightIsWhereItIsDrawnAndClicked runs the real client with
// sidebar_side = "right": the sidebar in the right-hand columns, the panes
// from the left edge, and its two handles — the one that puts it away, in
// its outer corner, and the one that brings it back — answering clicks
// where they are drawn. If it regresses, the sidebar is drawn on one side
// and clicked on the other.
func TestASidebarOnTheRightIsWhereItIsDrawnAndClicked(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(configPath, []byte("[ui]\nsidebar = true\nsidebar_side = \"right\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withConfig(t, configPath)
	const cols, rows = 100, 16
	a := startSessionIn(t, cols, rows, t.TempDir())
	a.waitForScreen(t, "the sidebar", func(s string) bool { return strings.Contains(s, "spaces") })

	lines := strings.Split(a.text(), "\n")
	for _, l := range lines {
		if i := strings.Index(l, "spaces"); i >= 0 && len([]rune(l[:i])) < cols/2 {
			t.Fatalf("the sidebar is on the left:\n%s", a.text())
		}
	}
	paneTop := -1
	for y, l := range lines {
		if strings.HasPrefix(l, "┌") {
			paneTop = y
			break
		}
	}
	if paneTop < 0 {
		t.Fatalf("no pane starts at the left edge:\n%s", a.text())
	}

	// The hide handle, »: the sidebar's last line, its outer corner.
	a.clickAt(t, cols, rows-1)
	a.waitForScreen(t, "the sidebar put away", func(s string) bool { return !strings.Contains(s, "spaces") })
	// And the handle that brings it back, «, top right.
	a.clickAt(t, cols, 1)
	a.waitForScreen(t, "the sidebar back", func(s string) bool { return strings.Contains(s, "spaces") })
}
