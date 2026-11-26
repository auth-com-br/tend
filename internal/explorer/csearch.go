package explorer

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The search view looks for text in every file of the project, as an
// editor's project search does: a query, three switches (case, whole word,
// regular expression), and globs of files to include and exclude. It
// searches as the query is typed, a moment after the last key, so a fast
// typist does not start a search per letter.

// searchDelay is how long after the last change a search starts.
const searchDelay = 250 * time.Millisecond

// searchListTop is the first row of the results, under the query line, the
// switches and the count.
const searchListTop = 5

// Fields of the search view that take typing.
const (
	fieldQuery = iota
	fieldInclude
	fieldExclude
)

// contentSearch is the search view's state.
type contentSearch struct {
	opts    GrepOptions
	editing bool
	field   int

	// gen numbers each change to what is searched for; a result for an
	// older one is dropped. due is when the pending search should start,
	// zero when none is pending.
	gen     uint64
	due     time.Time
	running bool

	results   []GrepMatch
	truncated bool
	err       error
	// rows is the results as listed: a file, then its matches.
	rows []searchRow
}

// searchRow is a line of the results: a file (match < 0) or one match.
type searchRow struct {
	path  string
	match int
	count int
}

// SearchRequest is a search for the loop to run away from the model.
type SearchRequest struct {
	Gen    uint64
	Dir    string
	InRepo bool
	Opts   GrepOptions
}

// SearchResult is its answer.
type SearchResult struct {
	Gen       uint64
	Matches   []GrepMatch
	Truncated bool
	Err       error
}

// field is the text of a field.
func (c *contentSearch) fieldText(f int) *string {
	switch f {
	case fieldInclude:
		return &c.opts.Include
	case fieldExclude:
		return &c.opts.Exclude
	}
	return &c.opts.Query
}

// changed notes that what is searched for changed, and schedules a search.
func (m *Model) searchChanged() {
	c := &m.csearch
	c.gen++
	c.err = nil
	if strings.TrimSpace(c.opts.Query) == "" {
		c.due, c.results, c.rows, c.truncated, c.running = time.Time{}, nil, nil, false, false
		m.clamp()
		return
	}
	c.due = m.now().Add(searchDelay)
}

// SearchDue returns the search to run now, if one is waiting and its moment
// has come. The loop runs it and hands the answer to ApplySearch.
func (m *Model) SearchDue(now time.Time) (SearchRequest, bool) {
	c := &m.csearch
	if c.due.IsZero() || now.Before(c.due) {
		return SearchRequest{}, false
	}
	c.due = time.Time{}
	c.running = true
	return SearchRequest{Gen: c.gen, Dir: m.tree.Root, InRepo: m.git.Top != "", Opts: c.opts}, true
}

// RunSearch runs a request.
func RunSearch(ctx context.Context, req SearchRequest) SearchResult {
	matches, truncated, err := Grep(ctx, req.Dir, req.InRepo, req.Opts)
	return SearchResult{Gen: req.Gen, Matches: matches, Truncated: truncated, Err: err}
}

// ApplySearch takes a search's answer, unless something was typed since.
func (m *Model) ApplySearch(r SearchResult) {
	c := &m.csearch
	if r.Gen != c.gen {
		return
	}
	c.running = false
	c.results, c.truncated, c.err = r.Matches, r.Truncated, r.Err
	c.rows = c.rows[:0]
	file := -1
	for i, match := range r.Matches {
		if i == 0 || r.Matches[i-1].Path != match.Path {
			file = len(c.rows)
			c.rows = append(c.rows, searchRow{path: match.Path, match: -1})
		}
		c.rows[file].count++
		c.rows = append(c.rows, searchRow{path: match.Path, match: i})
	}
	m.cursor[ViewSearch], m.scroll[ViewSearch] = 0, 0
	m.clamp()
}

// searchNow runs the pending search at once, in the caller's goroutine:
// what a test does in place of the loop.
func (m *Model) searchNow() {
	c := &m.csearch
	if c.due.IsZero() {
		return
	}
	c.due = m.now()
	if req, ok := m.SearchDue(m.now()); ok {
		m.ApplySearch(RunSearch(context.Background(), req))
	}
}

// openContentSearch goes to the search view with the query being typed.
func (m *Model) openContentSearch() {
	m.mode = modeList
	m.view = ViewSearch
	m.csearch.editing, m.csearch.field = true, fieldQuery
	m.clamp()
}

