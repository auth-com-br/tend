// Package explorer is tend's file explorer: a tree of the project with what
// git says about each file, the changes with their diffs, and a search for
// any file by name. It runs as a program in a pane (`tend files`), docked on
// the left of a tab, so it runs on the machine the files are on — a client
// attached over ssh sees the server's project, not the laptop's.
//
// The model is kept apart from the terminal: it takes keys and clicks and
// draws into a grid, and everything it does to the world goes through Git and
// an Opener, so a test drives it without a terminal, a repository daemon or a
// session.
package explorer

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/auth-com-br/tend/internal/vt"
)

// View is which list is shown.
type View int

const (
	ViewFiles View = iota
	// ViewSearch looks for text in the files, as herdr-sidebar's second
	// view does; 1, 2 and 3 are the three views in its order.
	ViewSearch
	ViewChanges
)

// mode is what has the keys: the list, or something over it.
type mode int

const (
	modeList mode = iota
	modeViewer
	modeSearch
	modeCommit
	modeHelp
	modeBranch
	modeMenu
	modePrompt
	modeHistory
	modeSettings
)

// Opener opens a file for editing somewhere other than here: in tend, an
// editor in a tab of its own. line, when above zero, is where to put the
// editor's cursor.
type Opener interface {
	Open(path string, line int) error
}

// Reviewer sends a review of a change to the agent working beside the panel
// (tend's pane.send_text, submitted), preferring the pane the panel follows,
// and says which agent it went to.
type Reviewer interface {
	Review(prefer, text string) (string, error)
}

// Previewer shows a file read-only in a tab of its own (tend's preview
// tab). An opener that is not one leaves previews to the panel itself.
type Previewer interface {
	Preview(path string, line int, dir string) error
}

// preview shows a file in the preview tab, or in the panel when there is
// no preview tab to be had.
func (m *Model) preview(title, path string, line int) {
	if pv, ok := m.opener.(Previewer); ok && m.opener != nil {
		if err := pv.Preview(path, line, m.tree.Root); err == nil {
			return
		}
	}
	m.showFile(title, path)
	if line > 0 {
		m.viewer.top = max(line-1-m.listRows()/2, 0)
		m.viewer.mark = line
	}
}

// Model is the explorer's whole state.
type Model struct {
	tree   *Tree
	git    Git
	status *Status
	opener Opener

	view   View
	mode   mode
	cols   int
	rows   int
	cursor [3]int
	scroll [3]int

	csearch contentSearch

	// repos are the repositories shown — one, or a folder's — and active
	// the one m.git and m.status are, the selection's.
	repos  []repoState
	active int

	// neighbours says where the other panes of the tab are, and follow is
	// what the panel made of it last time.
	neighbours Neighbours
	follow     follower

	settings      Settings
	configured    bool
	watchSettings func() (Settings, bool)
	// binaryChanged says a new build of the panel is installed (upgrade.go).
	binaryChanged func() bool
	// writeSetting writes one of [files] for the gear's settings.
	writeSetting   func(key, value string) error
	settingsCursor int
	// body is what the panel under the activity bar is drawn into, kept
	// from one frame to the next; bar is whether the last frame had the
	// bar, which is what a click lands on.
	body *vt.Grid
	bar  bool
	// gitFooter is the row the branch and sync are on under the activity
	// bar, or -1 when there is none.
	gitFooter int

	menu   contextMenu
	prompt prompt
	hist   history
	// viewerBack is where q in the viewer goes: the list the thing shown
	// was chosen from.
	viewerBack mode

	branches   branchPicker
	job        Job
	jobRunning bool

	// rows as last laid out, so a click lands on what was drawn.
	fileRows   []*Node
	changeRows []changeRow

	viewer viewer
	search search
	input  string

	// confirm names what the next press of the same key will do, for the
	// one action that loses work.
	confirm string

	message    string
	messageErr bool

	lastClick     time.Time
	lastClickRow  int
	now           func() time.Time
	quit          bool
	statusErr     error
	repoless      bool
	changesStaged int
}

// changeRow is a line of the changes list: a section heading, or a change
// in the staged or the unstaged half.
type changeRow struct {
	heading string
	change  Change
	staged  bool
	// repo is the repository the row is of; count is a heading's entries.
	repo  int
	count int
}

