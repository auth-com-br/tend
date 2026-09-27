package policy

import (
	"reflect"
	"testing"
)

func rules(vs []Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

// TestNoPolicyAllowsEverything: tend never refused an agent before there were
// policies. A user who has written none must not find one refused now.
func TestNoPolicyAllowsEverything(t *testing.T) {
	var p Policy
	if !p.Empty() {
		t.Fatal("the zero policy is not empty")
	}
	if v := p.Check(Place{InRepo: true, Branch: "main", Running: 50}); len(v) != 0 {
		t.Errorf("Check = %v", v)
	}
}

// TestEachRuleIsBrokenOnlyWhereItSays: every rule, alone and together. A
// protected-branch rule that also caught feature branches, or a worktree rule
// that caught agents outside any repository, would refuse work nobody meant to
// forbid.
func TestEachRuleIsBrokenOnlyWhereItSays(t *testing.T) {
	p := Policy{ProtectedBranches: []string{"main", "release/*"}, RequireWorktree: true, MaxAgents: 2}
	for _, c := range []struct {
		name string
		at   Place
		want []string
	}{
		{"a feature branch in a worktree", Place{InRepo: true, Branch: "issue-4", Worktree: true, Running: 1}, nil},
		{"main in the main checkout", Place{InRepo: true, Branch: "main"}, []string{RuleProtectedBranch, RuleRequireWorktree}},
		{"a release branch by pattern", Place{InRepo: true, Branch: "release/2.0", Worktree: true}, []string{RuleProtectedBranch}},
		{"a feature branch in the main checkout", Place{InRepo: true, Branch: "issue-4"}, []string{RuleRequireWorktree}},
		{"outside any repository", Place{}, nil},
		{"one too many", Place{InRepo: true, Branch: "x", Worktree: true, Running: 2}, []string{RuleMaxAgents}},
	} {
		if got := rules(p.Check(c.at)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: broke %v, want %v", c.name, got, c.want)
		}
	}
}
