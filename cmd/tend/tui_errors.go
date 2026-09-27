package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/api"
	"github.com/auth-com-br/tend/internal/capture"
	"github.com/auth-com-br/tend/internal/config"
	"github.com/auth-com-br/tend/internal/glitchtip"
	"github.com/auth-com-br/tend/internal/keyring"
	"github.com/auth-com-br/tend/internal/proto"
	sessionpkg "github.com/auth-com-br/tend/internal/session"
	"github.com/auth-com-br/tend/internal/ui"
	"github.com/auth-com-br/tend/internal/worktree"
)

// The errors panel (internal/ui/errors.go, internal/glitchtip): tend's own,
// for the owner's error monitor. The client talks to GlitchTip itself — a
// web service, not something on the machine the projects are on — with the
// server and token the user gives it in the panel's connect box, kept in
// this client's settings file, readable by its owner alone. What it lists
// is the errors the systems reported; what it does with one is hand it,
// with its stack, to an agent: typed into the pane the panel was opened
// from, or started in a worktree of that space's project, or put in the
// context; and mark it resolved or ignored on the server.

// errorsState is what the panel keeps between frames that the view does
// not draw.
type errorsState struct {
	// pane is the pane the panel was opened from: where a fix is typed,
	// and whose space a worktree is made from.
	pane      uint64
	seq       int
	detailSeq int
	projects  []glitchtip.Project
	// items are the errors listed, in the view's order, and event the open
	// one's latest event.
	items []glitchtip.Issue
	event *glitchtip.Event
	// source is the server shown while the panel is up when it is not the
	// one remembered: the one the project folder is tied to.
	source string
	// dir is where the pane is working and root the project that is, and
	// want the project to show once the server's projects are read.
	dir, root string
	want      string
	// tokens are the tokens kept in the system keyring, read when the
	// panel opened, by their name there: asking the keyring can wait on the
	// user unlocking it, which must not happen under the client's lock.
	tokens map[string]string
}

func (t *tui) errorsUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.errors != nil
}

// errorSourceLocked is the server the panel shows: the one last chosen,
// else the first kept, with its place among them.
func (t *tui) errorSourceLocked() (config.NamedErrorSource, int, bool) {
	sources := t.config.ErrorSources()
	for _, name := range []string{t.errorState.source, t.config.Errors.Source} {
		for i, s := range sources {
			if name != "" && s.Name == name {
				return s, i, true
			}
		}
	}
	if len(sources) > 0 {
		return sources[0], 0, true
	}
	return config.NamedErrorSource{}, 0, false
}

// errorsClientLocked is a client for the server shown, or nil.
func (t *tui) errorsClientLocked() *glitchtip.Client {
	s, _, ok := t.errorSourceLocked()
	if !ok {
		return nil
	}
	return glitchtip.New(s.URL, t.tokenLocked(s.Token))
}

// tokenLocked is a source's token: the setting itself, or, for one kept in
// the keyring, what was read from it when the panel opened — "" when it
// could not be, which the client then says is not connected.
func (t *tui) tokenLocked(value string) string {
	if name, ok := keyring.Name(value); ok {
		return t.errorState.tokens[name]
	}
	return value
}

// readKeyringTokens reads the tokens the settings keep in the keyring, and
// says which could not be read.
func (t *tui) readKeyringTokens() {
	t.mu.Lock()
	var names []string
	for _, s := range t.config.ErrorSources() {
		if name, ok := keyring.Name(s.Token); ok {
			if _, have := t.errorState.tokens[name]; !have {
				names = append(names, name)
			}
		}
	}
	t.mu.Unlock()
	var missing []string
	for _, name := range names {
		secret, err := keyring.Get(name)
		t.mu.Lock()
		if t.errorState.tokens == nil {
			t.errorState.tokens = map[string]string{}
		}
		if err == nil {
			t.errorState.tokens[name] = secret
		}
		t.mu.Unlock()
		if err != nil {
			missing = append(missing, strings.TrimPrefix(name, "glitchtip:"))
		}
	}
	if len(missing) > 0 {
		t.errorSay("the keyring did not give the token for " + strings.Join(missing, ", ") + "; add the server again")
	}
}

// hostOf is a server's address as the panel names it.
func hostOf(url string) string {
	return strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
}

// showSourceLocked says in the view which servers there are and which one
// is shown, and forgets what was read from another one.
func (t *tui) showSourceLocked() {
	v := t.errors
	if v == nil {
		return
	}
	s, at, ok := t.errorSourceLocked()
	v.Connected, v.Server, v.Source = ok, s.URL, at
	v.Sources = v.Sources[:0]
	for _, src := range t.config.ErrorSources() {
		v.Sources = append(v.Sources, hostOf(src.URL))
	}
	if m := v.Manage; m != nil {
		m.Servers, m.Active = append([]string(nil), v.Sources...), at
		m.Cursor = max(min(m.Cursor, len(m.Servers)-1), 0)
	}
	t.showLinkLocked()
}

// errorProjectKey is how a project is named in a folder's tie: org/slug.
func errorProjectKey(p glitchtip.Project) string { return p.Org + "/" + p.Slug }

// shownProjectLocked is the project the list is of, as a tie names it, ""
// for all of them.
func (t *tui) shownProjectLocked() string {
	v := t.errors
	if v == nil || v.Scope <= 0 || v.Scope > len(t.errorState.projects) {
		return ""
	}
	return errorProjectKey(t.errorState.projects[v.Scope-1])
}

