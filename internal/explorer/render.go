package explorer

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// Colours are the terminal's own sixteen, by index, so the explorer takes on
// whatever theme the terminal and tend already have instead of bringing a
// palette of its own into the middle of them.
var (
	styleNormal = vt.Style{}
	styleDim    = vt.Style{Attrs: vt.AttrDim}
	styleBold   = vt.Style{Attrs: vt.AttrBold}
	// styleSel is herdr-sidebar's selection: its DarkGray under the text,
	// in bold, rather than the text reversed.
	styleSel     = vt.Style{BG: vt.IndexedColor(8), Attrs: vt.AttrBold}
	styleAccent  = vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold}
	styleTabOn   = vt.Style{FG: vt.IndexedColor(4), Attrs: vt.AttrBold | vt.AttrUnderline}
	styleErr     = vt.Style{FG: vt.IndexedColor(1)}
	styleGreen   = vt.Style{FG: vt.IndexedColor(2)}
	styleYellow  = vt.Style{FG: vt.IndexedColor(3)}
	styleRed     = vt.Style{FG: vt.IndexedColor(1)}
	styleCyan    = vt.Style{FG: vt.IndexedColor(6)}
	styleMagenta = vt.Style{FG: vt.IndexedColor(5), Attrs: vt.AttrBold}
	// styleProject is the project's name over the tree, herdr-sidebar's
	// header_accent: LightBlue, bold.
	styleProject = vt.Style{FG: vt.IndexedColor(12), Attrs: vt.AttrBold}
)

// letterStyle is the colour of a git letter, as source-control views colour
// them: new is green, changed yellow, gone red, moved cyan, stuck magenta.
func letterStyle(letter byte) vt.Style {
	switch letter {
	case 'U', 'A', '?':
		return styleGreen
	case 'M', 'T':
		return styleYellow
	case 'D':
		return styleRed
	case 'R', 'C':
		return styleCyan
	case '!':
		return styleMagenta
	}
	return styleNormal
}

// Draw paints the explorer into g, which is the size of the pane. It
// returns where the terminal's cursor belongs, and whether to show it: only
// while something is being typed.
func (m *Model) Draw(g *vt.Grid) (cx, cy int, visible bool) {
	m.Resize(g.Cols(), g.Rows())
	m.bar = m.barActive()
	if !m.bar {
		return m.drawBody(g)
	}
	// herdr-sidebar's layout: the activity bar, the project's name, the
	// list, and the branch and sync on the bottom line. The activity bar
	// takes the header's row and two more, and the git line one at the
	// bottom; what is between is drawn as it always was, into a grid that
	// much shorter, and copied in. Its second row, which is the git bar
	// when there is no activity bar, is where the name goes.
	cols, rows := g.Cols(), g.Rows()-barRows
	m.gitFooter = -1
	if m.status != nil && m.status.Branch != "" {
		rows--
		m.gitFooter = g.Rows() - 1
	}
	if m.body == nil || m.body.Cols() != cols || m.body.Rows() != rows {
		m.body = vt.NewGrid(cols, rows, 0)
	}
	cx, cy, visible = m.drawBody(m.body)
	g.Clear(styleNormal)
	for y := 2; y < rows; y++ {
		from, to := m.body.Line(y), g.Line(y+barRows)
		for x := 0; x < cols; x++ {
			to.SetCell(x, from.Cell(x))
		}
	}
	m.drawActivityBar(g)
	put(g, 1, barRows+1, truncate(strings.ToUpper(m.projectName()), cols-2), styleProject, cols)
	if m.gitFooter >= 0 {
		m.drawGitFooter(g, m.gitFooter)
	}
	m.Resize(cols, rows)
	return cx, cy + barRows, visible
}

