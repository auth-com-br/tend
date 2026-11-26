package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The weight budget of docs/DESKTOP.md, rule 4, held by a test rather than
// hoped for: the desktop exists partly because the owner did not want an
// Electron app's weight, and a budget nobody measures is one nobody keeps.
const (
	budgetPSS    = 150 << 20 // with ten panes and the sidebar
	budgetBinary = 40 << 20  // stripped
)

// TestTheWindowStaysLight: ten panes open, the window up, its memory (PSS,
// which counts a shared library once among the processes that map it) under
// the budget, and the binary, stripped, under its own. Offscreen, so it
// runs without a display; on the owner's GNOME Wayland the same window
// measured 119 MB with four panes, the GPU's buffers included.
func TestTheWindowStaysLight(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two binaries and opens ten panes")
	}
	dir := t.TempDir()
	desktop := filepath.Join(dir, "tend-desktop")
	tend := filepath.Join(dir, "tend")
	build := exec.Command("go", "build", "-ldflags", "-s -w", "-o", desktop, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the desktop: %v\n%s", err, out)
	}
	buildTend := exec.Command("go", "build", "-o", tend, "./cmd/tend")
	buildTend.Dir = filepath.Join("..", "..", "..")
	buildTend.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := buildTend.CombinedOutput(); err != nil {
		t.Fatalf("building tend: %v\n%s", err, out)
	}
	if info, err := os.Stat(desktop); err != nil {
		t.Fatal(err)
	} else if info.Size() > budgetBinary {
		t.Errorf("the stripped binary is %d MB, over the %d MB budget", info.Size()>>20, budgetBinary>>20)
	}

	run, err := os.MkdirTemp("/tmp", "tdw")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(run)
	env := append(os.Environ(), "XDG_RUNTIME_DIR="+run, "XDG_STATE_HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(), "SHELL=/bin/sh", "QT_QPA_PLATFORM=offscreen")
	tendCmd := func(args ...string) {
		t.Helper()
		c := exec.Command(tend, args...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("tend %v: %v\n%s", args, err, out)
		}
	}
	start := exec.Command(tend, "serve", "-s", "w")
	start.Env = env
	if err := start.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c := exec.Command(tend, "kill", "-s", "w", "-server")
		c.Env = env
		_ = c.Run()
		_ = start.Wait()
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(run, "tend", "w.sock")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the server never came up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// One pane, then nine split off it: a tab of ten.
	tendCmd("new", "-s", "w", "--", "/bin/sh")
	for i := 0; i < 9; i++ {
		dir := "columns"
		if i%2 == 1 {
			dir = "rows"
		}
		tendCmd("new", "-s", "w", "-split", "1", "-dir", dir, "--", "/bin/sh")
	}

	shot := filepath.Join(dir, "shot.png")
	if keep := os.Getenv("TEND_DESKTOP_WEIGHT_SHOT"); keep != "" {
		shot = keep // to look at what was measured
	}
	win := exec.Command(desktop, "-s", "w", "-tend", tend, "-shot", shot)
	win.Env = env
	if err := win.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2500 * time.Millisecond)
	pss, err := pssOf(win.Process.Pid)
	_ = win.Wait()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ten panes: %d MB PSS", pss>>20)
	if pss > budgetPSS {
		t.Errorf("the window with ten panes takes %d MB, over the %d MB budget", pss>>20, budgetPSS>>20)
	}
}

// pssOf is a process's proportional set size, in bytes.
func pssOf(pid int) (int64, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/smaps_rollup", pid))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "Pss:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			return kb << 10, err
		}
	}
	return 0, fmt.Errorf("no Pss in smaps_rollup")
}
