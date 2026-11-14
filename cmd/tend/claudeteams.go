package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/tmuxshim"
)

// Claude Code's agent teams open each teammate in a pane of its own only
// under tmux. `tend claude-teams` runs Claude Code as though it were in tmux
// — $TMUX set, a `tmux` first on its PATH that is this binary — and the
// teammates open as tend panes beside it. internal/tmuxshim says why it is
// done this way and what is answered; this file is the launch and the wire.

// The variables a claude-teams launch hands its `tmux`.
const (
	// envTmuxAPI is the automation socket the shim talks to. Set means the
	// `tmux` was started by a claude-teams launch, and is the shim's.
	envTmuxAPI    = "TEND_TMUX_API"
	envTmuxLeader = "TEND_TMUX_LEADER"
	envTmuxState  = "TEND_TMUX_STATE"
)

const claudeTeamsUsage = `usage: tend claude-teams [claude's own arguments]

Starts Claude Code with agent teams on, in this pane, and opens each
teammate it starts as a tend pane beside it — named, detected, notified,
like any other. Run it in a tend pane. Every argument goes to claude;
--teammate-mode is set to tmux unless one is given.
`

func runClaudeTeams(args []string) error {
	fs := flag.NewFlagSet("claude-teams", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, claudeTeamsUsage) }
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "-help") {
		fs.Usage()
		return nil
	}
	socket, pane := os.Getenv("TEND_SOCKET_PATH"), os.Getenv("TEND_PANE_ID")
	if socket == "" || pane == "" {
		return errors.New("claude-teams runs in a tend pane: its teammates open beside it")
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("claude is not on PATH")
	}
	shimDir, err := installTmuxShim(socket)
	if err != nil {
		return err
	}
	env := claudeTeamsEnv(os.Environ(), socket, pane, shimDir, os.Getpid())
	argv := append([]string{claude}, claudeTeamsArgs(args)...)
	return reexec(claude, argv, env)
}

// claudeTeamsArgs is claude's command line: teammates in panes, which is
// not Claude Code's default (in-process), unless the person chose.
func claudeTeamsArgs(args []string) []string {
	for _, a := range args {
		if a == "--teammate-mode" || strings.HasPrefix(a, "--teammate-mode=") {
			return args
		}
	}
	return append([]string{"--teammate-mode", "tmux"}, args...)
}

// claudeTeamsEnv is the environment Claude Code runs in. $TMUX is tmux's
// "socket,pid,session"; Claude Code gives the socket back as -S, which is
// how the shim tells its own commands from ones for a real tmux server.
func claudeTeamsEnv(env []string, socket, pane, shimDir string, pid int) []string {
	set := map[string]string{
		"TMUX":                                 socket + "," + strconv.Itoa(pid) + ",0",
		"TMUX_PANE":                            tmuxshim.TmuxPane(pane),
		"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1",
		envTmuxAPI:                             socket,
		envTmuxLeader:                          pane,
		envTmuxState:                           filepath.Join(shimDir, "team-"+strconv.Itoa(pid)+".json"),
	}
	path := shimDir
	out := make([]string, 0, len(env)+len(set))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "PATH" {
			path = shimDir + string(os.PathListSeparator) + v
			continue
		}
		if _, ok := set[k]; ok {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return append(out, "PATH="+path)
}

// installTmuxShim puts a `tmux` that is this binary in a directory of its
// own beside the session's sockets, and returns the directory. It is put
// there again on every launch, so it follows tend when tend is updated.
func installTmuxShim(socket string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(socket)), "tmux-shim")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	link := filepath.Join(dir, "tmux")
	tmp := link + "." + strconv.Itoa(os.Getpid())
	_ = os.Remove(tmp)
	if err := os.Symlink(self, tmp); err != nil {
		return "", fmt.Errorf("putting tend's tmux in place: %w", err)
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return dir, nil
}

// runTmux is this binary run as `tmux`. It answers when a claude-teams
// launch started it and the command is for that launch's server; anything
// else — a tmux the agent starts for its own work with -L, or this link
// found some other way — is the real tmux's.
func runTmux(args []string) int {
	socket := os.Getenv(envTmuxAPI)
	if socket == "" || !forTend(args, socket) {
		return realTmux(args)
	}
	shim := &tmuxshim.Shim{
		Backend: &apiPanes{socket: socket},
		Leader:  os.Getenv(envTmuxLeader),
		Session: strings.TrimSuffix(filepath.Base(socket), ".sock"),
		State:   os.Getenv(envTmuxState),
		Stdout:  os.Stdout, Stderr: os.Stderr,
	}
	return shim.Main(args)
}

// forTend is whether a tmux command line is for the session's server: one
// naming no server, or naming the one $TMUX does.
func forTend(args []string, socket string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "--" {
			return true
		}
		switch {
		case a == "-L" || strings.HasPrefix(a, "-L"):
			return false
		case a == "-S":
			return i+1 < len(args) && args[i+1] == socket
		case strings.HasPrefix(a, "-S"):
			return a[2:] == socket
		case a == "-f" || a == "-T":
			i++
		}
	}
	return true
}