// drawBody is the panel without the activity bar, into g.
func (m *Model) drawBody(g *vt.Grid) (cx, cy int, visible bool) {
	m.Resize(g.Cols(), g.Rows())
	g.Clear(styleNormal)
	m.clamp()
	if m.cols < 4 || m.rows < 4 {
		return 0, 0, false
	}

	switch m.mode {
	case modeViewer:
		m.drawViewer(g)
		return 0, 0, false
	case modeSearch:
		return m.drawSearch(g)
	case modeBranch:
		return m.drawBranches(g)
	case modeMenu:
		m.drawMenu(g)
		return 0, 0, false
	case modeHistory:
		m.drawHistory(g)
		return 0, 0, false
	case modeSettings:
		m.drawSettings(g)
		return 0, 0, false
	}

	m.drawHeader(g)
	switch {
	case m.mode == modeHelp:
		m.drawHelp(g)
	case m.view == ViewFiles:
		m.drawFiles(g)
	case m.view == ViewSearch:
		cx, cy, cursor := m.drawSearchView(g)
		if fx, fy, fcursor := m.drawFooter(g); fcursor {
			return fx, fy, true
		}
		return cx, cy, cursor
	default:
		m.drawChanges(g)
	}
	return m.drawFooter(g)
}

// headerNames are the views as the header names them, in order.
var headerNames = [3]string{"files", "search", "changes"}

// viewStyles are the colour each view's icon takes on the activity bar
// while it is the one shown: the tree in the blue of its folders, search
// in magenta, changes in the yellow of a changed file's letter.
var viewStyles = [3]vt.Style{
	{FG: vt.IndexedColor(4), Attrs: vt.AttrBold},
	{FG: vt.IndexedColor(5), Attrs: vt.AttrBold},
	{FG: vt.IndexedColor(3), Attrs: vt.AttrBold},
}

// headerViewAt is the view whose name is at column x of the header: each
// name runs to the divider after it, so a click between two lands on one.
// With icons on, the activity bar is in the header's place (activity.go).
func headerViewAt(x int) View {
	at := 1
	for i, name := range headerNames[:2] {
		at += len(name) + 1
		if x <= at {
			return View(i)
		}
		at += 2
	}
	return ViewChanges
}

// changeCount is every repository's changes, which is what the changes view
// lists, as the header shows it: "" when there are none.
func (m *Model) changeCount() string {
	total := 0
	for _, r := range m.repos {
		if r.status != nil {
			total += len(r.status.Changes)
		}
	}
	if total == 0 {
		return ""
	}
	return strconv.Itoa(total)
}

func (m *Model) drawHeader(g *vt.Grid) {
	x := 1
	for i, name := range headerNames {
		style := styleDim
		if View(i) == m.view {
			style = styleTabOn
		}
		if i > 0 {
			x = put(g, x+1, 0, "│", styleDim, m.cols) + 1
		}
		x = put(g, x, 0, name, style, m.cols)
	}
	if count := m.changeCount(); count != "" {
		put(g, x+1, 0, count, styleYellow, m.cols)
	}

	// The second line says where this is: the project, the branch and how
	// far it is from its upstream, and the sync button.
	m.drawGitBar(g)
}

func (m *Model) drawFiles(g *vt.Grid) {
	v := ViewFiles
	rows := m.listRows()
	if len(m.fileRows) == 0 {
		put(g, 2, 2, "empty directory", styleDim, m.cols)
	}
	for i := 0; i < rows; i++ {
		at := m.scroll[v] + i
		if at >= len(m.fileRows) {
			break
		}
		n := m.fileRows[at]
		y := 2 + i
		m.drawFileRow(g, y, n, at == m.cursor[v])
	}
}

