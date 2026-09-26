// Command stats says how tend is being taken up, from what is public and
// nothing else: the repository's stars and forks, each release's downloads,
// and the repository's traffic when the token may read it. tend itself
// sends nothing anywhere, and this does not change that (#6).
//
//	go run ./tools/stats                       # now
//	go run ./tools/stats -history stats.jsonl  # now, and the change in a week
//
// Two kinds of download are told apart. A binary downloaded is an install
// or an update. latest.json is the update manifest, which a running tend
// server of a release asks for when it starts and every 30 minutes after
// (internal/server/updatecheck.go) — until it finds a newer release, when
// it stops; development builds never ask. Its count measures time in use by
// servers on the newest release, not people: 336 a week is one such server
// running all week. Neither says who.
//
// A token makes the traffic readable and the limits higher: GITHUB_TOKEN,
// else the one `gh auth token` prints, else none.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Snapshot is what is counted at one moment, kept as a line of the history.
type Snapshot struct {
	Time     time.Time      `json:"time"`
	Stars    int            `json:"stars"`
	Forks    int            `json:"forks"`
	Watchers int            `json:"watchers"`
	Binaries int            `json:"binaries"`
	Checks   int            `json:"checks"`
	Releases []ReleaseCount `json:"releases"`
	// Views and Clones are the last fourteen days', as GitHub keeps them,
	// and -1 when the token may not read them.
	Views        int `json:"views"`
	UniqueViews  int `json:"unique_views"`
	Clones       int `json:"clones"`
	UniqueClones int `json:"unique_clones"`
}

// ReleaseCount is one release's downloads.
type ReleaseCount struct {
	Tag      string         `json:"tag"`
	Binaries map[string]int `json:"binaries"`
	Checks   int            `json:"checks"`
}

// checksPerServerWeek is how many times a server running all week asks
// for the manifest.
const checksPerServerWeek = 7 * 24 * 2

func main() {
	repo := flag.String("repo", "auth-com-br/tend", "the GitHub repository")
	history := flag.String("history", "", "a file to keep each snapshot in, one JSON line each, and to compare with the one a week before")
	asJSON := flag.Bool("json", false, "print the snapshot as JSON")
	flag.Parse()
	g := github{token: token(), base: "https://api.github.com"}
	now, err := take(g, *repo, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "stats:", err)
		os.Exit(1)
	}
	var before *Snapshot
	if *history != "" {
		before = weekBefore(*history, now.Time)
		if err := keep(*history, now); err != nil {
			fmt.Fprintln(os.Stderr, "stats: keeping the history:", err)
		}
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(now)
		return
	}
	fmt.Print(report(*repo, now, before))
}

// token is a GitHub token for the API, or "" to ask without one.
func token() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type github struct {
	token, base string
}

var errForbidden = errors.New("not allowed with this token")

func (g github) get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, g.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return errForbidden
	case resp.StatusCode >= 300:
		return fmt.Errorf("GitHub answered %s for %s", resp.Status, path)
	}
	return json.Unmarshal(body, out)
}

