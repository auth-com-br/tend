package core

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/keyring"
	"github.com/auth-com-br/tend/internal/tickets"
	"github.com/auth-com-br/tend/internal/workon"
	"github.com/auth-com-br/tend/internal/worktree"
)

// The tickets tab: the TUI's tickets panel (cmd/tend/tui_tickets.go)
// natively — Linear, Jira, ClickUp and the Issue Gateway, through
// internal/tickets, with the accounts the settings keep. Like the errors
// tab it talks to the tracker itself, opens a ticket with its comments,
// comments on it, marks it done, and hands it to an agent: typed into the
// pane the tab was opened from, or started in a worktree of the space's
// project on ticket-<key>. An account is added in the TUI's panel
// (prefix+T), whose set-up asks per tracker for different things; the
// window reads what it saved.

// ticketsTab is what the session keeps of the tickets tab.
type ticketsTab struct {
	opened    bool
	pane      uint64
	source    string
	dir, root string
	want      string
	tokens    map[string]string
	scopes    []tickets.Scope
	scopesOf  string
	scope     int // 0 is all, else 1 + its place
	filter    int
	query     string
	seq       int
	detailSeq int
	items     []tickets.Ticket
	open      *tickets.Ticket
	detail    *tickets.Detail
	reading   bool
	busy      bool
	message   string
}

// TicketsState is the tickets tab as QML draws it.
type TicketsState struct {
	Connected bool         `json:"connected"`
	Account   string       `json:"account"`
	Accounts  []string     `json:"accounts"`
	Active    int          `json:"active"`
	Folder    string       `json:"folder"`
	Scopes    []string     `json:"scopes"`
	Scope     int          `json:"scope"`
	Filters   []string     `json:"filters"`
	Filter    int          `json:"filter"`
	Query     string       `json:"query"`
	Tickets   []TicketRow  `json:"tickets"`
	Open      *TicketShown `json:"open,omitempty"`
	Reading   bool         `json:"reading,omitempty"`
	Busy      bool         `json:"busy,omitempty"`
	Message   string       `json:"message,omitempty"`
	Agent     string       `json:"agent"`
}

// TicketRow is a ticket in the list.
type TicketRow struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Done     bool   `json:"done"`
	Assignee string `json:"assignee"`
	Scope    string `json:"scope"`
	Age      string `json:"age"`
	URL      string `json:"url"`
}

// TicketShown is a ticket open: its body as the tracker wrote it, which is
// markdown for most of them.
type TicketShown struct {
	TicketRow
	Body     string       `json:"body"`
	Comments []CommentRow `json:"comments"`
}

func ticketRow(i tickets.Ticket) TicketRow {
	return TicketRow{Key: i.Key, Title: i.Title, State: i.State, Done: i.Done, Assignee: i.Assignee, Scope: i.Scope, Age: ago(i.Updated), URL: i.URL}
}

// ticketsClientLocked is a client for the account shown, or nil.
func (s *Session) ticketsClientLocked() (tickets.Client, config.NamedTicketSource) {
	t := &s.dock.tickets
	src, _, ok := s.cfg.TicketSourceShown(t.source)
	if !ok {
		return nil, src
	}
	c, err := tickets.New(tickets.Account{Kind: tickets.Kind(src.Kind), URL: src.URL, Email: src.Email, Token: tokenOf(src.Token, t.tokens)})
	if err != nil {
		return nil, src
	}
	return c, src
}

// ticketsStateLocked is the tab now. The caller holds the lock.
func (s *Session) ticketsStateLocked() *TicketsState {
	t := &s.dock.tickets
	src, at, ok := s.cfg.TicketSourceShown(t.source)
	st := &TicketsState{
		Connected: ok, Account: src.Name, Active: at, Accounts: []string{}, Scopes: []string{},
		Scope: t.scope, Filter: t.filter, Query: t.query, Tickets: []TicketRow{},
		Reading: t.reading, Busy: t.busy, Message: t.message, Agent: s.agentForLocked(t.pane),
	}
	for _, f := range tickets.Filters {
		st.Filters = append(st.Filters, f.String())
	}
	if t.root != "" {
		st.Folder = filepath.Base(t.root)
	}
	for _, a := range s.cfg.TicketSources() {
		st.Accounts = append(st.Accounts, a.Name)
	}
	if len(t.scopes) > 0 {
		st.Scopes = append(st.Scopes, "all")
		for _, sc := range t.scopes {
			st.Scopes = append(st.Scopes, sc.Name)
		}
	}
	for _, i := range t.items {
		st.Tickets = append(st.Tickets, ticketRow(i))
	}
	if i := t.open; i != nil {
		shown := &TicketShown{TicketRow: ticketRow(*i), Comments: []CommentRow{}}
		if d := t.detail; d != nil {
			shown.Body = d.Body
			for _, c := range d.Comments {
				shown.Comments = append(shown.Comments, CommentRow{Author: c.Author, Body: c.Body, Age: ago(c.Created)})
			}
		}
		st.Open = shown
	}
	return st
}

