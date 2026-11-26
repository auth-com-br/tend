//go:build unix

package explorer

import (
	"context"
	"strings"
	"testing"
)

// TestThePanelPlansAndMergesAnAgentsWorktree: the plan says what the merge
// will do for the person to confirm, the main worktree is refused before
// anything runs, and finishing merges with a merge commit. If it regresses,
// the desktop's merge button asks a question whose answer can only fail,
// or merges into the wrong branch.
func TestThePanelPlansAndMergesAnAgentsWorktree(t *testing.T) {
	main, wt := agentWorktree(t)
	if _, err := NewPanel(main).PlanFinish(main, FinishMerge); err == nil || !strings.Contains(err.Error(), "main worktree") {
		t.Errorf("the main worktree merging into itself: %v", err)
	}
	p := NewPanel(wt)
	plan, err := p.PlanFinish(wt, FinishMerge)
	if err != nil || plan.What != "merge agent/fix into main" || plan.Doing != "merging…" {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if pr, err := p.PlanFinish(wt, FinishPullRequest); err != nil || pr.What != "open a pull request for agent/fix" {
		t.Errorf("pull request plan: %+v %v", pr, err)
	}
	msg, err := p.Finish(context.Background(), wt, FinishMerge)
	if err != nil || msg != "merged agent/fix into main" {
		t.Fatalf("finish: %q %v", msg, err)
	}
	if log := gitIn(t, main, "log", "-1", "--format=%s"); !strings.HasPrefix(log, "Merge branch 'agent/fix'") {
		t.Errorf("main's last commit: %s", log)
	}
}