// showLinkLocked says in the title whether the project folder opens on
// what is shown.
func (t *tui) showLinkLocked() {
	v := t.errors
	if v == nil {
		return
	}
	v.Folder, v.Linked = "", false
	if t.errorState.root == "" {
		return
	}
	v.Folder = filepath.Base(t.errorState.root)
	if s, _, ok := t.errorSourceLocked(); ok {
		p, tied := s.Projects[t.errorState.root]
		v.Linked = tied && p == t.shownProjectLocked()
	}
}

// projectFolder is where a pane is working — its program's directory, as
// the automation socket's pane.get says — and the project that is: the
// repository's own folder, which for a worktree is the repository it was
// made from, so a fix in a worktree opens on the same errors.
func (t *tui) projectFolder(session string, pane uint64) (string, string) {
	if pane == 0 {
		return "", ""
	}
	res, err := apiCall(session, api.MethodPaneGet, map[string]any{"pane_id": api.PaneID(sessionpkg.PaneID(pane))}, false)
	if err != nil {
		return "", ""
	}
	info, _ := res["pane"].(map[string]any)
	dir, _ := info["foreground_cwd"].(string)
	if dir == "" {
		dir, _ = info["cwd"].(string)
	}
	if dir == "" {
		return "", ""
	}
	return dir, projectRoot(dir)
}

// projectRoot is the repository a directory is in, through its common git
// directory; a directory in no repository — a folder of several, say — is
// its own project.
func projectRoot(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return dir
	}
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return dir
}

// findErrorsFolder learns the project folder of the pane the panel was
// opened from, and when a server is tied to it shows that server and, once
// its projects are read, that project. The list is asked for only after,
// so the first one shown is the right one.
func (t *tui) findErrorsFolder(session string, pane uint64) {
	t.readKeyringTokens()
	dir, root := t.projectFolder(session, pane)
	t.mu.Lock()
	v := t.errors
	if v == nil {
		t.mu.Unlock()
		return
	}
	t.errorState.dir, t.errorState.root = dir, root
	if src, project, ok := t.config.ErrorSourceFor(root, dir); ok {
		t.errorState.source, t.errorState.want = src.Name, project
	}
	t.showSourceLocked()
	connected, want := v.Connected, t.errorState.want
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if !connected {
		return
	}
	go t.loadErrorProjects()
	if want == "" {
		t.reloadErrors()
	}
}

// toggleErrorLink ties the project folder to the server and project shown,
// or, when it is tied to them already, unties it. A folder is tied to one
// server: tying it here takes it from any other.
func (t *tui) toggleErrorLink() {
	t.mu.Lock()
	root := t.errorState.root
	src, _, ok := t.errorSourceLocked()
	project := t.shownProjectLocked()
	if root == "" || !ok {
		t.mu.Unlock()
		t.errorSay("no project folder is known for this pane")
		return
	}
	p, tied := src.Projects[root]
	untie := tied && p == project
	type write struct {
		name    string
		legacy  bool
		section string
		table   map[string]string
	}
	var writes []write
	for _, s := range t.config.ErrorSources() {
		table := map[string]string{}
		for k, v := range s.Projects {
			table[k] = v
		}
		_, had := table[root]
		switch {
		case s.Name == src.Name && untie:
			delete(table, root)
		case s.Name == src.Name:
			table[root] = project
		case had:
			delete(table, root)
		default:
			continue
		}
		section := "errors.sources." + s.Name
		if s.Legacy {
			section = "errors"
		}
		writes = append(writes, write{s.Name, s.Legacy, section, table})
	}
	t.mu.Unlock()
	go func() {
		for _, w := range writes {
			if err := config.Set(w.section, "projects", config.InlineTable(w.table)); err != nil {
				t.errorSay("could not tie the folder: " + err.Error())
				return
			}
			t.mu.Lock()
			if w.legacy {
				t.config.Errors.Projects = w.table
			} else if s, ok := t.config.Errors.Sources[w.name]; ok {
				s.Projects = w.table
				t.config.Errors.Sources[w.name] = s
			}
			t.mu.Unlock()
		}
		what := hostOf(src.URL)
		if project != "" {
			what += " · " + project
		}
		message := filepath.Base(root) + " now opens on " + what
		if untie {
			message = filepath.Base(root) + " no longer opens on " + what
		}
		t.mu.Lock()
		t.showLinkLocked()
		t.dirty = true
		t.mu.Unlock()
		t.errorSay(message)
	}()
}

// useErrorSource shows another server's errors, and remembers it as the
// one to show next time.
func (t *tui) useErrorSource(i int) {
	t.mu.Lock()
	sources := t.config.ErrorSources()
	if t.errors == nil || i < 0 || i >= len(sources) {
		t.mu.Unlock()
		return
	}
	name := sources[i].Name
	t.config.Errors.Source, t.errorState.source, t.errorState.want = name, name, ""
	v := t.errors
	v.Scopes, v.Scope, v.Errors, v.Cursor, v.Scroll, v.Error = nil, 0, nil, 0, 0, ""
	v.Detail, v.Message = nil, ""
	t.errorState.projects, t.errorState.items, t.errorState.event = nil, nil, nil
	t.showSourceLocked()
	t.dirty = true
	t.mu.Unlock()
	go func() {
		if err := config.Set("errors", "source", config.Quote(name)); err != nil {
			t.errorSay("showing " + name + ", but it could not be remembered: " + err.Error())
		}
	}()
	go t.loadErrorProjects()
	t.reloadErrors()
}

