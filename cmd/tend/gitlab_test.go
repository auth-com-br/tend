package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/auth-com-br/tend/internal/github"
)

// TestGitLabLoginKeepsATokenItHasChecked: `tend gitlab login` reads a
// token from a pipe, asks the GitLab whom it signs in as, and keeps it for
// that host only then — in the settings file when no keyring tool is here —
// where the issues panel's calls find it; a token GitLab refuses is not
// kept; logout forgets it. If it regresses, a mistyped token is saved and
// every call fails later, or a login is not used by the panel.
func TestGitLabLoginKeepsATokenItHasChecked(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "glpat-good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"username":"akira"}`)
	}))
	defer api.Close()
	old := github.GitLabBase
	defer func() { github.GitLabBase = old }()
	github.GitLabBase = func(string) string { return api.URL + "/api/v4" }
	cfg := filepath.Join(t.TempDir(), "tend.toml")
	t.Setenv("TEND_CONFIG", cfg)
	t.Setenv("PATH", t.TempDir()) // no keyring tool
	t.Setenv("GITLAB_TOKEN", "")

	withStdin := func(text string, run func() error) error {
		r, w, _ := os.Pipe()
		_, _ = w.WriteString(text)
		_ = w.Close()
		saved := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = saved }()
		return run()
	}
	if err := withStdin("glpat-bad\n", func() error { return gitlabLogin("gitlab.acme.com") }); err == nil {
		t.Fatal("a refused token was taken")
	}
	if b, _ := os.ReadFile(cfg); strings.Contains(string(b), "glpat-bad") {
		t.Fatalf("a refused token was kept:\n%s", b)
	}
	if err := withStdin("glpat-good\n", func() error { return gitlabLogin("gitlab.acme.com") }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	if !strings.Contains(string(b), `[gitlab.tokens]`) || !strings.Contains(string(b), `"gitlab.acme.com" = "glpat-good"`) {
		t.Fatalf("settings:\n%s", b)
	}
	if got := github.GitLabToken("gitlab.acme.com"); got != "glpat-good" {
		t.Errorf("the panel's calls find %q", got)
	}
	if got := github.GitLabToken("gitlab.com"); got != "" {
		t.Errorf("another host got %q", got)
	}
	if err := gitlabLogout("gitlab.acme.com"); err != nil {
		t.Fatal(err)
	}
	if got := github.GitLabToken("gitlab.acme.com"); got != "" {
		t.Errorf("after logout: %q", got)
	}
}
