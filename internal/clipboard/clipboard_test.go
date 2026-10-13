package clipboard

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestCopyOverSSHDoesNotUseALocalTool: the desktop this process can reach is
// not the one the user is looking at, and copying there would report success
// while putting the text somewhere nobody will paste from.
func TestCopyOverSSHDoesNotUseALocalTool(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 1 10.0.0.2 22")
	if !Remote() {
		t.Fatal("an ssh connection should read as remote")
	}
	if via, err := Copy("hello"); !errors.Is(err, ErrNoTool) || via != "" {
		t.Errorf("Copy = %q, %v; want no tool", via, err)
	}

	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "/dev/pts/3")
	if !Remote() {
		t.Error("an ssh tty should read as remote too")
	}
}

// TestCopyNeedsADisplay: a tool that is installed but has no display to talk
// to would fail at the moment it is needed, so it is not chosen.
func TestCopyNeedsADisplay(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("pbcopy talks to the pasteboard, not to a display")
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")

	if via, err := Copy("hello"); !errors.Is(err, ErrNoTool) || via != "" {
		t.Errorf("Copy with no display = %q, %v; want no tool", via, err)
	}
}

// TestCopyNothingIsNotAnError: an empty selection is not a failure to copy.
func TestCopyNothingIsNotAnError(t *testing.T) {
	if via, err := Copy(""); err != nil || via != "" {
		t.Errorf("Copy(\"\") = %q, %v", via, err)
	}
}

// TestCopyReachesTheClipboard runs the real tool when there is one, because
// what this package promises cannot be checked any other way.
func TestCopyReachesTheClipboard(t *testing.T) {
	if Remote() {
		t.Skip("over ssh there is no local clipboard to check")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("no display")
	}
	read, err := exec.LookPath("xclip")
	if err != nil {
		t.Skip("xclip is not installed")
	}

	const want = "tend clipboard probe"
	via, err := Copy(want)
	if err != nil {
		t.Skipf("no clipboard tool worked here: %v", err)
	}
	if via == "" {
		t.Fatal("Copy reported no tool but no error")
	}

	out, err := exec.Command(read, "-selection", "clipboard", "-o").Output()
	if err != nil {
		t.Skipf("reading the clipboard back: %v", err)
	}
	if got := strings.TrimRight(string(out), "\n"); got != want {
		t.Errorf("clipboard = %q, want %q (copied via %s)", got, want, via)
	}
}
