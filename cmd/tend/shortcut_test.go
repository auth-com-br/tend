package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestTheWindowsShortcutStartsTheTendDesktopBesideTendFirst: the one beside
// tend is the one of the same version, as tend-desktop prefers the tend
// beside it; the one on PATH is the fallback; and neither is an error that
// says how to get one. If it regresses, `tend shortcut -desktop` points the
// launcher at an older tend-desktop left on PATH, or writes an entry for a
// program that is not there.
func TestTheWindowsShortcutStartsTheTendDesktopBesideTendFirst(t *testing.T) {
	install, onPath := t.TempDir(), t.TempDir()
	tend := filepath.Join(install, "tend")
	t.Setenv("PATH", onPath)
	if _, err := findWindow(tend); !errors.Is(err, errNoWindow) {
		t.Fatalf("with no tend-desktop anywhere: %v", err)
	}
	other := filepath.Join(onPath, "tend-desktop")
	if err := os.WriteFile(other, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := findWindow(tend); err != nil || got != other {
		t.Fatalf("only on PATH: %q %v", got, err)
	}
	beside := filepath.Join(install, "tend-desktop")
	if err := os.WriteFile(beside, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := findWindow(tend); err != nil || got != beside {
		t.Fatalf("beside tend and on PATH: %q %v", got, err)
	}
}
