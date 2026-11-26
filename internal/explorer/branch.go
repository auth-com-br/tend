package explorer

import (
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/auth-com-br/tend/internal/vt"
)

// The git bar is the header's second line in every view: the project, the
// branch, how far it is from its upstream, and a sync button — herdr-
// sidebar's Git footer, kept where the panel already says which branch this
// is. A click on the branch picks another; the button syncs.

// gitBar is where the bar's clickable parts are drawn, from the same
// arithmetic that draws them.
type gitBar struct {
	branchFrom, branchTo int // columns of "⎇ name", -1 when there is none
	syncAt               int // column of the sync button, -1 when none
}

func (m *Model) gitBarLayout() gitBar {
	bar := gitBar{branchFrom: -1, branchTo: -1, syncAt: -1}
	if m.status == nil || m.status.Branch == "" {
		return bar
	}
	x := 1 + runewidth.StringWidth(m.projectName()) + 1
	bar.branchFrom = x
	bar.branchTo = x + runewidth.StringWidth("⎇ "+m.status.Branch)
	if m.cols >= 12 {
		bar.syncAt = m.cols - 2
	}
	return bar
}

// drawGitBar paints the header's second line.
func (m *Model) drawGitBar(g *vt.Grid) {
	bar := m.gitBarLayout()
	limit := m.cols - 3
	if bar.syncAt < 0 {
		limit = m.cols
	}
	x := put(g, 1, 1, m.projectName(), styleBold, limit)
	if bar.branchFrom < 0 {
		return
	}
	x = put(g, x+1, 1, "⎇ "+m.status.Branch, vt.Style{FG: vt.IndexedColor(6), Attrs: vt.AttrUnderline}, limit)
	if m.status.Ahead > 0 {
		x = put(g, x+1, 1, "↑"+strconv.Itoa(m.status.Ahead), styleGreen, limit)
	}
	if m.status.Behind > 0 {
		put(g, x+1, 1, "↓"+strconv.Itoa(m.status.Behind), styleYellow, limit)
	}
	if bar.syncAt >= 0 {
		glyph, style := "⟳", styleAccent
		if m.jobRunning {
			glyph, style = "…", styleDim
		}
		put(g, bar.syncAt, 1, glyph, style, m.cols)
	}
}

// gitFooterLayout is where herdr-sidebar's git footer puts its two buttons
// (`branch_ui.rs`, draw_git_footer): " ⎇ branch " from the left edge, then
// the sync glyph with how far behind and ahead the upstream is, or the
// glyph alone without one. branch and sync are [from, to) columns.
func (m *Model) gitFooterLayout() (branch, sync barZone, branchText, syncText string) {
	icon := "⎇"
	if m.settings.Icons == IconsNerd {
		icon = "\ue725" // nf-dev-git_branch
	}
	branchText = " " + icon + " " + m.status.Branch + " "
	syncText = "⟳"
	if m.jobRunning {
		syncText = "…"
	}
	if m.status.Upstream != "" {
		syncText += " " + strconv.Itoa(m.status.Behind) + "↓ " + strconv.Itoa(m.status.Ahead) + "↑"
	}
	bw := min(runewidth.StringWidth(branchText), max(m.cols-runewidth.StringWidth(syncText), 0))
	branch = barZone{0, bw}
	sync = barZone{bw, min(bw+runewidth.StringWidth(syncText), m.cols)}
	return branch, sync, branchText, syncText
}

// drawGitFooter paints the branch and sync buttons on row y, dimmed as
// herdr-sidebar's idle buttons are.
func (m *Model) drawGitFooter(g *vt.Grid, y int) {
	branch, sync, branchText, syncText := m.gitFooterLayout()
	put(g, branch.from, y, truncate(branchText, branch.to-branch.from), styleDim, branch.to)
	put(g, sync.from, y, syncText, styleDim, m.cols)
}

// gitFooterClick answers a press on the git footer: the branch opens the
// branch picker, the sync syncs, as the git bar's do.
func (m *Model) gitFooterClick(x int) {
	branch, sync, _, _ := m.gitFooterLayout()
	switch {
	case x >= branch.from && x < branch.to:
		m.openBranches()
	case x >= sync.from && x < sync.to:
		m.startSync()
	}
}

func (m *Model) projectName() string {
	name := m.tree.Root
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// In a folder of repositories, the one the bar is about.
	if m.multiRepo() && m.active >= 0 {
		name += "/" + m.repoName(m.active)
	}
	return name
}

// gitBarClick answers a click on the header's second line.
func (m *Model) gitBarClick(x int) {
	bar := m.gitBarLayout()
	switch {
	case bar.syncAt >= 0 && x >= bar.syncAt-1:
		m.startSync()
	case bar.branchFrom >= 0 && x >= bar.branchFrom && x < bar.branchTo:
		m.openBranches()
	}
}

// --- work done away from the loop ----------------------------------------------

// Job is something slow the panel asked for — a push, a pull, a drafted
// commit message — run away from the loop so the panel keeps drawing. What
// it returns is applied to the model back on the loop, the only goroutine
// that touches it.
type Job func() func(*Model)

