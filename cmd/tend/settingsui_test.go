//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/integration"
)

// TestTheSettingsScreenInstallsAnAgentsHooks is herdr's integrations
// section: an agent found on this machine gets a row, and stepping it
// installs the hooks and takes them out again. If it regresses, the only way
// to install them is a command somebody has to know about.
func TestTheSettingsScreenInstallsAnAgentsHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// Installed means its settings directory is there, as Claude Code makes it.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	var row *settingRow
	for _, r := range integrationRows(false) {
		if strings.HasSuffix(r.Label, "claude") {
			r := r
			row = &r
		}
	}
	if row == nil {
		t.Fatal("claude is on PATH and has no row")
	}
	if len(integrationRows(true)) != 0 {
		t.Error("a client on another machine must not offer to install here")
	}

	if _, err := row.Apply(row.Choices[1]); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := row.Value(config.Defaults()); got != string(integration.StatusCurrent) {
		t.Errorf("after installing, the row says %q", got)
	}
	if _, err := row.Apply(row.Choices[0]); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if got := row.Value(config.Defaults()); got != string(integration.StatusNotInstalled) {
		t.Errorf("after removing, the row says %q", got)
	}
}
