package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// `tend whatsapp …` is tend-whatsapp, a binary of its own: whatsmeow and the
// pure-Go SQLite it needs made tend 16 MB heavier (22.2 to 38.3 MB) when
// measured, for a module only some will use, so it lives in its own Go
// module (whatsapp/) and is found here when installed. Beside tend first, as
// tend-desktop finds tend: the two installed together are one version.

// errNoWhatsApp is tend-whatsapp not being installed.
var errNoWhatsApp = errors.New("tend-whatsapp is not installed: build it from the repository with `make whatsapp-install` (see docs/TOOLS.md, WhatsApp)")

// findWhatsApp is the tend-whatsapp to run: beside this tend, else on PATH.
func findWhatsApp() (string, error) {
	name := "tend-whatsapp"
	if self, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(self); err == nil {
			self = real
		}
		beside := filepath.Join(filepath.Dir(self), name)
		if info, err := os.Stat(beside); err == nil && !info.IsDir() {
			return beside, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", errNoWhatsApp
}

func runWhatsApp(args []string) error {
	path, err := findWhatsApp()
	if err != nil {
		return err
	}
	// Replaced rather than run as a child where the system allows it: as
	// `tend whatsapp serve` it is an agent's MCP server on stdio, and a tend
	// in the middle would be one more process holding the pipe.
	argv := append([]string{path}, args...)
	if err := reexec(path, argv, os.Environ()); err == nil {
		return nil
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		return fmt.Errorf("running %s: %w", path, err)
	}
	return nil
}
