package core

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/glitchtip"
	"github.com/auth-com-br/tend/internal/keyring"
	"github.com/auth-com-br/tend/internal/workon"
	"github.com/auth-com-br/tend/internal/worktree"
)

// The errors tab: the TUI's errors panel (cmd/tend/tui_errors.go) natively.
// The window talks to GlitchTip — or Sentry, through the same client —
// itself, with the servers and tokens the settings keep (tokens in the
// system keyring when they are there), as the TUI does: it is a web
// service, not something on the machine the projects are on. It lists the
// errors the systems reported, opens one with its stack, marks it resolved
// or ignored, and hands it to an agent: typed into the pane the tab was
// opened from, or started in a worktree of the space's project. A server is
// added in the TUI's panel (prefix+E): the window reads what it saved.

// errorsTab is what the session keeps of the errors tab.
type errorsTab struct {
	// opened is that the folder and the tokens were read, which happens
	// the first time the tab opens, from the pane in focus then.
	opened bool
	pane   uint64
	// source is the server shown when it is not the one remembered: the
	// one chosen here, or the one the project folder is tied to; want is
	// the tied project, chosen once the server's projects are read.
	source    string
	dir, root string
	want      string
	tokens    map[string]string
	projects  []glitchtip.Project
	projectOf string // the source the projects are of
	scope     int    // 0 is all projects, else 1 + its place
	status    int
	query     string
	seq       int
	detailSeq int
	items     []glitchtip.Issue
	open      *glitchtip.Issue
	event     *glitchtip.Event
	reading   bool
	busy      bool
	message   string
}

// ErrorsState is the errors tab as QML draws it.
type ErrorsState struct {
	Connected bool        `json:"connected"`
	Server    string      `json:"server"`
	Sources   []string    `json:"sources"`
	Source    int         `json:"source"`
	Folder    string      `json:"folder"`
	Scopes    []string    `json:"scopes"`
	Scope     int         `json:"scope"`
	Statuses  []string    `json:"statuses"`
	Status    int         `json:"status"`
	Query     string      `json:"query"`
	Errors    []ErrorRow  `json:"errors"`
	Open      *ErrorShown `json:"open,omitempty"`
	Reading   bool        `json:"reading,omitempty"`
	Busy      bool        `json:"busy,omitempty"`
	Message   string      `json:"message,omitempty"`
	Agent     string      `json:"agent"`
}

// ErrorRow is an error in the list.
type ErrorRow struct {
	ID      string `json:"id"`
	ShortID string `json:"shortId"`
	Project string `json:"project"`
	Title   string `json:"title"`
	Culprit string `json:"culprit"`
	Level   string `json:"level"`
	Status  string `json:"status"`
	Count   int    `json:"count"`
	Users   int    `json:"users"`
	Last    string `json:"last"`
	First   string `json:"first"`
	URL     string `json:"url"`
}

// ErrorShown is an error open, with its latest event.
type ErrorShown struct {
	ErrorRow
	Request    string           `json:"request"`
	Release    string           `json:"release"`
	Tags       []string         `json:"tags"`
	Exceptions []ExceptionShown `json:"exceptions"`
	Crumbs     []string         `json:"crumbs"`
}

// ExceptionShown is one exception of an event, its frames innermost last
// as GlitchTip gives them.
type ExceptionShown struct {
	Type   string       `json:"type"`
	Value  string       `json:"value"`
	Frames []FrameShown `json:"frames"`
}

// FrameShown is one frame of a stack.
type FrameShown struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
	InApp    bool   `json:"inApp"`
	Code     string `json:"code"`
}

func errorRow(i glitchtip.Issue) ErrorRow {
	return ErrorRow{ID: i.ID, ShortID: i.ShortID, Project: i.Project, Title: i.Title, Culprit: i.Culprit,
		Level: i.Level, Status: i.Status, Count: i.Count, Users: i.Users, Last: ago(i.LastSeen), First: ago(i.FirstSeen), URL: i.URL}
}

// hostOf is a server's address as the tab names it.
func hostOf(url string) string {
	return strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
}

// tokenOf is a source's token: the setting itself, or what the keyring gave
// for one kept there — "" when it could not, which the client then says is
// not connected.
func tokenOf(value string, read map[string]string) string {
	if name, ok := keyring.Name(value); ok {
		return read[name]
	}
	return value
}

