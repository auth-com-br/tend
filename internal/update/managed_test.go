package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestATendAPackageManagerInstalledIsUpdatedByIt: a tend in Homebrew's
// Cellar — reached through the bin link Homebrew puts on PATH — in a mise
// install or in the Nix store is its manager's, with its manager's
// command; one anywhere else is tend's own. herdr's update_install_command.
// If it regresses, `tend update` overwrites a file Homebrew owns and the
// next brew upgrade puts the old one back.
func TestATendAPackageManagerInstalledIsUpdatedByIt(t *testing.T) {
	dir := t.TempDir()
	keg := filepath.Join(dir, "Cellar", "tend", "0.18.0", "bin", "tend")
	if err := os.MkdirAll(filepath.Dir(keg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keg, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "bin", "tend")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(keg, link); err != nil {
		t.Fatal(err)
	}
	if m, ok := ManagerOf(link); !ok || m.Name != "Homebrew" || m.Command != "brew update && brew upgrade tend" {
		t.Errorf("through Homebrew's link: %+v %v", m, ok)
	}
	if m, ok := ManagerOf("/home/me/.local/share/mise/installs/tend/0.18.0/bin/tend"); !ok || m.Name != "mise" {
		t.Errorf("mise: %+v %v", m, ok)
	}
	t.Setenv("MISE_INSTALLS_DIR", "/opt/tools")
	if m, ok := ManagerOf("/opt/tools/tend/0.18.0/bin/tend"); !ok || m.Name != "mise" {
		t.Errorf("mise under MISE_INSTALLS_DIR: %+v %v", m, ok)
	}
	if m, ok := ManagerOf("/nix/store/abc-tend-0.18.0/bin/tend"); !ok || m.Name != "Nix" {
		t.Errorf("Nix: %+v %v", m, ok)
	}
	for _, own := range []string{"/home/me/.local/bin/tend", "/usr/local/bin/tend", "/opt/Cellar/other/1/bin/tend"} {
		if m, ok := ManagerOf(own); ok {
			t.Errorf("%s is tend's own, not %s's", own, m.Name)
		}
	}
}

// TestATendTheDebInstalledIsUpdatedWithTheDeb: the tend in the tend
// package's file list is dpkg's; one elsewhere on the same machine, or any
// tend on a machine without the package, is tend's own. If it regresses,
// `tend update` fails on /usr/bin/tend for want of root, or, run as root,
// replaces a file dpkg records as the package's.
func TestATendTheDebInstalledIsUpdatedWithTheDeb(t *testing.T) {
	info := t.TempDir()
	old := dpkgInfo
	dpkgInfo = info
	t.Cleanup(func() { dpkgInfo = old })
	if _, ok := ManagerOf("/usr/bin/tend"); ok {
		t.Fatal("managed with no package installed")
	}
	list := "/.\n/usr\n/usr/bin\n/usr/bin/tend\n/usr/share/applications/tend.desktop\n"
	if err := os.WriteFile(filepath.Join(info, "tend.list"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	if m, ok := ManagerOf("/usr/bin/tend"); !ok || m.Name != ".deb" {
		t.Fatalf("the package's tend: %+v %v", m, ok)
	}
	if m, ok := ManagerOf("/home/me/.local/bin/tend"); ok {
		t.Fatalf("a tend of its own beside the package's is %s's", m.Name)
	}
}
