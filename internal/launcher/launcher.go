// Package launcher puts tend in the desktop's application launcher: an entry
// a person clicks to get a terminal with their session in it, as they open
// any other program. herdr has nothing like it; the owner asked for it
// (docs/PORTING.md).
//
// It is the freedesktop.org desktop entry, which GNOME, KDE, COSMIC, Xfce
// and the rest read from $XDG_DATA_HOME/applications: one file for the
// entry and one for its icon, both the user's own, nothing system-wide and
// nothing needing root.
package launcher

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed icon.svg
var icon []byte

// ErrNoHome is returned when there is nowhere to put the entry: neither
// XDG_DATA_HOME nor HOME is set.
var ErrNoHome = errors.New("launcher: no XDG_DATA_HOME and no HOME to put the entry in")

// Icon is tend's icon, the one both entries show. What the packages
// (tools/deb, tools/appimage) install system-wide is this same file.
func Icon() []byte { return icon }

// Paths are the two files of the entry.
type Paths struct {
	Entry string
	Icon  string
}

// Locate says where the entry and its icon go for a data directory
// ($XDG_DATA_HOME, or ~/.local/share).
func Locate(dataHome string) Paths {
	return Paths{
		Entry: filepath.Join(dataHome, "applications", "tend.desktop"),
		Icon:  filepath.Join(dataHome, "icons", "hicolor", "scalable", "apps", "tend.svg"),
	}
}

// LocateWindow says where tend-desktop's entry goes. It is an entry of its
// own beside tend's, not a second action of tend's: they are two programs a
// person may have one or both of, and the launcher shows each by its name.
// The icon is the same file.
func LocateWindow(dataHome string) Paths {
	p := Locate(dataHome)
	p.Entry = filepath.Join(dataHome, "applications", "tend-desktop.desktop")
	return p
}

// DataHome is $XDG_DATA_HOME, or ~/.local/share when it is unset or not an
// absolute path, as the base directory specification has it.
func DataHome() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", ErrNoHome
	}
	return filepath.Join(home, ".local", "share"), nil
}

// Entry is the desktop entry's text for a tend at exe, opening session (the
// default one when empty). The path is absolute and quoted: a launcher
// starts programs with the desktop's PATH, which often lacks ~/.local/bin,
// and an entry that names only "tend" is one that does nothing when
// clicked. Terminal=true has the desktop open its own terminal for it.
func Entry(exe, session string) string {
	name, command := "tend", quote(exe)
	if session != "" {
		name = "tend (" + session + ")"
		command += " attach -s " + quote(session)
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=" + value(name),
		"GenericName=Terminal for coding agents",
		"Comment=Run AI coding agents and follow every one of them on one screen",
		"Exec=" + command,
		"Icon=tend",
		"Terminal=true",
		"Categories=Development;",
		"Keywords=agents;terminal;claude;codex;",
		"StartupNotify=false",
		"",
	}, "\n")
}

// WindowEntry is the desktop entry's text for a tend-desktop at exe, opening
// session (the default one when empty). Unlike tend's it asks for no
// terminal: tend-desktop is the window. StartupWMClass is the class Qt gives
// the window (the binary's name), so the dock groups the running window
// under this entry instead of showing a second one with no icon.
func WindowEntry(exe, session string) string {
	name, command := "tend desktop", quote(exe)
	if session != "" {
		name = "tend desktop (" + session + ")"
		command += " -s " + quote(session)
	}
	return strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=" + value(name),
		"GenericName=Terminal for coding agents",
		"Comment=Run AI coding agents and follow every one of them, in a window",
		"Exec=" + command,
		"Icon=tend",
		"Terminal=false",
		"Categories=Development;",
		"Keywords=agents;terminal;claude;codex;",
		"StartupWMClass=tend-desktop",
		"StartupNotify=true",
		"",
	}, "\n")
}

// quote is one argument of an Exec line. The specification has an argument
// with a space or a reserved character in double quotes, with ", `, $ and \
// escaped by a backslash; and a % is written twice anywhere, or the
// launcher takes it for a field code. A newline cannot be written at all —
// it ends the line, and what follows would be a key of its own — so it
// becomes a space.
func quote(arg string) string {
	arg = strings.NewReplacer("%", "%%", "\n", " ", "\r", " ").Replace(arg)
	if arg != "" && !strings.ContainsAny(arg, " \t\n\"'\\><~|&;$*?#()`") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range arg {
		if strings.ContainsRune("\"`$\\", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// value is a string value of the entry: a newline would end it and start a
// key of somebody else's choosing.
func value(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", " ", "\r", " ").Replace(s)
}

// Install writes the entry and its icon under dataHome, replacing what was
// there: installing again is how the entry follows a tend that moved.
func Install(dataHome, exe, session string) (Paths, error) {
	if !filepath.IsAbs(exe) {
		return Paths{}, fmt.Errorf("launcher: %q is not an absolute path to tend", exe)
	}
	return install(Locate(dataHome), Entry(exe, session))
}

// InstallWindow is Install for tend-desktop's entry.
func InstallWindow(dataHome, exe, session string) (Paths, error) {
	if !filepath.IsAbs(exe) {
		return Paths{}, fmt.Errorf("launcher: %q is not an absolute path to tend-desktop", exe)
	}
	return install(LocateWindow(dataHome), WindowEntry(exe, session))
}

func install(p Paths, entry string) (Paths, error) {
	if err := write(p.Icon, icon); err != nil {
		return Paths{}, err
	}
	if err := write(p.Entry, []byte(entry)); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// Remove takes the entry and its icon away. Not being there is not an
// error: the person asked for it gone, and it is.
func Remove(dataHome string) (Paths, error) {
	return remove(Locate(dataHome), LocateWindow(dataHome))
}

// RemoveWindow is Remove for tend-desktop's entry.
func RemoveWindow(dataHome string) (Paths, error) {
	return remove(LocateWindow(dataHome), Locate(dataHome))
}

// remove takes p away, and the icon with it unless the other entry is still
// there: the two share it, and taking one out must not leave the other
// showing a blank square.
func remove(p, other Paths) (Paths, error) {
	paths := []string{p.Entry}
	if _, err := os.Stat(other.Entry); err != nil {
		paths = append(paths, p.Icon)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Paths{}, err
		}
	}
	return p, nil
}

// write puts a file in place whole: a launcher reading the directory while
// it is written must not find half an entry.
func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tend-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
