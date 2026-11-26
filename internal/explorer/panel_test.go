package explorer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// entry is the tree entry at rel, failing the test when there is none.
func entry(t *testing.T, p *Panel, rel string) Entry {
	t.Helper()
	for _, e := range p.Entries() {
		if e.Rel == rel {
			return e
		}
	}
	t.Fatalf("no entry %s in %v", rel, entryNames(p))
	return Entry{}
}

func entryNames(p *Panel) []string {
	var out []string
	for _, e := range p.Entries() {
		out = append(out, e.Rel)
	}
	return out
}

func hasEntry(p *Panel, rel string) bool {
	for _, e := range p.Entries() {
		if e.Rel == rel {
			return true
		}
	}
	return false
}

func changePaths(cs []Change) string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Path)
	}
	return strings.Join(out, ",")
}

// TestThePanelsTreeHasGitsLettersAndOpensADirectoryAtATime: the desktop's
// tree is the panel's — a directory with a change inside shows the change's
// letter, an untracked file U, an ignored directory ignored, a dotfile
// marked — and opening a directory lists its entries one level deeper, and
// hiding dotfiles hides them. If it regresses, the desktop's files panel
// disagrees with `tend files` about the same project.
func TestThePanelsTreeHasGitsLettersAndOpensADirectoryAtATime(t *testing.T) {
	dir := repo(t)
	p := NewPanel(dir)
	if p.Root() != dir || len(p.Repos()) != 1 || p.MultiRepo() {
		t.Fatalf("root %s, repos %+v", p.Root(), p.Repos())
	}
	if e := entry(t, p, "src"); !e.Dir || e.Status != 'M' || e.Expanded || e.Repo != dir {
		t.Errorf("src: %+v", e)
	}
	if e := entry(t, p, "brandnew.md"); e.Status != 'U' || e.Dir {
		t.Errorf("brandnew.md: %+v", e)
	}
	if e := entry(t, p, "build"); !e.Ignored {
		t.Errorf("build is ignored: %+v", e)
	}
	if e := entry(t, p, ".gitignore"); !e.Dot {
		t.Errorf(".gitignore is a dotfile: %+v", e)
	}
	if hasEntry(p, "src/changed.go") {
		t.Fatal("a closed directory listed its entries")
	}
	if err := p.SetExpanded("src", true); err != nil {
		t.Fatal(err)
	}
	if e := entry(t, p, "src/changed.go"); e.Depth != 1 || e.Status != 'M' || e.RepoPath != "src/changed.go" || e.Path != filepath.Join(dir, "src", "changed.go") {
		t.Errorf("src/changed.go: %+v", e)
	}
	if err := p.SetExpanded("src", false); err != nil || hasEntry(p, "src/changed.go") {
		t.Errorf("closing src: %v, %v", err, entryNames(p))
	}
	if err := p.SetExpanded("nowhere", true); !errors.Is(err, ErrNoSuchEntry) {
		t.Errorf("a path the tree lacks: %v", err)
	}
	p.SetHidden(true)
	if hasEntry(p, ".gitignore") || !p.Hidden() {
		t.Error("dotfiles hidden still lists .gitignore")
	}
	if err := p.Reveal(".gitignore"); err != nil || hasEntry(p, ".gitignore") == false {
		t.Errorf("revealing a dotfile shows dotfiles: %v", err)
	}
	if err := p.Reveal("src/changed.go"); err != nil || !entry(t, p, "src").Expanded {
		t.Errorf("reveal opens the directories above: %v", err)
	}
}

