package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/sidebar"
	"github.com/auth-com-br/tend/internal/when"
)

// The dock is the window's right side: one tab at a time of the panels the
// TUI has as overlays and panes — tasks, the files, issues, errors,
// tickets, WhatsApp — drawn natively (docs/DESKTOP.md, "The window"). Which
// tab is open is the window's own; what it shows is read from the server
// when it opens and again when the server says it changed.

// DockState is the dock as QML draws it: which tab, and that tab's data.
type DockState struct {
	Tab     string        `json:"tab"`
	Loading bool          `json:"loading,omitempty"`
	Error   string        `json:"error,omitempty"`
	Tasks   []TaskRow     `json:"tasks,omitempty"`
	Files   *FilesState   `json:"files,omitempty"`
	Issues  *IssuesState  `json:"issues,omitempty"`
	Errors  *ErrorsState  `json:"errors,omitempty"`
	Tickets *TicketsState `json:"tickets,omitempty"`
	// WhatsApp is tend-whatsapp's daemon as the tab shows it (whatsapp.go).
	WhatsApp *WhatsAppState `json:"whatsapp,omitempty"`
}

// TaskRow is a task as the dock lists it.
type TaskRow struct {
	ID     uint64 `json:"id"`
	Title  string `json:"title"`
	Notes  string `json:"notes,omitempty"`
	Status string `json:"status"`
	// Due and StartAt are as a person reads them; Day is the day the task
	// is for (its start, else its due), which the list is grouped by.
	Due       string `json:"due,omitempty"`
	StartAt   string `json:"startAt,omitempty"`
	Day       string `json:"day"`
	Agent     string `json:"agent,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Pane      uint64 `json:"pane,omitempty"`
	Error     string `json:"error,omitempty"`
}

// dock is what the session keeps of it.
type dock struct {
	tab string
	// space is the space the tabs were read for: the files, issues,
	// errors and tickets are those of the project the space works in.
	space   uint64
	loading bool
	err     string
	tasks   []proto.TaskInfo
	issues  issuesTab
	errors  errorsTab
	tickets ticketsTab
}

// ToggleDock opens a tab of the dock, or closes the dock when it is the tab
// open.
func (s *Session) ToggleDock(tab string) {
	s.mu.Lock()
	if s.dock.tab == tab {
		s.dock.tab = ""
	} else {
		s.dock.tab = tab
	}
	open := s.dock.tab
	s.followSpaceLocked()
	s.mu.Unlock()
	s.stateChanged()
	if open != "" {
		go s.loadDock(open)
	}
}

// spaceTabs are the dock's tabs that show the project of the space shown;
// tasks and WhatsApp are the session's and the owner's, whatever the space.
var spaceTabs = map[string]bool{"files": true, "issues": true, "errors": true, "tickets": true}

// followSpaceLocked moves the dock to the space shown when it is another
// than the one its tabs were read for, and says which tab to read again
// ("" for none). The TUI's panels fix their project when they open and go
// when they close; the window's dock stays open across spaces, and kept
// showing the first space's — its files, and its issues looked for on
// that project's forge, which the owner found from another space. The
// project each tab fixed is let go, and found again from the pane in focus
// in the space shown; what was chosen that is not the project's (issues
// or pull requests, a filter) is kept. The caller holds the lock.
func (s *Session) followSpaceLocked() string {
	if s.workspace == s.dock.space {
		return ""
	}
	s.dock.space = s.workspace
	is := &s.dock.issues
	*is = issuesTab{prs: is.prs, filter: is.filter, seq: is.seq, detailSeq: is.detailSeq}
	er := &s.dock.errors
	er.opened, er.pane, er.want, er.projectOf, er.scope = false, 0, "", "", 0
	tk := &s.dock.tickets
	tk.opened, tk.pane, tk.want, tk.scopesOf, tk.scope = false, 0, "", "", 0
	if spaceTabs[s.dock.tab] {
		return s.dock.tab
	}
	return ""
}

// loadDock reads what a tab shows, off the main thread.
func (s *Session) loadDock(tab string) {
	s.mu.Lock()
	c := s.client
	s.dock.loading = true
	s.mu.Unlock()
	s.stateChanged()

	var err error
	switch tab {
	case "files":
		err = s.loadFiles()
	case "whatsapp":
		err = s.loadWhatsApp()
		s.pollWhatsApp(whatsAppPoll)
	case "issues":
		err = s.loadIssues()
	case "errors":
		err = s.loadErrors()
	case "tickets":
		err = s.loadTickets()
	case "tasks":
		var tasks []proto.TaskInfo
		if !c.Supports(proto.MethodTaskList) {
			err = fmt.Errorf("this server is older than tasks: tend handoff moves it to this build")
		} else {
			tasks, err = c.Tasks()
		}
		s.mu.Lock()
		if err == nil {
			s.dock.tasks = tasks
		}
		s.mu.Unlock()
		// The tickets tend already reads (Linear, Jira, ClickUp…) beside
		// the tasks, the plan of the work in one view (#72). Quietly: no
		// account set up is no tickets, not an error on the tasks.
		_ = s.loadTickets()
	}
	s.mu.Lock()
	s.dock.loading = false
	s.dock.err = ""
	if err != nil {
		s.dock.err = err.Error()
	}
	s.mu.Unlock()
	s.stateChanged()
}

// reloadDockIf reads a tab again when it is the one open: what an event
// saying it changed asks for.
func (s *Session) reloadDockIf(tab string) {
	s.mu.Lock()
	open := s.dock.tab == tab
	s.mu.Unlock()
	if open {
		go s.loadDock(tab)
	}
}

// dockStateLocked is the dock now, nil while it is closed. The caller holds
// the lock.
func (s *Session) dockStateLocked() *DockState {
	if s.dock.tab == "" {
		return nil
	}
	st := &DockState{Tab: s.dock.tab, Loading: s.dock.loading, Error: s.dock.err}
	if s.dock.tab == "files" {
		st.Files = s.filesStateLocked()
	}
	switch s.dock.tab {
	case "whatsapp":
		st.WhatsApp = s.whatsAppStateLocked()
	case "issues":
		st.Issues = s.issuesStateLocked()
	case "errors":
		st.Errors = s.errorsStateLocked()
	case "tickets":
		st.Tickets = s.ticketsStateLocked()
	}
	if s.dock.tab == "tasks" {
		if tk := s.ticketsStateLocked(); tk != nil && tk.Connected {
			st.Tickets = tk
		}
		names := map[uint64]string{}
		for _, w := range s.snap.Workspaces {
			names[w.ID] = sidebar.OrDash(w.Name)
		}
		for _, t := range s.dock.tasks {
			st.Tasks = append(st.Tasks, taskRow(t, names))
		}
	}
	return st
}

func taskRow(t proto.TaskInfo, spaces map[uint64]string) TaskRow {
	stamp := func(unix int64) string {
		if unix == 0 {
			return ""
		}
		return time.Unix(unix, 0).Local().Format("Mon 02 Jan 15:04")
	}
	day := t.StartAt
	if day == 0 {
		day = t.Due
	}
	r := TaskRow{
		ID: t.ID, Title: t.Title, Notes: t.Notes, Status: t.Status,
		Due: stamp(t.Due), StartAt: stamp(t.StartAt),
		Agent: t.Agent, Prompt: t.Prompt, Pane: t.Pane, Error: t.Error,
	}
	if day != 0 {
		r.Day = time.Unix(day, 0).Local().Format("2006-01-02")
	}
	if t.Workspace != 0 {
		r.Workspace = spaces[t.Workspace]
	}
	return r
}

// NewTask is the task form's fields, as typed.
type NewTask struct {
	Title  string `json:"title"`
	Notes  string `json:"notes"`
	Due    string `json:"due"`
	At     string `json:"at"`
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
	// Here puts it in the space shown, where its agent will start.
	Here bool `json:"here"`
}

// CreateTask makes a task from the form. Times are read as the CLI reads
// them (when.Parse): 09:00, +30m, a date.
func (s *Session) CreateTask(n NewTask) error {
	title := strings.TrimSpace(n.Title)
	if title == "" {
		return fmt.Errorf("a task needs a title")
	}
	p := proto.TaskParams{Title: &title}
	if notes := strings.TrimSpace(n.Notes); notes != "" {
		p.Notes = &notes
	}
	now := time.Now()
	for _, f := range []struct {
		text string
		into **int64
	}{{n.Due, &p.Due}, {n.At, &p.StartAt}} {
		if strings.TrimSpace(f.text) == "" {
			continue
		}
		t, err := when.Parse(f.text, now)
		if err != nil {
			return err
		}
		unix := t.Unix()
		*f.into = &unix
	}
	if agent := strings.TrimSpace(n.Agent); agent != "" {
		p.Agent = &agent
		if prompt := strings.TrimSpace(n.Prompt); prompt != "" {
			p.Prompt = &prompt
		}
	}
	s.mu.Lock()
	c, ws := s.client, s.workspace
	s.mu.Unlock()
	if n.Here && ws != 0 {
		p.Workspace = &ws
	}
	_, err := c.CreateTask(p)
	return err
}

// SetTaskStatus marks a task todo, doing or done.
func (s *Session) SetTaskStatus(id uint64, status string) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	_, err := c.UpdateTask(proto.TaskParams{Task: id, Status: &status})
	return err
}

// DeleteTask removes a task.
func (s *Session) DeleteTask(id uint64) error {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	return c.DeleteTask(id)
}
