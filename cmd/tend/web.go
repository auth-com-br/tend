package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
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

const webUsage = `usage: tend web [-s session] [-listen address:port] [-https] [-new-token]
       tend web [-s session] -set-password

serves the session's agents to a phone on this network: watch them, message
them, answer what they ask, wake them and put them to sleep. Open the address
it prints on the phone, or sign in there with the login -set-password sets;
either pairs that phone. With -https (or [web] https = true) the page is
served over TLS, which a phone needs to be notified with the page closed.`

func runWeb(args []string) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	fs.Usage = func() { fmt.Fprintln(os.Stderr, webUsage); fs.PrintDefaults() }
	name := sessionFlag(fs)
	listen := fs.String("listen", "", "address to listen on (default: [web] listen, else 0.0.0.0:7777)")
	fresh := fs.Bool("new-token", false, "make a new token, unpairing every phone paired before")
	setPassword := fs.Bool("set-password", false, "set the login and password a phone signs in with, and exit")
	https := fs.Bool("https", false, "serve over TLS, for notifications with the page closed (default: [web] https)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var webCfg config.Web
	if cfg, err := config.LoadLenient(); err == nil {
		webCfg = cfg.Web
	}
	if *listen == "" {
		*listen = "0.0.0.0:7777"
		if webCfg.Listen != "" {
			*listen = webCfg.Listen
		}
	}
	useTLS := *https || webCfg.HTTPS
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
	scheme := "http"
	var caNote string
	if useTLS {
		scheme = "https"
		tlsCfg, note, err := webTLS(stateDir, session, webCfg, ln.Addr().(*net.TCPAddr))
		if err != nil {
			_ = ln.Close()
			return err
		}
		caNote = note
		ln = tls.NewListener(ln, tlsCfg)
		// Notifications with the page closed: a browser offers them only to
		// a secure page, so only here.
		key, err := web.LoadVAPID(web.VAPIDPath(stateDir, session))
		if err != nil {
			return err
		}
		var caPEM []byte
		if webCfg.Cert == "" {
			_, _, caPEM, _ = web.LoadCA(stateDir)
		}
		srv.WithPush(web.NewPusher(key, web.PushPath(stateDir, session), "mailto:tend@localhost"), caPEM)
		srv.PrimePhones()
	}
	urls := web.URLsFor(scheme, ln.Addr().(*net.TCPAddr), token)
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
	if caNote != "" {
		fmt.Fprint(os.Stderr, caNote)
	}
	fmt.Fprintln(os.Stderr, "anyone with the address can message the agents: keep it to your own devices. ctrl-c stops.")
	return (&http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}).Serve(ln)
}

// setWebPassword asks for a login and a password, twice, unechoed, and
// keeps the password's hash.
// webTLS is the page's TLS: the certificate the settings name, or one the
// local authority signs for this machine's addresses (internal/web/tls.go),
// with what to tell the person about making the phone trust it.
func webTLS(stateDir, session string, cfg config.Web, addr *net.TCPAddr) (*tls.Config, string, error) {
	if cfg.Cert != "" || cfg.Key != "" {
		cert, err := tls.LoadX509KeyPair(expandHome(cfg.Cert), expandHome(cfg.Key))
		if err != nil {
			return nil, "", fmt.Errorf("[web] cert/key: %w", err)
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, "", nil
	}
	ca, caKey, _, err := web.LoadCA(stateDir)
	if err != nil {
		return nil, "", fmt.Errorf("web: the local certificate authority: %w", err)
	}
	var hosts []string
	for _, u := range web.URLsFor("https", addr, "") {
		if h, _, err := net.SplitHostPort(strings.TrimSuffix(strings.TrimPrefix(u, "https://"), "/?t=")); err == nil {
			hosts = append(hosts, h)
		}
	}
	if name, err := os.Hostname(); err == nil {
		hosts = append(hosts, name, name+".local")
	}
	leaf, err := web.LeafFor(ca, caKey, hosts)
	if err != nil {
		return nil, "", err
	}
	certPath, _ := web.CAPaths(stateDir)
	port := strconv.Itoa(addr.Port)
	host := "this machine"
	if len(hosts) > 0 {
		host = net.JoinHostPort(hosts[0], port)
	}
	note := "the page's certificate is signed by tend's own authority (" + certPath + "), limited to private networks.\n" +
		"on the phone, once: open https://" + host + "/ca.crt (accept the warning this one time), then\n" +
		"  Android: Settings › Security › Encryption & credentials › Install a certificate › CA certificate\n" +
		"  iPhone: Settings › Profile Downloaded › Install, then General › About › Certificate Trust Settings › on\n" +
		"after that, the bell on the page turns notifications on.\n"
	return &tls.Config{Certificates: []tls.Certificate{leaf}, MinVersion: tls.VersionTLS12}, note, nil
}

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
