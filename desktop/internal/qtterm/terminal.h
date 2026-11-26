// The C face of the terminal item, the only part of qtterm Go calls into.
// Kept to plain C types so cgo can read it.
#ifndef TEND_QTTERM_TERMINAL_H
#define TEND_QTTERM_TERMINAL_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// TendCell is core.Cell, field for field: Go hands a frame over as one
// pointer to its slice, so the two layouts must never drift apart
// (qtterm_test.go checks the size).
typedef struct {
	uint32_t ch;
	uint32_t fg;
	uint32_t bg;
	uint16_t attrs;
	uint8_t width;
	uint8_t pad;
} TendCell;

// tend_term_register makes the item known to QML as Tend.Terminal 1.0.
// Called once, before the QML is loaded.
void tend_term_register(void);

// tend_term_set gives every Terminal item showing pane this frame, and asks
// it to repaint. Main thread only: it touches QQuickItems.
void tend_term_set(int64_t pane, const TendCell *cells, int cols, int rows,
                   int cursor_x, int cursor_y, int cursor_visible, uint32_t bg);

// tend_term_grab saves what the window holding the first Terminal item
// shows to a PNG at path, and returns 0 on success. It is how the window is
// checked without a person looking at it (tend-desktop -shot).
int tend_term_grab(const char *path);

#ifdef __cplusplus
}
#endif

#endif
