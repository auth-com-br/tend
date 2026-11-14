package explorer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Everything the explorer knows about git it asks git for, in the porcelain
// formats git promises to keep stable. Parsing `git status` for people, or
// reading .git by hand, is how tools end up disagreeing with git about a
// rename or a conflict.

// Change is one path git has something to say about.
type Change struct {
	// Path is relative to the repository's top, with forward slashes, as git
	// writes it. Orig is where a rename came from.
	Path, Orig string
	// Index and Work are git's two status letters: what is staged, and what
	// is changed in the working tree but not staged. '.' or ' ' is nothing.
	Index, Work byte
}

// Staged reports whether some of the change is in the index.
func (c Change) Staged() bool { return c.Index != ' ' && c.Index != '?' && c.Index != '!' }

// Unstaged reports whether some of the change is only in the working tree.
func (c Change) Unstaged() bool { return c.Work != ' ' }

// Untracked is a file git has never been told about.
func (c Change) Untracked() bool { return c.Index == '?' }

// Conflicted is a path mid-merge, which git marks with a U on either side or
// with both sides adding or deleting.
func (c Change) Conflicted() bool {
	pair := string([]byte{c.Index, c.Work})
	return c.Index == 'U' || c.Work == 'U' || pair == "AA" || pair == "DD"
}

// Letter is the one letter the explorer shows for a path, as an editor's
// source-control view does: the working tree's news first, since that is
// what was touched most recently.
func (c Change) Letter() byte {
	switch {
	case c.Conflicted():
		return '!'
	case c.Untracked():
		return 'U'
	case c.Work != ' ':
		return c.Work
	default:
		return c.Index
	}
}

// Status is the repository as `git status` sees it.
type Status struct {
	Branch         string
	Upstream       string
	Ahead, Behind  int
	Changes        []Change
	byPath         map[string]Change
	dirtyDirs      map[string]byte
	detachedOrNone bool
}

// Of is the change at a path, relative to the repository's top.
func (s *Status) Of(path string) (Change, bool) {
	if s == nil {
		return Change{}, false
	}
	c, ok := s.byPath[path]
	return c, ok
}

// DirLetter is the letter a directory shows for the changes inside it, or 0.
func (s *Status) DirLetter(dir string) byte {
	if s == nil {
		return 0
	}
	return s.dirtyDirs[dir]
}

// parseStatus reads `git status --porcelain=v2 --branch -z`.
//
// Version 2 because it says the upstream and ahead/behind on header lines,
// and version 1 hides them in a string meant for people.
func parseStatus(out []byte) *Status {
	st := &Status{byPath: map[string]Change{}, dirtyDirs: map[string]byte{}}
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		line := string(fields[i])
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			st.detachedOrNone = st.Branch == "(detached)"
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					st.Ahead = n
				} else {
					st.Behind = n
				}
			}
		case strings.HasPrefix(line, "1 "):
			// 1 XY sub mH mI mW hH hI path
			parts := strings.SplitN(line, " ", 9)
			if len(parts) == 9 {
				st.add(Change{Path: parts[8], Index: parts[1][0], Work: parts[1][1]})
			}
		case strings.HasPrefix(line, "2 "):
			// 2 XY sub mH mI mW hH hI Xscore path, then the original path as
			// the next NUL-separated field.
			parts := strings.SplitN(line, " ", 10)
			if len(parts) == 10 {
				c := Change{Path: parts[9], Index: parts[1][0], Work: parts[1][1]}
				if i+1 < len(fields) {
					c.Orig = string(fields[i+1])
					i++
				}
				st.add(c)
			}
		case strings.HasPrefix(line, "u "):
			// u XY sub m1 m2 m3 mW h1 h2 h3 path
			parts := strings.SplitN(line, " ", 11)
			if len(parts) == 11 {
				st.add(Change{Path: parts[10], Index: parts[1][0], Work: parts[1][1]})
			}
		case strings.HasPrefix(line, "? "):
			st.add(Change{Path: strings.TrimPrefix(line, "? "), Index: '?', Work: '?'})
		}
	}
	sort.Slice(st.Changes, func(a, b int) bool { return st.Changes[a].Path < st.Changes[b].Path })
	return st
}

// add records a change and marks every directory above it. v2 writes '.'
// for "nothing" where v1 wrote a space; the rest of the explorer speaks v1.
func (s *Status) add(c Change) {
	if c.Index == '.' {
		c.Index = ' '
	}
	if c.Work == '.' {
		c.Work = ' '
	}
	// An untracked directory is listed once with a trailing slash; the tree
	// wants the directory itself.
	c.Path = strings.TrimSuffix(c.Path, "/")
	s.Changes = append(s.Changes, c)
	s.byPath[c.Path] = c
	letter := c.Letter()
	for dir := filepath.ToSlash(filepath.Dir(c.Path)); dir != "." && dir != "/"; dir = filepath.ToSlash(filepath.Dir(dir)) {
		// A conflict inside outranks everything else, then any change.
		if prev := s.dirtyDirs[dir]; prev != '!' {
			s.dirtyDirs[dir] = letter
		}
	}
}