// New is an explorer of dir, which shows the whole repository when dir is in
// one: the project, not whichever of its directories the pane was in.
func New(dir string, opener Opener) *Model {
	root, repos := reposFor(dir)
	m := &Model{tree: NewTree(root), opener: opener, now: time.Now, active: -1, gitFooter: -1}
	for _, g := range repos {
		m.repos = append(m.repos, repoState{git: g})
	}
	m.tree.repos = repos
	m.repoless = len(repos) == 0
	m.Refresh()
	return m
}

// Settings are the panel's choices from the settings file ([files]).
type Settings struct {
	Icons  string
	Hidden bool
	Follow bool
	// Dock and Width are how the panel opens, shown on the gear's
	// settings; an open panel does not act on them.
	Dock     string
	Width    int
	AutoOpen bool
}

// Configure applies settings. Only what changed since the last is applied,
// so a setting read again does not undo what the user toggled by key since.
func (m *Model) Configure(s Settings) {
	if s.Hidden != m.settings.Hidden || !m.configured {
		m.tree.Hidden = s.Hidden
		m.layout()
		m.clamp()
	}
	m.settings, m.configured = s, true
}

// WatchSettings gives the panel a way to read its settings again, which it
// does on the refresh timer; ok is false when nothing changed.
func (m *Model) WatchSettings(fn func() (Settings, bool)) { m.watchSettings = fn }

// Tick is what the refresh timer does: settings, following, the disk.
func (m *Model) Tick() {
	if m.watchSettings != nil {
		if s, ok := m.watchSettings(); ok {
			m.Configure(s)
		}
	}
	// A panel no settings were given to follows, as the default is.
	if m.settings.Follow || !m.configured {
		m.Follow()
	}
	m.Refresh()
}

// Warn shows a problem in the panel's footer.
func (m *Model) Warn(msg string) { m.say(msg, true) }

// FollowPanes makes the panel follow the directory of the pane beside it.
func (m *Model) FollowPanes(n Neighbours) { m.neighbours = n }

// Quit reports whether the user asked to leave.
func (m *Model) Quit() bool { return m.quit }

// Resize sets the size the model draws at.
func (m *Model) Resize(cols, rows int) { m.cols, m.rows = cols, rows }

// Refresh reads the disk and git again, keeping the cursor on the entry it
// was on. It runs on a timer: agents write files while the explorer is
// shown, and a tree that has to be told to look is one nobody trusts.
func (m *Model) Refresh() {
	var keep string
	if m.view == ViewFiles && m.cursor[ViewFiles] < len(m.fileRows) {
		keep = m.fileRows[m.cursor[ViewFiles]].Rel
	}
	for i := range m.repos {
		st, err := m.repos[i].git.Status()
		m.repos[i].err = err
		if err == nil {
			m.repos[i].status = st
		}
	}
	switch {
	case m.active >= 0:
		m.activate(m.active)
	case len(m.repos) > 0:
		m.activate(0)
	default:
		m.activate(-1)
	}
	m.tree.Reload(m.git)
	m.layout()
	if keep != "" {
		for i, n := range m.fileRows {
			if n.Rel == keep {
				m.cursor[ViewFiles] = i
			}
		}
	}
	m.clamp()
}

// layout rebuilds both lists from the tree and the status.
func (m *Model) layout() {
	m.fileRows = m.tree.Rows(m.git)
	m.changeRows = m.changeRows[:0]
	multi := m.multiRepo()
	for i, r := range m.repos {
		if r.status == nil {
			continue
		}
		var staged, unstaged []Change
		for _, c := range r.status.Changes {
			if c.Staged() {
				staged = append(staged, c)
			}
			if c.Unstaged() {
				unstaged = append(unstaged, c)
			}
		}
		if len(staged)+len(unstaged) == 0 {
			continue
		}
		if multi {
			// Each repository under its name, as herdr-sidebar lists them.
			m.changeRows = append(m.changeRows, changeRow{heading: "repo", repo: i})
		}
		if len(staged) > 0 {
			m.changeRows = append(m.changeRows, changeRow{heading: "staged", repo: i, count: len(staged)})
			for _, c := range staged {
				m.changeRows = append(m.changeRows, changeRow{change: c, staged: true, repo: i})
			}
		}
		if len(unstaged) > 0 {
			m.changeRows = append(m.changeRows, changeRow{heading: "changes", repo: i, count: len(unstaged)})
			for _, c := range unstaged {
				m.changeRows = append(m.changeRows, changeRow{change: c, repo: i})
			}
		}
	}
}

