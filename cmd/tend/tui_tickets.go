package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/keyring"
	"github.com/auth-com-br/tend/internal/tickets"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/worktree"
)

// The tickets panel (internal/ui/tickets.go, internal/tickets): Linear, Jira,
// ClickUp and the Issue Gateway, as the errors panel is GlitchTip. The client talks to each
// tracker itself, with the accounts kept in its settings — tokens in the
// system keyring when there is one — and does with a ticket what it does
// with an error: types it into the pane the panel was opened from, starts
// an agent on it in a worktree, or marks it done in the tracker. A project
// folder is tied to an account and a scope, and opens on them.

type ticketsState struct {
	pane      uint64
	seq       int
	detailSeq int
	scopes    []tickets.Scope
	items     []tickets.Ticket
	detail    *tickets.Detail
	// source is the account shown while the panel is up when it is not the
	// one remembered: the one the project folder is tied to.
	source    string
	dir, root string
	want      string
	tokens    map[string]string
}

func (t *tui) ticketsUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tickets != nil
}

// ticketSourceLocked is the account shown, and its place among them.
func (t *tui) ticketSourceLocked() (config.NamedTicketSource, int, bool) {
	sources := t.config.TicketSources()
	for _, name := range []string{t.ticketState.source, t.config.Tickets.Source} {
		for i, s := range sources {
			if name != "" && s.Name == name {
				return s, i, true
			}
		}
	}
	if len(sources) > 0 {
		return sources[0], 0, true
	}
	return config.NamedTicketSource{}, 0, false
}

// ticketClientLocked is a client for the account shown, or nil.
func (t *tui) ticketClientLocked() tickets.Client {
	s, _, ok := t.ticketSourceLocked()
	if !ok {
		return nil
	}
	token := s.Token
	if name, isRef := keyring.Name(token); isRef {
		token = t.ticketState.tokens[name]
	}
	c, err := tickets.New(tickets.Account{Kind: tickets.Kind(s.Kind), URL: s.URL, Email: s.Email, Token: token})
	if err != nil {
		return nil
	}
	return c
}

// showTicketSourceLocked says in the view which accounts there are and which
// one is shown.
func (t *tui) showTicketSourceLocked() {
	v := t.tickets
	if v == nil {
		return
	}
	s, at, ok := t.ticketSourceLocked()
	v.Connected, v.Account, v.Active = ok, s.Name, at
	v.Accounts = v.Accounts[:0]
	for _, src := range t.config.TicketSources() {
		v.Accounts = append(v.Accounts, src.Name)
	}
	if m := v.Manage; m != nil {
		m.Servers, m.Active = append([]string(nil), v.Accounts...), at
		m.Cursor = max(min(m.Cursor, len(m.Servers)-1), 0)
	}
	t.showTicketLinkLocked()
}

func (t *tui) shownTicketScopeLocked() string {
	v := t.tickets
	if v == nil || v.Scope <= 0 || v.Scope > len(t.ticketState.scopes) {
		return ""
	}
	return t.ticketState.scopes[v.Scope-1].ID
}

func (t *tui) showTicketLinkLocked() {
	v := t.tickets
	if v == nil {
		return
	}
	v.Folder, v.Linked = "", false
	if t.ticketState.root == "" {
		return
	}
	v.Folder = filepath.Base(t.ticketState.root)
	if s, _, ok := t.ticketSourceLocked(); ok {
		scope, tied := s.Projects[t.ticketState.root]
		v.Linked = tied && scope == t.shownTicketScopeLocked()
	}
}

// openTickets puts the panel up; the list waits for the project folder to
// be known, so the first one shown is the account tied to it.
func (t *tui) openTickets() error {
	t.mu.Lock()
	fresh := t.tickets == nil
	if fresh {
		names := make([]string, 0, len(tickets.Filters))
		for _, f := range tickets.Filters {
			names = append(names, f.String())
		}
		t.tickets = &ui.TicketsView{Filters: names, Now: time.Now()}
		t.ticketState = ticketsState{pane: t.focus}
	}
	v := t.tickets
	t.showTicketSourceLocked()
	v.Loading = v.Connected
	session, pane := t.session, t.ticketState.pane
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if fresh {
		go t.findTicketsFolder(session, pane)
	}
	return nil
}

