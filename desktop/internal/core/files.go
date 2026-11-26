package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/explorer"
	"github.com/auth-com-br/tend/internal/proto"
)

// The files tab: the TUI's files panel (`tend files`, herdr-sidebar's
// views) drawn natively, on explorer.Panel — the same tree, git and jj,
// search, changes, review and finish the terminal panel runs, without its
// grid (#69). It shows the folder the pane in focus is working in, as the
// TUI's panel follows the pane beside it.
//
// A file opened from it is a document tab of the window: its text with
// the highlighter's tokens, markdown for Qt to render, or an image; a
// change opened is its diff, side by side, with a comment on a line sent to
// the tab's agent (#71). Document tabs are the window's, like browser tabs.

// filesTab is what the session keeps of the files tab.
type filesTab struct {
	panel   *explorer.Panel
	root    string
	view    string // tree, search, changes
	query   string
	hits    []explorer.SearchHit
	found   []string
	message string
	busy    bool
	// history is the history view's list (commits, stashes or tags of
	// histRepo), and branches the branch picker's, while it is open.
	history  []explorer.HistoryEntry
	histKind explorer.HistoryKind
	histRepo string
	branches []explorer.Branch
	brRepo   string
	// target is what the tree's menu was opened on (filemenu.go).
	target fileTarget
}

// FilesState is the files tab as QML draws it.
type FilesState struct {
	Root    string       `json:"root"`
	View    string       `json:"view"`
	Repos   []RepoRow    `json:"repos"`
	Entries []EntryRow   `json:"entries,omitempty"`
	Changes []ChangeRepo `json:"changes,omitempty"`
	Query   string       `json:"query,omitempty"`
	Hits    []HitRow     `json:"hits,omitempty"`
	Found   []string     `json:"found,omitempty"`
	Message string       `json:"message,omitempty"`
	Busy    bool         `json:"busy,omitempty"`
	Hidden  bool         `json:"hidden"`
	// History is the history view: what kind, and its entries.
	HistoryKind string       `json:"historyKind,omitempty"`
	History     []HistoryRow `json:"history,omitempty"`
	// Branches is the branch picker, open for BranchRepo.
	Branches   []BranchRow `json:"branches,omitempty"`
	BranchRepo string      `json:"branchRepo,omitempty"`
}

// HistoryRow is a commit, a stash or a tag.
type HistoryRow struct {
	Ref     string `json:"ref"`
	Subject string `json:"subject"`
	Meta    string `json:"meta,omitempty"`
}

// BranchRow is a branch the picker offers.
type BranchRow struct {
	Name    string `json:"name"`
	Remote  bool   `json:"remote,omitempty"`
	Current bool   `json:"current,omitempty"`
}

var historyKinds = map[string]explorer.HistoryKind{
	"commits": explorer.HistoryCommits, "stashes": explorer.HistoryStashes, "tags": explorer.HistoryTags,
}

func historyName(k explorer.HistoryKind) string {
	for n, v := range historyKinds {
		if v == k {
			return n
		}
	}
	return "commits"
}

// RepoRow is a repository shown, for the header.
type RepoRow struct {
	Top    string `json:"top"`
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Ahead  int    `json:"ahead,omitempty"`
	Behind int    `json:"behind,omitempty"`
	JJ     bool   `json:"jj,omitempty"`
}

// EntryRow is a row of the tree.
type EntryRow struct {
	Name     string `json:"name"`
	Rel      string `json:"rel"`
	Path     string `json:"path"`
	Depth    int    `json:"depth"`
	Dir      bool   `json:"dir,omitempty"`
	Expanded bool   `json:"expanded,omitempty"`
	Status   string `json:"status,omitempty"`
	Ignored  bool   `json:"ignored,omitempty"`
}

// ChangeRepo is a repository's changes, grouped as git groups them.
type ChangeRepo struct {
	Repo       string      `json:"repo"`
	Name       string      `json:"name"`
	Staged     []ChangeRow `json:"staged"`
	Unstaged   []ChangeRow `json:"unstaged"`
	Untracked  []ChangeRow `json:"untracked"`
	Conflicted []ChangeRow `json:"conflicted"`
}

