package browserext

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheProfileHasTheExtensionAndItsHost: preparing writes the extension,
// a start script for the bridge that runs the tend given, and the host's
// registration in the browser's data directory, naming the extension by the
// ID its manifest's key gives it. If it regresses, the browser starts with
// an extension that cannot reach tend — its host missing, or registered for
// another extension's ID.
func TestTheProfileHasTheExtensionAndItsHost(t *testing.T) {
	root := t.TempDir()
	p, err := Prepare(root, "/opt/it's/tend")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "background.js", "content.js"} {
		if _, err := os.Stat(filepath.Join(p.Extension, name)); err != nil {
			t.Errorf("extension/%s: %v", name, err)
		}
	}
	script, _ := os.ReadFile(p.Host)
	if info, _ := os.Stat(p.Host); info == nil || info.Mode()&0o111 == 0 || !strings.Contains(string(script), `'/opt/it'\''s/tend' browser bridge`) {
		t.Errorf("the host script runs the bridge: %q", script)
	}
	raw, err := os.ReadFile(filepath.Join(p.UserData, "NativeMessagingHosts", HostName+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var host struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Type    string   `json:"type"`
		Origins []string `json:"allowed_origins"`
	}
	if err := json.Unmarshal(raw, &host); err != nil {
		t.Fatal(err)
	}
	if host.Name != HostName || host.Path != p.Host || host.Type != "stdio" || len(host.Origins) != 1 || host.Origins[0] != "chrome-extension://"+ExtensionID+"/" {
		t.Errorf("registration: %+v", host)
	}

	// The ID Chromium gives an extension with a key: the first half of the
	// key's SHA-256, each hex digit as a letter from a.
	var manifest struct {
		Key string `json:"key"`
	}
	mraw, _ := os.ReadFile(filepath.Join(p.Extension, "manifest.json"))
	if err := json.Unmarshal(mraw, &manifest); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	id := []byte(hex.EncodeToString(sum[:])[:32])
	for i, c := range id {
		if c >= 'a' {
			id[i] = 'a' + 10 + c - 'a'
		} else {
			id[i] = 'a' + c - '0'
		}
	}
	if string(id) != ExtensionID {
		t.Errorf("the manifest's key makes the ID %s, not %s", id, ExtensionID)
	}

	if _, err := Prepare(root, "/usr/bin/tend"); err != nil {
		t.Errorf("preparing again: %v", err)
	}
}

// TestTheBrowserFoundIsOneThatLoadsTheExtension: the browser named is the
// one run; otherwise the first of those that load the extension through the
// DevTools pipe the arguments ask for, Google's Chrome among them; never
// Brave, whose bridge never starts.
// If it regresses, a machine with only Chrome opens the desktop's browser
// without the extension, as the owner's second machine did.
func TestTheBrowserFoundIsOneThatLoadsTheExtension(t *testing.T) {
	// Not this machine's packaging: the paths below are made up.
	defer func(f func(string) bool) { snapPackaged = f }(snapPackaged)
	snapPackaged = func(string) bool { return false }
	have := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range names {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	if got, _ := Find("", have("google-chrome", "microsoft-edge", "chromium")); got != "/usr/bin/chromium" {
		t.Errorf("chromium first: %s", got)
	}
	if got, _ := Find("", have("google-chrome", "microsoft-edge")); got != "/usr/bin/microsoft-edge" {
		t.Errorf("then edge: %s", got)
	}
	if got, _ := Find("", have("google-chrome", "brave-browser")); got != "/usr/bin/google-chrome" {
		t.Errorf("chrome, when there is only it and brave: %s", got)
	}
	if _, err := Find("", have("brave-browser")); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("brave alone: %v", err)
	}
	if _, err := Find("", have("firefox")); !errors.Is(err, ErrNoBrowser) {
		t.Errorf("none of them: %v", err)
	}
	if got, _ := Find("brave-browser", have("brave-browser")); got != "/usr/bin/brave-browser" {
		t.Errorf("the one named: %s", got)
	}
	args := strings.Join(Args(Profile{UserData: "/p", Extension: "/e"}, "https://x.test"), " ")
	if args != "--user-data-dir=/p --remote-debugging-pipe --enable-unsafe-extension-debugging --no-first-run --no-default-browser-check https://x.test" {
		t.Errorf("args: %s", args)
	}
}

// TestASnapBrowserIsPassedOver: a browser from a snap loads the extension
// but its confinement refuses the extension's host a connection to tend's
// socket, so it is skipped for one that can, and when a snap is all there
// is, the reason is given. If it regresses, tend opens Ubuntu's snap
// Chromium, status says no browser is attached, and nothing says why.
func TestASnapBrowserIsPassedOver(t *testing.T) {
	defer func(f func(string) bool) { snapPackaged = f }(snapPackaged)
	snapPackaged = func(p string) bool { return strings.HasPrefix(p, "/snap/") }
	look := func(found map[string]string) func(string) (string, error) {
		return func(n string) (string, error) {
			if p, ok := found[n]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		}
	}
	both := look(map[string]string{"chromium": "/snap/bin/chromium", "google-chrome": "/usr/bin/google-chrome"})
	if got, err := Find("", both); err != nil || got != "/usr/bin/google-chrome" {
		t.Errorf("a snap chromium and a .deb chrome: got %q, %v; want the chrome", got, err)
	}
	only := look(map[string]string{"chromium": "/snap/bin/chromium"})
	if _, err := Find("", only); !errors.Is(err, ErrSnapBrowser) {
		t.Errorf("only a snap: %v, want ErrSnapBrowser", err)
	}
	if _, err := Find("chromium", only); !errors.Is(err, ErrSnapBrowser) {
		t.Errorf("a snap named: %v, want ErrSnapBrowser", err)
	}
}

// TestUbuntusChromiumScriptIsKnownForASnap: /usr/bin/chromium-browser on
// Ubuntu is a shell script that runs the snap, so it has the snap's problem.
func TestUbuntusChromiumScriptIsKnownForASnap(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "chromium-browser")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec /snap/bin/chromium \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "chromium")
	if err := os.WriteFile(plain, []byte("\x7fELF not a script"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsSnapPackaged(script) {
		t.Error("the script that runs /snap/bin/chromium was not known for a snap")
	}
	if IsSnapPackaged(plain) {
		t.Error("a binary was taken for a snap")
	}
	if !IsSnapPackaged("/snap/bin/chromium") {
		t.Error("/snap/bin/chromium was not known for a snap")
	}
}