func (m *Model) toggleSearchOption(name string) {
	o := &m.csearch.opts
	switch name {
	case "case":
		o.Case = !o.Case
	case "word":
		o.Word = !o.Word
	case "regex":
		o.Regex = !o.Regex
	}
	m.searchChanged()
}

// searchKey handles a key in the search view. It reports whether it took it.
func (m *Model) searchViewKey(k Key) bool {
	c := &m.csearch
	switch k.Name {
	case "alt+c":
		m.toggleSearchOption("case")
		return true
	case "alt+w":
		m.toggleSearchOption("word")
		return true
	case "alt+r":
		m.toggleSearchOption("regex")
		return true
	}
	if c.editing {
		text := c.fieldText(c.field)
		switch k.Name {
		case "esc":
			c.editing = false
		case "enter", "down":
			c.editing = false
			// Enter is "search now", not in a quarter of a second.
			if !c.due.IsZero() {
				c.due = m.now()
			}
		case "tab":
			c.field = (c.field + 1) % 3
		case "shift+tab":
			c.field = (c.field + 2) % 3
		case "backspace":
			*text = dropLastRune(*text)
			m.searchChanged()
		case "ctrl+u":
			*text = ""
			m.searchChanged()
		case "ctrl+w":
			*text = dropLastWord(*text)
			m.searchChanged()
		case "ctrl+c":
			m.quit = true
		default:
			if k.Rune == 0 {
				return true
			}
			*text += string(k.Rune)
			m.searchChanged()
		}
		return true
	}

	switch k.Name {
	case "i", "a":
		c.editing, c.field = true, fieldQuery
	case "C":
		m.toggleSearchOption("case")
	case "W":
		m.toggleSearchOption("word")
	case "R":
		m.toggleSearchOption("regex")
	case "enter", "o":
		if path, line, ok := m.selectedSearchHit(); ok {
			m.openAt(path, line)
		}
	case "space", "v":
		if path, line, ok := m.selectedSearchHit(); ok {
			m.preview(filepath.ToSlash(m.relToRoot(path)), path, line)
		}
	default:
		return false
	}
	return true
}

// relToRoot is a path as the tree names it.
func (m *Model) relToRoot(path string) string {
	if r, err := filepath.Rel(m.tree.Root, path); err == nil {
		return r
	}
	return path
}

// selectedSearchHit is the file and line under the cursor: a match's own,
// or a file's first.
func (m *Model) selectedSearchHit() (string, int, bool) {
	c := &m.csearch
	i := m.cursor[ViewSearch]
	if i >= len(c.rows) {
		return "", 0, false
	}
	r := c.rows[i]
	idx := r.match
	if idx < 0 && i+1 < len(c.rows) {
		idx = c.rows[i+1].match
	}
	if idx < 0 || idx >= len(c.results) {
		return "", 0, false
	}
	hit := c.results[idx]
	return filepath.Join(m.tree.Root, filepath.FromSlash(hit.Path)), hit.Line, true
}

