package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
)

// Session is what the page needs of a session: *client.Client is one. An
// interface so the server can be tested without a session behind it.
type Session interface {
	Snapshot() (proto.SessionSnapshot, error)
	SubscribePanes(panes []uint64) error
	PaneScreen(pane uint64) (proto.PaneScreenResult, error)
	PastePane(pane uint64, text string) error
	SendInput(pane uint64, data []byte) error
	SleepPane(pane uint64) (proto.PaneSleepResult, error)
	WakePane(pane uint64) error
}

//go:embed assets
var assets embed.FS

const (
	cookieName = "tend_web"
	// submitDelay is herdr's pause between a prompt's text and its Enter
	// (server.SubmitDelay): an agent's prompt box that takes the Enter
	// before the paste sends an empty prompt.
	submitDelay = 300 * time.Millisecond
	// screenEvery is how often one pane's screen is sent at most: a phone
	// reads, it does not need every frame of a streaming answer.
	screenEvery = 250 * time.Millisecond
	// agentsEvery gathers the events that change the list into one send.
	agentsEvery = 300 * time.Millisecond
	// maxFailures wrong tokens a minute, and every unpaired request is
	// refused until the minute is out.
	maxFailures = 10
	maxPrompt   = 64 << 10
)

// keys are the keys a phone may send an agent: the ones an agent asks for —
// an approval's digit, a menu's arrows, Enter, Esc. Nothing that makes a
// shell of the pane.
var keys = map[string]string{
	"enter": "\r", "esc": "\x1b", "up": "\x1b[A", "down": "\x1b[B", "tab": "\t",
	"1": "1", "2": "2", "3": "3", "4": "4", "5": "5", "6": "6", "7": "7", "8": "8", "9": "9",
}

// Server serves the page and its API for one session.
type Server struct {
	sess  Session
	token string
	now   func() time.Time
	// loginPath is where the login and password hash are (auth.go), read
	// at each sign-in so `tend web -set-password` needs no restart; ""
	// is no login.
	loginPath string

	mu       sync.Mutex
	watchers map[*watcher]struct{}
	// subscribed is the set of panes asked of the session, the union of
	// what every watcher looks at.
	subscribed string
	screens    map[uint64]*paneScreen
	failures   []time.Time
	listDue    bool
	// phone is push to phones (phone.go), nil when it is off.
	phone *phone
}

// watcher is one open page: the pane it shows, if any, and where its
// messages go.
type watcher struct {
	pane uint64
	out  chan sseMessage
}

type sseMessage struct {
	event string
	data  []byte
}

// paneScreen is the last screen of a pane, kept to send at most every
// screenEvery.
type paneScreen struct {
	cols, rows int
	ansi       []byte
	sentAt     time.Time
	pending    bool
}

// WithLogin lets a phone pair by the login kept at path, besides the token.
func (s *Server) WithLogin(path string) *Server {
	s.loginPath = path
	return s
}

// New is a server for a session, paired by token.
func New(sess Session, token string) *Server {
	return &Server{
		sess: sess, token: token, now: time.Now,
		watchers: map[*watcher]struct{}{}, screens: map[uint64]*paneScreen{},
	}
}

// assetsSub is the page's files.
func assetsSub() fs.FS {
	sub, _ := fs.Sub(assets, "assets")
	return sub
}

// Handler is the routes: the page, its files, and the API — nothing else.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assets, "assets")
	files := http.FileServer(http.FS(static))
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("POST /login", s.signIn)
	mux.Handle("POST /logout", s.paired(http.HandlerFunc(s.signOut)))
	// The manifest and the icon are what "add to home screen" reads, and
	// say nothing about the session.
	mux.Handle("GET /manifest.json", files)
	mux.Handle("GET /icon.svg", files)
	mux.Handle("GET /app.js", s.paired(files))
	mux.Handle("GET /app.css", s.paired(files))
	mux.Handle("GET /api/agents", s.paired(http.HandlerFunc(s.agents)))
	mux.Handle("GET /api/events", s.paired(http.HandlerFunc(s.events)))
	mux.Handle("POST /api/prompt", s.paired(s.action(s.prompt)))
	mux.Handle("POST /api/key", s.paired(s.action(s.key)))
	mux.Handle("POST /api/sleep", s.paired(s.action(s.sleep)))
	mux.Handle("POST /api/wake", s.paired(s.action(s.wake)))
	s.phoneRoutes(mux)
	return securityHeaders(mux)
}

