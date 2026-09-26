//go:build unix

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/auth-com-br/tend/internal/pty"
	"github.com/auth-com-br/tend/internal/vt"
)

// TestTheErrorsPanelConnectsListsAndFixes: with no server connected the
// errors panel offers to connect one; a token the server refuses is said in
// the box and not kept; one it takes is written to the settings file,
// readable by its owner alone, and the errors are listed; enter opens one
// with its stack; r resolves it on the server; and f types it, with its
// stack and what to do, into the pane the panel was opened from, submitting
// nothing. GlitchTip is a stand-in answering as it does. If it regresses,
// the key cannot be given, a wrong one is kept, or an error never reaches
// the agent.
func TestTheErrorsPanelConnectsListsAndFixes(t *testing.T) {
	var mu sync.Mutex
	var changed []string
	gt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer right-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/0/organizations/":
			_, _ = io.WriteString(w, `[{"slug":"shop","name":"Shop"}]`)
		case p == "/api/0/organizations/shop/projects/":
			_, _ = io.WriteString(w, `[{"slug":"api","name":"api"}]`)
		case p == "/api/0/organizations/shop/issues/":
			_, _ = io.WriteString(w, `[{"id":"75","shortId":"API-26","title":"KnexTimeoutError: pool full","level":"error","status":"unresolved","count":"1543","userCount":2,"firstSeen":"2026-08-01T10:00:00Z","lastSeen":"2026-09-20T12:00:00Z","project":{"slug":"api"}}]`)
		case p == "/api/0/issues/75/events/latest/":
			_, _ = io.WriteString(w, `{"eventID":"e1","entries":[{"type":"exception","data":{"values":[{"type":"KnexTimeoutError","value":"pool full","stacktrace":{"frames":[
				{"absPath":"/app/node_modules/knex/client.js","lineNo":465,"function":"acquire","inApp":false},
				{"absPath":"/app/src/models/Score.js","lineNo":144,"function":"complement","inApp":true,"context_line":"await pg('t').first();"}]}}]}}]}`)
		case p == "/api/0/issues/75/" && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			changed = append(changed, body["status"])
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer gt.Close()

	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "TEND_GLITCHTIP_TOKEN=")
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "errors"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "errors") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })

	a.send(t, "\x02E")
	a.waitForScreen(t, "the offer to connect", func(s string) bool { return strings.Contains(s, "GlitchTip is not connected") })
	a.send(t, "\r")
	a.waitForScreen(t, "the connect box", func(s string) bool { return strings.Contains(s, "connect GlitchTip") })
	a.send(t, "\x15"+gt.URL+"\twrong-token\r")
	a.waitForScreen(t, "the token refused", func(s string) bool { return strings.Contains(s, "GlitchTip refused the token") })
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "wrong-token") {
		t.Fatal("a refused token was kept")
	}
	a.send(t, "\x15right-token\r")
	a.waitForScreen(t, "the errors", func(s string) bool {
		return strings.Contains(s, "API-26") && strings.Contains(s, "KnexTimeoutError: pool full") && strings.Contains(s, "1543")
	})
	b, _ := os.ReadFile(cfg)
	st, _ := os.Stat(cfg)
	if !strings.Contains(string(b), `token = "right-token"`) || !strings.Contains(string(b), `url = "`+gt.URL+`"`) || st.Mode().Perm() != 0o600 {
		t.Errorf("settings (%v):\n%s", st.Mode(), b)
	}

	// Resolved from the list, without opening it.
	a.send(t, "\x18")
	a.waitForScreen(t, "resolved from the list", func(s string) bool { return strings.Contains(s, "API-26 is resolved") })

	a.send(t, "\r")
	a.waitForScreen(t, "the error's stack", func(s string) bool {
		return strings.Contains(s, "▸ at complement") && strings.Contains(s, "await pg('t').first();")
	})
	a.send(t, "r")
	a.waitForScreen(t, "resolved", func(s string) bool { return strings.Contains(s, "API-26 is resolved") })
	mu.Lock()
	got := strings.Join(changed, ",")
	mu.Unlock()
	if got != "resolved,resolved" {
		t.Errorf("changed: %q", got)
	}

	a.send(t, "f")
	a.waitForScreen(t, "the error typed into the pane", func(s string) bool {
		return !strings.Contains(s, "ERRORS ·") && strings.Contains(s, "Fix this error reported by GlitchTip (API-26") &&
			strings.Contains(s, "typed into the pane")
	})
}

