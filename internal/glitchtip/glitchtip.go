// Package glitchtip reads the errors a GlitchTip server has collected from
// the systems that report to it, and marks them resolved or ignored. It is
// tend's own, for the owner's error monitor: GlitchTip speaks Sentry's web
// API, so this is that API's small part — organizations, their issues (an
// issue is one error, grouped), an issue's latest event with its stack, and
// its status — with a token the user gives tend in the errors panel.
package glitchtip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Errors a caller tells apart.
var (
	// ErrNotConnected is no server or token set.
	ErrNotConnected = errors.New("No GlitchTip or Sentry server is connected")
	// ErrBadToken is a token the server refused.
	ErrBadToken = errors.New("GlitchTip refused the token")
)

// callTimeout bounds one request.
const callTimeout = 20 * time.Second

// listLimit is how many issues a list reads from each organization.
const listLimit = 100

// Client talks to one GlitchTip server with one token.
type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

// New is a client for a server and a token; the URL may end in a slash.
func New(base, token string) *Client {
	return &Client{URL: strings.TrimRight(strings.TrimSpace(base), "/"), Token: strings.TrimSpace(token),
		HTTP: &http.Client{Timeout: callTimeout}}
}

func (c *Client) do(method, path string, body any, out any) error {
	if c.URL == "" || c.Token == "" {
		return ErrNotConnected
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("GlitchTip did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrBadToken
	case resp.StatusCode >= 300:
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return &statusError{code: resp.StatusCode, text: fmt.Sprintf("GlitchTip answered %s: %s", resp.Status, msg)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("reading GlitchTip's answer: %w", err)
	}
	return nil
}

// statusError is an answer the server gave with a failing status.
type statusError struct {
	code int
	text string
}

func (e *statusError) Error() string { return e.text }

// notHere reports whether a path is one the server does not have.
func notHere(err error) bool {
	var s *statusError
	return errors.As(err, &s) && (s.code == http.StatusNotFound || s.code == http.StatusMethodNotAllowed)
}

// issuePath asks for an issue's path the way GlitchTip has it
// (/api/0/issues/{id}/…), and, where the server has not that, the way
// Sentry documents it (/api/0/organizations/{org}/issues/{id}/…): the same
// client serves both.
func (c *Client) issuePath(method, org, issueID, rest string, body, out any) error {
	err := c.do(method, "/api/0/issues/"+url.PathEscape(issueID)+"/"+rest, body, out)
	if notHere(err) && org != "" {
		return c.do(method, "/api/0/organizations/"+url.PathEscape(org)+"/issues/"+url.PathEscape(issueID)+"/"+rest, body, out)
	}
	return err
}

// Org is an organization.
type Org struct {
	Slug, Name string
}

// Orgs is the organizations the token can see: what Connect checks.
func (c *Client) Orgs() ([]Org, error) {
	var wire []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := c.do("GET", "/api/0/organizations/", nil, &wire); err != nil {
		return nil, err
	}
	out := make([]Org, 0, len(wire))
	for _, o := range wire {
		out = append(out, Org{Slug: o.Slug, Name: o.Name})
	}
	return out, nil
}

// Project is a project, in its organization.
type Project struct {
	Org, Slug, Name, Platform string
}

// Projects is every project of every organization the token can see.
func (c *Client) Projects() ([]Project, error) {
	orgs, err := c.Orgs()
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var out []Project
	err = each(orgs, func(o Org) error {
		var wire []struct {
			Slug     string `json:"slug"`
			Name     string `json:"name"`
			Platform string `json:"platform"`
		}
		if err := c.do("GET", "/api/0/organizations/"+url.PathEscape(o.Slug)+"/projects/", nil, &wire); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		for _, p := range wire {
			out = append(out, Project{Org: o.Slug, Slug: p.Slug, Name: p.Name, Platform: p.Platform})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Org+"/"+out[i].Slug < out[j].Org+"/"+out[j].Slug })
	return out, err
}

// Status is an issue's: unresolved, resolved or ignored.
const (
	StatusUnresolved = "unresolved"
	StatusResolved   = "resolved"
	StatusIgnored    = "ignored"
)

// Statuses are the list's presets in the order it walks them.
var Statuses = []string{StatusUnresolved, StatusResolved, StatusIgnored}

// Issue is one error, grouped, as the list shows it.
type Issue struct {
	ID        string
	ShortID   string
	Org       string
	Project   string
	Title     string
	Culprit   string
	Level     string
	Status    string
	Count     int
	Users     int
	FirstSeen time.Time
	LastSeen  time.Time
	// URL is its page on the server.
	URL string
}

type wireIssue struct {
	ID      string          `json:"id"`
	ShortID string          `json:"shortId"`
	Title   string          `json:"title"`
	Culprit string          `json:"culprit"`
	Level   string          `json:"level"`
	Status  string          `json:"status"`
	Count   json.RawMessage `json:"count"`
	Users   int             `json:"userCount"`
	First   time.Time       `json:"firstSeen"`
	Last    time.Time       `json:"lastSeen"`
	Project struct {
		Slug string `json:"slug"`
	} `json:"project"`
	// Permalink is the issue's page, which Sentry gives and GlitchTip does
	// not; without it the page is made from the address.
	Permalink string `json:"permalink"`
}

// count reads a count GlitchTip gives as a number or as a string of one.
func count(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		fmt.Sscan(s, &n)
	}
	return n
}

func (c *Client) issue(org string, w wireIssue) Issue {
	return Issue{ID: w.ID, ShortID: w.ShortID, Org: org, Project: w.Project.Slug, Title: w.Title,
		Culprit: w.Culprit, Level: w.Level, Status: w.Status, Count: count(w.Count), Users: w.Users,
		FirstSeen: w.First, LastSeen: w.Last, URL: c.pageOf(org, w)}
}

func (c *Client) pageOf(org string, w wireIssue) string {
	if w.Permalink != "" {
		return w.Permalink
	}
	return c.URL + "/" + url.PathEscape(org) + "/issues/" + url.PathEscape(w.ID)
}

// Issues is the issues in a status across the projects asked for — every
// project of every organization when none are — with what was typed as the
// search, the last seen first.
func (c *Client) Issues(projects []Project, status, typed string) ([]Issue, error) {
	byOrg := map[string][]string{}
	var orgs []Org
	if len(projects) == 0 {
		var err error
		if orgs, err = c.Orgs(); err != nil {
			return nil, err
		}
	} else {
		for _, p := range projects {
			if _, ok := byOrg[p.Org]; !ok {
				orgs = append(orgs, Org{Slug: p.Org})
			}
			byOrg[p.Org] = append(byOrg[p.Org], p.Slug)
		}
	}
	query := "is:" + status
	if typed = strings.TrimSpace(typed); typed != "" {
		query += " " + typed
	}
	var mu sync.Mutex
	var out []Issue
	err := each(orgs, func(o Org) error {
		q := url.Values{"query": {query}, "limit": {fmt.Sprint(listLimit)}}
		var wire []wireIssue
		if err := c.do("GET", "/api/0/organizations/"+url.PathEscape(o.Slug)+"/issues/?"+q.Encode(), nil, &wire); err != nil {
			return err
		}
		want := map[string]bool{}
		for _, p := range byOrg[o.Slug] {
			want[p] = true
		}
		mu.Lock()
		defer mu.Unlock()
		for _, w := range wire {
			if len(want) == 0 || want[w.Project.Slug] {
				out = append(out, c.issue(o.Slug, w))
			}
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, err
}

// Frame is one line of a stack.
type Frame struct {
	File     string
	Line     int
	Function string
	// InApp is the application's own code, not a library's.
	InApp bool
	// Code is the line itself, as the SDK sent it.
	Code string
}

// Exception is one exception of an event's chain, its stack last call last.
type Exception struct {
	Type, Value string
	Frames      []Frame
}

// Crumb is one breadcrumb: what happened before.
type Crumb struct {
	Time     time.Time
	Category string
	Message  string
	Level    string
}

// Event is an issue's latest event: what an agent needs to fix it.
type Event struct {
	ID         string
	Title      string
	Message    string
	Platform   string
	Release    string
	When       time.Time
	Tags       [][2]string
	Exceptions []Exception
	Request    string
	Crumbs     []Crumb
}

// Latest is an issue's latest event.
func (c *Client) Latest(org, issueID string) (Event, error) {
	var w struct {
		EventID  string    `json:"eventID"`
		Title    string    `json:"title"`
		Message  string    `json:"message"`
		Platform string    `json:"platform"`
		Release  any       `json:"release"`
		Created  time.Time `json:"dateCreated"`
		Tags     []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
		Entries []struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"entries"`
	}
	if err := c.issuePath("GET", org, issueID, "events/latest/", nil, &w); err != nil {
		return Event{}, err
	}
	e := Event{ID: w.EventID, Title: w.Title, Message: w.Message, Platform: w.Platform, When: w.Created}
	switch r := w.Release.(type) {
	case string:
		e.Release = r
	case map[string]any:
		e.Release, _ = r["version"].(string)
	}
	for _, t := range w.Tags {
		e.Tags = append(e.Tags, [2]string{t.Key, t.Value})
	}
	for _, entry := range w.Entries {
		switch entry.Type {
		case "exception":
			var data struct {
				Values []struct {
					Type       string `json:"type"`
					Value      string `json:"value"`
					Stacktrace *struct {
						Frames []struct {
							Filename string `json:"filename"`
							AbsPath  string `json:"absPath"`
							Line     int    `json:"lineNo"`
							Function string `json:"function"`
							InApp    bool   `json:"inApp"`
							Code     string `json:"context_line"`
						} `json:"frames"`
					} `json:"stacktrace"`
				} `json:"values"`
			}
			if json.Unmarshal(entry.Data, &data) != nil {
				continue
			}
			for _, v := range data.Values {
				ex := Exception{Type: v.Type, Value: v.Value}
				if v.Stacktrace != nil {
					for _, f := range v.Stacktrace.Frames {
						file := f.AbsPath
						if file == "" {
							file = f.Filename
						}
						ex.Frames = append(ex.Frames, Frame{File: file, Line: f.Line, Function: f.Function,
							InApp: f.InApp, Code: strings.TrimSpace(f.Code)})
					}
				}
				e.Exceptions = append(e.Exceptions, ex)
			}
		case "request":
			var data struct {
				Method string `json:"method"`
				URL    string `json:"url"`
			}
			if json.Unmarshal(entry.Data, &data) == nil && data.URL != "" {
				e.Request = strings.TrimSpace(data.Method + " " + data.URL)
			}
		case "breadcrumbs":
			var data struct {
				Values []struct {
					Timestamp any    `json:"timestamp"`
					Category  string `json:"category"`
					Message   string `json:"message"`
					Level     string `json:"level"`
				} `json:"values"`
			}
			if json.Unmarshal(entry.Data, &data) != nil {
				continue
			}
			for _, b := range data.Values {
				crumb := Crumb{Category: b.Category, Message: b.Message, Level: b.Level}
				if s, ok := b.Timestamp.(string); ok {
					crumb.Time, _ = time.Parse(time.RFC3339Nano, s)
				}
				e.Crumbs = append(e.Crumbs, crumb)
			}
		}
	}
	return e, nil
}

// SetStatus marks an issue of an organization resolved, ignored or
// unresolved.
func (c *Client) SetStatus(org, issueID, status string) error {
	switch status {
	case StatusUnresolved, StatusResolved, StatusIgnored:
	default:
		return fmt.Errorf("an issue is unresolved, resolved or ignored, not %q", status)
	}
	return c.issuePath("PUT", org, issueID, "", map[string]string{"status": status}, nil)
}

// each asks every organization at once; the first failure is the answer.
func each(orgs []Org, ask func(Org) error) error {
	errs := make([]error, len(orgs))
	var wg sync.WaitGroup
	for i, o := range orgs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = ask(o)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// FixPrompt is what an agent is told to fix an issue with: the error, where
// it is in the application's own code — the stack's frames that are, last
// call first — the request and the environment, and that the paths are the
// server's, so the code is to be found by what follows the application's
// root.
func FixPrompt(i Issue, e Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fix this error reported by GlitchTip (%s, %s, seen %d times; %s).\n", i.ShortID, i.Level, i.Count, i.URL)
	fmt.Fprintf(&b, "\n%s\n", i.Title)
	if i.Culprit != "" {
		fmt.Fprintf(&b, "in %s\n", i.Culprit)
	}
	for _, ex := range e.Exceptions {
		fmt.Fprintf(&b, "\n%s: %s\n", ex.Type, ex.Value)
		for n := len(ex.Frames) - 1; n >= 0; n-- {
			f := ex.Frames[n]
			if !f.InApp {
				continue
			}
			fmt.Fprintf(&b, "  at %s (%s:%d)", f.Function, f.File, f.Line)
			if f.Code != "" {
				fmt.Fprintf(&b, "  →  %s", f.Code)
			}
			b.WriteString("\n")
		}
	}
	if e.Request != "" {
		fmt.Fprintf(&b, "\nRequest: %s\n", e.Request)
	}
	var env []string
	for _, t := range e.Tags {
		switch t[0] {
		case "environment", "release", "server_name", "runtime", "os.name", "url", "transaction":
			env = append(env, t[0]+"="+t[1])
		}
	}
	if len(env) > 0 {
		fmt.Fprintf(&b, "Context: %s\n", strings.Join(env, ", "))
	}
	b.WriteString("\nThe paths are where the code runs on the server; find the same files in this repository. Find the cause, fix it, and say what you changed.")
	return b.String()
}