// securityHeaders keeps the page from being framed or sniffed, and from
// loading anything that is not its own.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		// same-origin, not no-referrer: with no-referrer a browser sends a form's
		// Origin as "null", and the sign-in could not tell this page from another.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// isPaired is whether a request carries the token, by cookie.
func (s *Server) isPaired(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1
}

// limited is whether too many wrong tokens came in the last minute; it
// counts this one when it is wrong.
func (s *Server) limited(wrong bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := s.now().Add(-time.Minute)
	kept := s.failures[:0]
	for _, t := range s.failures {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	s.failures = kept
	if wrong {
		s.failures = append(s.failures, s.now())
	}
	return len(s.failures) >= maxFailures
}

func (s *Server) paired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isPaired(r) {
			s.limited(r.Header.Get("Cookie") != "")
			http.Error(w, "this device is not paired: open the address `tend web` printed", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// page is the app, for a paired phone; pairing, for a URL carrying the
// token, which is set as a cookie and taken out of the address bar.
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if t := r.URL.Query().Get("t"); t != "" {
		if s.limited(false) {
			http.Error(w, "too many wrong tokens; wait a minute", http.StatusTooManyRequests)
			return
		}
		if subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) != 1 {
			s.limited(true)
			http.Error(w, "wrong token: open the address `tend web` printed", http.StatusUnauthorized)
			return
		}
		s.setCookie(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.isPaired(r) {
		s.loginPage(w, r.URL.Query().Get("e") != "")
		return
	}
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// loginPage is what an unpaired phone sees: a login, or how to set one.
func (s *Server) loginPage(w http.ResponseWriter, failed bool) {
	data, err := assets.ReadFile("assets/login.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	note := "Sign in to watch and message your agents."
	if _, err := s.credentials(); err != nil {
		note = "No login is set yet. On the computer run: tend web -set-password — or open the address tend web printed."
	}
	msg := ""
	if failed {
		msg = "Wrong login or password."
	}
	page := strings.NewReplacer("{{NOTE}}", html.EscapeString(note), "{{ERROR}}", html.EscapeString(msg)).Replace(string(data))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

func (s *Server) credentials() (Credentials, error) {
	if s.loginPath == "" {
		return Credentials{}, ErrNoLogin
	}
	return LoadCredentials(s.loginPath)
}

// failedSignInDelay is how long a wrong password waits before it is
// answered: nothing to a person, a wall to a script guessing.
const failedSignInDelay = 700 * time.Millisecond

// signIn checks a login and password from the login page and, right, pairs
// the phone as the token's address does.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || u.Host != r.Host {
			http.Error(w, "not from this page", http.StatusForbidden)
			return
		}
	}
	if s.limited(false) {
		http.Error(w, "too many wrong attempts; wait a minute", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	user, password := r.PostFormValue("user"), r.PostFormValue("password")
	creds, err := s.credentials()
	if err != nil || !creds.Check(user, password) {
		s.limited(true)
		time.Sleep(failedSignInDelay)
		http.Redirect(w, r, "/?e=1", http.StatusSeeOther)
		return
	}
	s.setCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// signOut unpairs this phone; the others stay.
func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) setCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600,
	})
}

func (s *Server) agents(w http.ResponseWriter, r *http.Request) {
	snap, err := s.sess.Snapshot()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, Agents(snap))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// errNotAnAgent is a pane that is not an agent's: the page acts on agents
// only, never on a shell.
var errNotAnAgent = errors.New("that pane is not an agent's")

// actionRequest is what every action names: the pane, and for some a text
// or a key.
type actionRequest struct {
	Pane uint64 `json:"pane"`
	Text string `json:"text,omitempty"`
	Key  string `json:"key,omitempty"`
}

// action reads an action's request — JSON, from this page and no other —
// checks its pane is an agent's, and does it.
func (s *Server) action(do func(actionRequest) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			http.Error(w, "actions are JSON", http.StatusUnsupportedMediaType)
			return
		}
		// A page of another site open on the phone cannot act: the
		// browser says where a request comes from.
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || u.Host != r.Host {
				http.Error(w, "not from this page", http.StatusForbidden)
				return
			}
		}
		var req actionRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPrompt)).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.isAgent(req.Pane); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		out, err := do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, out)
	})
}

