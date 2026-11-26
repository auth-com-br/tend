// Command deb writes the Debian packages of tend's three programs (#75):
// tend, tend-desktop and tend-whatsapp, each from a binary already built,
// with dpkg-deb and nothing from the network.
//
//	go run ./tools/deb -version v0.18.0 -out dist \
//	    -tend bin/tend -desktop desktop/bin/tend-desktop -whatsapp whatsapp/bin/tend-whatsapp
//
// A program left out (-desktop "", for a machine without Qt) is a package
// not written. `make deb` builds the binaries and runs this.
//
// tend and tend-whatsapp are static and depend on nothing. tend-desktop is
// linked against the Qt 6 of the machine that built it, so its Depends are
// what dpkg-shlibdeps reads from the binary — the same tool Debian's own
// packages are built with — plus the QML modules it imports, which no
// linker sees: without them the window fails at start with "module
// QtQuick is not installed". The browser tab's QtWebEngine is a Recommends,
// since tend-desktop loads it only when a browser tab opens.
//
// Both other packages depend on tend of the same version: tend-desktop
// starts every session's server with the tend beside it (both are in
// /usr/bin), and tend-whatsapp is run by `tend whatsapp` and talks to the
// session the same way.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/auth-com-br/tend/internal/launcher"
)

func main() {
	version := flag.String("version", "", "git describe of what was built: v0.18.0, or v0.18.0-17-gfb2cbbb")
	arch := flag.String("arch", debArch(runtime.GOARCH), "the Debian architecture of the binaries")
	out := flag.String("out", "dist", "where the .deb files go")
	root := flag.String("root", ".", "the repository, for LICENSE and NOTICE")
	maintainer := flag.String("maintainer", "Auth Tecnologia Ltda <seguranca@auth.com.br>", "the packages' Maintainer field")
	tend := flag.String("tend", "", "the tend binary (static); empty skips its package")
	desktop := flag.String("desktop", "", "the tend-desktop binary; empty skips its package")
	whatsapp := flag.String("whatsapp", "", "the tend-whatsapp binary (static); empty skips its package")
	flag.Parse()
	b := builder{arch: *arch, out: *out, root: *root, maintainer: *maintainer}
	if err := b.run(*version, *tend, *desktop, *whatsapp); err != nil {
		fmt.Fprintln(os.Stderr, "deb:", err)
		os.Exit(1)
	}
}

// Paths the packages install to. /usr/bin, not /usr/local/bin: /usr/local
// is the administrator's, and a package's files belong under /usr.
const (
	binDir  = "/usr/bin"
	appsDir = "/usr/share/applications"
	iconSVG = "/usr/share/icons/hicolor/scalable/apps/tend.svg"
)

// desktopQML are the QML modules tend-desktop loads, found by running it
// with QML_IMPORT_TRACE=1 against Ubuntu 26.04's Qt 6.10 and asking dpkg
// -S whose each directory is: QtQuick and QtQuick.Window are imported by
// main.qml, and QtQml, QtQml.Models and QtQml.WorkerScript come with them.
// A new import in desktop/cmd/tend-desktop/qml belongs here too.
var desktopQML = []string{
	"qml6-module-qtqml",
	"qml6-module-qtqml-models",
	"qml6-module-qtqml-workerscript",
	"qml6-module-qtquick",
	"qml6-module-qtquick-window",
}

// desktopRecommends are what makes the window whole but not start:
// QtWebEngine for the browser tab (loaded only when one opens), and Qt's
// Wayland platform, without which Qt draws through XWayland on a Wayland
// desktop — it works, scaled and blurrier.
var desktopRecommends = []string{"qml6-module-qtwebengine", "qt6-wayland"}

type builder struct {
	arch, out, root, maintainer string
	// shlibdeps reads a binary's library dependencies; a variable so the
	// test does not need dpkg-dev.
	shlibdeps func(binary string) (string, error)
}

// pkg is one package: its control fields and the files it installs.
type pkg struct {
	name, summary, about string
	depends, recommends  []string
	files                []file
}

type file struct {
	path string // where it is installed, absolute
	from string // a file to copy, or
	data []byte // the contents
	mode fs.FileMode
}