// realTmux runs the tmux that would have been found without the shim, with
// the $TMUX of the launch taken away: a real tmux reads it as "already
// inside tmux" and refuses to start.
func realTmux(args []string) int {
	self, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	var found string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		cand := filepath.Join(dir, "tmux")
		info, err := os.Stat(cand)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		if r, err := filepath.EvalSymlinks(cand); err == nil && r == self {
			continue
		}
		found = cand
		break
	}
	if found == "" {
		fmt.Fprintln(os.Stderr, "tmux: not installed (tend's tmux answers only Claude Code's agent teams)")
		return 127
	}
	env := os.Environ()
	if os.Getenv(envTmuxAPI) != "" {
		kept := env[:0]
		for _, kv := range env {
			k, _, _ := strings.Cut(kv, "=")
			if k != "TMUX" && k != "TMUX_PANE" {
				kept = append(kept, kv)
			}
		}
		env = kept
	}
	if err := reexec(found, append([]string{"tmux"}, args...), env); err != nil {
		fmt.Fprintf(os.Stderr, "tmux: %v\n", err)
		return 1
	}
	return 0
}

// apiPanes is the shim's Backend: the session's automation socket.
type apiPanes struct{ socket string }

func (a *apiPanes) call(method string, params map[string]any) (map[string]any, error) {
	conn, err := net.DialTimeout("unix", a.socket, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("the tend session is not answering: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(apiTimeout))
	line, err := json.Marshal(map[string]any{"id": "tmux", "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return nil, err
	}
	reply, err := bufio.NewReaderSize(conn, 1<<16).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var out struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, errors.New(out.Error.Message)
	}
	return out.Result, nil
}

func paneOf(v any) tmuxshim.Pane {
	m, _ := v.(map[string]any)
	p := tmuxshim.Pane{ID: text(m["pane_id"]), Tab: text(m["tab_id"]), Title: text(m["title"])}
	p.Cwd = text(m["foreground_cwd"])
	if p.Cwd == "" {
		p.Cwd = text(m["cwd"])
	}
	if pid, ok := m["pid"].(float64); ok {
		p.Pid = int(pid)
	}
	p.Focused, _ = m["focused"].(bool)
	return p
}

func (a *apiPanes) Panes() ([]tmuxshim.Pane, error) {
	r, err := a.call("pane.list", nil)
	if err != nil {
		return nil, err
	}
	list, _ := r["panes"].([]any)
	out := make([]tmuxshim.Pane, 0, len(list))
	for _, v := range list {
		out = append(out, paneOf(v))
	}
	return out, nil
}

func runParams(run tmuxshim.Run) map[string]any {
	p := map[string]any{"close_on_exit": run.CloseOnExit}
	if len(run.Argv) > 0 {
		p["command"] = run.Argv
	}
	if run.Dir != "" {
		p["dir"] = run.Dir
	}
	if run.Agent != "" {
		p["agent"] = run.Agent
	}
	return p
}

func (a *apiPanes) Split(target string, down bool, run tmuxshim.Run) (tmuxshim.Pane, error) {
	p := runParams(run)
	p["pane_id"], p["direction"] = target, "right"
	if down {
		p["direction"] = "down"
	}
	r, err := a.call("pane.split", p)
	if err != nil {
		return tmuxshim.Pane{}, err
	}
	return paneOf(r["pane"]), nil
}

func (a *apiPanes) NewTab(near, name string, run tmuxshim.Run) (tmuxshim.Pane, error) {
	got, err := a.call("pane.get", map[string]any{"pane_id": near})
	if err != nil {
		return tmuxshim.Pane{}, err
	}
	m, _ := got["pane"].(map[string]any)
	p := runParams(run)
	p["workspace_id"] = text(m["workspace_id"])
	if name != "" {
		p["name"] = name
	}
	r, err := a.call("tab.create", p)
	if err != nil {
		return tmuxshim.Pane{}, err
	}
	return paneOf(r["root_pane"]), nil
}

func (a *apiPanes) Respawn(pane string, run tmuxshim.Run) error {
	p := runParams(run)
	p["pane_id"] = pane
	_, err := a.call("pane.respawn", p)
	return err
}

func (a *apiPanes) Close(pane string) error {
	_, err := a.call("pane.close", map[string]any{"pane_id": pane})
	return err
}

func (a *apiPanes) Rename(pane, title string) error {
	_, err := a.call("pane.rename", map[string]any{"pane_id": pane, "label": title})
	return err
}

func (a *apiPanes) Focus(pane string) error {
	_, err := a.call("pane.focus", map[string]any{"pane_id": pane})
	return err
}

func (a *apiPanes) Read(pane string, lines int) (string, error) {
	p := map[string]any{"pane_id": pane, "source": "visible"}
	if lines > 0 {
		p["source"], p["lines"] = "recent", lines
	}
	r, err := a.call("pane.read", p)
	if err != nil {
		return "", err
	}
	return text(r["text"]), nil
}

func (a *apiPanes) SendText(pane, s string) error {
	_, err := a.call("pane.send_text", map[string]any{"pane_id": pane, "text": s})
	return err
}

func (a *apiPanes) SendKeys(pane string, keys []string) error {
	_, err := a.call("pane.send_keys", map[string]any{"pane_id": pane, "keys": keys})
	return err
}
