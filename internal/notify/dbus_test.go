package notify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeNotifySend puts a notify-send on PATH that writes its arguments down,
// waits a moment as a real one waits for the click, and prints answer.
func fakeNotifySend(t *testing.T, answer string, exit int) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + args + "\nsleep 0.7\necho '" + answer + "'\nexit " + string(rune('0'+exit)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "notify-send"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return args
}

// TestClickingANotificationCallsBack: notify-send is given a default
// action and waited on behind the caller; its saying the action was
// clicked calls back, once, and its closing without a click does not. The
// notify-send is a stand-in: no notify-send is installed on the machine
// this was written on, so what a real one prints is the libnotify manual's
// word, not seen here. If it regresses, clicking the notice that an agent
// needs you does nothing (herdr discussion #625).
func TestClickingANotificationCallsBack(t *testing.T) {
	args := fakeNotifySend(t, "default", 0)
	clicked := make(chan bool, 2)
	start := time.Now()
	if err := SystemWithClick("claude needs attention", "it asks", func() { clicked <- true }); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 650*time.Millisecond {
		t.Error("the caller waited for the click")
	}
	select {
	case <-clicked:
	case <-time.After(3 * time.Second):
		t.Fatal("the click was not heard")
	}
	got, _ := os.ReadFile(args)
	if !strings.Contains(string(got), "--action=default=Open\nclaude needs attention\nit asks") {
		t.Errorf("notify-send was given %q", got)
	}

	fakeNotifySend(t, "", 0) // closed, not clicked
	if err := SystemWithClick("x", "", func() { clicked <- true }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-clicked:
		t.Error("a notification closed without a click called back")
	case <-time.After(1200 * time.Millisecond):
	}
}

// TestAnOldNotifySendIsNotAskedForAClick: one that refuses --action at
// once leaves the notification to System.
func TestAnOldNotifySendIsNotAskedForAClick(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notify-send"), []byte("#!/bin/sh\necho 'Unknown option --action' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	if err := SystemWithClick("x", "", func() {}); err != ErrNoActions {
		t.Errorf("err = %v, want ErrNoActions", err)
	}
}

// TestWithoutNotifySendTheServerIsAskedOverDBus: a desktop with gdbus and
// no notify-send — the owner's — gets its notification from the server
// itself, the text quoted for gdbus whatever an agent put in it.
func TestWithoutNotifySendTheServerIsAskedOverDBus(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	if err := os.WriteFile(filepath.Join(dir, "gdbus"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+args+"\necho '(uint32 7,)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	old := commandFor
	commandFor = func(ctx context.Context, name string, a ...string) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(dir, name), a...)
	}
	t.Cleanup(func() { commandFor = old })
	if err := System("claude finished", "it's \\ done"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(args)
	if !strings.Contains(string(got), "org.freedesktop.Notifications.Notify\n'tend'\nuint32 0\n''\n'claude finished'\n'it\\'s \\\\ done'\n@as []\n@a{sv} {}\nint32 -1") {
		t.Errorf("gdbus was given %q", got)
	}
}
