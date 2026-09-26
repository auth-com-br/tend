package glitchtip

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeServer answers as GlitchTip does, for two organizations, and records
// what was changed.
func fakeServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var changed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := r.URL.Path
		switch {
		case p == "/api/0/organizations/":
			_, _ = io.WriteString(w, `[{"slug":"shop","name":"Shop"},{"slug":"crm","name":"CRM"}]`)
		case p == "/api/0/organizations/shop/projects/":
			_, _ = io.WriteString(w, `[{"slug":"api","name":"api","platform":"node"},{"slug":"web","name":"web","platform":"javascript"}]`)
		case p == "/api/0/organizations/crm/projects/":
			_, _ = io.WriteString(w, `[{"slug":"crm-api","name":"crm-api","platform":"node"}]`)
		case p == "/api/0/organizations/shop/issues/":
			if r.URL.Query().Get("query") != "is:unresolved timeout" {
				t.Errorf("query: %q", r.URL.Query().Get("query"))
			}
			_, _ = io.WriteString(w, `[
				{"id":"75","shortId":"API-26","title":"KnexTimeoutError: pool full","culprit":"ApiController","level":"error","status":"unresolved","count":"1543","userCount":2,"firstSeen":"2026-08-01T10:00:00Z","lastSeen":"2026-08-09T12:00:00Z","project":{"slug":"api"}},
				{"id":"80","shortId":"WEB-3","title":"TypeError: x is undefined","level":"error","status":"unresolved","count":4,"lastSeen":"2026-09-01T12:00:00Z","project":{"slug":"web"}}]`)
		case p == "/api/0/organizations/crm/issues/":
			_, _ = io.WriteString(w, `[{"id":"9","shortId":"CRM-1","title":"fatal boot","level":"fatal","status":"unresolved","count":1,"lastSeen":"2026-09-10T12:00:00Z","project":{"slug":"crm-api"}}]`)
		case p == "/api/0/issues/75/events/latest/":
			_, _ = io.WriteString(w, `{"eventID":"e1","title":"KnexTimeoutError","platform":"node","dateCreated":"2026-08-09T12:00:00Z",
				"tags":[{"key":"environment","value":"production"},{"key":"server_name","value":"ip-1"}],
				"entries":[
				 {"type":"exception","data":{"values":[{"type":"KnexTimeoutError","value":"pool full","stacktrace":{"frames":[
				   {"absPath":"/app/node_modules/knex/client.js","lineNo":465,"function":"acquire","inApp":false},
				   {"absPath":"/app/src/models/Score.js","lineNo":144,"function":"complement","inApp":true,"context_line":"  await pg('t').first();"},
				   {"absPath":"/app/src/controllers/Api.js","lineNo":288,"function":"Api.external","inApp":true}]}}]}},
				 {"type":"request","data":{"method":"POST","url":"https://api.test/consults"}},
				 {"type":"breadcrumbs","data":{"values":[{"timestamp":"2026-08-09T11:59:59Z","category":"http","message":"GET /health","level":"info"}]}}]}`)
		case p == "/api/0/issues/75/" && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			changed = append(changed, "75 "+body["status"])
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &changed
}

// TestErrorsAreReadAcrossOrganizations: every organization's issues are
// read with the status and what was typed, put together the last seen
// first, each with its organization, project and page; a count given as a
// string is read; and only the projects asked for are kept. If it
// regresses, the errors panel shows one organization's errors, or an
// issue's page is the wrong one.
func TestErrorsAreReadAcrossOrganizations(t *testing.T) {
	srv, _ := fakeServer(t)
	c := New(srv.URL+"/", "good")
	projects, err := c.Projects()
	if err != nil || len(projects) != 3 || projects[0].Org != "crm" || projects[1].Slug != "api" {
		t.Fatalf("projects: %+v %v", projects, err)
	}
	issues, err := c.Issues(nil, StatusUnresolved, "timeout")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, i := range issues {
		got = append(got, i.ShortID)
	}
	if strings.Join(got, " ") != "CRM-1 WEB-3 API-26" {
		t.Errorf("order: %v", got)
	}
	api := issues[2]
	if api.Org != "shop" || api.Project != "api" || api.Count != 1543 || api.Users != 2 || api.URL != srv.URL+"/shop/issues/75" {
		t.Errorf("issue: %+v", api)
	}
	only, err := c.Issues([]Project{{Org: "shop", Slug: "api"}}, StatusUnresolved, "timeout")
	if err != nil || len(only) != 1 || only[0].ShortID != "API-26" {
		t.Errorf("one project: %+v %v", only, err)
	}
}

