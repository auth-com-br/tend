// Command appimage makes tend-desktop an AppImage (#75): one file that runs
// on a Linux without Qt installed, because the Qt it was built against is
// inside it.
//
//	go run ./tools/appimage -version v0.18.0 -binary desktop/bin/tend-desktop-appimage -out dist
//
// It assembles the AppDir itself — the binary, the Qt libraries it loads,
// the Qt plugins and QML modules it uses, the launcher entry and the icon —
// and hands it to appimagetool, the AppImage project's packer, which is the
// one thing here not on an Ubuntu machine and must be downloaded. Without
// it the AppDir is still built and runs as it is (its AppRun), which is how
// it was checked where this was written.
//
// What it does not do, on purpose:
//
//   - It does not bundle tend. tend-desktop starts each session's server
//     with the tend beside it; a tend inside the AppImage would be a server
//     running from a mount that disappears when the window closes. The
//     AppImage uses the tend on PATH, which `tend update` keeps current.
//   - It does not set LD_LIBRARY_PATH. The binary is linked with an rpath to
//     its lib/ (`make appimage` does that), because every program the
//     window starts would inherit the variable: the server, and through it
//     the shells and agents in the panes.
//   - It does not bundle QtWebEngine. The browser tab is optional and
//     QtWebEngine is a Chromium, hundreds of megabytes; in the AppImage the
//     tab opens only where the system has qml6-module-qtwebengine.
//   - It does not bundle glibc, the C++ runtime or the graphics stack (see
//     excluded): those must be the host's. So the AppImage runs on systems
//     with a glibc at least as new as the builder's — build it on the
//     oldest distribution it should run on.
package main

import (
	"debug/elf"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/auth-com-br/tend/internal/launcher"
)

func main() {
	version := flag.String("version", "", "git describe of what was built, for the file's name")
	binary := flag.String("binary", "", "tend-desktop, linked with -rpath '$ORIGIN/../lib' (make appimage)")
	out := flag.String("out", "dist", "where the AppDir and the AppImage go")
	tool := flag.String("appimagetool", "appimagetool", "the appimagetool to pack the AppDir with")
	flag.Parse()
	if err := run(*version, *binary, *out, *tool); err != nil {
		fmt.Fprintln(os.Stderr, "appimage:", err)
		os.Exit(1)
	}
}

// errNoTool is the AppDir built and nothing to pack it with.
var errNoTool = errors.New("appimagetool not found")

func run(version, binary, out, tool string) error {
	if version == "" || binary == "" {
		return errors.New("usage: appimage -version v0.18.0 -binary tend-desktop [-out dist]")
	}
	if err := hasRpath(binary); err != nil {
		return err
	}
	qt, err := qtPaths()
	if err != nil {
		return err
	}
	appDir := filepath.Join(out, "tend-desktop.AppDir")
	if err := os.RemoveAll(appDir); err != nil {
		return err
	}
	usr := filepath.Join(appDir, "usr")
	if err := copyFile(binary, filepath.Join(usr, "bin", "tend-desktop"), 0o755); err != nil {
		return err
	}
	elves := []string{filepath.Join(usr, "bin", "tend-desktop")}
	for _, p := range plugins {
		matches, _ := filepath.Glob(filepath.Join(qt.plugins, p))
		for _, m := range matches {
			dst := filepath.Join(usr, "plugins", strings.TrimPrefix(m, qt.plugins+"/"))
			if err := copyFile(m, dst, 0o755); err != nil {
				return err
			}
			elves = append(elves, dst)
		}
	}
	for _, module := range qmlModules {
		got, err := copyModule(filepath.Join(qt.qml, module), filepath.Join(usr, "qml", module))
		if err != nil {
			return err
		}
		elves = append(elves, got...)
	}
	libs, err := closure(elves)
	if err != nil {
		return err
	}
	for name, from := range libs {
		if err := copyFile(from, filepath.Join(usr, "lib", name), 0o755); err != nil {
			return err
		}
	}
	// qt.conf beside the binary is where Qt looks for its plugins and QML
	// modules before its compiled-in /usr/lib paths, which inside an
	// AppImage would be the host's Qt of another version.
	conf := "[Paths]\nPrefix=..\nPlugins=plugins\nQmlImports=qml\nQml2Imports=qml\n"
	if err := os.WriteFile(filepath.Join(usr, "bin", "qt.conf"), []byte(conf), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "tend-desktop.desktop"), []byte(launcher.WindowEntry("tend-desktop", "")), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "tend.svg"), launcher.Icon(), 0o644); err != nil {
		return err
	}
	if err := os.Symlink("tend.svg", filepath.Join(appDir, ".DirIcon")); err != nil {
		return err
	}
	// AppRun is the binary itself: a link, so /proc/self/exe — and with it
	// Qt's application directory, where qt.conf is — is usr/bin.
	if err := os.Symlink("usr/bin/tend-desktop", filepath.Join(appDir, "AppRun")); err != nil {
		return err
	}
	fmt.Printf("%s: tend-desktop, %d libraries, %d plugins and QML files\n", appDir, len(libs), len(elves)-1)

	path, err := exec.LookPath(tool)
	if err != nil {
		return fmt.Errorf("%w (%s): the AppDir above runs as it is (%s/AppRun); get appimagetool from the AppImage project and run make appimage APPIMAGETOOL=/path/to/it", errNoTool, tool, appDir)
	}
	arch := appImageArch(runtime.GOARCH)
	image := filepath.Join(out, fmt.Sprintf("tend-desktop-%s-%s.AppImage", strings.TrimPrefix(version, "v"), arch))
	cmd := exec.Command(path, appDir, image)
	cmd.Env = append(os.Environ(), "ARCH="+arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("appimagetool: %w", err)
	}
	fmt.Println(image)
	return nil
}