// ChangeRow is one changed file.
type ChangeRow struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// HitRow is a search result.
type HitRow struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// focusedCwd is where the pane in focus is working now, as the automation
// socket's pane.get says (foreground_cwd, the TUI's panel follows the same).
func (s *Session) focusedCwd() string {
	s.mu.Lock()
	pane := s.focus
	s.mu.Unlock()
	if pane == 0 {
		return ""
	}
	res, err := s.apiCall("pane.get", map[string]any{"pane_id": fmt.Sprintf("p_%d", pane)})
	if err != nil {
		return ""
	}
	p, _ := res["pane"].(map[string]any)
	for _, k := range []string{"foreground_cwd", "cwd"} {
		if d, _ := p[k].(string); d != "" {
			return d
		}
	}
	return ""
}

// loadFiles points the tab at the folder the pane in focus works in, and
// reads it.
func (s *Session) loadFiles() error {
	dir := s.focusedCwd()
	s.mu.Lock()
	f := &s.files
	if dir != "" && dir != f.root {
		f.panel = explorer.NewPanel(dir)
		f.panel.SetReviewer(sessionReviewer{s})
		f.root = dir
	}
	if f.view == "" {
		f.view = "tree"
	}
	p := f.panel
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("no folder to show: the pane in focus is not working in one")
	}
	p.Refresh()
	return nil
}

// filesStateLocked is the tab now. The caller holds the lock; the panel's
// own lock is its own.
func (s *Session) filesStateLocked() *FilesState {
	f := &s.files
	if f.panel == nil {
		return &FilesState{View: f.view, Message: f.message}
	}
	p := f.panel
	st := &FilesState{Root: p.Root(), View: f.view, Query: f.query, Found: f.found, Message: f.message, Busy: f.busy, Hidden: p.Hidden()}
	for _, r := range p.Repos() {
		st.Repos = append(st.Repos, RepoRow{Top: r.Top, Name: r.Name, Branch: r.Branch, Ahead: r.Ahead, Behind: r.Behind, JJ: r.JJ})
	}
	switch f.view {
	case "tree":
		for _, e := range p.Entries() {
			row := EntryRow{Name: e.Name, Rel: e.Rel, Path: e.Path, Depth: e.Depth, Dir: e.Dir, Expanded: e.Expanded, Ignored: e.Ignored}
			if e.Status != 0 {
				row.Status = string(e.Status)
			}
			st.Entries = append(st.Entries, row)
		}
	case "changes":
		for _, rc := range p.Changes() {
			cr := ChangeRepo{Repo: rc.Repo, Name: rc.Name}
			conv := func(cs []explorer.Change, staged bool) []ChangeRow {
				out := []ChangeRow{}
				for _, c := range cs {
					letter := c.Work
					if staged {
						letter = c.Index
					}
					out = append(out, ChangeRow{Path: c.Path, Status: strings.TrimSpace(string(letter))})
				}
				return out
			}
			cr.Staged, cr.Unstaged = conv(rc.Staged, true), conv(rc.Unstaged, false)
			cr.Untracked, cr.Conflicted = conv(rc.Untracked, false), conv(rc.Conflicted, false)
			st.Changes = append(st.Changes, cr)
		}
	case "history":
		st.HistoryKind = historyName(f.histKind)
		for _, h := range f.history {
			st.History = append(st.History, HistoryRow{Ref: h.Ref, Subject: h.Subject, Meta: h.Meta})
		}
	case "search":
		for _, h := range f.hits {
			st.Hits = append(st.Hits, HitRow{Path: h.Path, Line: h.Line, Text: h.Text})
		}
	}
	if f.brRepo != "" {
		st.BranchRepo = f.brRepo
		for _, b := range f.branches {
			st.Branches = append(st.Branches, BranchRow{Name: b.Name, Remote: b.Remote, Current: b.Current})
		}
	}
	return st
}

