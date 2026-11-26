// tend-desktop is tend in a window: a Qt Quick client of the same session
// server the terminal tend runs, so the two can be open on one session at
// once. It is built apart from tend (desktop/go.mod), because it needs cgo
// and Qt and the terminal tend must stay pure Go. See docs/DESKTOP.md.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
	"github.com/mappu/miqt/qt6/qml"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/ui"

	"github.com/auth-com-br/tend/desktop/internal/core"
	"github.com/auth-com-br/tend/desktop/internal/qtterm"
)

//go:embed qml/main.qml
var mainQML []byte

//go:embed qml/Browser.qml
var browserQML []byte

// Qt wants its objects made and used on the thread that made the
// application, and Go moves goroutines between threads; locking main to the
// first thread keeps Qt's main thread the one main runs on.
func init() { runtime.LockOSThread() }

func main() {
	session := flag.String("s", "default", "the session to open")
	tendBin := flag.String("tend", "", "the tend binary to start a session's server with (default: beside this one, else on PATH)")
	shot := flag.String("shot", "", "save the window as a PNG at this path a moment after it opens, and quit (for checking it without looking)")
	startURL := flag.String("url", "", "open this page in a browser tab when the window opens")
	shotCall := flag.String("shot-call", "", "with -shot: a Bridge call made first, as 'method {json args}', to capture what it opens")
	shotOpen := flag.String("shot-open", "", "with -shot: the objectName of an item whose open property is set first, to capture a menu")
	flag.Parse()
	debug = os.Getenv("TEND_DESKTOP_DEBUG") != ""

	tendPath, err := core.FindTend(*tendBin)
	if err != nil {
		fail(err)
	}
	// The terminal tend's settings: the same sidebar rows, sort and
	// indicators. A file with a mistake in it still opens the window, with
	// what could be read, as the TUI does.
	cfg, err := config.LoadLenient()
	if err != nil {
		logf("settings: %v", err)
	}

	// QtWebEngine shares OpenGL contexts with the window, and asks for it
	// before the application exists (what QtWebEngineQuick::initialize
	// does); set here, it costs nothing until a browser tab is opened.
	qt.QCoreApplication_SetAttribute2(qt.AA_ShareOpenGLContexts, true)
	qt.NewQApplication(os.Args)
	qtterm.Register()
	qtterm.SetBrowserQML(browserQML)
	qtterm.SetStartURL(*startURL)

	sess, err := core.Open(*session, tendPath, cfg)
	if err != nil {
		fail(err)
	}
	defer sess.Close()

	// Everything that reports to Go is wired up before the QML is loaded,
	// not after: on Wayland the items are given their size while the QML
	// loads, and a size reported to a handler not yet set was lost — the
	// pane was never sized or subscribed to, and the window stayed empty.
	qtterm.Handlers.Key = func(pane uint64, key, mods int, text string) {
		logf("key pane=%d key=%#x mods=%#x text=%q", pane, key, mods, text)
		background(func() error { return sess.Key(pane, key, mods, text) })
	}
	qtterm.Handlers.Resize = func(pane uint64, cols, rows int) {
		logf("resize pane=%d %dx%d", pane, cols, rows)
		background(func() error { return sess.ResizePane(pane, cols, rows) })
	}
	qtterm.Handlers.Mouse = func(pane uint64, kind, button, x, y, mods int) {
		ev := ui.MouseEvent{Kind: ui.MouseKind(kind), Button: button, X: x, Y: y, Mods: ui.MouseMod(mods)}
		background(func() error { return sess.Mouse(pane, ev) })
	}
	qtterm.Handlers.Paste = func(pane uint64, text string) {
		background(func() error { return sess.Paste(pane, text) })
	}
	sess.ClipboardText = func() string {
		return mainthread.Wait2(func() string { return qt.QGuiApplication_Clipboard().Text() })
	}
	sess.Copied = func(text string) {
		mainthread.Start(func() { qtterm.SetClipboard(text) })
	}
	qtterm.Handlers.Call = func(method string, args []byte) {
		logf("call %s %s", method, args)
		background(func() error { return dispatch(sess, method, args) })
	}

	// Paints and state are coalesced: what changes while one is waiting for
	// the main thread is taken by that one, so a burst of output is one
	// paint per pane rather than a queue the window falls behind on.
	var (
		dirtyMu  sync.Mutex
		dirty    = map[uint64]bool{}
		pending  atomic.Bool
		stateDue atomic.Bool
		buf      []core.Cell
	)
	flush := func() {
		pending.Store(false)
		if stateDue.Swap(false) {
			qtterm.SetState(sess.StateJSON())
		}
		dirtyMu.Lock()
		panes := make([]uint64, 0, len(dirty))
		for p := range dirty {
			panes = append(panes, p)
		}
		clear(dirty)
		dirtyMu.Unlock()
		for _, p := range panes {
			f, ok := sess.Frame(p, buf)
			if !ok {
				continue
			}
			buf = f.Cells
			qtterm.Set(p, f)
		}
	}
	schedule := func() {
		if pending.CompareAndSwap(false, true) {
			mainthread.Start(flush)
		}
	}
	sess.PaneChanged = func(pane uint64) {
		dirtyMu.Lock()
		dirty[pane] = true
		dirtyMu.Unlock()
		schedule()
	}
	sess.StateChanged = func() {
		stateDue.Store(true)
		// A new layout shows panes whose items are new: give them what
		// their panes show now.
		dirtyMu.Lock()
		for _, p := range sess.State().Panes {
			dirty[p.Pane] = true
		}
		dirtyMu.Unlock()
		schedule()
	}
	sess.Status = func(msg string) { setStatus(msg) }

	engine := qml.NewQQmlApplicationEngine()
	qtterm.SetState(sess.StateJSON())
	engine.LoadData(mainQML)
	if len(engine.RootObjects()) == 0 {
		fail(fmt.Errorf("the window's QML did not load"))
	}
	// A click on a notification brings the window to the front.
	window := qt.UnsafeNewQWindow(engine.RootObjects()[0].UnsafePointer())
	sess.Raise = func() {
		mainthread.Start(func() {
			window.Show()
			window.Raise()
			window.RequestActivate()
		})
	}
	logf("window loaded, focus on pane %d", sess.Focused())
	sess.StateChanged()
	background(sess.LoadAgents)

	if *shot != "" {
		go func() {
			time.Sleep(3 * time.Second)
			if *shotCall != "" {
				method, args, _ := strings.Cut(*shotCall, " ")
				if err := dispatch(sess, method, []byte(args)); err != nil {
					fmt.Fprintln(os.Stderr, "tend-desktop: -shot-call:", err)
				}
				time.Sleep(300 * time.Millisecond)
			}
			mainthread.Start(func() {
				if *shotOpen != "" {
					if item := findObject(engine.RootObjects()[0], *shotOpen); item != nil {
						item.SetProperty("open", qt.NewQVariant8(true))
					}
				}
			})
			time.Sleep(300 * time.Millisecond)
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

// dispatch is every action the window can take, by the name QML calls it
// with (Bridge.call). It runs off the main thread: each one talks to the
// server.
func dispatch(sess *core.Session, method string, raw []byte) error {
	var a struct {
		Pane      uint64 `json:"pane"`
		Tab       uint64 `json:"tab"`
		Workspace uint64 `json:"workspace"`
		Group     string `json:"group"`
		Direction string `json:"direction"`
		Agent     string `json:"agent"`
		Text      string `json:"text"`
		Side      string `json:"side"`
		Cells     int    `json:"cells"`
		Kind      string `json:"kind"`
		X         int    `json:"x"`
		Y         int    `json:"y"`
		Index     int    `json:"index"`
		Focused   bool   `json:"focused"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
	}
	switch method {
	case "area":
		return sess.SetArea(a.Cols, a.Rows)
	case "focus":
		return sess.Focus(a.Pane)
	case "jump":
		return sess.JumpToPane(a.Pane)
	case "workspace":
		return sess.ShowWorkspace(a.Workspace)
	case "tab":
		return sess.ShowTab(a.Tab)
	case "tab.new":
		return sess.NewTab()
	case "tab.agent":
		return sess.NewAgentTab(a.Agent)
	case "tab.close":
		return sess.CloseTab(a.Tab)
	case "workspace.new":
		return sess.NewWorkspace()
	case "pane.split":
		return sess.Split(a.Direction)
	case "pane.adjust":
		return sess.AdjustSplit(a.Pane, a.Side, a.Cells)
	case "pane.close":
		return sess.ClosePane()
	case "menu.open":
		sess.OpenMenu(a.Kind, a.Pane, a.Tab, a.Workspace, a.Group, a.X, a.Y)
		return nil
	case "menu.pick":
		return sess.PickMenu(a.Index)
	case "menu.close":
		sess.CloseMenu()
		return nil
	case "prompt.submit":
		return sess.SubmitPrompt(a.Text)
	case "prompt.cancel":
		sess.CancelPrompt()
		return nil
	case "pane.zoom":
		return sess.ToggleZoom(a.Pane)
	case "window.focus":
		return sess.SetWindowFocused(a.Focused)
	case "toast.dismiss":
		sess.DismissToast(a.Index)
		return nil
	case "toast.open":
		sess.DismissToast(a.Index)
		return sess.JumpToPane(a.Pane)
	case "status":
		setStatus(a.Text)
		return nil
	case "group.toggle":
		sess.ToggleGroup(a.Group)
		return nil
	case "agents.grouped":
		sess.ToggleGrouped()
		return nil
	}
	return fmt.Errorf("the window asked for %q, which it does not know", method)
}

// findObject is the object of that name under root, for -shot-open.
func findObject(root *qt.QObject, name string) *qt.QObject {
	if root.ObjectName() == name {
		return root
	}
	for _, c := range root.Children() {
		if found := findObject(c, name); found != nil {
			return found
		}
	}
	return nil
}

// background runs an action off the main thread and shows its error.
func background(f func() error) {
	go func() {
		if err := f(); err != nil {
			setStatus(err.Error())
		}
	}()
}

// setStatus shows a line at the bottom of the window. Safe from any
// goroutine.
func setStatus(msg string) {
	logf("status: %s", msg)
	mainthread.Start(func() { qtterm.SetStatus(msg) })
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tend-desktop:", err)
	os.Exit(1)
}

// debug is TEND_DESKTOP_DEBUG set: what the window is told and does, on
// standard error.
var debug bool

func logf(format string, args ...any) {
	if debug {
		fmt.Fprintf(os.Stderr, "tend-desktop: "+format+"\n", args...)
	}
}