// listLen is how many rows the current list has.
func (m *Model) listLen() int {
	switch m.view {
	case ViewFiles:
		return len(m.fileRows)
	case ViewSearch:
		return len(m.csearch.rows)
	}
	return len(m.changeRows)
}

// listRows is how many rows of the screen the list gets: all but the two
// header rows and the footer, and in the search view the three rows of the
// query, its switches and its count as well.
func (m *Model) listRows() int {
	if m.view == ViewSearch && m.mode == modeList {
		return max(m.rows-searchListTop-1, 1)
	}
	if m.mode == modeHistory {
		// The title, then the list, then what the selection is and the keys.
		return max(m.rows-4, 1)
	}
	return max(m.rows-3, 1)
}

// clamp keeps the cursor on the list, off headings, and in view.
func (m *Model) clamp() {
	v := m.view
	n := m.listLen()
	if n == 0 {
		m.cursor[v], m.scroll[v] = 0, 0
		return
	}
	m.cursor[v] = min(max(m.cursor[v], 0), n-1)
	if v == ViewChanges && m.changeRows[m.cursor[v]].heading != "" {
		// A heading is not something to act on; the entry under it is.
		if m.cursor[v]+1 < n {
			m.cursor[v]++
		} else if m.cursor[v] > 0 {
			m.cursor[v]--
		}
	}
	visible := m.listRows()
	if m.cursor[v] < m.scroll[v] {
		m.scroll[v] = m.cursor[v]
	}
	if m.cursor[v] >= m.scroll[v]+visible {
		m.scroll[v] = m.cursor[v] - visible + 1
	}
	m.scroll[v] = min(max(m.scroll[v], 0), max(n-visible, 0))
	m.syncActive()
}

// move moves the cursor by delta, stepping over headings in the direction
// of travel.
func (m *Model) move(delta int) {
	v := m.view
	n := m.listLen()
	if n == 0 {
		return
	}
	at := min(max(m.cursor[v]+delta, 0), n-1)
	if v == ViewChanges {
		step := 1
		if delta < 0 {
			step = -1
		}
		for at >= 0 && at < n && m.changeRows[at].heading != "" {
			at += step
		}
		if at < 0 || at >= n {
			return
		}
	}
	m.cursor[v] = at
	m.clamp()
}

func (m *Model) say(msg string, isErr bool) {
	m.message, m.messageErr = msg, isErr
}

func (m *Model) fail(err error) {
	if err != nil {
		m.say(err.Error(), true)
	}
}

// selectedNode is the tree entry under the cursor.
func (m *Model) selectedNode() *Node {
	if m.cursor[ViewFiles] < len(m.fileRows) {
		return m.fileRows[m.cursor[ViewFiles]]
	}
	return nil
}

// selectedChange is the change under the cursor.
func (m *Model) selectedChange() (changeRow, bool) {
	i := m.cursor[ViewChanges]
	if i < len(m.changeRows) && m.changeRows[i].heading == "" {
		return m.changeRows[i], true
	}
	return changeRow{}, false
}

// Key handles one key.
func (m *Model) Key(k Key) {
	pending := m.confirm
	m.confirm = ""
	if m.mode == modeList {
		m.message = ""
	}
	switch m.mode {
	case modeViewer:
		m.viewerKey(k)
	case modeSearch:
		m.searchKey(k)
	case modeCommit:
		m.commitKey(k)
	case modeHelp:
		m.mode = modeList
	case modeBranch:
		m.branchKey(k)
	case modeMenu:
		m.menuKey(k)
	case modeHistory:
		m.historyKey(k)
	case modeSettings:
		m.settingsKey(k)
	case modePrompt:
		m.promptKey(k)
	default:
		if m.view == ViewSearch && m.searchViewKey(k) {
			return
		}
		m.listKey(k, pending)
	}
}

