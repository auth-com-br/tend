package tickets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Jira, through its REST API's version 2, which Jira Cloud, Server and Data
// Center all keep and which takes and gives text as plain strings. A Jira
// Cloud token goes with the email it belongs to, by basic auth; a Server or
// Data Center personal access token alone, as a bearer; with no token, a
// public Jira is read anonymously.
//
// Jira Cloud replaced its search with /search/jql and took the old one
// away; Server and Data Center have only the old one. The new is asked
// first, and the old on a 404.
type jira struct {
	a    Account
	http *http.Client
}

func (j *jira) base() string { return strings.TrimRight(j.a.URL, "/") + "/rest/api/2" }

func (j *jira) header() map[string]string {
	switch {
	case j.a.Token == "":
		return nil
	case j.a.Email != "":
		return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(j.a.Email+":"+j.a.Token))}
	}
	return map[string]string{"Authorization": "Bearer " + j.a.Token}
}

func (j *jira) do(method, path string, body, out any) (int, error) {
	return call(j.http, method, j.base()+path, j.header(), body, out)
}

func (j *jira) WhoAmI() (string, error) {
	if j.a.Token == "" {
		return "", ErrNoToken
	}
	var me struct {
		DisplayName string `json:"displayName"`
		Name        string `json:"name"`
	}
	if _, err := j.do("GET", "/myself", nil, &me); err != nil {
		return "", err
	}
	if me.DisplayName != "" {
		return me.DisplayName, nil
	}
	return me.Name, nil
}

func (j *jira) Scopes() ([]Scope, error) {
	var projects []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if _, err := j.do("GET", "/project", nil, &projects); err != nil {
		return nil, err
	}
	out := make([]Scope, 0, len(projects))
	for _, p := range projects {
		out = append(out, Scope{ID: p.Key, Name: p.Key})
	}
	return out, nil
}

// jql is the search a list runs.
func jql(scope string, filter Filter, query string) string {
	var parts []string
	if scope != "" {
		parts = append(parts, fmt.Sprintf("project = %q", scope))
	}
	switch filter {
	case FilterMine:
		parts = append(parts, "assignee = currentUser()", "statusCategory != Done")
	case FilterClosed:
		parts = append(parts, "statusCategory = Done")
	default:
		parts = append(parts, "statusCategory != Done")
	}
	if q := strings.TrimSpace(query); q != "" {
		parts = append(parts, fmt.Sprintf("text ~ %q", q))
	}
	return strings.Join(parts, " AND ") + " ORDER BY updated DESC"
}

type jiraFields struct {
	Summary string `json:"summary"`
	Status  struct {
		Name     string `json:"name"`
		Category struct {
			Key string `json:"key"`
		} `json:"statusCategory"`
	} `json:"status"`
	Assignee *struct {
		DisplayName string `json:"displayName"`
	} `json:"assignee"`
	Updated     string               `json:"updated"`
	Project     struct{ Key string } `json:"project"`
	Description string               `json:"description"`
	Comment     struct {
		Comments []struct {
			Author struct {
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Body    string `json:"body"`
			Created string `json:"created"`
		} `json:"comments"`
	} `json:"comment"`
}

type jiraIssue struct {
	Key    string     `json:"key"`
	Fields jiraFields `json:"fields"`
}

// jiraTime reads Jira's times, whose offset has no colon.
func jiraTime(s string) time.Time {
	t, err := time.Parse("2006-01-02T15:04:05.000-0700", s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func (j *jira) ticket(w jiraIssue) Ticket {
	t := Ticket{Key: w.Key, Ref: w.Key, Title: w.Fields.Summary, State: w.Fields.Status.Name,
		Done: w.Fields.Status.Category.Key == "done", Scope: w.Fields.Project.Key,
		Updated: jiraTime(w.Fields.Updated), URL: strings.TrimRight(j.a.URL, "/") + "/browse/" + w.Key}
	if w.Fields.Assignee != nil {
		t.Assignee = w.Fields.Assignee.DisplayName
	}
	return t
}

func (j *jira) List(scope string, filter Filter, query string) ([]Ticket, error) {
	if filter == FilterMine && j.a.Token == "" {
		return nil, ErrNoToken
	}
	q := url.Values{
		"jql":        {jql(scope, filter, query)},
		"fields":     {"summary,status,assignee,updated,project"},
		"maxResults": {fmt.Sprint(listLimit)},
	}
	var res struct {
		Issues []jiraIssue `json:"issues"`
	}
	status, err := j.do("GET", "/search/jql?"+q.Encode(), nil, &res)
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		_, err = j.do("GET", "/search?"+q.Encode(), nil, &res)
	}
	if err != nil {
		return nil, err
	}
	out := make([]Ticket, 0, len(res.Issues))
	for _, w := range res.Issues {
		out = append(out, j.ticket(w))
	}
	return out, nil
}

func (j *jira) Get(ref string) (Detail, error) {
	var w jiraIssue
	path := "/issue/" + url.PathEscape(ref) + "?fields=summary,status,assignee,updated,project,description,comment"
	if _, err := j.do("GET", path, nil, &w); err != nil {
		return Detail{}, err
	}
	d := Detail{Ticket: j.ticket(w), Body: w.Fields.Description}
	for _, c := range w.Fields.Comment.Comments {
		d.Comments = append(d.Comments, Comment{Author: c.Author.DisplayName, Body: c.Body, Created: jiraTime(c.Created)})
	}
	return d, nil
}

func (j *jira) Comment(ref, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment needs some text")
	}
	if j.a.Token == "" {
		return ErrNoToken
	}
	_, err := j.do("POST", "/issue/"+url.PathEscape(ref)+"/comment", map[string]string{"body": body}, nil)
	return err
}

// Close takes the issue through the first transition its workflow offers
// into a finished status: Jira has no "close", only the transitions each
// project's workflow defines.
func (j *jira) Close(ref string) error {
	if j.a.Token == "" {
		return ErrNoToken
	}
	var res struct {
		Transitions []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			To   struct {
				Category struct {
					Key string `json:"key"`
				} `json:"statusCategory"`
			} `json:"to"`
		} `json:"transitions"`
	}
	if _, err := j.do("GET", "/issue/"+url.PathEscape(ref)+"/transitions", nil, &res); err != nil {
		return err
	}
	for _, t := range res.Transitions {
		if t.To.Category.Key == "done" {
			_, err := j.do("POST", "/issue/"+url.PathEscape(ref)+"/transitions",
				map[string]any{"transition": map[string]string{"id": t.ID}}, nil)
			return err
		}
	}
	return fmt.Errorf("%s's workflow offers no way from here to a finished status", ref)
}
