package explorer

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Jujutsu (jj) is tend's own; herdr has no jj support, and it is one of the
// things asked of it most (herdr#2209, #480). A repository is jj's when a
// `.jj` directory holds it, beside a `.git` (colocated, jj's default) or
// not. In one, the panel asks jj, not git: in a colocated repository git
// sees a detached HEAD and jj's working-copy commit as unstaged changes, and
// a `git add` and `git commit` from the panel would make commits jj has to
// import and the person did not mean; in one that is not colocated there is
// no .git at all and git sees nothing.
//
// jj has no index. Every change in the working copy is already part of the
// working-copy commit, @, which `jj commit` describes and closes. So in jj
// the panel lists one group, the changes in @ (kept in Change.Index, so the
// rest of the panel treats them as ready to commit), staging is refused with
// that said, and the commit box commits @.

// errNoStaging is what staging says in a jj repository.
var errNoStaging = errors.New("jj has no staging: every change is already in the working-copy commit (@); c commits it")

// findJJ is the jj workspace dir is in: the nearest directory above it
// holding a .jj directory. It is read off the disk, not asked of jj, since
// the panel looks every time it follows a pane, and jj may not be
// installed where git is.
func findJJ(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for d := abs; ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, ".jj")); err == nil && info.IsDir() {
			return d
		}
		if parent := filepath.Dir(d); parent == d {
			return ""
		}
	}
}

// colocated is whether a jj repository has a git one in the same place,
// which git's own commands (history, ignore rules) can still be asked of.
func (g Git) colocated() bool {
	info, err := os.Stat(filepath.Join(g.Top, ".git"))
	return err == nil && info != nil
}

// jj runs jj in the workspace and returns what it printed, or jj's reason.
// --color=never and --no-pager because the panel reads the text; jj
// snapshots the working copy first, as every jj command does, which is what
// makes a file just written show up.
func (g Git) jj(args ...string) ([]byte, error) {
	if g.Top == "" {
		return nil, ErrNoRepo
	}
	cmd := exec.Command("jj", append([]string{"-R", g.Top, "--color=never", "--no-pager"}, args...)...)
	// jj writes paths relative to where it is run, not to -R: run from
	// elsewhere, a change at the top was "../../repo/keep.txt".
	cmd.Dir = g.Top
	cmd.Env = networkEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("this is a jj repository, and jj is not installed")
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			// jj's reason is its "Error:" line; hints follow it.
			for _, l := range strings.Split(msg, "\n") {
				if rest, ok := strings.CutPrefix(l, "Error: "); ok {
					return out, errors.New(rest)
				}
			}
			return out, errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return out, err
	}
	return out, nil
}

// jjStatus is the working-copy commit's changes and conflicts, and where it
// is: the bookmark on @, else the one on its parent, else @'s change id.
func (g Git) jjStatus() (*Status, error) {
	out, err := g.jj("diff", "-r", "@", "--summary")
	if err != nil {
		return nil, err
	}
	st := parseJJSummary(out)
	// "No conflicts found" is an exit of 2, not a failure to report.
	if conflicts, err := g.jj("resolve", "--list", "-r", "@"); err == nil {
		for _, line := range strings.Split(string(conflicts), "\n") {
			if path, _, ok := strings.Cut(line, "    "); ok && path != "" {
				st.add(Change{Path: strings.TrimSpace(path), Index: 'U', Work: 'U'})
			}
		}
		sortChanges(st)
	}
	where, err := g.jj("log", "--no-graph", "-r", "@", "-T",
		`local_bookmarks.map(|b| b.name()).join(",") ++ "\t" ++ parents.map(|c| c.local_bookmarks().map(|b| b.name()).join(",")).join(",") ++ "\t" ++ change_id.shortest(8)`)
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(where)), "\t")
		for len(fields) < 3 {
			fields = append(fields, "")
		}
		switch {
		case fields[0] != "":
			st.Branch = firstOf(fields[0])
		case fields[1] != "":
			st.Branch = firstOf(fields[1]) + "+"
		default:
			st.Branch = "@" + fields[2]
		}
	}
	return st, nil
}

func firstOf(list string) string { return strings.SplitN(list, ",", 2)[0] }

func sortChanges(st *Status) {
	sort.Slice(st.Changes, func(a, b int) bool { return st.Changes[a].Path < st.Changes[b].Path })
}

// parseJJSummary reads `jj diff --summary`: a letter and a path a line, a
// rename or copy written as dir/{old => new}.
func parseJJSummary(out []byte) *Status {
	st := &Status{byPath: map[string]Change{}, dirtyDirs: map[string]byte{}}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 3 || line[1] != ' ' {
			continue
		}
		letter, path := line[0], line[2:]
		c := Change{Path: path, Index: letter, Work: ' '}
		if letter == 'R' || letter == 'C' {
			c.Orig, c.Path = splitJJRename(path)
		}
		st.add(c)
	}
	sortChanges(st)
	return st
}

// splitJJRename turns "src/{a.go => b.go}" into "src/a.go", "src/b.go".
func splitJJRename(s string) (from, to string) {
	open, end := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if open < 0 || end < open {
		if a, b, ok := strings.Cut(s, " => "); ok {
			return a, b
		}
		return "", s
	}
	prefix, suffix := s[:open], s[end+1:]
	a, b, _ := strings.Cut(s[open+1:end], " => ")
	clean := func(p string) string { return strings.ReplaceAll(prefix+p+suffix, "//", "/") }
	return clean(a), clean(b)
}

