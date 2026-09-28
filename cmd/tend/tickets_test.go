//go:build unix

package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheTicketsPanelConnectsListsAndWorksATicket: prefix+T with no
// tracker offers to connect one; the connect box takes Jira, its site, the
// email and a token, tests them and keeps the account; the list shows the
// open tickets; enter opens one with its text and comments; c comments; x
// and enter mark it done through the workflow's transition; and f types
// it, with its text, into the pane the panel was opened from. Jira is a
// stand-in answering as Jira Cloud's REST v2 does. If it regresses, a
// tracker cannot be connected, or a ticket never reaches the agent.
func TestTheTicketsPanelConnectsListsAndWorksATicket(t *testing.T) {
	var mu sync.Mutex
	var wrote []string
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("me@acme.com:jira-tok")) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = io.WriteString(w, `{"displayName":"Akira"}`)
		case r.URL.Path == "/rest/api/2/project":
			_, _ = io.WriteString(w, `[{"key":"APP","name":"App"}]`)
		case r.URL.Path == "/rest/api/2/search/jql":
			_, _ = io.WriteString(w, `{"issues":[{"key":"APP-42","fields":{"summary":"checkout button is grey","status":{"name":"To Do","statusCategory":{"key":"new"}},"assignee":{"displayName":"Ana"},"updated":"2026-09-20T12:00:00.000+0000","project":{"key":"APP"}}}]}`)
		case r.URL.Path == "/rest/api/2/issue/APP-42" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"key":"APP-42","fields":{"summary":"checkout button is grey","status":{"name":"To Do","statusCategory":{"key":"new"}},"description":"It should be blue, as in the design.","comment":{"comments":[{"author":{"displayName":"Bia"},"body":"the design is in Figma","created":"2026-09-21T10:00:00.000+0000"}]}}}`)
		case r.URL.Path == "/rest/api/2/issue/APP-42/transitions" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"transitions":[{"id":"21","to":{"statusCategory":{"key":"indeterminate"}}},{"id":"41","to":{"statusCategory":{"key":"done"}}}]}`)
		case r.Method == "POST":
			mu.Lock()
			wrote = append(wrote, r.URL.Path+" "+string(body))
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer jira.Close()

	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "PATH=/usr/bin:/bin")
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "tickets"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "tickets") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02T")
	a.waitForScreen(t, "the offer to connect", func(s string) bool { return strings.Contains(s, "No tracker is connected") })
	a.send(t, "\r")
	a.waitForScreen(t, "the connect box", func(s string) bool { return strings.Contains(s, "connect a tracker") && strings.Contains(s, "linear") })
	a.send(t, "\x1b[Z\x1b[C") // to the tracker, then Jira
	a.waitForScreen(t, "Jira's fields", func(s string) bool { return strings.Contains(s, "Jira Cloud") })
	a.send(t, "\t\x15"+jira.URL+"\tme@acme.com\tjira-tok\r")
	a.waitForScreen(t, "the tickets", func(s string) bool {
		return strings.Contains(s, "APP-42") && strings.Contains(s, "checkout button is grey") && strings.Contains(s, "connected to jira as Akira")
	})
	if b, _ := os.ReadFile(cfg); !strings.Contains(string(b), `kind = "jira"`) || !strings.Contains(string(b), `token = "jira-tok"`) {
		t.Fatalf("the account kept:\n%s", b)
	}

	a.send(t, "\r")
	a.waitForScreen(t, "the ticket whole", func(s string) bool {
		return strings.Contains(s, "It should be blue, as in the design.") && strings.Contains(s, "the design is in Figma")
	})
	a.send(t, "c")
	a.waitForScreen(t, "the comment line", func(s string) bool { return strings.Contains(s, "comment:") })
	a.send(t, "on it\r")
	a.waitForScreen(t, "commented", func(s string) bool { return strings.Contains(s, "commented on APP-42") })
	a.send(t, "x")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "mark APP-42 done?") })
	a.send(t, "\r")
	a.waitForScreen(t, "done", func(s string) bool { return strings.Contains(s, "APP-42 is done") })
	mu.Lock()
	got := strings.Join(wrote, "\n")
	mu.Unlock()
	if !strings.Contains(got, `/rest/api/2/issue/APP-42/comment {"body":"on it"}`) || !strings.Contains(got, `/rest/api/2/issue/APP-42/transitions {"transition":{"id":"41"}}`) {
		t.Errorf("wrote:\n%s", got)
	}

	a.send(t, "f")
	a.waitForScreen(t, "the ticket typed into the pane", func(s string) bool {
		return !strings.Contains(s, "TICKETS ·") && strings.Contains(s, "Work on this ticket (APP-42)") && strings.Contains(s, "typed into the pane")
	})
}

