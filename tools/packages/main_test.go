package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sums = `0000000000000000000000000000000000000000000000000000000000000001  tend-darwin-amd64
0000000000000000000000000000000000000000000000000000000000000002  tend-darwin-arm64
0000000000000000000000000000000000000000000000000000000000000003  tend-linux-amd64
00000000000000000000000000000000000000000000000000000000000000ff  tend-linux-arm64
`

// TestAReleasesPackagesPointAtItsOwnBinaries: from make dist's checksums,
// the Homebrew formula has each machine's release binary with its sha256
// under on_macos/on_linux and on_arm/on_intel, and the Nix expression the
// same binaries by system with the hash in SRI form; a release missing a
// build is refused. Neither Homebrew nor Nix was at hand where this was
// written: this checks what is generated, not that brew or nix take it.
// If it regresses, a brew install fetches another build than the one it
// checks, and fails.
func TestAReleasesPackagesPointAtItsOwnBinaries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run("v0.18.0", dir, "auth-com-br/tend", true); err != nil {
		t.Fatal(err)
	}
	rb, _ := os.ReadFile(filepath.Join(dir, "tend.rb"))
	formula := string(rb)
	for _, want := range []string{
		`version "0.18.0"`,
		"  on_linux do\n    on_arm do\n      url \"https://github.com/auth-com-br/tend/releases/download/v0.18.0/tend-linux-arm64\"\n      sha256 \"00000000000000000000000000000000000000000000000000000000000000ff\"",
		"  on_macos do\n    on_arm do",
		`bin.install Dir["tend-*"].first => "tend"`,
	} {
		if !strings.Contains(formula, want) {
			t.Errorf("the formula lacks %q:\n%s", want, formula)
		}
	}
	nix, _ := os.ReadFile(filepath.Join(dir, "tend.nix"))
	if !strings.Contains(string(nix), `aarch64-linux = { url = "https://github.com/auth-com-br/tend/releases/download/v0.18.0/tend-linux-arm64"; hash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAP8="; };`) {
		t.Errorf("the Nix expression:\n%s", nix)
	}

	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(strings.SplitN(sums, "\n", 2)[1]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run("v0.18.0", dir, "auth-com-br/tend", true); err == nil || !strings.Contains(err.Error(), "tend-darwin-amd64") {
		t.Errorf("a release missing a build: %v", err)
	}
}