func (b builder) run(describe, tend, desktop, whatsapp string) error {
	version, err := DebVersion(describe)
	if err != nil {
		return err
	}
	if tend == "" && desktop == "" && whatsapp == "" {
		return errors.New("no binaries given: -tend, -desktop, -whatsapp")
	}
	if b.shlibdeps == nil {
		b.shlibdeps = shlibdeps
	}
	same := fmt.Sprintf("tend (= %s)", version)
	var pkgs []pkg
	if tend != "" {
		pkgs = append(pkgs, pkg{
			name:    "tend",
			summary: "terminal runtime for AI coding agents",
			about: "tend runs AI coding agents (Claude Code, Codex and others) in panes\n" +
				"and tabs of one terminal, and shows which of them is working, which is\n" +
				"waiting for you and which has finished.",
			files: []file{
				{path: binDir + "/tend", from: tend, mode: 0o755},
				{path: appsDir + "/tend.desktop", data: []byte(launcher.Entry(binDir+"/tend", "")), mode: 0o644},
				{path: iconSVG, data: launcher.Icon(), mode: 0o644},
			},
		})
	}
	if desktop != "" {
		libs, err := b.shlibdeps(desktop)
		if err != nil {
			return err
		}
		depends := append([]string{same}, splitDepends(libs)...)
		pkgs = append(pkgs, pkg{
			name:    "tend-desktop",
			summary: "tend in a window",
			about: "tend-desktop is a native Qt window on a tend session: the same panes,\n" +
				"tabs and agents as the terminal tend, drawn by Qt Quick.",
			depends:    append(depends, desktopQML...),
			recommends: desktopRecommends,
			files: []file{
				{path: binDir + "/tend-desktop", from: desktop, mode: 0o755},
				{path: appsDir + "/tend-desktop.desktop", data: []byte(launcher.WindowEntry(binDir+"/tend-desktop", "")), mode: 0o644},
			},
		})
	}
	if whatsapp != "" {
		pkgs = append(pkgs, pkg{
			name:    "tend-whatsapp",
			summary: "WhatsApp for tend's agents",
			about: "tend-whatsapp links the owner's WhatsApp to a tend session, so agents\n" +
				"can be followed and answered from the phone. `tend whatsapp` runs it.",
			depends: []string{same},
			files:   []file{{path: binDir + "/tend-whatsapp", from: whatsapp, mode: 0o755}},
		})
	}
	if err := os.MkdirAll(b.out, 0o755); err != nil {
		return err
	}
	for _, p := range pkgs {
		deb, err := b.build(p, version)
		if err != nil {
			return fmt.Errorf("%s: %w", p.name, err)
		}
		fmt.Println(deb)
	}
	return nil
}

// build stages p in a temporary tree and has dpkg-deb make the .deb of it.
func (b builder) build(p pkg, version string) (string, error) {
	stage, err := os.MkdirTemp("", "tend-deb-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	// The tree's root is the package's /: MkdirTemp's 0700 would be
	// carried into the .deb as the mode of ./.
	if err := os.Chmod(stage, 0o755); err != nil {
		return "", err
	}
	copyright, err := b.copyright()
	if err != nil {
		return "", err
	}
	files := append(p.files, file{path: "/usr/share/doc/" + p.name + "/copyright", data: copyright, mode: 0o644})
	var size int64
	for _, f := range files {
		n, err := place(stage, f)
		if err != nil {
			return "", err
		}
		size += n
	}
	if err := os.MkdirAll(filepath.Join(stage, "DEBIAN"), 0o755); err != nil {
		return "", err
	}
	control := Control(p, version, b.arch, b.maintainer, size)
	if err := os.WriteFile(filepath.Join(stage, "DEBIAN", "control"), []byte(control), 0o644); err != nil {
		return "", err
	}
	deb := filepath.Join(b.out, fmt.Sprintf("%s_%s_%s.deb", p.name, version, b.arch))
	// --root-owner-group: the files are root's in the package whoever
	// built it, with no fakeroot. xz is what Debian's own packages use and
	// every dpkg since 1.15 reads.
	cmd := exec.Command("dpkg-deb", "--root-owner-group", "-Zxz", "--build", stage, deb)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("dpkg-deb: %v\n%s", err, out)
	}
	return deb, nil
}

// place writes f under stage with its mode set explicitly — the umask of
// whoever runs `make deb` is not what the package should carry — and says
// how many bytes it is.
func place(stage string, f file) (int64, error) {
	dst := filepath.Join(stage, filepath.FromSlash(f.path))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	data := f.data
	if f.from != "" {
		var err error
		if data, err = os.ReadFile(f.from); err != nil {
			return 0, err
		}
	}
	if err := os.WriteFile(dst, data, f.mode); err != nil {
		return 0, err
	}
	return int64(len(data)), os.Chmod(dst, f.mode)
}

// copyright is the package's /usr/share/doc/<name>/copyright, which Debian
// requires of every package: NOTICE, which attributes the code tend is
// derived from, then the licence.
func (b builder) copyright() ([]byte, error) {
	notice, err := os.ReadFile(filepath.Join(b.root, "NOTICE"))
	if err != nil {
		return nil, err
	}
	license, err := os.ReadFile(filepath.Join(b.root, "LICENSE"))
	if err != nil {
		return nil, err
	}
	return []byte(string(notice) + "\n" + string(license)), nil
}

