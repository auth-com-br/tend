package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/proto"
	"github.com/auth-com-br/tend/internal/transport"
	"github.com/auth-com-br/tend/internal/ui"
)

// Companies (internal/ui/companies.go, internal/session/company.go) are
// herdr's user-defined Spaces: named sets of spaces, kept by the server so
// every client sees the same ones. Which one is chosen is this client's —
// what one person is looking at — and while one is, the sidebar lists its
// spaces and their agents only, next and previous space stay among them,
// and a new space goes into it. Going to a space outside it, from the
// navigator or an agent's notice, chooses a company that has it, or all of
// them: somewhere the user has just gone to is never hidden.

// companyFile is where this client keeps the company it last chose for a
// session, so tend opens where it was left. One file per session: the ids
// are the session's own.
func companyFile(session string) string {
	dir, err := transport.StateDir()
	if err != nil || session == "" {
		return ""
	}
	return filepath.Join(dir, "company-"+session)
}

func loadCompany(session string) uint64 {
	path := companyFile(session)
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	id, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return id
}

// saveCompany writes the choice down. Failing to is not worth telling the
// user about: the choice still holds until tend is closed.
func saveCompany(session string, id uint64) {
	path := companyFile(session)
	if path == "" {
		return
	}
	if id == 0 {
		_ = os.Remove(path)
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(strconv.FormatUint(id, 10)+"\n"), 0o600)
}

// companySpacesFile is where this client keeps the space last shown in
// each company of a session: one "company space" pair a line.
func companySpacesFile(session string) string {
	dir, err := transport.StateDir()
	if err != nil || session == "" {
		return ""
	}
	return filepath.Join(dir, "company-spaces-"+session)
}

func loadCompanySpaces(session string) map[uint64]uint64 {
	out := make(map[uint64]uint64)
	path := companySpacesFile(session)
	if path == "" {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		c, err1 := strconv.ParseUint(f[0], 10, 64)
		w, err2 := strconv.ParseUint(f[1], 10, 64)
		if err1 == nil && err2 == nil && w != 0 {
			out[c] = w
		}
	}
	return out
}