// openErrors puts the panel up: the errors when a server is connected,
// else the offer to connect one.
func (t *tui) openErrors() error {
	t.mu.Lock()
	fresh := t.errors == nil
	if fresh {
		t.errors = &ui.ErrorsView{Filters: glitchtip.Statuses, Now: time.Now()}
		t.errorState = errorsState{pane: t.focus}
	}
	v := t.errors
	t.showSourceLocked()
	v.Loading = v.Connected
	session, pane := t.session, t.errorState.pane
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if fresh {
		go t.findErrorsFolder(session, pane)
	} else if v.Connected {
		go t.loadErrorProjects()
		t.reloadErrors()
	}
	return nil
}

func (t *tui) closeErrors() {
	t.mu.Lock()
	t.errors = nil
	t.errorState.seq++
	t.errorState.detailSeq++
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// loadErrorProjects reads the projects, for the chips over the list.
func (t *tui) loadErrorProjects() {
	t.mu.Lock()
	c := t.errorsClientLocked()
	asked, _, _ := t.errorSourceLocked()
	t.mu.Unlock()
	if c == nil {
		return
	}
	projects, err := c.Projects()
	t.mu.Lock()
	v := t.errors
	shown, _, _ := t.errorSourceLocked()
	if v == nil || shown.Name != asked.Name {
		// Closed, or another server chosen while this one answered.
		t.mu.Unlock()
		return
	}
	want := t.errorState.want
	t.errorState.want = ""
	if err == nil {
		t.errorState.projects = projects
		v.Scopes = []string{"all"}
		for i, p := range projects {
			v.Scopes = append(v.Scopes, p.Slug)
			if want != "" && errorProjectKey(p) == want {
				v.Scope = i + 1
			}
		}
	}
	t.showLinkLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	// The list waited for the tied project to be known; with the chips
	// only a help, a failure to read them still lists every project.
	if want != "" {
		t.reloadErrors()
	}
}

// reloadErrors asks for the list as the view says, and shows the answer if
// nothing was asked since.
func (t *tui) reloadErrors() {
	t.mu.Lock()
	v := t.errors
	c := t.errorsClientLocked()
	if v == nil || c == nil {
		t.mu.Unlock()
		return
	}
	t.errorState.seq++
	seq := t.errorState.seq
	var projects []glitchtip.Project
	if v.Scope > 0 && v.Scope <= len(t.errorState.projects) {
		projects = []glitchtip.Project{t.errorState.projects[v.Scope-1]}
	}
	status := glitchtip.Statuses[max(min(v.Filter, len(glitchtip.Statuses)-1), 0)]
	query := v.Query
	v.Loading = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		items, err := c.Issues(projects, status, query)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.errors
		if v == nil || seq != t.errorState.seq {
			return
		}
		v.Loading, v.Now = false, time.Now()
		if err != nil && len(items) == 0 {
			v.Error, v.Errors, t.errorState.items = err.Error(), nil, nil
			return
		}
		v.Error = ""
		if err != nil {
			v.Message = "some of it could not be read: " + err.Error()
		}
		var at string
		if v.Cursor < len(v.Errors) {
			at = v.Errors[v.Cursor].ID
		}
		t.errorState.items = items
		v.Errors, v.Cursor = v.Errors[:0], 0
		for n, i := range items {
			v.Errors = append(v.Errors, errorEntry(i))
			if i.ID == at {
				v.Cursor = n
			}
		}
		v.Scroll = ui.ErrorsScrollFor(v, t.cols, t.rows)
	}()
}

func errorEntry(i glitchtip.Issue) ui.ErrorEntry {
	return ui.ErrorEntry{ID: i.ID, ShortID: i.ShortID, Org: i.Org, Project: i.Project, Title: i.Title,
		Culprit: i.Culprit, Level: i.Level, Status: i.Status, Count: i.Count, Users: i.Users,
		FirstSeen: i.FirstSeen, LastSeen: i.LastSeen, URL: i.URL}
}

// errorsInput is every key and click while the panel is up.
func (t *tui) errorsInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		t.mu.Lock()
		v, cols, rows := t.errors, t.cols, t.rows
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
				t.scrollError(by)
			} else {
				t.moveErrorCursor(by)
			}
			continue
		}
		if ev.Kind != ui.MousePress || ev.Button != 0 {
			continue
		}
		if id, ok := ui.ErrorsAt(v, cols, rows, ev.X, ev.Y); ok {
			if t.errorsAction(id) {
				return nil
			}
		}
	}
	for _, key := range splitKeys(forward) {
		t.mu.Lock()
		v := t.errors
		connecting := v != nil && v.Connect != nil
		managing := v != nil && v.Manage != nil
		connected := v != nil && v.Connected
		detail := v != nil && v.Detail != nil
		t.mu.Unlock()
		if v == nil {
			return nil
		}
		var done bool
		switch {
		case connecting:
			t.errorsConnectKey(key)
		case managing:
			t.errorsManageKey(key)
		case !connected:
			switch key {
			case "\r", "\n":
				t.openErrorsConnect()
			case "\x1b", "q":
				t.closeErrors()
				done = true
			}
		case detail:
			done = t.errorDetailKey(key)
		default:
			done = t.errorListKey(key)
		}
		if done {
			return nil
		}
	}
	t.wakeUp()
	return nil
}

