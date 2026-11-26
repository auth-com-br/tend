package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain turns the first-run welcome off for every tend these tests start:
// each begins with no settings file, which is exactly when the welcome shows,
// and it takes every key. The one test about the welcome turns it back on.
//
// It also points every tend at a settings file of its own, of the defaults
// but for the files panel opening in every tab (quietConfig). Before, a test
// that named no file read the settings of whoever ran it — the owner's own,
// on the owner's machine — which went unnoticed until a default changed.
// A test about settings still names its own.
func TestMain(m *testing.M) {
	os.Setenv(skipOnboardingEnv, "1")
	dir, err := os.MkdirTemp("", "tend-test-config")
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(quietSettings), 0o644); err != nil {
		panic(err)
	}
	os.Setenv("TEND_CONFIG", path)
	pinGoCaches()
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestTheFirstRunWelcomesAndThenOffersTheIntegrations is herdr's
// onboarding: with no settings file, the client opens on the welcome, which
// takes every key and does nothing with any but enter; enter writes
// `onboarding = false` and opens the settings screen; and a client started
// after that opens straight on the session. If it regresses, a first-time
// user is dropped into a shell with no word on how tend is used — or the
// welcome comes back on every start.
func TestTheFirstRunWelcomesAndThenOffersTheIntegrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(path, []byte(quietSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	withConfig(t, path)
	a := startSessionEnv(t, 110, 30, skipOnboardingEnv+"=")
	a.waitForScreen(t, "the welcome", func(s string) bool {
		return strings.Contains(s, "mouse-first terminal") && strings.Contains(s, "ctrl+b enters prefix mode") &&
			strings.Contains(s, "↵ continue")
	})

	// Everything but enter is taken and does nothing: typing, and the
	// prefix with it. What they would have done is looked for once the
	// welcome and the settings are gone.
	a.send(t, "echo TYPED")
	a.send(t, "\x02c")

	a.send(t, "\r")
	a.waitForScreen(t, "the settings screen", func(s string) bool {
		// Where it was written is read from the file just below: the path
		// of a test's temporary directory is longer than the screen gives it.
		return !strings.Contains(s, "mouse-first terminal") && strings.Contains(s, "written to ")
	})
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "onboarding = false") {
		t.Fatalf("the settings file should say it has been through: %v\n%s", err, data)
	}
	if !strings.HasPrefix(string(data), "onboarding = false\n") {
		t.Errorf("the key belongs before every table:\n%s", data)
	}

	a.send(t, "q")
	a.sendUntil(t, "printf AFTER-%s OK\r", "the shell, after the welcome", func(s string) bool {
		return strings.Contains(s, "AFTER-OK")
	})
	if s := a.text(); strings.Contains(s, "TYPED") || strings.Contains(s, "tab 2") {
		t.Errorf("keys pressed during the welcome reached the session:\n%s", s)
	}
}

// pinGoCaches fixes where go keeps its modules and builds to where they are
// now, for the go build the tests run (buildBinary). Tests that point HOME
// at a folder of their own — so the person's agent settings are not read —
// otherwise had that build download every module into it, read-only as the
// module cache is: on CI, where nothing had set GOMODCACHE, the test's
// directory could then not be removed ("unlinkat …/go/pkg/mod/…:
// permission denied").
func pinGoCaches() {
	out, err := exec.Command("go", "env", "GOMODCACHE", "GOCACHE", "GOPATH").Output()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, name := range []string{"GOMODCACHE", "GOCACHE", "GOPATH"} {
		if i < len(lines) && lines[i] != "" {
			os.Setenv(name, lines[i])
		}
	}
}
