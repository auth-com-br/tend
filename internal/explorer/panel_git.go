package explorer

import (
	"context"
	"errors"
	"strings"
)

// The Panel's git: the changes view, the commit box, the review, history and
// the branch bar, each a call that does what the panel's key does and
// refreshes after, as the panel does. Paths are relative to the
// repository's top, as git names them.

// RepoChanges is one repository's changes, in the groups a source-control
// view shows them in. A change can be in Staged and Unstaged both: staged,
// then changed again.
type RepoChanges struct {
	Repo string // its top
	Name string
	JJ   bool
	// Staged is what the next commit takes: the index's, or in jj every
	// change in the working-copy commit (@), which has no index.
	Staged []Change
	// Unstaged is a tracked file's change not yet staged.
	Unstaged []Change
	// Untracked is a file git has never been told about, Conflicted a path
	// mid-merge; neither is in the two groups above.
	Untracked  []Change
	Conflicted []Change
}

// Changes lists every repository's changes, in Repos's order, a repository
// with none included with its groups empty.
func (p *Panel) Changes() []RepoChanges {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]RepoChanges, 0, len(p.repos))
	for _, r := range p.repos {
		rc := RepoChanges{Repo: r.git.Top, Name: repoLabel(p.tree.Root, r.git.Top), JJ: r.git.JJ}
		if r.status != nil {
			for _, c := range r.status.Changes {
				switch {
				case c.Conflicted():
					rc.Conflicted = append(rc.Conflicted, c)
				case c.Untracked():
					rc.Untracked = append(rc.Untracked, c)
				default:
					if c.Staged() {
						rc.Staged = append(rc.Staged, c)
					}
					if c.Unstaged() {
						rc.Unstaged = append(rc.Unstaged, c)
					}
				}
			}
		}
		out = append(out, rc)
	}
	return out
}

// ErrNoSuchChange is a path git has nothing to say about.
var ErrNoSuchChange = errors.New("no change at that path")

// change is the change at a path of a repository, from the last status.
func (p *Panel) change(repo, path string) (Git, Change, error) {
	g, st, err := p.repo(repo)
	if err != nil {
		return Git{}, Change{}, err
	}
	c, ok := st.Of(path)
	if !ok {
		return Git{}, Change{}, ErrNoSuchChange
	}
	return g, c, nil
}

// Diff is a change's diff: what is staged of it, or what is not. An
// untracked file is all added; in jj, the change in @.
func (p *Panel) Diff(repo, path string, staged bool) (Diff, error) {
	g, c, err := p.change(repo, path)
	if err != nil {
		return Diff{}, err
	}
	text, err := g.Diff(c, staged)
	if err != nil {
		return Diff{}, err
	}
	return newDiff(text), nil
}

// Stage stages a path, a directory's every change under it; Unstage takes
// one out of the index, leaving the file. Both are refused in jj, which has
// no index.
func (p *Panel) Stage(repo, path string) error {
	return p.act(repo, func(g Git) error { return g.Stage(path) })
}

// Unstage takes a path out of the index.
func (p *Panel) Unstage(repo, path string) error {
	return p.act(repo, func(g Git) error { return g.Unstage(path) })
}

// StageAll stages everything, as git add -A.
func (p *Panel) StageAll(repo string) error {
	return p.act(repo, func(g Git) error { return g.StageAll() })
}

// errUnstageFirst refuses to discard what is staged: discarding puts the
// working copy back as the index has it, and the staged change is the
// index's.
var errUnstageFirst = errors.New("unstage it first: discarding puts back the working copy")

// Discard puts a file back as the index has it (in jj, as @'s parent has
// it), losing its change: the client asks first, as the panel asks for x
// twice. A staged change is refused, and an untracked file, of which git
// has no copy.
func (p *Panel) Discard(repo, path string, staged bool) error {
	g, c, err := p.change(repo, path)
	if err != nil {
		return err
	}
	if staged && !g.JJ {
		return errUnstageFirst
	}
	err = g.Discard(c)
	p.Refresh()
	return err
}

// act runs a change to a repository and refreshes, whether it worked or
// not: a failed git add can still have added some.
func (p *Panel) act(repo string, do func(Git) error) error {
	g, _, err := p.repo(repo)
	if err != nil {
		return err
	}
	err = do(g)
	p.Refresh()
	return err
}

// errEmptyMessage is a commit with nothing to say.
var errEmptyMessage = errors.New("a commit needs a message")

// Commit commits what is staged (in jj, @) with a message, refusing an
// empty message or nothing staged as the panel's commit box does.
func (p *Panel) Commit(repo, message string) error {
	g, st, err := p.repo(repo)
	if err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return errEmptyMessage
	}
	staged := 0
	if st != nil {
		for _, c := range st.Changes {
			if c.Staged() {
				staged++
			}
		}
	}
	if err := nothingToCommit(g, staged); err != nil {
		return err
	}
	err = g.Commit(message)
	p.Refresh()
	return err
}