// FilesAction is one thing the files tab asks for.
type FilesAction struct {
	Do      string `json:"do"`
	View    string `json:"view"`
	Rel     string `json:"rel"`
	Path    string `json:"path"`
	Repo    string `json:"repo"`
	Staged  bool   `json:"staged"`
	Open    bool   `json:"open"`
	Query   string `json:"query"`
	Message string `json:"message"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Action  string `json:"action"`
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Name    string `json:"name"`
	Remote  bool   `json:"remote"`
}

// Files does what the files tab asks.
func (s *Session) Files(a FilesAction) error {
	s.mu.Lock()
	p := s.files.panel
	s.mu.Unlock()
	if a.Do == "open" {
		// A file is shown from its path; the tab's folder is not needed.
		return s.OpenFile(a.Path, a.Line)
	}
	if p == nil && a.Do != "view" {
		return fmt.Errorf("the files tab has no folder")
	}
	var err error
	say := ""
	switch a.Do {
	case "view":
		s.mu.Lock()
		s.files.view = a.View
		s.mu.Unlock()
	case "history":
		kind := historyKinds[a.Kind]
		repo := a.Repo
		if repo == "" {
			if rs := p.Repos(); len(rs) > 0 {
				repo = rs[0].Top
			}
		}
		entries, herr := p.History(repo, kind, a.Path)
		s.mu.Lock()
		s.files.view, s.files.history, s.files.histKind, s.files.histRepo = "history", entries, kind, repo
		s.mu.Unlock()
		err = herr
	case "show":
		s.mu.Lock()
		repo, kind := s.files.histRepo, s.files.histKind
		s.mu.Unlock()
		diff, serr := p.Show(repo, kind, a.Ref, a.Path)
		if serr != nil {
			return serr
		}
		d := Doc{Kind: "diff", Title: "⎇ " + a.Ref, Path: a.Ref, Repo: repo}
		fillDiff(&d, diff)
		s.addDoc(d)
		return nil
	case "stash-apply", "stash-pop":
		s.mu.Lock()
		repo := s.files.histRepo
		s.mu.Unlock()
		err = p.StashApply(repo, a.Ref, a.Do == "stash-pop")
	case "branches":
		bs, berr := p.Branches(a.Repo)
		s.mu.Lock()
		s.files.branches, s.files.brRepo = bs, a.Repo
		s.mu.Unlock()
		err = berr
	case "branches-close":
		s.mu.Lock()
		s.files.branches, s.files.brRepo = nil, ""
		s.mu.Unlock()
	case "switch":
		say, err = p.Switch(a.Repo, explorer.Branch{Name: a.Name, Remote: a.Remote})
		s.mu.Lock()
		s.files.branches, s.files.brRepo = nil, ""
		s.mu.Unlock()
	case "toggle":
		err = p.SetExpanded(a.Rel, a.Open)
	case "hidden":
		p.SetHidden(!p.Hidden())
	case "refresh":
		err = s.loadFiles()
	case "search":
		s.setFilesBusy(true)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		res, serr := p.Search(ctx, explorer.GrepOptions{Query: a.Query})
		cancel()
		s.mu.Lock()
		s.files.query, s.files.hits, s.files.busy = a.Query, res.Hits, false
		s.mu.Unlock()
		err = serr
	case "find":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		found := p.FindFiles(ctx, a.Query)
		cancel()
		s.mu.Lock()
		s.files.found = found
		s.mu.Unlock()
	case "stage":
		err = p.Stage(a.Repo, a.Path)
	case "unstage":
		err = p.Unstage(a.Repo, a.Path)
	case "stage-all":
		err = p.StageAll(a.Repo)
	case "discard":
		err = p.Discard(a.Repo, a.Path, a.Staged)
	case "commit":
		if err = p.Commit(a.Repo, a.Message); err == nil {
			say = "committed"
		}
	case "draft":
		s.setFilesBusy(true)
		ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
		msg, derr := p.DraftMessage(ctx, a.Repo)
		cancel()
		s.setFilesBusy(false)
		if derr != nil {
			return derr
		}
		s.mu.Lock()
		s.files.message = msg
		s.mu.Unlock()
	case "diff":
		return s.OpenDiff(a.Repo, a.Path, a.Staged)
	case "sync":
		s.setFilesBusy(true)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		say, err = p.Sync(ctx, a.Repo)
		cancel()
		s.setFilesBusy(false)
	case "review":
		say, err = p.Review(a.Path, a.Line, a.Text)
	case "plan-finish", "finish":
		action := explorer.FinishMerge
		if a.Action == "pr" {
			action = explorer.FinishPullRequest
		}
		if a.Do == "plan-finish" {
			var plan explorer.FinishPlan
			plan, err = p.PlanFinish(a.Repo, action)
			if err == nil {
				s.setPrompt(&Prompt{Kind: "finish-" + a.Action, Title: plan.What + " — type yes", Text: "", Group: a.Repo})
			}
		} else {
			s.setFilesBusy(true)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			say, err = p.Finish(ctx, a.Repo, action)
			cancel()
			s.setFilesBusy(false)
		}
	default:
		return fmt.Errorf("the files tab asked for %q", a.Do)
	}
	if err == nil && a.Do != "view" && a.Do != "search" && a.Do != "find" && a.Do != "plan-finish" {
		p.Refresh()
		s.refreshDiffs()
	}
	if say != "" && s.Status != nil {
		s.Status(say)
	}
	s.stateChanged()
	return err
}

func (s *Session) setFilesBusy(busy bool) {
	s.mu.Lock()
	s.files.busy = busy
	s.mu.Unlock()
	s.stateChanged()
}

// sessionReviewer is the files tab's Reviewer: a comment on a file and line
// goes to the agent in the tab shown, submitted, as the TUI's panel sends
// it (explorer.SessionOpener).
type sessionReviewer struct{ s *Session }

func (r sessionReviewer) Review(prefer, text string) (string, error) {
	s := r.s
	s.mu.Lock()
	c := s.client
	var agent uint64
	info := map[uint64]proto.PaneInfo{}
	for _, p := range s.snap.Panes {
		info[p.ID] = p
	}
	for _, rect := range s.rects {
		if p := info[rect.Pane]; p.Agent != "" && agent == 0 {
			agent = rect.Pane
		}
	}
	s.mu.Unlock()
	if agent == 0 {
		return "", fmt.Errorf("no agent in this tab to send it to")
	}
	if err := c.PastePane(agent, text); err != nil {
		return "", err
	}
	time.Sleep(150 * time.Millisecond)
	if err := c.SendInput(agent, []byte("\r")); err != nil {
		return "", err
	}
	return "sent to " + info[agent].Agent, nil
}

// --- document tabs ---------------------------------------------------------

// Doc is a document tab: a file shown, or a diff.
type Doc struct {
	ID        int    `json:"id"`
	Kind      string `json:"kind"` // file, diff
	Title     string `json:"title"`
	Workspace uint64 `json:"workspace"`
	// A file: its path, how it is shown, its lines of tokens or its source.
	Path      string        `json:"path,omitempty"`
	View      string        `json:"view,omitempty"` // text, markdown, image, binary
	Lines     [][]TokenSpan `json:"lines,omitempty"`
	Source    string        `json:"source,omitempty"`
	Line      int           `json:"line,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
	// A diff: its repository and file, and its rows side by side.
	Repo   string    `json:"repo,omitempty"`
	Staged bool      `json:"staged,omitempty"`
	Hunks  []HunkRow `json:"hunks,omitempty"`
	Binary bool      `json:"binary,omitempty"`
}

