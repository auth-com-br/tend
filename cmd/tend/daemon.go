package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/auth-com-br/tend/internal/activity"
	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/policy"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/server"
	"github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/transport"
)

// sessionFlag adds the -s flag every client command shares.
func sessionFlag(fs *flag.FlagSet) *string {
	// Registered alongside the session name, so every command that can name a
	// session can also say which machine it is on. A remote session that
	// could be attached to but not listed or killed would be half a feature.
	fs.StringVar(&remoteHost, "ssh", "", "the machine the session is on, as `user@host`")
	return fs.String("s", transport.DefaultSessionName, "session name")
}

// remoteHost is the machine named by -ssh, or empty for this one. One process
// talks to one session, so this is set once while the flags are read.
var remoteHost string

// connect dials a session, reporting the missing-server case in a way that
// says what to do about it rather than quoting a socket error.
//
// It does not start a server. Commands that inspect or change a session
// should fail when there is none, because creating one on the way to listing
// it would report an empty session rather than the absence of one. Commands
// that are meant to put you in a session use openSession instead.
func connect(name string, handler client.Handler) (*client.Client, error) {
	if remoteHost != "" {
		return client.DialRemote(transport.RemoteArgv(remoteHost, name), handler)
	}
	path, err := transport.SocketPath(name)
	if err != nil {
		return nil, err
	}
	c, err := client.Dial(path, handler)
	if err != nil {
		if notRunning(err) {
			return nil, fmt.Errorf("no session %q; start one with \"tend attach -s %s\"", name, name)
		}
		return nil, err
	}
	return c, nil
}

// notRunning reports whether the error means there is nothing listening, as
// opposed to something being wrong with a server that is.
func notRunning(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// openSession connects, starting a server first if there is none.
//
// Needing a daemon is tend's problem, not the user's: a session is what they
// asked for, and a second terminal running a server is a step that exists
// only because of how this is built. The server outlives the client either
// way, so starting it here changes nothing about what happens afterwards.
func openSession(name string, handler client.Handler) (*client.Client, error) {
	return openSessionOn(remoteHost, name, handler)
}

// openSessionOn opens a session on another machine when host is set, and here
// otherwise. Starting the server is the far side's job in the first case:
// `tend bridge` does there what this function does here.
func openSessionOn(host, name string, handler client.Handler) (*client.Client, error) {
	if host != "" {
		return client.DialRemote(transport.RemoteArgv(host, name), handler)
	}
	path, err := transport.SocketPath(name)
	if err != nil {
		return nil, err
	}

	c, err := client.Dial(path, handler)
	if err == nil {
		return c, nil
	}
	if !notRunning(err) {
		return nil, err
	}

	if err := startServer(name); err != nil {
		return nil, err
	}

	// Wait for it to come up. Two clients starting at once is fine: the second
	// server fails to take the socket and exits, and the client that started it
	// connects to the first one.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := client.Dial(path, handler); err == nil {
			return c, nil
		} else if !notRunning(err) {
			return nil, err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("started a server for %q but it never came up; see %s", name, serverLog(path))
}

// startServer launches this same binary as a background server.
func startServer(name string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding tend: %w", err)
	}
	path, err := transport.SocketPath(name)
	if err != nil {
		return err
	}

	// The server's output goes to a file rather than nowhere. It is the only
	// record of why a session failed to start, and a background process with
	// no output is one that fails silently.
	// The directory is made here, not left to the server. The log is opened
	// before the server exists, and the runtime directory is on a filesystem
	// that is emptied at logout: the first tend after a reboot found nothing
	// there and failed on its own log file, before it had started anything.
	// Private, like the socket that goes in it.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating the runtime directory: %w", err)
	}
	log, err := os.OpenFile(serverLog(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening the server log: %w", err)
	}
	defer log.Close()

	cmd := exec.Command(self, "serve", "-s", name)
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	detach(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting a server for %q: %w", name, err)
	}
	// Waited on in the background, purely to reap it. The server is meant to
	// outlive this process and does — waiting does not hold it back — but a
	// child that exits before its parent stays a zombie until somebody asks
	// how it went. Most of these exit at once, because starting a server when
	// one already holds the socket is the ordinary way two clients race, and
	// a long-lived client collects one corpse per attempt.
	go func() { _ = cmd.Wait() }()
	return nil
}

// serverLog is where a background server writes, beside its socket.
func serverLog(socketPath string) string {
	return strings.TrimSuffix(socketPath, ".sock") + ".log"
}