// drawFileRow is one entry of the tree, as herdr-sidebar's row_line draws
// it: the indent, the chevron dimmed, the icon in its colour, the name in
// the text's own colour whether folder or file — the chevron and the icon
// tell them apart — or in its git letter's, and the letter against the
// right with two cells after it (a folder's is a dot). The selection is a
// background under all of that rather than a reversal of it, so the icon
// and the letter keep their colours on the selected row.
func (m *Model) drawFileRow(g *vt.Grid, y int, n *Node, selected bool) {
	sel := func(s vt.Style) vt.Style {
		if selected {
			s.BG = styleSel.BG
			s.Attrs |= styleSel.Attrs
		}
		return s
	}
	if selected {
		fill(g, y, 0, m.cols, styleSel)
	}
	letter := m.statusFor(n)
	nameStyle := styleNormal
	switch {
	case n.Ignored:
		nameStyle = styleDim
	case letter != 0:
		nameStyle = letterStyle(letter)
	}

	x := 2 * n.Depth
	marker := "  "
	if n.Dir {
		marker = "▸ "
		if n.Expanded {
			marker = "▾ "
		}
	}
	x = put(g, x, y, marker, sel(styleDim), m.cols)
	if icon, style := iconFor(n, m.settings.Icons); icon != "" {
		x = put(g, x, y, icon, sel(style), m.cols-3)
	}
	name := n.Name
	if n.Link {
		name += " →"
	}
	// The name yields to the letter and the two cells after it.
	limit := m.cols - 4
	if letter == 0 || n.Ignored {
		limit = m.cols
	}
	put(g, x, y, truncate(name, limit-x), sel(nameStyle), limit)
	if letter != 0 && !n.Ignored {
		shown := string(letter)
		if n.Dir {
			shown = "●"
		}
		put(g, m.cols-3, y, shown, sel(vt.Style{FG: letterStyle(letter).FG, Attrs: vt.AttrBold}), m.cols)
	}
}

func (m *Model) drawChanges(g *vt.Grid) {
	v := ViewChanges
	switch {
	case len(m.repos) == 0:
		put(g, 2, 2, "no git repository here", styleDim, m.cols)
		return
	case m.statusErr != nil && !m.multiRepo():
		put(g, 2, 2, truncate(m.statusErr.Error(), m.cols-3), styleErr, m.cols)
		return
	case len(m.changeRows) == 0:
		put(g, 2, 2, "nothing to commit ✓", styleGreen, m.cols)
		return
	}
	for i := 0; i < m.listRows(); i++ {
		at := m.scroll[v] + i
		if at >= len(m.changeRows) {
			break
		}
		r := m.changeRows[at]
		y := 2 + i
		if r.heading == "repo" {
			// A repository of a folder of them: its name and branch.
			x := put(g, 1, y, "▾ "+m.repoName(r.repo), styleBold, m.cols)
			if st := m.repos[r.repo].status; st != nil && st.Branch != "" {
				put(g, x+1, y, "⎇ "+st.Branch, styleCyan, m.cols)
			}
			continue
		}
		if r.heading != "" {
			indent := 1
			if m.multiRepo() {
				indent = 3
			}
			put(g, indent, y, strings.ToUpper(r.heading)+" "+strconv.Itoa(r.count), styleAccent, m.cols)
			continue
		}
		c := r.change
		letter := c.Work
		if r.staged {
			letter = c.Index
		}
		if c.Untracked() {
			letter = 'U'
		}
		if c.Conflicted() {
			letter = '!'
		}
		selected := at == m.cursor[v]
		nameStyle, dirStyle, letterSt := styleNormal, styleDim, letterStyle(letter)
		if isDeleted(c) && !r.staged || r.staged && c.Index == 'D' {
			nameStyle = vt.Style{Attrs: vt.AttrStrike}
		}
		if selected {
			fill(g, y, 0, m.cols, styleSel)
			nameStyle, dirStyle, letterSt = styleSel, styleSel, styleSel
		}
		limit := m.cols - 3
		name := filepath.Base(c.Path)
		x := put(g, 2, y, truncate(name, limit-2), nameStyle, limit)
		if dir := filepath.Dir(c.Path); dir != "." && x+2 < limit {
			put(g, x+1, y, truncate(dir, limit-x-1), dirStyle, limit)
		}
		put(g, m.cols-2, y, string(letter), letterSt, m.cols)
	}
}

