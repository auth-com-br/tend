package explorer

import (
	"context"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Panel is the files panel as data, for a client that draws it itself: the
// desktop's (docs/DESKTOP.md, "the dock"). It is the Model's logic without
// its grid, its keys or its modes — the same tree, the same repositories,
// the same git and jj, the same refusals — given as values a call returns.
// What the Model keeps of one person's looking (the cursor, the scroll, the
// mode, the text being typed, the second press that confirms) is the
// client's here, as the sidebar's is (internal/sidebar).
//
// Its methods are safe to call from several goroutines. A repository is
// named by its top directory (Repo.Top), which stays its name across a
// refresh, where an index into a list would point at another one after a
// reroot.
type Panel struct {
	// mu guards everything below. It is not held while git runs, except by
	// Refresh and the tree's methods, which read directories and ask git
	// which entries it ignores: the tree is read a directory at a time and
	// those are the reads it is made of.
	mu         sync.Mutex
	tree       *Tree
	repos      []repoState
	follow     follower
	neighbours Neighbours
	reviewer   Reviewer
}

// NewPanel is a panel of dir: the whole repository dir is in, or a folder
// with the repositories under it, as `tend files` shows it.
func NewPanel(dir string) *Panel {
	p := &Panel{}
	p.reroot(dir)
	p.Refresh()
	return p
}

// reroot points the panel at dir, keeping whether dotfiles are hidden. It
// reports whether that changed what is shown.
func (p *Panel) reroot(dir string) bool {
	root, repos := reposFor(dir)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tree != nil && p.tree.Root == root {
		return false
	}
	hidden := false
	if p.tree != nil {
		hidden = p.tree.Hidden
	}
	p.tree = NewTree(root)
	p.tree.Hidden, p.tree.repos = hidden, repos
	p.repos = nil
	for _, g := range repos {
		p.repos = append(p.repos, repoState{git: g})
	}
	return true
}

// Root is the directory shown.
func (p *Panel) Root() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tree.Root
}

// SetNeighbours is how the panel learns where the panes beside it are, for
// Follow.
func (p *Panel) SetNeighbours(n Neighbours) {
	p.mu.Lock()
	p.neighbours = n
	p.mu.Unlock()
}

// SetReviewer is where Review sends a review: in tend, the agent of the tab.
func (p *Panel) SetReviewer(r Reviewer) {
	p.mu.Lock()
	p.reviewer = r
	p.mu.Unlock()
}

// Refresh reads git's status of every repository and the open directories
// again, as the panel does on its timer.
func (p *Panel) Refresh() {
	p.mu.Lock()
	root := p.tree.Root
	gits := make([]Git, len(p.repos))
	for i, r := range p.repos {
		gits[i] = r.git
	}
	p.mu.Unlock()

	fresh := make([]repoState, len(gits))
	for i, g := range gits {
		st, err := g.Status()
		fresh[i] = repoState{git: g, status: st, err: err}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tree.Root != root {
		return // rerooted meanwhile; that reroot's refresh is the one to keep
	}
	for i := range fresh {
		// A status that failed keeps the last one, as the panel's does.
		if fresh[i].err != nil && i < len(p.repos) {
			fresh[i].status = p.repos[i].status
		}
	}
	p.repos = fresh
	p.tree.Reload(Git{})
}

// Repo is one repository the panel shows and where it is.
type Repo struct {
	// Top is the repository's directory, which names it to every method.
	Top string
	// Name is how the panel names it: its path from the folder shown.
	Name string
	JJ   bool
	// Branch is git's branch, or jj's bookmark (with a + when it is on
	// the parent, @ and a change id when none); "" when there is none.
	Branch   string
	Detached bool
	Upstream string
	Ahead    int
	Behind   int
	// Err is why the last status could not be read; the rest is then the
	// status before.
	Err error
}

// Repos lists the repositories shown: one, or a folder's in the tree's
// order. A folder with none is a tree without git.
func (p *Panel) Repos() []Repo {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Repo, 0, len(p.repos))
	for _, r := range p.repos {
		info := Repo{Top: r.git.Top, Name: repoLabel(p.tree.Root, r.git.Top), JJ: r.git.JJ, Err: r.err}
		if st := r.status; st != nil {
			info.Branch, info.Detached, info.Upstream = st.Branch, st.detachedOrNone, st.Upstream
			info.Ahead, info.Behind = st.Ahead, st.Behind
		}
		out = append(out, info)
	}
	return out
}

