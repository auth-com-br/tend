package tickets

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Linear, through its GraphQL API, with a personal API key sent as the
// Authorization header itself — no "Bearer", as Linear's documentation
// says for personal keys. An issue is named by its identifier (ENG-12),
// which Linear's issue(id:) takes; writing needs its id, read first.
type linear struct {
	a    Account
	http *http.Client
}

func (l *linear) endpoint() string {
	if l.a.URL != "" {
		return strings.TrimRight(l.a.URL, "/")
	}
	return "https://api.linear.app/graphql"
}

// gql runs a query and decodes its data into out. GraphQL answers 200 with
// errors in the body, which are read as errors here.
func (l *linear) gql(query string, vars map[string]any, out any) error {
	if l.a.Token == "" {
		return ErrNoToken
	}
	var res struct {
		Data   any `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Type string `json:"type"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	res.Data = out
	_, err := call(l.http, "POST", l.endpoint(), map[string]string{"Authorization": l.a.Token},
		map[string]any{"query": query, "variables": vars}, &res)
	if err != nil {
		return err
	}
	if len(res.Errors) > 0 {
		e := res.Errors[0]
		if strings.Contains(strings.ToLower(e.Extensions.Type+" "+e.Message), "authenticat") {
			return ErrBadToken
		}
		return fmt.Errorf("Linear said: %s", e.Message)
	}
	return nil
}

func (l *linear) WhoAmI() (string, error) {
	var d struct {
		Viewer struct {
			Name string `json:"name"`
		} `json:"viewer"`
	}
	if err := l.gql(`query { viewer { name } }`, nil, &d); err != nil {
		return "", err
	}
	return d.Viewer.Name, nil
}

func (l *linear) Scopes() ([]Scope, error) {
	var d struct {
		Teams struct {
			Nodes []struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := l.gql(`query { teams(first: 100) { nodes { id key } } }`, nil, &d); err != nil {
		return nil, err
	}
	out := make([]Scope, 0, len(d.Teams.Nodes))
	for _, t := range d.Teams.Nodes {
		out = append(out, Scope{ID: t.ID, Name: t.Key})
	}
	return out, nil
}

// linearIssueFields are what a ticket is read with.
const linearIssueFields = `id identifier title url updatedAt state { name type } assignee { name } team { key }`

type linearIssue struct {
	ID         string    `json:"id"`
	Identifier string    `json:"identifier"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	Updated    time.Time `json:"updatedAt"`
	State      struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"state"`
	Assignee *struct {
		Name string `json:"name"`
	} `json:"assignee"`
	Team struct {
		Key string `json:"key"`
	} `json:"team"`
	Description string `json:"description"`
	Comments    struct {
		Nodes []struct {
			Body    string    `json:"body"`
			Created time.Time `json:"createdAt"`
			User    *struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"nodes"`
	} `json:"comments"`
}

// linearDone are the state types Linear counts as over.
func linearDone(t string) bool { return t == "completed" || t == "canceled" }

func (w linearIssue) ticket() Ticket {
	t := Ticket{Key: w.Identifier, Ref: w.Identifier, Title: w.Title, State: w.State.Name, Done: linearDone(w.State.Type),
		Scope: w.Team.Key, Updated: w.Updated, URL: w.URL}
	if w.Assignee != nil {
		t.Assignee = w.Assignee.Name
	}
	return t
}

func (l *linear) List(scope string, filter Filter, query string) ([]Ticket, error) {
	f := map[string]any{}
	if scope != "" {
		f["team"] = map[string]any{"id": map[string]any{"eq": scope}}
	}
	over := []string{"completed", "canceled"}
	switch filter {
	case FilterClosed:
		f["state"] = map[string]any{"type": map[string]any{"in": over}}
	case FilterMine:
		f["assignee"] = map[string]any{"isMe": map[string]any{"eq": true}}
		f["state"] = map[string]any{"type": map[string]any{"nin": over}}
	default:
		f["state"] = map[string]any{"type": map[string]any{"nin": over}}
	}
	if q := strings.TrimSpace(query); q != "" {
		f["title"] = map[string]any{"containsIgnoreCase": q}
	}
	var d struct {
		Issues struct {
			Nodes []linearIssue `json:"nodes"`
		} `json:"issues"`
	}
	q := `query($f: IssueFilter) { issues(first: ` + fmt.Sprint(listLimit) + `, orderBy: updatedAt, filter: $f) { nodes { ` + linearIssueFields + ` } } }`
	if err := l.gql(q, map[string]any{"f": f}, &d); err != nil {
		return nil, err
	}
	out := make([]Ticket, 0, len(d.Issues.Nodes))
	for _, w := range d.Issues.Nodes {
		out = append(out, w.ticket())
	}
	return out, nil
}

func (l *linear) issue(ref string, fields string) (linearIssue, error) {
	var d struct {
		Issue *linearIssue `json:"issue"`
	}
	if err := l.gql(`query($id: String!) { issue(id: $id) { `+fields+` } }`, map[string]any{"id": ref}, &d); err != nil {
		return linearIssue{}, err
	}
	if d.Issue == nil {
		return linearIssue{}, fmt.Errorf("Linear has no issue %s", ref)
	}
	return *d.Issue, nil
}

func (l *linear) Get(ref string) (Detail, error) {
	w, err := l.issue(ref, linearIssueFields+` description comments(first: 100) { nodes { body createdAt user { name } } }`)
	if err != nil {
		return Detail{}, err
	}
	d := Detail{Ticket: w.ticket(), Body: w.Description}
	for _, c := range w.Comments.Nodes {
		author := "someone"
		if c.User != nil {
			author = c.User.Name
		}
		d.Comments = append(d.Comments, Comment{Author: author, Body: c.Body, Created: c.Created})
	}
	sort.SliceStable(d.Comments, func(i, j int) bool { return d.Comments[i].Created.Before(d.Comments[j].Created) })
	return d, nil
}

func (l *linear) Comment(ref, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment needs some text")
	}
	w, err := l.issue(ref, "id")
	if err != nil {
		return err
	}
	var d struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	return l.gql(`mutation($id: String!, $body: String!) { commentCreate(input: { issueId: $id, body: $body }) { success } }`,
		map[string]any{"id": w.ID, "body": body}, &d)
}

// Close moves the issue to its team's first completed state.
func (l *linear) Close(ref string) error {
	var d struct {
		Issue *struct {
			ID   string `json:"id"`
			Team struct {
				States struct {
					Nodes []struct {
						ID string `json:"id"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	q := `query($id: String!) { issue(id: $id) { id team { states(filter: { type: { eq: "completed" } }) { nodes { id } } } } }`
	if err := l.gql(q, map[string]any{"id": ref}, &d); err != nil {
		return err
	}
	if d.Issue == nil || len(d.Issue.Team.States.Nodes) == 0 {
		return fmt.Errorf("Linear has no completed state for %s's team", ref)
	}
	var u struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	return l.gql(`mutation($id: String!, $state: String!) { issueUpdate(id: $id, input: { stateId: $state }) { success } }`,
		map[string]any{"id": d.Issue.ID, "state": d.Issue.Team.States.Nodes[0].ID}, &u)
}