func (m *Model) listKey(k Key, pending string) {
	switch k.Name {
	case "up", "k":
		m.move(-1)
	case "down", "j", "ctrl+n":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-m.listRows() / 2)
	case "pgdown", "ctrl+d":
		m.move(m.listRows() / 2)
	case "home", "g":
		m.cursor[m.view] = 0
		m.clamp()
	case "end", "G":
		m.cursor[m.view] = m.listLen() - 1
		m.clamp()
	case "tab":
		m.switchView((m.view + 1) % 3)
	case "shift+tab":
		m.switchView((m.view + 2) % 3)
	case "1":
		m.switchView(ViewFiles)
	case "2":
		m.openContentSearch()
	case "3":
		m.switchView(ViewChanges)
	case "/", "ctrl+p":
		m.openSearch()
	case "ctrl+f":
		m.openContentSearch()
	case "r":
		m.Refresh()
		m.say("refreshed", false)
	case "B":
		m.openBranches()
	case "L":
		m.openHistory(histCommits, "")
	case "P":
		m.startSync()
	case ",":
		// herdr-sidebar's s, which here stages: the gear's settings.
		m.openSettings()
	case "?":
		m.mode = modeHelp
	case "q", "ctrl+c":
		m.quit = true
	default:
		if m.view == ViewFiles {
			m.filesKey(k)
		} else {
			m.changesKey(k, pending)
		}
	}
}

func (m *Model) switchView(v View) {
	m.view = v
	m.clamp()
}

func (m *Model) filesKey(k Key) {
	n := m.selectedNode()
	switch k.Name {
	case "enter", "o":
		if n == nil {
			return
		}
		if n.Dir {
			m.tree.Toggle(n, m.git)
			m.layout()
			m.clamp()
			return
		}
		m.open(m.tree.Path(n), 0)
	case "space", "v":
		if n == nil {
			return
		}
		if n.Dir {
			m.tree.Toggle(n, m.git)
			m.layout()
			m.clamp()
			return
		}
		m.preview(n.Rel, m.tree.Path(n), 0)
	case "right", "l":
		if n != nil && n.Dir {
			if !n.Expanded {
				m.tree.Toggle(n, m.git)
				m.layout()
			} else {
				m.move(1)
			}
		}
		m.clamp()
	case "left", "h":
		if n == nil {
			return
		}
		if n.Dir && n.Expanded {
			m.tree.Toggle(n, m.git)
			m.layout()
			m.clamp()
			return
		}
		if p := n.Parent(); p != nil {
			for i, r := range m.fileRows {
				if r == p {
					m.cursor[ViewFiles] = i
				}
			}
			m.clamp()
		}
	case "m":
		m.openMenu(n)
	case "s":
		m.stagePath(n)
	case ".":
		m.tree.Hidden = !m.tree.Hidden
		m.layout()
		m.clamp()
		if m.tree.Hidden {
			m.say("dotfiles hidden", false)
		} else {
			m.say("dotfiles shown", false)
		}
	}
}

func (m *Model) changesKey(k Key, pending string) {
	if m.git.Top == "" {
		return
	}
	row, ok := m.selectedChange()
	switch k.Name {
	case "enter", "space", "v":
		if ok {
			m.showDiff(row)
		}
	case "o":
		if ok && !isDeleted(row.change) {
			m.open(filepath.Join(m.git.Top, filepath.FromSlash(row.change.Path)), 0)
		}
	case "s":
		if !ok {
			return
		}
		var err error
		if row.staged {
			err = m.git.Unstage(row.change.Path)
		} else {
			err = m.git.Stage(row.change.Path)
		}
		m.fail(err)
		m.Refresh()
	case "S":
		m.fail(m.git.StageAll())
		m.Refresh()
	case "x":
		if !ok || row.staged {
			if ok {
				m.say("unstage it first (s); x puts back the working copy", true)
			}
			return
		}
		want := "discard " + row.change.Path
		if pending != want {
			m.confirm = want
			m.say("x again to discard changes to "+row.change.Path, true)
			return
		}
		if err := m.git.Discard(row.change); err != nil {
			m.fail(err)
		} else {
			m.say("discarded "+row.change.Path, false)
		}
		m.Refresh()
	case "A":
		m.startSuggest()
	case "M", "R":
		m.startFinish(k.Name, pending)
	case "c":
		if m.changesStaged == 0 {
			m.say("nothing staged: s stages a file, S everything", true)
			return
		}
		m.mode, m.input = modeCommit, ""
	}
}

func isDeleted(c Change) bool { return c.Work == 'D' || (c.Index == 'D' && c.Work == ' ') }