// plugins are the Qt plugins the window can use, as globs under Qt's
// plugin directory: the X11, Wayland and offscreen platforms and what each
// needs to draw, input methods (a compose key, ibus), and the image formats
// and icon engines Qt Quick reads. A missing one is skipped: not every Qt
// build has Wayland.
var plugins = []string{
	"platforms/libqxcb.so",
	"platforms/libqwayland*.so",
	"platforms/libqoffscreen.so",
	"xcbglintegrations/*.so",
	"wayland-shell-integration/*.so",
	"wayland-graphics-integration-client/*.so",
	"wayland-decoration-client/*.so",
	"platforminputcontexts/*.so",
	"imageformats/*.so",
	"iconengines/*.so",
}

// qmlModules are the QML modules tend-desktop loads (the same list as
// tools/deb's desktopQML, by directory). Each is copied without the modules
// nested in its directory — QtQuick's holds Controls and the rest, which
// the window does not import — so those it does use are listed on their own.
var qmlModules = []string{"QtQml", "QtQml/Models", "QtQml/WorkerScript", "QtQuick", "QtQuick/Window"}

type qtDirs struct{ plugins, qml string }

// qtPaths asks Qt where its plugins and QML modules are, as Qt's own
// deployment tools do.
func qtPaths() (qtDirs, error) {
	query := func(key string) (string, error) {
		for _, tool := range []string{"qtpaths6", "qtpaths", "qmake6"} {
			if out, err := exec.Command(tool, "-query", key).Output(); err == nil {
				return strings.TrimSpace(string(out)), nil
			}
		}
		return "", errors.New("neither qtpaths6 nor qmake6 answers: is Qt 6 installed (qt6-base-dev)?")
	}
	var d qtDirs
	var err error
	if d.plugins, err = query("QT_INSTALL_PLUGINS"); err != nil {
		return d, err
	}
	d.qml, err = query("QT_INSTALL_QML")
	return d, err
}

// hasRpath refuses a binary that would load the host's Qt instead of its
// own: one not linked by `make appimage`.
func hasRpath(binary string) error {
	f, err := elf.Open(binary)
	if err != nil {
		return err
	}
	defer f.Close()
	paths, _ := f.DynString(elf.DT_RPATH)
	for _, p := range paths {
		if strings.Contains(p, "$ORIGIN/../lib") {
			return nil
		}
	}
	return fmt.Errorf("%s has no DT_RPATH $ORIGIN/../lib: build it with make appimage, which links it so", binary)
}

// copyModule copies one QML module's files, not the modules nested in it,
// and says which of them are libraries.
func copyModule(from, to string) ([]string, error) {
	entries, err := os.ReadDir(from)
	if err != nil {
		return nil, fmt.Errorf("QML module %s: %w", from, err)
	}
	var elves []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dst := filepath.Join(to, e.Name())
		if err := copyFile(filepath.Join(from, e.Name()), dst, 0o644); err != nil {
			return nil, err
		}
		if strings.HasSuffix(e.Name(), ".so") {
			if err := os.Chmod(dst, 0o755); err != nil {
				return nil, err
			}
			elves = append(elves, dst)
		}
	}
	return elves, nil
}

// lddLine is one of ldd's "libfoo.so.1 => /usr/lib/…/libfoo.so.1 (0x…)".
var lddLine = regexp.MustCompile(`^\s*(\S+) => (/\S+) \(0x[0-9a-f]+\)$`)

// closure is every library the ELF files load, by the name they ask for it
// by, less those that must be the host's. ldd answers with the whole tree,
// so one call per file is the closure.
func closure(elves []string) (map[string]string, error) {
	libs := map[string]string{}
	for _, e := range elves {
		out, err := exec.Command("ldd", e).Output()
		if err != nil {
			return nil, fmt.Errorf("ldd %s: %w", e, err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			m := lddLine.FindStringSubmatch(line)
			if m == nil || excluded(m[1]) {
				continue
			}
			libs[m[1]] = m[2]
		}
	}
	return libs, nil
}

// hostLibs are the libraries an AppImage leaves to the host, from the
// AppImage project's excludelist: the C library and its parts, which must
// match the dynamic loader; the C++ runtime, which the graphics drivers
// load too; and the graphics stack, which is the host's drivers'. Bundling
// any of them breaks the AppImage on the machines it is for.
var hostLibs = regexp.MustCompile(`^(ld-linux.*|lib(c|m|dl|rt|pthread|resolv|util|anl|nsl|mvec|thread_db|BrokenLocale|nss_.*)\.so\..*` +
	`|lib(stdc\+\+|gcc_s)\.so\..*` +
	`|lib(GL|EGL|GLX|GLdispatch|OpenGL|GLESv2|drm|gbm)\.so\..*` +
	`|lib(X11|X11-xcb|xcb|xcb-dri2|xcb-dri3|xcb-present|xcb-sync|xshmfence)\.so\..*` +
	`|lib(wayland-client|wayland-server|wayland-egl|wayland-cursor)\.so\..*` +
	`|lib(fontconfig|freetype|harfbuzz|expat|z|asound|gpg-error|com_err)\.so\..*)$`)

func excluded(soname string) bool { return hostLibs.MatchString(soname) }

// appImageArch is Go's architecture as AppImage file names spell it.
func appImageArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	}
	return goarch
}

// copyFile copies the bytes behind a path — a library's link is followed —
// to dst, creating its directory.
func copyFile(from, to string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
