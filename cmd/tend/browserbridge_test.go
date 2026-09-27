//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/auth-com-br/tend/internal/browserext"
	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/vt"
)

// startRealSession is a session run as a user runs one, with its automation
// socket, for the tests that go through it from outside.
func startRealSession(t *testing.T, name string) (bin string, env []string) {
	t.Helper()
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env = append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	bin = buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", name}, pty.Options{Size: pty.Size{Cols: 100, Rows: 30}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(100, 30, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() {
		_ = p.Close()
		stopSession(t, name)
	})
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	return bin, env
}

func tendOutput(t *testing.T, bin string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func waitAttached(t *testing.T, bin string, env []string, session string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(tendOutput(t, bin, env, "browser", "status", "-s", session), "tend browser") {
		if time.Now().After(deadline) {
			t.Fatal("no browser attached")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func nativeFrame(v any) []byte {
	raw, _ := json.Marshal(v)
	var b bytes.Buffer
	_ = binary.Write(&b, binary.NativeEndian, uint32(len(raw)))
	b.Write(raw)
	return b.Bytes()
}

// TestTheBridgeCarriesTheExtensionsWay: the native messaging host, spoken
// to as a browser speaks to it, attaches to the session, passes on the
// commands the session gives, takes a capture to the context and answers
// it, and refuses what is not the browser's to ask. If it regresses, the
// extension loads and nothing it does reaches tend.
func TestTheBridgeCarriesTheExtensionsWay(t *testing.T) {
	bin, env := startRealSession(t, "bridge")
	host := exec.Command(bin, "browser", "bridge", "chrome-extension://"+"kafdikfjfbpngnlobakdlnepmciniffa/")
	host.Env = append(env, browserSessionEnv+"=bridge")
	in, _ := host.StdinPipe()
	out, _ := host.StdoutPipe()
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = host.Process.Kill(); _ = host.Wait() })
	messages := make(chan map[string]any, 8)
	go func() {
		for {
			raw, err := readNative(out)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			messages <- m
		}
	}()
	next := func(what string) map[string]any {
		t.Helper()
		select {
		case m := <-messages:
			return m
		case <-time.After(10 * time.Second):
			t.Fatalf("no %s", what)
			return nil
		}
	}
	waitAttached(t, bin, env, "bridge")

	tendOutput(t, bin, env, "browser", "open", "-s", "bridge", "https://example.com/login")
	if m := next("command"); m["type"] != "browser_command" || m["action"] != "open" || m["url"] != "https://example.com/login" {
		t.Errorf("command: %v", m)
	}

	_, _ = in.Write(nativeFrame(map[string]any{"id": "7", "method": "browser.context",
		"params": map[string]any{"url": "https://example.com/login", "selector": "#email", "tag": "input"}}))
	if m := next("reply"); m["type"] != "reply" || m["id"] != "7" || m["error"] != nil {
		t.Errorf("reply: %v", m)
	}
	if got := tendOutput(t, bin, env, "context", "list", "-s", "bridge"); !strings.Contains(got, "#email") {
		t.Errorf("the capture in the context: %q", got)
	}

	_, _ = in.Write(nativeFrame(map[string]any{"id": "8", "method": "server.stop"}))
	if m := next("refusal"); m["id"] != "8" || m["error"] == nil {
		t.Errorf("server.stop is not the browser's: %v", m)
	}
}

// TestTheBridgeSaysWhenTheServerIsOlderThanTheExtension: a server from
// before browser.context took items and a message — here a stand-in that
// attaches the browser as one did, with no features — gets none of the
// extension's sends; the extension is told to hand the server over instead.
// If it regresses, pressing Send in the browser after an update and before a
// handoff does nothing, and says nothing, as it did on the owner's machine.
func TestTheBridgeSaysWhenTheServerIsOlderThanTheExtension(t *testing.T) {
	t.Setenv("TEND_RUNTIME_DIR", t.TempDir())
	path, err := transport.APISocketPath("older")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	reached := make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadString('\n')
				reached <- line
				if strings.Contains(line, "browser.attach") {
					_, _ = conn.Write([]byte(`{"id":"bridge","result":{"type":"browser_attached"}}` + "\n"))
					_, _ = io.Copy(io.Discard, conn)
				}
			}()
		}
	}()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() { _ = bridge(inR, outW, "older", "") }()
	t.Cleanup(func() { _ = inW.Close() })
	if got := <-reached; !strings.Contains(got, "browser.attach") {
		t.Fatalf("first request: %q", got)
	}
	_, _ = inW.Write(nativeFrame(map[string]any{"id": "1", "method": "browser.context",
		"params": map[string]any{"message": "fix these", "items": []any{map[string]any{"selector": "h1"}}}}))
	raw, err := readNative(outR)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		ID    string `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &reply)
	if reply.ID != "1" || !strings.Contains(reply.Error.Message, "tend handoff -s older") {
		t.Errorf("reply: %s", raw)
	}
	select {
	case got := <-reached:
		t.Errorf("the send reached the older server: %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestEachBrowserComesUpWithTheExtensionWorking: the browser tend opens —
// Chromium, Google Chrome or Edge, each that this machine has, run headless
// through the keeper as tend runs it — has the extension loaded, which
// starts the bridge and attaches, and follows the session's open to a page.
// The extension is loaded through the keeper's DevTools pipe, the way
// Chrome takes it (it no longer reads --load-extension). Chromium and
// Edge still read that switch, but giving both loaded the extension twice
// and dropped the first bridge — status had "tend browser", then open
// returned no_browser. (Brave loads it the same way but never starts the
// bridge, so tend does not choose it.) If it regresses, the browser opens
// without the extension, as it did on a machine with only Chrome.
//
// The page having been opened is this server receiving /it, not Chromium's
// DevTools /json/list mentioning the URL. That list is served on
// --remote-debugging-port, which tend does not open. Under make check —
// the machine full of tests, then the same again with the race detector —
// Chromium sometimes had the extension attached and the command delivered
// while /json/list still had no page, or the port picked before start had
// been taken, and the test died at 15s saying the browser never opened it.
// Chrome and Edge on the same run did not. Open is sent until the page is
// fetched, so a command that met no browser yet is not the last word.
// The wait is longer than a quiet run needs (~2s) because the renderer
// shares the machine with that gate.
func TestEachBrowserComesUpWithTheExtensionWorking(t *testing.T) {
	for _, name := range []string{"chromium", "google-chrome", "microsoft-edge"} {
		t.Run(name, func(t *testing.T) {
			browser, err := exec.LookPath(name)
			if err != nil {
				t.Skip("needs " + name)
			}
			session := strings.ReplaceAll(name, "-", "")
			bin, env := startRealSession(t, session)
			hits := make(chan string, 32)
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits <- r.URL.Path
				_, _ = w.Write([]byte(`<title>tend test</title><h1 id="it">picked</h1>`))
			}))
			defer page.Close()

			data := t.TempDir()
			cmd := exec.Command(bin, "browser", "launch", "-s", session)
			cmd.Env = append(env, "XDG_DATA_HOME="+data, "PATH=/nonexistent") // prepare only: no browser to start
			_ = cmd.Run()
			profile := filepath.Join(data, "tend", "browser", session)
			if _, err := os.Stat(filepath.Join(profile, "extension", "manifest.json")); err != nil {
				t.Fatalf("launch prepared no profile: %v", err)
			}
			ext := filepath.Join(profile, "extension")
			prepared := browserext.Profile{Extension: ext, UserData: filepath.Join(profile, "profile")}
			keeper := exec.Command(bin, append([]string{"browser", "keep", "--", ext, browser, "--headless=new"},
				browserext.Args(prepared, "about:blank")...)...)
			keeper.Env = append(env, browserSessionEnv+"="+session)
			if err := keeper.Start(); err != nil {
				t.Fatal(err)
			}
			// Stopped as a keeper is: it takes the browser with it, and
			// waits, so the profile is not being written as it is removed.
			t.Cleanup(func() { _ = keeper.Process.Signal(syscall.SIGTERM); _ = keeper.Wait() })
			waitAttached(t, bin, env, session)

			open := func() {
				cmd := exec.Command(bin, "browser", "open", "-s", session, page.URL+"/it")
				cmd.Env = env
				_ = cmd.Run()
			}
			open()
			var seen []string
			tick := time.NewTicker(250 * time.Millisecond)
			defer tick.Stop()
			deadline := time.After(30 * time.Second)
			for {
				select {
				case path := <-hits:
					seen = append(seen, path)
					if path == "/it" {
						return
					}
				case <-tick.C:
					open()
				case <-deadline:
					t.Fatalf("the browser never opened the page it was sent; requests: %q; status:\n%s",
						seen, tendOutput(t, bin, env, "browser", "status", "-s", session))
				}
			}
		})
	}
}

// TestTheContextPanelOpensForWhatTheBrowserSends: what a browser sends to
// tend — here over the socket, as the extension's bridge does — opens the
// context panel in the client by itself, where S sends it all to the pane
// this tab works with. If it regresses, "Send all to tend" in the browser
// leaves nothing to see in tend.
func TestTheContextPanelOpensForWhatTheBrowserSends(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "SHELL=/bin/sh")
	bin := buildBinary(t)
	p, err := pty.Start(bin, []string{"attach", "-s", "arrive"}, pty.Options{Size: pty.Size{Cols: 110, Rows: 34}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(110, 34, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "arrive") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	out := tendOutput(t, bin, env, "api", "-s", "arrive", "browser.context",
		`{"message":"fix these on mobile","items":[{"url":"https://example.com/","selector":"#save","note":"should be green"},{"url":"https://example.com/","selector":"h1","note":"too big"}]}`)
	if !strings.Contains(out, "context_items") {
		t.Fatalf("browser.context: %s", out)
	}
	a.waitForScreen(t, "the context panel, opened by itself", func(s string) bool {
		return strings.Contains(s, "CONTEXT") && strings.Contains(s, "[text] fix these on mobile") && strings.Contains(s, "#save")
	})
	a.send(t, "S")
	a.waitForScreen(t, "all of it typed into the pane", func(s string) bool {
		return !strings.Contains(s, "CONTEXT") && strings.Contains(s, "note: should be green") && strings.Contains(s, "selector: h1")
	})
}
