package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/ui"
)

// These tests drive Session against a real tend server, built from this
// checkout, in a runtime and state directory of their own: what the window
// does is what the server is asked, and only the real server says whether
// it was asked right.

var tendBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tend-desktop-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	tendBin = filepath.Join(dir, "tend")
	// Built in the repository's root, by its own go.mod: the desktop's
	// module needs only the packages it imports, not all of tend's.
	build := exec.Command("go", "build", "-o", tendBin, "./cmd/tend")
	build.Dir = filepath.Join("..", "..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building tend: %v\n%s", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var sessionCount atomic.Int32

// openTest opens a session of its own on a server of its own, stopped when
// the test ends.
func openTest(t *testing.T) *Session {
	t.Helper()
	// A short directory: a unix socket's path is limited to about a
	// hundred bytes, and t.TempDir's are long.
	run, err := os.MkdirTemp("/tmp", "tdt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv(skipOnboardingEnv, "1")
	name := fmt.Sprintf("t%d", sessionCount.Add(1))
	s, err := Open(name, tendBin, config.Defaults())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		_ = exec.Command(tendBin, "kill", "-s", name, "-server").Run()
		// The server writes the session's history as it stops, after it
		// has answered the shutdown: its state directory is removed only
		// once it has gone, or the removal races the write.
		if s.serverExited != nil {
			select {
			case <-s.serverExited:
			case <-time.After(10 * time.Second):
				t.Error("the server did not stop")
			}
		}
		_ = os.RemoveAll(run)
	})
	return s
}

// waitFor polls until cond holds, or fails the test.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// screenText is what a pane's terminal here shows, live.
func screenText(s *Session, pane uint64) string {
	f, ok := s.Frame(pane, nil)
	if !ok {
		return ""
	}
	var b strings.Builder
	for i, c := range f.Cells {
		if i > 0 && i%f.Cols == 0 {
			b.WriteByte('\n')
		}
		if c.Width != 0 {
			b.WriteRune(rune(c.Ch))
		}
	}
	return b.String()
}

