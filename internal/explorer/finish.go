package explorer

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// Finishing a worktree's work from the changes view (issue #45): once the
// agent's changes are committed, M merges its branch into the branch the
// main worktree has checked out, and R opens a pull request for it — the two
// ways a branch an agent worked on ends. Both ask for the key twice, as
// discarding does: a merge lands on another branch, and a pull request is
// published for everyone with access to the repository.

// Worktree is one of the repository's working trees, as
// `git worktree list --porcelain` lists them.
type Worktree struct {
	Path   string
	Branch string // short name; "" when detached
}

// Worktrees lists the repository's working trees, the main one first.
func (g Git) Worktrees() ([]Worktree, error) {
	out, err := g.run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []Worktree
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			all = append(all, Worktree{Path: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch ") && len(all) > 0:
			all[len(all)-1].Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	return all, nil
}

// MergeTarget is where this worktree's branch would be merged: the main
// worktree and the branch it has checked out. A merge happens there because
// git will not check a branch out in two worktrees at once — and the main
// worktree is where the base branch already is.
func (g Git) MergeTarget(st *Status) (Worktree, error) {
	if st == nil || st.Branch == "" || st.detachedOrNone {
		return Worktree{}, errors.New("not on a branch")
	}
	all, err := g.Worktrees()
	if err != nil {
		return Worktree{}, err
	}
	if len(all) == 0 || sameDir(all[0].Path, g.Top) {
		return Worktree{}, errors.New("this is the main worktree: merge is for an agent's worktree")
	}
	main := all[0]
	if main.Branch == "" {
		return Worktree{}, errors.New("the main worktree is not on a branch")
	}
	if main.Branch == st.Branch {
		return Worktree{}, errors.New("the main worktree is on " + st.Branch + " too")
	}
	return main, nil
}

// Merge merges this worktree's branch into the main worktree's, with a merge
// commit, so the agent's work stays one piece of history. Uncommitted work
// on either side refuses it — here it would be left out of the merge, there
// it would be mixed into it — and a conflict is undone, leaving the main
// worktree as it was: resolving it is work for a person or an agent, not a
// panel.
func (g Git) Merge(st *Status) (string, error) {
	target, err := g.MergeTarget(st)
	if err != nil {
		return "", err
	}
	for _, c := range st.Changes {
		if !c.Untracked() {
			return "", errors.New("uncommitted changes here: commit them first, or they are left out")
		}
	}
	there := Git{Top: target.Path}
	dirty, err := there.run("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(dirty)) > 0 {
		return "", errors.New("the main worktree (" + target.Branch + ") has uncommitted changes")
	}
	if _, err := there.run("merge", "--no-ff", "--no-edit", st.Branch); err != nil {
		_, _ = there.run("merge", "--abort")
		return "", errors.New("conflict merging into " + target.Branch + ": undone, nothing changed")
	}
	return "merged " + st.Branch + " into " + target.Branch, nil
}

// PullRequest publishes the branch — pushing what the remote lacks — and
// opens a pull request for it with gh, titled and described from its
// commits. It says the pull request's address, or the one already open.
func (g Git) PullRequest(st *Status) (string, error) {
	if st == nil || st.Branch == "" || st.detachedOrNone {
		return "", errors.New("not on a branch")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", errors.New("a pull request needs the gh command (cli.github.com)")
	}
	if st.Upstream == "" {
		if err := g.runNet("push", "-u", "origin", st.Branch); err != nil {
			return "", err
		}
	} else if st.Ahead > 0 {
		if err := g.runNet("push", "--quiet"); err != nil {
			return "", err
		}
	}
	cmd := exec.Command("gh", "pr", "create", "--fill", "--head", st.Branch)
	cmd.Dir = g.Top
	// gh asks on the terminal for what it lacks; the panel's terminal is
	// nobody's to answer, so it is told not to.
	cmd.Env = append(networkEnv(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		// An open one is not a failure: gh names it on its last line.
		if strings.Contains(msg, "already exists") {
			lines := strings.Split(msg, "\n")
			return "already open: " + strings.TrimSpace(lines[len(lines)-1]), nil
		}
		if msg != "" {
			return "", errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	return "pull request: " + strings.TrimSpace(lines[len(lines)-1]), nil
}

// startFinish runs a merge or a pull request in the background, as sync
// does, after the key was pressed twice.
func (m *Model) startFinish(key, pending string) {
	if m.jobRunning || m.job != nil {
		m.say("busy: wait for the last one to finish", false)
		return
	}
	st, g := m.status, m.git
	if st == nil || st.Branch == "" || st.detachedOrNone {
		m.say("not on a branch", true)
		return
	}
	var want, ask, doing string
	var run func(*Status) (string, error)
	if key == "M" {
		target, err := g.MergeTarget(st)
		if err != nil {
			m.fail(err)
			return
		}
		want = "merge " + st.Branch
		ask = "M again to merge " + st.Branch + " into " + target.Branch
		doing, run = "merging…", g.Merge
	} else {
		want = "pr " + st.Branch
		ask = "R again to open a pull request for " + st.Branch
		doing, run = "opening the pull request…", g.PullRequest
	}
	if pending != want {
		m.confirm = want
		m.say(ask, true)
		return
	}
	m.job = func() func(*Model) {
		msg, err := run(st)
		return func(m *Model) {
			if err != nil {
				m.fail(err)
			} else {
				m.say(msg, false)
			}
			m.Refresh()
		}
	}
	m.say(doing, false)
}

// sameDir is whether two paths are one directory, links resolved: git
// prints a worktree's real path, and the panel may have been started through
// a link to it.
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}