func (g Git) jjDiff(c Change) (string, error) {
	args := []string{"diff", "-r", "@", "--git", "--"}
	if c.Orig != "" {
		args = append(args, c.Orig)
	}
	out, err := g.jj(append(args, c.Path)...)
	return string(out), err
}

func (g Git) jjDiscard(c Change) error {
	args := []string{"restore", "--"}
	if c.Orig != "" {
		args = append(args, c.Orig)
	}
	_, err := g.jj(append(args, c.Path)...)
	return err
}

func (g Git) jjFiles() ([]string, error) {
	out, err := g.jj("file", "list")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// jjBranches are jj's bookmarks: the local ones, then the remote ones no
// local bookmark has the name of.
func (g Git) jjBranches() ([]Branch, error) {
	out, err := g.jj("bookmark", "list", "--all-remotes", "-T", `name ++ "\t" ++ if(remote, remote, "") ++ "\n"`)
	if err != nil {
		return nil, err
	}
	current := map[string]bool{}
	if here, err := g.jj("log", "--no-graph", "-r", "@", "-T", `local_bookmarks.map(|b| b.name()).join(",")`); err == nil {
		for _, b := range strings.Split(strings.TrimSpace(string(here)), ",") {
			current[b] = b != ""
		}
	}
	var local, remote []Branch
	have := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		name, rem, ok := strings.Cut(line, "\t")
		if !ok || name == "" {
			continue
		}
		switch rem {
		case "":
			have[name] = true
			local = append(local, Branch{Name: name, Current: current[name]})
		case "git":
			// The colocated git repository's own view of the bookmark.
		default:
			remote = append(remote, Branch{Name: name + "@" + rem, Remote: true})
		}
	}
	all := local
	for _, r := range remote {
		if !have[r.Name[:strings.LastIndexByte(r.Name, '@')]] {
			all = append(all, r)
		}
	}
	return all, nil
}

// jjSwitch goes to a bookmark the jj way: a new change on top of it, which
// leaves the change being worked on where it was.
func (g Git) jjSwitch(b Branch) error {
	_, err := g.jj("new", b.Name)
	return err
}

// jjSync fetches from the remotes and pushes the bookmarks that track one
// and have moved. jj says when there was nothing to push.
func (g Git) jjSync() (string, error) {
	if _, err := g.jj("git", "fetch"); err != nil {
		return "", err
	}
	cmd := exec.Command("jj", "-R", g.Top, "--color=never", "--no-pager", "git", "push")
	cmd.Dir = g.Top
	cmd.Env = networkEnv()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	said := strings.TrimSpace(out.String())
	if err != nil {
		for _, l := range strings.Split(said, "\n") {
			if rest, ok := strings.CutPrefix(l, "Error: "); ok {
				return "fetched", errors.New(rest)
			}
		}
		return "fetched", err
	}
	if strings.Contains(said, "Nothing changed") {
		return "fetched; nothing to push", nil
	}
	return "fetched and pushed", nil
}

// jjSuggestInput is what a commit message is drafted from: @'s diff.
func (g Git) jjSuggestInput() (string, error) {
	stat, err := g.jj("diff", "-r", "@", "--stat")
	if err != nil {
		return "", err
	}
	patch, err := g.jj("diff", "-r", "@", "--git")
	if err != nil {
		return "", err
	}
	return string(stat) + "\n" + string(patch), nil
}

// jjWorkspaces are the repository's workspaces, the default one first, each
// with its directory and the bookmark its working copy's parent is on.
func (g Git) jjWorkspaces() ([]Worktree, error) {
	out, err := g.jj("workspace", "list", "-T", `name ++ "\t" ++ self.target().parents().map(|c| c.local_bookmarks().map(|b| b.name()).join(",")).join(",") ++ "\n"`)
	if err != nil {
		return nil, err
	}
	var all []Worktree
	for _, line := range strings.Split(string(out), "\n") {
		name, bookmark, ok := strings.Cut(line, "\t")
		if !ok || name == "" {
			continue
		}
		root, err := g.jj("workspace", "root", "--name", name)
		if err != nil {
			continue
		}
		w := Worktree{Path: strings.TrimSpace(string(root)), Branch: firstOf(bookmark)}
		if name == "default" {
			all = append([]Worktree{w}, all...)
		} else {
			all = append(all, w)
		}
	}
	return all, nil
}

// errJJMerge is what M says in jj, where there is no branch to merge.
var errJJMerge = errors.New("in jj every workspace's changes are already in the one repository: jj rebase or jj squash puts them where you want them")

// jjPublish pushes the last finished change (@-, what `jj commit` closed) on
// a bookmark jj names for it, and says the bookmark: what a pull request is
// opened from.
func (g Git) jjPublish() (string, error) {
	if !g.colocated() {
		return "", errors.New("a pull request through gh needs the jj repository colocated with git (jj git init --colocate)")
	}
	if _, err := g.jj("git", "push", "-c", "@-"); err != nil {
		return "", err
	}
	out, err := g.jj("log", "--no-graph", "-r", "@-", "-T", `local_bookmarks.map(|b| b.name()).join(",")`)
	if err != nil {
		return "", err
	}
	name := firstOf(strings.TrimSpace(string(out)))
	if name == "" {
		return "", errors.New("jj pushed the change but put no bookmark on it")
	}
	return name, nil
}