// TestThePanelGroupsChangesAndStagesDiffsAndCommits: the changes come in
// the groups a source-control view has, a diff comes as text and parsed
// with the new file's line numbers, staging moves a change between groups,
// and a commit takes what is staged — refusing an empty message and an
// empty index with the panel's words. If it regresses, the desktop's
// changes view commits nothing, or the wrong thing.
func TestThePanelGroupsChangesAndStagesDiffsAndCommits(t *testing.T) {
	withIdentity(t)
	dir := repo(t)
	p := NewPanel(dir)
	cs := p.Changes()
	if len(cs) != 1 || cs[0].Repo != dir {
		t.Fatalf("changes: %+v", cs)
	}
	if got := changePaths(cs[0].Unstaged); got != "src/changed.go" {
		t.Errorf("unstaged: %s", got)
	}
	if got := changePaths(cs[0].Untracked); got != "brandnew.md" {
		t.Errorf("untracked: %s", got)
	}
	if len(cs[0].Staged) != 0 {
		t.Errorf("staged: %s", changePaths(cs[0].Staged))
	}
	if err := p.Commit(dir, "nothing"); err == nil || !strings.Contains(err.Error(), "nothing staged") {
		t.Errorf("a commit of nothing staged: %v", err)
	}

	d, err := p.Diff(dir, "src/changed.go", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Text, "+var x = 1") || len(d.Files) != 1 || d.Files[0].Path() != "src/changed.go" {
		t.Fatalf("diff: %q %+v", d.Text, d.Files)
	}
	var added *DiffLine
	for i, l := range d.Files[0].Hunks[0].Lines {
		if l.Kind == DiffAdded && l.Text == "var x = 1" {
			added = &d.Files[0].Hunks[0].Lines[i]
		}
	}
	if added == nil || added.New != 3 || added.Old != 0 {
		t.Errorf("the added line, numbered: %+v", d.Files[0].Hunks)
	}
	if d, err := p.Diff(dir, "brandnew.md", false); err != nil || len(d.Files) != 1 || d.Files[0].OldPath != "" || d.Files[0].NewPath != "brandnew.md" {
		t.Errorf("an untracked file is all added: %v %+v", err, d.Files)
	}
	if _, err := p.Diff(dir, "kept.txt", false); !errors.Is(err, ErrNoSuchChange) {
		t.Errorf("a path with no change: %v", err)
	}

	if err := p.Stage(dir, "src/changed.go"); err != nil {
		t.Fatal(err)
	}
	cs = p.Changes()
	if changePaths(cs[0].Staged) != "src/changed.go" || len(cs[0].Unstaged) != 0 {
		t.Fatalf("after staging: %+v", cs[0])
	}
	if err := p.Discard(dir, "src/changed.go", true); !errors.Is(err, errUnstageFirst) {
		t.Errorf("discarding a staged change: %v", err)
	}
	if err := p.Commit(dir, "   "); !errors.Is(err, errEmptyMessage) {
		t.Errorf("an empty message: %v", err)
	}
	if err := p.Commit(dir, "add x"); err != nil {
		t.Fatal(err)
	}
	log, err := p.History(dir, HistoryCommits, "")
	if err != nil || len(log) != 2 || log[0].Subject != "add x" {
		t.Fatalf("history after the commit: %v %+v", err, log)
	}
	if fileLog, err := p.History(dir, HistoryCommits, "kept.txt"); err != nil || len(fileLog) != 1 || fileLog[0].Subject != "first" {
		t.Errorf("a file's history: %v %+v", err, fileLog)
	}
	shown, err := p.Show(dir, HistoryCommits, log[0].Ref, "")
	if err != nil || len(shown.Files) != 1 || shown.Files[0].Path() != "src/changed.go" || !strings.Contains(shown.Text, "add x") {
		t.Errorf("show: %v %+v", err, shown.Files)
	}
	if len(p.Changes()[0].Staged) != 0 {
		t.Error("the commit left the change staged")
	}
}

// TestThePanelDiscardsATrackedFileAndRefusesAnUntrackedOne: discard puts a
// tracked file back, and an untracked one — of which git has no copy — is
// refused. If it regresses, a button in the desktop deletes work nobody can
// get back.
func TestThePanelDiscardsATrackedFileAndRefusesAnUntrackedOne(t *testing.T) {
	dir := repo(t)
	p := NewPanel(dir)
	if err := p.Discard(dir, "brandnew.md", false); err == nil {
		t.Error("an untracked file was discarded")
	}
	if _, err := os.Stat(filepath.Join(dir, "brandnew.md")); err != nil {
		t.Fatal("the untracked file is gone")
	}
	if err := p.Discard(dir, "src/changed.go", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "src", "changed.go")); string(data) != "package src\n" {
		t.Errorf("after discard: %q", data)
	}
}