// DraftMessage drafts a commit message for what is staged, or else what is
// changed: the panel's ✧, asking the local claude and naming the files when
// it cannot (suggest.go). It takes up to a minute.
func (p *Panel) DraftMessage(ctx context.Context, repo string) (string, error) {
	g, _, err := p.repo(repo)
	if err != nil {
		return "", err
	}
	diff, files, err := draftInput(g.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return SuggestMessage(ctx, diff, files), nil
}

// --- review and finishing --------------------------------------------------------

// errNoReviewer is a review with nobody to send it to.
var errNoReviewer = errors.New("no agent to send it to: not running in a tend pane")

// errEmptyReview is a review with nothing in it.
var errEmptyReview = errors.New("the review is empty")

// Review sends a comment about a file of a diff, and the line of the new
// file it is about (0 for the file as a whole), to the agent working
// beside the panel — the one it follows first — and says which agent it
// went to: the panel's r in a diff (review.go).
func (p *Panel) Review(path string, line int, comment string) (string, error) {
	p.mu.Lock()
	rev, prefer := p.reviewer, p.follow.selected
	p.mu.Unlock()
	if rev == nil {
		return "", errNoReviewer
	}
	comment = strings.Join(strings.Fields(comment), " ")
	if comment == "" {
		return "", errEmptyReview
	}
	return rev.Review(prefer, ReviewMessage(reviewAbout(path, line), comment))
}

// PlanFinish is what merging an agent's branch (M M in the panel) or
// opening its pull request (R R) would do, for the client to ask about, or
// why it cannot be done: the panel's refusals, before anything runs.
func (p *Panel) PlanFinish(repo string, action FinishAction) (FinishPlan, error) {
	g, st, err := p.repo(repo)
	if err != nil {
		return FinishPlan{}, err
	}
	return planFinish(g, st, action)
}

// Finish merges or opens the pull request, once the person has confirmed
// the plan, and says what it did. A pull request pushes first; ctx ends it.
func (p *Panel) Finish(ctx context.Context, repo string, action FinishAction) (string, error) {
	g, st, err := p.repo(repo)
	if err != nil {
		return "", err
	}
	plan, err := planFinish(g.WithContext(ctx), st, action)
	if err != nil {
		return "", err
	}
	msg, err := plan.run(st)
	p.Refresh()
	return msg, err
}

// --- history ---------------------------------------------------------------------

// History lists the recent commits, the stashes or the tags; with a path
// (relative to the top), the commits that touched that file.
func (p *Panel) History(repo string, kind HistoryKind, path string) ([]HistoryEntry, error) {
	g, _, err := p.repo(repo)
	if err != nil {
		return nil, err
	}
	switch kind {
	case HistoryStashes:
		return g.Stashes()
	case HistoryTags:
		return g.Tags()
	}
	return g.Log(path)
}

// Show is a commit, stash or tag as git shows it, with its diff — of one
// file, when path is set.
func (p *Panel) Show(repo string, kind HistoryKind, ref, path string) (Diff, error) {
	g, _, err := p.repo(repo)
	if err != nil {
		return Diff{}, err
	}
	text, err := g.Show(kind, ref, path)
	if err != nil {
		return Diff{}, err
	}
	return newDiff(text), nil
}

// StashApply applies a stash, and with pop drops it once applied.
func (p *Panel) StashApply(repo, ref string, pop bool) error {
	return p.act(repo, func(g Git) error { return g.StashApply(ref, pop) })
}

// StashDrop throws a stash away; the client asks first, as the panel asks
// for "yes".
func (p *Panel) StashDrop(repo, ref string) error {
	return p.act(repo, func(g Git) error { return g.StashDrop(ref) })
}

// --- branches --------------------------------------------------------------------

// Branches lists the local branches, then the remote ones no local branch
// has the name of; in jj, the bookmarks.
func (p *Panel) Branches(repo string) ([]Branch, error) {
	g, _, err := p.repo(repo)
	if err != nil {
		return nil, err
	}
	return g.Branches()
}

// Switch goes to a branch and says the branch now checked out: a remote one
// becomes a local branch tracking it; in jj, a new change on the bookmark.
func (p *Panel) Switch(repo string, b Branch) (string, error) {
	g, _, err := p.repo(repo)
	if err != nil {
		return "", err
	}
	if b.Current {
		return branchLocalName(b), nil
	}
	err = g.Switch(b)
	p.Refresh()
	if err != nil {
		return "", err
	}
	return branchLocalName(b), nil
}

// Sync is the sync button: pull what is behind (fast-forward only), push
// what is ahead, publish a branch with no upstream to origin; in jj, fetch
// and push. It says what it did; ctx ends it.
func (p *Panel) Sync(ctx context.Context, repo string) (string, error) {
	g, st, err := p.repo(repo)
	if err != nil {
		return "", err
	}
	msg, err := g.WithContext(ctx).Sync(st)
	p.Refresh()
	return msg, err
}