// take counts everything once.
func take(g github, repo string, at time.Time) (Snapshot, error) {
	s := Snapshot{Time: at.UTC().Truncate(time.Second)}
	var r struct {
		Stars    int `json:"stargazers_count"`
		Forks    int `json:"forks_count"`
		Watchers int `json:"subscribers_count"`
	}
	if err := g.get("/repos/"+repo, &r); err != nil {
		return s, err
	}
	s.Stars, s.Forks, s.Watchers = r.Stars, r.Forks, r.Watchers

	for page := 1; ; page++ {
		var releases []struct {
			Tag    string `json:"tag_name"`
			Assets []struct {
				Name      string `json:"name"`
				Downloads int    `json:"download_count"`
			} `json:"assets"`
		}
		if err := g.get(fmt.Sprintf("/repos/%s/releases?per_page=100&page=%d", repo, page), &releases); err != nil {
			return s, err
		}
		for _, rel := range releases {
			rc := ReleaseCount{Tag: rel.Tag, Binaries: map[string]int{}}
			for _, a := range rel.Assets {
				switch {
				case a.Name == "latest.json":
					rc.Checks += a.Downloads
				case strings.HasPrefix(a.Name, "tend-"):
					rc.Binaries[strings.TrimPrefix(a.Name, "tend-")] += a.Downloads
				}
			}
			s.Releases = append(s.Releases, rc)
			s.Checks += rc.Checks
			for _, n := range rc.Binaries {
				s.Binaries += n
			}
		}
		if len(releases) < 100 {
			break
		}
	}

	s.Views, s.UniqueViews, s.Clones, s.UniqueClones = -1, -1, -1, -1
	var views, clones struct {
		Count  int `json:"count"`
		Unique int `json:"uniques"`
	}
	if g.get("/repos/"+repo+"/traffic/views", &views) == nil {
		s.Views, s.UniqueViews = views.Count, views.Unique
	}
	if g.get("/repos/"+repo+"/traffic/clones", &clones) == nil {
		s.Clones, s.UniqueClones = clones.Count, clones.Unique
	}
	return s, nil
}

// report is the snapshot in words, with the change since before when
// there is one.
func report(repo string, s Snapshot, before *Snapshot) string {
	var b strings.Builder
	delta := func(now, then int) string {
		if before == nil {
			return ""
		}
		return fmt.Sprintf(" (%+d)", now-then)
	}
	var bs, bf, bw, bb, bc int
	if before != nil {
		bs, bf, bw, bb, bc = before.Stars, before.Forks, before.Watchers, before.Binaries, before.Checks
	}
	fmt.Fprintf(&b, "%s — %s\n\n", repo, s.Time.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "stars %d%s · forks %d%s · watching %d%s\n", s.Stars, delta(s.Stars, bs), s.Forks, delta(s.Forks, bf), s.Watchers, delta(s.Watchers, bw))
	fmt.Fprintf(&b, "binaries downloaded %d%s — installs and updates\n", s.Binaries, delta(s.Binaries, bb))
	fmt.Fprintf(&b, "update checks %d%s — a server on the newest release asks every 30 minutes", s.Checks, delta(s.Checks, bc))
	if before != nil {
		days := s.Time.Sub(before.Time).Hours() / 24
		if days > 0 {
			servers := float64(s.Checks-bc) / (float64(checksPerServerWeek) * days / 7)
			fmt.Fprintf(&b, "; over the last %.0f days, as many as %.1f servers running all the time", days, servers)
		}
	}
	b.WriteString("\n")
	if s.Views >= 0 {
		fmt.Fprintf(&b, "last 14 days: %d views by %d visitors · %d clones by %d (bots and mirrors clone too)\n", s.Views, s.UniqueViews, s.Clones, s.UniqueClones)
	} else {
		b.WriteString("last 14 days: traffic not readable with this token (it needs push access)\n")
	}
	b.WriteString("\nrelease    binaries  update checks\n")
	for _, r := range s.Releases {
		names := make([]string, 0, len(r.Binaries))
		total := 0
		for n, c := range r.Binaries {
			names = append(names, fmt.Sprintf("%s %d", n, c))
			total += c
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "%-10s %8d  %13d   %s\n", r.Tag, total, r.Checks, strings.Join(names, ", "))
	}
	return b.String()
}

// weekBefore is the last snapshot taken at least a week before at, else
// the first one there is, else nil.
func weekBefore(path string, at time.Time) *Snapshot {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var found, first *Snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var s Snapshot
		if json.Unmarshal(sc.Bytes(), &s) != nil {
			continue
		}
		if first == nil {
			first = &s
		}
		if !s.Time.After(at.Add(-7 * 24 * time.Hour)) {
			found = &s
		}
	}
	if found != nil {
		return found
	}
	return first
}

// keep adds a snapshot to the history.
func keep(path string, s Snapshot) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(s)
}