// errorsAction does what a click on a part of the panel is for; it reports
// whether the panel went.
func (t *tui) errorsAction(id string) bool {
	var n int
	switch {
	case strings.HasPrefix(id, "source:"):
		fmt.Sscan(id[len("source:"):], &n)
		t.useErrorSource(n)
		return false
	case strings.HasPrefix(id, "server:"):
		fmt.Sscan(id[len("server:"):], &n)
		t.mu.Lock()
		again := false
		if v := t.errors; v != nil && v.Manage != nil {
			m := v.Manage
			again = m.Cursor == n && !m.Confirm
			m.Cursor, m.Confirm = n, false
		}
		t.dirty = true
		t.mu.Unlock()
		if again {
			t.errorsManageKey("\r")
		}
		return false
	case strings.HasPrefix(id, "scope:"):
		fmt.Sscan(id[len("scope:"):], &n)
		t.mu.Lock()
		if t.errors != nil {
			t.errors.Scope, t.errors.Cursor, t.errors.Scroll = n, 0, 0
			t.showLinkLocked()
		}
		t.mu.Unlock()
		t.reloadErrors()
		return false
	case strings.HasPrefix(id, "filter:"):
		fmt.Sscan(id[len("filter:"):], &n)
		t.mu.Lock()
		if t.errors != nil {
			t.errors.Filter, t.errors.Cursor, t.errors.Scroll = n, 0, 0
		}
		t.mu.Unlock()
		t.reloadErrors()
		return false
	case strings.HasPrefix(id, "row:"):
		fmt.Sscan(id[len("row:"):], &n)
		t.mu.Lock()
		again := t.errors != nil && t.errors.Cursor == n
		if t.errors != nil {
			t.errors.Cursor = n
		}
		t.dirty = true
		t.mu.Unlock()
		if again {
			t.openError()
		}
		return false
	}
	switch id {
	case ui.ErrorsClose:
		t.closeErrors()
		return true
	case ui.ErrorsConnectB:
		t.openErrorsConnect()
	case ui.ErrorsSettings:
		t.openErrorsManage()
	case ui.ErrorsLink:
		t.toggleErrorLink()
	case ui.ErrorsUse:
		t.errorsManageKey("\r")
	case ui.ErrorsAdd:
		t.errorsManageKey("a")
	case ui.ErrorsRemove:
		t.errorsManageKey("d")
	case ui.ErrorsManageEnd:
		t.errorsManageKey("\x1b")
	case ui.ErrorsSave:
		t.saveErrorsConnect()
	case ui.ErrorsCancel:
		t.mu.Lock()
		if t.errors != nil {
			t.errors.Connect = nil
		}
		t.dirty = true
		t.mu.Unlock()
	case "url", "token":
		t.mu.Lock()
		if t.errors != nil && t.errors.Connect != nil {
			t.errors.Connect.InToken = id == "token"
		}
		t.dirty = true
		t.mu.Unlock()
	case ui.ErrorsOpen:
		t.openError()
	case ui.ErrorsBack:
		t.errorDetailKey("\x1b")
	case ui.ErrorsFix:
		return t.fixError()
	case ui.ErrorsWork:
		return t.fixErrorInWorktree()
	case ui.ErrorsResolve:
		t.setErrorStatus(glitchtip.StatusResolved)
	case ui.ErrorsIgnore:
		t.setErrorStatus(glitchtip.StatusIgnored)
	case ui.ErrorsReopen:
		t.setErrorStatus(glitchtip.StatusUnresolved)
	case ui.ErrorsBrowser:
		t.openErrorInBrowser()
	}
	return false
}

// errorListKey is a key over the list; it reports whether the panel went.
func (t *tui) errorListKey(key string) bool {
	switch key {
	case "\x1b":
		t.mu.Lock()
		v := t.errors
		if v != nil && v.Query != "" {
			v.Query = ""
			t.mu.Unlock()
			t.reloadErrors()
			return false
		}
		t.mu.Unlock()
		t.closeErrors()
		return true
	case "\r", "\n":
		t.openError()
	case "\x1b[A", "\x10":
		t.moveErrorCursor(-1)
	case "\x1b[B", "\x0e":
		t.moveErrorCursor(1)
	case "\x1b[5~":
		t.moveErrorCursor(-10)
	case "\x1b[6~":
		t.moveErrorCursor(10)
	case "\t", "\x1b[Z":
		t.mu.Lock()
		if v := t.errors; v != nil {
			step := 1
			if key == "\x1b[Z" {
				step = len(v.Filters) - 1
			}
			v.Filter, v.Cursor, v.Scroll = (v.Filter+step)%len(v.Filters), 0, 0
		}
		t.mu.Unlock()
		t.reloadErrors()
	case "\x14": // ctrl+t: the next project
		t.mu.Lock()
		if v := t.errors; v != nil && len(v.Scopes) > 0 {
			v.Scope, v.Cursor, v.Scroll = (v.Scope+1)%len(v.Scopes), 0, 0
			t.showLinkLocked()
		}
		t.mu.Unlock()
		t.reloadErrors()
	case "\x0f": // ctrl+o
		t.openErrorInBrowser()
	case "\x12": // ctrl+r
		t.reloadErrors()
	case "\x0b": // ctrl+k: the servers kept
		t.openErrorsManage()
	case "\x0c": // ctrl+l: tie the project folder to what is shown
		t.toggleErrorLink()
	case "\x07": // ctrl+g: the next server
		t.mu.Lock()
		next := -1
		if v := t.errors; v != nil && len(v.Sources) > 1 {
			next = (v.Source + 1) % len(v.Sources)
		}
		t.mu.Unlock()
		t.useErrorSource(next)
	case "\x06": // ctrl+f: fix the one under the cursor
		return t.fixError()
	case "\x18": // ctrl+x: resolve the one under the cursor — or, listing
		// the resolved or ignored, open it again
		t.mu.Lock()
		status := glitchtip.StatusResolved
		if t.errors != nil && t.errors.Filter != 0 {
			status = glitchtip.StatusUnresolved
		}
		t.mu.Unlock()
		t.setErrorStatus(status)
	case "\x15":
		t.editErrorQuery(func(string) string { return "" })
	case "\x7f", "\x08":
		t.editErrorQuery(func(q string) string {
			_, size := utf8.DecodeLastRuneInString(q)
			return q[:len(q)-size]
		})
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			t.editErrorQuery(func(q string) string { return q + key })
		}
	}
	return false
}

