package tickets

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// The Issue Gateway, a self-hosted REST API (auth-com-br/issue-gateway#27),
// at the address the account keeps, with a bearer token. Unlike the other
// trackers it filters, searches and orders on the server: a list asks with
// the preset's name and what was typed, and reads the answer as it comes.
// A ticket's Ref is the gateway's id, its Key what people call it (IG-12).
type issueGateway struct {
	a    Account
	http *http.Client
}

func (g *issueGateway) base() string { return strings.TrimRight(g.a.URL, "/") + "/v1" }

func (g *issueGateway) do(method, path string, body, out any) error {
	if g.a.Token == "" {
		return ErrNoToken
	}
	_, err := call(g.http, method, g.base()+path, map[string]string{"Authorization": "Bearer " + g.a.Token}, body, out)
	return err
}

func (g *issueGateway) WhoAmI() (string, error) {
	var me struct {
		Name string `json:"name"`
	}
	if err := g.do("GET", "/me", nil, &me); err != nil {
		return "", err
	}
	return me.Name, nil
}

func (g *issueGateway) Scopes() ([]Scope, error) {
	var res struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := g.do("GET", "/scopes", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Scope, 0, len(res.Items))
	for _, s := range res.Items {
		out = append(out, Scope{ID: s.ID, Name: s.Name})
	}
	return out, nil
}

type issueGatewayIssue struct {
	Key      string    `json:"key"`
	Ref      string    `json:"ref"`
	Title    string    `json:"title"`
	State    string    `json:"state"`
	Done     bool      `json:"done"`
	Assignee string    `json:"assignee"`
	Scope    string    `json:"scope"`
	Updated  time.Time `json:"updated"`
	URL      string    `json:"url"`
	Body     string    `json:"body"`
	Comments []struct {
		Author  string    `json:"author"`
		Body    string    `json:"body"`
		Created time.Time `json:"created"`
	} `json:"comments"`
}

func (w issueGatewayIssue) ticket() Ticket {
	return Ticket{Key: w.Key, Ref: w.Ref, Title: w.Title, State: w.State, Done: w.Done,
		Assignee: w.Assignee, Scope: w.Scope, Updated: w.Updated, URL: w.URL}
}

func (g *issueGateway) List(scope string, filter Filter, query string) ([]Ticket, error) {
	q := url.Values{"scope": {scope}, "filter": {filter.String()}, "q": {strings.TrimSpace(query)}}
	var res struct {
		Items []issueGatewayIssue `json:"items"`
	}
	if err := g.do("GET", "/issues?"+q.Encode(), nil, &res); err != nil {
		return nil, err
	}
	out := make([]Ticket, 0, len(res.Items))
	for _, w := range res.Items {
		out = append(out, w.ticket())
	}
	return out, nil
}

func (g *issueGateway) Get(ref string) (Detail, error) {
	var w issueGatewayIssue
	if err := g.do("GET", "/issues/"+url.PathEscape(ref), nil, &w); err != nil {
		return Detail{}, err
	}
	d := Detail{Ticket: w.ticket(), Body: w.Body}
	for _, c := range w.Comments {
		author := c.Author
		if author == "" {
			author = "someone"
		}
		d.Comments = append(d.Comments, Comment{Author: author, Body: c.Body, Created: c.Created})
	}
	// The contract names no order for the comments; the panel reads a
	// thread down.
	sort.SliceStable(d.Comments, func(i, j int) bool { return d.Comments[i].Created.Before(d.Comments[j].Created) })
	return d, nil
}

func (g *issueGateway) Comment(ref, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment needs some text")
	}
	return g.do("POST", "/issues/"+url.PathEscape(ref)+"/comments", map[string]string{"body": body}, nil)
}

// Close asks the gateway to close the issue: what closing means is its
// own, as a workflow is Jira's.
func (g *issueGateway) Close(ref string) error {
	return g.do("POST", "/issues/"+url.PathEscape(ref)+"/close", nil, nil)
}