// TestAnErrorsLatestEventIsWhatAnAgentNeeds: the latest event gives the
// exception, its stack, the request, the tags and the breadcrumbs; the
// prompt an agent is given has the error and the application's own frames
// only, last call first, with their code, the request and where it ran.
// If it regresses, the agent is handed a library's frames to fix, or
// nothing to find the code by.
func TestAnErrorsLatestEventIsWhatAnAgentNeeds(t *testing.T) {
	srv, _ := fakeServer(t)
	c := New(srv.URL, "good")
	issues, _ := c.Issues([]Project{{Org: "shop", Slug: "api"}}, StatusUnresolved, "timeout")
	e, err := c.Latest("shop", "75")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Exceptions) != 1 || len(e.Exceptions[0].Frames) != 3 || e.Request != "POST https://api.test/consults" || len(e.Crumbs) != 1 {
		t.Fatalf("event: %+v", e)
	}
	prompt := FixPrompt(issues[0], e)
	for _, want := range []string{"API-26", "KnexTimeoutError: pool full",
		"at Api.external (/app/src/controllers/Api.js:288)\n  at complement (/app/src/models/Score.js:144)  →  await pg('t').first();",
		"Request: POST https://api.test/consults", "environment=production"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "node_modules") {
		t.Errorf("a library's frame went to the agent:\n%s", prompt)
	}
}

// TestAnErrorIsResolvedAndATokenRefused: a status is set with PUT, only to
// what GlitchTip has; a token the server refuses, and no token at all, are
// told apart. If it regresses, resolving an error does nothing, or a wrong
// token reads as GlitchTip being down.
func TestAnErrorIsResolvedAndATokenRefused(t *testing.T) {
	srv, changed := fakeServer(t)
	if err := New(srv.URL, "good").SetStatus("shop", "75", StatusResolved); err != nil {
		t.Fatal(err)
	}
	if err := New(srv.URL, "good").SetStatus("shop", "75", "deleted"); err == nil {
		t.Error("a status GlitchTip does not have was sent")
	}
	if strings.Join(*changed, ",") != "75 resolved" {
		t.Errorf("changed: %v", *changed)
	}
	if _, err := New(srv.URL, "bad").Orgs(); !errors.Is(err, ErrBadToken) {
		t.Errorf("bad token: %v", err)
	}
	if _, err := New(srv.URL, "").Orgs(); !errors.Is(err, ErrNotConnected) {
		t.Errorf("no token: %v", err)
	}
}

// TestSentryIsServedByTheSameClient: a server that has only the paths
// Sentry documents — an issue's event and status under its organization —
// is still read and changed, and an issue's page is the permalink it gives.
// The server is a stand-in written from Sentry's API documentation; no
// real Sentry was asked. If it regresses, connecting sentry.io lists the
// errors and then cannot open or resolve any of them.
func TestSentryIsServedByTheSameClient(t *testing.T) {
	var mu sync.Mutex
	var put []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sntrys" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch p := r.URL.Path; {
		case p == "/api/0/organizations/":
			_, _ = io.WriteString(w, `[{"slug":"acme","name":"Acme"}]`)
		case p == "/api/0/organizations/acme/projects/":
			_, _ = io.WriteString(w, `[{"slug":"web","name":"web"}]`)
		case p == "/api/0/organizations/acme/issues/":
			_, _ = io.WriteString(w, `[{"id":"4001","shortId":"WEB-1","title":"TypeError","level":"error","status":"unresolved","count":"7","userCount":3,"lastSeen":"2026-09-20T12:00:00Z","permalink":"https://acme.sentry.io/issues/4001/","project":{"slug":"web"}}]`)
		case p == "/api/0/organizations/acme/issues/4001/events/latest/":
			_, _ = io.WriteString(w, `{"eventID":"e9","entries":[{"type":"exception","data":{"values":[{"type":"TypeError","value":"x is undefined"}]}}]}`)
		case p == "/api/0/organizations/acme/issues/4001/" && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			put = append(put, body["status"])
			mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound) // the paths without the organization
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "sntrys")
	issues, err := c.Issues(nil, StatusUnresolved, "")
	if err != nil || len(issues) != 1 || issues[0].URL != "https://acme.sentry.io/issues/4001/" || issues[0].Count != 7 {
		t.Fatalf("issues %+v %v", issues, err)
	}
	e, err := c.Latest("acme", "4001")
	if err != nil || len(e.Exceptions) != 1 || e.Exceptions[0].Value != "x is undefined" {
		t.Fatalf("latest %+v %v", e, err)
	}
	if err := c.SetStatus("acme", "4001", StatusResolved); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(put, ",") != "resolved" {
		t.Errorf("put %v", put)
	}
	if _, err := c.Latest("acme", "9999"); err == nil {
		t.Error("an issue neither path has read as found")
	}
}