// MultiRepo is whether the panel shows a folder of repositories, whose
// changes are then listed under each one's name.
func (p *Panel) MultiRepo() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return isMultiRepo(p.tree.Root, p.repos)
}

// repo is a repository's git and last status, by its top.
func (p *Panel) repo(top string) (Git, *Status, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.repos {
		if r.git.Top == top {
			return r.git, r.status, nil
		}
	}
	if top == "" {
		return Git{}, nil, ErrNoRepo
	}
	return Git{}, nil, errors.New(top + " is not a repository the panel shows")
}

// --- the tree ------------------------------------------------------------------

// Entry is a row of the tree: an entry whose directories above are open.
type Entry struct {
	Name string
	// Rel is the path from the root, with forward slashes: what names the
	// entry to SetExpanded and Reveal. Path is where it is on disk.
	Rel, Path string
	Depth     int
	Dir       bool
	Link      bool
	Expanded  bool
	// Ignored is an entry git ignores; Dot one whose name starts with a
	// dot, which the panel hides when dotfiles are off.
	Ignored, Dot bool
	// Status is git's letter for it ('M', 'A', 'D', 'R', 'U' untracked,
	// '!' conflicted), a directory's the letter of what is inside; 0 for
	// nothing.
	Status byte
	// Repo is the top of the repository it is in, "" for none, and
	// RepoPath its path as that repository names it (for History).
	Repo, RepoPath string
}

// Entries is the tree as it is listed, top to bottom.
func (p *Panel) Entries() []Entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	nodes := p.tree.Rows(Git{})
	out := make([]Entry, len(nodes))
	for i, n := range nodes {
		e := Entry{
			Name: n.Name, Rel: n.Rel, Path: p.tree.Path(n), Depth: n.Depth,
			Dir: n.Dir, Link: n.Link, Expanded: n.Expanded, Ignored: n.Ignored,
			Dot:    strings.HasPrefix(n.Name, "."),
			Status: entryLetter(p.tree, p.repos, n),
		}
		if r := repoIndex(p.tree.Root, p.repos, n.Rel); r >= 0 {
			e.Repo = p.repos[r].git.Top
			e.RepoPath = p.tree.repoPath(p.repos[r].git, n.Rel)
		}
		out[i] = e
	}
	return out
}

// ErrNoSuchEntry is a path the tree does not have.
var ErrNoSuchEntry = errors.New("no such entry in the tree")

// SetExpanded opens or closes a directory of the tree. Its directories
// above must be open: it is a row being clicked, not a path being found
// (Reveal is that).
func (p *Panel) SetExpanded(rel string, open bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.tree.find(rel)
	if n == nil {
		return ErrNoSuchEntry
	}
	if n.Dir && n.Expanded != open {
		p.tree.Toggle(n, Git{})
	}
	return nil
}

// Reveal opens every directory above a path, as the panel's quick open
// shows a file where it lives — dotfiles shown when the path is one.
func (p *Panel) Reveal(rel string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.HasPrefix(filepath.Base(rel), ".") || strings.Contains(rel, "/.") {
		p.tree.Hidden = false
	}
	if p.tree.Reveal(rel, Git{}) == nil {
		return ErrNoSuchEntry
	}
	return nil
}

// SetHidden hides dotfiles, or shows them.
func (p *Panel) SetHidden(hidden bool) {
	p.mu.Lock()
	p.tree.Hidden = hidden
	p.mu.Unlock()
}

// Hidden is whether dotfiles are hidden.
func (p *Panel) Hidden() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tree.Hidden
}

// --- what is shown -------------------------------------------------------------

// Reroot shows another directory: the repository it is in, or itself. It
// reports whether what is shown changed; within the repository shown, it
// does not.
func (p *Panel) Reroot(dir string) bool {
	if !p.reroot(dir) {
		return false
	}
	p.Refresh()
	return true
}

// ChangeFolder shows a folder chosen by hand, which holds until a pane
// beside moves: the panel's "Change folder…".
func (p *Panel) ChangeFolder(dir string) error {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return errors.New(dir + " is not a folder")
	}
	p.Reroot(dir)
	p.mu.Lock()
	p.follow.manual = true
	p.mu.Unlock()
	return nil
}

// Follow asks the neighbours where they are and shows the project of the
// one the panel follows (follow.go), as the panel does on its timer. It
// says the root it moved to; ok is false when nothing moved.
func (p *Panel) Follow() (root string, ok bool) {
	p.mu.Lock()
	n := p.neighbours
	p.mu.Unlock()
	if n == nil {
		return "", false
	}
	siblings, err := n.Siblings()
	if err != nil {
		return "", false
	}
	p.mu.Lock()
	dir := p.follow.nextFor(siblings, p.tree.Root)
	p.mu.Unlock()
	if dir == "" || !p.Reroot(dir) {
		return "", false
	}
	return p.Root(), true
}