// helpLines are the keys, in the words of what they do.
var helpLines = []string{
	"files",
	"  enter   open in the editor",
	"  space   preview in a tab (click too)",
	"  ← →     close, open folder",
	"  .       dotfiles on, off",
	"  s       stage the file or folder",
	"  m       menu: new, rename, delete, copy path",
	"changes",
	"  enter   diff",
	"  o       open in the editor",
	"  s       stage, unstage",
	"  S       stage everything",
	"  x x     discard the change",
	"  c       commit what is staged",
	"  A       ✧ draft the message (claude)",
	"  r       in a diff: review, sent to the agent",
	"  M M     merge the worktree's branch into main's",
	"  R R     open a pull request (gh)",
	"search (2, ctrl+f)",
	"  type    search as you type",
	"  tab     include, exclude globs",
	"  alt+c   case  alt+w word  alt+r regex",
	"  enter   open at the line",
	"  space   view here",
	"anywhere",
	"  1 2 3   files, search, changes",
	"  B       switch branch (or click it)",
	"  L       history: commits, stashes, tags",
	"  P       sync: pull, push (or ⟳)",
	"  /       find a file by name",
	"  r       refresh",
	"  ,       settings (or the gear)",
	"  q       close the panel",
}

func (m *Model) drawHelp(g *vt.Grid) {
	for i, line := range helpLines {
		if i >= m.listRows() {
			break
		}
		style := styleNormal
		if !strings.HasPrefix(line, " ") {
			style = styleAccent
		}
		put(g, 1, 2+i, truncate(line, m.cols-2), style, m.cols)
	}
}

func (m *Model) drawFooter(g *vt.Grid) (int, int, bool) {
	y := m.rows - 1
	if m.mode == modeCommit || m.mode == modePrompt {
		label, text := "commit: ", m.input
		if m.mode == modePrompt {
			// The question on the row above, the answer on this one: a
			// question like "delete src and everything in it?" does not fit
			// beside a name on a narrow panel.
			fill(g, y-1, 0, m.cols, styleNormal)
			put(g, 1, y-1, truncate(m.prompt.label, m.cols-2), styleAccent, m.cols)
			label, text = "› ", m.prompt.text
		}
		x := put(g, 1, y, label, styleAccent, m.cols)
		// The end of what is typed is what is being looked at.
		if room := m.cols - x - 2; runewidth.StringWidth(text) > room && room > 1 {
			runes := []rune(text)
			for runewidth.StringWidth(string(runes)) > room-1 {
				runes = runes[1:]
			}
			text = "…" + string(runes)
		}
		x = put(g, x, y, text, styleNormal, m.cols)
		return x, y, true
	}
	if m.message != "" {
		style := styleDim
		if m.messageErr {
			style = styleErr
		}
		put(g, 1, y, truncate(m.message, m.cols-2), style, m.cols)
		return 0, 0, false
	}
	hint := "? keys  / find"
	switch {
	case m.view == ViewChanges && m.git.Top != "":
		hint = "s stage  c commit  A ✧  ? keys"
	case m.view == ViewSearch && m.csearch.editing:
		hint = "enter results  tab next field"
	case m.view == ViewSearch:
		hint = "enter open  space view  i edit"
	}
	if m.mode == modeHelp {
		hint = "any key closes this"
	} else if m.bar && m.view == ViewFiles {
		// herdr-sidebar's line over its git footer, against the right, in
		// its DarkGray.
		hint = "m / right-click for menus"
		put(g, max(m.cols-1-len(hint), 1), y, truncate(hint, m.cols-2), vt.Style{FG: vt.IndexedColor(8)}, m.cols)
		return 0, 0, false
	}
	put(g, 1, y, truncate(hint, m.cols-2), styleDim, m.cols)
	return 0, 0, false
}

