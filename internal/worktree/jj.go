package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// In a Jujutsu repository an agent's checkout of its own is a jj workspace,
// not a git worktree: jj keeps one working-copy commit per workspace, and a
// git worktree added under it is a checkout jj does not know, whose changes
// it never snapshots. herdr has no jj; this is tend's (issue #54). The
// workspace is named after the branch a worktree would have had, so the
// space, the sidebar and `worktree.remove` speak of it the same way.

// jjFind is the jj workspace dir is in, read off the disk (a .jj directory
// above it), and the repository's main workspace, asked of jj.
func jjFind(dir string) (Repo, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Repo{}, false
	}
	top := ""
	for d := abs; ; d = filepath.Dir(d) {
		if info, err := os.Stat(filepath.Join(d, ".jj")); err == nil && info.IsDir() {
			top = d
			break
		}
		if filepath.Dir(d) == d {
			return Repo{}, false
		}
	}
	// --ignore-working-copy: finding the repository should not snapshot a
	// workspace, which is a write.
	root, err := jj(top, "--ignore-working-copy", "workspace", "root", "--name", "default")
	if err != nil {
		// jj missing, or the default workspace forgotten: this workspace
		// stands for the repository.
		root = top
	}
	return Repo{Root: root, Name: filepath.Base(root), Checkout: top, JJ: true}, true
}

// jj runs jj in a workspace and returns what it printed, or jj's reason.
func jj(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "jj", append([]string{"-R", dir, "--color=never", "--no-pager"}, args...)...)
	// jj prints paths relative to where it runs, not to -R.
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("worktree: this is a jj repository, and jj is not installed")
		}
		for _, l := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
			if rest, ok := strings.CutPrefix(l, "Error: "); ok {
				return "", errors.New("jj: " + rest)
			}
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New("jj: " + strings.SplitN(msg, "\n", 2)[0])
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// jjList is every workspace, the default one first and the rest linked.
func jjList(repo Repo) ([]Worktree, error) {
	out, err := jj(repo.Root, "--ignore-working-copy", "workspace", "list", "-T", `name ++ "\n"`)
	if err != nil {
		return nil, err
	}
	var list []Worktree
	for _, name := range strings.Split(out, "\n") {
		if name == "" {
			continue
		}
		root, err := jj(repo.Root, "--ignore-working-copy", "workspace", "root", "--name", name)
		if err != nil {
			continue
		}
		w := Worktree{Path: root, Branch: name, Linked: name != "default"}
		if name == "default" {
			list = append([]Worktree{w}, list...)
		} else {
			list = append(list, w)
		}
	}
	return list, nil
}

// jjAdd makes a workspace at path named name, its working copy on base (a
// revision, a bookmark), or where jj puts one by default: on the parents of
// the main workspace's.
func jjAdd(repo Repo, path, name, base string) error {
	args := []string{"workspace", "add", "--name", name}
	if base != "" && base != "HEAD" {
		args = append(args, "-r", base)
	}
	_, err := jj(repo.Root, append(args, path)...)
	return err
}

// jjRemove removes the workspace at path, its directory with it. Nothing is
// lost by it, which is why it needs no force: jj snapshots the working copy
// into its commit first, and the commit stays in the repository.
func jjRemove(repo Repo, path string) error {
	list, err := jjList(repo)
	if err != nil {
		return err
	}
	for _, w := range list {
		if !Same(w.Path, path) {
			continue
		}
		if !w.Linked {
			return errors.New("worktree: the default workspace is the repository's own and is not removed")
		}
		_, err := jj(repo.Root, "workspace", "remove", w.Branch)
		return err
	}
	return errors.New("worktree: no jj workspace at " + path)
}
