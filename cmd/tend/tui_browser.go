package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/browserext"
	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
)

// The browser, tend's own (server/browser.go, internal/browserext). A page
// asked for here — the toolbar's Browser, prefix+B — goes to the browser
// attached to the session when there is one. With none, tend's browser is
// opened: a Chromium-family browser in a profile of the session's own with
// tend's extension in it, which attaches as it starts, so picking elements
// for the context works from the first page. [browser] command instead
// opens a browser of the user's own, without the extension; and a machine
// with no browser that can load the extension opens the desktop's own, and
// says so. All on this machine, not the server's: over --remote the page is
// wanted where the user is.

// openInBrowser opens a page.
func (t *tui) openInBrowser(url string) {
	url = strings.TrimSpace(url)
	// The prompt starts at "https://" with the cursor after it, so an
	// address pasted there whole came out https://https://…
	for _, seed := range []string{"https://", "http://"} {
		if rest, ok := strings.CutPrefix(url, seed); ok && strings.Contains(rest, "://") {
			url = rest
		}
	}
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	t.mu.Lock()
	t.lastURL = url
	t.urlHistory = rememberURL(t.urlHistory, url)
	history := append([]string(nil), t.urlHistory...)
	command, program := t.config.Browser.Command, t.config.Browser.Program
	session, host := t.session, t.host
	t.mu.Unlock()
	saveURLHistory(history)

	if t.serverKnows(proto.MethodBrowserOpen) {
		if n, err := t.client.BrowserOpen(url); err == nil && n > 0 {
			t.setMessage("opened in the attached browser: "+url, false)
			return
		}
	}
	if command == "" {
		used, err := launchTendBrowser(session, host, program, url)
		if err == nil {
			t.setMessage("opening "+url+" in tend's browser ("+used+", with the extension)", false)
			return
		}
		snap := errors.Is(err, browserext.ErrSnapBrowser)
		if !errors.Is(err, browserext.ErrNoBrowser) && !snap {
			t.setMessage("tend's browser: "+err.Error(), true)
			return
		}
		// No browser here can load the extension and reach tend: the
		// desktop's own, then, which opens the page and picks nothing. A
		// snap Chromium is said by name, since "none here" would be false
		// on a machine that has one.
		if err := openURL(url); err == nil {
			why := "no Chromium or Edge here"
			if snap {
				why = "the Chromium here is a snap, which cannot reach tend"
			}
			t.setMessage("opening "+url+" ("+why+", so without tend's extension)", false)
			return
		}
	}
	if err := openURLWith(command, url); err != nil {
		t.copyToClipboard(url, "no browser here ("+err.Error()+"); link copied")
		return
	}
	t.setMessage("opening "+url, false)
}

// openURLWith opens a page with the configured browser command, the URL
// last, or the desktop's own when none is set.
func openURLWith(command, url string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return openURL(url)
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return errors.New("not a web link")
	}
	if strings.HasPrefix(fields[0], "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			fields[0] = home + fields[0][1:]
		}
	}
	cmd := exec.Command(fields[0], append(fields[1:], url)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// urlHistoryMax is how many pages the prompt offers again: the owner's five.
const urlHistoryMax = 5

// urlHistoryFile is where the pages last opened are kept, so they are
// offered after a restart too. This client's, on this machine — what one
// person opened is nobody else's — beside tend's other state.
func urlHistoryFile() string {
	dir, err := transport.StateDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "browser-history")
}

// loadURLHistory reads the pages last opened, the last first.
func loadURLHistory() []string {
	path := urlHistoryFile()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !seen[line] && len(out) < urlHistoryMax {
			seen[line] = true
			out = append(out, line)
		}
	}
	return out
}

// rememberURL puts a page first among those last opened, once, and keeps
// the five most recent.
func rememberURL(history []string, url string) []string {
	out := []string{url}
	for _, u := range history {
		if u != url && len(out) < urlHistoryMax {
			out = append(out, u)
		}
	}
	return out
}

// saveURLHistory writes the pages last opened. Not being able to is not
// worth a word: the page opens all the same.
func saveURLHistory(history []string) {
	path := urlHistoryFile()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(history, "\n")+"\n"), 0o600)
}
