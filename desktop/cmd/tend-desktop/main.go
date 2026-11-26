// tend-desktop is tend in a window: a Qt Quick client of the same session
// server the terminal tend runs, so the two can be open on one session at
// once. It is built apart from tend (desktop/go.mod), because it needs cgo
// and Qt and the terminal tend must stay pure Go. See docs/DESKTOP.md.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
	"github.com/mappu/miqt/qt6/qml"

	"github.com/auth-com-br/tend/desktop/internal/core"
	"github.com/auth-com-br/tend/desktop/internal/qtterm"
)

//go:embed qml/main.qml
var mainQML []byte

// Qt wants its objects made and used on the thread that made the
// application, and Go moves goroutines between threads; locking main to the
// first thread keeps Qt's main thread the one main runs on.
func init() { runtime.LockOSThread() }

func main() {
	session := flag.String("s", "default", "the session to open")
	tendBin := flag.String("tend", "", "the tend binary to start a session's server with (default: beside this one, else on PATH)")
	shot := flag.String("shot", "", "save the window as a PNG at this path a moment after it opens, and quit (for checking it without looking)")
	flag.Parse()
	debug = os.Getenv("TEND_DESKTOP_DEBUG") != ""

	tendPath, err := core.FindTend(*tendBin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tend-desktop:", err)
		os.Exit(1)
	}

	qt.NewQApplication(os.Args)
	qtterm.Register()

	sess, err := core.Open(*session, tendPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tend-desktop:", err)
		os.Exit(1)
	}
	defer sess.Close()

	// The window, once the QML is loaded. Everything that reports to Go is
	// wired up before that, not after: on Wayland the Terminal item is given
	// its size while the QML loads, and a size reported to a handler not yet
	// set was lost — the pane was never sized or subscribed to, and the
	// window stayed empty with nothing reaching it from the keyboard either.
	// Offscreen the size came again later, which is why it worked there.
	var root *qt.QObject
	qtterm.Handlers.Key = func(pane uint64, key, mods int, text string) {
		logf("key pane=%d key=%#x mods=%#x text=%q", pane, key, mods, text)
		go func() {
			if err := sess.Key(key, mods, text); err != nil {
				setStatus(root, err.Error())
			}
		}()
	}
	qtterm.Handlers.Resize = func(pane uint64, cols, rows int) {
		logf("resize pane=%d %dx%d", pane, cols, rows)
		go func() {
			if err := sess.Resize(cols, rows); err != nil {
				setStatus(root, err.Error())
			}
		}()
	}

	// Repaints are coalesced: output arriving while one is waiting for the
	// main thread is drawn by that one, so a burst of output from a pane is
	// one paint rather than a queue of them the window falls behind on.
	var pending atomic.Bool
	var buf []core.Cell
	shown := sess.Pane()
	sess.Changed = func() {
		if !pending.CompareAndSwap(false, true) {
			return
		}
		mainthread.Start(func() {
			pending.Store(false)
			pane := sess.Pane()
			if pane != shown && root != nil {
				shown = pane
				root.SetProperty("pane", qt.NewQVariant6(int64(pane)))
			}
			f, ok := sess.Frame(buf)
			if !ok {
				logf("paint: nothing to show yet for pane %d", pane)
				return
			}
			logf("paint pane=%d %dx%d", pane, f.Cols, f.Rows)
			buf = f.Cells
			qtterm.Set(pane, f)
		})
	}
	sess.Status = func(msg string) { setStatus(root, msg) }

	engine := qml.NewQQmlApplicationEngine()
	engine.SetInitialProperties(map[string]qt.QVariant{
		"session": *qt.NewQVariant14(*session),
		"pane":    *qt.NewQVariant6(int64(sess.Pane())),
	})
	engine.LoadData(mainQML)
	roots := engine.RootObjects()
	if len(roots) == 0 {
		fmt.Fprintln(os.Stderr, "tend-desktop: the window's QML did not load")
		os.Exit(1)
	}
	root = roots[0]
	logf("window loaded, showing pane %d", sess.Pane())
	// The first frame: what arrived before the window was there to take it.
	sess.Changed()

	if *shot != "" {
		go func() {
			time.Sleep(3 * time.Second)
			mainthread.Start(func() {
				if err := qtterm.Grab(*shot); err != nil {
					fmt.Fprintln(os.Stderr, "tend-desktop:", err)
				}
				qt.QCoreApplication_Quit()
			})
		}()
	}

	qt.QApplication_Exec()
}

// debug is TEND_DESKTOP_DEBUG set: what the window is told and does, on
// standard error.
var debug bool

func logf(format string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stderr, "tend-desktop: "+format+"\n", args...)
	}
}

// setStatus shows a line at the bottom of the window. Safe from any goroutine.
// A line before the window exists goes to standard error instead.
func setStatus(root *qt.QObject, msg string) {
	if root == nil {
		fmt.Fprintln(os.Stderr, "tend-desktop:", msg)
		return
	}
	mainthread.Start(func() { root.SetProperty("status", qt.NewQVariant14(msg)) })
}
