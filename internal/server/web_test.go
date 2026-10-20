//go:build unix

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/web"
)

// fakeWeb is a phone view as the server sees one: it writes its status as
// tend web does once it listens, and stays. Its last argument is "web", as
// tend web's is, which is how the server tells it from a stranger.
func fakeWeb(dir string) []string {
	status := filepath.Join(dir, "web-s.json")
	script := `printf '{"pid":%d,"listen":"0.0.0.0:7777","urls":["http://192.0.2.7:7777/?t=abc"]}' $$ > ` + status + `.tmp && mv ` + status + `.tmp ` + status + `; while :; do sleep 1; done`
	return []string{"/bin/sh", "-c", script, "web"}
}

func webServer(t *testing.T, dir string, command []string) *Server {
	t.Helper()
	s, err := New(Config{
		DetectInterval: 10 * time.Millisecond,
		DefaultSize:    pty.Size{Cols: 80, Rows: 24},
		SessionName:    "s",
		WebStateDir:    dir,
		WebCommand:     command,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestThePhoneViewIsTurnedOnAndOffFromAClient: on starts it and says where
// a phone reaches it; on again does not start a second; off ends it and
// forgets it. If it regresses, the menu's switch starts nothing, starts one
// per press, or leaves one running after it says off.
func TestThePhoneViewIsTurnedOnAndOffFromAClient(t *testing.T) {
	dir := t.TempDir()
	s := webServer(t, dir, fakeWeb(dir))
	if st := s.WebStatus(); st.On || st.Running {
		t.Fatalf("on before anything: %+v", st)
	}
	st := s.WebStart()
	if !st.On || !st.Running || st.Error != "" || len(st.URLs) != 1 || !strings.HasSuffix(st.URLs[0], "?t=abc") {
		t.Fatalf("after on: %+v", st)
	}
	first, _ := web.ReadStatus(filepath.Join(dir, "web-s.json"))
	if again := s.WebStart(); !again.Running {
		t.Fatalf("on again: %+v", again)
	}
	if second, _ := web.ReadStatus(filepath.Join(dir, "web-s.json")); second.Pid != first.Pid {
		t.Fatalf("on again started another view: %d, then %d", first.Pid, second.Pid)
	}
	st = s.WebStop()
	if st.On || st.Running || st.Error != "" {
		t.Fatalf("after off: %+v", st)
	}
	if isWebProcess(first.Pid) {
		t.Fatalf("the view (pid %d) is still running after off", first.Pid)
	}
	if _, err := os.Stat(filepath.Join(dir, "web-s.on")); !os.IsNotExist(err) {
		t.Fatalf("off was not remembered: %v", err)
	}
}

// TestThePhoneViewComesBackWithTheServer: left on, a server started later
// starts it again, as the owner's machine restarting would. If it
// regresses, the phone loses the agents at every reboot.
func TestThePhoneViewComesBackWithTheServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "web-s.on"), []byte("on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := webServer(t, dir, fakeWeb(dir))
	deadline := time.Now().Add(5 * time.Second)
	for !s.WebStatus().Running {
		if time.Now().After(deadline) {
			t.Fatalf("the view was not started again: %+v", s.WebStatus())
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.WebStop()
}

// TestAPhoneViewThatCannotStartSaysWhy: the port taken, say — what it
// said is what the panel shows, and on stays asked for.
func TestAPhoneViewThatCannotStartSaysWhy(t *testing.T) {
	dir := t.TempDir()
	s := webServer(t, dir, []string{"/bin/sh", "-c", "echo 'listen tcp 0.0.0.0:7777: bind: address already in use' >&2; exit 1", "web"})
	st := s.WebStart()
	if st.Running || !st.On || !strings.Contains(st.Error, "address already in use") {
		t.Fatalf("a view that failed: %+v", st)
	}
	s.WebStop()
}