// openTicketsOnce is the TUI's findTicketsFolder: the pane in focus's
// project folder, the account tied to it, and the keyring's tokens, read
// off the lock the first time the tab opens.
func (s *Session) openTicketsOnce() {
	s.mu.Lock()
	t := &s.dock.tickets
	if t.opened {
		s.mu.Unlock()
		return
	}
	t.opened, t.pane = true, s.focus
	pane := t.pane
	var names []string
	for _, src := range s.cfg.TicketSources() {
		if name, ok := keyring.Name(src.Token); ok {
			names = append(names, name)
		}
	}
	s.mu.Unlock()
	tokens := map[string]string{}
	var missing []string
	for _, name := range names {
		if secret, err := keyring.Get(name); err == nil {
			tokens[name] = secret
		} else {
			missing = append(missing, strings.TrimPrefix(name, "tickets:"))
		}
	}
	dir := s.paneCwd(pane)
	root := ""
	if dir != "" {
		root = workon.ProjectRoot(dir)
	}
	s.mu.Lock()
	t.tokens, t.dir, t.root = tokens, dir, root
	if src, scope, ok := s.cfg.TicketSourceFor(root, dir); ok {
		t.source, t.want = src.Name, scope
	}
	if len(missing) > 0 {
		t.message = "the keyring did not give the token for " + strings.Join(missing, ", ") + "; add the account again in tend (prefix+T)"
	}
	s.mu.Unlock()
}

// loadTickets reads the account's scopes when they are not read yet, then
// the list.
func (s *Session) loadTickets() error {
	s.openTicketsOnce()
	s.mu.Lock()
	t := &s.dock.tickets
	c, src := s.ticketsClientLocked()
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	need := t.scopesOf != src.Name
	s.mu.Unlock()
	if need {
		scopes, err := c.Scopes()
		s.mu.Lock()
		t.scopesOf, t.scopes, t.scope = src.Name, nil, 0
		if err == nil {
			t.scopes = scopes
			for i, sc := range scopes {
				if t.want != "" && sc.ID == t.want {
					t.scope = i + 1
				}
			}
		}
		t.want = ""
		s.mu.Unlock()
	}
	return s.listTickets()
}

// listTickets asks for the list as the tab says.
func (s *Session) listTickets() error {
	s.mu.Lock()
	t := &s.dock.tickets
	c, _ := s.ticketsClientLocked()
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	t.seq++
	seq := t.seq
	scope := ""
	if t.scope > 0 && t.scope <= len(t.scopes) {
		scope = t.scopes[t.scope-1].ID
	}
	filter := tickets.Filters[max(min(t.filter, len(tickets.Filters)-1), 0)]
	query := t.query
	t.busy = true
	s.mu.Unlock()
	s.stateChanged()
	items, err := c.List(scope, filter, query)
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.stateChanged()
	}()
	if t.seq != seq {
		return nil
	}
	t.busy = false
	if err != nil {
		t.items = nil
		return err
	}
	t.items = items
	return nil
}

// TicketsAction is one thing the tickets tab asks for.
type TicketsAction struct {
	Do    string `json:"do"`
	Index int    `json:"index"`
	Query string `json:"query"`
	Key   string `json:"key"`
	Body  string `json:"body"`
}

// Tickets does what the tickets tab asks.
func (s *Session) Tickets(a TicketsAction) error {
	t := &s.dock.tickets
	relist := func(change func()) error {
		s.mu.Lock()
		change()
		s.mu.Unlock()
		return s.listTickets()
	}
	switch a.Do {
	case "source":
		s.mu.Lock()
		sources := s.cfg.TicketSources()
		if a.Index < 0 || a.Index >= len(sources) {
			s.mu.Unlock()
			return nil
		}
		name := sources[a.Index].Name
		t.source, t.want, t.scope, t.items, t.open, t.detail, t.message = name, "", 0, nil, nil, nil, ""
		s.cfg.Tickets.Source = name
		s.mu.Unlock()
		if err := config.Set("tickets", "source", config.Quote(name)); err != nil {
			s.ticketsSay("showing " + name + ", but it could not be remembered: " + err.Error())
		}
		return s.loadTickets()
	case "scope":
		return relist(func() { t.scope = a.Index })
	case "filter":
		return relist(func() { t.filter = a.Index })
	case "search":
		return relist(func() { t.query = a.Query })
	case "refresh":
		if err := s.ReloadSettings(); err != nil {
			return err
		}
		s.mu.Lock()
		t.opened, t.scopesOf = false, ""
		s.mu.Unlock()
		return s.loadTickets()
	case "open":
		return s.openTicket(a.Key)
	case "back":
		s.mu.Lock()
		t.open, t.detail, t.message = nil, nil, ""
		t.detailSeq++
		s.mu.Unlock()
		s.stateChanged()
		return nil
	case "comment":
		return s.commentTicket(a.Key, a.Body)
	case "done":
		return s.markTicketDone(a.Key)
	case "fix":
		d, err := s.ticketDetail(a.Key)
		if err != nil {
			return err
		}
		s.mu.Lock()
		pane := t.pane
		s.mu.Unlock()
		return s.handToPane(pane, workon.ContextItem("ticket", d.Key+" "+d.Title, d.URL, tickets.FixPrompt(d)), d.Key)
	case "worktree":
		d, err := s.ticketDetail(a.Key)
		if err != nil {
			return err
		}
		s.mu.Lock()
		c, ws, agentName := s.client, s.shownSpaceLocked(), s.agentForLocked(t.pane)
		s.mu.Unlock()
		s.ticketsSay("making a worktree…")
		got, err := workon.Fix(s.apiCaller(), c, ws, "ticket-"+worktree.Slug(d.Key), d.Key, agentName, tickets.FixPrompt(d))
		if err != nil {
			s.ticketsSay(err.Error())
			return err
		}
		s.ticketsSay(got.Message)
		return s.goToStarted(got)
	}
	return fmt.Errorf("the tickets tab asked for %q", a.Do)
}