func (m *Model) commitKey(k Key) {
	switch k.Name {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "enter":
		msg := strings.TrimSpace(m.input)
		if msg == "" {
			m.mode = modeList
			return
		}
		m.mode = modeList
		if err := m.git.Commit(msg); err != nil {
			m.fail(err)
		} else {
			m.say("committed: "+msg, false)
		}
		m.Refresh()
	case "backspace":
		m.input = dropLastRune(m.input)
	case "ctrl+w":
		m.input = dropLastWord(m.input)
	case "alt+a":
		// Drafted here too, as herdr-sidebar's ✧ sits in the commit box.
		m.mode = modeList
		m.startSuggest()
	default:
		if k.Rune != 0 {
			m.input += string(k.Rune)
		}
	}
}

func dropLastRune(s string) string {
	if s == "" {
		return s
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

func dropLastWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndex(s, " "); i >= 0 {
		return s[:i+1]
	}
	return ""
}

// openAt opens a file with the editor's cursor on a line.
func (m *Model) openAt(path string, line int) { m.open(path, line) }

// open hands a file to the opener, with the line to start on or zero.
func (m *Model) open(path string, line int) {
	if m.opener == nil {
		m.say("nowhere to open files: not running in a tend pane", true)
		return
	}
	if err := m.opener.Open(path, line); err != nil {
		m.fail(err)
		return
	}
	m.say("opened "+filepath.Base(path), false)
}

// --- the viewer ------------------------------------------------------------

// viewer shows a file or a diff over the list, a line at a time.
type viewer struct {
	title string
	path  string
	// rel is a diff's file as git names it, from the repository's top: what
	// a review sent to the agent is about.
	rel   string
	lines []string
	diff  bool
	top   int
	left  int
	// mark is a line to show lit, counted from one: the search result the
	// file was opened at. Zero marks nothing.
	mark int
}

// maxViewBytes bounds what the viewer reads: past it, a file is for an
// editor, and reading a log of gigabytes into a side panel would stall it.
const maxViewBytes = 2 << 20

func (m *Model) showFile(title, path string) {
	f, err := os.Open(path)
	if err != nil {
		m.fail(err)
		return
	}
	defer f.Close()
	buf := make([]byte, maxViewBytes+1)
	n, _ := f.Read(buf)
	data := buf[:n]
	var lines []string
	switch {
	case bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0:
		lines = []string{"binary file — o opens it elsewhere"}
	default:
		text := string(data)
		if n > maxViewBytes {
			text = text[:maxViewBytes]
		}
		lines = strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n")
		if n > maxViewBytes {
			lines = append(lines, "… the rest is past 2 MB; o opens it in the editor")
		}
	}
	m.viewer = viewer{title: title, path: path, lines: lines}
	m.mode = modeViewer
}

func (m *Model) showDiff(row changeRow) {
	text, err := m.git.Diff(row.change, row.staged)
	if err != nil {
		m.fail(err)
		return
	}
	if strings.TrimSpace(text) == "" {
		text = "no textual change (mode, or a binary file)"
	}
	which := "unstaged"
	if row.staged {
		which = "staged"
	}
	path := ""
	if !isDeleted(row.change) {
		path = filepath.Join(m.git.Top, filepath.FromSlash(row.change.Path))
	}
	m.viewer = viewer{
		title: row.change.Path + " · " + which,
		path:  path,
		rel:   row.change.Path,
		lines: strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\t", "    "), "\n"), "\n"),
		diff:  true,
	}
	m.mode = modeViewer
}

func (m *Model) viewerKey(k Key) {
	page := m.listRows()
	v := &m.viewer
	switch k.Name {
	case "q", "esc", "ctrl+c", "backspace":
		m.mode, m.viewerBack = m.viewerBack, modeList
	case "up", "k":
		v.top--
	case "down", "j", "enter":
		v.top++
	case "pgup", "ctrl+u", "b":
		v.top -= page
	case "pgdown", "ctrl+d", "space":
		v.top += page
	case "home", "g":
		v.top = 0
	case "end", "G":
		v.top = len(v.lines)
	case "left", "h":
		v.left = max(v.left-8, 0)
	case "right", "l":
		v.left += 8
	case "o":
		if v.path != "" {
			m.open(v.path, v.mark)
		}
	case "r":
		if v.diff && v.rel != "" {
			m.startReview()
		}
	}
	v.top = min(max(v.top, 0), max(len(v.lines)-page, 0))
}

// --- search ------------------------------------------------------------------

// search finds any file in the project by name, as an editor's quick open
// does: the list of files is read when it opens, and matched as each key is
// typed.
type search struct {
	query   string
	all     []string
	results []string
	cursor  int
}

// maxSearchFiles bounds a directory walk outside git, which has no list of
// files to ask for and could be the whole home directory.
const maxSearchFiles = 50000

func (m *Model) openSearch() {
	files, err := m.git.Files()
	if err != nil || m.git.Top == "" {
		files = walkFiles(m.tree.Root, maxSearchFiles)
	} else if rel, rerr := filepath.Rel(m.git.Top, m.tree.Root); rerr == nil && rel != "." {
		// The tree is the repository's top when there is one, so this is a
		// guard rather than a path taken; it keeps paths relative to what
		// is shown.
		prefix := filepath.ToSlash(rel) + "/"
		var inside []string
		for _, f := range files {
			if strings.HasPrefix(f, prefix) {
				inside = append(inside, strings.TrimPrefix(f, prefix))
			}
		}
		files = inside
	}
	m.search = search{all: files}
	m.search.match()
	m.mode = modeSearch
}

func walkFiles(root string, limit int) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(root, path); err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		if len(out) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}