// glitchTipStandIn answers as GlitchTip does for one organization with one
// error, to a token.
func glitchTipStandIn(t *testing.T, token, shortID, title string) *httptest.Server {
	t.Helper()
	gt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/0/organizations/":
			_, _ = io.WriteString(w, `[{"slug":"org","name":"Org"}]`)
		case "/api/0/organizations/org/projects/":
			_, _ = io.WriteString(w, `[{"slug":"app","name":"app"}]`)
		case "/api/0/organizations/org/issues/":
			_, _ = io.WriteString(w, `[{"id":"1","shortId":"`+shortID+`","title":"`+title+`","level":"error","status":"unresolved","count":3,"lastSeen":"2026-09-20T12:00:00Z","project":{"slug":"app"}}]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(gt.Close)
	return gt
}

// TestTheErrorsPanelKeepsSeveralServers: a second GlitchTip is added from
// the gear's servers box; the panel shows a chip for each, and ctrl+g goes
// to the other's errors; the one shown is remembered across a restart of
// the client; and removing a server from the box takes its own table out
// of the settings file — its token with it — and shows the one left. If it
// regresses, connecting a second server replaces the first, or a server
// once added can never be taken away.
func TestTheErrorsPanelKeepsSeveralServers(t *testing.T) {
	shop := glitchTipStandIn(t, "shop-token", "SHOP-1", "shop is down")
	blog := glitchTipStandIn(t, "blog-token", "BLOG-7", "blog is slow")

	cfg := filepath.Join(t.TempDir(), "tend.toml")
	settings := quietSettings + "\n[errors]\n# my comment\nurl = \"" + shop.URL + "\"\ntoken = \"shop-token\"\n"
	if err := os.WriteFile(cfg, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "TEND_GLITCHTIP_TOKEN=")
	tend := buildBinary(t)
	attach := func() *attached {
		t.Helper()
		p, err := pty.Start(tend, []string{"attach", "-s", "servers"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
		if err != nil {
			t.Fatal(err)
		}
		a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
		go func() { _, _ = io.Copy(a, p) }()
		t.Cleanup(func() { _ = p.Close() })
		a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
		return a
	}
	t.Cleanup(func() { stopSession(t, "servers") })
	host := func(u string) string { return strings.TrimPrefix(u, "http://") }

	a := attach()
	a.send(t, "\x02E")
	a.waitForScreen(t, "the first server's errors", func(s string) bool { return strings.Contains(s, "SHOP-1") })
	a.send(t, "\x0b")
	a.waitForScreen(t, "the servers box", func(s string) bool {
		return strings.Contains(s, "GlitchTip servers") && strings.Contains(s, "● "+host(shop.URL))
	})
	a.send(t, "a")
	a.waitForScreen(t, "the connect box", func(s string) bool { return strings.Contains(s, "connect GlitchTip") })
	a.send(t, "\x15"+blog.URL+"\tblog-token\r")
	a.waitForScreen(t, "the second server's errors, both chips", func(s string) bool {
		return strings.Contains(s, "BLOG-7") && !strings.Contains(s, "SHOP-1") &&
			strings.Contains(s, "server") && strings.Contains(s, host(shop.URL)) && strings.Contains(s, host(blog.URL))
	})
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), `token = "shop-token"`) || !strings.Contains(string(b), `token = "blog-token"`) || !strings.Contains(string(b), "# my comment") {
		t.Fatalf("both servers kept, the file as it was:\n%s", b)
	}

	a.send(t, "\x07")
	a.waitForScreen(t, "ctrl+g to the other", func(s string) bool { return strings.Contains(s, "SHOP-1") && !strings.Contains(s, "BLOG-7") })
	a.send(t, "\x07")
	a.waitForScreen(t, "and back", func(s string) bool { return strings.Contains(s, "BLOG-7") })

	// The client goes and comes back: the blog is still the one shown.
	a.send(t, "\x1b\x1b")
	a.waitForScreen(t, "the panel gone", func(s string) bool { return !strings.Contains(s, "ERRORS ·") })
	a.send(t, "\x02d")
	_ = a.pty.Wait()
	a = attach()
	a.send(t, "\x02E")
	a.waitForScreen(t, "the blog remembered", func(s string) bool { return strings.Contains(s, "BLOG-7") })

	// Remove the blog: its table goes, and the shop is shown.
	a.send(t, "\x0b")
	a.waitForScreen(t, "the box on the blog", func(s string) bool { return strings.Contains(s, "● "+host(blog.URL)) })
	a.send(t, "d")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "remove "+host(blog.URL)+"?") })
	a.send(t, "\r")
	a.waitForScreen(t, "the shop left", func(s string) bool {
		return strings.Contains(s, "SHOP-1") && strings.Contains(s, "removed "+host(blog.URL)) &&
			!strings.Contains(s, "● "+host(blog.URL)) && !strings.Contains(s, "│ server ")
	})
	b, _ = os.ReadFile(cfg)
	if strings.Contains(string(b), "blog-token") || strings.Contains(string(b), "errors.sources") || !strings.Contains(string(b), `token = "shop-token"`) {
		t.Errorf("after removing the blog:\n%s", b)
	}

	// And the first, kept in [errors] itself: its keys are emptied, and
	// the panel offers to connect one again.
	a.send(t, "d")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "remove "+host(shop.URL)+"?") })
	a.send(t, "\r")
	a.waitForScreen(t, "nothing kept", func(s string) bool { return strings.Contains(s, "GlitchTip is not connected") })
	b, _ = os.ReadFile(cfg)
	if strings.Contains(string(b), "shop-token") || !strings.Contains(string(b), "# my comment") {
		t.Errorf("after removing the shop:\n%s", b)
	}
}

// TestAProjectFolderOpensOnItsServerAndProject: in a pane working in a
// repository, ctrl+l ties the repository's folder to the server and the
// project shown, written to that server's table; opened from elsewhere the
// panel shows the server remembered, and opened in the repository again —
// a folder under it — it shows the tied server with the tied project's
// errors only, whatever was remembered; ctrl+l there unties it. GlitchTip
// is two stand-ins; the repository and the pane's shell are real. If it
// regresses, the panel opens on whichever server was used last, not the
// one the project reports to.
func TestAProjectFolderOpensOnItsServerAndProject(t *testing.T) {
	two := func(token string) *httptest.Server {
		gt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch r.URL.Path {
			case "/api/0/organizations/":
				_, _ = io.WriteString(w, `[{"slug":"org","name":"Org"}]`)
			case "/api/0/organizations/org/projects/":
				_, _ = io.WriteString(w, `[{"slug":"api","name":"api"},{"slug":"web","name":"web"}]`)
			case "/api/0/organizations/org/issues/":
				_, _ = io.WriteString(w, `[{"id":"1","shortId":"`+strings.ToUpper(token)+`-API","title":"api down","level":"error","status":"unresolved","count":1,"lastSeen":"2026-09-20T12:00:00Z","project":{"slug":"api"}},
					{"id":"2","shortId":"`+strings.ToUpper(token)+`-WEB","title":"web down","level":"error","status":"unresolved","count":1,"lastSeen":"2026-09-20T12:00:00Z","project":{"slug":"web"}}]`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		t.Cleanup(gt.Close)
		return gt
	}
	shop, blog := two("shop"), two("blog")

	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("needs git: %v %s", err, out)
	}
	deep := filepath.Join(repo, "src", "app")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	settings := quietSettings + "\n[errors]\nurl = \"" + shop.URL + "\"\ntoken = \"shop\"\nsource = \"" + "127-0-0-1-" + strings.Split(shop.URL, ":")[2] + "\"\n\n[errors.sources.blog]\nurl = \"" + blog.URL + "\"\ntoken = \"blog\"\n"
	if err := os.WriteFile(cfg, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh", "TEND_GLITCHTIP_TOKEN=")
	tend := buildBinary(t)
	p, err := pty.Start(tend, []string{"attach", "-s", "linked"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
	if err != nil {
		t.Fatal(err)
	}
	a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
	go func() { _, _ = io.Copy(a, p) }()
	t.Cleanup(func() { _ = p.Close(); stopSession(t, "linked") })
	a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
	cd := func(dir string) {
		t.Helper()
		a.send(t, "cd "+dir+" && echo IN-$(basename $PWD)\r")
		a.waitForScreen(t, "the shell there", func(s string) bool { return strings.Contains(s, "IN-"+filepath.Base(dir)) })
	}
	folder := filepath.Base(repo)

	cd(repo)
	a.send(t, "\x02E")
	a.waitForScreen(t, "the remembered server, and the offer", func(s string) bool {
		return strings.Contains(s, "SHOP-API") && strings.Contains(s, "[ link "+folder+" here ]")
	})
	a.send(t, "\x07") // the blog
	a.waitForScreen(t, "the blog", func(s string) bool { return strings.Contains(s, "BLOG-API") && strings.Contains(s, "BLOG-WEB") })
	a.send(t, "\x14\x14") // project: web
	a.waitForScreen(t, "the web project only", func(s string) bool { return strings.Contains(s, "BLOG-WEB") && !strings.Contains(s, "BLOG-API") })
	a.send(t, "\x0c")
	a.waitForScreen(t, "tied", func(s string) bool {
		return strings.Contains(s, folder+" now opens on") && strings.Contains(s, "⇄ "+folder+" opens here")
	})
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), `projects = { "`+repo+`" = "org/web" }`) {
		t.Fatalf("the tie in the settings:\n%s", b)
	}

	// Elsewhere, the shop is chosen, and remembered.
	a.send(t, "\x1b")
	a.waitForScreen(t, "closed", func(s string) bool { return !strings.Contains(s, "ERRORS ·") })
	cd("/")
	a.send(t, "\x02E")
	a.waitForScreen(t, "the blog remembered", func(s string) bool { return strings.Contains(s, "BLOG-API") })
	a.send(t, "\x07")
	a.waitForScreen(t, "the shop", func(s string) bool { return strings.Contains(s, "SHOP-API") })
	a.send(t, "\x1b")
	a.waitForScreen(t, "closed", func(s string) bool { return !strings.Contains(s, "ERRORS ·") })

	// Under the repository, the tie wins over what was remembered.
	cd(deep)
	a.send(t, "\x02E")
	a.waitForScreen(t, "the tied server and project", func(s string) bool {
		return strings.Contains(s, "BLOG-WEB") && !strings.Contains(s, "BLOG-API") && !strings.Contains(s, "SHOP-") &&
			strings.Contains(s, "⇄ "+folder+" opens here")
	})
	a.send(t, "\x0c")
	a.waitForScreen(t, "untied", func(s string) bool { return strings.Contains(s, folder+" no longer opens on") })
	b, _ = os.ReadFile(cfg)
	if strings.Contains(string(b), `"`+repo+`"`) {
		t.Errorf("the tie is still in the settings:\n%s", b)
	}
}

// TestAWorktreeIsItsRepositorysProject: a directory in a worktree belongs
// to the repository the worktree was made from, and one in no repository
// is its own project. If it regresses, an error fixed in its worktree
// opens on no server, or the wrong one.
func TestAWorktreeIsItsRepositorysProject(t *testing.T) {
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("needs git: %v %s", err, out)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "one")
	tree := filepath.Join(t.TempDir(), "fix-1")
	git("worktree", "add", "-q", "-b", "fix-1", tree)
	sub := filepath.Join(tree, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(repo)
	if got, _ := filepath.EvalSymlinks(projectRoot(sub)); got != real {
		t.Errorf("worktree dir's project is %q, want %q", got, real)
	}
	plain := t.TempDir()
	if got := projectRoot(plain); got != plain {
		t.Errorf("a folder in no repository is %q, want itself", got)
	}
}

// TestATokenGoesToTheKeyringWhenThereIsOne: with a keyring tool on the
// machine — here a stand-in secret-tool keeping secrets in a folder — the
// token given in the connect box goes to the keyring and the settings file
// names it there, never holding it; a client started later reads it back
// and lists the errors; and removing the server forgets the secret too. If
// it regresses, a token sits in a file when the system had somewhere safer
// for it, or a server kept in the keyring cannot be read back.
func TestATokenGoesToTheKeyringWhenThereIsOne(t *testing.T) {
	gt := glitchTipStandIn(t, "vault-token", "VAULT-1", "kept safe")
	bin, store := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
cmd=$1; shift
name=""
while [ $# -gt 0 ]; do [ "$1" = account ] && name=$2; shift; done
f="` + store + `/$(echo "$name" | tr ':/' '__')"
case $cmd in
store) cat > "$f" ;;
lookup) [ -f "$f" ] || exit 1; cat "$f" ;;
clear) rm -f "$f" ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "secret-tool"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	if err := os.WriteFile(cfg, []byte(quietSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()
	t.Setenv("TEND_RUNTIME_DIR", runtimeDir)
	t.Setenv("TEND_CONFIG", cfg)
	env := append(os.Environ(), "TEND_RUNTIME_DIR="+runtimeDir, "TEND_CONFIG="+cfg, "SHELL=/bin/sh",
		"TEND_GLITCHTIP_TOKEN=", "PATH="+bin+":"+os.Getenv("PATH"))
	tend := buildBinary(t)
	attach := func() *attached {
		t.Helper()
		p, err := pty.Start(tend, []string{"attach", "-s", "vault"}, pty.Options{Size: pty.Size{Cols: 130, Rows: 40}, Env: env})
		if err != nil {
			t.Fatal(err)
		}
		a := &attached{pty: p, screen: vt.NewScreen(130, 40, 100)}
		go func() { _, _ = io.Copy(a, p) }()
		t.Cleanup(func() { _ = p.Close() })
		a.waitForScreen(t, "a pane", func(s string) bool { return strings.Contains(s, "┌") })
		return a
	}
	t.Cleanup(func() { stopSession(t, "vault") })

	a := attach()
	a.send(t, "\x02E")
	a.waitForScreen(t, "the offer to connect", func(s string) bool { return strings.Contains(s, "GlitchTip is not connected") })
	a.send(t, "\r")
	a.waitForScreen(t, "the connect box", func(s string) bool { return strings.Contains(s, "connect GlitchTip") })
	a.send(t, "\x15"+gt.URL+"\tvault-token\r")
	a.waitForScreen(t, "the errors", func(s string) bool { return strings.Contains(s, "VAULT-1") })
	b, _ := os.ReadFile(cfg)
	if strings.Contains(string(b), "vault-token") || !strings.Contains(string(b), `token = "keyring:glitchtip:`) {
		t.Fatalf("the settings hold the token, or no reference to it:\n%s", b)
	}
	kept, _ := os.ReadDir(store)
	if len(kept) != 1 {
		t.Fatalf("the keyring holds %d secrets", len(kept))
	}

	// Another client reads it back from the keyring.
	a.send(t, "\x1b")
	a.waitForScreen(t, "closed", func(s string) bool { return !strings.Contains(s, "ERRORS ·") })
	a.send(t, "\x02d")
	_ = a.pty.Wait()
	a = attach()
	a.send(t, "\x02E")
	a.waitForScreen(t, "the errors, the token from the keyring", func(s string) bool { return strings.Contains(s, "VAULT-1") })

	a.send(t, "\x0bd")
	a.waitForScreen(t, "the question", func(s string) bool { return strings.Contains(s, "? its token is forgotten") })
	a.send(t, "\r")
	a.waitForScreen(t, "nothing kept", func(s string) bool { return strings.Contains(s, "GlitchTip is not connected") })
	if kept, _ := os.ReadDir(store); len(kept) != 0 {
		t.Errorf("the keyring still holds %d secrets", len(kept))
	}
}