// JobDue hands the loop the job waiting to run, if any.
func (m *Model) JobDue() (Job, bool) {
	if m.job == nil || m.jobRunning {
		return nil, false
	}
	job := m.job
	m.job, m.jobRunning = nil, true
	return job, true
}

// ApplyJob takes a job's answer.
func (m *Model) ApplyJob(apply func(*Model)) {
	m.jobRunning = false
	if apply != nil {
		apply(m)
	}
}

// runJobNow runs the waiting job at once: what a test does in place of the
// loop.
func (m *Model) runJobNow() {
	if job, ok := m.JobDue(); ok {
		m.ApplyJob(job())
	}
}

// startSync syncs the branch with its upstream in the background.
func (m *Model) startSync() {
	if m.git.Top == "" {
		m.say("not a git repository", true)
		return
	}
	if m.jobRunning || m.job != nil {
		m.say("already syncing", false)
		return
	}
	st, g := m.status, m.git
	m.job = func() func(*Model) {
		msg, err := g.Sync(st)
		return func(m *Model) {
			if err != nil {
				m.fail(err)
			} else {
				m.say(msg, false)
			}
			m.Refresh()
		}
	}
	m.say("syncing…", false)
}

// --- the branch picker ---------------------------------------------------------

type branchPicker struct {
	all    []Branch
	shown  []Branch
	filter string
	cursor int
}

func (b *branchPicker) match() {
	b.shown = b.shown[:0]
	q := strings.ToLower(b.filter)
	for _, br := range b.all {
		if strings.Contains(strings.ToLower(br.Name), q) {
			b.shown = append(b.shown, br)
		}
	}
	b.cursor = min(b.cursor, max(len(b.shown)-1, 0))
}

func (m *Model) openBranches() {
	if m.git.Top == "" {
		m.say("not a git repository", true)
		return
	}
	all, err := m.git.Branches()
	if err != nil {
		m.fail(err)
		return
	}
	m.branches = branchPicker{all: all}
	m.branches.match()
	for i, b := range m.branches.shown {
		if b.Current {
			m.branches.cursor = i
		}
	}
	m.mode = modeBranch
}

func (m *Model) branchKey(k Key) {
	b := &m.branches
	switch k.Name {
	case "esc", "ctrl+c":
		m.mode = modeList
	case "up", "ctrl+p":
		b.cursor = max(b.cursor-1, 0)
	case "down", "ctrl+n":
		b.cursor = min(b.cursor+1, max(len(b.shown)-1, 0))
	case "enter":
		m.switchTo()
	case "backspace":
		b.filter = dropLastRune(b.filter)
		b.match()
	default:
		if k.Rune != 0 {
			b.filter += string(k.Rune)
			b.cursor = 0
			b.match()
		}
	}
}

func (m *Model) switchTo() {
	b := &m.branches
	m.mode = modeList
	if b.cursor >= len(b.shown) {
		return
	}
	br := b.shown[b.cursor]
	if br.Current {
		return
	}
	if err := m.git.Switch(br); err != nil {
		m.fail(err)
		return
	}
	m.say("on "+m.currentBranchName(br), false)
	m.Refresh()
}

func (m *Model) currentBranchName(br Branch) string { return branchLocalName(br) }

// branchLocalName is the branch a switch to br checks out: a remote one's
// local namesake.
func branchLocalName(br Branch) string {
	if br.Remote {
		return br.Name[strings.Index(br.Name, "/")+1:]
	}
	return br.Name
}

func (m *Model) branchMouse(ev Mouse) {
	b := &m.branches
	switch {
	case ev.Wheel != 0:
		b.cursor = min(max(b.cursor+ev.Wheel, 0), max(len(b.shown)-1, 0))
	case ev.Press && ev.Y >= 2:
		i := branchTop(m) + ev.Y - 2
		if i < len(b.shown) {
			if i == b.cursor {
				m.switchTo()
			} else {
				b.cursor = i
			}
		}
	}
}

func branchTop(m *Model) int { return max(m.branches.cursor-m.listRows()+1, 0) }

func (m *Model) drawBranches(g *vt.Grid) (int, int, bool) {
	b := &m.branches
	put(g, 1, 0, "switch branch", styleAccent, m.cols)
	x := put(g, 1, 1, "› ", styleAccent, m.cols)
	cx := put(g, x, 1, truncate(b.filter, m.cols-x-1), styleNormal, m.cols)
	top := branchTop(m)
	for i := 0; i < m.listRows(); i++ {
		at := top + i
		if at >= len(b.shown) {
			break
		}
		y := 2 + i
		br := b.shown[at]
		style := styleNormal
		if br.Remote {
			style = styleDim
		}
		mark := "  "
		if br.Current {
			mark, style = "● ", styleGreen
		}
		if at == b.cursor {
			fill(g, y, 0, m.cols, styleSel)
			style = styleSel
		}
		put(g, 1, y, mark+truncate(br.Name, m.cols-4), style, m.cols)
	}
	if len(b.shown) == 0 {
		put(g, 2, 2, "no branch matches", styleDim, m.cols)
	}
	put(g, 1, m.rows-1, truncate("enter switch  esc", m.cols-2), styleDim, m.cols)
	return cx, 1, true
}
