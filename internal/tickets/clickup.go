package tickets

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClickUp, through its API v2, with a personal token sent as the
// Authorization header itself. A scope is a workspace (a "team" in the
// API), whose tasks are listed across its lists; ClickUp searches no task
// text through this API, so what is typed filters the names read.
type clickup struct {
	a    Account
	http *http.Client

	once  sync.Once
	me    int
	meErr error
}

func (c *clickup) base() string {
	if c.a.URL != "" {
		return strings.TrimRight(c.a.URL, "/")
	}
	return "https://api.clickup.com/api/v2"
}

func (c *clickup) do(method, path string, body, out any) error {
	if c.a.Token == "" {
		return ErrNoToken
	}
	_, err := call(c.http, method, c.base()+path, map[string]string{"Authorization": c.a.Token}, body, out)
	return err
}

type clickupUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

func (c *clickup) user() (clickupUser, error) {
	var d struct {
		User clickupUser `json:"user"`
	}
	err := c.do("GET", "/user", nil, &d)
	return d.User, err
}

func (c *clickup) WhoAmI() (string, error) {
	u, err := c.user()
	return u.Username, err
}

// myID is the signed-in user's id, which "mine" filters by; asked once.
func (c *clickup) myID() (int, error) {
	c.once.Do(func() {
		u, err := c.user()
		c.me, c.meErr = u.ID, err
	})
	return c.me, c.meErr
}

func (c *clickup) Scopes() ([]Scope, error) {
	var d struct {
		Teams []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := c.do("GET", "/team", nil, &d); err != nil {
		return nil, err
	}
	out := make([]Scope, 0, len(d.Teams))
	for _, t := range d.Teams {
		out = append(out, Scope{ID: t.ID, Name: t.Name})
	}
	return out, nil
}

type clickupTask struct {
	ID       string `json:"id"`
	CustomID string `json:"custom_id"`
	Name     string `json:"name"`
	Text     string `json:"text_content"`
	Status   struct {
		Status string `json:"status"`
		Type   string `json:"type"`
	} `json:"status"`
	Assignees []clickupUser `json:"assignees"`
	Updated   string        `json:"date_updated"`
	URL       string        `json:"url"`
	List      struct {
		ID string `json:"id"`
	} `json:"list"`
	TeamID string `json:"team_id"`
}

// clickupTime reads ClickUp's times, milliseconds since 1970 in a string.
func clickupTime(s string) time.Time {
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (w clickupTask) ticket(scope string) Ticket {
	key := w.CustomID
	if key == "" {
		key = w.ID
	}
	t := Ticket{Key: key, Ref: w.ID, Title: w.Name, State: w.Status.Status,
		Done: w.Status.Type == "closed" || w.Status.Type == "done", Scope: scope, Updated: clickupTime(w.Updated), URL: w.URL}
	var names []string
	for _, a := range w.Assignees {
		names = append(names, a.Username)
	}
	t.Assignee = strings.Join(names, ", ")
	return t
}

func (c *clickup) List(scope string, filter Filter, query string) ([]Ticket, error) {
	scopes := []Scope{{ID: scope}}
	if scope == "" {
		all, err := c.Scopes()
		if err != nil {
			return nil, err
		}
		scopes = all
	}
	q := url.Values{"order_by": {"updated"}, "subtasks": {"true"}}
	switch filter {
	case FilterClosed:
		q.Set("include_closed", "true")
	case FilterMine:
		me, err := c.myID()
		if err != nil {
			return nil, err
		}
		q.Add("assignees[]", strconv.Itoa(me))
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	var out []Ticket
	for _, s := range scopes {
		var d struct {
			Tasks []clickupTask `json:"tasks"`
		}
		if err := c.do("GET", "/team/"+url.PathEscape(s.ID)+"/task?"+q.Encode(), nil, &d); err != nil {
			return nil, err
		}
		for _, w := range d.Tasks {
			t := w.ticket(s.Name)
			if filter == FilterClosed && !t.Done {
				continue // include_closed lists the open too
			}
			if needle != "" && !strings.Contains(strings.ToLower(t.Title), needle) {
				continue
			}
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > listLimit {
		out = out[:listLimit]
	}
	return out, nil
}

func (c *clickup) Get(ref string) (Detail, error) {
	var w clickupTask
	if err := c.do("GET", "/task/"+url.PathEscape(ref), nil, &w); err != nil {
		return Detail{}, err
	}
	var cs struct {
		Comments []struct {
			Text string      `json:"comment_text"`
			User clickupUser `json:"user"`
			Date string      `json:"date"`
		} `json:"comments"`
	}
	if err := c.do("GET", "/task/"+url.PathEscape(ref)+"/comment", nil, &cs); err != nil {
		return Detail{}, err
	}
	d := Detail{Ticket: w.ticket(""), Body: w.Text}
	for _, x := range cs.Comments {
		d.Comments = append(d.Comments, Comment{Author: x.User.Username, Body: x.Text, Created: clickupTime(x.Date)})
	}
	// ClickUp lists comments newest first; the panel reads a thread down.
	sort.SliceStable(d.Comments, func(i, j int) bool { return d.Comments[i].Created.Before(d.Comments[j].Created) })
	return d, nil
}

func (c *clickup) Comment(ref, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("a comment needs some text")
	}
	return c.do("POST", "/task/"+url.PathEscape(ref)+"/comment", map[string]any{"comment_text": body, "notify_all": false}, nil)
}

// Close sets the task to its list's closed status, whatever that list
// calls it.
func (c *clickup) Close(ref string) error {
	var w clickupTask
	if err := c.do("GET", "/task/"+url.PathEscape(ref), nil, &w); err != nil {
		return err
	}
	var list struct {
		Statuses []struct {
			Status string `json:"status"`
			Type   string `json:"type"`
		} `json:"statuses"`
	}
	if err := c.do("GET", "/list/"+url.PathEscape(w.List.ID), nil, &list); err != nil {
		return err
	}
	for _, s := range list.Statuses {
		if s.Type == "closed" || s.Type == "done" {
			return c.do("PUT", "/task/"+url.PathEscape(ref), map[string]string{"status": s.Status}, nil)
		}
	}
	return fmt.Errorf("the list of %s has no closed status", ref)
}
