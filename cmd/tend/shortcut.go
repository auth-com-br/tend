package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/auth-com-br/tend/internal/launcher"
)

// `tend shortcut`: tend in the desktop's application launcher, opened with a
// click like any other program — a terminal with the session in it. It
// writes the user's own desktop entry and icon (internal/launcher); nothing
// system-wide, nothing as root.
//
// `-desktop` puts tend-desktop there instead: the window, with no terminal.
// It is a flag of tend's rather than a command of tend-desktop's because
// this is where a person already looks for it, and it is an entry of its
// own (tend-desktop.desktop) beside tend's, since a person may want either
// or both. The .deb packages install both entries system-wide (tools/deb);
// this is for a tend-desktop built from a checkout.

const shortcutUsage = `usage: tend shortcut [-desktop] [-s session]   put tend (or, with -desktop, its window) in the application launcher
       tend shortcut [-desktop] -remove         take it out`

// errNoWindow is -desktop without a tend-desktop to point the entry at.
var errNoWindow = errors.New("tend-desktop is neither beside tend nor on PATH: build it with `make desktop-install` (docs/DESKTOP.md)")

func runShortcut(args []string) error {
	fs := flag.NewFlagSet("shortcut", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, shortcutUsage); fs.PrintDefaults() }
	session := fs.String("s", "", "the session the shortcut opens (default: the default one)")
	remove := fs.Bool("remove", false, "take the shortcut out of the launcher")
	window := fs.Bool("desktop", false, "the shortcut is tend-desktop's, the window, rather than tend in a terminal")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(shortcutUsage)
	}
	if runtime.GOOS != "linux" {
		// macOS wants an application bundle and has no entry file to
		// write; said rather than writing a file nothing reads.
		return fmt.Errorf("tend shortcut is for Linux desktops so far; on %s, add tend to your terminal's profiles", runtime.GOOS)
	}
	dataHome, err := launcher.DataHome()
	if err != nil {
		return err
	}
	flags := ""
	if *window {
		flags = " -desktop"
	}
	if *remove {
		takeOut := launcher.Remove
		if *window {
			takeOut = launcher.RemoveWindow
		}
		p, err := takeOut(dataHome)
		if err != nil {
			return err
		}
		refreshLauncher(dataHome)
		fmt.Printf("removed %s\n", p.Entry)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// The path as it is run, not what a symlink points to: a tend put on
	// the PATH by a link keeps working when what it links to is replaced.
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	name, put := "tend", launcher.Install
	if *window {
		if exe, err = findWindow(exe); err != nil {
			return err
		}
		name, put = "tend desktop", launcher.InstallWindow
	}
	p, err := put(dataHome, exe, *session)
	if err != nil {
		return err
	}
	refreshLauncher(dataHome)
	fmt.Printf("%s is in the application launcher: look for %q there.\n  %s\n  it runs %s\n", name, name, p.Entry, exe)
	fmt.Printf("`tend shortcut%s -remove` takes it out; run `tend shortcut%s` again if it moves.\n", flags, flags)
	return nil
}

// findWindow is the tend-desktop the window's entry starts: the one beside
// this tend first, as tend-desktop looks for the tend beside it first
// (desktop/internal/core.FindTend), so two installed together are the pair
// that is used; else the one on PATH, made absolute, because the launcher
// has the desktop's PATH and not the shell's.
func findWindow(tend string) (string, error) {
	beside := filepath.Join(filepath.Dir(tend), "tend-desktop")
	if info, err := os.Stat(beside); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return beside, nil
	}
	if p, err := exec.LookPath("tend-desktop"); err == nil {
		return filepath.Abs(p)
	}
	return "", errNoWindow
}

// refreshLauncher tells the desktop its entries changed, where the tool for
// that is installed. Desktops watch the directory and most need no telling;
// a failure here is not a failure of the shortcut.
func refreshLauncher(dataHome string) {
	if tool, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(tool, "-q", filepath.Join(dataHome, "applications")).Run()
	}
}