// errorsClientLocked is a client for the server shown, or nil. The caller
// holds the lock.
func (s *Session) errorsClientLocked() (*glitchtip.Client, config.NamedErrorSource) {
	t := &s.dock.errors
	src, _, ok := s.cfg.ErrorSourceShown(t.source)
	if !ok {
		return nil, src
	}
	return glitchtip.New(src.URL, tokenOf(src.Token, t.tokens)), src
}

// errorsStateLocked is the tab now. The caller holds the lock.
func (s *Session) errorsStateLocked() *ErrorsState {
	t := &s.dock.errors
	src, at, ok := s.cfg.ErrorSourceShown(t.source)
	st := &ErrorsState{
		Connected: ok, Server: hostOf(src.URL), Source: at, Sources: []string{}, Scopes: []string{},
		Scope: t.scope, Statuses: glitchtip.Statuses, Status: t.status, Query: t.query, Errors: []ErrorRow{},
		Reading: t.reading, Busy: t.busy, Message: t.message, Agent: s.agentForLocked(t.pane),
	}
	if t.root != "" {
		st.Folder = filepath.Base(t.root)
	}
	for _, x := range s.cfg.ErrorSources() {
		st.Sources = append(st.Sources, hostOf(x.URL))
	}
	if len(t.projects) > 0 {
		st.Scopes = append(st.Scopes, "all")
		for _, p := range t.projects {
			st.Scopes = append(st.Scopes, p.Slug)
		}
	}
	for _, i := range t.items {
		st.Errors = append(st.Errors, errorRow(i))
	}
	if i := t.open; i != nil {
		shown := &ErrorShown{ErrorRow: errorRow(*i), Tags: []string{}, Exceptions: []ExceptionShown{}, Crumbs: []string{}}
		if e := t.event; e != nil {
			shown.Request, shown.Release = e.Request, e.Release
			for _, tag := range e.Tags {
				shown.Tags = append(shown.Tags, tag[0]+"="+tag[1])
			}
			for _, ex := range e.Exceptions {
				out := ExceptionShown{Type: ex.Type, Value: ex.Value, Frames: []FrameShown{}}
				for _, f := range ex.Frames {
					out.Frames = append(out.Frames, FrameShown{File: f.File, Line: f.Line, Function: f.Function, InApp: f.InApp, Code: f.Code})
				}
				shown.Exceptions = append(shown.Exceptions, out)
			}
			for _, cr := range e.Crumbs {
				line := cr.Message
				if cr.Category != "" {
					line = cr.Category + ": " + line
				}
				if !cr.Time.IsZero() {
					line = cr.Time.Local().Format("15:04:05") + "  " + line
				}
				shown.Crumbs = append(shown.Crumbs, line)
			}
		}
		st.Open = shown
	}
	return st
}

// openErrorsOnce learns, the first time the tab opens, the project folder
// of the pane in focus and the tokens the keyring keeps, and chooses the
// server the folder is tied to — the TUI's findErrorsFolder. Asking the
// keyring can wait on the user unlocking it, so it is done off the lock.
func (s *Session) openErrorsOnce() {
	s.mu.Lock()
	t := &s.dock.errors
	if t.opened {
		s.mu.Unlock()
		return
	}
	t.opened, t.pane = true, s.focus
	pane := t.pane
	var names []string
	for _, src := range s.cfg.ErrorSources() {
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
			missing = append(missing, strings.TrimPrefix(name, "glitchtip:"))
		}
	}
	dir := s.paneCwd(pane)
	root := ""
	if dir != "" {
		root = workon.ProjectRoot(dir)
	}
	s.mu.Lock()
	t.tokens, t.dir, t.root = tokens, dir, root
	if src, project, ok := s.cfg.ErrorSourceFor(root, dir); ok {
		t.source, t.want = src.Name, project
	}
	if len(missing) > 0 {
		t.message = "the keyring did not give the token for " + strings.Join(missing, ", ") + "; add the server again in tend (prefix+E)"
	}
	s.mu.Unlock()
}

