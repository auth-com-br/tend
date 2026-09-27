package tickets

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// recorder is a stand-in tracker's log of what was written.
type recorder struct {
	mu    sync.Mutex
	wrote []string
}

func (r *recorder) add(s string) { r.mu.Lock(); r.wrote = append(r.wrote, s); r.mu.Unlock() }
func (r *recorder) all() string  { r.mu.Lock(); defer r.mu.Unlock(); return strings.Join(r.wrote, "\n") }

// TestJiraCloudIsSearchedAndChangedAsItsAPIWants: Jira Cloud's
// /search/jql is asked with the JQL of each preset and what was typed, by
// basic auth with the email; a comment is posted as text; and closing takes
// the workflow's transition into a finished status. A stand-in written from
// Atlassian's REST v2; the reading was checked against the real Apache
// Jira. If it regresses, Jira Cloud lists nothing, or closing picks a
// transition that does not finish the issue.
func TestJiraCloudIsSearchedAndChangedAsItsAPIWants(t *testing.T) {
	var rec recorder
	var jqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("me@acme.com:tok"))
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/rest/api/2/search/jql":
			jqls = append(jqls, r.URL.Query().Get("jql"))
			_, _ = io.WriteString(w, `{"issues":[{"key":"PROJ-42","fields":{"summary":"grey button","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},"assignee":{"displayName":"Ana"},"updated":"2026-09-20T12:00:00.000-0300","project":{"key":"PROJ"}}}]}`)
		case r.URL.Path == "/rest/api/2/issue/PROJ-42/transitions" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"transitions":[{"id":"11","name":"Review","to":{"statusCategory":{"key":"indeterminate"}}},{"id":"31","name":"Done","to":{"statusCategory":{"key":"done"}}}]}`)
		case r.Method == "POST":
			rec.add(r.URL.Path + " " + string(body))
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/rest/api/2/myself":
			_, _ = io.WriteString(w, `{"displayName":"Akira"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, _ := New(Account{Kind: Jira, URL: srv.URL + "/", Email: "me@acme.com", Token: "tok"})
	if who, err := c.WhoAmI(); err != nil || who != "Akira" {
		t.Fatalf("whoami %q %v", who, err)
	}
	list, err := c.List("PROJ", FilterMine, `say "hi"`)
	if err != nil || len(list) != 1 || list[0].Key != "PROJ-42" || list[0].Assignee != "Ana" || list[0].Done ||
		list[0].URL != srv.URL+"/browse/PROJ-42" || list[0].Updated.UTC().Hour() != 15 {
		t.Fatalf("list %+v %v", list, err)
	}
	if want := `project = "PROJ" AND assignee = currentUser() AND statusCategory != Done AND text ~ "say \"hi\"" ORDER BY updated DESC`; jqls[0] != want {
		t.Errorf("jql %s", jqls[0])
	}
	if err := c.Comment("PROJ-42", "on it"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close("PROJ-42"); err != nil {
		t.Fatal(err)
	}
	got := rec.all()
	if !strings.Contains(got, `/rest/api/2/issue/PROJ-42/comment {"body":"on it"}`) || !strings.Contains(got, `/rest/api/2/issue/PROJ-42/transitions {"transition":{"id":"31"}}`) {
		t.Errorf("wrote:\n%s", got)
	}
	bad, _ := New(Account{Kind: Jira, URL: srv.URL, Email: "me@acme.com", Token: "wrong"})
	if _, err := bad.WhoAmI(); !errors.Is(err, ErrBadToken) {
		t.Errorf("a bad token: %v", err)
	}
}

// TestLinearIsAskedThroughGraphQL: the key goes as the Authorization header
// itself; each preset is Linear's filter — a team, the state types, isMe,
// a title — and closing moves the issue to its team's completed state,
// by the issue's id, read from its identifier. A stand-in written from
// Linear's GraphQL documentation; no real Linear was asked. If it
// regresses, Linear refuses every call, or "closed" lists the open.
func TestLinearIsAskedThroughGraphQL(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "lin_api_x" {
			_, _ = io.WriteString(w, `{"errors":[{"message":"Authentication required, not authenticated","extensions":{"type":"authentication error"}}]}`)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		q := req.Query
		switch {
		case strings.Contains(q, "viewer"):
			_, _ = io.WriteString(w, `{"data":{"viewer":{"name":"Akira"}}}`)
		case strings.Contains(q, "issues("):
			f, _ := json.Marshal(req.Variables["f"])
			rec.add("filter " + string(f))
			_, _ = io.WriteString(w, `{"data":{"issues":{"nodes":[{"id":"u1","identifier":"ENG-12","title":"slow login","url":"https://linear.app/acme/issue/ENG-12","updatedAt":"2026-09-20T12:00:00Z","state":{"name":"Todo","type":"unstarted"},"team":{"key":"ENG"}}]}}}`)
		case strings.Contains(q, "states("):
			_, _ = io.WriteString(w, `{"data":{"issue":{"id":"u1","team":{"states":{"nodes":[{"id":"done-1"}]}}}}}`)
		case strings.Contains(q, "comments("):
			_, _ = io.WriteString(w, `{"data":{"issue":{"id":"u1","identifier":"ENG-12","title":"slow login","description":"takes 9s","state":{"name":"Todo","type":"unstarted"},"team":{"key":"ENG"},"comments":{"nodes":[{"body":"second","createdAt":"2026-09-22T00:00:00Z","user":{"name":"Bia"}},{"body":"first","createdAt":"2026-09-21T00:00:00Z","user":{"name":"Ana"}}]}}}}`)
		case strings.Contains(q, "mutation"):
			v, _ := json.Marshal(req.Variables)
			rec.add(strings.Fields(strings.SplitN(q, "{", 3)[1])[0] + " " + string(v))
			_, _ = io.WriteString(w, `{"data":{"commentCreate":{"success":true},"issueUpdate":{"success":true}}}`)
		case strings.Contains(q, "issue(id"):
			_, _ = io.WriteString(w, `{"data":{"issue":{"id":"u1"}}}`)
		}
	}))
	defer srv.Close()
	c, _ := New(Account{Kind: Linear, URL: srv.URL, Token: "lin_api_x"})
	if who, err := c.WhoAmI(); err != nil || who != "Akira" {
		t.Fatalf("whoami %q %v", who, err)
	}
	list, err := c.List("team-1", FilterMine, "login")
	if err != nil || len(list) != 1 || list[0].Key != "ENG-12" || list[0].Done {
		t.Fatalf("list %+v %v", list, err)
	}
	if _, err := c.List("", FilterClosed, ""); err != nil {
		t.Fatal(err)
	}
	d, err := c.Get("ENG-12")
	if err != nil || d.Body != "takes 9s" || len(d.Comments) != 2 || d.Comments[0].Body != "first" {
		t.Errorf("detail %+v %v", d, err)
	}
	if err := c.Comment("ENG-12", "on it"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close("ENG-12"); err != nil {
		t.Fatal(err)
	}
	got := rec.all()
	for _, want := range []string{
		`filter {"assignee":{"isMe":{"eq":true}},"state":{"type":{"nin":["completed","canceled"]}},"team":{"id":{"eq":"team-1"}},"title":{"containsIgnoreCase":"login"}}`,
		`filter {"state":{"type":{"in":["completed","canceled"]}}}`,
		`commentCreate(input: {"body":"on it","id":"u1"}`,
		`issueUpdate(id: {"id":"u1","state":"done-1"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	bad, _ := New(Account{Kind: Linear, URL: srv.URL, Token: "nope"})
	if _, err := bad.WhoAmI(); !errors.Is(err, ErrBadToken) {
		t.Errorf("a bad key: %v", err)
	}
}

// TestClickUpTasksAreListedAcrossAWorkspace: the token goes as the
// Authorization header itself; a workspace's tasks are listed, the user's
// by their id, what was typed filtering the names, the closed kept to the
// finished; a task is read with its comments oldest first; and closing sets
// the list's closed status, whatever it is called. A stand-in written from
// ClickUp's API reference; no real ClickUp was asked. If it regresses,
// "mine" lists everyone's, or closing sets a status the list has not got.
func TestClickUpTasksAreListedAcrossAWorkspace(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "pk_1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/user":
			_, _ = io.WriteString(w, `{"user":{"id":77,"username":"akira"}}`)
		case r.URL.Path == "/team":
			_, _ = io.WriteString(w, `{"teams":[{"id":"900","name":"Acme"}]}`)
		case r.URL.Path == "/team/900/task":
			rec.add("list " + r.URL.RawQuery)
			_, _ = io.WriteString(w, `{"tasks":[
				{"id":"86a","custom_id":"ACME-7","name":"Fix invoice","status":{"status":"in progress","type":"custom"},"assignees":[{"username":"akira"}],"date_updated":"1758369600000","url":"https://app.clickup.com/t/86a"},
				{"id":"86b","name":"Old thing","status":{"status":"complete","type":"closed"},"date_updated":"1758283200000"}]}`)
		case r.URL.Path == "/task/86a" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"id":"86a","custom_id":"ACME-7","name":"Fix invoice","text_content":"the total is wrong","status":{"status":"in progress","type":"custom"},"list":{"id":"L1"}}`)
		case r.URL.Path == "/task/86a/comment" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"comments":[{"comment_text":"newer","user":{"username":"bia"},"date":"1758369600000"},{"comment_text":"older","user":{"username":"ana"},"date":"1758283200000"}]}`)
		case r.URL.Path == "/list/L1":
			_, _ = io.WriteString(w, `{"statuses":[{"status":"to do","type":"open"},{"status":"shipped","type":"closed"}]}`)
		case r.Method != "GET":
			rec.add(r.Method + " " + r.URL.Path + " " + string(body))
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c, _ := New(Account{Kind: ClickUp, URL: srv.URL, Token: "pk_1"})
	mine, err := c.List("", FilterMine, "invoice")
	if err != nil || len(mine) != 1 || mine[0].Key != "ACME-7" || mine[0].Ref != "86a" || mine[0].Scope != "Acme" {
		t.Fatalf("mine %+v %v", mine, err)
	}
	closed, _ := c.List("900", FilterClosed, "")
	if len(closed) != 1 || closed[0].Ref != "86b" || !closed[0].Done {
		t.Errorf("closed %+v", closed)
	}
	d, err := c.Get("86a")
	if err != nil || d.Body != "the total is wrong" || len(d.Comments) != 2 || d.Comments[0].Body != "older" {
		t.Errorf("detail %+v %v", d, err)
	}
	if err := c.Comment("86a", "on it"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close("86a"); err != nil {
		t.Fatal(err)
	}
	got := rec.all()
	for _, want := range []string{"assignees%5B%5D=77", "include_closed=true", `POST /task/86a/comment {"comment_text":"on it","notify_all":false}`, `PUT /task/86a {"status":"shipped"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}

