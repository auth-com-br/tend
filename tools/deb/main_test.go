package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitDescribeBecomesADebianVersionThatSortsAsTheBuildsDo: a release, a
// build after it, a pre-release, a dirty tree and a tree with no tag. If it
// regresses, dpkg refuses the package's version, or apt takes a build from
// before a release for an upgrade over it.
func TestGitDescribeBecomesADebianVersionThatSortsAsTheBuildsDo(t *testing.T) {
	for describe, want := range map[string]string{
		"v0.18.0":                   "0.18.0",
		"v0.18.0-17-gfb2cbbb":       "0.18.0+git17.gfb2cbbb",
		"v0.18.0-17-gfb2cbbb-dirty": "0.18.0+git17.gfb2cbbb+dirty",
		"v0.19.0-rc1":               "0.19.0~rc1",
		"v0.19.0-rc1-2-gabc1234":    "0.19.0~rc1+git2.gabc1234",
		"fb2cbbb":                   "0~gfb2cbbb",
		"fb2cbbb-dirty":             "0~gfb2cbbb+dirty",
	} {
		got, err := DebVersion(describe)
		if err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", describe, got, err, want)
		}
	}
	for _, bad := range []string{"", "unknown", "v0.18.0 rm"} {
		if v, err := DebVersion(bad); err == nil {
			t.Errorf("%q was taken, as %q", bad, v)
		}
	}
	// dpkg's own ordering, where dpkg is here: the order the builds were
	// made in is the order apt upgrades in.
	if _, err := exec.LookPath("dpkg"); err == nil {
		order := []string{"0~gfb2cbbb", "0.19.0~rc1", "0.19.0~rc1+git2.gabc1234", "0.19.0", "0.19.0+git1.gabc"}
		for i := 1; i < len(order); i++ {
			if err := exec.Command("dpkg", "--compare-versions", order[i-1], "lt", order[i]).Run(); err != nil {
				t.Errorf("dpkg does not sort %s before %s", order[i-1], order[i])
			}
		}
	}
}

// TestThePackagesCarryTheirFilesAndDependOnTheRightThings: the control file
// of each package and, where dpkg-deb is installed, the .deb it builds. If
// it regresses, tend-desktop installs without the QML modules it cannot
// start without, or with a tend of another version than its own, or the
// launcher entry is missing.
func TestThePackagesCarryTheirFilesAndDependOnTheRightThings(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb is not installed")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tend", "tend-desktop", "tend-whatsapp"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, _ := filepath.Abs("../..")
	b := builder{arch: "amd64", out: filepath.Join(dir, "out"), root: root, maintainer: "Ana <ana@example.com>",
		shlibdeps: func(string) (string, error) { return "libqt6quick6 (>= 6.6.0), libc6 (>= 2.34)", nil }}
	if err := b.run("v0.18.0-3-gabc1234", filepath.Join(bin, "tend"), filepath.Join(bin, "tend-desktop"), filepath.Join(bin, "tend-whatsapp")); err != nil {
		t.Fatal(err)
	}
	inspect := func(deb string, args ...string) string {
		out, err := exec.Command("dpkg-deb", append(args, filepath.Join(b.out, deb))...).CombinedOutput()
		if err != nil {
			t.Fatalf("dpkg-deb %v %s: %v\n%s", args, deb, err, out)
		}
		return string(out)
	}
	desktop := inspect("tend-desktop_0.18.0+git3.gabc1234_amd64.deb", "-f")
	for _, want := range []string{
		"Depends: tend (= 0.18.0+git3.gabc1234), libc6 (>= 2.34), libqt6quick6 (>= 6.6.0), qml6-module-qtqml,",
		"qml6-module-qtquick-window", "Recommends: qml6-module-qtwebengine, qt6-wayland",
	} {
		if !strings.Contains(desktop, want) {
			t.Errorf("tend-desktop's control lacks %q:\n%s", want, desktop)
		}
	}
	files := inspect("tend-desktop_0.18.0+git3.gabc1234_amd64.deb", "-c")
	for _, want := range []string{"-rwxr-xr-x root/root", "./usr/bin/tend-desktop", "./usr/share/applications/tend-desktop.desktop", "./usr/share/doc/tend-desktop/copyright"} {
		if !strings.Contains(files, want) {
			t.Errorf("tend-desktop's files lack %q:\n%s", want, files)
		}
	}
	files = inspect("tend_0.18.0+git3.gabc1234_amd64.deb", "-c")
	for _, want := range []string{"./usr/bin/tend", "./usr/share/applications/tend.desktop", "./usr/share/icons/hicolor/scalable/apps/tend.svg"} {
		if !strings.Contains(files, want) {
			t.Errorf("tend's files lack %q:\n%s", want, files)
		}
	}
	if control := inspect("tend_0.18.0+git3.gabc1234_amd64.deb", "-f"); strings.Contains(control, "Depends:") {
		t.Errorf("tend is static and depends on nothing:\n%s", control)
	}
	if control := inspect("tend-whatsapp_0.18.0+git3.gabc1234_amd64.deb", "-f"); !strings.Contains(control, "Depends: tend (= 0.18.0+git3.gabc1234)\n") {
		t.Errorf("tend-whatsapp's control:\n%s", control)
	}
}