// loadErrors reads the server's projects when they are not read yet, then
// the list as the tab says.
func (s *Session) loadErrors() error {
	s.openErrorsOnce()
	s.mu.Lock()
	t := &s.dock.errors
	c, src := s.errorsClientLocked()
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	needProjects := t.projectOf != src.Name
	s.mu.Unlock()
	if needProjects {
		projects, err := c.Projects()
		s.mu.Lock()
		// With the chips only a help, a failure to read them still lists
		// every project.
		t.projectOf, t.projects, t.scope = src.Name, nil, 0
		if err == nil {
			t.projects = projects
			for i, p := range projects {
				if t.want != "" && p.Org+"/"+p.Slug == t.want {
					t.scope = i + 1
				}
			}
		}
		t.want = ""
		s.mu.Unlock()
	}
	return s.listErrors()
}

// listErrors asks for the list as the tab says, and keeps the answer if
// nothing was asked since.
func (s *Session) listErrors() error {
	s.mu.Lock()
	t := &s.dock.errors
	c, _ := s.errorsClientLocked()
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	t.seq++
	seq := t.seq
	var projects []glitchtip.Project
	if t.scope > 0 && t.scope <= len(t.projects) {
		projects = []glitchtip.Project{t.projects[t.scope-1]}
	}
	status := glitchtip.Statuses[max(min(t.status, len(glitchtip.Statuses)-1), 0)]
	query := t.query
	t.busy = true
	s.mu.Unlock()
	s.stateChanged()
	items, err := c.Issues(projects, status, query)
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		s.stateChanged()
	}()
	if t.seq != seq {
		return nil
	}
	t.busy = false
	if err != nil && len(items) == 0 {
		t.items = nil
		return err
	}
	if err != nil {
		t.message = "some of it could not be read: " + err.Error()
	}
	t.items = items
	return nil
}

// ErrorsAction is one thing the errors tab asks for.
type ErrorsAction struct {
	Do    string `json:"do"`
	Index int    `json:"index"`
	Query string `json:"query"`
	// ID is the error's, as "error": dispatch reads "id" as a number.
	ID     string `json:"error"`
	Status string `json:"status"`
}

// Errors does what the errors tab asks.
func (s *Session) Errors(a ErrorsAction) error {
	t := &s.dock.errors
	relist := func(change func()) error {
		s.mu.Lock()
		change()
		s.mu.Unlock()
		return s.listErrors()
	}
	switch a.Do {
	case "source":
		sources := s.sourcesNow()
		if a.Index < 0 || a.Index >= len(sources) {
			return nil
		}
		name := sources[a.Index].Name
		s.mu.Lock()
		t.source, t.want, t.scope, t.items, t.open, t.event, t.message = name, "", 0, nil, nil, nil, ""
		s.cfg.Errors.Source = name
		s.mu.Unlock()
		// Remembered as the TUI remembers it, for the next time either
		// client opens the panel.
		if err := config.Set("errors", "source", config.Quote(name)); err != nil {
			s.errorsSay("showing " + name + ", but it could not be remembered: " + err.Error())
		}
		return s.loadErrors()
	case "scope":
		return relist(func() { t.scope = a.Index })
	case "status":
		return relist(func() { t.status = a.Index })
	case "search":
		return relist(func() { t.query = a.Query })
	case "refresh":
		// The settings are read again first: a server added in the TUI's
		// panel is then shown here.
		if err := s.ReloadSettings(); err != nil {
			return err
		}
		s.mu.Lock()
		t.opened, t.projectOf = false, ""
		s.mu.Unlock()
		return s.loadErrors()
	case "open":
		return s.openError(a.ID)
	case "back":
		s.mu.Lock()
		t.open, t.event, t.message = nil, nil, ""
		t.detailSeq++
		s.mu.Unlock()
		s.stateChanged()
		return nil
	case "set":
		return s.setErrorStatus(a.ID, a.Status)
	case "fix":
		issue, prompt, err := s.errorPrompt(a.ID)
		if err != nil {
			return err
		}
		s.mu.Lock()
		pane := t.pane
		s.mu.Unlock()
		return s.handToPane(pane, workon.ContextItem("glitchtip", issue.ShortID+" "+issue.Title, issue.URL, prompt), issue.ShortID)
	case "context":
		issue, prompt, err := s.errorPrompt(a.ID)
		if err != nil {
			return err
		}
		s.mu.Lock()
		c := s.client
		s.mu.Unlock()
		if _, err := c.ContextAdd(workon.ContextItem("glitchtip", issue.ShortID+" "+issue.Title, issue.URL, prompt)); err != nil {
			return fmt.Errorf("could not put it in the context: %w", err)
		}
		s.errorsSay(issue.ShortID + " is in the context")
		return nil
	case "worktree":
		issue, prompt, err := s.errorPrompt(a.ID)
		if err != nil {
			return err
		}
		s.mu.Lock()
		c, ws, agentName := s.client, s.shownSpaceLocked(), s.agentForLocked(t.pane)
		s.mu.Unlock()
		s.errorsSay("making a worktree…")
		got, err := workon.Fix(s.apiCaller(), c, ws, "fix-"+worktree.Slug(issue.ShortID), issue.ShortID, agentName, prompt)
		if err != nil {
			s.errorsSay(err.Error())
			return err
		}
		s.errorsSay(got.Message)
		return s.goToStarted(got)
	}
	return fmt.Errorf("the errors tab asked for %q", a.Do)
}