func (s *Server) isAgent(pane uint64) error {
	snap, err := s.sess.Snapshot()
	if err != nil {
		return err
	}
	for _, p := range snap.Panes {
		if p.ID == pane && p.Agent != "" {
			return nil
		}
	}
	return errNotAnAgent
}

// prompt types a message into an agent and sends it: a paste, the pause an
// agent's prompt box needs, then Enter.
func (s *Server) prompt(req actionRequest) (any, error) {
	text := strings.TrimRight(req.Text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("the message is empty")
	}
	if err := s.sess.PastePane(req.Pane, text); err != nil {
		return nil, err
	}
	time.Sleep(submitDelay)
	return map[string]bool{"ok": true}, s.sess.SendInput(req.Pane, []byte("\r"))
}

func (s *Server) key(req actionRequest) (any, error) {
	b, ok := keys[req.Key]
	if !ok {
		return nil, fmt.Errorf("%q is not a key the page sends", req.Key)
	}
	return map[string]bool{"ok": true}, s.sess.SendInput(req.Pane, []byte(b))
}

func (s *Server) sleep(req actionRequest) (any, error) {
	return s.sess.SleepPane(req.Pane)
}

func (s *Server) wake(req actionRequest) (any, error) {
	return map[string]bool{"ok": true}, s.sess.WakePane(req.Pane)
}

