// Package tickets reads and changes tickets in the trackers teams keep
// their work in beside their code — Linear, Jira and ClickUp (#9) — for
// the tickets panel: what is open, what is the user's, one ticket whole
// with its comments, a comment, and closing one, which each tracker does
// its own way. The client talks to each tracker's web API itself, as it
// talks to GlitchTip: a tracker is a web service, not something on the
// machine the projects are on.
package tickets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Kind is a tracker.
type Kind string

const (
	Linear  Kind = "linear"
	Jira    Kind = "jira"
	ClickUp Kind = "clickup"
)

// Kinds are the trackers tend knows, in the order it offers them.
var Kinds = []Kind{Linear, Jira, ClickUp}

// Account is one account on a tracker: its token, and for Jira the site
// and, on Jira Cloud, the email the token belongs to (Jira Server and Data
// Center take a personal access token alone). URL may also point Linear or
// ClickUp elsewhere, which only a test does.
type Account struct {
	Kind  Kind
	URL   string
	Email string
	Token string
}

// Scope is where tickets are kept: a Linear team, a Jira project, a
// ClickUp workspace.
type Scope struct {
	ID   string
	Name string
}

// Filter is one of the list's presets.
type Filter int

const (
	FilterOpen Filter = iota
	FilterMine
	FilterClosed
)

// Filters are the presets in the order the list walks them.
var Filters = []Filter{FilterOpen, FilterMine, FilterClosed}

func (f Filter) String() string {
	switch f {
	case FilterMine:
		return "mine"
	case FilterClosed:
		return "closed"
	}
	return "open"
}

// Ticket is one ticket as the list shows it.
type Ticket struct {
	// Key is how people name it (ENG-12, PROJ-42, a ClickUp task's custom
	// id or its id), and Ref how the tracker's API does.
	Key, Ref string
	Title    string
	State    string
	// Done is a state the tracker counts as finished.
	Done     bool
	Assignee string
	Scope    string
	Updated  time.Time
	URL      string
}

// Comment is one comment on a ticket.
type Comment struct {
	Author  string
	Body    string
	Created time.Time
}

// Detail is a ticket whole.
type Detail struct {
	Ticket
	Body     string
	Comments []Comment
}

// Client is a tracker account.
type Client interface {
	// WhoAmI is the user the token signs in as: what connecting checks.
	WhoAmI() (string, error)
	// Scopes are the teams, projects or workspaces the account sees.
	Scopes() ([]Scope, error)
	// List is the tickets of a scope — all of them for "" — for a preset
	// and what was typed, the last updated first.
	List(scope string, filter Filter, query string) ([]Ticket, error)
	// Get is one ticket whole, by its Ref.
	Get(ref string) (Detail, error)
	// Comment adds a comment to a ticket.
	Comment(ref, body string) error
	// Close moves a ticket to its tracker's finished state.
	Close(ref string) error
}

var (
	// ErrBadToken is a token the tracker refused.
	ErrBadToken = errors.New("the tracker refused the token")
	// ErrNoToken is an account with no token, which every tracker wants
	// but a public Jira.
	ErrNoToken = errors.New("this needs a token")
)

// New is a client for an account.
func New(a Account) (Client, error) {
	switch a.Kind {
	case Linear:
		return &linear{a: a, http: httpClient()}, nil
	case Jira:
		if strings.TrimSpace(a.URL) == "" {
			return nil, errors.New("a Jira account needs its site's address")
		}
		return &jira{a: a, http: httpClient()}, nil
	case ClickUp:
		return &clickup{a: a, http: httpClient()}, nil
	}
	return nil, fmt.Errorf("tend does not know the tracker %q", a.Kind)
}

// callTimeout bounds one call: a tracker that does not answer should not
// hold the panel.
const callTimeout = 20 * time.Second

func httpClient() *http.Client { return &http.Client{Timeout: callTimeout} }

// listLimit is how many tickets a list reads.
const listLimit = 100

// call asks a tracker and decodes its answer into out; status is the
// answer's status, for a caller that falls back on a 404.
func call(c *http.Client, method, url string, header map[string]string, body, out any) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("the tracker did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return resp.StatusCode, ErrBadToken
	case resp.StatusCode >= 300:
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return resp.StatusCode, fmt.Errorf("the tracker answered %s: %s", resp.Status, msg)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("reading the tracker's answer: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// FixPrompt is what an agent is told to work on a ticket with: the ticket,
// its text and its comments, and where it is.
func FixPrompt(d Detail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Work on this ticket (%s): %s\n", d.Key, d.Title)
	if d.URL != "" {
		fmt.Fprintf(&b, "%s\n", d.URL)
	}
	if d.State != "" {
		fmt.Fprintf(&b, "State: %s\n", d.State)
	}
	if body := strings.TrimSpace(d.Body); body != "" {
		fmt.Fprintf(&b, "\n%s\n", body)
	}
	if len(d.Comments) > 0 {
		b.WriteString("\nComments, oldest first:\n")
		for _, c := range d.Comments {
			fmt.Fprintf(&b, "- %s: %s\n", c.Author, strings.Join(strings.Fields(c.Body), " "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