// saveCompanySpaces writes the spaces down; as with the company chosen, a
// failure is not worth telling the user about.
func saveCompanySpaces(session string, spaces map[uint64]uint64) {
	path := companySpacesFile(session)
	if path == "" {
		return
	}
	ids := make([]uint64, 0, len(spaces))
	for c := range spaces {
		ids = append(ids, c)
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, c := range ids {
		b.WriteString(strconv.FormatUint(c, 10) + " " + strconv.FormatUint(spaces[c], 10) + "\n")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(b.String()), 0o600)
}

// rememberCompanySpaceLocked notes the space being shown as the one to come
// back to in the company chosen, before the company changes.
func (t *tui) rememberCompanySpaceLocked() {
	if t.workspace == 0 || !t.shownLocked(t.workspace) {
		return
	}
	if t.companySpace == nil {
		t.companySpace = make(map[uint64]uint64)
	}
	if t.companySpace[t.company] == t.workspace {
		return
	}
	t.companySpace[t.company] = t.workspace
	saveCompanySpaces(t.session, t.companySpace)
}

// rememberedSpaceLocked is the space last shown in the company chosen, when
// it still exists and is still in the company; zero otherwise.
func (t *tui) rememberedSpaceLocked() uint64 {
	ws := t.companySpace[t.company]
	if ws == 0 || !t.shownLocked(ws) {
		return 0
	}
	for _, w := range t.snap.Workspaces {
		if w.ID == ws {
			return ws
		}
	}
	return 0
}

// companyLocked is the company chosen, when it still exists.
func (t *tui) companyLocked() (proto.CompanyInfo, bool) {
	if t.company == 0 {
		return proto.CompanyInfo{}, false
	}
	for _, c := range t.snap.Companies {
		if c.ID == t.company {
			return c, true
		}
	}
	return proto.CompanyInfo{}, false
}

// shownLocked reports whether a space of the session being shown is in the
// list: every one when no company is chosen, the company's otherwise.
func (t *tui) shownLocked(ws uint64) bool {
	c, ok := t.companyLocked()
	return !ok || slices.Contains(c.Workspaces, ws)
}

// syncCompaniesLocked settles the choice against a new snapshot: a company
// deleted, here or by another client, stops being chosen, and a panel that
// is up shows what is there now. It is called before the view is resolved,
// so a view that must move lands inside the company.
func (t *tui) syncCompaniesLocked() {
	if !t.companyLoaded {
		t.company, t.companyLoaded = loadCompany(t.session), true
		t.companySpace = loadCompanySpaces(t.session)
	}
	if _, ok := t.companyLocked(); !ok && t.company != 0 && len(t.snap.Workspaces) > 0 {
		// Only once there is a session to judge by: an empty snapshot
		// before the first one arrives must not throw the choice away.
		t.company = 0
		saveCompany(t.session, 0)
	}
	if t.companies != nil {
		t.fillCompaniesLocked()
	}
}

// revealCompanyLocked makes a space about to be shown one the list shows:
// the chosen company when it has it, else the first that does, else all.
func (t *tui) revealCompanyLocked(ws uint64) {
	if ws == 0 || t.shownLocked(ws) {
		return
	}
	next := uint64(0)
	for _, c := range t.snap.Companies {
		if slices.Contains(c.Workspaces, ws) {
			next = c.ID
			break
		}
	}
	t.rememberCompanySpaceLocked()
	t.company = next
	saveCompany(t.session, next)
}

// landingSpaceLocked is where choosing the company chosen goes, zero to
// stay: the space last shown in it, so going back to a company lands where it
// was left; else, when the space being shown is not in it, its first space in
// the session's order, which is the order the list shows.
func (t *tui) landingSpaceLocked() uint64 {
	if ws := t.rememberedSpaceLocked(); ws != 0 {
		if ws == t.workspace {
			return 0
		}
		return ws
	}
	if t.shownLocked(t.workspace) {
		return 0
	}
	c, _ := t.companyLocked()
	for _, w := range t.snap.Workspaces {
		if slices.Contains(c.Workspaces, w.ID) {
			return w.ID
		}
	}
	return 0
}

// chooseCompany shows a company's spaces, or every space for zero, and goes
// to the space last shown in it, or to one of them when the space being shown
// is not.
func (t *tui) chooseCompany(id uint64) error {
	t.mu.Lock()
	t.rememberCompanySpaceLocked()
	t.company = id
	saveCompany(t.session, id)
	t.spacesScroll, t.agentsScroll = 0, 0
	move := t.landingSpaceLocked()
	if move != 0 {
		t.rememberFocusLocked()
		t.workspace, t.tab, t.focus, t.zoom = move, 0, 0, false
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	if move != 0 {
		return t.refresh()
	}
	return nil
}

func (t *tui) companiesUp() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.companies != nil
}

// openCompanies puts the panel up, the cursor on the company chosen.
func (t *tui) openCompanies() error {
	t.mu.Lock()
	if t.companies == nil {
		t.companies = &ui.CompaniesView{}
		if !t.client.Supports(proto.MethodCompanyCreate) {
			t.companies.Message = "this server is older than companies; " + handoffCommand(t.session) + " moves it to this build"
		}
	}
	t.fillCompaniesLocked()
	v := t.companies
	for i, e := range v.Entries {
		if e.ID == t.company {
			v.Cursor = i
		}
	}
	v.Scroll = ui.CompaniesScrollFor(v, t.cols, t.rows)
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
	return nil
}

func (t *tui) closeCompanies() {
	t.mu.Lock()
	t.companies = nil
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}

// fillCompaniesLocked lays the panel's lists out from the snapshot,
// keeping the cursors on what they were on.
func (t *tui) fillCompaniesLocked() {
	v := t.companies
	var at uint64
	if v.Cursor < len(v.Entries) {
		at = v.Entries[v.Cursor].ID
	}
	waiting := map[uint64]int{}
	info := paneInfo(&t.snap)
	for _, w := range t.snap.Workspaces {
		waiting[w.ID] = waitingIn(w, info)
	}
	all := ui.CompanyEntry{Spaces: len(t.snap.Workspaces)}
	for _, n := range waiting {
		all.Waiting += n
	}
	v.Entries = []ui.CompanyEntry{all}
	for _, c := range t.snap.Companies {
		e := ui.CompanyEntry{ID: c.ID, Name: c.Name, Spaces: len(c.Workspaces)}
		for _, ws := range c.Workspaces {
			e.Waiting += waiting[ws]
		}
		v.Entries = append(v.Entries, e)
	}
	v.Active = t.company
	if _, ok := t.companyLocked(); !ok {
		v.Active = 0
	}
	v.Cursor = 0
	for i, e := range v.Entries {
		if e.ID == at {
			v.Cursor = i
		}
	}

	if v.Choosing == 0 {
		return
	}
	var company proto.CompanyInfo
	found := false
	for _, c := range t.snap.Companies {
		if c.ID == v.Choosing {
			company, found = c, true
		}
	}
	if !found {
		// Deleted while its spaces were being chosen: back to the list.
		v.Choosing, v.Spaces, v.SpaceCursor = 0, nil, 0
		return
	}
	v.ChoosingName = company.Name
	v.Spaces = v.Spaces[:0]
	for _, w := range t.snap.Workspaces {
		detail := w.Branch
		if w.Group != "" {
			detail = strings.TrimSpace(w.Group + " · " + detail)
		}
		v.Spaces = append(v.Spaces, ui.CompanySpace{
			ID: w.ID, Name: orDash(w.Name), Detail: strings.TrimSuffix(detail, " ·"),
			In: slices.Contains(company.Workspaces, w.ID),
		})
	}
	v.SpaceCursor = max(min(v.SpaceCursor, len(v.Spaces)-1), 0)
}

// waitingIn is how many of a space's agents want the user.
func waitingIn(w proto.WorkspaceInfo, info map[uint64]proto.PaneInfo) int {
	n := 0
	for _, tab := range w.Tabs {
		for _, id := range tab.Panes {
			if p := info[id]; p.Agent != "" && p.State == "blocked" {
				n++
			}
		}
	}
	return n
}

// waitingElsewhereLocked is how many agents want the user in spaces the
// list does not show, which the heading says so a company chosen does not
// hide an agent that stopped.
func (t *tui) waitingElsewhereLocked() int {
	if _, ok := t.companyLocked(); !ok {
		return 0
	}
	n := 0
	info := paneInfo(&t.snap)
	for _, w := range t.snap.Workspaces {
		if !t.shownLocked(w.ID) {
			n += waitingIn(w, info)
		}
	}
	return n
}

// companiesInput is every key and click while the panel is up. Typing is
// only for a name, so the list's actions are single letters; esc steps
// back — out of a name or a question, then out of a company's spaces, then
// out of the panel.
func (t *tui) companiesInput(data []byte) error {
	forward, _, mice := t.keys.FeedAll(data)
	for _, ev := range mice {
		if err := t.companiesMouse(ev); err != nil {
			return err
		}
	}
	for _, key := range splitKeys(forward) {
		if err := t.companiesKey(key); err != nil {
			t.companiesSay(err.Error())
		}
	}
	t.wakeUp()
	return nil
}

func (t *tui) companiesMouse(ev ui.MouseEvent) error {
	switch ev.Kind {
	case ui.MouseWheelUp:
		t.moveCompanyCursor(-3)
		return nil
	case ui.MouseWheelDown:
		t.moveCompanyCursor(3)
		return nil
	}
	if ev.Kind != ui.MousePress || ev.Button != 0 {
		return nil
	}
	t.mu.Lock()
	v, cols, rows := t.companies, t.cols, t.rows
	if v == nil {
		t.mu.Unlock()
		return nil
	}
	g := ui.CompaniesLayout(v, cols, rows)
	i, onLine := ui.CompaniesLineAt(v, cols, rows, ev.X, ev.Y)
	choosing := v.Choosing != 0
	t.mu.Unlock()

	if inRect(g.Close, ev.X, ev.Y) || ui.OnCloseMark(g.Box, ev.X, ev.Y) {
		t.closeCompanies()
		return nil
	}
	for b, r := range g.Buttons {
		if !inRect(r, ev.X, ev.Y) {
			continue
		}
		if choosing {
			return t.companiesKey("\x1b")
		}
		return t.companiesKey([]string{"\r", "n", "r", "s", "d"}[b])
	}
	if !onLine {
		return nil
	}
	t.mu.Lock()
	again := false
	if choosing {
		v.SpaceCursor = i
		again = true // a click on a space ticks it, as a checkbox does
	} else {
		again = v.Cursor == i && !v.Confirm && v.Naming == ui.CompanyNamingNone
		v.Cursor, v.Confirm = i, false
	}
	t.dirty = true
	t.mu.Unlock()
	if again {
		// A click on the company already under the cursor switches to it.
		return t.companiesKey("\r")
	}
	return nil
}

func (t *tui) companiesKey(key string) error {
	t.mu.Lock()
	v := t.companies
	if v == nil {
		t.mu.Unlock()
		return nil
	}
	t.dirty = true

	if v.Naming != ui.CompanyNamingNone {
		switch key {
		case "\x1b":
			v.Naming, v.Input = ui.CompanyNamingNone, ""
		case "\r", "\n":
			naming, name := v.Naming, strings.TrimSpace(v.Input)
			var target uint64
			if v.Cursor < len(v.Entries) {
				target = v.Entries[v.Cursor].ID
			}
			v.Naming, v.Input = ui.CompanyNamingNone, ""
			t.mu.Unlock()
			if name == "" {
				return nil
			}
			return t.nameCompany(naming, target, name)
		case "\x7f", "\x08":
			_, size := utf8.DecodeLastRuneInString(v.Input)
			v.Input = v.Input[:len(v.Input)-size]
		case "\x15": // ctrl+u
			v.Input = ""
		default:
			if len(key) == 1 && key[0] >= 0x20 && key[0] != 0x7f && len(v.Input) < 80 {
				// A byte of what was typed; one of several for a letter with
				// an accent, which the next ones complete.
				v.Input += key
			}
		}
		t.mu.Unlock()
		return nil
	}

	if v.Confirm {
		v.Confirm = false
		if key != "\r" && key != "\n" {
			t.mu.Unlock()
			return nil
		}
		id := v.Entries[v.Cursor].ID
		t.mu.Unlock()
		if err := t.client.DeleteCompany(id); err != nil {
			return err
		}
		t.companiesSay("deleted; its spaces are all still there")
		return t.refreshSnapshot()
	}

	if v.Choosing != 0 {
		switch key {
		case "\x1b", "\x7f", "\x08":
			v.Choosing, v.Spaces, v.SpaceCursor = 0, nil, 0
			v.Scroll = ui.CompaniesScrollFor(v, t.cols, t.rows)
		case "\x1b[A", "\x10", "k":
			t.mu.Unlock()
			t.moveCompanyCursor(-1)
			return nil
		case "\x1b[B", "\x0e", "j":
			t.mu.Unlock()
			t.moveCompanyCursor(1)
			return nil
		case " ", "\r", "\n":
			if v.SpaceCursor >= len(v.Spaces) {
				break
			}
			s := v.Spaces[v.SpaceCursor]
			company := v.Choosing
			t.mu.Unlock()
			if err := t.client.AssignCompany(company, s.ID, !s.In); err != nil {
				return err
			}
			return t.refreshSnapshot()
		}
		t.mu.Unlock()
		return nil
	}

	var selected uint64
	if v.Cursor < len(v.Entries) {
		selected = v.Entries[v.Cursor].ID
	}
	switch key {
	case "\x1b", "q":
		t.companies = nil
		t.mu.Unlock()
		return nil
	case "\x1b[A", "\x10", "k":
		t.mu.Unlock()
		t.moveCompanyCursor(-1)
		return nil
	case "\x1b[B", "\x0e", "j":
		t.mu.Unlock()
		t.moveCompanyCursor(1)
		return nil
	case "\r", "\n":
		t.companies = nil
		t.mu.Unlock()
		return t.chooseCompany(selected)
	case "n":
		v.Naming, v.Input, v.Message = ui.CompanyNamingNew, "", ""
	case "r", "s", "d", "\x1b[3~":
		if selected == 0 {
			v.Message = "choose a company first: all spaces is not one"
			break
		}
		switch key {
		case "r":
			v.Naming, v.Input, v.Message = ui.CompanyNamingRename, v.Entries[v.Cursor].Name, ""
		case "s":
			v.Choosing, v.SpaceCursor, v.Scroll, v.Message = selected, 0, 0, ""
			t.fillCompaniesLocked()
		default:
			v.Confirm, v.Message = true, ""
		}
	}
	t.mu.Unlock()
	return nil
}

// nameCompany makes a company or renames one. A new company is chosen at
// once and its spaces offered to be ticked: an empty company chosen would
// only show an empty list.
func (t *tui) nameCompany(naming ui.CompanyNaming, target uint64, name string) error {
	if naming == ui.CompanyNamingRename {
		if err := t.client.RenameCompany(target, name); err != nil {
			return err
		}
		return t.refreshSnapshot()
	}
	id, err := t.client.CreateCompany(name, 0)
	if err != nil {
		return err
	}
	if err := t.refreshSnapshot(); err != nil {
		return err
	}
	t.mu.Lock()
	if v := t.companies; v != nil {
		for i, e := range v.Entries {
			if e.ID == id {
				v.Cursor = i
			}
		}
		v.Choosing, v.SpaceCursor, v.Scroll = id, 0, 0
		v.Message = ""
		t.fillCompaniesLocked()
	}
	t.dirty = true
	t.mu.Unlock()
	return nil
}

func (t *tui) moveCompanyCursor(by int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.companies
	if v == nil {
		return
	}
	if v.Choosing != 0 {
		if len(v.Spaces) > 0 {
			v.SpaceCursor = max(min(v.SpaceCursor+by, len(v.Spaces)-1), 0)
		}
	} else if len(v.Entries) > 0 {
		v.Cursor = max(min(v.Cursor+by, len(v.Entries)-1), 0)
		v.Confirm = false
	}
	v.Scroll = ui.CompaniesScrollFor(v, t.cols, t.rows)
	t.dirty = true
}

// companiesSay puts a line under the list.
func (t *tui) companiesSay(message string) {
	t.mu.Lock()
	if v := t.companies; v != nil {
		v.Message = message
	}
	t.dirty = true
	t.mu.Unlock()
	t.wakeUp()
}
