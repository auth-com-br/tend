package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestASecretIsKeptFoundAndForgotten runs the package against a stand-in
// secret-tool that keeps secrets in a folder and logs its arguments: a
// secret is stored, read back, replaced, and forgotten; one never kept is
// ErrNotFound; and the secret is never on the command line, where every
// user can read it. Without the tool, Available is false. If it regresses,
// a token is lost, or shows in ps. The real secret-tool is not run here.
func TestASecretIsKeptFoundAndForgotten(t *testing.T) {
	bin, store := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
echo "$@" >> "` + store + `/argv"
cmd=$1; shift
name=""
while [ $# -gt 0 ]; do [ "$1" = account ] && name=$2; shift; done
case $cmd in
store) cat > "` + store + `/s-$name" ;;
lookup) [ -f "` + store + `/s-$name" ] || exit 1; cat "` + store + `/s-$name" ;;
clear) rm -f "` + store + `/s-$name" ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "secret-tool"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	if !Available() {
		t.Fatal("the tool on PATH was not found")
	}
	if err := Set("glitchtip:rvx", "s3cret-one"); err != nil {
		t.Fatal(err)
	}
	if got, err := Get("glitchtip:rvx"); err != nil || got != "s3cret-one" {
		t.Errorf("get = %q %v", got, err)
	}
	if err := Set("glitchtip:rvx", "s3cret-two"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Get("glitchtip:rvx"); got != "s3cret-two" {
		t.Errorf("replaced = %q", got)
	}
	if err := Delete("glitchtip:rvx"); err != nil {
		t.Fatal(err)
	}
	if _, err := Get("glitchtip:rvx"); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: %v", err)
	}
	argv, _ := os.ReadFile(filepath.Join(store, "argv"))
	if strings.Contains(string(argv), "s3cret") || !strings.Contains(string(argv), "service tend account glitchtip:rvx") {
		t.Errorf("command lines:\n%s", argv)
	}

	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Error("available with no tool")
	}
	if err := Set("x", "y"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("set with no tool: %v", err)
	}
	if name, ok := Name(Ref("glitchtip:rvx")); !ok || name != "glitchtip:rvx" {
		t.Errorf("ref round trip: %q %v", name, ok)
	}
	if _, ok := Name("plain-token"); ok {
		t.Error("a plain value read as a ref")
	}
}
