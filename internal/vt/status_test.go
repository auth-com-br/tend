package vt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestAProgramsOwnStatusIsReadAndItsProbeAnswered: Claude Code's OSC 7501,
// recorded from the real one (2.1.322) — its probe, then idle, working,
// blocked on a Bash permission, idle, working, done. The probe is answered
// with "OSC 7501 ; ?", without which Claude Code never sends the rest; each
// report is a new revision; the blocked one carries what it waits on, its
// message decoded; a subagent's entry, with an id, is not the pane's; a
// clear empties it. If it regresses, tend reads the agent's state off its
// screen only — where herdr's state bugs come from (discussion #5073).
func TestAProgramsOwnStatusIsReadAndItsProbeAnswered(t *testing.T) {
	recorded, err := os.ReadFile(filepath.Join("testdata", "claude-osc7501.bin"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewScreen(80, 24, 0)
	var replies []byte
	s.Reply = func(b []byte) { replies = append(replies, b...) }

	reports := bytes.SplitAfter(recorded, []byte("\x1b\\"))
	var states []string
	var blocked ProgramStatus
	last := uint64(0)
	for _, r := range reports {
		s.Write(r)
		ps, rev := s.ProgramStatus()
		if rev != last {
			states = append(states, ps.State)
			last = rev
			if ps.State == "blocked" {
				blocked = ps
			}
		}
	}
	if string(replies) != "\x1b]7501;?\x1b\\" {
		t.Errorf("the probe was answered with %q", replies)
	}
	eq(t, "states", states, []string{"idle", "working", "blocked", "idle", "working", "done"})
	if blocked.App != "claude-code" || blocked.Kind != "permission" || blocked.Msg != "approve Bash: touch osc-probe.txt" {
		t.Errorf("blocked = %+v", blocked)
	}

	s.Write([]byte("\x1b]7501;state=working:app=claude-code:id=agent-1\x1b\\"))
	if ps, rev := s.ProgramStatus(); rev != last || ps.State != "done" {
		t.Errorf("a subagent's report changed the pane's: %+v", ps)
	}
	s.Write([]byte("\x1b]7501;state=clear\a"))
	if ps, rev := s.ProgramStatus(); rev == last || ps.State != "" {
		t.Errorf("clear: %+v (rev %d)", ps, rev)
	}
}
