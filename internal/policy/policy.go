// Package policy says whether an agent may run where it is about to: rules a
// team sets on its agents, such as "never on main", "always in a worktree"
// and "no more than four at once".
//
// It is tend's own, and it is pure: it is given the facts — the branch, the
// checkout, how many agents are already running — and says which rules they
// break. Finding the facts is git and the session, which is the server's
// business; what to do about a broken rule depends on who started the agent,
// which is the server's business too. tend refuses to start an agent against
// a policy, and can only report one that somebody started by typing its name
// into a shell, since it did not start it and will not kill it.
package policy

import (
	"fmt"
	"path"
	"strings"
)

// Rules name the policies, as they are written in the record.
const (
	RuleProtectedBranch = "protected_branch"
	RuleRequireWorktree = "require_worktree"
	RuleMaxAgents       = "max_agents"
)

// Policy is the rules in force. The zero Policy allows everything, which is
// what a user who has written no [policy] gets: tend never used to refuse an
// agent, and does not start to without being asked.
type Policy struct {
	// ProtectedBranches are branches agents do not work on. A name may be a
	// pattern ("release/*"), matched as a path.
	ProtectedBranches []string `toml:"protected_branches"`
	// RequireWorktree has agents that work on a repository do it in a linked
	// worktree of it, never in the main checkout. An agent outside any
	// repository is not held to it: there is nothing to keep it out of.
	RequireWorktree bool `toml:"require_worktree"`
	// MaxAgents is how many agents may run at once in a session. Zero is no
	// limit.
	MaxAgents int `toml:"max_agents"`
}

// Empty reports whether no rule is set.
func (p Policy) Empty() bool {
	return len(p.ProtectedBranches) == 0 && !p.RequireWorktree && p.MaxAgents <= 0
}

// Place is what the rules are checked against.
type Place struct {
	// InRepo is whether the agent works inside a git repository at all.
	InRepo   bool
	Branch   string
	Worktree bool
	// Running is how many other agents are running in the session.
	Running int
}

// Violation is one rule a place breaks.
type Violation struct {
	Rule    string
	Message string
}

func (v Violation) Error() string { return v.Message }

// Check is every rule the place breaks, in a fixed order.
func (p Policy) Check(at Place) []Violation {
	var out []Violation
	if at.InRepo && at.Branch != "" && p.Protected(at.Branch) {
		out = append(out, Violation{
			Rule:    RuleProtectedBranch,
			Message: fmt.Sprintf("agents do not run on %s, a protected branch", at.Branch),
		})
	}
	if p.RequireWorktree && at.InRepo && !at.Worktree {
		out = append(out, Violation{
			Rule:    RuleRequireWorktree,
			Message: "agents start in a worktree, not in the repository's main checkout",
		})
	}
	if p.MaxAgents > 0 && at.Running >= p.MaxAgents {
		out = append(out, Violation{
			Rule:    RuleMaxAgents,
			Message: fmt.Sprintf("%d agents are already running, the most allowed at once", at.Running),
		})
	}
	return out
}

// Protected reports whether a branch is one agents stay off.
func (p Policy) Protected(branch string) bool {
	for _, pattern := range p.ProtectedBranches {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == branch {
			return true
		}
		if ok, err := path.Match(pattern, branch); err == nil && ok {
			return true
		}
	}
	return false
}