// TestTheWindowShowsASessionsPaneAtItsSize: a new session has a shell, laid
// out over the window's area, and what the shell prints reaches the pane's
// terminal here. If it regresses, the window opens empty, as its first
// version did on Wayland.
func TestTheWindowShowsASessionsPaneAtItsSize(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	if len(st.Panes) != 1 || st.Panes[0].Cols != 100 || st.Panes[0].Rows != 40 {
		t.Fatalf("panes %+v, want one of 100x40", st.Panes)
	}
	pane := st.Panes[0].Pane
	if err := s.ResizePane(pane, 98, 38); err != nil {
		t.Fatal(err)
	}
	for _, k := range "echo hello-desktop\r" {
		if err := s.Key(pane, int(k), 0, string(k)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "the shell's output", func() bool {
		return strings.Count(screenText(s, pane), "hello-desktop") >= 2
	})
	if len(st.Spaces) != 1 || len(st.Tabs) != 1 {
		t.Errorf("sidebar %d spaces, %d tabs; want one each", len(st.Spaces), len(st.Tabs))
	}
}

// TestSplitsAndDividersAreTheServers: a split adds a pane beside the one in
// focus and gives it the focus; dragging the divider moves it by whole
// cells. If it regresses, the window's splits and the TUI's disagree.
func TestSplitsAndDividersAreTheServers(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	first := s.Focused()
	if err := s.Split("columns"); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	if len(st.Panes) != 2 {
		t.Fatalf("%d panes after a split, want 2", len(st.Panes))
	}
	if s.Focused() == first {
		t.Error("the new pane did not get the focus")
	}
	var before int
	for _, p := range st.Panes {
		if p.Pane == first {
			before = p.Cols
		}
	}
	if err := s.AdjustSplit(first, "right", 7); err != nil {
		t.Fatal(err)
	}
	for _, p := range s.State().Panes {
		if p.Pane == first && p.Cols != before+7 {
			t.Errorf("the divider moved the pane from %d to %d columns, want %d", before, p.Cols, before+7)
		}
	}
}

// TestATabOpensAndIsShown: + opens a tab in the space shown and goes to it.
func TestATabOpensAndIsShown(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(80, 24); err != nil {
		t.Fatal(err)
	}
	if err := s.NewTab(); err != nil {
		t.Fatal(err)
	}
	st := s.State()
	if len(st.Tabs) != 2 || !st.Tabs[1].Active {
		t.Fatalf("tabs %+v, want two with the new one shown", st.Tabs)
	}
	if len(st.Panes) != 1 {
		t.Errorf("the new tab shows %d panes, want its one", len(st.Panes))
	}
}

// TestExitClosesThePane: a shell that exits takes its pane, as herdr's do,
// and the window shows what is left.
func TestExitClosesThePane(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	if err := s.Split("rows"); err != nil {
		t.Fatal(err)
	}
	gone := s.Focused()
	for _, k := range "exit\r" {
		_ = s.Key(gone, int(k), 0, string(k))
	}
	waitFor(t, "the pane to close", func() bool {
		for _, p := range s.State().Panes {
			if p.Pane == gone {
				return false
			}
		}
		return len(s.State().Panes) == 1
	})
}

// TestTheWheelScrollsAShellsHistory: in a pane whose program did not ask
// for the mouse, the wheel looks back through tend's history, and typing
// returns to the present.
func TestTheWheelScrollsAShellsHistory(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(80, 24); err != nil {
		t.Fatal(err)
	}
	pane := s.Focused()
	if err := s.ResizePane(pane, 80, 24); err != nil {
		t.Fatal(err)
	}
	for _, k := range "seq 1 200\r" {
		_ = s.Key(pane, int(k), 0, string(k))
	}
	waitFor(t, "the output", func() bool { return strings.Contains(screenText(s, pane), "200") })
	waitFor(t, "the history to be scrollable", func() bool {
		_ = s.Mouse(pane, ui.MouseEvent{Kind: ui.MouseWheelUp})
		for _, p := range s.State().Panes {
			if p.Pane == pane && p.Scroll > 0 {
				return true
			}
		}
		return false
	})
	_ = s.Key(pane, 'x', 0, "x")
	for _, p := range s.State().Panes {
		if p.Pane == pane && p.Scroll != 0 {
			t.Errorf("typing left the pane %d lines back", p.Scroll)
		}
	}
}

// TestTheMenusDoWhatTheTUIsDo: the space menu renames through a prompt,
// a group made for a space holds it, the tab menu closes a tab, and the pane
// menu splits and zooms. If it regresses, a right click in the window offers
// what the TUI offers and does something else.
func TestTheMenusDoWhatTheTUIsDo(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	ws := s.State().Workspace
	pick := func(label string) {
		t.Helper()
		st := s.State()
		if st.Menu == nil {
			t.Fatalf("no menu open to pick %q from", label)
		}
		for i, it := range st.Menu.Items {
			if it == label {
				if err := s.PickMenu(i); err != nil {
					t.Fatalf("%s: %v", label, err)
				}
				return
			}
		}
		t.Fatalf("menu %q has no %q: %v", st.Menu.Title, label, st.Menu.Items)
	}

	s.OpenMenu("space", 0, 0, ws, "", 10, 10)
	pick("rename")
	if st := s.State(); st.Prompt == nil || st.Prompt.Kind != promptRenameSpace {
		t.Fatalf("rename asked for %+v", st.Prompt)
	}
	if err := s.SubmitPrompt("api"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the space renamed", func() bool {
		_ = s.refresh()
		sp := s.State().Spaces
		return len(sp) > 0 && sp[0].Label == "api"
	})

	s.OpenMenu("space", 0, 0, ws, "", 10, 10)
	pick("new group...")
	if err := s.SubmitPrompt("acme"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the space in its group", func() bool {
		_ = s.refresh()
		sp := s.State().Spaces
		return len(sp) == 2 && sp[0].Kind == "group" && sp[0].Label == "acme"
	})

	pane := s.Focused()
	s.OpenMenu("pane", pane, 0, 0, "", 10, 10)
	pick("split right")
	waitFor(t, "two panes", func() bool { return len(s.State().Panes) == 2 })
	s.OpenMenu("pane", pane, 0, 0, "", 10, 10)
	pick("zoom")
	if st := s.State(); !st.Zoomed || len(st.Panes) != 1 || st.Panes[0].Cols != 100 {
		t.Errorf("zoom shows %+v", st.Panes)
	}

	if err := s.NewTab(); err != nil {
		t.Fatal(err)
	}
	tab := s.State().Tabs[1].ID
	s.OpenMenu("tab", 0, tab, 0, "", 10, 10)
	pick("close tab")
	waitFor(t, "the tab closed", func() bool {
		_ = s.refresh()
		return len(s.State().Tabs) == 1
	})
}

