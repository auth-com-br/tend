package main

import (
	"os"
	"testing"
)

// TestTheHostKeepsItsCLibraryAndGraphicsAndTheAppImageCarriesQt: what is
// left to the host and what is bundled. If it regresses, an AppImage that
// bundles libc crashes at start on every machine but the builder's, one
// that bundles libGL draws nothing on another GPU's drivers, and one that
// leaves Qt out loads the host's Qt of another version, or none.
func TestTheHostKeepsItsCLibraryAndGraphicsAndTheAppImageCarriesQt(t *testing.T) {
	for _, host := range []string{"libc.so.6", "libm.so.6", "ld-linux-x86-64.so.2", "libstdc++.so.6", "libgcc_s.so.1",
		"libGL.so.1", "libEGL.so.1", "libxcb.so.1", "libX11.so.6", "libwayland-client.so.0", "libfontconfig.so.1", "libz.so.1"} {
		if !excluded(host) {
			t.Errorf("%s would be bundled", host)
		}
	}
	for _, bundled := range []string{"libQt6Core.so.6", "libQt6Quick.so.6", "libicuuc.so.78", "libxkbcommon.so.0",
		"libxcb-cursor.so.0", "libdouble-conversion.so.3", "libmd4c.so.0", "libzstd.so.1"} {
		if excluded(bundled) {
			t.Errorf("%s would be left to the host", bundled)
		}
	}
}

// TestABinaryWithoutTheRpathIsRefused: the AppImage's binary finds its Qt
// through DT_RPATH, which `make appimage` links in. If it regresses, an
// AppImage made from the ordinary build runs with the host's Qt where there
// is one — looking fine on the builder — and fails everywhere else.
func TestABinaryWithoutTheRpathIsRefused(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := hasRpath(self); err == nil {
		t.Fatal("a test binary, linked without the rpath, was taken")
	}
}