// TestThePanelOverAFolderOfRepositoriesActsOnEach: a folder of projects is
// each repository's changes under its name, its entries marked with the
// repository they are in, and a search through all of them. If it
// regresses, the desktop over ~/src shows no git, or one project's.
func TestThePanelOverAFolderOfRepositoriesActsOnEach(t *testing.T) {
	withIdentity(t)
	folder, api, web := projects(t)
	p := NewPanel(folder)
	repos := p.Repos()
	if !p.MultiRepo() || len(repos) != 2 || repos[0].Top != api || repos[0].Name != "api" || repos[1].Name != "clients/web" || repos[0].Branch != "main" {
		t.Fatalf("repos: %+v", repos)
	}
	cs := p.Changes()
	if changePaths(cs[0].Unstaged) != "readme.txt" || changePaths(cs[1].Untracked) != "new.js" {
		t.Errorf("changes: %+v", cs)
	}
	if e := entry(t, p, "api"); e.Status != 'M' || e.Repo != api {
		t.Errorf("api: %+v", e)
	}
	if e := entry(t, p, "notes"); e.Repo != "" || e.Status != 0 {
		t.Errorf("a plain folder: %+v", e)
	}
	if err := p.Stage(web, "new.js"); err != nil {
		t.Fatal(err)
	}
	if err := p.Commit(web, "add new.js"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitIn(t, web, "log", "-1", "--format=%s")); got != "add new.js" {
		t.Errorf("web's last commit: %s", got)
	}
	if got := strings.TrimSpace(gitIn(t, api, "log", "-1", "--format=%s")); got != "first" {
		t.Errorf("api was committed to: %s", got)
	}
	res, err := p.Search(context.Background(), GrepOptions{Query: "changed"})
	if err != nil || len(res.Hits) != 1 || res.Hits[0].Path != "api/readme.txt" {
		t.Errorf("a search of the folder: %v %+v", err, res)
	}
	if found := p.FindFiles(context.Background(), "todo"); len(found) == 0 || found[0] != "notes/todo.txt" {
		t.Errorf("finding a file in the folder: %v", found)
	}
	if _, err := p.Branches("/nowhere"); err == nil {
		t.Error("a repository the panel does not show was asked")
	}
}