// ticketsSay puts a line under the tab.
func (s *Session) ticketsSay(message string) {
	s.mu.Lock()
	s.dock.tickets.message = message
	s.mu.Unlock()
	s.stateChanged()
}

// ticketByKeyLocked is a ticket of the list by its key, or the one open.
func (s *Session) ticketByKeyLocked(key string) (tickets.Ticket, bool) {
	t := &s.dock.tickets
	if t.open != nil && (key == "" || t.open.Key == key) {
		return *t.open, true
	}
	for _, i := range t.items {
		if i.Key == key {
			return i, true
		}
	}
	return tickets.Ticket{}, false
}

// openTicket reads a ticket whole.
func (s *Session) openTicket(key string) error {
	s.mu.Lock()
	t := &s.dock.tickets
	item, ok := s.ticketByKeyLocked(key)
	c, _ := s.ticketsClientLocked()
	if !ok || c == nil {
		s.mu.Unlock()
		return fmt.Errorf("no such ticket")
	}
	t.open, t.detail, t.reading = &item, nil, true
	t.detailSeq++
	seq := t.detailSeq
	s.mu.Unlock()
	s.stateChanged()
	d, err := c.Get(item.Ref)
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.stateChanged()
	}()
	if t.detailSeq != seq {
		return nil
	}
	t.reading = false
	if err != nil {
		t.message = "could not read the ticket: " + err.Error()
		return nil
	}
	d.Ref, d.Key = item.Ref, item.Key
	t.detail = &d
	return nil
}

// ticketDetail is a ticket whole, read when it has not been.
func (s *Session) ticketDetail(key string) (tickets.Detail, error) {
	s.mu.Lock()
	t := &s.dock.tickets
	item, ok := s.ticketByKeyLocked(key)
	c, _ := s.ticketsClientLocked()
	var d *tickets.Detail
	if t.detail != nil && t.detail.Key == item.Key {
		d = t.detail
	}
	s.mu.Unlock()
	if !ok || c == nil {
		return tickets.Detail{}, fmt.Errorf("no ticket chosen")
	}
	if d != nil {
		return *d, nil
	}
	got, err := c.Get(item.Ref)
	if err != nil {
		return tickets.Detail{}, err
	}
	got.Ref, got.Key = item.Ref, item.Key
	return got, nil
}

func (s *Session) commentTicket(key, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("a comment needs some text")
	}
	s.mu.Lock()
	item, ok := s.ticketByKeyLocked(key)
	c, _ := s.ticketsClientLocked()
	s.mu.Unlock()
	if !ok || c == nil {
		return fmt.Errorf("no ticket chosen")
	}
	if err := c.Comment(item.Ref, body); err != nil {
		return fmt.Errorf("the tracker said: %w", err)
	}
	// Read again to show the comment; what was said about it comes after.
	err := s.openTicket(item.Key)
	s.ticketsSay("commented on " + item.Key)
	return err
}

// markTicketDone moves a ticket to the tracker's done state.
func (s *Session) markTicketDone(key string) error {
	s.mu.Lock()
	t := &s.dock.tickets
	item, ok := s.ticketByKeyLocked(key)
	c, _ := s.ticketsClientLocked()
	s.mu.Unlock()
	if !ok || c == nil {
		return fmt.Errorf("no ticket chosen")
	}
	if err := c.Close(item.Ref); err != nil {
		return fmt.Errorf("the tracker said: %w", err)
	}
	s.mu.Lock()
	if t.open != nil && t.open.Key == item.Key {
		t.open.Done, t.open.State = true, "done"
	}
	s.mu.Unlock()
	s.ticketsSay(item.Key + " is done")
	return s.listTickets()
}
