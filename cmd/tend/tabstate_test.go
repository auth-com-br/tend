package main

import "testing"

// TestATabsStateIsItsMostPressingAgents: a tab with a working agent and one
// waiting says waiting; finished-unseen beats working. If it regresses, the
// tab of an agent waiting for an answer shows ◐ and is passed over.
func TestATabsStateIsItsMostPressingAgents(t *testing.T) {
	if got := moreUrgent(moreUrgent("", "working"), "blocked"); got != "blocked" {
		t.Errorf("working then blocked: %q", got)
	}
	if got := moreUrgent("done", "working"); got != "done" {
		t.Errorf("done and working: %q", got)
	}
	if got := moreUrgent("", "idle"); got != "" {
		t.Errorf("idle says nothing: %q", got)
	}
}