// Git runs git in one repository. The zero value is not a repository.
type Git struct {
	// Top is the repository's top directory, or "" when the explorer's
	// directory is not in one.
	Top string
	// JJ marks a Jujutsu workspace, which is asked of jj instead (jj.go).
	JJ bool
}

// FindRepo is the repository dir is in, if it is in one: a jj workspace
// before a git repository, since a colocated one is both and is jj's.
func FindRepo(dir string) Git {
	if top := findJJ(dir); top != "" {
		return Git{Top: top, JJ: true}
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return Git{}
	}
	return Git{Top: strings.TrimSpace(string(out))}
}

// ErrNoRepo is what a git action says outside a repository.
var ErrNoRepo = errors.New("not in a git repository")

// run runs git in the repository and returns what it printed, or its own
// explanation of why not.
func (g Git) run(args ...string) ([]byte, error) {
	if g.Top == "" {
		return nil, ErrNoRepo
	}
	if g.JJ && !g.colocated() {
		// What is left asked of git — history, stashes, tags — has no git
		// repository to ask in a jj one that is not colocated.
		return nil, errors.New("a jj repository with no git beside it: jj log has its history")
	}
	cmd := exec.Command("git", append([]string{"-C", g.Top}, args...)...)
	// The panel reads the status every two seconds, and a status takes the
	// index's lock to write back what it refreshed — an optional lock, which
	// this turns off, as git documents for tools that poll. With it on, the
	// user's own git add or commit, run at the same moment, failed with
	// "index.lock: File exists"; and a panel ended in the middle of one left
	// the lock behind. Locks git needs, for the panel's own add, are kept.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			// git's first line is the reason; the rest is advice.
			return out, errors.New(strings.SplitN(msg, "\n", 2)[0])
		}
		return out, err
	}
	return out, nil
}