// TokenSpan is a run of text in one of the highlighter's kinds.
type TokenSpan struct {
	T string `json:"t"`
	K int    `json:"k,omitempty"`
}

// HunkRow is a hunk of a diff side by side.
type HunkRow struct {
	Header string    `json:"header"`
	Rows   []DiffRow `json:"rows"`
}

// DiffRow is a row of a side-by-side diff: each side's number, text and
// kind (0 context, 1 added, 2 removed), number 0 where that side has none.
type DiffRow struct {
	OldN int    `json:"on,omitempty"`
	Old  string `json:"o"`
	OldK int    `json:"ok,omitempty"`
	NewN int    `json:"nn,omitempty"`
	New  string `json:"n"`
	NewK int    `json:"nk,omitempty"`
}

// docs is what the session keeps of them.
type docs struct {
	list   []Doc
	active int
	next   int
}

// maxTokenLines is how much of a file is sent highlighted; the rest of a
// long file is sent as plain text, which QML draws as fast.
const maxTokenLines = 4000

// OpenFile shows a file in a document tab, at a line when one is given.
func (s *Session) OpenFile(path string, line int) error {
	prev, err := explorer.ReadPreview(path)
	if err != nil {
		return err
	}
	d := Doc{Kind: "file", Title: filepath.Base(path), Path: path, Line: line, Truncated: prev.Truncated}
	switch prev.Kind {
	case explorer.PreviewMarkdown:
		d.View, d.Source = "markdown", prev.Source
	case explorer.PreviewImage:
		d.View = "image"
	case explorer.PreviewBinary:
		d.View = "binary"
	default:
		d.View = "text"
		for i, line := range prev.Lines {
			if i >= maxTokenLines {
				break
			}
			// Never nil: an empty line is an empty list, not null, which QML
			// cannot draw from.
			spans := []TokenSpan{}
			for _, t := range line {
				spans = append(spans, TokenSpan{T: t.Text, K: int(t.Kind)})
			}
			d.Lines = append(d.Lines, spans)
		}
		if len(prev.Lines) > maxTokenLines {
			d.Truncated = true
		}
	}
	s.addDoc(d)
	return nil
}