func (t *tui) closeTickets() {
	t.mu.Lock()
	t.tickets = nil
	t.ticketState.seq++
	t.ticketState.detailSeq++
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// ticketsSay puts a line under the panel.
func (t *tui) ticketsSay(message string) {
	t.mu.Lock()
	if t.tickets != nil {
		t.tickets.Message = message
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// readTicketTokens reads the tokens kept in the keyring, off the lock.
func (t *tui) readTicketTokens() {
	t.mu.Lock()
	var names []string
	for _, s := range t.config.TicketSources() {
		if name, ok := keyring.Name(s.Token); ok {
			if _, have := t.ticketState.tokens[name]; !have {
				names = append(names, name)
			}
		}
	}
	t.mu.Unlock()
	for _, name := range names {
		secret, err := keyring.Get(name)
		t.mu.Lock()
		if t.ticketState.tokens == nil {
			t.ticketState.tokens = map[string]string{}
		}
		if err == nil {
			t.ticketState.tokens[name] = secret
		}
		t.mu.Unlock()
		if err != nil {
			t.ticketsSay("the keyring did not give the token for " + strings.TrimPrefix(name, "tickets:") + "; add the account again")
		}
	}
}

func (t *tui) findTicketsFolder(session string, pane uint64) {
	t.readTicketTokens()
	dir, root := t.projectFolder(session, pane)
	t.mu.Lock()
	v := t.tickets
	if v == nil {
		t.mu.Unlock()
		return
	}
	t.ticketState.dir, t.ticketState.root = dir, root
	if src, scope, ok := t.config.TicketSourceFor(root, dir); ok {
		t.ticketState.source, t.ticketState.want = src.Name, scope
	}
	t.showTicketSourceLocked()
	connected, want := v.Connected, t.ticketState.want
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if !connected {
		return
	}
	go t.loadTicketScopes()
	if want == "" {
		t.reloadTickets()
	}
}

// loadTicketScopes reads the account's scopes, for the chips, and chooses
// the one the folder is tied to.
func (t *tui) loadTicketScopes() {
	t.mu.Lock()
	c := t.ticketClientLocked()
	asked, _, _ := t.ticketSourceLocked()
	t.mu.Unlock()
	if c == nil {
		return
	}
	scopes, err := c.Scopes()
	t.mu.Lock()
	v := t.tickets
	shown, _, _ := t.ticketSourceLocked()
	if v == nil || shown.Name != asked.Name {
		t.mu.Unlock()
		return
	}
	want := t.ticketState.want
	t.ticketState.want = ""
	if err == nil {
		t.ticketState.scopes = scopes
		v.Scopes = []string{"all"}
		for i, s := range scopes {
			v.Scopes = append(v.Scopes, s.Name)
			if want != "" && s.ID == want {
				v.Scope = i + 1
			}
		}
	}
	t.showTicketLinkLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if want != "" {
		t.reloadTickets()
	}
}

// reloadTickets asks for the list as the view says.
func (t *tui) reloadTickets() {
	t.mu.Lock()
	v := t.tickets
	c := t.ticketClientLocked()
	if v == nil || c == nil {
		t.mu.Unlock()
		return
	}
	t.ticketState.seq++
	seq := t.ticketState.seq
	scope := t.shownTicketScopeLocked()
	filter := tickets.Filters[max(min(v.Filter, len(tickets.Filters)-1), 0)]
	query := v.Query
	v.Loading = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		items, err := c.List(scope, filter, query)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.tickets
		if v == nil || seq != t.ticketState.seq {
			return
		}
		v.Loading, v.Now = false, time.Now()
		if err != nil {
			v.Error, v.Tickets, t.ticketState.items = err.Error(), nil, nil
			return
		}
		v.Error = ""
		var at string
		if v.Cursor < len(v.Tickets) {
			at = v.Tickets[v.Cursor].Key
		}
		t.ticketState.items = items
		v.Tickets, v.Cursor = v.Tickets[:0], 0
		for n, i := range items {
			v.Tickets = append(v.Tickets, ticketEntry(i))
			if i.Key == at {
				v.Cursor = n
			}
		}
		v.Scroll = ui.TicketsScrollFor(v, t.cols, t.rows)
	}()
}

func ticketEntry(i tickets.Ticket) ui.TicketEntry {
	return ui.TicketEntry{Key: i.Key, Title: i.Title, State: i.State, Assignee: i.Assignee, Scope: i.Scope, Done: i.Done, Updated: i.Updated}
}

// currentTicketLocked is the ticket the panel is on: the one open, else the
// one under the cursor.
func (t *tui) currentTicketLocked() (tickets.Ticket, bool) {
	v := t.tickets
	if v == nil {
		return tickets.Ticket{}, false
	}
	key := ""
	switch {
	case v.Detail != nil:
		key = v.Detail.Key
	case v.Cursor < len(v.Tickets):
		key = v.Tickets[v.Cursor].Key
	}
	for _, i := range t.ticketState.items {
		if i.Key == key {
			return i, true
		}
	}
	return tickets.Ticket{}, false
}

// openTicket reads the ticket under the cursor whole.
func (t *tui) openTicket() {
	t.mu.Lock()
	v := t.tickets
	item, ok := t.currentTicketLocked()
	c := t.ticketClientLocked()
	if v == nil || !ok || c == nil {
		t.mu.Unlock()
		return
	}
	v.Detail = &ui.TicketDetailView{TicketEntry: ticketEntry(item), URL: item.URL, Loading: true}
	v.Message = item.URL
	t.ticketState.detail = nil
	t.ticketState.detailSeq++
	seq := t.ticketState.detailSeq
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		d, err := c.Get(item.Ref)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.tickets
		if v == nil || v.Detail == nil || seq != t.ticketState.detailSeq {
			return
		}
		v.Detail.Loading = false
		if err != nil {
			v.Message = "could not read the ticket: " + err.Error()
			return
		}
		d.Ref, d.Key = item.Ref, item.Key
		t.ticketState.detail = &d
		v.Detail.Body = d.Body
		v.Detail.Comments = v.Detail.Comments[:0]
		for _, c := range d.Comments {
			v.Detail.Comments = append(v.Detail.Comments, ui.TicketComment{Author: c.Author, Body: c.Body, Created: c.Created})
		}
	}()
}

// ticketDetail is the current ticket whole, read when it has not been.
func (t *tui) ticketDetail() (tickets.Detail, error) {
	t.mu.Lock()
	item, ok := t.currentTicketLocked()
	c := t.ticketClientLocked()
	var d *tickets.Detail
	if t.tickets != nil && t.tickets.Detail != nil {
		d = t.ticketState.detail
	}
	t.mu.Unlock()
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

// fixTicket types the ticket into the pane the panel was opened from,
// through the context, submitting nothing.
func (t *tui) fixTicket() {
	t.mu.Lock()
	pane := t.ticketState.pane
	t.mu.Unlock()
	t.ticketsSay("reading the ticket…")
	go func() {
		d, err := t.ticketDetail()
		if err != nil {
			t.ticketsSay(err.Error())
			return
		}
		item, err := t.client.ContextAdd(errorItemText("ticket", d.Key+" "+d.Title, d.URL, tickets.FixPrompt(d)))
		if err == nil {
			err = t.client.ContextSend(pane, []uint64{item.ID})
		}
		if err != nil {
			t.ticketsSay("could not hand it over: " + err.Error())
			return
		}
		t.closeTickets()
		_ = t.jumpToPane(pane)
		t.setMessage(d.Key+" typed into the pane — read it over and press enter to send", false)
	}()
}

// fixTicketInWorktree starts the agent on the ticket in a worktree of the
// space's project, on ticket-<key>.
func (t *tui) fixTicketInWorktree() {
	ws := t.shownWorkspace()
	t.mu.Lock()
	session := t.session
	agentName := t.agentForLocked(t.ticketState.pane)
	t.mu.Unlock()
	t.ticketsSay("making a worktree…")
	go func() {
		d, err := t.ticketDetail()
		if err != nil {
			t.ticketsSay(err.Error())
			return
		}
		branch := "ticket-" + worktree.Slug(d.Key)
		wsID, tab, message, err := t.errorWorktree(session, ws, branch, d.Key, agentName, tickets.FixPrompt(d))
		if err != nil {
			t.ticketsSay(err.Error())
			return
		}
		t.closeTickets()
		t.mu.Lock()
		t.rememberFocusLocked()
		t.workspace, t.tab, t.focus, t.zoom = wsID, tab, 0, false
		t.mu.Unlock()
		t.setMessage(message, false)
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}()
}

func (t *tui) markTicketDone() {
	t.mu.Lock()
	item, ok := t.currentTicketLocked()
	c := t.ticketClientLocked()
	t.mu.Unlock()
	if !ok || c == nil {
		return
	}
	t.ticketsSay("asking the tracker…")
	go func() {
		if err := c.Close(item.Ref); err != nil {
			t.ticketsSay("the tracker said: " + err.Error())
			return
		}
		t.mu.Lock()
		if v := t.tickets; v != nil && v.Detail != nil {
			v.Detail.Done = true
			v.Detail.State = "done"
		}
		t.mu.Unlock()
		t.ticketsSay(item.Key + " is done")
		t.reloadTickets()
	}()
}

func (t *tui) sendTicketComment() {
	t.mu.Lock()
	v := t.tickets
	item, ok := t.currentTicketLocked()
	c := t.ticketClientLocked()
	if v == nil || v.Input == nil || !ok || c == nil {
		t.mu.Unlock()
		return
	}
	text := strings.TrimSpace(v.Input.Text)
	if text == "" {
		v.Input = nil
		t.dirty = true
		t.mu.Unlock()
		return
	}
	v.Input.Sending = true
	t.dirty = true
	t.mu.Unlock()
	go func() {
		err := c.Comment(item.Ref, text)
		t.mu.Lock()
		if v := t.tickets; v != nil {
			if err != nil {
				if v.Input != nil {
					v.Input.Sending = false // what was written stays, to send again
				}
				v.Message = "the tracker said: " + err.Error()
			} else {
				v.Input = nil
				v.Message = "commented on " + item.Key
			}
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		if err == nil {
			t.mu.Lock()
			open := t.tickets != nil && t.tickets.Detail != nil
			t.mu.Unlock()
			if open {
				// Read again to show the comment; what was said about it
				// comes after, or the ticket's address would cover it.
				t.openTicket()
				t.ticketsSay("commented on " + item.Key)
			}
		}
	}()
}

func (t *tui) openTicketInBrowser() {
	t.mu.Lock()
	item, ok := t.currentTicketLocked()
	t.mu.Unlock()
	if !ok || item.URL == "" {
		return
	}
	message := "opened " + item.URL
	if err := openURL(item.URL); err != nil {
		message = item.URL + " — " + err.Error()
	}
	t.ticketsSay(message)
}

// useTicketSource shows another account, and remembers it.
func (t *tui) useTicketSource(i int) {
	t.mu.Lock()
	sources := t.config.TicketSources()
	if t.tickets == nil || i < 0 || i >= len(sources) {
		t.mu.Unlock()
		return
	}
	name := sources[i].Name
	t.config.Tickets.Source, t.ticketState.source, t.ticketState.want = name, name, ""
	v := t.tickets
	v.Scopes, v.Scope, v.Tickets, v.Cursor, v.Scroll, v.Error = nil, 0, nil, 0, 0, ""
	v.Detail, v.Message = nil, ""
	t.ticketState.scopes, t.ticketState.items, t.ticketState.detail = nil, nil, nil
	t.showTicketSourceLocked()
	t.dirty = true
	t.mu.Unlock()
	go func() {
		if err := config.Set("tickets", "source", config.Quote(name)); err != nil {
			t.ticketsSay("showing " + name + ", but it could not be remembered: " + err.Error())
		}
	}()
	go t.loadTicketScopes()
	t.reloadTickets()
}

// toggleTicketLink ties the project folder to the account and scope shown,
// or unties it; a folder is tied to one account.
func (t *tui) toggleTicketLink() {
	t.mu.Lock()
	root := t.ticketState.root
	src, _, ok := t.ticketSourceLocked()
	scope := t.shownTicketScopeLocked()
	if root == "" || !ok {
		t.mu.Unlock()
		t.ticketsSay("no project folder is known for this pane")
		return
	}
	p, tied := src.Projects[root]
	untie := tied && p == scope
	type write struct {
		name  string
		table map[string]string
	}
	var writes []write
	for _, s := range t.config.TicketSources() {
		table := map[string]string{}
		for k, v := range s.Projects {
			table[k] = v
		}
		_, had := table[root]
		switch {
		case s.Name == src.Name && untie:
			delete(table, root)
		case s.Name == src.Name:
			table[root] = scope
		case had:
			delete(table, root)
		default:
			continue
		}
		writes = append(writes, write{s.Name, table})
	}
	t.mu.Unlock()
	go func() {
		for _, w := range writes {
			if err := config.Set("tickets.sources."+w.name, "projects", config.InlineTable(w.table)); err != nil {
				t.ticketsSay("could not tie the folder: " + err.Error())
				return
			}
			t.mu.Lock()
			if s, ok := t.config.Tickets.Sources[w.name]; ok {
				s.Projects = w.table
				t.config.Tickets.Sources[w.name] = s
			}
			t.mu.Unlock()
		}
		message := filepath.Base(root) + " now opens on " + src.Name
		if untie {
			message = filepath.Base(root) + " no longer opens on " + src.Name
		}
		t.mu.Lock()
		t.showTicketLinkLocked()
		t.dirty = true
		t.mu.Unlock()
		t.ticketsSay(message)
	}()
}

// --- accounts ----------------------------------------------------------------

func (t *tui) openTicketsConnect() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tickets == nil {
		return
	}
	kinds := make([]string, 0, len(tickets.Kinds))
	for _, k := range tickets.Kinds {
		kinds = append(kinds, string(k))
	}
	t.tickets.Connect = &ui.TicketsConnect{Kinds: kinds, Field: 3, URL: "https://"}
	t.dirty = true
}

func (t *tui) openTicketsManage() {
	t.mu.Lock()
	v := t.tickets
	if v == nil {
		t.mu.Unlock()
		return
	}
	if len(t.config.TicketSources()) == 0 {
		t.mu.Unlock()
		t.openTicketsConnect()
		return
	}
	_, at, _ := t.ticketSourceLocked()
	v.Manage = &ui.ErrorsManage{Title: "tracker accounts", Subtitle: "the accounts kept; the panel shows one at a time", Cursor: at}
	t.showTicketSourceLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) ticketsManageKey(key string) {
	t.mu.Lock()
	v := t.tickets
	if v == nil || v.Manage == nil {
		t.mu.Unlock()
		return
	}
	m := v.Manage
	t.dirty = true
	if m.Confirm {
		m.Confirm = false
		if key == "\r" || key == "\n" {
			sources := t.config.TicketSources()
			if m.Cursor < len(sources) {
				src := sources[m.Cursor]
				t.mu.Unlock()
				t.removeTicketSource(src)
				return
			}
		}
		t.mu.Unlock()
		return
	}
	switch key {
	case "\x1b", "q":
		v.Manage = nil
	case "\x1b[A", "k", "\x10":
		m.Cursor = max(m.Cursor-1, 0)
	case "\x1b[B", "j", "\x0e":
		m.Cursor = max(min(m.Cursor+1, len(m.Servers)-1), 0)
	case "\r", "\n":
		at := m.Cursor
		v.Manage = nil
		t.mu.Unlock()
		t.useTicketSource(at)
		return
	case "a":
		v.Manage = nil
		t.mu.Unlock()
		t.openTicketsConnect()
		return
	case "d", "\x1b[3~":
		if len(m.Servers) > 0 {
			m.Confirm, m.Message = true, ""
		}
	}
	t.mu.Unlock()
	t.wakeUp()
}

func (t *tui) removeTicketSource(src config.NamedTicketSource) {
	go func() {
		if name, ok := keyring.Name(src.Token); ok {
			_ = keyring.Delete(name)
		}
		err := config.RemoveSection("tickets.sources." + src.Name)
		t.mu.Lock()
		v := t.tickets
		if err != nil {
			if v != nil && v.Manage != nil {
				v.Manage.Message = "could not remove it: " + err.Error()
			}
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		delete(t.config.Tickets.Sources, src.Name)
		shown := t.config.Tickets.Source == src.Name || t.ticketState.source == src.Name
		if v != nil {
			if len(t.config.TicketSources()) == 0 {
				v.Manage = nil
			} else if v.Manage != nil {
				v.Manage.Message = "removed " + src.Name
			}
		}
		if shown {
			t.config.Tickets.Source, t.ticketState.source = "", ""
		}
		t.mu.Unlock()
		if shown {
			t.useTicketSource(0)
		}
		t.mu.Lock()
		if v := t.tickets; v != nil {
			t.showTicketSourceLocked()
			if !v.Connected {
				v.Tickets, v.Scopes, v.Message = nil, nil, "removed "+src.Name+"; no account is kept"
			}
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
}

// ticketsConnectKey is a key in the connect box.
func (t *tui) ticketsConnectKey(key string) {
	t.mu.Lock()
	v := t.tickets
	if v == nil || v.Connect == nil {
		t.mu.Unlock()
		return
	}
	c := v.Connect
	if c.Testing {
		t.mu.Unlock()
		return
	}
	kind := ""
	if c.Kind < len(c.Kinds) {
		kind = c.Kinds[c.Kind]
	}
	// The fields a tracker asks for: the site and the email are Jira's, and
	// the site the Issue Gateway's too, which is wherever it is hosted.
	fields := []int{0, 3}
	switch tickets.Kind(kind) {
	case tickets.Jira:
		fields = []int{0, 1, 2, 3}
	case tickets.IssueGateway:
		fields = []int{0, 1, 3}
	}
	step := func(by int) {
		at := 0
		for i, f := range fields {
			if f == c.Field {
				at = i
			}
		}
		c.Field = fields[(at+by+len(fields))%len(fields)]
	}
	var field *string
	switch c.Field {
	case 1:
		field = &c.URL
	case 2:
		field = &c.Email
	case 3:
		field = &c.Token
	}
	save := false
	switch key {
	case "\x1b":
		v.Connect = nil
	case "\t", "\x1b[B":
		step(1)
	case "\x1b[Z", "\x1b[A":
		step(-1)
	case "\x1b[C", "\x1b[D":
		if c.Field == 0 {
			by := 1
			if key == "\x1b[D" {
				by = len(c.Kinds) - 1
			}
			c.Kind = (c.Kind + by) % len(c.Kinds)
		}
	case "\r", "\n":
		save = true
	case "\x7f", "\x08":
		if field != nil {
			_, size := utf8.DecodeLastRuneInString(*field)
			*field = (*field)[:len(*field)-size]
		}
	case "\x15":
		if field != nil {
			*field = ""
		}
	default:
		if field != nil && len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			*field += key
		}
	}
	c.Error = ""
	t.dirty = true
	t.mu.Unlock()
	if save {
		t.saveTicketsConnect()
	}
}

// ticketSourceName is the name an account is kept under: its tracker, and
// who it signs in as or the site, made a bare TOML key and unique.
func (t *tui) ticketSourceNameLocked(kind, who, site string) string {
	base := kind + "-" + config.ErrorSourceName(who)
	if kind == string(tickets.Jira) && site != "" {
		base = kind + "-" + config.ErrorSourceName(site)
	}
	name := base
	for i := 2; ; i++ {
		if _, taken := t.config.Tickets.Sources[name]; !taken {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// saveTicketsConnect tries the account and keeps it only when the tracker
// takes the token: the token in the keyring when there is one.
func (t *tui) saveTicketsConnect() {
	t.mu.Lock()
	v := t.tickets
	if v == nil || v.Connect == nil {
		t.mu.Unlock()
		return
	}
	c := v.Connect
	kind := c.Kinds[c.Kind]
	acct := tickets.Account{Kind: tickets.Kind(kind), Token: strings.TrimSpace(c.Token)}
	if kind == string(tickets.Jira) || kind == string(tickets.IssueGateway) {
		acct.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
		if kind == string(tickets.Jira) {
			acct.Email = strings.TrimSpace(c.Email)
		}
		if acct.URL == "" || acct.URL == "https:" || acct.URL == "http:" {
			c.Error = "Jira needs its site's address"
			if kind == string(tickets.IssueGateway) {
				c.Error = "the Issue Gateway needs its address"
			}
			t.dirty = true
			t.mu.Unlock()
			return
		}
	}
	if acct.Token == "" {
		c.Error = "a token, please"
		t.dirty = true
		t.mu.Unlock()
		return
	}
	c.Testing = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		client, err := tickets.New(acct)
		who := ""
		if err == nil {
			who, err = client.WhoAmI()
		}
		t.mu.Lock()
		name := t.ticketSourceNameLocked(kind, who, acct.URL)
		t.mu.Unlock()
		value, kept := acct.Token, ""
		if err == nil && keyring.Available() {
			kept = "tickets:" + name
			if keyring.Set(kept, acct.Token) == nil {
				value = keyring.Ref(kept)
			} else {
				kept = ""
			}
		}
		if err == nil {
			section := "tickets.sources." + name
			for _, kv := range [][2]string{{"kind", kind}, {"url", acct.URL}, {"email", acct.Email}, {"token", value}} {
				if kv[1] == "" && kv[0] != "token" {
					continue
				}
				if err = config.Set(section, kv[0], config.Quote(kv[1])); err != nil {
					break
				}
			}
			if err == nil {
				err = config.Set("tickets", "source", config.Quote(name))
			}
		}
		t.mu.Lock()
		v := t.tickets
		if v == nil || v.Connect == nil {
			t.mu.Unlock()
			return
		}
		if err != nil {
			v.Connect.Testing, v.Connect.Error = false, err.Error()
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		if kept != "" {
			if t.ticketState.tokens == nil {
				t.ticketState.tokens = map[string]string{}
			}
			t.ticketState.tokens[kept] = acct.Token
		}
		if t.config.Tickets.Sources == nil {
			t.config.Tickets.Sources = map[string]config.TicketSource{}
		}
		t.config.Tickets.Sources[name] = config.TicketSource{Kind: kind, URL: acct.URL, Email: acct.Email, Token: value}
		v.Connect, v.Manage = nil, nil
		at := 0
		for i, s := range t.config.TicketSources() {
			if s.Name == name {
				at = i
			}
		}
		t.mu.Unlock()
		t.useTicketSource(at)
		t.ticketsSay("connected to " + kind + " as " + who)
	}()
}

// --- input -------------------------------------------------------------------

func (t *tui) ticketsInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		t.mu.Lock()
		v, cols, rows := t.tickets, t.cols, t.rows
		detail := v != nil && v.Detail != nil
		t.mu.Unlock()
		if v == nil {
			return nil
		}
		switch ev.Kind {
		case ui.MouseWheelUp, ui.MouseWheelDown:
			by := 3
			if ev.Kind == ui.MouseWheelUp {
				by = -3
			}
			if detail {
				t.scrollTicket(by)
			} else {
				t.moveTicketCursor(by)
			}
			continue
		}
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		if id, ok := ui.TicketsAt(v, cols, rows, ev.X, ev.Y); ok {
			if t.ticketsAction(id) {
				return nil
			}
		}
	}
	for _, key := range splitKeys(forward) {
		t.mu.Lock()
		v := t.tickets
		connecting := v != nil && v.Connect != nil
		managing := v != nil && v.Manage != nil
		typing := v != nil && v.Input != nil
		confirming := v != nil && v.Confirm
		connected := v != nil && v.Connected
		detail := v != nil && v.Detail != nil
		t.mu.Unlock()
		if v == nil {
			return nil
		}
		var done bool
		switch {
		case connecting:
			t.ticketsConnectKey(key)
		case managing:
			t.ticketsManageKey(key)
		case typing:
			t.ticketInputKey(key)
		case confirming:
			t.mu.Lock()
			v.Confirm = false
			t.dirty = true
			t.mu.Unlock()
			if key == "\r" || key == "\n" || key == "y" {
				t.markTicketDone()
			}
		case !connected:
			switch key {
			case "\r", "\n":
				t.openTicketsConnect()
			case "\x1b", "q":
				t.closeTickets()
				done = true
			}
		case detail:
			done = t.ticketDetailKey(key)
		default:
			done = t.ticketListKey(key)
		}
		if done {
			return nil
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) ticketInputKey(key string) {
	t.mu.Lock()
	v := t.tickets
	if v == nil || v.Input == nil || v.Input.Sending {
		t.mu.Unlock()
		return
	}
	switch key {
	case "\x1b":
		v.Input = nil
	case "\r", "\n":
		t.mu.Unlock()
		t.sendTicketComment()
		return
	case "\x7f", "\x08":
		_, size := utf8.DecodeLastRuneInString(v.Input.Text)
		v.Input.Text = v.Input.Text[:len(v.Input.Text)-size]
	case "\x15":
		v.Input.Text = ""
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			v.Input.Text += key
		}
	}
	t.dirty = true
	t.mu.Unlock()
}

// ticketsAction does what a click is for; it reports whether the panel went.
func (t *tui) ticketsAction(id string) bool {
	var n int
	setView := func(f func(v *ui.TicketsView)) {
		t.mu.Lock()
		if v := t.tickets; v != nil {
			f(v)
		}
		t.dirty = true
		t.mu.Unlock()
	}
	switch {
	case strings.HasPrefix(id, "account:"):
		fmt.Sscan(id[len("account:"):], &n)
		t.useTicketSource(n)
		return false
	case strings.HasPrefix(id, "scope:"):
		fmt.Sscan(id[len("scope:"):], &n)
		setView(func(v *ui.TicketsView) { v.Scope, v.Cursor, v.Scroll = n, 0, 0 })
		t.mu.Lock()
		t.showTicketLinkLocked()
		t.mu.Unlock()
		t.reloadTickets()
		return false
	case strings.HasPrefix(id, "filter:"):
		fmt.Sscan(id[len("filter:"):], &n)
		setView(func(v *ui.TicketsView) { v.Filter, v.Cursor, v.Scroll = n, 0, 0 })
		t.reloadTickets()
		return false
	case strings.HasPrefix(id, "row:"):
		fmt.Sscan(id[len("row:"):], &n)
		again := false
		setView(func(v *ui.TicketsView) { again = v.Cursor == n; v.Cursor = n })
		if again {
			t.openTicket()
		}
		return false
	case strings.HasPrefix(id, "kind:"):
		fmt.Sscan(id[len("kind:"):], &n)
		setView(func(v *ui.TicketsView) {
			if v.Connect != nil {
				v.Connect.Kind, v.Connect.Field = n, 0
			}
		})
		return false
	case strings.HasPrefix(id, "field:"):
		fmt.Sscan(id[len("field:"):], &n)
		setView(func(v *ui.TicketsView) {
			if v.Connect != nil {
				v.Connect.Field = n
			}
		})
		return false
	case strings.HasPrefix(id, "server:"):
		fmt.Sscan(id[len("server:"):], &n)
		again := false
		setView(func(v *ui.TicketsView) {
			if m := v.Manage; m != nil {
				again = m.Cursor == n && !m.Confirm
				m.Cursor, m.Confirm = n, false
			}
		})
		if again {
			t.ticketsManageKey("\r")
		}
		return false
	}
	switch id {
	case ui.TicketsClose:
		t.closeTickets()
		return true
	case ui.TicketsConnB:
		t.openTicketsConnect()
	case ui.TicketsGear:
		t.openTicketsManage()
	case ui.TicketsSave:
		t.saveTicketsConnect()
	case ui.TicketsCancel:
		setView(func(v *ui.TicketsView) { v.Connect = nil })
	case ui.ErrorsUse:
		t.ticketsManageKey("\r")
	case ui.ErrorsAdd:
		t.ticketsManageKey("a")
	case ui.ErrorsRemove:
		t.ticketsManageKey("d")
	case ui.ErrorsManageEnd:
		t.ticketsManageKey("\x1b")
	case ui.TicketsOpen:
		t.openTicket()
	case ui.TicketsBack:
		t.ticketDetailKey("\x1b")
	case ui.TicketsFix:
		t.fixTicket()
	case ui.TicketsWork:
		t.fixTicketInWorktree()
	case ui.TicketsComment:
		t.ticketDetailKey("c")
	case ui.TicketsDone:
		setView(func(v *ui.TicketsView) { v.Confirm = true })
	case ui.TicketsBrowser:
		t.openTicketInBrowser()
	case ui.TicketsLink:
		t.toggleTicketLink()
	}
	return false
}

func (t *tui) ticketListKey(key string) bool {
	switch key {
	case "\x1b":
		t.mu.Lock()
		v := t.tickets
		if v != nil && v.Query != "" {
			v.Query = ""
			t.mu.Unlock()
			t.reloadTickets()
			return false
		}
		t.mu.Unlock()
		t.closeTickets()
		return true
	case "\r", "\n":
		t.openTicket()
	case "\x1b[A", "\x10":
		t.moveTicketCursor(-1)
	case "\x1b[B", "\x0e":
		t.moveTicketCursor(1)
	case "\x1b[5~":
		t.moveTicketCursor(-10)
	case "\x1b[6~":
		t.moveTicketCursor(10)
	case "\t", "\x1b[Z":
		t.mu.Lock()
		if v := t.tickets; v != nil {
			step := 1
			if key == "\x1b[Z" {
				step = len(v.Filters) - 1
			}
			v.Filter, v.Cursor, v.Scroll = (v.Filter+step)%len(v.Filters), 0, 0
		}
		t.mu.Unlock()
		t.reloadTickets()
	case "\x14": // ctrl+t: the next scope
		t.mu.Lock()
		if v := t.tickets; v != nil && len(v.Scopes) > 0 {
			v.Scope, v.Cursor, v.Scroll = (v.Scope+1)%len(v.Scopes), 0, 0
			t.showTicketLinkLocked()
		}
		t.mu.Unlock()
		t.reloadTickets()
	case "\x07": // ctrl+g: the next account
		t.mu.Lock()
		next := -1
		if v := t.tickets; v != nil && len(v.Accounts) > 1 {
			next = (v.Active + 1) % len(v.Accounts)
		}
		t.mu.Unlock()
		t.useTicketSource(next)
	case "\x0b": // ctrl+k: the accounts
		t.openTicketsManage()
	case "\x0c": // ctrl+l: tie the project folder
		t.toggleTicketLink()
	case "\x06": // ctrl+f
		t.fixTicket()
	case "\x18": // ctrl+x: mark done, after asking
		t.mu.Lock()
		if v := t.tickets; v != nil && v.Cursor < len(v.Tickets) {
			v.Confirm = true
		}
		t.dirty = true
		t.mu.Unlock()
	case "\x0f": // ctrl+o
		t.openTicketInBrowser()
	case "\x12": // ctrl+r
		t.reloadTickets()
	case "\x15":
		t.editTicketQuery(func(string) string { return "" })
	case "\x7f", "\x08":
		t.editTicketQuery(func(q string) string {
			_, size := utf8.DecodeLastRuneInString(q)
			return q[:len(q)-size]
		})
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			t.editTicketQuery(func(q string) string { return q + key })
		}
	}
	return false
}

func (t *tui) editTicketQuery(edit func(string) string) {
	t.mu.Lock()
	v := t.tickets
	if v == nil {
		t.mu.Unlock()
		return
	}
	v.Query = edit(v.Query)
	t.ticketState.seq++
	seq := t.ticketState.seq
	t.dirty = true
	t.mu.Unlock()
	time.AfterFunc(issueSearchDelay, func() {
		t.mu.Lock()
		current := seq == t.ticketState.seq
		t.mu.Unlock()
		if current {
			t.reloadTickets()
		}
	})
}

func (t *tui) ticketDetailKey(key string) bool {
	switch key {
	case "\x1b", "q":
		t.mu.Lock()
		if v := t.tickets; v != nil {
			v.Detail, v.Message = nil, ""
		}
		t.ticketState.detail = nil
		t.ticketState.detailSeq++
		t.dirty = true
		t.mu.Unlock()
	case "\x1b[A", "k":
		t.scrollTicket(-1)
	case "\x1b[B", "j":
		t.scrollTicket(1)
	case "\x1b[5~", "\x15":
		t.scrollTicket(-10)
	case "\x1b[6~", "\x04", " ":
		t.scrollTicket(10)
	case "f":
		t.fixTicket()
	case "w":
		t.fixTicketInWorktree()
	case "c":
		t.mu.Lock()
		if v := t.tickets; v != nil {
			v.Input = &ui.TicketsInput{}
		}
		t.dirty = true
		t.mu.Unlock()
	case "x":
		t.mu.Lock()
		if v := t.tickets; v != nil {
			v.Confirm = true
		}
		t.dirty = true
		t.mu.Unlock()
	case "o", "\x0f":
		t.openTicketInBrowser()
	}
	return false
}

func (t *tui) moveTicketCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.tickets
	if v == nil || len(v.Tickets) == 0 {
		return
	}
	v.Cursor = max(min(v.Cursor+by, len(v.Tickets)-1), 0)
	v.Scroll = ui.TicketsScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

func (t *tui) scrollTicket(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.tickets
	if v == nil || v.Detail == nil {
		return
	}
	v.Detail.Scroll = max(v.Detail.Scroll+by, 0)
	v.Detail.Scroll = ui.ClampTicketScroll(v, t.cols, t.rows, t.theme)
	t.dirty = true
}
