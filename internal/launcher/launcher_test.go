package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheEntryStartsTendByItsFullPathInATerminal: the launcher has the
// desktop's PATH, not the shell's, so the entry names tend by where it is;
// and tend needs a terminal. If it regresses, the icon is there and
// clicking it does nothing.
func TestTheEntryStartsTendByItsFullPathInATerminal(t *testing.T) {
	got := Entry("/home/ana/.local/bin/tend", "")
	for _, want := range []string{
		"[Desktop Entry]\n", "\nType=Application\n", "\nName=tend\n",
		"\nExec=/home/ana/.local/bin/tend\n", "\nTerminal=true\n", "\nIcon=tend\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
}

// TestAPathWithSpacesOrSignsIsQuotedAsTheSpecificationSays: a home folder
// with a space, a session with a quote. If it regresses, the launcher
// splits the path in two and starts nothing, or reads a % as a field code.
func TestAPathWithSpacesOrSignsIsQuotedAsTheSpecificationSays(t *testing.T) {
	got := Entry("/home/ana maria/bin/tend", `we"b $1 50%`)
	if !strings.Contains(got, `Exec="/home/ana maria/bin/tend" attach -s "we\"b \$1 50%%"`+"\n") {
		t.Fatalf("Exec line:\n%s", got)
	}
	if !strings.Contains(got, `Name=tend (we"b $1 50%)`+"\n") {
		t.Fatalf("Name line:\n%s", got)
	}
	if strings.Count(Entry("/bin/tend", "a\nExec=rm"), "\nExec=") != 1 {
		t.Fatal("a newline in a session's name started a key of its own")
	}
}

// TestInstallWritesTheEntryAndItsIconAndRemoveTakesThemAway: both files,
// under the data directory, readable by the launcher; installing again
// follows a tend that moved; removing twice is not an error.
func TestInstallWritesTheEntryAndItsIconAndRemoveTakesThemAway(t *testing.T) {
	dir := t.TempDir()
	p, err := Install(dir, "/opt/old/tend", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Entry != filepath.Join(dir, "applications", "tend.desktop") {
		t.Fatalf("entry at %s", p.Entry)
	}
	if _, err := Install(dir, "/opt/new/tend", ""); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(p.Entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(entry), "Exec=/opt/new/tend\n") || strings.Contains(string(entry), "old") {
		t.Fatalf("the entry did not follow tend:\n%s", entry)
	}
	svg, err := os.ReadFile(p.Icon)
	if err != nil || !strings.HasPrefix(string(svg), "<svg") {
		t.Fatalf("icon: %v %q", err, svg)
	}
	if info, _ := os.Stat(p.Entry); info.Mode().Perm() != 0o644 {
		t.Fatalf("entry mode %v", info.Mode().Perm())
	}
	if _, err := Install(dir, "tend", ""); err == nil {
		t.Fatal("a tend named without its path was taken")
	}
	for i := 0; i < 2; i++ {
		if _, err := Remove(dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(p.Entry); !os.IsNotExist(err) {
		t.Fatalf("the entry is still there: %v", err)
	}
	if _, err := os.Stat(p.Icon); !os.IsNotExist(err) {
		t.Fatalf("the icon is still there: %v", err)
	}
}

// TestTheDataDirectoryIsXDGsOrTheUsualOne: a relative XDG_DATA_HOME is
// ignored, as the specification says.
func TestTheDataDirectoryIsXDGsOrTheUsualOne(t *testing.T) {
	t.Setenv("HOME", "/home/ana")
	t.Setenv("XDG_DATA_HOME", "/data")
	if d, _ := DataHome(); d != "/data" {
		t.Fatalf("got %s", d)
	}
	t.Setenv("XDG_DATA_HOME", "relative")
	if d, _ := DataHome(); d != "/home/ana/.local/share" {
		t.Fatalf("got %s", d)
	}
}

// TestTheWindowsEntryStartsTendDesktopWithoutATerminal: tend-desktop is a
// window, so its entry asks for no terminal, names the binary by its full
// path, passes the session the way tend-desktop takes it (-s, not tend's
// `attach -s`) and is grouped with the running window by its class. If it
// regresses, clicking it opens an empty terminal beside the window, or the
// default session when another was asked for.
func TestTheWindowsEntryStartsTendDesktopWithoutATerminal(t *testing.T) {
	got := WindowEntry("/usr/bin/tend-desktop", "work")
	for _, want := range []string{
		"\nName=tend desktop (work)\n", "\nExec=/usr/bin/tend-desktop -s work\n",
		"\nTerminal=false\n", "\nIcon=tend\n", "\nStartupWMClass=tend-desktop\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing from:\n%s", want, got)
		}
	}
	if strings.Contains(WindowEntry("/usr/bin/tend-desktop", ""), " -s") {
		t.Error("the default session was passed as an argument")
	}
}

// TestTheTwoEntriesShareTheIconAndRemovingOneKeepsIt: tend's entry and the
// window's are separate files with one icon. If it regresses, taking the
// window's shortcut out leaves tend's with no icon.
func TestTheTwoEntriesShareTheIconAndRemovingOneKeepsIt(t *testing.T) {
	dir := t.TempDir()
	term, err := Install(dir, "/usr/bin/tend", "")
	if err != nil {
		t.Fatal(err)
	}
	win, err := InstallWindow(dir, "/usr/bin/tend-desktop", "")
	if err != nil {
		t.Fatal(err)
	}
	if win.Entry == term.Entry || win.Icon != term.Icon {
		t.Fatalf("entries %s and %s, icons %s and %s", term.Entry, win.Entry, term.Icon, win.Icon)
	}
	if _, err := RemoveWindow(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(win.Entry); !os.IsNotExist(err) {
		t.Fatalf("the window's entry is still there: %v", err)
	}
	if _, err := os.Stat(term.Icon); err != nil {
		t.Fatalf("the icon went with the window's entry while tend's still uses it: %v", err)
	}
	if _, err := Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(term.Icon); !os.IsNotExist(err) {
		t.Fatalf("the icon outlived both entries: %v", err)
	}
	if _, err := InstallWindow(dir, "tend-desktop", ""); err == nil {
		t.Fatal("a tend-desktop named without its path was taken")
	}
}