// TestTheIssueGatewayIsAskedAsItsContractSays: the token goes as a bearer
// under /v1 of the account's address; a list passes the scope, the
// preset's name and what was typed, and takes the order and a null
// assignee or address as they come; a ticket is read with its comments
// oldest first; a comment and closing are posted to the issue's ref; a 401
// is a refused token, and an error's body is said by its message. A
// stand-in written from the gateway's contract (issue-gateway#27); no real
// gateway was asked. If it regresses, the gateway refuses every call, the
// presets list the same tickets, or a failure reads as raw JSON.
func TestTheIssueGatewayIsAskedAsItsContractSays(t *testing.T) {
	var rec recorder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ig_1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/v1/me":
			_, _ = io.WriteString(w, `{"name":"akira"}`)
		case r.URL.Path == "/v1/scopes":
			_, _ = io.WriteString(w, `{"items":[{"id":"s-1","name":"Cliente XYZ / Sistema de Recarga"}]}`)
		case r.URL.Path == "/v1/issues":
			rec.add("list " + r.URL.RawQuery)
			_, _ = io.WriteString(w, `{"items":[
				{"key":"IG-12","ref":"u-12","title":"recarga falha","state":"published","done":false,"assignee":"akira","scope":"Sistema de Recarga","updated":"2026-09-27T12:00:00Z","url":"https://github.com/acme/app/issues/3"},
				{"key":"IG-9","ref":"u-9","title":"old","state":"closed","done":true,"assignee":null,"scope":"Sistema de Recarga","updated":"2026-09-20T12:00:00Z","url":null}]}`)
		case r.URL.Path == "/v1/issues/u-12" && r.Method == "GET":
			_, _ = io.WriteString(w, `{"key":"IG-12","ref":"u-12","title":"recarga falha","state":"published","done":false,"assignee":"akira","scope":"Sistema de Recarga","updated":"2026-09-27T12:00:00Z","url":null,
				"body":"**falha** no pix","comments":[{"author":"bia","body":"newer","created":"2026-09-26T00:00:00Z"},{"author":"ana","body":"older","created":"2026-09-25T00:00:00Z"}]}`)
		case r.URL.Path == "/v1/issues/u-12/comments" && r.Method == "POST":
			rec.add("comment " + string(body))
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/v1/issues/u-12/close" && r.Method == "POST":
			rec.add("close " + string(body))
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"no issue u-404"}}`)
		}
	}))
	defer srv.Close()
	c, _ := New(Account{Kind: IssueGateway, URL: srv.URL + "/", Token: "ig_1"})
	if who, err := c.WhoAmI(); err != nil || who != "akira" {
		t.Fatalf("whoami %q %v", who, err)
	}
	scopes, err := c.Scopes()
	if err != nil || len(scopes) != 1 || scopes[0].ID != "s-1" || scopes[0].Name != "Cliente XYZ / Sistema de Recarga" {
		t.Fatalf("scopes %+v %v", scopes, err)
	}
	list, err := c.List("s-1", FilterMine, " recarga ")
	if err != nil || len(list) != 2 || list[0].Key != "IG-12" || list[0].Ref != "u-12" || list[0].Assignee != "akira" ||
		list[0].Done || list[0].URL != "https://github.com/acme/app/issues/3" || list[0].Updated.Day() != 27 ||
		!list[1].Done || list[1].Assignee != "" || list[1].URL != "" {
		t.Fatalf("list %+v %v", list, err)
	}
	if _, err := c.List("", FilterClosed, ""); err != nil {
		t.Fatal(err)
	}
	d, err := c.Get("u-12")
	if err != nil || d.Body != "**falha** no pix" || d.Key != "IG-12" || len(d.Comments) != 2 ||
		d.Comments[0].Body != "older" || d.Comments[0].Author != "ana" {
		t.Errorf("detail %+v %v", d, err)
	}
	if err := c.Comment("u-12", "on it"); err != nil {
		t.Fatal(err)
	}
	if err := c.Close("u-12"); err != nil {
		t.Fatal(err)
	}
	got := rec.all()
	for _, want := range []string{"list filter=mine&q=recarga&scope=s-1", "list filter=closed&q=&scope=", `comment {"body":"on it"}`, "close "} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	if _, err := c.Get("u-404"); err == nil || !strings.Contains(err.Error(), "no issue u-404") || strings.Contains(err.Error(), "not_found") {
		t.Errorf("an error's body: %v", err)
	}
	bad, _ := New(Account{Kind: IssueGateway, URL: srv.URL, Token: "nope"})
	if _, err := bad.WhoAmI(); !errors.Is(err, ErrBadToken) {
		t.Errorf("a bad token: %v", err)
	}
	none, _ := New(Account{Kind: IssueGateway, URL: srv.URL})
	if _, err := none.List("", FilterOpen, ""); !errors.Is(err, ErrNoToken) {
		t.Errorf("no token: %v", err)
	}
}

// TestAGatewayAccountIsATokenAlone: an Issue Gateway account without an
// address talks to Auth's gateway, or to TEND_ISSUE_GATEWAY_URL when set. If
// it regresses, connecting asks for an address people do not know.
func TestAGatewayAccountIsATokenAlone(t *testing.T) {
	t.Setenv("TEND_ISSUE_GATEWAY_URL", "")
	c, err := New(Account{Kind: IssueGateway, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.(*issueGateway).base(); got != DefaultIssueGatewayURL+"/v1" {
		t.Errorf("base = %q", got)
	}
	t.Setenv("TEND_ISSUE_GATEWAY_URL", "http://staging:8797")
	c, _ = New(Account{Kind: IssueGateway, Token: "t"})
	if got := c.(*issueGateway).base(); got != "http://staging:8797/v1" {
		t.Errorf("base with the override = %q", got)
	}
}

// TestAFixPromptCarriesTheTicket: what an agent is told has the ticket's
// key, title, address, state, text and comments. If it regresses, the
// agent is handed a title and has to ask for the rest.
func TestAFixPromptCarriesTheTicket(t *testing.T) {
	p := FixPrompt(Detail{Ticket: Ticket{Key: "ENG-12", Title: "slow login", URL: "https://x/ENG-12", State: "Todo"},
		Body: "takes 9s", Comments: []Comment{{Author: "Ana", Body: "seen\nit"}}})
	for _, want := range []string{"(ENG-12): slow login", "https://x/ENG-12", "State: Todo", "takes 9s", "- Ana: seen it"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q:\n%s", want, p)
		}
	}
}