// runServe runs the daemon in the foreground.
//
// It stays in the foreground on purpose. Daemonising is the shell's job — and
// a server that forks away loses the one thing that makes failures visible,
// which is its own output on a terminal.
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	name := sessionFlag(fs)
	interval := fs.Duration("interval", 0, "how often panes are re-examined (default: from the config file)")
	cols := fs.Int("cols", 120, "default pane width")
	rows := fs.Int("rows", 40, "default pane height")
	inherited := fs.Bool("inherit", false, "take over from the server that started this one (not for use by hand)")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend serve [options]\n\n"+
				"runs the session server in the foreground. panes outlive every client,\n"+
				"so clients may come and go while agents keep working.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	detect := *interval
	if detect <= 0 {
		detect, _ = cfg.DetectInterval()
	}

	path, err := transport.SocketPath(*name)
	if err != nil {
		return err
	}
	apiPath, err := transport.APISocketPath(*name)
	if err != nil {
		return err
	}

	stateFile := ""
	if cfg.Server.Persist {
		if stateFile, err = transport.StatePath(*name); err != nil {
			return err
		}
	}
	srvCfg := server.Config{
		Build:          version,
		StateFile:      stateFile,
		DetectInterval: detect,
		Scrollback:     cfg.Scrollback(),
		Shell:          cfg.Shell(),
		DefaultSize:    pty.Size{Cols: uint16(*cols), Rows: uint16(*rows)},
		// The release check reads the settings file each time, so turning
		// [update] version_check off or on needs no restart.
		UpdateSource: func() (string, bool) {
			c, err := config.LoadLenient()
			if err != nil {
				return "", false
			}
			return c.ManifestURL(), c.Update.VersionCheck
		},
		FakeUpdate: os.Getenv("TEND_FAKE_UPDATE_VERSION"),
	}
	if path, err := config.Path(); err == nil {
		srvCfg.NotesPath = filepath.Join(filepath.Dir(path), "release-notes.json")
	}
	// The record of what agents did goes beside the sessions, one file for
	// all of them, so a report can span every session on the machine.
	srvCfg.SessionName = *name
	if srvCfg.SessionName == "" {
		srvCfg.SessionName = transport.DefaultSessionName
	}
	if cfg.RecordActivity() {
		if dir, err := transport.StateDir(); err == nil {
			srvCfg.Activity = activity.NewLog(activity.Path(dir))
		}
	}
	// Read again at every check, as the release check is, so a rule
	// written into the settings file holds from the next agent on. A file
	// that stopped parsing keeps the rules the server started with rather
	// than dropping them.
	srvCfg.Policy = func() policy.Policy {
		c, err := config.LoadLenient()
		if err != nil {
			return cfg.Policy
		}
		return c.Policy
	}

	// The replacement is whatever binary is at this one's path by then, which
	// after an install is the new build — that being the point. It is started
	// the way this one was, plus the word that says its panes are waiting.
	self, selfErr := os.Executable()
	bin := ""
	if selfErr == nil {
		bin = self
	}
	// Plugins get the same door a hook does: the automation socket, and the
	// marker that says a program is running inside tend.
	pluginEnv := []string{api.EnvMarker + "=1", api.EnvSocketPath + "=" + apiPath}
	if selfErr == nil {
		pluginEnv = append(pluginEnv, api.EnvBinPath+"="+self)
	}
	srvCfg.Plugins = pluginHost(pluginEnv)
	srvCfg.CommandEnv = pluginEnv

	srvCfg.PaneEnv = func(id session.PaneID) []string {
		return api.PaneEnv(apiPath, id, bin)
	}
	replacement := []string{self, "serve", "-s", *name, "-inherit",
		"-cols", strconv.Itoa(*cols), "-rows", strconv.Itoa(*rows)}
	if *interval > 0 {
		replacement = append(replacement, "-interval", interval.String())
	}

	var (
		srv   *server.Server
		ln    net.Listener
		apiLn net.Listener
	)
	if *inherited {
		// Replace is set through listeners that do not exist until the server
		// does, hence the indirection. The automation socket may be missing
		// when the parent was from before this feature: start one then.
		if selfErr == nil {
			srvCfg.Replace = func(h *server.Handoff) error {
				return replaceWith(ln, apiLn, replacement)(h)
			}
		}
		if srv, ln, apiLn, err = inherit(srvCfg); err != nil {
			return err
		}
		if apiLn == nil {
			if apiLn, err = transport.Listen(apiPath); err != nil {
				_ = ln.Close()
				_ = srv.Close()
				return err
			}
		}
	} else {
		if ln, err = transport.Listen(path); err != nil {
			return err
		}
		if apiLn, err = transport.Listen(apiPath); err != nil {
			_ = ln.Close()
			return err
		}
		if selfErr == nil {
			srvCfg.Replace = replaceWith(ln, apiLn, replacement)
		}
		if srv, err = server.New(srvCfg); err != nil {
			_ = ln.Close()
			_ = apiLn.Close()
			return err
		}
	}
	defer ln.Close()
	defer apiLn.Close()

	// Started once the server is up and answering: a startup command that
	// calls back into the session must find it there.
	srv.ConfigureTabBar(cfg.UI.TabBarRight, cfg.UI.TabBarSeparator)
	srv.RunStartupPlugins()

	fmt.Fprintf(os.Stderr, "%s session %q listening on %s\n", tag(), *name, path)
	fmt.Fprintf(os.Stderr, "%s automation on %s\n", tag(), apiPath)

	automation := api.New(srv, version, cfg.Shell())
	automation.SetWorktreeDir(cfg.Worktrees.Directory)
	defer automation.Close()
	go func() {
		if err := automation.Serve(apiLn); err != nil {
			fmt.Fprintf(os.Stderr, "%s automation socket: %v\n", tag(), err)
		}
	}()

	// One shutdown path: a signal closes the server, which hangs up the panes
	// and the clients, which ends Serve.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	go func() {
		<-stop
		fmt.Fprintf(os.Stderr, "\n%s stopping session %q\n", tag(), *name)
		automation.Close()
		_ = srv.Close()
		_ = ln.Close()
		_ = apiLn.Close()
	}()

	if err := srv.Serve(ln); err != nil {
		return err
	}
	return srv.Close()
}

