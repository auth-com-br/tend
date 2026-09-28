// Package worktree gives an agent its own checkout of a repository.
//
// Two agents in one checkout step on each other: one switches branch under
// the other, one's half-written change breaks the other's build. A git
// worktree is a second checkout of the same repository, sharing its history
// and nothing else, and a space rooted in one is an agent with a room of its
// own. herdr makes this one command; so does tend.
//
// Everything here is git, run the way herdr runs it (`worktree.rs`): `git -C
// <repo>` so that where tend was started from does not matter, the porcelain
// list format so that output meant for people is never parsed, and git's own
// errors passed through, since they are better than anything that would
// replace them.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// defaultPrefix is what a generated branch is filed under, herdr's.
const defaultPrefix = "worktree"

// gitTimeout bounds a git command. A worktree add on a large repository takes
// a while; one that takes this long is stuck on something — a lock, a hook
// waiting for input it will never get.
const gitTimeout = 2 * time.Minute

var (
	// ErrNotARepository means the directory is not inside a git checkout.
	ErrNotARepository = errors.New("worktree: not inside a git repository")
	// ErrDirty means git refused to remove a worktree with changes in it.
	ErrDirty = errors.New("worktree: has modified or untracked files")
)

// Worktree is one checkout of a repository.
type Worktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Bare     bool   `json:"is_bare"`
	Detached bool   `json:"is_detached"`
	Prunable bool   `json:"is_prunable"`
	// Linked is every worktree except the repository's own main checkout.
	Linked bool `json:"is_linked_worktree"`
}

// Repo is a repository, found from any directory inside any of its checkouts.
type Repo struct {
	// Root is the main checkout: where `git worktree add` is run from, and
	// what the repository is named after.
	Root string `json:"repo_root"`
	// Name is what the repository is called, for grouping its spaces.
	Name string `json:"repo_name"`
	// Checkout is the checkout the directory was in, which may be a linked
	// worktree rather than the main one.
	Checkout string `json:"source_checkout_path"`
	// Trusted runs git with the repository named safe for this call only,
	// herdr's trust_repository: a checkout owned by another user is refused
	// by git ("dubious ownership") otherwise. Nothing is written to the
	// user's git configuration.
	Trusted bool `json:"-"`
}

// Find locates the repository a directory belongs to.
//
// A linked worktree's own top level is not the repository's: its common
// directory — the one .git all worktrees share — is what leads back to the
// main checkout. Asking git for both is how a space opened in a worktree finds
// its siblings.
func Find(dir string) (Repo, error) { return FindTrusted(dir, false) }

// FindTrusted is Find with the repository trusted for the call, as herdr's
// trust_repository does.
func FindTrusted(dir string, trust bool) (Repo, error) {
	git := func(dir string, args ...string) (string, error) { return gitTrust(dir, trust, args...) }
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, fmt.Errorf("%w: %s", ErrNotARepository, dir)
	}
	common, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Repo{}, err
	}
	root := filepath.Dir(common)
	if filepath.Base(common) != ".git" {
		// A bare repository, or one with its git directory somewhere
		// unusual. The checkout itself is the best name there is.
		root = top
	}
	return Repo{Root: root, Name: filepath.Base(root), Checkout: top, Trusted: trust}, nil
}

