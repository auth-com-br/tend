package web

import (
	"crypto/ecdh"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
)

// askingSession is fakeSession whose agent's screen is Claude Code's real
// prompt to run a command (internal/asks/testdata).
type askingSession struct{ fakeSession }

func (a *askingSession) PaneScreen(uint64) (proto.PaneScreenResult, error) {
	data, err := os.ReadFile(filepath.Join("..", "asks", "testdata", "claude-bash.txt"))
	return proto.PaneScreenResult{Text: string(data)}, err
}

// TestAPhoneIsToldWhenAnAgentStopsForIt: a paired page subscribes the phone;
// an agent that goes from working to blocked sends it, through its push
// service, the agent, where it is and what it asks, and a tap opens that
// pane; the state found at start is not news, and the same thing again
// within the cooldown is not sent twice. Push is refused without HTTPS, and
// the local authority's certificate is there for the phone to install. If
// it regresses, the phone is silent with the page closed — the point of
// turning notifications on.
func TestAPhoneIsToldWhenAnAgentStopsForIt(t *testing.T) {
	// Without push ([web] https off): subscribing is refused, and says why.
	plain := New(&fakeSession{}, testToken)
	if w := serve(plain, paired(post("/api/push/subscribe", `{"endpoint":"https://x/y"}`))); w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
		t.Errorf("subscribe without push: %d", w.Code)
	}
	if w := serve(plain, httptest.NewRequest("GET", "/ca.crt", nil)); w.Code != http.StatusNotFound {
		t.Errorf("no certificate to give without the local authority: %d", w.Code)
	}

	var mu sync.Mutex
	var bodies [][]byte
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer service.Close()

	dir := t.TempDir()
	key, err := LoadVAPID(filepath.Join(dir, "k.pem"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, caPEM, err := LoadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	pusher := NewPusher(key, filepath.Join(dir, "subs.json"), "mailto:t@example.com")
	pusher.client = service.Client()
	s := New(&askingSession{}, testToken).WithPush(pusher, caPEM)

	if w := serve(s, httptest.NewRequest("GET", "/ca.crt", nil)); w.Code != 200 || !strings.Contains(w.Body.String(), "BEGIN CERTIFICATE") {
		t.Errorf("the certificate for the phone: %d", w.Code)
	}
	if w := serve(s, httptest.NewRequest("GET", "/api/push", nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("push settings to an unpaired device: %d", w.Code)
	}
	w := serve(s, paired(httptest.NewRequest("GET", "/api/push", nil)))
	var info struct {
		Available bool   `json:"available"`
		Key       string `json:"key"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if !info.Available || info.Key != pusher.PublicKey() {
		t.Fatalf("push settings %s", w.Body.String())
	}

	phone, _ := ecdh.P256().GenerateKey(nil)
	auth := []byte("fedcba9876543210")
	sub, _ := json.Marshal(map[string]any{
		"endpoint": service.URL + "/send/1",
		"keys":     map[string]string{"p256dh": b64.EncodeToString(phone.PublicKey().Bytes()), "auth": b64.EncodeToString(auth)},
	})
	if w := serve(s, paired(post("/api/push/subscribe", string(sub)))); w.Code != 200 {
		t.Fatalf("subscribe: %d %s", w.Code, w.Body.String())
	}

	s.PrimePhones() // pane 3 is blocked at start: not news
	s.Event(proto.Event{Kind: "pane-state", Pane: 3, State: "working"})
	s.Event(proto.Event{Kind: "pane-state", Pane: 3, State: "blocked"})
	s.Event(proto.Event{Kind: "pane-state", Pane: 3, State: "working"})
	s.Event(proto.Event{Kind: "pane-state", Pane: 3, State: "blocked"}) // within the cooldown

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // a second one, if wrongly sent, would be in by now
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("%d notifications sent, want 1", len(bodies))
	}
	var n Notice
	if err := json.Unmarshal(decryptAsPhone(t, phone, auth, bodies[0]), &n); err != nil {
		t.Fatal(err)
	}
	if n.Pane != 3 || !strings.Contains(n.Title, "claude needs you") || !strings.Contains(n.Title, "acme") ||
		n.Body != "Bash command: touch outro-arquivo.txt — Do you want to proceed?" {
		t.Errorf("the phone shows %+v", n)
	}
}
