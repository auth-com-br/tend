// Package qtterm is the Terminal QML item and its bridge to Go.
//
// The item itself is C++ (terminalitem.cpp), because Qt Quick has no Go
// bindings; this file is the two directions across cgo. Go hands it frames
// with Set; it hands Go key presses and its size through the callbacks in
// Handlers.
package qtterm

//go:generate sh -c "$(pkg-config --variable=libexecdir Qt6Core)/moc terminalitem.h -o terminalitem.moc"

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
}

// Register makes Tend.Terminal available to QML. Call it after the
// application object exists and before the QML is loaded.
func Register() { C.tend_term_register() }

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

//export goTermResize
func goTermResize(pane C.int64_t, cols, rows C.int) {
	if Handlers.Resize != nil {
		Handlers.Resize(uint64(pane), int(cols), int(rows))
	}
}

// cellSize is C's TendCell size, for the test that holds it to core.Cell's.
var cellSize = int(unsafe.Sizeof(C.TendCell{}))