func (m *Model) drawViewer(g *vt.Grid) {
	v := &m.viewer
	x := put(g, 1, 0, "‹ ", styleAccent, m.cols)
	put(g, x, 0, truncate(v.title, m.cols-x-1), styleBold, m.cols)
	hint := "q back"
	if v.path != "" {
		hint += "  o edit"
	}
	put(g, 1, 1, hint, styleDim, m.cols)

	gutter := 0
	if !v.diff {
		gutter = len(strconv.Itoa(len(v.lines))) + 1
	}
	for i := 0; i < m.listRows()+1; i++ {
		at := v.top + i
		if at >= len(v.lines) {
			break
		}
		y := 2 + i
		line := v.lines[at]
		style := styleNormal
		if v.diff {
			switch {
			case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
				strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "),
				strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"):
				style = styleDim
			case strings.HasPrefix(line, "@@"):
				style = styleCyan
			case strings.HasPrefix(line, "+"):
				style = styleGreen
			case strings.HasPrefix(line, "-"):
				style = styleRed
			}
		} else {
			num := strconv.Itoa(at + 1)
			numStyle := styleDim
			if at+1 == v.mark {
				// The line a search result opened the file at.
				numStyle = vt.Style{FG: vt.IndexedColor(3), Attrs: vt.AttrBold | vt.AttrReverse}
				style = vt.Style{FG: vt.IndexedColor(3), Attrs: vt.AttrBold}
			}
			put(g, gutter-len(num), y, num, numStyle, m.cols)
		}
		put(g, gutter+1, y, cutLeft(line, v.left), style, m.cols)
	}
}

func (m *Model) drawSearch(g *vt.Grid) (int, int, bool) {
	s := &m.search
	put(g, 1, 0, "find a file", styleAccent, m.cols)
	x := put(g, 1, 1, "› ", styleAccent, m.cols)
	cx := put(g, x, 1, truncate(s.query, m.cols-x-1), styleNormal, m.cols)
	top := searchTop(m)
	for i := 0; i < m.listRows(); i++ {
		at := top + i
		if at >= len(s.results) {
			break
		}
		y := 2 + i
		p := s.results[at]
		selected := at == s.cursor
		nameStyle, dirStyle := styleNormal, styleDim
		if selected {
			fill(g, y, 0, m.cols, styleSel)
			nameStyle, dirStyle = styleSel, styleSel
		}
		limit := m.cols - 1
		xx := put(g, 2, y, truncate(filepath.Base(p), limit-2), nameStyle, limit)
		if dir := filepath.Dir(p); dir != "." && xx+2 < limit {
			put(g, xx+1, y, truncate(dir, limit-xx-1), dirStyle, limit)
		}
	}
	if len(s.results) == 0 {
		put(g, 2, 2, "no file matches", styleDim, m.cols)
	}
	put(g, 1, m.rows-1, truncate("enter open  tab show  esc", m.cols-2), styleDim, m.cols)
	return cx, 1, true
}

// searchTop is the first result shown, so the cursor stays on screen.
func searchTop(m *Model) int {
	return max(m.search.cursor-m.listRows()+1, 0)
}

// --- drawing helpers ---------------------------------------------------------

// put draws text from x and returns the column after it, stopping at limit.
// Wide characters take the two columns they need.
func put(g *vt.Grid, x, y int, text string, style vt.Style, limit int) int {
	row := g.Line(y)
	if row == nil {
		return x
	}
	limit = min(limit, g.Cols())
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if w == 0 {
			continue
		}
		if x < 0 || x+w > limit {
			break
		}
		row.SetCell(x, vt.Cell{R: r, Style: style, Width: uint8(w)})
		for i := 1; i < w; i++ {
			row.SetCell(x+i, vt.Cell{Style: style, Width: 0})
		}
		x += w
	}
	return x
}

func fill(g *vt.Grid, y, from, to int, style vt.Style) {
	row := g.Line(y)
	if row == nil {
		return
	}
	for x := max(from, 0); x < min(to, g.Cols()); x++ {
		row.SetCell(x, vt.Cell{R: ' ', Style: style, Width: 1})
	}
}

// truncate shortens text to cols columns, ending in an ellipsis when cut.
func truncate(text string, cols int) string {
	if cols <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= cols {
		return text
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		w := runewidth.RuneWidth(r)
		if used+w > cols-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	b.WriteRune('…')
	return b.String()
}

// cutLeft drops the first n columns of a line, for scrolling sideways.
func cutLeft(line string, n int) string {
	if n <= 0 {
		return line
	}
	used := 0
	for i, r := range line {
		if used >= n {
			return line[i:]
		}
		used += runewidth.RuneWidth(r)
	}
	return ""
}

func pick(cond bool, a, b vt.Style) vt.Style {
	if cond {
		return a
	}
	return b
}
