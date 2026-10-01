package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The test binary is also a fake MCP server: run again with
// TEND_FAKE_MCP set, it speaks stdio MCP the way the mode says.
func TestMain(m *testing.M) {
	if mode := os.Getenv("TEND_FAKE_MCP"); mode != "" {
		fakeStdioServer(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeStdioServer(mode string) {
	switch mode {
	case "crash":
		fmt.Fprintln(os.Stderr, "boot")
		fmt.Fprintln(os.Stderr, "fatal: API_KEY not accepted")
		os.Exit(1)
	case "silent":
		time.Sleep(time.Hour)
	}
	// Many servers log to stdout before speaking; the agents skip it.
	fmt.Println("Starting server v1…")
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var m message
		if json.Unmarshal(in.Bytes(), &m) != nil {
			continue
		}
		switch m.Method {
		case "initialize":
			// A ping first, as some servers send while they start: it
			// must be answered or the server waits.
			fmt.Println(`{"jsonrpc":"2.0","id":"p1","method":"ping"}`)
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"1.2"}}}`+"\n", m.ID)
		case "tools/list":
			fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"a"},{"name":"b"},{"name":"c"}]}}`+"\n", m.ID)
		}
	}
}

func fakeDef(mode string) Def {
	exe, _ := os.Executable()
	return Def{Transport: Stdio, Command: exe, Env: map[string]string{"TEND_FAKE_MCP": mode, "API_KEY": "sk-very-secret"}}
}

// TestAStdioServerIsSpokenToAsAnAgentWould: tend starts it, skips the lines
// that are not JSON-RPC, answers its ping, and reads its name and its
// tools. If it regresses, a working server that logs on stdout or pings
// while starting is shown as broken.
func TestAStdioServerIsSpokenToAsAnAgentWould(t *testing.T) {
	r := Probe(context.Background(), fakeDef("ok"), "test")
	if r.Status != "ok" || r.Tools != 3 || r.Server != "fake" || r.Version != "1.2" || r.Protocol != ProtocolVersion {
		t.Errorf("result = %+v", r)
	}
}

// TestAServerThatDiesSaysWhyWithoutItsSecrets: one that exits is an error
// with the last line of its stderr, and no env value appears in what is
// shown. If it regresses, the reason a server will not start is hidden, or
// its API key is printed in tend.
func TestAServerThatDiesSaysWhyWithoutItsSecrets(t *testing.T) {
	d := fakeDef("crash")
	r := Probe(context.Background(), d, "test")
	if r.Status != "error" || !strings.Contains(r.Stderr, "not accepted") {
		t.Errorf("result = %+v", r)
	}
	all, _ := json.Marshal(r)
	if strings.Contains(string(all), "sk-very-secret") {
		t.Errorf("a secret is in the result: %s", all)
	}
}

// TestAServerThatNeverAnswersIsATimeoutNotAHang: the test gives up when its
// context does, and the server is killed. If it regresses, one stuck server
// holds the MCP manager.
func TestAServerThatNeverAnswersIsATimeoutNotAHang(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := Probe(ctx, fakeDef("silent"), "test")
	if r.Status != "timeout" || time.Since(start) > 5*time.Second {
		t.Errorf("result = %+v after %s", r, time.Since(start))
	}
}

// fakeHTTP is a streamable HTTP server that answers in SSE and keeps a
// session, as most do; it refuses requests without the expected header.
func fakeHTTP(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-12345" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		var m message
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "s-1" {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		}
		if len(m.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Mcp-Session-Id", "s-1")
		w.Header().Set("Content-Type", "text/event-stream")
		result := `{"tools":[{"name":"x"}]}`
		if m.Method == "initialize" {
			result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"web","version":"9"}}`
		}
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", m.ID, result)
	}))
}

// TestAnHTTPServerIsSpokenToWithItsSessionAndHeaders: streamable HTTP with
// an SSE answer and a session id; a server that wants a login is "auth",
// not broken. If it regresses, the owner's HTTP servers all show red, or
// an OAuth server looks dead when it only needs the agent's login.
func TestAnHTTPServerIsSpokenToWithItsSessionAndHeaders(t *testing.T) {
	srv := fakeHTTP(t)
	defer srv.Close()
	d := Def{Transport: HTTP, URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer tok-12345"}}
	if r := Probe(context.Background(), d, "test"); r.Status != "ok" || r.Tools != 1 || r.Server != "web" {
		t.Errorf("result = %+v", r)
	}
	d.Headers = nil
	if r := Probe(context.Background(), d, "test"); r.Status != "auth" {
		t.Errorf("without its header the result = %+v", r)
	}
}

// TestAnSSEServerIsFoundBehindAPlainURL: a URL that is not streamable HTTP
// is tried as the older SSE transport, as Cursor and OpenCode do. If it
// regresses, an SSE server kept under a plain url is shown as broken.
func TestAnSSEServerIsFoundBehindAPlainURL(t *testing.T) {
	events := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: endpoint\ndata: /messages?sid=1\n\n")
			w.(http.Flusher).Flush()
			for {
				select {
				case e := <-events:
					fmt.Fprint(w, e)
					w.(http.Flusher).Flush()
				case <-r.Context().Done():
					return
				}
			}
		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			var m message
			_ = json.NewDecoder(r.Body).Decode(&m)
			w.WriteHeader(http.StatusAccepted)
			if len(m.ID) == 0 {
				return
			}
			result := `{"tools":[]}`
			if m.Method == "initialize" {
				result = `{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"old"}}`
			}
			events <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", m.ID, result)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()
	r := Probe(context.Background(), Def{Transport: HTTP, URL: srv.URL + "/sse"}, "test")
	if r.Status != "ok" || r.Server != "old" || r.Protocol != "2024-11-05" {
		t.Errorf("result = %+v", r)
	}
}

// TestAnUnsetVariableIsSaidNotTried: ${VAR} is filled from the server's
// environment, with a default where one is given, and one that is not set
// is reported without starting anything. If it regresses, a server is
// tested with a literal "${TOKEN}" and blamed for refusing it.
func TestAnUnsetVariableIsSaidNotTried(t *testing.T) {
	t.Setenv("TEND_TEST_SET", "v")
	d, err := expandDef(Def{Transport: HTTP, URL: "http://h/${TEND_TEST_SET}/{env:TEND_TEST_SET}/${env:TEND_TEST_SET}/${NOPE_X:-d}"})
	if err != nil || d.URL != "http://h/v/v/v/d" {
		t.Errorf("expanded %q, %v", d.URL, err)
	}
	r := Probe(context.Background(), Def{Transport: Stdio, Command: "x", Env: map[string]string{"K": "${TEND_SURELY_UNSET}"}}, "test")
	if r.Status != "error" || !strings.Contains(r.Error, "TEND_SURELY_UNSET") {
		t.Errorf("result = %+v", r)
	}
}

// TestArgumentsAndURLsAreShownWithoutSecrets: what the manager shows of a
// command line and a URL hides tokens, keys and query values. If it
// regresses, a key passed as an argument is printed on screen.
func TestArgumentsAndURLsAreShownWithoutSecrets(t *testing.T) {
	got := strings.Join(RedactArgs([]string{"-y", "pkg", "--api-key", "abc123", "--token=xyz", "DB_PASSWORD=pw", "https://u:p@h/x?key=k"}), " ")
	for _, secret := range []string{"abc123", "xyz", "pw", "u:p", "key=k"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q is shown: %s", secret, got)
		}
	}
	if !strings.Contains(got, "-y pkg --api-key") {
		t.Errorf("too much hidden: %s", got)
	}
}