// TestTheIssueGatewayIsConnectedByItsAddress: the connect box offers the
// Issue Gateway, asks for its address and a token but no email, and keeps
// the address with the account; the list is the gateway's. The gateway is
// a stand-in answering as its contract says (issue-gateway#27). If it
// regresses, a gateway cannot be connected, or is kept with no address to
// find it at again.
func TestTheIssueGatewayIsConnectedByItsAddress(t *testing.T) {
	var closed atomic.Int32
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ig-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/me":
			_, _ = io.WriteString(w, `{"name":"akira"}`)
		case "/v1/scopes":
			_, _ = io.WriteString(w, `{"items":[{"id":"s-1","name":"Sistema de Recarga"}]}`)
		case "/v1/issues":
			_, _ = io.WriteString(w, `{"items":[{"key":"IG-12","ref":"u-12","title":"recarga falha no pix","state":"published","done":false,"assignee":null,"scope":"Sistema de Recarga","updated":"2026-09-27T12:00:00Z","url":null}]}`)
		case "/v1/issues/u-12/close":
			if r.Method == http.MethodPost {
				closed.Add(1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gw.Close()

	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	t.Setenv("TEND_ISSUE_GATEWAY_URL", gw.URL)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "TEND_ISSUE_GATEWAY_URL="+gw.URL, "SHELL=/bin/sh", "PATH=/usr/bin:/bin")
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "tickets-gateway"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "tickets-gateway") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02T")
	a.waitForScreen(t, "the offer to connect", func(s string) bool { return strings.Contains(s, "No tracker is connected") })
	a.send(t, "\r")
	a.waitForScreen(t, "the connect box", func(s string) bool {
		return strings.Contains(s, "connect a tracker") && strings.Contains(s, "issuegateway")
	})
	a.send(t, "\x1b[Z\x1b[D") // to the tracker, then back round to the last, the gateway
	a.waitForScreen(t, "the gateway's fields", func(s string) bool { return strings.Contains(s, "a token from the gateway") })
	// The gateway's address is fixed: from the tracker, tab is the token.
	a.send(t, "\tig-tok\r")
	a.waitForScreen(t, "the tickets", func(s string) bool {
		return strings.Contains(s, "IG-12") && strings.Contains(s, "recarga falha no pix") && strings.Contains(s, "connected to issuegateway as akira")
	})
	b, _ := os.ReadFile(cfg)
	for _, want := range []string{`kind = "issuegateway"`, `token = "ig-tok"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in the account kept:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "email") || strings.Contains(string(b), "url") {
		t.Errorf("an email or an address was kept:\n%s", b)
	}

	// Mark done with the mouse: the first click asks, the second is the
	// answer. Before, only enter answered, and a click asked again.
	row, col := -1, -1
	for i, line := range a.lines() {
		if c := columnOfString(line, "[ Mark done ]"); c >= 0 {
			row, col = i, c
		}
	}
	if row < 0 {
		t.Fatalf("no Mark done button:\n%s", a.text())
	}
	a.clickAt(t, col+3, row+1)
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "mark IG-12 done?") })
	if closed.Load() != 0 {
		t.Fatal("the first click marked it done without asking")
	}
	a.clickAt(t, col+3, row+1)
	a.waitForScreen(t, "IG-12 done", func(s string) bool { return strings.Contains(s, "IG-12 is done") })
	if closed.Load() != 1 {
		t.Errorf("the gateway was asked to close it %d times", closed.Load())
	}
}