// drawSearchView paints the search view under the header.
func (m *Model) drawSearchView(g *vt.Grid) (cx, cy int, cursor bool) {
	c := &m.csearch
	labels := [3]string{"› ", "include › ", "exclude › "}
	shown := c.field
	if !c.editing {
		shown = fieldQuery
	}
	x := put(g, 1, 2, labels[shown], styleAccent, m.cols)
	text := *c.fieldText(shown)
	room := m.cols - x - 1
	if runewidth.StringWidth(text) > room && room > 1 {
		runes := []rune(text)
		for runewidth.StringWidth(string(runes)) > room-1 {
			runes = runes[1:]
		}
		text = "…" + string(runes)
	}
	switch {
	case text == "" && !c.editing:
		put(g, x, 2, "search in files (i)", styleDim, m.cols)
	default:
		cx = put(g, x, 2, text, styleNormal, m.cols)
		cy, cursor = 2, c.editing
	}

	// The switches, lit when on, and whether globs narrow the search.
	on := func(b bool) vt.Style {
		if b {
			return vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold | vt.AttrReverse}
		}
		return styleDim
	}
	x = put(g, 1, 3, "Aa", on(c.opts.Case), m.cols)
	x = put(g, x+1, 3, "ab", on(c.opts.Word), m.cols)
	x = put(g, x+1, 3, ".*", on(c.opts.Regex), m.cols)
	if c.opts.Include != "" {
		x = put(g, x+1, 3, "+"+truncate(c.opts.Include, 10), styleGreen, m.cols)
	}
	if c.opts.Exclude != "" {
		put(g, x+1, 3, "-"+truncate(c.opts.Exclude, 10), styleRed, m.cols)
	}

	var status string
	statusStyle := styleDim
	switch {
	case c.err != nil:
		status, statusStyle = c.err.Error(), styleErr
	case c.running || !c.due.IsZero():
		status = "searching…"
	case strings.TrimSpace(c.opts.Query) == "":
		status = "tab: include, exclude · alt+c w r"
	case len(c.results) == 0:
		status = "no results"
	default:
		files := 0
		for _, r := range c.rows {
			if r.match < 0 {
				files++
			}
		}
		status = strconv.Itoa(len(c.results)) + " in " + strconv.Itoa(files) + " files"
		if c.truncated {
			status += " (first " + strconv.Itoa(maxGrepMatches) + ")"
		}
	}
	put(g, 1, 4, truncate(status, m.cols-2), statusStyle, m.cols)

	v := ViewSearch
	for i := 0; i < m.listRows(); i++ {
		at := m.scroll[v] + i
		if at >= len(c.rows) {
			break
		}
		y := searchListTop + i
		r := c.rows[at]
		selected := at == m.cursor[v] && !c.editing
		base, dim, hit := styleNormal, styleDim, vt.Style{FG: vt.IndexedColor(3), Attrs: vt.AttrBold}
		if selected {
			fill(g, y, 0, m.cols, styleSel)
			base, dim, hit = styleSel, styleSel, styleSel
		}
		if r.match < 0 {
			name := filepath.Base(r.path)
			xx := put(g, 1, y, truncate(name, m.cols-6), pick(selected, styleSel, styleBold), m.cols-4)
			if dir := filepath.Dir(r.path); dir != "." && xx+2 < m.cols-4 {
				put(g, xx+1, y, truncate(dir, m.cols-xx-6), dim, m.cols-4)
			}
			count := strconv.Itoa(r.count)
			put(g, m.cols-1-len(count), y, count, pick(selected, styleSel, styleYellow), m.cols)
			continue
		}
		match := c.results[r.match]
		num := strconv.Itoa(match.Line)
		xx := put(g, 3, y, num, dim, m.cols)
		m.drawMatchLine(g, xx+1, y, match, base, hit)
	}
	return cx, cy, cursor
}

// drawMatchLine draws a matching line from x, with the match in it lit and
// in view: a line is cut from the left when the match is past the width,
// since the match is what the row is for.
func (m *Model) drawMatchLine(g *vt.Grid, x, y int, match GrepMatch, base, hit vt.Style) {
	text := strings.ReplaceAll(match.Text, "\t", " ")
	start := min(max(match.Col, 0), len(text))
	// Leading blanks say nothing about the match.
	trimmed := strings.TrimLeft(text, " ")
	cut := len(text) - len(trimmed)
	if start < cut {
		cut = start
	}
	room := m.cols - x - 1
	if room <= 0 {
		return
	}
	// Keep a little context before the match when it would fall off the edge.
	if w := runewidth.StringWidth(text[cut:start]); w > room/2 {
		skip := w - room/3
		for i, r := range text[cut:start] {
			if skip <= 0 {
				cut += i
				break
			}
			skip -= runewidth.RuneWidth(r)
		}
		x = put(g, x, y, "…", base, m.cols)
		room--
	}
	end := start + m.matchLen(text[start:])
	x = put(g, x, y, text[cut:start], base, x+room)
	x = put(g, x, y, text[start:end], hit, m.cols-1)
	put(g, x, y, text[end:], base, m.cols-1)
}

// matchLen is how long the match at the start of s is. git says where a
// match starts, not where it ends; for text it is the query's length, and
// for a pattern the query is found again here.
func (m *Model) matchLen(s string) int { return matchLength(m.csearch.opts, s) }

// matchLength is how long the match of o at the start of s is.
func matchLength(o GrepOptions, s string) int {
	if !o.Regex {
		if len(o.Query) <= len(s) {
			return len(o.Query)
		}
		return len(s)
	}
	re, err := compileSearch(o)
	if err != nil {
		return 0
	}
	if loc := re.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return loc[1]
	}
	return 0
}

// compileSearch is the query as Go reads it, for marking a pattern's match:
// git's extended expressions and Go's agree on everything a person types
// into a search box.
func compileSearch(o GrepOptions) (*regexp.Regexp, error) {
	expr := o.Query
	if o.Word {
		expr = `\b(?:` + expr + `)\b`
	}
	if !o.Case {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}
