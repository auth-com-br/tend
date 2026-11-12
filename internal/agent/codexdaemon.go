package agent

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Codex 0.167 runs its sessions in a daemon it shares between them (`codex
// app-server`), and its hooks run there — in the environment of whichever
// pane first started it, which the daemon keeps long after that Codex has
// gone. Every Codex opened in another pane then reports its conversation
// and its state as that first pane, or to a session that no longer exists
// (issue #65). `--no-daemon` runs a Codex on its own, as before, so tend
// starts Codex with it; an older Codex refuses the flag it does not know,
// so it is given only to one whose help lists it.

var codexDaemonFlag struct {
	once sync.Once
	has  bool
}

// probeCodex is how a Codex is asked whether it has --no-daemon; a test
// replaces it.
var probeCodex = func() bool {
	path, err := exec.LookPath("codex")
	if err != nil {
		return false
	}
	cmd := exec.Command(path, "--help")
	done := make(chan []byte, 1)
	go func() {
		out, _ := cmd.Output()
		done <- out
	}()
	select {
	case out := <-done:
		return bytes.Contains(out, []byte("--no-daemon"))
	case <-time.After(5 * time.Second):
		// A Codex that hangs on --help is not one to start differently.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return false
	}
}

// WarmCodex asks Codex, once per process, whether it has --no-daemon, so
// that LaunchArgv need not: it runs a program, and is called where no
// program should be run (under the server's lock).
func WarmCodex() {
	codexDaemonFlag.once.Do(func() { codexDaemonFlag.has = probeCodex() })
}

// LaunchArgv is the command an agent is started with: argv itself, but for
// a Codex that shares a daemon, which is started without it. The pane's
// record keeps argv as given — what the user asked for, and what a restart
// or a resume reads back.
//
// A Codex can also be the program a shell script is handed — `sh -c
// <script> <name> codex …`, as the issues and errors panels start one in a
// new worktree — and is found there too.
func LaunchArgv(argv []string) []string {
	at := codexAt(argv)
	if at < 0 {
		return argv
	}
	WarmCodex()
	if !codexDaemonFlag.has {
		return argv
	}
	for _, a := range argv[at+1:] {
		if a == "--no-daemon" {
			return argv
		}
	}
	out := make([]string, 0, len(argv)+1)
	out = append(out, argv[:at+1]...)
	out = append(out, "--no-daemon")
	return append(out, argv[at+1:]...)
}

// codexAt is where in argv the Codex program is, or -1.
func codexAt(argv []string) int {
	switch {
	case len(argv) == 0:
		return -1
	case filepath.Base(argv[0]) == "codex":
		return 0
	case len(argv) >= 5 && argv[1] == "-c" && isShell(argv[0]) && filepath.Base(argv[4]) == "codex":
		// sh -c script $0 $1…: the script's own name is argv[3], and what
		// it runs with "$@" begins at argv[4].
		return 4
	}
	return -1
}

func isShell(path string) bool {
	switch filepath.Base(path) {
	case "sh", "bash", "zsh", "dash":
		return true
	}
	return false
}
