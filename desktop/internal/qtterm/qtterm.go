// Package qtterm is the Terminal QML item and its bridge to Go.
//
// The item itself is C++ (terminalitem.cpp), because Qt Quick has no Go
// bindings; this file is the two directions across cgo. Go hands it frames
// with Set; it hands Go key presses and its size through the callbacks in
// Handlers.
package qtterm

//go:generate sh -c "$(pkg-config --variable=libexecdir Qt6Core)/moc terminalitem.h -o terminalitem.moc"
//go:generate sh -c "$(pkg-config --variable=libexecdir Qt6Core)/moc bridge.h -o bridge.moc"

/*
#cgo CXXFLAGS: -std=c++17 -fPIC
#cgo pkg-config: Qt6Quick Qt6Qml Qt6Gui Qt6Core
#include <stdlib.h>
#include "terminal.h"
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/auth-com-br/tend/desktop/internal/core"
)

// Handlers are what the items report, set once before the QML is loaded.
// They run on Qt's main thread and must not block it: anything that talks
// to the server goes to a goroutine.
var Handlers struct {
	Key    func(pane uint64, key, mods int, text string)
	Resize func(pane uint64, cols, rows int)
	// Call is an action QML asked for through Bridge.call: the method's
	// name and its arguments as JSON.
	Call func(method string, args []byte)
	// Mouse is the mouse over a pane, in its cells: kind and mods as
	// ui.MouseKind and ui.MouseMod, button as xterm numbers them.
	Mouse func(pane uint64, kind, button, x, y, mods int)
	// Paste is the clipboard pasted into a pane (ctrl+shift+v).
	Paste func(pane uint64, text string)
}

// Register makes Tend.Terminal available to QML. Call it after the
// application object exists and before the QML is loaded.
func Register() {
	C.tend_term_register()
	C.tend_bridge_register()
}

// SetState gives QML the window's state, as JSON. Main thread only.
func SetState(json []byte) {
	if len(json) == 0 {
		return
	}
	C.tend_bridge_set_state((*C.char)(unsafe.Pointer(&json[0])), C.int(len(json)))
}

// SetBrowserQML gives QML the browser page's source, made into a page only
// when a browser tab is opened. Before the QML is loaded.
func SetBrowserQML(qml []byte) {
	if len(qml) == 0 {
		return
	}
	C.tend_bridge_set_browser_qml((*C.char)(unsafe.Pointer(&qml[0])), C.int(len(qml)))
}

// SetStartURL is a page to open in a browser tab once the window is up.
// Before the QML is loaded.
func SetStartURL(url string) {
	cs := C.CString(url)
	defer C.free(unsafe.Pointer(cs))
	C.tend_bridge_set_start_url(cs, C.int(len(url)))
}

// SetStatus shows a line at the bottom of the window; "" hides it. Main
// thread only.
func SetStatus(text string) {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.tend_bridge_set_status(cs, C.int(len(text)))
}

// Set gives the items showing pane a frame to paint. Main thread only.
func Set(pane uint64, f core.Frame) {
	if f.Cols < 1 || f.Rows < 1 || len(f.Cells) < f.Cols*f.Rows {
		return
	}
	cursor := C.int(0)
	if f.CursorVisible {
		cursor = 1
	}
	C.tend_term_set(C.int64_t(pane), (*C.TendCell)(unsafe.Pointer(&f.Cells[0])),
		C.int(f.Cols), C.int(f.Rows), C.int(f.CursorX), C.int(f.CursorY), cursor, C.uint32_t(f.BG))
}

// Grab saves the window as a PNG. Main thread only.
func Grab(path string) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if rc := C.tend_term_grab(cpath); rc != 0 {
		return fmt.Errorf("could not save the window to %s (%d)", path, int(rc))
	}
	return nil
}

//export goTermKey
func goTermKey(pane C.int64_t, key, mods C.int, text *C.char, n C.int) {
	if Handlers.Key != nil {
		Handlers.Key(uint64(pane), int(key), int(mods), C.GoStringN(text, n))
	}
}

//export goBridgeCall
func goBridgeCall(method *C.char, mlen C.int, args *C.char, alen C.int) {
	if Handlers.Call != nil {
		Handlers.Call(C.GoStringN(method, mlen), C.GoBytes(unsafe.Pointer(args), alen))
	}
}

//export goTermMouse
func goTermMouse(pane C.int64_t, kind, button, x, y, mods C.int) {
	if Handlers.Mouse != nil {
		Handlers.Mouse(uint64(pane), int(kind), int(button), int(x), int(y), int(mods))
	}
}

//export goTermPaste
func goTermPaste(pane C.int64_t, text *C.char, n C.int) {
	if Handlers.Paste != nil {
		Handlers.Paste(uint64(pane), C.GoStringN(text, n))
	}
}

// SetClipboard puts text on the clipboard, and on the primary selection
// where there is one (Linux's middle click). Main thread only.
func SetClipboard(text string) {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	C.tend_set_clipboard(cs, C.int(len(text)))
}

//export goTermResize
func goTermResize(pane C.int64_t, cols, rows C.int) {
	if Handlers.Resize != nil {
		Handlers.Resize(uint64(pane), int(cols), int(rows))
	}
}

// cellSize is C's TendCell size, for the test that holds it to core.Cell's.
var cellSize = int(unsafe.Sizeof(C.TendCell{}))
