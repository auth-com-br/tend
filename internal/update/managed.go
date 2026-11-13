package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A tend a package manager installed is updated by that package manager:
// replacing its file from under it would leave the manager's record wrong
// and the next upgrade undoing it. herdr's update_install_command and
// self_update (`update.rs`), whose paths and words these are.

// Manager is the package manager that installed a tend, when one did.
type Manager struct {
	// Name is "Homebrew", "mise" or "Nix".
	Name string
	// Command is how that manager updates tend, as a person types it.
	Command string
}

var (
	homebrew = Manager{Name: "Homebrew", Command: "brew update && brew upgrade tend"}
	mise     = Manager{Name: "mise", Command: "mise upgrade tend"}
	nix      = Manager{Name: "Nix", Command: "nix profile upgrade, or update the flake input that provides tend"}
)

// ManagerOf is the package manager that put exe where it is, following its
// links: Homebrew's bin/tend is a link into its Cellar.
func ManagerOf(exe string) (Manager, bool) {
	paths := []string{exe}
	if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
		paths = append(paths, real)
	}
	for _, p := range paths {
		switch {
		case strings.HasPrefix(p, "/nix/store/"):
			return nix, true
		case inCellar(p):
			return homebrew, true
		case inMise(p):
			return mise, true
		}
	}
	return Manager{}, false
}

// inCellar is …/Cellar/tend/<version>/bin/tend.
func inCellar(p string) bool {
	version, ok := toolVersionDir(p)
	return ok && filepath.Base(filepath.Dir(filepath.Dir(version))) == "Cellar"
}

// inMise is …/installs/tend/<version>/bin/tend, or under MISE_INSTALLS_DIR.
func inMise(p string) bool {
	version, ok := toolVersionDir(p)
	if !ok {
		return false
	}
	installs := filepath.Dir(filepath.Dir(version))
	if dir := os.Getenv("MISE_INSTALLS_DIR"); dir != "" && filepath.Clean(dir) == installs {
		return true
	}
	return filepath.Base(installs) == "installs"
}

// toolVersionDir is <version> in …/tend/<version>/bin/tend.
func toolVersionDir(p string) (string, bool) {
	if filepath.Base(p) != "tend" {
		return "", false
	}
	bin := filepath.Dir(p)
	if filepath.Base(bin) != "bin" {
		return "", false
	}
	version := filepath.Dir(bin)
	if filepath.Base(filepath.Dir(version)) != "tend" {
		return "", false
	}
	return version, true
}

// ErrManaged is a self-update refused because a package manager owns the
// file, saying what to run instead.
func ErrManaged(m Manager) error {
	return fmt.Errorf("self-update is off for %s installs; run `%s`", m.Name, m.Command)
}
