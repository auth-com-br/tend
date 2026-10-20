package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/client"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/web"
	"golang.org/x/term"
)

// `tend web`: the session's agents on a phone (internal/web). It is a client
// of the session like the TUI, serving a page on the home network — or a
// VPN, by its address — paired by a token the URL it prints carries. It runs
// until stopped, and follows the session across a restart of its server.

const webUsage = `usage: tend web [-s session] [-listen address:port] [-new-token]
       tend web [-s session] -set-password

serves the session's agents to a phone on this network: watch them, message
them, answer what they ask, wake them and put them to sleep. Open the address
it prints on the phone, or sign in there with the login -set-password sets;
either pairs that phone.`

func runWeb(args []string) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, webUsage); fs.PrintDefaults() }
	name := sessionFlag(fs)
	listen := fs.String("listen", "", "address to listen on (default: [web] listen, else 0.0.0.0:7777)")
	fresh := fs.Bool("new-token", false, "make a new token, unpairing every phone paired before")
	setPassword := fs.Bool("set-password", false, "set the login and password a phone signs in with, and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *listen == "" {
		*listen = "0.0.0.0:7777"
		if cfg, err := config.LoadLenient(); err == nil && cfg.Web.Listen != "" {
			*listen = cfg.Web.Listen
		}
	}
	session := *name
	if session == "" {
		session = transport.DefaultSessionName
	}
	stateDir, err := transport.StateDir()
	if err != nil {
		return err
	}
	loginPath := web.PasswordPath(stateDir, session)
	if *setPassword {
		return setWebPassword(loginPath)
	}
	token, err := web.LoadToken(web.TokenPath(stateDir, session), *fresh)
	if err != nil {
		return err
	}

	link := &webSession{name: *name}
	srv := web.New(link, token).WithLogin(loginPath)
	link.srv = srv
	if err := link.dial(); err != nil {
		return err
	}
	go link.keep()

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	urls := web.URLs(ln.Addr().(*net.TCPAddr), token)
	// Said where the session's server reads it: the menu's phone view
	// shows these addresses and stops this process by its pid.
	statusPath := web.StatusPath(stateDir, session)
	if err := web.WriteStatus(statusPath, web.Status{Pid: os.Getpid(), Listen: ln.Addr().String(), URLs: urls}); err != nil {
		fmt.Fprintf(os.Stderr, "%s could not write %s: %v\n", tag(), statusPath, err)
	}
	fmt.Fprintf(os.Stderr, "%s session %q on the web — open one of these on the phone (it pairs it):\n", tag(), session)
	for _, u := range urls {
		fmt.Fprintln(os.Stderr, "  "+u)
	}
	if creds, err := web.LoadCredentials(loginPath); err == nil {
		fmt.Fprintf(os.Stderr, "or open it without the token and sign in as %q.\n", creds.User)
	} else {
		fmt.Fprintln(os.Stderr, "`tend web -set-password` sets a login, to sign in on the phone instead of typing the token.")
	}
	fmt.Fprintln(os.Stderr, "anyone with the address can message the agents: keep it to your own devices. ctrl-c stops.")
	return (&http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}).Serve(ln)
}

// setWebPassword asks for a login and a password, twice, unechoed, and
// keeps the password's hash.
func setWebPassword(path string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("-set-password asks for the password at a terminal")
	}
	in := bufio.NewReader(os.Stdin)
	def := os.Getenv("USER")
	fmt.Fprintf(os.Stderr, "login [%s]: ", def)
	user, _ := in.ReadString('\n')
	if user = strings.TrimSpace(user); user == "" {
		user = def
	}
	ask := func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(raw), err
	}
	first, err := ask("password (8 characters or more): ")
	if err != nil {
		return err
	}
	again, err := ask("the same again: ")
	if err != nil {
		return err
	}
	if first != again {
		return errors.New("the two passwords differ; nothing was changed")
	}
	creds, err := web.NewCredentials(user, first)
	if err != nil {
		return err
	}
	if err := web.SaveCredentials(path, creds); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s login %q set: open the page on the phone and sign in. A running tend web takes it at once.\n", tag(), user)
	return nil
}

// webSession is the session as the page sees it: a client connection made
// again when the session's server restarts, so the page outlives that.
type webSession struct {
	name string
	srv  *web.Server

	mu   sync.Mutex
	c    *client.Client
	lost chan struct{}
}

var errNoSession = errors.New("the session is not answering; it is being reconnected")

func (w *webSession) dial() error {
	h := &webHandler{w: w, lost: make(chan struct{})}
	c, err := connect(w.name, h)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.c, w.lost = c, h.lost
	w.mu.Unlock()
	return nil
}

// keep dials again whenever the connection is lost.
func (w *webSession) keep() {
	for {
		w.mu.Lock()
		lost := w.lost
		w.mu.Unlock()
		<-lost
		w.mu.Lock()
		w.c = nil
		w.mu.Unlock()
		for w.dial() != nil {
			time.Sleep(2 * time.Second)
		}
		w.srv.Reconnected()
	}
}

func (w *webSession) client() (*client.Client, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.c == nil {
		return nil, errNoSession
	}
	return w.c, nil
}

func (w *webSession) Snapshot() (proto.SessionSnapshot, error) {
	c, err := w.client()
	if err != nil {
		return proto.SessionSnapshot{}, err
	}
	return c.Snapshot()
}

func (w *webSession) SubscribePanes(panes []uint64) error {
	c, err := w.client()
	if err != nil {
		return err
	}
	return c.SubscribePanes(panes)
}

func (w *webSession) PaneScreen(pane uint64) (proto.PaneScreenResult, error) {
	c, err := w.client()
	if err != nil {
		return proto.PaneScreenResult{}, err
	}
	return c.PaneScreen(pane)
}

func (w *webSession) PastePane(pane uint64, text string) error {
	c, err := w.client()
	if err != nil {
		return err
	}
	return c.PastePane(pane, text)
}

func (w *webSession) SendInput(pane uint64, data []byte) error {
	c, err := w.client()
	if err != nil {
		return err
	}
	return c.SendInput(pane, data)
}

func (w *webSession) SleepPane(pane uint64) (proto.PaneSleepResult, error) {
	c, err := w.client()
	if err != nil {
		return proto.PaneSleepResult{}, err
	}
	return c.SleepPane(pane)
}

func (w *webSession) WakePane(pane uint64) error {
	c, err := w.client()
	if err != nil {
		return err
	}
	return c.WakePane(pane)
}

// webHandler hands the session's events and screens to the page's server.
type webHandler struct {
	w    *webSession
	lost chan struct{}
	once sync.Once
}

func (h *webHandler) Disconnected() { h.once.Do(func() { close(h.lost) }) }

func (h *webHandler) Event(ev proto.Event) { h.w.srv.Event(ev) }

func (h *webHandler) PaneOutput(pane uint64, data []byte) { h.w.srv.PaneOutput(pane, data) }