// Status asks git for the repository's status. Ignored files are not
// listed: in a project with a node_modules they are most of the disk, and
// the tree marks them itself by asking about the entries it shows.
func (g Git) Status() (*Status, error) {
	if g.JJ {
		return g.jjStatus()
	}
	out, err := g.run("status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// Ignored reports which of the given paths (relative to the top) git
// ignores, in one call for a whole directory's entries.
func (g Git) Ignored(paths []string) map[string]bool {
	out := map[string]bool{}
	// A jj repository with no git one beside it has nobody to ask; jj
	// itself keeps ignored files out of the changes all the same.
	if g.Top == "" || len(paths) == 0 || (g.JJ && !g.colocated()) {
		return out
	}
	cmd := exec.Command("git", "-C", g.Top, "check-ignore", "-z", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	// check-ignore exits 1 when nothing matched, which is not a failure.
	res, _ := cmd.Output()
	for _, p := range bytes.Split(res, []byte{0}) {
		if len(p) > 0 {
			out[string(p)] = true
		}
	}
	return out
}

// Diff is the change to one path as a unified diff: what is staged, or
// what is not. An untracked file is shown whole, as added.
func (g Git) Diff(c Change, staged bool) (string, error) {
	if g.JJ {
		return g.jjDiff(c)
	}
	switch {
	case c.Untracked():
		// --no-index exits 1 when the files differ, which they always do.
		out, _ := g.run("diff", "--no-color", "--no-index", "--", "/dev/null", c.Path)
		return string(out), nil
	case staged:
		out, err := g.run("diff", "--no-color", "--cached", "-M", "--", c.Path)
		return string(out), err
	default:
		out, err := g.run("diff", "--no-color", "--", c.Path)
		return string(out), err
	}
}

// Stage adds a path to the index, deletions included.
func (g Git) Stage(path string) error {
	if g.JJ {
		return errNoStaging
	}
	_, err := g.run("add", "-A", "--", path)
	return err
}

// StageAll adds everything, as `git add -A`.
func (g Git) StageAll() error {
	if g.JJ {
		return errNoStaging
	}
	_, err := g.run("add", "-A")
	return err
}

// Unstage takes a path out of the index and leaves the file alone. A
// repository with no commit yet has no HEAD to reset to, and there the only
// way out of the index is rm --cached.
func (g Git) Unstage(path string) error {
	if g.JJ {
		return errNoStaging
	}
	if _, err := g.run("reset", "-q", "--", path); err != nil {
		if _, err2 := g.run("rm", "-q", "--cached", "--", path); err2 != nil {
			return err
		}
	}
	return nil
}

// Discard puts a tracked file back as the index has it. Untracked files are
// refused: there is no copy of them anywhere, and deleting one is a decision
// for a command line, not a key next to the one that stages it.
func (g Git) Discard(c Change) error {
	if g.JJ {
		return g.jjDiscard(c)
	}
	if c.Untracked() {
		return fmt.Errorf("%s is untracked; git has no copy to put back", c.Path)
	}
	_, err := g.run("checkout", "--", c.Path)
	return err
}

// Commit commits what is staged.
func (g Git) Commit(message string) error {
	if g.JJ {
		_, err := g.jj("commit", "-m", message)
		return err
	}
	_, err := g.run("commit", "-q", "-m", message)
	return err
}

// Files is every file a search can find: tracked, and untracked but not
// ignored, which is what "the project" means to the person searching it.
func (g Git) Files() ([]string, error) {
	if g.JJ {
		return g.jjFiles()
	}
	out, err := g.run("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var files []string
	seen := map[string]bool{}
	for _, p := range bytes.Split(out, []byte{0}) {
		// A file deleted but not staged is still "cached"; it cannot be
		// opened, and listing it twice (it is also "other" after a rename)
		// helps nobody.
		if len(p) > 0 && !seen[string(p)] {
			seen[string(p)] = true
			files = append(files, string(p))
		}
	}
	return files, nil
}

// Branch is a branch to switch to: a local one, or a remote one with no
// local branch of the same name yet.
type Branch struct {
	Name    string
	Remote  bool
	Current bool
}

// Branches lists the local branches, then the remote ones that no local
// branch has the name of, each by name.
func (g Git) Branches() ([]Branch, error) {
	if g.JJ {
		return g.jjBranches()
	}
	out, err := g.run("for-each-ref", "--format=%(HEAD) %(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var local, remote []Branch
	have := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(line) < 3 {
			continue
		}
		current, ref := line[0] == '*', line[2:]
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			name := strings.TrimPrefix(ref, "refs/heads/")
			have[name] = true
			local = append(local, Branch{Name: name, Current: current})
		case strings.HasPrefix(ref, "refs/remotes/"):
			name := strings.TrimPrefix(ref, "refs/remotes/")
			if strings.HasSuffix(name, "/HEAD") {
				continue
			}
			remote = append(remote, Branch{Name: name, Remote: true})
		}
	}
	var out2 []Branch
	out2 = append(out2, local...)
	for _, r := range remote {
		// origin/main when main is already here is the same branch.
		if short := r.Name[strings.Index(r.Name, "/")+1:]; !have[short] {
			out2 = append(out2, r)
		}
	}
	return out2, nil
}

// Switch changes to a branch. A remote one becomes a local branch of the
// same name that tracks it, as an editor's branch picker does.
func (g Git) Switch(b Branch) error {
	if g.JJ {
		return g.jjSwitch(b)
	}
	if b.Remote {
		local := b.Name[strings.Index(b.Name, "/")+1:]
		_, err := g.run("switch", "-c", local, "--track", b.Name)
		return err
	}
	_, err := g.run("switch", b.Name)
	return err
}

// networkEnv keeps git from asking for anything on the panel's terminal: a
// password prompt there would be drawn over the panel and answered by
// nobody. A key that needs a passphrase fails instead, and says so.
func networkEnv() []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return env
}

// runNet runs a git command that talks to a remote.
func (g Git) runNet(args ...string) error {
	if g.Top == "" {
		return ErrNoRepo
	}
	cmd := exec.Command("git", append([]string{"-C", g.Top}, args...)...)
	cmd.Env = networkEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			lines := strings.Split(msg, "\n")
			// The last line of a failed push or pull is the reason; the
			// first is often only "To origin".
			for i := len(lines) - 1; i >= 0; i-- {
				if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "hint:") {
					return errors.New(strings.TrimPrefix(l, "fatal: "))
				}
			}
		}
		return err
	}
	return nil
}

// Sync brings the branch level with its upstream, as an editor's sync
// button does: pull what is behind (fast-forward only — a merge is a
// decision, not a button), push what is ahead. A branch with no upstream is
// published to origin. It says what it did.
func (g Git) Sync(st *Status) (string, error) {
	if g.JJ {
		return g.jjSync()
	}
	if st == nil || st.Branch == "" || st.detachedOrNone {
		return "", errors.New("not on a branch")
	}
	if st.Upstream == "" {
		out, _ := g.run("remote")
		hasOrigin := false
		for _, r := range strings.Fields(string(out)) {
			hasOrigin = hasOrigin || r == "origin"
		}
		if !hasOrigin {
			return "", errors.New("no upstream, and no origin to publish to")
		}
		if err := g.runNet("push", "-u", "origin", st.Branch); err != nil {
			return "", err
		}
		return "published " + st.Branch + " to origin", nil
	}
	if err := g.runNet("fetch", "--quiet"); err != nil {
		return "", err
	}
	fresh, err := g.Status()
	if err != nil {
		return "", err
	}
	var did []string
	if fresh.Behind > 0 {
		if err := g.runNet("pull", "--ff-only", "--quiet"); err != nil {
			return "", err
		}
		did = append(did, "pulled "+strconv.Itoa(fresh.Behind))
	}
	if fresh.Ahead > 0 {
		if err := g.runNet("push", "--quiet"); err != nil {
			return strings.Join(did, ", "), err
		}
		did = append(did, "pushed "+strconv.Itoa(fresh.Ahead))
	}
	if len(did) == 0 {
		return "up to date with " + st.Upstream, nil
	}
	return strings.Join(did, ", "), nil
}