// --- finding -------------------------------------------------------------------

// FindFiles is the quick open: the project's files whose path holds the
// query's characters in order, best first (the panel's ranking), at most
// 500. An empty query lists them shortest first.
func (p *Panel) FindFiles(ctx context.Context, query string) []string {
	p.mu.Lock()
	root, repos := p.tree.Root, p.repos
	single := len(repos) == 1 && !isMultiRepo(root, repos)
	var g Git
	if single {
		g = repos[0].git
	}
	p.mu.Unlock()
	var files []string
	var err error
	if single {
		files, err = g.WithContext(ctx).Files()
	}
	if !single || err != nil {
		// A folder of repositories is walked: each one's git knows only its
		// own files, and the folder's are theirs together.
		files = walkFiles(root, maxSearchFiles)
	}
	return rankFiles(files, query)
}

// SearchHit is a line that matched, with how long the match is, which git
// does not say.
type SearchHit struct {
	GrepMatch
	// Len is the match's length in bytes from Col.
	Len int
}

// SearchResults is a search's answer.
type SearchResults struct {
	Hits []SearchHit
	// Files is how many files the hits are in; Truncated says there were
	// more hits than are kept (maxGrepMatches).
	Files     int
	Truncated bool
}

// Search looks for text in the project's files, as the search view does,
// and stops when ctx ends: a new query makes the last one's answer
// worthless. Paths are relative to Root.
func (p *Panel) Search(ctx context.Context, o GrepOptions) (SearchResults, error) {
	p.mu.Lock()
	root, repos := p.tree.Root, p.repos
	p.mu.Unlock()
	// git grep searches a repository as one, and a folder of them as a
	// directory (--no-index), since the folder is not a repository itself.
	inRepo := len(repos) > 0 && !isMultiRepo(root, repos)
	matches, truncated, err := Grep(ctx, root, inRepo, o)
	if err != nil {
		return SearchResults{}, err
	}
	res := SearchResults{Truncated: truncated, Hits: make([]SearchHit, len(matches))}
	for i, m := range matches {
		text := m.Text
		start := min(max(m.Col, 0), len(text))
		res.Hits[i] = SearchHit{GrepMatch: m, Len: matchLength(o, text[start:])}
		if i == 0 || matches[i-1].Path != m.Path {
			res.Files++
		}
	}
	return res, nil
}

// --- the file preview ----------------------------------------------------------

// PreviewKind is what a file is shown as.
type PreviewKind int

const (
	PreviewText PreviewKind = iota
	// PreviewMarkdown is text the client renders as markdown itself.
	PreviewMarkdown
	// PreviewImage is a picture the client draws from the file.
	PreviewImage
	// PreviewBinary is a file not to show as text.
	PreviewBinary
)

// FilePreview is a file as the preview shows it.
type FilePreview struct {
	Path string
	Kind PreviewKind
	// Source is the text, at most 2 MB of it; Truncated says there was
	// more. Lines is it cut into lines of highlighter tokens.
	Source    string
	Lines     [][]Token
	Truncated bool
	// Width and Height are an image's size in pixels.
	Width, Height int
}

// ReadPreview reads a file for showing: its text with its tokens, markdown
// as its source for the client to render, an image's size, or that it is
// binary — the preview's rules (preview.go), without drawing.
func ReadPreview(path string) (FilePreview, error) {
	fp := FilePreview{Path: path}
	if isImage(path) {
		f, err := os.Open(path)
		if err != nil {
			return fp, err
		}
		defer f.Close()
		cfg, _, err := image.DecodeConfig(f)
		if err != nil {
			return fp, err
		}
		fp.Kind, fp.Width, fp.Height = PreviewImage, cfg.Width, cfg.Height
		return fp, nil
	}
	data, binary, cut, err := readForView(path)
	if err != nil {
		return fp, err
	}
	if binary {
		fp.Kind = PreviewBinary
		return fp, nil
	}
	if l := langFor(path); l != nil && l.markdown {
		fp.Kind = PreviewMarkdown
	}
	fp.Source, fp.Truncated = string(data), cut
	fp.Lines = Highlight(path, strings.Split(strings.TrimSuffix(fp.Source, "\n"), "\n"))
	return fp, nil
}