// List returns every worktree of a repository.
func List(repo Repo) ([]Worktree, error) {
	out, err := gitTrust(repo.Root, repo.Trusted, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	list := ParsePorcelain(out)
	main := canonical(repo.Root)
	for i := range list {
		list[i].Linked = canonical(list[i].Path) != main
	}
	return list, nil
}

// ParsePorcelain reads `git worktree list --porcelain`: blocks separated by a
// blank line, one attribute per line.
func ParsePorcelain(out string) []Worktree {
	var (
		list []Worktree
		cur  *Worktree
	)
	finish := func() {
		if cur != nil && cur.Path != "" {
			list = append(list, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			finish()
			continue
		}
		if cur == nil {
			cur = &Worktree{}
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			cur.Detached = true
		case line == "bare":
			cur.Bare = true
		case strings.HasPrefix(line, "prunable"):
			cur.Prunable = true
		}
	}
	finish()
	return list
}

// Add creates a worktree at path on branch. A branch that already exists is
// checked out there; one that does not is made from base.
func Add(repo Repo, path, branch, base string) error {
	if branch == "" {
		return errors.New("worktree: a worktree needs a branch")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if branchExists(repo, branch) {
		_, err := gitTrust(repo.Root, repo.Trusted, "worktree", "add", path, branch)
		return err
	}
	if base == "" {
		base = "HEAD"
	}
	_, err := gitTrust(repo.Root, repo.Trusted, "worktree", "add", "-b", branch, path, base)
	return err
}

// Remove deletes a worktree's checkout. Without force, git refuses one with
// changes in it, and that refusal is kept as ErrDirty so the caller can offer
// to force it rather than show a raw git message.
func Remove(repo Repo, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := gitTrust(repo.Root, repo.Trusted, args...)
	if err != nil && isDirty(err.Error()) {
		return fmt.Errorf("%w: %s", ErrDirty, path)
	}
	return err
}

// isDirty recognises git's refusal to remove a worktree with changes, herdr's
// test for it.
func isDirty(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "contains modified or untracked files") &&
		strings.Contains(lower, "use --force")
}

func branchExists(repo Repo, branch string) bool {
	_, err := gitTrust(repo.Root, repo.Trusted, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// DefaultPath is where a new worktree goes: under root, by repository, by
// branch — herdr's layout, so that a directory of worktrees reads as what it
// is.
func DefaultPath(root, repoName, branch string) string {
	return filepath.Join(ExpandHome(root), repoName, Slug(branch))
}

// Slug turns a branch name into a directory name: lowercase letters and
// digits, anything else a single dash.
func Slug(branch string) string {
	var b strings.Builder
	dash := false
	for _, r := range branch {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			dash = false
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return defaultPrefix
	}
	return slug
}

// GeneratedBranch names a branch nobody named: herdr's adjective-noun-hex,
// which is memorable enough to find again and unlikely to collide.
func GeneratedBranch(seed uint64) string {
	adjectives := []string{"brave", "calm", "clear", "green", "lucky", "quiet", "rapid", "silver"}
	nouns := []string{"river", "cloud", "field", "forest", "harbor", "meadow", "stone", "valley"}
	adjective := adjectives[seed%uint64(len(adjectives))]
	noun := nouns[(seed/uint64(len(adjectives)))%uint64(len(nouns))]
	return fmt.Sprintf("%s/%s-%s-%04x", defaultPrefix, adjective, noun, seed&0xffff)
}

// ExpandHome turns a leading ~ into the home directory.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// canonical resolves symlinks so that two spellings of one directory compare
// equal. A path that cannot be resolved is compared as written.
func canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// Same reports whether two paths are the same directory.
func Same(a, b string) bool { return canonical(a) == canonical(b) }

// Within reports whether path is dir or somewhere under it, with symlinks
// resolved in both.
func Within(path, dir string) bool {
	rel, err := filepath.Rel(canonical(dir), canonical(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// git runs git in dir and returns its output, or its complaint as the error.
//
// Nothing git runs may wait for a person: a hook or a credential helper that
// prompts would hang the server behind it. GIT_TERMINAL_PROMPT=0 is git's own
// switch for that, and stdin is closed for anything else that tries.
func git(dir string, args ...string) (string, error) { return gitTrust(dir, false, args...) }

// gitTrust is git, with dir named a safe directory for this one command when
// trust is set.
func gitTrust(dir string, trust bool, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	prefix := []string{"-C", dir}
	if trust {
		prefix = []string{"-c", "safe.directory=" + dir, "-C", dir}
	}
	cmd := exec.CommandContext(ctx, "git", append(prefix, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = nil
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}