// TestThePanelFindsFilesByNameAndTextLikeThePanel: the quick open ranks a
// file whose own name starts with the query first, and the text search
// says where each match is and how long it is, with the switches. If it
// regresses, the desktop's search finds other files than `tend files`.
func TestThePanelFindsFilesByNameAndTextLikeThePanel(t *testing.T) {
	dir := repo(t)
	write(t, dir, "src/other.go", "package src\n\n// Changed here, not var.\n")
	p := NewPanel(dir)
	found := p.FindFiles(context.Background(), "chan")
	if len(found) == 0 || found[0] != "src/changed.go" {
		t.Errorf("quick open: %v", found)
	}
	for _, f := range found {
		if strings.HasPrefix(f, "build/") {
			t.Errorf("an ignored file was found: %s", f)
		}
	}
	res, err := p.Search(context.Background(), GrepOptions{Query: "var"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 || res.Files != 2 {
		t.Fatalf("var: %+v", res)
	}
	for _, h := range res.Hits {
		if h.Text[h.Col:h.Col+h.Len] != "var" {
			t.Errorf("the match is not where it is said to be: %+v", h)
		}
	}
	res, err = p.Search(context.Background(), GrepOptions{Query: "x\\s*=", Regex: true, Include: "*.go"})
	if err != nil || len(res.Hits) != 1 || res.Hits[0].Line != 3 || res.Hits[0].Text[res.Hits[0].Col:res.Hits[0].Col+res.Hits[0].Len] != "x =" {
		t.Errorf("a pattern: %v %+v", err, res)
	}
	if res, err := p.Search(context.Background(), GrepOptions{Query: "changed", Case: true}); err != nil || len(res.Hits) != 0 {
		t.Errorf("case-sensitive: %v %+v", err, res)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Search(ctx, GrepOptions{Query: "var"}); err == nil {
		t.Error("a search whose context ended answered")
	}
}

// TestThePanelSendsAReviewToTheAgentItFollows: a comment about a line of a
// diff goes to the reviewer as the panel words it, preferring the pane the
// panel follows; an empty one and one with nobody to send it to are
// refused. If it regresses, the desktop's review reaches no agent, or
// another tab's.
func TestThePanelSendsAReviewToTheAgentItFollows(t *testing.T) {
	dir := repo(t)
	p := NewPanel(dir)
	if _, err := p.Review("src/changed.go", 3, "why"); !errors.Is(err, errNoReviewer) {
		t.Errorf("no reviewer: %v", err)
	}
	rev := &reviewingOpener{}
	p.SetReviewer(rev)
	p.SetNeighbours(&fakeNeighbours{siblings: []Sibling{{Pane: "p_7", Cwd: dir}}})
	p.Follow()
	if _, err := p.Review("src/changed.go", 3, "  \n "); !errors.Is(err, errEmptyReview) {
		t.Errorf("an empty review: %v", err)
	}
	agent, err := p.Review("src/changed.go", 3, "name it\nbetter")
	if err != nil || agent != "claude" {
		t.Fatalf("review: %v %q", err, agent)
	}
	if len(rev.sent) != 1 || rev.sent[0] != "Review of src/changed.go around line 3: name it better" || rev.prefer[0] != "p_7" {
		t.Errorf("sent %q to %q", rev.sent, rev.prefer)
	}
}

// TestThePanelFollowsThePaneBesideIt: the panel moves to the project the
// pane it follows moves to, says where, and stays within one. If it
// regresses, the desktop's files panel shows the project the agent left.
func TestThePanelFollowsThePaneBesideIt(t *testing.T) {
	first, second := repo(t), repo(t)
	n := &fakeNeighbours{siblings: []Sibling{{Pane: "p_1", Cwd: second}, {Pane: "p_2", Cwd: first + "/src"}}}
	p := NewPanel(first)
	p.SetNeighbours(n)
	if root, ok := p.Follow(); ok || p.Root() != first {
		t.Fatalf("the panel left the project it opened in for %s", root)
	}
	n.siblings[1].Cwd = second + "/src"
	if root, ok := p.Follow(); !ok || root != second || p.Root() != second {
		t.Fatalf("followed to %s %v", root, ok)
	}
	n.siblings[1].Cwd = second
	if _, ok := p.Follow(); ok {
		t.Error("moving within the repository moved the panel")
	}
	if err := p.ChangeFolder(first); err != nil || p.Root() != first {
		t.Fatalf("change folder: %v %s", err, p.Root())
	}
	n.siblings[0].Focused = true
	if _, ok := p.Follow(); ok {
		t.Error("a change of focus took a folder chosen by hand away")
	}
	if err := p.ChangeFolder(filepath.Join(first, "kept.txt")); err == nil {
		t.Error("a file was taken for a folder")
	}
}

// TestThePanelSyncsAndSwitchesBranches: the branch list has the remote
// branch with no local one, switching to it makes a local branch, and sync
// pushes what is ahead and says so. If it regresses, the desktop's branch
// bar shows branches it cannot switch to and a sync that does nothing.
func TestThePanelSyncsAndSwitchesBranches(t *testing.T) {
	work, _ := remoteAndClone(t)
	write(t, work, "b.txt", "b\n")
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "ahead")
	p := NewPanel(work)
	if r := p.Repos()[0]; r.Branch != "main" || r.Ahead != 1 || r.Upstream != "origin/main" {
		t.Fatalf("repo: %+v", r)
	}
	msg, err := p.Sync(context.Background(), work)
	if err != nil || msg != "pushed 1" {
		t.Fatalf("sync: %q %v", msg, err)
	}
	if r := p.Repos()[0]; r.Ahead != 0 {
		t.Errorf("after the sync: %+v", r)
	}
	branches, err := p.Branches(work)
	if err != nil {
		t.Fatal(err)
	}
	var feature Branch
	for _, b := range branches {
		if b.Name == "origin/feature" {
			feature = b
		}
	}
	if !feature.Remote {
		t.Fatalf("branches: %+v", branches)
	}
	if on, err := p.Switch(work, feature); err != nil || on != "feature" || p.Repos()[0].Branch != "feature" {
		t.Errorf("switch: %q %v %+v", on, err, p.Repos()[0])
	}
}

// TestThePanelKeepsStashes: a stash is listed, applied with pop, and gone.
// If it regresses, the desktop's stash list cannot give back what was
// shelved.
func TestThePanelKeepsStashes(t *testing.T) {
	withIdentity(t)
	dir := repo(t)
	gitIn(t, dir, "stash", "-q")
	p := NewPanel(dir)
	stashes, err := p.History(dir, HistoryStashes, "")
	if err != nil || len(stashes) != 1 || stashes[0].Ref != "stash@{0}" {
		t.Fatalf("stashes: %v %+v", err, stashes)
	}
	if d, err := p.Show(dir, HistoryStashes, "stash@{0}", ""); err != nil || len(d.Files) != 1 {
		t.Errorf("show a stash: %v %+v", err, d.Files)
	}
	if err := p.StashApply(dir, "stash@{0}", true); err != nil {
		t.Fatal(err)
	}
	if changePaths(p.Changes()[0].Unstaged) != "src/changed.go" {
		t.Errorf("the stash did not come back: %+v", p.Changes())
	}
	if stashes, _ := p.History(dir, HistoryStashes, ""); len(stashes) != 0 {
		t.Errorf("pop kept the stash: %+v", stashes)
	}
	gitIn(t, dir, "tag", "v1")
	if tags, err := p.History(dir, HistoryTags, ""); err != nil || len(tags) != 1 || tags[0].Ref != "v1" {
		t.Errorf("tags: %v %+v", err, tags)
	}
}

// TestThePanelDraftsACommitMessage: the drafted message is claude's line,
// from the diff it was given; with nothing changed it says so. If it
// regresses, the desktop's ✧ fills the commit box with nothing.
func TestThePanelDraftsACommitMessage(t *testing.T) {
	record := fakeClaude(t, "Add x to src")
	dir := repo(t)
	p := NewPanel(dir)
	msg, err := p.DraftMessage(context.Background(), dir)
	if err != nil || msg != "Add x to src" {
		t.Fatalf("draft: %q %v", msg, err)
	}
	if data, _ := os.ReadFile(record); !strings.Contains(string(data), "var x = 1") {
		t.Errorf("claude was not given the diff: %q", data)
	}
	clean := t.TempDir()
	gitIn(t, clean, "init", "-q")
	q := NewPanel(clean)
	if _, err := q.DraftMessage(context.Background(), clean); !errors.Is(err, errNothingToDescribe) {
		t.Errorf("nothing changed: %v", err)
	}
}

// TestThePanelWorksInAJJRepository: in jj every change is the working-copy
// commit's, so it is listed as staged, staging is refused as the panel
// refuses it, a commit commits @, and a merge is refused with jj's way
// said. If it regresses, the desktop's changes view runs git in a jj
// repository and makes commits jj has to import.
func TestThePanelWorksInAJJRepository(t *testing.T) {
	dir := jjRepo(t, false)
	write(t, dir, "keep.txt", "b\n")
	p := NewPanel(dir)
	repos := p.Repos()
	if len(repos) != 1 || !repos[0].JJ {
		t.Fatalf("repos: %+v", repos)
	}
	cs := p.Changes()
	if changePaths(cs[0].Staged) != "keep.txt" || !cs[0].JJ {
		t.Fatalf("changes: %+v", cs)
	}
	if err := p.Stage(dir, "keep.txt"); !errors.Is(err, errNoStaging) {
		t.Errorf("staging in jj: %v", err)
	}
	if d, err := p.Diff(dir, "keep.txt", true); err != nil || len(d.Files) != 1 || d.Files[0].Path() != "keep.txt" {
		t.Errorf("a jj diff: %v %+v", err, d.Files)
	}
	if _, err := p.PlanFinish(dir, FinishMerge); !errors.Is(err, errJJMerge) {
		t.Errorf("a merge in jj: %v", err)
	}
	if err := p.Commit(dir, "change keep"); err != nil {
		t.Fatal(err)
	}
	if cs := p.Changes(); len(cs[0].Staged) != 0 {
		t.Errorf("after the commit: %+v", cs)
	}
	if !strings.Contains(run(t, dir, "jj", "log", "--no-graph", "-r", "@-", "-T", "description"), "change keep") {
		t.Error("the commit is not @-")
	}
}

// TestThePreviewDataHasTokensMarkdownSourceAndImageSizes: a source file
// comes as lines of tokens by kind, markdown as its source for the client
// to render, an image as its size, a binary as binary. If it regresses, the
// desktop's preview colours nothing, or shows a picture as bytes.
func TestThePreviewDataHasTokensMarkdownSourceAndImageSizes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.go", "package main\n\n// hello\nvar s = \"hi\" + 42\n")
	fp, err := ReadPreview(filepath.Join(dir, "main.go"))
	if err != nil || fp.Kind != PreviewText || len(fp.Lines) != 4 {
		t.Fatalf("go: %v %+v", err, fp)
	}
	kinds := map[string]TokenKind{}
	for _, line := range fp.Lines {
		for _, tok := range line {
			kinds[tok.Text] = tok.Kind
		}
	}
	if kinds["package"] != TokenKeyword || kinds["// hello"] != TokenComment || kinds[`"hi"`] != TokenString || kinds["42"] != TokenNumber {
		t.Errorf("tokens: %v", kinds)
	}
	write(t, dir, "README.md", "# Title\n\ntext\n")
	if fp, err := ReadPreview(filepath.Join(dir, "README.md")); err != nil || fp.Kind != PreviewMarkdown || fp.Source != "# Title\n\ntext\n" || fp.Lines[0][0].Kind != TokenHeading {
		t.Errorf("markdown: %v %+v", err, fp)
	}
	writePNG(t, filepath.Join(dir, "pic.png"), 30, 20)
	if fp, err := ReadPreview(filepath.Join(dir, "pic.png")); err != nil || fp.Kind != PreviewImage || fp.Width != 30 || fp.Height != 20 {
		t.Errorf("image: %v %+v", err, fp)
	}
	write(t, dir, "blob.bin", "a\x00b")
	if fp, err := ReadPreview(filepath.Join(dir, "blob.bin")); err != nil || fp.Kind != PreviewBinary || fp.Source != "" {
		t.Errorf("binary: %v %+v", err, fp)
	}
	if _, err := ReadPreview(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("a missing file previewed")
	}
}

// TestThePanelIsSafeToCallFromSeveralGoroutines: the desktop refreshes on a
// timer while the person clicks; under the race detector, neither corrupts
// the other. If it regresses, the desktop crashes on a map written twice.
func TestThePanelIsSafeToCallFromSeveralGoroutines(t *testing.T) {
	dir := repo(t)
	p := NewPanel(dir)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				switch i {
				case 0:
					p.Refresh()
				case 1:
					_ = p.SetExpanded("src", j%2 == 0)
				case 2:
					_ = p.Entries()
					_ = p.Changes()
				case 3:
					p.SetHidden(j%2 == 0)
					_ = p.Repos()
				}
			}
		}(i)
	}
	wg.Wait()
}
