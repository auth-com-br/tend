package web

// The phone told with the page closed (push.go): the routes a page turns
// notifications on and off with, the service worker that shows them, the
// local authority's certificate for the phone to trust (tls.go), and when to
// tell — an agent that became blocked, or one that finished while nobody was
// looking (the session's "done"), saying what the blocked one asks.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auth-com-br/tend/internal/asks"
	"github.com/auth-com-br/tend/internal/proto"
)

// pushCooldown is how long one agent is not announced again for the same
// thing: an agent flickers between states while a tool runs, and a phone
// buzzing a dozen times a minute is turned off for good.
const pushCooldown = 30 * time.Second

// phone is the push side of a server; nil when push is off.
type phone struct {
	pusher *Pusher
	caPEM  []byte

	mu    sync.Mutex
	state map[uint64]string    // pane → last state heard
	told  map[string]time.Time // pane and kind → last announced
}

// WithPush lets phones turn notifications on: pusher sends them, and caPEM,
// when the page's certificate is the local authority's, is offered at
// /ca.crt for the phone to trust.
func (s *Server) WithPush(pusher *Pusher, caPEM []byte) *Server {
	s.phone = &phone{pusher: pusher, caPEM: caPEM, state: map[uint64]string{}, told: map[string]time.Time{}}
	return s
}

// phoneRoutes adds the routes push needs.
func (s *Server) phoneRoutes(mux *http.ServeMux) {
	static := http.FileServer(http.FS(assetsSub()))
	// The service worker and the certificate say nothing about the session;
	// the worker is fetched by the browser on its own, without the page.
	mux.Handle("GET /sw.js", static)
	mux.HandleFunc("GET /ca.crt", func(w http.ResponseWriter, r *http.Request) {
		if s.phone == nil || len(s.phone.caPEM) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="tend-local-ca.crt"`)
		_, _ = w.Write(s.phone.caPEM)
	})
	mux.Handle("GET /api/push", s.paired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := map[string]any{"available": s.phone != nil}
		if s.phone != nil {
			out["key"] = s.phone.pusher.PublicKey()
			out["phones"] = s.phone.pusher.Count()
		}
		writeJSON(w, out)
	})))
	mux.Handle("POST /api/push/subscribe", s.paired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sub Subscription
		if !s.readFromPage(w, r, &sub) {
			return
		}
		if s.phone == nil {
			http.Error(w, "notifications need `[web] https = true`", http.StatusConflict)
			return
		}
		if err := s.phone.pusher.Subscribe(sub); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})))
	mux.Handle("POST /api/push/unsubscribe", s.paired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Endpoint string `json:"endpoint"`
		}
		if !s.readFromPage(w, r, &req) {
			return
		}
		if s.phone != nil {
			_ = s.phone.pusher.Unsubscribe(req.Endpoint)
		}
		writeJSON(w, map[string]any{"ok": true})
	})))
}

// readFromPage reads a JSON request made by this page and no other, as
// action does.
func (s *Server) readFromPage(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		http.Error(w, "requests are JSON", http.StatusUnsupportedMediaType)
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || u.Host != r.Host {
			http.Error(w, "not from this page", http.StatusForbidden)
			return false
		}
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(v); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// tellPhones decides, from a pane's change of state, whether the phones hear
// of it, and tells them on a goroutine of its own: it reads the session.
func (s *Server) tellPhones(ev proto.Event) {
	ph := s.phone
	if ph == nil || ev.Kind != "pane-state" || ev.Pane == 0 || ph.pusher.Count() == 0 {
		return
	}
	ph.mu.Lock()
	was, known := ph.state[ev.Pane]
	ph.state[ev.Pane] = ev.State
	ph.mu.Unlock()
	if !known || was == ev.State {
		return
	}
	var kind string
	switch {
	case ev.State == "blocked":
		kind = "blocked"
	case ev.State == "idle" && (was == "working" || was == "blocked"):
		kind = "finished"
	default:
		return
	}
	go s.sendPhoneNotice(ev.Pane, kind)
}

func (s *Server) sendPhoneNotice(pane uint64, kind string) {
	ph := s.phone
	snap, err := s.sess.Snapshot()
	if err != nil {
		return
	}
	var who *Agent
	for _, a := range Agents(snap) {
		if a.Pane == pane {
			a := a
			who = &a
		}
	}
	if who == nil || who.Muted {
		// Muted in tend is muted on the phone too.
		return
	}
	// Finished is news only when nobody saw it (the session's "done"): an
	// agent finishing in the tab on someone's screen is not.
	if kind == "finished" && !who.Done {
		return
	}
	key := kind + ":" + strconv.FormatUint(pane, 10)
	ph.mu.Lock()
	if last, ok := ph.told[key]; ok && time.Since(last) < pushCooldown {
		ph.mu.Unlock()
		return
	}
	ph.told[key] = time.Now()
	ph.mu.Unlock()

	where := strings.Join(nonEmpty(who.Company, who.Space, who.Tab), " › ")
	n := Notice{Pane: pane, Tag: "pane-" + strconv.FormatUint(pane, 10)}
	switch kind {
	case "blocked":
		n.Title = who.Agent + " needs you · " + where
		n.Body = "waiting for your answer"
		if screen, err := s.sess.PaneScreen(pane); err == nil {
			if asked := asks.Read(screen.Text); asked != "" {
				n.Body = asked
			}
		}
	default:
		n.Title = who.Agent + " finished · " + where
		n.Body = who.Title
	}
	_ = ph.pusher.Send(n)
}

// PrimePhones learns every pane's state without announcing any, so the
// first change heard is told and the states found at start are not. Called
// at start and when the session comes back.
func (s *Server) PrimePhones() {
	if s.phone == nil {
		return
	}
	snap, err := s.sess.Snapshot()
	if err != nil {
		return
	}
	s.phone.mu.Lock()
	defer s.phone.mu.Unlock()
	for _, p := range snap.Panes {
		s.phone.state[p.ID] = p.State
	}
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