// Control is the package's DEBIAN/control. Installed-Size is in KiB, as
// dpkg counts it; the long description has each line indented by one
// space, and a blank line written as " .".
func Control(p pkg, version, arch, maintainer string, size int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Package: %s\nVersion: %s\nArchitecture: %s\nMaintainer: %s\n", p.name, version, arch, maintainer)
	fmt.Fprintf(&b, "Installed-Size: %d\nSection: devel\nPriority: optional\nHomepage: https://tend.auth.com.br\n", (size+1023)/1024)
	if len(p.depends) > 0 {
		fmt.Fprintf(&b, "Depends: %s\n", strings.Join(p.depends, ", "))
	}
	if len(p.recommends) > 0 {
		fmt.Fprintf(&b, "Recommends: %s\n", strings.Join(p.recommends, ", "))
	}
	fmt.Fprintf(&b, "Description: %s\n", p.summary)
	for _, line := range strings.Split(p.about, "\n") {
		if line == "" {
			line = "."
		}
		fmt.Fprintf(&b, " %s\n", line)
	}
	return b.String()
}

// describeRe is git describe's output: a tag, then optionally the commits
// since it and the commit, then optionally -dirty.
var describeRe = regexp.MustCompile(`^v?([0-9][0-9A-Za-z.-]*?)(?:-([0-9]+)-g([0-9a-f]+))?(-dirty)?$`)

// debVersionRe is what dpkg takes as a version with no epoch or revision.
var debVersionRe = regexp.MustCompile(`^[0-9][A-Za-z0-9.+~]*$`)

// DebVersion is git describe made a Debian version that sorts as the
// builds do: v0.18.0 is 0.18.0; seventeen commits after it, 0.18.0+git17.
// gfb2cbbb, which is newer than 0.18.0 and older than 0.18.1; a tag's
// pre-release part (v0.19.0-rc1) becomes ~rc1, which sorts before 0.19.0;
// and a tree with no tag at all, only a commit, is 0~gfb2cbbb, older than
// any release.
func DebVersion(describe string) (string, error) {
	describe = strings.TrimSpace(describe)
	if regexp.MustCompile(`^[0-9a-f]{7,40}(-dirty)?$`).MatchString(describe) {
		return "0~g" + strings.Replace(describe, "-dirty", "+dirty", 1), nil
	}
	m := describeRe.FindStringSubmatch(describe)
	if m == nil {
		return "", fmt.Errorf("-version %q is not git describe's (v0.18.0, v0.18.0-17-gfb2cbbb)", describe)
	}
	v := strings.ReplaceAll(m[1], "-", "~")
	if m[2] != "" {
		v += "+git" + m[2] + ".g" + m[3]
	}
	if m[4] != "" {
		v += "+dirty"
	}
	if !debVersionRe.MatchString(v) {
		return "", fmt.Errorf("-version %q makes %q, which is not a Debian version", describe, v)
	}
	return v, nil
}

// debArch is Go's name for an architecture as Debian names it.
func debArch(goarch string) string {
	switch goarch {
	case "386":
		return "i386"
	case "arm":
		return "armhf"
	}
	return goarch
}

// shlibdeps is dpkg-shlibdeps's Depends for binary. It insists on running
// from a source package's directory, so it is given a scratch one holding
// only the debian/control it reads.
func shlibdeps(binary string) (string, error) {
	tool, err := exec.LookPath("dpkg-shlibdeps")
	if err != nil {
		return "", errors.New("dpkg-shlibdeps is not installed (apt install dpkg-dev): it is what reads tend-desktop's Qt dependencies")
	}
	abs, err := filepath.Abs(binary)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "tend-shlibdeps-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := os.Mkdir(filepath.Join(dir, "debian"), 0o755); err != nil {
		return "", err
	}
	control := "Source: tend\n\nPackage: tend-desktop\nArchitecture: any\n"
	if err := os.WriteFile(filepath.Join(dir, "debian", "control"), []byte(control), 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command(tool, "-O", abs)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("dpkg-shlibdeps: %v\n%s", err, stderr.String())
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "shlibs:Depends="); ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("dpkg-shlibdeps printed no shlibs:Depends for %s", binary)
}

// splitDepends is a Depends value as its entries, sorted so the control
// file is the same from one build to the next.
func splitDepends(v string) []string {
	var out []string
	for _, d := range strings.Split(v, ",") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}