// events is the page's stream: the agent list whenever it changes, and the
// screen of the pane it shows (?pane=N) as it changes.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	pane, _ := strconv.ParseUint(r.URL.Query().Get("pane"), 10, 64)
	if pane != 0 {
		if err := s.isAgent(pane); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	wt := &watcher{pane: pane, out: make(chan sseMessage, 16)}
	s.mu.Lock()
	s.watchers[wt] = struct{}{}
	s.mu.Unlock()
	s.resubscribe()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, wt)
		s.mu.Unlock()
		s.resubscribe()
	}()

	send := func(m sseMessage) bool {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.event, m.data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if snap, err := s.sess.Snapshot(); err == nil {
		data, _ := json.Marshal(Agents(snap))
		send(sseMessage{"agents", data})
	}
	if pane != 0 {
		if scr, err := s.sess.PaneScreen(pane); err == nil {
			s.mu.Lock()
			ps := s.screenLocked(pane)
			ps.cols, ps.rows, ps.ansi = scr.Cols, scr.Rows, []byte(scr.ANSI)
			s.mu.Unlock()
			send(screenMessage(pane, ScreenHTML([]byte(scr.ANSI), scr.Cols, scr.Rows)))
		}
	}
	beat := time.NewTicker(20 * time.Second)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-wt.out:
			if !send(m) {
				return
			}
		case <-beat.C:
			// A comment keeps a phone's connection, and any proxy on the
			// way, from taking a quiet stream for a dead one.
			if _, err := fmt.Fprint(w, ": beat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func screenMessage(pane uint64, html string) sseMessage {
	data, _ := json.Marshal(map[string]any{"pane": pane, "html": html})
	return sseMessage{"screen", data}
}

func (s *Server) screenLocked(pane uint64) *paneScreen {
	ps := s.screens[pane]
	if ps == nil {
		ps = &paneScreen{}
		s.screens[pane] = ps
	}
	return ps
}

// Reconnected is told when the session's connection was made again: the
// new connection has subscribed to nothing, and the panes the pages show are
// asked for again.
func (s *Server) Reconnected() {
	s.mu.Lock()
	s.subscribed = ""
	s.mu.Unlock()
	s.resubscribe()
	s.PrimePhones()
}

// resubscribe asks the session for the panes some page shows.
func (s *Server) resubscribe() {
	s.mu.Lock()
	set := map[uint64]bool{}
	for wt := range s.watchers {
		if wt.pane != 0 {
			set[wt.pane] = true
		}
	}
	panes := make([]uint64, 0, len(set))
	for p := range set {
		panes = append(panes, p)
	}
	key := fmt.Sprint(sortedPanes(panes))
	changed := key != s.subscribed
	s.subscribed = key
	s.mu.Unlock()
	if changed {
		_ = s.sess.SubscribePanes(panes)
	}
}

func sortedPanes(p []uint64) []uint64 {
	out := append([]uint64(nil), p...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// broadcast sends a message to the pages that want it, dropping it for a
// page that is not keeping up: the next one replaces it anyway.
func (s *Server) broadcastLocked(m sseMessage, pane uint64) {
	for wt := range s.watchers {
		if pane != 0 && wt.pane != pane {
			continue
		}
		select {
		case wt.out <- m:
		default:
		}
	}
}

// Event is told of the session's events (the client's handler): those that
// change the agent list send it again, once for a burst.
func (s *Server) Event(ev proto.Event) {
	s.tellPhones(ev)
	switch ev.Kind {
	case "pane-state", "pane-opened", "pane-closed", "pane-exited", "session-changed", "workspace-created", "tab-created":
	default:
		return
	}
	s.mu.Lock()
	if s.listDue || len(s.watchers) == 0 {
		s.mu.Unlock()
		return
	}
	s.listDue = true
	s.mu.Unlock()
	time.AfterFunc(agentsEvery, func() {
		snap, err := s.sess.Snapshot()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.listDue = false
		if err != nil {
			return
		}
		data, _ := json.Marshal(Agents(snap))
		s.broadcastLocked(sseMessage{"agents", data}, 0)
	})
	if ev.Kind == "pane-state" && ev.Pane != 0 {
		// A pane's size can change with its state (a split, a zoom); the
		// next screen is drawn at the size it is now.
		go s.refreshSize(ev.Pane)
	}
}

func (s *Server) refreshSize(pane uint64) {
	s.mu.Lock()
	_, known := s.screens[pane]
	s.mu.Unlock()
	if !known {
		return
	}
	if scr, err := s.sess.PaneScreen(pane); err == nil {
		s.mu.Lock()
		ps := s.screenLocked(pane)
		ps.cols, ps.rows = scr.Cols, scr.Rows
		s.mu.Unlock()
	}
}

// PaneOutput is told of a pane's new screen (the client's handler): drawn
// and sent to the pages showing that pane, at most every screenEvery.
func (s *Server) PaneOutput(pane uint64, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.screens[pane]
	if !ok {
		return
	}
	ps.ansi = append(ps.ansi[:0], data...)
	if ps.pending {
		return
	}
	wait := screenEvery - s.now().Sub(ps.sentAt)
	if wait <= 0 {
		s.sendScreenLocked(pane, ps)
		return
	}
	ps.pending = true
	time.AfterFunc(wait, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		ps.pending = false
		s.sendScreenLocked(pane, ps)
	})
}

func (s *Server) sendScreenLocked(pane uint64, ps *paneScreen) {
	ps.sentAt = s.now()
	s.broadcastLocked(screenMessage(pane, ScreenHTML(ps.ansi, ps.cols, ps.rows)), pane)
}