// editErrorQuery changes what is typed, and asks GlitchTip once typing
// stops, as the issues panel asks GitHub.
func (t *tui) editErrorQuery(edit func(string) string) {
	t.mu.Lock()
	v := t.errors
	if v == nil {
		t.mu.Unlock()
		return
	}
	v.Query = edit(v.Query)
	t.errorState.seq++
	seq := t.errorState.seq
	t.dirty = true
	t.mu.Unlock()
	time.AfterFunc(issueSearchDelay, func() {
		t.mu.Lock()
		current := seq == t.errorState.seq
		t.mu.Unlock()
		if current {
			t.reloadErrors()
		}
	})
}

// errorDetailKey is a key over an open error.
func (t *tui) errorDetailKey(key string) bool {
	switch key {
	case "\x1b", "q":
		t.mu.Lock()
		if v := t.errors; v != nil {
			v.Detail, v.Message = nil, ""
		}
		t.errorState.event = nil
		t.errorState.detailSeq++
		t.dirty = true
		t.mu.Unlock()
	case "\x1b[A", "k":
		t.scrollError(-1)
	case "\x1b[B", "j":
		t.scrollError(1)
	case "\x1b[5~", "\x15":
		t.scrollError(-10)
	case "\x1b[6~", "\x04", " ":
		t.scrollError(10)
	case "g":
		t.scrollError(-1 << 20)
	case "G":
		t.scrollError(1 << 20)
	case "f":
		return t.fixError()
	case "w":
		return t.fixErrorInWorktree()
	case "c":
		t.errorToContext()
	case "r":
		t.setErrorStatus(glitchtip.StatusResolved)
	case "i":
		t.setErrorStatus(glitchtip.StatusIgnored)
	case "u":
		t.setErrorStatus(glitchtip.StatusUnresolved)
	case "o", "\x0f":
		t.openErrorInBrowser()
	}
	return false
}

func (t *tui) moveErrorCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.errors
	if v == nil || len(v.Errors) == 0 {
		return
	}
	v.Cursor = max(min(v.Cursor+by, len(v.Errors)-1), 0)
	v.Scroll = ui.ErrorsScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

func (t *tui) scrollError(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.errors
	if v == nil || v.Detail == nil {
		return
	}
	v.Detail.Scroll = max(v.Detail.Scroll+by, 0)
	v.Detail.Scroll = ui.ClampErrorScroll(v, t.cols, t.rows, t.theme)
	t.dirty = true
}

// currentErrorLocked is the error the panel is on: the one open, else the
// one under the cursor.
func (t *tui) currentErrorLocked() (glitchtip.Issue, bool) {
	v := t.errors
	if v == nil {
		return glitchtip.Issue{}, false
	}
	id := ""
	switch {
	case v.Detail != nil:
		id = v.Detail.ID
	case v.Cursor < len(v.Errors):
		id = v.Errors[v.Cursor].ID
	}
	for _, i := range t.errorState.items {
		if i.ID == id {
			return i, true
		}
	}
	return glitchtip.Issue{}, false
}

// openError opens the error under the cursor, and reads its latest event.
func (t *tui) openError() {
	t.mu.Lock()
	v := t.errors
	issue, ok := t.currentErrorLocked()
	c := t.errorsClientLocked()
	if v == nil || !ok || c == nil {
		t.mu.Unlock()
		return
	}
	v.Detail = &ui.ErrorDetailView{ErrorEntry: errorEntry(issue), Loading: true}
	v.Message = issue.URL
	t.errorState.event = nil
	t.errorState.detailSeq++
	seq := t.errorState.detailSeq
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	go func() {
		e, err := c.Latest(issue.Org, issue.ID)
		t.mu.Lock()
		defer func() {
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
		}()
		v := t.errors
		if v == nil || v.Detail == nil || seq != t.errorState.detailSeq {
			return
		}
		d := v.Detail
		d.Loading = false
		if err != nil {
			v.Message = "could not read its latest event: " + err.Error()
			return
		}
		t.errorState.event = &e
		d.Request, d.Release, d.Tags = e.Request, e.Release, e.Tags
		for _, ex := range e.Exceptions {
			out := ui.ErrorException{Type: ex.Type, Value: ex.Value}
			for _, f := range ex.Frames {
				out.Frames = append(out.Frames, ui.ErrorFrame{File: f.File, Line: f.Line, Function: f.Function, InApp: f.InApp, Code: f.Code})
			}
			d.Exceptions = append(d.Exceptions, out)
		}
		for _, cr := range e.Crumbs {
			line := cr.Message
			if cr.Category != "" {
				line = cr.Category + ": " + line
			}
			if !cr.Time.IsZero() {
				line = cr.Time.Local().Format("15:04:05") + "  " + line
			}
			d.Crumbs = append(d.Crumbs, line)
		}
	}()
}

