package activity

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/auth-com-br/tend/internal/worktree"
)

// gitTimeout bounds one question to git. These are all local and quick; one
// that is not is stuck on a lock, and the record is not worth waiting for.
const gitTimeout = 10 * time.Second

// Inspect is where a directory is: its repository, checkout, branch and the
// commit checked out. A directory in no repository is a place with only a
// directory, which is still worth recording.
func Inspect(dir string) Place {
	p := Place{Dir: dir}
	if dir == "" {
		return p
	}
	repo, err := worktree.Find(dir)
	if err != nil {
		return p
	}
	p.Project, p.Checkout = repo.Root, repo.Checkout
	p.Worktree = !worktree.Same(repo.Root, repo.Checkout)
	// The branch's whole name: session.Branch keeps only the last part of
	// it, which is right for a sidebar and wrong for a record — "feature/x"
	// and "fix/x" are different branches.
	if b, err := git(repo.Checkout, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		p.Branch = b
	}
	if h, err := git(repo.Checkout, "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		p.Head = h
		if p.Branch == "" && len(h) >= 7 {
			// Detached: the commit is the only name it has.
			p.Branch = h[:7]
		}
	}
	return p
}

// Changes is what changed in a checkout since the commit a run started at:
// the files that differ — committed since, staged or not — and the files
// that are new and not ignored, with how many commits were made on top.
//
// It is what changed in the checkout while the agent ran, which is what the
// agent did unless somebody else worked in the same checkout at the same
// time. That is the reason a worktree per agent is worth a policy.
func Changes(p Place) (files []string, commits int) {
	if p.Checkout == "" || p.Head == "" {
		return nil, 0
	}
	seen := map[string]bool{}
	add := func(out string) {
		for _, f := range strings.Split(out, "\n") {
			if f = strings.TrimSpace(f); f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	if out, err := git(p.Checkout, "diff", "--name-only", p.Head); err == nil {
		add(out)
	}
	if out, err := git(p.Checkout, "ls-files", "--others", "--exclude-standard"); err == nil {
		add(out)
	}
	if out, err := git(p.Checkout, "rev-list", "--count", p.Head+"..HEAD"); err == nil {
		commits, _ = strconv.Atoi(out)
	}
	sort.Strings(files)
	return files, commits
}

// git runs git in dir and returns what it printed, trimmed. Nothing it runs
// may wait for a person, as in internal/worktree.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