// OpenDiff shows a change's diff side by side in a document tab.
func (s *Session) OpenDiff(repo, path string, staged bool) error {
	s.mu.Lock()
	p := s.files.panel
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("the files tab has no folder")
	}
	diff, err := p.Diff(repo, path, staged)
	if err != nil {
		return err
	}
	title := filepath.Base(path)
	if staged {
		title += " (staged)"
	}
	d := Doc{Kind: "diff", Title: "± " + title, Path: path, Repo: repo, Staged: staged}
	fillDiff(&d, diff)
	s.addDoc(d)
	return nil
}

func fillDiff(d *Doc, diff explorer.Diff) {
	d.Hunks, d.Binary = nil, false
	kind := func(k explorer.DiffLineKind) int {
		switch k {
		case explorer.DiffAdded:
			return 1
		case explorer.DiffRemoved:
			return 2
		}
		return 0
	}
	for _, f := range diff.Files {
		d.Binary = d.Binary || f.Binary
		for _, h := range f.Hunks {
			hr := HunkRow{Header: fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section)}
			for _, r := range h.SideBySide() {
				var row DiffRow
				if r.Left != nil {
					row.OldN, row.Old, row.OldK = r.Left.Old, r.Left.Text, kind(r.Left.Kind)
				}
				if r.Right != nil {
					row.NewN, row.New, row.NewK = r.Right.New, r.Right.Text, kind(r.Right.Kind)
				}
				hr.Rows = append(hr.Rows, row)
			}
			d.Hunks = append(d.Hunks, hr)
		}
	}
}

// refreshDiffs reads the diffs shown again, after a change to the
// repository (a stage, a commit) moved them.
func (s *Session) refreshDiffs() {
	s.mu.Lock()
	p := s.files.panel
	list := append([]Doc(nil), s.docs.list...)
	s.mu.Unlock()
	if p == nil {
		return
	}
	for i, d := range list {
		if d.Kind != "diff" {
			continue
		}
		if diff, err := p.Diff(d.Repo, d.Path, d.Staged); err == nil {
			fillDiff(&list[i], diff)
		}
	}
	s.mu.Lock()
	for _, d := range list {
		for j := range s.docs.list {
			if s.docs.list[j].ID == d.ID {
				s.docs.list[j] = d
			}
		}
	}
	s.mu.Unlock()
}

func (s *Session) addDoc(d Doc) {
	s.mu.Lock()
	s.docs.next++
	d.ID = s.docs.next
	d.Workspace = s.workspace
	s.docs.list = append(s.docs.list, d)
	s.docs.active = d.ID
	s.mu.Unlock()
	s.stateChanged()
}

// ShowDoc shows a document tab, or none (0) to go back to the terminals.
func (s *Session) ShowDoc(id int) {
	s.mu.Lock()
	s.docs.active = id
	s.mu.Unlock()
	s.stateChanged()
}

// CloseDoc closes a document tab.
func (s *Session) CloseDoc(id int) {
	s.mu.Lock()
	for i, d := range s.docs.list {
		if d.ID == id {
			s.docs.list = append(s.docs.list[:i], s.docs.list[i+1:]...)
			break
		}
	}
	if s.docs.active == id {
		s.docs.active = 0
	}
	s.mu.Unlock()
	s.stateChanged()
}

// DocTab is a document tab as the tab bar lists it.
type DocTab struct {
	ID        int    `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Workspace uint64 `json:"workspace"`
}
