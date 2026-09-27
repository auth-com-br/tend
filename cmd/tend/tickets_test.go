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