// errorPrompt is what an agent is told to fix the current error with; the
// event is read first when it has not been.
func (t *tui) errorPrompt() (glitchtip.Issue, string, error) {
	t.mu.Lock()
	issue, ok := t.currentErrorLocked()
	c := t.errorsClientLocked()
	var event *glitchtip.Event
	if t.errors != nil && t.errors.Detail != nil {
		event = t.errorState.event
	}
	t.mu.Unlock()
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

// errorSay puts a line under the panel.
func (t *tui) errorSay(message string) {
	t.mu.Lock()
	if t.errors != nil {
		t.errors.Message = message
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// errorItem is the context item an error goes as.
func errorItem(issue glitchtip.Issue, prompt string) proto.ContextItem {
	return errorItemText("glitchtip", issue.ShortID+" "+issue.Title, issue.URL, prompt)
}

// errorItemText is the context item for what an agent is handed to fix, an
// error or a ticket.
func errorItemText(source, title, url, prompt string) proto.ContextItem {
	return proto.ContextItem{Kind: capture.KindText, Source: source, Title: title, URL: url, Text: prompt}
}

// fixError types the error, with its stack and what to do, into the pane
// the panel was opened from — the agent working on that project — through
// the context, which makes it inert and submits nothing: the user reads it
// and sends it. It reports whether the panel went.
func (t *tui) fixError() bool {
	t.mu.Lock()
	pane := t.errorState.pane
	t.mu.Unlock()
	t.errorSay("reading the error…")
	go func() {
		issue, prompt, err := t.errorPrompt()
		if err != nil {
			t.errorSay(err.Error())
			return
		}
		item, err := t.client.ContextAdd(errorItem(issue, prompt))
		if err == nil {
			err = t.client.ContextSend(pane, []uint64{item.ID})
		}
		if err != nil {
			t.errorSay("could not hand it over: " + err.Error())
			return
		}
		t.closeErrors()
		_ = t.jumpToPane(pane)
		t.setMessage(issue.ShortID+" typed into the pane — read it over and press enter to send", false)
	}()
	return false
}

// errorToContext puts the error in the context, for the context panel to
// send or copy.
func (t *tui) errorToContext() {
	go func() {
		issue, prompt, err := t.errorPrompt()
		if err == nil {
			_, err = t.client.ContextAdd(errorItem(issue, prompt))
		}
		if err != nil {
			t.errorSay("could not put it in the context: " + err.Error())
			return
		}
		t.errorSay(issue.ShortID + " is in the context (prefix+C)")
	}()
}

// fixErrorInWorktree is the issues panel's start work, for an error: a
// worktree of the space's project on fix-<its short id>, opened as a space,
// and the agent started there with the error as its prompt; an error with a
// worktree already is gone to. It reports whether the panel went.
func (t *tui) fixErrorInWorktree() bool {
	ws := t.shownWorkspace()
	t.mu.Lock()
	session := t.session
	agentName := t.agentForLocked(t.errorState.pane)
	t.mu.Unlock()
	t.errorSay("making a worktree…")
	go func() {
		issue, prompt, err := t.errorPrompt()
		if err != nil {
			t.errorSay(err.Error())
			return
		}
		branch := "fix-" + worktree.Slug(issue.ShortID)
		wsID, tab, message, err := t.errorWorktree(session, ws, branch, issue.ShortID, agentName, prompt)
		if err != nil {
			t.errorSay(err.Error())
			return
		}
		t.closeErrors()
		t.mu.Lock()
		t.rememberFocusLocked()
		t.workspace, t.tab, t.focus, t.zoom = wsID, tab, 0, false
		t.mu.Unlock()
		t.setMessage(message, false)
		if err := t.refresh(); err != nil {
			t.setMessage(err.Error(), true)
		}
	}()
	return false
}

// errorWorktree makes, or finds, the worktree for an error — or a ticket,
// named by label — in the space's repository, and starts the agent there
// when it is new.
func (t *tui) errorWorktree(session string, ws uint64, branch, label, agentName, prompt string) (uint64, uint64, string, error) {
	where := map[string]any{"workspace_id": api.WorkspaceID(sessionpkg.WorkspaceID(ws))}
	list, err := apiCall(session, api.MethodWorktreeList, where, true)
	if err != nil {
		return 0, 0, "", fmt.Errorf("this space is not a repository to fix it in: %w", err)
	}
	trees, _ := list["worktrees"].([]any)
	for _, raw := range trees {
		wt, _ := raw.(map[string]any)
		if b, _ := wt["branch"].(string); b == branch {
			path, _ := wt["path"].(string)
			opened, err := apiCall(session, api.MethodWorktreeOpen, map[string]any{"workspace_id": where["workspace_id"], "path": path}, true)
			if err != nil {
				return 0, 0, "", err
			}
			return workspaceOf(opened), 0, label + " already has a worktree: " + branch, nil
		}
	}
	created, err := apiCall(session, api.MethodWorktreeCreate, map[string]any{
		"workspace_id": where["workspace_id"], "branch": branch, "label": label,
	}, true)
	if err != nil {
		return 0, 0, "", err
	}
	wsID := workspaceOf(created)
	wt, _ := created["worktree"].(map[string]any)
	path, _ := wt["path"].(string)
	if wsID == 0 || path == "" {
		return 0, 0, "", fmt.Errorf("the worktree was made, but tend could not tell where")
	}
	argv := append(append([]string{}, issueAgents[agentName]...), prompt)
	tab, _, err := t.client.NewTab(wsID, label+" "+agentName, proto.PaneSpec{
		Command: append([]string{"/bin/sh", "-c", resumeScript, "tend-fix"}, argv...),
		Dir:     path,
		Agent:   agentName,
	})
	if err != nil {
		return wsID, 0, "", fmt.Errorf("the worktree %s is made, but %s did not start: %w", branch, agentName, err)
	}
	return wsID, tab, fmt.Sprintf("%s: %s on %s", label, agentName, branch), nil
}

// setErrorStatus marks the current error on the server, and reads the list
// again.
func (t *tui) setErrorStatus(status string) {
	t.mu.Lock()
	issue, ok := t.currentErrorLocked()
	c := t.errorsClientLocked()
	t.mu.Unlock()
	if !ok || c == nil {
		return
	}
	t.errorSay("asking GlitchTip…")
	go func() {
		if err := c.SetStatus(issue.Org, issue.ID, status); err != nil {
			t.errorSay("GlitchTip said: " + err.Error())
			return
		}
		t.mu.Lock()
		if v := t.errors; v != nil && v.Detail != nil && v.Detail.ID == issue.ID {
			v.Detail.Status = status
		}
		t.mu.Unlock()
		t.errorSay(issue.ShortID + " is " + status)
		t.reloadErrors()
	}()
}

func (t *tui) openErrorInBrowser() {
	t.mu.Lock()
	issue, ok := t.currentErrorLocked()
	t.mu.Unlock()
	if !ok {
		return
	}
	message := "opened " + issue.URL
	if err := openURL(issue.URL); err != nil {
		message = issue.URL + " — " + err.Error()
	}
	t.errorSay(message)
}

// openErrorsConnect opens the connect box for a server to add. A token is
// never shown back: to give a kept server a new one, its address is typed
// again with the new token, which replaces the old.
func (t *tui) openErrorsConnect() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.errors == nil {
		return
	}
	t.errors.Connect = &ui.ErrorsConnect{URL: "https://"}
	t.dirty = true
}

// openErrorsManage opens the servers box — or, with none kept, the connect
// box, since there is nothing to manage.
func (t *tui) openErrorsManage() {
	t.mu.Lock()
	v := t.errors
	if v == nil {
		t.mu.Unlock()
		return
	}
	if len(t.config.ErrorSources()) == 0 {
		t.mu.Unlock()
		t.openErrorsConnect()
		return
	}
	_, at, _ := t.errorSourceLocked()
	v.Manage = &ui.ErrorsManage{Cursor: at}
	t.showSourceLocked()
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// errorsManageKey is a key in the servers box: enter shows the server under
// the cursor, a adds one, d removes it after enter confirms, esc closes.
func (t *tui) errorsManageKey(key string) {
	t.mu.Lock()
	v := t.errors
	if v == nil || v.Manage == nil {
		t.mu.Unlock()
		return
	}
	m := v.Manage
	t.dirty = true
	if m.Confirm {
		m.Confirm = false
		if key == "\r" || key == "\n" {
			sources := t.config.ErrorSources()
			if m.Cursor < len(sources) {
				src := sources[m.Cursor]
				t.mu.Unlock()
				t.removeErrorSource(src)
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
		t.useErrorSource(at)
		return
	case "a":
		t.mu.Unlock()
		t.openErrorsConnect()
		return
	case "d", "\x1b[3~":
		if len(m.Servers) > 0 {
			m.Confirm, m.Message = true, ""
		}
	}
	t.mu.Unlock()
	t.wakeUp()
}

// removeErrorSource forgets a server and its token: its own table in the
// settings file goes, or, for the one kept in [errors], its keys are
// emptied. The panel shows the next one kept, or offers to connect one.
func (t *tui) removeErrorSource(src config.NamedErrorSource) {
	go func() {
		var err error
		if name, ok := keyring.Name(src.Token); ok {
			// Forgotten from the keyring too; a keyring that cannot be
			// asked still lets the server go from the settings.
			_ = keyring.Delete(name)
		}
		if src.Legacy {
			if err = config.Set("errors", "url", config.Quote("")); err == nil {
				err = config.Set("errors", "token", config.Quote(""))
			}
		} else {
			err = config.RemoveSection("errors.sources." + src.Name)
		}
		t.mu.Lock()
		v := t.errors
		if err != nil {
			if v != nil && v.Manage != nil {
				v.Manage.Message = "could not remove it: " + err.Error()
			}
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		if src.Legacy {
			t.config.Errors.URL, t.config.Errors.Token = "", ""
		} else {
			delete(t.config.Errors.Sources, src.Name)
		}
		shown := t.config.Errors.Source == src.Name
		if v != nil {
			if len(t.config.ErrorSources()) == 0 {
				v.Manage = nil
			} else if v.Manage != nil {
				v.Manage.Message = "removed " + hostOf(src.URL)
			}
		}
		t.mu.Unlock()
		if !shown {
			t.mu.Lock()
			t.showSourceLocked()
			t.dirty = true
			t.mu.Unlock()
			t.wakeUp()
			return
		}
		// The one shown went: show the first left, which also forgets
		// what was read from the one removed.
		t.mu.Lock()
		t.config.Errors.Source = ""
		t.mu.Unlock()
		t.useErrorSource(0)
		t.mu.Lock()
		if v := t.errors; v != nil {
			t.showSourceLocked()
			if !v.Connected {
				v.Errors, v.Scopes, v.Message = nil, nil, "removed "+hostOf(src.URL)+"; no server is kept"
			}
		}
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
	}()
}

// errorsConnectKey is a key in the connect box: typing goes to the field,
// tab moves between them, enter tests and saves, esc gives up.
func (t *tui) errorsConnectKey(key string) {
	t.mu.Lock()
	v := t.errors
	if v == nil || v.Connect == nil {
		t.mu.Unlock()
		return
	}
	c := v.Connect
	if c.Testing {
		t.mu.Unlock()
		return
	}
	field := &c.URL
	if c.InToken {
		field = &c.Token
	}
	save := false
	switch key {
	case "\x1b":
		v.Connect = nil
	case "\t", "\x1b[Z", "\x1b[A", "\x1b[B":
		c.InToken = !c.InToken
	case "\r", "\n":
		if c.InToken || c.Token != "" {
			save = true
		} else {
			c.InToken = true
		}
	case "\x7f", "\x08":
		_, size := utf8.DecodeLastRuneInString(*field)
		*field = (*field)[:len(*field)-size]
	case "\x15":
		*field = ""
	default:
		if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f {
			*field += key
		}
	}
	c.Error = ""
	t.dirty = true
	t.mu.Unlock()
	if save {
		t.saveErrorsConnect()
	}
}

// newErrorSourceLocked is where a server typed into the connect box is
// kept: the source that already has its address, whose token is replaced,
// or a new one named after its host, made unique.
func (t *tui) newErrorSourceLocked(url string) config.NamedErrorSource {
	for _, s := range t.config.ErrorSources() {
		if strings.TrimRight(s.URL, "/") == url {
			return s
		}
	}
	if t.config.Errors.URL != "" && strings.TrimRight(t.config.Errors.URL, "/") == url {
		// Kept in [errors] with no token yet (TEND_GLITCHTIP_TOKEN unset).
		return config.NamedErrorSource{Name: config.ErrorSourceName(url), Legacy: true}
	}
	base := config.ErrorSourceName(url)
	name := base
	taken := func(n string) bool {
		if _, ok := t.config.Errors.Sources[n]; ok {
			return true
		}
		return t.config.Errors.URL != "" && config.ErrorSourceName(t.config.Errors.URL) == n
	}
	for i := 2; taken(name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return config.NamedErrorSource{Name: name}
}

// saveErrorsConnect tries the server and token, and only when the server
// takes them writes them to the settings file and lists its errors: a token
// mistyped is said in the box, and nothing is kept.
func (t *tui) saveErrorsConnect() {
	t.mu.Lock()
	v := t.errors
	if v == nil || v.Connect == nil {
		t.mu.Unlock()
		return
	}
	c := v.Connect
	url, token := strings.TrimRight(strings.TrimSpace(c.URL), "/"), strings.TrimSpace(c.Token)
	if url == "" || url == "https:" || url == "http:" || token == "" {
		c.Error = "a server and a token, both"
		t.dirty = true
		t.mu.Unlock()
		return
	}
	c.Testing = true
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	t.mu.Lock()
	src := t.newErrorSourceLocked(url)
	t.mu.Unlock()
	go func() {
		orgs, err := glitchtip.New(url, token).Orgs()
		// The token goes to the system keyring when there is one, and the
		// settings file names it there; else it is written to the file,
		// readable by its owner alone.
		value, kept := token, ""
		if err == nil && keyring.Available() {
			kept = "glitchtip:" + src.Name
			if kerr := keyring.Set(kept, token); kerr == nil {
				value = keyring.Ref(kept)
			} else {
				kept = ""
			}
		}
		if err == nil {
			if src.Legacy {
				err = config.Set("errors", "token", config.Quote(value))
			} else {
				section := "errors.sources." + src.Name
				if err = config.Set(section, "url", config.Quote(url)); err == nil {
					err = config.Set(section, "token", config.Quote(value))
				}
			}
			if err == nil {
				err = config.Set("errors", "source", config.Quote(src.Name))
			}
		}
		t.mu.Lock()
		v := t.errors
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
			if t.errorState.tokens == nil {
				t.errorState.tokens = map[string]string{}
			}
			t.errorState.tokens[kept] = token
		}
		if src.Legacy {
			t.config.Errors.Token = value
		} else {
			if t.config.Errors.Sources == nil {
				t.config.Errors.Sources = map[string]config.ErrorSource{}
			}
			old := t.config.Errors.Sources[src.Name]
			t.config.Errors.Sources[src.Name] = config.ErrorSource{URL: url, Token: value, Projects: old.Projects}
		}
		v.Connect, v.Manage = nil, nil
		sources := t.config.ErrorSources()
		at := 0
		for i, s := range sources {
			if s.Name == src.Name {
				at = i
			}
		}
		t.mu.Unlock()
		t.useErrorSource(at)
		t.errorSay(fmt.Sprintf("connected to %s: %d organizations", hostOf(url), len(orgs)))
	}()
}
