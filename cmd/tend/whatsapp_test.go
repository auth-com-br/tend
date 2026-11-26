package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestTendWhatsAppIsFoundOnPathOrExplainedWhenMissing: `tend whatsapp` runs
// the tend-whatsapp it finds, and without one says how to install it. If it
// regresses, the command fails with a bare "not found", or runs nothing.
func TestTendWhatsAppIsFoundOnPathOrExplainedWhenMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for the binary")
	}
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if _, err := findWhatsApp(); !errors.Is(err, errNoWhatsApp) {
		t.Fatalf("with none installed: err = %v", err)
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "tend-whatsapp")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := findWhatsApp()
	if err != nil || got != fake {
		t.Fatalf("found %q, %v; want %q", got, err, fake)
	}
}
