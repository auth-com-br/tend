package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStatsTellInstallsFromUpdateChecks: against a stand-in GitHub API,
// binaries and the update manifest are counted apart, per release and in
// all; traffic the token may not read is said to be unreadable; and with a
// history a week old, the change since is shown with how many servers the
// checks amount to. If it regresses, update checks read as installs, and
// the project looks used by far more people than it is.
func TestStatsTellInstallsFromUpdateChecks(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r":
			_, _ = w.Write([]byte(`{"stargazers_count":12,"forks_count":2,"subscribers_count":3}`))
		case "/repos/o/r/releases":
			_, _ = w.Write([]byte(`[{"tag_name":"v2","assets":[{"name":"tend-linux-amd64","download_count":5},{"name":"latest.json","download_count":700},{"name":"SHA256SUMS","download_count":4}]},
				{"tag_name":"v1","assets":[{"name":"tend-darwin-arm64","download_count":1}]}]`))
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer api.Close()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	s, err := take(github{base: api.URL}, "o/r", now)
	if err != nil {
		t.Fatal(err)
	}
	if s.Stars != 12 || s.Binaries != 6 || s.Checks != 700 || s.Views != -1 || len(s.Releases) != 2 || s.Releases[0].Binaries["linux-amd64"] != 5 {
		t.Fatalf("snapshot %+v", s)
	}
	history := filepath.Join(t.TempDir(), "h.jsonl")
	old := s
	old.Time, old.Stars, old.Checks = now.Add(-7*24*time.Hour), 10, 364
	if err := keep(history, old); err != nil {
		t.Fatal(err)
	}
	before := weekBefore(history, now)
	out := report("o/r", s, before)
	for _, want := range []string{"stars 12 (+2)", "binaries downloaded 6 (+0)", "update checks 700 (+336)", "as many as 1.0 servers running all the time", "traffic not readable", "v2                5            700"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(history); err != nil {
		t.Fatal(err)
	}
}
