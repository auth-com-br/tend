package qtterm

import (
	"testing"
	"unsafe"

	"github.com/auth-com-br/tend/desktop/internal/core"
)

// TestTheCellLayoutIsTheSameOnBothSides: a frame crosses to C++ as one
// pointer to Go's slice, so core.Cell and TendCell must be the same size.
// If they drift, the painter reads every cell after the first from the
// wrong place and the window fills with garbage.
func TestTheCellLayoutIsTheSameOnBothSides(t *testing.T) {
	if goSize := int(unsafe.Sizeof(core.Cell{})); goSize != cellSize {
		t.Fatalf("core.Cell is %d bytes, TendCell %d", goSize, cellSize)
	}
}