// runList prints the panes of a session, or the sessions themselves.
func runList(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	name := sessionFlag(fs)
	all := fs.Bool("sessions", false, "list sessions instead of panes")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend ls [-s session] [-sessions]\n\n"+
				"lists the panes of a session, or every session.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *all {
		return listSessions()
	}

	c, err := connect(*name, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	if len(snap.Panes) == 0 {
		fmt.Fprintf(os.Stderr, "%s session %q has no panes\n", tag(), *name)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PANE\tCOMMAND\tAGENT\tSTATE\tTITLE")
	sort.Slice(snap.Panes, func(i, j int) bool { return snap.Panes[i].ID < snap.Panes[j].ID })
	for _, p := range snap.Panes {
		state := p.State
		if !p.Running {
			state = "exited"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			p.ID, orDash(commandName(p.Command)), orDash(p.Agent), state, orDash(p.Title))
	}
	return w.Flush()
}

func listSessions() error {
	names, err := transport.Sessions()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintf(os.Stderr, "%s no sessions\n", tag())
		return nil
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tPANES")
	for _, name := range names {
		// A socket file is not a running server: one left by a crash looks
		// identical until something tries to connect.
		count := "unreachable"
		if c, err := connect(name, nil); err == nil {
			if snap, err := c.Snapshot(); err == nil {
				count = strconv.Itoa(len(snap.Panes))
			}
			_ = c.Close()
		}
		fmt.Fprintf(w, "%s\t%s\n", name, count)
	}
	return w.Flush()
}

func commandName(cmd []string) string {
	if len(cmd) == 0 {
		return ""
	}
	return filepath.Base(cmd[0])
}

// runNew opens a pane in a running session.
func runNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	name := sessionFlag(fs)
	split := fs.Uint64("split", 0, "split this pane instead of opening a new tab")
	dir := fs.String("dir", "columns", "split direction: columns or rows")
	cwd := fs.String("cwd", "", "working directory for the pane")
	agentName := fs.String("agent", "", "detection manifest (default: inferred from the command)")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend new [options] -- command [args...]\n\n"+
				"opens a pane in a running session.\n\n"+
				"examples:\n"+
				"  tend new -- claude\n"+
				"  tend new -split 1 -dir rows -- codex\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	command := fs.Args()
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	if len(command) == 0 {
		fs.Usage()
		return errors.New("no command given")
	}

	c, err := openSession(*name, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	spec := proto.PaneSpec{Command: command, Dir: *cwd, Agent: *agentName}

	if *split != 0 {
		pane, err := c.SplitPane(*split, *dir, spec)
		if err != nil {
			return err
		}
		fmt.Println(pane)
		return nil
	}

	// No workspace yet means this is the first pane of the session, so make
	// one rather than telling the user to.
	snap, err := c.Snapshot()
	if err != nil {
		return err
	}
	ws := snap.ActiveWorkspace
	if ws == 0 {
		if ws, err = c.NewWorkspace("main"); err != nil {
			return err
		}
	}
	_, pane, err := c.NewTab(ws, commandName(command), spec)
	if err != nil {
		return err
	}
	fmt.Println(pane)
	return nil
}

// runKill closes a pane, or stops the whole session.
func runKill(args []string) error {
	fs := flag.NewFlagSet("kill", flag.ExitOnError)
	name := sessionFlag(fs)
	server := fs.Bool("server", false, "stop the session server and every pane in it")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend kill [-s session] <pane>...\n"+
				"       tend kill [-s session] -server\n\n"+
				"closes panes, or stops the whole session.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := connect(*name, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	if *server {
		if err := c.Shutdown(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s session %q stopped\n", tag(), *name)
		return nil
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return errors.New("no panes given")
	}
	for _, arg := range fs.Args() {
		id, err := strconv.ParseUint(arg, 10, 64)
		if err != nil {
			return fmt.Errorf("pane id %q: %w", arg, err)
		}
		if err := c.ClosePane(id); err != nil {
			return err
		}
	}
	return nil
}

// runHandoff replaces a session's server with the binary now installed,
// keeping what runs in its panes.
func runHandoff(args []string) error {
	fs := flag.NewFlagSet("handoff", flag.ExitOnError)
	name := sessionFlag(fs)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend handoff [-s session]\n\n"+
				"replaces the session's server with the tend now installed. the programs\n"+
				"in its panes keep running: their terminals are handed to the new server.\n"+
				"attached clients reconnect on their own. if the new server fails to\n"+
				"start, the old one carries on as it was.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := connect(*name, nil)
	if err != nil {
		return err
	}
	defer c.Close()

	// To this binary: the one that asked is the one meant, wherever it is
	// installed, as herdr hands off to the updated executable.
	self, _ := os.Executable()
	if err := c.HandoffTo(self); err != nil {
		if errors.Is(err, proto.ErrUnknownMethod) {
			return fmt.Errorf("the server running %q predates handoff; only a restart (tend kill -s %s -server) replaces it, and that ends its programs", *name, *name)
		}
		return err
	}
	fmt.Fprintf(os.Stderr, "%s session %q is on a new server, with its panes\n", tag(), *name)
	return nil
}

// watchHandler prints what the server pushes.
type watchHandler struct {
	quiet bool
}

func (h watchHandler) Event(ev proto.Event) {
	switch ev.Kind {
	case proto.EventPaneState:
		fmt.Fprintf(os.Stderr, "%s pane %d  %s%-8s%s  %s\n",
			tag(), ev.Pane, stateColorName(ev.State), ev.State, reset(), ev.Rule)
	case proto.EventPaneExited:
		fmt.Fprintf(os.Stderr, "%s pane %d exited%s\n", tag(), ev.Pane, errSuffix(ev.Err))
	case proto.EventPaneOpened:
		fmt.Fprintf(os.Stderr, "%s pane %d opened\n", tag(), ev.Pane)
	case proto.EventPaneClosed:
		fmt.Fprintf(os.Stderr, "%s pane %d closed\n", tag(), ev.Pane)
	}
}

// Disconnected ends the follow: there is nothing left to report.
func (h watchHandler) Disconnected() {
	fmt.Fprintf(os.Stderr, "%s the session ended\n", tag())
}

func (h watchHandler) PaneOutput(pane uint64, data []byte) {
	if h.quiet {
		return
	}
	fmt.Printf("\n--- pane %d ---\n%s\n", pane, strings.TrimRight(string(data), "\n"))
}

// runFollow prints a session's events, and optionally a pane's screen, as they
// arrive.
//
// It is the diagnostic view of the push path, not the interface: no drawing,
// no input, just what the server is saying. That makes it the right tool when
// the question is whether the server is reporting something, rather than
// whether the client is drawing it.
func runFollow(args []string) error {
	fs := flag.NewFlagSet("follow", flag.ExitOnError)
	name := sessionFlag(fs)
	watchPane := fs.Uint64("pane", 0, "also follow this pane's screen")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend follow [-s session] [-pane id]\n\n"+
				"prints a session's events until interrupted. with -pane, also prints\n"+
				"that pane's screen as it changes.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := connect(*name, watchHandler{quiet: *watchPane == 0})
	if err != nil {
		return err
	}
	defer c.Close()

	if *watchPane != 0 {
		if err := c.SubscribePanes([]uint64{*watchPane}); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "%s following %q. ctrl-c to stop.\n", tag(), *name)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	<-stop

	fmt.Fprintf(os.Stderr, "\n%s stopped following; the session keeps running\n", tag())
	return nil
}

func stateColorName(state string) string {
	if !colorsEnabled() {
		return ""
	}
	switch state {
	case "working":
		return "\x1b[33m"
	case "blocked":
		return "\x1b[31m"
	case "idle":
		return "\x1b[32m"
	default:
		return "\x1b[2m"
	}
}