func (s *Session) sourcesNow() []config.NamedErrorSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.ErrorSources()
}

// errorsSay puts a line under the tab.
func (s *Session) errorsSay(message string) {
	s.mu.Lock()
	s.dock.errors.message = message
	s.mu.Unlock()
	s.stateChanged()
}

// errorByIDLocked is an error of the list by its id, or the one open.
func (s *Session) errorByIDLocked(id string) (glitchtip.Issue, bool) {
	t := &s.dock.errors
	if id == "" && t.open != nil {
		return *t.open, true
	}
	for _, i := range t.items {
		if i.ID == id {
			return i, true
		}
	}
	if t.open != nil && t.open.ID == id {
		return *t.open, true
	}
	return glitchtip.Issue{}, false
}

// openError opens an error and reads its latest event.
func (s *Session) openError(id string) error {
	s.mu.Lock()
	t := &s.dock.errors
	issue, ok := s.errorByIDLocked(id)
	c, _ := s.errorsClientLocked()
	if !ok || c == nil {
		s.mu.Unlock()
		return fmt.Errorf("no such error")
	}
	t.open, t.event, t.reading = &issue, nil, true
	t.detailSeq++
	seq := t.detailSeq
	s.mu.Unlock()
	s.stateChanged()
	e, err := c.Latest(issue.Org, issue.ID)
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
		t.message = "could not read its latest event: " + err.Error()
		return nil
	}
	t.event = &e
	return nil
}

// errorPrompt is what an agent is told to fix an error with; its latest
// event is read when it has not been.
func (s *Session) errorPrompt(id string) (glitchtip.Issue, string, error) {
	s.mu.Lock()
	t := &s.dock.errors
	issue, ok := s.errorByIDLocked(id)
	c, _ := s.errorsClientLocked()
	var event *glitchtip.Event
	if t.open != nil && t.open.ID == issue.ID {
		event = t.event
	}
	s.mu.Unlock()
	if !ok || c == nil {
		return issue, "", fmt.Errorf("no error chosen")
	}
	if event == nil {
		e, err := c.Latest(issue.Org, issue.ID)
		if err != nil {
			return issue, "", err
		}
		event = &e
	}
	return issue, glitchtip.FixPrompt(issue, *event), nil
}

// setErrorStatus marks an error on the server — resolved, ignored,
// unresolved — and reads the list again.
func (s *Session) setErrorStatus(id, status string) error {
	s.mu.Lock()
	t := &s.dock.errors
	issue, ok := s.errorByIDLocked(id)
	c, _ := s.errorsClientLocked()
	s.mu.Unlock()
	if !ok || c == nil {
		return fmt.Errorf("no error chosen")
	}
	if err := c.SetStatus(issue.Org, issue.ID, status); err != nil {
		return fmt.Errorf("GlitchTip said: %w", err)
	}
	s.mu.Lock()
	if t.open != nil && t.open.ID == issue.ID {
		t.open.Status = status
	}
	s.mu.Unlock()
	s.errorsSay(issue.ShortID + " is " + status)
	return s.listErrors()
}