// match ranks the files against the query: every character of it must
// appear in order (so "srvgo" finds server.go), and a match in the file's
// own name beats one spread across its directories, and a shorter path
// beats a longer.
func (s *search) match() {
	q := strings.ToLower(strings.ReplaceAll(s.query, " ", ""))
	type scored struct {
		path  string
		score int
	}
	var hits []scored
	for _, p := range s.all {
		if q == "" {
			hits = append(hits, scored{p, len(p)})
			continue
		}
		lower := strings.ToLower(p)
		if !subsequence(q, lower) {
			continue
		}
		base := lower[strings.LastIndex(lower, "/")+1:]
		score := len(p)
		switch {
		case strings.HasPrefix(base, q):
			score -= 3000
		case strings.Contains(base, q):
			score -= 2000
		case strings.Contains(lower, q):
			score -= 1000
		case subsequence(q, base):
			score -= 500
		}
		hits = append(hits, scored{p, score})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score < hits[b].score
		}
		return hits[a].path < hits[b].path
	})
	s.results = s.results[:0]
	for i, h := range hits {
		if i >= 500 {
			break
		}
		s.results = append(s.results, h.path)
	}
	s.cursor = 0
}

func subsequence(q, s string) bool {
	i := 0
	for j := 0; j < len(s) && i < len(q); j++ {
		if s[j] == q[i] {
			i++
		}
	}
	return i == len(q)
}

func (m *Model) searchKey(k Key) {
	s := &m.search
	switch k.Name {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "up", "ctrl+p":
		s.cursor = max(s.cursor-1, 0)
	case "down", "ctrl+n":
		s.cursor = min(s.cursor+1, max(len(s.results)-1, 0))
	case "enter":
		if s.cursor < len(s.results) {
			m.mode = modeList
			m.open(filepath.Join(m.tree.Root, filepath.FromSlash(s.results[s.cursor])), 0)
		}
	case "tab":
		// Shows the file where it lives, which is often what was wanted: the
		// files beside it.
		if s.cursor < len(s.results) {
			m.reveal(s.results[s.cursor])
		}
	case "backspace":
		s.query = dropLastRune(s.query)
		s.match()
	case "ctrl+w", "ctrl+u":
		s.query = ""
		s.match()
	default:
		if k.Rune != 0 {
			s.query += string(k.Rune)
			s.match()
		}
	}
}

// reveal shows a file in the tree, opening the directories above it.
func (m *Model) reveal(rel string) {
	m.mode = modeList
	m.view = ViewFiles
	if strings.HasPrefix(filepath.Base(rel), ".") || strings.Contains(rel, "/.") {
		m.tree.Hidden = false
	}
	n := m.tree.Reveal(rel, m.git)
	m.layout()
	for i, r := range m.fileRows {
		if r == n {
			m.cursor[ViewFiles] = i
			// In the middle of the list, not at its edge, so what is around
			// it shows too.
			m.scroll[ViewFiles] = max(i-m.listRows()/2, 0)
		}
	}
	m.clamp()
}