// TestAnAgentThatNeedsYouIsAnnounced: an agent in a pane says it is working,
// then that it waits on a question (OSC 7501, as Claude Code says it); with
// the window behind another, the window shows a card and raises the system's
// notification, and a click on it goes to the agent. If it regresses, the
// agent that needs you is not the one on screen and nothing says so — the
// reason tend exists.
func TestAnAgentThatNeedsYouIsAnnounced(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	type raised struct{ title, body string }
	got := make(chan raised, 4)
	var click func()
	s.mu.Lock()
	s.notifyHook = func(title, body string, clicked func()) error {
		click = clicked
		got <- raised{title, body}
		return nil
	}
	s.mu.Unlock()
	if err := s.SetWindowFocused(false); err != nil {
		t.Fatal(err)
	}
	script := `printf '\033]7501;state=working:app=claude-code\007'; sleep 1; ` +
		`printf '\033]7501;state=blocked:app=claude-code:kind=question\007'; sleep 30`
	ws := s.State().Workspace
	_, pane, err := s.client.NewTab(ws, "agent", proto.PaneSpec{Command: []string{"/bin/sh", "-c", script}, Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if !strings.Contains(r.title, "claude needs attention") {
			t.Errorf("the notification says %q", r.title)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no notification for an agent that needs an answer")
	}
	if toasts := s.State().Toasts; len(toasts) == 0 || toasts[0].Pane != pane {
		t.Errorf("the window's cards: %+v", toasts)
	}
	click()
	waitFor(t, "the click to go to the agent", func() bool { return s.Focused() == pane })
}

// TestASettingChangedInTheWindowIsWrittenAndWorn: stepping the theme in the
// settings screen writes it to the settings file, as the TUI's screen does,
// and the window wears it at once. If it regresses, the window's screen
// changes something only it can see, or nothing at all.
func TestASettingChangedInTheWindowIsWrittenAndWorn(t *testing.T) {
	s := openTest(t)
	s.OpenPanel("settings")
	st := s.State()
	if st.Settings == nil || len(st.Settings.Rows) == 0 || st.Settings.Rows[0].Label != "theme" {
		t.Fatalf("the screen is %+v", st.Settings)
	}
	before := st.Theme.Base
	if _, err := s.ChangeSetting(0, 2); err != nil {
		t.Fatal(err)
	}
	path, _ := config.Path()
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "[ui.theme]") {
		t.Fatalf("the settings file is %q (%v)", raw, err)
	}
	after := s.State()
	if after.Theme.Base == before {
		t.Errorf("the window still wears %s", before)
	}
	if after.Settings.Rows[0].Current != 2 {
		t.Errorf("the screen shows choice %d, want 2", after.Settings.Rows[0].Current)
	}
}

// TestTheWelcomeIsShownOnceForBothClients: with no settings file the window
// opens on herdr's welcome; going on writes onboarding = false — the TUI's
// switch too — and opens the settings. If it regresses, the welcome comes
// back every time, or never.
func TestTheWelcomeIsShownOnceForBothClients(t *testing.T) {
	s := openTest(t)
	os.Unsetenv(skipOnboardingEnv)
	s.startOnboarding()
	if p := s.State().Panel; p != "welcome" {
		t.Fatalf("a first window opens on %q, want the welcome", p)
	}
	if err := s.FinishOnboarding(); err != nil {
		t.Fatal(err)
	}
	if p := s.State().Panel; p != "settings" {
		t.Errorf("going on opened %q, want the settings", p)
	}
	cfg, err := config.Load()
	if err != nil || cfg.ShowOnboarding() {
		t.Errorf("the welcome would show again (%v)", err)
	}
}

// TestATaskIsPlannedFromTheDock: the tasks tab lists what the server keeps;
// a task made in its form, with a time typed as a person types it and an
// agent, is the server's (so it starts with the window closed), and is
// marked done and deleted from the same list. If it regresses, the window's
// plan and the session's disagree.
func TestATaskIsPlannedFromTheDock(t *testing.T) {
	s := openTest(t)
	s.ToggleDock("tasks")
	waitFor(t, "the tasks tab", func() bool { st := s.State(); return st.Dock != nil && !st.Dock.Loading })
	if err := s.CreateTask(NewTask{Title: "review the PR", At: "+2h", Agent: "claude", Prompt: "review it", Here: true}); err != nil {
		t.Fatal(err)
	}
	var task TaskRow
	waitFor(t, "the task listed", func() bool {
		d := s.State().Dock
		if d == nil || len(d.Tasks) != 1 {
			return false
		}
		task = d.Tasks[0]
		return true
	})
	if task.Title != "review the PR" || task.StartAt == "" || task.Agent != "claude" || task.Workspace == "" {
		t.Errorf("the task is %+v", task)
	}
	if err := s.SetTaskStatus(task.ID, "done"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the task done", func() bool { d := s.State().Dock; return len(d.Tasks) == 1 && d.Tasks[0].Status == "done" })
	if err := s.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the task gone", func() bool { return len(s.State().Dock.Tasks) == 0 })
	if err := s.CreateTask(NewTask{Title: "x", At: "someday"}); err == nil {
		t.Error("a time nobody can read was taken")
	}
}

// TestTheFilesTabIsTheFilesPanel: the dock's files tab shows the folder the
// pane in focus works in, through explorer.Panel — the tree with git's
// letters, a change's diff side by side, stage and commit — and opens a
// file highlighted. If it regresses, the window's files and the TUI's
// panel disagree about the same repository.
func TestTheFilesTabIsTheFilesPanel(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "first")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\n// hello\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := s.State().Workspace
	tab, _, err := s.client.NewTab(ws, "repo", proto.PaneSpec{Dir: repo})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ShowTab(tab); err != nil {
		t.Fatal(err)
	}
	s.ToggleDock("files")
	waitFor(t, "the tree", func() bool {
		d := s.State().Dock
		if d == nil || d.Files == nil {
			return false
		}
		for _, e := range d.Files.Entries {
			if e.Name == "main.go" && e.Status == "M" {
				return true
			}
		}
		return false
	})
	if err := s.Files(FilesAction{Do: "view", View: "changes"}); err != nil {
		t.Fatal(err)
	}
	ch := s.State().Dock.Files.Changes
	if len(ch) != 1 || len(ch[0].Unstaged) != 1 || ch[0].Unstaged[0].Path != "main.go" {
		t.Fatalf("changes %+v", ch)
	}
	if err := s.Files(FilesAction{Do: "diff", Repo: ch[0].Repo, Path: "main.go"}); err != nil {
		t.Fatal(err)
	}
	doc := s.State().Doc
	if doc == nil || doc.Kind != "diff" || len(doc.Hunks) != 1 {
		t.Fatalf("the diff is %+v", doc)
	}
	added := false
	for _, r := range doc.Hunks[0].Rows {
		added = added || (r.New == "// hello" && r.NewK == 1 && r.Old == "")
	}
	if !added {
		t.Errorf("the added line is not on the new side alone: %+v", doc.Hunks[0].Rows)
	}
	if err := s.Files(FilesAction{Do: "stage", Repo: ch[0].Repo, Path: "main.go"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Files(FilesAction{Do: "commit", Repo: ch[0].Repo, Message: "say hello"}); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("git", "-C", repo, "log", "--format=%s", "-1").Output()
	if strings.TrimSpace(string(out)) != "say hello" {
		t.Errorf("the last commit is %q", out)
	}
	if err := s.OpenFile(filepath.Join(repo, "main.go"), 3); err != nil {
		t.Fatal(err)
	}
	doc = s.State().Doc
	if doc.View != "text" || len(doc.Lines) != 4 || doc.Lines[0][0].K == 0 {
		t.Errorf("main.go opened as %s with %d lines, first token %+v", doc.View, len(doc.Lines), doc.Lines[0])
	}
}

// TestATabInAWindowOfItsOwn: a window pinned to a tab shows that tab alone
// and stays on it, and the window it came from moves to another tab, so the
// two do not each size the same panes. If it regresses, a tab dragged out of
// the window is shown twice, its panes resized back and forth.
func TestATabInAWindowOfItsOwn(t *testing.T) {
	s := openTest(t)
	if err := s.SetArea(100, 40); err != nil {
		t.Fatal(err)
	}
	first := s.State().Tabs[0].ID
	if err := s.NewTab(); err != nil {
		t.Fatal(err)
	}
	second := s.State().Tabs[1].ID

	other, err := Open(s.Name(), tendBin, config.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Pin(second); err != nil {
		t.Fatal(err)
	}
	if err := s.LeaveTab(second); err != nil {
		t.Fatal(err)
	}
	st := other.State()
	if !st.Pinned || len(st.Tabs) != 1 || st.Tabs[0].ID != second {
		t.Errorf("the pinned window shows %+v", st.Tabs)
	}
	if err := other.ShowTab(first); err != nil {
		t.Fatal(err)
	}
	if got := other.State().Tabs[0].ID; got != second {
		t.Errorf("the pinned window went to tab %d", got)
	}
	if got := s.State(); !got.Tabs[0].Active || got.Tabs[0].ID != first {
		t.Errorf("the first window stayed on the tab it gave away: %+v", got.Tabs)
	}
}
